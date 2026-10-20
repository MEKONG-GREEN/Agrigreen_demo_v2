package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Điều tra giá thành W11.78 (pf 20261020161354; BRD Báo giá v1.3 FN-TI-02.10…02.12, FN-TW-01.04) qua HTTP. Mỗi kiểm thử
// một tenant mới: ba xã (Núi Cấm, An Châu — nhóm vùng An Giang; Hà Tiên — Kiên Giang), bốn mặt hàng (Dưa leo Loại 1, Mít
// Thái, Lúa OM 18, Xoài keo), kho định danh giả (họ tên, SĐT nông hộ / cán bộ điều tra không vào pf_price).

type surveyEnv struct {
	t     *testing.T
	r     *gin.Engine
	clk   *clock.Fixed
	tid   uuid.UUID
	vault *clienttest.Fake
	items map[string]string // tên hiển thị → mã
}

func newSurveyEnv(t *testing.T) *surveyEnv {
	t.Helper()
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC)) // Thứ Hai 20/07/2026 08:00 giờ VN
	prof := port.Profile{Name: "kn", Department: "thong_tin_truyen_thong", Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA",
		"kn_tw": "TW"}, ManageBy: []string{"admin"}, StewardDuty: authz.DutyPriceAdmin}
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clk, []port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	items, tickets := app.NewItemService(svc), app.NewTicketService(svc)
	survey := app.NewSurveyService(tickets)
	fake := clienttest.New()
	survey.Vault, survey.PII = fake, true
	tid := ids.New()
	who := map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh":  {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh", Duties: []string{authz.DutyPriceAdmin}}},
		"tinh2": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh"}}, // không có cờ QTNV
		"xa":    {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"tw":    {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tw"}},
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
		c.Next()
		for _, err := range c.Errors {
			t.Logf("lỗi 5xx %s: %v", c.Request.URL.Path, err.Err)
		}
	})
	g := r.Group("/v1")
	Handler{Svc: svc}.RegisterApp(g)
	PointHandler{Svc: svc}.RegisterApp(g)
	ItemHandler{Svc: items}.RegisterApp(g)
	SurveyHandler{Svc: survey}.RegisterApp(g)
	e := &surveyEnv{t: t, r: r, clk: clk, tid: tid, vault: fake, items: map[string]string{}}
	// Danh mục nền, ĐVHC, xã báo giá (nhóm vùng), mặt hàng.
	e.call("admin", http.MethodPost, "/v1/price/catalog/defaults", nil, http.StatusCreated)
	admin := env.AdminPool(t, migrations.PF)
	for code, name := range map[string]string{"C1": "Núi Cấm", "C2": "An Châu", "C3": "Hà Tiên"} {
		if _, err := admin.Exec(context.Background(), `INSERT INTO k_ref.admin_unit (tenant_id, code, valid_from, valid_to, name, level,
		    parent_code, successor_codes) VALUES ($1, $2, '2025-07-01', NULL, $3, 'commune', '89', '{}'::text[])`, tid, code, name); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []map[string]any{{"admin_unit_code": "C1", "region_code": "an_giang", "valid_from": "2026-07-20"},
		{"admin_unit_code": "C3", "region_code": "kien_giang", "valid_from": "2026-07-20"}} {
		e.call("admin", http.MethodPost, "/v1/price/points", p, http.StatusCreated)
	}
	for _, it := range []map[string]any{
		{"name": "Dưa leo", "variety": "Loại 1", "group_code": "rau_mau", "unit_code": "kg", "kind": "basic", "valid_from": "2026-07-20"},
		{"name": "Mít", "variety": "Thái", "group_code": "rau_mau", "unit_code": "kg", "kind": "basic", "valid_from": "2026-07-20"},
		{"name": "Lúa", "variety": "OM 18", "group_code": "lua_gao", "unit_code": "kg", "kind": "basic", "valid_from": "2026-07-20"},
		{"name": "Xoài", "variety": "keo", "group_code": "rau_mau", "unit_code": "kg", "kind": "basic", "valid_from": "2026-07-20"},
	} {
		w := e.call("tinh", http.MethodPost, "/v1/price/items", it, http.StatusCreated)
		e.items[fmt.Sprint(it["name"], " ", it["variety"])] = fmt.Sprint(w["code"])
	}
	return e
}

func (e *surveyEnv) raw(as string, req *http.Request, want int) *httptest.ResponseRecorder {
	e.t.Helper()
	req.Header.Set("X-Test-As", as)
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	if w.Code != want {
		e.t.Fatalf("%s %s %s: %d (muốn %d) %s", as, req.Method, req.URL.Path, w.Code, want, w.Body.String())
	}
	return w
}

