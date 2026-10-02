package handler_test

// KN v7.7 FN-XA-03.03 — Quản lý HTX & nông hộ (hợp đồng P7 §1): route /v1/kn/cooperatives*, /v1/kn/households*
// với ngữ cảnh phiên giả (header X-AG-Context → authz.KNContext, như middleware của P1).

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type knFix struct {
	*fixture
	srv  *httptest.Server
	tr   tree
	mu   sync.Mutex
	ctxs map[uuid.UUID]*authz.KNContext
}

func newKNFix(t *testing.T) *knFix {
	f := newFixture(t)
	kf := &knFix{fixture: f, ctxs: map[uuid.UUID]*authz.KNContext{}}
	ops := f.env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	f.mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), kf.mw(), idempotency.Middleware(ops)))
	kf.srv = httptest.NewServer(eng)
	t.Cleanup(kf.srv.Close)
	kf.tr = f.seedTree(f.root())
	return kf
}

// mw giả lập middleware X-AG-Context của P1.
func (kf *knFix) mw() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader(authz.ContextHeader)
		if raw == "" {
			c.Next()
			return
		}
		p, ok := authz.FromContext(c.Request.Context())
		id, err := uuid.Parse(raw)
		kf.mu.Lock()
		kc := kf.ctxs[id]
		kf.mu.Unlock()
		if !ok || err != nil || kc == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		cp := *kc
		if cp.Support != nil {
			s := *cp.Support
			s.Reason = c.GetHeader(authz.ReasonHeader)
			cp.Support = &s
		}
		p.KN = &cp
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
}

type knUser struct {
	kf     *knFix
	token  string
	actor  uuid.UUID
	ctxID  uuid.UUID
	reason string
}

func (kf *knFix) user(tid uuid.UUID, role string, communes []string, sup *authz.KNSupport) knUser {
	actor := ids.New()
	tok := kf.iss.Sign(kf.t, authztest.Token{Subject: actor, TenantID: tid, Roles: []string{"kn_user"}, MFA: true})
	kc := &authz.KNContext{ID: ids.New(), Role: role, Department: "trong_trot", Communes: communes, Support: sup}
	switch role {
	case authz.KNXa:
		kc.Scope = authz.Scope{Units: communes}
	case authz.KNTinh:
		kc.Scope = authz.Scope{Units: []string{"89", "30001", "30002"}}
	default:
		kc.Scope = authz.Scope{All: true}
	}
	if role == authz.KNAdmin {
		kc.Department = ""
	}
	kf.mu.Lock()
	kf.ctxs[kc.ID] = kc
	kf.mu.Unlock()
	return knUser{kf: kf, token: tok, actor: actor, ctxID: kc.ID}
}

type kresp struct {
	resp
	raw string
}

func (u knUser) do(method, path, idem string, body any) kresp {
	u.kf.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, u.kf.srv.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+u.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(authz.ContextHeader, u.ctxID.String())
	if u.reason != "" {
		req.Header.Set(authz.ReasonHeader, u.reason)
	}
	if idem == "" && method == http.MethodPost {
		idem = key()
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		u.kf.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return kresp{resp: resp{status: r.StatusCode, hdr: r.Header, body: m}, raw: string(raw)}
}

func (u knUser) get(path string) kresp { return u.do(http.MethodGet, path, "", nil) }

func (kf *knFix) countWhere(tid uuid.UUID, q string, args ...any) int {
	kf.t.Helper()
	var n int
	if err := tenant.WithTenantTx(context.Background(), kf.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), q, args...).Scan(&n)
	}); err != nil {
		kf.t.Fatal(err)
	}
	return n
}

func noPhone(t *testing.T, r kresp, phones ...string) {
	t.Helper()
	for _, p := range phones {
		if strings.Contains(r.raw, p) {
			t.Fatalf("phản hồi lộ SĐT đầy đủ %s: %s", p, r.raw)
		}
	}
}

func logChange(t *testing.T, r kresp, event, field string) (old, nw any) {
	t.Helper()
	for _, it := range items(r.resp) {
		if it["event"] != event {
			continue
		}
		if o, n, ok := changeOf(it, field); ok {
			return o, n
		}
	}
	t.Fatalf("Log không có %s.%s: %s", event, field, r.raw)
	return nil, nil
}

type fakeMachineCounts struct{ n int }

func (f fakeMachineCounts) CoopMachineTotal(context.Context, uuid.UUID) (int, error) { return f.n, nil }

