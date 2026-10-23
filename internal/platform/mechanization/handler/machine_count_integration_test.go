package handler_test

// KN v7.7 FN-XA-03.04 — Máy móc HTX (hợp đồng P7 §2): khai báo số lượng theo loại máy của HTX, tổng theo xã,
// lịch sử số lượng. Ngữ cảnh phiên giả (header X-AG-Context → authz.KNContext, như middleware của P1).

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type mcFix struct {
	*fixture
	srv  *httptest.Server
	mu   sync.Mutex
	ctxs map[uuid.UUID]*authz.KNContext
}

func newMCFix(t *testing.T) *mcFix {
	f := newFixture(t, defaultNow)
	m := &mcFix{fixture: f, ctxs: map[uuid.UUID]*authz.KNContext{}}
	ops := pgtest.Get(t).Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	eng.Use(logErrors(t))
	f.mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), m.mw(), idempotency.Middleware(ops)))
	m.srv = httptest.NewServer(eng)
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mcFix) mw() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader(authz.ContextHeader)
		if raw == "" {
			c.Next()
			return
		}
		p, ok := authz.FromContext(c.Request.Context())
		id, err := uuid.Parse(raw)
		m.mu.Lock()
		kc := m.ctxs[id]
		m.mu.Unlock()
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

type mcUser struct {
	m      *mcFix
	token  string
	actor  uuid.UUID
	ctxID  uuid.UUID
	reason string
}

func (m *mcFix) user(tid uuid.UUID, role string, communes []string, sup *authz.KNSupport) mcUser {
	actor := ids.New()
	tok := m.iss.Sign(m.t, authztest.Token{Subject: actor, TenantID: tid, Roles: []string{"kn_user"}, MFA: true})
	kc := &authz.KNContext{ID: ids.New(), Role: role, Department: "trong_trot", Communes: communes, Support: sup}
	switch role {
	case authz.KNXa:
		kc.Scope = authz.Scope{Units: communes}
		kc.OrgRef = ids.New() // như P1: lần gán Cấp Xã luôn có Nút (db.ForModule đòi Nút — FD)
	case authz.KNTinh:
		kc.Scope = authz.Scope{Units: []string{"89", "30001", "30002"}}
	default:
		kc.Scope = authz.Scope{All: true}
	}
	if role == authz.KNAdmin {
		kc.Department = ""
	}
	m.mu.Lock()
	m.ctxs[kc.ID] = kc
	m.mu.Unlock()
	return mcUser{m: m, token: tok, actor: actor, ctxID: kc.ID}
}

func (u mcUser) do(method, path, idem string, body any) resp {
	u.m.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, u.m.srv.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+u.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(authz.ContextHeader, u.ctxID.String())
	if u.reason != "" {
		req.Header.Set(authz.ReasonHeader, u.reason)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		u.m.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var mm map[string]any
	_ = json.Unmarshal(raw, &mm)
	return resp{status: r.StatusCode, hdr: r.Header, body: mm}
}

func (u mcUser) put(coop uuid.UUID, idem string, body any) resp {
	return u.do(http.MethodPut, "/v1/kn/machine-counts/"+coop.String(), idem, body)
}

func (u mcUser) get(path string) resp { return u.do(http.MethodGet, path, "", nil) }

func mcWant(t *testing.T, r resp, status int, code, what string) {
	t.Helper()
	if r.status != status || (code != "" && str(r.body, "code") != code) {
		t.Fatalf("%s: muốn %d %s, nhận %d %v", what, status, code, r.status, r.body)
	}
}

func line(t, q any) map[string]any { return map[string]any{"machine_type": t, "quantity": q} }

func (m *mcFix) count(tid uuid.UUID, q string, args ...any) int {
	m.t.Helper()
	var n int
	if err := tenant.WithTenantTx(context.Background(), m.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), q, args...).Scan(&n)
	}); err != nil {
		m.t.Fatal(err)
	}
	return n
}

// communeTotal đọc tổng của xã theo loại máy trong commune_totals.
func communeTotal(r resp, commune, mt string) int {
	ts, _ := r.body["commune_totals"].([]any)
	for _, x := range ts {
		m := x.(map[string]any)
		if m["commune_code"] == commune && m["machine_type"] == mt {
			return int(m["quantity"].(float64))
		}
	}
	return -1
}

