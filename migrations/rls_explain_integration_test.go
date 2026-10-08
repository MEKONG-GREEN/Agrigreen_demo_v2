package migrations_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Cổng EXPLAIN chung W9.1 (DB-14, ADR-0029, ADR-0030, kiến trúc v4.1 §12.1, §17): với MỌI bảng bật RLS ở mọi cụm
// (bảng gốc, không tính phân vùng con), truy vấn đại diện SELECT * … LIMIT chạy dưới vai ứng dụng có quyền đọc
// (ưu tiên r_<module chủ schema>, không BYPASSRLS) với ngữ cảnh mẫu (app.tenant_id, app.node_id, …). Kế hoạch
// KHÔNG được gọi hàm do repo định nghĩa theo từng hàng:
//
//   - trong Filter / Join Filter của bất kỳ nút nào (chính sách gọi hàm không inline được, vd plpgsql,
//     SECURITY DEFINER, hàm có truy vấn con — DB-14);
//   - ở bất kỳ biểu thức nào bên trong SubPlan (truy vấn con tương quan chạy lại theo hàng ngoài).
//
// Hàm SQL STABLE inline được (shared.current_tenant, k_ref.ctx_uuid/ctx_text) biến mất khỏi kế hoạch nên không
// bị tính; InitPlan (chạy một lần) được phép. Bảng không có vai ứng dụng nào đọc được thì bỏ qua (ghi log).
// Ngoại lệ tạm thời trong rls_explain_exceptions.yaml (chỉ được co lại, mỗi mục có gói gỡ).
func TestRLSExplainNoPerRowFunctions_W91(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	exc := loadExceptions(t, "rls_explain_exceptions.yaml",
		func(e explainException) string { return e.Cluster + "|" + e.Table },
		func(e explainException) error {
			if e.Cluster == "" || e.Table == "" || strings.TrimSpace(e.Reason) == "" || !rePkgOwner.MatchString(e.Owner) {
				return fmt.Errorf("cluster, table, reason, owner (W<n>.<m>) bắt buộc: %+v", e)
			}
			return nil
		})
	owner := map[string]string{} // schema → vai r_<module>
	for _, e := range modreg.Catalog() {
		for _, s := range e.OwnsSchema {
			owner[s] = modreg.DBRole(e.Name)
		}
	}
	used := make([]bool, len(exc))
	var checked, skipped int
	for _, c := range migrations.Clusters {
		admin := env.AdminPool(t, c)
		fns := userFunctions(ctx, t, admin, c)
		var groups []string
		for _, g := range dbadmin.ClusterGroups(c) {
			groups = append(groups, g.Role)
		}
		type tbl struct{ name, schema string }
		var tables []tbl
		rows, err := admin.Query(ctx, `
			SELECT c.oid::regclass::text, ns.nspname FROM pg_class c JOIN pg_namespace ns ON ns.oid = c.relnamespace
			 WHERE c.relrowsecurity AND c.relkind IN ('r', 'p') AND NOT c.relispartition
			   AND ns.nspname NOT IN ('pg_catalog', 'information_schema')
			 ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var x tbl
			if err := rows.Scan(&x.name, &x.schema); err != nil {
				t.Fatal(err)
			}
			tables = append(tables, x)
		}
		rows.Close()
		for _, x := range tables {
			role, err := readerRole(ctx, admin, x.name, owner[x.schema], groups)
			if err != nil {
				t.Fatal(err)
			}
			if role == "" {
				skipped++
				t.Logf("[%s] %s: không vai ứng dụng nào có SELECT — bỏ qua", c, x.name)
				continue
			}
			plan, err := explainAs(ctx, admin, role, x.name)
			if err != nil {
				t.Errorf("[%s] %s dưới %s: %v", c, x.name, role, err)
				continue
			}
			checked++
			found := perRowCalls(plan, fns)
			if len(found) == 0 {
				continue
			}
			i := slices.IndexFunc(exc, func(e explainException) bool { return e.Cluster == string(c) && e.Table == x.name })
			if i >= 0 {
				used[i] = true
				continue
			}
			t.Errorf("[%s] %s (vai %s): kế hoạch gọi hàm theo hàng (DB-14): %s", c, x.name, role, strings.Join(found, "; "))
		}
	}
	for i, e := range exc {
		if !used[i] {
			t.Errorf("ngoại lệ lỗi thời (%s) không còn xảy ra — xóa khỏi rls_explain_exceptions.yaml: [%s] %s", e.Owner, e.Cluster, e.Table)
		}
	}
	if checked == 0 {
		t.Fatal("không bảng RLS nào được kiểm (phép đo hỏng)")
	}
	t.Logf("W9.1 EXPLAIN RLS: %d bảng kiểm, %d bỏ qua, %d ngoại lệ", checked, skipped, len(exc))
}

type explainException struct {
	Cluster string `yaml:"cluster"`
	Table   string `yaml:"table"`
	Reason  string `yaml:"reason"`
	Owner   string `yaml:"owner"`
}

// userFunctions trả biểu thức chính quy khớp lời gọi "schema.tên(" của mọi hàm do repo định nghĩa (không thuộc
// schema hệ thống, không thuộc extension).
func userFunctions(ctx context.Context, t *testing.T, admin interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, c migrations.Cluster) *regexp.Regexp {
	t.Helper()
	rows, err := admin.Query(ctx, `
		SELECT DISTINCT ns.nspname || '.' || p.proname FROM pg_proc p JOIN pg_namespace ns ON ns.oid = p.pronamespace
		 WHERE ns.nspname NOT IN ('pg_catalog', 'information_schema') AND ns.nspname NOT LIKE 'pg\_%'
		   AND p.prokind IN ('f', 'a', 'w')
		   AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e')`)
	if err != nil {
		t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatalf("[%s] không thấy hàm nào của repo", c)
	}
	alts := make([]string, len(names))
	for i, n := range names {
		alts[i] = regexp.QuoteMeta(n)
	}
	return regexp.MustCompile(`\b(?:` + strings.Join(alts, "|") + `)\(`)
}

// readerRole chọn vai ứng dụng đọc được bảng: vai module chủ schema, rồi r_* khác của cụm, rồi vai đăng nhập/vai
// dịch vụ bất kỳ không superuser, không BYPASSRLS (vd ag_outbox_publisher cho shared.outbox).
func readerRole(ctx context.Context, admin interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, table, ownerRole string, groups []string) (string, error) {
	cands := groups
	if ownerRole != "" {
		cands = append([]string{ownerRole}, groups...)
	}
	var role string
	err := admin.QueryRow(ctx, `
		SELECT r.rolname FROM pg_roles r
		  LEFT JOIN unnest($2::text[]) WITH ORDINALITY pref(name, ord) ON pref.name = r.rolname
		 WHERE NOT r.rolsuper AND NOT r.rolbypassrls AND r.rolname NOT LIKE 'pg\_%'
		   AND has_table_privilege(r.oid, $1::regclass, 'SELECT')
		 ORDER BY pref.ord NULLS LAST, r.rolname LIMIT 1`, table, cands).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// explainAs chạy EXPLAIN (FORMAT JSON, VERBOSE) SELECT * FROM table LIMIT 50 dưới vai role với ngữ cảnh mẫu,
// trong giao dịch được hủy.
func explainAs(ctx context.Context, admin interface {
	Begin(context.Context) (pgx.Tx, error)
}, role, table string) (map[string]any, error) {
	tx, err := admin.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return explainInTx(ctx, tx, role, table)
}

func explainInTx(ctx context.Context, tx pgx.Tx, role, table string) (map[string]any, error) {
	if _, err := tx.Exec(ctx, `SELECT set_config('role', $1, true),
		set_config('app.tenant_id', '0190f0c4-0000-7000-8000-000000000001', true),
		set_config('app.node_id', '0190f0c4-0000-7000-8000-000000000002', true),
		set_config('app.actor_ref', '0190f0c4-0000-7000-8000-000000000003', true),
		set_config('app.on_behalf_of_ref', '', true),
		set_config('app.dept', 'trong_trot', true),
		set_config('app.xa_ids', '{}', true),
		set_config('app.purpose', 'operations', true)`, role); err != nil {
		return nil, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `EXPLAIN (FORMAT JSON, VERBOSE, COSTS OFF) SELECT * FROM `+table+` LIMIT 50`).Scan(&raw); err != nil {
		return nil, err
	}
	var out []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("kế hoạch JSON: %w", err)
	}
	if len(out) == 0 {
		return nil, errors.New("kế hoạch JSON rỗng")
	}
	return out[0].Plan, nil
}

// perRowCalls trả các lời gọi hàm của repo bị đánh giá theo hàng trong kế hoạch: trong Filter / Join Filter của
// mọi nút, hoặc ở bất kỳ biểu thức nào dưới một SubPlan.
func perRowCalls(plan map[string]any, fns *regexp.Regexp) []string {
	var found []string
	walkPlan(plan, false, func(n map[string]any, inSubPlan bool) {
		for k, v := range n {
			if k == "Plans" || (!inSubPlan && k != "Filter" && k != "Join Filter") {
				continue
			}
			for _, s := range exprStrings(v) {
				for _, f := range fns.FindAllString(s, -1) {
					where := k
					if inSubPlan {
						where = "SubPlan/" + k
					}
					found = append(found, fmt.Sprintf("%s trong %s", strings.TrimSuffix(f, "("), where))
				}
			}
		}
	})
	slices.Sort(found)
	return slices.Compact(found)
}

// walkPlan duyệt cây kế hoạch; inSubPlan đúng cho nút nằm dưới một SubPlan (truy vấn con chạy theo hàng ngoài).
func walkPlan(n map[string]any, inSubPlan bool, visit func(map[string]any, bool)) {
	if rel, _ := n["Parent Relationship"].(string); rel == "SubPlan" {
		inSubPlan = true
	}
	visit(n, inSubPlan)
	children, _ := n["Plans"].([]any)
	for _, c := range children {
		if m, ok := c.(map[string]any); ok {
			walkPlan(m, inSubPlan, visit)
		}
	}
}

func exprStrings(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, exprStrings(e)...)
		}
		return out
	}
	return nil
}

