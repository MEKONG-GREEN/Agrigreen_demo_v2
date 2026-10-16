package handler_test

// W11.43 (D4 CN3, BRD CNTS v3.7 FN-13, UR-TINH-08, UR-TINH-13, Mục III.5): quy tắc liên biểu + cảnh báo trên PostgreSQL
// thật qua module pf/extension (vai extension_app): consumer pf.extension.cnts_alert (bài nộp kn_cnts, ổ dịch mới),
// chống trùng, tự đóng, vòng đời qua HTTP (giao, nhận, đóng, khóa lạc quan, phạm vi Xã), job quá hạn / nhắc nộp / trễ
// liên tiếp, cấu hình nhắc. Bài nộp cap/forms, danh bạ cán bộ, đơn vị hành chính là bản giả.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const cnts = "chan_nuoi_thuy_san"

// 10:00 thứ Tư 13/10/2027 giờ Việt Nam.
var t0 = time.Date(2027, 10, 13, 3, 0, 0, 0, time.UTC)

type units struct{}

func (units) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	switch code {
	case "30001", "30002":
		return partyport.AdminUnit{Code: code, Level: partyport.LevelCommune, ParentCode: "89"}, nil
	}
	return partyport.AdminUnit{}, partyport.ErrUnitNotFound
}
func (units) Descendants(context.Context, string, time.Time) ([]string, error) { return nil, nil }
func (units) Current(context.Context, string, time.Time) ([]string, error)     { return nil, nil }

// feed giả cap/forms.
type feed struct {
	mu   sync.Mutex
	subs []formsport.RecordedSubmission
	due  []formsport.PeriodProgress
}

func (f *feed) Recorded(_ context.Context, _, id uuid.UUID) (*formsport.RecordedSubmission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.subs {
		if s.ID == id {
			s := s
			return &s, nil
		}
	}
	return nil, nil
}

func (f *feed) Latest(_ context.Context, _ uuid.UUID, q formsport.SubmissionQuery) ([]formsport.RecordedSubmission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	last := map[string]formsport.RecordedSubmission{}
	for _, s := range f.subs {
		if s.TemplateCode != q.TemplateCode || s.From == nil {
			continue
		}
		if (len(q.Units) > 0 || len(q.Parents) > 0) && !slices.Contains(q.Units, s.Unit) && !slices.Contains(q.Parents, s.Parent) {
			continue
		}
		if (q.From != nil && s.From.Before(*q.From)) || (q.To != nil && s.From.After(*q.To)) {
			continue
		}
		k := s.PeriodID.String() + s.Unit
		if cur, ok := last[k]; !ok || s.Version > cur.Version {
			last[k] = s
		}
	}
	out := make([]formsport.RecordedSubmission, 0, len(last))
	for _, s := range last {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].From.After(*out[j].From) })
	return out, nil
}

func (f *feed) DuePeriods(_ context.Context, _ uuid.UUID, _ string, from, to time.Time) ([]formsport.PeriodProgress, error) {
	var out []formsport.PeriodProgress
	for _, p := range f.due {
		if !p.DueAt.Before(from) && !p.DueAt.After(to) {
			out = append(out, p)
		}
	}
	return out, nil
}

// dir giả pf/accounts: cán bộ xã 30001 (xa1), Cấp Tỉnh (tinh — người phụ trách biểu), Lãnh đạo phòng (lead).
type dir struct{ xa1, tinh, lead uuid.UUID }

func (d dir) Officers(_ context.Context, _ uuid.UUID, dept, province string) ([]accport.KNOfficer, error) {
	if dept != cnts || province != "89" {
		return nil, nil
	}
	return []accport.KNOfficer{
		{ActorRef: d.xa1, Level: accport.ScheduleLevelCommune, ProvinceCode: "89", Communes: []string{"30001"}},
		{ActorRef: d.tinh, Level: accport.ScheduleLevelProvince, ProvinceCode: "89", MgmtLevel: accport.MgmtChuyenVien},
		{ActorRef: d.lead, Level: accport.ScheduleLevelProvince, ProvinceCode: "89", MgmtLevel: accport.MgmtTruongPhong},
	}, nil
}
func (d dir) RoleHolders(context.Context, uuid.UUID, string, string) ([]uuid.UUID, error) {
	return nil, nil
}
func (d dir) DutyHolders(_ context.Context, _ uuid.UUID, role, dept, duty string) ([]uuid.UUID, error) {
	if role == authz.KNTinh && dept == cnts && duty == authz.DutyFormOwner {
		return []uuid.UUID{d.tinh}, nil
	}
	return nil, nil
}

