package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type auditObserver struct {
	tenant.Beginner
	calls    int
	readOnly bool
	role     string
}

func (o *auditObserver) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	o.calls++
	tx, err := o.Beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	var mode string
	if err = tx.QueryRow(ctx, `SELECT current_user,current_setting('transaction_read_only')`).Scan(&o.role, &mode); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	o.readOnly = opts.AccessMode == pgx.ReadOnly && mode == "on"
	return tx, nil
}
func auditHTTP(t *testing.T, server, token string, q url.Values) resp {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, server+"/v1/accounts/audit?"+q.Encode(), nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	var body map[string]any
	if err = json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return resp{status: r.StatusCode, body: body}
}
func auditRows(t *testing.T, r resp) []any {
	t.Helper()
	rows, ok := r.body["items"].([]any)
	if !ok {
		t.Fatalf("missing audit items status%d", r.status)
	}
	return rows
}
func TestAuditNativeHTTPFiltersIsolationAndReadOnly(t *testing.T) {
	f := newFixture(t)
	nat := f.nationalAdmin()
	user := nat.create(person(f.coop30292, "coop_manager"))
	privateReason := "Thông tin không được công bố private-person@example.test"
	if r := nat.do(http.MethodPost, "/v1/accounts/users/"+user.String()+"/lock", map[string]any{"reason": privateReason}); r.status != http.StatusOK {
		t.Fatalf("lock seed%d", r.status)
	}
	other := newFixture(t)
	foreign := other.nationalAdmin().create(person(other.coop30292, "coop_manager"))
	obs := &auditObserver{Beginner: db.ForModule(f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), accounts.DBModule)}
	mod := accounts.New(obs, clock.System{}, accounts.Deps{})
	reg, err := port.NewAuditRegistry(mod.NativeAuditSource())
	if err != nil {
		t.Fatal(err)
	}
	receipts := []port.AuditReadReceipt{}
	access := port.AuditAccess{Authorize: func(ctx context.Context) error { _, err := authz.Require(ctx, authz.RolePlatformAdmin); return err }, RecordRead: func(_ context.Context, r port.AuditReadReceipt) error { receipts = append(receipts, r); return nil }}
	mod.UseAudit(reg, access)
	eng := httpx.NewEngine(telemetry.NewLogger("audit-test", io.Discard), 1<<20)
	// The service must enforce MFA too, even without the Admin router middleware.
	mod.RegisterAdmin(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier())))
	server := httptest.NewServer(eng)
	defer server.Close()
	admin := f.as(f.tenant, f.national, authz.RolePlatformAdmin)
	now := time.Now().UTC()
	q := url.Values{"from": {now.Add(-time.Hour).Format(time.RFC3339Nano)}, "to": {now.Add(time.Hour).Format(time.RFC3339Nano)}, "limit": {"1"}}
	seen := map[string]bool{}
	for page := 0; page < 10; page++ {
		r := auditHTTP(t, server.URL, admin.token, q)
		if r.status != http.StatusOK {
			t.Fatalf("audit%d %v", r.status, r.body)
		}
		for _, row := range auditRows(t, r) {
			v := row.(map[string]any)
			id := v["event_ref"].(string)
			if seen[id] {
				t.Fatal("cursor repeated event")
			}
			seen[id] = true
			if v["entity_ref"] == foreign.String() || v["entity_ref"] != user.String() || v["outcome"] != "unknown" || v["source"] != "accounts.account_event" {
				t.Fatal("tenant/source/outcome wrong")
			}
		}
		data, _ := json.Marshal(r.body)
		for _, secret := range []string{privateReason, "private-person@example.test", "Nguyễn Văn An", "reason", "details"} {
			if strings.Contains(string(data), secret) {
				t.Fatal("PII/raw text leaked", secret)
			}
		}
		if r.body["next_cursor"] == nil {
			break
		}
		q.Set("cursor", r.body["next_cursor"].(string))
	}
	if len(seen) != 3 || len(receipts) != 3 || !obs.readOnly || obs.role != "r_accounts" {
		t.Fatalf("bounded native read: events%d receipts%d role%s readonly%v", len(seen), len(receipts), obs.role, obs.readOnly)
	}
	for _, r := range receipts {
		if r.TenantID != f.tenant || r.ActorRef != admin.actor || r.RequestRef == uuid.Nil || len(r.Returned) != 1 {
			t.Fatal("receipt identity/refs wrong")
		}
	}
	q.Del("cursor")
	q.Set("limit", "50")
	q.Set("action", "account.created")
	q.Set("actor_ref", nat.actor.String())
	if r := auditHTTP(t, server.URL, admin.token, q); r.status != http.StatusOK || len(auditRows(t, r)) != 1 {
		t.Fatal("actor/action filter failed")
	}
	q.Set("department", "fixture_department")
	if r := auditHTTP(t, server.URL, admin.token, q); r.status != http.StatusOK || len(auditRows(t, r)) != 0 {
		t.Fatal("historical department invented")
	}
	q.Del("department")
	q.Set("actor_ref", ids.New().String())
	if r := auditHTTP(t, server.URL, admin.token, q); r.status != http.StatusOK || len(auditRows(t, r)) != 0 {
		t.Fatal("actor filter leaked")
	}
	q.Del("actor_ref")
	q.Set("module", "unknown")
	if r := auditHTTP(t, server.URL, admin.token, q); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown module%d", r.status)
	}
	q.Del("module")
	q.Set("tenant", other.tenant.String())
	if r := auditHTTP(t, server.URL, admin.token, q); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("tenant input%d", r.status)
	}
	// Even the internal reader rejects identity/tenant mismatches.
	p := authz.Principal{ActorRef: admin.actor, TenantID: f.tenant, MFA: true}
	if _, err = mod.NativeAuditSource().Read(authz.WithPrincipal(context.Background(), p), other.tenant, port.AuditReadQuery{}); err == nil {
		t.Fatal("native reader crossed tenant")
	}
}
func TestAuditHTTPMissingDependenciesAdminMFAAndRecorderFailure(t *testing.T) {
	f := newFixture(t)
	obs := &auditObserver{Beginner: db.ForModule(f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), accounts.DBModule)}
	mod := accounts.New(obs, clock.System{}, accounts.Deps{})
	reg, err := port.NewAuditRegistry(mod.NativeAuditSource())
	if err != nil {
		t.Fatal(err)
	}
	eng := httpx.NewEngine(telemetry.NewLogger("audit-test", io.Discard), 1<<20)
	mod.RegisterAdmin(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier())))
	srv := httptest.NewServer(eng)
	defer srv.Close()
	root := f.as(f.tenant, f.national, authz.RolePlatformAdmin)
	now := time.Now().UTC()
	q := url.Values{"from": {now.Add(-time.Hour).Format(time.RFC3339Nano)}, "to": {now.Add(time.Hour).Format(time.RFC3339Nano)}}
	if r := auditHTTP(t, srv.URL, root.token, q); r.status != http.StatusServiceUnavailable || obs.calls != 0 {
		t.Fatal("unconfigured audit exposed")
	}
	authCalls, recordCalls := 0, 0
	access := port.AuditAccess{Authorize: func(ctx context.Context) error {
		authCalls++
		_, err := authz.Require(ctx, authz.RolePlatformAdmin)
		return err
	}, RecordRead: func(context.Context, port.AuditReadReceipt) error { recordCalls++; return nil }}
	mod.UseAudit(reg, access)
	officer := f.as(f.tenant, f.team30292, "kn_officer")
	if r := auditHTTP(t, srv.URL, officer.token, q); r.status != http.StatusForbidden || obs.calls != 0 {
		t.Fatal("non-Admin read audit")
	}
	noMFA := f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tenant, Roles: []string{authz.RolePlatformAdmin}, MFA: false})
	beforeAuth := authCalls
	if r := auditHTTP(t, srv.URL, noMFA, q); r.status != http.StatusForbidden || str(r.body, "code") != "mfa_required" || authCalls != beforeAuth {
		t.Fatal("MFA bypass")
	}
	mod.UseAudit(reg, port.AuditAccess{Authorize: access.Authorize})
	if r := auditHTTP(t, srv.URL, root.token, q); r.status != http.StatusServiceUnavailable || obs.calls != 0 {
		t.Fatal("missing recorder reached source")
	}
	access.RecordRead = func(context.Context, port.AuditReadReceipt) error {
		recordCalls++
		return errors.New("recorder offline")
	}
	mod.UseAudit(reg, access)
	if r := auditHTTP(t, srv.URL, root.token, q); r.status != http.StatusServiceUnavailable || r.body["items"] != nil || recordCalls != 1 || obs.calls != 1 {
		t.Fatal("recorder failure released data")
	}
	access.Authorize = func(context.Context) error { return problem.Forbidden }
	mod.UseAudit(reg, access)
	if r := auditHTTP(t, srv.URL, root.token, q); r.status != http.StatusForbidden || obs.calls != 1 {
		t.Fatal("owner denial fell back to realm Admin")
	}
	q.Set("from", now.Add(-94*24*time.Hour).Format(time.RFC3339Nano))
	access.Authorize = func(context.Context) error { return nil }
	mod.UseAudit(reg, access)
	if r := auditHTTP(t, srv.URL, root.token, q); r.status != http.StatusUnprocessableEntity || obs.calls != 1 {
		t.Fatal("unbounded read accepted")
	}
}

