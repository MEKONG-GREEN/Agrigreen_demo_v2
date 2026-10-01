package app

// Menu theo cấu hình (UR-KT-02, FN-CH-00.01, FN-AD-04.03): dựng từ bảng tĩnh có phiên bản
// (domain/menu.go) + trạng thái phòng ban HIỆN HÀNH trong DB ở MỖI request — bật một phòng ban hiện
// menu ngay, không phát hành lại web/app.

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// MenuQuery là tham số dựng menu.
type MenuQuery struct {
	Audience   string    // kn_tw | kn_tinh | kn_xa | admin; rỗng = suy từ ngữ cảnh phiên/vai
	Department string    // mã phòng ban; rỗng = phòng ban Đang áp dụng duy nhất của đơn vị
	UnitRef    uuid.UUID // đơn vị (Nút cấp 1/2); Nil = đơn vị của Nút người gọi
}

// MenuItemView là một mục menu đã giải điều kiện.
type MenuItemView struct {
	Key         string
	Label       string
	Route       string
	Icon        string
	BadgeSource string
	Enabled     bool
	Status      string // chỉ mục phòng ban của Admin: active | not_open | suspended
	StatusLabel string
	Children    []MenuItemView
}

// MenuGroupView là một nhóm sidebar.
type MenuGroupView struct {
	Key   string
	Label string
	Items []MenuItemView
}

// MenuView là menu của một phiên.
type MenuView struct {
	Version      string
	Audience     string
	UnitRef      uuid.UUID
	Department   *port.Department
	DefaultRoute string
	Groups       []MenuGroupView
}

// audiencesOf ánh xạ vai Keycloak hiện có sang đối tượng menu KN. platform_admin được mọi menu
// (FN-AD-04.07: Admin vào menu mọi cấp để hỗ trợ). P1 thay bằng ngữ cảnh phiên (SessionContext).
func audiencesOf(p authz.Principal) []string {
	if p.Has(authz.RolePlatformAdmin) {
		return slices.Clone(port.Audiences)
	}
	var out []string
	add := func(role, aud string) {
		if p.Has(role) || p.Has(aud) {
			out = append(out, aud)
		}
	}
	add(port.RoleKNNationalAdmin, port.AudienceTW)
	add(port.RoleKNProvinceAdmin, port.AudienceTinh)
	add(port.RoleKNOfficer, port.AudienceXa)
	return out
}

// resolveAudience chọn đối tượng menu và kiểm người gọi có vai đó.
func (k *KN) resolveAudience(ctx context.Context, p authz.Principal, q *MenuQuery) error {
	q.Audience, q.Department = strings.TrimSpace(q.Audience), strings.TrimSpace(q.Department)
	if q.Audience != "" && !domain.KnownAudience(q.Audience) {
		return problem.Validation.WithField("audience", "kn_tw | kn_tinh | kn_xa | admin")
	}
	if k.Session != nil {
		if aud, dept, unit, ok := k.Session.Current(ctx); ok {
			isAdmin := p.Has(authz.RolePlatformAdmin)
			if !isAdmin && ((q.Audience != "" && q.Audience != aud) || (q.Department != "" && q.Department != dept)) {
				return ErrMenuAudience.WithDetail("chỉ trả menu của ngữ cảnh phiên đang dùng")
			}
			if q.Audience == "" {
				q.Audience = aud
			}
			if q.Department == "" && q.Audience == aud {
				q.Department = dept
			}
			if q.UnitRef == uuid.Nil {
				q.UnitRef = unit
			}
			return nil
		}
	}
	allowed := audiencesOf(p)
	switch {
	case q.Audience == "" && p.Has(authz.RolePlatformAdmin):
		q.Audience = port.AudienceAdmin
	case q.Audience == "" && len(allowed) == 1:
		q.Audience = allowed[0]
	case q.Audience == "":
		return problem.Validation.WithField("audience", "bắt buộc khi người gọi có nhiều vai trò KN")
	}
	if !slices.Contains(allowed, q.Audience) {
		return ErrMenuAudience
	}
	return nil
}

