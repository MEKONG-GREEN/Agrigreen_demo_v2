package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// MaxCoopImportRows giới hạn số dòng của một lần nhập HTX (mỗi dòng một lần gọi kho
// định danh cho người đại diện — giữ request dưới vài giây, R-52).
const MaxCoopImportRows = 200

// Trạng thái từng dòng nhập HTX (HTX US-HTX-02 AC-2).
const (
	CoopRowCreated  = "created"
	CoopRowRejected = "rejected"
)

// Cột của tệp mẫu nhập HTX. Khóa là tiêu đề đã chuẩn hóa (tabular.NormalizeHeader).
var coopImportAliases = map[string]string{
	"ten_htx": "name", "ten": "name", "name": "name", "ten_hop_tac_xa": "name",
	"dia_chi": "address", "address": "address", "dia_chi_tru_so": "address",
	"nguoi_dai_dien": "representative_name", "representative_name": "representative_name", "representative": "representative_name",
	"sdt": "representative_phone", "so_dien_thoai": "representative_phone", "sdt_nguoi_dai_dien": "representative_phone",
	"phone": "representative_phone", "representative_phone": "representative_phone",
	"ma_xa": "admin_unit_code", "admin_unit_code": "admin_unit_code", "commune_code": "admin_unit_code",
	"ma_so_thue": "tax_code", "mst": "tax_code", "tax_code": "tax_code",
	"dien_tich_ha": "area_ha", "area_ha": "area_ha", "dien_tich_dang_ky_ha": "area_ha",
	"ngay_thanh_lap": "established_on", "established_on": "established_on",
	"ma_to_chuc_cap_tren": "parent_code", "parent_code": "parent_code", "ma_cap_tren": "parent_code",
}

// coopImportRequired: cột bắt buộc theo AC-2 (Tên, Địa chỉ, Người đại diện, SĐT) và Mã xã
// (HTX phải gắn một xã để tính phạm vi, ADR-0009) — kèm tên cột hiển thị trong lý do.
var coopImportRequired = []struct{ key, label string }{
	{"name", "Tên HTX"},
	{"address", "Địa chỉ"},
	{"representative_name", "Người đại diện"},
	{"representative_phone", "SĐT"},
	{"admin_unit_code", "Mã xã"},
}

// CoopImportHeader là tiêu đề của tệp mẫu (GET /v1/cooperatives/import-template.xlsx).
var CoopImportHeader = []string{"Tên HTX", "Địa chỉ", "Người đại diện", "SĐT", "Mã xã", "Mã số thuế", "Diện tích (ha)", "Ngày thành lập", "Mã tổ chức cấp trên"}

// CoopImportRow là kết quả một dòng. Reason không chứa dữ liệu cá nhân (R-14).
type CoopImportRow struct {
	Row    int
	Status string
	Reason string
	OrgRef uuid.UUID
	Code   string
	Name   string // tên HTX (không phải dữ liệu cá nhân)
}

// CoopImportResult là kết quả cả tệp.
type CoopImportResult struct {
	Created, Rejected int
	Rows              []CoopImportRow
}

// CooperativeImportTemplate trả tệp .xlsx mẫu (một dòng ví dụ) để điền danh sách HTX.
func (r *Registry) CooperativeImportTemplate(ctx context.Context) ([]byte, error) {
	if _, err := authz.Require(ctx, adminRoles...); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err := tabular.WriteXLSX(&buf, "HTX", CoopImportHeader, [][]string{
		{"HTX Nông nghiệp Ví dụ", "Ấp 1, xã …", "Nguyễn Văn A", "0901234567", "<mã xã>", "", "50", "2020-01-15", ""},
	})
	return buf.Bytes(), err
}

