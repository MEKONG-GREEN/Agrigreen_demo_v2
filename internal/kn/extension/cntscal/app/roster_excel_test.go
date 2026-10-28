package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Tệp hư cấu theo bố cục "Lịch trực ksgm Kn" (một sheet mỗi tháng 8, 9, 10/2026; sheet tháng 10 có bản sao "T102026.";
// STT/tên/địa chỉ gộp ô theo cơ sở; ngày ở tiêu đề cột là ô ngày tháng hoặc số; SĐT là ô số mất số 0 đầu hoặc chuỗi có
// dấu chấm). Họ tên, SĐT đều không có thật.
var (
	tfA = uuid.MustParse("0192a000-0000-7000-8000-00000000f00a")
	tfB = uuid.MustParse("0192a000-0000-7000-8000-00000000f00b")
	toA = uuid.MustParse("0192a000-0000-7000-8000-00000000a00a")
	toB = uuid.MustParse("0192a000-0000-7000-8000-00000000a00b")
	toC = uuid.MustParse("0192a000-0000-7000-8000-00000000a00c")
)

func templateLike(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	months := []struct {
		sheet string
		m     time.Time
	}{
		{"LỊCH TRỰC KSGM T82026", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
		{"LỊCH TRỰC KSGM T92026", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
		{"LỊCH TRỰC KSGM T102026", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
		{"LỊCH TRỰC KSGM T102026.", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
	}
	for i, mo := range months {
		if i == 0 {
			_ = f.SetSheetName("Sheet1", mo.sheet)
		} else if _, err := f.NewSheet(mo.sheet); err != nil {
			t.Fatal(err)
		}
		sh := mo.sheet
		top := 3
		if i > 0 {
			top = 2 // sheet tháng 9 trở đi: tiêu đề lùi một dòng như mẫu
		}
		set := func(cell string, v any) {
			if err := f.SetCellValue(sh, cell, v); err != nil {
				t.Fatal(err)
			}
		}
		set(fmt.Sprintf("B%d", top), fmt.Sprintf("   BẢNG PHÂN CÔNG LỊCH TRỰC KSGM VIÊN CHỨC TĂNG CƯỜNG XÃ, PHƯỜNG, ĐẶC KHU THÁNG %d/%d", int(mo.m.Month()), mo.m.Year()))
		set(fmt.Sprintf("B%d", top+1), "   (theo thông báo số 9/XYZ ngày 1/7/2026 của sở X và kèm theo danh sách phân công viên chức trực KSGM)")
		set(fmt.Sprintf("B%d", top+2), "(Ghi chú: Đ: ca đêm;  N: ca ngày)")
		hr := top + 4
		for j, h := range []string{"STT", "TÊN CƠ SỞ GIẾT MỔ", "ĐỊA CHỈ", "VIÊN CHỨC TRỰC KSGM", "ĐT viên Chức"} {
			col, _ := excelize.ColumnNumberToName(j + 1)
			set(fmt.Sprintf("%s%d", col, hr), h)
		}
		days := domain.DaysIn(mo.m)
		for d := 1; d <= days; d++ {
			col, _ := excelize.ColumnNumberToName(5 + d)
			if i == 0 && d > 1 {
				set(fmt.Sprintf("%s%d", col, hr), d)
			} else {
				set(fmt.Sprintf("%s%d", col, hr), mo.m.AddDate(0, 0, d-1)) // ô ngày tháng
			}
		}
		r := hr + 1
		set(fmt.Sprintf("A%d", r), 1)
		set(fmt.Sprintf("B%d", r), "LÒ MỔ HƯ CẤU MỘT")
		set(fmt.Sprintf("C%d", r), "XÃ HƯ CẤU")
		set(fmt.Sprintf("D%d", r), "Nguyễn Văn Giả")
		set(fmt.Sprintf("E%d", r), 900000001) // ô số, mất số 0 đầu
		set(fmt.Sprintf("D%d", r+1), "Trần Thị Mẫu (CTV)")
		_ = f.SetCellStr(sh, fmt.Sprintf("E%d", r+1), "0900.000.002")
		for d := 1; d <= days; d++ {
			col, _ := excelize.ColumnNumberToName(5 + d)
			if d%2 == 1 {
				set(fmt.Sprintf("%s%d", col, r), "N")
				set(fmt.Sprintf("%s%d", col, r+1), "Đ ")
			} else {
				set(fmt.Sprintf("%s%d", col, r), "D")
				set(fmt.Sprintf("%s%d", col, r+1), "NĐ")
			}
		}
		for _, col := range []string{"A", "B", "C"} {
			_ = f.MergeCell(sh, fmt.Sprintf("%s%d", col, r), fmt.Sprintf("%s%d", col, r+2))
		}
		r += 3
		set(fmt.Sprintf("A%d", r), 2)
		set(fmt.Sprintf("B%d", r), "LÒ MỔ HƯ CẤU HAI")
		set(fmt.Sprintf("C%d", r), "PHƯỜNG HƯ CẤU")
		set(fmt.Sprintf("D%d", r+1), "Lê Văn Thử")
		set(fmt.Sprintf("E%d", r+1), "0900000003")
		set(fmt.Sprintf("F%d", r+1), "NĐ")
		set(fmt.Sprintf("G%d", r+1), "2/9") // ký hiệu lạ như trong mẫu
		r += 3
		set(fmt.Sprintf("A%d", r), "TC")
		set(fmt.Sprintf("B%d", r), 2)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func importLists() ([]domain.ImportFacility, []domain.ImportOfficer) {
	return []domain.ImportFacility{{Ref: tfA, Code: "CS_A", Name: "Lò mổ hư cấu một"}, {Ref: tfB, Code: "CS_B", Name: "Lò mổ hư cấu hai"}},
		[]domain.ImportOfficer{{Ref: toA, Name: "Nguyễn Văn Giả", Phone: "0900000001"}, {Ref: toB, Name: "Trần Thị Mẫu"}, {Ref: toC, Name: "Lê Văn Thử", Phone: "0900000003"}}
}

func TestReadBM03TemplateMonths(t *testing.T) {
	data := templateLike(t)
	facs, offs := importLists()
	for _, m := range []time.Time{time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)} {
		name, sh, err := ReadBM03(data, m)
		if err != nil {
			t.Fatalf("%s: %v", m.Format("1/2006"), err)
		}
		if !strings.HasSuffix(name, fmt.Sprintf("T%d2026", int(m.Month()))) || !strings.HasPrefix(sh.Basis, "(theo thông báo") {
			t.Fatalf("sheet/căn cứ của tháng %s: %q %q", m.Format("1/2006"), name, sh.Basis)
		}
		cells, issues := domain.MatchImport(m, sh, facs, offs)
		days := domain.DaysIn(m)
		if len(cells) != 2*days+1 {
			t.Fatalf("tháng %s: %d ô, muốn %d; lỗi %+v", m.Format("1/2006"), len(cells), 2*days+1, issues)
		}
		if len(issues) != 1 || issues[0].Field != "day:2" {
			t.Fatalf("chỉ một lỗi ký hiệu lạ: %+v", issues)
		}
	}
	if _, _, err := ReadBM03(data, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); !isImportInvalid(err, "Tệp không có sheet tháng 11/2026") {
		t.Fatalf("tháng không có trong tệp: %v", err)
	}
}

func isImportInvalid(err error, detail string) bool {
	var p *problem.Problem
	return errors.As(err, &p) && p.Code == ErrRosterImportInvalid.Code && strings.Contains(p.Detail, detail)
}

func TestReadBM03Rejects(t *testing.T) {
	m := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := ReadBM03([]byte{0xd0, 0xcf, 0x11, 0xe0}, m); !isImportInvalid(err, ".xls cũ") {
		t.Fatalf(".xls: %v", err)
	}
	if _, _, err := ReadBM03([]byte("STT,TÊN CƠ SỞ"), m); !isImportInvalid(err, ".xlsx") {
		t.Fatalf("CSV: %v", err)
	}
	if _, _, err := ReadBM03([]byte("PK\x03\x04rác"), m); !isImportInvalid(err, "Không mở được") {
		t.Fatalf("zip hỏng: %v", err)
	}
	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "không phải mẫu")
	buf, _ := f.WriteToBuffer()
	if _, _, err := ReadBM03(buf.Bytes(), m); !isImportInvalid(err, "Không thấy dòng tiêu đề cột") {
		t.Fatalf("không đúng mẫu: %v", err)
	}
	// Quá số dòng viên chức.
	_ = f.SetCellValue("Sheet1", "A1", "STT")
	_ = f.SetCellValue("Sheet1", "B1", "TÊN CƠ SỞ GIẾT MỔ")
	_ = f.SetCellValue("Sheet1", "D1", "VIÊN CHỨC TRỰC KSGM")
	_ = f.SetCellValue("Sheet1", "F1", 1)
	for r := 2; r <= domain.MaxImportRows+2; r++ {
		_ = f.SetCellValue("Sheet1", fmt.Sprintf("D%d", r), "X")
	}
	buf, _ = f.WriteToBuffer()
	if _, _, err := ReadBM03(buf.Bytes(), m); !isImportInvalid(err, "quá 2000 dòng") {
		t.Fatalf("quá dòng: %v", err)
	}
	// Sheet ẩn bị bỏ qua.
	g := excelize.NewFile()
	_ = g.SetCellValue("Sheet1", "A1", "STT")
	_ = g.SetCellValue("Sheet1", "B1", "TÊN CƠ SỞ GIẾT MỔ")
	_ = g.SetCellValue("Sheet1", "D1", "VIÊN CHỨC TRỰC KSGM")
	idx, _ := g.NewSheet("hiện")
	g.SetActiveSheet(idx)
	if err := g.SetSheetVisible("Sheet1", false); err != nil {
		t.Fatal(err)
	}
	buf, _ = g.WriteToBuffer()
	if _, _, err := ReadBM03(buf.Bytes(), m); !isImportInvalid(err, "Không thấy dòng tiêu đề cột") {
		t.Fatalf("sheet ẩn: %v", err)
	}
}

// Tệp xuất nạp lại được, ra cùng lịch (kể cả SĐT ô chữ giữ số 0 đầu, cơ sở trùng tên ghi kèm mã, cơ sở chưa có người trực).
func TestExportRoundTrip(t *testing.T) {
	m := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	tfC := uuid.MustParse("0192a000-0000-7000-8000-00000000f00c")
	cells := []domain.Cell{
		{Facility: tfA, Officer: toA, Day: 1, Shift: domain.ShiftDay}, {Facility: tfA, Officer: toA, Day: 28, Shift: domain.ShiftBoth},
		{Facility: tfA, Officer: toB, Day: 2, Shift: domain.ShiftNight}, {Facility: tfB, Officer: toA, Day: 3, Shift: domain.ShiftNight},
		{Facility: tfB, Officer: toC, Day: 3, Shift: domain.ShiftDay},
	}
	v := RosterView{Month: m, Province: "89", Cells: cells, CanViewPhone: true,
		Facilities: []FacilityView{{Ref: tfA, Code: "CS_A", Name: "Lò trùng tên", Address: "Ấp 1"}, {Ref: tfB, Code: "CS_B", Name: "Lò trùng  tên"}, {Ref: tfC, Code: "CS_C", Name: "Lò trống"}},
		Names:      map[uuid.UUID]string{toA: "Viên chức A", toB: "Viên chức B"},
		phones:     map[uuid.UUID]string{toA: "0900000001", toC: "0900000003"}}
	s := &Service{}
	data, err := s.rosterXLSX(context.Background(), v, domain.DefaultBasis)
	if err != nil {
		t.Fatal(err)
	}
	name, sh, err := ReadBM03(data, m)
	if err != nil || name != domain.ExportSheetName(m) {
		t.Fatalf("đọc lại: %q %v", name, err)
	}
	got, issues := domain.MatchImport(m, sh, []domain.ImportFacility{{Ref: tfA, Code: "CS_A", Name: "Lò trùng tên"}, {Ref: tfB, Code: "CS_B", Name: "Lò trùng  tên"}, {Ref: tfC, Code: "CS_C", Name: "Lò trống"}},
		[]domain.ImportOfficer{{Ref: toA, Name: "Viên chức A", Phone: "0900000001"}, {Ref: toB, Name: "Viên chức B"}, {Ref: toC, Phone: "0900000003"}})
	if len(issues) != 0 {
		t.Fatalf("lỗi khi nạp lại: %+v", issues)
	}
	want, _ := domain.NormalizeCells(m, cells)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("nạp lại khác lịch:\n%v\n%v", got, want)
	}
	f, _ := excelize.OpenReader(strings.NewReader(string(data)))
	defer func() { _ = f.Close() }()
	if v, _ := f.GetCellValue(name, "A1"); !strings.Contains(v, "THÁNG 2/2027") {
		t.Fatalf("tiêu đề tháng: %q", v)
	}
	if v, _ := f.GetCellValue(name, "E6"); v != "0900000001" {
		t.Fatalf("SĐT là ô chữ, giữ số 0 đầu: %q", v)
	}
	if v, _ := f.GetCellValue(name, "B6"); v != "Lò trùng tên (CS_A)" {
		t.Fatalf("cơ sở trùng tên ghi kèm mã: %q", v)
	}
	mc, _ := f.GetMergeCells(name)
	merged := false
	for _, x := range mc {
		if x.GetStartAxis() == "B6" && x.GetEndAxis() == "B7" {
			merged = true
		}
	}
	if !merged {
		t.Fatal("gộp ô tên cơ sở theo khối viên chức")
	}
	// Người không lập lịch: không có SĐT trong tệp.
	v.CanViewPhone = false
	data, _ = s.rosterXLSX(context.Background(), v, domain.DefaultBasis)
	g, _ := excelize.OpenReader(strings.NewReader(string(data)))
	defer func() { _ = g.Close() }()
	if v, _ := g.GetCellValue(name, "E6"); v != "" {
		t.Fatalf("không lộ SĐT với người không lập lịch: %q", v)
	}
}
