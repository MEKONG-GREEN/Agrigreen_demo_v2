package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// deliver giao một phong bì van MKG vào consumer pf.cropcycle.htx_collection_valve (qua shared.inbox).
func (f *fixture) deliver(tid uuid.UUID, evType string, payload any) (uuid.UUID, error) {
	f.t.Helper()
	b, _ := json.Marshal(payload)
	env := outbox.Envelope{EventID: ids.New(), EventType: evType, SchemaVersion: 1, TenantID: tid, OccurredAt: f.clk.Now(),
		RecordedAt: f.clk.Now(), Producer: "mkg/fieldops", Payload: b}
	_, err := inbox.Process(context.Background(), f.db, port.CollectionValveConsumer, env, f.mod.CollectionValveHandler())
	return env.EventID, err
}

func schedulePayload(ref, coop uuid.UUID, status string, rev int) map[string]any {
	return map[string]any{"schedule_ref": ref, "htx_ref": coop, "commune_code": "31117", "window_from": "2026-10-01",
		"window_to": "2026-10-05", "status": status, "revision": rev}
}

func statementPayload(ref, coop, plot uuid.UUID, rolls, rev int) map[string]any {
	return map[string]any{"statement_ref": ref, "htx_ref": coop, "plot_ref": plot, "season_ref": nil, "collected_on": "2026-09-28",
		"rolls": rolls, "revision": rev}
}

// countOutbox đếm sự kiện nền tảng evType của tenant (admin pool — vai module không đọc outbox).
func (f *fixture) countOutbox(evType string) int {
	f.t.Helper()
	pool := pgtest.Get(f.t).AdminPool(f.t, migrations.PF)
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2",
		f.tenant, evType).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// RB-6 chiều mua (G3-2): van collection_schedule → bảng chiếu lịch gom của HTX. Idempotent theo
// (ref, revision) kể cả khi MKG phát lại với event_id mới; bản cũ đến muộn không thay bản mới;
// mỗi revision mới hiện hành phát một pf.cropcycle.collection_schedule.received (báo quản lý HTX).
func TestCollectionValveScheduleLatestRevision_RB6_BuySide(t *testing.T) {
	f := newFixture(t)
	ref := ids.New()
	for _, p := range []map[string]any{
		schedulePayload(ref, f.coopA, "planned", 1),
		schedulePayload(ref, f.coopA, "planned", 1), // phát lại (event_id khác)
		schedulePayload(ref, f.coopA, "in_progress", 3),
		schedulePayload(ref, f.coopA, "confirmed", 2), // bản cũ đến muộn
	} {
		if _, err := f.deliver(f.mkg, port.EventMKGCollectionSchedulePublished, p); err != nil {
			t.Fatal(err)
		}
	}
	r := f.managerA().get("/v1/cropcycle/htx-collection/schedules")
	expect(t, r, http.StatusOK, "lịch gom HTX A")
	items := list(r.body, "items")
	if len(items) != 1 || str(items[0], "status") != "in_progress" || num(items[0], "revision") != 3 {
		t.Fatalf("lịch gom phải là revision 3: %s", r.raw)
	}
	if n := f.countOutbox(port.EventCollectionScheduleReceived); n != 2 {
		t.Fatalf("phải phát 2 lần received (rev 1, rev 3), có %d", n)
	}
	// Quản lý HTX B không thấy lịch của HTX A; cán bộ KN xã 31117 thấy (phạm vi xã).
	if r := f.managerB().get("/v1/cropcycle/htx-collection/schedules"); len(list(r.body, "items")) != 0 {
		t.Fatalf("HTX B không được thấy lịch HTX A: %s", r.raw)
	}
	if r := f.officerA().get("/v1/cropcycle/htx-collection/schedules"); len(list(r.body, "items")) != 1 {
		t.Fatalf("cán bộ KN xã 31117 thấy lịch gom: %s", r.raw)
	}
	// Cách ly tenant (RB-2): quản lý HTX cùng mã HTX ở tenant khác thấy rỗng.
	other := f.as(ids.New(), f.coopA, port.RoleCoopManager)
	if r := other.get("/v1/cropcycle/htx-collection/schedules"); len(list(r.body, "items")) != 0 {
		t.Fatalf("tenant khác thấy lịch gom: %s", r.raw)
	}
}

