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

func pubRoles() []string {
	var out []string
	for _, g := range ModuleGroups() {
		if g.Pub != "" {
			out = append(out, g.Pub)
		}
	}
	return out
}

// W6.3 (§14, ADR-0031 §2, R-DB-10): r_<module>_pub NOLOGIN cho đúng các module sở hữu schema _pub, tạo trước
// vai phân tích; chỉ r_metrics (cap/analytics), r_agg và bên đọc đã rà soát (PubReaders, ADR-0053) là thành viên;
// không thuộc ag_shared_user, không vượt RLS. PFPUB: + r_forms_pub (pf_forms_pub, pf 20261013065757).
func TestPubRoles_W63(t *testing.T) {
	want := []string{"r_cropcycle_pub", "r_extension_pub", "r_forms_pub", "r_geo_pub", "r_mechanization_pub", "r_price_pub"}
	if got := pubRoles(); !slices.Equal(got, want) {
		t.Fatalf("vai _pub: %v, muốn %v", got, want)
	}
	_, ai := roleByName(t, modreg.DBRole(AnalyticsModule))
	for _, name := range want {
		r, ri := roleByName(t, name)
		if r.Login || r.BypassRLS || r.NoInherit || len(r.MemberOf) != 0 || !slices.Equal(r.Clusters, []migrations.Cluster{migrations.PF}) || ri > ai {
			t.Errorf("%s: %+v (vị trí %d, r_metrics %d)", name, r, ri, ai)
		}
		for _, o := range Roles {
			if slices.Contains(o.MemberOf, name) && o.Name != modreg.DBRole(AnalyticsModule) && o.Name != AggRole && !pubReader(o.Name, name) {
				t.Errorf("%s không được là thành viên %s", o.Name, name)
			}
		}
	}
}

