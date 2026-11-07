package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type websiteNetworkTrap struct{ calls atomic.Int32 }

func (x *websiteNetworkTrap) RoundTrip(*http.Request) (*http.Response, error) {
	x.calls.Add(1)
	return nil, errors.New("website request attempted network access")
}

// FN-TI-02.07 is deferred by D2: a request records the disconnected state;
// approval stays usable inside KN and the request never queues external work.
func TestWebsiteDeferredHTTP(t *testing.T) {
	network := &websiteNetworkTrap{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = network
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	now := time.Date(2026, 11, 6, 6, 0, 0, 0, time.UTC)
	day := domain.BusinessDay(now)
	tid, actor := ids.New(), ids.New()
	prof := port.Profile{Session: testOwnerSession, Name: "kn", WebsiteDeferred: true, Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA", "kn_tw": "TW"}, ManageBy: []string{"admin"}}
	svc, err := app.NewService(db.ForModule(pool, app.DBModule), clock.NewFixed(now), []port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	tickets := app.NewTicketService(svc)
	err = db.WithModuleTenantTx(context.Background(), pool, app.DBModule, tid, func(tx pgx.Tx) error {
		wk := domain.DefaultWeekOf(day)
		_, e := store.InsertBoard(context.Background(), tx, ids.New(), tid, store.BoardWrite{
			Profile: "kn", Day: day, Action: domain.BoardApprove, Status: domain.BoardApproved,
			Version: 1, ApprovedAt: &now, WeekKey: wk.Key, WeekYear: wk.Year, WeekFrom: wk.From,
			WeekTo: wk.To, DaysScheduled: 5,
		}, now, actor, "268:seed:approved")
		if e != nil {
			return e
		}
		_, e = store.InsertBoard(context.Background(), tx, ids.New(), tid, store.BoardWrite{
			Profile: "kn", Day: day.AddDate(0, 0, -2), Action: domain.BoardReopen, Status: domain.BoardReopened,
			Version: 1, ApprovedAt: &now, Reason: "Đối chiếu giá đang sửa",
		}, now, actor, "268:seed:reopened")
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(telemetry.NewLogger("api-platform", &logs))
	t.Cleanup(func() { slog.SetDefault(previous) })
	r := gin.New()
	r.Use(func(c *gin.Context) {
		role := c.GetHeader("X-Test-Role")
		if role == "" {
			role = "kn_tinh"
		}
		p := bindTestSession(t, authz.Principal{TenantID: tid, ActorRef: actor, KN: &authz.KNContext{Role: role, ViewOnly: c.GetHeader("X-Test-Readonly") == "1"}})
		c.Request = c.Request.WithContext(telemetry.WithTraceID(authz.WithPrincipal(c.Request.Context(), p), "26800000000000000000000000000001"))
	})
	r.Use(idempotency.Middleware(db.ForModule(pool, db.Ops)))
	BoardHandler{Svc: tickets}.RegisterApp(r.Group("/v1"))
	admin := env.AdminPool(t, migrations.PF)
	var boardHash string
	if err := admin.QueryRow(context.Background(), `SELECT encode(row_hash,'hex') FROM pf_price.board WHERE tenant_id=$1 AND board_date=$2::date`, tid, day).Scan(&boardHash); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/price/boards/2026-11-06/website", strings.NewReader(`{"seq":1}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "268-website-request")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("website deferred request: got %d want 201: %s", response.Code, response.Body.String())
	}
	body := calJSON(t, response.Body.Bytes())
	website, ok := body["website"].(map[string]any)
	if !ok || website["status"] != "not_connected" {
		t.Fatalf("website request must report not_connected: %v", body)
	}
	if website["requested_at"] != now.Format(time.RFC3339) || fmt.Sprint(website["version"]) != "1" || fmt.Sprint(body["seq"]) != "1" || body["state"] != "approved" {
		t.Fatalf("ack changed board or claimed delivery: %v", body)
	}
	if _, exists := website["published_at"]; exists {
		t.Fatal("disconnected response must not claim publication")
	}
	// A retried request has the same local acknowledgement, without another ledger entry.
	again := httptest.NewRequest(http.MethodPost, request.URL.String(), strings.NewReader(`{"seq":1}`))
	again.Header = request.Header.Clone()
	replay := httptest.NewRecorder()
	r.ServeHTTP(replay, again)
	if replay.Code != 201 {
		t.Fatalf("idempotent retry: %d %s", replay.Code, replay.Body.String())
	}
	if replay.Header().Get(idempotency.ReplayedHeader) != "true" {
		t.Fatal("retry did not use actual idempotency middleware")
	}
	// Also cover crash recovery past the middleware: the ledger itself de-duplicates.
	retryCtx := authz.WithPrincipal(context.Background(), bindTestSession(t, authz.Principal{TenantID: tid, ActorRef: actor, KN: &authz.KNContext{Role: "kn_tinh"}}))
	if _, err := tickets.RequestWebsite(retryCtx, "kn", app.BoardInput{Day: day, Version: 1}, "268-website-request"); err != nil {
		t.Fatalf("ledger-level retry: %v", err)
	}
	var attempts int
	if err := admin.QueryRow(context.Background(), `SELECT count(*) FROM pf_price.board_website_attempt WHERE tenant_id=$1`, tid).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("retry appended attempts=%d err=%v", attempts, err)
	}
	for _, tc := range []struct {
		role, readonly, day, key string
		seq, status              int
	}{
		{"kn_tw", "", "2026-11-06", "268-denied-tw", 1, 403},
		{"kn_xa", "", "2026-11-06", "268-denied-xa", 1, 403},
		{"kn_tinh", "1", "2026-11-06", "268-denied-readonly", 1, 403},
		{"admin", "1", "2026-11-06", "268-denied-admin-readonly", 1, 403},
		{"kn_tinh", "", "2026-11-06", "268-stale", 0, 409},
		{"kn_tinh", "", "2026-11-05", "268-unapproved", 0, 409},
		{"kn_tinh", "", "2026-11-04", "268-reopened", 1, 409},
		{"admin", "", "2026-11-06", "268-admin-ack", 1, 201},
	} {
		q := httptest.NewRequest(http.MethodPost, "/v1/price/boards/"+tc.day+"/website", strings.NewReader(fmt.Sprintf(`{"seq":%d}`, tc.seq)))
		q.Header.Set("Content-Type", "application/json")
		q.Header.Set("Idempotency-Key", tc.key)
		q.Header.Set("X-Test-Role", tc.role)
		q.Header.Set("X-Test-Readonly", tc.readonly)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		if w.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
	}
	var afterHash string
	var outbox int
	if err := admin.QueryRow(context.Background(), `SELECT encode(row_hash,'hex') FROM pf_price.board WHERE tenant_id=$1 AND board_date=$2::date`, tid, day).Scan(&afterHash); err != nil || boardHash != afterHash {
		t.Fatalf("board hash changed: %s %s %v", boardHash, afterHash, err)
	}
	if err := admin.QueryRow(context.Background(), `SELECT count(*) FROM shared.outbox WHERE tenant_id=$1`, tid).Scan(&outbox); err != nil || outbox != 0 {
		t.Fatalf("ack queued work: %d %v", outbox, err)
	}
	if err := admin.QueryRow(context.Background(), `SELECT count(*) FROM pf_price.board_website_attempt WHERE tenant_id=$1`, tid).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatalf("invalid requests wrote attempts: %d %v", attempts, err)
	}
	err = db.WithModuleTenantTx(context.Background(), pool, app.DBModule, ids.New(), func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM pf_price.board_website_attempt`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Fatalf("attempt RLS exposed %d rows to another tenant", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`UPDATE pf_price.board_website_attempt SET status='not_connected'`, `DELETE FROM pf_price.board_website_attempt`} {
		err := db.WithModuleTenantTx(context.Background(), pool, app.DBModule, tid, func(tx pgx.Tx) error { _, err := tx.Exec(context.Background(), statement); return err })
		if err == nil {
			t.Fatalf("append-only audit allowed %s", statement)
		}
	}
	var entry map[string]any
	first := strings.Split(strings.TrimSpace(logs.String()), "\n")[0]
	if err := json.Unmarshal([]byte(first), &entry); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ts", "service", "trace_id", "tenant_id", "actor_ref", "event", "outcome", "profile", "board_date", "board_version"} {
		if _, ok := entry[k]; !ok {
			t.Fatalf("audit missing %s: %s", k, first)
		}
	}
	if entry["event"] != "price.website.requested" || entry["outcome"] != "not_connected" {
		t.Fatalf("untruthful audit: %s", first)
	}
	for _, k := range []string{"url", "phone", "name", "reason", "published_at"} {
		if _, ok := entry[k]; ok {
			t.Fatalf("audit contains %s", k)
		}
	}

	if network.calls.Load() != 0 {
		t.Fatalf("website request made %d network calls", network.calls.Load())
	}

}