// Menu dựng sidebar cho đối tượng + phòng ban (Application API; mọi người đã xác thực).
func (k *KN) Menu(ctx context.Context, q MenuQuery) (MenuView, error) {
	p, err := authenticated(ctx)
	if err != nil {
		return MenuView{}, err
	}
	if err := k.resolveAudience(ctx, p, &q); err != nil {
		return MenuView{}, err
	}
	var depts []port.Department
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		if q.UnitRef == uuid.Nil {
			u, err := departmentUnitTx(ctx, tx, p.OrgRef)
			switch {
			case errors.Is(err, port.ErrOrgNotFound) && q.Audience == port.AudienceAdmin:
				// Admin không thuộc Nút: mọi đơn vị có phòng ban (thường chỉ TTKN An Giang).
			case errors.Is(err, port.ErrOrgNotFound):
				return problem.Validation.WithField("org_ref", "bắt buộc khi người gọi không thuộc Nút nào")
			case err != nil:
				return err
			default:
				q.UnitRef = u
			}
		}
		var e error
		if q.UnitRef == uuid.Nil {
			depts, e = store.ListAllDepartments(ctx, tx)
		} else {
			depts, e = store.ListDepartments(ctx, tx, q.UnitRef, false)
		}
		return e
	})
	if err != nil {
		return MenuView{}, err
	}
	view := MenuView{Version: domain.MenuVersion, Audience: q.Audience, UnitRef: q.UnitRef}
	if q.Audience == port.AudienceAdmin {
		return k.adminMenu(view, depts), nil
	}
	d, err := pickDepartment(depts, q.Department)
	if err != nil {
		return MenuView{}, err
	}
	groups, err := k.departmentGroups(ctx, d, q.Audience)
	if err != nil {
		return MenuView{}, err
	}
	view.Department, view.Groups, view.DefaultRoute = &d, groups, domain.DefaultRoute(q.Audience, d.Code)
	return view, nil
}

// DepartmentModules trả cây module (menu) của MỘT phòng ban cho đối tượng — GET /v1/departments/{code}/modules.
// Với đối tượng admin: mục con của phòng ban trong nhóm "Phòng ban" (kể cả khi chưa mở, Enabled = false).
func (k *KN) DepartmentModules(ctx context.Context, code string, q MenuQuery) (MenuView, error) {
	q.Department = code
	p, err := authenticated(ctx)
	if err != nil {
		return MenuView{}, err
	}
	if err := k.resolveAudience(ctx, p, &q); err != nil {
		return MenuView{}, err
	}
	if q.Audience != port.AudienceAdmin {
		return k.Menu(ctx, q)
	}
	var d *port.Department
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		if q.UnitRef == uuid.Nil {
			u, err := departmentUnitTx(ctx, tx, p.OrgRef)
			if err != nil {
				return problem.Validation.WithField("org_ref", "bắt buộc khi người gọi không thuộc Nút nào")
			}
			q.UnitRef = u
		}
		var e error
		d, e = store.DepartmentByCode(ctx, tx, q.UnitRef, code)
		return e
	})
	if err != nil {
		return MenuView{}, err
	}
	if d == nil || d.Retired {
		return MenuView{}, ErrDepartmentNotFound
	}
	item := adminDeptItem(*d)
	return MenuView{Version: domain.MenuVersion, Audience: q.Audience, UnitRef: q.UnitRef, Department: d,
		DefaultRoute: domain.DefaultRoute(port.AudienceAdmin, ""),
		Groups:       []MenuGroupView{{Key: domain.AdminDepartmentGroupKey + "." + d.Code, Label: d.Name, Items: item.Children}}}, nil
}

func pickDepartment(depts []port.Department, code string) (port.Department, error) {
	if code == "" {
		var active []port.Department
		for _, d := range depts {
			if d.Selectable() {
				active = append(active, d)
			}
		}
		if len(active) == 1 {
			return active[0], nil
		}
		return port.Department{}, problem.Validation.WithField("department", "bắt buộc khi đơn vị có nhiều hơn một phòng ban Đang áp dụng")
	}
	for _, d := range depts {
		if d.Code == code {
			if !d.Selectable() {
				return port.Department{}, ErrDepartmentNotOpen.WithDetail(d.Name + ": " + domain.StatusLabel(d.Status))
			}
			return d, nil
		}
	}
	return port.Department{}, ErrDepartmentNotFound
}

