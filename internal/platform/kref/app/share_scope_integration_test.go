package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func intraGrant(id, grantee, scope uuid.UUID, table string, seq int64) map[string]any {
	return map[string]any{"grant_id": id, "grantee_tenant_id": grantee, "kinds": []string{},
		"valid_from": "2026-01-01T00:00:00Z", "valid_to": "2027-01-01T00:00:00Z", "seq": seq, "actor_ref": ids.New(),
		"event_time": "2026-01-01T00:00:00Z", "grant_class": "intra_platform", "module_table": table, "scope_node_ref": scope,
		"purpose_code": "khuyen-nong", "basis_ref": "TTCS-2026-001"}
}

// scopeNodes trả Nút của share_scope theo cấp phép g (khóa bảng phải là key).
func scopeNodes(t *testing.T, p Projection, g uuid.UUID, key string) map[uuid.UUID]bool {
	t.Helper()
	out := map[uuid.UUID]bool{}
	for _, r := range p.ShareScope {
		if r.Grant != g {
			continue
		}
		if r.ModuleTable != key || r.Purpose != "khuyen-nong" {
			t.Fatalf("dòng share_scope sai khóa/mục đích: %+v", r)
		}
		out[r.Node] = true
	}
	return out
}

func sameNodes(got map[uuid.UUID]bool, want ...uuid.UUID) bool {
	if len(got) != len(want) {
		return false
	}
	for _, w := range want {
		if !got[w] {
			return false
		}
	}
	return true
}

