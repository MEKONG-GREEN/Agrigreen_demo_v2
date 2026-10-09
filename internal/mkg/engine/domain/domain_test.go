package domain

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine/port"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func eq(t *testing.T, what string, got decimal.Decimal, want string) {
	t.Helper()
	if !got.Equal(d(want)) {
		t.Errorf("%s = %s, muốn %s", what, got, want)
	}
}

func TestQuantized(t *testing.T) {
	for _, c := range []struct{ in, v, u string }{
		{"812.40", "812.40", "0.005"},
		{"0.6526", "0.6526", "0.00005"},
		{"1", "1", "0.5"},
		{"-1.5", "-1.5", "0.05"},
	} {
		m, err := Quantized(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		eq(t, c.in+" value", m.Value, c.v)
		eq(t, c.in+" U", m.U, c.u)
	}
	for _, bad := range []string{"", "abc", "1e3", "1,5"} {
		if _, err := Quantized(bad); !errors.Is(err, ErrNotDecimal) {
			t.Errorf("%q: muốn ErrNotDecimal, nhận %v", bad, err)
		}
	}
}

// Lan truyền theo cận tuyến tính: mọi điểm trong khoảng đầu vào cho kết quả trong khoảng đầu ra.
func TestPropagation(t *testing.T) {
	a := Measured{Value: d("10"), U: d("0.5")}
	b := Measured{Value: d("4"), U: d("0.1")}
	s := a.Add(b)
	eq(t, "a+b", s.Value, "14")
	eq(t, "U(a+b)", s.U, "0.6")
	df := a.Sub(b)
	eq(t, "a-b", df.Value, "6")
	eq(t, "U(a-b)", df.U, "0.6")
	p := a.Mul(b)
	eq(t, "ab", p.Value, "40")
	eq(t, "U(ab)", p.U, "3.05") // 10·0.1 + 4·0.5 + 0.5·0.1
	q, err := a.Div(b)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "a/b", q.Value, "2.5")
	// (10·0.1 + 4·0.5) / (4·3.9) = 3/15.6 = 0.192307692307…, làm tròn lên 12 chữ số.
	eq(t, "U(a/b)", q.U, "0.192307692308")
	// Kiểm cận bằng các góc của khoảng: |f(góc) − f(tâm)| ≤ U.
	for _, x := range []decimal.Decimal{d("9.5"), d("10.5")} {
		for _, y := range []decimal.Decimal{d("3.9"), d("4.1")} {
			if x.Mul(y).Sub(p.Value).Abs().GreaterThan(p.U) {
				t.Errorf("tích góc %s·%s ngoài cận", x, y)
			}
			if x.Div(y).Sub(q.Value).Abs().GreaterThan(q.U) {
				t.Errorf("thương góc %s/%s ngoài cận", x, y)
			}
		}
	}
	if _, err := a.Div(Measured{Value: d("0.1"), U: d("0.2")}); !errors.Is(err, ErrDivUncertain) {
		t.Errorf("mẫu số có thể bằng 0: muốn ErrDivUncertain, nhận %v", err)
	}
	sc := b.Scale(d("-2"))
	eq(t, "scale", sc.Value, "-8")
	eq(t, "U scale", sc.U, "0.2")
	r, ok := a.Relative()
	if !ok {
		t.Fatal("Relative ok=false")
	}
	eq(t, "relative", r, "0.05")
	if _, ok := Exact(decimal.Zero).Relative(); ok {
		t.Error("Relative của 0 phải ok=false")
	}
	eq(t, "widen", a.Widen(d("-1")).U, "1.5")
}

func TestStale(t *testing.T) {
	asOf := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	if Stale(asOf, time.Hour, asOf.Add(time.Hour)) {
		t.Error("đúng mốc as_of + max_age vẫn còn tươi")
	}
	if !Stale(asOf, time.Hour, asOf.Add(time.Hour+time.Nanosecond)) {
		t.Error("quá as_of + max_age phải stale")
	}
	if !Stale(asOf, 0, asOf) {
		t.Error("không có max_age phải stale")
	}
	later := asOf.Add(2 * time.Hour)
	if got := EffectiveAsOf(asOf, &later); !got.Equal(later) {
		t.Errorf("EffectiveAsOf = %v, muốn as_of của feed", got)
	}
	earlier := asOf.Add(-time.Hour)
	if got := EffectiveAsOf(asOf, &earlier); !got.Equal(asOf) {
		t.Errorf("EffectiveAsOf không được lùi: %v", got)
	}
	if got := EffectiveAsOf(asOf, nil); !got.Equal(asOf) {
		t.Errorf("EffectiveAsOf(nil) = %v", got)
	}
}

