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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// W3.4 (pf 000134, shared 000012; kiến trúc v4.1 §17.2, docs/design/cooperative.md mục 9): RLS phạm vi theo Nút
// p_scope (RESTRICTIVE) trên pf_coop, chạy dưới vai r_cooperative với biến ngữ cảnh như db.ForModule đặt.
// Cây: Tỉnh P → tổ KN xã X1 (xã 10001), X2 (xã 10002) → HTX C1 (dưới X1), C2 (dưới X2).

type scopeTree struct {
	tid, p, x1, x2, c1, c2       uuid.UUID
	plot1, plot2, draft1, draft2 uuid.UUID
	hh1, hh2                     uuid.UUID
}

const (
	xa1 = "10001"
	xa2 = "10002"
)

func scopeExec(ctx context.Context, t *testing.T, tx pgx.Tx, q string, args ...any) {
	t.Helper()
	if _, err := tx.Exec(ctx, q, args...); err != nil {
		t.Fatalf("%s: %v", strings.SplitN(strings.TrimSpace(q), "\n", 2)[0], err)
	}
}

// seedKRef ghi bản chiếu k_ref (org + node_closure) của cây vào tenant tid (vai quản trị, như worker pf.kref).
func seedKRef(ctx context.Context, t *testing.T, tx pgx.Tx, s scopeTree) {
	t.Helper()
	type node struct {
		ref, parent uuid.UUID
		kind, unit  string
	}
	nodes := []node{
		{s.p, uuid.Nil, "extension_center", "P01"},
		{s.x1, s.p, "extension_team", xa1}, {s.x2, s.p, "extension_team", xa2},
		{s.c1, s.x1, "cooperative", xa1}, {s.c2, s.x2, "cooperative", xa2},
	}
	parent := map[uuid.UUID]uuid.UUID{}
	for _, n := range nodes {
		parent[n.ref] = n.parent
		var pr *uuid.UUID
		if n.parent != uuid.Nil {
			v := n.parent
			pr = &v
		}
		scopeExec(ctx, t, tx, `INSERT INTO k_ref.org (tenant_id, ref, kind, code, admin_unit_code, parent_ref, active, source_seq)
		  VALUES ($1, $2, $3, $4, $5, $6, true, 1)`, s.tid, n.ref, n.kind, "N"+n.ref.String()[24:], n.unit, pr)
	}
	for _, n := range nodes {
		for a, d := n.ref, 0; a != uuid.Nil; a, d = parent[a], d+1 {
			scopeExec(ctx, t, tx, `INSERT INTO k_ref.node_closure (tenant_id, ancestor, descendant, depth) VALUES ($1, $2, $3, $4)`,
				s.tid, a, n.ref, d)
		}
	}
}

const insertPlotSQL = `INSERT INTO pf_coop.plot (tenant_id, ref, code, commune_code, cooperative_ref, status, current_version,
    current_boundary_id, geom, area_ha, source, created_by)
  VALUES ($1, $2, $3, $4, $5::uuid, CASE WHEN $5::uuid IS NULL THEN 'draft' ELSE 'active' END, 1, gen_random_uuid(),
    ST_Multi(ST_GeomFromText('POLYGON((105 10, 105.001 10, 105.001 10.001, 105 10.001, 105 10))', 4326)), 1.2::numeric, 'W3.4', gen_random_uuid())`

