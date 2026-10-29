package domain

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Dữ liệu hư cấu theo bố cục mẫu "Lịch trực ksgm" (BM-03): tên, SĐT không phải dữ liệu thật.
func sampleRows() [][]string {
	day := func(vals ...string) []string { return vals }
	row := func(stt, fac, addr, off, phone string, shifts ...string) []string {
		return append([]string{stt, fac, addr, off, phone}, shifts...)
	}
	hdr := []string{"STT", "TÊN CƠ SỞ GIẾT MỔ", "ĐỊA CHỈ", "VIÊN CHỨC TRỰC KSGM", "ĐT viên Chức"}
	for d := 1; d <= 31; d++ {
		hdr = append(hdr, strconv.Itoa(d))
	}
	return [][]string{
		{},
		{"", "BẢNG PHÂN CÔNG LỊCH TRỰC KSGM VIÊN CHỨC TĂNG CƯỜNG XÃ, PHƯỜNG, ĐẶC KHU THÁNG 8/2026"},
		{"", "   (theo thông báo số 1/ABC ngày 1/7/2026 của sở X)"},
		{"", "(Ghi chú: Đ: ca đêm; N: ca ngày)"},
		{},
		hdr,
		row("1", "Lò mổ Hòa Bình", "Xã A", "Trần Văn Một", "912000001", day("N", "Đ", "", "NĐ")...),
		row("", "", "", "Lê Thị Hai (CTV)", "0912.000.002", day("", "D", "N")...),
		row("2", "Lò mổ Bình An", "Xã B", "", ""),                     // dòng đầu khối, chưa có viên chức
		row("", "", "", "Trần Văn Một", "+84912000001", day("Đ ")...), // trực cơ sở thứ hai
		row("", "", "", "Người Lạ", "0999999999", day("N")...),
		row("3", "Cơ sở không có", "Xã C", "Lê Thị Hai", "", day("N")...),
		row("4", "Lò mổ Hòa Bình", "Xã A", "Lê Thị Hai", "", day("", "", "", "", "2/9")...),
		{"TC", "4", "3"},
		{"", "Người lập biểu", "", "Trần Văn Một"},
	}
}

var (
	facA = uuid.MustParse("0192a000-0000-7000-8000-00000000000a")
	facB = uuid.MustParse("0192a000-0000-7000-8000-00000000000b")
	offA = uuid.MustParse("0192a000-0000-7000-8000-0000000000a1")
	offB = uuid.MustParse("0192a000-0000-7000-8000-0000000000b2")
)

func TestParseBM03Layout(t *testing.T) {
	sh, ok, err := ParseBM03(sampleRows())
	if err != nil || !ok {
		t.Fatalf("không nhận bố cục: ok=%v err=%v", ok, err)
	}
	if sh.Month == nil || sh.Month.Format("2006-01") != "2026-08" {
		t.Fatalf("tháng ở tiêu đề: %v", sh.Month)
	}
	if !strings.HasPrefix(sh.Basis, "(theo thông báo số") || sh.HeaderLine != 6 || sh.Days != 31 {
		t.Fatalf("căn cứ/dòng tiêu đề/số ngày: %+v", sh)
	}
	if len(sh.Rows) != 6 {
		t.Fatalf("6 dòng viên chức (bỏ dòng đầu khối, dừng ở TC): %d %+v", len(sh.Rows), sh.Rows)
	}
	if r := sh.Rows[1]; r.Facility != "Lò mổ Hòa Bình" || r.Address != "Xã A" || r.Line != 8 {
		t.Fatalf("dòng sau nối khối cơ sở: %+v", r)
	}
	if r := sh.Rows[2]; r.Facility != "Lò mổ Bình An" || r.Shifts[1] != "Đ" {
		t.Fatalf("khối cơ sở thứ hai: %+v", r)
	}
}

func TestParseBM03NotLayout(t *testing.T) {
	if _, ok, _ := ParseBM03([][]string{{"A", "B", "C"}, {"1", "2", "3"}}); ok {
		t.Fatal("bảng không có STT/VIÊN CHỨC không phải BM-03")
	}
	rows := sampleRows()[:6]
	for i := 0; i <= MaxImportRows; i++ {
		rows = append(rows, []string{"", "Lò mổ Hòa Bình", "", "Trần Văn Một", "", "N"})
	}
	if _, _, err := ParseBM03(rows); err != ErrTooManyRows {
		t.Fatalf("quá số dòng: %v", err)
	}
}