func TestAuditNativeHTTPNanosecondFiltersAndReceipt(t *testing.T) {
	f := newFixture(t)
	nat := f.nationalAdmin()
	user := nat.create(person(f.coop30292, "coop_manager"))
	var first time.Time
	var expected int
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT min(recorded_at),count(*) FROM pf_accounts.account_event WHERE user_ref=$1 AND actor_ref=$2 AND action='created'`, user, nat.actor).Scan(&first, &expected)
	})
	if err != nil || expected == 0 {
		t.Fatal("missing native audit fixture", err)
	}
	mod := accounts.New(f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, accounts.Deps{})
	registry, err := port.NewAuditRegistry(mod.NativeAuditSource())
	if err != nil {
		t.Fatal(err)
	}
	var receipt port.AuditReadReceipt
	mod.UseAudit(registry, port.AuditAccess{
		Authorize:  func(ctx context.Context) error { _, err := authz.Require(ctx, authz.RolePlatformAdmin); return err },
		RecordRead: func(_ context.Context, r port.AuditReadReceipt) error { receipt = r; return nil },
	})
	eng := httpx.NewEngine(telemetry.NewLogger("audit-nano", io.Discard), 1<<20)
	mod.RegisterAdmin(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier())))
	server := httptest.NewServer(eng)
	defer server.Close()
	admin := f.as(f.tenant, f.national, authz.RolePlatformAdmin)
	q := url.Values{"from": {first.Add(125 * time.Nanosecond).Format(time.RFC3339Nano)}, "to": {first.Add(time.Hour + 875*time.Nanosecond).Format(time.RFC3339Nano)}, "actor_ref": {nat.actor.String()}, "action": {"account.created"}, "module": {"accounts"}, "limit": {"50"}}
	r := auditHTTP(t, server.URL, admin.token, q)
	if r.status != http.StatusOK || len(auditRows(t, r)) != expected {
		t.Fatalf("nanosecond from produced status=%d body=%v", r.status, r.body)
	}
	if !receipt.From.Equal(first) || !receipt.To.Equal(first.Add(time.Hour)) || receipt.FilterActorRef != nat.actor || receipt.ActorRef != admin.actor || receipt.Module != "accounts" || receipt.Action != "account.created" || receipt.Limit != 50 || receipt.PurposeCode != port.AuditPurposeView || len(receipt.Returned) != expected {
		t.Fatal("durable recorder did not receive exact normalized filters/purpose")
	}
}
