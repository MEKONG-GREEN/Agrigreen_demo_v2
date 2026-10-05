package dbadmin

import (
	"io/fs"
	"regexp"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func roleByName(t *testing.T, name string) (Role, int) {
	t.Helper()
	for i, r := range Roles {
		if r.Name == name {
			return r, i
		}
	}
	t.Fatalf("thiếu vai %s", name)
	return Role{}, -1
}

// ADR-0028 §1: r_<module> NOLOGIN cho mọi module pf có vai cũ; vai cũ là thành viên r_*; app_<đơn vị> LOGIN
// NOINHERIT là thành viên đúng các r_* của module đơn vị ghép (modreg.UnitModules); thứ tự tạo nhóm trước.
func TestModuleGroupRoles_ADR0028(t *testing.T) {
	groups := ModuleGroups()
	if len(groups) != 18 || len(legacyGroups()) != 17 {
		t.Errorf("số module pf có vai nhóm: %d (muốn 18, 17 có vai cũ): %v", len(groups), groups)
	}
	seen := map[string]bool{}
	for _, g := range groups {
		if seen[g.Role] {
			t.Errorf("trùng vai nhóm %s", g.Role)
		}
		seen[g.Role] = true
		r, ri := roleByName(t, g.Role)
		if r.Login || r.BypassRLS || r.NoInherit || !slices.Equal(r.MemberOf, []string{SharedUser}) {
			t.Errorf("%s: %+v", g.Role, r)
		}
		if g.Legacy == "" {
			// Module không có vai cũ (W2.5: pf/kref) — không vai đăng nhập nào ngoài app_* là thành viên.
			for _, o := range Roles {
				if slices.Contains(o.MemberOf, g.Role) && !slices.ContainsFunc(LoginUnits, func(u LoginUnit) bool { return u.Role == o.Name }) {
					t.Errorf("%s không được là thành viên %s", o.Name, g.Role)
				}
			}
			continue
		}
		l, li := roleByName(t, g.Legacy)
		if !slices.Contains(l.MemberOf, g.Role) || li < ri {
			t.Errorf("%s phải là thành viên %s và tạo sau: %+v", g.Legacy, g.Role, l)
		}
	}
	for _, want := range []string{"pf/content", "pf/notify", "pf/party"} {
		if !slices.ContainsFunc(groups, func(g ModuleGroup) bool { return g.Module == want && g.Role == modreg.DBRole(want) }) {
			t.Errorf("thiếu nhóm của %s", want)
		}
	}
	for _, u := range LoginUnits {
		r, ri := roleByName(t, u.Role)
		if !r.Login || !r.NoInherit || r.BypassRLS || slices.Contains(r.MemberOf, SharedUser) || !slices.Equal(r.Clusters, []migrations.Cluster{migrations.PF}) {
			t.Errorf("%s: %+v", u.Role, r)
		}
		for _, m := range r.MemberOf {
			if _, gi := roleByName(t, m); gi > ri {
				t.Errorf("%s phải tạo trước %s", m, u.Role)
			}
			if !seen[m] {
				t.Errorf("%s chỉ được là thành viên r_*: %s", u.Role, m)
			}
		}
	}
	plat, _ := roleByName(t, AppPlatform)
	for _, m := range modreg.UnitModules("api-platform") {
		want := slices.ContainsFunc(groups, func(g ModuleGroup) bool { return g.Module == m })
		if got := slices.Contains(plat.MemberOf, modreg.DBRole(m)); got != want {
			t.Errorf("app_platform ∋ %s: %v (muốn %v)", modreg.DBRole(m), got, want)
		}
	}
	if pub, _ := roleByName(t, AppPublic); !slices.Contains(pub.MemberOf, "r_party") || slices.Contains(pub.MemberOf, "r_notify") {
		t.Errorf("app_public: %v", pub.MemberOf)
	}
	if syn, _ := roleByName(t, AppSync); !slices.Equal(syn.MemberOf, []string{"r_sync"}) {
		t.Errorf("app_sync: %v", syn.MemberOf)
	}
	// W2.5: chỉ app_worker (worker pf.kref) ghi k_ref — là thành viên r_kref; r_kref có ở cụm pf và mkg.
	if w, _ := roleByName(t, AppWorker); !slices.Contains(w.MemberOf, "r_kref") || slices.Contains(plat.MemberOf, "r_kref") {
		t.Errorf("r_kref: app_worker %v, app_platform %v", w.MemberOf, plat.MemberOf)
	}
	if k, _ := roleByName(t, "r_kref"); !slices.Equal(k.Clusters, []migrations.Cluster{migrations.PF, migrations.MKG}) {
		t.Errorf("r_kref: %+v", k)
	}
	// Không vai đặc quyền nào cấp cho app_* (ADR-0028 §1).
	for _, u := range LoginUnits {
		r, _ := roleByName(t, u.Role)
		for _, bad := range []string{OutboxPublisher, MKGMonitorOwner, TSMonitorOwner, Migrator} {
			if slices.Contains(r.MemberOf, bad) {
				t.Errorf("%s không được thuộc %s", u.Role, bad)
			}
		}
	}
}

var pairRe = regexp.MustCompile(`\['([a-z_]+)', '([a-z_]+)'\]`)

// legacyGroups: vai nhóm của module có vai cũ <module>_app — đúng tập của W2.3/W2.4 (pf 000090, 000091).
func legacyGroups() []ModuleGroup {
	var out []ModuleGroup
	for _, g := range ModuleGroups() {
		if g.Legacy != "" {
			out = append(out, g)
		}
	}
	return out
}

// pf 000090 chép quyền theo danh sách cặp ghi tay — phải khớp ModuleGroups có vai cũ (thêm module pf thì
// migration mới GRANT cho r_<module>, không sửa 000090).
func TestMigration90MatchesModuleGroups(t *testing.T) {
	src, err := fs.ReadFile(migrations.FS, "pf/000090_pf_module_group_roles.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	var got []ModuleGroup
	for _, m := range pairRe.FindAllStringSubmatch(string(src), -1) {
		got = append(got, ModuleGroup{Legacy: m[1], Role: m[2]})
	}
	groups := legacyGroups()
	if len(got) != len(groups) {
		t.Fatalf("000090 có %d cặp, ModuleGroups %d", len(got), len(groups))
	}
	for i, g := range groups {
		if got[i].Legacy != g.Legacy || got[i].Role != g.Role {
			t.Errorf("cặp %d: %+v, muốn %s → %s", i, got[i], g.Legacy, g.Role)
		}
	}
}

func migrationRoles(t *testing.T, file string) []string {
	t.Helper()
	src, err := fs.ReadFile(migrations.FS, file)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range regexp.MustCompile(`'(r_[a-z_]+)'`).FindAllStringSubmatch(string(src), -1) {
		got = append(got, m[1])
	}
	return got
}

func roles(gs []ModuleGroup) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.Role)
	}
	return out
}

// pf 000091 (W2.4) cấp EXECUTE k_ref.ctx_* cho danh sách r_* ghi tay — khớp các vai nhóm lúc W2.4 (có vai cũ).
func TestMigration91MatchesModuleGroups(t *testing.T) {
	if got, want := migrationRoles(t, "pf/000091_pf_k_ref_ctx_functions.up.sql"), roles(legacyGroups()); !slices.Equal(got, want) {
		t.Fatalf("000091 cấp cho %v, ModuleGroups %v", got, want)
	}
}

// pf 000092 (W2.5) cấp SELECT k_ref.* cho MỌI r_* (R-DB-05) — danh sách ghi tay khớp ModuleGroups.
func TestMigration92MatchesModuleGroups(t *testing.T) {
	if got, want := migrationRoles(t, "pf/000092_pf_kref_relations.up.sql"), roles(ModuleGroups()); !slices.Equal(got, want) {
		t.Fatalf("000092 cấp cho %v, ModuleGroups %v", got, want)
	}
}
