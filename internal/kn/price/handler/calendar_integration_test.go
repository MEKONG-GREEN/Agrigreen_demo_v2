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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// calJSON giải JSON với số là json.Number (không float — agnofloat cấm float trong toàn module, cả kiểm thử).
func calJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if len(b) == 0 {
		return m
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&m); err != nil {
		t.Fatalf("JSON: %v %s", err, b)
	}
	return m
}

type fixedTargets struct{ refs []uuid.UUID }

func (f fixedTargets) Targets(_ context.Context, _ uuid.UUID, _ string, _ time.Time, round int) ([]port.ReminderTarget, error) {
	if round == 2 {
		return []port.ReminderTarget{{CommuneCode: "00001", CommuneName: "Xã A"}}, nil // mọi xã đã gửi: không ai cần nhắc
	}
	return []port.ReminderTarget{{CommuneCode: "00001", CommuneName: "Xã A", RecipientRefs: f.refs}}, nil
}

// W11.74 qua HTTP + job: lịch mặc định của Profile (T2–T6, 12:00, 13:30), ghi phiên bản lịch có ràng buộc (AC-05, ngày
// áp dụng), ngoại lệ (AC-04), ngưỡng có phiên bản (AC-01, AC-04), job price_calendar khóa đúng giờ ±1 phút, nhắc một lần
// mỗi ngày qua outbox, port.Calendar/Thresholds theo tenant của ctx.
func TestPriceCalendarHTTP_W1174(t *testing.T) {
	env := pgtest.Get(t)
	now := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC) // Thứ Hai 10:00 giờ VN
	prof := port.Profile{Name: "kn", Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA"}, ManageBy: []string{"admin"}}
	if err := app.ValidateProfiles([]port.Profile{prof}); err != nil {
		t.Fatal(err)
	}
	pool := db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule)
	svc, err := app.NewService(pool, clock.NewFixed(now), []port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	tid := ids.New()
	admin := ids.New()
	who := map[string]authz.Principal{
		"admin": {ActorRef: admin, TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh":  {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh"}},
		"xa":    {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
	}
	r.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	})
	Handler{Svc: svc}.RegisterApp(r.Group("/v1"))
	call := func(as, method, path, body string, want int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-Test-As", as)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s %s: %d (muốn %d) %s", as, method, path, w.Code, want, w.Body.String())
		}
		return calJSON(t, w.Body.Bytes())
	}
	code := func(m map[string]any) string { return m["code"].(string) }

	// Lịch mặc định của Profile.
	cal := call("xa", http.MethodGet, "/v1/price/calendar", "", http.StatusOK)
	cur := cal["current"].(map[string]any)
	if cur["lock_at"] != "12:00" || cur["respond_by"] != "13:30" || cur["valid_from"] != nil || cur["version"].(json.Number).String() != "0" ||
		len(cur["weekdays"].([]any)) != 5 || cal["locked"] != false || cal["can_manage"] != false || cal["earliest_valid_from"] != "2026-10-13" {
		t.Fatalf("lịch mặc định: %v", cal)
	}
	days := cal["days"].([]any)
	if len(days) != 30 {
		t.Fatalf("lịch khóa 30 ngày: %d", len(days))
	}
	if d0 := days[0].(map[string]any); d0["day"] != "2026-10-12" || d0["kind"] != "regular" || d0["lock_at"] != "2026-10-12T05:00:00Z" {
		t.Fatalf("hôm nay: %v", d0)
	}
	if d5 := days[5].(map[string]any); d5["day"] != "2026-10-17" || d5["quote"] != false || d5["lock_at"] != nil {
		t.Fatalf("Thứ Bảy: %v", d5)
	}

	// Ghi lịch: chỉ vai quản trị; ràng buộc giờ và ngày áp dụng.
	sched := func(validFrom, lock, respond, remind2 string) string {
		return `{"weekdays":["mon","tue","wed","thu","fri"],"lock_at":"` + lock + `","respond_by":"` + respond + `","remind1_at":"07:30",` +
			`"remind2_at":"` + remind2 + `","channels":["in_app"],"template":"Nhập giá {ngày} trước {giờ khóa}","valid_from":"` + validFrom + `"}`
	}
	call("tinh", http.MethodPost, "/v1/price/calendar/schedule", sched("2026-10-14", "11:00", "13:30", "10:30"), http.StatusForbidden)
	if p := call("admin", http.MethodPost, "/v1/price/calendar/schedule", sched("2026-10-12", "11:00", "13:30", "10:30"), http.StatusUnprocessableEntity); code(p) != "price.valid_from" {
		t.Fatalf("hôm nay là ngày báo giá: %v", p)
	}
	if p := call("admin", http.MethodPost, "/v1/price/calendar/schedule", sched("2026-10-14", "12:00", "11:30", "10:30"), http.StatusUnprocessableEntity); code(p) != "price.respond_before_lock" {
		t.Fatalf("AC-05: %v", p)
	}
	if p := call("admin", http.MethodPost, "/v1/price/calendar/schedule", sched("2026-10-14", "11:00", "13:30", "11:00"), http.StatusUnprocessableEntity); code(p) != "price.remind_order" {
		t.Fatalf("nhắc 2 = giờ khóa: %v", p)
	}
	saved := call("admin", http.MethodPost, "/v1/price/calendar/schedule", sched("2026-10-14", "11:00", "13:30", "10:30"), http.StatusCreated)
	if saved["version"].(json.Number).String() != "1" || saved["valid_from"] != "2026-10-14" {
		t.Fatalf("phiên bản 1: %v", saved)
	}
	cal = call("admin", http.MethodGet, "/v1/price/calendar", "", http.StatusOK)
	days = cal["days"].([]any)
	if days[1].(map[string]any)["lock_at"] != "2026-10-13T05:00:00Z" || days[2].(map[string]any)["lock_at"] != "2026-10-14T04:00:00Z" ||
		len(cal["upcoming"].([]any)) != 1 || len(cal["history"].([]any)) != 1 || cal["current"].(map[string]any)["lock_at"] != "12:00" {
		t.Fatalf("AC-03 đổi giờ khóa từ ngày áp dụng: %v", cal)
	}

	// Ngoại lệ.
	exc := func(d, kind string) string { return `{"day":"` + d + `","kind":"` + kind + `","reason":"Nghỉ lễ"}` }
	call("admin", http.MethodPost, "/v1/price/calendar/exceptions", exc("2026-10-15", "holiday"), http.StatusCreated)
	if p := call("admin", http.MethodPost, "/v1/price/calendar/exceptions", exc("2026-10-15", "holiday"), http.StatusConflict); code(p) != "price.exception_exists" {
		t.Fatalf("trùng ngày: %v", p)
	}
	if p := call("admin", http.MethodPost, "/v1/price/calendar/exceptions", exc("2026-10-17", "holiday"), http.StatusUnprocessableEntity); code(p) != "price.exception_mismatch" {
		t.Fatalf("nghỉ Thứ Bảy: %v", p)
	}
	call("admin", http.MethodPost, "/v1/price/calendar/exceptions", exc("2026-10-12", "holiday"), http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/price/calendar/exceptions", exc("2026-10-18", "extra"), http.StatusCreated)
	cal = call("xa", http.MethodGet, "/v1/price/calendar", "", http.StatusOK)
	days = cal["days"].([]any)
	if days[3].(map[string]any)["kind"] != "holiday" || days[3].(map[string]any)["quote"] != false || days[6].(map[string]any)["kind"] != "extra" ||
		len(cal["exceptions"].([]any)) != 2 {
		t.Fatalf("AC-04 ngày nghỉ, ngày bổ sung: %v %v", days[3], days[6])
	}
	call("admin", http.MethodPost, "/v1/price/calendar/exceptions/2026-10-18/retire", "", http.StatusNoContent)
	call("admin", http.MethodPost, "/v1/price/calendar/exceptions/2026-10-18/retire", "", http.StatusNotFound)

	// Port Calendar theo tenant của ctx.
	ctx := tenant.WithID(context.Background(), tid)
	if p, err := svc.Day(ctx, "", time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)); err != nil || p.Quote || p.Kind != "holiday" {
		t.Fatalf("Day ngày nghỉ: %+v %v", p, err)
	}
	if p, err := svc.Day(ctx, "kn", now); err != nil || !p.Quote || p.Locked(now) || !p.Locked(now.Add(2*time.Hour)) {
		t.Fatalf("Day hôm nay: %+v %v", p, err)
	}

	// Ngưỡng.
	th := call("xa", http.MethodGet, "/v1/price/thresholds", "", http.StatusOK)
	def := th["default"].(map[string]any)
	if def["pct"] != "30.00" || def["flat_pct"] != "1.00" || def["survey_pct"] != "40.00" || def["lookback_days"].(json.Number).String() != "7" ||
		th["default_stored"] != false {
		t.Fatalf("ngưỡng mặc định: %v", th)
	}
	call("admin", http.MethodPost, "/v1/price/thresholds", `{"scope":"group","code":"rau_mau","pct":"40"}`, http.StatusUnprocessableEntity) // chưa có nhóm
	call("admin", http.MethodPost, "/v1/price/catalog/defaults", "", http.StatusCreated)
	// W11.73: ngưng theo mặt hàng tham chiếu danh mục mặt hàng (mã MH#####, phải có trong sổ pf_price.item).
	if _, err := app.NewItemService(svc).CreateItem(authz.WithPrincipal(context.Background(), who["admin"]), "", app.ItemInput{Name: "Heo hơi",
		Variety: "loại 1", GroupCode: "chan_nuoi", UnitCode: "kg", Kind: "basic"}, ""); err != nil {
		t.Fatal(err)
	}
	call("admin", http.MethodPost, "/v1/price/thresholds", `{"scope":"item","code":"MH00009","pct":"25.5"}`, http.StatusNotFound)
	// W11.73: "ngày báo giá kế tiếp" của danh mục mặt hàng theo lịch — 14/10 (T4) → bỏ ngày nghỉ 15/10 → 16/10 (T6).
	if err := db.WithModuleTenantTx(context.Background(), pool, app.DBModule, tid, func(tx pgx.Tx) error {
		d, err := svc.NextQuoteDay(context.Background(), tx, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
		if err == nil && d.Format(time.DateOnly) != "2026-10-16" {
			err = fmt.Errorf("NextQuoteDay = %s", d.Format(time.DateOnly))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	call("admin", http.MethodPost, "/v1/price/thresholds", `{"scope":"item","code":"heo_hoi","pct":"25.5"}`, http.StatusUnprocessableEntity)
	call("tinh", http.MethodPost, "/v1/price/thresholds", `{"scope":"group","code":"rau_mau","pct":"40"}`, http.StatusForbidden)
	call("admin", http.MethodPost, "/v1/price/thresholds", `{"scope":"group","code":"rau_mau","pct":40}`, http.StatusUnprocessableEntity) // số phải là chuỗi
	call("admin", http.MethodPost, "/v1/price/thresholds", `{"scope":"group","code":"rau_mau","pct":"0"}`, http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/price/thresholds", `{"scope":"group","code":"rau_mau","pct":"100.5"}`, http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/price/thresholds", `{"scope":"group","code":"rau_mau","pct":"40"}`, http.StatusCreated)
	call("admin", http.MethodPost, "/v1/price/thresholds", `{"scope":"item","code":"MH00001","pct":"25.5"}`, http.StatusCreated)
	call("admin", http.MethodPost, "/v1/price/thresholds",
		`{"scope":"default","code":"","pct":"35","lookback_days":10,"flat_pct":"0.5","survey_pct":"40"}`, http.StatusCreated)
	if p := call("admin", http.MethodPost, "/v1/price/thresholds/default/x/retire", "", http.StatusUnprocessableEntity); code(p) != "price.default_threshold" {
		t.Fatalf("AC-04 xóa mặc định: %v", p)
	}
	call("admin", http.MethodPost, "/v1/price/thresholds/item/MH00001/retire", "", http.StatusNoContent)
	th = call("tinh", http.MethodGet, "/v1/price/thresholds", "", http.StatusOK)
	if th["default"].(map[string]any)["pct"] != "35.00" || th["default_stored"] != true || len(th["items"].([]any)) != 1 || len(th["history"].([]any)) != 4 {
		t.Fatalf("ngưỡng sau ghi: %v", th)
	}
	if pct, scope, err := svc.Threshold(ctx, "", "rau_mau", "cai_xanh"); err != nil || pct.String() != "40" || scope != "group" {
		t.Fatalf("Threshold nhóm: %s %s %v", pct, scope, err)
	}
	if pct, scope, err := svc.Threshold(ctx, "", "chan_nuoi", "MH00001"); err != nil || pct.String() != "35" || scope != "default" {
		t.Fatalf("Threshold mặc định (mặt hàng đã xóa): %s %s %v", pct, scope, err)
	}
	if n, flat, survey, err := svc.AlertParams(ctx, ""); err != nil || n != 10 || flat.String() != "0.5" || survey.String() != "40" {
		t.Fatalf("AlertParams: %d %s %s %v", n, flat, survey, err)
	}

	// Job price_calendar: hôm nay (Thứ Hai) lịch mặc định — nhắc 1 lúc 07:30, nhắc 2 lúc 11:00, khóa 12:00 (giờ VN).
	xaRef := ids.New()
	jobAt := func(ts string) app.CalendarJobResult {
		t.Helper()
		at, _ := time.Parse(time.RFC3339, ts)
		js, err := app.NewService(pool, clock.NewFixed(at), []port.Profile{prof})
		if err != nil {
			t.Fatal(err)
		}
		js.Reminders = fixedTargets{refs: []uuid.UUID{xaRef}}
		res, err := js.CalendarJobTenant(context.Background(), tid, at)
		if err != nil {
			t.Fatalf("job %s: %v", ts, err)
		}
		return res
	}
	if r := jobAt("2026-10-12T00:29:00Z"); r != (app.CalendarJobResult{}) {
		t.Fatalf("trước giờ nhắc: %+v", r)
	}
	if r := jobAt("2026-10-12T00:30:20Z"); r.Reminders != 1 || r.Events != 1 || r.Locked != 0 {
		t.Fatalf("nhắc 1: %+v", r)
	}
	if r := jobAt("2026-10-12T00:31:00Z"); r != (app.CalendarJobResult{}) {
		t.Fatalf("nhắc 1 lặp: %+v", r)
	}
	if r := jobAt("2026-10-12T04:00:05Z"); r.Reminders != 1 || r.Events != 0 {
		t.Fatalf("nhắc 2 (mọi xã đã gửi): %+v", r)
	}
	if r := jobAt("2026-10-12T05:00:40Z"); r.Locked != 1 {
		t.Fatalf("khóa: %+v", r)
	}
	if r := jobAt("2026-10-12T05:01:40Z"); r != (app.CalendarJobResult{}) {
		t.Fatalf("khóa lặp: %+v", r)
	}
	if r := jobAt("2026-10-15T05:00:10Z"); r != (app.CalendarJobResult{}) {
		t.Fatalf("ngày nghỉ không khóa, không nhắc: %+v", r)
	}
	runs := call("tinh", http.MethodGet, "/v1/price/calendar/runs?from=2026-10-12&to=2026-10-15", "", http.StatusOK)["items"].([]any)
	if len(runs) != 3 {
		t.Fatalf("nhật ký: %v", runs)
	}
	for _, x := range runs {
		m := x.(map[string]any)
		lag, _ := m["lag_seconds"].(json.Number).Int64()
		if lag < 0 || lag > 60 {
			t.Fatalf("lệch quá 1 phút: %v", m)
		}
		if m["kind"] == "lock" && (m["scheduled_at"] != "2026-10-12T05:00:00Z" || m["ran_at"] != "2026-10-12T05:00:40Z") {
			t.Fatalf("mốc khóa: %v", m)
		}
		if m["kind"] == "remind1" && m["recipients"].(json.Number).String() != "1" {
			t.Fatalf("nhắc 1: %v", m)
		}
	}
	call("xa", http.MethodPost, "/v1/price/calendar/reminder-test", "", http.StatusForbidden)
	if b := call("admin", http.MethodPost, "/v1/price/calendar/reminder-test", "", http.StatusAccepted)["body"]; b != "Nhắc nhập giá ngày 12/10/2026 cho xã: vui lòng gửi báo giá trước 12:00." {
		t.Fatalf("gửi thử: %v", b)
	}
	var payloads []string
	err = tenant.WithTenantTx(context.Background(), env.AdminPool(t, migrations.PF), tid, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT payload::text FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2 ORDER BY occurred_at`,
			tid, port.EventReminderIssued)
		if err != nil {
			return err
		}
		payloads, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil || len(payloads) != 2 {
		t.Fatalf("outbox: %v %v", payloads, err)
	}
	var p port.ReminderIssuedV1
	if err := json.Unmarshal([]byte(payloads[0]), &p); err != nil || p.Round != 1 || p.Day != "2026-10-12" || len(p.RecipientRefs) != 1 ||
		p.RecipientRefs[0] != xaRef || !strings.Contains(p.Body, "Xã A") || p.Test {
		t.Fatalf("payload nhắc 1: %+v %v", p, err)
	}
	if err := json.Unmarshal([]byte(payloads[1]), &p); err != nil || !p.Test || p.RecipientRefs[0] != admin {
		t.Fatalf("payload gửi thử: %+v %v", p, err)
	}
}
