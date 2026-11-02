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
		kc.OrgRef = ids.New() // như P1: lần gán Cấp Xã luôn có Nút (db.ForModule đòi Nút — FD)
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

// Read-only legacy compatibility: scope, historical log and tenant isolation stay valid after #160.
func TestFN_XA_03_03_CooperativeReadScope(t *testing.T) {
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

	// Read-only compatibility: data was provisioned by the native fixture, never by KN.
	got := xa.get("/v1/kn/cooperatives/" + tr.c1.String())
	wantStatus(t, got.resp, http.StatusOK, "chi tiết HTX có sẵn")
	wantStatus(t, xa.get("/v1/kn/cooperatives/"+tr.c1.String()+"/log").resp, http.StatusOK, "Log lịch sử vẫn đọc")
	otherTenant := kf.user(ids.New(), authz.KNXa, []string{"30001"}, nil)
	wantCode(t, otherTenant.get("/v1/kn/cooperatives/"+tr.c1.String()).resp, http.StatusNotFound, "party.org_not_found", "tenant khác")
}

func TestKNLegacyCooperativeWritesAbsent(t *testing.T) {
	kf := newKNFix(t)
	ref := kf.tr.c1.String()
	before := kf.countWhere(kf.tenant, "SELECT count(*) FROM pf_party.org_event")
	for _, role := range []string{authz.KNXa, authz.KNTinh, authz.KNTW, authz.KNAdmin} {
		u := kf.user(kf.tenant, role, []string{"30001"}, nil)
		for _, route := range []struct {
			method, path string
			status       int
			code         string
		}{
			{http.MethodPost, "/v1/kn/cooperatives", http.StatusMethodNotAllowed, "method_not_allowed"},
			{http.MethodPut, "/v1/kn/cooperatives/" + ref, http.StatusMethodNotAllowed, "method_not_allowed"},
			{http.MethodPost, "/v1/kn/cooperatives/" + ref + "/deactivate", http.StatusNotFound, "not_found"},
			{http.MethodPost, "/v1/kn/cooperatives/" + ref + "/reactivate", http.StatusNotFound, "not_found"},
		} {
			t.Run(role+route.method+route.path, func(t *testing.T) {
				res := u.do(route.method, route.path, key(), map[string]any{"name": "Không được tạo", "commune_code": "30001", "reason": "Không được đổi dữ liệu HTX"})
				wantCode(t, res.resp, route.status, route.code, "route ghi KN legacy không đăng ký")
			})
		}
	}
	if after := kf.countWhere(kf.tenant, "SELECT count(*) FROM pf_party.org_event"); after != before {
		t.Fatalf("KN legacy đổi sổ HTX: %d -> %d", before, after)
	}
	wantStatus(t, kf.user(kf.tenant, authz.KNXa, []string{"30001"}, nil).get("/v1/kn/cooperatives/"+ref).resp, http.StatusOK, "đọc sau các lệnh nghỉ")
}
