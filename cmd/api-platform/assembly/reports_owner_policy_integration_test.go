package assembly

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestReportsOwnerAuthorizationPrecedesHTTPIdempotencyCache(t *testing.T) {
	env := pgtest.Get(t)
	tid, actor := ids.New(), ids.New()
	active := atomic.Bool{}
	active.Store(true)
	calls := atomic.Int32{}
	registration := port.SourceBuilderRegistration{Source: "example.audit",
		Build: func(context.Context, port.SourceBuildQuery) (port.SourceData, error) { return port.SourceData{}, nil },
		JobPolicy: &port.SourceJobPolicy{Version: 1, ScopeMode: port.SourceScopeTenant,
			Freeze: func(_ context.Context, q port.SourceFreezeRequest) (json.RawMessage, error) {
				var filter struct {
					Filter string `json:"filter"`
					AsOf   string `json:"as_of"`
				}
				decoder := json.NewDecoder(bytes.NewReader(q.SourceArgs))
				decoder.DisallowUnknownFields()
				if decoder.Decode(&filter) != nil || filter.Filter == "" {
					return nil, problem.Validation
				}
				if filter.AsOf == "" {
					filter.AsOf = q.RequestedAt.Format(time.RFC3339Nano)
				}
				return json.Marshal(filter)
			},
			AuthorizeJob: func(c context.Context, a port.SourceJobAccess) error {
				calls.Add(1)
				p, ok := authz.FromContext(c)
				if !ok || !p.MFA || p.ActorRef != actor || p.TenantID != tid || a.ActorRef != actor || a.RequestedBy != actor || a.TenantID != tid || !active.Load() {
					return problem.Forbidden.WithDetail("private-marker authorization")
				}
				return nil
			}}}
	catalog := port.CatalogRegistration{
		Sources: map[string]port.CatalogSource{"example.audit": {Key: "example.audit", Module: "example", Levels: []string{"province"}, DefaultLevel: "province",
			Columns: []port.CatalogColumn{{Key: "amount", Title: "Quantity", Kind: "text"}}}},
		Templates: []port.CatalogTemplate{{Key: "example.audit_layout", Def: port.CatalogDefinition{Title: "Owner report", Source: "example.audit", Grouping: "province",
			Columns: []port.CatalogTemplateColumn{{Key: "amount", Title: "Quantity"}}, Formats: []string{"xlsx"}}}},
	}
	rootPool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	mod := reports.New(rootPool, clock.NewFixed(time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)),
		reports.Deps{Builders: []port.SourceBuilderRegistration{registration}, Catalog: []port.CatalogRegistration{catalog}})
	issuer := authztest.New(t)
	router := NewRouter(Deps{Log: telemetry.NewLogger("test", io.Discard), Verifier: issuer.Verifier(), Ops: rootPool, Modules: []httpx.Module{mod}})
	server := httptest.NewServer(router)
	defer server.Close()
	token := issuer.Sign(t, authztest.Token{Subject: actor, TenantID: tid, Roles: []string{authz.RolePlatformAdmin}, MFA: true})
	requestBaseURL := server.URL
	postRaw := func(key, raw string) (int, string, bool) {
		req, e := http.NewRequest(http.MethodPost, requestBaseURL+"/v1/reports/exports", strings.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(idempotency.Header, key)
		response, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() {
			if e := response.Body.Close(); e != nil {
				t.Errorf("close response: %v", e)
			}
		}()
		body, _ := io.ReadAll(response.Body)
		if bytes.Contains(body, []byte("private-marker")) || bytes.Contains(body, []byte(`"source_args":`)) || bytes.Contains(body, []byte("_reports_source")) {
			t.Fatalf("HTTP body leaked filter: %s", body)
		}
		return response.StatusCode, string(body), response.Header.Get(idempotency.ReplayedHeader) == "true"
	}
	post := func(key, filter string) (int, string, bool) {
		return postRaw(key, `{"template_key":"example.audit_layout","format":"xlsx","params":{"level":"province"},"source_args":{"filter":"`+filter+`"}}`)
	}
	status, first, replayed := post("owner-http-cache-01", "private-marker")
	if status != http.StatusAccepted || replayed {
		t.Fatalf("first request: %d %s", status, first)
	}
	before := calls.Load()
	status, second, replayed := post("owner-http-cache-01", "private-marker")
	if status != http.StatusAccepted || !replayed || first != second || calls.Load() <= before {
		t.Fatalf("cache replay skipped live guard: %d %s", status, second)
	}
	// Same token, roles, scope, key, URL and body; only owner DB authority changes.
	active.Store(false)
	status, body, replayed := post("owner-http-cache-01", "private-marker")
	if status != http.StatusForbidden || replayed {
		t.Fatalf("revoked owner received cached 202: %d %s", status, body)
	}
	active.Store(true)
	status, body, _ = postRaw("owner-http-private-02", `{"template_key":"example.audit_layout","format":"xlsx","params":{"level":"province"},"source_args":{"filter":"private-marker","_reports_source":{"job_id":"forged"}}}`)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("private envelope injection accepted: %d %s", status, body)
	}
	status, body, _ = postRaw("owner-http-params-03", `{"template_key":"example.audit_layout","format":"xlsx","params":{"level":"province","_reports_source":{"source":"forged","job_id":"forged"}},"source_args":{"filter":"private-marker"}}`)
	if status != http.StatusAccepted {
		t.Fatalf("legacy unknown Params fields changed behavior: %d %s", status, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	var source, storedID string
	if err := env.AdminPool(t, migrations.PF).QueryRow(context.Background(), "SELECT params->'_reports_source'->>'source',params->'_reports_source'->>'job_id' FROM pf_reports.export_job WHERE tenant_id=$1 AND id=$2::uuid", tid, created.ID).Scan(&source, &storedID); err != nil {
		t.Fatal(err)
	}
	if source != "example.audit" || storedID != created.ID {
		t.Fatal("unknown Params fields replaced trusted private envelope")
	}
	status, body, _ = post("owner-http-cache-01", "different")
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("changed filter was replayed: %d %s", status, body)
	}
	// A restarted instance without the last owner policy shares the same cache.
	// It must still recognize a stored private job before cached 202 is released.
	removed := reports.New(rootPool, clock.NewFixed(time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)), reports.Deps{Catalog: []port.CatalogRegistration{catalog}})
	removedServer := httptest.NewServer(NewRouter(Deps{Log: telemetry.NewLogger("test", io.Discard), Verifier: issuer.Verifier(), Ops: rootPool, Modules: []httpx.Module{removed}}))
	defer removedServer.Close()
	requestBaseURL = removedServer.URL
	status, body, replayed = post("owner-http-cache-01", "private-marker")
	if status != http.StatusServiceUnavailable || replayed {
		t.Fatalf("removed policy returned cached job: %d %s", status, body)
	}
}
