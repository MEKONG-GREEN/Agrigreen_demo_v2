package handler_test

// W11.48 (D4 CN8, BRD CNTS v3.7 FN-10, FN-13, UR-XA-11, UR-XA-12, UR-TINH-12, Mục III.5): nhật ký nuôi BM-08/09/19 trên
// PostgreSQL thật qua module pf/extension (vai extension_app): lứa, lần ghi có chỉ số, quy tắc chặn, vị trí (khoảng cách
// tới điểm của hộ, giả lập, tọa độ ngoài sổ), ảnh mã hóa phong bì bằng khóa chủ hộ, rà soát/xác thực (người xác thực khác
// người ghi), điều chỉnh sau xác thực, hạch toán NC-15, cảnh báo dòng 6–9. Hộ mô hình dựng qua API W11.47. Ngữ cảnh phiên
// KN giả lập bằng middleware X-AG-Context; danh mục, đơn vị hành chính, danh bạ Cấp Xã, kho định danh, kho tệp là bản giả.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
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
		return partyport.AdminUnit{Code: code, Name: "Xã " + code, Level: partyport.LevelCommune, ParentCode: "89"}, nil
	case "89":
		return partyport.AdminUnit{Code: code, Name: "Tỉnh", Level: partyport.LevelProvince}, nil
	}
	return partyport.AdminUnit{}, partyport.ErrUnitNotFound
}
func (units) Descendants(context.Context, string, time.Time) ([]string, error) { return nil, nil }
func (units) Current(context.Context, string, time.Time) ([]string, error)     { return nil, nil }

type catalogs struct{ knport.CNTSCatalogs }

var items = map[string]mdport.CatalogItem{
	"cnts_model_type:GA_TP": {Catalog: "cnts_model_type", Code: "GA_TP", Name: "Gà thương phẩm", Active: true},
	"cnts_model_type:CA":    {Catalog: "cnts_model_type", Code: "CA", Name: "Cá", Active: true},
}

func (catalogs) ItemAt(_ context.Context, catalog, code string, _ time.Time) (mdport.CatalogItem, error) {
	if it, ok := items[catalog+":"+code]; ok {
		return it, nil
	}
	return mdport.CatalogItem{}, fmt.Errorf("giả: %w", mdport.ErrCatalogItemNotFound)
}

func (catalogs) ListAt(_ context.Context, catalog string, _ time.Time) ([]mdport.CatalogItem, error) {
	out := []mdport.CatalogItem{}
	for _, it := range items {
		if it.Catalog == catalog {
			out = append(out, it)
		}
	}
	return out, nil
}

type xaScopes struct{ list []accport.XaScope }

func (x *xaScopes) XaScopes(context.Context, uuid.UUID, string) ([]accport.XaScope, error) {
	return x.list, nil
}

// files là pf/evidence giả: giữ BẢN MÃ, gọi Resolver/FileAuthorizer của module chủ, mở DEK qua kho định danh như thật.
type files struct {
	mu       sync.Mutex
	resolver evport.Resolver
	vault    client.Client
	byRef    map[uuid.UUID]evport.File
	blobs    map[uuid.UUID][]byte
	purposes []string
	attaches int // số lần Attach thật (phát lại Idempotency-Key không được gọi lại — #172)
}

func (f *files) Attach(ctx context.Context, in evport.AttachInput) (evport.File, bool, error) {
	if in.Subject == nil {
		return evport.File{}, false, errors.New("thiếu chủ thể")
	}
	if _, err := f.resolver.Resolve(ctx, in.Subject.Type, in.Subject.ID); err != nil {
		return evport.File{}, false, err
	}
	ref, _ := uuid.Parse(*in.FileRef)
	subj, _ := uuid.Parse(in.Meta.SubjectRef)
	if envelope.SHA256Hex(in.Blob) != in.Meta.SHA256Ciphertext {
		return evport.File{}, false, errors.New("băm bản mã sai")
	}
	file := evport.File{Ref: ref, SubjectRef: subj, Nonce: in.Meta.Nonce, WrappedDEK: in.Meta.WrappedDEK, ContentType: in.Meta.ContentType,
		Subject: in.Subject, Size: int64(len(in.Blob))}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byRef[ref], f.blobs[ref] = file, append([]byte(nil), in.Blob...)
	f.attaches++
	return file, true, nil
}

func (f *files) Open(context.Context, uuid.UUID, string) (evport.Opened, error) {
	return evport.Opened{}, errors.New("không dùng")
}

func (f *files) OpenContent(ctx context.Context, ref uuid.UUID, purpose string) (string, []byte, error) {
	f.mu.Lock()
	file, ok := f.byRef[ref]
	blob := f.blobs[ref]
	f.purposes = append(f.purposes, purpose)
	f.mu.Unlock()
	if !ok {
		return "", nil, errors.New("không có tệp")
	}
	if allowed, err := f.resolver.(evport.FileAuthorizer).AuthorizeFile(ctx, file); err != nil || !allowed {
		return "", nil, fmt.Errorf("từ chối: %w", err)
	}
	dek, err := f.vault.Unwrap(ctx, client.Purpose(purpose), file.SubjectRef, file.WrappedDEK)
	if err != nil {
		return "", nil, err
	}
	pt, err := envelope.DecryptWithDEK(dek, file.Nonce, blob)
	return file.ContentType, pt, err
}

type fix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	pool   *pgxpool.Pool
	clk    *clock.Fixed
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
	xa     *xaScopes
	vault  *clienttest.Fake
	files  *files
}

