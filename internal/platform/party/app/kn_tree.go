package app

// Cây tổ chức 3 cấp TTKNQG → TTKN tỉnh → (Khu vực) → Xã (FN-AD-04.02), bảo đảm mỗi xã có Nút,
// bảng "Ảnh hưởng", dữ liệu nền phòng ban.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/seeddata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Loại nút trong cây trả về (ngoài loại tổ chức): nhóm khu vực — thuộc tính hiển thị như nhóm.
const (
	TreeKindRegion     = "region"
	TreeKindUnassigned = "region_unassigned"
)

// TreeNode là một nút của cây hiển thị.
type TreeNode struct {
	Ref           uuid.UUID // Nil với nhóm khu vực
	Kind          string
	Code          string
	Name          string
	AdminUnitCode string
	Level         int // 1 TTKNQG, 2 TTKN tỉnh, 3 xã; 0 với nhóm khu vực
	Active        bool
	Region        *port.CommuneRegion
	CommuneCount  int // nhóm khu vực và TTKN tỉnh: số xã
	Children      []TreeNode
}

func treeLevel(k port.OrgKind) int {
	switch k {
	case port.OrgTTKNNational:
		return 1
	case port.OrgTTKNProvince:
		return 2
	case port.OrgExtensionTeam:
		return 3
	}
	return 0
}

// OrgTree dựng cây trong phạm vi người gọi (xem xuống, không xem ngang, không xem lên): gốc là Nút
// thấy được cao nhất. Xã dưới TTKN tỉnh được nhóm theo Khu vực (sắp theo thứ tự khu vực), xã chưa gán
// vào nhóm "Chưa gán khu vực" cuối.
func (k *KN) OrgTree(ctx context.Context, includeInactive bool) ([]TreeNode, error) {
	p, err := authenticated(ctx)
	if err != nil {
		return nil, err
	}
	sc, err := k.Reg.scopeFor(ctx, p)
	if err != nil || sc.Empty() {
		return []TreeNode{}, err
	}
	var orgs []port.Org
	var regions map[uuid.UUID]port.CommuneRegion
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if orgs, e = store.TreeOrgs(ctx, tx, includeInactive); e != nil {
			return e
		}
		refs := make([]uuid.UUID, 0, len(orgs))
		for _, o := range orgs {
			if o.Kind == port.OrgExtensionTeam {
				refs = append(refs, o.Ref)
			}
		}
		regions, e = store.RegionsOfOrgs(ctx, tx, refs)
		return e
	})
	if err != nil {
		return nil, err
	}
	visibleRefs := map[uuid.UUID]bool{}
	for _, o := range orgs {
		if visible(sc, o.AdminUnitCode) {
			visibleRefs[o.Ref] = true
		}
	}
	children := map[uuid.UUID][]port.Org{}
	var roots []port.Org
	for _, o := range orgs {
		if !visibleRefs[o.Ref] {
			continue
		}
		if o.ParentRef != uuid.Nil && visibleRefs[o.ParentRef] {
			children[o.ParentRef] = append(children[o.ParentRef], o)
		} else {
			roots = append(roots, o)
		}
	}
	var build func(o port.Org) TreeNode
	build = func(o port.Org) TreeNode {
		n := TreeNode{Ref: o.Ref, Kind: string(o.Kind), Code: o.Code, Name: o.Name, AdminUnitCode: o.AdminUnitCode, Level: treeLevel(o.Kind), Active: o.Active}
		if r, ok := regions[o.Ref]; ok {
			rc := r
			n.Region = &rc
		}
		kids := children[o.Ref]
		if o.Kind != port.OrgTTKNProvince {
			for _, c := range kids {
				n.Children = append(n.Children, build(c))
			}
			return n
		}
		groups := map[string]*TreeNode{}
		sortKeys := map[string]int{}
		var order []string
		for _, c := range kids {
			cn := build(c)
			key, name, sortKey := port.Unassigned, "Chưa gán khu vực", 1<<30
			if cn.Region != nil {
				key, name, sortKey = cn.Region.RegionCode, cn.Region.RegionName, cn.Region.SortOrder
			}
			g, ok := groups[key]
			if !ok {
				kind := TreeKindRegion
				if key == port.Unassigned {
					kind = TreeKindUnassigned
				}
				g = &TreeNode{Kind: kind, Code: key, Name: name, Active: true}
				groups[key], sortKeys[key] = g, sortKey
				order = append(order, key)
			}
			g.Children = append(g.Children, cn)
		}
		sort.SliceStable(order, func(i, j int) bool {
			if sortKeys[order[i]] != sortKeys[order[j]] {
				return sortKeys[order[i]] < sortKeys[order[j]]
			}
			return order[i] < order[j]
		})
		for _, key := range order {
			g := groups[key]
			g.CommuneCount = len(g.Children)
			n.CommuneCount += g.CommuneCount
			n.Children = append(n.Children, *g)
		}
		return n
	}
	out := make([]TreeNode, 0, len(roots))
	for _, o := range roots {
		out = append(out, build(o))
	}
	return out, nil
}

