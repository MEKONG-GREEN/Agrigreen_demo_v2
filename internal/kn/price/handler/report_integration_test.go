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

// Báo cáo giá W11.77 (pf 20261013164235; BRD Báo giá v1.3) qua HTTP, một kịch bản hai ngày báo giá 08/10 và 09/10/2026, ba
// xã (hai xã nhóm vùng An Giang, một xã Kiên Giang):
//   - BG-FN-TI-02.01 Tiến độ nhập giá: thẻ khớp bảng sau lọc (AC-02), "Hệ thống đã nhắc" từ nhật ký job (AC-03), Bản nháp
//     11:59 → Không nộp sau 12:00 (AC-04), nhập thay hiện Nộp muộn (FN-TI-02.09 AC-01), xuất Excel;
//   - FN-TI-02.09 hậu điều kiện: nhập thay phát pf.price.ticket.proxied cho cán bộ Xã phụ trách;
//   - BG-FN-TW-01.01 Bảng giá tổng hợp: AC-01 (8.000 / 14.000 / 10.666,6667 / 3 xã), AC-02 trùng bảng giá đã duyệt, theo nhóm
//     vùng, theo xã, ngày chưa công bố → bảng gần nhất, AC-03 đang mở duyệt → phiên bản cũ "Đang cập nhật";
//   - BG-FN-TW-01.02 Biến động: tăng / đi ngang so kỳ trước, chỉ Cấp Tỉnh xuất (AC-03), PDF chưa nối → 503;
//   - BG-FN-TW-01.03 Dashboard: trùng Bảng giá tổng hợp cùng vùng (AC-01), bấm vùng → xã của vùng (AC-02), Cấp Xã không xem;
//   - BG-FN-TI-02.08 Thống kê phiếu: theo cán bộ Xã, nộp muộn tính cho cán bộ phụ trách (BR-02, AC-03 FN-TI-02.09), khóa
//     tháng chỉ QTNV (D11, Q-085), tháng chưa hết không khóa, ảnh chụp, mở lại có lý do.
func TestPriceReportsHTTP_W1177(t *testing.T) {
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Date(2026, 10, 8, 0, 31, 0, 0, time.UTC)) // Thứ Năm 08/10 07:31 giờ VN
	prof := port.Profile{Session: testOwnerSession, Name: "kn", Department: "thong_tin_truyen_thong", Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA",
		"kn_tw": "TW"}, ManageBy: []string{"admin"}, StewardDuty: authz.DutyPriceAdmin}
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clk, []port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	items, tickets := app.NewItemService(svc), app.NewTicketService(svc)
	svc.Points.Submitted = tickets.Submitted // như price.New
	svc.Reminders = app.PointReminders{S: svc}
	items.Hooks.HasPrice, items.Hooks.HasSheets = tickets.HasPrice, tickets.Submitted
	tickets.WireBoard()
	tid := ids.New()
	ctvA, ctvB, ctvC, tinhRef := ids.New(), ids.New(), ids.New(), ids.New()
	who := map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh":  {ActorRef: tinhRef, TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Duties: []string{authz.DutyPriceAdmin}}},
		"tinh2": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh"}}, // không có cờ QTNV
		"xaA":   {ActorRef: ctvA, TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"xaB":   {ActorRef: ctvB, TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"xaC":   {ActorRef: ctvC, TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
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
	BoardHandler{Svc: tickets}.RegisterApp(g)
	ReportHandler{Svc: tickets}.RegisterApp(g)
	raw := func(as, method, path string, body any, want int) *httptest.ResponseRecorder {
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
		if w.Code != want {
			t.Fatalf("%s %s %s: %d (muốn %d) %s", as, method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	call := func(as, method, path string, body any, want int) map[string]any {
		t.Helper()
		return calJSON(t, raw(as, method, path, body, want).Body.Bytes())
	}
	code := func(as, method, path string, body any, want int, problemCode string) {
		t.Helper()
		if p := call(as, method, path, body, want); p["code"] != problemCode {
			t.Fatalf("%s %s: mã lỗi %v (muốn %s)", method, path, p["code"], problemCode)
		}
	}
	str := func(v any) string { return fmt.Sprint(v) }
	ln := func(item, lo, hi string) map[string]any {
		return map[string]any{"item_code": item, "min": lo, "max": hi}
	}
	find := func(list any, key, val string) map[string]any {
		t.Helper()
		for _, x := range list.([]any) {
			if m := x.(map[string]any); str(m[key]) == val {
				return m
			}
		}
		t.Fatalf("không có %s=%s trong %v", key, val, list)
		return nil
	}
	ctx := context.Background()
	runJob := func() {
		t.Helper()
		if _, err := svc.CalendarJobTenant(ctx, tid, clk.Now()); err != nil {
			t.Fatal(err)
		}
	}
	approve := func(day string) {
		t.Helper()
		b := call("tinh", http.MethodGet, "/v1/price/boards/"+day, nil, http.StatusOK)
		b = call("tinh", http.MethodPost, "/v1/price/boards/"+day+"/lock", map[string]any{"seq": b["seq"]}, http.StatusCreated)
		call("tinh", http.MethodPost, "/v1/price/boards/"+day+"/approve", map[string]any{"seq": b["seq"]}, http.StatusCreated)
	}
	const d1, d2 = "2026-10-08", "2026-10-09"

	// ---- Dữ liệu nền ----
	call("admin", http.MethodPost, "/v1/price/catalog/defaults", nil, http.StatusCreated)
	seedCommunes(t, tid, "2025-07-01", "", "C1", "C2", "C3")
	for _, p := range []map[string]any{{"admin_unit_code": "C1", "region_code": "an_giang", "valid_from": d1},
		{"admin_unit_code": "C2", "region_code": "an_giang", "valid_from": d1},
		{"admin_unit_code": "C3", "region_code": "kien_giang", "valid_from": d1}} {
		call("admin", http.MethodPost, "/v1/price/points", p, http.StatusCreated)
	}
	for _, o := range []struct{ point, role, ref string }{{"XBG0001", "xa", ctvA.String()}, {"XBG0002", "xa", ctvB.String()},
		{"XBG0003", "xa", ctvC.String()}, {"XBG0001", "tinh", tinhRef.String()}} {
		call("admin", http.MethodPost, "/v1/price/points/"+o.point+"/officers", map[string]any{"role": o.role, "officer_ref": o.ref,
			"valid_from": d1}, http.StatusCreated)
	}
	for _, it := range []map[string]any{
		{"name": "Dưa leo", "variety": "Loại 1", "group_code": "rau_mau", "unit_code": "kg", "kind": "basic", "valid_from": d1},
		{"name": "Heo hơi", "variety": "Thịt", "group_code": "chan_nuoi", "unit_code": "kg", "kind": "basic", "valid_from": d1},
	} {
		call("tinh", http.MethodPost, "/v1/price/items", it, http.StatusCreated)
	}
	runJob() // 07:31 — nhắc lần 1 tới 3 xã có cán bộ Xã

	// ---- Ngày 1: Xã A, B gửi; Xã C lưu nháp ----
	clk.Advance(150 * time.Minute) // 10:01
	call("xaA", http.MethodPost, "/v1/price/tickets/sheet", map[string]any{"point": "XBG0001", "version": 0, "submit": true,
		"lines": []any{ln("MH00001", "8000", "10000"), ln("MH00002", "38000", "40000")}}, http.StatusCreated)
	call("xaB", http.MethodPost, "/v1/price/tickets/sheet", map[string]any{"point": "XBG0002", "version": 0, "submit": true,
		"lines": []any{ln("MH00001", "9000", "11000"), ln("MH00002", "40000", "42000")}}, http.StatusCreated)
	call("xaC", http.MethodPost, "/v1/price/tickets/sheet", map[string]any{"point": "XBG0003", "version": 0, "submit": false,
		"lines": []any{ln("MH00001", "12000", "14000")}}, http.StatusCreated)
	clk.Advance(60 * time.Minute) // 11:01
	runJob()                      // nhắc lần 2 chỉ tới xã chưa gửi (Xã C)
	clk.Advance(58 * time.Minute) // 11:59

	t.Run("BG_FN_TI_02_01_ProgressDashboard", func(t *testing.T) {
		p := call("tinh", http.MethodGet, "/v1/price/reports/progress", nil, http.StatusOK)
		k := p["cards"].(map[string]any)
		if p["day"] != d1 || p["locked"] != false || str(k["points"]) != "3" || str(k["submitted"]) != "2" || str(k["draft"]) != "1" ||
			str(k["missed"]) != "0" || len(p["rows"].([]any)) != 3 || str(k["unassigned"]) != "2" {
			t.Fatalf("11:59: %v", p)
		}
		// AC-03: Xã C nhắc 2 lần (07:31, 11:01), Xã A 1 lần (đã gửi trước lần 2).
		rc := find(p["rows"], "view", "draft")
		ra := find(p["rows"], "view", "submitted")
		if len(rc["reminders"].([]any)) != 2 || len(ra["reminders"].([]any)) != 1 || rc["updated_at"] == nil {
			t.Fatalf("hệ thống đã nhắc: %v / %v", rc, ra)
		}
		// AC-02: lọc "chưa hoàn thiện" → thẻ đếm đúng các dòng còn lại.
		f := call("tinh", http.MethodGet, "/v1/price/reports/progress?status=incomplete", nil, http.StatusOK)
		if len(f["rows"].([]any)) != 1 || str(f["cards"].(map[string]any)["points"]) != "1" || str(f["cards"].(map[string]any)["draft"]) != "1" {
			t.Fatalf("lọc chưa hoàn thiện: %v", f)
		}
		if m := call("tinh", http.MethodGet, "/v1/price/reports/progress?region=an_giang&mine=true", nil, http.StatusOK); len(m["rows"].([]any)) != 1 {
			t.Fatalf("xã được phân công trong vùng: %v", m)
		}
		code("xaA", http.MethodGet, "/v1/price/reports/progress", nil, http.StatusForbidden, "forbidden")
		code("tw", http.MethodGet, "/v1/price/reports/progress", nil, http.StatusForbidden, "forbidden")
		// AC-04: Bản nháp lúc 11:59 → ngay sau 12:00 Không nộp.
		clk.Advance(2 * time.Minute) // 12:01
		p = call("tinh", http.MethodGet, "/v1/price/reports/progress", nil, http.StatusOK)
		if p["locked"] != true || str(p["cards"].(map[string]any)["missed"]) != "1" || find(p["rows"], "view", "missed") == nil {
			t.Fatalf("12:01: %v", p)
		}
		// Ngày không phải ngày báo giá (Thứ Bảy) → ngày báo giá gần nhất trước đó.
		if s := call("tinh", http.MethodGet, "/v1/price/reports/progress?day=2026-10-10", nil, http.StatusOK); s["no_quote"] != true || s["day"] != d2 {
			t.Fatalf("không có kỳ báo giá: %v", s)
		}
		w := raw("tinh", http.MethodGet, "/v1/price/reports/progress/export", nil, http.StatusOK)
		if !strings.Contains(w.Header().Get("Content-Disposition"), "tien-do-nhap-gia-2026-10-08.xlsx") || w.Body.Len() == 0 {
			t.Fatalf("xuất Excel: %v", w.Header())
		}
	})

	// ---- Nhập thay Xã C (Nộp muộn) → cán bộ Xã nhận thông báo ----
	sheetC := call("tinh", http.MethodGet, "/v1/price/tickets/sheet?point=XBG0003&day="+d1, nil, http.StatusOK)
	call("tinh", http.MethodPost, "/v1/price/tickets/proxy", map[string]any{"point": "XBG0003", "day": d1,
		"version": sheetC["ticket"].(map[string]any)["version"], "submit": true, "reason": "Xã gửi qua Zalo",
		"lines": []any{ln("MH00001", "12000", "14000"), ln("MH00002", "41000", "43000")}}, http.StatusCreated)
	adminPool := env.AdminPool(t, migrations.PF)
	q := func(sql string, args ...any) []string {
		t.Helper()
		var out []string
		if err := tenant.WithTenantTx(ctx, adminPool, tid, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, sql, args...)
			if err != nil {
				return err
			}
			out, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	t.Run("BG_FN_TI_02_09_ProxyNotifiesOfficer", func(t *testing.T) {
		ev := q(`SELECT (payload->>'officer_ref') || '|' || (payload->>'actor_ref') || '|' || (payload->>'point_code') || '|' || (payload->>'items')
		  FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2`, tid, port.EventTicketProxied)
		if len(ev) != 1 || ev[0] != ctvC.String()+"|"+tinhRef.String()+"|XBG0003|2" {
			t.Fatalf("pf.price.ticket.proxied: %v", ev)
		}
		p := call("tinh", http.MethodGet, "/v1/price/reports/progress", nil, http.StatusOK)
		if k := p["cards"].(map[string]any); str(k["late"]) != "1" || str(k["missed"]) != "0" || find(p["rows"], "view", "late") == nil {
			t.Fatalf("Tiến độ hiện Nộp muộn: %v", p)
		}
	})
	approve(d1)
	if n := q(`SELECT count(*)::text FROM pf_price.board_point WHERE tenant_id = $1 AND board_date = $2::date AND board_version = 1`, tid, d1); n[0] != "6" {
		t.Fatalf("ảnh chụp giá theo xã: %v", n)
	}

	// ---- Ngày 2: ba xã gửi trước giờ khóa; duyệt ----
	clk.Advance(22 * time.Hour) // 09/10 10:01
	call("xaA", http.MethodPost, "/v1/price/tickets/sheet", map[string]any{"point": "XBG0001", "version": 0, "submit": true,
		"lines": []any{ln("MH00001", "8500", "9500"), ln("MH00002", "38000", "40000")}}, http.StatusCreated)
	call("xaB", http.MethodPost, "/v1/price/tickets/sheet", map[string]any{"point": "XBG0002", "version": 0, "submit": true,
		"lines": []any{ln("MH00001", "9500", "10500"), ln("MH00002", "40000", "42000")}}, http.StatusCreated)
	call("xaC", http.MethodPost, "/v1/price/tickets/sheet", map[string]any{"point": "XBG0003", "version": 0, "submit": true,
		"lines": []any{ln("MH00001", "13000", "14000"), ln("MH00002", "41000", "43000")}}, http.StatusCreated)
	clk.Advance(2 * time.Hour) // 12:01
	approve(d2)

	t.Run("BG_FN_TW_01_01_SummaryBoard", func(t *testing.T) {
		s := call("tw", http.MethodGet, "/v1/price/reports/summary?day="+d1, nil, http.StatusOK)
		l := find(s["lines"], "item_code", "MH00001")
		if s["day"] != d1 || str(s["version"]) != "1" || s["not_today"] != false || s["updating"] != false || s["read_only"] != true ||
			l["min"] != "8000" || l["max"] != "14000" || l["avg"] != "10666.6667" || str(l["points"]) != "3" || l["group_name"] != "Rau màu" {
			t.Fatalf("AC-01: %v", s)
		}
		// AC-02: trùng bảng giá đã duyệt (Kiểm duyệt & công bố, phiên bản mới nhất).
		b := call("tinh", http.MethodGet, "/v1/price/boards/"+d1, nil, http.StatusOK)
		bl := find(b["published"].(map[string]any)["lines"], "item_code", "MH00001")
		if bl["min"] != l["min"] || bl["max"] != l["max"] || bl["avg"] != l["avg"] {
			t.Fatalf("AC-02: %v ≠ %v", bl, l)
		}
		// Nhóm vùng An Giang: 2 xã; so kỳ trước (08/10): P 9000, 10000 → 9000, 10000 đi ngang.
		s = call("xaA", http.MethodGet, "/v1/price/reports/summary?day="+d2+"&region=an_giang", nil, http.StatusOK)
		l = find(s["lines"], "item_code", "MH00001")
		if s["prev_day"] != d1 || l["min"] != "8500" || l["max"] != "10500" || l["avg"] != "9500" || str(l["points"]) != "2" ||
			l["trend"] != "flat" || l["prev"] != "9500" {
			t.Fatalf("theo nhóm vùng: %v", s)
		}
		// Toàn tỉnh 09/10: (9000 + 10000 + 13500) / 3 = 10833,3333 → +1,56 % Tăng; Heo hơi không đổi → Đi ngang.
		s = call("tw", http.MethodGet, "/v1/price/reports/summary", nil, http.StatusOK)
		if l = find(s["lines"], "item_code", "MH00001"); s["day"] != d2 || l["avg"] != "10833.3333" || l["trend"] != "up" || l["change_pct"] != "1.56" {
			t.Fatalf("toàn tỉnh so kỳ trước: %v", s)
		}
		if l = find(s["lines"], "item_code", "MH00002"); l["trend"] != "flat" {
			t.Fatalf("đi ngang: %v", l)
		}
		// Theo xã / chi tiết một mặt hàng; lọc nhóm hàng.
		if s = call("tw", http.MethodGet, "/v1/price/reports/summary?view=point", nil, http.StatusOK); len(s["points"].([]any)) != 6 {
			t.Fatalf("theo xã: %v", s["points"])
		}
		if s = call("tw", http.MethodGet, "/v1/price/reports/summary?item=MH00001", nil, http.StatusOK); len(s["points"].([]any)) != 3 {
			t.Fatalf("giá từng xã của mặt hàng: %v", s["points"])
		}
		if s = call("tw", http.MethodGet, "/v1/price/reports/summary?group=chan_nuoi", nil, http.StatusOK); len(s["lines"].([]any)) != 1 {
			t.Fatalf("lọc nhóm hàng: %v", s["lines"])
		}
		// Ngày chưa công bố (Thứ Bảy) → bảng gần nhất, nhãn "Chưa công bố hôm nay"; trước mọi bảng → Chưa có dữ liệu.
		if s = call("tw", http.MethodGet, "/v1/price/reports/summary?day=2026-10-10", nil, http.StatusOK); s["day"] != d2 || s["not_today"] != true {
			t.Fatalf("chưa công bố hôm nay: %v", s)
		}
		if s = call("tw", http.MethodGet, "/v1/price/reports/summary?day=2026-10-01", nil, http.StatusOK); s["day"] != nil || len(s["lines"].([]any)) != 0 {
			t.Fatalf("chưa có dữ liệu: %v", s)
		}
		w := raw("tw", http.MethodGet, "/v1/price/reports/summary/export?region=an_giang", nil, http.StatusOK)
		if !strings.Contains(w.Header().Get("Content-Disposition"), "bang-gia-tong-hop-2026-10-09.xlsx") {
			t.Fatalf("xuất Excel: %v", w.Header())
		}
	})

	t.Run("BG_FN_TW_01_03_RegionDashboard", func(t *testing.T) {
		d := call("tw", http.MethodGet, "/v1/price/reports/dashboard", nil, http.StatusOK)
		k := d["cards"].(map[string]any)
		if d["day"] != d2 || str(k["up"]) != "1" || str(k["flat"]) != "1" || str(k["points"]) != "3" || len(d["lines"].([]any)) != 2 ||
			str(find(d["regions"], "code", "an_giang")["points"]) != "2" || str(find(d["regions"], "code", "kien_giang")["points"]) != "1" {
			t.Fatalf("dashboard: %v", d)
		}
		// AC-01: giá trung bình vùng trùng Bảng giá tổng hợp cùng ngày, cùng nhóm vùng.
		var line map[string]any
		for _, x := range d["lines"].([]any) {
			if m := x.(map[string]any); m["item"].(map[string]any)["item_code"] == "MH00001" {
				line = m
			}
		}
		ag := find(line["regions"], "region", "an_giang")
		s := find(call("tw", http.MethodGet, "/v1/price/reports/summary?region=an_giang", nil, http.StatusOK)["lines"], "item_code", "MH00001")
		if ag["avg"] != s["avg"] || find(line["regions"], "region", "kien_giang")["avg"] != "13500" || line["avg"] != "10833.3333" {
			t.Fatalf("AC-01: %v / %v", line, s)
		}
		// AC-02: bấm An Giang → đúng các xã của nhóm vùng có giá trong ngày.
		d = call("tinh", http.MethodGet, "/v1/price/reports/dashboard?region=an_giang", nil, http.StatusOK)
		pts := map[string]bool{}
		for _, x := range d["drill"].([]any) {
			m := x.(map[string]any)
			if m["region_code"] != "an_giang" {
				t.Fatalf("xã ngoài vùng: %v", m)
			}
			pts[str(m["point_code"])] = true
		}
		if len(pts) != 2 || !pts["XBG0001"] || !pts["XBG0002"] || len(d["trend"].([]any)) != 2 {
			t.Fatalf("AC-02: %v", d)
		}
		code("xaA", http.MethodGet, "/v1/price/reports/dashboard", nil, http.StatusForbidden, "forbidden")
		raw("tw", http.MethodGet, "/v1/price/reports/dashboard/export", nil, http.StatusOK)
	})

	t.Run("BG_FN_TW_01_02_Movement", func(t *testing.T) {
		m := call("xaA", http.MethodGet, "/v1/price/reports/movement?period=day", nil, http.StatusOK)
		k := m["cards"].(map[string]any)
		if m["insufficient"] != false || m["cur"].(map[string]any)["from"] != d2 || m["prev"].(map[string]any)["from"] != d1 ||
			str(k["up"]) != "1" || str(k["flat"]) != "1" || str(k["exceeded"]) != "0" || m["can_export"] != false ||
			len(m["trend"].([]any)) != 2 || len(m["points"].([]any)) != 3 {
			t.Fatalf("biến động theo ngày: %v", m)
		}
		if l := find(m["lines"], "item_code", "MH00001"); l["cur"] != "10833.3333" || l["prev"] != "10666.6667" || l["trend"] != "up" {
			t.Fatalf("dòng biến động: %v", l)
		}
		// Kỳ tuần: cả hai ngày cùng tuần 2026-W41, tuần trước chưa có bảng giá → chưa đủ dữ liệu so sánh.
		if w := call("tw", http.MethodGet, "/v1/price/reports/movement?period=week", nil, http.StatusOK); w["insufficient"] != true ||
			str(w["cur"].(map[string]any)["days"]) != "2" {
			t.Fatalf("kỳ tuần: %v", w)
		}
		// Xã cụ thể: Kiên Giang 12.000–14.000 → 13.000–14.000.
		if x := call("tw", http.MethodGet, "/v1/price/reports/movement?point=XBG0003&item=MH00001", nil, http.StatusOK); find(x["lines"], "item_code",
			"MH00001")["change_pct"] != "3.85" {
			t.Fatalf("theo xã: %v", x["lines"])
		}
		// AC-03: Cấp TW, Cấp Xã không xuất; Cấp Tỉnh xuất Excel đúng bộ lọc; PDF chưa nối bộ dựng → 503.
		code("tw", http.MethodGet, "/v1/price/reports/movement/export", nil, http.StatusForbidden, "forbidden")
		code("xaA", http.MethodGet, "/v1/price/reports/movement/export", nil, http.StatusForbidden, "forbidden")
		raw("tinh", http.MethodGet, "/v1/price/reports/movement/export?region=an_giang", nil, http.StatusOK)
		code("tinh", http.MethodGet, "/v1/price/reports/movement/export?format=pdf", nil, http.StatusServiceUnavailable, "price.pdf_unavailable")
		code("tinh", http.MethodGet, "/v1/price/reports/movement?period=year", nil, http.StatusUnprocessableEntity, "validation_failed")
	})

	t.Run("BG_FN_TW_01_01_AC03_ReopenShowsOldVersion", func(t *testing.T) {
		b := call("tinh", http.MethodGet, "/v1/price/boards/"+d2, nil, http.StatusOK)
		call("tinh", http.MethodPost, "/v1/price/boards/"+d2+"/reopen", map[string]any{"seq": b["seq"], "reason": "Sai giá"}, http.StatusCreated)
		rows := call("tinh", http.MethodGet, "/v1/price/tickets?day="+d2, nil, http.StatusOK)["rows"].([]any)
		var tA map[string]any
		for _, x := range rows {
			if m := x.(map[string]any); m["point"].(map[string]any)["code"] == "XBG0001" {
				tA = m["ticket"].(map[string]any)
			}
		}
		call("tinh", http.MethodPost, "/v1/price/tickets/"+str(tA["id"])+"/correct", map[string]any{"version": tA["version"], "reason": "Xã báo nhầm",
			"lines": []any{ln("MH00001", "20000", "20000")}}, http.StatusCreated)
		s := call("tw", http.MethodGet, "/v1/price/reports/summary?day="+d2, nil, http.StatusOK)
		if l := find(s["lines"], "item_code", "MH00001"); s["updating"] != true || str(s["version"]) != "1" || l["avg"] != "10833.3333" {
			t.Fatalf("AC-03 đang mở duyệt → phiên bản cũ: %v", s)
		}
		if s = call("tw", http.MethodGet, "/v1/price/reports/summary?day="+d2+"&item=MH00001", nil, http.StatusOK); find(s["points"], "point_code",
			"XBG0001")["mid"] != "9000" {
			t.Fatalf("giá theo xã của phiên bản cũ: %v", s["points"])
		}
	})

	t.Run("BG_FN_TI_02_08_TicketStatsPerOfficer", func(t *testing.T) {
		s := call("tinh", http.MethodGet, "/v1/price/reports/stats?month=2026-10", nil, http.StatusOK)
		k := s["cards"].(map[string]any)
		if s["monthly"] != true || s["snapshot"] != false || str(k["quote_days"]) != "22" || str(s["pending"]) != "15" ||
			str(k["required"]) != "6" || str(k["on_time"]) != "5" || str(k["late"]) != "1" || str(k["missed"]) != "0" ||
			s["can_close"] != false || len(s["rows"].([]any)) != 3 {
			t.Fatalf("thống kê tháng 10 (giữa tháng): %v", s)
		}
		// BR-02 / AC-03 FN-TI-02.09: phiếu nhập thay ở cột Nộp muộn của cán bộ Xã phụ trách Xã C.
		c := find(s["rows"], "officer_ref", ctvC.String())
		if str(c["required"]) != "2" || str(c["late"]) != "1" || str(c["on_time"]) != "1" || c["point_code"] != "XBG0003" {
			t.Fatalf("Xã C: %v", c)
		}
		if a := find(s["rows"], "officer_ref", ctvA.String()); str(a["adjusted_lines"]) != "1" {
			t.Fatalf("dòng bị hiệu chỉnh: %v", a)
		}
		// AC-02: chi tiết theo ngày khớp trạng thái phiếu.
		days := call("tinh", http.MethodGet, "/v1/price/reports/stats/detail?from="+d1+"&to="+d2+"&officer="+ctvC.String(), nil,
			http.StatusOK)["days"].([]any)
		if len(days) != 2 || find(days, "day", d1)["result"] != "late" || find(days, "day", d2)["result"] != "on_time" {
			t.Fatalf("chi tiết: %v", days)
		}
		if r := call("tinh", http.MethodGet, "/v1/price/reports/stats?from="+d1+"&to="+d1+"&region=kien_giang", nil, http.StatusOK); len(r["rows"].([]any)) != 1 ||
			r["monthly"] != false {
			t.Fatalf("khoảng ngày + nhóm vùng: %v", r)
		}
		code("tw", http.MethodGet, "/v1/price/reports/stats", nil, http.StatusForbidden, "forbidden")
		code("tinh", http.MethodPost, "/v1/price/reports/stats/close", map[string]any{"month": "2026-10", "seq": 0}, http.StatusConflict,
			"price.stat_month_open")
		raw("tinh", http.MethodGet, "/v1/price/reports/stats/export?month=2026-10", nil, http.StatusOK)

		// Sang tháng 11: khóa tháng 10 — chỉ QTNV (D11); mỗi cán bộ phải nộp 17 ngày (08/10 → 30/10), 45 ngày không nộp; mở lại
		// cần lý do.
		clk.Advance(24 * 24 * time.Hour) // 02/11
		code("tinh2", http.MethodPost, "/v1/price/reports/stats/close", map[string]any{"month": "2026-10", "seq": 0}, http.StatusForbidden,
			"price.board_steward_required")
		s = call("tinh", http.MethodPost, "/v1/price/reports/stats/close", map[string]any{"month": "2026-10", "seq": 0}, http.StatusCreated)
		k = s["cards"].(map[string]any)
		if s["snapshot"] != true || s["month"].(map[string]any)["status"] != "closed" || str(k["required"]) != "51" || str(k["missed"]) != "45" ||
			s["can_reopen"] != true || s["can_close"] != false {
			t.Fatalf("khóa tháng: %v", s)
		}
		code("tinh", http.MethodPost, "/v1/price/reports/stats/close", map[string]any{"month": "2026-10", "seq": 1}, http.StatusConflict,
			"price.stat_month_state")
		code("tinh", http.MethodPost, "/v1/price/reports/stats/reopen", map[string]any{"month": "2026-10", "seq": 1}, http.StatusUnprocessableEntity,
			"price.reason_required")
		s = call("tinh", http.MethodPost, "/v1/price/reports/stats/reopen", map[string]any{"month": "2026-10", "seq": 1, "reason": "Bổ sung phiếu"},
			http.StatusCreated)
		if s["snapshot"] != false || s["month"].(map[string]any)["status"] != "open" || s["can_close"] != true {
			t.Fatalf("mở lại: %v", s)
		}
	})
}
