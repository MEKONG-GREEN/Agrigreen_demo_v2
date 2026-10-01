package handler_test

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

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type admin struct {
	t      *testing.T
	r      *gin.Engine
	op     uuid.UUID
	actors map[string]authz.Principal
}

func newAdmin(t *testing.T) *admin {
	env := pgtest.Get(t)
	peppers, err := domain.NewPeppers(1, map[int][]byte{1: bytes.Repeat([]byte{'p'}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	op := ids.New()
	mod := apiclient.New(env.Pool(t, migrations.PF, port.DBRole), clock.NewFixed(time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)),
		apiclient.Deps{Peppers: peppers})
	a := &admin{t: t, op: op, actors: map[string]authz.Principal{
		"admin":    {ActorRef: ids.New(), TenantID: op, Roles: []string{port.RoleAPIClientAdmin}, MFA: true},
		"approver": {ActorRef: ids.New(), TenantID: op, Roles: []string{port.RoleAPIClientApprover}, MFA: true},
	}}
	r := httpx.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)), 1<<20)
	v1 := r.Group("/v1", func(c *gin.Context) {
		p := a.actors[c.GetHeader("X-Test-Actor")]
		ctx := authz.WithPrincipal(c.Request.Context(), p)
		c.Request = c.Request.WithContext(tenant.WithID(ctx, p.TenantID))
	}, idempotency.Middleware(env.Pool(t, migrations.PF, "platform_api")))
	mod.RegisterAdmin(v1)
	a.r = r
	return a
}

func (a *admin) do(actor, method, path string, body any, idem string) (*httptest.ResponseRecorder, map[string]any) {
	a.t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("X-Test-Actor", actor)
	if idem != "" {
		req.Header.Set(idempotency.Header, idem)
	}
	w := httptest.NewRecorder()
	a.r.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

// ADR-0022 mục 4 (P-03, W-01 phía backend): Admin API phát hành khóa — bí mật MỘT lần, Cache-Control:
// no-store; phát lại Idempotency-Key KHÔNG có bí mật; bảng shared.idempotency_key không lưu bí mật.
func TestAdminIssueKeySecretOnce_ADR0022(t *testing.T) {
	a := newAdmin(t)
	w, c := a.do("admin", http.MethodPost, "/v1/api-clients", map[string]any{"client_tenant_id": ids.New().String(), "display_name": "Nhà máy VFT",
		"purpose": "Kế hoạch thu mua rơm theo xã", "contact_ref": ids.New().String()}, "cl-0000001")
	if w.Code != http.StatusCreated {
		t.Fatalf("đề xuất khách hàng: %d %s", w.Code, w.Body)
	}
	id := c["id"].(string)
	if w, _ := a.do("admin", http.MethodPost, "/v1/api-clients/"+id+"/approve", nil, "ap-0000001"); w.Code != http.StatusForbidden {
		t.Fatalf("admin không duyệt được: %d", w.Code)
	}
	if w, _ := a.do("approver", http.MethodPost, "/v1/api-clients/"+id+"/approve", nil, "ap-0000002"); w.Code != http.StatusOK {
		t.Fatalf("duyệt: %d %s", w.Code, w.Body)
	}
	w1, k1 := a.do("admin", http.MethodPost, "/v1/api-clients/"+id+"/keys", nil, "key-0000001")
	secret, _ := k1["api_key"].(string)
	if w1.Code != http.StatusCreated || w1.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("phát hành: %d %v", w1.Code, w1.Header())
	}
	if _, ok := port.ParseKey(secret); !ok {
		t.Fatal("phản hồi đầu phải có bí mật")
	}
	w2, k2 := a.do("admin", http.MethodPost, "/v1/api-clients/"+id+"/keys", nil, "key-0000001")
	if w2.Header().Get(idempotency.ReplayedHeader) != "true" || k2["key_id"] != k1["key_id"] || k2["api_key"] != nil && k2["api_key"] != "" {
		t.Fatalf("phát lại không được có bí mật: %s", w2.Body)
	}
	owner := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.Migrator)
	var dump string
	if err := tenant.WithTenantTx(context.Background(), owner, a.op, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT coalesce(string_agg(convert_from(response_body, 'UTF8'), '|'), '')
		    FROM shared.idempotency_key WHERE tenant_id = $1::uuid`, a.op).Scan(&dump)
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dump, secret[17:]) || !strings.Contains(dump, k1["key_prefix"].(string)) {
		t.Fatal("bản lưu phát lại không được chứa bí mật")
	}
	// Danh sách khóa: chỉ prefix, không bí mật / HMAC.
	wl, _ := a.do("admin", http.MethodGet, "/v1/api-clients/"+id+"/keys", nil, "")
	if wl.Code != 200 || strings.Contains(wl.Body.String(), secret[17:]) || strings.Contains(wl.Body.String(), "hmac") {
		t.Fatalf("danh sách khóa: %s", wl.Body)
	}
}
