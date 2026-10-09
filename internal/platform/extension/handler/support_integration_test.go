package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// fakeFarmers thực hiện accounts/port.FarmerHouseholds (pf/accounts thật có kiểm thử riêng).
type fakeFarmers struct {
	mu    sync.Mutex
	links map[uuid.UUID][]accport.HouseholdLink // tenant:actor → liên kết
}

func (f *fakeFarmers) key(tid, actor uuid.UUID) uuid.UUID {
	var k uuid.UUID
	for i := range k {
		k[i] = tid[i] ^ actor[i]
	}
	return k
}

func (f *fakeFarmers) link(tid, actor uuid.UUID, l accport.HouseholdLink) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.key(tid, actor)
	f.links[k] = append(f.links[k], l)
}

func (f *fakeFarmers) HouseholdsForSubject(ctx context.Context, subject uuid.UUID) ([]accport.HouseholdLink, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]accport.HouseholdLink(nil), f.links[f.key(tid, subject)]...), nil
}

// farmerIn tạo nông dân của HTX c1 (xã 30001) có một nông hộ.
func (f *fixture) farmerIn(farmers *fakeFarmers, tid, coop uuid.UUID, commune string) (caller, uuid.UUID) {
	c := f.asTenant(tid, uuid.Nil, accport.RoleFarmer)
	hh := ids.New()
	farmers.link(tid, c.actor, accport.HouseholdLink{HouseholdRef: hh, CooperativeRef: coop, CommuneCode: commune})
	return c, hh
}

type supportRow struct {
	task, assignee, rule string
	received, created    time.Time
	gpsMissing           bool
}

