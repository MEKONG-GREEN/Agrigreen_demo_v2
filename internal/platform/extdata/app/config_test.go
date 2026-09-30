package app_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/port"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// ADR-0007: danh mục nguồn theo cấu hình; mặc định rỗng (không nguồn nào).
func TestLoadConfigDefaultsAndValidation_ADR0007(t *testing.T) {
	got, err := app.LoadConfig(env(map[string]string{}))
	if err != nil || len(got) != 0 {
		t.Fatalf("không cấu hình → không nguồn: %v %v", got, err)
	}
	got, err = app.LoadConfig(env(map[string]string{
		"AG_EXTDATA_SOURCES":     "wx,sat",
		"AG_EXTDATA_WX_URL":      "fake",
		"AG_EXTDATA_WX_KIND":     "weather",
		"AG_EXTDATA_SAT_URL":     "https://tiles.agrigreen.vn/sat/{z}/{x}/{y}.png",
		"AG_EXTDATA_SAT_KIND":    "tile",
		"AG_EXTDATA_SAT_TTL":     "12h",
		"AG_RESIDENCY_ALLOWLIST": "agrigreen.vn",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Info.Fake || got[0].Info.TTL != 30*time.Minute || got[0].Info.CellDeg.String() != "0.1" {
		t.Fatalf("wx = %+v", got)
	}
	if s := got[1].Info; s.Kind != port.KindTile || !s.InTerritory || s.MaxZoom != 16 || s.TTL != 12*time.Hour || got[1].URL == "" {
		t.Fatalf("sat = %+v", s)
	}
	for name, m := range map[string]map[string]string{
		"loại lạ":           {"AG_EXTDATA_SOURCES": "wx", "AG_EXTDATA_WX_URL": "fake", "AG_EXTDATA_WX_KIND": "news"},
		"thiếu URL":         {"AG_EXTDATA_SOURCES": "wx", "AG_EXTDATA_WX_KIND": "weather"},
		"ô quá mịn":         {"AG_EXTDATA_SOURCES": "wx", "AG_EXTDATA_WX_URL": "fake", "AG_EXTDATA_WX_KIND": "weather", "AG_EXTDATA_WX_CELL_DEG": "0.001"},
		"mã nguồn sai":      {"AG_EXTDATA_SOURCES": "Wx-1"},
		"ADR sai dạng":      {"AG_EXTDATA_SOURCES": "wx", "AG_EXTDATA_WX_URL": "fake", "AG_EXTDATA_WX_KIND": "weather", "AG_EXTDATA_WX_ADR": "adr7"},
		"scheme lạ":         {"AG_EXTDATA_SOURCES": "wx", "AG_EXTDATA_WX_URL": "ftp://x.vn/a", "AG_EXTDATA_WX_KIND": "weather"},
		"khai hai lần":      {"AG_EXTDATA_SOURCES": "wx,wx", "AG_EXTDATA_WX_URL": "fake", "AG_EXTDATA_WX_KIND": "weather"},
		"TTL quá ngắn":      {"AG_EXTDATA_SOURCES": "wx", "AG_EXTDATA_WX_URL": "fake", "AG_EXTDATA_WX_KIND": "weather", "AG_EXTDATA_WX_TTL": "5s"},
		"zoom ngoài khoảng": {"AG_EXTDATA_SOURCES": "tl", "AG_EXTDATA_TL_URL": "fake", "AG_EXTDATA_TL_KIND": "tile", "AG_EXTDATA_TL_MAX_ZOOM": "22"},
	} {
		if _, err := app.LoadConfig(env(m)); !errors.Is(err, app.ErrConfig) {
			t.Errorf("%s: muốn ErrConfig, được %v", name, err)
		}
	}
}

// ADR-0007 + RB-7 ở production: không nhà cung cấp giả; nguồn ngoài lãnh thổ cần
// AG_NONPII_ALLOWLIST VÀ ADR riêng; nguồn trong lãnh thổ không cần ADR.
func TestLoadConfigProductionResidency_ADR0007(t *testing.T) {
	base := func() map[string]string {
		return map[string]string{
			"AG_ENV": "production", "AG_RESIDENCY_ALLOWLIST": "agrigreen.vn,.gov.vn",
			"AG_EXTDATA_SOURCES": "wx", "AG_EXTDATA_WX_KIND": "weather",
		}
	}
	m := base()
	m["AG_EXTDATA_WX_URL"] = "fake"
	if _, err := app.LoadConfig(env(m)); err == nil || !strings.Contains(err.Error(), "giả") {
		t.Fatalf("fake ở production phải bị chặn: %v", err)
	}
	m = base()
	m["AG_EXTDATA_WX_URL"] = "https://api.nchmf.gov.vn/forecast?lat={lat}&lon={lon}"
	got, err := app.LoadConfig(env(m))
	if err != nil || !got[0].Info.InTerritory {
		t.Fatalf("nguồn trong lãnh thổ không cần ADR: %v %+v", err, got)
	}
	m = base()
	m["AG_EXTDATA_WX_URL"] = "https://api.open-meteo.com/v1/forecast?latitude={lat}&longitude={lon}"
	if _, err := app.LoadConfig(env(m)); err == nil || !strings.Contains(err.Error(), "AG_NONPII_ALLOWLIST") {
		t.Fatalf("nguồn ngoài chưa vào AG_NONPII_ALLOWLIST phải bị chặn: %v", err)
	}
	m["AG_NONPII_ALLOWLIST"] = "open-meteo.com"
	if _, err := app.LoadConfig(env(m)); err == nil || !strings.Contains(err.Error(), "ADR riêng") {
		t.Fatalf("nguồn ngoài thiếu ADR riêng phải bị chặn: %v", err)
	}
	m["AG_EXTDATA_WX_ADR"] = "ADR-0021"
	got, err = app.LoadConfig(env(m))
	if err != nil || got[0].Info.InTerritory || got[0].Info.ADR != "ADR-0021" {
		t.Fatalf("nguồn ngoài có ADR + NONPII phải qua: %v %+v", err, got)
	}
	m["AG_EXTDATA_WX_URL"] = "http://api.open-meteo.com/v1/forecast"
	if _, err := app.LoadConfig(env(m)); err == nil {
		t.Fatal("production phải dùng https")
	}
}
