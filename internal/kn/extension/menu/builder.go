package menu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Khóa/nhãn và hằng trình bày của cây KN (khớp hợp đồng web kn-menu.json).
const (
	groupOperations      = "dieu_hanh"
	groupBusiness        = "nghiep_vu"
	groupBusinessLabel   = "Nghiệp vụ"
	groupReports         = "bao_cao"
	groupDepartments     = "phong_ban"
	groupDepartmentLabel = "Phòng ban"
	statusComingSoon     = "coming_soon"
	statusComingSoonText = "Sắp có"
	levelProvince        = "province"
	levelCommune         = "commune"
	needsManager         = "manager"
	departmentToken      = "{department}"
	departmentPrefix     = "/kn/phong-ban/"
	// Route chưa có module tenant trong menuModules thuộc lõi luôn bật (không bị lọc — như compatibility).
	fallbackModule = "pf/party"
)

// fallbackOwners: tiền tố route không có ánh xạ module tenant nhưng có module sở hữu luôn bật rõ hơn pf/party.
var fallbackOwners = map[string]string{"/kn/dung-chung": "pf/contact"}

// ErrDenied là lỗi từ chối dựng menu: phiên/đối tượng/phòng ban/bộ menu không hợp lệ với chế độ yêu cầu.
// Lỗi là kết quả cuối — không fallback sang cây khác, realm khác hay phiên khác.
var ErrDenied = problem.Forbidden.WithDetail("ngữ cảnh menu không hợp lệ")

type requirement struct {
	ReportType string `json:"ReportType"`
	Level      string `json:"Level"`
}

type item struct {
	Key         string       `json:"Key"`
	Label       string       `json:"Label"`
	Route       string       `json:"Route"`
	Icon        string       `json:"Icon"`
	BadgeSource string       `json:"BadgeSource"`
	Requires    *requirement `json:"Requires"`
	ComingSoon  bool         `json:"ComingSoon"`
	Needs       string       `json:"Needs"`
	Children    []item       `json:"Children"`
}

type group struct {
	Key   string `json:"Key"`
	Label string `json:"Label"`
	Items []item `json:"Items"`
}

type tables struct {
	TrongTrot            map[string][]group           `json:"trongTrotMenus"`
	Generic              map[string][]group           `json:"genericMenus"`
	AdminDeptItems       map[string][]item            `json:"adminDeptItems"`
	AdminOverview        group                        `json:"AdminOverview"`
	AdminSystem          group                        `json:"AdminSystem"`
	DefaultRoutes        map[string]string            `json:"defaultRoutes"`
	ContextDefaultRoutes map[string]map[string]string `json:"contextDefaultRoutes"`
	MenuProfiles         []string                     `json:"MenuProfiles"`
	KHTCReportProfiles   map[string]bool              `json:"khtcReportProfiles"`
	KHTCReportItem       item                         `json:"KHTCReportItem"`
	KHTCReportAdminItem  item                         `json:"KHTCReportAdminItem"`
	SharedGroup          group                        `json:"SharedGroup"`
	SharedByAudience     map[string][]item            `json:"sharedByAudience"`
	DeptBusiness         map[string]map[string][]item `json:"deptBusiness"`
	AdminDeptBusiness    map[string][]item            `json:"adminDeptBusiness"`
	DeptProfileByCode    map[string]string            `json:"deptProfileByCode"`
	MenuModules          map[string]string            `json:"menuModules"`
	MenuDeptModules      map[string]string            `json:"menuDeptModules"`
}

// Catalog là cây menu KN đã biên dịch (bất biến) + nguồn FormAvailability nối muộn ở gốc lắp ráp.
type Catalog struct {
	t        tables
	profiles map[string]map[string][]group
	routes   []partyport.MenuRoute

	mu    sync.RWMutex
	forms partyport.FormAvailability
}

// New biên dịch các tài liệu nguồn (của ứng dụng KN) thành Catalog. Thiếu bảng → lỗi (không cây rỗng âm thầm).
func New(sources ...[]byte) (*Catalog, error) {
	compiled, err := Compile(sources...)
	if err != nil {
		return nil, err
	}
	for _, name := range tableNames {
		if _, ok := compiled[name]; !ok {
			return nil, fmt.Errorf("%w: missing menu table %s", ErrSource, name)
		}
	}
	raw, err := json.Marshal(compiled)
	if err != nil {
		return nil, err
	}
	c := &Catalog{}
	if err := json.Unmarshal(raw, &c.t); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSource, err)
	}
	c.profiles = make(map[string]map[string][]group, len(c.t.MenuProfiles))
	for _, profile := range c.t.MenuProfiles {
		switch profile {
		case partyport.MenuProfileTrongTrot:
			c.profiles[profile] = c.t.TrongTrot
		case partyport.MenuProfileGeneric:
			c.profiles[profile] = c.t.Generic
		default:
			c.profiles[profile] = c.withBusiness(profile)
		}
	}
	if c.routes, err = c.ownership(); err != nil {
		return nil, err
	}
	return c, nil
}

