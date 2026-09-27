package domain

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/seeddata"
)

func dp(t time.Time) *time.Time { return &t }

func TestDayOfUsesVietnamTime(t *testing.T) {
	// 17:30Z ngày 30/06 là 00:30 ngày 01/07 ở Việt Nam — đã sang tỉnh mới.
	got := DayOf(time.Date(2025, 6, 30, 17, 30, 0, 0, time.UTC))
	if !got.Equal(ReorgDate) {
		t.Fatalf("DayOf = %v, muốn %v", got, ReorgDate)
	}
	if s := DayStart(ReorgDate); !s.Equal(time.Date(2025, 6, 30, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("DayStart = %v", s)
	}
}

func TestUnitValidate(t *testing.T) {
	ok := Unit{Code: "91-X-RACH-GIA", Name: "Rạch Giá", Level: port.LevelCommune, ParentCode: "91", ValidFrom: ReorgDate}
	if err := ok.Validate(); err != nil {
		t.Fatalf("hợp lệ mà báo lỗi: %v", err)
	}
	cases := map[string]struct {
		u     Unit
		field string
		err   error
	}{
		"mã thường": {Unit{Code: "ag", Name: "x", Level: port.LevelProvince, ValidFrom: ReorgDate}, "code", ErrUnitCode},
		"tỉnh có cha": {Unit{Code: "91", Name: "x", Level: port.LevelProvince, ParentCode: "00", ValidFrom: ReorgDate},
			"parent_code", ErrUnitParent},
		"xã thiếu cha": {Unit{Code: "91-X", Name: "x", Level: port.LevelCommune, ValidFrom: ReorgDate}, "parent_code", ErrUnitParent},
		"cấp lạ":       {Unit{Code: "91", Name: "x", Level: "ward", ValidFrom: ReorgDate}, "level", ErrUnitLevel},
		"to trước from": {Unit{Code: "91", Name: "x", Level: port.LevelProvince, ValidFrom: ReorgDate, ValidTo: dp(Day(2025, 1, 1))},
			"valid_to", ErrUnitValidity},
		// Sau sắp xếp 2025 chỉ còn hai cấp: huyện phải chấm dứt trước 01/07/2025.
		"huyện còn mở": {Unit{Code: "89-H-A", Name: "x", Level: port.LevelDistrict, ParentCode: "89", ValidFrom: Day(2004, 1, 1)},
			"valid_to", ErrUnitDistrict},
		"huyện sau 2025": {Unit{Code: "89-H-A", Name: "x", Level: port.LevelDistrict, ParentCode: "89", ValidFrom: Day(2004, 1, 1),
			ValidTo: dp(Day(2026, 1, 1))}, "valid_to", ErrUnitDistrict},
		"kế thừa khi còn mở": {Unit{Code: "91", Name: "x", Level: port.LevelProvince, ValidFrom: ReorgDate, Successors: []string{"92"}},
			"successors", ErrUnitSuccessors},
	}
	for name, c := range cases {
		err := c.u.Validate()
		var fe FieldError
		if !errors.As(err, &fe) || fe.Field != c.field || !errors.Is(err, c.err) {
			t.Errorf("%s: muốn %s/%v, nhận %v", name, c.field, c.err, err)
		}
	}
}

func TestPlanImport(t *testing.T) {
	open := Unit{Code: "89", Name: "An Giang", Level: port.LevelProvince, ValidFrom: Day(2004, 1, 1)}
	closed := open
	closed.ValidTo, closed.Successors = dp(ReorgDate), []string{"91"}

	if a, err := PlanImport(nil, open); a != ActionCreate || err != nil {
		t.Fatalf("chưa có → tạo: %v %v", a, err)
	}
	if a, err := PlanImport(&open, open); a != ActionUnchanged || err != nil {
		t.Fatalf("nhập lặp → không đổi: %v %v", a, err)
	}
	if a, err := PlanImport(&open, closed); a != ActionClose || err != nil {
		t.Fatalf("đóng hiệu lực: %v %v", a, err)
	}
	if a, err := PlanImport(&closed, closed); a != ActionUnchanged || err != nil {
		t.Fatalf("đã đóng, nhập lặp: %v %v", a, err)
	}
	renamed := open
	renamed.Name = "An Giang mới"
	if _, err := PlanImport(&open, renamed); !errors.Is(err, ErrUnitConflict) {
		t.Fatalf("đổi tên phiên bản cũ phải xung đột: %v", err)
	}
	other := closed
	other.Successors = []string{"92"}
	if _, err := PlanImport(&closed, other); !errors.Is(err, ErrUnitConflict) {
		t.Fatalf("đổi kế thừa của phiên bản đã đóng phải xung đột: %v", err)
	}
	if _, err := PlanImport(&closed, open); !errors.Is(err, ErrUnitConflict) {
		t.Fatalf("mở lại phiên bản đã đóng phải xung đột: %v", err)
	}
}

// fakeGazetteer: tra phiên bản trong bộ nhớ cho ResolveCurrent.
type fakeGazetteer []Unit

func (g fakeGazetteer) at(code string, d time.Time) (*Unit, error) {
	for _, u := range g {
		if u.Code == code && u.ValidAt(d) {
			u := u
			return &u, nil
		}
	}
	return nil, nil
}

func (g fakeGazetteer) latest(code string, d time.Time) (*Unit, error) {
	var best *Unit
	for _, u := range g {
		if u.Code == code && !u.ValidFrom.After(d) && (best == nil || u.ValidFrom.After(best.ValidFrom)) {
			u := u
			best = &u
		}
	}
	return best, nil
}

func TestResolveCurrentMergerSplitAndReuse(t *testing.T) {
	base, y2027 := Day(2004, 1, 1), Day(2027, 1, 1)
	g := fakeGazetteer{
		// Sáp nhập: 89 (An Giang cũ) và 91 (Kiên Giang) → 91 (An Giang mới, dùng lại mã 91).
		{Code: "89", Level: port.LevelProvince, ValidFrom: base, ValidTo: dp(ReorgDate), Successors: []string{"91"}},
		{Code: "91", Level: port.LevelProvince, ValidFrom: base, ValidTo: dp(ReorgDate), Successors: []string{"91"}},
		{Code: "91", Level: port.LevelProvince, ValidFrom: ReorgDate},
		// Chia tách hai bước: A → (B, C) năm 2025, C → D năm 2027.
		{Code: "A", ValidFrom: base, ValidTo: dp(ReorgDate), Successors: []string{"B", "C"}},
		{Code: "B", ValidFrom: ReorgDate},
		{Code: "C", ValidFrom: ReorgDate, ValidTo: dp(y2027), Successors: []string{"D"}},
		{Code: "D", ValidFrom: y2027},
		// Bãi bỏ không kế thừa.
		{Code: "Z", ValidFrom: base, ValidTo: dp(ReorgDate)},
	}
	cases := []struct {
		code string
		day  time.Time
		want []string
	}{
		{"89", Day(2026, 3, 1), []string{"91"}},
		{"89", Day(2024, 3, 1), []string{"89"}},
		{"91", Day(2024, 3, 1), []string{"91"}},
		{"91", Day(2026, 3, 1), []string{"91"}},
		{"A", Day(2026, 3, 1), []string{"B", "C"}},
		{"A", Day(2028, 3, 1), []string{"B", "D"}},
	}
	for _, c := range cases {
		got, err := ResolveCurrent(c.code, c.day, g.at, g.latest)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("Current(%s, %s) = %v, %v; muốn %v", c.code, c.day.Format(time.DateOnly), got, err, c.want)
		}
	}
	if _, err := ResolveCurrent("Z", Day(2026, 1, 1), g.at, g.latest); !errors.Is(err, port.ErrUnitNotFound) {
		t.Fatalf("bãi bỏ không kế thừa phải ErrUnitNotFound: %v", err)
	}
	if _, err := ResolveCurrent("B", Day(2020, 1, 1), g.at, g.latest); !errors.Is(err, port.ErrUnitNotFound) {
		t.Fatalf("trước khi có hiệu lực phải ErrUnitNotFound: %v", err)
	}
}

