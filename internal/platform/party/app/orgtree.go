package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Nhập/xuất cây tổ chức bằng Excel (KN US-ADM-08). Tệp một dòng một tổ chức;
// cột "Mã tổ chức cấp trên" nối cây. Nhập đi qua đúng các use case CreateOrg,
// MoveOrg, RenameOrg nên mọi quy tắc cây, phạm vi và sổ biến động giữ nguyên.

// Giới hạn tệp cây tổ chức (xử lý đồng bộ dưới 2 giây, R-52).
const (
	MaxOrgTreeRows  = 500
	MaxOrgTreeBytes = 2 << 20
)

// DefaultOrgImportReason là lý do ghi sổ khi nhập di chuyển nhánh mà người nhập không nêu lý do.
const DefaultOrgImportReason = "Cập nhật cây tổ chức từ tệp nhập Excel"

// OrgTreeSheet là tên trang tính của tệp xuất.
const OrgTreeSheet = "Cay to chuc"

// OrgTreeHeader là tiêu đề cột của tệp xuất (cũng là tệp mẫu để nhập).
var OrgTreeHeader = []string{"Mã tổ chức", "Loại", "Tên", "Mã đơn vị hành chính", "Mã tổ chức cấp trên", "Trạng thái"}

var orgTreeAliases = map[string]string{
	"ma_to_chuc": "code", "ma": "code", "code": "code",
	"loai": "kind", "loai_to_chuc": "kind", "kind": "kind",
	"ten": "name", "ten_to_chuc": "name", "name": "name",
	"ma_don_vi_hanh_chinh": "admin_unit_code", "ma_dvhc": "admin_unit_code", "ma_don_vi": "admin_unit_code", "admin_unit_code": "admin_unit_code",
	"ma_to_chuc_cap_tren": "parent_code", "ma_cap_tren": "parent_code", "parent_code": "parent_code",
	"trang_thai": "status", "status": "status",
}

// Nhãn tiếng Việt của loại tổ chức — cũng nhận khi nhập (so sau chuẩn hóa).
var kindLabels = map[port.OrgKind]string{
	port.OrgTTKNNational: "TTKN Quốc gia", port.OrgTTKNProvince: "TTKN tỉnh", port.OrgExtensionTeam: "Tổ khuyến nông cộng đồng",
	port.OrgDCRD: "Cục KTHT và PTNT", port.OrgCooperative: "HTX/THT", port.OrgMKG: "Mekong Green", port.OrgPartner: "Đối tác",
}

// kindRank sắp nút cùng cấp khi xuất: cấp cao trước.
var kindRank = map[port.OrgKind]int{
	port.OrgTTKNNational: 0, port.OrgDCRD: 1, port.OrgMKG: 2, port.OrgTTKNProvince: 3, port.OrgExtensionTeam: 4, port.OrgCooperative: 5, port.OrgPartner: 6,
}

func parseKind(s string) (port.OrgKind, bool) {
	k := port.OrgKind(strings.ToLower(strings.TrimSpace(s)))
	if domain.KnownKind(k) {
		return k, true
	}
	n := tabular.NormalizeHeader(s)
	for kind, label := range kindLabels {
		if tabular.NormalizeHeader(label) == n {
			return kind, true
		}
	}
	return "", false
}

