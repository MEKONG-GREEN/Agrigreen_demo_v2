// Package cntsdiaryparams_test kiểm thử tích hợp KN-PARAMS-CNTS-DIARY-OWNER (#85) trên module THẬT: pf/extension
// (extension/cntsdiary, W11.48) đọc tham số có phiên bản của pf/masterdata (đăng ký riêng extension.CNTSDiaryMasterdataParams)
// trên PostgreSQL, mỗi module bằng vai DB của mình. Ngữ cảnh phiên KN giả lập bằng middleware X-AG-Context; danh mục, đơn
// vị hành chính, danh bạ Cấp Xã, kho định danh là bản giả (như kiểm thử W11.48 của module).
package cntsdiaryparams_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
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

const (
	cnts = "chan_nuoi_thuy_san"
	base = "/v1/extension/cnts/"
)

// 10:00 thứ Tư 13/10/2027 giờ Việt Nam.
var t0 = time.Date(2027, 10, 13, 3, 0, 0, 0, time.UTC)

type units struct{}

func (units) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	switch code {
	case "30001":
		return partyport.AdminUnit{Code: code, Name: "Xã " + code, Level: partyport.LevelCommune, ParentCode: "89"}, nil
	case "89":
		return partyport.AdminUnit{Code: code, Name: "Tỉnh", Level: partyport.LevelProvince}, nil
	}
	return partyport.AdminUnit{}, partyport.ErrUnitNotFound
}
func (units) Descendants(context.Context, string, time.Time) ([]string, error) { return nil, nil }
func (units) Current(context.Context, string, time.Time) ([]string, error)     { return nil, nil }

type catalogs struct{ knport.CNTSCatalogs }

var gaTP = mdport.CatalogItem{Catalog: "cnts_model_type", Code: "GA_TP", Name: "Gà thương phẩm", Active: true}

func (catalogs) ItemAt(_ context.Context, catalog, code string, _ time.Time) (mdport.CatalogItem, error) {
	if catalog == gaTP.Catalog && code == gaTP.Code {
		return gaTP, nil
	}
	return mdport.CatalogItem{}, fmt.Errorf("giả: %w", mdport.ErrCatalogItemNotFound)
}

func (catalogs) ListAt(_ context.Context, catalog string, _ time.Time) ([]mdport.CatalogItem, error) {
	if catalog == gaTP.Catalog {
		return []mdport.CatalogItem{gaTP}, nil
	}
	return []mdport.CatalogItem{}, nil
}

type xaScopes struct{ list []accport.XaScope }

func (x *xaScopes) XaScopes(context.Context, uuid.UUID, string) ([]accport.XaScope, error) {
	return x.list, nil
}

type fix struct {
	t      *testing.T
	env    *pgtest.Env
	iss    *authztest.FakeIssuer
	srv    *httptest.Server // API ứng dụng pf/extension
	mdSrv  *httptest.Server // API quản trị pf/masterdata
	pool   *pgxpool.Pool    // vai quản trị (đọc kiểm)
	clk    *clock.Fixed
	md     *masterdata.Module
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
	xa     *xaScopes
}

// newFix: pf/masterdata THẬT với đăng ký KN cũ + đăng ký riêng nhật ký (như gốc lắp ráp sau #84), Params nối vào
// pf/extension; cùng một clock cố định.
func newFix(t *testing.T) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, env: env, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}, xa: &xaScopes{},
		clk: clock.NewFixed(t0)}
	f.pool = env.AdminPool(t, migrations.PF)
	f.md = masterdata.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), f.clk, masterdata.Deps{
		Params:   []mdport.ParamRegistration{extension.MasterdataParams(), extension.CNTSDiaryMasterdataParams()},
		Catalogs: []mdport.CatalogRegistration{extension.MasterdataCatalogs()},
	})
	mod := extension.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), f.clk, extension.Deps{Units: units{}, Params: f.md.Params()})
	vault := clienttest.New()
	mod.UseCNTSCatalogs(catalogs{})
	mod.UseXaScopes(f.xa)
	mod.UseCNTSModelVault(vault)
	ops := env.Pool(t, migrations.PF, "platform_api")
	log := telemetry.NewLogger("test", io.Discard)
	eng := httpx.NewEngine(log, 4<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	adm := httpx.NewEngine(log, 1<<20)
	f.md.RegisterAdmin(adm.Group("/v1", httpx.Authenticate(f.iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	f.mdSrv = httptest.NewServer(adm)
	t.Cleanup(f.mdSrv.Close)
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

// user là người dùng API KN (ngữ cảnh phiên kc) hoặc API quản trị masterdata (roles, không ngữ cảnh KN).
type user struct {
	f     *fix
	srv   *httptest.Server
	token string
	ctxID uuid.UUID
	ref   uuid.UUID
}

func (f *fix) user(kc authz.KNContext) user {
	sub := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: sub, TenantID: f.tenant, Roles: []string{"kn_user", client.RoleRegistrar, client.RoleDisplay},
		MFA: true, TTL: 2 * time.Hour})
	kc.ID, kc.OrgRef = ids.New(), ids.New()
	f.mu.Lock()
	f.ctxs[kc.ID] = &kc
	f.mu.Unlock()
	return user{f: f, srv: f.srv, token: tok, ctxID: kc.ID, ref: sub}
}

func (f *fix) mdAdmin(roles ...string) user {
	sub := ids.New()
	return user{f: f, srv: f.mdSrv, ref: sub, token: f.iss.Sign(f.t, authztest.Token{Subject: sub, TenantID: f.tenant, Roles: roles, MFA: true})}
}

func tinh(group string) authz.KNContext {
	return authz.KNContext{Role: authz.KNTinh, Department: cnts, ProvinceGroup: group, Scope: authz.Scope{Units: []string{"89", "30001"}}}
}

func xa(communes ...string) authz.KNContext {
	return authz.KNContext{Role: authz.KNXa, Department: cnts, Communes: communes, Scope: authz.Scope{Units: communes}}
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
	r, _ := http.NewRequest(method, u.srv.URL+path, rd)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+u.token)
	if u.ctxID != uuid.Nil {
		r.Header.Set(authz.ContextHeader, u.ctxID.String())
	}
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

func key() string                            { return "k-" + ids.New().String() }
func (u user) post(p string, b any) resp     { return u.do(http.MethodPost, p, key(), b) }
func (u user) postK(p, k string, b any) resp { return u.do(http.MethodPost, p, k, b) }
func (u user) get(p string) resp             { return u.do(http.MethodGet, p, "", nil) }
func str(m map[string]any, k string) string  { s, _ := m[k].(string); return s }
func num(m map[string]any, k string) string  { n, _ := m[k].(json.Number); return n.String() }

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