func TestResolveCurrentStopsOnCycle(t *testing.T) {
	g := fakeGazetteer{
		{Code: "X", ValidFrom: Day(2000, 1, 1), ValidTo: dp(Day(2001, 1, 1)), Successors: []string{"Y"}},
		{Code: "Y", ValidFrom: Day(2001, 1, 1), ValidTo: dp(Day(2002, 1, 1)), Successors: []string{"X"}},
	}
	// X của 2002 không có (X chỉ có đến 2001) nên chuỗi dừng: không treo, trả không tìm thấy.
	if _, err := ResolveCurrent("X", Day(2030, 1, 1), g.at, g.latest); err == nil {
		t.Fatal("chuỗi kế thừa cụt phải báo lỗi")
	}
}

func TestEmbeddedGazetteerParses(t *testing.T) {
	src, units, err := ParseGazetteer(seeddata.AdminUnits)
	if err != nil || src == "" {
		t.Fatalf("dữ liệu nhúng: %v", err)
	}
	var provincesNow, communesNow int
	for _, u := range units {
		if u.ValidAt(Day(2026, 1, 1)) {
			switch u.Level {
			case port.LevelProvince:
				provincesNow++
			case port.LevelCommune:
				communesNow++
			}
		}
	}
	// ĐBSCL + Tây Ninh sau sắp xếp 2025: 6 tỉnh/thành.
	if provincesNow != 6 || communesNow < 100 {
		t.Fatalf("hiện hành: %d tỉnh, %d xã", provincesNow, communesNow)
	}
}

