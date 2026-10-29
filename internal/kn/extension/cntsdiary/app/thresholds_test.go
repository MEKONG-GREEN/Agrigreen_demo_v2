package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/domain"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// seedCatalogs trả danh mục chỉ tiêu theo dõi THẬT của module (masterdata_catalogs.json) cộng các mục thêm.
type seedCatalogs struct {
	knport.CNTSCatalogs
	items []mdport.CatalogItem
}

func (f seedCatalogs) ListAt(context.Context, string, time.Time) ([]mdport.CatalogItem, error) {
	return f.items, nil
}

func realSeedIndicators(t *testing.T, extra ...mdport.CatalogItem) []mdport.CatalogItem {
	t.Helper()
	raw, err := os.ReadFile("../../masterdata_catalogs.json")
	if err != nil {
		t.Fatal(err)
	}
	var all []mdport.CatalogItem
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	var out []mdport.CatalogItem
	for _, it := range all {
		if it.Catalog == knport.CatalogCNTSIndicator {
			it.Active = true
			out = append(out, it)
		}
	}
	return append(out, extra...)
}

func checkCodes(cs []domain.Check) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Code)
	}
	return out
}

var nhietDoAo = mdport.CatalogItem{Catalog: knport.CatalogCNTSIndicator, Code: "NHIET_DO_AO", Name: "Nhiệt độ nước ao", Active: true,
	Attrs: map[string]string{"diary_metric": "temp_c", "threshold_min": "25", "threshold_max": "32", "value_type": "decimal", "unit": "°C"}}

// Codex #170 [P2]: seed thật có NHIET_DO → temp_c CHƯA đặt ngưỡng; Admin thêm NHIET_DO_AO → temp_c 25–32. Chỉ tiêu chưa ngưỡng
// (mã nhỏ hơn) không được che ngưỡng của chỉ tiêu có ngưỡng: BM-19 34,5 °C → temp_c_out_of_range và NT-12 bắt buộc biện pháp.
func TestThresholds_RealSeedThresholdlessDoesNotShadow(t *testing.T) {
	items := realSeedIndicators(t, nhietDoAo)
	var seeded bool
	for _, it := range items {
		if it.Code == "NHIET_DO" && it.Attrs[knport.AttrDiaryMetric] == "temp_c" && it.Attrs[knport.AttrThresholdMin] == "" &&
			it.Attrs[knport.AttrThresholdMax] == "" {
			seeded = true
		}
	}
	if !seeded {
		t.Fatal("tiền đề: seed thật phải có NHIET_DO → temp_c chưa đặt ngưỡng (đổi seed thì cập nhật test)")
	}
	ctx := context.Background()
	s := &Service{Clock: clock.System{}, Catalogs: seedCatalogs{items: items}, Params: &fakeParams{}}
	cat, err := s.indicatorRanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	th, err := s.thresholds(ctx, "THUY_SAN", cat)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := th.Others["temp_c"]
	if !ok || r.Min == nil || r.Max == nil || r.Min.String() != "25" || r.Max.String() != "32" {
		t.Fatalf("temp_c phải lấy 25–32 của NHIET_DO_AO, có %+v (cat=%+v)", th.Others, cat["temp_c"])
	}
	if th.PHMin.String() != "7.5" || th.PHMax.String() != "8.5" || th.DOMin.String() != "4" {
		t.Fatalf("pH/oxy seed: %v %v %v", th.PHMin, th.PHMax, th.DOMin)
	}
	in := domain.EvalInput{Form: domain.FormBM19, Thresholds: th,
		Entry: domain.Entry{Kind: domain.KindEnv, Metrics: map[string]string{"ph": "7.8", "do_mgl": "5", "temp_c": "34.5"}}}
	got := checkCodes(domain.Evaluate(in))
	if !slices.Contains(got, "temp_c_out_of_range") || !slices.Contains(got, "action_note_missing") {
		t.Fatalf("34,5 °C phải cảnh báo temp_c_out_of_range và bắt NT-12: %v", got)
	}
	in.Entry.Metrics["temp_c"] = "30"
	if got := checkCodes(domain.Evaluate(in)); slices.Contains(got, "temp_c_out_of_range") || slices.Contains(got, "action_note_missing") {
		t.Fatalf("30 °C trong ngưỡng: %v", got)
	}

	// Tham số đã duyệt của chỉ tiêu chưa ngưỡng vẫn thắng ngưỡng mặc định của danh mục (tham số > danh mục), từng biên riêng.
	s.Params = &fakeParams{vals: map[string]string{knport.IndicatorKey(knport.ParamCNTSIndicatorMaxPrefix, "NHIET_DO", "THUY_SAN"): "35"}}
	if th, err = s.thresholds(ctx, "THUY_SAN", cat); err != nil {
		t.Fatal(err)
	}
	if r := th.Others["temp_c"]; r.Min == nil || r.Min.String() != "25" || r.Max == nil || r.Max.String() != "35" {
		t.Fatalf("max theo tham số NHIET_DO 35, min theo danh mục NHIET_DO_AO 25: %+v", r)
	}

	// Chỉ seed (không NHIET_DO_AO): temp_c không có ngưỡng như trước.
	s = &Service{Clock: clock.System{}, Catalogs: seedCatalogs{items: realSeedIndicators(t)}}
	if cat, err = s.indicatorRanges(ctx); err != nil {
		t.Fatal(err)
	}
	if th, err = s.thresholds(ctx, "THUY_SAN", cat); err != nil {
		t.Fatal(err)
	}
	if _, ok := th.Others["temp_c"]; ok {
		t.Fatalf("seed chưa đặt ngưỡng nhiệt độ: %+v", th.Others)
	}
}

// Codex #170 [P2]: lỗi tra tham số không được âm thầm rơi về mặc định — thresholds/loadEnv trả 503.
func TestThresholds_ParamErrorIsDependency(t *testing.T) {
	ctx := tenant.WithID(context.Background(), uuid.New())
	s := &Service{Clock: clock.System{}, Catalogs: seedCatalogs{items: realSeedIndicators(t, nhietDoAo)},
		Params: &fakeParams{err: errors.New("pool cạn")}}
	cat, err := s.indicatorRanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.thresholds(ctx, "THUY_SAN", cat); !isDependency(err) {
		t.Fatalf("thresholds: lỗi tham số phải 503: %v", err)
	}
	if _, err := s.loadEnv(ctx, "THUY_SAN", "30001"); !isDependency(err) {
		t.Fatalf("loadEnv: lỗi tham số phải 503: %v", err)
	}
	// Chưa có phiên bản đã duyệt → mặc định 200 m / 50 m.
	s.Params = &fakeParams{}
	env, err := s.loadEnv(ctx, "THUY_SAN", "30001")
	if err != nil || env.maxDist.String() != "200" || env.maxAcc.String() != "50" || env.modelType != "THUY_SAN" {
		t.Fatalf("mặc định: %+v %v", env, err)
	}
	if env.check("THUY_SAN", "30001") != nil || !errors.Is(env.check("GA_TP", "30001"), errEnvStale) ||
		!errors.Is(env.check("THUY_SAN", "30002"), errEnvStale) {
		t.Fatal("check: loại mô hình / xã đổi phải errEnvStale")
	}
}