// ExportOrgTree xuất cây tổ chức trong phạm vi người gọi ra tệp .xlsx: nút cha
// luôn đứng trước nút con, nên tệp xuất nhập lại được ngay (khứ hồi).
func (r *Registry) ExportOrgTree(ctx context.Context) ([]byte, error) {
	p, err := authz.Require(ctx, adminRoles...)
	if err != nil {
		return nil, err
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil {
		return nil, err
	}
	var orgs []port.Org
	parentCode := map[uuid.UUID]string{}
	if !sc.Empty() {
		err = tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
			after := uuid.Nil
			for {
				page, err := store.ListOrgs(ctx, tx, store.OrgFilter{All: sc.All, Units: sc.Units, IncludeInactive: true, After: after, Limit: 500})
				if err != nil {
					return err
				}
				orgs = append(orgs, page...)
				if len(page) < 500 {
					break
				}
				after = page[len(page)-1].Ref
			}
			for _, o := range orgs {
				parentCode[o.Ref] = o.Code
			}
			// Cấp trên ngoài phạm vi (vd TTKN QG của TTKN tỉnh): chỉ cần mã để nối cây.
			for _, o := range orgs {
				if _, ok := parentCode[o.ParentRef]; o.ParentRef != uuid.Nil && !ok {
					st, err := store.OrgByRef(ctx, tx, o.ParentRef, false)
					if err != nil {
						return err
					}
					if st != nil {
						parentCode[o.ParentRef] = st.Code
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	rows := make([][]string, 0, len(orgs))
	for _, o := range treeOrder(orgs) {
		status := "Hoạt động"
		if !o.Active {
			status = "Ngừng hoạt động"
		}
		rows = append(rows, []string{o.Code, string(o.Kind), o.Name, o.AdminUnitCode, parentCode[o.ParentRef], status})
	}
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, OrgTreeSheet, OrgTreeHeader, rows); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// treeOrder sắp tổ chức theo duyệt sâu từ gốc: cha trước con; cùng cấp theo loại rồi mã.
func treeOrder(orgs []port.Org) []port.Org {
	in := map[uuid.UUID]bool{}
	for _, o := range orgs {
		in[o.Ref] = true
	}
	children := map[uuid.UUID][]port.Org{}
	var roots []port.Org
	for _, o := range orgs {
		if o.ParentRef != uuid.Nil && in[o.ParentRef] {
			children[o.ParentRef] = append(children[o.ParentRef], o)
		} else {
			roots = append(roots, o)
		}
	}
	less := func(a, b port.Org) int {
		if d := kindRank[a.Kind] - kindRank[b.Kind]; d != 0 {
			return d
		}
		return strings.Compare(a.Code, b.Code)
	}
	out := make([]port.Org, 0, len(orgs))
	var walk func(list []port.Org)
	walk = func(list []port.Org) {
		slices.SortFunc(list, less)
		for _, o := range list {
			out = append(out, o)
			walk(children[o.Ref])
		}
	}
	walk(roots)
	return out
}

// Trạng thái từng dòng nhập cây tổ chức.
const (
	OrgRowCreated   = "created"
	OrgRowUpdated   = "updated"
	OrgRowUnchanged = "unchanged"
	OrgRowRejected  = "rejected"
)

// OrgImportInput là tệp cây tổ chức (CSV UTF-8 hoặc .xlsx).
type OrgImportInput struct {
	Body    io.Reader
	Reason  string // lý do ghi sổ khi di chuyển/đổi tên; rỗng = DefaultOrgImportReason
	IdemKey string
}

// OrgImportRow là kết quả một dòng. Changes: moved, renamed.
type OrgImportRow struct {
	Row     int
	Status  string
	Code    string
	OrgRef  uuid.UUID
	Changes []string
	Reason  string
}

// OrgImportResult là kết quả cả tệp.
type OrgImportResult struct {
	Created, Updated, Unchanged, Rejected int
	Rows                                  []OrgImportRow
}

type orgTreeItem struct {
	line                         int
	code, name, unit, parentCode string
	kind                         port.OrgKind
}

// ImportOrgTree nhập cây tổ chức: mã chưa có → tạo (CreateOrg); mã đã có → di
// chuyển nếu cấp trên/đơn vị khác (MoveOrg), đổi tên nếu tên khác (RenameOrg).
// Dòng được xử lý theo thứ tự cha trước con dù tệp sắp thế nào; kết quả trả
// theo dòng của tệp. HTX mới: để trống mã (mã do hệ thống sinh, CGH BR-01).
// Tệp không vô hiệu hóa tổ chức vắng mặt; cột Trạng thái chỉ để xem.
func (r *Registry) ImportOrgTree(ctx context.Context, in OrgImportInput) (OrgImportResult, error) {
	p, err := authz.Require(ctx, adminRoles...)
	if err != nil {
		return OrgImportResult{}, err
	}
	if in.IdemKey == "" {
		return OrgImportResult{}, problem.IdempotencyRequired
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = DefaultOrgImportReason
	}
	if err := domain.ValidateMoveReason(reason); err != nil {
		return OrgImportResult{}, fieldProblem(err)
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil {
		return OrgImportResult{}, err
	}
	tb, err := readImportTable(in.Body, tabular.Limits{MaxBytes: MaxOrgTreeBytes, MaxRows: MaxOrgTreeRows})
	if err != nil {
		return OrgImportResult{}, err
	}
	col := tb.Columns(orgTreeAliases)
	if missing := tabular.Missing(col, "code", "kind", "name"); len(missing) > 0 {
		return OrgImportResult{}, ErrImportInvalid.WithDetail("Thiếu cột bắt buộc: " + strings.Join(missing, ", "))
	}
	_, hasParent := col["parent_code"]
	_, hasUnit := col["admin_unit_code"]

	results := make([]OrgImportRow, len(tb.Rows))
	items := make([]orgTreeItem, len(tb.Rows))
	pending := map[int]bool{}
	lineOfCode := map[string]int{} // mã → chỉ số dòng khai báo mã đó trong tệp
	for i, rec := range tb.Rows {
		it := orgTreeItem{
			line: rec.Line, code: rec.Get(col, "code"), name: rec.Get(col, "name"),
			unit: rec.Get(col, "admin_unit_code"), parentCode: rec.Get(col, "parent_code"),
		}
		items[i] = it
		results[i] = OrgImportRow{Row: rec.Line, Code: it.code}
		kind, ok := parseKind(rec.Get(col, "kind"))
		switch {
		case !ok:
			results[i].Status, results[i].Reason = OrgRowRejected, "Loại tổ chức \""+rec.Get(col, "kind")+"\" không hợp lệ"
			continue
		case it.code == "" && kind != port.OrgCooperative:
			results[i].Status, results[i].Reason = OrgRowRejected, "Thiếu mã tổ chức"
			continue
		case it.code != "" && it.code == it.parentCode:
			results[i].Status, results[i].Reason = OrgRowRejected, "Tổ chức không thể là cấp trên của chính nó"
			continue
		}
		if it.code != "" {
			if prev, dup := lineOfCode[it.code]; dup {
				results[i].Status, results[i].Reason = OrgRowRejected, "Mã tổ chức trùng với dòng "+strconv.Itoa(items[prev].line)+" trong cùng tệp"
				continue
			}
			lineOfCode[it.code] = i
		}
		items[i].kind = kind
		pending[i] = true
	}

	// Xử lý theo lượt: dòng sẵn sàng khi cấp trên không khai trong tệp hoặc đã xử lý xong.
	for len(pending) > 0 {
		progressed := false
		for i := range items {
			if !pending[i] {
				continue
			}
			it := items[i]
			if pi, inFile := lineOfCode[it.parentCode]; it.parentCode != "" && inFile && pending[pi] {
				continue
			} else if it.parentCode != "" && inFile && results[pi].Status == OrgRowRejected {
				results[i].Status = OrgRowRejected
				results[i].Reason = "Tổ chức cấp trên (dòng " + strconv.Itoa(items[pi].line) + ") bị lỗi"
				delete(pending, i)
				progressed = true
				continue
			}
			row, err := r.importOrgRow(ctx, p, sc, it, hasParent, hasUnit, reason, in.IdemKey)
			if err != nil {
				return OrgImportResult{}, err
			}
			results[i] = row
			delete(pending, i)
			progressed = true
		}
		if !progressed { // còn lại là vòng lặp cấp trên trong tệp
			for i := range pending {
				results[i].Status, results[i].Reason = OrgRowRejected, "Cấp trên tạo vòng lặp trong tệp"
			}
			break
		}
	}

	res := OrgImportResult{Rows: results}
	for _, row := range results {
		switch row.Status {
		case OrgRowCreated:
			res.Created++
		case OrgRowUpdated:
			res.Updated++
		case OrgRowUnchanged:
			res.Unchanged++
		default:
			res.Rejected++
		}
	}
	return res, nil
}

// importOrgRow ghi một dòng hợp lệ về cú pháp. Lỗi nghiệp vụ thành dòng bị loại;
// lỗi hệ thống trả err (dừng cả lượt — gọi lại an toàn nhờ idem_key theo dòng).
func (r *Registry) importOrgRow(ctx context.Context, p authz.Principal, sc authz.Scope, it orgTreeItem,
	hasParent, hasUnit bool, reason, key string) (OrgImportRow, error) {
	row := OrgImportRow{Row: it.line, Code: it.code}
	reject := func(err error) (OrgImportRow, error) {
		var pr *problem.Problem
		if !errors.As(err, &pr) || pr.Status >= 500 {
			return row, err
		}
		row.Status, row.Reason = OrgRowRejected, rowReason(pr)
		return row, nil
	}
	rowKey := key + ":org:" + strconv.Itoa(it.line)

	var existing, parent *port.Org
	err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if it.code != "" {
			if existing, e = store.OrgByCode(ctx, tx, it.code); e != nil {
				return e
			}
		}
		if it.parentCode != "" {
			parent, e = store.OrgByCode(ctx, tx, it.parentCode)
		}
		return e
	})
	if err != nil {
		return row, err
	}
	if it.parentCode != "" && parent == nil {
		row.Status, row.Reason = OrgRowRejected, "Không có tổ chức cấp trên mã "+it.parentCode
		return row, nil
	}
	parentRef := uuid.Nil
	if parent != nil {
		parentRef = parent.Ref
	}

	if existing == nil {
		if it.kind == port.OrgCooperative && it.code != "" {
			row.Status, row.Reason = OrgRowRejected, "Không có HTX mã "+it.code+" — để trống cột Mã tổ chức để tạo HTX mới (mã do hệ thống sinh)"
			return row, nil
		}
		out, err := r.CreateOrg(ctx, CreateOrgInput{
			Kind: it.kind, Code: it.code, Name: it.name, AdminUnitCode: it.unit, ParentRef: parentRef, IdemKey: rowKey + ":create",
		})
		if err != nil {
			return reject(err)
		}
		row.Status, row.Code, row.OrgRef = OrgRowCreated, out.Org.Code, out.Org.Ref
		return row, nil
	}

	row.OrgRef = existing.Ref
	if existing.Kind != it.kind {
		row.Status, row.Reason = OrgRowRejected, "Loại tổ chức khác với tổ chức đã có mã này ("+string(existing.Kind)+")"
		return row, nil
	}
	if !visible(sc, existing.AdminUnitCode) {
		return reject(ErrOutOfScope)
	}
	newParent := existing.ParentRef
	if hasParent {
		newParent = parentRef
	}
	newUnit := existing.AdminUnitCode
	if hasUnit && it.unit != "" {
		newUnit = it.unit
	}
	if newParent != existing.ParentRef || newUnit != existing.AdminUnitCode {
		if _, err := r.MoveOrg(ctx, MoveOrgInput{
			Ref: existing.Ref, NewParentRef: newParent, NewAdminUnitCode: newUnit, Reason: reason, IdemKey: rowKey + ":move",
		}); err != nil {
			return reject(err)
		}
		row.Changes = append(row.Changes, "moved")
	}
	if strings.TrimSpace(it.name) != existing.Name {
		if _, err := r.RenameOrg(ctx, StatusInput{Ref: existing.Ref, Name: it.name, Reason: reason, IdemKey: rowKey + ":rename"}); err != nil {
			if len(row.Changes) > 0 { // đã di chuyển: báo phần đã làm cùng lý do không đổi được tên
				out, rerr := reject(err)
				out.Reason = "Đã di chuyển; không đổi được tên: " + out.Reason
				return out, rerr
			}
			return reject(err)
		}
		row.Changes = append(row.Changes, "renamed")
	}
	row.Status = OrgRowUnchanged
	if len(row.Changes) > 0 {
		row.Status = OrgRowUpdated
	}
	return row, nil
}
