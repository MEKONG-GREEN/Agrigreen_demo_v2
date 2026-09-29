package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
)

// CGH FN-01 AC-03: đúng 49 tham số BRD (23 thị trường / 24 giả định / 2 khác), không trùng, không thiếu STT; #50–#57 nhóm mở rộng.
func TestCatalogIntegrityFN01AC03(t *testing.T) {
	counts := map[Classification]int{}
	seen := map[int]bool{}
	codes := map[string]bool{}
	for _, p := range Catalog {
		if seen[p.Number] || codes[p.Code] {
			t.Fatalf("trùng #%d %s", p.Number, p.Code)
		}
		seen[p.Number], codes[p.Code] = true, true
		if p.Number <= BRDParamCount {
			counts[p.Classification]++
		}
		if p.Base != "" {
			if _, ok := CatalogDecimal(p.Base); !ok {
				t.Fatalf("#%d base không phải decimal", p.Number)
			}
		}
	}
	for i := 1; i <= 57; i++ {
		if !seen[i] {
			t.Fatalf("thiếu STT %d", i)
		}
	}
	if counts[ClassMarket] != 23 || counts[ClassAssumption] != 24 || counts[ClassOther] != 2 {
		t.Fatalf("phân loại %v", counts)
	}
}

// CGH FN-01 BR-04: tham số đang dùng mà thiếu phiên bản phê duyệt → unapproved; #48/#49 chưa chốt không tính.
func TestResolveParamsApproval(t *testing.T) {
	none := ResolveParams(map[string]ParamLookup{})
	if none.AllApproved || len(none.Unapproved) == 0 {
		t.Fatal("không có phê duyệt nào → phải thiếu")
	}
	for _, u := range none.Unapproved {
		if u.Code == "min_roi_pct" || u.Code == "max_payback_years" || u.Code == "htx_directory" {
			t.Fatalf("%s không được tính là thiếu phê duyệt", u.Code)
		}
	}
	all := map[string]ParamLookup{}
	for _, d := range Catalog {
		if d.ManagedElsewhere || d.Base == "" {
			continue
		}
		all[ParamKey(d.Code, "")] = ParamLookup{Found: true, Value: decimal.RequireFromString(d.Base), Ver: 1}
		for _, dim := range d.Dims {
			all[ParamKey(d.Code, dim.Key)] = ParamLookup{Found: true, Value: decimal.RequireFromString(dim.Base), Ver: 1}
		}
	}
	ok := ResolveParams(all)
	if !ok.AllApproved || len(ok.Unapproved) != 0 {
		t.Fatalf("đủ phê duyệt: %v", ok.Unapproved)
	}
	if ok.SetVersion == none.SetVersion {
		t.Fatal("set_version phải đổi theo phiên bản tham số")
	}
	delete(all, ParamKey("distance_correction", "waterway"))
	partial := ResolveParams(all)
	if partial.AllApproved || len(partial.Unapproved) != 1 || partial.Unapproved[0].Code != "distance_correction" {
		t.Fatalf("thiếu một chiều của #10: %v", partial.Unapproved)
	}
	if f := partial.CorrectionFactor(port.ModeWaterway); !f.Equal(decimal.RequireFromString("1.45")) {
		t.Fatalf("hệ số thủy = %s", f)
	}
}

