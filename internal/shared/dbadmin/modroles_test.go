package dbadmin

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
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

// ADR-0028 §1 (W2.3 + W2.6): r_<module> NOLOGIN cho mọi module có schema (registry, mọi cụm); vai cũ là thành viên
// r_*; app_<đơn vị> LOGIN NOINHERIT là thành viên đúng các r_* của module đơn vị ghép (modreg.UnitModules) + r_ops;
// thứ tự tạo nhóm trước.
func TestModuleGroupRoles_ADR0028(t *testing.T) {
	groups := ModuleGroups()
	// 17 pf có vai cũ + pf/kref + pf/workflow + 7 mkg + vault/identity; pf/extdata không schema.
	if len(groups) != 27 || len(legacyGroups()) != 25 {
		t.Errorf("số vai nhóm: %d (muốn 27, 25 có vai cũ): %v", len(groups), groups)
	}
	if slices.ContainsFunc(groups, func(g ModuleGroup) bool { return g.Module == "pf/extdata" }) {
		t.Error("pf/extdata không có schema — không có vai nhóm")
	}
	seen := map[string]bool{OpsGroup: true}
	for _, g := range groups {
		if seen[g.Role] {
			t.Errorf("trùng vai nhóm %s", g.Role)
		}
		seen[g.Role] = true
		r, ri := roleByName(t, g.Role)
		if r.Login || r.BypassRLS || r.NoInherit || !slices.Equal(r.MemberOf, []string{SharedUser}) || len(r.Clusters) == 0 {
			t.Errorf("%s: %+v", g.Role, r)
		}
		if g.Legacy == "" {
			// Module không có vai cũ (W2.5: pf/kref, W6.1: pf/workflow) — không vai đăng nhập nào ngoài app_* là thành viên.
			for _, o := range Roles {
				if slices.Contains(o.MemberOf, g.Role) && !slices.Contains(LoginRoles(), o.Name) {
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
	if g, _ := roleByName(t, "r_identity"); !slices.Equal(g.Clusters, []migrations.Cluster{migrations.Vault}) {
		t.Errorf("r_identity: %+v", g)
	}
	if g, _ := roleByName(t, "r_measurement"); !slices.Equal(g.Clusters, []migrations.Cluster{migrations.MKG, migrations.TS}) {
		t.Errorf("r_measurement: %+v", g)
	}
	for _, u := range LoginUnits {
		r, ri := roleByName(t, u.Role)
		if !r.Login || !r.NoInherit || r.BypassRLS || slices.Contains(r.MemberOf, SharedUser) || len(r.Clusters) == 0 {
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
		// Vai nhóm của module phải có ở cụm vai đăng nhập được CONNECT.
		for _, g := range groups {
			if slices.Contains(r.MemberOf, g.Role) && !slices.ContainsFunc(g.Clusters, func(c migrations.Cluster) bool { return slices.Contains(u.Clusters, c) }) {
				t.Errorf("%s ∋ %s nhưng không chung cụm", u.Role, g.Role)
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
	if ing, _ := roleByName(t, AppIngest); !slices.Equal(ing.MemberOf, []string{"r_measurement"}) {
		t.Errorf("app_ingest: %v", ing.MemberOf)
	}
	if v, _ := roleByName(t, AppVault); !slices.Equal(v.MemberOf, []string{"r_identity"}) {
		t.Errorf("app_vault: %v", v.MemberOf)
	}
	// W2.5: chỉ app_worker (worker pf.kref) ghi k_ref — là thành viên r_kref; r_kref có ở cụm pf và mkg.
	if w, _ := roleByName(t, AppWorker); !slices.Contains(w.MemberOf, "r_kref") || slices.Contains(plat.MemberOf, "r_kref") {
		t.Errorf("r_kref: app_worker %v, app_platform %v", w.MemberOf, plat.MemberOf)
	}
	if k, _ := roleByName(t, "r_kref"); !slices.Equal(k.Clusters, []migrations.Cluster{migrations.PF, migrations.MKG}) {
		t.Errorf("r_kref: %+v", k)
	}
	// W6.1: api-platform ghép pf/workflow — app_platform là thành viên r_workflow, app_worker thì không.
	if w, _ := roleByName(t, AppWorker); !slices.Contains(plat.MemberOf, "r_workflow") || slices.Contains(w.MemberOf, "r_workflow") {
		t.Errorf("r_workflow: app_platform %v, app_worker %v", plat.MemberOf, w.MemberOf)
	}
	// Không vai đặc quyền nào cấp cho app_* (ADR-0028 §1).
	for _, u := range LoginUnits {
		r, _ := roleByName(t, u.Role)
		for _, bad := range []string{OutboxPublisher, OpsJobs, MKGMonitorOwner, TSMonitorOwner, Migrator} {
			if slices.Contains(r.MemberOf, bad) {
				t.Errorf("%s không được thuộc %s", u.Role, bad)
			}
		}
	}
	// Vai đăng nhập gộp (không theo module) KHÔNG là thành viên vai nhóm (lùi bản không mở rộng quyền).
	for legacy, grp := range FoldedLogins {
		r, _ := roleByName(t, legacy)
		if _, gi := roleByName(t, grp); slices.Contains(r.MemberOf, grp) || gi < 0 {
			t.Errorf("%s: %+v", legacy, r)
		}
	}
}

// W2.6: mọi đơn vị triển khai/lệnh ghép module có vai đăng nhập theo đơn vị, và vai đó là thành viên r_* của
// MỌI module có schema mà nó ghép (không còn pool vai <module>_app nào trong cmd/*).
func TestEveryUnitHasLogin_ADR0028(t *testing.T) {
	covered := map[string]LoginUnit{}
	for _, u := range LoginUnits {
		covered[u.Unit] = u
		for _, tool := range slices.Concat(u.Tools, u.Aliases) {
			covered[tool] = u
		}
	}
	for _, unit := range []string{"api-platform", "integration-gateway", "api-public", "api-internal", "worker", "worker-internal", "ingest",
		"identity-vault", "drill", "formimport", "rebuilddrill", "objsync"} {
		u, ok := covered[unit]
		if !ok {
			t.Errorf("cmd/%s chưa có vai đăng nhập đơn vị", unit)
			continue
		}
		r, _ := roleByName(t, u.Role)
		for _, m := range modreg.UnitModules(unit) {
			e, _ := modreg.Entry(m)
			if len(e.OwnsSchema) > 0 && !slices.Contains(r.MemberOf, modreg.DBRole(m)) {
				t.Errorf("%s (cmd/%s) thiếu %s", u.Role, unit, modreg.DBRole(m))
			}
		}
	}
	// Số bí mật đăng nhập mỗi cụm (ADR-0028 §4: 4–6, + ag_ops_jobs của rà soát 08/10) — vai app_* + ag_migrator +
	// ag_outbox_publisher + ag_ops_jobs + giám sát.
	for _, c := range migrations.Clusters {
		n := 0
		for _, r := range Roles {
			if !r.Login || !slices.Contains(r.Clusters, c) {
				continue
			}
			if slices.Contains(LoginRoles(), r.Name) || r.Name == Migrator || r.Name == OutboxPublisher || r.Name == OpsJobs || r.Name == MKGMonitor || r.Name == TSMonitor {
				n++
			}
		}
		if n < 4 || n > 7 {
			t.Errorf("cụm %s: %d vai đăng nhập (muốn 4–7)", c, n)
		}
	}
}

var pairRe = regexp.MustCompile(`\['([a-z_]+)', '([a-z_]+)'(?:, '([a-z]+)')?\]`)

// legacyGroups: vai nhóm của module có vai cũ <module>_app.
func legacyGroups() []ModuleGroup {
	var out []ModuleGroup
	for _, g := range ModuleGroups() {
		if g.Legacy != "" {
			out = append(out, g)
		}
	}
	return out
}

func legacyGroupsIn(c migrations.Cluster) []ModuleGroup {
	var out []ModuleGroup
	for _, g := range ClusterGroups(c) {
		if g.Legacy != "" {
			out = append(out, g)
		}
	}
	return out
}

type migPair struct{ legacy, group, how string }

func migrationPairs(t *testing.T, file string) []migPair {
	t.Helper()
	src, err := fs.ReadFile(migrations.FS, file)
	if err != nil {
		t.Fatal(err)
	}
	var got []migPair
	for _, m := range pairRe.FindAllStringSubmatch(string(src), -1) {
		got = append(got, migPair{m[1], m[2], m[3]})
	}
	return got
}

// pf 000090 chép quyền theo danh sách cặp ghi tay — đúng 17 module pf có vai cũ lúc W2.3.
func TestMigration90MatchesModuleGroups(t *testing.T) {
	got := migrationPairs(t, "pf/000090_pf_module_group_roles.up.sql")
	groups := legacyGroupsIn(migrations.PF)
	if len(got) != len(groups) {
		t.Fatalf("000090 có %d cặp, ModuleGroups %d", len(got), len(groups))
	}
	for i, g := range groups {
		if got[i].legacy != g.Legacy || got[i].group != g.Role {
			t.Errorf("cặp %d: %+v, muốn %s → %s", i, got[i], g.Legacy, g.Role)
		}
	}
}

// W2.6: pf 000119, mkg 000035, vault 000003 'move' đúng mọi vai cũ <module>_app của cụm; phần 'copy' (và ts 000004)
// đúng FoldedLogins của cụm.
func TestMigrationsW26MatchModuleGroups(t *testing.T) {
	folded := map[migrations.Cluster][]string{}
	for legacy := range FoldedLogins {
		r, _ := roleByName(t, legacy)
		for _, c := range r.Clusters {
			folded[c] = append(folded[c], legacy)
		}
	}
	for c, file := range map[migrations.Cluster]string{
		migrations.PF:    "pf/000119_pf_revoke_legacy_module_roles.up.sql",
		migrations.MKG:   "mkg/000035_mkg_module_group_roles.up.sql",
		migrations.TS:    "ts/000004_ts_module_group_roles.up.sql",
		migrations.Vault: "vault/000003_vault_module_group_roles.up.sql",
	} {
		var moved, copied []string
		for _, p := range migrationPairs(t, file) {
			switch p.how {
			case "move":
				if !slices.ContainsFunc(ModuleGroups(), func(g ModuleGroup) bool { return g.Legacy == p.legacy && g.Role == p.group }) {
					t.Errorf("%s: cặp lạ %+v", file, p)
				}
				moved = append(moved, p.legacy)
			case "copy":
				if FoldedLogins[p.legacy] != p.group {
					t.Errorf("%s: cặp gộp lạ %+v", file, p)
				}
				copied = append(copied, p.legacy)
			default:
				t.Errorf("%s: cách lạ %+v", file, p)
			}
		}
		var want []string
		for _, g := range legacyGroupsIn(c) {
			r, _ := roleByName(t, g.Legacy)
			if slices.Contains(r.Clusters, c) { // measurement_app chỉ CONNECT mkg — không có gì ở ts
				want = append(want, g.Legacy)
			}
		}
		slices.Sort(moved)
		slices.Sort(want)
		slices.Sort(copied)
		slices.Sort(folded[c])
		if !slices.Equal(moved, want) || !slices.Equal(copied, folded[c]) {
			t.Errorf("%s: move %v (muốn %v), copy %v (muốn %v)", file, moved, want, copied, folded[c])
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

// pf 000091 (W2.4) cấp EXECUTE k_ref.ctx_* cho danh sách r_* ghi tay — khớp các vai nhóm pf có vai cũ.
func TestMigration91MatchesModuleGroups(t *testing.T) {
	if got, want := migrationRoles(t, "pf/000091_pf_k_ref_ctx_functions.up.sql"), roles(legacyGroupsIn(migrations.PF)); !slices.Equal(got, want) {
		t.Fatalf("000091 cấp cho %v, ModuleGroups %v", got, want)
	}
}

// laterGroups: module có vai nhóm sinh SAU pf 000092 — migration của chính module cấp k_ref cho r_* của nó.
var laterGroups = map[string]string{"pf/workflow": "pf/000118_pf_workflow.up.sql"}

// pf 000092 (W2.5) cấp SELECT k_ref.* cho MỌI r_* của module pf lúc đó (R-DB-05) — danh sách ghi tay khớp
// ModuleGroups cụm pf trừ module sinh sau (laterGroups).
func TestMigration92MatchesModuleGroups(t *testing.T) {
	var want []ModuleGroup
	for _, g := range ClusterGroups(migrations.PF) {
		if _, later := laterGroups[g.Module]; !later && strings.HasPrefix(g.Module, "pf/") {
			want = append(want, g)
		}
	}
	if got := migrationRoles(t, "pf/000092_pf_kref_relations.up.sql"); !slices.Equal(got, roles(want)) {
		t.Fatalf("000092 cấp cho %v, ModuleGroups %v", got, roles(want))
	}
}

// Module sinh sau 000092 phải tự cấp k_ref (docs/dev/modules.md, vai DB bước 5).
func TestLaterGroupsGrantKRef(t *testing.T) {
	for m, file := range laterGroups {
		src, err := fs.ReadFile(migrations.FS, file)
		if err != nil {
			t.Fatal(err)
		}
		role := modreg.DBRole(m)
		for _, want := range []string{"GRANT USAGE ON SCHEMA k_ref TO " + role, "k_ref.ctx_uuid(text) TO " + role,
			"k_ref.share_scope TO " + role} {
			if !regexp.MustCompile(regexp.QuoteMeta(want)).Match(src) {
				t.Errorf("%s: thiếu %q", file, want)
			}
		}
	}
}
