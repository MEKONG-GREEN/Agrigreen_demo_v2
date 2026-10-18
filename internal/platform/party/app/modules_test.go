package app

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
)

func viewKeys(groups []MenuGroupView) []string {
	var out []string
	var walk func([]MenuItemView)
	walk = func(items []MenuItemView) {
		for _, it := range items {
			out = append(out, it.Key)
			walk(it.Children)
		}
	}
	for _, g := range groups {
		out = append(out, "group:"+g.Key)
		walk(g.Items)
	}
	return out
}

// W1.4: menu bỏ màn của module đã tắt; mục cha/nhóm rỗng vì lọc bị bỏ; không tắt gì → giữ nguyên.
func TestFilterMenuGroups(t *testing.T) {
	d := port.Department{OrgRef: uuid.New(), Code: port.DeptTrongTrot, Name: "Trồng trọt", Status: port.DeptActive, MenuProfile: port.MenuProfileTrongTrot}
	xa, err := DepartmentMenu(context.Background(), nil, d, port.AudienceXa)
	if err != nil {
		t.Fatal(err)
	}
	before := viewKeys(xa.Groups)
	if got := viewKeys(FilterMenuGroups(xa.Groups, nil)); !slices.Equal(got, before) {
		t.Fatal("không tắt gì phải giữ nguyên")
	}
	got := viewKeys(FilterMenuGroups(xa.Groups, map[string]bool{"pf/mechanization": true, "pf/geo": true}))
	for _, gone := range []string{"xa.thu_thap.may_moc", "xa.thu_thap.thua"} {
		if slices.Contains(got, gone) {
			t.Fatalf("còn %s: %v", gone, got)
		}
	}
	if !slices.Contains(got, "xa.thu_thap.htx") || !slices.Contains(got, "xa.nhiem_vu.cong_viec") {
		t.Fatalf("mất màn của module bật: %v", got)
	}
	// Tắt pf/extension: nhóm Điều hành/Báo cáo của Cấp Xã rỗng → bỏ; còn Dữ liệu › HTX (lõi pf/party).
	got = viewKeys(FilterMenuGroups(xa.Groups, map[string]bool{"pf/extension": true}))
	if slices.Contains(got, "group:dieu_hanh") || slices.Contains(got, "group:bao_cao") || !slices.Contains(got, "xa.thu_thap.htx") ||
		slices.Contains(got, "xa.thu_thap.vat_tu") {
		t.Fatalf("tắt pf/extension: %v", got)
	}
	admin := AdminMenu(uuid.Nil, []port.Department{d})
	got = viewKeys(FilterMenuGroups(admin.Groups, map[string]bool{"pf/reports": true}))
	if slices.Contains(got, "admin.pb.mau_bao_cao") || slices.Contains(got, "admin.qt.cong_cu.mau_xuat") || !slices.Contains(got, "admin.pb.tong_hop") {
		t.Fatalf("tắt pf/reports: %v", got)
	}
}

// Mọi module mà bảng menu tham chiếu là module per_tenant có thật (modreg).
func TestMenuModulesArePerTenant(t *testing.T) {
	routes := []string{"/kn/dashboard", "/kn/bao-cao/chi-cuc", "/kn/du-lieu/may-moc", "/kn/du-lieu/ve-thua", "/kn/phong-ban/x/danh-muc",
		"/kn/phong-ban/x/mau-bao-cao", "/kn/bao-gia/xa-bao-gia", "/kn/phong-ban/x/bao-gia-lich", "/quan-tri/tham-so", "/quan-tri/xung-dot", "/quan-tri/bang-ke", "/dong-bo"}
	for _, r := range routes {
		m := domain.MenuModule(r)
		if !modreg.PerTenant(m) {
			t.Fatalf("%s → %q không phải module per_tenant", r, m)
		}
	}
	for _, r := range []string{"/kn/tong-quan", "/kn/quan-tri/phong-ban", "/kn/du-lieu/htx", "/quan-tri/tai-khoan", "/kn/phong-ban/x"} {
		if m := domain.MenuModule(r); m != "" {
			t.Fatalf("%s là màn lõi, nhận %q", r, m)
		}
	}
	if domain.MenuModule("/kn/bao-cao/chi-cuc") != "pf/reports" || domain.MenuModule("/kn/bao-cao/tuan") != "pf/extension" ||
		domain.MenuModule("/kn/bao-cao-khtc/da-gui") != "pf/extension" || domain.MenuModule("/kn/phong-ban/trong_trot/bao-cao-khtc") != "pf/extension" { // W11.11
		t.Fatal("khớp tiền tố dài nhất")
	}
}