func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}, xa: &xaScopes{}, vault: clienttest.New(),
		clk: clock.NewFixed(t0)}
	f.pool = env.AdminPool(t, migrations.PF)
	mod := extension.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), f.clk, extension.Deps{Units: units{}})
	mod.UseCNTSCatalogs(catalogs{})
	mod.UseXaScopes(f.xa)
	mod.UseCNTSModelVault(f.vault)
	f.files = &files{resolver: mod.DiaryFileResolver(), vault: f.vault, byRef: map[uuid.UUID]evport.File{}, blobs: map[uuid.UUID][]byte{}}
	mod.UseCNTSDiary(f.vault, f.files)
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fix) knMiddleware() gin.HandlerFunc {
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		f.t.Fatal(err)
	}
	return func(c *gin.Context) {
		p, ok := authz.FromContext(c.Request.Context())
		id, err := uuid.Parse(c.GetHeader(authz.ContextHeader))
		f.mu.Lock()
		kc := f.ctxs[id]
		f.mu.Unlock()
		if !ok || err != nil || kc == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		cp := *kc
		p.KN = &cp
		selected, err := registry.ResolveSelected(c.Request.Context(), p)
		if err != nil {
			f.t.Errorf("owner session projection: %v", err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		p.SelectedSession = &selected
		scope, _ := json.Marshal(cp)
		ctx := idempotency.WithAuthorizationScope(c.Request.Context(), scope)
		c.Request = c.Request.WithContext(authz.WithPrincipal(ctx, p))
		c.Next()
	}
}

type user struct {
	f     *fix
	token string
	ctxID uuid.UUID
	ref   uuid.UUID
}

func (f *fix) user(kc authz.KNContext) user {
	sub := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: sub, TenantID: f.tenant, Roles: []string{"kn_user", client.RoleRegistrar, client.RoleDisplay,
		client.RoleEvidenceViewer}, MFA: true, TTL: 2 * time.Hour})
	kc.ID, kc.OrgRef = ids.New(), ids.New()
	f.mu.Lock()
	f.ctxs[kc.ID] = &kc
	f.mu.Unlock()
	return user{f: f, token: tok, ctxID: kc.ID, ref: sub}
}

func tinh(group string) authz.KNContext {
	return authz.KNContext{Role: authz.KNTinh, Department: cnts, ProvinceGroup: group, Scope: authz.Scope{Units: []string{"89", "30001", "30002"}}}
}

func xa(communes ...string) authz.KNContext {
	return authz.KNContext{Role: authz.KNXa, Department: cnts, Communes: communes, Scope: authz.Scope{Units: communes}}
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
}

func (u user) send(r *http.Request, idem string) resp {
	u.f.t.Helper()
	r.Header.Set("Authorization", "Bearer "+u.token)
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

func (u user) do(method, path, idem string, body any) resp {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, u.f.srv.URL+path, rd)
	r.Header.Set("Content-Type", "application/json")
	return u.send(r, idem)
}

func (u user) upload(household, kind string, content []byte) resp {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("household_ref", household)
	_ = w.WriteField("kind", kind)
	_ = w.WriteField("captured_at", t0.Add(-time.Hour).Format(time.RFC3339))
	fw, _ := w.CreateFormFile("file", "anh.png")
	_, _ = fw.Write(content)
	_ = w.Close()
	r, _ := http.NewRequest(http.MethodPost, u.f.srv.URL+base+"diary-photos", &buf)
	r.Header.Set("Content-Type", w.FormDataContentType())
	return u.send(r, key())
}

// photoBody dựng thân multipart TẤT ĐỊNH như web (#172: boundary cố định, id + captured_at do client giữ khi bấm lại).
func photoBody(household, kind, id string, content []byte) ([]byte, string) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.SetBoundary("----AgFormBoundary172photo")
	_ = w.WriteField("household_ref", household)
	_ = w.WriteField("kind", kind)
	_ = w.WriteField("id", id)
	_ = w.WriteField("captured_at", t0.Add(-time.Hour).Format(time.RFC3339))
	fw, _ := w.CreateFormFile("file", "anh.jpg")
	_, _ = fw.Write(content)
	_ = w.Close()
	return buf.Bytes(), w.FormDataContentType()
}

func (u user) uploadRaw(k, ctype string, body []byte) resp {
	r, _ := http.NewRequest(http.MethodPost, u.f.srv.URL+base+"diary-photos", bytes.NewReader(body))
	r.Header.Set("Content-Type", ctype)
	return u.send(r, k)
}

func key() string                            { return "k-" + ids.New().String() }
func (u user) post(p string, b any) resp     { return u.do(http.MethodPost, p, key(), b) }
func (u user) postK(p, k string, b any) resp { return u.do(http.MethodPost, p, k, b) }
func (u user) get(p string) resp             { return u.do(http.MethodGet, p, "", nil) }
func str(m map[string]any, k string) string  { s, _ := m[k].(string); return s }
func num(m map[string]any, k string) string  { n, _ := m[k].(json.Number); return n.String() }
func list(m map[string]any, k string) []any  { l, _ := m[k].([]any); return l }
func obj(m map[string]any, k string) map[string]any {
	o, _ := m[k].(map[string]any)
	return o
}

func merge(a map[string]any, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func want(t *testing.T, r resp, status int, code, what string) map[string]any {
	t.Helper()
	if r.status != status || (code != "" && str(r.body, "code") != code) {
		t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, code, r.status, r.raw)
	}
	return r.body
}

func (f *fix) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func checkCodes(e map[string]any) []string {
	var out []string
	for _, c := range list(e, "checks") {
		out = append(out, str(c.(map[string]any), "code"))
	}
	return out
}

// pngBytes là một ảnh PNG 1×1 hợp lệ (đủ để đoán kiểu nội dung).
var pngBytes = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0,
	0x1f, 0x15, 0xc4, 0x89, 0, 0, 0, 0x0a, 'I', 'D', 'A', 'T', 0x78, 0x9c, 0x63, 0, 1, 0, 0, 5, 0, 1, 0x0d, 0x0a, 0x2d, 0xb4, 0, 0, 0, 0,
	'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82}

const base = "/v1/extension/cnts/"

