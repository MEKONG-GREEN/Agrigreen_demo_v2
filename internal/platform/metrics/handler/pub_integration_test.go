package handler_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/pubview"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

const repoRoot = "../../../.."

// pubViews trả mọi view _pub khai trong provides.publications (module.yaml), theo module chủ.
func pubViews(t *testing.T) map[string][]modreg.PubView {
	t.Helper()
	reg, err := modreg.Load(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]modreg.PubView{}
	for _, m := range reg.Modules() {
		for _, p := range m.Provides.Publications {
			if v, ok := modreg.ParsePubView(p); ok {
				out[m.Name] = append(out[m.Name], v)
			}
		}
	}
	return out
}

func countViews(views map[string][]modreg.PubView) int {
	n := 0
	for _, vs := range views {
		n += len(vs)
	}
	return n
}

// W6.3 (R-DB-10, ADR-0031 §2, §14): mọi view khai trong provides.publications tồn tại; schema _pub chỉ chứa view đã
// khai; vai r_<module>_pub có USAGE + SELECT, r_metrics (cap/analytics) đọc được qua membership nhưng KHÔNG có quyền
// nào trên bảng nội bộ của module nguồn; view chạy bằng chủ không vượt RLS trên bảng gốc FORCE RLS.
func TestPubViewsDeclaredAndPrivileges_W63(t *testing.T) {
	env := pgtest.Get(t)
	admin := env.AdminPool(t, migrations.PF)
	ctx := context.Background()
	views := pubViews(t)
	if len(views) != 6 { // W11.76: + pf/price; PFPUB: + pf/forms (pf_forms_pub)
		t.Fatalf("module công bố view _pub: %v", views)
	}
	// W6.3: 11 view (pf 000124); W11.10: + pf_extension_pub.v1_consolidation_closed (pf 000144); W11.76: +
	// pf_price_pub.v1_board_daily, v1_ticket_week (pf 000152); PFPUB (ADR-0053): + họ closed_figures/v1 —
	// pf_forms_pub.v1_closed_figures, pf_extension_pub.v1_closed_figures (pf 20261013150108, 20261013150110).
	if n := countViews(views); n != 16 {
		t.Fatalf("số view _pub đã khai: %d", n)
	}
	declared := map[string]bool{}
	for mod, vs := range views {
		pub := modreg.PubRole(mod)
		for _, v := range vs {
			declared[v.View()] = true
			var exists, roleSel, metricsSel bool
			if err := admin.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL,
			    has_table_privilege($2, $1, 'SELECT'), has_table_privilege('r_metrics', $1, 'SELECT')`,
				v.View(), pub).Scan(&exists, &roleSel, &metricsSel); err != nil {
				t.Fatalf("%s: %v", v.View(), err)
			}
			if !exists || !roleSel || !metricsSel {
				t.Errorf("%s: tồn tại %v, %s SELECT %v, r_metrics SELECT %v", v.View(), exists, pub, roleSel, metricsSel)
			}
		}
	}
	rows, err := admin.Query(ctx, `SELECT n.nspname || '.' || c.relname, c.relkind::text FROM pg_class c
	    JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname LIKE 'pf\_%\_pub' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	type rel struct{ Name, Kind string }
	rels, err := pgx.CollectRows(rows, pgx.RowToStructByPos[rel])
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rels {
		if r.Kind != "v" || !declared[r.Name] {
			t.Errorf("schema _pub chỉ chứa view đã khai: %s (relkind %s)", r.Name, r.Kind)
		}
	}
	if len(rels) != len(declared) {
		t.Errorf("số view _pub %d, đã khai %d", len(rels), len(declared))
	}

	// W11.10: view có hợp đồng cột (contracts/pubview, status published) — tên, kiểu, thứ tự cột trùng hợp đồng.
	cs, err := pubview.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Status != pubview.StatusPublished {
			continue
		}
		rows, err := admin.Query(ctx, `SELECT a.attname::text, format_type(a.atttypid, a.atttypmod) FROM pg_attribute a
		    WHERE a.attrelid = to_regclass($1) AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum`, c.View)
		if err != nil {
			t.Fatal(err)
		}
		type col struct{ Name, Type string }
		got, err := pgx.CollectRows(rows, pgx.RowToStructByPos[col])
		if err != nil {
			t.Fatal(err)
		}
		want := make([]col, 0, len(c.Columns))
		for _, x := range c.Columns {
			want = append(want, col{x.Name, x.Type})
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: cột view %v, hợp đồng %v", c.View, got, want)
		}
	}

	// Không vai nào ngoài r_<module>_pub (và chủ) có quyền trực tiếp trên schema/view _pub; r_metrics không có
	// quyền nào trên bảng/schema nội bộ của module nguồn.
	var leaks []string
	rows, err = admin.Query(ctx, `
	  SELECT n.nspname || ' → ' || r.rolname FROM pg_namespace n, aclexplode(n.nspacl) a JOIN pg_roles r ON r.oid = a.grantee
	   WHERE n.nspname LIKE 'pf\_%\_pub' AND r.rolname <> 'r_' || substring(n.nspname from 4 for length(n.nspname) - 7) || '_pub'
	     AND a.grantee <> n.nspowner
	  UNION ALL
	  SELECT n.nspname || '.' || c.relname || ' → ' || r.rolname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace,
	         aclexplode(c.relacl) a JOIN pg_roles r ON r.oid = a.grantee
	   WHERE n.nspname LIKE 'pf\_%\_pub' AND r.rolname <> 'r_' || substring(n.nspname from 4 for length(n.nspname) - 7) || '_pub'
	     AND a.grantee <> c.relowner
	  UNION ALL
	  SELECT n.nspname || '.' || c.relname || ' → r_metrics' FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	   WHERE n.nspname IN ('pf_cropcycle', 'pf_extension', 'pf_geo', 'pf_mechanization', 'pf_coop') AND c.relkind IN ('r', 'p', 'v', 'm')
	     AND has_table_privilege('r_metrics', c.oid, 'SELECT, INSERT, UPDATE, DELETE')
	  UNION ALL
	  SELECT n.nspname || ' USAGE → r_metrics' FROM pg_namespace n
	   WHERE n.nspname IN ('pf_cropcycle', 'pf_extension', 'pf_geo', 'pf_mechanization', 'pf_coop') AND has_schema_privilege('r_metrics', n.oid, 'USAGE')`)
	if err != nil {
		t.Fatal(err)
	}
	if leaks, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		t.Fatal(err)
	}
	if len(leaks) > 0 {
		t.Errorf("quyền thừa: %v", leaks)
	}

	// Chủ view không vượt RLS; mọi bảng gốc mà view _pub đọc có ENABLE + FORCE RLS (policy áp qua view).
	rows, err = admin.Query(ctx, `
	  SELECT DISTINCT v.oid::regclass::text || ' ← ' || b.oid::regclass::text || ' super=' || o.rolsuper || ' bypass=' || o.rolbypassrls
	         || ' rls=' || b.relrowsecurity || ' force=' || b.relforcerowsecurity
	    FROM pg_class v JOIN pg_namespace n ON n.oid = v.relnamespace AND n.nspname LIKE 'pf\_%\_pub'
	    JOIN pg_roles o ON o.oid = v.relowner
	    JOIN pg_rewrite rw ON rw.ev_class = v.oid
	    JOIN pg_depend d ON d.objid = rw.oid AND d.classid = 'pg_rewrite'::regclass AND d.refclassid = 'pg_class'::regclass
	    JOIN pg_class b ON b.oid = d.refobjid AND b.oid <> v.oid AND b.relkind IN ('r', 'p')
	   WHERE o.rolsuper OR o.rolbypassrls OR NOT b.relrowsecurity OR NOT b.relforcerowsecurity`)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(bad) > 0 {
		t.Errorf("view _pub vượt RLS: %v %v", bad, err)
	}

	// Chạy thật bằng vai r_metrics (vai đăng nhập app_platform + SET LOCAL ROLE): đọc được mọi view (tenant lạ →
	// 0 dòng), bị từ chối đọc bảng nội bộ (42501).
	mod := metrics.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, metrics.Deps{})
	db := mod.Service().DB
	for v := range declared {
		var n int64
		if err := tenant.WithTenantTx(ctx, db, ids.New(), func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM "+v).Scan(&n)
		}); err != nil || n != 0 {
			t.Errorf("r_metrics đọc %s: %d %v", v, n, err)
		}
	}
	// W3.3: sổ vụ, khai sản lượng, sổ thửa, ranh giới thửa/HTX chuyển sang pf_coop (view _pub giữ tên — bám OID).
	for _, base := range []string{"pf_coop.season_event", "pf_coop.harvest_declaration", "pf_extension.field_report",
		"pf_extension.task_event", "pf_extension.task_current", "pf_extension.directive_ack", "pf_coop.plot_event",
		"pf_coop.plot_boundary", "pf_coop.cooperative_boundary", "pf_geo.facility_event", "pf_mechanization.balance_run",
		"pf_mechanization.balance_result"} {
		err := tenant.WithTenantTx(ctx, db, ids.New(), func(tx pgx.Tx) error {
			_, e := tx.Exec(ctx, "SELECT 1 FROM "+base+" LIMIT 1")
			return e
		})
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "42501" {
			t.Errorf("r_metrics phải bị từ chối đọc %s: %v", base, err)
		}
	}
}

