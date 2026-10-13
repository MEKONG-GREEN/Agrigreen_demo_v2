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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Duyệt / mở duyệt bảng giá ngày W11.76 (pf 000151/000152, BRD Báo giá v1.3 FN-TI-02.05, 02.06) qua HTTP: chưa qua giờ
// khóa → chặn; Admin không duyệt (BR-03); khóa "Đang duyệt" một người (AC-03); duyệt → phiên bản 1, dòng CT-04, phiếu Đã
// duyệt, Cấp Tỉnh không sửa (price.board_approved); Cấp Xã / TW chỉ thấy bản đã duyệt; mở duyệt (lý do — AC-01), hủy mở
// duyệt khi chưa sửa, sửa rồi thì phải duyệt lại (phiên bản 2); giới hạn 30 ngày (AC-05); view _pub đúng số; giá tham chiếu
// CT-02 chỉ từ ngày đã duyệt (BR-05 FN-TI-02.06: ngày chưa duyệt dùng giá mới); sự kiện pf.price.board.published.
func TestPriceBoardsHTTP_W1176(t *testing.T) {
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC)) // Thứ Năm 08/10 10:00 giờ VN
	prof := port.Profile{Name: "kn", Department: "thong_tin_truyen_thong", Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA",
		"kn_tw": "TW"}, ManageBy: []string{"admin"}, StewardDuty: authz.DutyPriceAdmin}
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clk, []port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	items, tickets := app.NewItemService(svc), app.NewTicketService(svc)
	svc.Points.Submitted = tickets.Submitted // như price.New
	items.Hooks.HasPrice, items.Hooks.HasSheets = tickets.HasPrice, tickets.Submitted
	tickets.WireBoard()
	tid := ids.New()
	ctvA, ctvB, tinhRef, tinh2, tinh3 := ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	who := map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh":  {ActorRef: tinhRef, TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Duties: []string{authz.DutyPriceAdmin}}},
		"tinh2": {ActorRef: tinh2, TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh"}}, // Cấp Tỉnh không có cờ QTNV
		"tinh3": {ActorRef: tinh3, TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Duties: []string{authz.DutyPriceAdmin}}},
		"xaA":   {ActorRef: ctvA, TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"xaB":   {ActorRef: ctvB, TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"tw":    {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tw"}},
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	})
	g := r.Group("/v1")
	Handler{Svc: svc}.RegisterApp(g)
	PointHandler{Svc: svc}.RegisterApp(g)
	ItemHandler{Svc: items}.RegisterApp(g)
	TicketHandler{Svc: tickets}.RegisterApp(g)
	BoardHandler{Svc: tickets}.RegisterApp(g)
	call := func(as, method, path string, body any, want int) map[string]any {
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
		return calJSON(t, w.Body.Bytes())
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
	sheetLine := func(sh map[string]any, item string) map[string]any {
		t.Helper()
		for _, l := range sh["lines"].([]any) {
			if m := l.(map[string]any); m["item_code"] == item {
				return m
			}
		}
		t.Fatalf("không có dòng %s", item)
		return nil
	}
	boardLine := func(lines any, item string) map[string]any {
		t.Helper()
		for _, l := range lines.([]any) {
			if m := l.(map[string]any); m["item_code"] == item {
				return m
			}
		}
		t.Fatalf("bảng giá không có %s: %v", item, lines)
		return nil
	}
	op := func(as, day, action string, seq any, reason string, want int) map[string]any {
		t.Helper()
		return call(as, http.MethodPost, "/v1/price/boards/"+day+"/"+action, map[string]any{"seq": seq, "reason": reason}, want)
	}
	opCode := func(as, day, action string, seq any, reason string, want int, problemCode string) {
		t.Helper()
		if p := op(as, day, action, seq, reason, want); p["code"] != problemCode {
			t.Fatalf("%s %s: mã lỗi %v (muốn %s)", action, day, p["code"], problemCode)
		}
	}
	const d1 = "2026-10-08"

	// ---- Dữ liệu nền: 2 xã, 2 mặt hàng; Xã A gửi trước giờ khóa ----
	call("admin", http.MethodPost, "/v1/price/catalog/defaults", nil, http.StatusCreated)
	seedCommunes(t, tid, "2025-07-01", "", "B1", "B2")
	for _, p := range []map[string]any{{"admin_unit_code": "B1", "region_code": "an_giang", "valid_from": d1},
		{"admin_unit_code": "B2", "region_code": "kien_giang", "valid_from": d1}} {
		call("admin", http.MethodPost, "/v1/price/points", p, http.StatusCreated)
	}
	for _, o := range []struct{ point, role, ref string }{{"XBG0001", "xa", ctvA.String()}, {"XBG0002", "xa", ctvB.String()},
		{"XBG0001", "tinh", tinhRef.String()}} {
		call("admin", http.MethodPost, "/v1/price/points/"+o.point+"/officers", map[string]any{"role": o.role, "officer_ref": o.ref,
			"valid_from": d1}, http.StatusCreated)
	}
	for _, it := range []map[string]any{
		{"name": "Dưa leo", "variety": "Loại 1", "group_code": "rau_mau", "unit_code": "kg", "kind": "basic", "valid_from": d1},
		{"name": "Heo hơi", "variety": "Thịt", "group_code": "chan_nuoi", "unit_code": "kg", "kind": "basic", "valid_from": d1},
	} {
		call("tinh", http.MethodPost, "/v1/price/items", it, http.StatusCreated)
	}
	sh := call("xaA", http.MethodPost, "/v1/price/tickets/sheet", map[string]any{"point": "XBG0001", "version": 0, "submit": true,
		"lines": []any{ln("MH00001", "8000", "10000"), ln("MH00002", "38000", "40000")}}, http.StatusCreated)
	tA := str(sh["ticket"].(map[string]any)["id"])

	// Trước giờ khóa: nút Duyệt vô hiệu (blockers), khóa → 409.
	b := call("tinh", http.MethodGet, "/v1/price/boards/"+d1, nil, http.StatusOK)
	if b["state"] != "open" || b["can"].(map[string]any)["approve"] != false || len(b["blockers"].([]any)) == 0 || str(b["seq"]) != "0" {
		t.Fatalf("trước giờ khóa: %v", b)
	}
	opCode("tinh", d1, "lock", 0, "", http.StatusConflict, "price.board_not_locked")

	// 12:00: Cấp Tỉnh nhập thay xã B (Nộp muộn).
	clk.Advance(2 * time.Hour)
	call("tinh2", http.MethodPost, "/v1/price/tickets/proxy", map[string]any{"point": "XBG0002", "day": d1, "version": 0, "submit": true,
		"reason": "Xã gửi qua Zalo", "lines": []any{ln("MH00001", "9000", "9000"), ln("MH00002", "37000", "41000")}}, http.StatusCreated)
	b = call("tinh", http.MethodGet, "/v1/price/boards/"+d1, nil, http.StatusOK)
	sum := b["summary"].(map[string]any)
	if b["state"] != "open" || b["can"].(map[string]any)["approve"] != true || str(sum["submitted"]) != "1" || str(sum["late"]) != "1" ||
		str(sum["price_lines"]) != "4" || len(b["preview"].([]any)) != 2 || b["published"] != nil {
		t.Fatalf("sẵn sàng duyệt: %v", b)
	}
	// CT-04: MH00001 min 8000, max 10000, trung bình P = (9000 + 9000) / 2; MH00002 P = 39000, 39000.
	if l := boardLine(b["preview"], "MH00001"); l["min"] != "8000" || l["max"] != "10000" || l["avg"] != "9000" || str(l["points"]) != "2" ||
		l["item_name"] != "Dưa leo Loại 1" || l["unit_name"] == "" {
		t.Fatalf("xem trước MH00001: %v", l)
	}
	if x := call("xaA", http.MethodGet, "/v1/price/boards/"+d1, nil, http.StatusOK); x["summary"] != nil || len(x["preview"].([]any)) != 0 ||
		x["published"] != nil || x["can"].(map[string]any)["approve"] != false {
		t.Fatalf("Cấp Xã chưa xem bảng giá chưa duyệt: %v", x)
	}

	// Admin không duyệt khi vận hành chính thức (BR-03, AC-04); D11: Cấp Tỉnh không có cờ QTNV không duyệt / mở duyệt;
	// khóa "Đang duyệt" một người (AC-03).
	code("admin", http.MethodPost, "/v1/price/boards/"+d1+"/lock", map[string]any{"seq": 0}, http.StatusForbidden, "forbidden")
	if p := call("tinh2", http.MethodGet, "/v1/price/boards/"+d1, nil, http.StatusOK); p["can"].(map[string]any)["approve"] != false ||
		p["summary"] == nil {
		t.Fatalf("Cấp Tỉnh không cờ QTNV: xem được, không duyệt được: %v", p)
	}
	for _, a := range []string{"lock", "approve", "unlock", "reopen", "cancel-reopen"} {
		opCode("tinh2", d1, a, 0, "Lý do", http.StatusForbidden, "price.board_steward_required")
	}
	b = op("tinh", d1, "lock", 0, "", http.StatusCreated)
	if b["state"] != "approving" || b["locked_by"] != tinhRef.String() || b["can"].(map[string]any)["approve"] != true ||
		b["can"].(map[string]any)["unlock"] != true || str(b["seq"]) != "1" {
		t.Fatalf("đang duyệt: %v", b)
	}
	opCode("tinh3", d1, "lock", 1, "", http.StatusConflict, "price.board_busy")
	opCode("tinh3", d1, "approve", 1, "", http.StatusConflict, "price.board_busy")
	// Trong khi Đang duyệt, Cấp Tỉnh không sửa phiếu (dữ liệu cố định từ lúc xem trước tới lúc xác nhận).
	code("tinh", http.MethodPost, "/v1/price/tickets/"+tA+"/delete", map[string]any{"version": 1, "reason": "Thử xóa"},
		http.StatusConflict, "price.board_approved")
	opCode("tinh", d1, "approve", 0, "", http.StatusConflict, "price.board_stale")
	b = op("tinh", d1, "approve", 1, "", http.StatusCreated)
	pub := b["published"].(map[string]any)
	if b["state"] != "approved" || str(b["version"]) != "1" || str(pub["version"]) != "1" || pub["week_key"] != "2026-W41" ||
		len(pub["lines"].([]any)) != 2 || b["can"].(map[string]any)["reopen"] != true || len(b["log"].([]any)) != 2 {
		t.Fatalf("đã duyệt v1: %v", b)
	}
	if rows := call("tinh", http.MethodGet, "/v1/price/tickets?day="+d1, nil, http.StatusOK)["rows"].([]any); rows[0].(map[string]any)["view"] != "approved" ||
		rows[1].(map[string]any)["view"] != "approved" {
		t.Fatalf("phiếu Đã duyệt: %v", rows)
	}
	code("tinh", http.MethodPost, "/v1/price/tickets/"+tA+"/correct", map[string]any{"version": 1, "reason": "Sửa giá",
		"lines": []any{ln("MH00001", "8500", "9500")}}, http.StatusConflict, "price.board_approved")
	opCode("tinh", d1, "approve", 2, "", http.StatusConflict, "price.board_state")
	if x := call("tw", http.MethodGet, "/v1/price/boards/"+d1, nil, http.StatusOK); x["published"] == nil || x["summary"] != nil {
		t.Fatalf("Cấp TW xem bảng giá đã duyệt: %v", x)
	}
	if l := call("tw", http.MethodGet, "/v1/price/boards?from=2026-10-05&to=2026-10-09", nil, http.StatusOK)["boards"].([]any); len(l) != 5 ||
		l[3].(map[string]any)["state"] != "approved" || str(l[3].(map[string]any)["version"]) != "1" {
		t.Fatalf("danh sách bảng giá: %v", l)
	}

	// View _pub (pf 000152): một dòng mỗi mặt hàng; tuần 2026-W41 (07–13/10): 5 ngày báo giá, 1 ngày đã duyệt.
	adminPool := env.AdminPool(t, migrations.PF)
	ctx := context.Background()
	// q chạy truy vấn bằng vai quản trị trong tenant (RLS p_tenant áp qua view — chủ view không vượt RLS).
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
	pubBoard := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		for _, r := range q(`SELECT item_code || '=' || board_version::text || '|' || board_reopened::text || '|' || price_min::text || '|' ||
		    price_max::text || '|' || price_avg::text || '|' || points_reported::text || '|' || week_key
		  FROM pf_price_pub.v1_board_daily WHERE tenant_id = $1 AND board_date = $2::date`, tid, d1) {
			k, v, _ := strings.Cut(r, "=")
			out[k] = v
		}
		return out
	}
	if p := pubBoard(); len(p) != 2 || p["MH00001"] != "1|false|8000.00|10000.00|9000.0000|2|2026-W41" {
		t.Fatalf("v1_board_daily: %v", p)
	}
	pubWeek := func() []string {
		t.Helper()
		return q(`SELECT point_code || '|' || commune_code || '|' || tickets_collected || '|' || days_scheduled || '|' ||
		    days_approved || '|' || week_closed || '|' || week_from || '|' || week_to FROM pf_price_pub.v1_ticket_week
		  WHERE tenant_id = $1 AND week_key = '2026-W41' ORDER BY point_code`, tid)
	}
	if w := pubWeek(); len(w) != 2 || w[0] != "XBG0001|B1|1|5|1|false|2026-10-07|2026-10-13" || w[1] != "XBG0002|B2|1|5|1|false|2026-10-07|2026-10-13" {
		t.Fatalf("v1_ticket_week: %v", w)
	}
	if n := q(`SELECT count(*)::text FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2`, tid, port.EventBoardPublished); n[0] != "1" {
		t.Fatalf("sự kiện pf.price.board.published: %v", n)
	}

	// ---- Mở duyệt (FN-TI-02.06): lý do bắt buộc; hủy khi chưa sửa; sửa rồi phải duyệt lại ----
	opCode("tinh", d1, "reopen", 2, " ", http.StatusUnprocessableEntity, "price.reason_required")
	b = op("tinh", d1, "reopen", 2, "Sai giá dưa leo", http.StatusCreated)
	if b["state"] != "reopened" || b["can"].(map[string]any)["cancel_reopen"] != true || b["published"].(map[string]any)["reopened"] != true {
		t.Fatalf("đang mở duyệt: %v", b)
	}
	if p := pubBoard(); p["MH00001"] != "1|true|8000.00|10000.00|9000.0000|2|2026-W41" {
		t.Fatalf("mở duyệt: view vẫn trả v1 kèm cờ: %v", p)
	}
	b = op("tinh", d1, "cancel-reopen", 3, "", http.StatusCreated)
	if b["state"] != "approved" || str(b["version"]) != "1" {
		t.Fatalf("hủy mở duyệt: %v", b)
	}
	opCode("tinh2", d1, "reopen", 4, "Sai giá dưa leo", http.StatusForbidden, "price.board_steward_required")
	op("tinh3", d1, "reopen", 4, "Sai giá dưa leo", http.StatusCreated) // QTNV khác (D11), không cần phân công xã
	// Cấp Xã vẫn không sửa (BR-03); Cấp Tỉnh hiệu chỉnh được.
	code("xaA", http.MethodPost, "/v1/price/tickets/sheet", map[string]any{"point": "XBG0001", "day": d1, "version": 1, "submit": true,
		"lines": []any{ln("MH00001", "8800", "8800")}}, http.StatusConflict, "price.ticket_locked")
	call("tinh", http.MethodPost, "/v1/price/tickets/"+tA+"/correct", map[string]any{"version": 1, "reason": "Xã báo nhầm giá",
		"lines": []any{ln("MH00001", "8800", "8800")}}, http.StatusCreated)
	opCode("tinh", d1, "cancel-reopen", 5, "", http.StatusConflict, "price.board_changed")
	op("tinh", d1, "lock", 5, "", http.StatusCreated)
	b = op("tinh", d1, "approve", 6, "", http.StatusCreated)
	if str(b["version"]) != "2" || boardLine(b["published"].(map[string]any)["lines"], "MH00001")["min"] != "8800" {
		t.Fatalf("duyệt lại v2: %v", b)
	}
	if p := pubBoard(); p["MH00001"] != "2|false|8800.00|9000.00|8900.0000|2|2026-W41" {
		t.Fatalf("view theo phiên bản mới: %v", p)
	}
	if n := q(`SELECT count(*)::text FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2
	    AND payload->>'reapproval' = 'true' AND payload->>'version' = '2'`, tid, port.EventBoardPublished); n[0] != "1" {
		t.Fatalf("sự kiện duyệt lại: %v", n)
	}
	if pb, ok, err := tickets.Published(tenant.WithID(ctx, tid), "kn", time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)); err != nil || !ok ||
		pb.Version != 2 || len(pb.Lines) != 2 || pb.WeekKey != "2026-W41" {
		t.Fatalf("port.Boards: %+v %v %v", pb, ok, err)
	}

	// ---- Giá tham chiếu CT-02 chỉ từ ngày đã duyệt: ngày 2 (09/10) Xã A gửi 9500, chưa duyệt → ngày 3 (12/10) so với giá
	// đã hiệu chỉnh của ngày 1 (8800). ----
	clk.Advance(21 * time.Hour) // Thứ Sáu 09/10 09:00
	call("xaA", http.MethodPost, "/v1/price/tickets/sheet", map[string]any{"point": "XBG0001", "version": 0, "submit": true,
		"lines": []any{ln("MH00001", "9500", "9500"), ln("MH00002", "39000", "39000")}}, http.StatusCreated)
	clk.Advance(72 * time.Hour) // Thứ Hai 12/10 09:00
	sh = call("xaA", http.MethodGet, "/v1/price/tickets/sheet", nil, http.StatusOK)
	if ref := sheetLine(sh, "MH00001")["ref"].(map[string]any); ref["mid"] != "8800" || ref["day"] != d1 {
		t.Fatalf("giá tham chiếu chỉ từ ngày đã duyệt: %v", ref)
	}

	// Giới hạn mở duyệt 30 ngày (AC-05).
	clk.Advance(30 * 24 * time.Hour) // 11/11
	opCode("tinh", d1, "reopen", 7, "Muộn", http.StatusUnprocessableEntity, "price.reopen_expired")
}
