package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// W11.81 (BRD Báo giá v1.3 FN-TI-02.07, BR-04 FN-TI-02.06; hợp đồng price_board_daily.v1): nguồn feed của cổng tích hợp
// trên PostgreSQL thật — chỉ phiên bản ĐÃ DUYỆT hiện hành: ngày chưa duyệt / phiếu nháp không có bản ghi; mở duyệt →
// vẫn số đã duyệt, updating = true; hủy mở duyệt → false; duyệt lại → board_version 2, số mới; mọi bản ghi khớp hợp
// đồng tích hợp; không mã xã, phiếu, cán bộ (không dữ liệu cá nhân); tenant khác không thấy.
func TestPricePublicFeed_W1181(t *testing.T) {
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
	ctvA, ctvB, tinhRef := ids.New(), ids.New(), ids.New()
	who := map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh":  {ActorRef: tinhRef, TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Duties: []string{authz.DutyPriceAdmin}}},
		"xaA":   {ActorRef: ctvA, TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
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
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, strings.NewReader(string(b)))
		req.Header.Set("X-Test-As", as)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s %s: %d (muốn %d) %s", as, method, path, w.Code, want, w.Body.String())
		}
		return calJSON(t, w.Body.Bytes())
	}
	ln := func(item, lo, hi string) map[string]any {
		return map[string]any{"item_code": item, "min": lo, "max": hi}
	}
	op := func(action string, seq int) {
		t.Helper()
		call("tinh", http.MethodPost, "/v1/price/boards/"+d1Feed+"/"+action, map[string]any{"seq": seq, "reason": "Sai giá dưa leo"}, http.StatusCreated)
	}

	ctr, err := integration.Get(port.FeedPriceBoardDaily, 1)
	if err != nil {
		t.Fatal(err)
	}
	feed := app.PublicFeed{T: tickets, Profile: "kn"}
	ctx := context.Background()
	// recs đọc tập price_board_daily, kiểm từng bản ghi theo hợp đồng tích hợp, trả theo entity_key.
	recs := func(forTenant uuid.UUID) (map[string]map[string]any, string) {
		t.Helper()
		raw, err := feed.PublishedRecords(ctx, port.FeedPriceBoardDaily, forTenant, "89")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]map[string]any{}
		var all strings.Builder
		for _, x := range raw {
			if err := ctr.ValidateRecord(x); err != nil {
				t.Fatalf("bản ghi lệch hợp đồng: %v %s", err, x)
			}
			key, err := ctr.EntityKeyOf(x)
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			if err := json.Unmarshal(x, &m); err != nil {
				t.Fatal(err)
			}
			out[key] = m
			all.Write(x)
		}
		return out, all.String()
	}
	line := func(m map[string]map[string]any, item string) map[string]any {
		t.Helper()
		x, ok := m["89:"+d1Feed+":"+item]
		if !ok {
			t.Fatalf("thiếu %s: %v", item, m)
		}
		return x
	}

	// ---- Dữ liệu nền: 2 xã, 2 mặt hàng; Xã A gửi trước giờ khóa, Cấp Tỉnh nhập thay xã B lúc 12:00 ----
	call("admin", http.MethodPost, "/v1/price/catalog/defaults", nil, http.StatusCreated)
	seedCommunes(t, tid, "2025-07-01", "", "B1", "B2")
	for _, p := range []map[string]any{{"admin_unit_code": "B1", "region_code": "an_giang", "valid_from": d1Feed},
		{"admin_unit_code": "B2", "region_code": "kien_giang", "valid_from": d1Feed}} {
		call("admin", http.MethodPost, "/v1/price/points", p, http.StatusCreated)
	}
	for _, o := range []struct{ point, role, ref string }{{"XBG0001", "xa", ctvA.String()}, {"XBG0002", "xa", ctvB.String()},
		{"XBG0001", "tinh", tinhRef.String()}} {
		call("admin", http.MethodPost, "/v1/price/points/"+o.point+"/officers", map[string]any{"role": o.role, "officer_ref": o.ref,
			"valid_from": d1Feed}, http.StatusCreated)
	}
	for _, it := range []map[string]any{
		{"name": "Dưa leo", "variety": "Loại 1", "group_code": "rau_mau", "unit_code": "kg", "kind": "basic", "valid_from": d1Feed},
		{"name": "Heo hơi", "variety": "Thịt", "group_code": "chan_nuoi", "unit_code": "kg", "kind": "basic", "valid_from": d1Feed},
	} {
		call("tinh", http.MethodPost, "/v1/price/items", it, http.StatusCreated)
	}
	sh := call("xaA", http.MethodPost, "/v1/price/tickets/sheet", map[string]any{"point": "XBG0001", "version": 0, "submit": true,
		"lines": []any{ln("MH00001", "8000", "10000"), ln("MH00002", "38000", "40000")}}, http.StatusCreated)
	tA := sh["ticket"].(map[string]any)["id"].(string)
	clk.Advance(2 * time.Hour)
	call("tinh", http.MethodPost, "/v1/price/tickets/proxy", map[string]any{"point": "XBG0002", "day": d1Feed, "version": 0, "submit": true,
		"reason": "Xã gửi qua Zalo", "lines": []any{ln("MH00001", "9000", "9000"), ln("MH00002", "37000", "41000")}}, http.StatusCreated)

	// Chưa duyệt: phiếu đã gửi nhưng bảng giá chưa công bố → không bản ghi nào (BR-01 FN-TI-02.07).
	if m, _ := recs(tid); len(m) != 0 {
		t.Fatalf("chưa duyệt → tập rỗng: %v", m)
	}

	// Duyệt v1 → 2 bản ghi toàn tỉnh (CT-04), updating = false.
	op("lock", 0)
	op("approve", 1)
	m, raw := recs(tid)
	l := line(m, "MH00001")
	if len(m) != 2 || l["price_min"] != "8000" || l["price_max"] != "10000" || l["price_avg"] != "9000" || l["points_reported"] != float64(2) ||
		l["board_version"] != float64(1) || l["updating"] != false || l["week_key"] != "2026-W41" || l["province_code"] != "89" ||
		l["item_name"] != "Dưa leo Loại 1" || l["group_code"] != "rau_mau" || l["unit_code"] != "kg" || l["approved_at"] == "" {
		t.Fatalf("v1: %v", m)
	}
	// Không dữ liệu cá nhân / chi tiết nội bộ: không cán bộ, phiếu, mã xã báo giá, ĐVHC xã.
	for _, banned := range []string{tinhRef.String(), ctvA.String(), ctvB.String(), tA, "XBG0001", "XBG0002", `"B1"`, `"B2"`, "actor", "officer",
		"ticket", "commune", "Zalo", tid.String()} {
		if strings.Contains(raw, banned) {
			t.Fatalf("bản ghi không được chứa %q: %s", banned, raw)
		}
	}

	// Mở duyệt (BR-04 FN-TI-02.06): vẫn số v1, nhãn "Đang cập nhật"; hủy mở duyệt → bỏ nhãn.
	op("reopen", 2)
	m, _ = recs(tid)
	if l = line(m, "MH00001"); l["updating"] != true || l["board_version"] != float64(1) || l["price_min"] != "8000" {
		t.Fatalf("mở duyệt: %v", l)
	}
	op("cancel-reopen", 3)
	m, _ = recs(tid)
	if l = line(m, "MH00001"); l["updating"] != false || l["board_version"] != float64(1) {
		t.Fatalf("hủy mở duyệt: %v", l)
	}

	// Mở duyệt + hiệu chỉnh: số đang sửa (chưa duyệt) KHÔNG ra feed; duyệt lại → v2 đăng đè (BR-02 FN-TI-02.07).
	op("reopen", 4)
	call("tinh", http.MethodPost, "/v1/price/tickets/"+tA+"/correct", map[string]any{"version": 1, "reason": "Xã báo nhầm giá",
		"lines": []any{ln("MH00001", "8800", "8800")}}, http.StatusCreated)
	m, _ = recs(tid)
	if l = line(m, "MH00001"); l["price_min"] != "8000" || l["updating"] != true {
		t.Fatalf("số đang sửa không được công bố: %v", l)
	}
	op("lock", 5)
	op("approve", 6)
	m, _ = recs(tid)
	if l = line(m, "MH00001"); len(m) != 2 || l["board_version"] != float64(2) || l["price_min"] != "8800" || l["price_max"] != "9000" ||
		l["price_avg"] != "8900" || l["updating"] != false {
		t.Fatalf("duyệt lại v2: %v", m)
	}

	// Ngày sau: Xã gửi phiếu nhưng chưa duyệt → không bản ghi của ngày đó.
	clk.Advance(22 * time.Hour) // 09/10 10:00 giờ VN
	call("xaA", http.MethodPost, "/v1/price/tickets/sheet", map[string]any{"point": "XBG0001", "version": 0, "submit": true,
		"lines": []any{ln("MH00001", "9100", "9300"), ln("MH00002", "38000", "40000")}}, http.StatusCreated)
	if m, raw = recs(tid); len(m) != 2 || strings.Contains(raw, "2026-10-09") {
		t.Fatalf("ngày chưa duyệt không ra feed: %v", m)
	}

	// Tenant khác không thấy bảng giá của tenant này; tập lạ / mã tỉnh sai → price.unknown_public_dataset.
	if m, _ := recs(ids.New()); len(m) != 0 {
		t.Fatalf("tenant khác: %v", m)
	}
	if _, err := feed.PublishedRecords(ctx, "price_survey", tid, "89"); problem.From(err).Code != port.ErrUnknownPublicDataset.Code {
		t.Fatalf("tập lạ: %v", err)
	}
	if _, err := feed.PublishedRecords(ctx, port.FeedPriceBoardDaily, tid, "AG"); problem.From(err).Code != port.ErrUnknownPublicDataset.Code {
		t.Fatalf("mã tỉnh sai: %v", err)
	}
}

const d1Feed = "2026-10-08"