// Van mặc định từ chối (contracts/publication): khóa lạ (dữ liệu cá nhân) hoặc thiếu trường → lỗi,
// không ghi gì (giao lại rồi DLQ, R-62).
func TestCollectionValveDefaultDeny_RB6(t *testing.T) {
	f := newFixture(t)
	p := statementPayload(ids.New(), f.coopA, f.p1, 40, 1)
	p["owner_phone"] = "0912345678"
	if _, err := f.deliver(f.mkg, port.EventMKGHTXCollectionStatementPublished, p); err == nil {
		t.Fatal("khóa lạ phải bị từ chối")
	}
	q := statementPayload(ids.New(), f.coopA, f.p1, 40, 1)
	delete(q, "rolls")
	if _, err := f.deliver(f.mkg, port.EventMKGHTXCollectionStatementPublished, q); err == nil {
		t.Fatal("thiếu trường phải bị từ chối")
	}
	if r := f.managerA().get("/v1/cropcycle/htx-collection/statements"); len(list(r.body, "items")) != 0 {
		t.Fatalf("không được ghi bảng kê: %s", r.raw)
	}
}

// RB-6 chiều mua: quản lý HTX của đúng HTX xác nhận / tranh chấp bảng kê theo revision hiện hành.
// 409 khi revision đã cũ; phản hồi thứ hai cùng revision phải có lý do và thay bản trước
// (supersedes); POST lặp cùng Idempotency-Key trả bản cũ.
func TestHTXRespondStatement_RB6_BuySide(t *testing.T) {
	f := newFixture(t)
	ref := ids.New()
	if _, err := f.deliver(f.mkg, port.EventMKGHTXCollectionStatementPublished, statementPayload(ref, f.coopA, f.p1, 40, 1)); err != nil {
		t.Fatal(err)
	}
	mgr := f.managerA()
	path := "/v1/cropcycle/htx-collection/statements/" + ref.String() + "/responses"
	r := mgr.get("/v1/cropcycle/htx-collection/statements?status=pending")
	if items := list(r.body, "items"); len(items) != 1 || get(items[0], "pending") != true || str(items[0], "plot_code") == "" {
		t.Fatalf("bảng kê chờ phản hồi (kèm mã thửa): %s", r.raw)
	}
	// Vai / phạm vi.
	expectCode(t, f.managerB().post(path, map[string]any{"revision": 1, "outcome": "confirmed"}), http.StatusNotFound,
		"cropcycle.collection_statement_not_found", "HTX khác")
	expectCode(t, f.platformAdmin().post(path, map[string]any{"revision": 1, "outcome": "confirmed"}), http.StatusForbidden,
		"cropcycle.collection_manager_only", "quản trị không phản hồi thay HTX")
	// Quy tắc.
	expect(t, mgr.post(path, map[string]any{"revision": 1, "outcome": "disputed", "htx_rolls": 40, "reason": "Đếm lại tại ruộng"}),
		http.StatusUnprocessableEntity, "tranh chấp mà số bằng MKG")
	expect(t, mgr.post(path, map[string]any{"revision": 1, "outcome": "disputed", "htx_rolls": 38, "reason": "gọi 0912345678 nhé"}),
		http.StatusUnprocessableEntity, "lý do chứa SĐT")
	expect(t, mgr.post(path, map[string]any{"revision": 1, "outcome": "confirmed", "htx_rolls": 39}),
		http.StatusUnprocessableEntity, "xác nhận mà số khác MKG")
	// Xác nhận + phát lại.
	k := key()
	r = mgr.postKey(path, k, map[string]any{"revision": 1, "outcome": "confirmed"})
	expect(t, r, http.StatusCreated, "xác nhận")
	first := str(r.body, "response", "id")
	r = mgr.postKey(path, k, map[string]any{"revision": 1, "outcome": "confirmed"})
	if r.status >= 300 || str(r.body, "response", "id") != first {
		t.Fatalf("phát lại cùng khóa: %d %s", r.status, r.raw)
	}
	// Đổi ý cùng revision: bắt buộc lý do, bản mới thay bản trước.
	expect(t, mgr.post(path, map[string]any{"revision": 1, "outcome": "disputed", "htx_rolls": 38}), http.StatusUnprocessableEntity, "thiếu lý do")
	r = mgr.post(path, map[string]any{"revision": 1, "outcome": "disputed", "htx_rolls": 38, "reason": "Đếm lại tại ruộng thiếu 2 cuộn"})
	expect(t, r, http.StatusCreated, "tranh chấp thay xác nhận")
	d := mgr.get("/v1/cropcycle/htx-collection/statements/" + ref.String())
	hist := list(d.body, "responses")
	if len(hist) != 2 || str(hist[1], "supersedes_id") != first || str(d.body, "statement", "response", "outcome") != "disputed" {
		t.Fatalf("lịch sử phản hồi: %s", d.raw)
	}
	// MKG gửi bản sửa (revision 2, 38 cuộn): phản hồi theo revision 1 → 409.
	if _, err := f.deliver(f.mkg, port.EventMKGHTXCollectionStatementPublished, statementPayload(ref, f.coopA, f.p1, 38, 2)); err != nil {
		t.Fatal(err)
	}
	expectCode(t, mgr.post(path, map[string]any{"revision": 1, "outcome": "confirmed"}), http.StatusConflict,
		"cropcycle.collection_statement_stale", "revision cũ")
	r = mgr.get("/v1/cropcycle/htx-collection/statements?status=pending")
	if items := list(r.body, "items"); len(items) != 1 || num(items[0], "revision") != 2 || str(items[0], "response", "outcome") != "disputed" {
		t.Fatalf("bảng kê rev 2 chờ phản hồi, còn giữ phản hồi rev 1: %s", r.raw)
	}
	expect(t, mgr.post(path, map[string]any{"revision": 2, "outcome": "confirmed"}), http.StatusCreated, "xác nhận rev 2")
	if r := mgr.get("/v1/cropcycle/htx-collection/statements?status=confirmed"); len(list(r.body, "items")) != 1 {
		t.Fatalf("lọc confirmed: %s", r.raw)
	}
	if n := f.countOutbox(port.EventCollectionStatementReceived); n != 2 {
		t.Fatalf("received phải phát 2 lần, có %d", n)
	}
}

