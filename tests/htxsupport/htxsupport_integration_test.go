// Package htxsupport kiểm nghiệm thu PA1-HTX-SUPPORT-GPSLOG (#313): yêu cầu hỗ trợ App HTX chạy trọn trên module chủ HTX
// pf/cooperative khi tenant TẮT pf/extension (module KN) — gửi, danh sách quản lý HTX, đồng bộ, log GPS tới GIS.
package htxsupport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
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

type units struct{}

func (units) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	return partyport.AdminUnit{Code: code, Level: partyport.LevelCommune, ParentCode: "31"}, nil
}
func (units) Descendants(context.Context, string, time.Time) ([]string, error) { return nil, nil }
func (units) Current(_ context.Context, code string, _ time.Time) ([]string, error) {
	return []string{code}, nil
}

type farmers map[uuid.UUID][]uuid.UUID

func (f farmers) HouseholdsOf(_ context.Context, actor uuid.UUID) ([]uuid.UUID, error) {
	return f[actor], nil
}

type gpsLog struct {
	mu    sync.Mutex
	fixes map[string]geoport.GpsFix
}

func (g *gpsLog) RecordFix(_ context.Context, fix geoport.GpsFix) (geoport.GpsFixReceipt, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fixes[fix.IdemKey] = fix
	return geoport.GpsFixReceipt{}, nil
}

func call(t *testing.T, srv *httptest.Server, token, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Idempotency-Key", "k-"+ids.New().String())
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return r.StatusCode, m
}

