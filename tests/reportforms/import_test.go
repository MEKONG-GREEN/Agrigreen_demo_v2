package reportformstest

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/form"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/formimport"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

func headerSet(d port.FormDefinition) []string {
	var out []string
	for _, c := range d.Header.Cells {
		out = append(out, c.Letter+string(rune('0'+c.Row))+"="+c.Text)
	}
	slices.Sort(out)
	return out
}

// TestFN_AD_04_06_ImporterRoundTrip: xuất mẫu thành .xlsx đúng bố cục (tiêu đề gộp, hàng chữ
// cái, dòng loại cột, công thức Excel) rồi nạp lại → cùng chữ cái, loại cột, công thức, vùng
// gộp, ô tiêu đề, khối kỳ, cột cố định, “Yêu cầu biểu mẫu”.
func TestFN_AD_04_06_ImporterRoundTrip(t *testing.T) {
	for _, code := range []string{"T01", "T04", "T07", "V05", "M01"} {
		src := seed(t, code)
		b, err := formimport.Export(src)
		if err != nil {
			t.Fatalf("%s: xuất: %v", code, err)
		}
		got, rep, err := formimport.Import(bytes.NewReader(b), formimport.Options{Code: code, Title: src.Title, FileName: code + ".xlsx"})
		if err != nil {
			t.Fatalf("%s: nạp: %v (%v)", code, err, rep.Warnings)
		}
		if !rep.KindRow || rep.HeaderRows != src.Header.Rows || rep.DataRow != src.Header.Rows+3 || len(got.Columns) != len(src.Columns) {
			t.Fatalf("%s: báo cáo %+v, %d cột", code, rep, len(got.Columns))
		}
		for i, c := range src.Columns {
			g := got.Columns[i]
			if g.Letter != c.Letter || g.Kind != c.Kind || g.Formula != c.Formula || g.PeriodSlot != c.PeriodSlot {
				t.Fatalf("%s cột %s: nhận %+v, muốn %+v", code, c.Letter, g, c)
			}
		}
		sm, gm := append([]string{}, src.Header.Merges...), append([]string{}, got.Header.Merges...)
		slices.Sort(sm)
		slices.Sort(gm)
		if !slices.Equal(sm, gm) {
			t.Fatalf("%s: vùng gộp\n%v\n%v", code, gm, sm)
		}
		if !slices.Equal(headerSet(src), headerSet(got)) {
			t.Fatalf("%s: ô tiêu đề khác", code)
		}
		if len(got.PeriodBlocks) != len(src.PeriodBlocks) {
			t.Fatalf("%s: khối kỳ %v / %v", code, got.PeriodBlocks, src.PeriodBlocks)
		}
		for i, pb := range src.PeriodBlocks {
			g := got.PeriodBlocks[i]
			if g.FirstLetter != pb.FirstLetter || g.Width != pb.Width || g.Slots != pb.Slots || g.TitlePattern != pb.TitlePattern {
				t.Fatalf("%s: khối kỳ %+v, muốn %+v", code, g, pb)
			}
		}
		if got.FreezeThrough != src.FreezeThrough || got.Requirements != src.Requirements || got.SheetName != src.SheetName {
			t.Fatalf("%s: cố định %q/%q, yêu cầu %q, sheet %q", code, got.FreezeThrough, src.FreezeThrough, got.Requirements, got.SheetName)
		}
		for _, l := range []string{"A", "B", "C"} {
			i := int(l[0] - 'A')
			if got.Columns[i].FixedSource != src.Columns[i].FixedSource {
				t.Fatalf("%s: cột %s nguồn %q", code, l, got.Columns[i].FixedSource)
			}
		}
		// Tính được như mẫu gốc.
		in := port.EvalInput{Mode: port.EvalDraft, Rows: []port.EvalRow{{Commune: "c1", Input: map[string]string{"F": "1", "G": "2"}}}}
		if a, b := form.Evaluate(src, in), form.Evaluate(got, in); a.Rows[0].Cells["M"] != b.Rows[0].Cells["M"] {
			t.Fatalf("%s: kết quả tính khác sau khứ hồi", code)
		}
	}
}