// RB-6 chiều mua + RB-2 (ADR-0020): luồng phản hồi cho MKG (vai mkg_sync, tenant MKG riêng) theo con
// trỏ tăng dần, đọc ở tenant bên cấp qua cấp quyền htx_collection_responses, chỉ phản hồi thuộc bảng
// kê do tenant người gọi gửi; vai khác không đọc được luồng, mkg_sync không đọc được bảng kê.
func TestMKGSyncResponseFeed_RB6_RB2(t *testing.T) {
	f := newFixture(t)
	mgr := f.managerA()
	var refs []uuid.UUID
	for i := range 3 {
		ref := ids.New()
		refs = append(refs, ref)
		if _, err := f.deliver(f.mkg, port.EventMKGHTXCollectionStatementPublished, statementPayload(ref, f.coopA, f.p1, 10+i, 1)); err != nil {
			t.Fatal(err)
		}
		expect(t, mgr.post("/v1/cropcycle/htx-collection/statements/"+ref.String()+"/responses",
			map[string]any{"revision": 1, "outcome": "confirmed"}), http.StatusCreated, "xác nhận")
	}
	sa := f.as(f.mkg, uuid.Nil, port.RoleMKGSync) // tài khoản dịch vụ thuộc tenant MKG (ADR-0020)
	r := sa.get("/v1/cropcycle/htx-collection/responses?limit=2")
	expect(t, r, http.StatusOK, "luồng trang 1")
	items := list(r.body, "items")
	if len(items) != 2 || str(items[0], "statement_ref") != refs[0].String() || num(items[1], "mkg_rolls") != 11 {
		t.Fatalf("trang 1: %s", r.raw)
	}
	if _, ok := items[0].(map[string]any)["actor_ref"]; ok {
		t.Fatal("luồng không được mang người phản hồi")
	}
	next := int64(num(r.body, "next_cursor"))
	r = sa.get(fmt.Sprintf("/v1/cropcycle/htx-collection/responses?after=%d", next))
	if items := list(r.body, "items"); len(items) != 1 || str(items[0], "statement_ref") != refs[2].String() {
		t.Fatalf("trang 2: %s", r.raw)
	}
	r = sa.get(fmt.Sprintf("/v1/cropcycle/htx-collection/responses?after=%d", int64(num(r.body, "next_cursor"))))
	if len(list(r.body, "items")) != 0 {
		t.Fatalf("hết luồng: %s", r.raw)
	}
	expect(t, mgr.get("/v1/cropcycle/htx-collection/responses"), http.StatusForbidden, "quản lý HTX không đọc luồng MKG")
	expect(t, sa.get("/v1/cropcycle/htx-collection/statements"), http.StatusForbidden, "mkg_sync không đọc bảng kê")
	// Tenant khác không có cấp quyền → 403 party.share_grant_missing (không phải "rỗng").
	expectCode(t, f.as(ids.New(), uuid.Nil, port.RoleMKGSync).get("/v1/cropcycle/htx-collection/responses"), http.StatusForbidden,
		"party.share_grant_missing", "tenant khác không có cấp quyền")
	// Đường chuẩn của bề mặt đối tác trả cùng luồng; mỗi lời gọi ghi một dòng sổ đọc.
	before := f.share.ReadCount()
	if r := sa.get("/v1/partner/htx-collection/responses"); len(list(r.body, "items")) != 3 {
		t.Fatalf("đường /v1/partner: %s", r.raw)
	}
	if f.share.ReadCount() != before+1 {
		t.Fatal("mỗi lời gọi bề mặt đối tác phải ghi sổ đọc")
	}
}