type fix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	pool   *pgxpool.Pool
	mod    *extension.Module
	feed   *feed
	dir    dir
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
	// process chạy consumer pf.extension.cnts_alert với một phong bì (inbox thật, vai extension_app).
	process func(outbox.Envelope)
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{},
		feed: &feed{}, dir: dir{xa1: ids.New(), tinh: ids.New(), lead: ids.New()}}
	f.pool = env.AdminPool(t, migrations.PF)
	app := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	f.mod = extension.New(app, clock.NewFixed(t0), extension.Deps{Units: units{}})
	f.mod.UseCNTSAlerts(extension.CNTSAlertDeps{Feed: f.feed, Directory: f.dir, Holders: f.dir})
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	f.mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	f.process = func(env outbox.Envelope) {
		t.Helper()
		if _, err := inbox.Process(context.Background(), db.ForModule(app, extension.DBModule), port.CNTSAlertConsumer, env, f.mod.CNTSAlertHandler()); err != nil {
			t.Fatalf("consumer: %v", err)
		}
	}
	return f
}

func (f *fix) knMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.GetHeader(authz.ContextHeader))
		p, ok := authz.FromContext(c.Request.Context())
		f.mu.Lock()
		kc := f.ctxs[id]
		f.mu.Unlock()
		if !ok || err != nil || kc == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		cp := *kc
		p.KN = &cp
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
}

type user struct {
	f     *fix
	token string
	ctxID uuid.UUID
}

func (f *fix) user(actor uuid.UUID, kc authz.KNContext) user {
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: f.tenant, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
	kc.ID, kc.OrgRef = ids.New(), ids.New()
	f.mu.Lock()
	f.ctxs[kc.ID] = &kc
	f.mu.Unlock()
	return user{f: f, token: tok, ctxID: kc.ID}
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
}

func (u user) do(method, path, idem string, body any) resp {
	u.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, u.f.srv.URL+path, rd)
	r.Header.Set("Authorization", "Bearer "+u.token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(authz.ContextHeader, u.ctxID.String())
	if idem != "" {
		r.Header.Set("Idempotency-Key", idem)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		u.f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&m)
	return resp{status: res.StatusCode, raw: raw, body: m}
}

func key() string                                { return "k-" + ids.New().String() }
func (u user) post(p string, b any) resp         { return u.do(http.MethodPost, p, key(), b) }
func (u user) put(p string, b any) resp          { return u.do(http.MethodPut, p, key(), b) }
func (u user) get(p string) resp                 { return u.do(http.MethodGet, p, "", nil) }
func str(m map[string]any, k string) string      { s, _ := m[k].(string); return s }
func num(m map[string]any, k string) json.Number { n, _ := m[k].(json.Number); return n }
func list(m map[string]any, k string) []any      { l, _ := m[k].([]any); return l }

func want(t *testing.T, r resp, status int, code, what string) map[string]any {
	t.Helper()
	if r.status != status || (code != "" && str(r.body, "code") != code) {
		t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, code, r.status, r.raw)
	}
	return r.body
}

func (f *fix) outbox(eventType string) []map[string]any {
	f.t.Helper()
	var out []map[string]any
	rows, err := f.pool.Query(context.Background(), `SELECT payload FROM shared.outbox WHERE event_type = $1 AND tenant_id = $2 ORDER BY id`, eventType, f.tenant)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var m map[string]any
		if err := rows.Scan(&m); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func day(s string) *time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return &t
}

