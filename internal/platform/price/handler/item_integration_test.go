package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// Mặt hàng theo thời kỳ W11.73 (pf 000141, BRD Báo giá FN-AD-04.03) qua HTTP: thêm (mã tự sinh, hiệu lực từ ngày báo giá
// kế tiếp, AC-06 giống/phẩm cấp, trùng trong nhóm), danh sách mặt hàng của xã (Cơ bản ∪ Địa phương đã gán — AC-03/04),
// sửa + lịch sử, khóa ĐVT khi đã có giá (AC-01) → "thay thế cho" (mặt hàng cũ ngưng, nhãn "Giá mới"), ngưng (AC-02),
// đổi thứ tự, nhóm hàng (xóa nhóm còn mặt hàng bị chặn), đề xuất từ xã → duyệt (AC-05) / từ chối, bộ theo vụ bật/tắt,
// quyền theo cấp, cách ly tenant, port.Items.
func TestPriceItemsHTTP_W1173(t *testing.T) {
	env := pgtest.Get(t)
	now := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC) // 10:00 giờ VN, thứ Hai 12/10/2026
	prof := port.Profile{Name: "kn", Department: "thong_tin_truyen_thong", Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA", "kn_tw": "TW"},
		ManageBy: []string{"admin"}, StewardDuty: authz.DutyPriceAdmin}
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clock.NewFixed(now),
		[]port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	items := app.NewItemService(svc)
	priced := map[string]bool{}
	items.Hooks.HasPrice = func(_ context.Context, _ pgx.Tx, code string) (bool, error) { return priced[code], nil }

	gin.SetMode(gin.TestMode)
	r := gin.New()
	tid, other := ids.New(), ids.New()
	who := map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Department: "thong_tin_truyen_thong",
			Duties: []string{authz.DutyPriceAdmin}}}, // W11.72: QTNV báo giá
		"xa":    {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"xa2":   {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"tw":    {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tw"}},
		"other": {ActorRef: ids.New(), TenantID: other, KN: &authz.KNContext{Role: "kn_tinh"}},
	}
	r.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	})
	Handler{Svc: svc}.RegisterApp(r.Group("/v1"))
	ItemHandler{Svc: items}.RegisterApp(r.Group("/v1"))
	PointHandler{Svc: svc}.RegisterApp(r.Group("/v1")) // W11.72: xã báo giá có thật
	call := func(as, method, path string, body any, want int) map[string]any {
		t.Helper()
		var buf []byte
		if body != nil {
			buf, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(buf))
		req.Header.Set("X-Test-As", as)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		if w.Code != want {
			t.Fatalf("%s %s %s: %d (muốn %d) %s", as, method, path, w.Code, want, w.Body.String())
		}
		return m
	}
	code := func(m map[string]any) string { return fmt.Sprint(m["code"]) }
	list := func(as, path string) []any {
		t.Helper()
		m := call(as, http.MethodGet, path, nil, http.StatusOK)
		for _, k := range []string{"items", "groups", "assignments", "sets", "proposals"} {
			if v, ok := m[k]; ok {
				return v.([]any)
			}
		}
		t.Fatalf("không có danh sách: %v", m)
		return nil
	}
	pointCodes := func(point, at string) []string {
		t.Helper()
		var out []string
		for _, it := range list("xa", "/v1/price/point-items?point="+point+"&at="+at) {
			out = append(out, fmt.Sprint(it.(map[string]any)["code"]))
		}
		return out
	}
	eq := func(got []string, want ...string) {
		t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%v, muốn %v", got, want)
		}
	}

	call("admin", http.MethodPost, "/v1/price/catalog/defaults", nil, http.StatusCreated)
	// W11.72: point_code phải là xã báo giá có thật — 3 xã XBG0001…3 từ hôm nay; xa phụ trách XBG0002, xa2 phụ trách XBG0003.
	seedCommunes(t, tid, "2026-01-01", "", "A1", "A2", "A3")
	for _, au := range []string{"A1", "A2", "A3"} {
		call("admin", http.MethodPost, "/v1/price/points", map[string]any{"admin_unit_code": au, "region_code": "an_giang",
			"valid_from": "2026-10-12"}, http.StatusCreated)
	}
	for as, pt := range map[string]string{"xa": "XBG0002", "xa2": "XBG0003"} {
		call("admin", http.MethodPost, "/v1/price/points/"+pt+"/officers", map[string]any{"role": "xa",
			"officer_ref": who[as].ActorRef.String(), "valid_from": "2026-10-12"}, http.StatusCreated)
	}
	call("tinh", http.MethodPost, "/v1/price/assignments", map[string]any{"point_code": "X9", "item_codes": []string{"MH00001"}},
		http.StatusNotFound) // mã xã không có

	// Thêm mặt hàng (B2): mã tự sinh, hiệu lực từ ngày báo giá kế tiếp (13/10).
	xoai := map[string]any{"name": "Xoài", "variety": "cát Hòa Lộc loại 1", "group_code": "rau_mau", "unit_code": "kg", "kind": "basic"}
	call("xa", http.MethodPost, "/v1/price/items", xoai, http.StatusForbidden)
	call("tinh", http.MethodPost, "/v1/price/items", map[string]any{"name": "Xoài", "group_code": "rau_mau", "unit_code": "kg", "kind": "basic"},
		http.StatusUnprocessableEntity) // AC-06
	call("tinh", http.MethodPost, "/v1/price/items", map[string]any{"name": "Xoài", "variety": "keo", "group_code": "khong_co", "unit_code": "kg",
		"kind": "basic"}, http.StatusUnprocessableEntity)
	call("tinh", http.MethodPost, "/v1/price/items", map[string]any{"name": "Xoài", "variety": "keo", "group_code": "rau_mau", "unit_code": "kg",
		"kind": "basic", "valid_from": "2026-10-11"}, http.StatusUnprocessableEntity) // quá khứ
	w := call("tinh", http.MethodPost, "/v1/price/items", xoai, http.StatusCreated)
	if code(w) != "MH00001" || w["valid_from"] != "2026-10-13" {
		t.Fatalf("thêm: %v", w)
	}
	call("tinh", http.MethodPost, "/v1/price/items", map[string]any{"name": "xoài", "variety": "Cát Hòa Lộc  loại 1", "group_code": "rau_mau",
		"unit_code": "kg", "kind": "local"}, http.StatusConflict) // trùng trong nhóm
	lua := map[string]any{"name": "Lúa", "variety": "Jasmine 85 (tươi)", "group_code": "lua_gao", "unit_code": "kg", "kind": "local",
		"points": []string{"XBG0001"}, "valid_from": "2026-10-12"}
	if code(call("admin", http.MethodPost, "/v1/price/items", lua, http.StatusCreated)) != "MH00002" {
		t.Fatal("mã thứ hai")
	}
	eq(pointCodes("XBG0001", "2026-10-12"), "MH00002")
	eq(pointCodes("XBG0001", "2026-10-13"), "MH00002", "MH00001") // lua_gao trước rau_mau (AC-03, AC-04)
	eq(pointCodes("XBG0002", "2026-10-13"), "MH00001")

	l := call("tw", http.MethodGet, "/v1/price/items?at=2026-10-12", nil, http.StatusOK)
	if its := l["items"].([]any); len(its) != 2 || its[1].(map[string]any)["state"] != "pending" || l["can_edit"] != false {
		t.Fatalf("danh sách 12/10: %v", l)
	}
	if its := list("tinh", "/v1/price/items?at=2026-10-13&kind=local"); len(its) != 1 || fmt.Sprint(its[0].(map[string]any)["point_count"]) != "1" {
		t.Fatalf("lọc local: %v", its)
	}

	// Sửa (B3) + lịch sử; đã có giá → khóa ĐVT (AC-01), chỉ sửa tên.
	call("tinh", http.MethodPost, "/v1/price/items/MH00001/revise", map[string]any{"name": "Xoài", "variety": "cát Hòa Lộc loại 1",
		"group_code": "rau_mau", "unit_code": "kg", "kind": "basic", "sale_place": "Chợ Long Xuyên"}, http.StatusCreated)
	priced["MH00001"] = true
	call("tinh", http.MethodPost, "/v1/price/items/MH00001/revise", map[string]any{"name": "Xoài", "variety": "cát Hòa Lộc loại 1",
		"group_code": "rau_mau", "unit_code": "ta", "kind": "basic"}, http.StatusConflict)
	call("tinh", http.MethodPost, "/v1/price/items/MH00009/revise", xoai, http.StatusNotFound)
	det := call("xa", http.MethodGet, "/v1/price/items/MH00001?at=2026-10-13", nil, http.StatusOK)
	if h := det["history"].([]any); len(h) != 2 || det["sale_place"] != "Chợ Long Xuyên" || det["has_price"] != true {
		t.Fatalf("chi tiết: %v", det)
	}

	// "Thay thế cho": mặt hàng mới đơn vị tạ, mặt hàng cũ ngưng từ ngày hiệu lực của mặt hàng mới.
	w = call("tinh", http.MethodPost, "/v1/price/items", map[string]any{"name": "Xoài", "variety": "cát Hòa Lộc loại 1", "group_code": "rau_mau",
		"unit_code": "ta", "kind": "basic", "replaces_code": "MH00001", "valid_from": "2026-10-14"}, http.StatusCreated)
	if code(w) != "MH00003" {
		t.Fatalf("thay thế: %v", w)
	}
	eq(pointCodes("XBG0002", "2026-10-13"), "MH00001")
	eq(pointCodes("XBG0002", "2026-10-14"), "MH00003")
	det = call("tinh", http.MethodGet, "/v1/price/items/MH00003?at=2026-10-14", nil, http.StatusOK)
	if det["new_price"] != true || det["replaces_code"] != "MH00001" {
		t.Fatalf("giá mới: %v", det)
	}
	det = call("tinh", http.MethodGet, "/v1/price/items/MH00001?at=2026-10-14", nil, http.StatusOK)
	if det["state"] != "retired" || det["valid_to"] != "2026-10-13" || fmt.Sprint(det["replaced_by"]) != "[MH00003]" {
		t.Fatalf("mặt hàng cũ: %v", det)
	}

	// Ngưng (B4, AC-02): hiệu lực đến 20/10 → từ 21/10 không còn ở xã.
	call("tinh", http.MethodPost, "/v1/price/items/MH00002/retire", map[string]any{"valid_to": "2026-10-10", "reason": "Hết vụ"},
		http.StatusUnprocessableEntity)
	call("tinh", http.MethodPost, "/v1/price/items/MH00002/retire", map[string]any{"valid_to": "2026-10-20", "reason": ""}, http.StatusUnprocessableEntity)
	call("tinh", http.MethodPost, "/v1/price/items/MH00002/retire", map[string]any{"valid_to": "2026-10-20", "reason": "Hết vụ"}, http.StatusCreated)
	eq(pointCodes("XBG0001", "2026-10-20"), "MH00002", "MH00003")
	eq(pointCodes("XBG0001", "2026-10-21"), "MH00003")

	// Đổi thứ tự trong nhóm.
	call("tinh", http.MethodPost, "/v1/price/items", map[string]any{"name": "Dưa leo", "variety": "loại 1", "group_code": "rau_mau",
		"unit_code": "kg", "kind": "basic"}, http.StatusCreated) // MH00004
	call("tinh", http.MethodPost, "/v1/price/items/MH00004/move", map[string]any{"direction": "up"}, http.StatusCreated)
	eq(pointCodes("XBG0002", "2026-10-14"), "MH00004", "MH00003")

	// Nhóm hàng (B6).
	g := call("tinh", http.MethodPost, "/v1/price/groups", map[string]any{"name": "Cây ăn trái"}, http.StatusCreated)
	if code(g) != "nhom_6" {
		t.Fatalf("nhóm mới: %v", g)
	}
	call("tinh", http.MethodPost, "/v1/price/groups/nhom_6/revise", map[string]any{"name": "Trái cây"}, http.StatusCreated)
	call("tinh", http.MethodPost, "/v1/price/groups/rau_mau/retire", nil, http.StatusConflict)
	call("tinh", http.MethodPost, "/v1/price/groups/nhom_6/retire", nil, http.StatusCreated)
	if gs := list("xa", "/v1/price/groups"); len(gs) != 5 {
		t.Fatalf("nhóm: %v", gs)
	}

	// Gán cho xã (B5): mặt hàng Cơ bản không gán riêng.
	call("tinh", http.MethodPost, "/v1/price/assignments", map[string]any{"point_code": "XBG0002", "item_codes": []string{"MH00003"}},
		http.StatusUnprocessableEntity)
	call("tinh", http.MethodPost, "/v1/price/assignments", map[string]any{"point_code": "XBG0002", "item_codes": []string{"MH00002"}}, http.StatusCreated)
	eq(pointCodes("XBG0002", "2026-10-14"), "MH00002", "MH00004", "MH00003")
	call("tinh", http.MethodPost, "/v1/price/assignments/remove", map[string]any{"point_code": "XBG0002", "item_codes": []string{"MH00002"},
		"valid_from": "2026-10-15"}, http.StatusCreated)
	eq(pointCodes("XBG0002", "2026-10-15"), "MH00004", "MH00003")
	if as := list("tinh", "/v1/price/assignments?at=2026-10-14"); len(as) != 2 {
		t.Fatalf("gán: %v", as)
	}

	// Đề xuất từ xã → duyệt (AC-05) / từ chối.
	prop := map[string]any{"point_code": "XBG0002", "name": "Sầu riêng", "variety": "Ri6", "group_code": "rau_mau", "unit_code": "kg", "reason": "Vùng mới"}
	call("tw", http.MethodPost, "/v1/price/proposals", prop, http.StatusForbidden)
	p1 := code(call("xa", http.MethodPost, "/v1/price/proposals", prop, http.StatusCreated))
	prop["name"] = "Bưởi"
	p2 := code(call("xa", http.MethodPost, "/v1/price/proposals", prop, http.StatusCreated))
	call("xa2", http.MethodPost, "/v1/price/proposals", map[string]any{"point_code": "XBG0002", "name": "Mít", "variety": "Thái",
		"group_code": "rau_mau", "unit_code": "kg", "reason": "Có bán"}, http.StatusForbidden) // W11.72: không phụ trách XBG0002
	call("xa2", http.MethodPost, "/v1/price/proposals", map[string]any{"name": "Mít", "variety": "Thái", "group_code": "rau_mau",
		"unit_code": "kg", "reason": "Có bán"}, http.StatusCreated) // xã lấy từ phân công
	if ps := list("xa", "/v1/price/proposals"); len(ps) != 2 {
		t.Fatalf("Cấp Xã chỉ thấy đề xuất của mình: %v", ps)
	}
	if n := call("tinh", http.MethodGet, "/v1/price/items", nil, http.StatusOK)["pending_proposals"]; fmt.Sprint(n) != "3" {
		t.Fatalf("số đề xuất chờ: %v", n)
	}
	call("xa", http.MethodPost, "/v1/price/proposals/"+p1+"/approve", map[string]any{}, http.StatusForbidden)
	ap := call("tinh", http.MethodPost, "/v1/price/proposals/"+p1+"/approve", map[string]any{"name": "Sầu riêng", "variety": "Ri6",
		"group_code": "rau_mau", "unit_code": "kg"}, http.StatusCreated)
	if code(ap) != "MH00005" {
		t.Fatalf("duyệt: %v", ap)
	}
	call("tinh", http.MethodPost, "/v1/price/proposals/"+p1+"/approve", map[string]any{"name": "Sầu riêng", "variety": "Ri6",
		"group_code": "rau_mau", "unit_code": "kg"}, http.StatusConflict)
	call("tinh", http.MethodPost, "/v1/price/proposals/"+p2+"/reject", map[string]any{"note": ""}, http.StatusUnprocessableEntity)
	call("tinh", http.MethodPost, "/v1/price/proposals/"+p2+"/reject", map[string]any{"note": "Đã có mặt hàng tương tự"}, http.StatusCreated)
	call("tinh", http.MethodPost, "/v1/price/proposals/"+ids.New().String()+"/reject", map[string]any{"note": "x"}, http.StatusNotFound)
	eq(pointCodes("XBG0002", "2026-10-13"), "MH00002", "MH00004", "MH00001", "MH00005") // từ ngày báo giá kế tiếp
	eq(pointCodes("XBG0001", "2026-10-13"), "MH00002", "MH00004", "MH00001")
	if ps := list("xa", "/v1/price/proposals?status=rejected"); len(ps) != 1 || ps[0].(map[string]any)["decision_note"] != "Đã có mặt hàng tương tự" {
		t.Fatalf("từ chối: %v", ps)
	}

	// Bộ theo vụ (BR-08): bộ tắt → mặt hàng của bộ không có ở xã; bật lại từ 20/10.
	set := map[string]any{"code": "he_thu", "name": "Bộ Hè Thu", "season": "Hè Thu 2026", "item_codes": []string{"MH00005"}, "enabled": false}
	call("tinh", http.MethodPost, "/v1/price/item-sets", set, http.StatusCreated)
	call("tinh", http.MethodPost, "/v1/price/item-sets", set, http.StatusConflict)
	eq(pointCodes("XBG0002", "2026-10-13"), "MH00002", "MH00004", "MH00001")
	set["enabled"], set["valid_from"] = true, "2026-10-20"
	call("tinh", http.MethodPost, "/v1/price/item-sets/he_thu/revise", set, http.StatusCreated)
	eq(pointCodes("XBG0002", "2026-10-20"), "MH00004", "MH00003", "MH00005")
	if ss := list("tinh", "/v1/price/item-sets?at=2026-10-13"); len(ss) != 1 || ss[0].(map[string]any)["enabled"] != false {
		t.Fatalf("bộ: %v", ss)
	}
	call("tinh", http.MethodPost, "/v1/price/item-sets/khong_co/revise", set, http.StatusNotFound)

	// Cách ly tenant; port.Items theo tenant của ctx.
	if its := list("other", "/v1/price/items"); len(its) != 0 {
		t.Fatalf("tenant khác: %v", its)
	}
	pi, err := items.PointItems(tenant.WithID(context.Background(), tid), "XBG0002", time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
	if err != nil || len(pi) != 3 || pi[2].Code != "MH00005" || pi[2].Kind != "local" {
		t.Fatalf("PointItems: %v %v", pi, err)
	}
	all, err := items.ItemsAt(tenant.WithID(context.Background(), tid), time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	if err != nil || len(all) != 4 {
		t.Fatalf("ItemsAt: %v %v", all, err)
	}
}
