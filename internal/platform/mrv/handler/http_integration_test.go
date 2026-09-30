package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
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
	mod     *mrv.Module
	pool    *pgxpool.Pool
	store   *objstore.Local
	scopes  *fakeScopes
	consent *fakeConsent
	params  *fakeParams
	tenant  uuid.UUID
	// tổ chức mẫu: tỉnh 89 (xã 30001, 30002), tổ xã 30001, TW
	p89, t30001, national uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, mrv.DBRole)
	ops := env.Pool(t, migrations.PF, "platform_api")
	st, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scopes, consent, params := newFakeScopes(), newFakeConsent(), newFakeParams()
	mod := mrv.New(pool, clock.System{}, mrv.Deps{Scope: scopes, Consent: consent, Store: st, Params: params})
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	adminEng := httpx.NewEngine(log, 1<<20)
	mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	appSrv, adminSrv := httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(appSrv.Close)
	t.Cleanup(adminSrv.Close)
	f := &fixture{t: t, iss: iss, app: appSrv, admin: adminSrv, mod: mod, pool: pool, store: st, scopes: scopes,
		consent: consent, params: params, tenant: ids.New(), p89: ids.New(), t30001: ids.New(), national: ids.New()}
	scopes.set(f.p89, authz.Scope{Units: []string{"89", "30001", "30002"}})
	scopes.set(f.t30001, authz.Scope{Units: []string{"30001"}})
	scopes.set(f.national, authz.Scope{All: true})
	return f
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	org   uuid.UUID
	token string
	actor uuid.UUID
}

func (f *fixture) as(tid, org uuid.UUID, roles ...string) caller {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, MFA: true})
	return caller{f: f, tid: tid, org: org, token: tok, actor: actor}
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
	if method == http.MethodPost {
		if idem == "" {
			idem = "k-" + ids.New().String()
		}
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

func (c caller) app(method, path string, body any) resp {
	return c.do(c.f.app.URL, method, path, "", body)
}

func (c caller) adm(method, path string, body any) resp {
	return c.do(c.f.admin.URL, method, path, "", body)
}

func (r resp) code() string { s, _ := r.body["code"].(string); return s }

func (r resp) str(k string) string { s, _ := r.body[k].(string); return s }

func (r resp) num(k string) int { n, _ := r.body[k].(float64); return int(n) }

func (r resp) fieldErr(field string) bool {
	errs, _ := r.body["errors"].([]any)
	for _, e := range errs {
		if m, ok := e.(map[string]any); ok && m["field"] == field {
			return true
		}
	}
	return false
}

func expect(t *testing.T, r resp, status int, code string) {
	t.Helper()
	if r.status != status || (code != "" && r.code() != code) {
		t.Fatalf("mong %d %s, nhận %d %v", status, code, r.status, r.body)
	}
}

// approveSeed nạp danh mục mẫu G1 và cho một người khác duyệt (R-56).
func (f *fixture) approveSeed(tid uuid.UUID) {
	f.t.Helper()
	setter := f.as(tid, uuid.Nil, authz.RoleParamSetter)
	approver := f.as(tid, uuid.Nil, authz.RoleParamApprover)
	r := setter.adm(http.MethodPost, "/v1/mrv/catalog/seed", nil)
	expect(f.t, r, 200, "")
	items, _ := r.body["items"].([]any)
	if len(items) != 6 {
		f.t.Fatalf("seed phải tạo 6 danh mục: %v", r.body)
	}
	for _, it := range items {
		v, _ := it.(map[string]any)["version"].(map[string]any)
		if v == nil {
			continue
		}
		if v["provisional"] != true || v["status"] != "draft" {
			f.t.Fatalf("danh mục mẫu phải là nháp tạm (C-1): %v", v)
		}
		id := v["id"].(string)
		expect(f.t, setter.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+id+"/submit", nil), 200, "")
		expect(f.t, approver.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+id+"/approve", map[string]any{"note": "Duyệt tạm G1"}), 200, "")
	}
}

func sha() string { return strings.Repeat("ab", 32) }

func awd(unit string) map[string]any {
	return map[string]any{
		"evidence_type": port.EvidenceAWDLog, "admin_unit_code": unit, "plot_ref": ids.New().String(),
		"season_code": "2026-DX",
		"values":      map[string]any{"stage": "de_nhanh", "awd_water_level_cm": "-12.5", "awd_dry_days": 5},
		"file_refs":   []any{map[string]any{"file_ref": ids.New().String(), "sha256": sha()}},
		"gps":         map[string]any{"lat": "10.0300000", "lon": "105.7800000", "accuracy_m": "6.5"},
		"event_time":  time.Now().Add(-time.Hour).Format(time.RFC3339),
	}
}

