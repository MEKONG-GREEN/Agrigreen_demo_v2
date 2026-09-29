package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func tenantCtx(tid uuid.UUID) context.Context {
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: tid})
	return tenant.WithID(ctx, tid)
}

// Migration 000027: tham số G2 được nạp bằng cơ chế tham số có phiên bản với đúng
// khóa module khác đọc qua masterdata/port.Params; danh mục vessel_class có ngưỡng tuyến.
func TestG2ParamsSeeded(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	ctx := tenantCtx(tid)
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	want := map[string]string{
		port.ParamGPSMatchTolerance:                                     "50",
		port.ParamSowingWindowBefore:                                    "15",
		port.ParamSowingWindowAfter:                                     "30",
		port.ParamHubCapacityWarn:                                       "80",
		port.ParamHubCapacityFull:                                       "95",
		port.ParamPlotLayerMinZoom:                                      "13",
		port.ParamCGHAreaFeedStaleHours:                                 "48",
		port.ParamWaterwaySnapTolerance:                                 "25",
		port.ParamWaterwayAutoJoin:                                      "150",
		port.ParamWaterwayGapReport:                                     "1000",
		port.ParamHaversineFactor:                                       "1.3",
		port.ParamHaversineFactor + ":road":                             "1.3",
		port.ParamHaversineFactor + ":waterway":                         "1.45",
		port.ParamActivationGrantLifespan:                               "72",
		port.ParamYieldAnomalyMinPrefix:                                 "1",
		port.ParamYieldAnomalyMaxPrefix:                                 "12",
		port.YieldAnomalyKey(port.ParamYieldAnomalyMaxPrefix, "ST25"):   "8.75",
		port.YieldAnomalyKey(port.ParamYieldAnomalyMinPrefix, "ST25"):   "1.6",
		port.YieldAnomalyKey(port.ParamYieldAnomalyMaxPrefix, "OM5451"): "10.625",
	}
	for key, v := range want {
		got, err := f.mod.Params().At(ctx, key, at)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if got.Value.String() != v || got.Version != 1 {
			t.Fatalf("%s = %s (v%d), muốn %s", key, got.Value, got.Version, v)
		}
	}
	vessels, err := f.mod.Catalogs().List(ctx, port.CatalogVesselClass)
	if err != nil {
		t.Fatal(err)
	}
	byCode := map[string]port.CatalogItem{}
	for _, it := range vessels {
		byCode[it.Code] = it
	}
	for code, w := range map[string][3]string{"ghe_100": {"8.25", "1.8", "4"}, "sa_lan_1000": {"15", "2.9", "6.5"}, "sa_lan_2000": {"18", "3.6", "7.5"}} {
		it, ok := byCode[code]
		if !ok || it.Attrs["min_width_m"] != w[0] || it.Attrs["min_depth_m"] != w[1] || it.Attrs["min_clearance_m"] != w[2] {
			t.Fatalf("vessel_class %s: %+v", code, it)
		}
	}
	// Chạy lại không thêm gì (tenant cũ chỉ nhận khóa mới).
	res, err := f.mod.Seed(context.Background(), tid)
	if err != nil || res.Params != 0 || res.Items != 0 {
		t.Fatalf("seed lặp: %+v %v", res, err)
	}
}

func (c caller) upload(path, idem string, body []byte) (int, map[string]any) {
	c.f.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, c.f.admin.URL+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", tabular.ContentTypeXLSX)
	req.Header.Set("Idempotency-Key", idem)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, m
}