func TestParseMaxAge(t *testing.T) {
	for in, want := range map[string]time.Duration{"PT15M": 15 * time.Minute, "PT6H": 6 * time.Hour, "P1D": 24 * time.Hour, "P1DT1H": 25 * time.Hour} {
		got, err := ParseMaxAge(in)
		if err != nil || got != want {
			t.Errorf("%s = %v, %v; muốn %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "P", "PT", "P1M", "PT0S", "1h"} {
		if _, err := ParseMaxAge(bad); err == nil {
			t.Errorf("%q: muốn lỗi", bad)
		}
	}
}

// Mỗi feed có bộ ánh xạ có hợp đồng tích hợp; max_age của hợp đồng đọc được; feed MKG không là bên nhận thì không có.
func TestMappersCoverContracts(t *testing.T) {
	for _, f := range Feeds() {
		c, err := integration.Get(f, 1)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if c.Kind != integration.KindFeed || c.Freshness == nil {
			t.Fatalf("%s: không phải feed có freshness", f)
		}
		if _, err := ParseMaxAge(c.Freshness.MaxAge); err != nil {
			t.Errorf("%s: %v", f, err)
		}
		if c.PersonalData == nil || *c.PersonalData {
			t.Errorf("%s: kho quan sát không nhận dữ liệu cá nhân", f)
		}
	}
	if FeedKinds("collection_volume_by_commune") != nil {
		t.Error("collection_volume_by_commune: MKG không là bên nhận")
	}
	if want := []string{port.KindAdminUnit, port.KindCooperative, port.KindHarvestAreaRemaining, port.KindHarvestShare,
		port.KindPlot, port.KindSeasonHarvestWindow, port.KindWaterwayNetwork}; !slices.Equal(Kinds(), want) {
		t.Errorf("Kinds = %v", Kinds())
	}
}

type example struct {
	Items []struct {
		EntityKey string          `json:"entity_key"`
		Op        string          `json:"op"`
		Record    json.RawMessage `json:"record"`
	} `json:"items"`
}

func loadExample(t *testing.T, feed string) example {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "integration", "examples", feed+".v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var e example
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func mapOne(t *testing.T, feed string, i int) []Draft {
	t.Helper()
	it := loadExample(t, feed).Items[i]
	ds, err := Map(feed, it.Op, it.Record)
	if err != nil {
		t.Fatalf("%s[%d]: %v", feed, i, err)
	}
	return ds
}

// Mẫu hợp đồng (contracts/integration/examples) đi qua bộ ánh xạ.
func TestMapExamples(t *testing.T) {
	// harvest_progress_by_commune: dòng xã.
	ds := mapOne(t, "harvest_progress_by_commune", 0)
	if len(ds) != 2 || ds[0].Kind != port.KindHarvestShare || ds[1].Kind != port.KindHarvestAreaRemaining {
		t.Fatalf("harvest: %+v", ds)
	}
	eq(t, "share", ds[0].Value.Value, "0.6526")
	eq(t, "U share", ds[0].Value.U, "0.00005")
	if ds[0].Unit != port.UnitRatio || ds[1].Unit != port.UnitHectare {
		t.Errorf("đơn vị: %s, %s", ds[0].Unit, ds[1].Unit)
	}
	eq(t, "remaining", ds[1].Value.Value, "282.25") // 812.40 − 530.15
	eq(t, "U remaining", ds[1].Value.U, "0.01")     // hai sai số làm tròn 0.005
	if !slices.Equal(ds[1].Basis, []string{BasisQuantization}) {
		t.Errorf("basis xã: %v", ds[1].Basis)
	}
	// Dòng province_withheld: phần dư của xã bị ẩn dưới k.
	ds = mapOne(t, "harvest_progress_by_commune", 1)
	for _, x := range ds {
		if !slices.Contains(x.Basis, BasisKSuppressedResidual) {
			t.Errorf("%s: thiếu %s: %v", x.Kind, BasisKSuppressedResidual, x.Basis)
		}
	}
	// retract → một quan sát rút mỗi kind.
	ds = mapOne(t, "harvest_progress_by_commune", 2)
	if len(ds) != 2 || !ds[0].Retracted || !ds[1].Retracted || ds[0].Value != nil {
		t.Errorf("retract: %+v", ds)
	}

	ds = mapOne(t, "plot_directory", 0)
	eq(t, "area_ha", ds[0].Value.Value, "1.2500")
	eq(t, "U area_ha", ds[0].Value.U, "0.00005")
	if ds[0].Value.Value.Exponent() != -4 {
		t.Errorf("area_ha mất số lẻ của nguồn: %s", ds[0].Value.Value)
	}

	ds = mapOne(t, "cooperative_directory", 0)
	if len(ds) != 1 || ds[0].Value != nil || len(ds[0].Basis) != 0 {
		t.Errorf("cooperative: %+v", ds)
	}
	ds = mapOne(t, "admin_units", 0)
	if !slices.Equal(ds[0].Basis, []string{BasisPendingC9}) {
		t.Errorf("admin_unit kế thừa pending_c9: %v", ds[0].Basis)
	}
	if ds = mapOne(t, "admin_units", 1); len(ds[0].Basis) != 0 {
		t.Errorf("admin_unit không kế thừa: %v", ds[0].Basis)
	}
	if ds = mapOne(t, "season_harvest_dates", 0); !slices.Equal(ds[0].Basis, []string{BasisForecast}) {
		t.Errorf("season open: %v", ds[0].Basis)
	}
	if ds = mapOne(t, "waterway_routes", 0); ds[0].Kind != port.KindWaterwayNetwork {
		t.Errorf("waterway: %+v", ds)
	}
}

func TestMapEdgeCases(t *testing.T) {
	// Đã gặt > đã gieo: kẹp về 0, cận phủ giá trị gốc.
	ds, err := Map("harvest_progress_by_commune", "upsert", json.RawMessage(
		`{"area_level":"commune","area_ha_sown":"10.00","area_ha_harvested":"10.50","harvested_share":"1.0000"}`))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "clamped", ds[1].Value.Value, "0")
	eq(t, "U clamped", ds[1].Value.U, "0.51")
	if !slices.Contains(ds[1].Basis, BasisClamped) {
		t.Errorf("basis: %v", ds[1].Basis)
	}
	// HTX chưa có tọa độ.
	ds, err = Map("cooperative_directory", "upsert", json.RawMessage(`{"cooperative_ref":"x","lon":null,"lat":null}`))
	if err != nil || !slices.Equal(ds[0].Basis, []string{BasisLocationMissing}) {
		t.Errorf("cooperative không tọa độ: %+v %v", ds, err)
	}
	for _, c := range []struct{ feed, op, rec string }{
		{"collection_volume_by_commune", "upsert", `{}`},
		{"plot_directory", "upsert", `null`},
		{"plot_directory", "upsert", ``},
		{"plot_directory", "upsert", `{"area_ha":"abc"}`},
		{"plot_directory", "upsert", `{"area_ha":"-1.0"}`},
		{"plot_directory", "upsert", `[1]`},
		{"plot_directory", "delete", `{}`},
		{"harvest_progress_by_commune", "upsert", `{"area_ha_sown":"1","area_ha_harvested":"x","harvested_share":"0"}`},
		{"waterway_routes", "upsert", `{}`},
	} {
		if _, err := Map(c.feed, c.op, json.RawMessage(c.rec)); err == nil {
			t.Errorf("%s %s %s: muốn lỗi", c.feed, c.op, c.rec)
		}
	}
	rs := Retractions("harvest_progress_by_commune", []string{BasisGrantRevoked})
	if len(rs) != 2 || !rs[0].Retracted || !slices.Equal(rs[1].Basis, []string{BasisGrantRevoked}) {
		t.Errorf("Retractions: %+v", rs)
	}
}
