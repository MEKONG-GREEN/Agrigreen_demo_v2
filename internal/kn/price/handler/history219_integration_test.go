package handler

import (
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
)

// #219 proves the unchanged history contract against native PG: complete range,
// official holiday/extra days, UTC+7 clock, assignment-at-day isolation and read-only print/detail.
func TestPriceHistoryCalendar219(t *testing.T) {
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Date(2023, 12, 29, 3, 0, 0, 0, time.UTC))
	prof := port.Profile{Session: testOwnerSession, Name: "kn", Department: "thong_tin_truyen_thong", Roles: map[string]string{"kn_xa": "XA", "kn_tinh": "TINH", "kn_tw": "TW"}, ManageBy: []string{"admin"}}
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clk, []port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	items, tickets := app.NewItemService(svc), app.NewTicketService(svc)
	svc.Points.Submitted = tickets.Submitted
	items.Hooks.HasPrice, items.Hooks.HasSheets = tickets.HasPrice, tickets.Submitted
	tid, xaA, xaB, xaNew := ids.New(), ids.New(), ids.New(), ids.New()
	who := map[string]authz.Principal{
		"admin":       {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh":        {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh"}},
		"xaA":         {ActorRef: xaA, TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"xaB":         {ActorRef: xaB, TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"xaNew":       {ActorRef: xaNew, TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"tw":          {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tw"}},
		"otherTenant": {ActorRef: ids.New(), TenantID: ids.New(), KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), bindTestSession(t, p)))
		}
	})
	g := router.Group("/v1")
	Handler{Svc: svc}.RegisterApp(g)
	PointHandler{Svc: svc}.RegisterApp(g)
	ItemHandler{Svc: items}.RegisterApp(g)
	TicketHandler{Svc: tickets}.RegisterApp(g)
	call := func(as, method, path string, body any, want int) map[string]any {
		t.Helper()
		data := ""
		if body != nil {
			b, e := json.Marshal(body)
			if e != nil {
				t.Fatal(e)
			}
			data = string(b)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(data))
		req.Header.Set("X-Test-As", as)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s %s = %d want %d: %s", as, method, path, w.Code, want, w.Body.String())
		}
		if !strings.Contains(w.Header().Get("Content-Type"), "json") {
			return nil
		}
		return calJSON(t, w.Body.Bytes())
	}
	at := func(day, hour string) {
		t.Helper()
		now, e := time.Parse(time.RFC3339, day+"T"+hour+":00:00Z")
		if e != nil {
			t.Fatal(e)
		}
		clk.Advance(now.Sub(clk.Now()))
	}
	call("admin", "POST", "/v1/price/catalog/defaults", nil, 201)
	seedCommunes(t, tid, "2023-01-01", "", "A1", "A2")
	for i, ref := range []string{xaA.String(), xaB.String()} {
		call("admin", "POST", "/v1/price/points", map[string]any{"admin_unit_code": fmt.Sprintf("A%d", i+1), "region_code": "an_giang", "valid_from": "2023-12-29"}, 201)
		call("admin", "POST", fmt.Sprintf("/v1/price/points/XBG%04d/officers", i+1), map[string]any{"role": "xa", "officer_ref": ref, "valid_from": "2023-12-29"}, 201)
	}
	call("tinh", "POST", "/v1/price/items", map[string]any{"name": "Lúa", "variety": "Loại 1", "group_code": "lua_gao", "unit_code": "kg", "kind": "basic", "valid_from": "2023-12-29"}, 201)
	for _, exc := range []map[string]any{{"day": "2024-01-01", "kind": "holiday", "reason": "Nghỉ chính thức"}, {"day": "2024-01-06", "kind": "extra", "reason": "Ngày báo giá bổ sung"}} {
		call("admin", "POST", "/v1/price/calendar/exceptions", exc, 201)
	}
	save := func(as, day, route string, submit bool) map[string]any {
		t.Helper()
		return call(as, "POST", "/v1/price/tickets/"+route, map[string]any{"point": "XBG0001", "day": day, "version": 0, "submit": submit, "reason": "Mất mạng", "lines": []map[string]any{{"item_code": "MH00001", "min": "9000", "max": "10000"}}}, 201)
	}
	first := save("xaA", "2023-12-29", "sheet", true)["ticket"].(map[string]any)["id"].(string)
	at("2024-01-02", "03")
	save("xaA", "2024-01-02", "sheet", false)
	at("2024-01-03", "06")
	save("tinh", "2024-01-03", "proxy", true)
	at("2024-01-04", "06")
	deleted := save("tinh", "2024-01-04", "proxy", true)["ticket"].(map[string]any)["id"].(string)
	call("tinh", "POST", "/v1/price/tickets/"+deleted+"/delete", map[string]any{"version": 1, "reason": "Phiếu trùng"}, 201)
	at("2024-02-29", "03")
	leap := save("xaA", "2024-02-29", "sheet", true)["ticket"].(map[string]any)["id"].(string)
	at("2024-03-01", "03")
	draft := save("xaA", "2024-03-01", "sheet", false)["ticket"].(map[string]any)["id"].(string)
	history := "/v1/price/tickets/history?point=XBG0001&from=2023-12-29&to=2024-03-10"
	rows := call("xaA", "GET", history, nil, 200)["rows"].([]any)
	seen := map[string][]map[string]any{}
	for _, r := range rows {
		v := r.(map[string]any)
		d := v["day"].(string)
		seen[d] = append(seen[d], v)
		if v["point"].(map[string]any)["code"] != "XBG0001" {
			t.Fatalf("foreign point: %v", v)
		}
		if d > "2024-03-01" && v["view"] == "missed" {
			t.Fatalf("future missed: %v", v)
		}
	}
	start, _ := time.Parse(time.DateOnly, "2023-12-29")
	end, _ := time.Parse(time.DateOnly, "2024-03-10")
	expected := 1 // additional deleted ticket Jan 4
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		day := d.Format(time.DateOnly)
		quote := d.Weekday() != time.Saturday && d.Weekday() != time.Sunday
		if day == "2024-01-01" {
			quote = false
		}
		if day == "2024-01-06" {
			quote = true
		}
		if quote {
			expected++
			if len(seen[day]) == 0 {
				t.Fatalf("missing official reporting day %s", day)
			}
		} else if len(seen[day]) != 0 {
			t.Fatalf("off/holiday treated as missing: %s %v", day, seen[day])
		}
	}
	if len(rows) != expected || len(rows) <= 31 {
		t.Fatalf("truncated range: got %d want %d", len(rows), expected)
	}
	for day, view := range map[string]string{"2023-12-29": "locked", "2024-01-02": "missed", "2024-01-03": "late", "2024-02-29": "locked", "2024-03-01": "draft", "2024-03-04": "not_entered"} {
		if seen[day][0]["view"] != view {
			t.Fatalf("%s view: %v want %s", day, seen[day], view)
		}
	}
	if fmt.Sprint(seen["2024-02-29"][0]["priced"]) != "1" {
		t.Fatalf("real line count: %v", seen["2024-02-29"])
	}
	for _, as := range []string{"admin", "tinh"} {
		if got := call(as, "GET", history, nil, 200)["rows"].([]any); len(got) != expected {
			t.Fatalf("existing reviewer read permission lost: %s", as)
		}
	}
	for _, as := range []string{"xaB", "otherTenant"} {
		if got := call(as, "GET", history, nil, 200)["rows"].([]any); len(got) != 0 {
			t.Fatalf("scope leak to %s: %v", as, got)
		}
	}
	call("tw", "GET", history, nil, 403)
	call("xaB", "GET", "/v1/price/tickets/"+first, nil, 403)
	call("xaA", "GET", "/v1/price/tickets/print?format=xlsx&ticket="+deleted, nil, 409)
	call("xaA", "GET", "/v1/price/tickets/print?format=xlsx&ticket="+draft, nil, 200)
	call("xaA", "GET", "/v1/price/tickets/history?from=2024-03-02&to=2024-03-01", nil, 422)
	call("xaA", "GET", "/v1/price/tickets/history?from=2023-01-01&to=2024-03-01", nil, 422)
	call("xaA", "GET", "/v1/price/tickets/history?from=2024-02-30", nil, 422)
	if got := call("xaA", "GET", "/v1/price/tickets/"+leap, nil, 200)["ticket"].(map[string]any)["version"]; fmt.Sprint(got) != "1" {
		t.Fatalf("history mutated ticket version: %v", got)
	}
	// Assignment changes apply on their official date, even for a hand-edited point URL.
	call("admin", "POST", "/v1/price/points/XBG0001/officers", map[string]any{"role": "xa", "officer_ref": xaNew.String(), "valid_from": "2024-03-04"}, 201)
	for _, as := range []string{"xaA", "xaNew"} {
		for _, r := range call(as, "GET", history, nil, 200)["rows"].([]any) {
			day := r.(map[string]any)["day"].(string)
			if (as == "xaA" && day >= "2024-03-04") || (as == "xaNew" && day < "2024-03-04") {
				t.Fatalf("assignment boundary leaked to %s: %s", as, day)
			}
		}
	}
	call("xaNew", "GET", "/v1/price/tickets/"+leap, nil, 403)
	// At 17:00 UTC the business date has rolled into March 2 (UTC+7).
	at("2024-03-01", "17")
	implicit := call("admin", "GET", "/v1/price/tickets/history?point=XBG0001&from=2024-03-01", nil, 200)["rows"].([]any)
	if len(implicit) != 1 || implicit[0].(map[string]any)["view"] != "missed" {
		t.Fatalf("UTC+7 boundary: %v", implicit)
	}
}
