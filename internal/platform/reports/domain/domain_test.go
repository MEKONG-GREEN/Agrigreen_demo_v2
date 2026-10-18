package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// Định dạng số kiểu Việt Nam tính trên decimal — không có sai số float.
func TestFormatVN(t *testing.T) {
	cases := []struct {
		in    string
		scale int32
		want  string
	}{
		{"1234567.891", 2, "1.234.567,89"}, {"1234567.895", 2, "1.234.567,90"}, {"0.1", 2, "0,10"},
		{"999", 0, "999"}, {"1000", 0, "1.000"}, {"-1234.5", 1, "-1.234,5"}, {"-0.001", 2, "0,00"},
		{"60.0001", -1, "60,0001"}, {"120", 2, "120,00"}, {"100000", 4, "100.000,0000"},
	}
	for _, c := range cases {
		if got := FormatVN(decimal.RequireFromString(c.in), c.scale); got != c.want {
			t.Errorf("FormatVN(%s, %d) = %q, muốn %q", c.in, c.scale, got, c.want)
		}
	}
	// 0,1 + 0,2 bằng đúng 0,3 (float64 cho 0.30000000000000004).
	sum := decimal.RequireFromString("0.1").Add(decimal.RequireFromString("0.2"))
	if got := FormatVN(sum, -1); got != "0,3" {
		t.Fatalf("0,1 + 0,2 = %s", got)
	}
	if FormatIntVN(1234567) != "1.234.567" {
		t.Fatal("số nguyên")
	}
}

// FR-05: kỳ tháng/quý/năm.
func TestParsePeriod(t *testing.T) {
	q, err := ParsePeriod("2026-Q3")
	if err != nil || len(q.Months) != 3 || q.Months[0] != "2026-07" || q.Label != "Quý III/2026" {
		t.Fatalf("quý: %+v %v", q, err)
	}
	y, _ := ParsePeriod("2026")
	if len(y.Months) != 12 || y.Months[11] != "2026-12" {
		t.Fatalf("năm: %+v", y)
	}
	m, _ := ParsePeriod("2026-09")
	if m.Label != "Tháng 09/2026" {
		t.Fatal(m.Label)
	}
	for _, bad := range []string{"2026-13", "2026-Q5", "26", ""} {
		if _, err := ParsePeriod(bad); err == nil {
			t.Errorf("%q phải lỗi", bad)
		}
	}
	// 30/09 19:00 UTC = 01/10 02:00 giờ Việt Nam.
	if got := PreviousMonth(time.Date(2026, 9, 30, 19, 0, 0, 0, time.UTC)); got != "2026-09" {
		t.Fatalf("tháng trước theo giờ VN: %s", got)
	}
}

// US-DASH-03: mốc lịch tuần/tháng theo giờ Việt Nam, khóa mốc ổn định trong kỳ.
func TestScheduleSlot(t *testing.T) {
	w := Schedule{Frequency: FreqWeekly, Weekday: 1, Hour: 7}
	// Thứ Hai 28/09/2026 07:30 giờ VN = 00:30 UTC.
	at, key := w.Slot(time.Date(2026, 9, 28, 0, 30, 0, 0, time.UTC))
	if key != "2026-W40" || !at.Equal(time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("mốc tuần: %s %s", at, key)
	}
	// Chủ nhật 04/10 vẫn cùng mốc W40.
	if _, k := w.Slot(time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)); k != "2026-W40" {
		t.Fatalf("cùng tuần: %s", k)
	}
	// Thứ Hai 28/09 06:59 giờ VN: chưa tới giờ → mốc tuần trước.
	if _, k := w.Slot(time.Date(2026, 9, 27, 23, 59, 0, 0, time.UTC)); k != "2026-W39" {
		t.Fatalf("trước giờ gửi: %s", k)
	}
	m := Schedule{Frequency: FreqMonthly, DayOfMonth: 1, Hour: 6}
	at, key = m.Slot(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	if key != "2026-10" || m.ReportPeriod(at) != "2026-09" {
		t.Fatalf("lịch tháng gửi số liệu tháng trước: %s %s", key, m.ReportPeriod(at))
	}
	if w.ReportPeriod(at) != "2026-10" {
		t.Fatal("lịch tuần gửi lũy kế tháng đang chạy")
	}
}

func TestTemplateValidate(t *testing.T) {
	for _, b := range Builtins() {
		d := b.Def
		if err := d.Validate(); err != nil {
			t.Errorf("mẫu hệ thống %s: %v", b.Key, err)
		}
	}
	d := Definition{Title: "Báo cáo thử", Source: port.SourceCGHShortage, Columns: []Column{{Key: "sown_area_ha"}}, Formats: []string{"xlsx"}}
	if err := d.Validate(); err == nil {
		t.Fatal("cột không thuộc nguồn phải bị từ chối")
	}
	d = Definition{Title: "Báo cáo thử", Source: port.SourceCGHSummary, Grouping: LevelCooperative, Columns: []Column{{Key: "required"}}, Formats: []string{"xlsx"}}
	if err := d.Validate(); err == nil {
		t.Fatal("cấp HTX không hỗ trợ cho tổng hợp CGH")
	}
	d = Definition{Title: "Báo cáo thử", Source: port.SourceCGHSummary, Columns: []Column{{Key: "required"}}, Formats: []string{"docx"}}
	if err := d.Validate(); err == nil {
		t.Fatal("định dạng lạ")
	}
	d = Definition{Title: "Báo cáo thử", Source: port.SourceCGHSummary, Columns: []Column{{Key: "required"}}, Formats: []string{"pdf"}}
	if err := d.Validate(); err != nil || d.Columns[0].Title != "Nhu cầu (số máy cần)" || d.Grouping != LevelProvince {
		t.Fatalf("tiêu đề/cấp mặc định: %+v %v", d, err)
	}
}

// Column visibility is a generic capability rule, independent of an application.
func TestBuildColumnsByLevel(t *testing.T) {
	sources := map[string]SourceSpec{"example.measure": {Key: "example.measure", Module: "example", Columns: []ColumnSpec{
		{Key: "province_target", Kind: KindDecimal, MaxLevel: LevelProvince},
		{Key: "commune_name", Kind: KindText, MinLevel: LevelCommune},
	}}}
	def := Definition{Source: "example.measure", Columns: []Column{{Key: "province_target"}, {Key: "commune_name"}}}
	commune := BuildWithSources(def, LevelCommune, nil, nil, sources)
	if len(commune.Columns) != 1 || commune.Columns[0].Key != "commune_name" {
		t.Fatal("commune column visibility")
	}
	province := BuildWithSources(def, LevelProvince, nil, nil, sources)
	if len(province.Columns) != 1 || province.Columns[0].Key != "province_target" {
		t.Fatal("province column visibility")
	}
}
