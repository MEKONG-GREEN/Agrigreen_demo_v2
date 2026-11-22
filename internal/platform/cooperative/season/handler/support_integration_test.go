package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/app"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// PA1-HTX-SUPPORT-GPSLOG (#313): yêu cầu hỗ trợ App HTX (HTX US-SUPPORT-01) do module chủ HTX pf/cooperative phục vụ —
// bảng pf_coop.support_request, sự kiện pf.cooperative.support_request.submitted, log GPS qua consumer pf.cropcycle.gps_log.
// Không phụ thuộc pf/extension (kiểm thử chỉ dựng Service vụ mùa của pf/cooperative).

// fakeMembers: xã viên HTX của nông hộ (phần member của pf/cooperative có kiểm thử riêng).
type fakeMembers struct {
	m map[uuid.UUID]port.HouseholdMembership
}

func (f fakeMembers) Membership(_ context.Context, ref uuid.UUID) (port.HouseholdMembership, error) {
	if m, ok := f.m[ref]; ok {
		return m, nil
	}
	return port.HouseholdMembership{}, port.ErrHouseholdNotFound
}

// fakeUnits: mọi xã 5 chữ số thuộc tỉnh "31".
type fakeUnits struct{}

func (fakeUnits) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	if len(code) != 5 {
		return partyport.AdminUnit{}, partyport.ErrUnitNotFound
	}
	return partyport.AdminUnit{Code: code, Level: partyport.LevelCommune, ParentCode: "31"}, nil
}
func (fakeUnits) Descendants(context.Context, string, time.Time) ([]string, error) { return nil, nil }
func (fakeUnits) Current(_ context.Context, code string, _ time.Time) ([]string, error) {
	return []string{code}, nil
}

// supportFixture: h1–h3 xã viên HTX A (31117), h4 HTX B (31120), loose: nông hộ chưa vào HTX nào.
func supportFixture(t *testing.T) (*fixture, uuid.UUID) {
	f := newFixture(t)
	loose := ids.New()
	f.mod.svc.Members = fakeMembers{m: map[uuid.UUID]port.HouseholdMembership{
		f.h1:  {Ref: f.h1, CooperativeRef: f.coopA, CommuneCode: "31117", Active: true},
		f.h2:  {Ref: f.h2, CooperativeRef: f.coopA, CommuneCode: "31117", Active: true},
		f.h3:  {Ref: f.h3, CooperativeRef: f.coopA, CommuneCode: "31117", Active: true},
		f.h4:  {Ref: f.h4, CooperativeRef: f.coopB, CommuneCode: "31120", Active: true},
		loose: {Ref: loose, CommuneCode: "31117"},
	}}
	f.mod.svc.Units = fakeUnits{}
	return f, loose
}

func supportBody() map[string]any {
	return map[string]any{"category": "dich_hai", "urgency": "khan", "description": "Rầy nâu xuất hiện dày ở lô 3, lúa đang trổ",
		"photo_refs": []string{ids.New().String(), ids.New().String()}, "location": map[string]any{"lat": "10.3861", "lon": "105.4352"},
		"gps_accuracy_m": 8}
}

type supportRow struct {
	coop, household             uuid.UUID
	commune, province, category string
	gpsMissing                  bool
	photos                      int
	seq                         int64
}