// ImportCooperatives nhập hồ sơ HTX hàng loạt từ .xlsx/CSV (HTX US-HTX-02): mỗi dòng
// hợp lệ tạo một HTX (Mã HTX hệ thống sinh), người đại diện + SĐT đi thẳng vào kho
// định danh (không bao giờ ở pf). Tệp không đọc được → 422 "Sai định dạng file";
// thiếu cột bắt buộc hoặc ô bắt buộc trống → từng dòng bị loại với lý do
// "Thiếu cột bắt buộc: <tên cột>"; dòng hợp lệ khác vẫn được tạo.
//
// Mỗi dòng ghi sổ với idem_key = <Idempotency-Key>:row:<n>, nên gọi lặp cùng khóa
// (kể cả sau khi tiến trình chết giữa chừng) không tạo trùng.
func (r *Registry) ImportCooperatives(ctx context.Context, src io.Reader, idemKey string) (CoopImportResult, error) {
	p, err := authz.Require(ctx, adminRoles...)
	if err != nil {
		return CoopImportResult{}, err
	}
	if idemKey == "" {
		return CoopImportResult{}, problem.IdempotencyRequired
	}
	tb, err := tabular.Read(src, tabular.Limits{MaxBytes: MaxImportBytes, MaxRows: MaxCoopImportRows})
	if err != nil {
		if pe, ok := asImportInvalid(err); ok {
			return CoopImportResult{}, pe
		}
		return CoopImportResult{}, err
	}
	col := tb.Columns(coopImportAliases)
	var missingCols []string
	for _, rq := range coopImportRequired {
		if _, ok := col[rq.key]; !ok {
			missingCols = append(missingCols, rq.label)
		}
	}
	parents := map[string]uuid.UUID{}
	var res CoopImportResult
	for _, rec := range tb.Rows {
		row := CoopImportRow{Row: rec.Line, Name: rec.Get(col, "name")}
		var reason string
		if len(missingCols) > 0 {
			reason = "Thiếu cột bắt buộc: " + strings.Join(missingCols, ", ")
		} else {
			in, why := r.parseCoopRow(ctx, p.TenantID, rec, col, parents)
			reason = why
			if reason == "" {
				in.IdemKey = fmt.Sprintf("%s:row:%d", idemKey, rec.Line)
				out, err := r.createCoopUnique(ctx, in)
				switch {
				case err == nil:
					row.Status, row.OrgRef, row.Code = CoopRowCreated, out.Org.Ref, out.Org.Code
				default:
					var pr *problem.Problem
					if !errors.As(err, &pr) || pr.Status >= 500 {
						return CoopImportResult{}, err // lỗi hệ thống: dừng, client gọi lại cùng khóa
					}
					reason = coopRowReason(pr)
				}
			}
		}
		if reason != "" {
			row.Status, row.Reason = CoopRowRejected, reason
			res.Rejected++
		} else {
			res.Created++
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

// asImportInvalid quy lỗi đọc tệp về party.import_invalid "Sai định dạng file: …".
func asImportInvalid(err error) (error, bool) {
	var fe *tabular.FormulaError
	switch {
	case errors.As(err, &fe), errors.Is(err, tabular.ErrTooManyRows), errors.Is(err, tabular.ErrTooLarge),
		errors.Is(err, tabular.ErrTooManyColumns), errors.Is(err, tabular.ErrEncoding), errors.Is(err, tabular.ErrLegacyExcel),
		errors.Is(err, tabular.ErrEmpty), errors.Is(err, tabular.ErrFormat):
		return ErrImportInvalid.WithDetail("Sai định dạng file: " + tabular.Message(err, tabular.Limits{MaxBytes: MaxImportBytes, MaxRows: MaxCoopImportRows})), true
	}
	return nil, false
}

// coopRowReason là lý do loại một dòng; không lặp lại giá trị người dùng nhập (có thể là PII).
func coopRowReason(pr *problem.Problem) string {
	switch pr.Code {
	case ErrCoopNameTaken.Code:
		return "Tên HTX này đã tồn tại, vui lòng chọn tên khác."
	case ErrTaxCodeTaken.Code:
		return "Mã số thuế đã gắn với HTX khác"
	}
	return rowReason(pr)
}

// parseCoopRow đọc một dòng; trả lý do loại (rỗng = hợp lệ).
func (r *Registry) parseCoopRow(ctx context.Context, tid uuid.UUID, rec tabular.Row, col map[string]int, parents map[string]uuid.UUID) (CreateOrgInput, string) {
	cell := func(k string) string { return strings.TrimSpace(rec.Get(col, k)) }
	var empty []string
	for _, rq := range coopImportRequired {
		if cell(rq.key) == "" {
			empty = append(empty, rq.label)
		}
	}
	if len(empty) > 0 {
		return CreateOrgInput{}, "Thiếu cột bắt buộc: " + strings.Join(empty, ", ")
	}
	prof := domain.CooperativeProfile{TaxCode: cell("tax_code"), Address: cell("address")}
	if s := cell("area_ha"); s != "" {
		d, err := decimal.NewFromString(strings.ReplaceAll(s, ",", "."))
		if err != nil {
			return CreateOrgInput{}, "Diện tích (ha) không phải số"
		}
		prof.AreaHa = &d
	}
	if s := cell("established_on"); s != "" {
		t, err := time.Parse(time.DateOnly, s)
		if err != nil {
			return CreateOrgInput{}, "Ngày thành lập phải có dạng YYYY-MM-DD"
		}
		prof.EstablishedOn = &t
	}
	in := CreateOrgInput{
		Kind: port.OrgCooperative, Name: cell("name"), AdminUnitCode: cell("admin_unit_code"), Cooperative: &prof,
		Representative: &Representative{FullName: cell("representative_name"), Phone: cell("representative_phone")},
	}
	if code := cell("parent_code"); code != "" {
		ref, ok := parents[code]
		if !ok {
			var o *port.Org
			if err := tenant.WithTenantTx(ctx, r.DB, tid, func(tx pgx.Tx) error {
				var e error
				o, e = store.OrgByCode(ctx, tx, code)
				return e
			}); err != nil {
				return in, "không đọc được tổ chức cấp trên"
			}
			if o != nil {
				ref = o.Ref
			}
			parents[code] = ref
		}
		if ref == uuid.Nil {
			return in, "Không có tổ chức cấp trên với mã " + code
		}
		in.ParentRef = ref
	}
	return in, ""
}

// createCoopUnique tạo HTX với tên không trùng: kiểm sớm (trước khi ghi người đại diện
// vào kho định danh) rồi kiểm lại dưới khóa trong giao dịch ghi sổ.
func (r *Registry) createCoopUnique(ctx context.Context, in CreateOrgInput) (OrgResult, error) {
	p, err := authz.Require(ctx, adminRoles...)
	if err != nil {
		return OrgResult{}, err
	}
	in.UniqueName = true
	// Gọi lặp cùng khóa: trả kết quả cũ, không báo "trùng tên" với chính bản đã tạo.
	if prev, err := r.replay(ctx, p.TenantID, in.IdemKey); err != nil {
		return OrgResult{}, err
	} else if prev != nil {
		return *prev, nil
	}
	var owner uuid.UUID
	if err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		owner, e = store.CooperativeNameOwner(ctx, tx, in.Name)
		return e
	}); err != nil {
		return OrgResult{}, err
	}
	if owner != uuid.Nil {
		return OrgResult{}, ErrCoopNameTaken.With("existing_ref", owner.String())
	}
	return r.CreateOrg(ctx, in)
}

// CopyCooperativeInput: sao chép hồ sơ HTX (HTX US-HTX-03). Chỉ Tên và Người đại diện
// được sửa riêng; người đại diện của hồ sơ nguồn KHÔNG được sao chép.
type CopyCooperativeInput struct {
	SourceRef      uuid.UUID
	Name           string
	Representative *Representative // nil = bản sao chưa có người đại diện
	IdemKey        string
}

// CopyCooperative tạo HTX mới giữ các trường chung của hồ sơ nguồn — địa bàn (mã xã,
// địa chỉ), cấu trúc (tổ chức cấp trên), diện tích đăng ký, ngày thành lập. Không
// sao chép: Mã HTX (hệ thống sinh mới), mã số thuế (duy nhất theo HTX), toạ độ trụ
// sở, người đại diện. Tên trùng → 409 party.cooperative_name_taken (AC-3).
func (r *Registry) CopyCooperative(ctx context.Context, in CopyCooperativeInput) (OrgResult, error) {
	p, err := authz.Require(ctx, adminRoles...)
	if err != nil {
		return OrgResult{}, err
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil {
		return OrgResult{}, err
	}
	var src *store.Cooperative
	if err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		src, e = store.CooperativeByRef(ctx, tx, in.SourceRef)
		return e
	}); err != nil {
		return OrgResult{}, err
	}
	if src == nil || !visible(sc, src.AdminUnitCode) {
		return OrgResult{}, ErrOrgNotFound
	}
	prof := domain.CooperativeProfile{AreaHa: src.Profile.AreaHa, EstablishedOn: src.Profile.EstablishedOn, Address: src.Profile.Address}
	parent := src.ParentRef
	if parent != uuid.Nil {
		// Cấp trên đã ngừng hoạt động → bản sao đặt ở gốc thay vì chặn cả thao tác.
		var st *store.OrgState
		if err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
			var e error
			st, e = store.OrgByRef(ctx, tx, parent, false)
			return e
		}); err != nil {
			return OrgResult{}, err
		}
		if st == nil || !st.Active {
			parent = uuid.Nil
		}
	}
	return r.createCoopUnique(ctx, CreateOrgInput{
		Kind: port.OrgCooperative, Name: in.Name, AdminUnitCode: src.AdminUnitCode, ParentRef: parent,
		Representative: in.Representative, Cooperative: &prof, IdemKey: in.IdemKey,
	})
}