func values(b map[string]any) map[string]any { return b["values"].(map[string]any) }

// ---------------------------------------------------------------------------

// R-56 + ghim phiên bản: danh mục chỉ dùng được sau khi người KHÁC duyệt; bản ghi
// kiểm theo phiên bản đã ghim kể cả khi phiên bản mới hơn đã duyệt.
func TestCatalogSoDAndVersionPinning(t *testing.T) {
	f := newFixture(t)
	setter := f.as(f.tenant, uuid.Nil, authz.RoleParamSetter)
	other := f.as(f.tenant, uuid.Nil, authz.RoleParamSetter)
	approver := f.as(f.tenant, uuid.Nil, authz.RoleParamApprover)
	both := f.as(f.tenant, uuid.Nil, authz.RoleParamSetter, authz.RoleParamApprover)
	col := f.as(f.tenant, f.t30001, port.RoleCollector)

	r := setter.adm(http.MethodPost, "/v1/mrv/catalog/seed", nil)
	expect(t, r, 200, "")
	// Chưa duyệt → không thu được.
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", awd("30001")), 422, "mrv.catalog_version_unavailable")

	// Seed lặp: đã có phiên bản → không tạo thêm.
	r2 := setter.adm(http.MethodPost, "/v1/mrv/catalog/seed", nil)
	for _, it := range r2.body["items"].([]any) {
		if it.(map[string]any)["created"] != false {
			t.Fatalf("seed lặp không được tạo mới: %v", r2.body)
		}
	}
	list := setter.adm(http.MethodGet, "/v1/mrv/catalog/versions?evidence_type=awd_log", nil)
	awdID := list.body["items"].([]any)[0].(map[string]any)["id"].(string)

	expect(t, other.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+awdID+"/submit", nil), 403, "mrv.catalog_not_setter")
	expect(t, setter.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+awdID+"/approve", nil), 403, "forbidden")
	expect(t, setter.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+awdID+"/submit", nil), 200, "")

	// Người có cả hai vai tự đặt rồi tự duyệt → bị chặn (R-56).
	own := both.adm(http.MethodPost, "/v1/mrv/catalog/versions", map[string]any{
		"evidence_type": "pest_report", "source_note": "Thử", "fields": []any{
			map[string]any{"key": "pest_level", "label_vi": "Cấp gây hại", "data_type": "integer", "required": true, "min": "1", "max": "5"}},
	})
	expect(t, own, 201, "")
	expect(t, both.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+own.str("id")+"/submit", nil), 200, "")
	expect(t, both.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+own.str("id")+"/approve", nil), 403, "self_approval_forbidden")

	ok := approver.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+awdID+"/approve", map[string]any{"note": "Duyệt tạm"})
	expect(t, ok, 200, "")
	if ok.str("status") != "approved" || ok.str("approver_ref") != approver.actor.String() {
		t.Fatalf("duyệt: %v", ok.body)
	}
	// Đã quyết định → bất biến.
	expect(t, approver.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+awdID+"/reject", map[string]any{"note": "Đổi ý rồi"}), 409, "mrv.catalog_invalid_transition")

	// Bản ghi v1.
	e1 := col.app(http.MethodPost, "/v1/mrv/evidence", awd("30001"))
	expect(t, e1, 201, "")
	if e1.num("catalog_version") != 1 {
		t.Fatalf("phải ghim v1: %v", e1.body)
	}

	// v2: bỏ awd_dry_days, thêm trường bắt buộc awd_pipe_id.
	v2 := setter.adm(http.MethodPost, "/v1/mrv/catalog/versions", map[string]any{
		"evidence_type": "awd_log", "provisional": true, "source_note": "Bổ sung mã ống quan trắc (C-1 vòng 2)",
		"fields": []any{
			map[string]any{"key": "plot_ref", "label_vi": "Thửa", "data_type": "text", "required": true},
			map[string]any{"key": "awd_water_level_cm", "label_vi": "Mực nước", "data_type": "decimal", "unit": "cm", "required": true, "min": "-30", "max": "30"},
			map[string]any{"key": "awd_pipe_id", "label_vi": "Mã ống quan trắc", "data_type": "text", "required": true},
			map[string]any{"key": "photo", "label_vi": "Ảnh", "data_type": "photo", "required": true, "min": "1"},
			map[string]any{"key": "gps", "label_vi": "Toạ độ", "data_type": "geo_point", "required": true},
		},
	})
	expect(t, v2, 201, "")
	if v2.num("version") != 2 {
		t.Fatalf("phiên bản kế tiếp phải là 2: %v", v2.body)
	}
	expect(t, setter.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+v2.str("id")+"/submit", nil), 200, "")
	expect(t, approver.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+v2.str("id")+"/approve", nil), 200, "")

	latest := col.app(http.MethodGet, "/v1/mrv/catalog?evidence_type=awd_log", nil)
	if latest.num("version") != 2 {
		t.Fatalf("danh mục đã duyệt mới nhất phải là v2: %v", latest.body)
	}

	// Không ghim → theo v2: thiếu awd_pipe_id, và awd_dry_days/stage không còn trong danh mục.
	b := awd("30001")
	r = col.app(http.MethodPost, "/v1/mrv/evidence", b)
	expect(t, r, 422, "validation_failed")
	if !r.fieldErr("values.awd_pipe_id") || !r.fieldErr("values.awd_dry_days") || !r.fieldErr("values.stage") {
		t.Fatalf("lỗi theo v2: %v", r.body)
	}
	// Ghim v1 (máy ngoại tuyến còn danh mục cũ) → vẫn hợp lệ theo v1.
	b["catalog_version"] = 1
	r = col.app(http.MethodPost, "/v1/mrv/evidence", b)
	expect(t, r, 201, "")
	if r.num("catalog_version") != 1 {
		t.Fatalf("phải giữ v1 đã ghim: %v", r.body)
	}
	// Bản v1 cũ vẫn đọc theo v1.
	g := col.app(http.MethodGet, "/v1/mrv/evidence/"+e1.str("id"), nil)
	if int(g.body["evidence"].(map[string]any)["catalog_version"].(float64)) != 1 {
		t.Fatalf("bản cũ đổi phiên bản: %v", g.body)
	}
	// Ghim phiên bản không tồn tại/chưa duyệt.
	b["catalog_version"] = 9
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", b), 422, "mrv.catalog_version_unavailable")
}