// UseFormAvailability nối nguồn "phòng ban đã có mẫu báo cáo" (P3) cho mục có Requires. Nil = chưa có mẫu nào
// (mục có Requires bị ẩn — như compatibility). Gọi ở gốc lắp ráp trước khi phục vụ.
func (c *Catalog) UseFormAvailability(forms partyport.FormAvailability) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forms = forms
}

// Routes trả bản sao đăng ký quyền sở hữu route ↔ module (mỗi route một lần, kể cả khi nhiều mục trỏ tới).
func (c *Catalog) Routes() []partyport.MenuRoute { return slices.Clone(c.routes) }

// Builder trả partyport.MenuBuilder của cây KN.
func (c *Catalog) Builder() partyport.MenuBuilder { return c.Build }

// Registry dựng đăng ký menu theo instance cho pf/party (party.Deps.Menus).
func (c *Catalog) Registry() (*partyport.Menus, error) {
	return partyport.NewMenus(c.Build, c.routes...)
}

// effectiveProfile: phòng ban còn menu_profile "generic"/rỗng mà mã có khuôn riêng dùng khuôn riêng (alias
// của bộ menu do KN sở hữu — seed trước W11.06).
func (c *Catalog) effectiveProfile(profile, code string) string {
	if profile == partyport.MenuProfileGeneric || profile == "" {
		if p, ok := c.t.DeptProfileByCode[code]; ok {
			return p
		}
	}
	return profile
}

func (c *Catalog) knownProfile(profile string) bool {
	_, ok := c.profiles[profile]
	return ok
}

// NavigationPolicy là chính sách trang đích của cây KN có giải alias bộ menu (generic + mã có khuôn riêng) — dùng
// cho party.Deps.NavigationPolicy cùng Registry: đường đăng ký không qua bộ chuẩn hóa compatibility. Bộ menu lạ
// hoặc đối tượng lạ → lỗi (không trang mặc định khác).
func (c *Catalog) NavigationPolicy() partyport.NavigationPolicy {
	return func(in partyport.NavigationRequest) ([]string, error) {
		return c.preferred(in)
	}
}

func (c *Catalog) preferred(in partyport.NavigationRequest) ([]string, error) {
	fallback, ok := c.t.DefaultRoutes[in.Audience]
	if !ok {
		return nil, ErrDenied
	}
	if in.Audience == partyport.AudienceAdmin || in.Department.Code == "" {
		return []string{fallback}, nil
	}
	profile := c.effectiveProfile(in.Department.MenuProfile, in.Department.Code)
	if !c.knownProfile(profile) {
		return nil, ErrDenied
	}
	if route := c.t.ContextDefaultRoutes[profile][in.Audience]; route != "" && route != fallback {
		return []string{route, fallback}, nil
	}
	return []string{fallback}, nil
}

// Build là partyport.MenuBuilder: chỉ đọc request đã được máy chủ kiểm (không đọc phiên cũ trong ctx, không dựng
// Principal). Mọi bất thường → lỗi cuối.
func (c *Catalog) Build(ctx context.Context, request partyport.MenuBuildRequest) (partyport.MenuDefinition, error) {
	if _, ok := c.t.DefaultRoutes[request.Audience]; !ok {
		return partyport.MenuDefinition{}, ErrDenied
	}
	var (
		groups []partyport.MenuGroup
		err    error
	)
	admin := request.Audience == partyport.AudienceAdmin
	switch request.Mode {
	case partyport.MenuDepartment:
		if admin {
			return partyport.MenuDefinition{}, ErrDenied
		}
		groups, err = c.department(ctx, request)
	case partyport.MenuAdmin:
		if !admin {
			return partyport.MenuDefinition{}, ErrDenied
		}
		groups, err = c.admin(request)
	case partyport.MenuDepartmentModules:
		if !admin {
			return partyport.MenuDefinition{}, ErrDenied
		}
		groups, err = c.departmentModules(request)
	default:
		return partyport.MenuDefinition{}, ErrDenied
	}
	if err != nil {
		return partyport.MenuDefinition{}, err
	}
	preferred, err := c.preferred(request.NavigationRequest)
	if err != nil {
		return partyport.MenuDefinition{}, err
	}
	return partyport.MenuDefinition{Version: Version, DefaultRoute: landing(groups, preferred), Groups: groups}, nil
}

