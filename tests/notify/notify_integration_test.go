package notify_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	contentport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fixture struct {
	t       *testing.T
	iss     *authztest.FakeIssuer
	app     *httptest.Server
	admin   *httptest.Server
	mod     *notify.Module
	pool    *pgxpool.Pool   // notify_app (vai cũ): chỉ kiểm trực tiếp
	appDB   tenant.Beginner // app_platform bọc db.ForModule(…, notify.DBModule): giao dịch inbox như cmd/worker
	clk     *clock.Fixed
	scope   *fakeScope
	devices *fakeDevices
	staff   *fakeStaff
	pusher  *fakePusher
	tenant  uuid.UUID
	// Gói NOTIF: người nhận Cấp Tỉnh của Báo giá, danh bạ thư, kênh email theo người nhận (giả).
	desk   *fakePriceDesk
	emails *fakeEmails
	mailer *rcptMailer
	logs   *syncBuffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	env := pgtest.Get(t)
	f := &fixture{t: t, iss: authztest.New(t), pool: env.Pool(t, migrations.PF, notify.DBRole), clk: clock.NewFixed(time.Now()),
		scope: &fakeScope{scopes: map[uuid.UUID]authz.Scope{}}, devices: &fakeDevices{m: map[[2]uuid.UUID]syncport.DeviceInfo{}},
		staff: &fakeStaff{staff: map[uuid.UUID][]port.StaffRef{}}, pusher: &fakePusher{invalid: map[string]bool{}, flaky: map[string]int{}},
		tenant: ids.New(), desk: &fakePriceDesk{}, emails: &fakeEmails{addr: map[uuid.UUID]string{}}, mailer: &rcptMailer{}, logs: &syncBuffer{}}
	// ADR-0028 (W2.3): module chạy trên vai đăng nhập app_platform (NOINHERIT) — chỉ chạm pf_notify sau SET LOCAL ROLE r_notify.
	appPool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	f.appDB = db.ForModule(appPool, notify.DBModule)
	f.mod = notify.New(appPool, f.clk, notify.Deps{Digests: []port.DigestRegistration{extension.NotificationDigest()}, Builders: append(append(price.NotificationBuilders(f.desk), extension.TaskNotificationBuilders(f.staff)...), extension.DirectiveNotificationBuilders(f.staff, fakeUnits{})...), Renderers: extension.NotificationRenderers(), Consumers: []port.ConsumerRegistration{extension.NotificationConsumers(), price.NotificationConsumers()}, Scope: f.scope, Devices: f.devices, Staff: f.staff, Units: fakeUnits{}, Pusher: f.pusher,
		Emails: f.emails, EmailMailer: f.mailer, LinkBase: "https://kn.example.vn",
		Log: slog.New(slog.NewTextHandler(f.logs, nil))})
	ops := env.Pool(t, migrations.PF, "platform_api")
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 1<<20)
	f.mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), idempotency.Middleware(ops)))
	adminEng := httpx.NewEngine(log, 1<<20)
	f.mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	f.app, f.admin = httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(f.app.Close)
	t.Cleanup(f.admin.Close)
	return f
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	org   uuid.UUID
	actor uuid.UUID
	roles []string
	token string
}

// user tạo người dùng của tenant mặc định với phạm vi sc.
func (f *fixture) user(sc authz.Scope, roles ...string) caller {
	return f.userIn(f.tenant, ids.New(), sc, roles...)
}

func (f *fixture) userIn(tid, org uuid.UUID, sc authz.Scope, roles ...string) caller {
	f.scope.set(org, sc)
	return f.asActor(tid, org, ids.New(), roles...)
}

func (f *fixture) asActor(tid, org, actor uuid.UUID, roles ...string) caller {
	return caller{f: f, tid: tid, org: org, actor: actor, roles: roles,
		token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, MFA: true, TTL: time.Hour})}
}

func (c caller) ctx() context.Context {
	return authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: c.actor, TenantID: c.tid, OrgRef: c.org, Roles: c.roles})
}

type resp struct {
	status int
	hdr    http.Header
	body   map[string]any
}

func (c caller) do(base, method, path, idem string, body any) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: r.StatusCode, hdr: r.Header, body: m}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) post(path string, body any) resp {
	return c.do(c.f.app.URL, http.MethodPost, path, key(), body)
}
func (c caller) get(path string) resp { return c.do(c.f.app.URL, http.MethodGet, path, "", nil) }

func code(r resp) string { s, _ := r.body["code"].(string); return s }

func items(r resp) []map[string]any {
	raw, _ := r.body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		out = append(out, it.(map[string]any))
	}
	return out
}

// inbox trả hộp thư của người gọi (bắt buộc 200).
func (c caller) inbox() []map[string]any {
	c.f.t.Helper()
	r := c.get("/v1/notifications")
	if r.status != http.StatusOK {
		c.f.t.Fatalf("GET /v1/notifications: %d %v", r.status, r.body)
	}
	return items(r)
}