// Định nghĩa danh mục sai → 422 theo trường (geo_point ngoài khóa gps, trùng khóa, kiểu khóa dành riêng).
func TestCatalogDefinitionValidation(t *testing.T) {
	f := newFixture(t)
	setter := f.as(f.tenant, uuid.Nil, authz.RoleParamSetter)
	r := setter.adm(http.MethodPost, "/v1/mrv/catalog/versions", map[string]any{
		"evidence_type": "awd_log", "source_note": "x", "fields": []any{
			map[string]any{"key": "where", "label_vi": "Vị trí", "data_type": "geo_point"},
			map[string]any{"key": "photo", "label_vi": "Ảnh", "data_type": "text"},
			map[string]any{"key": "a1", "label_vi": "A", "data_type": "enum"},
			map[string]any{"key": "a1", "label_vi": "A", "data_type": "boolean", "min": "1"},
		},
	})
	expect(t, r, 422, "validation_failed")
	for _, fl := range []string{"fields[0].data_type", "fields[1].data_type", "fields[2].enum_values", "fields[3].key", "fields[3].min"} {
		if !r.fieldErr(fl) {
			t.Errorf("thiếu lỗi %s: %v", fl, r.body)
		}
	}
	// Collector không đề xuất được danh mục.
	col := f.as(f.tenant, f.t30001, port.RoleCollector)
	expect(t, col.adm(http.MethodPost, "/v1/mrv/catalog/versions", map[string]any{"evidence_type": "x_y_z"}), 403, "forbidden")
}

