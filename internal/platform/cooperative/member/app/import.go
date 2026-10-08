package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// MaxImportRows giới hạn số dòng xử lý đồng bộ trong một request (R-52: dưới 2 giây).
const MaxImportRows = 200

// MaxImportBytes giới hạn dung lượng tệp nhập (CSV hoặc .xlsx).
const MaxImportBytes = 2 << 20

// Trạng thái từng dòng nhập (KN US-HTX-03, HTX US-HH-02 AC-3).
const (
	RowAccepted  = "accepted"
	RowDuplicate = "duplicate"
	RowRejected  = "rejected"
)

// ImportRow là kết quả một dòng. Reason không bao giờ chứa dữ liệu cá nhân (R-14).
type ImportRow struct {
	Row          int
	Status       string
	Reason       string
	HouseholdRef uuid.UUID
}

// ImportResult là kết quả cả tệp.
type ImportResult struct {
	Accepted, Duplicate, Rejected int
	Rows                          []ImportRow
}

// Cột của tệp mẫu. Khóa là tiêu đề đã chuẩn hóa (tabular.NormalizeHeader), nên
// tiêu đề có dấu như "Họ tên", "Mã HTX", "Ngày vào HTX" cũng khớp.
var importAliases = map[string]string{
	"full_name": "full_name", "ho_ten": "full_name", "ho_va_ten": "full_name",
	"phone": "phone", "so_dien_thoai": "phone", "sdt": "phone",
	"national_id": "national_id", "cccd": "national_id",
	"address": "address", "dia_chi": "address",
	"cooperative_code": "cooperative_code", "ma_htx": "cooperative_code",
	"commune_code": "commune_code", "ma_xa": "commune_code",
	"cultivated_area_ha": "cultivated_area_ha", "dien_tich_ha": "cultivated_area_ha",
	"member_since": "member_since", "ngay_vao_htx": "member_since",
}

var importRequired = []string{"full_name", "phone", "cooperative_code"}

// readImportTable đọc tệp CSV/.xlsx và quy lỗi đọc về party.import_invalid kèm lý do.
func readImportTable(src io.Reader, lim tabular.Limits) (*tabular.Table, error) {
	tb, err := tabular.Read(src, lim)
	if err == nil {
		return tb, nil
	}
	var fe *tabular.FormulaError
	switch {
	case errors.As(err, &fe), errors.Is(err, tabular.ErrTooManyRows), errors.Is(err, tabular.ErrTooLarge),
		errors.Is(err, tabular.ErrTooManyColumns), errors.Is(err, tabular.ErrEncoding), errors.Is(err, tabular.ErrLegacyExcel),
		errors.Is(err, tabular.ErrEmpty), errors.Is(err, tabular.ErrFormat):
		return nil, ErrImportInvalid.WithDetail(tabular.Message(err, lim))
	}
	return nil, err
}

// ImportHouseholds nhập nông hộ từ CSV (UTF-8) hoặc Excel .xlsx (trang tính đầu,
// nhận theo chữ ký tệp); dòng đầu là tiêu đề, ô công thức bị từ chối. Tệp sai
// mẫu bị từ chối toàn bộ; dòng lỗi bị loại kèm lý do, chỉ nạp dòng hợp lệ.
func (s *Service) ImportHouseholds(ctx context.Context, src io.Reader) (ImportResult, error) {
	p, err := authz.Require(ctx, partyport.RoleRegistryAdmin, partyport.RoleKNProvinceAdmin)
	if err != nil {
		return ImportResult{}, err
	}
	sc, err := s.scopeFor(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	tb, err := readImportTable(src, tabular.Limits{MaxBytes: MaxImportBytes, MaxRows: MaxImportRows})
	if err != nil {
		return ImportResult{}, err
	}
	col := tb.Columns(importAliases)
	if missing := tabular.Missing(col, importRequired...); len(missing) > 0 {
		return ImportResult{}, ErrImportInvalid.WithDetail("Thiếu cột bắt buộc: " + strings.Join(missing, ", "))
	}

	coops := map[string]uuid.UUID{}
	var res ImportResult
	for _, rec := range tb.Rows {
		row := ImportRow{Row: rec.Line} // số dòng trong tệp (dòng 1 là tiêu đề)
		in, reason := s.parseImportRow(ctx, p.TenantID, rec, col, coops)
		if reason == "" {
			in.Source = port.HouseholdSourceImport
			out, err := s.createHousehold(ctx, p, sc, in)
			switch {
			case err == nil && out.Duplicate:
				row.Status, row.Reason, row.HouseholdRef = RowDuplicate, DuplicateLabel, out.Household.Ref
			case err == nil:
				row.Status, row.HouseholdRef = RowAccepted, out.Household.Ref
			default:
				var pr *problem.Problem
				if !errors.As(err, &pr) || pr.Status >= 500 {
					return ImportResult{}, err // lỗi hệ thống: dừng, client thử lại (idempotent nhờ chống trùng)
				}
				reason = rowReason(pr)
			}
		}
		if reason != "" {
			row.Status, row.Reason = RowRejected, reason
		}
		switch row.Status {
		case RowAccepted:
			res.Accepted++
		case RowDuplicate:
			res.Duplicate++
		default:
			res.Rejected++
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

func rowReason(pr *problem.Problem) string {
	if len(pr.Errors) > 0 {
		return pr.Errors[0].Field + ": " + pr.Errors[0].Reason
	}
	if pr.Detail != "" {
		return pr.Detail
	}
	return pr.Title
}

// parseImportRow đọc một dòng; trả lý do loại (rỗng = hợp lệ).
func (s *Service) parseImportRow(ctx context.Context, tid uuid.UUID, rec tabular.Row, col map[string]int, coops map[string]uuid.UUID) (CreateHouseholdInput, string) {
	cell := func(name string) string { return rec.Get(col, name) }
	in := CreateHouseholdInput{Identity: FarmerIdentity{
		FullName: cell("full_name"), Phone: cell("phone"),
		NationalID: cell("national_id"), Address: cell("address"),
	}, CommuneCode: cell("commune_code")}
	if in.Identity.FullName == "" {
		return in, "Thiếu trường bắt buộc: full_name"
	}
	if in.Identity.Phone == "" {
		return in, "Thiếu trường bắt buộc: phone"
	}
	code := cell("cooperative_code")
	if code == "" {
		return in, "Thiếu trường bắt buộc: cooperative_code"
	}
	if s := cell("cultivated_area_ha"); s != "" {
		d, err := decimal.NewFromString(strings.ReplaceAll(s, ",", "."))
		if err != nil {
			return in, "cultivated_area_ha không phải số"
		}
		in.CultivatedAreaHa = &d
	}
	if s := cell("member_since"); s != "" {
		t, err := time.Parse(time.DateOnly, s)
		if err != nil {
			return in, "member_since phải có dạng YYYY-MM-DD"
		}
		in.MemberSince = &t
	}
	ref, ok := coops[code]
	if !ok {
		if s.Codes == nil {
			return in, "không đọc được HTX"
		}
		o, err := s.Codes.ByCode(tenant.WithID(ctx, tid), code)
		switch {
		case errors.Is(err, partyport.ErrOrgNotFound):
		case err != nil:
			return in, "không đọc được HTX"
		case o.Kind == partyport.OrgCooperative:
			ref = o.Ref
		}
		coops[code] = ref
	}
	if ref == uuid.Nil {
		return in, "Không có HTX với mã " + code
	}
	in.CooperativeRef = ref
	return in, ""
}
