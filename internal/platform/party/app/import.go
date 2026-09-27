package app

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// MaxImportRows giới hạn số dòng xử lý đồng bộ trong một request (R-52: dưới 2 giây).
const MaxImportRows = 200

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

// Cột của tệp mẫu; chấp nhận tên tiếng Việt không dấu làm bí danh.
var importAliases = map[string]string{
	"full_name": "full_name", "ho_ten": "full_name",
	"phone": "phone", "so_dien_thoai": "phone", "sdt": "phone",
	"national_id": "national_id", "cccd": "national_id",
	"address": "address", "dia_chi": "address",
	"cooperative_code": "cooperative_code", "ma_htx": "cooperative_code",
	"commune_code": "commune_code", "ma_xa": "commune_code",
	"cultivated_area_ha": "cultivated_area_ha", "dien_tich_ha": "cultivated_area_ha",
	"member_since": "member_since", "ngay_vao_htx": "member_since",
}

var importRequired = []string{"full_name", "phone", "cooperative_code"}

// ImportHouseholds nhập nông hộ từ CSV (UTF-8, dòng đầu là tiêu đề). Tệp sai mẫu
// bị từ chối toàn bộ; dòng lỗi bị loại kèm lý do, chỉ nạp dòng hợp lệ.
func (r *Registry) ImportHouseholds(ctx context.Context, src io.Reader) (ImportResult, error) {
	p, err := authz.Require(ctx, port.RoleRegistryAdmin, port.RoleKNProvinceAdmin)
	if err != nil {
		return ImportResult{}, err
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil {
		return ImportResult{}, err
	}
	rd := csv.NewReader(src)
	rd.FieldsPerRecord = -1
	rd.TrimLeadingSpace = true
	header, err := rd.Read()
	if err != nil {
		return ImportResult{}, ErrImportInvalid.WithDetail("Tệp không đúng mẫu quy định, vui lòng tải mẫu chuẩn và thử lại")
	}
	col := map[string]int{}
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, string(rune(0xFEFF))))) // bỏ BOM của Excel
		if canon, ok := importAliases[h]; ok {
			col[canon] = i
		}
	}
	var missing []string
	for _, c := range importRequired {
		if _, ok := col[c]; !ok {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return ImportResult{}, ErrImportInvalid.WithDetail("Thiếu cột bắt buộc: " + strings.Join(missing, ", "))
	}
	records, err := rd.ReadAll()
	if err != nil {
		return ImportResult{}, ErrImportInvalid.WithDetail("Sai định dạng file")
	}
	if len(records) > MaxImportRows {
		return ImportResult{}, ErrImportInvalid.WithDetail(fmt.Sprintf("Tối đa %d dòng mỗi lần nhập", MaxImportRows))
	}

	coops := map[string]uuid.UUID{}
	var res ImportResult
	for i, rec := range records {
		row := ImportRow{Row: i + 2} // dòng 1 là tiêu đề
		in, reason := r.parseImportRow(ctx, p.TenantID, rec, col, coops)
		if reason == "" {
			out, err := r.createHousehold(ctx, p, sc, in)
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

func cell(rec []string, col map[string]int, name string) string {
	i, ok := col[name]
	if !ok || i >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[i])
}

// parseImportRow đọc một dòng; trả lý do loại (rỗng = hợp lệ).
func (r *Registry) parseImportRow(ctx context.Context, tid uuid.UUID, rec []string, col map[string]int, coops map[string]uuid.UUID) (CreateHouseholdInput, string) {
	in := CreateHouseholdInput{Identity: FarmerIdentity{
		FullName: cell(rec, col, "full_name"), Phone: cell(rec, col, "phone"),
		NationalID: cell(rec, col, "national_id"), Address: cell(rec, col, "address"),
	}, CommuneCode: cell(rec, col, "commune_code")}
	if in.Identity.FullName == "" {
		return in, "Thiếu trường bắt buộc: full_name"
	}
	if in.Identity.Phone == "" {
		return in, "Thiếu trường bắt buộc: phone"
	}
	code := cell(rec, col, "cooperative_code")
	if code == "" {
		return in, "Thiếu trường bắt buộc: cooperative_code"
	}
	if s := cell(rec, col, "cultivated_area_ha"); s != "" {
		d, err := decimal.NewFromString(strings.ReplaceAll(s, ",", "."))
		if err != nil {
			return in, "cultivated_area_ha không phải số"
		}
		in.CultivatedAreaHa = &d
	}
	if s := cell(rec, col, "member_since"); s != "" {
		t, err := time.Parse(time.DateOnly, s)
		if err != nil {
			return in, "member_since phải có dạng YYYY-MM-DD"
		}
		in.MemberSince = &t
	}
	ref, ok := coops[code]
	if !ok {
		var c *store.Cooperative
		if err := tenant.WithTenantTx(ctx, r.DB, tid, func(tx pgx.Tx) error {
			var e error
			c, e = store.CooperativeByCode(ctx, tx, code)
			return e
		}); err != nil {
			return in, "không đọc được HTX"
		}
		if c != nil {
			ref = c.Ref
		}
		coops[code] = ref
	}
	if ref == uuid.Nil {
		return in, "Không có HTX với mã " + code
	}
	in.CooperativeRef = ref
	return in, ""
}