// RegionsOf trả khu vực của các Nút xã (đã gán) — trang trí /v1/orgs.
func (k *KN) RegionsOf(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]port.CommuneRegion, error) {
	var out map[uuid.UUID]port.CommuneRegion
	err := tenant.FromCtxTx(ctx, k.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.RegionsOfOrgs(ctx, tx, refs)
		return e
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Bảo đảm mỗi xã của tỉnh có Nút extension_team (FN-AD-04.02; An Giang 102 xã/phường/đặc khu)
// ---------------------------------------------------------------------------

// EnsureResult tổng kết một lần bảo đảm Nút xã.
type EnsureResult struct {
	ProvinceCode string
	Communes     int
	Created      []port.Org
	Existing     int
}

// EnsureCommuneNodes tạo Nút extension_team (mã "<tiền tố>-<mã xã>", tên = tên ĐVHC) cho mọi xã còn
// hiệu lực của tỉnh của Nút province chưa có Nút. Mỗi Nút ghi sổ như tạo tay (CreateOrg); gọi lặp
// cùng Idempotency-Key không tạo thêm.
func (k *KN) EnsureCommuneNodes(ctx context.Context, province uuid.UUID, prefix, idem string) (EnsureResult, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return EnsureResult{}, err
	}
	if idem == "" {
		return EnsureResult{}, problem.IdempotencyRequired
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "XA"
	}
	if len(prefix) > 20 || strings.Trim(prefix, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.") != "" {
		return EnsureResult{}, problem.Validation.WithField("code_prefix", "tối đa 20 ký tự [A-Za-z0-9_.]")
	}
	if k.Reg.Units == nil {
		return EnsureResult{}, ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	var prov *store.OrgState
	if err := tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		prov, e = store.OrgByRef(ctx, tx, province, false)
		return e
	}); err != nil {
		return EnsureResult{}, err
	}
	if prov == nil {
		return EnsureResult{}, ErrOrgNotFound
	}
	if prov.Kind != port.OrgTTKNProvince || !prov.Active || prov.AdminUnitCode == "" {
		return EnsureResult{}, ErrInvalidHierarchy.WithDetail("chỉ bảo đảm Nút xã dưới một TTKN tỉnh đang hoạt động")
	}
	now := k.now()
	codes, err := k.Reg.Units.Descendants(ctx, prov.AdminUnitCode, now)
	if err != nil {
		return EnsureResult{}, err
	}
	communes := make([]port.AdminUnit, 0, len(codes))
	for _, c := range codes {
		if c == prov.AdminUnitCode {
			continue
		}
		u, err := k.Reg.Units.Get(ctx, c, now)
		if err != nil {
			return EnsureResult{}, err
		}
		if u.Level == port.LevelCommune {
			communes = append(communes, u)
		}
	}
	slices.SortFunc(communes, func(a, b port.AdminUnit) int { return strings.Compare(a.Code, b.Code) })
	res := EnsureResult{ProvinceCode: prov.AdminUnitCode, Communes: len(communes), Created: []port.Org{}}
	var existing map[string]uuid.UUID
	all := make([]string, 0, len(communes))
	for _, u := range communes {
		all = append(all, u.Code)
	}
	if err := tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		existing, e = store.ExtensionTeamUnits(ctx, tx, all)
		return e
	}); err != nil {
		return EnsureResult{}, err
	}
	for _, u := range communes {
		if _, ok := existing[u.Code]; ok {
			res.Existing++
			continue
		}
		r, err := k.Reg.CreateOrg(ctx, CreateOrgInput{Kind: port.OrgExtensionTeam, Code: prefix + "-" + u.Code, Name: u.Name,
			AdminUnitCode: u.Code, ParentRef: prov.Ref, IdemKey: "ensure:" + idem + ":" + u.Code})
		if err != nil {
			return res, fmt.Errorf("xã %s: %w", u.Code, err)
		}
		if r.Replayed {
			res.Existing++
			continue
		}
		res.Created = append(res.Created, r.Org)
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// Bảng "Ảnh hưởng" của một Nút (FN-AD-04.02)
// ---------------------------------------------------------------------------

// ImpactRow là một dòng bảng ảnh hưởng.
type ImpactRow struct {
	Kind      string
	Label     string
	Count     int
	AreaHa    *decimal.Decimal // thửa: tổng diện tích
	Available bool             // false = chưa nối nguồn đếm
}

// Impact là bảng ảnh hưởng khi di chuyển/vô hiệu hóa một Nút.
type Impact struct {
	Org      port.Org
	Communes []string
	Rows     []ImpactRow
	Warnings []string
	Movable  bool
}

var impactLabels = map[string]string{
	port.ImpactAccounts: "Tài khoản", port.ImpactCooperatives: "HTX", port.ImpactPlots: "Thửa ruộng", port.ImpactOpenPeriods: "Kỳ báo cáo chưa chốt",
}

// OrgImpact đếm tài khoản, HTX, thửa ruộng, kỳ chưa chốt của nhánh Nút.
func (k *KN) OrgImpact(ctx context.Context, ref uuid.UUID) (Impact, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return Impact{}, err
	}
	var o *store.OrgState
	var communes []string
	coops := 0
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if o, e = store.OrgByRef(ctx, tx, ref, false); e != nil || o == nil {
			return notFoundOr(e, ErrOrgNotFound)
		}
		if communes, e = store.SubtreeUnits(ctx, tx, ref); e != nil {
			return e
		}
		coops, e = store.CountSubtreeCooperatives(ctx, tx, ref)
		return e
	})
	if err != nil {
		return Impact{}, err
	}
	if err := k.Reg.requireWriteScope(ctx, p, o.AdminUnitCode); err != nil {
		return Impact{}, err
	}
	im := Impact{Org: o.Org, Communes: communes, Movable: o.Kind != port.OrgTTKNNational && o.Kind != port.OrgTTKNProvince}
	if !im.Movable {
		im.Warnings = append(im.Warnings, "Không di chuyển được Nút cấp 1, cấp 2 (FN-AD-04.02)")
	}
	byKind := map[string]ImpactRow{port.ImpactCooperatives: {Kind: port.ImpactCooperatives, Count: coops, Available: true}}
	for _, src := range k.Impact {
		n, err := src.OrgImpact(ctx, ref, communes)
		if err != nil {
			return Impact{}, err
		}
		row := byKind[src.ImpactKind()]
		row.Kind, row.Count, row.Available = src.ImpactKind(), row.Count+n, true
		byKind[row.Kind] = row
	}
	if k.Reg.Plots != nil {
		stats, err := k.Reg.Plots.DigitizedByCommune(ctx, communes)
		if err != nil {
			return Impact{}, err
		}
		area, n := decimal.Zero, 0
		for _, s := range stats {
			n += int(s.Plots)
			area = area.Add(s.AreaHa)
		}
		byKind[port.ImpactPlots] = ImpactRow{Kind: port.ImpactPlots, Count: n, AreaHa: &area, Available: true}
	}
	for _, kind := range []string{port.ImpactAccounts, port.ImpactCooperatives, port.ImpactPlots, port.ImpactOpenPeriods} {
		row, ok := byKind[kind]
		if !ok {
			row = ImpactRow{Kind: kind}
		}
		row.Label = impactLabels[kind]
		im.Rows = append(im.Rows, row)
		if kind == port.ImpactOpenPeriods && row.Count > 0 {
			im.Warnings = append(im.Warnings, "Nút có kỳ báo cáo chưa chốt — số liệu đã chốt giữ theo cấu trúc lúc chốt; kỳ đang mở theo cấu trúc mới")
		}
	}
	return im, nil
}

