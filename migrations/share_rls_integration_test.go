package migrations_test

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// W4.2 (pf 000136, shared 000014; kiến trúc v4.1 §12.1 T6a–T6e, T9, T10, §17.3, R-DB-08; ADR-0030): đọc tại chỗ
// có cấp phép — p_share (PERMISSIVE, FOR SELECT, tra k_ref.share_scope) bên cạnh p_tenant và p_scope (W3.4).
// Đơn vị chủ = tenant s (cây P → X1, X2 → C1, C2 của seedScope); đơn vị xem = tenant v (cây riêng, cùng hình).
// Dòng share_scope ghi như worker pf.kref (cấp phép × hậu duệ của Nút phạm vi — dựng thật ở internal/platform/kref).

const sharePurpose = "khuyen-nong"

// shareExpectedTables: bảng mang p_share theo `shares` (docs/design/cooperative.md §7.3) — khóa share_scope →
// bảng vật lý. Khóa phải khớp modreg.ShareTables() (manifest), bảng phải đúng tập p_share trong DB.
var shareExpectedTables = map[string][]string{
	"pf_coop.household":        {"pf_coop.household", "pf_coop.household_membership"},
	"pf_coop.plot":             {"pf_coop.plot"},
	"pf_coop.season":           {"pf_coop.coop_season_area", "pf_coop.season_current", "pf_coop.yield_summary"},
	"pf_coop.diary_entry":      {"pf_coop.farm_log_current"},
	"pf_mechanization.machine": {"pf_mechanization.machine"},
}

// grantScope ghi dòng share_scope cho một cấp phép của owner → viewer phạm vi Nút node (mọi hậu duệ theo
// node_closure), khóa key, mục đích purpose; hiệu lực (from, to) tương đối now() theo khoảng SQL.
func grantScope(ctx context.Context, t *testing.T, tx pgx.Tx, owner, viewer, node, grant uuid.UUID, key, purpose, from, to string) {
	t.Helper()
	scopeExec(ctx, t, tx, `INSERT INTO k_ref.share_scope (viewer_tenant, purpose, module_table, owner_tenant, node_id, grant_id, valid_from, valid_to)
	  SELECT $1, $2, $3, $4, c.descendant, $6, now() + $7::interval, now() + $8::interval
	    FROM k_ref.node_closure c WHERE c.tenant_id = $4 AND c.ancestor = $5`, viewer, purpose, key, owner, node, grant, from, to)
}

// explainPlan chạy EXPLAIN (ANALYZE, FORMAT JSON, VERBOSE) dưới ngữ cảnh hiện hành, trả nút gốc.
func explainPlan(ctx context.Context, t *testing.T, tx pgx.Tx, q string) map[string]any {
	t.Helper()
	var raw []byte
	if err := tx.QueryRow(ctx, `EXPLAIN (ANALYZE, FORMAT JSON, VERBOSE) `+q).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var out []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out) == 0 {
		t.Fatalf("kế hoạch: %v", err)
	}
	return out[0].Plan
}

func asViewer(ctx context.Context, t *testing.T, tx pgx.Tx, tid, node uuid.UUID, purpose string) {
	t.Helper()
	asCoop(ctx, t, tx, tid, node, "")
	scopeExec(ctx, t, tx, `SELECT set_config('app.purpose', $1, true)`, purpose)
}