// W6.4 (ADR-0031 §3, R-DB-11): r_agg NOLOGIN BYPASSRLS INHERIT, thành viên đúng mọi r_<module>_pub (không
// ag_shared_user, không vai module nào), tạo sau vai _pub và trước vai đăng nhập; thành viên DUY NHẤT là vai đăng nhập
// riêng app_agg (gói FC) — NOINHERIT, chỉ thuộc r_agg, mặc định chỉ đọc + statement_timeout; không app_<đơn vị> nào
// thuộc r_agg. Không vai BYPASSRLS mới nào khác.
func TestAggRole_W64(t *testing.T) {
	r, ri := roleByName(t, AggRole)
	if r.Login || !r.BypassRLS || r.NoInherit || !slices.Equal(r.MemberOf, pubRoles()) ||
		!slices.Equal(r.Clusters, []migrations.Cluster{migrations.PF}) {
		t.Errorf("r_agg: %+v", r)
	}
	for _, p := range pubRoles() {
		if _, pi := roleByName(t, p); pi > ri {
			t.Errorf("%s tạo sau r_agg", p)
		}
	}
	var members, bypass []string
	for i, o := range Roles {
		if slices.Contains(o.MemberOf, AggRole) {
			members = append(members, o.Name)
			if !o.Login || !o.NoInherit || i < ri {
				t.Errorf("%s: thành viên r_agg phải là vai đăng nhập NOINHERIT tạo sau r_agg: %+v", o.Name, o)
			}
		}
		if o.BypassRLS {
			bypass = append(bypass, o.Name)
		}
	}
	if !slices.Equal(members, []string{AppAgg}) || !slices.Equal(ExclusiveMembers[AggRole], members) {
		t.Errorf("thành viên r_agg: %v, muốn [app_agg] (ExclusiveMembers %v)", members, ExclusiveMembers[AggRole])
	}
	a, _ := roleByName(t, AppAgg)
	if !slices.Equal(a.MemberOf, []string{AggRole}) || a.BypassRLS || !slices.Equal(a.Clusters, []migrations.Cluster{migrations.PF}) ||
		a.Settings["default_transaction_read_only"] != "on" || a.Settings["statement_timeout"] == "" || !slices.Contains(LoginRoles(), AppAgg) {
		t.Errorf("app_agg: %+v", a)
	}
	slices.Sort(bypass)
	// ADR-0031 §6: danh mục vai BYPASSRLS.
	if want := []string{OpsJobs, OutboxPublisher, MKGMonitorOwner, AggRole, TSMonitorOwner}; !slices.Equal(bypass, want) {
		t.Errorf("vai BYPASSRLS: %v, muốn %v", bypass, want)
	}
}

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
	// 17 pf có vai cũ + pf/kref + pf/workflow + pf/forms + pf/evidence + pf/cooperative (W3.3) + pf/price (W11.71) + pf/hrm (W11.21)
	// + 7 mkg có vai cũ + mkg/engine (W8.2, không vai cũ) + vault/identity; pf/extdata không schema.
	if len(groups) != 33 || len(legacyGroups()) != 25 {
		t.Errorf("số vai nhóm: %d (muốn 33, 25 có vai cũ): %v", len(groups), groups)
	}
	if slices.ContainsFunc(groups, func(g ModuleGroup) bool { return g.Module == "pf/extdata" }) {
		t.Error("pf/extdata không có schema — không có vai nhóm")
	}
	seen := map[string]bool{OpsGroup: true, AggRole: true} // r_agg: W6.4, TestAggRole_W64
	for _, g := range groups {
		if seen[g.Role] {
			t.Errorf("trùng vai nhóm %s", g.Role)
		}
		seen[g.Role] = true
		r, ri := roleByName(t, g.Role)
		want := []string{SharedUser}
		if g.Module == AnalyticsModule {
			want = append(want, pubRoles()...) // W6.3: cap/analytics đọc mọi view _pub
		}
		want = append(want, pubReaderRoles(g.Module, groups)...) // ADR-0053: bên đọc _pub đã rà soát
		if r.Login || r.BypassRLS || r.NoInherit || !slices.Equal(r.MemberOf, want) || len(r.Clusters) == 0 {
			t.Errorf("%s: %+v", g.Role, r)
		}
		if g.Legacy == "" {
			// Module không có vai cũ (W2.5: pf/kref, W6.1: pf/workflow, W6.2: pf/forms, W6.5: pf/evidence, W3.3: pf/cooperative, W11.71: pf/price, W11.21: pf/hrm) — không vai đăng nhập nào ngoài app_* là thành viên.
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
	// Gói FC (cooperative.md §17.10): worker chạy consumer pf.simulation.snapshot — app_worker ∋ r_simulation.
	if w, _ := roleByName(t, AppWorker); !slices.Contains(w.MemberOf, "r_simulation") {
		t.Errorf("r_simulation: app_worker %v", w.MemberOf)
	}
	if k, _ := roleByName(t, "r_kref"); !slices.Equal(k.Clusters, []migrations.Cluster{migrations.PF, migrations.MKG}) {
		t.Errorf("r_kref: %+v", k)
	}
	// W6.1: api-platform ghép pf/workflow — app_platform là thành viên r_workflow, app_worker thì không.
	if w, _ := roleByName(t, AppWorker); !slices.Contains(plat.MemberOf, "r_workflow") || slices.Contains(w.MemberOf, "r_workflow") {
		t.Errorf("r_workflow: app_platform %v, app_worker %v", plat.MemberOf, w.MemberOf)
	}
	// W6.2: api-platform ghép pf/forms — app_platform là thành viên r_forms.
	if !slices.Contains(plat.MemberOf, "r_forms") {
		t.Errorf("r_forms: app_platform %v", plat.MemberOf)
	}
	// W11.71: api-platform ghép pf/price (ADR-0049) — app_platform là thành viên r_price.
	if !slices.Contains(plat.MemberOf, "r_price") {
		t.Errorf("r_price: app_platform %v", plat.MemberOf)
	}
	// W11.21: api-platform ghép pf/hrm (ADR-0050) — app_platform là thành viên r_hrm.
	if !slices.Contains(plat.MemberOf, "r_hrm") {
		t.Errorf("r_hrm: app_platform %v", plat.MemberOf)
	}
	// W8.2: api-internal ghép mkg/engine (job engine_observation_feed) — app_internal là thành viên r_engine (chỉ cụm mkg).
	if in, _ := roleByName(t, AppInternal); !slices.Contains(in.MemberOf, "r_engine") {
		t.Errorf("r_engine: app_internal %v", in.MemberOf)
	}
	if e, _ := roleByName(t, "r_engine"); !slices.Equal(e.Clusters, []migrations.Cluster{migrations.MKG}) || e.Login {
		t.Errorf("r_engine: %+v", e)
	}
	// W6.5: api-platform ghép pf/evidence (route /v1/files của pf/sync gọi port) — app_platform là thành viên r_evidence.
	if w, _ := roleByName(t, AppWorker); !slices.Contains(plat.MemberOf, "r_evidence") || slices.Contains(w.MemberOf, "r_evidence") {
		t.Errorf("r_evidence: app_platform %v, app_worker %v", plat.MemberOf, w.MemberOf)
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
		// pf: + app_agg (ADR-0031 §3, gói FC) → 8.
		if n < 4 || n > 8 {
			t.Errorf("cụm %s: %d vai đăng nhập (muốn 4–8)", c, n)
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
var laterGroups = map[string]string{
	"pf/workflow":    "pf/000118_pf_workflow.up.sql",
	"pf/forms":       "pf/000122_pf_forms_grants.up.sql",
	"pf/evidence":    "pf/000123_pf_evidence.up.sql",
	"pf/cooperative": "pf/000131_pf_cooperative_schema.up.sql", // W3.3: vai r_cooperative sinh khi module có owns_schema
	"pf/price":       "pf/000138_pf_price.up.sql",              // W11.71 (ADR-0049): khung cap/price
	"pf/hrm":         "pf/000147_pf_hrm_catalog.up.sql",        // W11.21 (ADR-0050): dom/hrm, danh mục nhân sự
}

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

// pubReader: vai nhóm reader là thành viên vai _pub pub theo PubReaders (ADR-0053 §4).
func pubReader(reader, pub string) bool {
	for mod, owners := range PubReaders {
		if modreg.DBRole(mod) != reader {
			continue
		}
		for _, o := range owners {
			if modreg.PubRole(o) == pub {
				return true
			}
		}
	}
	return false
}

// ADR-0053 §4 (PFPUB): KHTC trong pf/extension đọc họ hợp đồng closed_figures/v1 của hai nguồn — r_extension là thành
// viên r_extension_pub, r_forms_pub, r_price_pub (tạo trước r_extension), không thành viên vai _pub nào khác; mục PubReaders chỉ trỏ
// module có schema _pub.
func TestPubReaders_PFPUB(t *testing.T) {
	r, ri := roleByName(t, "r_extension")
	for _, pub := range []string{"r_extension_pub", "r_forms_pub", "r_price_pub"} {
		p, pi := roleByName(t, pub)
		if !slices.Contains(r.MemberOf, pub) || pi > ri || p.BypassRLS {
			t.Errorf("r_extension phải là thành viên %s (tạo trước): %+v", pub, r.MemberOf)
		}
	}
	for _, m := range r.MemberOf {
		if m != SharedUser && m != "r_extension_pub" && m != "r_forms_pub" && m != "r_price_pub" {
			t.Errorf("r_extension thành viên thừa %s", m)
		}
	}
	groups := ModuleGroups()
	for mod, owners := range PubReaders {
		if mod == AnalyticsModule {
			t.Errorf("PubReaders không chứa cap/analytics (đã đọc mọi view)")
		}
		if got := pubReaderRoles(mod, groups); len(got) != len(owners) {
			t.Errorf("PubReaders[%s] = %v: chủ không có schema _pub (%v)", mod, owners, got)
		}
	}
}