func (f *fixture) supportRow(id string) (supportRow, bool) {
	f.t.Helper()
	var r supportRow
	err := tenant.WithTenantTx(context.Background(), f.db, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT cooperative_ref, household_ref, commune_code, province_code, category,
		    location IS NULL, cardinality(photo_refs), sync_seq FROM pf_coop.support_request WHERE id = $1::uuid`, id).
			Scan(&r.coop, &r.household, &r.commune, &r.province, &r.category, &r.gpsMissing, &r.photos, &r.seq)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return r, true
}

func supportIDs(r resp) []string {
	var out []string
	for _, it := range r.body["items"].([]any) {
		out = append(out, it.(map[string]any)["id"].(string))
	}
	return out
}

// HTX US-SUPPORT-01: nông dân gửi yêu cầu → ghi ở pf_coop (HTX, xã, tỉnh theo xã viên hiện tại) và phát sự kiện của
// module chủ HTX trong cùng giao dịch; gửi lặp cùng khóa → bản đã ghi; nông dân chỉ thấy yêu cầu của hộ mình.
func TestSupportRequestOwnedByCooperative_PA1_313(t *testing.T) {
	f, _ := supportFixture(t)
	fa := f.farmer(f.h1)
	k, body := key(), supportBody()
	r := fa.postKey("/v1/support-requests", k, body)
	expect(t, r, http.StatusCreated, "gửi yêu cầu")
	sr := r.body["support_request"].(map[string]any)
	id := sr["id"].(string)
	if sr["household_ref"] != f.h1.String() || sr["gps_missing"] != false || len(sr["photo_refs"].([]any)) != 2 || sr["task_status"] != nil {
		t.Fatalf("dữ liệu trả về: %s", r.raw)
	}
	row, ok := f.supportRow(id)
	if !ok || row.coop != f.coopA || row.household != f.h1 || row.commune != "31117" || row.province != "31" || row.gpsMissing || row.photos != 2 {
		t.Fatalf("dòng pf_coop.support_request: %+v %v", row, ok)
	}
	evs := f.outbox(port.EventSupportRequestSubmitted)
	if len(evs) != 1 || evs[0]["support_request_id"] != id || evs[0]["cooperative_ref"] != f.coopA.String() || evs[0]["photo_count"].(float64) != 2 ||
		evs[0]["gps_missing"] != false || evs[0]["province_code"] != "31" {
		t.Fatalf("sự kiện: %v", evs)
	}
	if again := fa.postKey("/v1/support-requests", k, body); again.status != http.StatusOK && again.status != http.StatusCreated {
		t.Fatalf("gửi lặp: %d %s", again.status, again.raw)
	}
	if n := len(f.outbox(port.EventSupportRequestSubmitted)); n != 1 {
		t.Fatalf("gửi lặp không phát thêm sự kiện: %d", n)
	}

	// Thiếu GPS vẫn nhận, gắn nhãn (AC-1).
	r2 := fa.post("/v1/support-requests", map[string]any{"category": "thoi_tiet", "description": "Mưa đá làm đổ ngã lúa diện rộng"})
	expect(t, r2, http.StatusCreated, "không GPS")
	if s := r2.body["support_request"].(map[string]any); s["gps_missing"] != true || s["labels"].([]any)[0] != "Thiếu vị trí GPS" {
		t.Fatalf("nhãn thiếu GPS: %s", r2.raw)
	}
	// Nông dân HTX B gửi; mỗi người chỉ thấy yêu cầu của hộ mình.
	fb := f.farmer(f.h4)
	expect(t, fb.post("/v1/support-requests", supportBody()), http.StatusCreated, "HTX B gửi")
	mine := fa.get("/v1/support-requests")
	expect(t, mine, http.StatusOK, "danh sách của tôi")
	if ids := supportIDs(mine); len(ids) != 2 || ids[1] != id {
		t.Fatalf("danh sách nông dân A (mới nhất trước): %v", ids)
	}
	if ids := supportIDs(fb.get("/v1/support-requests")); len(ids) != 1 {
		t.Fatalf("danh sách nông dân B: %v", ids)
	}
	pg := fa.get("/v1/support-requests?limit=1")
	if cur, _ := pg.body["next_cursor"].(string); cur == "" || len(supportIDs(fa.get("/v1/support-requests?limit=1&cursor="+cur))) != 1 {
		t.Fatalf("phân trang: %s", pg.raw)
	}
}

// Quy tắc nhận (AC-3), vai và nông hộ.
func TestSupportRequestValidation_PA1_313(t *testing.T) {
	f, loose := supportFixture(t)
	fa := f.farmer(f.h1)
	expectCode(t, fa.post("/v1/support-requests", map[string]any{"category": "", "description": "Mưa đá làm đổ ngã lúa"}),
		http.StatusUnprocessableEntity, "validation_failed", "thiếu loại")
	six := []string{ids.New().String(), ids.New().String(), ids.New().String(), ids.New().String(), ids.New().String(), ids.New().String()}
	expectCode(t, fa.post("/v1/support-requests", map[string]any{"category": "khac", "description": "Cần hỗ trợ kỹ thuật bón phân", "photo_refs": six}),
		http.StatusUnprocessableEntity, "validation_failed", "6 ảnh")
	expectCode(t, fa.post("/v1/support-requests", map[string]any{"category": "khac", "description": "Gọi tôi số 0912345678 nhé"}),
		http.StatusUnprocessableEntity, "validation_failed", "SĐT trong mô tả")
	expectCode(t, fa.post("/v1/support-requests", map[string]any{"category": "khac", "description": "Toạ độ sai hoàn toàn rồi",
		"location": map[string]any{"lat": "91", "lon": "105"}}), http.StatusUnprocessableEntity, "validation_failed", "vĩ độ > 90")
	for _, bad := range []map[string]any{
		{"location": map[string]any{"lat": "1e100000", "lon": "105"}},
		{"location": map[string]any{"lat": "10.5", "lon": "1e-100000"}},
		{"location": map[string]any{"lat": 1e300, "lon": "105"}},
		{"location": map[string]any{"lat": "10.5", "lon": "105"}, "gps_accuracy_m": "12345678901234567890123456789"},
		{"location": map[string]any{"lat": "10.5"}},
	} {
		body := map[string]any{"category": "khac", "description": "Toạ độ sai cú pháp thập phân"}
		for k, v := range bad {
			body[k] = v
		}
		expectCode(t, fa.post("/v1/support-requests", body), http.StatusUnprocessableEntity, "validation_failed", "số không hợp lệ")
	}
	expect(t, fa.post("/v1/support-requests", map[string]any{"category": "khac", "description": "Toạ độ dạng số JSON hợp lệ",
		"location": map[string]any{"lat": 10.3861, "lon": 105.4352}, "gps_accuracy_m": 5}), http.StatusCreated, "toạ độ dạng số JSON")
	expect(t, f.managerA().post("/v1/support-requests", map[string]any{"category": "khac", "description": "Quản lý gửi thay"}), http.StatusForbidden, "quản lý gửi")
	expectCode(t, f.farmer().post("/v1/support-requests", supportBody()), http.StatusForbidden, "cropcycle.farmer_household_required", "chưa liên kết hộ")
	two := f.farmer(f.h1, f.h2)
	expectCode(t, two.post("/v1/support-requests", supportBody()), http.StatusUnprocessableEntity, "cropcycle.household_invalid", "nhiều hộ, không chọn")
	b := supportBody()
	b["household_ref"] = f.h4.String()
	expectCode(t, two.post("/v1/support-requests", b), http.StatusUnprocessableEntity, "cropcycle.household_invalid", "hộ không thuộc tài khoản")
	b["household_ref"] = f.h2.String()
	expect(t, two.post("/v1/support-requests", b), http.StatusCreated, "chọn hộ h2")
	expectCode(t, f.farmer(loose).post("/v1/support-requests", supportBody()), http.StatusUnprocessableEntity, "cropcycle.household_no_cooperative", "hộ chưa vào HTX")
	// id do máy sinh đã dùng (khóa idem khác) → 409.
	id := ids.New().String()
	b = supportBody()
	b["id"] = id
	expect(t, fa.post("/v1/support-requests", b), http.StatusCreated, "id máy sinh")
	expectCode(t, fa.post("/v1/support-requests", b), http.StatusConflict, "cropcycle.id_reused", "id dùng lại")
}

// HTX US-SUPPORT-01: quản lý HTX biết để hỗ trợ kịp thời — GET /v1/cropcycle/support-requests chỉ HTX mình; quản trị
// toàn tenant; không trả toạ độ.
func TestSupportRequestManagerList_PA1_313(t *testing.T) {
	f, _ := supportFixture(t)
	fa, fb := f.farmer(f.h1), f.farmer(f.h4)
	expect(t, fa.post("/v1/support-requests", supportBody()), http.StatusCreated, "A1")
	expect(t, fa.post("/v1/support-requests", map[string]any{"category": "may_moc", "description": "Máy gặt đập hỏng giữa ruộng"}), http.StatusCreated, "A2")
	expect(t, fb.post("/v1/support-requests", supportBody()), http.StatusCreated, "B1")

	ma := f.managerA().get("/v1/cropcycle/support-requests")
	expect(t, ma, http.StatusOK, "quản lý A")
	items := ma.body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("quản lý A thấy %d: %s", len(items), ma.raw)
	}
	first := items[0].(map[string]any)
	if first["cooperative_ref"] != f.coopA.String() || first["gps_missing"] != true || first["legacy_task_status"] != nil || first["location"] != nil {
		t.Fatalf("mục mới nhất: %v", first)
	}
	if second := items[1].(map[string]any); second["photo_count"].(float64) != 2 || second["gps_accuracy_m"] != "8" {
		t.Fatalf("mục có ảnh và GPS: %v", second)
	}
	if n := len(f.managerA().get("/v1/cropcycle/support-requests?gps_missing=false").body["items"].([]any)); n != 1 {
		t.Fatalf("lọc có GPS: %d", n)
	}
	if n := len(f.managerA().get("/v1/cropcycle/support-requests?category=may_moc").body["items"].([]any)); n != 1 {
		t.Fatalf("lọc loại: %d", n)
	}
	expectCode(t, f.managerA().get("/v1/cropcycle/support-requests?cooperative_ref="+f.coopB.String()), http.StatusForbidden,
		"cropcycle.out_of_scope", "quản lý A hỏi HTX B")
	expectCode(t, f.managerA().get("/v1/cropcycle/support-requests?category=xyz"), http.StatusUnprocessableEntity, "validation_failed", "loại sai")
	expectCode(t, f.managerA().get("/v1/cropcycle/support-requests?gps_missing=co"), http.StatusUnprocessableEntity, "validation_failed", "gps_missing sai")
	if n := len(f.managerB().get("/v1/cropcycle/support-requests").body["items"].([]any)); n != 1 {
		t.Fatalf("quản lý B: %d", n)
	}
	adm := f.platformAdmin()
	if n := len(adm.get("/v1/cropcycle/support-requests").body["items"].([]any)); n != 3 {
		t.Fatalf("quản trị: %d", n)
	}
	if n := len(adm.get("/v1/cropcycle/support-requests?cooperative_ref=" + f.coopB.String()).body["items"].([]any)); n != 1 {
		t.Fatalf("quản trị lọc HTX B: %d", n)
	}
	pg := adm.get("/v1/cropcycle/support-requests?limit=2")
	cur, _ := pg.body["next_cursor"].(string)
	if cur == "" || len(adm.get("/v1/cropcycle/support-requests?limit=2&cursor=" + cur).body["items"].([]any)) != 1 {
		t.Fatalf("phân trang quản trị: %s", pg.raw)
	}
	expect(t, f.officerA().get("/v1/cropcycle/support-requests"), http.StatusForbidden, "cán bộ KN không đọc yêu cầu hỗ trợ HTX")
	expect(t, fa.get("/v1/cropcycle/support-requests"), http.StatusForbidden, "nông dân không đọc danh sách quản lý")
}

// HTX US-SUPPORT-01 AC-2: gửi ngoại tuyến qua pf/sync (entity support_request giữ tên) và chiều xuống theo nông hộ.
func TestSupportRequestSync_PA1_313(t *testing.T) {
	f, _ := supportFixture(t)
	fa := f.farmer(f.h1)
	ctx := authz.WithPrincipal(tenant.WithID(context.Background(), f.tenant),
		authz.Principal{TenantID: f.tenant, ActorRef: fa.actor, Roles: []string{port.RoleFarmer}})
	snk, src := app.SupportSyncSink{Svc: f.mod.svc}, app.SupportSyncSource{Svc: f.mod.svc}
	payload, _ := json.Marshal(supportBody())
	device := ids.New()
	evTime := startTime.Add(-2 * time.Hour)
	rec := syncport.Record{ID: ids.New(), IdemKey: "dev:" + device.String() + ":1", Entity: port.EntitySupportRequest, Op: syncport.OpCreate,
		DeviceID: device, EventTime: evTime, Payload: payload}
	if res := snk.Accept(ctx, authz.Scope{}, rec); res.Status != syncport.StatusAccepted {
		t.Fatalf("nhận ngoại tuyến: %+v", res)
	}
	if res := snk.Accept(ctx, authz.Scope{}, rec); res.Status != syncport.StatusDuplicate {
		t.Fatalf("gửi lặp: %+v", res)
	}
	bad := rec
	bad.ID, bad.IdemKey, bad.Op = ids.New(), "dev:x:2", syncport.OpCorrect
	if res := snk.Accept(ctx, authz.Scope{}, bad); res.Status != syncport.StatusRejected {
		t.Fatalf("op correct phải bị từ chối: %+v", res)
	}
	if _, ok := f.supportRow(rec.ID.String()); !ok {
		t.Fatal("bản ghi ngoại tuyến chưa ghi")
	}
	ch, last, err := src.FarmerChanges(ctx, syncport.FarmerScope{Households: []uuid.UUID{f.h1}}, port.EntitySupportRequest, 0, 100)
	if err != nil || len(ch) != 1 || ch[0].ID != rec.ID || last != ch[0].Version {
		t.Fatalf("chiều xuống: %+v %d %v", ch, last, err)
	}
	var d map[string]any
	_ = json.Unmarshal(ch[0].Data, &d)
	if d["event_time"] != evTime.UTC().Format(time.RFC3339) || d["household_ref"] != f.h1.String() {
		t.Fatalf("dữ liệu chiều xuống: %v", d)
	}
	if ch, _, _ := src.FarmerChanges(ctx, syncport.FarmerScope{Households: []uuid.UUID{f.h1}}, port.EntitySupportRequest, last, 100); len(ch) != 0 {
		t.Fatalf("sau con trỏ: %+v", ch)
	}
	if ch, _, _ := src.FarmerChanges(ctx, syncport.FarmerScope{Households: []uuid.UUID{f.h4}}, port.EntitySupportRequest, 0, 100); len(ch) != 0 {
		t.Fatalf("hộ khác: %+v", ch)
	}
	if ch, _, err := src.Changes(ctx, authz.Scope{All: true}, port.EntitySupportRequest, 0, 100); err != nil || len(ch) != 0 {
		t.Fatalf("vai không phải nông dân nhận rỗng: %+v %v", ch, err)
	}
}

// HTX US-GPS-03 (mục đích support): log GPS của yêu cầu hỗ trợ ghi SAU commit qua consumer pf.cropcycle.gps_log (thay
// pf.extension.gps_log) — một log mỗi yêu cầu có toạ độ, khóa chống trùng giữ tiền tố cũ; P0-POOL-SHARED-2 (#143): ghi
// qua pf/geo NGOÀI giao dịch inbox.
func TestSupportRequestGpsLog_PA1_313(t *testing.T) {
	f, _ := supportFixture(t)
	fa := f.farmer(f.h1)
	id := fa.post("/v1/support-requests", supportBody()).body["support_request"].(map[string]any)["id"].(string)
	expect(t, fa.post("/v1/support-requests", map[string]any{"category": "khac", "description": "Không bật định vị khi gửi"}), http.StatusCreated, "thiếu GPS")
	envs := f.envelopes(port.EventSupportRequestSubmitted)
	if len(envs) != 2 {
		t.Fatalf("sự kiện: %d", len(envs))
	}
	sized := pgtest.Get(t).SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	db := shareddb.ForModule(sized, shareddb.Module("pf/cooperative"))
	gl := &probeGpsLog{fakeGpsLog: &fakeGpsLog{fixes: map[string]geoport.GpsFix{}}, pool: sized}
	f.mod.UseGpsLog(gl)
	h := f.mod.GpsLogHandler()
	dup := envs[0]
	dup.EventID = uuid.New()
	for _, e := range append(envs, envs[0], dup) {
		if _, err := inbox.Process(context.Background(), db, port.GpsLogConsumer, e, h); err != nil {
			t.Fatal(err)
		}
	}
	gl.mu.Lock()
	defer gl.mu.Unlock()
	fx, ok := gl.fixes["extension:support_request:"+id]
	if len(gl.fixes) != 1 || !ok {
		t.Fatalf("log GPS: %v", gl.fixes)
	}
	if fx.Purpose != geoport.GpsPurposeSupport || fx.ActorRef != fa.actor || fx.TargetRef.String() != id || fx.CommuneCode != "31117" ||
		fx.Lat == nil || fx.Lat.String() != "10.3861" || fx.AccuracyM == nil || fx.AccuracyM.String() != "8" || fx.Source != geoport.GpsSourceDevice {
		t.Fatalf("bản ghi log: %+v", fx)
	}
	for i, n := range gl.held {
		if n != 0 {
			t.Errorf("P0-POOL-SHARED-2: ghi log GPS lần %d trong lúc giữ %d kết nối pool inbox", i+1, n)
		}
	}
}

// probeGpsLog ghi số kết nối đang mượn của pool inbox mỗi lần ghi log (port pf/geo).
type probeGpsLog struct {
	*fakeGpsLog
	pool *pgxpool.Pool
	mu   sync.Mutex
	held []int32
}

func (p *probeGpsLog) RecordFix(ctx context.Context, fix geoport.GpsFix) (geoport.GpsFixReceipt, error) {
	p.mu.Lock()
	p.held = append(p.held, p.pool.Stat().AcquiredConns())
	p.mu.Unlock()
	return p.fakeGpsLog.RecordFix(ctx, fix)
}