func TestCNTSDiary_W1148(t *testing.T) {
	f := newFix(t)
	ctx := context.Background()
	chu := f.user(tinh("chu_nhiem"))
	chu2 := f.user(tinh("chu_nhiem"))
	viewer := f.user(tinh("vien_chuc"))
	officer := f.user(xa("30001"))
	other := f.user(xa("30002"))
	admin := f.user(authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}})
	f.xa.list = []accport.XaScope{{ActorRef: officer.ref, Communes: []string{"30001"}}, {ActorRef: other.ref, Communes: []string{"30002"}}}

	// ----- Hộ mô hình của KN (W11.47) -----
	p := want(t, chu.post(base+"projects", map[string]any{"code": "da-nk-2027", "name": "Mô hình gà, cá 2027", "kind": "tinh",
		"start_date": "2027-03-01"}), 201, "", "dự án")
	pref := str(p, "ref")
	site := func(name, typ, form string, lat float64) string {
		s := want(t, chu.post(base+"demo-sites", map[string]any{"project_ref": pref, "commune_code": "30001", "name": name, "model_type_code": typ,
			"officer_ref": officer.ref.String(), "diary_form": form, "location": map[string]any{"lat": lat, "lng": 105.43, "source": "map"}}),
			201, "", "điểm "+form)
		return str(s, "ref")
	}
	household := func(sref, name, phone string, lat float64, withPoint bool) string {
		body := map[string]any{"site_ref": sref, "owner_name": name, "owner_phone": phone, "keeper_kind": "officer", "keeper_ref": officer.ref.String()}
		if withPoint {
			body["location"] = map[string]any{"lat": lat, "lng": 105.431, "source": "gps"}
		}
		return str(want(t, officer.post(base+"model-households", body), 201, "", "hộ "+name), "ref")
	}
	sGa, sCa := site("Điểm gà Bình Hòa", "GA_TP", "BM-08", 10.38), site("Điểm cá Bình Hòa", "CA", "BM-19", 10.50)
	hGa := household(sGa, "Nguyễn Văn Bình", "0912345678", 10.381, true)
	hCa := household(sCa, "Trần Thị Cá", "0912000111", 10.501, true)

	// ----- Lứa (NC-02, NC-03) -----
	cyc := map[string]any{"household_ref": hGa, "breed": "Gà lai Mía", "received_date": "2027-10-01", "initial_count": 500,
		"initial_weight_g": "38.5", "age_days": 1, "supplies": []map[string]any{{"date": "2027-10-01", "kind": "Thức ăn gà con", "quantity": "200", "unit": "kg"}}}
	want(t, viewer.post(base+"diary-cycles", cyc), 403, "", "Cấp Tỉnh không mở lứa (không nhập số liệu gốc)")
	want(t, other.post(base+"diary-cycles", cyc), 404, "", "xã khác không thấy hộ")
	want(t, officer.post(base+"diary-cycles", merge(cyc, map[string]any{"form": "BM-19"})), 422, "extension.cnts_diary_cycle_invalid",
		"mẫu khác mẫu của điểm")
	want(t, officer.post(base+"diary-cycles", merge(cyc, map[string]any{"initial_count": 0})), 422, "extension.cnts_diary_cycle_invalid", "số con")
	cy := want(t, officer.post(base+"diary-cycles", cyc), 201, "", "mở lứa")
	cref := str(cy, "ref")
	if str(cy, "form") != "BM-08" || str(cy, "status") != "open" || cy["can_edit"] != true || len(list(cy, "supplies")) != 1 {
		t.Fatalf("lứa: %s", cy)
	}
	want(t, officer.post(base+"diary-cycles", cyc), 409, "extension.cnts_diary_cycle_open_exists", "một lứa đang nuôi mỗi hộ")

	// ----- Lần ghi BM-08 (UR-XA-11) -----
	near := map[string]any{"lat": 10.3811, "lng": 105.4311, "accuracy_m": "8"}
	day1 := map[string]any{"cycle_ref": cref, "entry_kind": "daily", "captured_at": t0.Add(-48 * time.Hour).Format(time.RFC3339),
		"metrics": map[string]any{"alive": "498", "dead": "2", "feed_kg": "12.5", "health": "tot"}, "location": near, "submit": true}
	b := want(t, officer.post(base+"diary-entries", day1), 422, "extension.cnts_diary_entry_blocked", "NC-06 > 0 thiếu NC-07 → chặn")
	if !bytes.Contains(r(b), []byte("NC-07")) {
		t.Fatalf("lỗi chặn: %v", b)
	}
	want(t, chu.post(base+"diary-entries", merge(day1, map[string]any{"metrics": map[string]any{"alive": "498", "dead": "2", "death_cause": "yeu",
		"feed_kg": "12.5"}})), 403, "", "chủ nhiệm không nhập số liệu gốc")
	want(t, officer.post(base+"diary-entries", merge(day1, map[string]any{"metrics": map[string]any{"alive": "498", "dead": "2",
		"death_cause": "yeu", "feed_kg": "12.5", "ph": "7"}})), 422, "extension.cnts_diary_entry_invalid", "chỉ số không có trong mẫu")
	offlineID := ids.New().String()
	k1 := key()
	d1 := merge(day1, map[string]any{"id": offlineID, "metrics": map[string]any{"alive": "498", "dead": "2", "death_cause": "yeu", "feed_kg": "12.5"}})
	e1 := want(t, officer.postK(base+"diary-entries", k1, d1), 201, "", "AC-03: lần ghi ngoại tuyến (mã máy khách, thời điểm thiết bị)")
	if str(e1, "ref") != offlineID || str(e1, "status") != "submitted" || str(e1, "entry_date") != "2027-10-11" ||
		str(e1, "captured_at") != t0.Add(-48*time.Hour).Format(time.RFC3339) {
		t.Fatalf("lần ghi 1: %s", e1)
	}
	loc := obj(e1, "location")
	if loc["far"] != false || loc["has_fix"] != true || str(loc, "distance_m") == "" || loc["mock_suspected"] != false {
		t.Fatalf("vị trí gần điểm: %v", loc)
	}
	if again := want(t, officer.postK(base+"diary-entries", k1, d1), 201, "", "gửi lại cùng khóa"); str(again, "ref") != offlineID {
		t.Fatalf("gửi lại: %s", again)
	}
	// QĐ-8: sổ không có tọa độ; tọa độ ở bảng thường, không sửa được.
	if n := f.count(`SELECT count(*) FROM information_schema.columns WHERE table_schema = 'pf_extension' AND table_name = 'cnts_diary_entry'
	    AND data_type IN ('USER-DEFINED', 'double precision')`); n != 0 {
		t.Fatalf("sổ lần ghi có cột tọa độ: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_diary_fix WHERE tenant_id = $1 AND entry_ref = $2`, f.tenant, offlineID); n != 1 {
		t.Fatalf("tọa độ lúc ghi: %d", n)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE pf_extension.cnts_diary_fix SET accuracy_m = 1 WHERE entry_ref = $1`, offlineID); err == nil {
		t.Fatal("NT-15: tọa độ lúc ghi phải không sửa được")
	}

	// Ngày 2: xuất bán 10 con, cách điểm ~2 km, thiết bị báo giả lập.
	day2 := map[string]any{"cycle_ref": cref, "entry_kind": "daily", "captured_at": t0.Add(-24 * time.Hour).Format(time.RFC3339),
		"metrics": map[string]any{"alive": "497", "dead": "1", "death_cause": "benh", "feed_kg": "13", "age_week": "2", "weight_avg_g": "180"},
		"tables": map[string]any{"sales": []map[string]any{{"date": "2027-10-12", "heads": 10, "weight_kg": "18", "unit_price": "70000"}},
			"treatments":     []map[string]any{{"date": "2027-10-10", "drug": "Enrofloxacin", "withdrawal_days": 5}},
			"feed_purchases": []map[string]any{{"date": "2027-10-12", "name": "Cám gà", "quantity_kg": "25", "unit_price": "12000"}}},
		"location": map[string]any{"lat": 10.40, "lng": 105.43, "accuracy_m": "5", "mock_location": true}, "submit": true}
	want(t, officer.post(base+"diary-entries", day2), 422, "extension.cnts_diary_entry_blocked", "NC-05 lệch số sống − chết − bán → chặn")
	day2["metrics"].(map[string]any)["alive"] = "487"
	e2 := want(t, officer.post(base+"diary-entries", day2), 201, "", "lần ghi 2")
	e2ref := str(e2, "ref")
	codes := checkCodes(e2)
	for _, c := range []string{"far_from_point", "mock_location", "withdrawal_period"} {
		if !slices.Contains(codes, c) {
			t.Fatalf("thiếu kiểm tra %s: %v", c, codes)
		}
	}
	if l := obj(e2, "location"); l["far"] != true || l["mock_location"] != true || l["mock_suspected"] != true {
		t.Fatalf("vị trí xa/giả lập: %v", l)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_alert_current WHERE tenant_id = $1 AND kind = 'log_location' AND source_ref = $2
	    AND $3 = ANY (recipient_refs)`, f.tenant, e2ref, chu.ref); n != 1 {
		t.Fatalf("cảnh báo vị trí lần ghi (Mục III.5 dòng 9) gửi chủ nhiệm: %d", n)
	}
	want(t, officer.post(base+"diary-entries", merge(day2, map[string]any{"location": near})), 409, "extension.cnts_diary_entry_day_taken",
		"một lần ghi mỗi ngày mỗi lứa")

	// ----- Rà soát, xác thực (UR-TINH-12) -----
	if l := list(want(t, chu.get(base+"diary-entries?review=true"), 200, "", "hàng chờ rà soát"), "items"); len(l) != 2 ||
		l[0].(map[string]any)["can_review"] != true {
		t.Fatalf("chủ nhiệm thấy 2 lần ghi chờ rà soát: %v", l)
	}
	if l := list(want(t, chu2.get(base+"diary-entries?review=true"), 200, "", "chủ nhiệm khác"), "items"); len(l) != 0 {
		t.Fatalf("chủ nhiệm chỉ rà soát chương trình mình: %v", l)
	}
	if l := list(want(t, chu.get(base+"diary-entries?flagged=true&cycle_ref="+cref), 200, "", "lọc có cảnh báo"), "items"); len(l) != 1 {
		t.Fatalf("lọc lần ghi có cảnh báo: %v", l)
	}
	diaryProvinceList(t, viewer, other, offlineID, e2ref)
	want(t, viewer.post(base+"diary-entries/"+e2ref+"/verify", map[string]any{"base_version": 1}), 403, "", "viên chức không xác thực")
	want(t, officer.post(base+"diary-entries/"+e2ref+"/verify", map[string]any{"base_version": 1}), 403, "", "người ghi không xác thực")
	want(t, chu.post(base+"diary-entries/"+e2ref+"/return", map[string]any{"base_version": 1, "reason": ""}), 422, "", "thiếu lý do")
	rt := want(t, chu.post(base+"diary-entries/"+e2ref+"/return", map[string]any{"base_version": 1, "reason": "Kiểm tra lại vị trí ghi"}),
		200, "", "yêu cầu bổ sung")
	if str(rt, "status") != "returned" || str(rt, "reviewer_ref") != chu.ref.String() {
		t.Fatalf("yêu cầu bổ sung: %s", rt)
	}
	want(t, chu.post(base+"diary-entries/"+e2ref+"/verify", map[string]any{"base_version": 2}), 409, "extension.cnts_diary_entry_state",
		"chưa gửi lại thì chưa xác thực")
	edit := map[string]any{"base_version": 2, "entry_kind": "daily", "metrics": map[string]any{"alive": "487", "dead": "1", "death_cause": "benh",
		"feed_kg": "13", "age_week": "2", "weight_avg_g": "180", "health_note": "Đã kiểm tra lại"}, "tables": day2["tables"], "submit": true}
	want(t, officer.post(base+"diary-entries/"+e2ref, merge(edit, map[string]any{"location": near})), 422, "", "vị trí lúc ghi không sửa được")
	rs := want(t, officer.post(base+"diary-entries/"+e2ref, edit), 200, "", "sửa và gửi lại")
	if str(rs, "status") != "submitted" || num(rs, "version") != "4" || obj(rs, "location")["far"] != true {
		t.Fatalf("gửi lại giữ vị trí lúc ghi: %s", rs)
	}
	vf := want(t, chu.post(base+"diary-entries/"+e2ref+"/verify", map[string]any{"base_version": 4}), 200, "", "xác thực")
	if str(vf, "status") != "verified" || vf["can_correct"] != false {
		t.Fatalf("xác thực: %s", vf)
	}
	if ov := want(t, officer.get(base+"diary-entries/"+e2ref), 200, "", "người ghi xem"); ov["can_edit"] != false || ov["can_correct"] != true {
		t.Fatalf("AC-04: đã xác thực không có nút Sửa: %s", ov)
	}
	want(t, officer.post(base+"diary-entries/"+e2ref, merge(edit, map[string]any{"base_version": 5})), 409, "extension.cnts_diary_entry_locked",
		"đã xác thực không sửa")
	want(t, officer.post(base+"diary-entries/"+e2ref+"/correct", merge(edit, map[string]any{"base_version": 5})), 422, "", "điều chỉnh thiếu lý do")
	co := want(t, officer.post(base+"diary-entries/"+e2ref+"/correct", merge(edit, map[string]any{"base_version": 5, "submit": false,
		"reason": "Sửa lượng thức ăn theo sổ giấy", "metrics": merge(edit["metrics"].(map[string]any), map[string]any{"feed_kg": "13.5"})})),
		200, "", "điều chỉnh tạo bản mới có lý do")
	if str(co, "status") != "submitted" || str(co, "event") != "corrected" || len(list(co, "versions")) != 6 {
		t.Fatalf("điều chỉnh: %s", co)
	}
	want(t, chu.post(base+"diary-entries/"+e2ref+"/verify", map[string]any{"base_version": 6}), 200, "", "xác thực lại bản điều chỉnh")
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_diary_entry WHERE tenant_id = $1 AND entry_ref = $2`, f.tenant, e2ref); n != 7 {
		t.Fatalf("sổ chỉ ghi thêm — mọi phiên bản còn: %d", n)
	}
	// Người xác thực khác người ghi: Admin tự ghi rồi tự xác thực → chặn (ứng dụng + CHECK của sổ).
	ad := want(t, admin.post(base+"diary-entries", map[string]any{"cycle_ref": cref, "entry_kind": "daily",
		"metrics": map[string]any{"alive": "487", "dead": "0", "feed_kg": "13"}, "location": near, "submit": true}), 201, "", "Admin ghi")
	want(t, admin.post(base+"diary-entries/"+str(ad, "ref")+"/verify", map[string]any{"base_version": 1}), 403, "extension.cnts_diary_self_verify",
		"người ghi không tự xác thực")
	if _, err := f.pool.Exec(ctx, `INSERT INTO pf_extension.cnts_diary_entry (id, tenant_id, stream_key, entry_ref, event, status, cycle_ref,
	    household_ref, site_ref, project_ref, commune_code, form, entry_kind, entry_date, captured_at, metrics, has_fix, fix_stored,
	    no_target_point, far, weak_accuracy, mock_location, mock_suspected, author_ref, reviewer_ref, event_time, actor_ref, idem_key, rule_version)
	  SELECT $1::uuid, tenant_id, stream_key, entry_ref, 'verified', 'verified', cycle_ref, household_ref, site_ref, project_ref, commune_code, form,
	    entry_kind, entry_date, captured_at, metrics, has_fix, fix_stored, no_target_point, far, weak_accuracy, mock_location, mock_suspected,
	    author_ref, author_ref, now(), author_ref, $3, 'test'
	  FROM pf_extension.cnts_diary_entry WHERE entry_ref = $2 LIMIT 1`, ids.New(), str(ad, "ref"), "x-"+key()); err == nil ||
		!strings.Contains(err.Error(), "cnts_diary_entry_four_eyes") {
		t.Fatalf("CHECK cnts_diary_entry_four_eyes phải chặn tự xác thực: %v", err)
	}

	// Chi tiết cho người rà soát: tọa độ lúc ghi, tọa độ hộ, biểu đồ tăng trọng; Cấp Tỉnh không rà soát được: không có tọa độ.
	dt := want(t, chu.get(base+"diary-entries/"+e2ref), 200, "", "chi tiết")
	if dt["fix"] == nil || dt["target"] == nil || len(list(dt, "weights")) != 1 || dt["previous"] == nil {
		t.Fatalf("chi tiết rà soát: %s", dt)
	}
	if dv := want(t, viewer.get(base+"diary-entries/"+e2ref), 200, "", "viên chức xem"); dv["fix"] != nil {
		t.Fatalf("viên chức không thấy tọa độ lúc ghi: %v", dv["fix"])
	}

	// ----- BM-19 thủy sản: ảnh cân mẫu bắt buộc, pH 8,7 → cảnh báo ngay (AC-02) -----
	cc := want(t, officer.post(base+"diary-cycles", map[string]any{"household_ref": hCa, "breed": "Cá rô phi", "received_date": "2027-09-01",
		"initial_count": 3000}), 201, "", "lứa cá")
	ccref := str(cc, "ref")
	fish := map[string]any{"cycle_ref": ccref, "entry_kind": "growth_env", "metrics": map[string]any{"sample_count": "30", "weight_avg_g": "120.5",
		"length_avg_cm": "15.2", "loss": "12", "ph": "8.7", "do_mgl": "5.1", "feed_kg": "40"}, "location": map[string]any{"lat": 10.5011,
		"lng": 105.4311, "accuracy_m": "6"}, "submit": true}
	bl := want(t, officer.post(base+"diary-entries", fish), 422, "extension.cnts_diary_entry_blocked", "thiếu ảnh cân mẫu, thiếu biện pháp")
	if !bytes.Contains(r(bl), []byte("NT-13")) || !bytes.Contains(r(bl), []byte("NT-12")) {
		t.Fatalf("lỗi chặn BM-19: %v", bl)
	}
	want(t, other.upload(hCa, "weighing", pngBytes), 404, "", "xã khác không tải ảnh")
	want(t, officer.upload(hCa, "weighing", []byte("không phải ảnh")), 422, "extension.cnts_diary_photo_invalid", "kiểu tệp")
	want(t, officer.upload(hCa, "weighing", bytes.Repeat([]byte{0}, 600<<10)), 422, "extension.cnts_diary_photo_invalid", "quá 500 KB")
	ph := want(t, officer.upload(hCa, "weighing", pngBytes), 201, "", "tải ảnh cân mẫu")
	photo := str(ph, "file_ref")
	f.files.mu.Lock()
	blob := f.files.blobs[uuid.MustParse(photo)]
	f.files.mu.Unlock()
	if len(blob) == 0 || bytes.Contains(blob, []byte("IHDR")) {
		t.Fatal("kho tệp phải giữ bản mã, không giữ ảnh rõ")
	}
	// #172: mất phản hồi sau khi đã lưu → web gửi lại đúng thân multipart (cùng id, cùng boundary) với cùng khóa → máy chủ
	// phát lại ảnh đã lưu, không mã hóa/lưu lần hai; cùng khóa mà khác ảnh → idempotency_key_reused.
	pid := ids.New().String()
	pb, pct := photoBody(hCa, "weighing", pid, pngBytes)
	pk := key()
	p1 := officer.uploadRaw(pk, pct, pb)
	want(t, p1, 201, "", "tải ảnh (multipart tất định)")
	f.files.mu.Lock()
	before := f.files.attaches
	f.files.mu.Unlock()
	p2 := officer.uploadRaw(pk, pct, pb)
	if want(t, p2, 201, "", "bấm lại sau mất phản hồi"); str(p2.body, "file_ref") != pid || !bytes.Equal(p2.raw, p1.raw) {
		t.Fatalf("phải phát lại đúng ảnh đã lưu: %s / %s", p1.raw, p2.raw)
	}
	f.files.mu.Lock()
	after := f.files.attaches
	f.files.mu.Unlock()
	if after != before {
		t.Fatalf("phát lại không được lưu ảnh lần hai: %d → %d", before, after)
	}
	ob, oct := photoBody(hCa, "weighing", pid, append(bytes.Clone(pngBytes), 0))
	want(t, officer.uploadRaw(pk, oct, ob), 422, "idempotency_key_reused", "cùng khóa, khác ảnh")
	// Review #172: khác ảnh + khóa mới nhưng dùng lại id ảnh đã lưu (cùng hộ) → 422 "mã ảnh đã dùng", không còn 409
	// "chủ hộ chưa có khóa", và không mã hóa/lưu gì thêm.
	reused := officer.uploadRaw(key(), oct, ob)
	if want(t, reused, 422, "extension.cnts_diary_photo_invalid", "khóa mới, id ảnh đã dùng"); !bytes.Contains(reused.raw, []byte("mã ảnh đã dùng")) {
		t.Fatalf("id ảnh đã dùng phải báo ở trường id: %s", reused.raw)
	}
	f.files.mu.Lock()
	reAttach := f.files.attaches
	f.files.mu.Unlock()
	if reAttach != after {
		t.Fatalf("id ảnh đã dùng không được Attach: %d → %d", after, reAttach)
	}
	want(t, officer.post(base+"diary-entries", merge(fish, map[string]any{"photo_refs": []string{ids.New().String()},
		"metrics": merge(fish["metrics"].(map[string]any), map[string]any{"action_note": "Thay 30% nước"})})), 422,
		"extension.cnts_diary_entry_invalid", "ảnh chưa tải")
	fe := want(t, officer.post(base+"diary-entries", merge(fish, map[string]any{"photo_refs": []string{photo},
		"metrics": merge(fish["metrics"].(map[string]any), map[string]any{"action_note": "Thay 30% nước, bón vôi"})})), 201, "", "lần ghi BM-19")
	if !slices.Contains(checkCodes(fe), "ph_out_of_range") {
		t.Fatalf("pH 8,7 ngoài ngưỡng: %v", checkCodes(fe))
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_alert_current WHERE tenant_id = $1 AND kind = 'water_quality' AND source_ref = $2
	    AND $3 = ANY (recipient_refs) AND $4 = ANY (recipient_refs)`, f.tenant, str(fe, "ref"), chu.ref, officer.ref); n != 1 {
		t.Fatalf("AC-02: cảnh báo môi trường nước tới chủ nhiệm và cán bộ theo dõi: %d", n)
	}
	open := func(u user) resp {
		req, _ := http.NewRequest(http.MethodGet, f.srv.URL+base+"diary-photos/"+photo+"/content", nil)
		return u.send(req, "")
	}
	if rr := open(chu); rr.status != 200 || !bytes.Equal(rr.raw, pngBytes) {
		t.Fatalf("chủ nhiệm xem ảnh: %d %s", rr.status, rr.raw)
	}
	want(t, open(viewer), 404, "extension.cnts_diary_photo_not_found", "viên chức không xem ảnh")
	want(t, open(other), 404, "extension.cnts_diary_photo_not_found", "xã khác không xem ảnh")
	if !slices.Contains(f.files.purposes, "extension.diary") {
		t.Fatalf("mục đích mở ảnh: %v", f.files.purposes)
	}

	// ----- Hạch toán NC-15, kết thúc lứa -----
	cg := want(t, officer.get(base+"diary-cycles/"+cref), 200, "", "lứa")
	sg := obj(cg, "suggest")
	if str(sg, "feed_cost") != "300000" || num(sg, "sold_heads") != "10" || str(sg, "sales_amount") != "1260000" || num(sg, "current_heads") != "487" {
		t.Fatalf("gợi ý hạch toán: %v", sg)
	}
	ver := num(cg, "version")
	want(t, officer.post(base+"diary-cycles/"+cref+"/close", map[string]any{"base_version": json.Number(ver), "cost_seed": "5000000"}), 422,
		"extension.cnts_diary_cycle_invalid", "thiếu tổng thu")
	cl := want(t, officer.post(base+"diary-cycles/"+cref+"/close", map[string]any{"base_version": json.Number(ver), "cost_seed": "5000000",
		"cost_feed": "30000000", "cost_vet": "1500000", "revenue_heads": 480, "revenue_output_kg": "840", "revenue_price": "70000",
		"revenue_amount": "58800000"}), 200, "", "kết thúc lứa")
	acc := obj(cl, "accounting")
	if str(cl, "status") != "closed" || str(acc, "total_cost") != "36500000" || str(acc, "profit") != "22300000" || str(acc, "unit_cost") != "43452" {
		t.Fatalf("hạch toán: %v", acc)
	}
	if lc := want(t, chu.get(base+"diary-cycles?site_ref="+sGa), 200, "", "lứa theo điểm"); str(lc, "avg_profit") != "22300000" ||
		num(lc, "closed_count") != "1" {
		t.Fatalf("lợi nhuận trung bình/hộ: %s", lc)
	}
	want(t, officer.post(base+"diary-entries", merge(day1, map[string]any{"captured_at": t0.Format(time.RFC3339)})), 409,
		"extension.cnts_diary_cycle_closed", "lứa đã kết thúc")
	want(t, chu.post(base+"diary-cycles/"+cref+"/reopen", map[string]any{"base_version": json.Number(num(cl, "version")),
		"reason": "Bổ sung lần ghi"}), 200, "", "chủ nhiệm mở lại lứa")

	// ----- Hạn sửa 7 ngày, dọn tọa độ, xóa định danh chủ hộ -----
	draft := want(t, officer.post(base+"diary-entries", map[string]any{"cycle_ref": ccref, "entry_kind": "env",
		"metrics": map[string]any{"loss": "0", "ph": "7.8", "do_mgl": "5", "feed_kg": "35"}}), 201, "", "nháp")
	if str(draft, "status") != "draft" {
		t.Fatalf("nháp: %s", draft)
	}
	if l := list(want(t, chu.get(base+"diary-entries?cycle_ref="+ccref), 200, "", "chủ nhiệm"), "items"); len(l) != 1 {
		t.Fatalf("bản nháp chỉ người ghi thấy: %v", l)
	}
	f.clk.Advance(9 * 24 * time.Hour)
	want(t, officer.post(base+"diary-entries/"+str(fe, "ref"), map[string]any{"base_version": 1, "entry_kind": "growth_env",
		"metrics": fish["metrics"], "photo_refs": []string{photo}}), 409, "extension.cnts_diary_entry_locked", "quá 7 ngày không sửa lần ghi đã gửi")
	var purged int
	if err := f.pool.QueryRow(ctx, `SELECT pf_extension.purge_diary_fixes('2099-01-01', 100000)`).Scan(&purged); err != nil || purged < 4 {
		t.Fatalf("dọn tọa độ quá hạn lưu: %d %v", purged, err)
	}
	if dd := want(t, chu.get(base+"diary-entries/"+offlineID), 200, "", "sau khi dọn"); dd["fix"] != nil || str(obj(dd, "location"), "distance_m") == "" {
		t.Fatalf("dọn tọa độ giữ khoảng cách trong sổ: %s", dd)
	}
	var person uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT person_ref FROM pf_extension.cnts_model_household WHERE household_ref = $1 LIMIT 1`, hCa).Scan(&person); err != nil {
		t.Fatal(err)
	}
	pctx := authz.WithPrincipal(ctx, authz.Principal{TenantID: f.tenant, ActorRef: chu.ref, Roles: []string{client.RoleEraser}, MFA: true})
	if err := f.vault.Erase(pctx, person, "Chủ hộ yêu cầu xóa dữ liệu cá nhân"); err != nil {
		t.Fatal(err)
	}
	if rr := open(chu); rr.status == 200 {
		t.Fatal("xóa định danh chủ hộ → ảnh không còn đọc được")
	}
}

func r(m map[string]any) []byte { b, _ := json.Marshal(m); return b }

// BF-CN-DIARY (#109, CN-08/CN-12): nhật ký BM-08/BM-09 đủ trường sổ gốc 2026 — đầu sổ (mô hình, thời gian thực hiện, cán bộ
// theo dõi), mục 1 vật tư (vật tư + chủng loại), mục 4 mua nguyên liệu thức ăn (cộng vào gợi ý chi thức ăn NC-15), mục 5 ghi chú
// tiêm phòng, phần 3 họ tên cán bộ; sổ vịt xiêm vẫn không có phòng/trị bệnh, xuất bán.
func TestCNTSDiary_BFCNDiaryPaperFields(t *testing.T) {
	f := newFix(t)
	chu := f.user(tinh("chu_nhiem"))
	officer := f.user(xa("30001"))
	f.xa.list = []accport.XaScope{{ActorRef: officer.ref, Communes: []string{"30001"}}}
	p := want(t, chu.post(base+"projects", map[string]any{"code": "da-vit-2027", "name": "Mô hình vịt xiêm ATSH 2027", "kind": "tinh",
		"start_date": "2027-03-01", "end_date": "2027-12-31"}), 201, "", "dự án")
	s := want(t, chu.post(base+"demo-sites", map[string]any{"project_ref": str(p, "ref"), "commune_code": "30001", "name": "Điểm vịt Bình Hòa",
		"model_type_code": "GA_TP", "officer_ref": officer.ref.String(), "diary_form": "BM-09",
		"location": map[string]any{"lat": 10.38, "lng": 105.43, "source": "map"}}), 201, "", "điểm BM-09")
	h := str(want(t, officer.post(base+"model-households", map[string]any{"site_ref": str(s, "ref"), "owner_name": "Lê Văn Vịt",
		"owner_phone": "0912333444", "keeper_kind": "officer", "keeper_ref": officer.ref.String(), "location": map[string]any{"lat": 10.381,
			"lng": 105.431, "source": "gps"}}), 201, "", "hộ"), "ref")

	cyc := map[string]any{"household_ref": h, "breed": "Vịt xiêm Pháp", "received_date": "2027-10-01", "initial_count": 300, "age_days": 1,
		"supplies": []map[string]any{
			{"item": "Con giống", "date": "2027-10-01", "kind": "Vịt xiêm Pháp 1 ngày tuổi", "quantity": "300", "unit": "con"},
			{"item": "Thức ăn vịt con", "date": "2027-10-01", "kind": "", "quantity": "200", "unit": "kg"},
			{"date": "2027-10-01", "kind": "Vaccine viêm gan vịt", "quantity": "3", "unit": "lọ"}}}
	want(t, officer.post(base+"diary-cycles", merge(cyc, map[string]any{"supplies": []map[string]any{{"date": "2027-10-01", "kind": "",
		"quantity": "1", "unit": ""}}})), 422, "extension.cnts_diary_cycle_invalid", "mục 1: thiếu cả vật tư và chủng loại")
	cy := want(t, officer.post(base+"diary-cycles", cyc), 201, "", "mở lứa vịt")
	cref := str(cy, "ref")
	d := want(t, officer.get(base+"diary-cycles/"+cref), 200, "", "chi tiết lứa")
	sup := list(d, "supplies")
	if len(sup) != 3 || str(sup[0].(map[string]any), "item") != "Con giống" || str(sup[1].(map[string]any), "item") != "Thức ăn vịt con" {
		t.Fatalf("mục 1 vật tư: %v", sup)
	}
	if str(d, "project_name") != "Mô hình vịt xiêm ATSH 2027" || str(d, "project_start") != "2027-03-01" || str(d, "project_end") != "2027-12-31" ||
		str(d, "officer_ref") != officer.ref.String() {
		t.Fatalf("đầu sổ NC-01 (mô hình, thời gian thực hiện, cán bộ theo dõi): %v", d)
	}

	day := map[string]any{"cycle_ref": cref, "entry_kind": "daily", "metrics": map[string]any{"alive": "298", "dead": "2", "death_cause": "yeu",
		"feed_kg": "6"}, "location": map[string]any{"lat": 10.3811, "lng": 105.4311, "accuracy_m": "8"}, "submit": true,
		"tables": map[string]any{
			"feed_purchases": []map[string]any{{"date": "2027-10-12", "name": "Cám vịt con", "quantity_kg": "25", "unit_price": "12000", "note": "đại lý"}},
			"feed_materials": []map[string]any{{"date": "2027-10-12", "name": "Lúa", "quantity_kg": "10.5", "unit_price": "8000", "note": "tự trộn"}},
			"vaccinations": []map[string]any{{"vaccine": "Dịch tả vịt", "shot": 0, "date": "2027-10-12", "dosage": "1 liều/con",
				"method": "tiêm dưới da", "mfg_date": "2027-09-01", "note": "lô 0927"}},
			"officer_notes": []map[string]any{{"date": "2027-10-12", "officer": "Nguyễn Văn Cán", "content": "Rửa máng uống hằng ngày",
				"farmer_opinion": "Đồng ý"}}}}
	want(t, officer.post(base+"diary-entries", merge(day, map[string]any{"tables": map[string]any{"treatments": []map[string]any{{"date": "2027-10-12",
		"drug": "X", "withdrawal_days": 1}}}})), 422, "extension.cnts_diary_entry_invalid", "sổ vịt xiêm 2026 không có phòng và trị bệnh")
	e := want(t, officer.post(base+"diary-entries", day), 201, "", "lần ghi đủ trường sổ gốc")
	tb := obj(e, "tables")
	fm := list(tb, "feed_materials")
	if len(fm) != 1 || str(fm[0].(map[string]any), "name") != "Lúa" || str(fm[0].(map[string]any), "quantity_kg") != "10.5" {
		t.Fatalf("mục 4 nguyên liệu thức ăn: %v", tb)
	}
	if v := list(tb, "vaccinations")[0].(map[string]any); str(v, "note") != "lô 0927" || str(v, "method") != "tiêm dưới da" {
		t.Fatalf("mục 5 tiêm phòng: %v", v)
	}
	if o := list(tb, "officer_notes")[0].(map[string]any); str(o, "officer") != "Nguyễn Văn Cán" {
		t.Fatalf("phần 3 họ tên cán bộ: %v", o)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_diary_entry WHERE tenant_id = $1 AND entry_ref = $2
	    AND tables -> 'feed_materials' -> 0 ->> 'name' = 'Lúa'`, f.tenant, str(e, "ref")); n != 1 {
		t.Fatalf("sổ lần ghi lưu mục 4: %d", n)
	}
	d = want(t, officer.get(base+"diary-cycles/"+cref), 200, "", "gợi ý hạch toán")
	if fc := str(obj(d, "suggest"), "feed_cost"); fc != "384000" { // 25×12.000 + 10,5×8.000
		t.Fatalf("NC-09 mục 3 + mục 4 cộng vào gợi ý chi thức ăn: %s", fc)
	}
}

