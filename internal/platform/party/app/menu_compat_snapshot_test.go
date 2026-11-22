package app

// KNSPLIT-2-MENU-PARITY (#122): bộ sinh ảnh chụp compatibility của #76 (trước là testdata/compat_snapshot_gen.go.txt,
// chép tạm vào gói này) giữ thành kiểm thử thật. Ảnh chụp ghi ở internal/kn/extension/menu/testdata/compat-menus.json.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

type snapForms bool

func (f snapForms) HasForms(context.Context, string, string, string) (bool, error) {
	return bool(f), nil
}

type snapDept struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	MenuProfile string `json:"menu_profile"`
	Retired     bool   `json:"retired,omitempty"`
}

type snapItem struct {
	Key         string     `json:"key"`
	Label       string     `json:"label"`
	Route       string     `json:"route"`
	Icon        string     `json:"icon"`
	BadgeSource string     `json:"badge_source"`
	Enabled     bool       `json:"enabled"`
	Status      string     `json:"status"`
	StatusLabel string     `json:"status_label"`
	Children    []snapItem `json:"children"`
}

type snapGroup struct {
	Key   string     `json:"key"`
	Label string     `json:"label"`
	Items []snapItem `json:"items"`
}

type snapView struct {
	Version      string      `json:"menu_version"`
	DefaultRoute string      `json:"default_route"`
	Groups       []snapGroup `json:"groups"`
}

type snapCase struct {
	Name         string     `json:"name"`
	Mode         string     `json:"mode"`
	Audience     string     `json:"audience"`
	Department   *snapDept  `json:"department,omitempty"`
	Departments  []snapDept `json:"departments,omitempty"`
	FilterTraits bool       `json:"filter_traits,omitempty"`
	Manager      bool       `json:"manager,omitempty"`
	Duties       []string   `json:"duties,omitempty"`
	Forms        bool       `json:"forms,omitempty"`
	Disabled     []string   `json:"disabled,omitempty"`
	Result       string     `json:"result,omitempty"`
	Denied       bool       `json:"denied,omitempty"`
}

func snapItems(items []MenuItemView) []snapItem {
	out := []snapItem{}
	for _, it := range items {
		out = append(out, snapItem{Key: it.Key, Label: it.Label, Route: it.Route, Icon: it.Icon, BadgeSource: it.BadgeSource, Enabled: it.Enabled,
			Status: it.Status, StatusLabel: it.StatusLabel, Children: snapItems(it.Children)})
	}
	return out
}

