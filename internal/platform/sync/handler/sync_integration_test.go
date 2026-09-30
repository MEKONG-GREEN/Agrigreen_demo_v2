package handler_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	syncmod "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// Đơn vị hành chính dùng trong kiểm thử.
const (
	unitA = "30001"
	unitB = "30002"
)

type fixture struct {
	t       *testing.T
	iss     *authztest.FakeIssuer
	app     *httptest.Server
	admin   *httptest.Server
	mod     *syncmod.Module
	pool    *pgxpool.Pool
	clk     *clock.Fixed
	scope   *fakeScope
	consent *fakeConsent
	vault   *clienttest.Fake
	objects *objstore.Local
	source  *memSource
	sink    *memSink
	tenant  uuid.UUID
}

type opts struct {
	slaHours *decimal.Decimal
	extra    []port.Source            // Source thêm (vd FarmerSource)
	farmers  accport.FarmerHouseholds // nối UseFarmers
}

func newFixture(t *testing.T, o ...opts) *fixture {
	t.Helper()
	env := pgtest.Get(t)
	var op opts
	if len(o) > 0 {
		op = o[0]
	}
	f := &fixture{
		t: t, iss: authztest.New(t), pool: env.Pool(t, migrations.PF, syncmod.DBRole), clk: clock.NewFixed(time.Now()),
		scope: &fakeScope{scopes: map[uuid.UUID]authz.Scope{}}, consent: &fakeConsent{denied: map[uuid.UUID]bool{}},
		vault: clienttest.New(), source: newMemSource("plot", "task"), sink: newMemSink("field_report", "plot_boundary"),
		tenant: ids.New(),
	}
	objs, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{PresignSecret: []byte("presign-secret-kiem-thu-0123456789"), BaseURL: "http://objstore.test"})
	if err != nil {
		t.Fatal(err)
	}
	f.objects = objs
	ops := env.Pool(t, migrations.PF, "platform_api")
	f.mod = syncmod.New(f.pool, f.clk, syncmod.Deps{
		Scope: f.scope, Consent: f.consent, Params: fakeParams{hours: op.slaHours}, Vault: f.vault, Store: objs,
		CursorKey: []byte("khoa-con-tro-kiem-thu-0123456789abcdef"),
		Sources:   append([]port.Source{f.source}, op.extra...), Sinks: []port.Sink{f.sink},
	})
	if op.farmers != nil {
		f.mod.UseFarmers(op.farmers)
	}
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
	f      *fixture
	tid    uuid.UUID
	org    uuid.UUID
	actor  uuid.UUID
	roles  []string
	token  string
	device uuid.UUID
}

// officer là cán bộ hiện trường của tenant mặc định, phạm vi unitA.
func (f *fixture) officer(roles ...string) caller {
	org := ids.New()
	f.scope.set(org, authz.Scope{Units: []string{unitA}})
	return f.as(f.tenant, org, roles...)
}

func (f *fixture) as(tid, org uuid.UUID, roles ...string) caller {
	actor := ids.New()
	return caller{f: f, tid: tid, org: org, actor: actor, roles: roles,
		token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, MFA: true})}
}

// ctx là context mang Principal của người gọi — để gọi thẳng kho giả.
func (c caller) ctx() context.Context {
	return authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: c.actor, TenantID: c.tid, OrgRef: c.org, Roles: c.roles})
}

type resp struct {
	status int
	hdr    http.Header
	raw    []byte
	body   map[string]any
}

func (c caller) do(base, method, path, idem string, hdr map[string]string, ctype string, body []byte) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if c.device != uuid.Nil {
		req.Header.Set(port.HeaderDeviceID, c.device.String())
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: r.StatusCode, hdr: r.Header, raw: raw, body: m}
}

func key() string { return "k-" + ids.New().String() }

func (c caller) post(path string, body any) resp {
	b, _ := json.Marshal(body)
	return c.do(c.f.app.URL, http.MethodPost, path, key(), nil, "application/json", b)
}

func (c caller) get(path string, hdr map[string]string) resp {
	return c.do(c.f.app.URL, http.MethodGet, path, "", hdr, "", nil)
}

func (c caller) adminPost(path string, body any) resp {
	b, _ := json.Marshal(body)
	return c.do(c.f.admin.URL, http.MethodPost, path, key(), nil, "application/json", b)
}

func (c caller) adminGet(path string) resp {
	return c.do(c.f.admin.URL, http.MethodGet, path, "", nil, "", nil)
}

func code(r resp) string { s, _ := r.body["code"].(string); return s }