// App HTX ngoại tuyến: pull htx_collection_schedule / htx_collection_statement (chỉ quản lý HTX của
// mình) và push htx_collection_response (op create) — lặp lại là duplicate; revision cũ bị từ chối.
func TestCollectionSyncPushPull_RB6(t *testing.T) {
	f := newFixture(t)
	ref := ids.New()
	if _, err := f.deliver(f.mkg, port.EventMKGCollectionSchedulePublished, schedulePayload(ids.New(), f.coopA, "planned", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.deliver(f.mkg, port.EventMKGHTXCollectionStatementPublished, statementPayload(ref, f.coopA, f.p1, 20, 1)); err != nil {
		t.Fatal(err)
	}
	mgr := f.managerA()
	ctx := f.ctxFor(mgr, port.RoleCoopManager)
	src := f.mod.SyncSource()
	for _, e := range []string{port.EntityCollectionSchedule, port.EntityCollectionStatement} {
		ch, _, err := src.Changes(ctx, authz.Scope{}, e, 0, 100)
		if err != nil || len(ch) != 1 {
			t.Fatalf("pull %s: %v %v", e, ch, err)
		}
	}
	ctxB := f.ctxFor(f.managerB(), port.RoleCoopManager)
	if ch, _, _ := src.Changes(ctxB, authz.Scope{}, port.EntityCollectionStatement, 0, 100); len(ch) != 0 {
		t.Fatal("HTX B không nhận bảng kê HTX A")
	}
	snk := f.mod.SyncSink()
	rc := rec(port.EntityCollectionResponse, syncport.OpCreate, map[string]any{"statement_ref": ref, "revision": 1, "outcome": "disputed",
		"htx_rolls": 18, "reason": "Thiếu 2 cuộn ở bờ kênh"}, nil, "", startTime.Add(-time.Hour))
	if res := snk.Accept(ctx, authz.Scope{}, rc); res.Status != syncport.StatusAccepted {
		t.Fatalf("push: %+v", res)
	}
	if res := snk.Accept(ctx, authz.Scope{}, rc); res.Status != syncport.StatusDuplicate {
		t.Fatalf("push lặp: %+v", res)
	}
	stale := rec(port.EntityCollectionResponse, syncport.OpCreate, map[string]any{"statement_ref": ref, "revision": 0, "outcome": "confirmed"},
		nil, "", startTime)
	if res := snk.Accept(ctx, authz.Scope{}, stale); res.Status != syncport.StatusRejected {
		t.Fatalf("revision sai phải bị từ chối: %+v", res)
	}
	ch, _, _ := src.Changes(ctx, authz.Scope{}, port.EntityCollectionStatement, 0, 100)
	var d map[string]any
	_ = json.Unmarshal(ch[len(ch)-1].Data, &d)
	if str(d, "response", "outcome") != "disputed" || d["pending"] != false {
		t.Fatalf("bảng kê sau phản hồi: %v", d)
	}
}

func rec(entity, op string, payload any, sup *uuid.UUID, reason string, at time.Time) syncport.Record {
	b, _ := json.Marshal(payload)
	r := syncport.Record{ID: ids.New(), IdemKey: "dev:" + ids.New().String() + ":1", Entity: entity, Op: op, EventTime: at,
		DeviceID: ids.New(), Payload: b, SupersedesID: sup}
	if reason != "" {
		r.CorrectionReason = &reason
	}
	return r
}