// seedScope tạo một tenant có cây, hai thửa có HTX, hai thửa nháp, hai hộ (thành viên C1, C2). withKRef = false:
// tenant chưa có bản chiếu k_ref.
func seedScope(ctx context.Context, t *testing.T, tx pgx.Tx, withKRef bool) scopeTree {
	t.Helper()
	s := scopeTree{tid: ids.New(), p: ids.New(), x1: ids.New(), x2: ids.New(), c1: ids.New(), c2: ids.New(),
		plot1: ids.New(), plot2: ids.New(), draft1: ids.New(), draft2: ids.New(), hh1: ids.New(), hh2: ids.New()}
	if withKRef {
		seedKRef(ctx, t, tx, s)
	}
	for _, c := range []struct {
		ref  uuid.UUID
		unit string
	}{{s.c1, xa1}, {s.c2, xa2}} {
		scopeExec(ctx, t, tx, `INSERT INTO pf_party.org (tenant_id, ref, kind, code, name, admin_unit_code, last_event_id, last_seq)
		  VALUES ($1, $2, 'cooperative', $3, 'HTX W3.4', $4, gen_random_uuid(), 1)`, s.tid, c.ref, "H"+c.ref.String()[24:], c.unit)
		scopeExec(ctx, t, tx, `INSERT INTO pf_coop.cooperative (tenant_id, org_ref) VALUES ($1, $2)`, s.tid, c.ref)
	}
	for _, p := range []struct {
		ref, coop uuid.UUID
		unit      string
	}{{s.plot1, s.c1, xa1}, {s.plot2, s.c2, xa2}, {s.draft1, uuid.Nil, xa1}, {s.draft2, uuid.Nil, xa2}} {
		var coop *uuid.UUID
		if p.coop != uuid.Nil {
			v := p.coop
			coop = &v
		}
		scopeExec(ctx, t, tx, insertPlotSQL, s.tid, p.ref, "T"+p.ref.String()[24:], p.unit, coop)
	}
	for _, h := range []struct {
		ref, coop uuid.UUID
		unit      string
	}{{s.hh1, s.c1, xa1}, {s.hh2, s.c2, xa2}} {
		scopeExec(ctx, t, tx, `INSERT INTO pf_coop.household (tenant_id, household_ref, commune_code, created_by)
		  VALUES ($1, $2, $3, gen_random_uuid())`, s.tid, h.ref, h.unit)
		scopeExec(ctx, t, tx, `INSERT INTO pf_coop.household_membership (tenant_id, id, household_ref, org_ref, since_date)
		  VALUES ($1, gen_random_uuid(), $2, $3, '2026-01-01')`, s.tid, h.ref, h.coop)
	}
	return s
}

// asCoop chuyển giao dịch sang vai r_cooperative với ngữ cảnh (tenant, Nút, xã) như db.ForModule đặt; asAdmin trở
// lại vai quản trị.
func asCoop(ctx context.Context, t *testing.T, tx pgx.Tx, tid, node uuid.UUID, xa string) {
	t.Helper()
	n := ""
	if node != uuid.Nil {
		n = node.String()
	}
	scopeExec(ctx, t, tx, `SELECT set_config('role', 'r_cooperative', true), set_config('app.tenant_id', $1, true),
	  set_config('app.node_id', $2, true), set_config('app.xa_ids', $3, true)`, tid.String(), n, xa)
}

func asAdmin(ctx context.Context, t *testing.T, tx pgx.Tx) {
	t.Helper()
	scopeExec(ctx, t, tx, `RESET ROLE`)
}