// landing chọn trang đầu ưu tiên đang bật, rồi mục lá bật đầu tiên; không có → trang mặc định của đối tượng
// (được đăng ký) để shared áp NavigationPolicy và từ chối như compatibility khi cây không còn trang khả dụng.
func landing(groups []partyport.MenuGroup, preferred []string) string {
	var enabled []string
	var visit func([]partyport.MenuItem)
	visit = func(items []partyport.MenuItem) {
		for _, it := range items {
			if !it.Enabled {
				continue
			}
			if len(it.Children) > 0 {
				visit(it.Children)
				continue
			}
			enabled = append(enabled, it.Route)
		}
	}
	for _, g := range groups {
		visit(g.Items)
	}
	for _, want := range preferred {
		if slices.Contains(enabled, want) {
			return want
		}
	}
	if len(enabled) > 0 {
		return enabled[0]
	}
	return preferred[len(preferred)-1]
}

func (c *Catalog) department(ctx context.Context, request partyport.MenuBuildRequest) ([]partyport.MenuGroup, error) {
	d := request.Department
	if request.UnitRef == uuid.Nil || d.Code == "" || d.OrgRef != request.UnitRef || !d.Selectable() {
		return nil, ErrDenied
	}
	profile := c.effectiveProfile(d.MenuProfile, d.Code)
	if !c.knownProfile(profile) {
		return nil, ErrDenied
	}
	source := c.departmentGroups(profile, request.Audience, d.Code)
	if request.FilterTraits {
		source = filterNeeds(source, request.Manager, request.Duties)
	}
	level := levelProvince
	if request.Audience == partyport.AudienceXa {
		level = levelCommune
	}
	c.mu.RLock()
	forms := c.forms
	c.mu.RUnlock()
	cache := map[string]bool{}
	allowed := func(req *requirement) (bool, error) {
		if req == nil {
			return true, nil
		}
		if forms == nil {
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
		v, err := forms.HasForms(ctx, d.Code, lv, req.ReportType)
		cache[key] = v
		return v, err
	}
	var convert func([]item) ([]partyport.MenuItem, error)
	convert = func(items []item) ([]partyport.MenuItem, error) {
		out := []partyport.MenuItem{}
		for _, it := range items {
			ok, err := allowed(it.Requires)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			kids, err := convert(it.Children)
			if err != nil {
				return nil, err
			}
			if len(it.Children) > 0 && len(kids) == 0 {
				continue
			}
			out = append(out, c.view(it, true, kids))
		}
		return out, nil
	}
	var out []partyport.MenuGroup
	for _, g := range source {
		items, err := convert(g.Items)
		if err != nil {
			return nil, err
		}
		if len(items) > 0 {
			out = append(out, partyport.MenuGroup{Key: g.Key, Label: g.Label, Items: items})
		}
	}
	return out, nil
}

// departmentGroups: khuôn của bộ menu × đối tượng, "Báo cáo cho KHTC" cuối nhóm "Báo cáo" của Cấp Tỉnh phòng
// chuyên môn, rồi nhóm "Dùng chung" (+ mục theo đối tượng). Route đã thay {department}.
func (c *Catalog) departmentGroups(profile, audience, code string) []group {
	groups := c.profiles[profile][audience]
	out := make([]group, 0, len(groups)+1)
	for _, g := range groups {
		items := g.Items
		if g.Key == groupReports && audience == partyport.AudienceTinh && c.t.KHTCReportProfiles[profile] {
			items = append(slices.Clone(items), c.t.KHTCReportItem)
		}
		out = append(out, group{Key: g.Key, Label: g.Label, Items: bind(items, code)})
	}
	shared := group{Key: c.t.SharedGroup.Key, Label: c.t.SharedGroup.Label, Items: bind(c.t.SharedGroup.Items, "")}
	if extra := c.t.SharedByAudience[audience]; len(extra) > 0 {
		shared.Items = append(shared.Items, bind(extra, code)...)
	}
	return append(out, shared)
}

func (c *Catalog) withBusiness(profile string) map[string][]group {
	out := map[string][]group{}
	for audience, groups := range c.t.Generic {
		business := c.t.DeptBusiness[profile][audience]
		composed := make([]group, 0, len(groups)+1)
		for _, g := range groups {
			composed = append(composed, g)
			if g.Key == groupOperations && len(business) > 0 {
				composed = append(composed, group{Key: groupBusiness, Label: groupBusinessLabel, Items: business})
			}
		}
		out[audience] = composed
	}
	return out
}

func has(manager bool, duties []string, needs string) bool {
	switch needs {
	case "":
		return true
	case needsManager:
		return manager
	}
	return slices.Contains(duties, needs)
}

// filterNeeds bỏ mục có Needs mà phiên không đáp ứng; mục cha hết con và nhóm bị lọc rỗng cũng bỏ.
func filterNeeds(groups []group, manager bool, duties []string) []group {
	var filter func([]item) []item
	filter = func(items []item) []item {
		out := make([]item, 0, len(items))
		for _, it := range items {
			if !has(manager, duties, it.Needs) {
				continue
			}
			if len(it.Children) > 0 {
				it.Children = filter(it.Children)
				if len(it.Children) == 0 {
					continue
				}
			}
			out = append(out, it)
		}
		return out
	}
	out := make([]group, 0, len(groups))
	for _, g := range groups {
		n := len(g.Items)
		g.Items = filter(g.Items)
		if n > 0 && len(g.Items) == 0 {
			continue
		}
		out = append(out, g)
	}
	return out
}

func bind(items []item, code string) []item {
	out := make([]item, 0, len(items))
	for _, it := range items {
		bound := it
		bound.Route = strings.ReplaceAll(it.Route, departmentToken, code)
		bound.Children = bind(it.Children, code)
		out = append(out, bound)
	}
	return out
}

// view chuyển một mục nguồn; mục "Sắp có" luôn tắt. Lá mang module sở hữu route.
func (c *Catalog) view(it item, enabled bool, kids []partyport.MenuItem) partyport.MenuItem {
	v := partyport.MenuItem{Key: it.Key, Label: it.Label, Icon: it.Icon, BadgeSource: it.BadgeSource, Enabled: enabled, Children: kids}
	if len(it.Children) == 0 {
		v.Route, v.Module = it.Route, c.module(it.Route)
	}
	if it.ComingSoon {
		v.Enabled, v.Status, v.StatusLabel = false, statusComingSoon, statusComingSoonText
	}
	return v
}

func (c *Catalog) plain(items []item, enabled bool) []partyport.MenuItem {
	out := []partyport.MenuItem{}
	for _, it := range items {
		out = append(out, c.view(it, enabled, c.plain(it.Children, enabled)))
	}
	return out
}

func statusLabel(status string) string {
	switch status {
	case partyport.DeptActive:
		return "Đang áp dụng"
	case partyport.DeptNotOpen:
		return "Chưa mở"
	case partyport.DeptSuspended:
		return "Ngưng hoạt động"
	}
	return status
}

func departmentIcon(profile string) string {
	if profile == partyport.MenuProfileTrongTrot {
		return "sprout"
	}
	return "building"
}

// adminDepartment: mục của một phòng ban trong nhóm "Phòng ban" của Admin (mục con tắt khi phòng chưa mở).
func (c *Catalog) adminDepartment(d partyport.Department) (partyport.MenuItem, error) {
	profile := c.effectiveProfile(d.MenuProfile, d.Code)
	if d.Code == "" || d.Name == "" || !c.knownProfile(profile) {
		return partyport.MenuItem{}, ErrDenied
	}
	items, ok := c.t.AdminDeptItems[profile]
	switch {
	case !ok:
		items = slices.Clone(c.t.AdminDeptItems[partyport.MenuProfileGeneric])
		if c.t.KHTCReportProfiles[profile] {
			items = append(items, c.t.KHTCReportAdminItem)
		}
		items = append(items, c.t.AdminDeptBusiness[profile]...)
	case c.t.KHTCReportProfiles[profile]:
		items = append(slices.Clone(items), c.t.KHTCReportAdminItem)
	}
	selectable := d.Selectable()
	return partyport.MenuItem{Key: groupDepartments + "." + d.Code, Label: d.Name, Icon: departmentIcon(d.MenuProfile), Enabled: selectable,
		Status: d.Status, StatusLabel: statusLabel(d.Status), Children: c.plain(bind(items, d.Code), selectable)}, nil
}

func (c *Catalog) admin(request partyport.MenuBuildRequest) ([]partyport.MenuGroup, error) {
	departments := make([]partyport.MenuItem, 0, len(request.Departments))
	for _, d := range request.Departments {
		if request.UnitRef != uuid.Nil && d.OrgRef != request.UnitRef {
			return nil, ErrDenied
		}
		it, err := c.adminDepartment(d)
		if err != nil {
			return nil, err
		}
		departments = append(departments, it)
	}
	return []partyport.MenuGroup{
		{Key: c.t.AdminOverview.Key, Label: c.t.AdminOverview.Label, Items: c.plain(c.t.AdminOverview.Items, true)},
		{Key: groupDepartments, Label: groupDepartmentLabel, Items: departments},
		{Key: c.t.AdminSystem.Key, Label: c.t.AdminSystem.Label, Items: c.plain(c.t.AdminSystem.Items, true)},
		{Key: c.t.SharedGroup.Key, Label: c.t.SharedGroup.Label, Items: c.plain(c.t.SharedGroup.Items, true)},
	}, nil
}

func (c *Catalog) departmentModules(request partyport.MenuBuildRequest) ([]partyport.MenuGroup, error) {
	d := request.Department
	if request.UnitRef == uuid.Nil || d.Code == "" || d.OrgRef != request.UnitRef || len(request.Departments) != 1 || request.Departments[0] != d {
		return nil, ErrDenied
	}
	it, err := c.adminDepartment(d)
	if err != nil {
		return nil, err
	}
	return []partyport.MenuGroup{{Key: groupDepartments + "." + d.Code, Label: d.Name, Items: it.Children}}, nil
}

// module trả module sở hữu route (ánh xạ menuModules/menuDeptModules của nguồn; route chưa ánh xạ thuộc lõi luôn bật).
func (c *Catalog) module(route string) string {
	if rest, ok := strings.CutPrefix(route, departmentPrefix); ok {
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			if m := c.t.MenuDeptModules[rest[i:]]; m != "" {
				return m
			}
		}
		return fallbackModule
	}
	best, owner := -1, ""
	for prefix, m := range c.t.MenuModules {
		if (route == prefix || strings.HasPrefix(route, prefix+"/")) && len(prefix) > best {
			best, owner = len(prefix), m
		}
	}
	if owner != "" {
		return owner
	}
	for prefix, m := range fallbackOwners {
		if route == prefix || strings.HasPrefix(route, prefix+"/") {
			return m
		}
	}
	return fallbackModule
}

