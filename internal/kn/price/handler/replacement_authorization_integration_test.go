package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Native owner-session codec + real HTTP service/PG ledgers. The test adapter
// binds synthetic trusted sessions; it does not claim a deployed OIDC realm.
type replacement198 struct {
	t     *testing.T
	pool  *pgxpool.Pool
	items *app.ItemService
	r     *gin.Engine
	tid   uuid.UUID
	n     int
	old   string
	other uuid.UUID
}

func newReplacement198(t *testing.T) *replacement198 {
	t.Helper()
	env := pgtest.Get(t)
	_ = env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	dsn, err := dbadmin.RoleURL(env.PoolURL, migrations.PF, env.Suffix, dbadmin.AppPlatform)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.Open(context.Background(), dsn, db.Options{ViaPgBouncer: true, MaxConns: 1, AppName: "pgtest-price198"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := &replacement198{t: t, pool: pool, tid: ids.New()}
	profile := port.Profile{Session: testOwnerSession, Name: "kn", Department: "thong_tin_truyen_thong", Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA", "kn_tw": "TW"}, ManageBy: []string{"admin"}, StewardDuty: authz.DutyPriceAdmin}
	svc, err := app.NewService(db.ForModule(pool, app.DBModule), clock.NewFixed(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)), []port.Profile{profile})
	if err != nil {
		t.Fatal(err)
	}
	f.items = app.NewItemService(svc)
	who := map[string]authz.Principal{
		"admin":     {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"steward":   {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "kn_tinh", Department: "thong_tin_truyen_thong", Duties: []string{authz.DutyPriceAdmin}}},
		"plain":     {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "kn_tinh", Department: "thong_tin_truyen_thong"}},
		"tw":        {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "kn_tw"}},
		"xa":        {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"other":     {ActorRef: ids.New(), TenantID: ids.New(), KN: &authz.KNContext{Role: "kn_tinh", Duties: []string{authz.DutyPriceAdmin}}},
		"view_only": {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "kn_tinh", Duties: []string{authz.DutyPriceAdmin}, ViewOnly: true}},
	}
	f.other = who["other"].TenantID
	who["other_admin"] = authz.Principal{ActorRef: ids.New(), TenantID: f.other, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}}
	gin.SetMode(gin.TestMode)
	f.r = gin.New()
	f.r.Use(func(c *gin.Context) {
		as := c.GetHeader("X-Test-As")
		if as == "missing_session" || as == "stale_session" {
			as = "steward"
		}
		if p, ok := who[as]; ok {
			p = bindTestSession(t, p)
			switch c.GetHeader("X-Test-As") {
			case "missing_session":
				p.SelectedSession = nil
				p.Roles = []string{"admin"} // native owner must not fall back to realm/legacy roles.
			case "stale_session":
				other := p
				k := *p.KN
				k.ID = ids.New()
				other.KN, other.SelectedSession = &k, nil
				other = bindTestSession(t, other)
				stale := p.SelectedSession.WithPayload(other.SelectedSession.Payload())
				p.SelectedSession = &stale // payload ID no longer matches the selected native session ID.
			}
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	})
	f.r.Use(idempotency.Middleware(db.ForModule(pool, db.Ops)))
	Handler{Svc: svc}.RegisterApp(f.r.Group("/v1"))
	ItemHandler{Svc: f.items}.RegisterApp(f.r.Group("/v1"))
	PointHandler{Svc: svc}.RegisterApp(f.r.Group("/v1"))
	f.call("admin", http.MethodPost, "/v1/price/catalog/defaults", nil, http.StatusCreated)
	seedCommunes(t, f.tid, "2026-01-01", "", "PRICE198")
	point := f.call("admin", http.MethodPost, "/v1/price/points", map[string]any{"admin_unit_code": "PRICE198", "region_code": "an_giang", "valid_from": "2026-10-12"}, http.StatusCreated)
	created := f.call("steward", http.MethodPost, "/v1/price/items", map[string]any{"name": "Xoài gốc198", "variety": "Loại gốc", "group_code": "rau_mau", "unit_code": "kg", "kind": "local", "points": []string{fmt.Sprint(point["code"])}, "valid_from": "2026-10-12"}, http.StatusCreated)
	f.old = fmt.Sprint(created["code"])
	return f
}

