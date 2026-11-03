package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type review204Env struct {
	t       *testing.T
	r       *gin.Engine
	clk     *clock.Fixed
	tid     uuid.UUID
	admin   *pgxpool.Pool
	who     map[string]authz.Principal
	ticket  string
	version any
}

func newReview204(t *testing.T, withAlert ...bool) *review204Env {
	t.Helper()
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC))
	prof := port.Profile{Session: testOwnerSession, Name: "kn", Department: "thong_tin_truyen_thong", Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA", "kn_tw": "TW"}, ManageBy: []string{"admin"}, StewardDuty: authz.DutyPriceAdmin}
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	svc, err := app.NewService(db.ForModule(pool, app.DBModule), clk, []port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	items, tickets := app.NewItemService(svc), app.NewTicketService(svc)
	svc.Points.Submitted = tickets.Submitted
	items.Hooks.HasPrice, items.Hooks.HasSheets = tickets.HasPrice, tickets.Submitted
	tid := ids.New()
	xa, tinh := ids.New(), ids.New()
	e := &review204Env{t: t, clk: clk, tid: tid, admin: env.AdminPool(t, migrations.PF), who: map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh":  {ActorRef: tinh, TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Duties: []string{authz.DutyPriceAdmin}}},
		"other": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh"}},
		"xa":    {ActorRef: xa, TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"tw":    {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tw"}},
	}}
	gin.SetMode(gin.TestMode)
	router := func(replay bool) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if p, ok := e.who[c.GetHeader("X-Test-As")]; ok {
				c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), bindTestSession(t, p)))
			}
			c.Next()
			for _, x := range c.Errors {
				t.Logf("HTTP error: %v", x.Err)
			}
		})
		if replay {
			r.Use(idempotency.Middleware(db.ForModule(pool, db.Ops)))
		}
		g := r.Group("/v1")
		Handler{Svc: svc}.RegisterApp(g)
		PointHandler{Svc: svc}.RegisterApp(g)
		ItemHandler{Svc: items}.RegisterApp(g)
		TicketHandler{Svc: tickets}.RegisterApp(g)
		return r
	}
	e.r = router(false)
	e.call("admin", "POST", "/v1/price/catalog/defaults", nil, "", 201)
	seedCommunes(t, tid, "2025-07-01", "", "R204A", "R204B")
	for _, code := range []string{"R204A", "R204B"} {
		e.call("admin", "POST", "/v1/price/points", map[string]any{"admin_unit_code": code, "region_code": "an_giang", "valid_from": "2026-10-05"}, "", 201)
	}
	for _, a := range []struct {
		role string
		ref  uuid.UUID
	}{{"xa", xa}, {"tinh", tinh}} {
		e.call("admin", "POST", "/v1/price/points/XBG0001/officers", map[string]any{"role": a.role, "officer_ref": a.ref.String(), "valid_from": "2026-10-05"}, "", 201)
	}
	e.call("tinh", "POST", "/v1/price/items", map[string]any{"name": "Dưa leo204", "variety": "Loại1", "group_code": "rau_mau", "unit_code": "kg", "kind": "basic", "valid_from": "2026-10-05"}, "", 201)
	if len(withAlert) > 0 && withAlert[0] {
		e.call("tinh", "POST", "/v1/price/tickets/proxy", map[string]any{"point": "XBG0001", "day": "2026-10-07", "version": 0, "submit": true, "reason": "Giá tham chiếu204", "lines": []map[string]any{{"item_code": "MH00001", "min": "100", "max": "100"}}}, "", 201)
	}
	sh := e.call("xa", "POST", "/v1/price/tickets/sheet", map[string]any{"point": "XBG0001", "day": "2026-10-08", "version": 0, "submit": true, "lines": []map[string]any{{"item_code": "MH00001", "min": "00001.2300", "max": "9000.00", "note": "Đã đối chiếu204"}}}, "", 201)
	e.ticket = sh["ticket"].(map[string]any)["id"].(string)
	e.version = sh["ticket"].(map[string]any)["version"]
	clk.Advance(2 * time.Hour)
	e.r = router(true)
	return e
}
func (e *review204Env) raw(as, method, path string, body any, key string) *httptest.ResponseRecorder {
	e.t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-As", as)
	if method == "POST" {
		if key == "" {
			key = ids.New().String()
		}
		req.Header.Set(idempotency.Header, key)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}
func (e *review204Env) call(as, method, path string, body any, key string, want int) map[string]any {
	e.t.Helper()
	w := e.raw(as, method, path, body, key)
	if w.Code != want {
		e.t.Fatalf("%s %s: %d want%d: %s", as, path, w.Code, want, w.Body.String())
	}
	return calJSON(e.t, w.Body.Bytes())
}
func (e *review204Env) counts() [3]int64 {
	e.t.Helper()
	var n [3]int64
	err := e.admin.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM pf_price.ticket WHERE tenant_id=$1),(SELECT count(*) FROM pf_price.ticket_line WHERE tenant_id=$1),(SELECT count(*) FROM pf_price.price_alert WHERE tenant_id=$1)`, e.tid).Scan(&n[0], &n[1], &n[2])
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}
func (e *review204Env) correction(lo, hi string) map[string]any {
	return map[string]any{"version": e.version, "reason": "Đối chiếu phiếu204", "lines": []map[string]any{{"item_code": "MH00001", "min": lo, "max": hi}}}
}

func TestPriceReview204NativeRejectExponent(t *testing.T) {
	e := newReview204(t)
	before := e.counts()
	w := e.raw("tinh", "POST", "/v1/price/tickets/"+e.ticket+"/correct", e.correction("1e3", "1000"), "204-exponent-red")
	after := e.counts()
	if w.Code != 422 || before != after {
		t.Fatalf("small exponent must422/no ledger write; got%d before%v after%v: %s", w.Code, before, after, w.Body.String())
	}
}

func TestPriceReview204HostileNoWrites(t *testing.T) {
	e := newReview204(t)
	for i, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), strings.Repeat("0", 513), "1E3"} {
		for _, field := range []string{"min", "max"} {
			t.Run(fmt.Sprintf("%d/%s", i, field), func(t *testing.T) {
				lo, hi := "1", "1000"
				if field == "min" {
					lo = raw
				} else {
					hi = raw
				}
				before := e.counts()
				w := e.raw("tinh", "POST", "/v1/price/tickets/"+e.ticket+"/correct", e.correction(lo, hi), "")
				if w.Code != 422 || before != e.counts() {
					t.Fatalf("hostile %s got%d or wrote: %s", field, w.Code, w.Body.String())
				}
			})
		}
	}
}

// Real ledger + real HTTP idempotency, not a client/mock substitute for persistence.
func TestPriceReview204LifecycleAndReplay(t *testing.T) {
	e := newReview204(t)
	path := "/v1/price/tickets/" + e.ticket
	before := e.counts()
	for _, as := range []string{"other", "xa", "tw"} {
		e.call(as, "POST", path+"/correct", e.correction("2", "9000"), "", 403)
		if before != e.counts() {
			t.Fatalf("%s wrote despite denial", as)
		}
	}
	e.call("", "GET", path, nil, "", 401)
	e.call("tw", "GET", path+"/log", nil, "", 403)
	e.who["tenantB"] = authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), KN: &authz.KNContext{Role: "kn_tinh"}}
	e.call("tenantB", "GET", path, nil, "", 404)
	e.call("tenantB", "POST", path+"/correct", e.correction("2", "9000"), "", 404)
	if before != e.counts() {
		t.Fatal("foreign tenant changed native ledgers")
	}
	body := e.correction("00002.5000", "9000.00")
	key := "review204-correct-replay"
	first := e.raw("tinh", "POST", path+"/correct", body, key)
	if first.Code != 201 {
		t.Fatalf("valid pair: %d %s", first.Code, first.Body.String())
	}
	after := e.counts()
	if after[0] != before[0]+1 || after[1] != before[1]+1 {
		t.Fatalf("correct ledger %v->%v", before, after)
	}
	replay := e.raw("tinh", "POST", path+"/correct", body, key)
	if replay.Code != 201 || replay.Body.String() != first.Body.String() || after != e.counts() {
		t.Fatalf("retry not exact replay: %d %s", replay.Code, replay.Body.String())
	}
	changed := e.correction("3", "9000")
	e.call("tinh", "POST", path+"/correct", changed, key, 409)
	e.call("tinh", "POST", path+"/correct", changed, "", 409)
	for _, raw := range []string{"0", "-1", "1.234", "1000000000000", strings.Repeat("9", 512)} {
		b := e.correction(raw, raw)
		b["version"] = 2
		e.call("tinh", "POST", path+"/correct", b, "", 422)
		if after != e.counts() {
			t.Fatal("domain-invalid price wrote")
		}
	}
	sh := e.call("tinh", "GET", path, nil, "", 200)
	if fmt.Sprint(sh["ticket"].(map[string]any)["version"]) != "2" {
		t.Fatalf("version %v", sh)
	}
	log := e.call("tinh", "GET", path+"/log", nil, "", 200)
	lines := log["lines"].([]any)
	if len(lines) != 2 {
		t.Fatalf("native line versions %v", lines)
	}
	found := false
	for _, v := range lines {
		l := v.(map[string]any)
		if l["adj_min"] == "2.5" {
			found = true
			if l["min"] != "1.23" || l["max"] != "9000" || l["actor_ref"] != e.who["tinh"].ActorRef.String() || l["at"] != "2026-10-08T05:00:00Z" || l["adj_reason"] != "Đối chiếu phiếu204" {
				t.Fatalf("log false provenance: %v", l)
			}
		}
	}
	if !found {
		t.Fatalf("missing correction %v", lines)
	}
	badDay := map[string]any{"version": 2, "day": "2026-10-09", "reason": "Ngày tương lai"}
	e.call("tinh", "POST", path+"/redate", badDay, "", 422)
	moved := e.call("tinh", "POST", path+"/redate", map[string]any{"version": 2, "day": "2026-10-07", "reason": "Sửa nhầm ngày"}, "", 201)
	version := moved["ticket"].(map[string]any)["version"]
	del := map[string]any{"version": version, "reason": "Phiếu sai204"}
	e.call("tinh", "POST", path+"/delete", del, "review204-delete-replay", 201)
	deleted := e.counts()
	e.call("tinh", "POST", path+"/delete", del, "review204-delete-replay", 201)
	if deleted != e.counts() {
		t.Fatal("delete retry appended")
	}
	final := e.call("tinh", "GET", path, nil, "", 200)
	if final["view"] != "deleted" {
		t.Fatalf("not softdeleted %v", final)
	}
	finalLog := e.call("tinh", "GET", path+"/log", nil, "", 200)
	if len(finalLog["ticket"].([]any)) != 4 || len(finalLog["lines"].([]any)) != 2 {
		t.Fatalf("history missing: %v", finalLog)
	}
}

func TestPriceReview204SharedGuardAndKeep(t *testing.T) {
	e := newReview204(t, true)
	e.call("tinh", "POST", "/v1/price/alerts/request", map[string]any{"alerts": []map[string]any{{"ticket_id": e.ticket, "item_code": "MH00001"}}, "message": "Đề nghị đối chiếu204"}, "", 201)
	before := e.counts()
	for _, field := range []string{"min", "max"} {
		for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000)} {
			for _, flow := range []string{"entry", "proxy", "respond"} {
				var body map[string]any
				as, path := "xa", "/v1/price/tickets/sheet"
				if flow == "respond" {
					body = map[string]any{"ticket_id": e.ticket, "item_code": "MH00001", "choice": "keep", "explanation": "Giữ giá204", field: raw}
					path = "/v1/price/alerts/respond"
				} else {
					body = map[string]any{"point": "XBG0001", "day": "2026-10-08", "version": e.version, "submit": false, "lines": []map[string]any{{"item_code": "MH00001", "min": "1", "max": "1000"}}}
					body["lines"].([]map[string]any)[0][field] = raw
					if flow == "proxy" {
						as = "tinh"
						path = "/v1/price/tickets/proxy"
						body["point"] = "XBG0002"
						body["version"] = 0
						body["reason"] = "Nhập thay204"
					}
				}
				e.call(as, "POST", path, body, "", 422)
				if before != e.counts() {
					t.Fatalf("%s/%s hostile wrote", flow, field)
				}
			}
		}
	}
	// Keep deliberately discards bounded prices after parsing: no new money/scale cap here.
	e.call("xa", "POST", "/v1/price/alerts/respond", map[string]any{"ticket_id": e.ticket, "item_code": "MH00001", "choice": "keep", "explanation": "Giữ giá đã kiểm204", "min": "-1.234", "max": "1000000000000"}, "", 201)
	after := e.counts()
	if after[0] != before[0] || after[1] != before[1] || after[2] != before[2]+1 {
		t.Fatalf("keep must only append response: %v -> %v", before, after)
	}
	log := e.call("tinh", "GET", "/v1/price/tickets/"+e.ticket+"/log", nil, "", 200)
	for _, a := range log["alerts"].([]any) {
		entry := a.(map[string]any)["entry"].(map[string]any)
		if entry["response"] == "keep" && (entry["proposed_min"] != nil || entry["proposed_max"] != nil) {
			t.Fatalf("keep retained ignored prices: %v", entry)
		}
	}
}