// diaryProvinceList: BF-CN-XA-DATA (#175) — màn Tỉnh "Số liệu cấp xã › Nhật ký sản xuất": lọc xã, mẫu, khoảng ngày ở máy
// chủ; trang theo con trỏ (khóa entry_date, captured_at, entry_ref giảm dần); xã ngoài phạm vi → rỗng; tham số sai → 422.
func diaryProvinceList(t *testing.T, viewer, outsider user, e1, e2 string) {
	t.Helper()
	p1 := want(t, viewer.get(base+"diary-entries?limit=1"), 200, "", "trang 1")
	l1 := list(p1, "items")
	if len(l1) != 1 || str(l1[0].(map[string]any), "ref") != e2 || str(p1, "next_cursor") == "" {
		t.Fatalf("trang 1 (mới trước, còn trang sau): %v", p1)
	}
	p2 := want(t, viewer.get(base+"diary-entries?limit=1&cursor="+str(p1, "next_cursor")), 200, "", "trang 2")
	l2 := list(p2, "items")
	if len(l2) != 1 || str(l2[0].(map[string]any), "ref") != e1 || p2["next_cursor"] != nil {
		t.Fatalf("trang 2 (hết): %v", p2)
	}
	if all := want(t, viewer.get(base+"diary-entries"), 200, "", "không limit"); len(list(all, "items")) != 2 || all["next_cursor"] != nil {
		t.Fatalf("không limit: %v", all)
	}
	for q, n := range map[string]int{
		"commune=30001":                             2,
		"commune=30002":                             0,
		"commune=30001&form=BM-19":                  0,
		"form=BM-08&from=2027-10-12&to=2027-10-12":  1,
		"from=2027-10-13":                           0,
		"to=2027-10-11":                             1,
		"commune=30001&status=submitted&form=BM-08": 2,
		"commune=30001&status=verified":             0,
	} {
		if l := list(want(t, viewer.get(base+"diary-entries?"+q), 200, "", q), "items"); len(l) != n {
			t.Fatalf("%s: %d lần ghi, muốn %d", q, len(l), n)
		}
	}
	if l := list(want(t, outsider.get(base+"diary-entries?commune=30001"), 200, "", "xã ngoài phạm vi"), "items"); len(l) != 0 {
		t.Fatalf("xã ngoài phạm vi phải rỗng: %v", l)
	}
	// #175 review [P2]: phân trang SAU khi lọc phạm vi — tài khoản xã 30002 không thấy lần ghi nào của xã 30001 thì không
	// được nhận con trỏ (con trỏ cũ mang ngày ghi, captured_at, UUID của lần ghi ngoài phạm vi).
	for _, q := range []string{"limit=1", "limit=2", ""} {
		p := want(t, outsider.get(base+"diary-entries?"+q), 200, "", "ngoài phạm vi "+q)
		if len(list(p, "items")) != 0 || p["next_cursor"] != nil {
			t.Fatalf("ngoài phạm vi (%q) phải rỗng và không con trỏ: %v", q, p)
		}
	}
	// Con trỏ chỉ mang khóa của dòng cuối ĐÃ TRẢ cho người gọi.
	if raw, err := base64.RawURLEncoding.DecodeString(str(p1, "next_cursor")); err != nil || !strings.HasSuffix(string(raw), "|"+e2) {
		t.Fatalf("con trỏ trang 1 phải là khóa của %s: %q %v", e2, raw, err)
	}
	for _, q := range []string{"form=BM-99", "limit=0", "limit=201", "limit=x", "cursor=YWJj", "cursor=!!!", "from=2027-10-12&to=2027-10-11", "from=12/10/2027"} {
		want(t, viewer.get(base+"diary-entries?"+q), 422, "", "tham số sai: "+q)
	}
}