// submit ghi một bài nộp giả và chạy consumer với sự kiện pf.forms.submission.recorded.
func (f *fix) submit(code, unit, parent, kind string, period uuid.UUID, key, from, to string, version int64, values map[string]string, nilReport bool,
	submitter uuid.UUID) formsport.RecordedSubmission {
	s := formsport.RecordedSubmission{ID: ids.New(), PeriodID: period, Profile: "kn_cnts", TemplateCode: code, PeriodLabel: key, PeriodKey: key,
		From: day(from), To: day(to), DueAt: t0, UnitKind: kind, Unit: unit, Parent: parent, Version: version, Values: values, NilReport: nilReport,
		Submitter: submitter, EventTime: t0}
	f.feed.mu.Lock()
	f.feed.subs = append(f.feed.subs, s)
	f.feed.mu.Unlock()
	pl, _ := json.Marshal(formsport.SubmissionRecordedV1{SubmissionID: s.ID.String(), PeriodRef: period.String(), Profile: "kn_cnts", TemplateCode: code,
		Unit: unit, Version: version, ActorRef: submitter.String(), EventTime: t0.Format(time.RFC3339)})
	f.process(outbox.Envelope{EventID: ids.New(), EventType: formsport.EventSubmissionRecorded, SchemaVersion: 1, TenantID: f.tenant, OccurredAt: t0,
		Producer: "pf/forms", Payload: pl})
	return s
}

func herd(heo, ga string) map[string]string {
	return map[string]string{"tong_dan_heo": heo, "tong_dan_de": "0", "tong_dan_trau": "10", "tong_dan_bo": "20", "tong_dan_vit": "100",
		"tong_dan_ga": ga, "tong_dan_cho_meo": "50", "tong_dan_yen": "3", "ksgm_gia_suc": "12", "ksgm_gia_cam": "40"}
}