// KN US-MRV-01: bắt buộc ảnh + toạ độ; số thập phân dạng chuỗi; ngoài phạm vi xã bị từ chối.
func TestEvidenceValidationAndScope(t *testing.T) {
	f := newFixture(t)
	f.approveSeed(f.tenant)
	col := f.as(f.tenant, f.t30001, port.RoleCollector)

	b := awd("30001")
	delete(b, "file_refs")
	delete(b, "gps")
	r := col.app(http.MethodPost, "/v1/mrv/evidence", b)
	expect(t, r, 422, "validation_failed")
	if !r.fieldErr("file_refs") || !r.fieldErr("gps") {
		t.Fatalf("thiếu ảnh/toạ độ bắt buộc: %v", r.body)
	}
	if strings.Contains(r.str("detail")+strings.Join(nil, ""), "105.78") {
		t.Fatal("không được lộ toạ độ trong lỗi")
	}

	// Số thập phân gửi dạng số JSON (float) → từ chối; sai phạm vi; quá số chữ số lẻ; enum sai; khóa lạ.
	b = awd("30001")
	values(b)["awd_water_level_cm"] = 12.5
	values(b)["stage"] = "khong_co"
	values(b)["mystery"] = "x"
	r = col.app(http.MethodPost, "/v1/mrv/evidence", b)
	expect(t, r, 422, "validation_failed")
	for _, fl := range []string{"values.awd_water_level_cm", "values.stage", "values.mystery"} {
		if !r.fieldErr(fl) {
			t.Errorf("thiếu lỗi %s: %v", fl, r.body)
		}
	}
	for _, bad := range []string{"45", "-12.55", "1e2", "abc"} {
		b = awd("30001")
		values(b)["awd_water_level_cm"] = bad
		if r := col.app(http.MethodPost, "/v1/mrv/evidence", b); r.status != 422 || !r.fieldErr("values.awd_water_level_cm") {
			t.Errorf("%q phải bị từ chối: %d %v", bad, r.status, r.body)
		}
	}
	b = awd("30001")
	values(b)["awd_dry_days"] = 2.5
	if r := col.app(http.MethodPost, "/v1/mrv/evidence", b); !r.fieldErr("values.awd_dry_days") {
		t.Errorf("số ngày phải nguyên: %v", r.body)
	}
	// Trường bắt buộc thiếu.
	b = awd("30001")
	delete(values(b), "awd_dry_days")
	if r := col.app(http.MethodPost, "/v1/mrv/evidence", b); r.status != 422 || !r.fieldErr("values.awd_dry_days") {
		t.Errorf("thiếu trường bắt buộc: %v", r.body)
	}
	// Hợp lệ: decimal chuỗi trong phạm vi.
	b = awd("30001")
	values(b)["awd_water_level_cm"] = "-30"
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", b), 201, "")

	// Ngoài địa bàn: tổ xã 30001 ghi cho xã 30002.
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", awd("30002")), 403, "mrv.out_of_scope")
	// Thiếu vai.
	viewer := f.as(f.tenant, f.p89, port.RoleViewer)
	expect(t, viewer.app(http.MethodPost, "/v1/mrv/evidence", awd("30001")), 403, "forbidden")
}

// Mục 10.5: minh chứng gắn với nông hộ cần đồng ý mục đích mrv.evidence.
func TestConsentRequired(t *testing.T) {
	f := newFixture(t)
	f.approveSeed(f.tenant)
	col := f.as(f.tenant, f.t30001, port.RoleCollector)
	hh := ids.New()
	b := awd("30001")
	b["household_ref"] = hh.String()
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", b), 422, "mrv.consent_missing")

	// Đồng ý mục đích khác không đủ.
	f.consent.grant(f.tenant, hh, "registry.admin")
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", b), 422, "mrv.consent_missing")

	f.consent.grant(f.tenant, hh, port.ConsentPurpose)
	r := col.app(http.MethodPost, "/v1/mrv/evidence", b)
	expect(t, r, 201, "")
	if r.str("household_ref") != hh.String() {
		t.Fatalf("household_ref: %v", r.body)
	}
}

// Sửa sai bằng bản ghi bù: chỉ sửa được bản hiện hành, bắt buộc lý do; chuỗi giữ đủ lịch sử.
func TestCorrectionChain(t *testing.T) {
	f := newFixture(t)
	f.approveSeed(f.tenant)
	col := f.as(f.tenant, f.t30001, port.RoleCollector)
	e1 := col.app(http.MethodPost, "/v1/mrv/evidence", awd("30001"))
	expect(t, e1, 201, "")

	c := awd("30001")
	c["supersedes_id"] = e1.str("id")
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", c), 422, "validation_failed") // thiếu lý do
	c["correction_reason"] = "Đọc nhầm mực nước ống quan trắc"
	values(c)["awd_water_level_cm"] = "-8"
	e2 := col.app(http.MethodPost, "/v1/mrv/evidence", c)
	expect(t, e2, 201, "")
	if e2.str("root_id") != e1.str("id") || e2.num("chain_version") != 2 || e2.str("supersedes_id") != e1.str("id") {
		t.Fatalf("bản ghi bù: %v", e2.body)
	}
	// Sửa lại bản đã bị thay → 409.
	c2 := awd("30001")
	c2["supersedes_id"] = e1.str("id")
	c2["correction_reason"] = "Sửa lần nữa trên bản cũ"
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", c2), 409, "mrv.evidence_superseded")
	// Khác loại minh chứng → 422.
	c3 := awd("30001")
	c3["evidence_type"] = port.EvidenceFieldChecklist
	c3["supersedes_id"] = e2.str("id")
	c3["correction_reason"] = "Đổi sang loại khác"
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", c3), 422, "validation_failed")

	g := col.app(http.MethodGet, "/v1/mrv/evidence/"+e1.str("id"), nil)
	expect(t, g, 200, "")
	chain := g.body["chain"].([]any)
	if len(chain) != 2 || g.body["is_current"] != false || g.str("head_id") != e2.str("id") {
		t.Fatalf("lịch sử chuỗi: %v", g.body)
	}
	lst := col.app(http.MethodGet, "/v1/mrv/evidence", nil)
	items := lst.body["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != e2.str("id") {
		t.Fatalf("danh sách chỉ có bản hiện hành: %v", lst.body)
	}
	// Sổ vẫn còn bản gốc nguyên vẹn (append-only).
	var n int
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_mrv.evidence WHERE root_id = $1", uuid.MustParse(e1.str("id"))).Scan(&n)
	}); err != nil || n != 2 {
		t.Fatalf("sổ phải có 2 phiên bản, có %d (%v)", n, err)
	}
	if _, err := f.pool.Exec(context.Background(), "UPDATE pf_mrv.evidence SET season_code = 'x'"); err == nil {
		t.Fatal("vai mrv_app không được UPDATE sổ")
	}
}