// HTX US-CAT-02: nhập danh mục hàng loạt từ Excel — dòng hợp lệ được tạo trong một
// lần nhập, dòng lỗi bị loại kèm lý do, kết quả xem lại được; gọi lặp cùng khóa phát lại.
func TestCatalogImportPerRowErrors(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, "Danh mục", []string{"Danh mục", "Mã", "Tên", "growth_days", "expected_yield_t_per_ha"}, [][]string{
		{"rice_variety", "OM9999", "OM9999 thử", "96", "6.5"}, // 2: tạo
		{"rice_variety", "OM5451", "OM5451", "95", "6.75"},    // 3: đã có cùng nội dung → skipped
		{"rice_variety", "ST25", "ST25 khác", "105", "5.5"},   // 4: đã có khác nội dung → rejected
		{"rice_variety", "BAD", "Thiếu năng suất", "90", ""},  // 5: thiếu thuộc tính bắt buộc
		{"khong_co", "X1", "Danh mục lạ", "", ""},             // 6: danh mục không tồn tại
		{"rice_variety", "OM9999", "OM9999 lặp", "96", "6.5"}, // 7: trùng dòng 2
		{"season", "td", "Thu Đông", "", ""},                  // 8: mã vụ chuẩn hóa HOA → TD đã có, khác tên → rejected
	}); err != nil {
		t.Fatal(err)
	}
	key := "k-" + ids.New().String()
	code, body := admin.upload("/v1/masterdata/catalog-imports", key, buf.Bytes())
	if code != http.StatusOK {
		t.Fatalf("nhập: %d %v", code, body)
	}
	rows := body["rows"].([]any)
	status := map[int]string{}
	for _, r := range rows {
		m := r.(map[string]any)
		status[int(m["row"].(float64))] = m["status"].(string)
		if m["status"] == "rejected" && m["reason"] == "" {
			t.Fatalf("dòng lỗi phải có lý do: %v", m)
		}
	}
	for row, want := range map[int]string{2: "created", 3: "skipped", 4: "rejected", 5: "rejected", 6: "rejected", 7: "rejected", 8: "rejected"} {
		if status[row] != want {
			t.Fatalf("dòng %d: %q, muốn %q (%v)", row, status[row], want, rows)
		}
	}
	if body["total"].(float64) != 7 || body["rejected"].(float64) != 5 {
		t.Fatalf("tổng: %v", body)
	}
	if it, err := f.mod.Catalogs().Get(tenantCtx(tid), port.CatalogRiceVariety, "OM9999"); err != nil || it.Attrs["growth_days"] != "96" {
		t.Fatalf("mục vừa nhập: %+v %v", it, err)
	}
	// Gọi lặp cùng Idempotency-Key → phát lại, không tạo thêm.
	if c2, b2 := admin.upload("/v1/masterdata/catalog-imports", key, buf.Bytes()); c2 != http.StatusOK || b2["import_id"] != body["import_id"] {
		t.Fatalf("phát lại: %d %v", c2, b2)
	}
	// Xem lại kết quả không cần tải tệp riêng (AC-2).
	if c3, b3 := admin.adm(http.MethodGet, "/v1/masterdata/catalog-imports/"+body["import_id"].(string), nil); c3 != http.StatusOK || b3["created"] != body["created"] {
		t.Fatalf("xem lại: %d %v", c3, b3)
	}
	// Nhập lại cùng tệp bằng khóa mới: mục đã tạo → skipped, không trùng.
	_, again := admin.upload("/v1/masterdata/catalog-imports", "k-"+ids.New().String(), buf.Bytes())
	for _, r := range again["rows"].([]any) {
		if m := r.(map[string]any); m["row"].(float64) == 2 && m["status"] != "skipped" {
			t.Fatalf("nhập lại: %v", m)
		}
	}
	// Tenant khác không thấy lần nhập.
	other := f.caller(ids.New(), port.RoleMasterdataAdmin)
	if c4, _ := other.adm(http.MethodGet, "/v1/masterdata/catalog-imports/"+body["import_id"].(string), nil); c4 != http.StatusNotFound {
		t.Fatalf("tenant khác: %d", c4)
	}
	// Tệp sai mẫu → từ chối cả tệp.
	if c5, b5 := admin.upload("/v1/masterdata/catalog-imports", "k-"+ids.New().String(), []byte("foo,bar\n1,2\n")); c5 != http.StatusUnprocessableEntity || b5["code"] != "masterdata.catalog_import_invalid" {
		t.Fatalf("sai mẫu: %d %v", c5, b5)
	}
}
