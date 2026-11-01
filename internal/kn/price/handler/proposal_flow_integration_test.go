package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// BG-ITEM-PROPOSAL reuses W11.73 through real HTTP with the W11.74 calendar,
// including a weekend and explicit holiday/extra quote days. No test-only domain hook.
func TestPriceProposalFlowHTTP(t *testing.T) {
	env := pgtest.Get(t)
	for _, tc := range []struct{ name, exceptionDay, exceptionKind, effective string }{
		{"weekend_and_holiday", "2026-10-19", "holiday", "2026-10-20"},
		{"extra_quote_on_saturday", "2026-10-17", "extra", "2026-10-17"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC) // Friday 10:00 VN.
			prof := port.Profile{Session: testOwnerSession, Name: "kn", Department: "thong_tin_truyen_thong",
				Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA", "kn_tw": "TW"}, ManageBy: []string{"admin"}, StewardDuty: authz.DutyPriceAdmin}
			svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clock.NewFixed(now), []port.Profile{prof})
			if err != nil {
				t.Fatal(err)
			}
			items := app.NewItemService(svc)
			items.Hooks.NextDay = svc.NextQuoteDay // Same public wiring as Module.WireItemCalendar.
			tid, other := ids.New(), ids.New()
			who := map[string]authz.Principal{
				"admin":   {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
				"steward": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Department: "thong_tin_truyen_thong", Duties: []string{authz.DutyPriceAdmin}}},
				"tinh":    {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Department: "thong_tin_truyen_thong"}},
				"xa":      {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
				"xa2":     {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
				"tw":      {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tw"}},
				"other":   {ActorRef: ids.New(), TenantID: other, KN: &authz.KNContext{Role: "kn_tinh", Department: "thong_tin_truyen_thong", Duties: []string{authz.DutyPriceAdmin}}},
			}
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(func(c *gin.Context) {
				if p, ok := who[c.GetHeader("X-Test-As")]; ok {
					c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), bindTestSession(t, p)))
				}
			})
			Handler{Svc: svc}.RegisterApp(r.Group("/v1"))
			ItemHandler{Svc: items}.RegisterApp(r.Group("/v1"))
			PointHandler{Svc: svc}.RegisterApp(r.Group("/v1"))
			call := func(as, method, path string, body any, want int) map[string]any {
				t.Helper()
				var buf []byte
				if body != nil {
					var e error
					buf, e = json.Marshal(body)
					if e != nil {
						t.Fatal(e)
					}
				}
				req := httptest.NewRequest(method, path, bytes.NewReader(buf))
				req.Header.Set("X-Test-As", as)
				req.Header.Set("Content-Type", "application/json")
				out := httptest.NewRecorder()
				r.ServeHTTP(out, req)
				if out.Code != want {
					t.Fatalf("%s %s %s: %d want %d %s", as, method, path, out.Code, want, out.Body.String())
				}
				return calJSON(t, out.Body.Bytes())
			}
			call("admin", http.MethodPost, "/v1/price/catalog/defaults", nil, http.StatusCreated)
			seedCommunes(t, tid, "2026-01-01", "", "P1", "P2")
			for _, unit := range []string{"P1", "P2"} {
				call("admin", http.MethodPost, "/v1/price/points", map[string]any{"admin_unit_code": unit, "region_code": "an_giang", "valid_from": "2026-10-16"}, http.StatusCreated)
			}
			for as, point := range map[string]string{"xa": "XBG0001", "xa2": "XBG0002"} {
				call("admin", http.MethodPost, "/v1/price/points/"+point+"/officers", map[string]any{"role": "xa", "officer_ref": who[as].ActorRef.String(), "valid_from": "2026-10-16"}, http.StatusCreated)
			}
			call("admin", http.MethodPost, "/v1/price/calendar/exceptions", map[string]any{"day": tc.exceptionDay, "kind": tc.exceptionKind, "reason": "Lịch áp dụng kiểm chứng đề xuất"}, http.StatusCreated)
			proposal := func(name, point string) map[string]any {
				return map[string]any{"name": name, "variety": "loại 1", "group_code": "rau_mau", "unit_code": "kg", "reason": "Có bán tại chợ xã", "point_code": point}
			}
			call("tw", http.MethodPost, "/v1/price/proposals", proposal("Mít", "XBG0001"), http.StatusForbidden)
			call("xa2", http.MethodPost, "/v1/price/proposals", proposal("Mít", "XBG0001"), http.StatusForbidden)
			p1 := call("xa", http.MethodPost, "/v1/price/proposals", proposal("Mít", ""), http.StatusCreated)["code"].(string)
			p2 := call("xa", http.MethodPost, "/v1/price/proposals", proposal("Bưởi", ""), http.StatusCreated)["code"].(string)
			p3 := call("xa2", http.MethodPost, "/v1/price/proposals", proposal("Chanh", ""), http.StatusCreated)["code"].(string)
			own := call("xa", http.MethodGet, "/v1/price/proposals", nil, http.StatusOK)["proposals"].([]any)
			if len(own) != 2 {
				t.Fatalf("own history: %v", own)
			}
			for _, p := range own {
				if p.(map[string]any)["id"] == p3 {
					t.Fatal("another officer proposal leaked")
				}
			}
			if all := call("other", http.MethodGet, "/v1/price/proposals", nil, http.StatusOK)["proposals"].([]any); len(all) != 0 {
				t.Fatalf("tenant leak: %v", all)
			}
			approve := map[string]any{"name": "Mít", "variety": "loại 1", "group_code": "rau_mau", "unit_code": "kg"}
			for _, as := range []string{"xa", "tinh", "tw"} {
				call(as, http.MethodPost, "/v1/price/proposals/"+p1+"/approve", approve, http.StatusForbidden)
			}
			call("other", http.MethodPost, "/v1/price/proposals/"+p1+"/approve", approve, http.StatusNotFound)
			saved := call("steward", http.MethodPost, "/v1/price/proposals/"+p1+"/approve", approve, http.StatusCreated)
			item := saved["code"].(string)
			if saved["valid_from"] != tc.effective {
				t.Fatalf("calendar next quote: %v, want %s", saved, tc.effective)
			}
			repeat := call("steward", http.MethodPost, "/v1/price/proposals/"+p1+"/approve", approve, http.StatusConflict)
			if repeat["code"] != "price.proposal_decided" {
				t.Fatalf("repeat decision: %v", repeat)
			}
			call("tinh", http.MethodPost, "/v1/price/proposals/"+p2+"/reject", map[string]any{"note": "Không có quyền"}, http.StatusForbidden)
			call("steward", http.MethodPost, "/v1/price/proposals/"+p2+"/reject", map[string]any{"note": "  "}, http.StatusUnprocessableEntity)
			call("steward", http.MethodPost, "/v1/price/proposals/"+p2+"/reject", map[string]any{"note": "  Đã có giống tương đương  "}, http.StatusCreated)
			rejected := call("xa", http.MethodGet, "/v1/price/proposals?status=rejected", nil, http.StatusOK)["proposals"].([]any)
			if len(rejected) != 1 || rejected[0].(map[string]any)["decision_note"] != "Đã có giống tương đương" {
				t.Fatalf("rejection reason: %v", rejected)
			}
			call("steward", http.MethodPost, "/v1/price/proposals/"+p2+"/reject", map[string]any{"note": "Không sửa quyết định"}, http.StatusConflict)
			for _, check := range []struct {
				point, day string
				present    bool
			}{{"XBG0001", "2026-10-16", false}, {"XBG0001", tc.effective, true}, {"XBG0002", tc.effective, false}} {
				got := call("xa", http.MethodGet, fmt.Sprintf("/v1/price/point-items?point=%s&at=%s", check.point, check.day), nil, http.StatusOK)["items"].([]any)
				count := 0
				for _, row := range got {
					if row.(map[string]any)["code"] == item {
						count++
					}
				}
				if (count == 1) != check.present || count > 1 {
					t.Fatalf("effective/historical point isolation %+v: %v", check, got)
				}
			}
			all := call("steward", http.MethodGet, "/v1/price/items?at="+tc.effective, nil, http.StatusOK)["items"].([]any)
			if len(all) != 1 {
				t.Fatalf("repeat approval/rejected proposal must not add items: %v", all)
			}
		})
	}
}
