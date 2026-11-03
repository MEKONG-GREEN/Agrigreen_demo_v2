package hcplan_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Native HRM/forms + opaque KN owner-session fixture, with real common replay middleware.
// Identity vault and internal unit directory remain fixture adapters, not OIDC/staging evidence.
func TestAdjustmentRoutes203NativeContract(t *testing.T) {
	f, _ := newPlanFixture(t)
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	p := f.call("hcth", http.MethodPost, "/v1/hrm/plans", map[string]any{"year": 2027, "due_on": "2026-11-30", "cutoff_on": "2026-10-12", "units": []string{"trong_trot"}}, http.StatusCreated)
	input := map[string]any{"leaving": 0, "additional": 2, "positions": []map[string]any{{"title": "Kỹ sư mẫu", "count": 2, "when": "Quý I/2027"}}, "note": "Ghi chú giữ nguyên", "base_version": 0}
	f.call("ld_tt", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/submit", input, http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/close", nil, http.StatusOK)
	pid := p["period_ref"].(string)
	frozenPath := "/v1/forms/periods/" + pid + "/snapshots?profile=hrm_plan"
	frozen, _ := json.Marshal(f.call("hcth", http.MethodGet, frozenPath, nil, http.StatusOK))
	unitPath := "/v1/hrm/plans/2027/units/trong_trot"
	before := f.call("hcth", http.MethodGet, unitPath, nil, http.StatusOK)
	oldSubmissions, _ := json.Marshal(before["submissions"])

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if principal, ok := f.who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), principal))
		}
	})
	rawOps := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	// Explain the initial harness 500 without weakening the middleware or its assertions.
	err := tenant.WithTenantTx(context.Background(), rawOps, f.who["hcth"].TenantID, func(tx pgx.Tx) error {
		var n int
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM shared.idempotency_key").Scan(&n)
	})
	if err != nil {
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "42501" {
			t.Fatalf("unwrapped ops diagnostic: %v", err)
		}
		t.Logf("unwrapped app_platform SQLSTATE=%s message=%s; assembly router uses db.ForModule(pool, db.Ops)", pe.Code, pe.Message)
	}
	r.Use(idempotency.Middleware(db.ForModule(rawOps, db.Ops)))
	r.Any("/v1/*path", func(c *gin.Context) { f.r.ServeHTTP(c.Writer, c.Request) })
	request := func(as, key, body string, want int) (map[string]any, *httptest.ResponseRecorder) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, unitPath+"/adjustments", strings.NewReader(body))
		req.Header.Set("X-Test-As", as)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("as=%s status=%d want=%d body=%s", as, w.Code, want, w.Body.String())
		}
		var out map[string]any
		dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
		dec.UseNumber()
		if err := dec.Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out, w
	}
	bodyFor := func(reason string) string {
		b, _ := json.Marshal(map[string]any{"leaving": 1, "additional": 3, "positions": input["positions"], "note": input["note"], "reason": reason})
		return string(b)
	}
	keyNo := 0
	key := func() string { keyNo++; return "adjust203-" + jsonInt(keyNo) }
	for _, field := range []string{"leaving", "additional"} {
		for _, raw := range []string{"1e3", "1e100000", "1e-100000", strings.Repeat("9", 100000)} {
			valid := bodyFor("Điều chỉnh có lý do hợp lệ")
			ordinary := `"` + field + `":1`
			if field == "additional" {
				ordinary = `"additional":3`
			}
			hostile := strings.Replace(valid, ordinary, `"`+strings.ToUpper(field)+`":`+raw+`,"`+field+`":0`, 1)
			bad, _ := request("hcth", key(), hostile, http.StatusUnprocessableEntity)
			if bad["code"] != "validation_failed" {
				t.Fatalf("typed integer guard code: %v", bad["code"])
			}
		}
	}
	for _, reason := range []string{strings.Repeat("😀", 9), strings.Repeat("😀", 501)} {
		request("hcth", key(), bodyFor(reason), http.StatusUnprocessableEntity)
	}
	for _, as := range []string{"ld_tt", "vo_admin", "vo_hcth", "realmonly"} {
		request(as, key(), bodyFor("Điều chỉnh không được phép"), http.StatusForbidden)
	}
	request("other", key(), bodyFor("Điều chỉnh tenant khác"), http.StatusNotFound)
	unchanged := f.call("hcth", http.MethodGet, unitPath, nil, http.StatusOK)
	if len(unchanged["adjustments"].([]any)) != 0 {
		t.Fatal("denied or invalid request appended adjustment")
	}

	reason := "\u0085" + strings.Repeat("😀", 500) + "\u0085"
	k := key()
	first, w1 := request("hcth", k, bodyFor(reason), http.StatusCreated)
	replay, w2 := request("hcth", k, bodyFor(reason), http.StatusCreated)
	if !bytes.Equal(w1.Body.Bytes(), w2.Body.Bytes()) || w2.Header().Get(idempotency.ReplayedHeader) != "true" {
		t.Fatal("retry did not replay original response")
	}
	if first["reason"] != strings.Repeat("😀", 500) || first["actor_ref"] != f.who["hcth"].ActorRef.String() || first["recorded_at"] != "2026-10-12T03:00:00Z" {
		t.Fatalf("real adjustment metadata: %v", first)
	}
	if num(first["seq"]) != num(replay["seq"]) {
		t.Fatal("retry changed seq")
	}
	request("hcth", k, bodyFor("Đổi thân nhưng giữ khóa cũ"), http.StatusUnprocessableEntity)
	request("admin", k, bodyFor(reason), http.StatusUnprocessableEntity)
	// FEFF is not Go White_Space: FEFF + nine emoji is ten valid codepoints.
	second, _ := request("hcth", key(), bodyFor("\ufeff"+strings.Repeat("😀", 9)), http.StatusCreated)
	after := f.call("hcth", http.MethodGet, unitPath, nil, http.StatusOK)
	adjs := after["adjustments"].([]any)
	if len(adjs) != 2 {
		t.Fatalf("want exactly two real adjustments, got%d", len(adjs))
	}
	if num(adjs[0].(map[string]any)["seq"]) != num(first["seq"]) || num(second["seq"]) == num(first["seq"]) {
		t.Fatal("historical seq changed or second adjustment reused seq")
	}
	original, _ := json.Marshal(adjs[0])
	expected, _ := json.Marshal(first)
	if !bytes.Equal(original, expected) {
		t.Fatal("old adjustment snapshot or metadata changed")
	}
	subs, _ := json.Marshal(after["submissions"])
	if !bytes.Equal(subs, oldSubmissions) {
		t.Fatal("closed submission changed")
	}
	finalFrozen, _ := json.Marshal(f.call("hcth", http.MethodGet, frozenPath, nil, http.StatusOK))
	if !bytes.Equal(frozen, finalFrozen) {
		t.Fatal("native cap/forms closed snapshot changed")
	}
	t.Log("native adjustment metadata/Unicode/replay/denial/immutable closed snapshots PASS; fixture owner codec, not OIDC realm")
}