// KN US-MRV-05: người duyệt ≠ người thu; mỗi phiên bản một quyết định; ngoài phạm vi 404.
func TestReviewerIsNotCapturer(t *testing.T) {
	f := newFixture(t)
	f.approveSeed(f.tenant)
	dual := f.as(f.tenant, f.p89, port.RoleCollector, port.RoleReviewer)
	e := dual.app(http.MethodPost, "/v1/mrv/evidence", awd("30001"))
	expect(t, e, 201, "")
	path := "/v1/mrv/evidence/" + e.str("id") + "/reviews"
	expect(t, dual.app(http.MethodPost, path, map[string]any{"decision": "accepted", "reason": "Đạt chuẩn"}), 403, "mrv.reviewer_is_capturer")

	// Người sửa sai (bản ghi bù) cũng không được duyệt chuỗi.
	fixer := f.as(f.tenant, f.t30001, port.RoleCollector, port.RoleReviewer)
	c := awd("30001")
	c["supersedes_id"] = e.str("id")
	c["correction_reason"] = "Bổ sung ảnh ống quan trắc"
	e2 := fixer.app(http.MethodPost, "/v1/mrv/evidence", c)
	expect(t, e2, 201, "")
	path2 := "/v1/mrv/evidence/" + e2.str("id") + "/reviews"
	expect(t, fixer.app(http.MethodPost, path2, map[string]any{"decision": "accepted", "reason": "Đạt chuẩn"}), 403, "mrv.reviewer_is_capturer")

	rev := f.as(f.tenant, f.p89, port.RoleReviewer)
	// Duyệt bản cũ đã bị thay → 409.
	expect(t, rev.app(http.MethodPost, path, map[string]any{"decision": "accepted", "reason": "Đạt chuẩn"}), 409, "mrv.evidence_superseded")
	expect(t, rev.app(http.MethodPost, path2, map[string]any{"decision": "maybe", "reason": "x"}), 422, "validation_failed")
	ok := rev.app(http.MethodPost, path2, map[string]any{"decision": "rejected", "reason": "Yêu cầu kiểm tra lại: ảnh mờ"})
	expect(t, ok, 201, "")
	if ok.str("reviewer_ref") != rev.actor.String() {
		t.Fatalf("review: %v", ok.body)
	}
	expect(t, rev.app(http.MethodPost, path2, map[string]any{"decision": "accepted", "reason": "Đổi ý"}), 409, "mrv.review_exists")

	pending := rev.app(http.MethodGet, "/v1/mrv/evidence?review_status=rejected", nil)
	if len(pending.body["items"].([]any)) != 1 {
		t.Fatalf("lọc theo trạng thái duyệt: %v", pending.body)
	}
	// Người duyệt tỉnh khác không thấy.
	other := f.as(f.tenant, ids.New(), port.RoleReviewer)
	f.scopes.set(other.org, authz.Scope{Units: []string{"92", "31001"}})
	expect(t, other.app(http.MethodPost, path2, map[string]any{"decision": "accepted", "reason": "Đạt chuẩn"}), 404, "mrv.evidence_not_found")
	expect(t, other.app(http.MethodGet, "/v1/mrv/evidence/"+e2.str("id"), nil), 404, "mrv.evidence_not_found")
}