// departmentGroups giải điều kiện mục (FormAvailability) và bỏ mục cha không còn mục con.
func (k *KN) departmentGroups(ctx context.Context, d port.Department, audience string) ([]MenuGroupView, error) {
	level := domain.LevelProvince
	if audience == port.AudienceXa {
		level = domain.LevelCommune
	}
	cache := map[string]bool{}
	allowed := func(req *domain.MenuRequirement) (bool, error) {
		if req == nil {
			return true, nil
		}
		if k.Forms == nil {
			return false, nil
		}
		lv := req.Level
		if lv == "" {
			lv = level
		}
		key := req.ReportType + "|" + lv
		if v, ok := cache[key]; ok {
			return v, nil
		}
		v, err := k.Forms.HasForms(ctx, d.Code, lv, req.ReportType)
		cache[key] = v
		return v, err
	}
	var conv func(items []domain.MenuItem) ([]MenuItemView, error)
	conv = func(items []domain.MenuItem) ([]MenuItemView, error) {
		out := []MenuItemView{}
		for _, it := range items {
			ok, err := allowed(it.Requires)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			kids, err := conv(it.Children)
			if err != nil {
				return nil, err
			}
			if len(it.Children) > 0 && len(kids) == 0 {
				continue
			}
			out = append(out, MenuItemView{Key: it.Key, Label: it.Label, Route: it.Route, Icon: it.Icon, BadgeSource: it.BadgeSource,
				Enabled: true, Children: kids})
		}
		return out, nil
	}
	var out []MenuGroupView
	for _, g := range domain.DepartmentMenu(d.MenuProfile, audience, d.Code) {
		items, err := conv(g.Items)
		if err != nil {
			return nil, err
		}
		if len(items) > 0 {
			out = append(out, MenuGroupView{Key: g.Key, Label: g.Label, Items: items})
		}
	}
	return out, nil
}

func plainItems(items []domain.MenuItem, enabled bool) []MenuItemView {
	out := []MenuItemView{}
	for _, it := range items {
		out = append(out, MenuItemView{Key: it.Key, Label: it.Label, Route: it.Route, Icon: it.Icon, BadgeSource: it.BadgeSource,
			Enabled: enabled, Children: plainItems(it.Children, enabled)})
	}
	return out
}

func adminDeptItem(d port.Department) MenuItemView {
	item := MenuItemView{Key: domain.AdminDepartmentGroupKey + "." + d.Code, Label: d.Name, Icon: "building", Enabled: d.Selectable(),
		Status: d.Status, StatusLabel: domain.StatusLabel(d.Status)}
	item.Children = plainItems(domain.AdminDepartmentItems(d.MenuProfile, d.Code), d.Selectable())
	return item
}

// adminMenu: Điều hành › Tổng quan hệ thống; Phòng ban › mỗi phòng ban (chưa mở: mờ, không mục con
// dùng được); Quản trị hệ thống (Hình 3c).
func (k *KN) adminMenu(view MenuView, depts []port.Department) MenuView {
	deptItems := make([]MenuItemView, 0, len(depts))
	for _, d := range depts {
		deptItems = append(deptItems, adminDeptItem(d))
	}
	view.DefaultRoute = domain.DefaultRoute(port.AudienceAdmin, "")
	view.Groups = []MenuGroupView{
		{Key: domain.AdminOverview.Key, Label: domain.AdminOverview.Label, Items: plainItems(domain.AdminOverview.Items, true)},
		{Key: domain.AdminDepartmentGroupKey, Label: domain.AdminDepartmentGroupLabel, Items: deptItems},
		{Key: domain.AdminSystem.Key, Label: domain.AdminSystem.Label, Items: plainItems(domain.AdminSystem.Items, true)},
	}
	return view
}
