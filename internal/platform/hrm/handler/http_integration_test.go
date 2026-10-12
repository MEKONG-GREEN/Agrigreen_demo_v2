package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// W11.21 (FN-19, ADR-0050) qua HTTP: quyền (Admin ghi, Cấp Tỉnh – HCTH xem, phòng khác/Cấp Xã/TW bị chặn), cài mặc định
// (lặp lại không ghi trùng), thêm/sửa/ngừng có phiên bản, chuẩn hóa mã ngạch (AC-03), trùng mã (chặn), tham chiếu chéo,
// bảng hệ số có hiệu lực (AC-02), tham số có phiên bản, cách ly tenant (RLS), port.Catalog — hình dạng JSON khớp
// contracts/openapi/application.yaml (mục pf/hrm).
func TestHRMCatalogHTTP_W1121(t *testing.T) {
	env := pgtest.Get(t)
	now := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC) // 10:00 giờ VN
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clock.NewFixed(now),
		port.Access{Department: "hanh_chinh_tong_hop", ManageBy: []string{"admin"}, ReadBy: []string{"kn_tinh"}})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	tid, other := ids.New(), ids.New()
	who := map[string]authz.Principal{
		"admin":   {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"hcth":    {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Department: "hanh_chinh_tong_hop"}},
		"tt":      {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Department: "trong_trot"}},
		"xa":      {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", Department: "hanh_chinh_tong_hop", OrgRef: ids.New()}},
		"other":   {ActorRef: ids.New(), TenantID: other, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"realmxa": {ActorRef: ids.New(), TenantID: tid, Roles: []string{"kn_officer"}},
	}
	r.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	})
	Handler{Svc: svc}.RegisterApp(r.Group("/v1"))
	call := func(as, method, path string, body any, want int) map[string]any {
		t.Helper()
		var rd *bytes.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			rd = bytes.NewReader(raw)
		} else {
			rd = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, rd)
		req.Header.Set("X-Test-As", as)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var m map[string]any
		dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
		dec.UseNumber() // số JSON là json.Number — không float (R-10, agnofloat)
		_ = dec.Decode(&m)
		if w.Code != want {
			t.Fatalf("%s %s %s: %d (muốn %d) %s", as, method, path, w.Code, want, w.Body.String())
		}
		return m
	}
	codeOf := func(m map[string]any) any { return m["code"] }

	// Quyền.
	call("", http.MethodGet, "/v1/hrm/status", nil, http.StatusUnauthorized)
	for _, as := range []string{"tt", "xa", "realmxa"} {
		call(as, http.MethodGet, "/v1/hrm/catalogs/grade", nil, http.StatusForbidden)
	}
	st := call("hcth", http.MethodGet, "/v1/hrm/status", nil, http.StatusOK)
	if st["can_manage"] != false || st["day"] != "2026-10-12" || st["rule_version"] != "hrm.v1" || st["scales"] != json.Number("0") {
		t.Fatalf("status: %v", st)
	}
	call("hcth", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusForbidden)
	call("hcth", http.MethodPost, "/v1/hrm/catalogs/position", map[string]any{"code": "x", "name": "X"}, http.StatusForbidden)
	call("admin", http.MethodGet, "/v1/hrm/catalogs/salary", nil, http.StatusNotFound)

	// Cài mặc định — lặp lại không ghi gì.
	d := call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	if d["items"] != json.Number(strconv.Itoa(len(domain.DefaultItems()))) || d["scales"] != json.Number("6") || d["rules"] != json.Number("2") {
		t.Fatalf("mặc định: %v", d)
	}
	if d := call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated); d["items"] != json.Number("0") || d["scales"] != json.Number("0") || d["rules"] != json.Number("0") {
		t.Fatalf("cài lại: %v", d)
	}
	st = call("admin", http.MethodGet, "/v1/hrm/status", nil, http.StatusOK)
	if st["can_manage"] != true || st["counts"].(map[string]any)["grade"] != json.Number("6") || st["scales"] != json.Number("6") || st["rules"] != json.Number("2") {
		t.Fatalf("status sau cài: %v", st)
	}

	// Danh sách ngạch (Cấp Tỉnh – HCTH xem).
	gl := call("hcth", http.MethodGet, "/v1/hrm/catalogs/grade", nil, http.StatusOK)["items"].([]any)
	g0 := gl[0].(map[string]any)
	if len(gl) != 6 || g0["code"] != "01.002" || g0["placeholder"] != false || g0["attrs"].(map[string]any)["equiv_group"] != "CVC" ||
		g0["version"] != json.Number("1") || g0["active"] != true {
		t.Fatalf("ngạch: %v", gl)
	}

	// Thêm ngạch: mã có khoảng trắng/xuống dòng → chuẩn hóa (AC-03); trùng → chặn.
	ng := map[string]any{"code": " V.03.04.12 \n", "name": "Kỹ thuật viên hạng IV", "sort_order": 70,
		"attrs": map[string]any{"equiv_group": "CS", "grade_type": "vien_chuc"}}
	if c := codeOf(call("admin", http.MethodPost, "/v1/hrm/catalogs/grade", ng, http.StatusCreated)); c != "V.03.04.12" {
		t.Fatalf("mã chuẩn hóa: %v", c)
	}
	if p := call("admin", http.MethodPost, "/v1/hrm/catalogs/grade", ng, http.StatusConflict); p["code"] != "hrm.item_exists" {
		t.Fatalf("trùng mã: %v", p)
	}
	call("admin", http.MethodPost, "/v1/hrm/catalogs/grade", map[string]any{"code": "X.1", "name": "X", "attrs": map[string]any{"equiv_group": "A"}},
		http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/hrm/catalogs/grade", map[string]any{"code": "X.1", "name": "X", "salary": 1}, http.StatusUnprocessableEntity)

	// Sửa chức vụ mẫu (thêm PCCV) → phiên bản 2, không còn là mẫu; ngừng → không ở danh sách đang dùng, vẫn có lịch sử.
	rv := call("admin", http.MethodPost, "/v1/hrm/catalogs/position/truong_phong/revise",
		map[string]any{"name": "Trưởng phòng", "sort_order": 30, "attrs": map[string]any{"leader": true, "allowance": "0.30"}}, http.StatusCreated)
	if rv["version"] != json.Number("2") {
		t.Fatalf("sửa: %v", rv)
	}
	call("admin", http.MethodPost, "/v1/hrm/catalogs/position/truong_phong/revise", map[string]any{"code": "khac", "name": "X"}, http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/hrm/catalogs/position/khong_co/revise", map[string]any{"name": "X"}, http.StatusNotFound)
	call("admin", http.MethodPost, "/v1/hrm/catalogs/position/vien_chuc/revise", map[string]any{"name": "Viên chức", "sort_order": 70, "active": false},
		http.StatusCreated)
	pl := call("hcth", http.MethodGet, "/v1/hrm/catalogs/position", nil, http.StatusOK)["items"].([]any)
	for _, x := range pl {
		m := x.(map[string]any)
		if m["code"] == "vien_chuc" {
			t.Fatal("mục đã ngừng không ở danh sách đang dùng")
		}
		if m["code"] == "truong_phong" && (m["placeholder"] != false || m["attrs"].(map[string]any)["allowance"] != "0.3") {
			t.Fatalf("trưởng phòng: %v", m)
		}
	}
	if all := call("hcth", http.MethodGet, "/v1/hrm/catalogs/position?include_retired=true", nil, http.StatusOK)["items"].([]any); len(all) != len(pl)+1 {
		t.Fatalf("kể cả đã ngừng: %d / %d", len(all), len(pl))
	}
	h := call("hcth", http.MethodGet, "/v1/hrm/catalogs/position/vien_chuc/history", nil, http.StatusOK)["versions"].([]any)
	if len(h) != 2 || h[1].(map[string]any)["active"] != false {
		t.Fatalf("lịch sử: %v", h)
	}

	// Tham chiếu chéo: chuyên ngành gắn nhóm của trình độ khác → chặn; nhóm chuyên ngành cho trình độ không có → chặn.
	if p := call("admin", http.MethodPost, "/v1/hrm/catalogs/major", map[string]any{"code": "lam_nghiep", "name": "Lâm nghiệp",
		"attrs": map[string]any{"groups": map[string]any{"dai_hoc": "cd_khac"}}}, http.StatusUnprocessableEntity); p["code"] != "hrm.item_reference" {
		t.Fatalf("nhóm khác trình độ: %v", p)
	}
	call("admin", http.MethodPost, "/v1/hrm/catalogs/major_group", map[string]any{"code": "x_khac", "name": "Khác", "attrs": map[string]any{"level": "tien_si_khoa_hoc"}},
		http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/hrm/catalogs/major", map[string]any{"code": "lam_nghiep", "name": "Lâm nghiệp",
		"attrs": map[string]any{"groups": map[string]any{"dai_hoc": "dh_khac", "trung_cap": "tc_khac"}}}, http.StatusCreated)

	// Bảng hệ số có hiệu lực (BR-02, AC-02): bảng mới từ 01/07/2026; hỏi 30/06 → bảng cũ, hỏi hôm nay → bảng mới.
	call("hcth", http.MethodPost, "/v1/hrm/grade-scales/01.003", map[string]any{"valid_from": "2026-07-01"}, http.StatusForbidden)
	newScale := map[string]any{"valid_from": "2026-07-01", "hold_months": 36, "note": "thử",
		"coefficients": []string{"2.40", "2.75", "3.10", "3.45", "3.80", "4.15", "4.50", "4.85", "5.20"}}
	if v := call("admin", http.MethodPost, "/v1/hrm/grade-scales/01.003", newScale, http.StatusCreated); v["version"] != json.Number("2") || v["code"] != "01.003" {
		t.Fatalf("bảng hệ số: %v", v)
	}
	call("admin", http.MethodPost, "/v1/hrm/grade-scales/01.003", map[string]any{"valid_from": "2026-07-01", "hold_months": 36,
		"coefficients": []string{"2.40", "2.30"}}, http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/hrm/grade-scales/01.003", map[string]any{"valid_from": "01/07/2026", "hold_months": 36,
		"coefficients": []string{"2.40"}}, http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/hrm/grade-scales/99.999", newScale, http.StatusUnprocessableEntity)
	scaleOf := func(at string) map[string]any {
		gs := call("hcth", http.MethodGet, "/v1/hrm/grade-scales?at="+at, nil, http.StatusOK)["grades"].([]any)
		for _, g := range gs {
			m := g.(map[string]any)
			if m["grade"].(map[string]any)["code"] == "01.003" {
				return m["scale"].(map[string]any)
			}
		}
		t.Fatal("thiếu 01.003")
		return nil
	}
	if old := scaleOf("2026-06-30"); old["coefficients"].([]any)[0] != "2.34" || old["placeholder"] != true || old["valid_from"] != "2004-10-01" {
		t.Fatalf("bảng cũ: %v", old)
	}
	if cur := scaleOf("2026-10-12"); cur["coefficients"].([]any)[8] != "5.20" || cur["steps"] != json.Number("9") || cur["placeholder"] != false {
		t.Fatalf("bảng mới: %v", cur)
	}
	if hs := call("hcth", http.MethodGet, "/v1/hrm/grade-scales/01.003/history", nil, http.StatusOK)["versions"].([]any); len(hs) != 2 {
		t.Fatalf("lịch sử bảng hệ số: %v", hs)
	}
	call("hcth", http.MethodGet, "/v1/hrm/grade-scales?at=12/10/2026", nil, http.StatusUnprocessableEntity)

	// Tham số có phiên bản.
	rl := call("hcth", http.MethodGet, "/v1/hrm/rules/promotion", nil, http.StatusOK)
	if cur := rl["current"].(map[string]any); cur["placeholder"] != true || cur["payload"].(map[string]any)["tnvk_start_pct"] != "5" {
		t.Fatalf("tham số mẫu: %v", rl)
	}
	conv := map[string]any{"valid_from": "2027-01-01", "payload": map[string]any{"rules": []any{
		map[string]any{"award_code": "bang_khen_ubnd_tinh", "months": 12}, map[string]any{"award_code": "cstd_co_so", "months": 9}}}}
	call("admin", http.MethodPost, "/v1/hrm/rules/achievement_conversion", conv, http.StatusCreated)
	if p := call("admin", http.MethodPost, "/v1/hrm/rules/achievement_conversion", map[string]any{"valid_from": "2027-01-01",
		"payload": map[string]any{"rules": []any{map[string]any{"award_code": "khong_co", "months": 9}}}}, http.StatusUnprocessableEntity); p["code"] != "hrm.item_reference" {
		t.Fatalf("mã khen thưởng lạ: %v", p)
	}
	call("admin", http.MethodPost, "/v1/hrm/rules/luong", conv, http.StatusNotFound)
	ac := call("hcth", http.MethodGet, "/v1/hrm/rules/achievement_conversion?at=2027-02-01", nil, http.StatusOK)
	if ac["current"].(map[string]any)["valid_from"] != "2027-01-01" || len(ac["versions"].([]any)) != 2 {
		t.Fatalf("quy đổi thành tích: %v", ac)
	}

	// Tenant khác không thấy (RLS p_tenant).
	if xs := call("other", http.MethodGet, "/v1/hrm/catalogs/grade", nil, http.StatusOK)["items"].([]any); len(xs) != 0 {
		t.Fatalf("tenant khác: %v", xs)
	}

	// port.Catalog: tenant lấy từ ctx.
	ctx := tenant.WithID(context.Background(), tid)
	sc, ok, err := svc.ScaleAt(ctx, " 01.003 ", time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC))
	if err != nil || !ok || sc.Coefficients[0].String() != "2.34" || sc.HoldMonths != 36 || !sc.Placeholder {
		t.Fatalf("ScaleAt: %+v %v %v", sc, ok, err)
	}
	if _, ok, err := svc.ScaleAt(tenant.WithID(context.Background(), other), "01.003", now); ok || err != nil {
		t.Fatalf("ScaleAt tenant khác: %v %v", ok, err)
	}
	items, err := svc.Items(ctx, "major")
	if err != nil || len(items) == 0 {
		t.Fatalf("Items: %v %v", items, err)
	}
	if ru, ok, err := svc.RuleAt(ctx, "promotion", now); err != nil || !ok || ru.Version != 1 {
		t.Fatalf("RuleAt: %+v %v %v", ru, ok, err)
	}
}