// Gọi lặp POST cùng Idempotency-Key trả lại đúng phản hồi; id do máy sinh dùng lại cho bản khác → 409.
func TestIdempotentPost(t *testing.T) {
	f := newFixture(t)
	f.approveSeed(f.tenant)
	col := f.as(f.tenant, f.t30001, port.RoleCollector)
	b := awd("30001")
	b["id"] = ids.New().String()
	k := "k-" + ids.New().String()
	r1 := col.do(f.app.URL, http.MethodPost, "/v1/mrv/evidence", k, b)
	r2 := col.do(f.app.URL, http.MethodPost, "/v1/mrv/evidence", k, b)
	expect(t, r1, 201, "")
	if r2.status != 201 || r2.hdr.Get(idempotency.ReplayedHeader) != "true" || r2.str("id") != r1.str("id") {
		t.Fatalf("phát lại: %d %v %v", r2.status, r2.hdr, r2.body)
	}
	// Cùng id, khóa khác, nội dung khác.
	b2 := awd("30001")
	b2["id"] = b["id"]
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", b2), 409, "mrv.evidence_id_reused")
	var n int
	_ = tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_mrv.evidence").Scan(&n)
	})
	if n != 1 {
		t.Fatalf("chỉ được một bản ghi, có %d", n)
	}
}

// RB-2: dữ liệu, danh mục và gói của tenant này không lộ sang tenant khác.
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	f.approveSeed(f.tenant)
	col := f.as(f.tenant, f.t30001, port.RoleCollector)
	e := col.app(http.MethodPost, "/v1/mrv/evidence", awd("30001"))
	expect(t, e, 201, "")
	exp := f.as(f.tenant, f.national, port.RoleExporter)
	j := exp.app(http.MethodPost, "/v1/mrv/exports", map[string]any{"filters": map[string]any{}})
	expect(t, j, 202, "")

	other := ids.New()
	col2 := f.as(other, f.t30001, port.RoleCollector, port.RoleViewer)
	expect(t, col2.app(http.MethodGet, "/v1/mrv/evidence/"+e.str("id"), nil), 404, "mrv.evidence_not_found")
	if items := col2.app(http.MethodGet, "/v1/mrv/evidence", nil).body["items"].([]any); len(items) != 0 {
		t.Fatalf("tenant khác thấy minh chứng: %v", items)
	}
	expect(t, col2.app(http.MethodGet, "/v1/mrv/catalog?evidence_type=awd_log", nil), 404, "mrv.catalog_version_not_found")
	expect(t, col2.app(http.MethodPost, "/v1/mrv/evidence", awd("30001")), 422, "mrv.catalog_version_unavailable")
	exp2 := f.as(other, f.national, port.RoleExporter)
	expect(t, exp2.app(http.MethodGet, "/v1/mrv/exports/"+j.str("id"), nil), 404, "mrv.export_not_found")
	setter2 := f.as(other, uuid.Nil, authz.RoleParamSetter)
	if items := setter2.adm(http.MethodGet, "/v1/mrv/catalog/versions", nil).body["items"].([]any); len(items) != 0 {
		t.Fatalf("tenant khác thấy danh mục: %v", items)
	}
}

