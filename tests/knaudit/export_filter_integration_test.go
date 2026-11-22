package knaudit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// This test exercises the real reports HTTP/private-JSONB contract with the KN
// codec. Identity/authority are explicit test doubles. The builder always errors
// and is never run: this is not proof of live authority, a worker or Excel/DLP.
func TestAuditExportFilterPrivateRoundtrip83(t *testing.T) {
	const source, template = "kn.audit_export", "kn.audit_log"
	env := pgtest.Get(t)
	ctx := context.Background()
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	admin := env.AdminPool(t, migrations.PF)
	clk := clock.NewFixed(time.Date(2026, 11, 7, 2, 3, 4, 123456000, clock.Zone()))
	now := clk.Now()
	kn := extension.New(pool, clk, extension.Deps{})
	acc := accounts.New(pool, clk, accounts.Deps{})
	registry, err := accport.NewAuditRegistry(append([]accport.AuditSource{acc.NativeAuditSource()}, kn.AuditSources()...)...)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := extension.NewAuditExportFilterCodec(registry)
	if err != nil {
		t.Fatal(err)
	}
	p := authz.Principal{TenantID: ids.New(), ActorRef: ids.New(), MFA: true, Roles: []string{authz.RolePlatformAdmin}}
	active, authCalls, buildCalls := true, 0, 0
	registration := repport.SourceBuilderRegistration{Source: source,
		Build: func(context.Context, repport.SourceBuildQuery) (repport.SourceData, error) {
			buildCalls++
			return repport.SourceData{}, errors.New("audit83 fixture does not implement a worker")
		},
		JobPolicy: &repport.SourceJobPolicy{Version: 1, ScopeMode: repport.SourceScopeTenant, Freeze: codec.Freeze,
			AuthorizeJob: func(c context.Context, a repport.SourceJobAccess) error {
				authCalls++
				principal, ok := authz.FromContext(c)
				if !active || !ok || !principal.MFA || principal.ActorRef != p.ActorRef || principal.TenantID != p.TenantID ||
					a.ActorRef != p.ActorRef || a.RequestedBy != p.ActorRef || a.TenantID != p.TenantID {
					return problem.Forbidden
				}
				_, err := codec.Decode(a)
				return err
			}}}
	catalog := repport.CatalogRegistration{Sources: map[string]repport.CatalogSource{source: {Key: source, Module: "kn.audit", Levels: []string{"province"}, DefaultLevel: "province",
		Columns: []repport.CatalogColumn{{Key: "event_ref", Title: "Mã sự kiện", Kind: "text"}}}},
		Templates: []repport.CatalogTemplate{{Key: template, Def: repport.CatalogDefinition{Title: "Audit codec fixture", Source: source, Grouping: "province",
			Columns: []repport.CatalogTemplateColumn{{Key: "event_ref", Title: "Mã sự kiện"}}, Formats: []string{"xlsx"}}}}}
	mod := reports.New(pool, clk, reports.Deps{Catalog: []repport.CatalogRegistration{catalog}, Builders: []repport.SourceBuilderRegistration{registration}})
	routerFor := func(m *reports.Module) http.Handler {
		router := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
		group := router.Group("/v1", func(c *gin.Context) {
			c.Request = c.Request.WithContext(tenant.WithID(authz.WithPrincipal(c.Request.Context(), p), p.TenantID))
		}, m.Middleware(), idempotency.Middleware(shareddb.ForModule(pool, shareddb.Ops)))
		m.RegisterApp(group)
		return router
	}
	router := routerFor(mod)
	filterActor := ids.New()
	filter := `{"from":"2026-11-01T00:00:00+07:00","to":"2026-11-07T00:00:00+07:00","actor_ref":"` + filterActor.String() + `","department":"auditprivate83","module":"kn.report","sources":["kn.review_event","kn.period_event"]}`
	request := func(method, path, key, args string) (int, []byte, bool) {
		t.Helper()
		body := ""
		if method == http.MethodPost {
			body = `{"template_key":"` + template + `","format":"xlsx","params":{"level":"province"},"source_args":` + args + `}`
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(idempotency.Header, key)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		for _, private := range []string{"auditprivate83", filterActor.String(), "_reports_source", "private-marker"} {
			if bytes.Contains(w.Body.Bytes(), []byte(private)) {
				t.Fatal("HTTP response disclosed private filter data")
			}
		}
		if w.Code < 400 && bytes.Contains(w.Body.Bytes(), []byte(`"source_args"`)) {
			t.Fatal("public job DTO exposed a private source_args field")
		}
		return w.Code, bytes.Clone(w.Body.Bytes()), w.Header().Get(idempotency.ReplayedHeader) == "true"
	}
	counts := func() [2]int64 {
		t.Helper()
		var n [2]int64
		if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM pf_reports.export_job WHERE tenant_id=$1),
  (SELECT count(*) FROM shared.outbox WHERE tenant_id=$1 AND event_type=$2)`, p.TenantID, repport.EventExportRequested).Scan(&n[0], &n[1]); err != nil {
			t.Fatal(err)
		}
		return n
	}
	status, first, replayed := request(http.MethodPost, "/v1/reports/exports", "audit83-roundtrip-01", filter)
	if status != http.StatusAccepted || replayed || counts() != [2]int64{1, 1} {
		t.Fatalf("first request: status %d, replay %v, counts %v", status, replayed, counts())
	}
	var job struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(first, &job); err != nil || job.ID == uuid.Nil {
		t.Fatalf("invalid public job ID: %v", err)
	}
	// Read through a +07 transaction to verify equality survives PostgreSQL's
	// timestamptz precision and JSONB key/whitespace normalization.
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SET LOCAL TIME ZONE 'Asia/Ho_Chi_Minh'"); err != nil {
		t.Fatal(err)
	}
	var snapshot []byte
	var params string
	a := repport.SourceJobAccess{Source: source, TemplateKey: template, Format: "xlsx", JobID: job.ID, TenantID: p.TenantID, RequestedBy: p.ActorRef}
	if err = tx.QueryRow(ctx, `SELECT params->'_reports_source'->'args', requested_at,
  (params->'_reports_source'->>'version')::integer, scope_all, scope_units, params::text
  FROM pf_reports.export_job WHERE tenant_id=$1 AND id=$2`, p.TenantID, job.ID).
		Scan(&snapshot, &a.RequestedAt, &a.ArgsVersion, &a.Scope.All, &a.Scope.Units, &params); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	a.SourceArgs = snapshot
	frozen, err := codec.Decode(a)
	if err != nil || !frozen.AsOf.Equal(now) || frozen.ActorRef != filterActor || frozen.Sources[0] != "kn.period_event" || a.Scope.All || len(a.Scope.Units) != 0 {
		t.Fatalf("private JSONB roundtrip failed: %v", err)
	}
	var event string
	if err = admin.QueryRow(ctx, "SELECT payload::text FROM shared.outbox WHERE tenant_id=$1 AND event_type=$2", p.TenantID, repport.EventExportRequested).Scan(&event); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(event, "auditprivate83") || strings.Contains(event, filterActor.String()) || strings.Contains(event, "_reports_source") || strings.Contains(event, "source_args") {
		t.Fatal("outbox copied private source arguments")
	}
	clk.Advance(25 * time.Hour)
	beforeAuth := authCalls
	status, second, replayed := request(http.MethodPost, "/v1/reports/exports", "audit83-roundtrip-01", filter)
	if status != http.StatusAccepted || !replayed || !bytes.Equal(first, second) || authCalls <= beforeAuth || counts() != [2]int64{1, 1} {
		t.Fatal("replay changed the job, bypassed the fixture guard or duplicated outbox")
	}
	for _, path := range []string{"/v1/reports/exports/" + job.ID.String(), "/v1/reports/exports?limit=10"} {
		if status, _, _ = request(http.MethodGet, path, "", ""); status != http.StatusOK {
			t.Fatalf("private job view: %d", status)
		}
	}
	invalid := []string{
		`{"from":"2026-01-01T00:00:00Z","to":"2026-04-05T00:00:00Z"}`,
		`{"from":"2026-11-01T00:00:00Z","to":"2026-11-07T00:00:00Z","payload":"private-marker"}`,
		`{"from":1e100000,"to":"2026-11-07T00:00:00Z"}`,
		`{"from":1e-100000,"to":"2026-11-07T00:00:00Z"}`,
		`{"from":"` + strings.Repeat("1", 100000) + `","to":"2026-11-07T00:00:00Z"}`,
	}
	for _, args := range invalid {
		if status, _, _ = request(http.MethodPost, "/v1/reports/exports", "audit83-invalid-"+ids.New().String(), args); status != http.StatusUnprocessableEntity {
			t.Fatalf("invalid filter accepted: %d", status)
		}
	}
	changed := strings.ReplaceAll(filter, filterActor.String(), ids.New().String())
	if status, _, _ = request(http.MethodPost, "/v1/reports/exports", "audit83-roundtrip-01", changed); status != http.StatusUnprocessableEntity {
		t.Fatalf("changed filter replay accepted: %d", status)
	}
	active = false
	if status, _, replayed = request(http.MethodPost, "/v1/reports/exports", "audit83-roundtrip-01", filter); status != http.StatusForbidden || replayed {
		t.Fatalf("test-double denial bypassed before HTTP cache: %d", status)
	}
	active = true
	var after string
	if err = admin.QueryRow(ctx, "SELECT params::text FROM pf_reports.export_job WHERE tenant_id=$1 AND id=$2", p.TenantID, job.ID).Scan(&after); err != nil || after != params || counts() != [2]int64{1, 1} {
		t.Fatal("invalid/replayed requests changed the persisted snapshot or duplicated job/outbox", err)
	}
	// The actual reports trigger must reject snapshot rebinding even for the
	// test administrator. Do not disable it to fabricate a mutable job.
	_, err = admin.Exec(ctx, `UPDATE pf_reports.export_job SET params=jsonb_set(params,
	  '{_reports_source,args,job_id}',to_jsonb($3::text),false) WHERE tenant_id=$1 AND id=$2`, p.TenantID, job.ID, ids.New().String())
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.SQLState() != "42501" {
		t.Fatalf("snapshot update was not rejected by the immutable trigger: %v", err)
	}
	if err = admin.QueryRow(ctx, "SELECT params::text FROM pf_reports.export_job WHERE tenant_id=$1 AND id=$2", p.TenantID, job.ID).Scan(&after); err != nil || after != params {
		t.Fatal("rejected update changed the private snapshot", err)
	}
	rebound := a
	rebound.JobID = ids.New()
	if _, err = codec.Decode(rebound); err == nil {
		t.Fatal("persisted snapshot accepted with a different server job binding")
	}
	router = routerFor(reports.New(pool, clk, reports.Deps{Catalog: []repport.CatalogRegistration{catalog}}))
	if status, _, replayed = request(http.MethodPost, "/v1/reports/exports", "audit83-roundtrip-01", filter); status != http.StatusServiceUnavailable || replayed {
		t.Fatalf("missing owner policy did not fail closed before cached response: %d", status)
	}
	if buildCalls != 0 {
		t.Fatal("codec test unexpectedly ran an export worker")
	}
}

func TestAuditExportSourceRemainsUnregistered83(t *testing.T) {
	if _, registered := extension.ReportCatalog().Sources["kn.audit_export"]; registered {
		t.Fatal("codec slice must not expose an export catalog without real authority")
	}
	for _, builder := range extension.ReportBuilders(extension.ReportBuilderDeps{}) {
		if builder.Source == "kn.audit_export" {
			t.Fatal("codec slice must not register an export source without real authority")
		}
	}
}