// device đăng ký thiết bị giả ở pf/sync và gắn token đẩy.
func (c caller) device(token string) uuid.UUID {
	c.f.t.Helper()
	id := ids.New()
	c.f.devices.add(c.tid, syncport.DeviceInfo{ID: id, ActorRef: c.actor, Platform: "android"})
	r := c.post("/v1/devices/"+id.String()+"/push-token", map[string]any{"provider": "fcm", "token": token})
	if r.status != http.StatusOK {
		c.f.t.Fatalf("gắn token: %d %v", r.status, r.body)
	}
	return id
}

func (f *fixture) event(tid uuid.UUID, typ string, payload any) outbox.Envelope {
	raw, _ := json.Marshal(payload)
	return outbox.Envelope{EventID: ids.New(), EventType: typ, SchemaVersion: 1, TenantID: tid, OccurredAt: f.clk.Now(),
		RecordedAt: f.clk.Now(), Producer: "test", Payload: raw}
}

func (f *fixture) process(consumer string, env outbox.Envelope) bool {
	f.t.Helper()
	ok, err := inbox.Process(context.Background(), f.appDB, consumer, env, f.mod.EventHandler())
	if err != nil {
		f.t.Fatalf("xử lý %s: %v", env.EventType, err)
	}
	return ok
}