func visible(ctx context.Context, t *testing.T, tx pgx.Tx, q string) []uuid.UUID {
	t.Helper()
	rows, err := tx.Query(ctx, q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(got, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return got
}

func sameSet(got []uuid.UUID, want ...uuid.UUID) bool {
	w := slices.Clone(want)
	slices.SortFunc(w, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return slices.Equal(got, w)
}

// rlsViolation: lỗi 42501 "new row violates row-level security policy".
func rlsViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "42501"
}

// tryExec chạy câu ghi trong savepoint; trả lỗi (giao dịch vẫn dùng tiếp được) và số dòng.
func tryExec(ctx context.Context, t *testing.T, tx pgx.Tx, q string, args ...any) (int64, error) {
	t.Helper()
	scopeExec(ctx, t, tx, `SAVEPOINT w34`)
	tag, err := tx.Exec(ctx, q, args...)
	if err != nil {
		scopeExec(ctx, t, tx, `ROLLBACK TO SAVEPOINT w34`)
		return 0, err
	}
	scopeExec(ctx, t, tx, `RELEASE SAVEPOINT w34`)
	return tag.RowsAffected(), nil
}

func TestCoopScopeRLS_W34(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	tx, err := env.AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s := seedScope(ctx, t, tx, true)
	other := seedScope(ctx, t, tx, true) // tenant khác, cùng hình cây
	bare := seedScope(ctx, t, tx, false) // tenant chưa có bản chiếu k_ref

	const plots = `SELECT ref FROM pf_coop.plot`
	const households = `SELECT household_ref FROM pf_coop.household`
	cases := []struct {
		name       string
		tid, node  uuid.UUID
		xa         string
		plots, hhs []uuid.UUID
	}{
		{"HTX C1: HTX mình + thửa nháp (không giới hạn xã)", s.tid, s.c1, "", []uuid.UUID{s.plot1, s.draft1, s.draft2}, []uuid.UUID{s.hh1}},
		{"tổ KN xã X1 phụ trách xã 10001", s.tid, s.x1, `{"10001"}`, []uuid.UUID{s.plot1, s.draft1}, []uuid.UUID{s.hh1}},
		{"cán bộ xã chỉ có xã 10002 (Nút X1): nhánh xã", s.tid, s.x1, `{"10002"}`, []uuid.UUID{s.plot1, s.plot2, s.draft2}, []uuid.UUID{s.hh1, s.hh2}},
		{"Cấp Tỉnh P: mọi hậu duệ", s.tid, s.p, "", []uuid.UUID{s.plot1, s.plot2, s.draft1, s.draft2}, []uuid.UUID{s.hh1, s.hh2}},
		{"không Nút (consumer/job/Admin): chỉ lọc tenant", s.tid, uuid.Nil, "", []uuid.UUID{s.plot1, s.plot2, s.draft1, s.draft2}, []uuid.UUID{s.hh1, s.hh2}},
		{"Nút chưa có trong bản chiếu: đóng (chỉ thửa nháp)", s.tid, ids.New(), "", []uuid.UUID{s.draft1, s.draft2}, nil},
		{"Nút của tenant khác: không thấy gì của tenant này", s.tid, other.p, `{"99999"}`, nil, nil},
		{"tenant khác, Nút P của tenant này: chỉ dữ liệu tenant khác qua nhánh nháp", other.tid, s.p, "", []uuid.UUID{other.draft1, other.draft2}, nil},
		{"tenant chưa có k_ref: lùi về p_tenant", bare.tid, bare.c1, "", []uuid.UUID{bare.plot1, bare.plot2, bare.draft1, bare.draft2}, []uuid.UUID{bare.hh1, bare.hh2}},
	}
	for _, c := range cases {
		asCoop(ctx, t, tx, c.tid, c.node, c.xa)
		if got := visible(ctx, t, tx, plots); !sameSet(got, c.plots...) {
			t.Errorf("%s: thửa %v, muốn %v", c.name, got, c.plots)
		}
		if got := visible(ctx, t, tx, households); !sameSet(got, c.hhs...) {
			t.Errorf("%s: hộ %v, muốn %v", c.name, got, c.hhs)
		}
		asAdmin(ctx, t, tx)
	}

	// Bảng không có commune_code: nhánh xã qua xã của Nút (k_ref.org.admin_unit_code).
	asCoop(ctx, t, tx, s.tid, s.x2, `{"10001"}`)
	if got := visible(ctx, t, tx, `SELECT org_ref FROM pf_coop.cooperative`); !sameSet(got, s.c1, s.c2) {
		t.Errorf("cooperative (X2 + xã 10001): %v, muốn C1 (xã) + C2 (Nút)", got)
	}
	asAdmin(ctx, t, tx)
	asCoop(ctx, t, tx, s.tid, s.c2, "")
	if got := visible(ctx, t, tx, `SELECT org_ref FROM pf_coop.cooperative`); !sameSet(got, s.c2) {
		t.Errorf("cooperative (C2): %v", got)
	}

	// WITH CHECK: HTX C2 không ghi được dòng ngoài phạm vi.
	if _, err := tryExec(ctx, t, tx, insertPlotSQL, s.tid, ids.New(), "TW34X1", xa1, s.c1); !rlsViolation(err) {
		t.Errorf("C2 chèn thửa của C1: lỗi %v, muốn 42501", err)
	}
	if _, err := tryExec(ctx, t, tx, insertPlotSQL, s.tid, ids.New(), "TW34X2", xa2, s.c2); err != nil {
		t.Errorf("C2 chèn thửa của mình: %v", err)
	}
	if _, err := tryExec(ctx, t, tx, insertPlotSQL, s.tid, ids.New(), "TW34X3", xa1, nil); err != nil {
		t.Errorf("C2 chèn thửa nháp: %v", err)
	}
	if n, err := tryExec(ctx, t, tx, `UPDATE pf_coop.plot SET name = 'x' WHERE ref = $1`, s.plot1); err != nil || n != 0 {
		t.Errorf("C2 sửa thửa của C1: %d dòng, lỗi %v — muốn 0 dòng", n, err)
	}
	if _, err := tryExec(ctx, t, tx, `UPDATE pf_coop.plot SET cooperative_ref = $2 WHERE ref = $1`, s.plot2, s.c1); !rlsViolation(err) {
		t.Errorf("C2 chuyển thửa sang C1: lỗi %v, muốn 42501", err)
	}
	if _, err := tryExec(ctx, t, tx, `UPDATE pf_coop.plot SET cooperative_ref = $2, status = 'active' WHERE ref = $1`, s.draft1, s.c2); err != nil {
		t.Errorf("C2 nhận thửa nháp: %v", err)
	}
	// household: WITH CHECK (true) — ghi được trước thành viên; thấy lại khi là thành viên HTX trong phạm vi.
	hh := ids.New()
	if _, err := tryExec(ctx, t, tx, `INSERT INTO pf_coop.household (tenant_id, household_ref, commune_code, created_by)
	  VALUES ($1, $2, '10002', gen_random_uuid())`, s.tid, hh); err != nil {
		t.Fatalf("C2 chèn hộ: %v", err)
	}
	if got := visible(ctx, t, tx, fmt.Sprintf(`SELECT household_ref FROM pf_coop.household WHERE household_ref = '%s'`, hh)); len(got) != 0 {
		t.Errorf("hộ chưa là thành viên đã thấy: %v", got)
	}
	if _, err := tryExec(ctx, t, tx, `INSERT INTO pf_coop.household_membership (tenant_id, id, household_ref, org_ref, since_date)
	  VALUES ($1, gen_random_uuid(), $2, $3, '2026-02-01')`, s.tid, hh, s.c1); !rlsViolation(err) {
		t.Errorf("C2 ghi thành viên HTX C1: lỗi %v, muốn 42501", err)
	}
	if _, err := tryExec(ctx, t, tx, `INSERT INTO pf_coop.household_membership (tenant_id, id, household_ref, org_ref, since_date)
	  VALUES ($1, gen_random_uuid(), $2, $3, '2026-02-01')`, s.tid, hh, s.c2); err != nil {
		t.Fatalf("C2 ghi thành viên HTX mình: %v", err)
	}
	if got := visible(ctx, t, tx, fmt.Sprintf(`SELECT household_ref FROM pf_coop.household WHERE household_ref = '%s'`, hh)); !sameSet(got, hh) {
		t.Errorf("hộ thành viên C2 không thấy: %v", got)
	}
	// Tenant khác vẫn bị chặn (p_tenant), kể cả khi p_scope cho qua (Nút NULL).
	asAdmin(ctx, t, tx)
	asCoop(ctx, t, tx, s.tid, uuid.Nil, "")
	if _, err := tryExec(ctx, t, tx, insertPlotSQL, other.tid, ids.New(), "TW34X4", xa1, other.c1); !rlsViolation(err) {
		t.Errorf("chèn thửa tenant khác: lỗi %v, muốn 42501", err)
	}
	if n, err := tryExec(ctx, t, tx, `UPDATE pf_coop.plot SET name = 'x' WHERE tenant_id = $1`, other.tid); err != nil || n != 0 {
		t.Errorf("sửa thửa tenant khác: %d dòng, lỗi %v", n, err)
	}
	// Cán bộ xã X1 (xã 10001) không ghi thửa nháp ở xã 10002.
	asAdmin(ctx, t, tx)
	asCoop(ctx, t, tx, s.tid, s.x1, `{"10001"}`)
	if _, err := tryExec(ctx, t, tx, insertPlotSQL, s.tid, ids.New(), "TW34X5", xa2, nil); !rlsViolation(err) {
		t.Errorf("cán bộ xã 10001 chèn thửa nháp xã 10002: lỗi %v, muốn 42501", err)
	}
	if _, err := tryExec(ctx, t, tx, insertPlotSQL, s.tid, ids.New(), "TW34X6", xa1, nil); err != nil {
		t.Errorf("cán bộ xã 10001 chèn thửa nháp xã mình: %v", err)
	}
	asAdmin(ctx, t, tx)

	// db.WithoutNodeScope (kiểm chồng lấn giữa HTX): trong fn thấy cả tenant, sau fn trở lại phạm vi C1.
	asCoop(ctx, t, tx, s.tid, s.c1, "")
	if err := db.WithoutNodeScope(ctx, tx, func() error {
		if got := visible(ctx, t, tx, `SELECT ref FROM pf_coop.plot WHERE tenant_id = '`+s.tid.String()+`' AND code LIKE 'T%'`); len(got) < 4 {
			t.Errorf("WithoutNodeScope: thấy %d thửa, muốn cả tenant", len(got))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := visible(ctx, t, tx, `SELECT ref FROM pf_coop.plot WHERE code LIKE 'T%' AND code NOT LIKE 'TW34%'`); !sameSet(got, s.plot1, s.draft2) {
		t.Errorf("sau WithoutNodeScope: %v, muốn lại phạm vi C1 (thửa C1 + nháp còn lại)", got)
	}
	asAdmin(ctx, t, tx)

	// Gate chung (shared 000012): rỗng sau 000134; p_scope PERMISSIVE hay chính sách thứ ba → báo.
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM shared.v_missing_rls`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("v_missing_rls: %d (%v)", n, err)
	}
	for q, want := range map[string]string{
		`CREATE POLICY p_scope ON pf_coop.sop_template USING (true)`:                       "p_scope phải RESTRICTIVE",
		`CREATE POLICY p_extra ON pf_coop.sop_template AS RESTRICTIVE USING (true)`:        "có policy ngoài p_tenant/p_scope",
		`ALTER POLICY p_tenant ON pf_coop.sop_template RENAME TO p_tenant_old`:             "thiếu policy p_tenant",
		`CREATE POLICY p_scope ON pf_coop.variety_sop_binding AS RESTRICTIVE USING (true)`: "",
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
}

// Cổng DB-14 có dữ liệu thật cỡ vừa: 1 tỉnh, 40 tổ xã, 800 HTX, 40 000 thửa. Dưới vai r_cooperative ở Nút một tổ xã,
// truy vấn thửa đọc node_closure bằng chỉ mục (không quét tuần tự), nhánh Nút không phải SubPlan chạy lại theo
// từng thửa (hashed SubPlan / semi-join / InitPlan), không gọi hàm của repo, và cho đúng số dòng.
func TestCoopScopeExplainVolume_W34(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	tx, err := env.AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tid, prov := ids.New(), ids.New()
	scopeExec(ctx, t, tx, `
	  CREATE TEMP TABLE w34_tree ON COMMIT DROP AS
	  SELECT x, c, md5($1::text || 'x' || x)::uuid AS xref, md5($1::text || 'c' || x || '.' || c)::uuid AS cref
	    FROM generate_series(1, 40) x, generate_series(1, 20) c`, tid)
	scopeExec(ctx, t, tx, `
	  INSERT INTO k_ref.node_closure (tenant_id, ancestor, descendant, depth)
	  SELECT $1::uuid, $2::uuid, $2::uuid, 0
	  UNION ALL SELECT DISTINCT $1::uuid, $2::uuid, xref, 1 FROM w34_tree
	  UNION ALL SELECT DISTINCT $1, xref, xref, 0 FROM w34_tree
	  UNION ALL SELECT $1::uuid, $2::uuid, cref, 2 FROM w34_tree
	  UNION ALL SELECT $1, xref, cref, 1 FROM w34_tree
	  UNION ALL SELECT $1, cref, cref, 0 FROM w34_tree`, tid, prov)
	// Nhiễu: 30 tenant khác, mỗi tenant 2 000 dòng node_closure (tenant đo chỉ ≈ 4% bảng).
	scopeExec(ctx, t, tx, `
	  INSERT INTO k_ref.node_closure (tenant_id, ancestor, descendant, depth)
	  SELECT md5($1::text || (g % 30))::uuid, gen_random_uuid(), gen_random_uuid(), 1 FROM generate_series(1, 60000) g`, tid)
	scopeExec(ctx, t, tx, `
	  INSERT INTO pf_coop.plot (tenant_id, ref, code, commune_code, cooperative_ref, status, current_version,
	    current_boundary_id, geom, area_ha, source, created_by)
	  SELECT $1, gen_random_uuid(), 'V' || x || '.' || c || '.' || p, lpad(x::text, 5, '0'), cref, 'active', 1, gen_random_uuid(),
	    ST_Multi(ST_GeomFromText('POLYGON((105 10, 105.001 10, 105.001 10.001, 105 10.001, 105 10))', 4326)), 1.2::numeric, 'W3.4', gen_random_uuid()
	    FROM w34_tree, generate_series(1, 50) p`, tid)
	// Hộ: 10 hộ mỗi HTX, thành viên đang mở (Nút của hộ suy từ household_membership — thiết kế mục 9, R-6).
	scopeExec(ctx, t, tx, `
	  INSERT INTO pf_party.org (tenant_id, ref, kind, code, name, admin_unit_code, last_event_id, last_seq)
	  SELECT $1, cref, 'cooperative', 'V' || x || '.' || c, 'HTX W3.4', lpad(x::text, 5, '0'), gen_random_uuid(), 1 FROM w34_tree`, tid)
	scopeExec(ctx, t, tx, `
	  CREATE TEMP TABLE w34_hh ON COMMIT DROP AS
	  SELECT gen_random_uuid() AS hh, cref, x FROM w34_tree, generate_series(1, 10)`)
	scopeExec(ctx, t, tx, `
	  INSERT INTO pf_coop.household (tenant_id, household_ref, commune_code, created_by)
	  SELECT $1, hh, lpad(x::text, 5, '0'), gen_random_uuid() FROM w34_hh`, tid)
	scopeExec(ctx, t, tx, `
	  INSERT INTO pf_coop.household_membership (tenant_id, id, household_ref, org_ref, since_date)
	  SELECT $1, gen_random_uuid(), hh, cref, '2026-01-01' FROM w34_hh`, tid)
	for _, tb := range []string{"k_ref.node_closure", "pf_coop.plot", "pf_coop.household", "pf_coop.household_membership"} {
		scopeExec(ctx, t, tx, `ANALYZE `+tb)
	}
	var xref uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT xref FROM w34_tree WHERE x = 7 LIMIT 1`).Scan(&xref); err != nil {
		t.Fatal(err)
	}
	fns := userFunctions(ctx, t, tx, migrations.PF)
	asCoop(ctx, t, tx, tid, xref, "")
	checkScopePlan(ctx, t, tx, fns, `SELECT ref, code FROM pf_coop.plot`, 20*50)
	checkScopePlan(ctx, t, tx, fns, `SELECT household_ref, commune_code FROM pf_coop.household`, 20*10)
}

// checkScopePlan chạy EXPLAIN ANALYZE câu q dưới ngữ cảnh hiện hành và kiểm: đúng số dòng; node_closure chỉ được
// đọc bằng chỉ mục; không SubPlan nào chạy lại theo hàng (> 1 vòng); không gọi hàm của repo theo hàng.
func checkScopePlan(ctx context.Context, t *testing.T, tx pgx.Tx, fns *regexp.Regexp, q string, want int) {
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
	plan := out[0].Plan
	if rows, _ := plan["Actual Rows"].(float64); int(rows) != want {
		t.Errorf("%s: %v dòng thấy được ở Nút tổ xã, muốn %d", q, rows, want)
	}
	var closureIdx, closureSeq int
	var perRow []string
	walkPlan(plan, false, func(n map[string]any, _ bool) {
		rel, _ := n["Relation Name"].(string)
		typ, _ := n["Node Type"].(string)
		if rel == "node_closure" {
			if strings.Contains(typ, "Index") {
				closureIdx++
			} else {
				closureSeq++
			}
		}
		name, _ := n["Subplan Name"].(string)
		loops, _ := n["Actual Loops"].(float64)
		if strings.HasPrefix(name, "SubPlan") && loops > 1 {
			perRow = append(perRow, fmt.Sprintf("%s (%s %s, %v vòng)", name, typ, rel, loops))
		}
	})
	if closureIdx == 0 || closureSeq > 0 {
		t.Errorf("%s: node_closure %d lần quét chỉ mục, %d lần quét khác — muốn chỉ dùng chỉ mục\n%s", q, closureIdx, closureSeq, raw)
	}
	if len(perRow) > 0 {
		t.Errorf("%s: SubPlan chạy lại theo hàng: %v\n%s", q, perRow, raw)
	}
	if found := perRowCalls(plan, fns); len(found) > 0 {
		t.Errorf("%s: gọi hàm của repo theo hàng: %v", q, found)
	}
	t.Logf("W3.4 EXPLAIN %s: %v ms, %d lần quét chỉ mục node_closure", q, plan["Actual Total Time"], closureIdx)
}