func (f *fixture) supportRow(t *testing.T, tid uuid.UUID, id string) supportRow {
	t.Helper()
	var r supportRow
	var assignee *uuid.UUID
	var task uuid.UUID
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT s.task_id, s.assignee_ref, s.assignment_rule, s.received_at, t.created_at, s.location IS NULL
		  FROM pf_extension.support_request s JOIN pf_extension.task_current t ON t.task_id = s.task_id WHERE s.id = $1`, id).
			Scan(&task, &assignee, &r.rule, &r.received, &r.created, &r.gpsMissing)
	})
	if err != nil {
		t.Fatalf("đọc yêu cầu %s: %v", id, err)
	}
	r.task = task.String()
	if assignee != nil {
		r.assignee = assignee.String()
	}
	return r
}

func (f *fixture) outboxBy(t *testing.T, tid uuid.UUID, eventType, field, value string) []map[string]any {
	t.Helper()
	var out []map[string]any
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT payload::text FROM shared.outbox WHERE event_type = $1 AND payload->>$2 = $3`, eventType, field, value)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var m map[string]any
			_ = json.Unmarshal([]byte(raw), &m)
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// HTX US-SUPPORT-01 → KN US-TASK-01: yêu cầu hỗ trợ của nông dân tự sinh nhiệm vụ
// trong CÙNG giao dịch (mốc = giờ máy chủ nhận, ≤ 1 phút), giao theo chuỗi dự phòng:
// cán bộ xã đang trực → cán bộ tỉnh đang trực → cán bộ tỉnh → hàng chờ quản trị.
func TestSupportRequestCreatesTaskWithDutyFallback(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	farmers := &fakeFarmers{links: map[uuid.UUID][]accport.HouseholdLink{}}
	f.mod.UseFarmers(farmers)
	farmer, hh := f.farmerIn(farmers, f.tenant, f.org.c1, "30001")
	body := func() map[string]any {
		return map[string]any{"category": "dich_hai", "urgency": "khan", "description": "Rầy nâu xuất hiện dày ở lô 3, lúa đang trổ",
			"photo_refs": []string{ids.New().String(), ids.New().String()}, "location": map[string]any{"lat": "10.3861", "lon": "105.4352"}, "gps_accuracy_m": 8}
	}

	// 1. Cán bộ xã đang trực nhận nhiệm vụ.
	p.off30001b.must(p.off30001b.post("/v1/extension/roster/status", map[string]any{"status": "on_duty"}), http.StatusCreated, "trực")
	r := farmer.must(farmer.post("/v1/support-requests", body()), http.StatusCreated, "gửi yêu cầu")
	id := str(r.body, "support_request", "id")
	row := f.supportRow(t, f.tenant, id)
	if row.rule != port.AssignCommuneOnDuty || row.assignee != p.off30001b.actor.String() || row.gpsMissing {
		t.Fatalf("giao xã đang trực: %+v", row)
	}
	if lat := row.created.Sub(row.received); lat < 0 || lat > time.Minute || !row.received.Equal(t0) {
		t.Fatalf("mốc nhận/tạo: %v → %v", row.received, row.created)
	}
	task := p.off30001b.must(p.off30001b.get("/v1/extension/tasks/"+row.task), http.StatusOK, "cán bộ đọc nhiệm vụ")
	if str(task.body, "task", "source") != "htx_app" || str(task.body, "task", "category") != "sau_benh" || str(task.body, "task", "priority") != "urgent" ||
		str(task.body, "task", "status") != "assigned" || str(task.body, "task", "household_ref") != hh.String() {
		t.Fatalf("nhiệm vụ: %v", task.body)
	}
	if ev := f.outboxBy(t, f.tenant, port.EventSupportRequestSubmitted, "support_request_id", id); len(ev) != 1 || ev[0]["task_id"] != row.task || ev[0]["photo_count"].(float64) != 2 {
		t.Fatalf("submitted: %v", ev)
	}
	if ev := f.outboxBy(t, f.tenant, port.EventTaskCreated, "task_id", row.task); len(ev) != 1 || ev[0]["assignee_ref"] != p.off30001b.actor.String() || ev[0]["assignment_rule"] != port.AssignCommuneOnDuty {
		t.Fatalf("task.created: %v", ev)
	}
	for _, ev := range f.outboxBy(t, f.tenant, port.EventSupportRequestSubmitted, "support_request_id", id) {
		raw, _ := json.Marshal(ev)
		if strings.Contains(string(raw), "Rầy nâu") || strings.Contains(string(raw), "105.4352") {
			t.Fatal("sự kiện không mang mô tả/toạ độ")
		}
	}

	// 2. Xã không ai trực → cán bộ tỉnh đang trực.
	p.off30001b.must(p.off30001b.post("/v1/extension/roster/status", map[string]any{"status": "off_duty"}), http.StatusCreated, "nghỉ")
	p.admin89.must(p.admin89.post("/v1/extension/roster/status", map[string]any{"status": "on_duty"}), http.StatusCreated, "tỉnh trực")
	r2 := farmer.must(farmer.post("/v1/support-requests", body()), http.StatusCreated, "gửi 2")
	if row := f.supportRow(t, f.tenant, str(r2.body, "support_request", "id")); row.rule != port.AssignProvinceOnDuty || row.assignee != p.admin89.actor.String() {
		t.Fatalf("giao tỉnh đang trực: %+v", row)
	}
	// 3. Không ai trực → cán bộ cấp tỉnh đang hoạt động.
	p.admin89.must(p.admin89.post("/v1/extension/roster/status", map[string]any{"status": "off_duty"}), http.StatusCreated, "tỉnh nghỉ")
	r3 := farmer.must(farmer.post("/v1/support-requests", body()), http.StatusCreated, "gửi 3")
	if row := f.supportRow(t, f.tenant, str(r3.body, "support_request", "id")); row.rule != port.AssignProvinceStaff || row.assignee != p.admin89.actor.String() {
		t.Fatalf("giao cán bộ tỉnh: %+v", row)
	}
	// 4. Tenant chưa có danh bạ → hàng chờ quản trị (không người nhận), vẫn tạo nhiệm vụ.
	other := ids.New()
	farmerB, _ := f.farmerIn(farmers, other, f.org.c1, "30001")
	r4 := farmerB.must(farmerB.post("/v1/support-requests", body()), http.StatusCreated, "gửi tenant khác")
	if row := f.supportRow(t, other, str(r4.body, "support_request", "id")); row.rule != port.AssignAdminQueue || row.assignee != "" {
		t.Fatalf("hàng chờ quản trị: %+v", row)
	}
	// Cách ly tenant: tenant mặc định không thấy yêu cầu của tenant khác.
	if n := len(f.outboxBy(t, f.tenant, port.EventSupportRequestSubmitted, "support_request_id", str(r4.body, "support_request", "id"))); n != 0 {
		t.Fatal("lộ sự kiện sang tenant khác")
	}
	// Nông dân xem yêu cầu của hộ mình kèm trạng thái nhiệm vụ.
	lst := farmer.must(farmer.get("/v1/support-requests"), http.StatusOK, "danh sách")
	if len(items(lst, "items")) != 3 {
		t.Fatalf("danh sách: %v", lst.body)
	}
}

