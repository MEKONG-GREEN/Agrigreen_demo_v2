package masterdata_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func fieldReason(b map[string]any) string {
	es, _ := b["errors"].([]any)
	if len(es) == 0 {
		return ""
	}
	e, _ := es[0].(map[string]any)
	r, _ := e["reason"].(string)
	return r
}

func proposeBody(key string, value, text any, eff string) map[string]any {
	b := map[string]any{"key": key, "unit": "", "effective_from": eff, "source_doc": "QĐ quy chuẩn hiển thị số 1"}
	if value != nil {
		b["value"] = value
	}
	if text != nil {
		b["text"] = text
	}
	return b
}

// GIS US-CFG-01 AC-1/AC-2, US-CFG-02 AC-3: màu heatmap phải là HEX "#RRGGBB" — sai định dạng
// bị chặn với đúng thông điệp của AC; màu đúng định dạng qua duyệt hai người (R-56) rồi mới
// có hiệu lực ("áp dụng ngay" = áp dụng ngay khi được duyệt); legend đọc qua display-config.
func TestDisplayPaletteHexValidation_GIS_US_CFG_01(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	setter := f.caller(tid, port.RoleParamSetter)
	approver := f.caller(tid, port.RoleParamApprover)
	viewer := f.caller(tid, "plot_viewer")
	key := port.ParamGISStageColorPrefix + ":chin_ro"

	for _, bad := range []any{"red", "#12345", "#1234567", "1E90FF", "#GG0000"} {
		code, b := setter.adm(http.MethodPost, "/v1/masterdata/params/versions", proposeBody(key, nil, bad, "2026-06-01"))
		if code != 422 || fieldReason(b) != "Mã màu không đúng định dạng HEX (ví dụ: #1E90FF)." {
			t.Fatalf("màu %v phải bị chặn: %d %v", bad, code, b)
		}
	}
	// Giá trị số cho khóa màu cũng bị chặn.
	if code, b := setter.adm(http.MethodPost, "/v1/masterdata/params/versions", proposeBody(key, "12", nil, "2026-06-01")); code != 422 {
		t.Fatalf("khóa màu nhận số: %d %v", code, b)
	}

	// Mặc định (seed) trước khi đổi.
	code, cfg := viewer.get("/v1/masterdata/display-config")
	if code != 200 {
		t.Fatalf("display-config: %d %v", code, cfg)
	}
	cur := cfg["stage_colors"].(map[string]any)["chin_ro"].(map[string]any)
	if cur["value"] != "#D6452F" || cur["version"].(float64) != 1 || cur["source"] != "approved" {
		t.Fatalf("màu mặc định đã seed: %v", cur)
	}
	if len(cfg["stage_colors"].(map[string]any)) != len(port.StageCodes) || len(cfg["capacity_colors"].(map[string]any)) != len(port.CapacityClasses) {
		t.Fatalf("đủ chiều bảng màu: %v", cfg)
	}

	eff := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	code, b := setter.adm(http.MethodPost, "/v1/masterdata/params/versions", proposeBody(key, nil, "#1e90ff", eff))
	if code != 201 {
		t.Fatalf("đề xuất màu hợp lệ: %d %v", code, b)
	}
	id := b["id"].(string)
	if code, b := setter.adm(http.MethodPost, "/v1/masterdata/params/versions/"+id+"/submit", map[string]any{}); code != 200 {
		t.Fatalf("gửi duyệt: %d %v", code, b)
	}
	// Chưa duyệt → legend vẫn màu cũ.
	_, cfg = viewer.get("/v1/masterdata/display-config")
	if cfg["stage_colors"].(map[string]any)["chin_ro"].(map[string]any)["value"] != "#D6452F" {
		t.Fatalf("chưa duyệt không được áp dụng: %v", cfg["stage_colors"])
	}
	// R-56: người đặt không tự duyệt được.
	if code, _ := setter.adm(http.MethodPost, "/v1/masterdata/params/versions/"+id+"/approve", map[string]any{}); code != 403 {
		t.Fatalf("người đặt không có vai duyệt: %d", code)
	}
	approve(t, approver, id)
	_, cfg = viewer.get("/v1/masterdata/display-config")
	cur = cfg["stage_colors"].(map[string]any)["chin_ro"].(map[string]any)
	if cur["value"] != "#1e90ff" || cur["version"].(float64) != 2 || cur["default"] != "#D6452F" {
		t.Fatalf("áp dụng khi duyệt: %v", cur)
	}
	// Cách ly tenant: tenant khác vẫn màu mặc định.
	other := f.caller(f.seeded(), "plot_viewer")
	_, ocfg := other.get("/v1/masterdata/display-config")
	if ocfg["stage_colors"].(map[string]any)["chin_ro"].(map[string]any)["value"] != "#D6452F" {
		t.Fatalf("rò tenant: %v", ocfg["stage_colors"])
	}
}