func TestParseGazetteerRejects(t *testing.T) {
	bad := map[string]string{
		"sai định dạng": `{"format":"x","source":"s","units":[{"code":"91","name":"A","level":"province","valid_from":"2025-07-01"}]}`,
		"trường lạ":     `{"format":"agrigreen.admin_units/v1","source":"s","units":[],"x":1}`,
		"ngày sai":      `{"format":"agrigreen.admin_units/v1","source":"s","units":[{"code":"91","name":"A","level":"province","valid_from":"01/07/2025"}]}`,
		"trùng": `{"format":"agrigreen.admin_units/v1","source":"s","units":[
			{"code":"91","name":"A","level":"province","valid_from":"2025-07-01"},
			{"code":"91","name":"B","level":"province","valid_from":"2025-07-01"}]}`,
	}
	for name, raw := range bad {
		if _, _, err := ParseGazetteer([]byte(raw)); err == nil {
			t.Errorf("%s: phải bị từ chối", name)
		}
	}
	_, _, err := ParseGazetteer([]byte(`{"format":"agrigreen.admin_units/v1","source":"s","units":[
		{"code":"91","name":"A","level":"province","valid_from":"2025-07-01"},
		{"code":"91-X","name":"B","level":"commune","valid_from":"2025-07-01"}]}`))
	var fe FieldError
	if !errors.As(err, &fe) || fe.Field != "units[1].parent_code" {
		t.Fatalf("lỗi phải chỉ đúng dòng: %v", err)
	}
}

