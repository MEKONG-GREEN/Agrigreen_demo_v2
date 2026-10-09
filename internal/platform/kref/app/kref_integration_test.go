package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fx struct {
	t      *testing.T
	ctx    context.Context
	worker tenant.Beginner // app_worker → r_kref
	mod    *Module
	now    time.Time
}

func newFx(t *testing.T) *fx {
	env := pgtest.Get(t)
	p := env.Pool(t, migrations.PF, dbadmin.AppWorker)
	now := time.Now().UTC()
	return &fx{t: t, ctx: context.Background(), worker: db.ForModule(p, DBModule), mod: New(p, clock.NewFixed(now)), now: now}
}

// send áp một sự kiện qua inbox như consumer pf.kref; trả processed.
func (f *fx) send(tid uuid.UUID, typ string, payload any) (bool, error) {
	f.t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	env := outbox.Envelope{EventID: ids.New(), EventType: typ, SchemaVersion: 1, TenantID: tid, OccurredAt: f.now, RecordedAt: f.now,
		Producer: "pf/party", Payload: raw}
	return inbox.Process(f.ctx, f.worker, Consumer, env, f.mod.Handler())
}

func (f *fx) must(tid uuid.UUID, typ string, payload any) {
	f.t.Helper()
	if _, err := f.send(tid, typ, payload); err != nil {
		f.t.Fatalf("%s: %v", typ, err)
	}
}

func orgEv(ref, parent uuid.UUID, seq int64) map[string]any {
	ev := map[string]any{"org_ref": ref, "kind": "extension_team", "code": "C" + ref.String()[:6], "admin_unit_code": "31135",
		"parent_ref": nil, "active": true, "seq": seq, "actor_ref": ids.New(), "event_time": time.Now().Format(time.RFC3339)}
	if parent != uuid.Nil {
		ev["parent_ref"] = parent
	}
	return ev
}

func closureOf(p Projection, desc uuid.UUID) map[uuid.UUID]int {
	out := map[uuid.UUID]int{}
	for _, c := range p.Closure {
		if c.Descendant == desc {
			out[c.Ancestor] = c.Depth
		}
	}
	return out
}

func pgCode(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return ""
}