func TestSupportRequestsWithExtensionDisabled_PA1_313(t *testing.T) {
	env := pgtest.Get(t)
	ctx := context.Background()
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	admin := env.AdminPool(t, migrations.PF)
	clk := clock.NewFixed(time.Date(2026, 11, 16, 3, 0, 0, 0, time.UTC))
	tid, coop, hh := ids.New(), ids.New(), ids.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO pf_party.org (tenant_id, ref, kind, code, name, admin_unit_code, last_event_id, last_seq)
		  VALUES ($1, $2, 'cooperative', $3, 'HTX #313', '31117', gen_random_uuid(), 1)`, []any{tid, coop, "H" + coop.String()[24:]}},
		{`INSERT INTO pf_coop.cooperative (tenant_id, org_ref) VALUES ($1, $2)`, []any{tid, coop}},
		{`INSERT INTO pf_coop.household (tenant_id, household_ref, commune_code, created_by) VALUES ($1, $2, '31117', gen_random_uuid())`, []any{tid, hh}},
		{`INSERT INTO pf_coop.household_membership (tenant_id, id, household_ref, org_ref, since_date)
		  VALUES ($1, gen_random_uuid(), $2, $3, '2026-01-01')`, []any{tid, hh, coop}},
		// Tenant HTX tắt module KN.
		{`INSERT INTO pf_party.module_enablement_current (tenant_id, module, status, reason, last_event_id, last_seq, updated_by, updated_at)
		  VALUES ($1, 'pf/extension', 'disabled', 'PA1 #313: HTX chạy không có KN', gen_random_uuid(), 1, gen_random_uuid(), now())`, []any{tid}},
	} {
		if _, err := admin.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("nạp dữ liệu: %v\n%s", err, q.sql)
		}
	}

	gl := &gpsLog{fixes: map[string]geoport.GpsFix{}}
	mod := cooperative.New(pool, clk, cooperative.Deps{AdminUnits: units{}, GpsLog: gl})
	iss := authztest.New(t)
	farmer, manager := ids.New(), ids.New()
	mod.UseFarmerHouseholds(farmers{farmer: {hh}})
	pty := party.New(pool, clk)
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	g := eng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops))
	// Như gốc lắp ráp: route của module per_tenant đi qua cổng bật module của pf/party.
	mod.RegisterApp(g.Group("", pty.ModuleGate("pf/cooperative")))
	g.GET("/extension/probe", pty.ModuleGate("pf/extension"), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	ft := iss.Sign(t, authztest.Token{Subject: farmer, TenantID: tid, Roles: []string{coopport.RoleFarmer}})
	mt := iss.Sign(t, authztest.Token{Subject: manager, TenantID: tid, Roles: []string{coopport.RoleCoopManager}, OrgRef: coop})

	if st, b := call(t, srv, mt, http.MethodGet, "/v1/extension/probe", nil); st != http.StatusForbidden || b["code"] != "module_not_enabled" {
		t.Fatalf("đối chứng: pf/extension phải tắt cho tenant: %d %v", st, b)
	}
	st, b := call(t, srv, ft, http.MethodPost, "/v1/support-requests", map[string]any{"category": "dich_hai", "urgency": "khan",
		"description": "Rầy nâu xuất hiện dày ở lô 3, lúa đang trổ", "location": map[string]any{"lat": "10.3861", "lon": "105.4352"}, "gps_accuracy_m": "8"})
	if st != http.StatusCreated {
		t.Fatalf("nông dân gửi yêu cầu khi KN tắt: %d %v", st, b)
	}
	id := b["support_request"].(map[string]any)["id"].(string)
	if st, b := call(t, srv, ft, http.MethodGet, "/v1/support-requests", nil); st != http.StatusOK || len(b["items"].([]any)) != 1 {
		t.Fatalf("nông dân xem yêu cầu: %d %v", st, b)
	}
	st, b = call(t, srv, mt, http.MethodGet, "/v1/cropcycle/support-requests", nil)
	if st != http.StatusOK || len(b["items"].([]any)) != 1 || b["items"].([]any)[0].(map[string]any)["cooperative_ref"] != coop.String() {
		t.Fatalf("quản lý HTX xem danh sách khi KN tắt: %d %v", st, b)
	}

	// Đồng bộ ngoại tuyến: entity support_request do pf/cooperative nhận và gửi xuống.
	var srcOK, sinkOK bool
	for _, s := range mod.SyncSources() {
		srcOK = srcOK || slices.Contains(s.Entities(), coopport.EntitySupportRequest)
	}
	for _, s := range mod.SyncSinks() {
		sinkOK = sinkOK || slices.Contains(s.Entities(), coopport.EntitySupportRequest)
	}
	if !srcOK || !sinkOK {
		t.Fatalf("pf/cooperative phải phục vụ entity support_request (chiều xuống %v, chiều lên %v)", srcOK, sinkOK)
	}

	// Sự kiện gps_log tới GIS: consumer pf.cropcycle.gps_log của pf/cooperative ghi log GPS (mục đích support).
	if !slices.Contains(coopport.GpsLogEvents, coopport.EventSupportRequestSubmitted) {
		t.Fatal("consumer log GPS phải đăng ký sự kiện yêu cầu hỗ trợ")
	}
	var ev outbox.Envelope
	coopDB := shareddb.ForModule(pool, cooperative.DBModule)
	if err := tenant.WithTenantTx(ctx, coopDB, tid, func(tx pgx.Tx) error {
		ev = outbox.Envelope{TenantID: tid, SchemaVersion: 1}
		return tx.QueryRow(ctx, `SELECT id, event_type, payload FROM shared.outbox WHERE event_type = $1`, coopport.EventSupportRequestSubmitted).
			Scan(&ev.EventID, &ev.EventType, &ev.Payload)
	}); err != nil {
		t.Fatalf("sự kiện %s: %v", coopport.EventSupportRequestSubmitted, err)
	}
	if _, err := inbox.Process(ctx, coopDB, coopport.GpsLogConsumer, ev, mod.GpsLogHandler()); err != nil {
		t.Fatal(err)
	}
	fx, ok := gl.fixes["extension:support_request:"+id]
	if !ok || fx.Purpose != geoport.GpsPurposeSupport || fx.Lon == nil || fx.Lon.String() != "105.4352" || fx.ActorRef != farmer {
		t.Fatalf("log GPS tới GIS: %v", gl.fixes)
	}
}