// compatMenuSnapshot sinh ảnh chụp đầu ra compatibility (JSON đã thụt lề, có dòng cuối).
func compatMenuSnapshot(t *testing.T) []byte {
	t.Helper()
	nav, err := domain.RegisteredNavigation()
	if err != nil {
		t.Fatal(err)
	}
	unit := uuid.MustParse("7d1c2a4e-0000-4000-8000-00000000aa01")
	toPort := func(d snapDept) port.Department {
		return port.Department{OrgRef: unit, Code: d.Code, Name: d.Name, Status: d.Status, MenuProfile: d.MenuProfile, Retired: d.Retired}
	}
	var variants []snapDept
	for _, p := range domain.MenuProfiles {
		code := p
		if p == port.MenuProfileGeneric {
			code = "phong_moi"
		}
		variants = append(variants, snapDept{Code: code, Name: "Phòng " + code, Status: port.DeptActive, MenuProfile: p})
	}
	for _, code := range []string{"chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"} {
		variants = append(variants, snapDept{Code: code, Name: "Phòng cũ " + code, Status: port.DeptActive, MenuProfile: port.MenuProfileGeneric})
	}
	variants = append(variants, snapDept{Code: "ke_hoach_tai_chinh", Name: "Phòng rỗng", Status: port.DeptActive, MenuProfile: ""})
	type traits struct {
		filter, manager bool
		duties          []string
	}
	traitSets := []traits{{}, {filter: true}, {filter: true, manager: true, duties: []string{domain.NeedsPriceAdmin}}, {filter: true, duties: []string{domain.NeedsPriceAdmin}}}
	disabledSets := [][]string{nil, {"pf/extension"}, {"pf/hrm"}, {"pf/price"},
		{"pf/masterdata", "pf/reports", "pf/sync", "pf/metering", "pf/geo", "pf/mechanization"}, {"pf/extension", "pf/hrm", "pf/price", "pf/masterdata"}}
	results := map[string]snapView{}
	var cases []snapCase
	record := func(c snapCase, view MenuView, err error) {
		if err == nil {
			disabled := map[string]bool{}
			for _, m := range c.Disabled {
				disabled[m] = true
			}
			view.Groups = FilterMenuGroups(view.Groups, disabled)
			view, err = ApplyNavigation(view, nav)
		}
		if err != nil {
			c.Denied = true
		} else {
			groups := []snapGroup{}
			for _, g := range view.Groups {
				groups = append(groups, snapGroup{Key: g.Key, Label: g.Label, Items: snapItems(g.Items)})
			}
			v := snapView{Version: view.Version, DefaultRoute: view.DefaultRoute, Groups: groups}
			raw, _ := json.Marshal(v)
			sum := sha256.Sum256(raw)
			c.Result = hex.EncodeToString(sum[:8])
			results[c.Result] = v
		}
		cases = append(cases, c)
	}
	ctx := context.Background()
	department := func(d snapDept, aud string, tr traits, f bool, disabled []string) {
		dd := d
		c := snapCase{Name: "department/" + aud + "/" + d.Code + "/" + d.MenuProfile, Mode: string(port.MenuDepartment), Audience: aud, Department: &dd,
			FilterTraits: tr.filter, Manager: tr.manager, Duties: tr.duties, Forms: f, Disabled: disabled}
		pd := toPort(d)
		var fa port.FormAvailability
		if f {
			fa = snapForms(true)
		}
		groups, err := departmentGroupsWithTraits(ctx, fa, pd, aud, domain.MenuTraits{Manager: tr.manager, Duties: tr.duties}, tr.filter)
		record(c, MenuView{Version: domain.MenuVersion, Audience: aud, UnitRef: unit, Department: &pd, Groups: groups, DefaultRoute: domain.DefaultRoute(aud)}, err)
	}
	auds := []string{port.AudienceTW, port.AudienceTinh, port.AudienceXa}
	for _, d := range variants {
		for _, aud := range auds {
			for _, tr := range traitSets {
				for _, f := range []bool{false, true} {
					department(d, aud, tr, f, nil)
				}
			}
			for _, dis := range disabledSets[1:] {
				department(d, aud, traitSets[2], true, dis)
			}
		}
	}
	seed := []snapDept{
		{Code: "trong_trot", Name: "Phòng Trồng trọt", Status: port.DeptActive, MenuProfile: "trong_trot"},
		{Code: "chan_nuoi_thuy_san", Name: "Phòng Chăn nuôi - Thủy sản", Status: port.DeptNotOpen, MenuProfile: "chan_nuoi_thuy_san"},
		{Code: "ke_hoach_tai_chinh", Name: "Phòng Kế hoạch - Tài chính", Status: port.DeptNotOpen, MenuProfile: "ke_hoach_tai_chinh"},
		{Code: "thong_tin_truyen_thong", Name: "Phòng Thông tin Tuyên truyền", Status: port.DeptNotOpen, MenuProfile: "thong_tin_truyen_thong"},
		{Code: "hanh_chinh_tong_hop", Name: "Phòng Hành chính - Tổng hợp", Status: port.DeptNotOpen, MenuProfile: "hanh_chinh_tong_hop"},
	}
	mixed := []snapDept{}
	statuses := []string{port.DeptActive, port.DeptNotOpen, port.DeptSuspended}
	for i, d := range variants[:6] {
		d.Status = statuses[i%3]
		d.Retired = i == 4
		mixed = append(mixed, d)
	}
	mixed = append(mixed, snapDept{Code: "phong_ban_cu", Name: "Phòng cũ (alias rỗng)", Status: port.DeptActive, MenuProfile: port.MenuProfileGeneric})
	for i, set := range [][]snapDept{seed, mixed} {
		for _, dis := range disabledSets {
			ports := make([]port.Department, 0, len(set))
			for _, d := range set {
				ports = append(ports, toPort(d))
			}
			name := "admin/seed"
			if i == 1 {
				name = "admin/mixed"
			}
			record(snapCase{Name: name, Mode: string(port.MenuAdmin), Audience: port.AudienceAdmin, Departments: set, Disabled: dis}, AdminMenu(unit, ports), nil)
		}
	}
	for _, d := range append(variants, mixed...) {
		for _, status := range []string{"", port.DeptNotOpen} {
			dd := d
			if status != "" {
				dd.Status = status
			}
			for _, dis := range disabledSets {
				pd := toPort(dd)
				item := adminDeptItem(pd)
				view := MenuView{Version: domain.MenuVersion, Audience: port.AudienceAdmin, UnitRef: unit, Department: &pd, DefaultRoute: domain.DefaultRoute(port.AudienceAdmin),
					Groups: []MenuGroupView{{Key: domain.AdminDepartmentGroupKey + "." + pd.Code, Label: pd.Name, Items: item.Children}}}
				d2 := dd
				record(snapCase{Name: "modules/" + dd.Code + "/" + dd.MenuProfile + "/" + dd.Status, Mode: string(port.MenuDepartmentModules), Audience: port.AudienceAdmin,
					Department: &d2, Disabled: dis}, view, nil)
			}
		}
	}
	out, err := json.MarshalIndent(map[string]any{
		"_note":   "Ảnh chụp đầu ra cây menu compatibility của pf/party (domain/app tại dev a943fbaf) cho kiểm parity builder KN — KNSPLIT-2-MENU-KN. Không sửa tay.",
		"version": domain.MenuVersion, "unit": unit, "cases": cases, "results": results,
	}, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("cases=%d results=%d bytes=%d", len(cases), len(results), len(out))
	return append(out, '\n')
}

// TestCompatMenuSnapshotCurrent giữ ảnh chụp compatibility mà builder của ứng dụng (internal/kn/extension/menu,
// TestBuilderMatchesCompatibilitySnapshot) so từng byte luôn khớp bảng hiện hành của pf/party: đổi menu mà quên sinh
// lại thì hỏng ở đây. Sinh lại: AG_UPDATE_MENU_SNAPSHOT=1 go test -count=1 -run TestCompatMenuSnapshotCurrent
// ./internal/platform/party/app, rồi go test ./internal/kn/extension/menu.
func TestCompatMenuSnapshotCurrent(t *testing.T) {
	path := filepath.Join("..", "..", "..", "kn", "extension", "menu", "testdata", "compat-menus.json")
	got := compatMenuSnapshot(t)
	if os.Getenv("AG_UPDATE_MENU_SNAPSHOT") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("%s lệch đầu ra compatibility hiện hành (%d/%d byte): sinh lại bằng AG_UPDATE_MENU_SNAPSHOT=1", path, len(want), len(got))
	}
}