// flushPush chạy consumer pf.notify.push cho mọi lô đang mở của tenant.
func (f *fixture) flushPush(tid uuid.UUID) error {
	f.t.Helper()
	var batches []uuid.UUID
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT DISTINCT batch_id FROM pf_notify.push_delivery WHERE status IN ('pending','failed')`)
		if err != nil {
			return err
		}
		batches, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	var last error
	for _, b := range batches {
		env := f.event(tid, port.EventPushRequested, port.PushRequestedV1{BatchID: b, Kind: "x", Deliveries: 1})
		if _, err := inbox.Process(context.Background(), f.appDB, port.ConsumerPush, env, f.mod.PushHandler()); err != nil {
			last = err
		}
	}
	return last
}

func (f *fixture) directive(tid uuid.UUID, severity string, units []string, targets ...uuid.UUID) (outbox.Envelope, uuid.UUID) {
	id := ids.New()
	return f.event(tid, extport.EventDirectiveIssued, extport.DirectiveIssuedV1{DirectiveID: id, Severity: severity,
		AudienceUnits: units, ProvinceCodes: []string{prov91}, TargetRefs: targets, IssuerRef: ids.New(),
		IssuedAt: f.clk.Now().Format(time.RFC3339)}), id
}

// KN US-TASK-03: chỉ đạo chỉ tới đúng cán bộ trong target_refs (danh bạ chụp lúc ban
// hành), không tới cán bộ khác cùng xã; thông báo không chứa tiêu đề/nội dung chỉ đạo.
func TestDirectiveNotifiesTargetOfficersOnly(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	b := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	other := f.user(authz.Scope{Units: []string{ward92}}, partyport.RoleKNOfficer)
	env, dirID := f.directive(f.tenant, port.SeverityUrgent, []string{ward1}, a.actor)
	f.process(extport.NotifyConsumerDirective, env)

	got := a.inbox()
	if len(got) != 1 || got[0]["kind"] != port.KindDirective || got[0]["severity"] != "urgent" ||
		got[0]["deep_link"] != "/extension/directives/"+dirID.String() || got[0]["topic"] != false {
		t.Fatalf("hộp thư người nhận: %v", got)
	}
	if n := len(b.inbox()) + len(other.inbox()); n != 0 {
		t.Fatalf("người không nằm trong target_refs nhận %d thông báo", n)
	}

	// Không có target_refs → tra danh bạ hiện tại của vùng (cán bộ xã 30292 là b).
	f.staff.add(f.tenant, port.StaffRef{ActorRef: b.actor, UnitCode: ward1, ProvinceCode: prov91})
	env2, _ := f.directive(f.tenant, port.SeverityNormal, []string{ward1})
	f.process(extport.NotifyConsumerDirective, env2)
	if len(b.inbox()) != 1 || len(other.inbox()) != 0 {
		t.Fatal("chỉ đạo không có target_refs phải tới cán bộ trong danh bạ của vùng")
	}
}

// US-LIB-06 / HTX US-NEWS-03: cảnh báo cấp xã chỉ tới người có phạm vi chứa xã; tài
// liệu cấp tỉnh tới mọi xã của tỉnh; người phạm vi toàn quốc chỉ nhận chủ đề toàn quốc.
func TestAlertFanOutRespectsScope(t *testing.T) {
	f := newFixture(t)
	off1 := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	off2 := f.user(authz.Scope{Units: []string{ward2}}, partyport.RoleCoopManager)
	off92 := f.user(authz.Scope{Units: []string{ward92}}, partyport.RoleKNOfficer)
	prov := f.user(authz.Scope{Units: []string{prov91, ward1, ward2}}, partyport.RoleKNProvinceAdmin)
	nat := f.user(authz.Scope{All: true}, partyport.RoleKNNationalAdmin)
	none := f.user(authz.Scope{}, partyport.RoleKNOfficer)

	doc := ids.New()
	f.process(port.ConsumerContent, f.event(f.tenant, contentport.EventAlertPublished, contentport.AlertPublishedV1{
		DocumentRef: doc.String(), VersionNo: 1, Title: "Rầy nâu bùng phát", Summary: "Phun thuốc theo khuyến cáo trong 3 ngày",
		AudienceLevel: "commune", AudienceUnits: []string{ward1}, PublisherRef: ids.New().String(), PublishedAt: f.clk.Now().Format(time.RFC3339)}))
	want := map[string]int{"xã 30292": 1, "xã 30301": 0, "Cần Thơ": 0, "tỉnh 91": 1, "toàn quốc": 0, "rỗng": 0}
	got := map[string]int{"xã 30292": len(off1.inbox()), "xã 30301": len(off2.inbox()), "Cần Thơ": len(off92.inbox()),
		"tỉnh 91": len(prov.inbox()), "toàn quốc": len(nat.inbox()), "rỗng": len(none.inbox())}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("cảnh báo xã 30292 → %s: muốn %d, nhận %d", k, v, got[k])
		}
	}
	if it := off1.inbox()[0]; it["kind"] != "alert" || it["title"] != "Rầy nâu bùng phát" || it["topic"] != true {
		t.Fatalf("thông báo cảnh báo: %v", it)
	}

	// Tài liệu cấp tỉnh 91 → mở rộng ra xã của tỉnh; bản "alert" của document.published bị bỏ qua.
	f.process(port.ConsumerContent, f.event(f.tenant, contentport.EventDocumentPublished, contentport.DocumentPublishedV1{
		DocumentRef: ids.New().String(), Kind: "procedure", VersionNo: 1, VersionID: ids.New().String(), Title: "Quy trình lúa thơm",
		Tags: []string{}, AudienceLevel: "province", AudienceUnits: []string{prov91}, AuthorRef: ids.New().String(),
		PublisherRef: ids.New().String(), PublishedAt: f.clk.Now().Format(time.RFC3339), FirstVersion: true, Seq: 1}))
	f.process(port.ConsumerContent, f.event(f.tenant, contentport.EventDocumentPublished, contentport.DocumentPublishedV1{
		DocumentRef: doc.String(), Kind: "alert", VersionNo: 1, VersionID: ids.New().String(), Title: "Rầy nâu bùng phát",
		Tags: []string{}, AudienceLevel: "commune", AudienceUnits: []string{ward1}, AuthorRef: ids.New().String(),
		PublisherRef: ids.New().String(), PublishedAt: f.clk.Now().Format(time.RFC3339), FirstVersion: true, Seq: 1}))
	if len(off1.inbox()) != 2 || len(off2.inbox()) != 1 || len(off92.inbox()) != 0 || len(nat.inbox()) != 0 {
		t.Fatalf("tài liệu cấp tỉnh: xã1=%d xã2=%d CT=%d QG=%d", len(off1.inbox()), len(off2.inbox()), len(off92.inbox()), len(nat.inbox()))
	}

	// Toàn quốc → mọi người có phạm vi khác rỗng.
	f.process(port.ConsumerContent, f.event(f.tenant, contentport.EventAlertPublished, contentport.AlertPublishedV1{
		DocumentRef: ids.New().String(), VersionNo: 1, Title: "Bão số 5", Summary: "Gia cố bờ bao",
		AudienceLevel: "national", AudienceUnits: []string{}, PublisherRef: ids.New().String(), PublishedAt: f.clk.Now().Format(time.RFC3339)}))
	if len(nat.inbox()) != 1 || len(off92.inbox()) != 1 || len(none.inbox()) != 0 {
		t.Fatal("cảnh báo toàn quốc phải tới mọi phạm vi khác rỗng")
	}
}

// R-61: giao lặp cùng sự kiện → không có thông báo trùng (inbox, và chỉ mục duy nhất
// (sự kiện, người nhận) khi handler bị gọi thẳng hai lần).
func TestDuplicateEventDeliveryNoDuplicates(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	a.device(fcmToken)
	env, _ := f.directive(f.tenant, port.SeverityNormal, []string{ward1}, a.actor)
	if !f.process(extport.NotifyConsumerDirective, env) || f.process(extport.NotifyConsumerDirective, env) {
		t.Fatal("lần giao thứ hai phải bị inbox bỏ qua")
	}
	for range 2 {
		if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
			return f.mod.EventHandler()(context.Background(), tx, env)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(a.inbox()); n != 1 {
		t.Fatalf("muốn 1 thông báo, nhận %d", n)
	}
	if err := f.flushPush(f.tenant); err != nil {
		t.Fatal(err)
	}
	if n := len(f.pusher.messages()); n != 1 {
		t.Fatalf("muốn 1 thông điệp đẩy, nhận %d", n)
	}
}

// RB-7 (ADR-0015): thông điệp đẩy chỉ có kind + notification_id; token hỏng bị ngừng
// dùng; lỗi tạm của một thiết bị không chặn thiết bị khác và được thử lại.
func TestPushDataOnlyAndPerDeviceFailures(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	b := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	c := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	a.device(fcmToken)
	bad := "fcm-token-da-go-app-000000000000000"
	b.device(bad)
	flaky := "fcm-token-mang-cham-0000000000000000"
	c.device(flaky)
	f.pusher.invalid[bad] = true
	f.pusher.flaky[flaky] = 1
	env, _ := f.directive(f.tenant, port.SeverityUrgent, []string{ward1}, a.actor, b.actor, c.actor)
	f.process(extport.NotifyConsumerDirective, env)

	if err := f.flushPush(f.tenant); err == nil {
		t.Fatal("còn lần gửi lỗi tạm → consumer phải trả lỗi để hàng đợi giao lại")
	}
	if err := f.flushPush(f.tenant); err != nil {
		t.Fatalf("lần giao lại: %v", err)
	}
	msgs := f.pusher.messages()
	if len(msgs) != 2 {
		t.Fatalf("muốn 2 thông điệp (a và c, mỗi máy một lần), nhận %d", len(msgs))
	}
	for i, body := range f.pusher.bodies {
		var m map[string]map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatal(err)
		}
		msg := m["message"]
		if _, has := msg["notification"]; has {
			t.Fatal("không được có khối notification")
		}
		data := msg["data"].(map[string]any)
		if len(data) != 2 || data["kind"] != port.KindDirective || data["notification_id"] == "" {
			t.Fatalf("data chỉ được có kind + notification_id: %v", data)
		}
		s := string(body)
		for _, bad := range []string{"title", "body", "Chỉ đạo", a.actor.String(), a.org.String()} {
			if strings.Contains(s, bad) {
				t.Fatalf("thông điệp %d chứa %q: %s", i, bad, s)
			}
		}
	}
	// Token hỏng đã ngừng dùng: thông báo sau không xếp lần gửi cho b.
	env2, _ := f.directive(f.tenant, port.SeverityNormal, []string{ward1}, b.actor)
	f.process(extport.NotifyConsumerDirective, env2)
	_ = f.flushPush(f.tenant)
	if len(f.pusher.messages()) != 2 {
		t.Fatal("token đã báo hỏng vẫn được gửi")
	}
}

// Gắn token đẩy: thiết bị phải đã đăng ký ở pf/sync bởi chính người gọi, chưa thu hồi.
func TestPushTokenRequiresRegisteredDevice(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	b := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	body := map[string]any{"provider": "fcm", "token": fcmToken}

	if r := a.post("/v1/devices/"+ids.New().String()+"/push-token", body); r.status != http.StatusForbidden || code(r) != "notify.device_unregistered" {
		t.Fatalf("thiết bị chưa đăng ký: %d %v", r.status, r.body)
	}
	bDev := ids.New()
	f.devices.add(f.tenant, syncport.DeviceInfo{ID: bDev, ActorRef: b.actor, Platform: "ios"})
	if r := a.post("/v1/devices/"+bDev.String()+"/push-token", body); r.status != http.StatusForbidden || code(r) != "notify.device_unregistered" {
		t.Fatalf("thiết bị của người khác: %d %v", r.status, r.body)
	}
	rev := ids.New()
	f.devices.add(f.tenant, syncport.DeviceInfo{ID: rev, ActorRef: a.actor, Platform: "android", Revoked: true})
	if r := a.post("/v1/devices/"+rev.String()+"/push-token", body); r.status != http.StatusForbidden || code(r) != "notify.device_revoked" {
		t.Fatalf("thiết bị đã thu hồi: %d %v", r.status, r.body)
	}
	if r := a.post("/v1/devices/"+ids.New().String()+"/push-token", map[string]any{"token": "ngắn"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("token sai dạng: %d", r.status)
	}
	ok := ids.New()
	f.devices.add(f.tenant, syncport.DeviceInfo{ID: ok, ActorRef: a.actor, Platform: "android"})
	r := a.post("/v1/devices/"+ok.String()+"/push-token", body)
	if r.status != http.StatusOK || r.body["active"] != true || r.body["platform"] != "android" {
		t.Fatalf("gắn token: %d %v", r.status, r.body)
	}
	if _, has := r.body["token"]; has {
		t.Fatal("phản hồi không được trả lại token")
	}
	// Tenant khác không thấy thiết bị của tenant này.
	x := f.userIn(ids.New(), ids.New(), authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	x2 := f.asActor(x.tid, x.org, a.actor, partyport.RoleKNOfficer)
	if r := x2.post("/v1/devices/"+ok.String()+"/push-token", body); r.status != http.StatusForbidden {
		t.Fatalf("tenant khác: %d", r.status)
	}

	// pf.sync.device.revoked → token ngừng dùng, không còn nhận đẩy.
	f.process(port.ConsumerSync, f.event(f.tenant, syncport.EventDeviceRevoked, syncport.DeviceRevokedV1{
		DeviceID: ok, ActorRef: a.actor, RevokedBy: ids.New(), Reason: "Máy bị mất khi đi thực địa", EventTime: f.clk.Now().Format(time.RFC3339)}))
	env, _ := f.directive(f.tenant, port.SeverityNormal, []string{ward1}, a.actor)
	f.process(extport.NotifyConsumerDirective, env)
	_ = f.flushPush(f.tenant)
	if len(f.pusher.messages()) != 0 {
		t.Fatal("thiết bị đã thu hồi vẫn nhận đẩy")
	}
}

// US-SYNC-02: đồng bộ ngay theo đơn vị/thiết bị trong phạm vi; giới hạn tần suất.
func TestNudgeScopeAndRateLimit(t *testing.T) {
	f := newFixture(t)
	off := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	offDev := off.device(fcmToken)
	off92 := f.user(authz.Scope{Units: []string{ward92}}, partyport.RoleKNOfficer)
	dev92 := off92.device("fcm-token-can-tho-00000000000000000")
	prov := f.user(authz.Scope{Units: []string{prov91, ward1, ward2}}, partyport.RoleKNProvinceAdmin)
	devAdmin := f.user(authz.Scope{}, syncport.RoleDeviceAdmin)

	if r := off.post("/v1/sync/nudge", map[string]any{"unit": ward1}); r.status != http.StatusForbidden || code(r) != "forbidden" {
		t.Fatalf("cán bộ xã không được nudge: %d %v", r.status, r.body)
	}
	if r := prov.post("/v1/sync/nudge", map[string]any{"unit": ward92}); r.status != http.StatusForbidden || code(r) != "notify.out_of_scope" {
		t.Fatalf("đơn vị ngoài phạm vi: %d %v", r.status, r.body)
	}
	if r := prov.post("/v1/sync/nudge", map[string]any{"unit": ward1, "device_id": offDev.String()}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("cả unit và device_id: %d", r.status)
	}
	if r := prov.post("/v1/sync/nudge", map[string]any{"device_id": dev92.String()}); r.status != http.StatusNotFound || code(r) != "notify.nudge_target_not_found" {
		t.Fatalf("thiết bị ngoài phạm vi: %d %v", r.status, r.body)
	}
	r := prov.post("/v1/sync/nudge", map[string]any{"unit": ward1})
	if r.status != http.StatusAccepted || r.body["devices"] != float64(1) {
		t.Fatalf("nudge xã: %d %v", r.status, r.body)
	}
	r = prov.post("/v1/sync/nudge", map[string]any{"unit": ward1})
	if r.status != http.StatusTooManyRequests || code(r) != "notify.nudge_rate_limited" || r.hdr.Get("Retry-After") == "" {
		t.Fatalf("nudge lặp trong 5 phút: %d %v %v", r.status, r.body, r.hdr)
	}
	f.clk.Advance(6 * time.Minute)
	if r := prov.post("/v1/sync/nudge", map[string]any{"unit": ward1}); r.status != http.StatusAccepted {
		t.Fatalf("sau thời gian chờ: %d %v", r.status, r.body)
	}
	// device_admin nudge được mọi thiết bị của tenant, cả qua Admin API.
	ar := devAdmin.do(f.admin.URL, http.MethodPost, "/v1/sync/nudge", key(), map[string]any{"device_id": dev92.String()})
	if ar.status != http.StatusAccepted || ar.body["devices"] != float64(1) {
		t.Fatalf("device_admin: %d %v", ar.status, ar.body)
	}
	if err := f.flushPush(f.tenant); err != nil {
		t.Fatal(err)
	}
	msgs := f.pusher.messages()
	if len(msgs) != 3 {
		t.Fatalf("muốn 3 thông điệp sync_now, nhận %d", len(msgs))
	}
	for _, m := range msgs {
		if d := m.Data(); d["kind"] != port.KindSyncNow || len(d) != 1 || !m.Urgent {
			t.Fatalf("thông điệp sync_now: %v", d)
		}
	}
}

// Entity đồng bộ "notification": con trỏ tăng dần; đánh dấu đã đọc làm thông báo đi
// xuống lần nữa với read_at.
func TestNotificationSyncSourceCursor(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	env, _ := f.directive(f.tenant, port.SeverityNormal, []string{ward1}, a.actor)
	f.process(extport.NotifyConsumerDirective, env)
	f.process(port.ConsumerContent, f.event(f.tenant, contentport.EventAlertPublished, contentport.AlertPublishedV1{
		DocumentRef: ids.New().String(), VersionNo: 1, Title: "Mặn xâm nhập", Summary: "Trữ nước ngọt",
		AudienceLevel: "commune", AudienceUnits: []string{ward1}, PublisherRef: ids.New().String(), PublishedAt: f.clk.Now().Format(time.RFC3339)}))
	src := f.mod.SyncSource()
	sc := authz.Scope{Units: []string{ward1}}
	ch, last, err := src.Changes(a.ctx(), sc, port.EntityNotification, 0, 100)
	if err != nil || len(ch) != 2 {
		t.Fatalf("lượt đầu: %v %d", err, len(ch))
	}
	// App HTX: nông dân cùng xã không nhận thông báo chủ đề (chỉ đạo, cảnh báo theo xã) qua pull.
	farmer := f.asActor(f.tenant, uuid.Nil, ids.New(), "farmer")
	if fch, _, err := src.(syncport.FarmerSource).FarmerChanges(farmer.ctx(), syncport.FarmerScope{Communes: []string{ward1}}, port.EntityNotification, 0, 100); err != nil || len(fch) != 0 {
		t.Fatalf("nông dân nhận thông báo chủ đề: %v %d", err, len(fch))
	}
	if ch[0].Version >= ch[1].Version || last != ch[1].Version {
		t.Fatal("version phải tăng dần, last = version lớn nhất")
	}
	if ch2, _, _ := src.Changes(a.ctx(), sc, port.EntityNotification, last, 100); len(ch2) != 0 {
		t.Fatalf("sau con trỏ không còn thay đổi, nhận %d", len(ch2))
	}
	if ch1, l1, _ := src.Changes(a.ctx(), sc, port.EntityNotification, 0, 1); len(ch1) != 1 || l1 != ch[0].Version {
		t.Fatal("limit 1 phải trả thay đổi đầu tiên")
	}
	// Đọc thông báo chủ đề (biên nhận) trên web → đi xuống lại với read_at.
	topicID := ch[1].ID
	if r := a.post("/v1/notifications/"+topicID.String()+"/read", nil); r.status != http.StatusOK || r.body["read_at"] == nil {
		t.Fatalf("đánh dấu đọc: %d %v", r.status, r.body)
	}
	ch3, last3, _ := src.Changes(a.ctx(), sc, port.EntityNotification, last, 100)
	if len(ch3) != 1 || ch3[0].ID != topicID || last3 <= last {
		t.Fatalf("sau khi đọc: %d thay đổi", len(ch3))
	}
	var d map[string]any
	_ = json.Unmarshal(ch3[0].Data, &d)
	if d["read_at"] == nil || d["kind"] != "alert" {
		t.Fatalf("data: %v", d)
	}
	// Người khác đọc không làm thay đổi hộp thư của a.
	b := f.user(sc, partyport.RoleKNOfficer)
	_ = b.post("/v1/notifications/"+topicID.String()+"/read", nil)
	if ch4, _, _ := src.Changes(a.ctx(), sc, port.EntityNotification, last3, 100); len(ch4) != 0 {
		t.Fatal("biên nhận của người khác không được đi xuống máy của a")
	}
}

// Đọc, đọc hết, đếm chưa đọc; thông báo chủ đề đọc riêng từng người.
func TestReadUnreadCountAndReadAll(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	b := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	for range 2 {
		env, _ := f.directive(f.tenant, port.SeverityNormal, []string{ward1}, a.actor)
		f.process(extport.NotifyConsumerDirective, env)
	}
	f.process(port.ConsumerContent, f.event(f.tenant, contentport.EventAlertPublished, contentport.AlertPublishedV1{
		DocumentRef: ids.New().String(), VersionNo: 1, Title: "Rét hại", Summary: "Che phủ mạ",
		AudienceLevel: "commune", AudienceUnits: []string{ward1}, PublisherRef: ids.New().String(), PublishedAt: f.clk.Now().Format(time.RFC3339)}))
	unread := func(c caller) float64 {
		r := c.get("/v1/notifications/unread-count")
		n, _ := r.body["unread"].(float64)
		return n
	}
	if unread(a) != 3 || unread(b) != 1 {
		t.Fatalf("chưa đọc: a=%v b=%v", unread(a), unread(b))
	}
	// Phân trang cursor, mới nhất trước.
	p1 := a.get("/v1/notifications?limit=2")
	cur, _ := p1.body["next_cursor"].(string)
	if len(items(p1)) != 2 || cur == "" {
		t.Fatalf("trang 1: %v", p1.body)
	}
	if p2 := a.get("/v1/notifications?limit=2&cursor=" + cur); len(items(p2)) != 1 || p2.body["next_cursor"] != nil {
		t.Fatalf("trang 2: %v", p2.body)
	}
	if r := a.post("/v1/notifications/read-all", nil); r.status != http.StatusOK || r.body["marked"] != float64(3) {
		t.Fatalf("read-all: %d %v", r.status, r.body)
	}
	if unread(a) != 0 || unread(b) != 1 {
		t.Fatal("đọc hết của a không được đánh dấu cho b")
	}
	if n := len(items(a.get("/v1/notifications?unread=true"))); n != 0 {
		t.Fatalf("unread=true còn %d", n)
	}
	// Thông báo cá nhân của người khác → 404.
	id := a.inbox()[1]["id"].(string)
	if r := b.post("/v1/notifications/"+id+"/read", nil); r.status != http.StatusNotFound || code(r) != "notify.notification_not_found" {
		t.Fatalf("đọc thông báo của người khác: %d %v", r.status, r.body)
	}
}

// Cách ly tenant: cùng actor_ref ở tenant khác không thấy, không đọc được.
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	env, _ := f.directive(f.tenant, port.SeverityNormal, []string{ward1}, a.actor)
	f.process(extport.NotifyConsumerDirective, env)
	f.process(port.ConsumerContent, f.event(f.tenant, contentport.EventAlertPublished, contentport.AlertPublishedV1{
		DocumentRef: ids.New().String(), VersionNo: 1, Title: "Lũ sớm", Summary: "Thu hoạch sớm",
		AudienceLevel: "national", PublisherRef: ids.New().String(), PublishedAt: f.clk.Now().Format(time.RFC3339)}))
	id := a.inbox()[0]["id"].(string)

	other := f.userIn(ids.New(), a.org, authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	same := f.asActor(other.tid, a.org, a.actor, partyport.RoleKNOfficer)
	if n := len(same.inbox()); n != 0 {
		t.Fatalf("tenant khác thấy %d thông báo", n)
	}
	if r := same.post("/v1/notifications/"+id+"/read", nil); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác đọc: %d", r.status)
	}
	if r := same.post("/v1/notifications/read-all", nil); r.body["marked"] != float64(0) {
		t.Fatalf("tenant khác read-all: %v", r.body)
	}
}

// R-51: gọi lặp POST cùng Idempotency-Key trả đúng phản hồi cũ.
func TestIdempotentPOST(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	env, _ := f.directive(f.tenant, port.SeverityNormal, []string{ward1}, a.actor)
	f.process(extport.NotifyConsumerDirective, env)
	id := a.inbox()[0]["id"].(string)
	k := key()
	r1 := a.do(f.app.URL, http.MethodPost, "/v1/notifications/"+id+"/read", k, nil)
	f.clk.Advance(time.Minute)
	r2 := a.do(f.app.URL, http.MethodPost, "/v1/notifications/"+id+"/read", k, nil)
	if r1.status != http.StatusOK || r2.status != http.StatusOK || r2.hdr.Get("Idempotent-Replayed") != "true" || r1.body["read_at"] != r2.body["read_at"] {
		t.Fatalf("idempotency: %d %d %v", r1.status, r2.status, r2.hdr)
	}
	if r := a.do(f.app.URL, http.MethodPost, "/v1/notifications/read-all", "", nil); r.status != http.StatusBadRequest {
		t.Fatalf("thiếu Idempotency-Key: %d", r.status)
	}
}

// KN US-TASK-02 AC-2: leo thang tới người được giao + cán bộ cấp tỉnh trong danh bạ;
// tỉnh chưa có cán bộ trong danh bạ → chủ đề cho vai kn_province_admin của tỉnh.
func TestTaskEscalatedRecipients(t *testing.T) {
	f := newFixture(t)
	assignee := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	commune := f.user(authz.Scope{Units: []string{ward2}}, partyport.RoleKNOfficer)
	admin := f.user(authz.Scope{Units: []string{prov91, ward1, ward2}}, partyport.RoleKNProvinceAdmin)
	f.staff.add(f.tenant, port.StaffRef{ActorRef: admin.actor, UnitCode: prov91, ProvinceCode: prov91})
	f.staff.add(f.tenant, port.StaffRef{ActorRef: commune.actor, UnitCode: ward2, ProvinceCode: prov91})
	task := func(province string, assigneeRef *uuid.UUID) outbox.Envelope {
		return f.event(f.tenant, extport.EventTaskEscalated, extport.TaskEscalatedV1{TaskID: ids.New(), CommuneCode: ward1,
			ProvinceCode: province, Reason: extport.EscalationReasonSLA, Status: "assigned", Priority: "high", Category: "sau_benh",
			AssigneeRef: assigneeRef, DueAt: f.clk.Now().Add(-time.Hour).Format(time.RFC3339), EventTime: f.clk.Now().Format(time.RFC3339), Seq: 3})
	}
	f.process(extport.NotifyConsumerTask, task(prov91, &assignee.actor))
	if len(assignee.inbox()) != 1 || len(admin.inbox()) != 1 || len(commune.inbox()) != 0 {
		t.Fatalf("leo thang: giao=%d tỉnh=%d xã khác=%d", len(assignee.inbox()), len(admin.inbox()), len(commune.inbox()))
	}
	if it := admin.inbox()[0]; it["kind"] != extport.KindTaskEscalated || it["severity"] != "urgent" || it["topic"] != false {
		t.Fatalf("thông báo leo thang: %v", it)
	}
	// Cần Thơ chưa có cán bộ tỉnh trong danh bạ → chủ đề.
	ct := f.user(authz.Scope{Units: []string{prov92, ward92}}, partyport.RoleKNProvinceAdmin)
	ctOfficer := f.user(authz.Scope{Units: []string{prov92, ward92}}, partyport.RoleKNOfficer)
	f.process(extport.NotifyConsumerTask, task(prov92, nil))
	if len(ct.inbox()) != 1 || len(ctOfficer.inbox()) != 0 || len(admin.inbox()) != 1 {
		t.Fatal("chủ đề leo thang chỉ tới vai kn_province_admin của tỉnh")
	}
}

// C-8: xung đột đồng bộ tới người xử lý (vai sync_conflict_resolver) của tổ chức phụ
// trách; tổ chức bị vô hiệu hóa → token của tổ chức ngừng dùng.
func TestConflictAndOrgDeactivated(t *testing.T) {
	f := newFixture(t)
	owner := ids.New()
	resolver := f.userIn(f.tenant, owner, authz.Scope{Units: []string{ward1}}, syncport.RoleConflictResolver)
	member := f.asActor(f.tenant, owner, ids.New(), partyport.RoleKNOfficer)
	otherResolver := f.user(authz.Scope{Units: []string{ward1}}, syncport.RoleConflictResolver)
	f.process(port.ConsumerSync, f.event(f.tenant, syncport.EventConflictOpened, syncport.ConflictOpenedV1{ConflictID: ids.New(),
		Entity: "plot_boundary", RecordID: ids.New(), DeviceID: ids.New(), OwnerOrgRef: &owner,
		OpenedAt: f.clk.Now().Format(time.RFC3339), DueAt: f.clk.Now().Add(72 * time.Hour).Format(time.RFC3339)}))
	if len(resolver.inbox()) != 1 || len(member.inbox()) != 0 || len(otherResolver.inbox()) != 0 {
		t.Fatalf("xung đột: phụ trách=%d thành viên=%d tổ chức khác=%d", len(resolver.inbox()), len(member.inbox()), len(otherResolver.inbox()))
	}

	// Vô hiệu hóa tổ chức: token của thành viên ngừng dùng; cán bộ địa bàn nhận thông báo.
	member.device(fcmToken)
	prov := f.user(authz.Scope{Units: []string{prov91, ward1, ward2}}, partyport.RoleKNProvinceAdmin)
	f.process(port.ConsumerParty, f.event(f.tenant, partyport.EventOrgDeactivated, partyport.OrgDeactivatedV1{OrgRef: owner,
		Kind: "cooperative", Code: "1801234567", AdminUnitCode: ward1, Reason: "HTX ngừng hoạt động theo quyết định giải thể",
		ActorRef: ids.New(), EventTime: f.clk.Now().Format(time.RFC3339), Seq: 2, ActiveHouseholds: 12}))
	got := prov.inbox()
	if len(got) != 1 || got[0]["kind"] != port.KindOrgDeactivated || strings.Contains(got[0]["body"].(string), "1801234567") {
		t.Fatalf("thông báo vô hiệu hóa: %v", got)
	}
	env, _ := f.directive(f.tenant, port.SeverityNormal, []string{ward1}, member.actor)
	f.process(extport.NotifyConsumerDirective, env)
	_ = f.flushPush(f.tenant)
	for _, m := range f.pusher.messages() {
		if m.Token == fcmToken {
			t.Fatal("thiết bị của tổ chức đã vô hiệu hóa vẫn nhận đẩy")
		}
	}
}

// Chỉ đạo khẩn được xếp vào email tóm tắt; email không có dữ liệu cá nhân.
func TestUrgentDirectiveEmailDigest(t *testing.T) {
	env := pgtest.Get(t)
	f := newFixture(t)
	mail := &fakeMailer{}
	mod := notify.New(f.pool, f.clk, notify.Deps{Scope: f.scope, Consumers: []port.ConsumerRegistration{extension.NotificationConsumers()}, Builders: extension.DirectiveNotificationBuilders(nil, nil), Digests: []port.DigestRegistration{extension.NotificationDigest()}, Mailer: mail, DigestTo: []string{"ttkn.angiang@example.vn"}})
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	urgent, id := f.directive(f.tenant, port.SeverityUrgent, []string{ward1}, a.actor)
	normal, _ := f.directive(f.tenant, port.SeverityNormal, []string{ward1}, a.actor)
	for _, e := range []outbox.Envelope{urgent, normal, urgent} {
		_, _ = inbox.Process(context.Background(), f.pool, extport.NotifyConsumerDirective, e, mod.EventHandler())
	}
	all := notify.DigestTenants(env.Pool(t, migrations.PF, dbadmin.OpsJobs))
	// Chỉ tenant của kiểm thử này (database dùng chung giữa các kiểm thử của gói).
	mine := func(ctx context.Context) ([]uuid.UUID, error) {
		ts, err := all(ctx)
		var out []uuid.UUID
		for _, x := range ts {
			if x == f.tenant {
				out = append(out, x)
			}
		}
		return out, err
	}
	run := func() {
		for _, j := range mod.Jobs(mine, nil) {
			if err := j.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
	run()
	run()
	if len(mail.sent) != 1 {
		t.Fatalf("muốn đúng 1 email, nhận %d", len(mail.sent))
	}
	m := mail.sent[0]
	if !strings.Contains(m.body, id.String()) || strings.Count(m.body, "/extension/directives/") != 1 || strings.Contains(m.body, a.actor.String()) {
		t.Fatalf("nội dung email: %s", m.body)
	}
}

type sentMail struct{ subject, body string }

type fakeMailer struct{ sent []sentMail }

func (m *fakeMailer) Send(_ context.Context, _ []string, subject, body string) error {
	m.sent = append(m.sent, sentMail{subject, body})
	return nil
}