// R-DB-10: mã không phải kiểm thử của pf/metrics chỉ nhắc schema của chính nó (pf_metrics), k_ref, shared và view
// _pub đã khai — không bảng nội bộ của module khác.
func TestMetricsReadsOnlyDeclaredPubViews_W63(t *testing.T) {
	declared := map[string]bool{}
	for _, vs := range pubViews(t) {
		for _, v := range vs {
			declared[v.View()] = true
		}
	}
	re := regexp.MustCompile(`\b((?:pf|mkg|ts)_[a-z0-9_]+)\.([a-z_][a-z0-9_]*)`)
	var bad, used []string
	err := filepath.WalkDir("..", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		src, err := os.ReadFile(p) //nolint:gosec // tệp trong repo
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				switch {
				case m[1] == "pf_metrics":
				case declared[m[1]+"."+m[2]]:
					used = append(used, m[1]+"."+m[2])
				default:
					bad = append(bad, filepath.ToSlash(p)+": "+m[0])
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		t.Errorf("pf/metrics đọc ngoài view _pub đã khai: %v", bad)
	}
	slices.Sort(used)
	used = slices.Compact(used)
	// W11.10: view có bên đọc khác pf/metrics (hợp đồng contracts/pubview — vd nguồn module của KHTC) không bắt buộc
	// pf/metrics đọc; pf/metrics vẫn chỉ được đọc view đã khai.
	others := map[string]bool{}
	cs, err := pubview.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Status == pubview.StatusPublished {
			others[c.View] = true
		}
	}
	want := make([]string, 0, len(declared))
	for v := range declared {
		if !others[v] || slices.Contains(used, v) {
			want = append(want, v)
		}
	}
	sort.Strings(want)
	if !slices.Equal(used, want) {
		t.Errorf("view _pub đã khai nhưng pf/metrics không đọc (hoặc ngược lại): dùng %v, khai %v", used, want)
	}
}