// TestFN_AD_04_06_ImportCustomerSheet: tệp kiểu tệp gốc của khách (không có hàng chữ cái,
// không dòng loại cột): nhận cột cố định theo tiêu đề, Tỉnh giao, khối “Tuần n”, công thức
// cùng dòng; công thức tham chiếu dòng khác / hàm ngoài tập con → cảnh báo, cột nhập.
func TestFN_AD_04_06_ImportCustomerSheet(t *testing.T) {
	x := excelize.NewFile()
	sh := "PL 01_Tđộ XG TĐ 2026"
	_ = x.SetSheetName(x.GetSheetName(0), sh)
	cells := map[string]string{
		"A1": "BÁO CÁO TIẾN ĐỘ XUỐNG GIỐNG", // dòng tên biểu trên khung tiêu đề
		"A2": "STT", "B2": "Khu vực", "C2": "Xã, phường", "D2": "KH tỉnh giao (ha)", "E2": "DIỆN TÍCH XUỐNG GIỐNG (ha)",
		"E3": "Tuần 1", "F3": "Tuần 2", "G3": "Tuần 3", "H2": "LŨY KẾ (ha)", "I2": "Ngày kết thúc XG", "J2": "Tỷ lệ (%)", "K2": "Tra cứu",
		"A4": "1", "B4": "Khu vực 1", "C4": "Xã A",
		"A9": "Yêu cầu biểu mẫu: Số xuống giống được nhập là số tăng thêm trong tuần, không nhập số lũy kế",
	}
	for k, v := range cells {
		_ = x.SetCellStr(sh, k, v)
	}
	for _, m := range [][2]string{{"A2", "A3"}, {"B2", "B3"}, {"C2", "C3"}, {"D2", "D3"}, {"E2", "G2"}, {"H2", "H3"}, {"I2", "I3"}, {"J2", "J3"}, {"K2", "K3"}} {
		_ = x.MergeCell(sh, m[0], m[1])
	}
	_ = x.SetCellFormula(sh, "H4", "SUM(E4:G4)")
	_ = x.SetCellFormula(sh, "J4", "IF(D4=0,0,H4/D5*100)")
	_ = x.SetCellFormula(sh, "K4", "VLOOKUP(C4,A1:B2,2,0)")
	var buf bytes.Buffer
	if err := x.Write(&buf); err != nil {
		t.Fatal(err)
	}
	d, rep, err := formimport.Import(bytes.NewReader(buf.Bytes()), formimport.Options{Code: "T01", HeaderStart: 2})
	if err != nil {
		t.Fatalf("%v %v", err, rep.Warnings)
	}
	want := map[string]string{"A": port.ColumnFixed, "B": port.ColumnFixed, "C": port.ColumnFixed, "D": port.ColumnProvincial, "E": port.ColumnPerPeriod, "G": port.ColumnPerPeriod, "H": port.ColumnFormula, "I": port.ColumnInput, "J": port.ColumnInput, "K": port.ColumnInput}
	for l, k := range want {
		c := d.Columns[l[0]-'A']
		if c.Kind != k {
			t.Errorf("cột %s: %s, muốn %s", l, c.Kind, k)
		}
	}
	if d.Columns[7].Formula != "SUM(E#:G#)" || d.Columns[8].DType != port.DTypeDate || d.Columns[3].Unit != "ha" || rep.DataRow != 4 || rep.HeaderRows != 2 {
		t.Fatalf("nạp sai: %+v %+v", d.Columns[7], rep)
	}
	if len(d.PeriodBlocks) != 1 || d.PeriodBlocks[0].FirstLetter != "E" || d.PeriodBlocks[0].Slots != 3 || d.PeriodBlocks[0].TitlePattern != "Tuần {n}" {
		t.Fatalf("khối kỳ: %+v", d.PeriodBlocks)
	}
	if !strings.HasPrefix(d.Requirements, "Số xuống giống được nhập") || d.Completeness != port.CompletenessPartial || len(rep.Warnings) < 2 {
		t.Fatalf("yêu cầu/cảnh báo: %q %v", d.Requirements, rep.Warnings)
	}
	if !d.Columns[9].Placeholder || !strings.Contains(d.Columns[9].Note, "D5") {
		t.Fatalf("công thức dòng khác phải giữ nguyên văn để Admin sửa: %+v", d.Columns[9])
	}
	if _, _, err := formimport.Import(strings.NewReader("không phải xlsx"), formimport.Options{Code: "T01"}); !errors.Is(err, formimport.ErrInvalid) {
		t.Fatalf("tệp hỏng: %v", err)
	}
	if _, _, err := formimport.Import(bytes.NewReader(buf.Bytes()), formimport.Options{Code: "T01", Sheet: "Không có"}); !errors.Is(err, formimport.ErrInvalid) {
		t.Fatalf("sheet sai: %v", err)
	}
}

// TestFN_AD_04_06_ToRowFormula: công thức Excel của dòng dữ liệu → công thức dòng.
func TestFN_AD_04_06_ToRowFormula(t *testing.T) {
	for src, want := range map[string]string{
		"=SUM(F7:L7)":          "SUM(F#:L#)",
		"IF($E7=0,0,M7/$E$7)":  "IF(E#=0,0,M#/E#)",
		`IF(A7="B7",1,0)`:      `IF(A#="B7",1,0)`,
		"ROUND(LOG10(1),0)+A7": "",
		"F7+Sheet2!A7":         "",
		"F8+1":                 "",
		"round(f7*1.5,2)":      "ROUND(F#*1.5,2)",
	} {
		got, err := formimport.ToRowFormula(src, 7)
		if want == "" {
			if err == nil {
				t.Errorf("%s phải lỗi, nhận %s", src, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%s → %s %v, muốn %s", src, got, err, want)
		}
	}
}