// GIS US-CFG-01 AC-2 / US-CFG-02 AC-3: bộ ngưỡng % lấp đầy kho/Hub (warn < full) phải tăng dần
// và nằm trong 0–100 % — kiểm lúc đề xuất (với mốc còn lại đã duyệt) và lại lúc duyệt.
func TestPercentThresholdsAscending0to100_GIS_US_CFG_01(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded() // warn 80, full 95
	setter := f.caller(tid, port.RoleParamSetter)
	approver := f.caller(tid, port.RoleParamApprover)
	msg := "Ngưỡng % phải tăng dần và nằm trong khoảng 0–100%."
	for _, tc := range []struct {
		key, value string
	}{
		{port.ParamHubCapacityWarn, "101"},
		{port.ParamHubCapacityWarn, "-1"},
		{port.ParamHubCapacityFull, "120"},
	} {
		code, b := setter.adm(http.MethodPost, "/v1/masterdata/params/versions", proposeBody(tc.key, tc.value, nil, "2026-07-01"))
		if code != 422 || fieldReason(b) != msg {
			t.Fatalf("%s=%s phải bị chặn (ngoài 0–100): %d %v", tc.key, tc.value, code, b)
		}
	}
	// Không tăng dần so với mốc đã duyệt: warn 96 ≥ full 95.
	code, b := setter.adm(http.MethodPost, "/v1/masterdata/params/versions", proposeBody(port.ParamHubCapacityWarn, "96", nil, "2026-07-01"))
	if code != 422 || b["code"] != "masterdata.param_percent_thresholds" || b["title"] != msg {
		t.Fatalf("warn ≥ full phải bị chặn: %d %v", code, b)
	}
	// Hợp lệ: warn 70 < full 95 → qua; nhưng nếu trong lúc chờ duyệt full bị hạ xuống 65 thì duyệt warn bị chặn.
	code, b = setter.adm(http.MethodPost, "/v1/masterdata/params/versions", proposeBody(port.ParamHubCapacityWarn, "70", nil, "2026-07-01"))
	if code != 201 {
		t.Fatalf("warn 70: %d %v", code, b)
	}
	warnID := b["id"].(string)
	setter.adm(http.MethodPost, "/v1/masterdata/params/versions/"+warnID+"/submit", map[string]any{})
	// full 75 > warn đã duyệt (80)? Không: 75 < 80 → bị chặn lúc đề xuất.
	if code, b := setter.adm(http.MethodPost, "/v1/masterdata/params/versions", proposeBody(port.ParamHubCapacityFull, "75", nil, "2026-07-01")); code != 422 {
		t.Fatalf("full 75 < warn 80 đã duyệt: %d %v", code, b)
	}
	approve(t, approver, warnID) // warn = 70 từ 01/07
	fullID := propose(t, setter, port.ParamHubCapacityFull, "75", "2026-07-01", nil)
	approve(t, approver, fullID) // full = 75 > 70
	got, err := f.mod.Params().At(setter.ctx(), port.ParamHubCapacityFull, day("2026-08-01"))
	if err != nil || got.Value.String() != "75" {
		t.Fatalf("full sau duyệt: %v %v", got, err)
	}
}