// Kiểm có nghĩa cho cổng EXPLAIN: chính sách gọi hàm plpgsql theo hàng (Filter) và chính sách EXISTS tương quan
// gọi hàm trong SubPlan đều bị phát hiện; chính sách viết thẳng bằng hàm inline được (k_ref.ctx_uuid) thì không.
func TestRLSExplainGateDetects_W91(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	tx, err := env.AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, q := range []string{
		`CREATE SCHEMA w91_canary`,
		`CREATE FUNCTION w91_canary.allowed(t uuid) RETURNS boolean LANGUAGE plpgsql STABLE AS
		   $$ BEGIN RETURN t = k_ref.ctx_uuid('app.tenant_id'); END $$`,
		`CREATE TABLE w91_canary.per_row (tenant_id uuid, x int)`,
		`CREATE TABLE w91_canary.sub (tenant_id uuid, x int)`,
		`CREATE TABLE w91_canary.inline (tenant_id uuid, x int)`,
		`CREATE TABLE w91_canary.scope (tenant_id uuid, node uuid)`,
		`ALTER TABLE w91_canary.per_row ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE w91_canary.sub ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE w91_canary.inline ENABLE ROW LEVEL SECURITY`,
		`CREATE POLICY p ON w91_canary.per_row USING (w91_canary.allowed(tenant_id))`,
		`CREATE POLICY p ON w91_canary.sub USING (EXISTS (SELECT 1 FROM w91_canary.scope s
		   WHERE s.tenant_id = sub.tenant_id AND w91_canary.allowed(s.node)))`,
		`CREATE POLICY p ON w91_canary.inline USING (tenant_id = k_ref.ctx_uuid('app.tenant_id'))`,
		`GRANT USAGE ON SCHEMA w91_canary TO r_content`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA w91_canary TO r_content`,
	} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	fns := regexp.MustCompile(`\b(?:w91_canary\.allowed|k_ref\.ctx_uuid|k_ref\.ctx_text)\(`)
	for table, want := range map[string]string{
		"w91_canary.per_row": "w91_canary.allowed trong Filter",
		"w91_canary.sub":     "w91_canary.allowed trong SubPlan/",
		"w91_canary.inline":  "",
	} {
		if _, err := tx.Exec(ctx, `SAVEPOINT e`); err != nil {
			t.Fatal(err)
		}
		plan, err := explainInTx(ctx, tx, "r_content", table)
		if err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT e`); err != nil {
			t.Fatal(err)
		}
		got := strings.Join(perRowCalls(plan, fns), "; ")
		if (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Errorf("%s: phát hiện %q, muốn chứa %q", table, got, want)
		}
	}
}