// FN-XA-03.03: danh sách/chi tiết HTX theo phạm vi ngữ cảnh phiên (Cấp Xã chỉ xã phụ trách, Cấp Tỉnh trong
// tỉnh, Cấp Quốc gia toàn quốc, chỉ xem); thêm HTX (gọi lặp cùng Idempotency-Key → cùng HTX, một dòng sổ); trùng
// tên → 409; sửa = sự kiện bù có Log cũ → mới, không PII; SĐT người đại diện luôn che; cách ly tenant.
func TestFN_XA_03_03_CooperativeScopeCreateUpdateLog(t *testing.T) {
	kf := newKNFix(t)
	tr := kf.tr
	xa := kf.user(kf.tenant, authz.KNXa, []string{"30001"}, nil)
	tinh := kf.user(kf.tenant, authz.KNTinh, nil, nil)
	tw := kf.user(kf.tenant, authz.KNTW, nil, nil)

	// Phạm vi đọc.
	r := xa.get("/v1/kn/cooperatives")
	wantStatus(t, r.resp, http.StatusOK, "Cấp Xã liệt kê")
	if got := refs(r.resp, "ref"); !slices.Equal(got, []string{tr.c1.String()}) {
		t.Fatalf("Cấp Xã chỉ thấy HTX xã 30001: %v", got)
	}
	if it := items(r.resp)[0]; it["area_ha"] != "120.5" || it["commune_code"] != "30001" || it["household_count"].(float64) != 0 {
		t.Fatalf("HTX: %v", it)
	}
	wantCode(t, xa.get("/v1/kn/cooperatives/"+tr.c2.String()).resp, http.StatusNotFound, "party.org_not_found", "HTX xã khác")
	wantCode(t, xa.get("/v1/kn/cooperatives?commune=31001").resp, http.StatusForbidden, "party.out_of_scope", "lọc xã ngoài phạm vi")
	if got := refs(tinh.get("/v1/kn/cooperatives").resp, "ref"); !slices.Contains(got, tr.c1.String()) || slices.Contains(got, tr.c2.String()) {
		t.Fatalf("Cấp Tỉnh 89: %v", got)
	}
	if got := refs(tw.get("/v1/kn/cooperatives").resp, "ref"); len(got) != 2 {
		t.Fatalf("Cấp Quốc gia: %v", got)
	}
	if got := refs(xa.get("/v1/kn/cooperatives?area_min=200").resp, "ref"); len(got) != 0 {
		t.Fatalf("lọc diện tích: %v", got)
	}

	// Không có ngữ cảnh phiên → 428.
	noCtx := kf.as(kf.tenant, uuid.Nil, "kn_user")
	wantCode(t, noCtx.call(kf.srv.URL, http.MethodGet, "/v1/kn/cooperatives", "", "application/json", nil), http.StatusPreconditionRequired,
		"accounts.context_required", "thiếu X-AG-Context")

	body := map[string]any{"name": "HTX Mới Xã A", "commune_code": "30001", "area_ha": "50.25", "tax_code": "0312345678",
		"head_office": "Ấp 1, xã A", "representative": map[string]any{"full_name": "Trần Văn Bình", "phone": "0912345678"}}
	// Chỉ xem: Cấp Tỉnh, Cấp Quốc gia.
	wantCode(t, tinh.do(http.MethodPost, "/v1/kn/cooperatives", "", body).resp, http.StatusForbidden, "party.read_only", "Cấp Tỉnh thêm HTX")
	wantCode(t, tw.do(http.MethodPost, "/v1/kn/cooperatives", "", body).resp, http.StatusForbidden, "party.read_only", "Cấp Quốc gia thêm HTX")
	other := map[string]any{"name": "HTX Xã B", "commune_code": "30002"}
	wantCode(t, xa.do(http.MethodPost, "/v1/kn/cooperatives", "", other).resp, http.StatusForbidden, "party.out_of_scope", "thêm HTX xã khác")

	// Gọi lặp cùng khóa → cùng HTX, một dòng sổ.
	k := key()
	r1 := xa.do(http.MethodPost, "/v1/kn/cooperatives", k, body)
	wantStatus(t, r1.resp, http.StatusCreated, "thêm HTX")
	r2 := xa.do(http.MethodPost, "/v1/kn/cooperatives", k, body)
	if str(r2.body, "ref") != str(r1.body, "ref") || r2.hdr.Get("Idempotent-Replayed") == "" {
		t.Fatalf("gọi lặp: %s / %s", r1.raw, r2.raw)
	}
	ref := str(r1.body, "ref")
	if n := kf.countWhere(kf.tenant, "SELECT count(*) FROM pf_party.org_event WHERE org_ref = $1", uuid.MustParse(ref)); n != 1 {
		t.Fatalf("sổ org_event sau gọi lặp: %d", n)
	}
	if !strings.HasPrefix(str(r1.body, "code"), "HTX") || str(r1.body, "representative", "label") == "" ||
		!strings.HasSuffix(str(r1.body, "representative", "masked_phone"), "678") {
		t.Fatalf("HTX mới: %s", r1.raw)
	}
	noPhone(t, r1, "0912345678")
	wantCode(t, xa.do(http.MethodPost, "/v1/kn/cooperatives", "", map[string]any{"name": "htx mới xã a", "commune_code": "30001"}).resp,
		http.StatusConflict, "party.cooperative_name_taken", "trùng tên")

	// Sửa: sự kiện bù, gọi lặp PUT cùng khóa → một dòng sổ.
	upd := map[string]any{"name": "HTX Mới Xã A (đổi tên)", "area_ha": "60", "reason": "Cập nhật theo biên bản"}
	pk := key()
	u1 := xa.do(http.MethodPut, "/v1/kn/cooperatives/"+ref, pk, upd)
	wantStatus(t, u1.resp, http.StatusOK, "sửa HTX")
	u2 := xa.do(http.MethodPut, "/v1/kn/cooperatives/"+ref, pk, map[string]any{"area_ha": "70"})
	wantStatus(t, u2.resp, http.StatusOK, "sửa lặp")
	if str(u2.body, "area_ha") != "60" || str(u2.body, "name") != "HTX Mới Xã A (đổi tên)" {
		t.Fatalf("PUT lặp phải trả kết quả lần đầu: %s", u2.raw)
	}
	if n := kf.countIn(kf.tenant, "pf_party.kn_profile_event"); n != 1 {
		t.Fatalf("sổ kn_profile_event sau PUT lặp: %d", n)
	}
	// Đổi người đại diện: sổ chỉ ghi representative_ref.
	u3 := xa.do(http.MethodPut, "/v1/kn/cooperatives/"+ref, key(), map[string]any{"representative": map[string]any{"full_name": "Lê Thị Cúc", "phone": "0987654321"}})
	wantStatus(t, u3.resp, http.StatusOK, "đổi người đại diện")
	noPhone(t, u3, "0987654321")
	// Không thay đổi → không ghi sổ.
	wantStatus(t, xa.do(http.MethodPut, "/v1/kn/cooperatives/"+ref, key(), map[string]any{"area_ha": "60"}).resp, http.StatusOK, "không đổi")
	if n := kf.countIn(kf.tenant, "pf_party.kn_profile_event"); n != 2 {
		t.Fatalf("sổ sau PUT không đổi: %d", n)
	}
	wantCode(t, xa.do(http.MethodPut, "/v1/kn/cooperatives/"+ref, key(), map[string]any{"area_ha": "-1"}).resp, http.StatusUnprocessableEntity,
		"validation_failed", "diện tích âm")
	wantCode(t, tw.do(http.MethodPut, "/v1/kn/cooperatives/"+ref, key(), upd).resp, http.StatusForbidden, "party.read_only", "Cấp Quốc gia sửa")
	wantCode(t, xa.do(http.MethodPut, "/v1/kn/cooperatives/"+tr.c2.String(), key(), upd).resp, http.StatusNotFound, "party.org_not_found", "sửa HTX xã khác")

	lg := xa.get("/v1/kn/cooperatives/" + ref + "/log")
	wantStatus(t, lg.resp, http.StatusOK, "Log HTX")
	if o, n := logChange(t, lg, "updated", "area_ha"); o != "50.25" || n != "60" {
		t.Fatalf("Log diện tích: %v → %v", o, n)
	}
	if o, n := logChange(t, lg, "updated", "name"); o != "HTX Mới Xã A" || n != "HTX Mới Xã A (đổi tên)" {
		t.Fatalf("Log tên: %v → %v", o, n)
	}
	if o, n := logChange(t, lg, "updated", "representative_ref"); o == nil || n == nil || o == n {
		t.Fatalf("Log người đại diện: %v → %v", o, n)
	}
	if _, n := logChange(t, lg, "created", "name"); n != "HTX Mới Xã A" {
		t.Fatalf("Log tạo: %v", n)
	}
	noPhone(t, lg, "0912345678", "0987654321")
	if strings.Contains(lg.raw, "Trần Văn Bình") || strings.Contains(lg.raw, "Lê Thị Cúc") {
		t.Fatalf("Log lộ họ tên: %s", lg.raw)
	}
	if items(lg.resp)[1]["reason"] != "Cập nhật theo biên bản" {
		t.Fatalf("lý do sửa: %s", lg.raw)
	}

	// Cách ly tenant.
	otherTenant := kf.user(ids.New(), authz.KNXa, []string{"30001"}, nil)
	wantCode(t, otherTenant.get("/v1/kn/cooperatives/"+ref).resp, http.StatusNotFound, "party.org_not_found", "tenant khác")
	if got := items(otherTenant.get("/v1/kn/cooperatives").resp); len(got) != 0 {
		t.Fatalf("tenant khác thấy HTX: %v", got)
	}
}