// T6a–T6e + bất biến chính sách.
func TestShareScopeRLS_W42(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	tx, err := env.AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s := seedScope(ctx, t, tx, true) // đơn vị chủ
	v := seedScope(ctx, t, tx, true) // đơn vị xem
	// Hộ thứ hai trong phạm vi (C1) — T6b "đúng hai hộ trong phạm vi".
	hh3 := ids.New()
	scopeExec(ctx, t, tx, `INSERT INTO pf_coop.household (tenant_id, household_ref, commune_code, created_by) VALUES ($1, $2, $3, gen_random_uuid())`,
		s.tid, hh3, xa1)
	scopeExec(ctx, t, tx, `INSERT INTO pf_coop.household_membership (tenant_id, id, household_ref, org_ref, since_date)
	  VALUES ($1, gen_random_uuid(), $2, $3, '2026-01-01')`, s.tid, hh3, s.c1)
	// Máy của đơn vị chủ ở C1 và C2.
	m1, m2 := ids.New(), ids.New()
	owner := ids.New()
	scopeExec(ctx, t, tx, `INSERT INTO pf_mechanization.owner (tenant_id, id, code, owner_type, cooperative_ref, last_seq)
	  VALUES ($1, $2, 'CSH-WT-00001', 'htx', $3, 1)`, s.tid, owner, s.c1)
	for i, m := range []struct{ ref, coop uuid.UUID }{{m1, s.c1}, {m2, s.c2}} {
		scopeExec(ctx, t, tx, `INSERT INTO pf_mechanization.machine (tenant_id, ref, code, machine_type, farm_stage, cooperative_ref, owner_id,
		    serial_no, owned_since, status, status_source, status_at, last_seq)
		  VALUES ($1, $2, 'MAY-WT-0000' || $5::text, 'may_cay', 'lam_dat', $3, $4, $6, '2025-01-01',
		    'hoat_dong', 'manual', now(), 1)`, s.tid, m.ref, m.coop, owner, strconv.Itoa(i+1), m.ref.String())
	}
	// Cấp phép: s → v, phạm vi X1 (một "tỉnh" con của cây), mục đích khuyen-nong, cho hộ, thửa, máy.
	g := ids.New()
	for _, key := range []string{"pf_coop.household", "pf_coop.plot", "pf_mechanization.machine"} {
		grantScope(ctx, t, tx, s.tid, v.tid, s.x1, g, key, sharePurpose, "-1 day", "+1 year")
	}

	const plots = `SELECT ref FROM pf_coop.plot WHERE tenant_id = '%s'`
	foreignPlots := strings.Replace(plots, "%s", s.tid.String(), 1)
	ownPlots := strings.Replace(plots, "%s", v.tid.String(), 1)
	foreignHH := `SELECT household_ref FROM pf_coop.household WHERE tenant_id = '` + s.tid.String() + `'`
	foreignMachines := `SELECT ref FROM pf_mechanization.machine WHERE tenant_id = '` + s.tid.String() + `'`

	// T6a: đơn vị chủ đọc dữ liệu của mình (có hay không mục đích) — như W3.4.
	asViewer(ctx, t, tx, s.tid, s.p, sharePurpose)
	if got := visible(ctx, t, tx, foreignPlots); !sameSet(got, s.plot1, s.plot2, s.draft1, s.draft2) {
		t.Errorf("T6a: đơn vị chủ thấy thửa %v", got)
	}
	if got := visible(ctx, t, tx, foreignHH); !sameSet(got, s.hh1, s.hh2, hh3) {
		t.Errorf("T6a: đơn vị chủ thấy hộ %v", got)
	}
	asAdmin(ctx, t, tx)

	// T6b: đơn vị xem, mục đích khớp: đúng dòng trong phạm vi X1 (C1), không thấy C2 hay thửa nháp; dữ liệu của
	// chính mình giữ phạm vi W3.4. Nút của người xem (thuộc cây đơn vị xem) không thu hẹp dòng chia sẻ.
	for _, node := range []uuid.UUID{v.p, v.c2, uuid.Nil} {
		asViewer(ctx, t, tx, v.tid, node, sharePurpose)
		if got := visible(ctx, t, tx, foreignPlots); !sameSet(got, s.plot1) {
			t.Errorf("T6b (Nút xem %v): thửa chia sẻ %v, muốn chỉ thửa C1", node, got)
		}
		if got := visible(ctx, t, tx, foreignHH); !sameSet(got, s.hh1, hh3) {
			t.Errorf("T6b (Nút xem %v): hộ chia sẻ %v, muốn đúng hai hộ của C1", node, got)
		}
		scopeExec(ctx, t, tx, `SELECT set_config('role', 'r_mechanization', true)`)
		if got := visible(ctx, t, tx, foreignMachines); !sameSet(got, m1) {
			t.Errorf("T6b (Nút xem %v): máy chia sẻ %v, muốn chỉ máy C1", node, got)
		}
		asAdmin(ctx, t, tx)
	}
	asViewer(ctx, t, tx, v.tid, v.c2, sharePurpose)
	if got := visible(ctx, t, tx, ownPlots); !sameSet(got, v.plot2, v.draft1, v.draft2) {
		t.Errorf("T6b: dữ liệu của chính đơn vị xem phải giữ phạm vi W3.4 (C2 + nháp): %v", got)
	}
	// Bảng khai shares nhưng không có cấp phép cho khóa đó (season, diary) → không thấy gì của đơn vị chủ.
	for _, q := range []string{`SELECT count(*) FROM pf_coop.season_current`, `SELECT count(*) FROM pf_coop.farm_log_current`} {
		var n int
		if err := tx.QueryRow(ctx, q+` WHERE tenant_id = '`+s.tid.String()+`'`).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: %d %v — không có cấp phép cho khóa này", q, n, err)
		}
	}
	// Bảng không khai shares (sổ, hồ sơ HTX) → không thấy.
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_coop.cooperative WHERE tenant_id = $1`, s.tid).Scan(&n); err != nil || n != 0 {
		t.Errorf("hồ sơ HTX (không khai shares) đọc chéo: %d %v", n, err)
	}
	asAdmin(ctx, t, tx)

	// T6c: mục đích không khớp / không có mục đích / tenant thứ ba → 0 dòng của đơn vị chủ.
	third := ids.New()
	for _, c := range []struct {
		tid     uuid.UUID
		purpose string
	}{{v.tid, "thong-ke"}, {v.tid, ""}, {third, sharePurpose}} {
		asViewer(ctx, t, tx, c.tid, uuid.Nil, c.purpose)
		if got := visible(ctx, t, tx, foreignPlots); len(got) != 0 {
			t.Errorf("T6c (%v, %q): thấy %v", c.tid, c.purpose, got)
		}
		if got := visible(ctx, t, tx, foreignHH); len(got) != 0 {
			t.Errorf("T6c (%v, %q): thấy hộ %v", c.tid, c.purpose, got)
		}
		asAdmin(ctx, t, tx)
	}

	// Cấp phép hết hạn (valid_to đã qua) → đóng ngay trong chính sách, không chờ worker.
	scopeExec(ctx, t, tx, `SAVEPOINT exp`)
	scopeExec(ctx, t, tx, `UPDATE k_ref.share_scope SET valid_to = now() - interval '1 second', valid_from = now() - interval '2 days'
	  WHERE grant_id = $1`, g)
	asViewer(ctx, t, tx, v.tid, uuid.Nil, sharePurpose)
	if got := visible(ctx, t, tx, foreignPlots); len(got) != 0 {
		t.Errorf("cấp phép hết hạn: vẫn thấy %v", got)
	}
	asAdmin(ctx, t, tx)
	scopeExec(ctx, t, tx, `ROLLBACK TO SAVEPOINT exp`)

	// T6e: đơn vị xem ghi vào dữ liệu đơn vị chủ qua cấp phép → bị chặn (WITH CHECK không có nhánh chia sẻ; UPDATE,
	// DELETE không thấy dòng — p_share chỉ FOR SELECT).
	asViewer(ctx, t, tx, v.tid, uuid.Nil, sharePurpose)
	if _, err := tryExec(ctx, t, tx, insertPlotSQL, s.tid, ids.New(), "TW42X1", xa1, s.c1); !rlsViolation(err) {
		t.Errorf("T6e: chèn thửa vào tenant đơn vị chủ: %v, muốn 42501 new row violates row-level security policy", err)
	} else if !strings.Contains(err.Error(), "row-level security") {
		t.Errorf("T6e: lỗi %v", err)
	}
	if n, err := tryExec(ctx, t, tx, `UPDATE pf_coop.plot SET name = 'W4.2' WHERE ref = $1`, s.plot1); err != nil || n != 0 {
		t.Errorf("T6e: UPDATE thửa chia sẻ: %d dòng, %v — muốn 0", n, err)
	}
	scopeExec(ctx, t, tx, `SELECT set_config('role', 'r_mechanization', true)`)
	if n, err := tryExec(ctx, t, tx, `UPDATE pf_mechanization.machine SET brand = 'W4.2' WHERE ref = $1`, m1); err != nil || n != 0 {
		t.Errorf("T6e: UPDATE máy chia sẻ: %d dòng, %v — muốn 0", n, err)
	}
	scopeExec(ctx, t, tx, `SELECT set_config('role', 'r_cooperative', true)`)
	if n, err := tryExec(ctx, t, tx, `UPDATE pf_coop.household SET commune_code = '99999' WHERE household_ref = $1`, s.hh1); err == nil && n != 0 {
		t.Errorf("T6e: UPDATE hộ chia sẻ: %d dòng", n)
	}
	asAdmin(ctx, t, tx)

	// Bất biến: đúng các bảng của `shares` có p_share, khóa trong biểu thức khớp modreg; v_missing_rls rỗng.
	keys := modreg.ShareTables()
	var gotKeys []string
	for _, k := range keys {
		gotKeys = append(gotKeys, k)
	}
	var wantKeys []string
	for k := range shareExpectedTables {
		wantKeys = append(wantKeys, k)
	}
	sort.Strings(gotKeys)
	sort.Strings(wantKeys)
	if !slices.Equal(gotKeys, wantKeys) {
		t.Errorf("modreg.ShareTables %v ≠ bảng chia sẻ mong đợi %v — cập nhật pf migration p_share + docs/design §7.3", gotKeys, wantKeys)
	}
	rows, err := tx.Query(ctx, `SELECT c.oid::regclass::text, pg_get_expr(p.polqual, p.polrelid) FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid
	  WHERE p.polname = 'p_share' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	policies := map[string]string{}
	for rows.Next() {
		var tb, q string
		if err := rows.Scan(&tb, &q); err != nil {
			t.Fatal(err)
		}
		policies[tb] = q
	}
	rows.Close()
	if len(policies) != 8 {
		t.Errorf("p_share trên %d bảng, muốn 8: %v", len(policies), policies)
	}
	for key, tables := range shareExpectedTables {
		for _, tb := range tables {
			if q, ok := policies[tb]; !ok || !strings.Contains(q, "'"+key+"'") {
				t.Errorf("%s: p_share thiếu hoặc không tra khóa %s: %q", tb, key, q)
			}
		}
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM shared.v_missing_rls`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("v_missing_rls: %d (%v)", n, err)
	}
	// Cổng (shared 000014): p_share sai hình → báo.
	for q, want := range map[string]string{
		`CREATE POLICY p_share ON pf_coop.sop_template USING (EXISTS (SELECT 1 FROM k_ref.share_scope))`:                             "p_share phải PERMISSIVE FOR SELECT",
		`CREATE POLICY p_share ON pf_coop.sop_template FOR SELECT USING (true)`:                                                      "p_share phải PERMISSIVE FOR SELECT",
		`CREATE POLICY p_share ON pf_coop.sop_template AS RESTRICTIVE FOR SELECT USING (EXISTS (SELECT 1 FROM k_ref.share_scope))`:   "p_share phải PERMISSIVE FOR SELECT",
		`CREATE POLICY p_share ON pf_coop.sop_template FOR SELECT TO r_cooperative USING (EXISTS (SELECT 1 FROM k_ref.share_scope))`: "p_share phải PERMISSIVE FOR SELECT",
		`CREATE POLICY p_other ON pf_coop.sop_template FOR SELECT USING (EXISTS (SELECT 1 FROM k_ref.share_scope))`:                  "có policy ngoài p_tenant/p_scope/p_share",
		`CREATE POLICY p_share ON pf_coop.sop_template FOR SELECT USING (EXISTS (SELECT 1 FROM k_ref.share_scope))`:                  "",
	} {
		scopeExec(ctx, t, tx, `SAVEPOINT gate`)
		scopeExec(ctx, t, tx, q)
		var problem string
		err := tx.QueryRow(ctx, `SELECT coalesce(string_agg(problem, '; '), '') FROM shared.v_missing_rls`).Scan(&problem)
		scopeExec(ctx, t, tx, `ROLLBACK TO SAVEPOINT gate`)
		if err != nil {
			t.Fatal(err)
		}
		if (want == "") != (problem == "") || !strings.Contains(problem, want) {
			t.Errorf("%s: v_missing_rls %q, muốn chứa %q", q, problem, want)
		}
	}
	// apply_share_rls từ chối biểu thức không tra share_scope.
	scopeExec(ctx, t, tx, `SAVEPOINT gate`)
	if _, err := tx.Exec(ctx, `SELECT shared.apply_share_rls('pf_coop.sop_template', 'true')`); err == nil {
		t.Error("apply_share_rls phải từ chối biểu thức không tra k_ref.share_scope")
	}
	scopeExec(ctx, t, tx, `ROLLBACK TO SAVEPOINT gate`)
}

// T6d + thu hồi trên kết nối tái sử dụng (PgBouncer transaction mode): biến app.* là SET LOCAL (set_config(…,
// true)); cùng một kết nối vật lý, giao dịch sau thu hồi (worker pf.kref xóa dòng share_scope của cấp phép trong
// một giao dịch) không còn thấy dữ liệu; giao dịch không đặt ngữ cảnh → 0 dòng, không lỗi ép kiểu (DB-13).
func TestShareScopeRevokeReusedConnection_W42(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	admin := env.AdminPool(t, migrations.PF)
	var s, v scopeTree
	g := ids.New()
	commit := func(fn func(tx pgx.Tx)) {
		t.Helper()
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		fn(tx)
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	commit(func(tx pgx.Tx) {
		s, v = seedScope(ctx, t, tx, true), seedScope(ctx, t, tx, true)
		grantScope(ctx, t, tx, s.tid, v.tid, s.p, g, "pf_coop.plot", sharePurpose, "-1 day", "+1 year")
	})
	conn, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	pid := func(tx pgx.Tx) int {
		var p int
		if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	read := func(setCtx bool) ([]uuid.UUID, int) {
		t.Helper()
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if setCtx {
			asViewer(ctx, t, tx, v.tid, uuid.Nil, sharePurpose)
		} else {
			scopeExec(ctx, t, tx, `SELECT set_config('role', 'r_cooperative', true)`)
		}
		got := visible(ctx, t, tx, `SELECT ref FROM pf_coop.plot WHERE tenant_id = '`+s.tid.String()+`'`)
		p := pid(tx)
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return got, p
	}
	before, p1 := read(true)
	if !sameSet(before, s.plot1, s.plot2) {
		t.Fatalf("trước thu hồi: %v, muốn thửa C1, C2 (phạm vi P)", before)
	}
	// T6d: cùng kết nối, giao dịch sau không đặt ngữ cảnh — biến đã đặt trước đó thành '' → 0 dòng, không lỗi.
	if got, p := read(false); len(got) != 0 || p != p1 {
		t.Fatalf("T6d: kết nối tái sử dụng thiếu ngữ cảnh thấy %v (pid %d/%d)", got, p, p1)
	}
	// Thu hồi: worker xóa dòng share_scope của cấp phép (một giao dịch).
	commit(func(tx pgx.Tx) {
		scopeExec(ctx, t, tx, `DELETE FROM k_ref.share_scope WHERE owner_tenant = $1 AND grant_id = $2`, s.tid, g)
	})
	if got, p := read(true); len(got) != 0 || p != p1 {
		t.Fatalf("sau thu hồi, cùng kết nối %d: vẫn thấy %v", p, got)
	}
}

// T9 + T10: 1 tỉnh, 40 tổ xã, 800 HTX, 40 000 thửa của đơn vị chủ; 20 cấp phép, mỗi cấp phép phạm vi một tổ xã
// (một nửa số Nút). Dưới r_cooperative của đơn vị xem: nhánh chia sẻ là hashed SubPlan một vòng, không gọi hàm của
// repo theo hàng (DB-14), share_scope đọc bằng chỉ mục; số dòng qua RLS = đếm đối chứng.
func TestShareScopeExplainVolume_W42(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	tx, err := env.AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tid, viewer, prov := ids.New(), ids.New(), ids.New()
	scopeExec(ctx, t, tx, `
	  CREATE TEMP TABLE w42_tree ON COMMIT DROP AS
	  SELECT x, c, md5($1::text || 'x' || x)::uuid AS xref, md5($1::text || 'c' || x || '.' || c)::uuid AS cref
	    FROM generate_series(1, 40) x, generate_series(1, 20) c`, tid)
	scopeExec(ctx, t, tx, `
	  INSERT INTO k_ref.node_closure (tenant_id, ancestor, descendant, depth)
	  SELECT $1::uuid, $2::uuid, $2::uuid, 0
	  UNION ALL SELECT DISTINCT $1::uuid, $2::uuid, xref, 1 FROM w42_tree
	  UNION ALL SELECT DISTINCT $1, xref, xref, 0 FROM w42_tree
	  UNION ALL SELECT $1::uuid, $2::uuid, cref, 2 FROM w42_tree
	  UNION ALL SELECT $1, xref, cref, 1 FROM w42_tree
	  UNION ALL SELECT $1, cref, cref, 0 FROM w42_tree`, tid, prov)
	scopeExec(ctx, t, tx, `
	  INSERT INTO pf_coop.plot (tenant_id, ref, code, commune_code, cooperative_ref, status, current_version,
	    current_boundary_id, geom, area_ha, source, created_by)
	  SELECT $1, gen_random_uuid(), 'V' || x || '.' || c || '.' || p, lpad(x::text, 5, '0'), cref, 'active', 1, gen_random_uuid(),
	    ST_Multi(ST_GeomFromText('POLYGON((105 10, 105.001 10, 105.001 10.001, 105 10.001, 105 10))', 4326)), 1.2::numeric, 'W4.2', gen_random_uuid()
	    FROM w42_tree, generate_series(1, 50) p`, tid)
	// 20 cấp phép (tổ xã x chẵn), mỗi cấp phép phạm vi một tổ xã; nhiễu: cấp phép của 30 đơn vị xem khác.
	scopeExec(ctx, t, tx, `
	  INSERT INTO k_ref.share_scope (viewer_tenant, purpose, module_table, owner_tenant, node_id, grant_id, valid_from, valid_to)
	  SELECT $2, $3, 'pf_coop.plot', $1::uuid, c.descendant, md5($1::text || 'g' || t.x)::uuid, now() - interval '1 day', now() + interval '1 year'
	    FROM (SELECT DISTINCT x, xref FROM w42_tree WHERE x % 2 = 0) t
	    JOIN k_ref.node_closure c ON c.tenant_id = $1::uuid AND c.ancestor = t.xref`, tid, viewer, sharePurpose)
	scopeExec(ctx, t, tx, `
	  INSERT INTO k_ref.share_scope (viewer_tenant, purpose, module_table, owner_tenant, node_id, grant_id, valid_from, valid_to)
	  SELECT md5($1::text || 'v' || (g % 30))::uuid, $2, 'pf_coop.plot', $1::uuid, gen_random_uuid(), gen_random_uuid(),
	    now() - interval '1 day', now() + interval '1 year'
	    FROM generate_series(1, 30000) g`, tid, sharePurpose)
	for _, tb := range []string{"k_ref.node_closure", "k_ref.share_scope", "pf_coop.plot"} {
		scopeExec(ctx, t, tx, `ANALYZE `+tb)
	}
	// T10: đối chứng (vai quản trị, không RLS).
	var control int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_coop.plot p
	  WHERE p.tenant_id = $1 AND p.cooperative_ref IN (SELECT cref FROM w42_tree WHERE x % 2 = 0)`, tid).Scan(&control); err != nil {
		t.Fatal(err)
	}
	if control != 20*20*50 {
		t.Fatalf("đối chứng %d", control)
	}
	fns := userFunctions(ctx, t, tx, migrations.PF)
	asViewer(ctx, t, tx, viewer, uuid.Nil, sharePurpose)
	plan := explainPlan(ctx, t, tx, `SELECT ref, code FROM pf_coop.plot`)
	if rows, _ := plan["Actual Rows"].(float64); int(rows) != control {
		t.Errorf("T10: qua RLS %v hàng, đối chứng %d", rows, control)
	}
	var hashed, scopeSeq int
	var perRow []string
	walkPlan(plan, false, func(n map[string]any, _ bool) {
		name, _ := n["Subplan Name"].(string)
		loops, _ := n["Actual Loops"].(float64)
		if strings.HasPrefix(name, "SubPlan") && loops > 1 {
			perRow = append(perRow, name)
		}
		if strings.HasPrefix(name, "hashed SubPlan") || (strings.HasPrefix(name, "SubPlan") && n["Parent Relationship"] == "SubPlan") {
			hashed++
		}
		if rel, _ := n["Relation Name"].(string); rel == "share_scope" {
			if typ, _ := n["Node Type"].(string); typ == "Seq Scan" {
				scopeSeq++
			}
		}
	})
	if len(perRow) > 0 {
		t.Errorf("T9: SubPlan chạy lại theo hàng: %v", perRow)
	}
	if scopeSeq > 0 {
		t.Errorf("T9: share_scope quét không qua chỉ mục %d lần", scopeSeq)
	}
	if found := perRowCalls(plan, fns); len(found) > 0 {
		t.Errorf("T9: gọi hàm của repo theo hàng: %v", found)
	}
	t.Logf("W4.2 T9: %d hàng qua nhánh chia sẻ, %v ms, %d SubPlan", control, plan["Actual Total Time"], hashed)
	asAdmin(ctx, t, tx)
}
