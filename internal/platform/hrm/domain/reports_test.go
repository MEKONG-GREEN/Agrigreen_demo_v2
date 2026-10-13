package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func testCatalog() Catalog {
	c := Catalog{Items: map[Kind]map[string]CatItem{}, Units: []UnitRef{{"bgd", "Ban Giám đốc"}, {"hcth", "Phòng HC-TH"}, {"trong_trot", "Phòng Trồng trọt"}}}
	for _, it := range DefaultItems() {
		if c.Items[it.Kind] == nil {
			c.Items[it.Kind] = map[string]CatItem{}
		}
		c.Items[it.Kind][it.Code] = CatItem{Name: it.Name, SortOrder: it.SortOrder, Attrs: it.Attrs}
	}
	return c
}

func rday(s string) time.Time { t, _ := time.Parse(time.DateOnly, s); return t }

func person(code string, t StaffType, unit, position, gender, birth string, degrees ...Degree) Person {
	b := rday(birth)
	start := rday("2010-01-01")
	return Person{StaffID: uuid.New(), Code: code, Type: t, FullName: "Người " + code, Gender: gender, Birth: &b, BirthPrec: PrecisionDay,
		Snap: Snapshot{Status: StatusActive, StartedOn: &start, Unit: unit, Position: position, Grade: "01.003"}, Degrees: degrees,
		Sensitive: true, Ethnicity: "kinh", Religion: "khong", Hometown: "An Giang", RevolutionDate: "2010-01-01"}
}

func TestBuildNS01OrderAndChecks(t *testing.T) {
	cat := testCatalog()
	asOf := rday("2026-07-31")
	ps := []Person{
		person("VC-00003", StaffVienChuc, "trong_trot", "vien_chuc", "nu", "1990-01-01"),
		person("VC-00002", StaffVienChuc, "trong_trot", "truong_phong", "nam", "1975-03-04"),
		person("VC-00001", StaffVienChuc, "bgd", "giam_doc", "nam", "1970-01-01"),
		person("HD-00001", StaffHopDong, "hcth", "vien_chuc", "nu", "1995-01-01"),
	}
	ps[0].Ethnicity = "" // thiếu TK
	tb, err := Build(NS01, BuildInput{AsOf: asOf, Cat: cat, People: ps, Prev: []uuid.UUID{ps[1].StaffID}, PrevAsOf: rday("2026-06-30")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tb.Columns) != 19 || tb.Period != "THÁNG 7/2026" {
		t.Fatalf("cột %d, kỳ %q", len(tb.Columns), tb.Period)
	}
	var order []string
	for _, r := range tb.Rows {
		order = append(order, r.Kind+":"+r.Cells[1])
	}
	want := []string{"group:I. TRUNG TÂM KHUYẾN NÔNG", "unit:Ban Giám đốc (1 VC)", "person:Người VC-00001", "unit:Phòng Trồng trọt (2 VC)",
		"person:Người VC-00002", "person:Người VC-00003"}
	if len(order) != len(want) {
		t.Fatalf("dòng %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("dòng %d = %q, muốn %q", i, order[i], want[i])
		}
	}
	// Nữ: ngày sinh vào cột Nữ.
	if r := tb.Rows[5].Cells; r[2] != "" || r[3] != "01/01/1990" {
		t.Fatalf("ngày sinh nữ: %v", r[2:4])
	}
	if tb.Status() != "warning" || tb.Blocked() {
		t.Fatalf("trạng thái %s", tb.Status())
	}
	for _, c := range tb.Checks {
		if c.Rule == "R3" && c.OK {
			t.Fatal("R3 phải cảnh báo: 1 + 0 − 0 ≠ 3")
		}
		if c.Rule == "TK" && (c.OK || len(c.StaffIDs) != 1) {
			t.Fatalf("TK: %+v", c)
		}
	}
}

func TestBuildNS05NS06Rules(t *testing.T) {
	cat := testCatalog()
	asOf := rday("2026-09-30")
	ps := []Person{
		person("VC-1", StaffVienChuc, "trong_trot", "vien_chuc", "nu", "1990-01-01", Degree{Level: "thac_si", Major: "trong_trot"}),
		person("VC-2", StaffVienChuc, "trong_trot", "vien_chuc", "nam", "1964-01-01", Degree{Level: "dai_hoc", Major: "chan_nuoi"}),
		person("VC-3", StaffVienChuc, "hcth", "vien_chuc", "nam", "1980-01-01", Degree{Level: "dai_hoc", Major: "khong_co"}),
		person("HD-1", StaffHopDong, "hcth", "vien_chuc", "nu", "2000-01-01"),
	}
	ps[2].Ethnicity = "khmer"
	in := BuildInput{AsOf: asOf, Cat: cat, People: ps, Retirement: RetirementAges{MaleMonths: 738, FemaleMonths: 684, RemindMonths: 6}}
	t5, _ := Build(NS05, in)
	if t5.Blocked() || len(t5.Cells[CellKey(RowKeyCenter, "tong")]) != 3 {
		t.Fatalf("NS05: %+v", t5.Checks)
	}
	if got := t5.Cells[CellKey("trong_trot", "g:ths_nong_hoc")]; len(got) != 1 || got[0] != ps[0].StaffID {
		t.Fatalf("ô ThS Nông học Phòng TT: %v", got)
	}
	if len(t5.Cells[CellKey(RowKeyCenter, "dtts")]) != 1 {
		t.Fatal("DTTS")
	}
	for _, c := range t5.Checks {
		if c.Rule == "CLASS" && (c.OK || c.StaffIDs[0] != ps[2].StaffID) {
			t.Fatalf("chưa phân loại: %+v", c)
		}
	}
	t6, _ := Build(NS06, in)
	if t6.Blocked() || len(t6.Columns) != 39 {
		t.Fatalf("NS06: %d cột %+v", len(t6.Columns), t6.Checks)
	}
	if len(t6.Cells[CellKey(RowKeyCenter, "tong")]) != 4 || len(t6.Cells[CellKey(RowKeyCenter, "age_over")]) != 1 {
		t.Fatal("NS06 tổng / trên tuổi nghỉ hưu (VC-2 sinh 1964 > 61 tuổi 6 tháng)")
	}
	// R1 chặn khi thiếu ngày sinh.
	ps[3].Birth = nil
	t6, _ = Build(NS06, BuildInput{AsOf: asOf, Cat: cat, People: ps})
	if !t6.Blocked() {
		t.Fatal("R1 phải chặn")
	}
	d := BuildDashboard(ps, cat, asOf, in.Retirement, true)
	if d.Total != 3 || d.Staff != 4 || d.Over != 1 || d.Gender["nu"] != 2 {
		t.Fatalf("dashboard %+v", d)
	}
}

func TestPeriodLabel(t *testing.T) {
	if PeriodLabel(PeriodQuarter, rday("2026-08-15")) != "QUÝ III/2026" || PeriodLabel(PeriodAdHoc, rday("2026-08-15")) != "NGÀY 15/08/2026" {
		t.Fatal("nhãn kỳ")
	}
	if AgeMonths(rday("1965-01-15"), rday("2026-07-14")) != 737 {
		t.Fatal("tháng tuổi")
	}
}
