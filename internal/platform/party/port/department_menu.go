package port

// Menu theo cấu hình và nạp phòng ban mặc định qua cổng DepartmentAdmin (KNSPLIT-2 #222). pf/party giữ bảng menu,
// trạng thái phòng ban hiện hành, luật đối tượng/đơn vị và dữ liệu nhúng; binding HTTP GET /v1/menu,
// GET /v1/departments/{code}/modules và POST /v1/orgs/{ref}/departments/seed do chủ KN (internal/kn/extension/departments)
// đăng ký. MenuDocument là thân JSON KNMenu duy nhất — chủ HTTP và ca vàng web (kn-menu.json) cùng dùng.

import (
	"github.com/google/uuid"
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
	Status      string // mục phòng ban của Admin: active | not_open | suspended; mục "Sắp có": coming_soon
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
	Department   *Department
	DefaultRoute string
	Groups       []MenuGroupView
}

// DepartmentSeedResult tổng kết một lần nạp phòng ban mặc định.
type DepartmentSeedResult struct {
	Created  []Department
	Existing int
	Status   string
	// W11.06: mã tạm cũ (phong_ban_5/6) đã xóa / còn giữ trong lần nạp này.
	LegacyRetired []string
	LegacyKept    []string
}

// MenuDocument là thân JSON KNMenu (contracts/openapi/application.yaml). Trường xếp theo thứ tự chữ cái để byte trùng
// với thân cũ dựng bằng map (kn-menu.json không đổi).
type MenuDocument struct {
	Audience     string                  `json:"audience"`
	DefaultRoute string                  `json:"default_route"`
	Department   *MenuDocumentDepartment `json:"department"`
	Groups       []MenuDocumentGroup     `json:"groups"`
	MenuVersion  string                  `json:"menu_version"`
	OrgRef       *string                 `json:"org_ref"`
}

// MenuDocumentDepartment là phòng ban của menu trong MenuDocument.
type MenuDocumentDepartment struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	MenuProfile string `json:"menu_profile"`
}

// MenuDocumentGroup là một nhóm sidebar trong MenuDocument.
type MenuDocumentGroup struct {
	Key   string             `json:"key"`
	Label string             `json:"label"`
	Items []MenuDocumentItem `json:"items"`
}

// MenuDocumentItem là một mục sidebar trong MenuDocument.
type MenuDocumentItem struct {
	Key         string             `json:"key"`
	Label       string             `json:"label"`
	Route       *string            `json:"route"`
	Icon        string             `json:"icon"`
	BadgeSource *string            `json:"badge_source"`
	Enabled     bool               `json:"enabled"`
	Status      *string            `json:"status,omitempty"`
	StatusLabel *string            `json:"status_label,omitempty"`
	Children    []MenuDocumentItem `json:"children"`
}

// NewMenuDocument dựng thân JSON KNMenu từ menu đã giải.
func NewMenuDocument(v MenuView) MenuDocument {
	groups := make([]MenuDocumentGroup, 0, len(v.Groups))
	for _, g := range v.Groups {
		groups = append(groups, MenuDocumentGroup{Key: g.Key, Label: g.Label, Items: menuDocumentItems(g.Items)})
	}
	var dept *MenuDocumentDepartment
	if v.Department != nil {
		dept = &MenuDocumentDepartment{Code: v.Department.Code, Name: v.Department.Name, Status: v.Department.Status, MenuProfile: v.Department.MenuProfile}
	}
	var org *string
	if v.UnitRef != uuid.Nil {
		s := v.UnitRef.String()
		org = &s
	}
	return MenuDocument{Audience: v.Audience, DefaultRoute: v.DefaultRoute, Department: dept, Groups: groups, MenuVersion: v.Version, OrgRef: org}
}

func menuDocumentItems(items []MenuItemView) []MenuDocumentItem {
	out := make([]MenuDocumentItem, 0, len(items))
	for _, it := range items {
		out = append(out, MenuDocumentItem{Key: it.Key, Label: it.Label, Route: optString(it.Route), Icon: it.Icon,
			BadgeSource: optString(it.BadgeSource), Enabled: it.Enabled, Status: optString(it.Status), StatusLabel: optString(it.StatusLabel),
			Children: menuDocumentItems(it.Children)})
	}
	return out
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
