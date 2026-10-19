package handler

import (
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

// Phiếu báo giá + xác minh W11.75 (pf 000148, BRD Báo giá v1.3 FN-XA-03.01…03, 03.05, FN-TI-02.02…04, 02.09) qua HTTP:
// Xã lưu nháp / gửi (đủ mặt hàng, giá hợp lệ, khóa lạc quan), khóa nhập đúng giờ khóa (DayPlan.Locked), giá tham chiếu
// (CT-02) + cảnh báo tạm tính (CT-03, AC-03 +55,56 %) + tự hủy, xác minh hai chiều (yêu cầu, phản hồi, quá hạn —
// AC-01/02 FN-XA-03.05), nhập thay (Nộp muộn), hiệu chỉnh (giá hiệu chỉnh làm giá tham chiếu — AC-05), loại khỏi bảng
// giá, sửa ngày (AC-04), xóa mềm, in / xuất, nhật ký, lịch sử, port.Tickets, hook W11.72/W11.73. Số so sánh dạng chuỗi.
func TestPriceTicketsHTTP_W1175(t *testing.T) {
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC)) // Thứ Năm 08/10 10:00 giờ VN
	prof := port.Profile{Session: testOwnerSession, Name: "kn", Department: "thong_tin_truyen_thong", Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA",
		"kn_tw": "TW"}, ManageBy: []string{"admin"}, StewardDuty: authz.DutyPriceAdmin}
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clk, []port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	items, tickets := app.NewItemService(svc), app.NewTicketService(svc)
	svc.Points.Submitted = tickets.Submitted // như price.New
	items.Hooks.HasPrice, items.Hooks.HasSheets = tickets.HasPrice, tickets.Submitted
	tid := ids.New()
	ctvA, ctvB, tinhRef, tinh2 := ids.New(), ids.New(), ids.New(), ids.New()
	who := map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh":  {ActorRef: tinhRef, TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Duties: []string{authz.DutyPriceAdmin}}},
		"tinh2": {ActorRef: tinh2, TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh"}},
		"xaA":   {ActorRef: ctvA, TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"xaB":   {ActorRef: ctvB, TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"tw":    {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tw"}},
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), bindTestSession(t, p)))
		}
	})
	g := r.Group("/v1")
	Handler{Svc: svc}.RegisterApp(g)
	PointHandler{Svc: svc}.RegisterApp(g)
	ItemHandler{Svc: items}.RegisterApp(g)
	TicketHandler{Svc: tickets}.RegisterApp(g)
	raw := func(as, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var buf string
		if body != nil {
			b, _ := json.Marshal(body)
			buf = string(b)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(buf))
		req.Header.Set("X-Test-As", as)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	call := func(as, method, path string, body any, want int) map[string]any {
		t.Helper()
		w := raw(as, method, path, body)
		if w.Code != want {
			t.Fatalf("%s %s %s: %d (muốn %d) %s", as, method, path, w.Code, want, w.Body.String())
		}
		return calJSON(t, w.Body.Bytes())
	}
	code := func(as, method, path string, body any, want int, problemCode string) {
		t.Helper()
		if p := call(as, method, path, body, want); p["code"] != problemCode {
			t.Fatalf("%s %s: mã lỗi %v (muốn %s)", method, path, p["code"], problemCode)
		}
	}
	str := func(v any) string { return fmt.Sprint(v) }
	line := func(sh map[string]any, item string) map[string]any {
		t.Helper()
		for _, l := range sh["lines"].([]any) {
			if m := l.(map[string]any); m["item_code"] == item {
				return m
			}
		}
		t.Fatalf("không có dòng %s: %v", item, sh["lines"])
		return nil
	}
	ln := func(item, lo, hi string, note ...string) map[string]any {
		m := map[string]any{"item_code": item, "min": lo, "max": hi}
		if len(note) > 0 {
			m["note"] = note[0]
		}
		return m
	}
	entry := func(point, day string, version any, submit bool, lines ...map[string]any) map[string]any {
		return map[string]any{"point": point, "day": day, "version": version, "submit": submit, "lines": lines}
	}

	// ---- Dữ liệu nền: 2 xã báo giá, phân công, 2 mặt hàng Cơ bản (hiệu lực từ 08/10) ----
	call("admin", http.MethodPost, "/v1/price/catalog/defaults", nil, http.StatusCreated)
	seedCommunes(t, tid, "2025-07-01", "", "A1", "A2")
	for _, p := range []map[string]any{{"admin_unit_code": "A1", "region_code": "an_giang", "valid_from": "2026-10-08"},
		{"admin_unit_code": "A2", "region_code": "kien_giang", "valid_from": "2026-10-08"}} {
		call("admin", http.MethodPost, "/v1/price/points", p, http.StatusCreated)
	}
	off := func(point, role string, ref uuid.UUID) {
		call("admin", http.MethodPost, "/v1/price/points/"+point+"/officers", map[string]any{"role": role, "officer_ref": ref.String(),
			"valid_from": "2026-10-08"}, http.StatusCreated)
	}
	off("XBG0001", "xa", ctvA)
	off("XBG0002", "xa", ctvB)
	off("XBG0001", "tinh", tinhRef) // XBG0002 chưa phân công Cấp Tỉnh
	for _, it := range []map[string]any{
		{"name": "Dưa leo", "variety": "Loại 1", "group_code": "rau_mau", "unit_code": "kg", "kind": "basic", "valid_from": "2026-10-08"},
		{"name": "Heo hơi", "variety": "Thịt", "group_code": "chan_nuoi", "unit_code": "kg", "kind": "basic", "valid_from": "2026-10-08"},
	} {
		call("tinh", http.MethodPost, "/v1/price/items", it, http.StatusCreated)
	}

	// ---- Ngày 1 (Thứ Năm 08/10): Xã nhập, lưu nháp, gửi ----
	sh := call("xaA", http.MethodGet, "/v1/price/tickets/sheet", nil, http.StatusOK) // vắng point: CTV phụ trách đúng 1 xã
	if sh["view"] != "not_entered" || sh["ticket"] != nil || len(sh["lines"].([]any)) != 2 || sh["can"].(map[string]any)["enter"] != true ||
		sh["point"].(map[string]any)["code"] != "XBG0001" || line(sh, "MH00001")["ref"] != nil || sh["lock_at"] != "2026-10-08T05:00:00Z" {
		t.Fatalf("phiếu mới: %v", sh)
	}
	code("tw", http.MethodGet, "/v1/price/tickets/sheet?point=XBG0001", nil, http.StatusForbidden, "forbidden")
	code("xaB", http.MethodGet, "/v1/price/tickets/sheet?point=XBG0001", nil, http.StatusForbidden, "price.point_not_assigned")
	code("tinh", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", 0, false), http.StatusForbidden, "forbidden")
	code("xaB", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", 0, false, ln("MH00001", "9000", "9000")),
		http.StatusForbidden, "price.point_not_assigned")
	code("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", 0, false, ln("MH00001", "9000", "8000")),
		http.StatusUnprocessableEntity, "price.price_invalid") // thấp nhất > cao nhất
	code("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", 0, false, ln("MH00001", "-1", "9000")),
		http.StatusUnprocessableEntity, "price.price_invalid")
	code("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "2026-10-09", 0, false, ln("MH00001", "9000", "9000")),
		http.StatusUnprocessableEntity, "price.not_quote_day") // chỉ nhập ngày hôm nay
	sh = call("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", 0, false, ln("MH00001", "9000", "9000")), http.StatusCreated)
	tk := sh["ticket"].(map[string]any)
	if sh["view"] != "draft" || str(tk["version"]) != "1" || line(sh, "MH00001")["min"] != "9000" || line(sh, "MH00002")["entered"] != false {
		t.Fatalf("lưu nháp: %v", sh)
	}
	// AC-01: thiếu 1 mặt hàng → không gửi được; phiên bản cũ → 409.
	code("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", 1, true), http.StatusUnprocessableEntity, "price.ticket_incomplete")
	code("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", 0, true), http.StatusConflict, "price.ticket_stale")
	sh = call("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", 1, true, ln("MH00002", "38000", "38000")), http.StatusCreated)
	if sh["view"] != "submitted" || str(sh["ticket"].(map[string]any)["version"]) != "2" || line(sh, "MH00001")["alert"] != nil {
		t.Fatalf("gửi (Giá mới — không cảnh báo): %v", sh)
	}
	code("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", 2, false), http.StatusConflict, "conflict") // không lưu nháp đè phiếu đã gửi
	ctx := tenant.WithID(context.Background(), tid)

	// Nhập thay trước giờ khóa → 409; Xã sau giờ khóa (12:00 đúng) → 409 price.ticket_locked.
	code("tinh", http.MethodPost, "/v1/price/tickets/proxy", map[string]any{"point": "XBG0002", "day": "2026-10-08", "reason": "Mất mạng",
		"submit": true}, http.StatusConflict, "price.ticket_not_locked")
	clk.Advance(2 * time.Hour) // 12:00 giờ VN
	code("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", 2, true, ln("MH00001", "9100", "9100")),
		http.StatusConflict, "price.ticket_locked")
	day1 := call("tinh", http.MethodGet, "/v1/price/tickets?day=2026-10-08", nil, http.StatusOK)
	rows := day1["rows"].([]any)
	if day1["locked"] != true || len(rows) != 2 || rows[0].(map[string]any)["view"] != "locked" || rows[1].(map[string]any)["view"] != "missed" ||
		str(rows[0].(map[string]any)["priced"]) != "2" || rows[0].(map[string]any)["mine"] != true {
		t.Fatalf("phiếu trong ngày: %v", day1)
	}
	if mine := call("tinh", http.MethodGet, "/v1/price/tickets?day=2026-10-08&mine=true", nil, http.StatusOK)["rows"].([]any); len(mine) != 1 {
		t.Fatalf("lọc xã được phân công: %v", mine)
	}
	if xa := call("xaB", http.MethodGet, "/v1/price/tickets?day=2026-10-08", nil, http.StatusOK)["rows"].([]any); len(xa) != 1 ||
		xa[0].(map[string]any)["point"].(map[string]any)["code"] != "XBG0002" {
		t.Fatalf("Cấp Xã chỉ thấy xã mình: %v", xa)
	}

	// Nhập thay (FN-TI-02.09): lý do bắt buộc; Cấp Tỉnh khác chỉ xã phân công / chưa phân công; đủ mặt hàng → Nộp muộn.
	proxy := func(as string, version any, submit bool, reason string, lines ...map[string]any) map[string]any {
		return map[string]any{"point": "XBG0002", "day": "2026-10-08", "version": version, "submit": submit, "reason": reason, "lines": lines}
	}
	code("tinh", http.MethodPost, "/v1/price/tickets/proxy", proxy("tinh", 0, true, " "), http.StatusUnprocessableEntity, "price.reason_required")
	code("tinh2", http.MethodPost, "/v1/price/tickets/proxy", map[string]any{"point": "XBG0001", "day": "2026-10-08", "reason": "Gửi qua Zalo",
		"submit": true}, http.StatusForbidden, "price.point_not_handled")
	code("tinh", http.MethodPost, "/v1/price/tickets/proxy", proxy("tinh", 0, true, "Xã gửi qua Zalo", ln("MH00001", "8000", "9000")),
		http.StatusUnprocessableEntity, "price.ticket_incomplete")
	sh = call("tinh2", http.MethodPost, "/v1/price/tickets/proxy", proxy("tinh2", 0, true, "Xã gửi qua Zalo", ln("MH00001", "8000", "9000"),
		map[string]any{"item_code": "MH00002", "no_goods": true}), http.StatusCreated)
	lateID := sh["ticket"].(map[string]any)["id"].(string)
	if sh["view"] != "late" || sh["ticket"].(map[string]any)["source"] != "proxy" || sh["ticket"].(map[string]any)["reason"] != "Xã gửi qua Zalo" {
		t.Fatalf("nhập thay: %v", sh)
	}
	code("tinh", http.MethodPost, "/v1/price/tickets/proxy", proxy("tinh", 1, true, "Lần hai", ln("MH00001", "8000", "9000")),
		http.StatusConflict, "price.ticket_exists") // BR-01: không nhập thay đè phiếu đã gửi

	// Hook W11.72/W11.73: đã gửi phiếu, đã có giá.
	if err := db.WithModuleTenantTx(ctx, svc.DB, app.DBModule, tid, func(tx pgx.Tx) error {
		sub, err := tickets.Submitted(ctx, tx, time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), "XBG0001")
		if err != nil || !sub {
			return fmt.Errorf("submitted XBG0001 = %v: %w", sub, err)
		}
		has, err := tickets.HasPrice(ctx, tx, "MH00002")
		if err != nil || !has {
			return fmt.Errorf("HasPrice MH00002 = %v: %w", has, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// W11.73: mặt hàng đã có giá → khóa ĐVT.
	code("tinh", http.MethodPost, "/v1/price/items/MH00001/revise", map[string]any{"name": "Dưa leo", "variety": "Loại 1", "group_code": "rau_mau",
		"unit_code": "ta", "kind": "basic"}, http.StatusConflict, "price.item_locked")

	// ---- Ngày 2 (Thứ Sáu 09/10) 10:00: giá tham chiếu, cảnh báo tạm tính (AC-03), tự hủy, xác minh ----
	clk.Advance(22 * time.Hour)
	sh = call("xaA", http.MethodGet, "/v1/price/tickets/sheet", nil, http.StatusOK)
	if ref := line(sh, "MH00001")["ref"].(map[string]any); ref["mid"] != "9000" || ref["day"] != "2026-10-08" || line(sh, "MH00001")["threshold_pct"] != "30.00" {
		t.Fatalf("giá tham chiếu: %v", line(sh, "MH00001"))
	}
	code("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", 0, true, ln("MH00001", "13000", "15000"), ln("MH00002", "38000", "38000")),
		http.StatusUnprocessableEntity, "price.ticket_incomplete") // bất thường chưa ghi chú
	sh = call("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", 0, true, ln("MH00001", "13000", "15000", "Mưa, hàng ít"),
		ln("MH00002", "56000", "56000", "Dịch, hàng khan")), http.StatusCreated)
	a1 := line(sh, "MH00001")["alert"].(map[string]any)
	if a1["status"] != "provisional" || a1["direction"] != "high" || a1["change_pct"] != "55.56" || a1["price"] != "14000" || a1["ref_price"] != "9000" {
		t.Fatalf("AC-03 cảnh báo tạm tính: %v", a1)
	}
	if e := line(sh, "MH00001")["eval"].(map[string]any); e["direction"] != "high" || e["change_pct"] != "55.56" {
		t.Fatalf("eval: %v", e)
	}
	t2 := sh["ticket"].(map[string]any)["id"].(string)
	// Xã sửa về dưới ngưỡng, gửi lại → tự hủy; gửi lại vượt ngưỡng → Tạm tính lại.
	v := sh["ticket"].(map[string]any)["version"]
	sh = call("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", v, true, ln("MH00001", "9000", "10000")), http.StatusCreated)
	if a := line(sh, "MH00001")["alert"].(map[string]any); a["status"] != "cancelled" {
		t.Fatalf("tự hủy: %v", a)
	}
	v = sh["ticket"].(map[string]any)["version"]
	sh = call("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", v, true, ln("MH00001", "13000", "15000", "Mưa, hàng ít")),
		http.StatusCreated)
	if a := line(sh, "MH00001")["alert"].(map[string]any); a["status"] != "provisional" {
		t.Fatalf("tạm tính lại: %v", a)
	}
	// Cấp Tỉnh yêu cầu xác minh trước giờ khóa (nội dung bắt buộc; tinh2 không xử lý XBG0001).
	reqBody := func(items ...string) map[string]any {
		var as []map[string]any
		for _, it := range items {
			as = append(as, map[string]any{"ticket_id": t2, "item_code": it})
		}
		return map[string]any{"alerts": as, "message": "Giá chênh so với giá tham chiếu, đề nghị xã kiểm tra lại"}
	}
	code("tinh", http.MethodPost, "/v1/price/alerts/request", map[string]any{"alerts": reqBody("MH00001")["alerts"], "message": ""},
		http.StatusUnprocessableEntity, "price.reason_required")
	code("tinh2", http.MethodPost, "/v1/price/alerts/request", reqBody("MH00001"), http.StatusForbidden, "price.point_not_handled")
	code("xaA", http.MethodPost, "/v1/price/alerts/request", reqBody("MH00001"), http.StatusForbidden, "forbidden")
	if w := call("tinh", http.MethodPost, "/v1/price/alerts/request", reqBody("MH00001", "MH00002"), http.StatusCreated); str(w["written"]) != "2" {
		t.Fatalf("yêu cầu xác minh: %v", w)
	}
	xaAlerts := call("xaA", http.MethodGet, "/v1/price/alerts?view=requested", nil, http.StatusOK)["alerts"].([]any)
	if len(xaAlerts) != 2 || xaAlerts[0].(map[string]any)["respond_by"] != "2026-10-09T06:30:00Z" || xaAlerts[0].(map[string]any)["can_act"] != true {
		t.Fatalf("Xã thấy yêu cầu (hạn 13:30): %v", xaAlerts)
	}
	if b := call("xaB", http.MethodGet, "/v1/price/alerts", nil, http.StatusOK)["alerts"].([]any); len(b) != 0 {
		t.Fatalf("Xã khác không thấy: %v", b)
	}

	// 12:15: khóa; Xã phản hồi đề xuất giá đúng (AC-01) — giá trên phiếu không đổi.
	clk.Advance(2*time.Hour + 15*time.Minute)
	resp := func(item, choice, expl string, lo, hi any) map[string]any {
		return map[string]any{"ticket_id": t2, "item_code": item, "choice": choice, "explanation": expl, "min": lo, "max": hi}
	}
	code("xaA", http.MethodPost, "/v1/price/alerts/respond", resp("MH00001", "propose", "", "9000", "10000"), http.StatusUnprocessableEntity,
		"price.reason_required") // AC-03
	code("xaA", http.MethodPost, "/v1/price/alerts/respond", resp("MH00001", "propose", "Kiểm lại thương lái", "10000", "9000"),
		http.StatusUnprocessableEntity, "price.price_invalid")
	code("xaB", http.MethodPost, "/v1/price/alerts/respond", resp("MH00001", "keep", "Giữ", nil, nil), http.StatusForbidden, "price.point_not_assigned")
	call("xaA", http.MethodPost, "/v1/price/alerts/respond", resp("MH00001", "propose", "Kiểm lại thương lái", "9000", "10000"), http.StatusCreated)
	code("xaA", http.MethodPost, "/v1/price/alerts/respond", resp("MH00001", "keep", "Lần hai", nil, nil), http.StatusConflict, "price.alert_state")
	sh = call("tinh", http.MethodGet, "/v1/price/tickets/"+t2, nil, http.StatusOK)
	a1 = line(sh, "MH00001")["alert"].(map[string]any)
	if sh["view"] != "locked" || a1["status"] != "responded" || a1["reply"].(map[string]any)["proposed_min"] != "9000" ||
		line(sh, "MH00001")["min"] != "13000" {
		t.Fatalf("Xã đã phản hồi: %v", line(sh, "MH00001"))
	}
	// 13:31: quá hạn phản hồi (AC-02).
	clk.Advance(76 * time.Minute)
	code("xaA", http.MethodPost, "/v1/price/alerts/respond", resp("MH00002", "keep", "Đúng giá", nil, nil), http.StatusConflict, "price.respond_overdue")
	if a := line(call("tinh", http.MethodGet, "/v1/price/tickets/"+t2, nil, http.StatusOK), "MH00002")["alert"].(map[string]any); a["view"] != "overdue" {
		t.Fatalf("quá hạn phản hồi: %v", a)
	}

	// Cấp Tỉnh chốt: hiệu chỉnh MH00001 bằng giá Xã đề xuất (lý do bắt buộc), loại MH00002 khỏi bảng giá.
	version := func(id string) any {
		return call("tinh", http.MethodGet, "/v1/price/tickets/"+id, nil, http.StatusOK)["ticket"].(map[string]any)["version"]
	}
	dec := func(item, d, note string) map[string]any {
		return map[string]any{"ticket_id": t2, "item_code": item, "decision": d, "note": note}
	}
	code("tinh", http.MethodPost, "/v1/price/tickets/"+t2+"/correct", map[string]any{"version": version(t2), "reason": "",
		"lines": []any{map[string]any{"item_code": "MH00001", "min": "9000", "max": "10000"}}}, http.StatusUnprocessableEntity, "price.reason_required")
	code("tinh", http.MethodPost, "/v1/price/tickets/"+t2+"/correct", map[string]any{"version": 1, "reason": "Dùng giá Xã đề xuất",
		"lines": []any{map[string]any{"item_code": "MH00001", "min": "9000", "max": "10000"}}}, http.StatusConflict, "price.ticket_stale")
	sh = call("tinh", http.MethodPost, "/v1/price/tickets/"+t2+"/correct", map[string]any{"version": version(t2), "reason": "Dùng giá Xã đề xuất",
		"lines": []any{map[string]any{"item_code": "MH00001", "min": "9000", "max": "10000"}}}, http.StatusCreated)
	if l := line(sh, "MH00001"); l["adj_min"] != "9000" || l["min"] != "13000" || l["alert"].(map[string]any)["status"] != "adjusted" ||
		sh["ticket"].(map[string]any)["op"] != "correct" {
		t.Fatalf("hiệu chỉnh (BR-05 giữ giá Xã gửi): %v", sh)
	}
	code("tinh", http.MethodPost, "/v1/price/alerts/decide", dec("MH00002", "exclude", ""), http.StatusUnprocessableEntity, "price.reason_required")
	code("tinh", http.MethodPost, "/v1/price/alerts/decide", dec("MH00001", "keep", "Giữ"), http.StatusConflict, "price.alert_state")
	call("tinh", http.MethodPost, "/v1/price/alerts/decide", dec("MH00002", "exclude", "Xã không phản hồi, giá bất thường"), http.StatusCreated)
	code("tinh", http.MethodPost, "/v1/price/alerts/decide", dec("MH00002", "reopen", "Mở lại"), http.StatusConflict, "price.alert_state")

	// port.Tickets (W11.76): dòng giá tính vào bảng giá = giá hiệu chỉnh, bỏ dòng đã loại; không còn cảnh báo mở.
	sum, err := tickets.Day(ctx, "kn", time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	if err != nil || sum.Points != 2 || sum.Submitted != 1 || sum.Missed != 1 || len(sum.Lines) != 1 || sum.Lines[0].Mid.String() != "9500" ||
		!sum.Lines[0].Adjusted || sum.Excluded != 1 || sum.OpenAlerts != 0 {
		t.Fatalf("port.Tickets.Day: %+v %v", sum, err)
	}

	// In / xuất: PDF chưa nối → 503; Excel được; phiếu Không nộp không in.
	code("xaA", http.MethodGet, "/v1/price/tickets/print?ticket="+t2, nil, http.StatusServiceUnavailable, "price.pdf_unavailable")
	if w := raw("xaA", http.MethodGet, "/v1/price/tickets/print?format=xlsx&ticket="+t2+"&ticket="+tk["id"].(string), nil); w.Code != http.StatusOK ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "phieu-bao-gia-XBG0001") {
		t.Fatalf("in hàng loạt Excel: %d %s", w.Code, w.Body.String())
	}
	if w := raw("tinh", http.MethodGet, "/v1/price/tickets/export?day=2026-10-09", nil); w.Code != http.StatusOK || w.Body.Len() < 100 {
		t.Fatalf("xuất Excel: %d", w.Code)
	}
	code("xaA", http.MethodGet, "/v1/price/tickets/export?day=2026-10-09", nil, http.StatusForbidden, "forbidden")

	// Sửa ngày phiếu (AC-04): sang ngày xã đã có phiếu → chặn; ngày tương lai → chặn; ngày trống → được.
	code("tinh", http.MethodPost, "/v1/price/tickets/"+tk["id"].(string)+"/redate", map[string]any{"version": version(tk["id"].(string)),
		"day": "2026-10-09", "reason": "Nhập nhầm ngày"}, http.StatusConflict, "price.ticket_exists")
	code("tinh2", http.MethodPost, "/v1/price/tickets/"+lateID+"/redate", map[string]any{"version": version(lateID), "day": "2026-10-12",
		"reason": "Nhập nhầm ngày"}, http.StatusConflict, "price.ticket_not_locked")
	sh = call("tinh2", http.MethodPost, "/v1/price/tickets/"+lateID+"/redate", map[string]any{"version": version(lateID), "day": "2026-10-09",
		"reason": "Nhập nhầm ngày"}, http.StatusCreated)
	if sh["day"] != "2026-10-09" || sh["view"] != "late" {
		t.Fatalf("sửa ngày: %v", sh)
	}
	// Xóa phiếu (xóa mềm, lý do bắt buộc) → ngày đó xã lại "Không nộp", nhập thay lại được.
	code("tinh2", http.MethodPost, "/v1/price/tickets/"+lateID+"/delete", map[string]any{"version": version(lateID), "reason": ""},
		http.StatusUnprocessableEntity, "price.reason_required")
	sh = call("tinh2", http.MethodPost, "/v1/price/tickets/"+lateID+"/delete", map[string]any{"version": version(lateID), "reason": "Phiếu trùng"},
		http.StatusCreated)
	if sh["view"] != "deleted" {
		t.Fatalf("xóa phiếu: %v", sh)
	}
	if rs := call("tinh", http.MethodGet, "/v1/price/tickets?day=2026-10-09", nil, http.StatusOK)["rows"].([]any); rs[1].(map[string]any)["view"] != "missed" {
		t.Fatalf("sau xóa: %v", rs)
	}

	// Nhật ký, lịch sử (Cấp Xã chỉ xã mình).
	lg := call("tinh", http.MethodGet, "/v1/price/tickets/"+t2+"/log", nil, http.StatusOK)
	if len(lg["ticket"].([]any)) != 4 || len(lg["alerts"].([]any)) < 6 {
		t.Fatalf("nhật ký: %v", lg)
	}
	code("xaB", http.MethodGet, "/v1/price/tickets/"+t2+"/log", nil, http.StatusForbidden, "price.point_not_assigned")
	hist := call("xaA", http.MethodGet, "/v1/price/tickets/history?from=2026-10-08&to=2026-10-09", nil, http.StatusOK)["rows"].([]any)
	if len(hist) != 2 || hist[0].(map[string]any)["day"] != "2026-10-09" || hist[1].(map[string]any)["view"] != "locked" ||
		str(hist[0].(map[string]any)["adjusted"]) != "1" {
		t.Fatalf("lịch sử: %v", hist)
	}
	code("xaA", http.MethodGet, "/v1/price/tickets/history?point=XBG0001&from=2025-01-01&to=2026-10-09", nil, http.StatusUnprocessableEntity,
		"validation_failed")

	// Ngày 3 (Thứ Hai 12/10): giá tham chiếu dùng giá hiệu chỉnh (AC-05 FN-TI-02.04): P = 9500.
	clk.Advance(3*24*time.Hour - 4*time.Hour) // Thứ Hai 09:31
	if ref := line(call("xaA", http.MethodGet, "/v1/price/tickets/sheet", nil, http.StatusOK), "MH00001")["ref"].(map[string]any); ref["mid"] != "9500" ||
		ref["day"] != "2026-10-09" {
		t.Fatalf("giá tham chiếu sau hiệu chỉnh: %v", ref)
	}
	// MH00002 bị loại ngày 09/10 → tham chiếu lùi về 08/10 (38000).
	if ref := line(call("xaA", http.MethodGet, "/v1/price/tickets/sheet", nil, http.StatusOK), "MH00002")["ref"].(map[string]any); ref["mid"] != "38000" {
		t.Fatalf("giá tham chiếu bỏ dòng đã loại: %v", ref)
	}
	// Nhắc lần 2 (W11.72) bỏ xã đã gửi.
	call("xaA", http.MethodPost, "/v1/price/tickets/sheet", entry("XBG0001", "", 0, true, ln("MH00001", "9500", "9500"), ln("MH00002", "38000", "38000")),
		http.StatusCreated)
	targets, err := app.PointReminders{S: svc}.Targets(context.Background(), tid, "kn", time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), 2)
	if err != nil || len(targets) != 1 || targets[0].CommuneCode != "XBG0002" {
		t.Fatalf("nhắc lần 2 bỏ xã đã gửi: %+v %v", targets, err)
	}

	// ---- Gói NOTIF: sự kiện vòng đời cho pf/notify (outbox cùng giao dịch) ----
	events := func(typ string) []string {
		t.Helper()
		var out []string
		err := tenant.WithTenantTx(context.Background(), env.AdminPool(t, migrations.PF), tid, func(tx pgx.Tx) error {
			rows, err := tx.Query(context.Background(), `SELECT payload::text FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2
			  ORDER BY occurred_at, id`, tid, typ)
			if err != nil {
				return err
			}
			out, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	var raised port.AlertRaisedV1
	if ps := events(port.EventAlertRaised); len(ps) == 0 || json.Unmarshal([]byte(ps[0]), &raised) != nil || raised.PointCode != "XBG0001" ||
		raised.OwnerRef == nil || *raised.OwnerRef != tinhRef || len(raised.ItemCodes) == 0 || raised.PointName == "" {
		t.Fatalf("pf.price.alert.raised → Cấp Tỉnh phụ trách: %v %+v", ps, raised)
	}
	var req port.VerificationRequestedV1
	if ps := events(port.EventVerificationRequested); len(ps) != 1 || json.Unmarshal([]byte(ps[0]), &req) != nil ||
		req.OfficerRef == nil || *req.OfficerRef != ctvA || strings.Join(req.ItemCodes, ",") != "MH00001,MH00002" ||
		req.RequesterRef != tinhRef || req.RespondBy != "2026-10-09T06:30:00Z" {
		t.Fatalf("pf.price.verification.requested (một sự kiện mỗi phiếu): %v %+v", ps, req)
	}
	var rep port.VerificationRespondedV1
	if ps := events(port.EventVerificationResponded); len(ps) != 1 || json.Unmarshal([]byte(ps[0]), &rep) != nil ||
		rep.OwnerRef == nil || *rep.OwnerRef != tinhRef || rep.Choice != "propose" || rep.ItemCode != "MH00001" || rep.ResponderRef != ctvA {
		t.Fatalf("pf.price.verification.responded: %v %+v", ps, rep)
	}
	ps := events(port.EventVerificationDecided)
	if len(ps) != 2 {
		t.Fatalf("pf.price.verification.decided (hiệu chỉnh + loại): %v", ps)
	}
	for i, want := range []string{port.DecisionAdjusted, port.DecisionExcluded} {
		var d port.VerificationDecidedV1
		if json.Unmarshal([]byte(ps[i]), &d) != nil || d.Decision != want || d.OfficerRef == nil || *d.OfficerRef != ctvA || d.DeciderRef != tinhRef {
			t.Fatalf("decided %d: %s", i, ps[i])
		}
	}
	// Mốc khóa 12:00 ngày 12/10 (job price_calendar) phát tổng hợp cho Cấp Tỉnh đúng một lần: 2 xã, 1 đã gửi, 1 không nộp.
	svc.LockSummary = tickets.LockSummary // như price.New
	lockAt := time.Date(2026, 10, 12, 5, 0, 30, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if _, err := svc.CalendarJobTenant(context.Background(), tid, lockAt); err != nil {
			t.Fatalf("job khóa: %v", err)
		}
	}
	var lockSum port.LockSummarizedV1
	if ps := events(port.EventLockSummarized); len(ps) != 1 || json.Unmarshal([]byte(ps[0]), &lockSum) != nil || lockSum.Day != "2026-10-12" ||
		lockSum.Points != 2 || lockSum.Submitted != 1 || lockSum.Missed != 1 || lockSum.Late != 0 || lockSum.OpenAlerts != 0 || lockSum.LockAt != "2026-10-12T05:00:00Z" {
		t.Fatalf("pf.price.lock.summarized: %v %+v", ps, lockSum)
	}
}