func (f *replacement198) request(as, method, path string, body any, key string) *httptest.ResponseRecorder {
	f.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal(err)
	}
	f.n++
	if key == "" {
		key = fmt.Sprintf("price198-%d", f.n)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(raw))
	r.Header.Set("X-Test-As", as)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(idempotency.Header, key)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, r)
	return w
}

func (f *replacement198) call(as, method, path string, body any, want int) map[string]any {
	f.t.Helper()
	w := f.request(as, method, path, body, "")
	if w.Code != want {
		f.t.Fatalf("%s %s %s: status=%d want=%d %s", as, method, path, w.Code, want, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *replacement198) snapshot() [3]int64 { return f.tenantSnapshot(f.tid) }

func (f *replacement198) tenantSnapshot(tid uuid.UUID) [3]int64 {
	f.t.Helper()
	var out [3]int64
	err := db.WithModuleTenantTx(context.Background(), f.pool, app.DBModule, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM pf_price.item), (SELECT count(*) FROM pf_price.item_assignment), (SELECT coalesce(max(substr(code,3)::int),0) FROM pf_price.item)`).Scan(&out[0], &out[1], &out[2])
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *replacement198) replacement() map[string]any {
	return map[string]any{"name": "Xoài thay thế198", "variety": "Loại mới", "group_code": "rau_mau", "unit_code": "kg", "kind": "local", "replaces_code": f.old}
}

func TestPrice198OrdinaryEditorCannotReplace(t *testing.T) {
	f := newReplacement198(t)
	status := f.call("plain", http.MethodGet, "/v1/price/items", nil, http.StatusOK)
	if status["can_edit"] != true || status["can_steward"] != false {
		t.Fatalf("native ordinary editor flags=%v", status)
	}
	before := f.snapshot()
	if before[0] == 0 || before[1] == 0 || before[2] == 0 {
		t.Fatalf("missing nonzero ledger witnesses: %v", before)
	}
	w := f.request("plain", http.MethodPost, "/v1/price/items", f.replacement(), "price198-denied-replacement")
	after := f.snapshot()
	if w.Code != http.StatusForbidden || after != before {
		t.Fatalf("ordinary editor replacement must be 403 / no item-assignment writes or code consumption: status=%d before=%v after=%v", w.Code, before, after)
	}
}

func TestPrice198ReplacementPermissionsAndReplay(t *testing.T) {
	f := newReplacement198(t)
	for _, tc := range []struct {
		as     string
		status int
	}{
		{"tw", http.StatusForbidden}, {"xa", http.StatusForbidden}, {"view_only", http.StatusForbidden},
		{"missing_session", http.StatusPreconditionRequired}, {"stale_session", http.StatusForbidden},
		{"other", http.StatusUnprocessableEntity}, // Empty tenant: next code equals the source, so identity validation rejects it before lookup.
	} {
		t.Run(tc.as, func(t *testing.T) {
			before, otherBefore := f.snapshot(), f.tenantSnapshot(f.other)
			w := f.request(tc.as, http.MethodPost, "/v1/price/items", f.replacement(), "")
			if w.Code != tc.status || f.snapshot() != before || f.tenantSnapshot(f.other) != otherBefore {
				t.Fatalf("%s replacement status=%d want=%d or ledgers changed: %s", tc.as, w.Code, tc.status, w.Body.String())
			}
		})
	}
	unknown := f.replacement()
	unknown["replaces_code"] = "MH99999"
	before := f.snapshot()
	w := f.request("steward", http.MethodPost, "/v1/price/items", unknown, "")
	if w.Code != http.StatusNotFound || f.snapshot() != before {
		t.Fatalf("unknown source must be 404 without writes: status=%d %s", w.Code, w.Body.String())
	}
	key := "price198-replacement-replay"
	w = f.request("steward", http.MethodPost, "/v1/price/items", f.replacement(), key)
	if w.Code != http.StatusCreated {
		t.Fatalf("steward replacement=%d %s", w.Code, w.Body.String())
	}
	after := f.snapshot()
	if after != [3]int64{before[0] + 2, before[1] + 1, before[2] + 1} {
		t.Fatalf("replacement must append retirement + new item + inherited assignment: before=%v after=%v", before, after)
	}
	replay := f.request("steward", http.MethodPost, "/v1/price/items", f.replacement(), key)
	if replay.Code != http.StatusCreated || replay.Header().Get(idempotency.ReplayedHeader) != "true" || replay.Body.String() != w.Body.String() || f.snapshot() != after {
		t.Fatalf("same-key retry must replay the committed result without writes: status=%d replay=%s", replay.Code, replay.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	old := f.call("steward", http.MethodGet, "/v1/price/items/"+f.old+"?at=2026-10-13", nil, http.StatusOK)
	newItem := f.call("steward", http.MethodGet, "/v1/price/items/"+fmt.Sprint(created["code"])+"?at=2026-10-13", nil, http.StatusOK)
	if old["state"] != "retired" || old["valid_to"] != "2026-10-12" || newItem["replaces_code"] != f.old || newItem["new_price"] != true {
		t.Fatalf("old/new effective history=%v / %v", old, newItem)
	}
	previous := f.call("steward", http.MethodGet, "/v1/price/items/"+f.old+"?at=2026-10-12", nil, http.StatusOK)
	if previous["state"] != "active" {
		t.Fatalf("replacement rewrote the earlier catalogue: %v", previous)
	}
	for _, as := range []string{"tw", "xa"} {
		read := f.call(as, http.MethodGet, "/v1/price/items/"+f.old+"?at=2026-10-13", nil, http.StatusOK)
		if read["code"] != f.old {
			t.Fatalf("read-only snapshot lost for %s", as)
		}
		// Observe the existing payload contract for the documented #204 gap;
		// do not make exposure of actor metadata a required read capability.
		history, _ := read["history"].([]any)
		actor, event := false, false
		if len(history) > 0 {
			first, _ := history[0].(map[string]any)
			actor = first["actor_ref"] != nil
			event = first["event_time"] != nil
		}
		t.Logf("existing payload evidence role=%s status=200 actor_ref_present=%t event_time_present=%t (gap #204; synthetic sessions)", as, actor, event)
	}
	// Ordinary Tỉnh retains its existing right to add and revise an unpriced item.
	normal := map[string]any{"name": "Chanh198", "variety": "Không hạt", "group_code": "rau_mau", "unit_code": "kg", "kind": "basic", "valid_from": "2026-10-12"}
	added := f.call("plain", http.MethodPost, "/v1/price/items", normal, http.StatusCreated)
	normal["name"] = "Chanh không hạt198"
	f.call("plain", http.MethodPost, "/v1/price/items/"+fmt.Sprint(added["code"])+"/revise", normal, http.StatusCreated)
}

func TestPrice198StewardHookOutsideTransactionPoolOne(t *testing.T) {
	f := newReplacement198(t)
	calls := 0
	f.items.Hooks.Steward = func(ctx context.Context) bool {
		calls++
		if acquired := f.pool.Stat().AcquiredConns(); acquired != 0 {
			t.Errorf("Steward hook ran while holding %d pool connections", acquired)
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		var one int
		if err := f.pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
			t.Errorf("pool-one hook I/O failed: %v", err)
			return false
		}
		return one == 1
	}
	f.call("steward", http.MethodPost, "/v1/price/items", f.replacement(), http.StatusCreated)
	if calls != 1 {
		t.Fatalf("one external capability decision expected, hook calls=%d", calls)
	}
	f.items.Hooks.Steward = func(context.Context) bool { calls++; return false }
	before := f.snapshot()
	w := f.request("steward", http.MethodPost, "/v1/price/items", f.replacement(), "")
	if w.Code != http.StatusForbidden || f.snapshot() != before || calls != 2 {
		t.Fatalf("denied hook must stop replacement before writes, status=%d calls=%d", w.Code, calls)
	}
}

func TestPrice198MalformedReplacementCodeWritesNothing(t *testing.T) {
	f := newReplacement198(t)
	for _, tc := range []struct{ name, value string }{
		{"large exponent", "1e100000"}, {"small exponent", "1e-100000"}, {"long digits", strings.Repeat("9", 100000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := f.replacement()
			body["replaces_code"] = tc.value
			before := f.snapshot()
			w := f.request("steward", http.MethodPost, "/v1/price/items", body, "")
			if w.Code != http.StatusUnprocessableEntity || f.snapshot() != before {
				t.Fatalf("malformed code length=%d status=%d or item/assignment/code ledger changed", len(tc.value), w.Code)
			}
		})
	}
}

func TestPrice198ReplacementCannotReadOtherTenant(t *testing.T) {
	f := newReplacement198(t)
	// Create a source that exists only in tenant A (MH00003). Tenant B has a
	// valid catalogue, point and assigned MH00001; its next code MH00002 differs
	// from the source, so only the tenant-scoped source lookup can reject it.
	for i := 0; i < 2; i++ {
		created := f.call("steward", http.MethodPost, "/v1/price/items", map[string]any{
			"name": fmt.Sprintf("Nguồn riêng A198-%d", i), "variety": "Loại A", "group_code": "rau_mau", "unit_code": "kg", "kind": "basic", "valid_from": "2026-10-12",
		}, http.StatusCreated)
		f.old = fmt.Sprint(created["code"])
	}
	if f.old != "MH00003" {
		t.Fatalf("tenant A source fixture=%s", f.old)
	}
	f.call("other_admin", http.MethodPost, "/v1/price/catalog/defaults", nil, http.StatusCreated)
	seedCommunes(t, f.other, "2026-01-01", "", "PRICE198B")
	point := f.call("other_admin", http.MethodPost, "/v1/price/points", map[string]any{"admin_unit_code": "PRICE198B", "region_code": "an_giang", "valid_from": "2026-10-12"}, http.StatusCreated)
	first := f.call("other", http.MethodPost, "/v1/price/items", map[string]any{
		"name": "Mặt hàng riêng B198", "variety": "Loại B", "group_code": "rau_mau", "unit_code": "kg", "kind": "local", "points": []string{fmt.Sprint(point["code"])}, "valid_from": "2026-10-12",
	}, http.StatusCreated)
	if first["code"] != "MH00001" {
		t.Fatalf("tenant B first code=%v", first)
	}
	beforeA, beforeB := f.snapshot(), f.tenantSnapshot(f.other)
	if beforeB != [3]int64{1, 1, 1} {
		t.Fatalf("nonzero tenant B item/assignment/counter witnesses=%v", beforeB)
	}
	// The same request without replacement is valid in B (as established above);
	// code MH00003 is absent there although it is readable in A.
	w := f.request("other", http.MethodPost, "/v1/price/items", f.replacement(), "")
	if w.Code != http.StatusNotFound || f.snapshot() != beforeA || f.tenantSnapshot(f.other) != beforeB {
		t.Fatalf("cross-tenant replacement must reach 404 source lookup without either ledger/counter changing: status=%d beforeA=%v afterA=%v beforeB=%v afterB=%v %s", w.Code, beforeA, f.snapshot(), beforeB, f.tenantSnapshot(f.other), w.Body.String())
	}
}