// FN-XA-03.04: khai báo số lượng theo loại máy (thay toàn bộ, mỗi loại đổi = một sự kiện set/removed); PUT lặp
// cùng Idempotency-Key → một lần ghi; AC tổng theo loại của xã = Σ HTX; lịch sử số lượng cũ → mới; quy tắc số
// lượng (âm/không nguyên → 422), trùng loại → 422, loại ngoài danh mục → 422, HTX ngừng hoạt động → 422.
func TestFN_XA_03_04_MachineCountsDeclareTotalsLog(t *testing.T) {
	m := newMCFix(t)
	c3, c4 := ids.New(), ids.New()
	m.orgs.put(partyport.Org{Ref: c3, Kind: partyport.OrgCooperative, Code: "HTX000003", Name: "HTX Bình An", AdminUnitCode: "30001", Active: true})
	m.orgs.put(partyport.Org{Ref: c4, Kind: partyport.OrgCooperative, Code: "HTX000004", Name: "HTX Đã Giải Thể", AdminUnitCode: "30001"})
	xa := m.user(m.tenant, authz.KNXa, []string{"30001"}, nil)

	k := key()
	r1 := xa.put(m.c1, k, map[string]any{"lines": []any{line("MLD", 2), map[string]any{"machine_type": "MGD", "quantity": 1, "note": "Mua 2025"}}})
	mcWant(t, r1, http.StatusOK, "", "khai báo HTX 1")
	if num(r1.body, "total") != 3 || len(r1.body["lines"].([]any)) != 2 {
		t.Fatalf("khai báo: %v", r1.body)
	}
	if l := r1.body["lines"].([]any)[0].(map[string]any); l["machine_type"] != "MGD" || l["machine_type_name"] != "Máy gặt đập liên hợp" || l["note"] != "Mua 2025" {
		t.Fatalf("dòng: %v", l)
	}
	r2 := xa.put(m.c1, k, map[string]any{"lines": []any{line("MLD", 9)}})
	if num(r2.body, "total") != 3 {
		t.Fatalf("PUT lặp phải giữ kết quả lần đầu: %v", r2.body)
	}
	if n := m.count(m.tenant, "SELECT count(*) FROM pf_mechanization.coop_machine_count_event"); n != 2 {
		t.Fatalf("sổ sau PUT lặp: %d", n)
	}
	mcWant(t, xa.put(c3, key(), map[string]any{"lines": []any{line("MLD", 3)}}), http.StatusOK, "", "khai báo HTX 3")

	ov := xa.get("/v1/kn/machine-counts?commune=30001")
	mcWant(t, ov, http.StatusOK, "", "tổng xã")
	if communeTotal(ov, "30001", "MLD") != 5 || communeTotal(ov, "30001", "MGD") != 1 || len(ov.body["cooperatives"].([]any)) != 2 {
		t.Fatalf("AC tổng xã = Σ HTX: %v", ov.body)
	}

	// Thay toàn bộ: MLD 2 → 4, MGD bị bỏ.
	mcWant(t, xa.put(m.c1, key(), map[string]any{"lines": []any{line("MLD", 4)}, "reason": "Kiểm kê lại"}), http.StatusOK, "", "sửa khai báo")
	ov = xa.get("/v1/kn/machine-counts")
	if communeTotal(ov, "30001", "MLD") != 7 || communeTotal(ov, "30001", "MGD") != -1 {
		t.Fatalf("tổng sau sửa: %v", ov.body)
	}
	lg := xa.get("/v1/kn/machine-counts/" + m.c1.String() + "/log")
	mcWant(t, lg, http.StatusOK, "", "lịch sử")
	its := lg.body["items"].([]any)
	if len(its) != 4 {
		t.Fatalf("lịch sử: %v", lg.body)
	}
	var sawSet, sawRemoved bool
	for _, x := range its[2:] {
		e := x.(map[string]any)
		ch := e["changes"].([]any)[0].(map[string]any)
		switch {
		case e["event"] == "set" && e["machine_type"] == "MLD":
			sawSet = ch["old"] == "2" && ch["new"] == "4" && e["reason"] == "Kiểm kê lại"
		case e["event"] == "removed" && e["machine_type"] == "MGD":
			sawRemoved = ch["old"] == "1" && ch["new"] == nil
		}
	}
	if !sawSet || !sawRemoved {
		t.Fatalf("lịch sử cũ → mới: %v", its)
	}
	if n, err := m.mod.MachineCounts().CoopMachineTotal(tenant.WithID(context.Background(), m.tenant), m.c1); err != nil || n != 4 {
		t.Fatalf("tổng máy của HTX cho pf/party: %d %v", n, err)
	}

	// TT-10: số 0 không phải khai báo; bỏ dòng để ghi removed.
	mcWant(t, xa.put(m.c1, key(), map[string]any{"lines": []any{line("MLD", 0)}}), http.StatusUnprocessableEntity, "mechanization.machine_quantity_invalid", "số không")
	// Quy tắc số lượng và loại máy.
	mcWant(t, xa.put(m.c1, key(), map[string]any{"lines": []any{line("MLD", -1)}}), http.StatusUnprocessableEntity, "mechanization.machine_quantity_invalid", "âm")
	mcWant(t, xa.put(m.c1, key(), map[string]any{"lines": []any{line("MLD", 1.5)}}), http.StatusUnprocessableEntity, "mechanization.machine_quantity_invalid", "không nguyên")
	mcWant(t, xa.put(m.c1, key(), map[string]any{"lines": []any{line("MLD", "2")}}), http.StatusUnprocessableEntity, "mechanization.machine_quantity_invalid", "chuỗi")
	mcWant(t, xa.put(m.c1, key(), map[string]any{"lines": []any{line("MLD", 1), line("MLD", 2)}}), http.StatusUnprocessableEntity,
		"mechanization.machine_type_duplicate", "trùng loại")
	mcWant(t, xa.put(m.c1, key(), map[string]any{"lines": []any{line("XYZ", 1)}}), http.StatusUnprocessableEntity, "mechanization.machine_type_unknown", "loại lạ")
	mcWant(t, xa.put(c4, key(), map[string]any{"lines": []any{line("MLD", 1)}}), http.StatusUnprocessableEntity, "mechanization.cooperative_invalid", "HTX ngừng")
	if n := m.count(m.tenant, "SELECT count(*) FROM pf_mechanization.coop_machine_count_event"); n != 5 {
		t.Fatalf("lỗi kiểm không ghi sổ: %d", n)
	}
}