// FN-XA-03.03: nông hộ của HTX (thêm, sửa diện tích = sự kiện bù có Log, SĐT chủ hộ che); vô hiệu hóa thay xóa
// cứng, kèm cảnh báo dữ liệu liên kết (hộ, máy); HTX đã vô hiệu hóa không sửa được; kích hoạt lại; chế độ hỗ trợ
// Admin bắt buộc lý do, lưu on_behalf_of.
func TestFN_XA_03_03_HouseholdsDeactivateSupport(t *testing.T) {
	kf := newKNFix(t)
	c1 := kf.tr.c1.String()
	xa := kf.user(kf.tenant, authz.KNXa, []string{"30001"}, nil)
	xaB := kf.user(kf.tenant, authz.KNXa, []string{"30002"}, nil)
	tw := kf.user(kf.tenant, authz.KNTW, nil, nil)
	kf.mod.UseMachineCounts(fakeMachineCounts{n: 3})

	hb := map[string]any{"full_name": "Nguyễn Văn An", "phone": "0901234567", "cultivated_area_ha": "1.5"}
	wantCode(t, xaB.do(http.MethodPost, "/v1/kn/cooperatives/"+c1+"/households", "", hb).resp, http.StatusNotFound, "party.org_not_found", "hộ HTX xã khác")
	wantCode(t, tw.do(http.MethodPost, "/v1/kn/cooperatives/"+c1+"/households", "", hb).resp, http.StatusForbidden, "party.read_only", "TW thêm hộ")
	k := key()
	h1 := xa.do(http.MethodPost, "/v1/kn/cooperatives/"+c1+"/households", k, hb)
	wantStatus(t, h1.resp, http.StatusCreated, "thêm hộ")
	h2 := xa.do(http.MethodPost, "/v1/kn/cooperatives/"+c1+"/households", k, hb)
	if str(h2.body, "ref") != str(h1.body, "ref") {
		t.Fatalf("gọi lặp thêm hộ: %s / %s", h1.raw, h2.raw)
	}
	noPhone(t, h1, "0901234567")
	if !strings.HasSuffix(str(h1.body, "holder", "masked_phone"), "567") || str(h1.body, "cultivated_area_ha") != "1.5" {
		t.Fatalf("hộ: %s", h1.raw)
	}
	wantCode(t, xa.do(http.MethodPost, "/v1/kn/cooperatives/"+c1+"/households", "", hb).resp, http.StatusConflict,
		"party.household_phone_taken", "trùng SĐT")
	hh := str(h1.body, "ref")
	lst := xa.get("/v1/kn/cooperatives/" + c1 + "/households")
	if got := refs(lst.resp, "ref"); !slices.Equal(got, []string{hh}) {
		t.Fatalf("danh sách hộ: %s", lst.raw)
	}
	noPhone(t, lst, "0901234567")

	pk := key()
	wantStatus(t, xa.do(http.MethodPut, "/v1/kn/households/"+hh, pk, map[string]any{"cultivated_area_ha": "2", "reason": "Đo lại"}).resp,
		http.StatusOK, "sửa diện tích hộ")
	again := xa.do(http.MethodPut, "/v1/kn/households/"+hh, pk, map[string]any{"cultivated_area_ha": "9"})
	if str(again.body, "cultivated_area_ha") != "2" {
		t.Fatalf("PUT lặp hộ: %s", again.raw)
	}
	if n := kf.countIn(kf.tenant, "pf_party.kn_profile_event"); n != 1 {
		t.Fatalf("sổ hộ sau PUT lặp: %d", n)
	}
	wantCode(t, xaB.do(http.MethodPut, "/v1/kn/households/"+hh, key(), map[string]any{"cultivated_area_ha": "3"}).resp, http.StatusNotFound,
		"party.household_not_found", "sửa hộ xã khác")
	hl := xa.get("/v1/kn/households/" + hh + "/log")
	wantStatus(t, hl.resp, http.StatusOK, "Log hộ")
	if o, n := logChange(t, hl, "updated", "cultivated_area_ha"); o != "1.5" || n != "2" {
		t.Fatalf("Log hộ: %v → %v", o, n)
	}
	if _, n := logChange(t, hl, "created", "cultivated_area_ha"); n != "1.5" {
		t.Fatalf("Log tạo hộ: %v", n)
	}
	noPhone(t, hl, "0901234567")

	// Vô hiệu hóa thay xóa cứng.
	if r := xa.do(http.MethodDelete, "/v1/kn/cooperatives/"+c1, "", nil); r.status != http.StatusNotFound && r.status != http.StatusMethodNotAllowed {
		t.Fatalf("không có xóa cứng: %d", r.status)
	}
	wantCode(t, xa.do(http.MethodPost, "/v1/kn/cooperatives/"+c1+"/deactivate", "", map[string]any{"reason": "ngắn"}).resp,
		http.StatusUnprocessableEntity, "validation_failed", "lý do ngắn")
	wantCode(t, tw.do(http.MethodPost, "/v1/kn/cooperatives/"+c1+"/deactivate", "", map[string]any{"reason": "HTX giải thể theo quyết định của UBND xã"}).resp,
		http.StatusForbidden, "party.read_only", "TW vô hiệu hóa")
	d := xa.do(http.MethodPost, "/v1/kn/cooperatives/"+c1+"/deactivate", "", map[string]any{"reason": "HTX giải thể theo quyết định của UBND xã"})
	wantStatus(t, d.resp, http.StatusOK, "vô hiệu hóa")
	w := d.body["warnings"].(map[string]any)
	if w["households"].(float64) != 1 || w["machines"].(float64) != 3 || w["plots"] != nil {
		t.Fatalf("cảnh báo: %s", d.raw)
	}
	if d.body["cooperative"].(map[string]any)["active"] != false {
		t.Fatalf("HTX còn hoạt động: %s", d.raw)
	}
	if got := refs(xa.get("/v1/kn/cooperatives").resp, "ref"); len(got) != 0 {
		t.Fatalf("mặc định chỉ HTX đang hoạt động: %v", got)
	}
	if got := refs(xa.get("/v1/kn/cooperatives?status=inactive").resp, "ref"); !slices.Equal(got, []string{c1}) {
		t.Fatalf("lọc ngừng hoạt động: %v", got)
	}
	wantCode(t, xa.do(http.MethodPut, "/v1/kn/cooperatives/"+c1, key(), map[string]any{"area_ha": "1"}).resp, http.StatusConflict,
		"party.org_state_conflict", "sửa HTX đã vô hiệu hóa")
	wantStatus(t, xa.do(http.MethodPost, "/v1/kn/cooperatives/"+c1+"/reactivate", "", map[string]any{"reason": "UBND xã hủy quyết định"}).resp,
		http.StatusOK, "kích hoạt lại")
	lg := xa.get("/v1/kn/cooperatives/" + c1 + "/log")
	if o, n := logChange(t, lg, "deactivated", "active"); o != "true" || n != "false" {
		t.Fatalf("Log vô hiệu hóa: %s", lg.raw)
	}

	// Chế độ hỗ trợ Admin.
	sup := kf.user(kf.tenant, authz.KNXa, []string{"30001"}, &authz.KNSupport{SessionID: ids.New(), OnBehalfOf: xa.actor})
	wantCode(t, sup.do(http.MethodPut, "/v1/kn/cooperatives/"+c1, key(), map[string]any{"area_ha": "130"}).resp, http.StatusUnprocessableEntity,
		"party.support_reason_required", "hỗ trợ thiếu lý do")
	sup.reason = "Hỗ trợ cán bộ xã nhập số liệu"
	wantStatus(t, sup.do(http.MethodPut, "/v1/kn/cooperatives/"+c1, key(), map[string]any{"area_ha": "130"}).resp, http.StatusOK, "hỗ trợ sửa")
	if n := kf.countWhere(kf.tenant, "SELECT count(*) FROM pf_party.kn_profile_event WHERE on_behalf_of = $1 AND support_reason IS NOT NULL", xa.actor); n != 1 {
		t.Fatalf("on_behalf_of: %d", n)
	}
}