func TestCNTSAlerts_W1143(t *testing.T) {
	f := newFix(t)
	ctx := context.Background()
	tinh := f.user(f.dir.tinh, authz.KNContext{Role: authz.KNTinh, Department: cnts, Scope: authz.Scope{Units: []string{"89", "30001", "30002"}}})
	lead := f.user(f.dir.lead, authz.KNContext{Role: authz.KNTinh, Department: cnts, Manager: true, Scope: authz.Scope{Units: []string{"89", "30001", "30002"}}})
	xa1 := f.user(f.dir.xa1, authz.KNContext{Role: authz.KNXa, Department: cnts, Communes: []string{"30001"}, Scope: authz.Scope{Units: []string{"30001"}}})
	xa2 := f.user(ids.New(), authz.KNContext{Role: authz.KNXa, Department: cnts, Communes: []string{"30002"}, Scope: authz.Scope{Units: []string{"30002"}}})
	tt := f.user(ids.New(), authz.KNContext{Role: authz.KNTinh, Department: "trong_trot"})

	// Tuần BM-04 (lịch CNTS Thứ Ba – Thứ Hai): W40 28/09–04/10, W41 05/10–11/10.
	w40, w41 := ids.New(), ids.New()
	f.submit("CNTS-BM04", "30001", "30001", "commune", w40, "2027-W40", "2027-09-28", "2027-10-04", 1, herd("100", "0"), false, f.dir.xa1)
	if n := len(f.outbox(port.EventCNTSAlertChanged)); n != 0 {
		t.Fatalf("kỳ đầu (không có kỳ gần nhất) không cảnh báo, có %d", n)
	}
	// Dòng 1: heo +20 % (> 15 %), gà 0 → 30 "Phát sinh mới" → một cảnh báo Cần chú ý tới người phụ trách biểu.
	f.submit("CNTS-BM04", "30001", "30001", "commune", w41, "2027-W41", "2027-10-05", "2027-10-11", 1, herd("120", "30"), false, f.dir.xa1)
	evs := f.outbox(port.EventCNTSAlertChanged)
	if len(evs) != 1 || evs[0]["kind"] != "herd_change" || evs[0]["level"] != "attention" || evs[0]["action"] != "raised" {
		t.Fatalf("muốn 1 cảnh báo tổng đàn: %v", evs)
	}
	if rs, _ := evs[0]["recipient_refs"].([]any); len(rs) != 1 || rs[0] != f.dir.tinh.String() {
		t.Fatalf("AC-05: người nhận = người phụ trách biểu: %v", evs[0]["recipient_refs"])
	}
	// Gửi lại cùng kỳ vẫn vi phạm → không tạo cảnh báo thứ hai (BR-04, AC-02).
	f.submit("CNTS-BM04", "30001", "30001", "commune", w41, "2027-W41", "2027-10-05", "2027-10-11", 2, herd("121", "30"), false, f.dir.xa1)
	if n := len(f.outbox(port.EventCNTSAlertChanged)); n != 1 {
		t.Fatalf("không tạo trùng, có %d", n)
	}
	l := want(t, tinh.get("/v1/extension/cnts/alerts?status=open"), 200, "", "Tỉnh xem danh sách")
	items := list(l, "items")
	if len(items) != 1 {
		t.Fatalf("muốn 1 cảnh báo mở: %s", l)
	}
	herdAlert := items[0].(map[string]any)
	ref := str(herdAlert, "alert_ref")
	if str(herdAlert, "status") != "new" || str(herdAlert, "commune_code") != "30001" || herdAlert["can_assign"] != true {
		t.Fatalf("cảnh báo: %v", herdAlert)
	}
	// Phạm vi: Xã 30001 thấy, Xã 30002 không, phòng khác bị chặn.
	if n := len(list(want(t, xa1.get("/v1/extension/cnts/alerts"), 200, "", "xa1"), "items")); n != 1 {
		t.Fatalf("Xã phụ trách thấy cảnh báo của xã: %d", n)
	}
	if n := len(list(want(t, xa2.get("/v1/extension/cnts/alerts"), 200, "", "xa2"), "items")); n != 0 {
		t.Fatalf("Xã khác không thấy: %d", n)
	}
	want(t, xa2.get("/v1/extension/cnts/alerts/"+ref), 404, "extension.cnts_alert_not_found", "xa2 chi tiết")
	want(t, tt.get("/v1/extension/cnts/alerts"), 403, "", "phòng Trồng trọt")

	// Vòng đời: Xã không giao được; Tỉnh giao cho xa1; khóa lạc quan; xa1 nhận xử lý; đóng phải có kết quả.
	want(t, xa1.post("/v1/extension/cnts/alerts/"+ref+"/assign", map[string]any{"assignee_ref": f.dir.xa1.String()}), 403, "", "Xã giao")
	want(t, tinh.post("/v1/extension/cnts/alerts/"+ref+"/assign", map[string]any{"assignee_ref": ids.New().String()}), 422,
		"extension.cnts_alert_invalid", "giao người ngoài phòng")
	want(t, xa1.post("/v1/extension/cnts/alerts/"+ref+"/start", map[string]any{}), 403, "forbidden", "nhận khi chưa được giao")
	d := want(t, tinh.post("/v1/extension/cnts/alerts/"+ref+"/assign", map[string]any{"assignee_ref": f.dir.xa1.String(), "base_seq": 1,
		"note": "Xác minh lại số heo"}), 200, "", "giao")
	if str(d, "status") != "assigned" || str(d["assignee"].(map[string]any), "ref") != f.dir.xa1.String() {
		t.Fatalf("đã giao: %v", d)
	}
	if evs := f.outbox(port.EventCNTSAlertChanged); len(evs) != 2 || evs[1]["action"] != "assigned" {
		t.Fatalf("thông báo người được giao: %v", evs)
	}
	want(t, tinh.post("/v1/extension/cnts/alerts/"+ref+"/assign", map[string]any{"assignee_ref": f.dir.xa1.String(), "base_seq": 1}), 409,
		"extension.cnts_alert_version_conflict", "khóa lạc quan")
	want(t, xa2.post("/v1/extension/cnts/alerts/"+ref+"/start", map[string]any{}), 404, "", "Xã khác nhận")
	want(t, xa1.post("/v1/extension/cnts/alerts/"+ref+"/start", map[string]any{}), 200, "", "nhận xử lý")
	want(t, xa1.post("/v1/extension/cnts/alerts/"+ref+"/close", map[string]any{"result": "ngắn"}), 422, "extension.cnts_alert_invalid", "kết quả ngắn")
	d = want(t, xa1.post("/v1/extension/cnts/alerts/"+ref+"/close", map[string]any{"result": "Đã xác minh: đàn heo tăng do nhập giống mới"}), 200, "", "đóng")
	if str(d, "status") != "closed" || len(list(d, "history")) != 4 {
		t.Fatalf("đóng + lịch sử raised/assigned/started/closed: %v", d)
	}
	want(t, tinh.post("/v1/extension/cnts/alerts/"+ref+"/close", map[string]any{"result": "Đóng lại lần nữa cho chắc"}), 409,
		"extension.cnts_alert_transition", "đóng lần hai")

	// Quy tắc liên biểu số 1 / dòng 4: BM-01 hai cơ sở của xã 30001 trong W41 (tổng gia súc 12, gia cầm 40) khớp CN-06.
	fac1, fac2 := ids.New().String(), ids.New().String()
	d1, d2 := ids.New(), ids.New()
	f.submit("CNTS-BM01", fac1, "30001", "facility", d1, "2027-10-06", "2027-10-06", "2027-10-06", 1,
		map[string]string{"trau_bo": "1", "heo": "5", "gia_cam": "20", "de": "0"}, false, f.dir.xa1)
	f.submit("CNTS-BM01", fac2, "30001", "facility", d2, "2027-10-07", "2027-10-07", "2027-10-07", 1,
		map[string]string{"trau_bo": "0", "heo": "6", "gia_cam": "20", "de": "0"}, false, f.dir.xa1)
	if n := countKind(f, "ksgm_mismatch"); n != 0 {
		t.Fatalf("khớp → không cảnh báo lệch KSGM (%d)", n)
	}
	// BM-01 đổi sau khi xã gửi BM-04 → lệch → cảnh báo tới cán bộ xã + người phụ trách biểu; sửa lại → tự đóng.
	f.submit("CNTS-BM01", fac2, "30001", "facility", d2, "2027-10-07", "2027-10-07", "2027-10-07", 2,
		map[string]string{"trau_bo": "0", "heo": "9", "gia_cam": "20", "de": "0"}, false, f.dir.xa1)
	mis := lastKind(f, "ksgm_mismatch")
	if mis == nil {
		t.Fatal("BM-01 đổi → cảnh báo lệch KSGM")
	}
	if rs, _ := mis["recipient_refs"].([]any); len(rs) != 2 {
		t.Fatalf("người nhận: cán bộ xã + người phụ trách biểu: %v", mis["recipient_refs"])
	}
	f.submit("CNTS-BM01", fac2, "30001", "facility", d2, "2027-10-07", "2027-10-07", "2027-10-07", 3,
		map[string]string{"trau_bo": "0", "heo": "6", "gia_cam": "20", "de": "0"}, false, f.dir.xa1)
	closed := want(t, tinh.get("/v1/extension/cnts/alerts?kind=ksgm_mismatch"), 200, "", "lệch KSGM")
	if a := list(closed, "items")[0].(map[string]any); str(a, "status") != "closed" || a["auto_closed"] != true {
		t.Fatalf("số liệu sửa hết vi phạm → tự đóng: %v", a)
	}

	// Dòng 3: KSGM = 0 khi cơ sở hoạt động → người nhập; "không hoạt động" thì không.
	f.submit("CNTS-BM01", fac1, "30001", "facility", ids.New(), "2027-10-08", "2027-10-08", "2027-10-08", 1,
		map[string]string{"trau_bo": "0", "heo": "0", "gia_cam": "0", "de": "0"}, true, f.dir.xa1)
	if countKind(f, "slaughter_zero") != 0 {
		t.Fatal("cơ sở không hoạt động → không cảnh báo")
	}
	f.submit("CNTS-BM01", fac1, "30001", "facility", ids.New(), "2027-10-09", "2027-10-09", "2027-10-09", 1,
		map[string]string{"trau_bo": "0", "heo": "0", "gia_cam": "0", "de": "0"}, false, f.dir.xa1)
	z := lastKind(f, "slaughter_zero")
	if rs, _ := z["recipient_refs"].([]any); z == nil || len(rs) != 1 || rs[0] != f.dir.xa1.String() {
		t.Fatalf("KSGM = 0 → người nhập: %v", z)
	}

	// Dòng 12: ổ dịch mới → Khẩn tới Lãnh đạo phòng + Cấp Tỉnh; sự kiện giao lại không tạo trùng.
	ob := port.OutbreakReportedV1{OutbreakRef: ids.New(), VersionID: ids.New(), CommuneCode: "30001", DiseaseCode: "dtlcp", SpeciesCode: "heo",
		Notifiable: true, ReportedAt: t0.Format(time.RFC3339), ReporterRef: f.dir.xa1, Department: cnts, HerdSize: 120, Dead: 5, AsOf: "2027-10-12"}
	pl, _ := json.Marshal(ob)
	for i := 0; i < 2; i++ {
		f.process(outbox.Envelope{EventID: ids.New(), EventType: port.EventOutbreakReported, SchemaVersion: 1, TenantID: f.tenant, OccurredAt: t0,
			Producer: "pf/extension", Payload: pl})
	}
	if countKind(f, "outbreak") != 1 {
		t.Fatal("ổ dịch: đúng một cảnh báo")
	}
	o := lastKind(f, "outbreak")
	if o["level"] != "urgent" {
		t.Fatalf("ổ dịch mới luôn Khẩn: %v", o)
	}
	if rs, _ := o["recipient_refs"].([]any); len(rs) != 2 {
		t.Fatalf("Lãnh đạo phòng + Cấp Tỉnh: %v", o["recipient_refs"])
	}

	// Job: quá hạn xử lý (Khẩn 24 giờ) → Quá hạn, báo Lãnh đạo phòng; chạy lại không báo lần hai.
	later := t0.Add(25 * time.Hour)
	r, err := f.mod.CNTSAlertJobRun(ctx, f.tenant, later)
	if err != nil || r.Overdue != 1 {
		t.Fatalf("quá hạn: %+v %v", r, err)
	}
	if r, _ := f.mod.CNTSAlertJobRun(ctx, f.tenant, later); r.Overdue != 0 {
		t.Fatalf("không báo quá hạn hai lần: %+v", r)
	}
	if ov := lastAction(f, "overdue"); ov == nil || ov["kind"] != "outbreak" {
		t.Fatalf("sự kiện quá hạn: %v", ov)
	}
	_ = lead

	// Cấu hình nhắc nhở: mặc định BRD; Tỉnh lưu phiên bản mới; Xã không sửa; giá trị sai → 422.
	cfg := want(t, xa1.get("/v1/extension/cnts/alert-config"), 200, "", "Xã đọc cấu hình")
	if len(list(cfg, "forms")) != 5 || len(list(cfg, "kinds")) != 13 {
		t.Fatalf("5 biểu, 13 loại: %v", cfg)
	}
	want(t, xa1.put("/v1/extension/cnts/alert-config/forms/CNTS-BM04", map[string]any{"before_due_hours": 24}), 403, "", "Xã sửa")
	want(t, tinh.put("/v1/extension/cnts/alert-config/forms/CNTS-BM04", map[string]any{"before_due_hours": 24, "repeat_daily": true,
		"repeat_at": "25:00", "channels": []string{"sms"}}), 422, "extension.cnts_alert_config_invalid", "cấu hình sai")
	cfg = want(t, tinh.put("/v1/extension/cnts/alert-config/forms/CNTS-BM04", map[string]any{"before_due_hours": 48, "on_overdue": true,
		"repeat_daily": true, "repeat_at": "08:00", "late_streak": 2, "channels": []string{"in_app", "email"}, "message": "Nộp {bieu} kỳ {ky} trước {han}"}),
		200, "", "lưu cấu hình")
	bm04 := list(cfg, "forms")[1].(map[string]any)
	if bm04["custom"] != true || num(bm04["reminder"].(map[string]any), "before_due_hours").String() != "48" {
		t.Fatalf("cấu hình BM-04: %v", bm04)
	}
	want(t, tinh.put("/v1/extension/cnts/alert-config/kinds/herd_change", map[string]any{"sla_hours": 48, "urgent": true}), 422,
		"extension.cnts_alert_config_invalid", "mức cố định")
	want(t, tinh.put("/v1/extension/cnts/alert-config/kinds/water_quality", map[string]any{"sla_hours": 12, "urgent": true}), 200, "", "môi trường nước Khẩn")

	// Nhắc nộp: kỳ W42 hạn 12:00 19/10 (giờ VN), xã 30002 chưa gửi; nhắc trước hạn 48 giờ, khi trễ, nhắc lặp 08:00.
	due := time.Date(2027, 10, 19, 5, 0, 0, 0, time.UTC)
	w42 := ids.New()
	f.feed.due = []formsport.PeriodProgress{{ID: w42, Profile: "kn_cnts", Label: "Tuần 42", TemplateCode: "CNTS-BM04", DueAt: due, Status: "open",
		PeriodKey: "2027-W42", Units: []formsport.UnitProgress{{Unit: "30001", Parent: "30001", Status: "not_submitted"},
			{Unit: "30002", Parent: "30002", Status: "submitted", Versions: 1}}}}
	r, _ = f.mod.CNTSAlertJobRun(ctx, f.tenant, due.Add(-47*time.Hour))
	r2, _ := f.mod.CNTSAlertJobRun(ctx, f.tenant, due.Add(-46*time.Hour))
	if r.Reminders != 1 || r2.Reminders != 0 {
		t.Fatalf("nhắc trước hạn đúng một lần: %+v %+v", r, r2)
	}
	rem := f.outbox(port.EventCNTSFormReminded)
	if len(rem) != 1 || rem[0]["slot"] != "before" || rem[0]["message"] != "Nộp BM-04 Nhập liệu tuần chăn nuôi kỳ Tuần 42 trước 12:00 19/10/2027" {
		t.Fatalf("nhắc trước hạn: %v", rem)
	}
	if rs, _ := rem[0]["recipient_refs"].([]any); len(rs) != 1 || rs[0] != f.dir.xa1.String() {
		t.Fatalf("nhắc cán bộ xã phụ trách: %v", rem[0]["recipient_refs"])
	}
	if r, _ := f.mod.CNTSAlertJobRun(ctx, f.tenant, due.Add(time.Minute)); r.Reminders != 1 {
		t.Fatalf("nhắc khi trễ: %+v", r)
	}
	// 08:30 giờ VN ngày 20/10 → nhắc lặp.
	if r, _ := f.mod.CNTSAlertJobRun(ctx, f.tenant, time.Date(2027, 10, 20, 1, 30, 0, 0, time.UTC)); r.Reminders != 1 {
		t.Fatalf("nhắc lặp: %+v", r)
	}

	// Trễ liên tiếp (AC-06): W42 chưa gửi + W41 gửi sau hạn → 2 kỳ → người phụ trách biểu + Lãnh đạo phòng.
	f.feed.due = append(f.feed.due, formsport.PeriodProgress{ID: ids.New(), Profile: "kn_cnts", Label: "Tuần 41", TemplateCode: "CNTS-BM04",
		DueAt: due.AddDate(0, 0, -7), Status: "closed", PeriodKey: "2027-W41", Units: []formsport.UnitProgress{{Unit: "30001", Parent: "30001",
			Status: "submitted", Versions: 1, Late: true}}})
	r, _ = f.mod.CNTSAlertJobRun(ctx, f.tenant, time.Date(2027, 10, 20, 2, 0, 0, 0, time.UTC))
	if r.Streaks != 1 {
		t.Fatalf("trễ 2 kỳ liên tiếp → cảnh báo: %+v", r)
	}
	ls := lastKind(f, "late_streak")
	if rs, _ := ls["recipient_refs"].([]any); len(rs) != 2 {
		t.Fatalf("người phụ trách biểu + Lãnh đạo phòng: %v", ls)
	}
	if r, _ := f.mod.CNTSAlertJobRun(ctx, f.tenant, time.Date(2027, 10, 20, 2, 5, 0, 0, time.UTC)); r.Streaks != 0 {
		t.Fatal("trễ liên tiếp không tạo trùng")
	}
	cfg = want(t, tinh.get("/v1/extension/cnts/alert-config"), 200, "", "đọc cấu hình")
	_ = cfg
}

func countKind(f *fix, kind string) int {
	n := 0
	for _, e := range f.outbox(port.EventCNTSAlertChanged) {
		if e["kind"] == kind && e["action"] == "raised" {
			n++
		}
	}
	return n
}

func lastKind(f *fix, kind string) map[string]any {
	var out map[string]any
	for _, e := range f.outbox(port.EventCNTSAlertChanged) {
		if e["kind"] == kind && e["action"] == "raised" {
			out = e
		}
	}
	return out
}

func lastAction(f *fix, action string) map[string]any {
	var out map[string]any
	for _, e := range f.outbox(port.EventCNTSAlertChanged) {
		if e["action"] == action {
			out = e
		}
	}
	return out
}