func TestCatalogNormalize(t *testing.T) {
	it, err := Item{Catalog: port.CatalogRiceVariety, Code: "OM5451", Name: " OM5451 ",
		Attrs: map[string]string{"growth_days": " 95", "expected_yield_t_per_ha": "6.750"}}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if it.Name != "OM5451" || it.Attrs["growth_days"] != "95" || it.Attrs["expected_yield_t_per_ha"] != "6.75" {
		t.Fatalf("chuẩn hóa: %+v", it)
	}
	pi := port.CatalogItem{Attrs: it.Attrs}
	if d, err := pi.Decimal("expected_yield_t_per_ha"); err != nil || !d.Equal(decimal.RequireFromString("6.75")) {
		t.Fatalf("decimal: %v %v", d, err)
	}
	bad := []Item{
		{Catalog: "giong", Code: "A", Name: "A"},
		{Catalog: port.CatalogRiceVariety, Code: "A", Name: "A", Attrs: map[string]string{"growth_days": "95"}},
		{Catalog: port.CatalogRiceVariety, Code: "A", Name: "A", Attrs: map[string]string{"growth_days": "95", "expected_yield_t_per_ha": "0"}},
		{Catalog: port.CatalogFarmStage, Code: "lam_dat", Name: "Làm đất", Attrs: map[string]string{"pilot": "có"}},
		{Catalog: port.CatalogFarmStage, Code: "lam_dat", Name: "Làm đất", Attrs: map[string]string{"pilot": "true", "mau": "đỏ"}},
		{Catalog: port.CatalogMachineType, Code: "MGD", Name: "Máy gặt", Attrs: map[string]string{}},
		{Catalog: port.CatalogSeason, Code: "DX", Name: "Đông Xuân", Attrs: map[string]string{"start_month": "11"}},
		{Catalog: port.CatalogSeason, Code: "DX", Name: "Đông Xuân", Attrs: map[string]string{"start_month": "13", "end_month": "3"}},
		{Catalog: port.CatalogPest, Code: "x y", Name: "Rầy", Attrs: map[string]string{"kind": "sau_hai"}},
		{Catalog: port.CatalogPest, Code: "ray", Name: "", Attrs: map[string]string{"kind": "sau_hai"}},
	}
	for i, b := range bad {
		if _, err := b.Normalize(); err == nil {
			t.Errorf("mục %d phải bị từ chối: %+v", i, b)
		}
	}
	if err := ValidateReason("ngắn"); err == nil {
		t.Fatal("lý do ngắn phải bị từ chối")
	}
}

func TestParamProposalAndTransitions(t *testing.T) {
	setter := uuid.New()
	v90 := decimal.NewFromInt(90)
	v := ParamVersion{Key: port.NormKey("MGD", "thu_hoach"), Value: &v90, Unit: "ha/máy/vụ",
		EffectiveFrom: DayStart(Day(2026, 1, 1)), SourceDoc: "QĐ 128/QĐ-KTHT", Status: StatusDraft, SetterRef: setter}
	if err := v.ValidateProposal(); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"CGH.norm", "cgh", "cgh.norm:", "cgh.norm:a b", "1cgh.x"} {
		if ValidateParamKey(k) == nil {
			t.Errorf("khóa %q phải sai", k)
		}
	}
	txt := "OSM"
	tooFine := decimal.RequireFromString("0.000000001")
	for name, bad := range map[string]ParamVersion{
		"cả value và text": func() ParamVersion { b := v; b.Text = &txt; return b }(),
		"không có giá trị": func() ParamVersion { b := v; b.Value = nil; return b }(),
		"quá 8 chữ số lẻ":  func() ParamVersion { b := v; b.Value = &tooFine; return b }(),
		"thiếu văn bản":    func() ParamVersion { b := v; b.SourceDoc = " "; return b }(),
		"thiếu hiệu lực":   func() ParamVersion { b := v; b.EffectiveFrom = time.Time{}; return b }(),
	} {
		if bad.ValidateProposal() == nil {
			t.Errorf("%s: phải bị từ chối", name)
		}
	}
	if err := v.CanSubmit(uuid.New()); !errors.Is(err, ErrNotSetter) {
		t.Fatalf("người khác gửi duyệt: %v", err)
	}
	if err := v.CanSubmit(setter); err != nil {
		t.Fatal(err)
	}
	if err := v.CanDecide(); !errors.Is(err, ErrNotSubmitted) {
		t.Fatalf("nháp không được duyệt: %v", err)
	}
	v.Status = StatusApproved
	if v.CanDecide() == nil || v.CanSubmit(setter) == nil {
		t.Fatal("phiên bản đã duyệt không được chuyển tiếp")
	}
	if ValidateDecisionNote("không") == nil {
		t.Fatal("lý do từ chối quá ngắn phải bị từ chối")
	}
}

func TestParseInstant(t *testing.T) {
	d, err := ParseInstant("2026-01-01")
	if err != nil || !d.Equal(time.Date(2025, 12, 31, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("ngày = 00:00 giờ Việt Nam: %v %v", d, err)
	}
	if _, err := ParseInstant("hôm nay"); err == nil {
		t.Fatal("phải lỗi")
	}
}