// requestedEvent đọc sự kiện pf.mrv.export.requested của một job từ outbox.
func (f *fixture) requestedEvent(tid uuid.UUID, exportID string) outbox.Envelope {
	f.t.Helper()
	var e outbox.Envelope
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT id, tenant_id, event_type, schema_version, occurred_at, recorded_at,
		    producer, payload FROM shared.outbox WHERE event_type = $1 AND payload->>'export_id' = $2`,
			port.EventExportRequested, exportID).
			Scan(&e.EventID, &e.TenantID, &e.EventType, &e.SchemaVersion, &e.OccurredAt, &e.RecordedAt, &e.Producer, &e.Payload)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return e
}

// Mục 12 + 16: POST → 202 + job; worker (inbox) dựng gói; giao lặp → một gói; băm
// ảnh chụp dựng lại được từ sổ kể cả khi dữ liệu thay đổi sau đó.
func TestExportEndToEnd(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.approveSeed(f.tenant)
	col1 := f.as(f.tenant, f.p89, port.RoleCollector)
	e1 := col1.app(http.MethodPost, "/v1/mrv/evidence", awd("30001"))
	e2 := col1.app(http.MethodPost, "/v1/mrv/evidence", awd("30002"))
	expect(t, e1, 201, "")
	expect(t, e2, 201, "")
	rev := f.as(f.tenant, f.p89, port.RoleReviewer)
	expect(t, rev.app(http.MethodPost, "/v1/mrv/evidence/"+e1.str("id")+"/reviews", map[string]any{"decision": "accepted", "reason": "Đạt chuẩn"}), 201, "")

	exp := f.as(f.tenant, f.p89, port.RoleExporter)
	// Ngoài phạm vi → 403; tổ xã chỉ xuất được xã mình.
	expect(t, exp.app(http.MethodPost, "/v1/mrv/exports", map[string]any{"filters": map[string]any{"admin_unit_codes": []string{"31001"}}}), 403, "mrv.out_of_scope")

	k := "k-" + ids.New().String()
	body := map[string]any{"filters": map[string]any{"evidence_type": "awd_log"}}
	j := exp.do(f.app.URL, http.MethodPost, "/v1/mrv/exports", k, body)
	expect(t, j, 202, "")
	id := j.str("id")
	if j.hdr.Get("Location") != "/v1/mrv/exports/"+id || j.str("status") != "queued" {
		t.Fatalf("202 + Location: %v %v", j.hdr, j.body)
	}
	jr := exp.do(f.app.URL, http.MethodPost, "/v1/mrv/exports", k, body)
	if jr.status != 202 || jr.str("id") != id || jr.hdr.Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("POST lặp: %d %v", jr.status, jr.body)
	}
	expect(t, exp.app(http.MethodGet, "/v1/mrv/exports/"+id+"/package", nil), 409, "mrv.export_not_ready")

	// Worker: giao cùng sự kiện hai lần → xử lý một lần (R-61).
	env := f.requestedEvent(f.tenant, id)
	h := f.mod.ExportHandler()
	for i := 0; i < 2; i++ {
		processed, err := inbox.Process(ctx, f.pool, port.ExportConsumer, env, h)
		if err != nil {
			t.Fatal(err)
		}
		if processed != (i == 0) {
			t.Fatalf("lần giao %d: processed=%v", i+1, processed)
		}
	}
	// Một sự kiện khác (vd publisher phát lại với event_id mới) cho cùng job → không tạo gói thứ hai.
	env2 := env
	env2.EventID = ids.New()
	if _, err := inbox.Process(ctx, f.pool, port.ExportConsumer, env2, h); err != nil {
		t.Fatal(err)
	}
	var packages, completed int
	_ = tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM pf_mrv.export_package WHERE export_id = $1", uuid.MustParse(id)).Scan(&packages); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT count(*) FROM shared.outbox WHERE event_type = $1 AND payload->>'export_id' = $2",
			port.EventExportCompleted, id).Scan(&completed)
	})
	if packages != 1 || completed != 1 {
		t.Fatalf("mong đúng 1 gói và 1 sự kiện completed, có %d và %d", packages, completed)
	}

	st := exp.app(http.MethodGet, "/v1/mrv/exports/"+id, nil)
	if st.str("status") != "completed" || st.num("evidence_count") != 2 || st.str("object_key") != "packages/"+f.tenant.String()+"/"+id+".json" {
		t.Fatalf("trạng thái job: %v", st.body)
	}
	pk := exp.app(http.MethodGet, "/v1/mrv/exports/"+id+"/package", nil)
	expect(t, pk, 200, "")
	if pk.str("url") == "" || pk.str("package_sha256") != st.str("package_sha256") {
		t.Fatalf("URL gói: %v", pk.body)
	}

	// Gói trong kho: băm tệp = package_sha256; băm phần snapshot chuẩn hóa = snapshot_sha256.
	rc, info, err := f.store.Get(ctx, st.str("object_key"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(rc)
	_ = rc.Close()
	if objstore.SHA256Hex(raw) != st.str("package_sha256") || info.SHA256 != st.str("package_sha256") {
		t.Fatal("băm gói không khớp")
	}
	var pkg domain.Package
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatal(err)
	}
	var snap domain.Snapshot
	if err := json.Unmarshal(pkg.Snapshot, &snap); err != nil {
		t.Fatal(err)
	}
	_, sum, err := snap.Canonical()
	if err != nil || sum != st.str("snapshot_sha256") || pkg.SnapshotSHA256 != sum || !pkg.Provisional {
		t.Fatalf("băm ảnh chụp: %s vs %s (%v), provisional=%v", sum, st.str("snapshot_sha256"), err, pkg.Provisional)
	}
	if len(snap.Evidence) != 2 || len(snap.Catalogs) != 1 || snap.Catalogs[0].Version != 1 || len(snap.Catalogs[0].Fields) == 0 {
		t.Fatalf("nội dung ảnh chụp: %+v", snap.Catalogs)
	}
	accepted := 0
	for _, e := range snap.Evidence {
		if e.Review != nil && e.Review.Decision == "accepted" {
			accepted++
		}
		if e.GPS == nil || e.RowHash == "" || len(e.FileRefs) != 1 {
			t.Fatalf("minh chứng trong gói thiếu toạ độ/row_hash/tệp: %+v", e.ID)
		}
	}
	if accepted != 1 {
		t.Fatalf("mong 1 minh chứng đã duyệt trong gói, có %d", accepted)
	}

	// Dữ liệu đổi SAU as_of (bản ghi bù, bản mới, quyết định duyệt) → dựng lại vẫn ra cùng băm.
	c := awd("30002")
	c["supersedes_id"] = e2.str("id")
	c["correction_reason"] = "Cập nhật mực nước sau xuất gói"
	expect(t, col1.app(http.MethodPost, "/v1/mrv/evidence", c), 201, "")
	expect(t, col1.app(http.MethodPost, "/v1/mrv/evidence", awd("30001")), 201, "")
	v := exp.app(http.MethodGet, "/v1/mrv/exports/"+id+"/verify", nil)
	if v.status != 200 || v.body["match"] != true || v.str("rebuilt_sha256") != st.str("snapshot_sha256") {
		t.Fatalf("dựng lại ảnh chụp: %v", v.body)
	}
	// Job đã xong là bất biến.
	if _, err := f.pool.Exec(ctx, "UPDATE pf_mrv.export_package SET size_bytes = 1"); err == nil {
		t.Fatal("sổ gói phải bất biến")
	}

	// Tổ xã 30001 không thấy job phủ cả 30002.
	team := f.as(f.tenant, f.t30001, port.RoleExporter)
	expect(t, team.app(http.MethodGet, "/v1/mrv/exports/"+id, nil), 404, "mrv.export_not_found")
}

// Sync (docs/dev/sync-contract.md): accepted / duplicate / rejected theo từng bản ghi.
func TestSyncSink(t *testing.T) {
	f := newFixture(t)
	f.approveSeed(f.tenant)
	actor := ids.New()
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: actor, TenantID: f.tenant, Roles: []string{port.RoleCollector}, OrgRef: f.t30001})
	ctx = tenant.WithID(ctx, f.tenant)
	sink := f.mod.SyncSink()
	if sink.Entities()[0] != "evidence" {
		t.Fatal(sink.Entities())
	}
	scope := authz.Scope{Units: []string{"30001"}}
	device := ids.New()
	payload := func(b map[string]any) json.RawMessage {
		delete(b, "event_time")
		raw, _ := json.Marshal(b)
		return raw
	}
	rec := func(b map[string]any, seq int) syncport.Record {
		return syncport.Record{ID: ids.New(), IdemKey: "dev:" + device.String() + ":" + string(rune('0'+seq)), Entity: "evidence",
			Op: "create", EventTime: time.Now().Add(-2 * time.Hour), DeviceID: device, Payload: payload(b)}
	}

	r1 := rec(awd("30001"), 1)
	if res := sink.Accept(ctx, scope, r1); res.Status != "accepted" || res.ID != r1.ID {
		t.Fatalf("accepted: %+v", res)
	}
	if res := sink.Accept(ctx, scope, r1); res.Status != "duplicate" {
		t.Fatalf("duplicate: %+v", res)
	}
	bad := awd("30001")
	values(bad)["awd_water_level_cm"] = 3.5
	res := sink.Accept(ctx, scope, rec(bad, 2))
	if res.Status != "rejected" || res.Code != "validation_failed" || len(res.Errors) == 0 || res.Errors[0].Field != "values.awd_water_level_cm" {
		t.Fatalf("rejected validation: %+v", res)
	}
	hh := awd("30001")
	hh["household_ref"] = ids.New().String()
	if res := sink.Accept(ctx, scope, rec(hh, 3)); res.Status != "rejected" || res.Code != "mrv.consent_missing" {
		t.Fatalf("rejected consent: %+v", res)
	}
	if res := sink.Accept(ctx, scope, rec(awd("30002"), 4)); res.Status != "rejected" || res.Code != "mrv.out_of_scope" {
		t.Fatalf("rejected scope: %+v", res)
	}
	// Bản ghi bù ngoại tuyến.
	corr := rec(awd("30001"), 5)
	corr.Op = "correct"
	corr.SupersedesID = &r1.ID
	reason := "Sửa mực nước đo sai ngoài ruộng"
	corr.CorrectionReason = &reason
	if res := sink.Accept(ctx, scope, corr); res.Status != "accepted" {
		t.Fatalf("correct: %+v", res)
	}
	noReason := rec(awd("30001"), 6)
	noReason.Op = "correct"
	noReason.SupersedesID = &corr.ID
	if res := sink.Accept(ctx, scope, noReason); res.Status != "rejected" || res.Code != "validation_failed" {
		t.Fatalf("correct thiếu lý do: %+v", res)
	}
}