func TestMatchImport(t *testing.T) {
	sh, _, _ := ParseBM03(sampleRows())
	m := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) // tháng 9: 30 ngày
	facs := []ImportFacility{{Ref: facA, Code: "CS_A", Name: "LÒ MỔ HÒA BÌNH"}, {Ref: facB, Code: "CS_B", Name: "Lò mổ  Bình An"}}
	offs := []ImportOfficer{{Ref: offA, Name: "Trần Văn Một", Phone: "0912000001"}, {Ref: offB, Name: "Lê Thị Hai", Phone: ""}}
	cells, issues := MatchImport(m, sh, facs, offs)
	want := map[[3]string]string{
		{facA.String(), offA.String(), "1"}: ShiftDay, {facA.String(), offA.String(), "2"}: ShiftNight, {facA.String(), offA.String(), "4"}: ShiftBoth,
		{facA.String(), offB.String(), "2"}: ShiftNight, {facA.String(), offB.String(), "3"}: ShiftDay,
		{facB.String(), offA.String(), "1"}: ShiftNight,
	}
	if len(cells) != len(want) {
		t.Fatalf("số ô: %d %+v", len(cells), cells)
	}
	for _, c := range cells {
		if want[[3]string{c.Facility.String(), c.Officer.String(), strconv.Itoa(c.Day)}] != c.Shift {
			t.Fatalf("ô sai: %+v", c)
		}
	}
	byField := map[string]int{}
	for _, is := range issues {
		byField[is.Field]++
		if strings.Contains(is.Message, "0999999999") {
			t.Fatalf("thông báo lỗi không được lặp SĐT: %s", is.Message)
		}
	}
	if byField["officer"] != 1 || byField["facility"] != 1 || byField["day:5"] != 1 {
		t.Fatalf("lỗi theo dòng: %+v", issues)
	}
}

func TestMatchImportAmbiguousAndDayOutOfMonth(t *testing.T) {
	sh := ImportSheet{Rows: []ImportRow{
		{Line: 7, Facility: "Trùng", Officer: "A", Shifts: map[int]string{1: "N"}},
		{Line: 8, Facility: "Trùng (CS2)", Officer: "A", Shifts: map[int]string{31: "N", 1: "nd"}},
		{Line: 9, Facility: "CS2", Officer: "A", Shifts: map[int]string{1: "N"}},
		{Line: 10, Facility: "CS2", Officer: "A", Shifts: map[int]string{2: "N Đ", 3: "Đ·", 4: "Mạng lưới thú y"}},
	}}
	m := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	f1, f2 := uuid.New(), uuid.New()
	cells, issues := MatchImport(m, sh, []ImportFacility{{Ref: f1, Code: "CS1", Name: "Trùng"}, {Ref: f2, Code: "CS2", Name: "trùng"}},
		[]ImportOfficer{{Ref: offA, Name: "A"}})
	if len(cells) != 3 || cells[0].Facility != f2 || cells[0].Shift != ShiftBoth || cells[1].Shift != ShiftBoth || cells[2].Shift != ShiftNight {
		t.Fatalf("ghép theo \"tên (mã)\", ký hiệu có khoảng trắng/dấu chấm: %+v", cells)
	}
	if len(issues) != 4 || issues[0].Field != "facility" || issues[1].Field != "day:31" || issues[2].Field != "day:1" || issues[3].Field != "day:4" {
		t.Fatalf("lỗi trùng tên / ngày ngoài tháng / trùng ô / ký hiệu lạ: %+v", issues)
	}
}

func TestPhoneHelpers(t *testing.T) {
	for in, want := range map[string]string{"912345678": "0912345678", "0912.345.678": "0912345678", "+84 912 345 678": "0912345678",
		"12345": "", "": "", strings.Repeat("9", 400): ""} {
		if got := NormalizePhone(in); got != want {
			t.Fatalf("NormalizePhone(%q)=%q, muốn %q", in, got, want)
		}
	}
	if MaskPhone("0912345678") != "0912•••678" || MaskPhone("x") != "" {
		t.Fatal("MaskPhone")
	}
	if _, err := CheckBasis(strings.Repeat("a", 301)); err == nil {
		t.Fatal("căn cứ quá dài")
	}
	if b, _ := CheckBasis("  "); b != DefaultBasis {
		t.Fatal("căn cứ mặc định")
	}
}