// FN-XA-03.04 + AC-CHUNG: phạm vi xã (HTX xã khác → 404, lọc xã khác → 403), Cấp Tỉnh xem trong tỉnh nhưng
// không ghi, Cấp Quốc gia chỉ xem (403 mechanization.read_only), chế độ hỗ trợ Admin bắt buộc lý do, Admin ghi
// phòng ban mặc định trong_trot, cách ly tenant.
func TestFN_XA_03_04_MachineCountsScopeReadOnlyTenant(t *testing.T) {
	m := newMCFix(t)
	xa := m.user(m.tenant, authz.KNXa, []string{"30001"}, nil)
	tinh := m.user(m.tenant, authz.KNTinh, nil, nil)
	tw := m.user(m.tenant, authz.KNTW, nil, nil)
	body := map[string]any{"lines": []any{line("MLD", 2)}}

	mcWant(t, xa.put(m.c1, key(), body), http.StatusOK, "", "Cấp Xã khai báo")
	mcWant(t, xa.put(m.c2, key(), body), http.StatusNotFound, "mechanization.cooperative_not_found", "HTX xã khác")
	mcWant(t, xa.get("/v1/kn/machine-counts/"+m.c2.String()), http.StatusNotFound, "mechanization.cooperative_not_found", "đọc HTX xã khác")
	mcWant(t, xa.get("/v1/kn/machine-counts?commune=31001"), http.StatusForbidden, "mechanization.out_of_scope", "lọc xã khác")
	mcWant(t, tinh.put(m.c1, key(), body), http.StatusForbidden, "mechanization.read_only", "Cấp Tỉnh ghi")
	mcWant(t, tw.put(m.c1, key(), body), http.StatusForbidden, "mechanization.read_only", "Cấp Quốc gia ghi")
	if communeTotal(tinh.get("/v1/kn/machine-counts"), "30001", "MLD") != 2 {
		t.Fatalf("Cấp Tỉnh xem trong tỉnh")
	}
	if r := tw.get("/v1/kn/machine-counts/" + m.c1.String()); num(r.body, "total") != 2 {
		t.Fatalf("Cấp Quốc gia xem: %v", r.body)
	}

	sup := m.user(m.tenant, authz.KNAdmin, nil, &authz.KNSupport{SessionID: ids.New(), OnBehalfOf: xa.actor})
	mcWant(t, sup.put(m.c1, key(), body), http.StatusUnprocessableEntity, "mechanization.support_reason_required", "hỗ trợ thiếu lý do")
	sup.reason = "Hỗ trợ cán bộ xã khai báo máy"
	mcWant(t, sup.put(m.c1, key(), map[string]any{"lines": []any{line("MLD", 5)}}), http.StatusOK, "", "hỗ trợ khai báo")
	if n := m.count(m.tenant, `SELECT count(*) FROM pf_mechanization.coop_machine_count_event
	    WHERE on_behalf_of = $1 AND support_reason IS NOT NULL AND department_code = 'trong_trot'`, xa.actor); n != 1 {
		t.Fatalf("on_behalf_of, phòng ban mặc định: %d", n)
	}

	other := m.user(ids.New(), authz.KNXa, []string{"30001"}, nil)
	if r := other.get("/v1/kn/machine-counts"); len(r.body["cooperatives"].([]any)) != 0 {
		t.Fatalf("tenant khác thấy khai báo: %v", r.body)
	}
	if r := other.get("/v1/kn/machine-counts/" + m.c1.String()); len(r.body["lines"].([]any)) != 0 {
		t.Fatalf("tenant khác thấy dòng: %v", r.body)
	}
	// Không có ngữ cảnh phiên → 428.
	noCtx := m.as(m.tenant, m.adminOrg, "kn_user")
	r := noCtx.call(m.srv, http.MethodGet, "/v1/kn/machine-counts", "", "application/json", nil)
	mcWant(t, r, http.StatusPreconditionRequired, "accounts.context_required", "thiếu X-AG-Context")
}