// ownership: mọi route lá của mọi bảng + trang mặc định, mỗi route (mẫu {department}) đăng ký một lần.
func (c *Catalog) ownership() ([]partyport.MenuRoute, error) {
	owners := map[string]string{}
	add := func(route string) error {
		if route == "" {
			return nil
		}
		m := c.module(route)
		if previous, ok := owners[route]; ok && previous != m {
			return fmt.Errorf("%w: route %s has two owners", ErrSource, route)
		}
		owners[route] = m
		return nil
	}
	var walk func([]item) error
	walk = func(items []item) error {
		for _, it := range items {
			if len(it.Children) > 0 {
				if err := walk(it.Children); err != nil {
					return err
				}
				continue
			}
			if it.Route == "" {
				return fmt.Errorf("%w: leaf %s has no route", ErrSource, it.Key)
			}
			if err := add(it.Route); err != nil {
				return err
			}
		}
		return nil
	}
	lists := [][]item{c.t.AdminOverview.Items, c.t.AdminSystem.Items, c.t.SharedGroup.Items, {c.t.KHTCReportItem, c.t.KHTCReportAdminItem}}
	for _, byAudience := range []map[string][]group{c.t.TrongTrot, c.t.Generic} {
		for _, groups := range byAudience {
			for _, g := range groups {
				lists = append(lists, g.Items)
			}
		}
	}
	for _, items := range c.t.AdminDeptItems {
		lists = append(lists, items)
	}
	for _, items := range c.t.AdminDeptBusiness {
		lists = append(lists, items)
	}
	for _, items := range c.t.SharedByAudience {
		lists = append(lists, items)
	}
	for _, byAudience := range c.t.DeptBusiness {
		for _, items := range byAudience {
			lists = append(lists, items)
		}
	}
	for _, items := range lists {
		if err := walk(items); err != nil {
			return nil, err
		}
	}
	for _, route := range c.t.DefaultRoutes {
		if err := add(route); err != nil {
			return nil, err
		}
	}
	for _, byAudience := range c.t.ContextDefaultRoutes {
		for _, route := range byAudience {
			if err := add(route); err != nil {
				return nil, err
			}
		}
	}
	routes := make([]partyport.MenuRoute, 0, len(owners))
	for pattern, m := range owners {
		routes = append(routes, partyport.MenuRoute{Pattern: pattern, Module: m})
	}
	slices.SortFunc(routes, func(a, b partyport.MenuRoute) int { return strings.Compare(a.Pattern, b.Pattern) })
	if len(routes) == 0 {
		return nil, errors.Join(ErrSource, errors.New("no menu routes"))
	}
	return routes, nil
}