func (e *surveyEnv) call(as, method, path string, body any, want int) map[string]any {
	e.t.Helper()
	var buf string
	if body != nil {
		b, _ := json.Marshal(body)
		buf = string(b)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	return calJSON(e.t, e.raw(as, req, want).Body.Bytes())
}

func (e *surveyEnv) code(as, method, path string, body any, want int, problemCode string) {
	e.t.Helper()
	if p := e.call(as, method, path, body, want); p["code"] != problemCode {
		e.t.Fatalf("%s %s: mã lỗi %v (muốn %s)", method, path, p["code"], problemCode)
	}
}

// upload gửi multipart (file + các phần chữ).
func (e *surveyEnv) upload(as, path, csv string, parts map[string]string, want int) map[string]any {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "DieuTraGiaThanh_HeThu2026.csv")
	_, _ = fw.Write([]byte(csv))
	for k, v := range parts {
		_ = mw.WriteField(k, v)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return calJSON(e.t, e.raw(as, req, want).Body.Bytes())
}

// importFile xem trước rồi ghi nhận tệp, trả kết quả ghi nhận.
func (e *surveyEnv) importFile(round, csv string, names map[string]map[string]string) map[string]any {
	e.t.Helper()
	parts := map[string]string{}
	if names != nil {
		b, _ := json.Marshal(names)
		parts["names"] = string(b)
	}
	pv := e.upload("tinh", "/v1/price/surveys/"+round+"/imports/preview", csv, parts, http.StatusOK)
	parts["sha256"] = fmt.Sprint(pv["sha256"])
	return e.upload("tinh", "/v1/price/surveys/"+round+"/imports", csv, parts, http.StatusCreated)
}

const surveyHeader = "Dấu thời gian,Tên cán bộ điều tra,Xã,Nhóm cây,Mặt hàng,Họ và tên nông hộ,Số điện thoại,Diện tích (ha),Sản lượng (kg)," +
	"Giá bán (đ/kg),Chi phí giống,Chi phí phân bón,Thuốc BVTV,Nhân công,Máy móc,Chi khác,Tổng chi phí\n"

// srow dựng một dòng tệp: chi phí giống = seed, các khoản khác 0, tổng chi phí khai = total.
func srow(officer, commune, crop, item, farmer, phone, area, output, price, seed, total string) string {
	return strings.Join([]string{"20/08/2026 10:00", officer, commune, crop, item, farmer, phone, area, output, price, seed, "0", "0", "0", "0",
		"0", total}, ",") + "\n"
}

func (e *surveyEnv) newRound(body map[string]any) string {
	e.t.Helper()
	d := e.call("tinh", http.MethodPost, "/v1/price/surveys", body, http.StatusCreated)
	return fmt.Sprint(d["round"].(map[string]any)["id"])
}

func heThu(extra map[string]any) map[string]any {
	b := map[string]any{"name": "Hè Thu 2026", "season": "he_thu", "year": 2026, "collect_from": "2026-07-14", "collect_to": "2026-08-25",
		"crops": []string{"lua", "rau_mau", "cay_an_trai"}, "open": true,
		"communes": []map[string]any{{"code": "C1", "targets": map[string]int{"lua": 4, "rau_mau": 4, "cay_an_trai": 4}},
			{"code": "C2", "targets": map[string]int{"lua": 2, "rau_mau": 2}}, {"code": "C3", "targets": map[string]int{"cay_an_trai": 3}}},
		"officers": []map[string]any{{"commune": "C2", "full_name": "Lê Văn Hải", "phone": "0912000111", "unit": "TTKN An Giang"},
			{"commune": "C2", "full_name": "Trần Thị Lan", "phone": "0912000222", "unit": "TTKN An Giang"},
			{"commune": "C1", "full_name": "Phạm Minh Tâm", "phone": "0912000333", "unit": "Trạm KN Tịnh Biên"}}}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

func listOf(v any) []any {
	if v == nil {
		return nil
	}
	return v.([]any)
}

// BG-FN-TI-02.10: tạo / mở đợt, cán bộ điều tra (AC-01 gán đúng cán bộ, xã theo tên trong tệp), hết hạn thu thập → Đang rà
// soát (AC-02), tiến độ phiếu đã nhập / chỉ tiêu (AC-03); trùng vụ / năm / nhóm cây cảnh báo; không chọn xã → không mở được;
// gia hạn có lý do; quyền (TW, Xã không vào; Admin chỉ xem).
func TestBG_FN_TI_02_10_SurveyRoundAssignOfficers(t *testing.T) {
	e := newSurveyEnv(t)
	d := e.call("tinh", http.MethodPost, "/v1/price/surveys", heThu(nil), http.StatusCreated)
	rd := d["round"].(map[string]any)
	round := fmt.Sprint(rd["id"])
	if rd["status"] != "collecting" || fmt.Sprint(rd["target"]) != "19" || len(listOf(d["communes"])) != 3 {
		t.Fatalf("đợt mới: %v", d)
	}
	if w := listOf(d["warnings"]); len(w) != 1 || !strings.Contains(fmt.Sprint(w[0]), "Hà Tiên") {
		t.Fatalf("cảnh báo xã chưa giao cán bộ: %v", d["warnings"])
	}
	// Họ tên, SĐT cán bộ điều tra ở kho định danh, trả cho Cấp Tỉnh.
	var anChau map[string]any
	for _, c := range listOf(d["communes"]) {
		if m := c.(map[string]any); m["code"] == "C2" {
			anChau = m
		}
	}
	offs := listOf(anChau["officers"])
	if len(offs) != 2 || anChau["name"] != "An Châu" {
		t.Fatalf("cán bộ xã An Châu: %v", anChau)
	}
	names := map[string]string{}
	for _, o := range offs {
		m := o.(map[string]any)
		names[fmt.Sprint(m["full_name"])] = fmt.Sprint(m["id"])
		if m["phone"] == "" {
			t.Fatalf("thiếu SĐT cán bộ: %v", m)
		}
	}
	if names["Lê Văn Hải"] == "" || names["Trần Thị Lan"] == "" {
		t.Fatalf("tên cán bộ: %v", names)
	}
	// Quyền: TW, Xã không xem; Admin xem, không tạo.
	e.call("tw", http.MethodGet, "/v1/price/surveys", nil, http.StatusForbidden)
	e.call("xa", http.MethodGet, "/v1/price/surveys/"+round, nil, http.StatusForbidden)
	if l := e.call("admin", http.MethodGet, "/v1/price/surveys", nil, http.StatusOK); len(listOf(l["items"])) != 1 {
		t.Fatalf("admin xem danh sách: %v", l)
	}
	e.call("admin", http.MethodPost, "/v1/price/surveys", heThu(nil), http.StatusForbidden)
	// Trùng vụ, năm, nhóm cây → 409; xác nhận → lưu (Nháp), không xã → không mở được.
	e.code("tinh", http.MethodPost, "/v1/price/surveys", heThu(map[string]any{"open": false}), http.StatusConflict, "price.survey_round_duplicate")
	draft := e.newRound(heThu(map[string]any{"open": false, "confirm": true, "name": "Hè Thu 2026 — bổ sung", "communes": []any{},
		"officers": []any{}}))
	e.code("tinh", http.MethodPost, "/v1/price/surveys/"+draft+"/actions", map[string]any{"op": "open"}, http.StatusUnprocessableEntity,
		"price.survey_round_incomplete")
	// AC-01: cột "Tên cán bộ điều tra" khớp tên đã khai báo → phiếu gán đúng cán bộ và xã.
	csv := surveyHeader +
		srow("le van hai", "An Châu", "Lúa", "Lúa OM 18", "Nguyễn Văn A", "0901000001", "1", "6000", "7000", "1000000", "") +
		srow("Trần Thị Lan", "xã An Châu", "Rau màu", "Dưa leo", "Nguyễn Văn B", "0901000002", `"0,5"`, "10.000", "9000", "500000", "") +
		srow("Phạm Minh Tâm", "Núi Cấm", "Cây ăn trái", "Mít Thái", "Nguyễn Văn C", "0901000003", "1", "8000", "10000", "2000000", "") +
		srow("Người Lạ", "Núi Cấm", "Lúa", "Lúa OM 18", "Nguyễn Văn D", "0901000004", "1", "6000", "7100", "1000000", "")
	res := e.importFile(round, csv, nil)
	if fmt.Sprint(res["created"]) != "4" || fmt.Sprint(res["errors"]) != "0" {
		t.Fatalf("ghi nhận: %v", res)
	}
	fl := e.call("tinh", http.MethodGet, "/v1/price/surveys/"+round+"/forms", nil, http.StatusOK)
	byFarmer := map[string]map[string]any{}
	for _, r := range listOf(fl["rows"]) {
		m := r.(map[string]any)
		byFarmer[fmt.Sprint(m["farmer_name"])] = m
	}
	if a := byFarmer["Nguyễn Văn A"]; a == nil || a["officer_id"] != names["Lê Văn Hải"] || a["commune"] != "C2" || a["officer_name"] != "Lê Văn Hải" {
		t.Fatalf("AC-01: phiếu của Lê Văn Hải: %v", a)
	}
	if b := byFarmer["Nguyễn Văn B"]; b == nil || b["officer_id"] != names["Trần Thị Lan"] {
		t.Fatalf("AC-01: phiếu của Trần Thị Lan: %v", b)
	}
	if dd := byFarmer["Nguyễn Văn D"]; dd == nil || dd["officer_id"] != nil {
		t.Fatalf("cán bộ không khai báo → không gán: %v", dd)
	}
	// AC-03: tiến độ phiếu đã nhập / chỉ tiêu theo xã, nhóm cây.
	d = e.call("tinh", http.MethodGet, "/v1/price/surveys/"+round, nil, http.StatusOK)
	for _, c := range listOf(d["communes"]) {
		m := c.(map[string]any)
		imp := m["imported"].(map[string]any)
		switch m["code"] {
		case "C2":
			if fmt.Sprint(imp["lua"]) != "1" || fmt.Sprint(imp["rau_mau"]) != "1" {
				t.Fatalf("AC-03 An Châu: %v", m)
			}
		case "C1":
			if fmt.Sprint(imp["lua"]) != "1" || fmt.Sprint(imp["cay_an_trai"]) != "1" {
				t.Fatalf("AC-03 Núi Cấm: %v", m)
			}
		}
	}
	if rd := d["round"].(map[string]any); fmt.Sprint(rd["forms"]) != "4" || fmt.Sprint(rd["pending"]) != "4" {
		t.Fatalf("tổng phiếu: %v", rd)
	}
	// AC-02: hết ngày kết thúc thu thập → Đang rà soát; gia hạn (lý do bắt buộc) → Đang thu thập.
	e.clk.Advance(time.Date(2026, 8, 25, 17, 1, 0, 0, time.UTC).Sub(e.clk.Now())) // 26/08 00:01 giờ VN
	d = e.call("tinh", http.MethodGet, "/v1/price/surveys/"+round, nil, http.StatusOK)
	if d["round"].(map[string]any)["status"] != "reviewing" {
		t.Fatalf("AC-02: %v", d["round"])
	}
	e.call("tinh", http.MethodPost, "/v1/price/surveys/"+round+"/actions", map[string]any{"op": "extend", "collect_to": "2026-09-05"},
		http.StatusUnprocessableEntity)
	d = e.call("tinh", http.MethodPost, "/v1/price/surveys/"+round+"/actions", map[string]any{"op": "extend", "collect_to": "2026-09-05",
		"reason": "Xã Hà Tiên chưa thu đủ phiếu cây ăn trái"}, http.StatusOK)
	if rd := d["round"].(map[string]any); rd["status"] != "collecting" || rd["collect_to"] != "2026-09-05" {
		t.Fatalf("gia hạn: %v", rd)
	}
	// Sửa đợt: giao cán bộ cho Hà Tiên (giữ cán bộ cũ theo id).
	upd := heThu(map[string]any{"collect_to": "2026-09-05", "seq": d["round"].(map[string]any)["seq"], "confirm": true})
	upd["officers"] = []map[string]any{{"id": names["Lê Văn Hải"], "commune": "C2", "unit": "TTKN An Giang"},
		{"id": names["Trần Thị Lan"], "commune": "C2", "unit": "TTKN An Giang"},
		{"commune": "C3", "full_name": "Võ Thị Hồng", "phone": "0912000444", "unit": "Trạm KN Hà Tiên"}}
	d = e.call("tinh", http.MethodPut, "/v1/price/surveys/"+round, upd, http.StatusOK)
	if len(listOf(d["warnings"])) != 1 { // Núi Cấm mất cán bộ Phạm Minh Tâm
		t.Fatalf("sau sửa: %v", d["warnings"])
	}
	// Nhân bản đợt làm điểm bắt đầu.
	cl := e.call("tinh", http.MethodPost, "/v1/price/surveys/"+round+"/clone", nil, http.StatusCreated)
	if cr := cl["round"].(map[string]any); cr["status"] != "draft" || fmt.Sprint(cr["communes"]) != "3" {
		t.Fatalf("nhân bản: %v", cr)
	}
	// Kho định danh: mọi lời gọi mang mục đích price.cost_survey.
	for _, a := range e.vault.Accesses() {
		if a.Purpose != port.SurveyPurpose {
			t.Fatalf("mục đích kho: %+v", a)
		}
	}
}

// BG-FN-TI-02.11: chuẩn hóa tên mặt hàng (AC-01), diện tích 0 → lỗi dòng (AC-02), trùng SĐT + mặt hàng → bỏ dòng sau (AC-03);
// ánh xạ tên thủ công + lưu từ đồng nghĩa; cột công thức khác → dùng giá trị tự tính; ghi nhận phải đúng tệp đã xem trước;
// nhập lại cùng tệp không tạo phiếu trùng; lỗi được lưu và tải được.
func TestBG_FN_TI_02_11_ImportNormalizesItems(t *testing.T) {
	e := newSurveyEnv(t)
	round := e.newRound(heThu(nil))
	base := "/v1/price/surveys/" + round + "/imports"
	csv := surveyHeader +
		srow("Lê Văn Hải", "An Châu", "Rau màu", "Dưa leo", "Nguyễn Văn A", "0901000001", `"0,5"`, "10000", "9000", "500000", "500000") +
		srow("Lê Văn Hải", "An Châu", "Rau màu", "dưa leo", "Nguyễn Văn B", "0901000002", `"0,5"`, "10000", "9500", "500000", "") +
		srow("Lê Văn Hải", "An Châu", "rau màu", "DƯA LEO", "Nguyễn Văn C", "0901000003", `"0,5"`, "10000", "9200", "500000", "999") +
		srow("Lê Văn Hải", "An Châu", "Rau màu", "Dưa leo", "Nguyễn Văn E", "0901000005", "0", "10000", "9000", "500000", "") +
		srow("Lê Văn Hải", "An Châu", "Rau màu", "Dưa leo", "Nguyễn Văn A", "0901 000 001", `"0,5"`, "10000", "9100", "500000", "") +
		srow("Lê Văn Hải", "Nú Cấm", "Rau màu", "Dưa lê", "Nguyễn Văn F", "0901000006", "1", "9000", "8000", "800000", "")
	e.call("tw", http.MethodGet, "/v1/price/surveys/"+round+"/imports", nil, http.StatusForbidden)
	pv := e.upload("tinh", base+"/preview", csv, nil, http.StatusOK)
	lines := listOf(pv["lines"])
	if len(lines) != 6 {
		t.Fatalf("xem trước: %v", pv)
	}
	ln := func(i int) map[string]any { return lines[i].(map[string]any) }
	item := e.items["Dưa leo Loại 1"]
	for i := 0; i < 3; i++ { // AC-01
		if ln(i)["item"] != item || ln(i)["commune"] != "C2" {
			t.Fatalf("AC-01 dòng %d: %v", i+2, ln(i))
		}
	}
	if ln(0)["level"] != "ok" || ln(2)["level"] != "warning" || !strings.Contains(fmt.Sprint(ln(2)["messages"]), "Tổng chi phí") {
		t.Fatalf("cột công thức: %v / %v", ln(0), ln(2))
	}
	if ln(3)["level"] != "error" || !strings.Contains(fmt.Sprint(ln(3)["messages"]), "bằng 0") { // AC-02
		t.Fatalf("AC-02: %v", ln(3))
	}
	if ln(4)["skip"] != true || !strings.Contains(fmt.Sprint(ln(4)["messages"]), "Trùng phiếu với dòng 2") { // AC-03
		t.Fatalf("AC-03: %v", ln(4))
	}
	if ln(5)["level"] != "error" || len(listOf(pv["unknown"].(map[string]any)["item"])) != 1 ||
		len(listOf(pv["unknown"].(map[string]any)["commune"])) != 1 {
		t.Fatalf("giá trị chưa khớp: %v / %v", ln(5), pv["unknown"])
	}
	if fmt.Sprint(pv["ok"]) != "2" || fmt.Sprint(pv["errors"]) != "2" || fmt.Sprint(pv["skipped"]) != "1" {
		t.Fatalf("đếm: %v", pv)
	}
	// Ghi nhận khác tệp đã xem trước → 409.
	e.upload("tinh", base, csv, map[string]string{"sha256": strings.Repeat("0", 64)}, http.StatusConflict)
	// Ánh xạ thủ công "Dưa lê" → Dưa leo, "Nú Cấm" → Núi Cấm (C1), lưu từ đồng nghĩa.
	names := map[string]map[string]string{"item": {"Dưa lê": item}, "commune": {"Nú Cấm": "C1"}}
	nb, _ := json.Marshal(names)
	pv = e.upload("tinh", base+"/preview", csv, map[string]string{"names": string(nb)}, http.StatusOK)
	if l := listOf(pv["lines"])[5].(map[string]any); l["level"] != "ok" || l["commune"] != "C1" {
		t.Fatalf("ánh xạ thủ công: %v", l)
	}
	res := e.upload("tinh", base, csv, map[string]string{"names": string(nb), "sha256": fmt.Sprint(pv["sha256"]), "save_synonyms": "true"},
		http.StatusCreated)
	if fmt.Sprint(res["created"]) != "4" || fmt.Sprint(res["skipped"]) != "1" || fmt.Sprint(res["errors"]) != "1" {
		t.Fatalf("ghi nhận: %v", res)
	}
	// Từ đồng nghĩa đã lưu → lần sau không cần ánh xạ thủ công; cùng tệp nhập lại → bỏ qua hết (không phiếu trùng).
	pv = e.upload("tinh", base+"/preview", csv, nil, http.StatusOK)
	if l := listOf(pv["lines"])[5].(map[string]any); l["level"] != "ok" || pv["already_imported_at"] == nil {
		t.Fatalf("từ đồng nghĩa / đã nhập: %v %v", l, pv["already_imported_at"])
	}
	res = e.upload("tinh", base, csv, map[string]string{"sha256": fmt.Sprint(pv["sha256"])}, http.StatusCreated)
	if fmt.Sprint(res["created"]) != "0" || fmt.Sprint(res["skipped"]) != "5" {
		t.Fatalf("nhập lại cùng tệp: %v", res)
	}
	fl := e.call("tinh", http.MethodGet, "/v1/price/surveys/"+round+"/forms", nil, http.StatusOK)
	if fmt.Sprint(fl["total"]) != "4" {
		t.Fatalf("số phiếu sau hai lần nhập: %v", fl["total"])
	}
	// Lần nhập + danh sách lỗi tải được.
	ims := listOf(e.call("tinh", http.MethodGet, base, nil, http.StatusOK)["items"])
	if len(ims) != 2 {
		t.Fatalf("lần nhập: %v", ims)
	}
	first := ims[1].(map[string]any)
	req := httptest.NewRequest(http.MethodGet, base+"/"+fmt.Sprint(first["id"])+"/issues", nil)
	if w := e.raw("tinh", req, http.StatusOK); !strings.Contains(w.Header().Get("Content-Type"), "spreadsheet") {
		t.Fatalf("tải danh sách lỗi: %v", w.Header())
	}
	// Thiếu cột bắt buộc → báo trường thiếu, ghi nhận bị từ chối.
	bad := "Xã,Mặt hàng\nAn Châu,Dưa leo\n"
	pv = e.upload("tinh", base+"/preview", bad, nil, http.StatusOK)
	if len(listOf(pv["missing"])) == 0 {
		t.Fatalf("thiếu cột: %v", pv)
	}
	e.upload("tinh", base, bad, map[string]string{"sha256": fmt.Sprint(pv["sha256"])}, http.StatusUnprocessableEntity)
	// PII: nông hộ là chủ thể kho định danh (mục đích price.cost_survey), không có cột họ tên / SĐT ở pf_price.
	creates := 0
	for _, a := range e.vault.Accesses() {
		if a.Op == "create" && a.Purpose == port.SurveyPurpose {
			creates++
		}
	}
	if creates < 4 {
		t.Fatalf("tạo chủ thể nông hộ: %d", creates)
	}
}

// BG-FN-TI-02.12: cờ lệch trung vị (AC-02 Mít Thái 25.000 / trung vị 10.000 → Cao bất thường), duyệt hàng loạt phiếu
// không cờ, Cần xác minh → không chốt được (AC-03), ghi nhận xác minh với giá đúng (lưu giá gốc), duyệt, loại có lý do, xóa,
// chốt đợt, mở lại chỉ Quản trị nghiệp vụ.
func TestBG_FN_TI_02_12_MedianOutliersAndClose(t *testing.T) {
	e := newSurveyEnv(t)
	round := e.newRound(heThu(nil))
	base := "/v1/price/surveys/" + round
	csv := surveyHeader
	for i, p := range []string{"10000", "10000", "9800", "10200", "25000"} {
		csv += srow("Phạm Minh Tâm", "Núi Cấm", "Cây ăn trái", "Mít Thái", fmt.Sprintf("Hộ Mít %d", i), fmt.Sprintf("090100010%d", i), "1",
			"8000", p, "2000000", "")
	}
	csv += srow("Phạm Minh Tâm", "Núi Cấm", "Lúa", "Lúa OM 18", "Hộ Lúa", "0901000200", "1", "6000", "7000", "1000000", "")
	pv := e.upload("tinh", base+"/imports/preview", csv, nil, http.StatusOK)
	if l := listOf(pv["lines"])[4].(map[string]any); l["flag"] != "high" {
		t.Fatalf("cờ dự kiến khi xem trước: %v", l)
	}
	e.importFile(round, csv, nil)
	fl := e.call("tinh", http.MethodGet, base+"/forms?flag=flag", nil, http.StatusOK)
	rows := listOf(fl["rows"])
	if len(rows) != 1 {
		t.Fatalf("phiếu có cờ: %v", fl)
	}
	hi := rows[0].(map[string]any)
	if hi["flag"] != "high" || hi["median"] != "10000" || hi["pct"] != "150.00" || hi["price"] != "25000" { // AC-02
		t.Fatalf("AC-02: %v", hi)
	}
	k := fl["kpis"].(map[string]any)
	if fmt.Sprint(k["sent"]) != "6" || fmt.Sprint(k["flagged"]) != "1" || fmt.Sprint(k["pending"]) != "6" {
		t.Fatalf("thẻ chỉ số: %v", k)
	}
	formID := fmt.Sprint(hi["id"])
	// Duyệt phiếu có cờ chưa xác minh không ghi chú → 422.
	e.call("tinh", http.MethodPost, base+"/forms/"+formID+"/actions", map[string]any{"op": "approve"}, http.StatusUnprocessableEntity)
	if n := e.call("tinh", http.MethodPost, base+"/forms/approve-unflagged", nil, http.StatusOK); fmt.Sprint(n["approved"]) != "5" {
		t.Fatalf("duyệt hàng loạt: %v", n)
	}
	// Cần xác minh → hiện cán bộ điều tra để liên hệ; chốt đợt bị chặn (AC-03).
	f := e.call("tinh", http.MethodPost, base+"/forms/"+formID+"/actions", map[string]any{"op": "request_verify"}, http.StatusOK)
	if f["status"] != "verify" || f["officer_name"] != "Phạm Minh Tâm" || f["officer_phone"] != "0912000333" || f["farmer_name"] != "Hộ Mít 4" {
		t.Fatalf("cần xác minh: %v", f)
	}
	fl = e.call("tinh", http.MethodGet, base+"/forms", nil, http.StatusOK)
	if fl["can_close"] != false {
		t.Fatalf("AC-03: can_close phải false: %v", fl["can_close"])
	}
	e.code("tinh", http.MethodPost, base+"/actions", map[string]any{"op": "close"}, http.StatusConflict, "price.survey_round_pending")
	e.code("tinh", http.MethodPost, base+"/forms/"+formID+"/actions", map[string]any{"op": "approve", "note": "x"}, http.StatusConflict,
		"price.survey_form_state")
	// Ghi nhận xác minh: giá đúng 11.000, giữ giá gốc 25.000 (BR-04); rồi duyệt không cần ghi chú.
	f = e.call("tinh", http.MethodPost, base+"/forms/"+formID+"/actions", map[string]any{"op": "record_verify",
		"note": "Cán bộ nhập nhầm, giá đúng 11.000", "price": "11000"}, http.StatusOK)
	if f["status"] != "verified" || f["price"] != "11000" || f["orig_price"] != "25000" || f["corrected"] != true || f["flag"] != "" {
		t.Fatalf("ghi nhận xác minh: %v", f)
	}
	f = e.call("tinh", http.MethodPost, base+"/forms/"+formID+"/actions", map[string]any{"op": "approve"}, http.StatusOK)
	if f["status"] != "approved" || len(listOf(f["log"])) != 4 {
		t.Fatalf("duyệt sau xác minh: %v", f)
	}
	// Loại cần lý do; xóa phiếu nhập nhầm có lý do.
	fl = e.call("tinh", http.MethodGet, base+"/forms?item="+e.items["Lúa OM 18"], nil, http.StatusOK)
	lua := fmt.Sprint(listOf(fl["rows"])[0].(map[string]any)["id"])
	e.call("tinh", http.MethodPost, base+"/forms/"+lua+"/actions", map[string]any{"op": "request_verify"}, http.StatusOK)
	e.call("tinh", http.MethodPost, base+"/forms/"+lua+"/actions", map[string]any{"op": "reject"}, http.StatusUnprocessableEntity)
	e.call("tinh", http.MethodPost, base+"/forms/"+lua+"/actions", map[string]any{"op": "reject", "note": "Không xác minh được hộ"},
		http.StatusOK)
	// Phân bố giá theo xã của một mặt hàng.
	fl = e.call("tinh", http.MethodGet, base+"/forms?item="+e.items["Mít Thái"], nil, http.StatusOK)
	if dist := listOf(fl["distribution"]); len(dist) != 1 || dist[0].(map[string]any)["max"] != "11000" {
		t.Fatalf("phân bố giá: %v", fl["distribution"])
	}
	// Xuất danh sách phiếu (không họ tên, SĐT nông hộ); Cấp TW không xem danh sách phiếu.
	e.call("tw", http.MethodGet, base+"/forms", nil, http.StatusForbidden)
	req := httptest.NewRequest(http.MethodGet, base+"/forms/export", nil)
	if w := e.raw("tinh", req, http.StatusOK); bytes.Contains(w.Body.Bytes(), []byte("Hộ Mít")) {
		t.Fatal("tệp xuất có họ tên nông hộ")
	}
	// Chốt đợt; thao tác rà soát sau chốt → 409; mở lại: Cấp Tỉnh không có cờ QTNV → 403, QTNV có lý do → Đang rà soát.
	d := e.call("tinh", http.MethodPost, base+"/actions", map[string]any{"op": "close"}, http.StatusOK)
	if d["round"].(map[string]any)["status"] != "closed" {
		t.Fatalf("chốt đợt: %v", d["round"])
	}
	e.code("tinh", http.MethodPost, base+"/forms/"+formID+"/actions", map[string]any{"op": "request_verify"}, http.StatusConflict,
		"price.survey_round_state")
	e.call("tinh2", http.MethodPost, base+"/actions", map[string]any{"op": "reopen", "reason": "Rà soát lại"}, http.StatusForbidden)
	e.call("tinh", http.MethodPost, base+"/actions", map[string]any{"op": "reopen"}, http.StatusUnprocessableEntity)
	d = e.call("tinh", http.MethodPost, base+"/actions", map[string]any{"op": "reopen", "reason": "Sở yêu cầu rà soát lại Mít Thái"},
		http.StatusOK)
	if d["round"].(map[string]any)["status"] != "reviewing" {
		t.Fatalf("mở lại: %v", d["round"])
	}
	// Đợt mở lại: phiếu Đã duyệt chuyển Cần xác minh được.
	e.call("tinh", http.MethodPost, base+"/forms/"+formID+"/actions", map[string]any{"op": "request_verify"}, http.StatusOK)
}

// BG-FN-TW-01.04: chỉ phiếu Đã duyệt của đợt Đã chốt (AC-02 phiếu Loại, Đã xóa không tính); AC-01 giá thành 5.274, 5.580,
// 5.384 → bình quân 5.413, trung vị 5.384; AC-03 Cấp TW không thấy họ tên, SĐT nông hộ; chỉ Cấp Tỉnh, Admin xuất; "Mẫu nhỏ";
// theo nhóm vùng; so sánh hai đợt.
func TestBG_FN_TW_01_04_CostMedianNoPII(t *testing.T) {
	e := newSurveyEnv(t)
	round := e.newRound(heThu(nil))
	base := "/v1/price/surveys/" + round
	// Chưa có đợt chốt → báo cáo rỗng.
	if v := e.call("tw", http.MethodGet, "/v1/price/survey-report", nil, http.StatusOK); v["main"] != nil {
		t.Fatalf("chưa có đợt chốt: %v", v)
	}
	// Lúa OM 18: diện tích 1 ha, sản lượng 1.000 kg, chi phí giống = giá thành × 1.000.
	csv := surveyHeader +
		srow("", "Núi Cấm", "Lúa", "Lúa OM 18", "Trương Văn Một", "0907000001", "1", "1000", "7000", "5274000", "") +
		srow("", "Núi Cấm", "Lúa", "Lúa OM 18", "Trương Văn Hai", "0907000002", "1", "1000", "7200", "5580000", "") +
		srow("", "Hà Tiên", "Lúa", "Lúa OM 18", "Trương Văn Ba", "0907000003", "1", "1000", "6800", "5384000", "") +
		srow("", "Hà Tiên", "Lúa", "Lúa OM 18", "Trương Văn Bốn", "0907000004", "1", "1000", "6900", "9000000", "") +
		srow("", "Hà Tiên", "Lúa", "Lúa OM 18", "Trương Văn Năm", "0907000005", "1", "1000", "6900", "1000000", "") +
		srow("", "Núi Cấm", "Cây ăn trái", "Xoài keo", "Trương Văn Sáu", "0907000006", "1", "9000", "12000", "30000000", "")
	e.importFile(round, csv, nil)
	fl := e.call("tinh", http.MethodGet, base+"/forms", nil, http.StatusOK)
	byFarmer := map[string]string{}
	for _, r := range listOf(fl["rows"]) {
		m := r.(map[string]any)
		byFarmer[fmt.Sprint(m["farmer_name"])] = fmt.Sprint(m["id"])
	}
	e.call("tinh", http.MethodPost, base+"/forms/"+byFarmer["Trương Văn Bốn"]+"/actions", map[string]any{"op": "request_verify"}, http.StatusOK)
	e.call("tinh", http.MethodPost, base+"/forms/"+byFarmer["Trương Văn Bốn"]+"/actions", map[string]any{"op": "reject",
		"note": "Chi phí không hợp lý"}, http.StatusOK)
	e.call("tinh", http.MethodPost, base+"/forms/"+byFarmer["Trương Văn Năm"]+"/actions", map[string]any{"op": "delete",
		"note": "Nhập nhầm hộ"}, http.StatusOK)
	e.call("tinh", http.MethodPost, base+"/forms/approve-unflagged", nil, http.StatusOK)
	// Đợt chưa chốt → chưa vào báo cáo.
	if v := e.call("tw", http.MethodGet, "/v1/price/survey-report", nil, http.StatusOK); v["main"] != nil {
		t.Fatalf("đợt chưa chốt không vào báo cáo: %v", v["main"])
	}
	e.call("tinh", http.MethodPost, base+"/actions", map[string]any{"op": "close"}, http.StatusOK)
	w := e.raw("tw", httptest.NewRequest(http.MethodGet, "/v1/price/survey-report", nil), http.StatusOK)
	body := w.Body.String()
	for _, pii := range []string{"Trương Văn", "0907000", "907000"} { // AC-03
		if strings.Contains(body, pii) {
			t.Fatalf("AC-03: báo cáo cho Cấp TW có dữ liệu cá nhân %q", pii)
		}
	}
	v := calJSON(t, w.Body.Bytes())
	main := v["main"].(map[string]any)
	var lua, xoai map[string]any
	for _, r := range listOf(main["rows"]) {
		m := r.(map[string]any)
		switch m["item"] {
		case e.items["Lúa OM 18"]:
			lua = m
		case e.items["Xoài keo"]:
			xoai = m
		}
	}
	// AC-01, AC-02: 3 phiếu (Loại, Đã xóa không tính) → bình quân 5.413, trung vị 5.384.
	if lua == nil || fmt.Sprint(lua["n"]) != "3" || lua["avg_cost_per_kg"] != "5413" || lua["median_cost_per_kg"] != "5384" ||
		lua["small_sample"] != false || lua["avg_price"] != "7000" || lua["median_price"] != "7000" {
		t.Fatalf("AC-01/02: %v", lua)
	}
	if xoai == nil || xoai["small_sample"] != true || xoai["avg_cost_per_kg"] != "3333" {
		t.Fatalf("Mẫu nhỏ: %v", xoai)
	}
	if k := main["kpis"].(map[string]any); fmt.Sprint(k["forms"]) != "4" {
		t.Fatalf("thẻ chỉ số: %v", k)
	}
	regions := map[string]bool{}
	for _, r := range listOf(main["regions"]) {
		m := r.(map[string]any)
		if m["item"] == e.items["Lúa OM 18"] {
			regions[fmt.Sprint(m["region"])] = true
		}
	}
	if !regions["an_giang"] || !regions["kien_giang"] {
		t.Fatalf("theo nhóm vùng: %v", main["regions"])
	}
	// Lọc nhóm vùng + bấm mặt hàng → phân bố theo xã.
	v = e.call("tinh", http.MethodGet, "/v1/price/survey-report?region=kien_giang&item="+e.items["Lúa OM 18"], nil, http.StatusOK)
	main = v["main"].(map[string]any)
	if cs := listOf(main["communes"]); len(cs) != 1 || cs[0].(map[string]any)["commune_name"] != "Hà Tiên" {
		t.Fatalf("phân bố theo xã: %v", main["communes"])
	}
	// Xuất: Cấp TW 403, Cấp Tỉnh có tệp; Cấp Xã không xem.
	e.call("tw", http.MethodGet, "/v1/price/survey-report/export", nil, http.StatusForbidden)
	e.call("xa", http.MethodGet, "/v1/price/survey-report", nil, http.StatusForbidden)
	if w := e.raw("tinh", httptest.NewRequest(http.MethodGet, "/v1/price/survey-report/export", nil), http.StatusOK); bytes.Contains(w.Body.Bytes(),
		[]byte("Trương")) {
		t.Fatal("tệp xuất có họ tên nông hộ")
	}
	// So sánh hai đợt: đợt Đông Xuân chốt sau cũng có Lúa OM 18.
	r2 := e.newRound(map[string]any{"name": "Thu Đông 2026", "season": "thu_dong", "year": 2026, "collect_from": "2026-09-01",
		"collect_to": "2026-10-15", "crops": []string{"lua"}, "open": true, "communes": []map[string]any{{"code": "C1", "targets": map[string]int{"lua": 2}}}})
	e.importFile(r2, surveyHeader+srow("", "Núi Cấm", "Lúa", "Lúa OM 18", "Trương Văn Bảy", "0907000007", "1", "1000", "7500", "6000000", ""), nil)
	e.call("tinh", http.MethodPost, "/v1/price/surveys/"+r2+"/forms/approve-unflagged", nil, http.StatusOK)
	e.call("tinh", http.MethodPost, "/v1/price/surveys/"+r2+"/actions", map[string]any{"op": "close"}, http.StatusOK)
	v = e.call("tw", http.MethodGet, "/v1/price/survey-report?round="+r2+"&compare="+round, nil, http.StatusOK)
	if len(listOf(v["rounds"])) != 2 || v["compare"] == nil || v["main"].(map[string]any)["round"].(map[string]any)["id"] != r2 {
		t.Fatalf("so sánh hai đợt: %v", v)
	}
}