// GIS US-CFG-02 AC-2: "Khôi phục mặc định" / "Xóa" không xóa phiên bản nào (RB-1): tạo phiên bản
// nháp mới mang giá trị mặc định, qua duyệt hai người rồi mới áp dụng; giá trị cũ còn trong sổ.
// Gọi lặp cùng Idempotency-Key trả lại cùng phiên bản. Khóa không có mặc định → 404.
func TestRestoreDefaultIsNewVersion_GIS_US_CFG_02(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	setter := f.caller(tid, port.RoleParamSetter)
	approver := f.caller(tid, port.RoleParamApprover)
	viewer := f.caller(tid, "plot_viewer")
	key := port.ParamGISCapacityColorPrefix + ":full"

	eff := time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339)
	code, b := setter.adm(http.MethodPost, "/v1/masterdata/params/versions", proposeBody(key, nil, "#000000", eff))
	if code != 201 {
		t.Fatalf("đề xuất: %d %v", code, b)
	}
	id := b["id"].(string)
	setter.adm(http.MethodPost, "/v1/masterdata/params/versions/"+id+"/submit", map[string]any{})
	approve(t, approver, id)
	_, cfg := viewer.get("/v1/masterdata/display-config")
	if cfg["capacity_colors"].(map[string]any)["full"].(map[string]any)["value"] != "#000000" {
		t.Fatalf("màu đã đổi: %v", cfg["capacity_colors"])
	}

	idem := "k-" + ids.New().String()
	body := map[string]any{"key": key, "reason": "Khôi phục màu chuẩn theo BRD"}
	code, rv := setter.do(f.admin, http.MethodPost, "/v1/masterdata/params/restore-default", idem, body)
	if code != 201 || rv["text"] != "#B3261E" || rv["status"] != "draft" || rv["version"].(float64) != 3 {
		t.Fatalf("khôi phục mặc định: %d %v", code, rv)
	}
	if code, again := setter.do(f.admin, http.MethodPost, "/v1/masterdata/params/restore-default", idem, body); (code != 200 && code != 201) || again["id"] != rv["id"] {
		t.Fatalf("gọi lặp: %d %v", code, again)
	}
	// Chưa duyệt → vẫn màu đã đổi.
	_, cfg = viewer.get("/v1/masterdata/display-config")
	if cfg["capacity_colors"].(map[string]any)["full"].(map[string]any)["value"] != "#000000" {
		t.Fatalf("khôi phục chỉ áp dụng khi duyệt: %v", cfg["capacity_colors"])
	}
	rid := rv["id"].(string)
	setter.adm(http.MethodPost, "/v1/masterdata/params/versions/"+rid+"/submit", map[string]any{})
	if code, _ := setter.adm(http.MethodPost, "/v1/masterdata/params/versions/"+rid+"/approve", map[string]any{}); code != 403 {
		t.Fatalf("người đặt không duyệt được: %d", code)
	}
	approve(t, approver, rid)
	_, cfg = viewer.get("/v1/masterdata/display-config")
	if cur := cfg["capacity_colors"].(map[string]any)["full"].(map[string]any); cur["value"] != "#B3261E" || cur["version"].(float64) != 3 {
		t.Fatalf("sau duyệt khôi phục: %v", cur)
	}
	// Giá trị cũ còn trong lịch sử phiên bản (kiểm toán).
	code, hist := setter.adm(http.MethodGet, "/v1/masterdata/params/versions?key="+key, nil)
	if code != 200 || len(hist["items"].([]any)) != 3 {
		t.Fatalf("lịch sử phải giữ 3 phiên bản: %d %v", code, hist)
	}
	if code, b := setter.adm(http.MethodPost, "/v1/masterdata/params/restore-default", map[string]any{"key": "gis.khong_co_mac_dinh"}); code != 404 ||
		b["code"] != "masterdata.param_no_default" {
		t.Fatalf("khóa không có mặc định: %d %v", code, b)
	}
	if code, _ := viewer.adm(http.MethodPost, "/v1/masterdata/params/restore-default", body); code != 403 {
		t.Fatalf("không có vai param_setter: %d", code)
	}
}