// register đăng ký một thiết bị mới cho người gọi và gắn vào header các lời gọi sau.
func (c *caller) register() {
	c.f.t.Helper()
	id := ids.New()
	r := c.post("/v1/devices", map[string]any{"device_id": id.String(), "platform": "android", "app_version": "1.0.0+12"})
	if r.status != http.StatusCreated {
		c.f.t.Fatalf("đăng ký thiết bị: %d %s", r.status, r.raw)
	}
	c.device = id
}

type pullPage struct {
	Changes []struct {
		Entity  string          `json:"entity"`
		ID      string          `json:"id"`
		Version int64           `json:"version"`
		Op      string          `json:"op"`
		Data    json.RawMessage `json:"data"`
	} `json:"changes"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
	ServerTime string `json:"server_time"`
	Reset      bool   `json:"reset"`
}

func (c caller) pull(cursor string, limit int) (pullPage, resp) {
	c.f.t.Helper()
	q := url.Values{}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprint(limit))
	}
	r := c.get("/v1/sync/pull?"+q.Encode(), nil)
	var pg pullPage
	if r.status == http.StatusOK {
		if err := json.Unmarshal(r.raw, &pg); err != nil {
			c.f.t.Fatal(err)
		}
	}
	return pg, r
}

// ---------------------------------------------------------------------------
// Thiết bị
// ---------------------------------------------------------------------------

// Mục 15: thiết bị bị thu hồi → mọi lời gọi đồng bộ trả 403 sync.device_revoked.
func TestDeviceRegisterRevoke(t *testing.T) {
	f := newFixture(t)
	c := f.officer()
	id := ids.New()
	body := map[string]any{"device_id": id.String(), "platform": "android", "app_version": "1.0.0"}
	r := c.post("/v1/devices", body)
	if r.status != http.StatusCreated || r.body["device_id"] != id.String() || r.body["registered_at"] == nil {
		t.Fatalf("đăng ký: %d %s", r.status, r.raw)
	}
	if r := c.post("/v1/devices", body); r.status != http.StatusOK {
		t.Fatalf("đăng ký lại cùng người: %d %s", r.status, r.raw)
	}
	other := f.officer()
	if r := other.post("/v1/devices", body); r.status != http.StatusConflict || code(r) != "sync.device_taken" {
		t.Fatalf("người khác lấy mã thiết bị: %d %s", r.status, r.raw)
	}
	if r := c.post("/v1/devices", map[string]any{"device_id": uuid.New().String(), "platform": "symbian", "app_version": ""}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("UUIDv4/nền tảng lạ: %d", r.status)
	}
	c.device = id
	if _, r := c.pull("", 0); r.status != http.StatusOK {
		t.Fatalf("pull trước thu hồi: %d %s", r.status, r.raw)
	}

	// Không có vai device_admin → 403.
	if r := f.officer().adminPost("/v1/devices/"+id.String()+"/revoke", map[string]any{"reason": "mất máy ngoài đồng"}); r.status != http.StatusForbidden {
		t.Fatalf("thu hồi không có vai: %d", r.status)
	}
	adm := f.officer(port.RoleDeviceAdmin)
	if r := adm.adminPost("/v1/devices/"+id.String()+"/revoke", map[string]any{"reason": "ngắn"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("lý do ngắn: %d", r.status)
	}
	if r := adm.adminPost("/v1/devices/"+id.String()+"/revoke", map[string]any{"reason": "Cán bộ báo mất máy ngày 02/12"}); r.status != http.StatusOK || r.body["revoked"] != true {
		t.Fatalf("thu hồi: %d %s", r.status, r.raw)
	}
	if r := adm.adminPost("/v1/devices/"+id.String()+"/revoke", map[string]any{"reason": "Cán bộ báo mất máy ngày 02/12"}); code(r) != "sync.device_already_revoked" {
		t.Fatalf("thu hồi lần hai: %d %s", r.status, r.raw)
	}

	if _, r := c.pull("", 0); r.status != http.StatusForbidden || code(r) != "sync.device_revoked" {
		t.Fatalf("pull sau thu hồi: %d %s", r.status, r.raw)
	}
	if r := c.post("/v1/sync/push", map[string]any{"device_id": id.String(), "records": []any{}}); code(r) != "sync.device_revoked" {
		t.Fatalf("push sau thu hồi: %d %s", r.status, r.raw)
	}
	up := f.upload(c, c.sealFor(unitA, []byte("ảnh")), nil)
	if code(up) != "sync.device_revoked" {
		t.Fatalf("tải tệp sau thu hồi: %d %s", up.status, up.raw)
	}
	if r := c.post("/v1/devices", body); code(r) != "sync.device_revoked" {
		t.Fatalf("đăng ký lại mã đã thu hồi: %d %s", r.status, r.raw)
	}

	// Thiết bị lạ, thiếu header, thiết bị của người khác → sync.device_unregistered.
	c.device = ids.New()
	if _, r := c.pull("", 0); code(r) != "sync.device_unregistered" {
		t.Fatalf("thiết bị lạ: %d %s", r.status, r.raw)
	}
	c.device = uuid.Nil
	if _, r := c.pull("", 0); code(r) != "sync.device_unregistered" {
		t.Fatalf("thiếu header: %d %s", r.status, r.raw)
	}
	other.register()
	other.device, c.device = uuid.Nil, other.device
	if _, r := c.pull("", 0); code(r) != "sync.device_unregistered" {
		t.Fatalf("thiết bị của người khác: %d %s", r.status, r.raw)
	}

	lst := adm.adminGet("/v1/devices?actor_ref=" + c.actor.String())
	if items, _ := lst.body["items"].([]any); lst.status != http.StatusOK || len(items) != 1 {
		t.Fatalf("liệt kê thiết bị: %d %s", lst.status, lst.raw)
	}
}

// ---------------------------------------------------------------------------
// Pull
// ---------------------------------------------------------------------------

// TestPullLimitOneNoStarvation: limit=1, entity đầu (plot) luôn còn dữ liệu — entity đứng sau
// (task) vẫn phải được cấp lượt (vòng chia hạn mức xoay theo trang, KN US-SYNC-01).
func TestPullLimitOneNoStarvation(t *testing.T) {
	f := newFixture(t)
	c := f.officer()
	c.register()
	f.source.add("plot", unitA, 40)
	tasks := f.source.add("task", unitA, 1)

	cursor, seenTask := "", false
	for page := 1; page <= 12 && !seenTask; page++ {
		pg, r := c.pull(cursor, 1)
		if r.status != http.StatusOK {
			t.Fatalf("pull: %d %s", r.status, r.raw)
		}
		if len(pg.Changes) > 1 {
			t.Fatalf("trang vượt limit=1: %d", len(pg.Changes))
		}
		for _, ch := range pg.Changes {
			if ch.Entity == "task" && ch.ID == tasks[0].String() {
				seenTask = true
			}
		}
		cursor = pg.NextCursor
	}
	if !seenTask {
		t.Fatal("task bị bỏ đói: 12 trang limit=1 không giao bản ghi task nào")
	}
}

func TestPullPaginationRoundRobin(t *testing.T) {
	f := newFixture(t)
	c := f.officer(client.RolePubKeySync)
	c.register()
	plots := f.source.add("plot", unitA, 7)
	tasks := f.source.add("task", unitA, 2)
	f.source.add("plot", unitB, 3) // ngoài phạm vi — không được giao
	hh, err := f.vault.Create(c.ctx(), client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectHousehold,
		FullName: "Nguyễn Văn A", Phone: "0912345678", AreaCode: unitA})
	if err != nil {
		t.Fatal(err)
	}

	got := map[string][]string{}
	cursor, pages := "", 0
	for {
		pg, r := c.pull(cursor, 4)
		if r.status != http.StatusOK {
			t.Fatalf("pull: %d %s", r.status, r.raw)
		}
		pages++
		if len(pg.Changes) > 4 {
			t.Fatalf("trang vượt limit: %d", len(pg.Changes))
		}
		if pages == 1 {
			seen := map[string]bool{}
			for _, ch := range pg.Changes {
				seen[ch.Entity] = true
			}
			if !seen["plot"] || !seen["task"] || !pg.HasMore {
				t.Fatalf("trang đầu phải chia cho mọi entity (round-robin) và has_more: %+v", pg)
			}
		}
		for _, ch := range pg.Changes {
			got[ch.Entity] = append(got[ch.Entity], ch.ID)
		}
		cursor = pg.NextCursor
		if !pg.HasMore {
			break
		}
		if pages > 10 {
			t.Fatal("không dừng")
		}
	}
	if fmt.Sprint(got["plot"]) != fmt.Sprint(uuidStrings(plots)) || fmt.Sprint(got["task"]) != fmt.Sprint(uuidStrings(tasks)) {
		t.Fatalf("thiếu, thừa hoặc sai thứ tự: %v", got)
	}
	if want := []string{hh.Ref.String(), uuid.Nil.String()}; fmt.Sprint(got["pubkey"]) != fmt.Sprint(want) {
		t.Fatalf("pubkey: khóa của hộ rồi dấu kết thúc lượt, có %v", got["pubkey"])
	}

	// Đã hết: pull lại không có gì. Thay đổi mới → chỉ thay đổi đó.
	if pg, _ := c.pull(cursor, 4); len(pg.Changes) != 0 || pg.HasMore {
		t.Fatalf("sau khi hết: %+v", pg)
	}
	more := f.source.add("task", unitA, 1)
	pg, _ := c.pull(cursor, 4)
	if len(pg.Changes) != 1 || pg.Changes[0].ID != more[0].String() || pg.HasMore {
		t.Fatalf("thay đổi mới: %+v", pg)
	}

	// Hộ mới trong địa bàn → lượt khóa mới, version lớn hơn, kết thúc bằng dấu.
	if _, err := f.vault.Create(c.ctx(), client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectHousehold,
		FullName: "Trần Thị B", Phone: "0987654321", AreaCode: unitA}); err != nil {
		t.Fatal(err)
	}
	pg2, _ := c.pull(pg.NextCursor, 10)
	if len(pg2.Changes) != 3 || pg2.Changes[2].ID != uuid.Nil.String() || pg2.Changes[2].Op != port.OpTombstone {
		t.Fatalf("lượt khóa mới: %+v", pg2)
	}
}

func uuidStrings(xs []uuid.UUID) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.String())
	}
	return out
}

func TestPullCursorTamperRejected(t *testing.T) {
	f := newFixture(t)
	c := f.officer()
	c.register()
	f.source.add("plot", unitA, 3)
	pg, _ := c.pull("", 1)
	if pg.NextCursor == "" || !pg.HasMore {
		t.Fatalf("trang đầu: %+v", pg)
	}
	body, sig, _ := strings.Cut(pg.NextCursor, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(body)
	forged := base64.RawURLEncoding.EncodeToString(bytes.Replace(raw, []byte(`"plot":1`), []byte(`"plot":0`), 1)) + "." + sig
	for name, cur := range map[string]string{"sửa nội dung": forged, "rác": "abc.def", "không chữ ký": body} {
		if _, r := c.pull(cur, 1); r.status != http.StatusBadRequest || code(r) != "sync.cursor_invalid" {
			t.Fatalf("%s: %d %s", name, r.status, r.raw)
		}
	}
	// Con trỏ của thiết bị khác (cùng người) cũng bị từ chối.
	d2 := c
	d2.register()
	if _, r := d2.pull(pg.NextCursor, 1); code(r) != "sync.cursor_invalid" {
		t.Fatalf("con trỏ thiết bị khác: %d %s", r.status, r.raw)
	}
}

// Phạm vi rỗng (không có đơn vị công tác, tổ chức bị vô hiệu hóa) → không thấy gì.
func TestPullEmptyScope(t *testing.T) {
	f := newFixture(t)
	c := f.as(f.tenant, ids.New()) // org không có phạm vi
	c.register()
	f.source.add("plot", unitA, 3)
	before := f.source.calls.Load()
	pg, r := c.pull("", 0)
	if r.status != http.StatusOK || len(pg.Changes) != 0 || pg.HasMore || pg.NextCursor == "" || pg.ServerTime == "" {
		t.Fatalf("phạm vi rỗng: %d %s", r.status, r.raw)
	}
	if f.source.calls.Load() != before {
		t.Fatal("phạm vi rỗng không được gọi Source")
	}
}

// Phạm vi đổi (điều chuyển xã) → con trỏ cũ hết hiệu lực, pull lại từ đầu với reset = true.
func TestPullScopeChangeResets(t *testing.T) {
	f := newFixture(t)
	c := f.officer()
	c.register()
	f.source.add("plot", unitA, 2)
	f.source.add("plot", unitB, 1)
	pg, _ := c.pull("", 0)
	if len(pg.Changes) != 3 || pg.Reset { // 2 thửa + dấu kết thúc lượt khóa
		t.Fatalf("lần đầu: %+v", pg)
	}
	f.scope.set(c.org, authz.Scope{Units: []string{unitB}})
	pg2, _ := c.pull(pg.NextCursor, 0)
	if !pg2.Reset || len(pg2.Changes) != 2 || pg2.Changes[0].Entity != "plot" {
		t.Fatalf("sau khi đổi phạm vi: %+v", pg2)
	}
}

// ---------------------------------------------------------------------------
// Push
// ---------------------------------------------------------------------------

func rec(c caller, seq int, entity string, payload map[string]any) map[string]any {
	b, _ := json.Marshal(payload)
	return map[string]any{
		"id": ids.New().String(), "idem_key": fmt.Sprintf("dev:%s:%d", c.device, seq), "entity": entity, "op": "create",
		"event_time": c.f.clk.Now().Add(-time.Hour).Format(time.RFC3339), "payload": json.RawMessage(b),
	}
}

type pushResult struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	ConflictID string `json:"conflict_id"`
	Code       string `json:"code"`
	Errors     []struct {
		Field string `json:"field"`
	} `json:"errors"`
}

func results(t *testing.T, r resp) []pushResult {
	t.Helper()
	var out struct {
		Results []pushResult `json:"results"`
	}
	if r.status != http.StatusOK {
		t.Fatalf("push: %d %s", r.status, r.raw)
	}
	if err := json.Unmarshal(r.raw, &out); err != nil {
		t.Fatal(err)
	}
	return out.Results
}

// Mục 15: một lô gồm accepted, duplicate, conflict, rejected — bản ghi lỗi không
// làm hỏng lô; gọi lặp cùng Idempotency-Key trả đúng phản hồi cũ; C-8 SLA từ tham số.
func TestPushMixedBatchAndReplay(t *testing.T) {
	hours := decimal.NewFromInt(48)
	f := newFixture(t, opts{slaHours: &hours})
	c := f.officer()
	c.register()

	first := rec(c, 1, "field_report", map[string]any{"note": "lúa đẻ nhánh"})
	if res := results(t, c.post("/v1/sync/push", map[string]any{"device_id": c.device.String(), "records": []any{first}})); res[0].Status != port.StatusAccepted {
		t.Fatalf("lô đầu: %+v", res)
	}
	sinkCalls := f.sink.calls.Load()

	future := rec(c, 7, "field_report", map[string]any{})
	future["event_time"] = f.clk.Now().Add(10 * time.Minute).Format(time.RFC3339)
	badID := rec(c, 8, "field_report", map[string]any{})
	badID["id"] = "khong-phai-uuid"
	otherDev := rec(c, 9, "field_report", map[string]any{})
	otherDev["idem_key"] = "dev:" + ids.New().String() + ":9"
	records := []any{
		rec(c, 2, "field_report", map[string]any{"note": "ok"}), // 0 accepted
		first, // 1 duplicate (lô khác)
		rec(c, 3, "plot_boundary", map[string]any{"behave": "conflict", "conflict_key": "thửa-1"}), // 2 conflict
		rec(c, 4, "plot_boundary", map[string]any{"behave": "reject"}),                             // 3 rejected bởi Sink
		rec(c, 5, "consent", map[string]any{}),                                                     // 4 entity chưa có Sink
		future,                                                                                     // 5 event_time tương lai
		badID,                                                                                      // 6 id sai
		otherDev,                                                                                   // 7 idem_key của thiết bị khác
		rec(c, 10, "field_report", map[string]any{"behave": "panic"}),                               // 8 Sink hoảng
		rec(c, 11, "plot_boundary", map[string]any{"behave": "conflict", "conflict_key": "thửa-1"}), // 9 cùng xung đột
	}
	payload, _ := json.Marshal(map[string]any{"device_id": c.device.String(), "records": records})
	batch := key()
	r := c.do(f.app.URL, http.MethodPost, "/v1/sync/push", batch, nil, "application/json", payload)
	res := results(t, r)
	want := []struct{ status, code, field string }{
		{port.StatusAccepted, "", ""}, {port.StatusDuplicate, "", ""}, {port.StatusConflict, "", ""},
		{port.StatusRejected, "validation_failed", "payload.area_ha"}, {port.StatusRejected, "sync.unknown_entity", "entity"},
		{port.StatusRejected, "validation_failed", "event_time"}, {port.StatusRejected, "validation_failed", "id"},
		{port.StatusRejected, "validation_failed", "idem_key"}, {port.StatusRejected, "sync.sink_failed", ""},
		{port.StatusConflict, "", ""},
	}
	if len(res) != len(want) {
		t.Fatalf("số kết quả: %d", len(res))
	}
	for i, w := range want {
		if res[i].Status != w.status || res[i].Code != w.code || (w.field != "" && (len(res[i].Errors) == 0 || res[i].Errors[0].Field != w.field)) {
			t.Errorf("bản ghi %d: muốn %+v, có %+v", i, w, res[i])
		}
	}
	if res[6].ID != "khong-phai-uuid" {
		t.Fatalf("id sai dạng phải được trả lại nguyên văn để máy khớp: %q", res[6].ID)
	}
	if res[2].ConflictID == "" || res[2].ConflictID != res[9].ConflictID {
		t.Fatalf("hai bản ghi cùng xung đột phải cùng conflict_id: %+v %+v", res[2], res[9])
	}
	// duplicate được nhận ra từ sổ inbound, không gọi lại Sink.
	if calls := f.sink.calls.Load() - sinkCalls; calls != 5 { // 0, 2, 3, 8, 9
		t.Fatalf("số lần gọi Sink: %d", calls)
	}

	// Gọi lặp cùng Idempotency-Key → đúng phản hồi cũ, không gọi Sink.
	calls := f.sink.calls.Load()
	r2 := c.do(f.app.URL, http.MethodPost, "/v1/sync/push", batch, nil, "application/json", payload)
	if r2.status != http.StatusOK || !bytes.Equal(r2.raw, r.raw) || r2.hdr.Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("phát lại: %d %s", r2.status, r2.raw)
	}
	if f.sink.calls.Load() != calls {
		t.Fatal("phát lại không được gọi Sink")
	}

	// Sổ inbound: mọi bản ghi nhận được đều có dòng, chỉ băm payload.
	var rows []store.Inbound
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		var e error
		rows, e = store.InboundByDevice(context.Background(), tx, c.device)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1+len(records) {
		t.Fatalf("sổ inbound: %d dòng", len(rows))
	}
	for _, row := range rows {
		if row.PayloadSHA256 == nil || len(*row.PayloadSHA256) != 64 {
			t.Fatalf("sổ phải có băm payload: %+v", row)
		}
		if (row.Status == port.StatusConflict) != (row.ConflictID != nil) {
			t.Fatalf("conflict_id: %+v", row)
		}
	}

	// Sổ đăng ký xung đột: một mục, due_at = now + 48 giờ (tham số masterdata).
	cid := uuid.MustParse(res[2].ConflictID)
	resolver := f.officer(port.RoleConflictResolver)
	g := resolver.adminGet("/v1/sync/conflicts/" + cid.String())
	due, _ := time.Parse(time.RFC3339Nano, g.body["due_at"].(string))
	if g.status != http.StatusOK || g.body["status"] != "open" || !due.Equal(f.clk.Now().Add(48*time.Hour).Truncate(time.Microsecond)) {
		t.Fatalf("xung đột: %d %s (due %v, now %v)", g.status, g.raw, due, f.clk.Now())
	}
}

// C-8: xung đột quá SLA được đếm cho cảnh báo (mục 19) và lọc được ở Admin API.
func TestConflictSLA(t *testing.T) {
	f := newFixture(t) // chưa có tham số → mặc định 72 giờ
	c := f.officer()
	c.register()
	res := results(t, c.post("/v1/sync/push", map[string]any{"device_id": c.device.String(), "records": []any{
		rec(c, 1, "plot_boundary", map[string]any{"behave": "conflict", "conflict_key": "a"}),
		rec(c, 2, "plot_boundary", map[string]any{"behave": "conflict", "conflict_key": "b"}),
	}}))
	ctx := context.Background()
	sla, err := f.mod.ConflictSLA(ctx, f.tenant)
	if err != nil || sla.Open != 2 || sla.Overdue != 0 {
		t.Fatalf("mới mở: %+v %v", sla, err)
	}
	f.clk.Advance(71 * time.Hour)
	if sla, _ := f.mod.ConflictSLA(ctx, f.tenant); sla.Overdue != 0 {
		t.Fatalf("71 giờ chưa quá hạn: %+v", sla)
	}
	f.clk.Advance(2 * time.Hour)
	sla, _ = f.mod.ConflictSLA(ctx, f.tenant)
	if sla.Overdue != 2 || sla.OldestDueAt == nil {
		t.Fatalf("73 giờ phải quá hạn: %+v", sla)
	}

	resolver := f.officer(port.RoleConflictResolver)
	lst := resolver.adminGet("/v1/sync/conflicts?status=open&overdue=true")
	if items, _ := lst.body["items"].([]any); lst.status != http.StatusOK || len(items) != 2 {
		t.Fatalf("liệt kê quá hạn: %d %s", lst.status, lst.raw)
	}
	if r := c.adminGet("/v1/sync/conflicts"); r.status != http.StatusForbidden {
		t.Fatalf("không có vai: %d", r.status)
	}
	path := "/v1/sync/conflicts/" + res[0].ConflictID + "/resolve"
	r := resolver.adminPost(path, map[string]any{"note": "Giữ ranh giới vẽ sau, đã đối chiếu thực địa"})
	if r.status != http.StatusOK || r.body["status"] != "resolved" || r.body["overdue"] != true {
		t.Fatalf("đóng: %d %s", r.status, r.raw)
	}
	if r := resolver.adminPost(path, map[string]any{"note": "Giữ ranh giới vẽ sau, đã đối chiếu thực địa"}); code(r) != "sync.conflict_state" {
		t.Fatalf("đóng lần hai: %d %s", r.status, r.raw)
	}
	if sla, _ := f.mod.ConflictSLA(ctx, f.tenant); sla.Open != 1 || sla.Overdue != 1 {
		t.Fatalf("sau khi đóng một: %+v", sla)
	}
}

// ---------------------------------------------------------------------------
// Tệp minh chứng
// ---------------------------------------------------------------------------

type sealed struct {
	meta      envelope.Metadata
	blob      []byte
	plaintext []byte
	subject   uuid.UUID
}

// sealFor tạo hộ ở kho giả và mã hóa plaintext bằng khóa công khai của hộ — như máy.
func (c caller) sealFor(area string, plaintext []byte) sealed {
	c.f.t.Helper()
	cr, err := c.f.vault.Create(c.ctx(), client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectHousehold,
		FullName: "Lê Văn C", Phone: fmt.Sprintf("09%08d", time.Now().UnixNano()%100000000), AreaCode: area})
	if err != nil {
		c.f.t.Fatal(err)
	}
	pub, _ := envelope.Key32(cr.PublicKey)
	s, err := envelope.Seal(plaintext, pub)
	if err != nil {
		c.f.t.Fatal(err)
	}
	m := envelope.NewMetadata(cr.Ref.String(), s, "image/jpeg", c.f.clk.Now().Add(-time.Minute).Format(time.RFC3339))
	m.GPS = &envelope.GPS{Lat: 10.03, Lon: 105.78, AccuracyM: 6.5}
	return sealed{meta: m, blob: s.Ciphertext, plaintext: plaintext, subject: cr.Ref}
}

func (f *fixture) upload(c caller, s sealed, fileRef *string, idem ...string) resp {
	f.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	mb, _ := json.Marshal(s.meta)
	_ = w.WriteField("meta", string(mb))
	if fileRef != nil {
		_ = w.WriteField("file_ref", *fileRef)
	}
	part, _ := w.CreateFormFile("blob", "blob.bin")
	_, _ = part.Write(s.blob)
	_ = w.Close()
	k := key()
	if len(idem) > 0 {
		k = idem[0]
	}
	return c.do(f.app.URL, http.MethodPost, "/v1/files", k, nil, w.FormDataContentType(), buf.Bytes())
}

func TestFilesUploadAndView(t *testing.T) {
	f := newFixture(t)
	c := f.officer()
	c.register()
	// Ảnh 2 MB — lớn hơn giới hạn thân chung 1 MB, dưới 15 MB của /v1/files.
	photo := bytes.Repeat([]byte("JPEG"), 512<<10)
	s := c.sealFor(unitA, photo)

	// Sai băm → 422, không có đối tượng nào được ghi.
	bad := s
	bad.meta.SHA256Ciphertext = strings.Repeat("0", 64)
	if r := f.upload(c, bad, nil); r.status != http.StatusUnprocessableEntity || code(r) != "sync.file_checksum_mismatch" {
		t.Fatalf("sai băm: %d %s", r.status, r.raw)
	}
	// wrapped_dek phải đúng 80 byte.
	bad = s
	bad.meta.WrappedDEK = s.meta.WrappedDEK[:79]
	if r := f.upload(c, bad, nil); r.status != http.StatusUnprocessableEntity || !strings.Contains(string(r.raw), "meta.wrapped_dek") {
		t.Fatalf("wrapped_dek 79 byte: %d %s", r.status, r.raw)
	}
	bad = s
	bad.meta.V, bad.meta.SubjectRef = 2, ""
	if r := f.upload(c, bad, nil); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("v = 2, thiếu subject_ref: %d %s", r.status, r.raw)
	}

	ref := ids.New().String()
	r := f.upload(c, s, &ref)
	if r.status != http.StatusCreated || r.body["file_ref"] != ref {
		t.Fatalf("tải: %d %s", r.status, r.raw)
	}
	// Kho đối tượng giữ ĐÚNG bản mã tại files/<tenant>/<file_ref>, không phải bản rõ (R-40).
	rc, info, err := f.objects.Get(context.Background(), "files/"+f.tenant.String()+"/"+ref)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(stored, s.blob) || bytes.Contains(stored, []byte("JPEGJPEG")) || info.ContentType != "application/octet-stream" {
		t.Fatalf("đối tượng lưu phải là bản mã (%d byte, %s)", len(stored), info.ContentType)
	}
	// Gửi lại cùng file_ref, cùng bản mã → 200 bản cũ; khác bản mã → 409.
	if r := f.upload(c, s, &ref); r.status != http.StatusOK {
		t.Fatalf("gửi lại: %d %s", r.status, r.raw)
	}
	s2 := c.sealFor(unitA, []byte("ảnh khác"))
	if r := f.upload(c, s2, &ref); code(r) != "sync.file_ref_taken" {
		t.Fatalf("file_ref đã dùng: %d %s", r.status, r.raw)
	}

	// Xem: cần vai xem minh chứng + mục đích hợp lệ + đồng ý.
	viewer := f.officer(client.RoleEvidenceViewer)
	purpose := map[string]string{port.HeaderPurpose: string(client.PurposeMRVEvidence)}
	if r := c.get("/v1/files/"+ref, purpose); r.status != http.StatusForbidden {
		t.Fatalf("không có vai: %d", r.status)
	}
	if r := viewer.get("/v1/files/"+ref, nil); code(r) != "sync.purpose_required" {
		t.Fatalf("thiếu mục đích: %d %s", r.status, r.raw)
	}
	if r := viewer.get("/v1/files/"+ref, map[string]string{port.HeaderPurpose: "registry.dedup"}); code(r) != "sync.purpose_not_allowed" {
		t.Fatalf("mục đích sai: %d %s", r.status, r.raw)
	}
	g := viewer.get("/v1/files/"+ref, purpose)
	file, _ := g.body["file"].(map[string]any)
	if g.status != http.StatusOK || file["sha256_ciphertext"] != s.meta.SHA256Ciphertext || !strings.HasPrefix(fmt.Sprint(g.body["download_url"]), "http://objstore.test/") {
		t.Fatalf("siêu dữ liệu: %d %s", g.status, g.raw)
	}
	ct := viewer.get("/v1/files/"+ref+"/content", purpose)
	if ct.status != http.StatusOK || !bytes.Equal(ct.raw, photo) || ct.hdr.Get("Content-Type") != "image/jpeg" || ct.hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("nội dung giải mã: %d %d byte", ct.status, len(ct.raw))
	}
	var unwraps int
	for _, a := range f.vault.Accesses() {
		if a.Op == "unwrap" && a.Purpose == client.PurposeMRVEvidence && a.Ref == s.subject {
			unwraps++
		}
	}
	if unwraps != 1 {
		t.Fatalf("mở DEK phải qua kho định danh đúng một lần, có mục đích: %d", unwraps)
	}
	f.consent.mu.Lock()
	f.consent.denied[s.subject] = true
	f.consent.mu.Unlock()
	if r := viewer.get("/v1/files/"+ref, purpose); code(r) != "sync.consent_missing" {
		t.Fatalf("chưa đồng ý: %d %s", r.status, r.raw)
	}
	if r := viewer.get("/v1/files/"+ids.New().String(), purpose); code(r) != "sync.file_not_found" {
		t.Fatalf("tệp lạ: %d %s", r.status, r.raw)
	}
}

// RB-2: tenant khác không thấy thiết bị, tệp, xung đột của tenant này.
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	a := f.officer()
	a.register()
	s := a.sealFor(unitA, []byte("ảnh"))
	up := f.upload(a, s, nil)
	if up.status != http.StatusCreated {
		t.Fatalf("tải: %d %s", up.status, up.raw)
	}
	ref := up.body["file_ref"].(string)
	results(t, a.post("/v1/sync/push", map[string]any{"device_id": a.device.String(), "records": []any{
		rec(a, 1, "plot_boundary", map[string]any{"behave": "conflict", "conflict_key": "x"})}}))

	otherTenant := ids.New()
	org := ids.New()
	f.scope.set(org, authz.Scope{Units: []string{unitA}})
	b := f.as(otherTenant, org, client.RoleEvidenceViewer, port.RoleConflictResolver, port.RoleDeviceAdmin)
	b.actor = a.actor // cùng sub nhưng khác tenant
	b.token = f.iss.Sign(t, authztest.Token{Subject: a.actor, TenantID: otherTenant, Roles: b.roles, OrgRef: org, MFA: true})
	b.device = a.device
	if _, r := b.pull("", 0); code(r) != "sync.device_unregistered" {
		t.Fatalf("thiết bị của tenant khác: %d %s", r.status, r.raw)
	}
	if r := b.adminPost("/v1/devices/"+a.device.String()+"/revoke", map[string]any{"reason": "thử thu hồi chéo tenant"}); code(r) != "sync.device_not_found" {
		t.Fatalf("thu hồi chéo tenant: %d %s", r.status, r.raw)
	}
	if r := b.get("/v1/files/"+ref, map[string]string{port.HeaderPurpose: string(client.PurposeMRVEvidence)}); code(r) != "sync.file_not_found" {
		t.Fatalf("tệp của tenant khác: %d %s", r.status, r.raw)
	}
	if lst := b.adminGet("/v1/sync/conflicts"); len(lst.body["items"].([]any)) != 0 {
		t.Fatalf("xung đột của tenant khác: %s", lst.raw)
	}
	if sla, _ := f.mod.ConflictSLA(context.Background(), otherTenant); sla.Open != 0 {
		t.Fatalf("SLA của tenant khác: %+v", sla)
	}
	// Tenant gốc vẫn thấy.
	if sla, _ := f.mod.ConflictSLA(context.Background(), f.tenant); sla.Open != 1 {
		t.Fatalf("SLA tenant gốc: %+v", sla)
	}
}