// Prototype attachHub: sửa một ô không reset các ô còn lại; kiểm miền giá trị.
func TestHubConfigApplyKeepsOtherFields(t *testing.T) {
	barge := decimal.NewFromInt(1000)
	cur := ScenarioHub{ServiceRadiusKm: decimal.NewFromInt(40), DesignCapacityTons: decimal.NewFromInt(60000), BargePayloadTons: &barge,
		ModeFieldHub: "auto", ModeHubPlant: "waterway", LandMode: "thue"}
	nb := decimal.NewFromInt(2000)
	got, err := HubConfig{BargePayloadTons: &nb}.Apply(cur)
	if err != nil || !got.BargePayloadTons.Equal(nb) || !got.ServiceRadiusKm.Equal(cur.ServiceRadiusKm) || got.LandMode != "thue" || got.ModeHubPlant != "waterway" {
		t.Fatalf("%+v %v", got, err)
	}
	bad := "boat"
	if _, err := (HubConfig{ModeFieldHub: &bad}).Apply(cur); !errors.Is(err, ErrMode) {
		t.Fatal(err)
	}
	neg := decimal.NewFromInt(-1)
	if _, err := (HubConfig{DesignCapacityTons: &neg}).Apply(cur); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

// Mục 16: băm tính trên JSON chuẩn; đọc lại từ jsonb (khóa bị sắp lại) vẫn ra đúng byte và băm; sửa một byte → phát hiện.
func TestSnapshotCanonicalHashSurvivesJSONBRoundTrip(t *testing.T) {
	s := Snapshot{SchemaVersion: 1, SnapshotID: uuid.NewString(), HarvestObservations: map[string][]SnapObservation{"HT": {{DayOfYear: 201, Tons: "5200"}}, "DX": {}},
		Params: ResolveParams(nil), Options: SnapOptions{Mode: "evaluate", SensitivityCodes: []string{}}}
	raw, sha, err := s.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any // mô phỏng jsonb: thứ tự khóa khác, khoảng trắng khác
	_ = json.Unmarshal(raw, &generic)
	reordered, _ := json.MarshalIndent(generic, "", "  ")
	_, back, err := DecodeSnapshot(reordered, sha)
	if err != nil || string(back) != string(raw) {
		t.Fatalf("đọc lại: %v", err)
	}
	tampered := strings.Replace(string(reordered), `"5200"`, `"5201"`, 1)
	if _, _, err := DecodeSnapshot([]byte(tampered), sha); !errors.Is(err, ErrSnapshotTampered) {
		t.Fatalf("phải phát hiện sửa ảnh chụp: %v", err)
	}
}

// FN-05: chỉ HTX trong bán kính (chim bay) và có toạ độ; mọi chặng cả bộ lẫn thủy; không trùng HTX khi thuộc nhiều Hub.
func TestPlanRoutesRadiusAndLegs(t *testing.T) {
	h1, h2 := uuid.New(), uuid.New()
	hubs := []HubSpec{
		{HubRef: h1, Location: port.LatLng{Lat: 10.38, Lng: 105.435}, RadiusKm: decimal.NewFromInt(30)},
		{HubRef: h2, Location: port.LatLng{Lat: 10.46, Lng: 105.64}, RadiusKm: decimal.NewFromInt(30)},
	}
	in, far, nogps := uuid.New(), uuid.New(), uuid.New()
	coops := []port.CooperativeSupply{
		{CooperativeRef: in, Location: &port.LatLng{Lat: 10.42, Lng: 105.53}},
		{CooperativeRef: far, Location: &port.LatLng{Lat: 12, Lng: 108}},
		{CooperativeRef: nogps},
	}
	p := PlanRoutes(hubs, port.LatLng{Lat: 10.9, Lng: 106.7}, coops)
	if len(p.Cooperatives) != 1 || p.Cooperatives[0].CooperativeRef != in {
		t.Fatalf("HTX trong bán kính: %v", p.Cooperatives)
	}
	if len(p.Radius) != 2 { // thuộc cả hai bán kính → engine chọn Hub gần nhất (FN-05 BR-02)
		t.Fatalf("radius %v", p.Radius)
	}
	// 2 Hub→NM + 2 HTX→Hub + 1 HTX→NM, mỗi chặng 2 phương thức.
	if len(p.Needs) != 10 {
		t.Fatalf("số chặng %d", len(p.Needs))
	}
	r := ToRoute(p.Needs[0], port.RouteResult{}, decimal.RequireFromString("1.3"))
	if r.Source != port.SourceHaversine || r.StraightKm == nil || r.WaterwayAccess != nil {
		t.Fatalf("dự phòng Haversine: %+v", r)
	}
	w := ToRoute(p.Needs[1], port.RouteResult{Found: true, DistanceKm: decimal.RequireFromString("208.0764"), Source: port.SourceRouted}, decimal.NewFromInt(1))
	if w.DistanceKm != "208.076" || w.WaterwayAccess == nil || !*w.WaterwayAccess {
		t.Fatalf("tuyến thủy: %+v", w)
	}
}

// CGH FN-14 BR-02/BR-03: khác chế độ chiết khấu → chặn; xếp TCO/Ton tăng dần, null cuối, baseline ghim cuối.
func TestCompareRules(t *testing.T) {
	d := func(v int64) *decimal.Decimal { x := decimal.NewFromInt(v); return &x }
	a := ResultSummary{ScenarioCode: "KB-1", LifecycleYears: 10, TCOPerTon: d(300), BaselineAvailable: true, BaselineDeliveredTons: decimal.NewFromInt(100),
		PlantAnnualDemandTons: decimal.NewFromInt(400), ParameterSetVersion: "a"}
	b := ResultSummary{ScenarioCode: "KB-2", LifecycleYears: 10, TCOPerTon: d(200), ParameterSetVersion: "b"}
	n := ResultSummary{ScenarioCode: "KB-3", LifecycleYears: 10, ParameterSetVersion: "a"}
	c := Compare([]ResultSummary{a, b, n}, nil, 3, true)
	got := []string{}
	for _, r := range c.Rows {
		got = append(got, r.ScenarioCode)
	}
	if strings.Join(got, ",") != "KB-2,KB-1,KB-3,BASELINE" {
		t.Fatalf("thứ tự %v", got)
	}
	if !c.Rows[3].PlantDemandCoveragePct.Equal(decimal.NewFromInt(25)) {
		t.Fatalf("độ phủ baseline %s", c.Rows[3].PlantDemandCoveragePct)
	}
	if len(c.Warnings) != 2 { // khác bộ tham số + cột baseline
		t.Fatalf("cảnh báo %v", c.Warnings)
	}
	b.Discounted = true
	if blocked := Compare([]ResultSummary{a, b}, nil, 2, true); blocked.Blocked == nil || !strings.Contains(*blocked.Blocked, "chiết khấu") {
		t.Fatal("khác chế độ chiết khấu phải bị chặn")
	}
}