// ADR-0029 §3: sự kiện → dòng k_ref; Nút di chuyển dựng lại bao đóng cả nhánh; idempotent; sai thứ tự → lỗi
// thử lại (không ghi nửa vời); vô hiệu hóa; đơn vị hành chính đóng thắng mở; cấp phép duyệt rồi thu hồi.
func TestEventsProjectIntoKRef_W25(t *testing.T) {
	f := newFx(t)
	tid := ids.New()
	root, p1, p2, team, coop := ids.New(), ids.New(), ids.New(), ids.New(), ids.New()

	// Con tới trước cha → ErrNotYetProjected, không ghi gì (giao lại sau).
	if _, err := f.send(tid, EventOrgCreated, orgEv(team, p1, 1)); !errors.Is(err, ErrNotYetProjected) {
		t.Fatalf("cha chưa có phải ErrNotYetProjected: %v", err)
	}
	for _, e := range []map[string]any{orgEv(root, uuid.Nil, 1), orgEv(p1, root, 1), orgEv(p2, root, 1), orgEv(team, p1, 1), orgEv(coop, team, 1)} {
		f.must(tid, EventOrgCreated, e)
	}
	snap, err := f.mod.Snapshot(f.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Orgs) != 5 || len(snap.Closure) != 12 { // 5 tự thân + root→4 + p1→team,coop + team→coop
		t.Fatalf("org %d, closure %d: %v", len(snap.Orgs), len(snap.Closure), snap.Closure)
	}
	if got := closureOf(snap, coop); got[root] != 3 || got[p1] != 2 || got[team] != 1 || got[coop] != 0 || len(got) != 4 {
		t.Fatalf("tổ tiên của coop: %v", got)
	}

	// Di chuyển nhánh team (kèm coop) từ p1 sang p2.
	mv := orgEv(team, p2, 2)
	mv["prev_parent_ref"], mv["prev_admin_unit_code"], mv["reason"] = p1, "31135", "Sáp nhập theo nghị quyết"
	f.must(tid, EventOrgMoved, mv)
	snap, _ = f.mod.Snapshot(f.ctx, tid)
	if got := closureOf(snap, coop); got[p2] != 2 || got[root] != 3 || got[team] != 1 || len(got) != 4 {
		t.Fatalf("sau di chuyển, tổ tiên của coop: %v", got)
	}
	if _, ok := closureOf(snap, team)[p1]; ok {
		t.Fatal("p1 không còn là tổ tiên của team")
	}
	// Giao lại sự kiện cũ (seq nhỏ hơn) không làm lùi trạng thái.
	f.must(tid, EventOrgMoved, func() map[string]any { e := orgEv(team, p1, 1); e["reason"] = "cũ hơn, bỏ qua"; return e }())
	if got := closureOf(mustSnap(t, f, tid), coop); got[p2] != 2 {
		t.Fatalf("sự kiện cũ không được áp: %v", got)
	}
	// Vòng (đưa p2 xuống dưới coop) bị từ chối.
	if _, err := f.send(tid, EventOrgMoved, orgEv(p2, coop, 2)); err == nil {
		t.Fatal("tạo vòng phải lỗi")
	}

	// Vô hiệu hóa / kích hoạt lại (theo seq).
	f.must(tid, EventOrgDeactivated, map[string]any{"org_ref": coop, "seq": 2})
	f.must(tid, EventOrgReactivated, map[string]any{"org_ref": coop, "seq": 1}) // cũ hơn → bỏ qua
	for _, o := range mustSnap(t, f, tid).Orgs {
		if o.Ref == coop && o.Active {
			t.Fatal("coop phải đang vô hiệu hóa")
		}
	}
	if _, err := f.send(tid, EventOrgDeactivated, map[string]any{"org_ref": ids.New(), "seq": 1}); !errors.Is(err, ErrNotYetProjected) {
		t.Fatalf("Nút lạ phải ErrNotYetProjected: %v", err)
	}

	// Đơn vị hành chính: đóng thắng mở.
	unit := map[string]any{"code": "31135", "name": "Ninh Kiều", "level": "commune", "parent_code": "92", "valid_from": "2020-01-01",
		"valid_to": nil, "successor_codes": []string{}, "change": "created"}
	closed := map[string]any{"code": "31135", "name": "Ninh Kiều", "level": "commune", "parent_code": "92", "valid_from": "2020-01-01",
		"valid_to": "2025-07-01", "successor_codes": []string{"31136"}, "change": "closed"}
	f.must(tid, EventAdminUnitChanged, closed)
	f.must(tid, EventAdminUnitChanged, unit)
	us := mustSnap(t, f, tid).AdminUnits
	if len(us) != 1 || us[0].ValidTo == nil || us[0].ValidTo.Format(time.DateOnly) != "2025-07-01" || !slices.Equal(us[0].SuccessorCodes, []string{"31136"}) {
		t.Fatalf("đơn vị hành chính: %+v", us)
	}

	// Cấp phép: duyệt → có; thu hồi → mất; duyệt cũ giao lại sau thu hồi (seq nhỏ hơn) không mở lại nếu dòng còn.
	g := ids.New()
	grant := func(seq int64) map[string]any {
		return map[string]any{"grant_id": g, "grantee_tenant_id": ids.New(), "kinds": []string{"plot_directory", "cooperative_directory"},
			"valid_from": "2026-01-01T00:00:00Z", "valid_to": "2027-01-01T00:00:00Z", "seq": seq, "actor_ref": ids.New(), "event_time": "2026-01-01T00:00:00Z"}
	}
	f.must(tid, EventShareGrantApprove, grant(2))
	if gs := mustSnap(t, f, tid).ShareGrants; len(gs) != 1 || !slices.Equal(gs[0].Kinds, []string{"cooperative_directory", "plot_directory"}) {
		t.Fatalf("cấp phép đang hiệu lực: %+v", gs)
	}
	f.must(tid, EventShareGrantRevoke, grant(3))
	if gs := mustSnap(t, f, tid).ShareGrants; len(gs) != 0 {
		t.Fatalf("thu hồi phải bỏ dòng: %+v", gs)
	}
	// W4.1: payload v2 (trường tùy chọn grant_class, module_table, scope_node_ref, purpose_code, basis_ref; intra_platform
	// không kinds) — W4.2 chiếu đủ cột v2 vào k_ref.share_grant_active và dựng share_scope (mọi Nút dưới root).
	v2 := ids.New()
	f.must(tid, EventShareGrantApprove, map[string]any{"grant_id": v2, "grantee_tenant_id": ids.New(), "kinds": []string{},
		"valid_from": "2026-01-01T00:00:00Z", "valid_to": "2027-01-01T00:00:00Z", "seq": 2, "actor_ref": ids.New(), "event_time": "2026-01-01T00:00:00Z",
		"grant_class": "intra_platform", "module_table": "pf/cooperative:household", "scope_node_ref": root, "purpose_code": "khuyen-nong",
		"basis_ref": "TTCS-2026-001"})
	if sn := mustSnap(t, f, tid); len(sn.ShareGrants) != 1 || sn.ShareGrants[0].GrantID != v2 || len(sn.ShareGrants[0].Kinds) != 0 ||
		sn.ShareGrants[0].Class != port.GrantClassIntraPlatform || sn.ShareGrants[0].ModuleTable != "pf/cooperative:household" ||
		sn.ShareGrants[0].ScopeNode != root || sn.ShareGrants[0].Purpose != "khuyen-nong" || sn.ShareGrants[0].Basis != "TTCS-2026-001" ||
		len(sn.ShareScope) != len(sn.Orgs) {
		t.Fatalf("cấp phép v2 intra_platform: %+v, share_scope %d dòng / %d Nút", sn.ShareGrants, len(sn.ShareScope), len(sn.Orgs))
	}
	f.must(tid, EventShareGrantRevoke, map[string]any{"grant_id": v2, "grantee_tenant_id": ids.New(), "kinds": []string{},
		"valid_from": "2026-01-01T00:00:00Z", "valid_to": "2027-01-01T00:00:00Z", "seq": 3, "actor_ref": ids.New(), "event_time": "2026-01-01T00:00:00Z",
		"grant_class": "intra_platform"})
	if sn := mustSnap(t, f, tid); len(sn.ShareGrants) != 0 || len(sn.ShareScope) != 0 {
		t.Fatalf("thu hồi v2 phải bỏ dòng: %+v, share_scope %d", sn.ShareGrants, len(sn.ShareScope))
	}

	// Idempotent theo event_id (inbox).
	raw, _ := json.Marshal(orgEv(ids.New(), root, 1))
	env := outbox.Envelope{EventID: ids.New(), EventType: EventOrgCreated, SchemaVersion: 1, TenantID: tid, OccurredAt: f.now, Producer: "pf/party", Payload: raw}
	for i, want := range []bool{true, false} {
		got, err := inbox.Process(f.ctx, f.worker, Consumer, env, f.mod.Handler())
		if err != nil || got != want {
			t.Fatalf("lần %d: processed=%v err=%v", i, got, err)
		}
	}
	// Payload hỏng → lỗi (giao lại rồi DLQ), không panic.
	if _, err := f.send(tid, EventOrgCreated, map[string]any{"org_ref": "x"}); err == nil {
		t.Fatal("payload hỏng phải lỗi")
	}
}