// HTX US-SUPPORT-01 AC-1..3: thiếu GPS vẫn gửi (nhãn "Thiếu vị trí GPS"); thiếu mô tả
// hoặc loại sự cố, quá 5 ảnh → chặn với thông điệp của AC; gửi lặp cùng khóa không tạo trùng.
func TestSupportRequestValidationAndReplay(t *testing.T) {
	f := newFixture(t)
	f.seedPeople()
	farmers := &fakeFarmers{links: map[uuid.UUID][]accport.HouseholdLink{}}
	f.mod.UseFarmers(farmers)
	farmer, _ := f.farmerIn(farmers, f.tenant, f.org.c1, "30001")

	r := farmer.must(farmer.post("/v1/support-requests", map[string]any{"category": "thoi_tiet", "description": "Mưa đá làm đổ ngã lúa diện rộng"}), http.StatusCreated, "không GPS")
	if r.body["support_request"].(map[string]any)["gps_missing"] != true || !strings.Contains(string(mustJSON(r.body)), "Thiếu vị trí GPS") {
		t.Fatalf("nhãn thiếu GPS: %v", r.body)
	}
	if r := farmer.post("/v1/support-requests", map[string]any{"category": "", "description": "Mưa đá làm đổ ngã lúa"}); r.status != http.StatusUnprocessableEntity ||
		!strings.Contains(string(mustJSON(r.body)), "Vui lòng nhập mô tả và chọn loại sự cố trước khi gửi.") {
		t.Fatalf("thiếu loại: %d %v", r.status, r.body)
	}
	six := []string{}
	for range 6 {
		six = append(six, ids.New().String())
	}
	if r := farmer.post("/v1/support-requests", map[string]any{"category": "khac", "description": "Cần hỗ trợ kỹ thuật bón phân", "photo_refs": six}); r.status != http.StatusUnprocessableEntity ||
		!strings.Contains(string(mustJSON(r.body)), "tối đa 5") {
		t.Fatalf("quá 5 ảnh: %d %v", r.status, r.body)
	}
	if r := farmer.post("/v1/support-requests", map[string]any{"category": "khac", "description": "Gọi tôi số 0912345678 nhé"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("SĐT trong mô tả: %d", r.status)
	}
	// Gửi lặp cùng Idempotency-Key (mạng chập chờn) → một yêu cầu, một nhiệm vụ.
	k := key()
	a := farmer.call(http.MethodPost, "/v1/support-requests", k, map[string]any{"category": "may_moc", "description": "Máy gặt đập hỏng giữa ruộng"})
	b := farmer.call(http.MethodPost, "/v1/support-requests", k, map[string]any{"category": "may_moc", "description": "Máy gặt đập hỏng giữa ruộng"})
	if a.status != http.StatusCreated || str(a.body, "support_request", "task_id") != str(b.body, "support_request", "task_id") {
		t.Fatalf("gửi lặp: %d %v / %v", a.status, a.body, b.body)
	}
	// Vai khác không gửi được; nông dân chưa liên kết nông hộ → 403.
	if r := f.as(f.org.c1, port.RoleCoopManager).post("/v1/support-requests", map[string]any{"category": "khac", "description": "Quản lý gửi thay"}); r.status != http.StatusForbidden {
		t.Fatalf("vai khác: %d", r.status)
	}
	lonely := f.as(uuid.Nil, accport.RoleFarmer)
	if r := lonely.post("/v1/support-requests", map[string]any{"category": "khac", "description": "Chưa có nông hộ"}); r.status != http.StatusForbidden || code(r) != "extension.farmer_household_required" {
		t.Fatalf("chưa liên kết: %d %v", r.status, r.body)
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// HTX US-SUPPORT-01 AC-2: gửi ngoại tuyến qua pf/sync (entity support_request) — bản
// ghi lặp là duplicate; chiều xuống trả yêu cầu của hộ mình kèm trạng thái nhiệm vụ.
func TestSupportRequestViaSync(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	farmers := &fakeFarmers{links: map[uuid.UUID][]accport.HouseholdLink{}}
	f.mod.UseFarmers(farmers)
	farmer, _ := f.farmerIn(farmers, f.tenant, f.org.c1, "30001")
	p.off30001a.must(p.off30001a.post("/v1/extension/roster/status", map[string]any{"status": "on_duty"}), http.StatusCreated, "trực")

	ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: farmer.actor, TenantID: f.tenant, Roles: []string{accport.RoleFarmer}})
	ctx = tenant.WithID(ctx, f.tenant)
	device := ids.New()
	rec := syncport.Record{
		ID: ids.New(), IdemKey: "dev:" + device.String() + ":1", Entity: port.EntitySupportRequest, Op: syncport.OpCreate,
		EventTime: t0.Add(-3 * time.Hour), DeviceID: device,
		Payload: []byte(`{"category":"benh_lua","description":"Lá lúa có vết đạo ôn lan nhanh","photo_refs":[]}`),
	}
	res := f.mod.SyncSink().Accept(ctx, authz.Scope{}, rec)
	if res.Status != syncport.StatusAccepted {
		t.Fatalf("nhận: %+v", res)
	}
	if again := f.mod.SyncSink().Accept(ctx, authz.Scope{}, rec); again.Status != syncport.StatusDuplicate {
		t.Fatalf("gửi lặp: %+v", again)
	}
	row := f.supportRow(t, f.tenant, rec.ID.String())
	if row.assignee != p.off30001a.actor.String() || !row.received.Equal(t0) {
		t.Fatalf("yêu cầu ngoại tuyến: %+v", row)
	}
	src := f.mod.SyncSource()
	ch, last, err := src.Changes(ctx, authz.Scope{}, port.EntitySupportRequest, 0, 100)
	if err != nil || len(ch) != 1 || ch[0].ID != rec.ID {
		t.Fatalf("chiều xuống: %v %v", ch, err)
	}
	var d map[string]any
	_ = json.Unmarshal(ch[0].Data, &d)
	if d["task_status"] != "assigned" || d["event_time"] != t0.Add(-3*time.Hour).Format(time.RFC3339) {
		t.Fatalf("dữ liệu: %v", d)
	}
	// Cán bộ tiếp nhận → nhiệm vụ đổi → yêu cầu đi xuống lại với trạng thái mới.
	p.off30001a.must(p.off30001a.post("/v1/extension/tasks/"+row.task+"/status", map[string]any{"action": "accepted"}), http.StatusOK, "tiếp nhận")
	ch2, _, err := src.Changes(ctx, authz.Scope{}, port.EntitySupportRequest, last, 100)
	if err != nil || len(ch2) != 1 {
		t.Fatalf("thay đổi sau tiếp nhận: %v %v", ch2, err)
	}
	_ = json.Unmarshal(ch2[0].Data, &d)
	if d["task_status"] != "accepted" {
		t.Fatalf("trạng thái: %v", d)
	}
	// Cán bộ (không phải nông dân) pull entity này → rỗng, không lỗi.
	offCtx := tenant.WithID(authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: p.off30001a.actor, TenantID: f.tenant, OrgRef: p.off30001a.org, Roles: []string{port.RoleKNOfficer}}), f.tenant)
	if ch, _, err := src.Changes(offCtx, authz.Scope{Units: []string{"30001"}}, port.EntitySupportRequest, 0, 100); err != nil || len(ch) != 0 {
		t.Fatalf("cán bộ pull support_request: %v %v", ch, err)
	}
}