// W4.2 (kiến trúc §17.3, ADR-0030 mục 3): worker dựng share_scope = cấp phép intra_platform × hậu duệ của Nút phạm
// vi, mỗi sự kiện trong một giao dịch; cây Nút đổi → dựng lại; partner / share không khai → không dòng; thu hồi →
// mất dòng; Rebuild = bản dựng độc lập trong Go; đơn vị xem đọc dòng của mình, tenant thứ ba không thấy.
func TestShareScopeWorker_W42(t *testing.T) {
	f := newFx(t)
	tid, viewer := ids.New(), ids.New()
	root, p1, p2, team, coop := ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	for _, e := range []map[string]any{orgEv(root, uuid.Nil, 1), orgEv(p1, root, 1), orgEv(p2, root, 1), orgEv(team, p1, 1), orgEv(coop, team, 1)} {
		f.must(tid, EventOrgCreated, e)
	}
	g := ids.New()
	f.must(tid, EventShareGrantApprove, intraGrant(g, viewer, p1, "pf/cooperative:plot", 2))
	if got := scopeNodes(t, mustSnap(t, f, tid), g, "pf_coop.plot"); !sameNodes(got, p1, team, coop) {
		t.Fatalf("share_scope sau duyệt: %v", got)
	}
	// Nút mới dưới p1 → thêm dòng (sự kiện Nút dựng lại share_scope cùng giao dịch).
	extra := ids.New()
	f.must(tid, EventOrgCreated, orgEv(extra, p1, 1))
	if got := scopeNodes(t, mustSnap(t, f, tid), g, "pf_coop.plot"); !sameNodes(got, p1, team, coop, extra) {
		t.Fatalf("share_scope sau thêm Nút: %v", got)
	}
	// Chuyển nhánh team (kèm coop) sang p2 → ra khỏi phạm vi.
	mv := orgEv(team, p2, 2)
	mv["prev_parent_ref"], mv["prev_admin_unit_code"], mv["reason"] = p1, "31135", "Sáp nhập theo nghị quyết"
	f.must(tid, EventOrgMoved, mv)
	if got := scopeNodes(t, mustSnap(t, f, tid), g, "pf_coop.plot"); !sameNodes(got, p1, extra) {
		t.Fatalf("share_scope sau chuyển nhánh: %v", got)
	}
	// Cấp phép partner và share không khai trong `shares` → không dòng; máy (pf/mechanization:machine) → khóa riêng.
	partner, undeclared, mach := ids.New(), ids.New(), ids.New()
	f.must(tid, EventShareGrantApprove, map[string]any{"grant_id": partner, "grantee_tenant_id": viewer, "kinds": []string{"plot_directory"},
		"valid_from": "2026-01-01T00:00:00Z", "valid_to": "2027-01-01T00:00:00Z", "seq": 2, "actor_ref": ids.New(),
		"event_time": "2026-01-01T00:00:00Z", "grant_class": "partner"})
	f.must(tid, EventShareGrantApprove, intraGrant(undeclared, viewer, root, "pf/cooperative:cooperative", 2))
	f.must(tid, EventShareGrantApprove, intraGrant(mach, viewer, p2, "pf/mechanization:machine", 2))
	snap := mustSnap(t, f, tid)
	if len(scopeNodes(t, snap, partner, "")) != 0 || len(scopeNodes(t, snap, undeclared, "")) != 0 {
		t.Fatalf("partner / share không khai không được sinh dòng: %+v", snap.ShareScope)
	}
	if got := scopeNodes(t, snap, mach, "pf_mechanization.machine"); !sameNodes(got, p2, team, coop) {
		t.Fatalf("share_scope máy: %v", got)
	}
	// Đọc: đơn vị xem thấy dòng của mình, đơn vị chủ thấy dòng mình cấp, tenant thứ ba không thấy; vai khác r_kref không ghi.
	env := pgtest.Get(t)
	content := db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), db.Module("pf/content"))
	count := func(b tenant.Beginner, as uuid.UUID) int {
		var n int
		if err := tenant.WithTenantTx(f.ctx, b, as, func(tx pgx.Tx) error {
			return tx.QueryRow(f.ctx, `SELECT count(*) FROM k_ref.share_scope WHERE owner_tenant = $1`, tid).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if v, o, x := count(content, viewer), count(content, tid), count(content, ids.New()); v != 5 || o != 5 || x != 0 {
		t.Fatalf("đọc share_scope: xem %d, chủ %d, thứ ba %d (muốn 5, 5, 0)", v, o, x)
	}
	err := tenant.WithTenantTx(f.ctx, content, viewer, func(tx pgx.Tx) error {
		_, e := tx.Exec(f.ctx, `DELETE FROM k_ref.share_scope`)
		return e
	})
	if pgCode(err) != "42501" {
		t.Fatalf("r_content ghi share_scope phải 42501: %v", err)
	}
	// Thu hồi cấp phép thửa → chỉ mất dòng của nó (dòng máy còn).
	rv := intraGrant(g, viewer, p1, "pf/cooperative:plot", 3)
	f.must(tid, EventShareGrantRevoke, rv)
	snap = mustSnap(t, f, tid)
	if len(scopeNodes(t, snap, g, "")) != 0 || len(scopeNodes(t, snap, mach, "pf_mechanization.machine")) != 3 {
		t.Fatalf("sau thu hồi: %+v", snap.ShareScope)
	}

	// Rebuild từ nguồn khớp bản dựng độc lập (gồm share_scope), kể cả khi k_ref đang lệch.
	src := memSource{
		orgs: []port.Org{{Ref: root, Kind: "ttkn_national", Code: "QG", Active: true, Seq: 1},
			{Ref: p1, Kind: "ttkn_province", Code: "P1", ParentRef: root, Active: true, Seq: 1},
			{Ref: coop, Kind: "cooperative", Code: "HTX", ParentRef: p1, Active: true, Seq: 3}},
		grants: []port.ShareGrant{{GrantID: mach, Grantee: viewer, Kinds: []string{}, ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			ValidTo: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), Seq: 2, Class: port.GrantClassIntraPlatform,
			ModuleTable: "pf/cooperative:season", ScopeNode: p1, Purpose: "khuyen-nong", Basis: "TTCS-2026-001"}},
	}
	sources := port.Sources{Orgs: src, AdminUnits: src, ShareGrants: src,
		Tenants: func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{tid}, nil }}
	if _, err := f.mod.Rebuild(f.ctx, sources); err != nil {
		t.Fatal(err)
	}
	want, err := Expected(f.ctx, sources, tid)
	if err != nil {
		t.Fatal(err)
	}
	got := mustSnap(t, f, tid)
	if d := Diff(got, want); len(d) != 0 {
		t.Fatalf("dựng lại lệch %v:\n%+v\n%+v", d, got.ShareScope, want.ShareScope)
	}
	if nodes := scopeNodes(t, got, mach, "pf_coop.season"); !sameNodes(nodes, p1, coop) {
		t.Fatalf("share_scope sau dựng lại: %v", nodes)
	}
}