func mustSnap(t *testing.T, f *fx, tid uuid.UUID) Projection {
	t.Helper()
	p, err := f.mod.Snapshot(f.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type memSource struct {
	orgs   []port.Org
	units  []port.AdminUnit
	grants []port.ShareGrant
}

func (m memSource) OrgNodes(context.Context, uuid.UUID) ([]port.Org, error) { return m.orgs, nil }
func (m memSource) AdminUnitVersions(context.Context, uuid.UUID) ([]port.AdminUnit, error) {
	return m.units, nil
}
func (m memSource) ActiveShareGrants(context.Context, uuid.UUID) ([]port.ShareGrant, error) {
	return m.grants, nil
}

// ADR-0029 §5: Rebuild xóa + dựng lại k_ref của tenant từ nguồn trong một giao dịch, khớp bản dựng độc lập
// (Go) — kể cả khi k_ref đang lệch; tenant khác không bị đụng; nguồn hỏng (vòng) → lỗi, giữ nguyên trạng thái.
func TestRebuildMatchesSource_W25(t *testing.T) {
	f := newFx(t)
	tid, other := ids.New(), ids.New()
	a, b, c := ids.New(), ids.New(), ids.New()
	f.must(other, EventOrgCreated, orgEv(ids.New(), uuid.Nil, 1))
	f.must(tid, EventOrgCreated, orgEv(ids.New(), uuid.Nil, 1)) // dòng thừa, nguồn không có
	to := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	src := memSource{
		orgs: []port.Org{{Ref: a, Kind: "ttkn_national", Code: "QG", Active: true, Seq: 1},
			{Ref: b, Kind: "ttkn_province", Code: "P92", AdminUnitCode: "92", ParentRef: a, Active: true, Seq: 3},
			{Ref: c, Kind: "cooperative", Code: "HTX1", AdminUnitCode: "31135", ParentRef: b, Active: false, Seq: 2}},
		units: []port.AdminUnit{{Code: "92", ValidFrom: time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC), Name: "Cần Thơ", Level: "province"},
			{Code: "31135", ValidFrom: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), ValidTo: &to, Name: "Ninh Kiều", Level: "commune",
				ParentCode: "92", SuccessorCodes: []string{"31136", "31137"}}},
		grants: []port.ShareGrant{{GrantID: ids.New(), Grantee: other, Kinds: []string{"cooperative_directory"},
			ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 123000, time.UTC), ValidTo: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), Seq: 2}},
	}
	sources := port.Sources{Orgs: src, AdminUnits: src, ShareGrants: src,
		Tenants: func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{tid}, nil }}
	res, err := f.mod.Rebuild(f.ctx, sources)
	if err != nil || res.Tenants != 1 || res.Rows[port.RelNodeClosure] != 6 {
		t.Fatalf("rebuild: %+v %v", res, err)
	}
	want, err := Expected(f.ctx, sources, tid)
	if err != nil {
		t.Fatal(err)
	}
	got := mustSnap(t, f, tid)
	if d := Diff(got, want); len(d) != 0 {
		t.Fatalf("sau dựng lại lệch %v:\n%+v\n%+v", d, got, want)
	}
	if n := len(mustSnap(t, f, other).Orgs); n != 1 {
		t.Fatalf("tenant khác bị đụng: %d", n)
	}
	// Cộng dồn tiếp sau dựng lại vẫn đúng (seq của nguồn được giữ).
	f.must(tid, EventOrgMoved, func() map[string]any {
		e := orgEv(c, a, 3)
		e["kind"], e["code"], e["active"], e["reason"] = "cooperative", "HTX1", false, "Chuyển thẳng về quốc gia"
		return e
	}())
	if got := closureOf(mustSnap(t, f, tid), c); got[a] != 1 || len(got) != 2 {
		t.Fatalf("di chuyển sau dựng lại: %v", got)
	}
	// Nguồn có vòng → lỗi, k_ref giữ nguyên.
	before := mustSnap(t, f, tid)
	bad := src
	bad.orgs = []port.Org{{Ref: a, Kind: "x", Code: "A", ParentRef: b, Active: true, Seq: 1}, {Ref: b, Kind: "x", Code: "B", ParentRef: a, Active: true, Seq: 1}}
	if _, err := f.mod.RebuildTenant(f.ctx, port.Sources{Orgs: bad, AdminUnits: src, ShareGrants: src}, tid); err == nil {
		t.Fatal("nguồn có vòng phải lỗi")
	}
	if d := Diff(before, mustSnap(t, f, tid)); len(d) != 0 {
		t.Fatalf("dựng lỗi không được đổi k_ref: %v", d)
	}
}