// ---------------------------------------------------------------------------
// Dữ liệu nền phòng ban (tạm — Q-02)
// ---------------------------------------------------------------------------

type deptSeedFile struct {
	Format      string `json:"format"`
	Status      string `json:"status"`
	Source      string `json:"source"`
	Departments []struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
		SortOrder   int    `json:"sort_order"`
		Status      string `json:"status"`
		MenuProfile string `json:"menu_profile"`
	} `json:"departments"`
}

// DeptSeedResult tổng kết một lần nạp phòng ban.
type DeptSeedResult struct {
	Created  []port.Department
	Existing int
	Status   string
}

// SeedDepartments khai báo 6 phòng ban mặc định (TTKN An Giang, Phòng Trồng trọt Đang áp dụng) cho
// đơn vị; phòng ban đã có mã giữ nguyên. Gọi lặp an toàn.
func (k *KN) SeedDepartments(ctx context.Context, unit uuid.UUID, idem string) (DeptSeedResult, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return DeptSeedResult{}, err
	}
	if idem == "" {
		return DeptSeedResult{}, problem.IdempotencyRequired
	}
	var f deptSeedFile
	dec := json.NewDecoder(bytes.NewReader(seeddata.Departments))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil || f.Format != "agrigreen.departments/v1" {
		return DeptSeedResult{}, fmt.Errorf("party: dữ liệu nhúng departments lỗi: %w", err)
	}
	res := DeptSeedResult{Created: []port.Department{}, Status: f.Status}
	for _, sd := range f.Departments {
		d, err := domain.NormalizeDepartment(port.Department{OrgRef: unit, Code: sd.Code, Name: sd.Name, Description: sd.Description,
			SortOrder: sd.SortOrder, Status: sd.Status, MenuProfile: sd.MenuProfile})
		if err != nil {
			return res, fmt.Errorf("party: phòng ban nhúng %s: %w", sd.Code, err)
		}
		out, replayed, err := k.createDepartment(ctx, p, d, "seed:dept:"+unit.String()+":"+d.Code)
		switch {
		case errors.Is(err, ErrDepartmentCodeTaken):
			res.Existing++
		case err != nil:
			return res, err
		case replayed:
			res.Existing++
		default:
			res.Created = append(res.Created, out)
		}
	}
	return res, nil
}