// RLS + quyền (R-DB-05, ADR-0029 §2): chỉ r_kref ghi; r_* khác chỉ SELECT; dòng của tenant khác không thấy;
// cấp phép thấy ở cả bên cấp lẫn bên nhận; vai đăng nhập không ghép pf/kref không SET ROLE r_kref được.
func TestKRefRLSAndGrants_W25(t *testing.T) {
	f := newFx(t)
	env := pgtest.Get(t)
	tid, grantee := ids.New(), ids.New()
	a := ids.New()
	f.must(tid, EventOrgCreated, orgEv(a, uuid.Nil, 1))
	f.must(tid, EventShareGrantApprove, map[string]any{"grant_id": ids.New(), "grantee_tenant_id": grantee, "kinds": []string{"plot_directory"},
		"valid_from": "2026-01-01T00:00:00Z", "valid_to": "2027-01-01T00:00:00Z", "seq": 2, "actor_ref": ids.New(), "event_time": "2026-01-01T00:00:00Z"})

	count := func(b tenant.Beginner, t2 uuid.UUID, q string) (int, error) {
		var n int
		err := tenant.WithTenantTx(f.ctx, b, t2, func(tx pgx.Tx) error { return tx.QueryRow(f.ctx, q).Scan(&n) })
		return n, err
	}
	// r_content (qua app_platform + SET LOCAL ROLE) và vai cũ party_app (thành viên r_party) đọc được.
	content := db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), db.Module("pf/content"))
	party := env.Pool(t, migrations.PF, "party_app")
	for name, b := range map[string]tenant.Beginner{"r_content": content, "party_app": party} {
		if n, err := count(b, tid, "SELECT count(*) FROM k_ref.node_closure WHERE ancestor = '"+a.String()+"'"); err != nil || n != 1 {
			t.Errorf("%s đọc node_closure của tenant mình: %d %v", name, n, err)
		}
		if n, err := count(b, ids.New(), "SELECT count(*) FROM k_ref.org"); err != nil || n != 0 {
			t.Errorf("%s thấy org của tenant khác: %d %v", name, n, err)
		}
		if n, err := count(b, grantee, "SELECT count(*) FROM k_ref.share_grant_active"); err != nil || n != 1 {
			t.Errorf("%s (bên nhận) thấy cấp phép: %d %v", name, n, err)
		}
		if n, err := count(b, grantee, "SELECT count(*) FROM k_ref.org"); err != nil || n != 0 {
			t.Errorf("%s (bên nhận) không thấy cây Nút bên cấp: %d %v", name, n, err)
		}
		for _, q := range []string{
			"INSERT INTO k_ref.org (tenant_id, ref, kind, code, active, source_seq) VALUES (k_ref.ctx_uuid('app.tenant_id'), gen_random_uuid(), 'x', 'x', true, 1)",
			"UPDATE k_ref.org SET active = false",
			"DELETE FROM k_ref.node_closure",
		} {
			err := tenant.WithTenantTx(f.ctx, b, tid, func(tx pgx.Tx) error { _, e := tx.Exec(f.ctx, q); return e })
			if pgCode(err) != "42501" {
				t.Errorf("%s ghi k_ref phải 42501: %q → %v", name, q, err)
			}
		}
	}
	// r_kref ghi được, nhưng WITH CHECK chặn ghi sang tenant khác.
	err := tenant.WithTenantTx(f.ctx, f.worker, tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(f.ctx, "INSERT INTO k_ref.org (tenant_id, ref, kind, code, active, source_seq) VALUES ($1, gen_random_uuid(), 'x', 'x', true, 1)", ids.New())
		return e
	})
	if pgCode(err) != "42501" {
		t.Fatalf("r_kref ghi tenant khác phải bị RLS chặn (42501): %v", err)
	}
	// app_platform không ghép pf/kref → không SET ROLE r_kref.
	err = db.WithModuleTenantTx(f.ctx, env.Pool(t, migrations.PF, dbadmin.AppPlatform), DBModule, tid, func(pgx.Tx) error { return nil })
	if pgCode(err) != "42501" {
		t.Fatalf("app_platform SET ROLE r_kref phải 42501: %v", err)
	}
	// share_scope rỗng, đọc được.
	if n, err := count(content, tid, "SELECT count(*) FROM k_ref.share_scope"); err != nil || n != 0 {
		t.Fatalf("share_scope: %d %v", n, err)
	}
}

// mkg 000022: k_ref cùng hình ở cụm mkg — rỗng, vai module mkg đọc được, không ghi được.
func TestKRefInMKGCluster_W25(t *testing.T) {
	env := pgtest.Get(t)
	p := env.Pool(t, migrations.MKG, "hub_app")
	ctx := context.Background()
	err := tenant.WithTenantTx(ctx, p, ids.New(), func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM k_ref.org) + (SELECT count(*) FROM k_ref.node_closure)
		  + (SELECT count(*) FROM k_ref.admin_unit) + (SELECT count(*) FROM k_ref.share_grant_active)
		  + (SELECT count(*) FROM k_ref.share_scope WHERE viewer_tenant = k_ref.ctx_uuid('app.tenant_id'))`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("k_ref cụm mkg phải rỗng: %d", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = tenant.WithTenantTx(ctx, p, ids.New(), func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "DELETE FROM k_ref.org")
		return e
	})
	if pgCode(err) != "42501" {
		t.Fatalf("hub_app ghi k_ref phải 42501: %v", err)
	}
}
