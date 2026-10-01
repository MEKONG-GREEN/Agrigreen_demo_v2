package formula_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/formula"
)

// row là một dòng dữ liệu kiểm thử: chữ cái cột → giá trị nhập (chuỗi như ô lưới).
type row map[string]string

func (r row) Cell(col int) formula.Value {
	s, ok := r[formula.ColumnLetter(col)]
	if !ok || s == "" {
		return formula.Value{}
	}
	switch s {
	case "TRUE":
		return formula.BoolValue(true)
	case "FALSE":
		return formula.BoolValue(false)
	}
	if d, err := decimal.NewFromString(s); err == nil {
		return formula.NumberValue(d)
	}
	return formula.TextValue(s)
}

func (r row) XRef(form, letter string) formula.Value {
	if form != "T01" {
		return formula.Value{}
	}
	return r.Cell(formula.ColumnIndex(letter))
}

func eval(t *testing.T, src string, r row) string {
	t.Helper()
	p, err := formula.Compile(src)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return p.Run(r).String()
}

// Dòng mẫu theo PL 01 (T01): D KH tỉnh giao, E KHXG địa phương, F…L tuần 1…7.
var t01Row = row{"A": "1", "B": "Khu vực 1", "C": "Xã Mỹ Hòa Hưng", "D": "1200.5", "E": "1180", "F": "100.25", "G": "250.5", "H": "", "I": "75.125", "J": "0", "K": "", "L": "10"}

// TestFN_AD_04_06_FormulaGolden: bộ ca thử vàng — kết quả chính xác (decimal) của tập
// công thức con trên dòng mẫu T01/T04 (kn-v7.7-plan P3 mục 2).
func TestFN_AD_04_06_FormulaGolden(t *testing.T) {
	cases := []struct{ src, want string }{
		{"=SUM(F#:L#)", "435.875"},                         // T01 cột M "LŨY KẾ" = tổng tuần (ô trống bỏ qua)
		{"SUM(F#:L#)+E#", "1615.875"},                      // dấu = đầu tùy chọn
		{"F#+H#", "100.25"},                                // ô trống = 0 trong phép cộng
		{"E#-D#", "-20.5"},                                 // âm
		{"G#*2", "501"},                                    // nhân
		{"F#/4", "25.0625"},                                // chia chính xác (không float)
		{"1/3", "0.33333333333333333333"},                  // chia: 20 chữ số thập phân
		{"ROUND(1/3,2)", "0.33"},                           // ROUND
		{"ROUND(2.5,0)", "3"},                              // nửa xa số 0 (Excel)
		{"ROUND(-2.5,0)", "-3"},                            //
		{"ROUND(1234.5678,-2)", "1200"},                    // ROUND chữ số âm
		{"ROUNDUP(1.231,2)", "1.24"},                       //
		{"ROUNDUP(-1.231,2)", "-1.24"},                     //
		{"ROUNDDOWN(1.239,2)", "1.23"},                     //
		{"ROUNDDOWN(-1239,-2)", "-1200"},                   //
		{"IF(E#=0,0,ROUND(SUM(F#:L#)/E#*100,2))", "36.94"}, // % so kế hoạch
		{"IF(J#=0,0,F#/J#)", "0"},                          // năng suất = sản lượng/diện tích, chặn chia 0
		{"F#/J#", "#DIV/0!"},                               // lỗi Excel
		{"IFERROR(F#/J#,-1)", "-1"},                        //
		{"MIN(F#:L#)", "0"},                                //
		{"MAX(F#:L#,300)", "300"},                          //
		{"AVERAGE(F#:I#)", "141.95833333333333333333"},
		{"COUNT(F#:L#)", "5"},                   //
		{"IF(D#>E#,\"thiếu\",\"đủ\")", "thiếu"}, // chuỗi
		{"AND(D#>0,E#>0)", "TRUE"},              //
		{"OR(H#>0,K#>0)", "FALSE"},              // ô trống so 0
		{"NOT(ISBLANK(H#))", "FALSE"},           //
		{"ISNUMBER(C#)", "FALSE"},               //
		{"H#=0", "TRUE"},                        // trống = 0
		{"H#=\"\"", "TRUE"},                     // trống = ""
		{"C#>1000000", "TRUE"},                  // chuỗi > số (thứ tự Excel)
		{"\"abc\"=\"ABC\"", "TRUE"},             // không phân biệt hoa thường
		{"50%", "0.5"},                          //
		{"-2^2", "4"},                           // dấu âm trước mũ như Excel
		{"2^3^2", "64"},                         // mũ kết hợp trái
		{"2^-2", "0.25"},                        //
		{"1+2*3-4/2", "5"},                      // ưu tiên
		{"(1+2)*3", "9"},                        //
		{"\"a\"&1+1", "a2"},                     // & thấp hơn +
		{"B#+1", "#VALUE!"},                     // chuỗi không phải số
		{"SUM(B#:C#)", "0"},                     // SUM bỏ qua chuỗi trong dải
		{"SUM(\"5\",1)", "6"},                   // đối số trực tiếp được ép
		{"ABS(E#-D#)<=0.5", "FALSE"},            //
		{"IF(F#>100,G#)", "250.5"},              //
		{"IF(F#>1000,G#)", "FALSE"},             // IF hai đối số
		{"IF(TRUE,H#)", "0"},                    // ô trống trả từ IF hiển thị 0
		{"SUM(F#:L#)-SUM(F#,G#,I#,L#)", "0"},
	}
	for _, c := range cases {
		if got := eval(t, c.src, t01Row); got != c.want {
			t.Errorf("%s = %s, muốn %s", c.src, got, c.want)
		}
	}
}

// TestFN_AD_04_06_FormulaSyntax: công thức sai cú pháp hoặc không thuộc tập con bị từ chối.
func TestFN_AD_04_06_FormulaSyntax(t *testing.T) {
	for _, src := range []string{"", "=", "SUM(F7:L7)", "F7+1", "VLOOKUP(A#,B#,2)", "SUM(F#:L#", "1+", "\"abc", "F#:", "XREF(T01,M)", "A1:B2", "$F$7", "ROUND(1)", "@F#"} {
		if _, err := formula.Parse(src); err == nil {
			t.Errorf("%q phải lỗi cú pháp", src)
		}
	}
}

// TestFN_AD_04_06_ColumnLetters: chữ cái cột Excel ↔ số thứ tự (T04 tới cột HR).
func TestFN_AD_04_06_ColumnLetters(t *testing.T) {
	for _, c := range []struct {
		l string
		n int
	}{{"A", 1}, {"Z", 26}, {"AA", 27}, {"AZ", 52}, {"HR", 226}, {"XFD", 16384}} {
		if formula.ColumnIndex(c.l) != c.n || formula.ColumnLetter(c.n) != c.l {
			t.Errorf("%s ↔ %d sai: %d %s", c.l, c.n, formula.ColumnIndex(c.l), formula.ColumnLetter(c.n))
		}
	}
	if formula.ColumnIndex("XFE") != 0 || formula.ColumnIndex("A1") != 0 || formula.ColumnLetter(0) != "" {
		t.Fatal("cột ngoài phạm vi phải bị từ chối")
	}
}

// TestFN_AD_04_06_ShiftLikeExcel: chèn/xóa cột dời tham chiếu như Excel; xóa cột đang
// được tham chiếu → ErrRefDeleted (trình thiết kế báo reports.formula_invalid_ref).
func TestFN_AD_04_06_ShiftLikeExcel(t *testing.T) {
	at := func(l string) int { return formula.ColumnIndex(l) }
	cases := []struct {
		src   string
		at    int
		delta int
		want  string
	}{
		{"SUM(F#:L#)", at("G"), 1, "SUM(F#:M#)"}, // chèn giữa dải → nở
		{"SUM(F#:L#)", at("F"), 1, "SUM(G#:M#)"}, // chèn trước dải → dời
		{"SUM(F#:L#)", at("M"), 1, "SUM(F#:L#)"}, // chèn sau → giữ
		{"IF(E#=0,0,M#/E#)", at("A"), 1, "IF(F#=0,0,N#/F#)"},
		{"SUM(F#:L#)", at("G"), -1, "SUM(F#:K#)"}, // xóa trong dải → co
		{"SUM(F#:L#)", at("F"), -1, "SUM(F#:K#)"}, // xóa đầu dải
		{"SUM(F#:L#)+N#", at("M"), -1, "SUM(F#:L#)+M#"},
		{"XREF(\"T01\",\"M\")-E#", at("A"), 1, "XREF(\"T01\",\"M\")-F#"}, // cột mẫu khác không dời
		{"-(2^2)", at("A"), 1, "-(2^2)"},
		{"1-(2-3)", at("A"), 1, "1-(2-3)"},
	}
	for _, c := range cases {
		got, err := formula.ShiftString(c.src, c.at, c.delta)
		if err != nil || got != c.want {
			t.Errorf("Shift(%s,%d,%d) = %s %v, muốn %s", c.src, c.at, c.delta, got, err, c.want)
		}
	}
	for _, src := range []string{"M#/E#", "SUM(F#:F#)"} {
		l := "E"
		if strings.HasPrefix(src, "SUM") {
			l = "F"
		}
		if _, err := formula.ShiftString(src, at(l), -1); !errors.Is(err, formula.ErrRefDeleted) {
			t.Errorf("xóa cột %s được %s tham chiếu phải lỗi, nhận %v", l, src, err)
		}
	}
}

// TestFN_AD_04_06_FormulaMatchesExcelize: bộ ca thử vàng đối chiếu với bộ tính công thức
// của excelize (kn-v7.7-plan rủi ro "Công thức Excel khác kết quả"). IF hai đối số khi
// sai: Excel trả FALSE, excelize trả rỗng — ca đó chỉ có trong bộ vàng ở trên. Mỗi công thức
// được ghi vào một tệp .xlsx đúng như mẫu Excel (dòng 7) rồi so kết quả.
func TestFN_AD_04_06_FormulaMatchesExcelize(t *testing.T) {
	const dataRow = 7
	x := excelize.NewFile()
	defer func() { _ = x.Close() }()
	sh := x.GetSheetName(0)
	for l, v := range t01Row {
		cell := l + "7"
		if v == "" {
			continue
		}
		if _, err := decimal.NewFromString(v); err == nil {
			// Ô số ghi nguyên văn (không qua float — R-10).
			if err := x.SetCellDefault(sh, cell, v); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := x.SetCellStr(sh, cell, v); err != nil {
			t.Fatal(err)
		}
	}
	srcs := []string{
		"SUM(F#:L#)", "SUM(F#:L#)+E#", "F#+H#", "E#-D#", "G#*2", "F#/4", "ROUND(1/3,2)", "ROUND(2.5,0)", "ROUND(-2.5,0)",
		"ROUND(1234.5678,-2)", "ROUNDUP(1.231,2)", "ROUNDDOWN(1.239,2)", "IF(E#=0,0,ROUND(SUM(F#:L#)/E#*100,2))",
		"IF(J#=0,0,F#/J#)", "MIN(F#:L#)", "MAX(F#:L#,300)", "COUNT(F#:L#)", "AND(D#>0,E#>0)", "OR(H#>0,K#>0)",
		"H#=0", "50%", "2^3", "1+2*3-4/2", "SUM(B#:C#)", "ABS(E#-D#)", "IF(F#>100,G#)", "D#*1.1-E#/3", "F#/J#",
		"IFERROR(F#/J#,-1)", "ROUND(G#/F#,4)", "NOT(ISBLANK(H#))",
	}
	col := 30 // AD trở đi: ô chứa công thức
	for _, src := range srcs {
		p, err := formula.Compile(src)
		if err != nil {
			t.Fatal(err)
		}
		cell := formula.ColumnLetter(col) + "7"
		col++
		if err := x.SetCellFormula(sh, cell, formula.ToExcel(p.Root, dataRow)); err != nil {
			t.Fatal(err)
		}
		want, err := x.CalcCellValue(sh, cell, excelize.Options{RawCellValue: true})
		if err != nil && want == "" {
			want = err.Error()
		}
		got := p.Run(t01Row)
		if !sameAsExcel(got, want) {
			t.Errorf("%s (Excel %s): ta %s, excelize %s", src, formula.ToExcel(p.Root, dataRow), got.String(), want)
		}
	}
}

// sameAsExcel: số so tới 1e-9 tương đối (excelize tính bằng float64, ta bằng decimal);
// logic và mã lỗi so đúng chữ.
func sameAsExcel(got formula.Value, excel string) bool {
	excel = strings.TrimSpace(excel)
	switch got.Kind {
	case formula.Number:
		d, err := decimal.NewFromString(excel)
		if err != nil {
			return false
		}
		diff := d.Sub(got.Num).Abs()
		tol := decimal.New(1, -9).Mul(decimal.Max(decimal.NewFromInt(1), got.Num.Abs()))
		return diff.LessThanOrEqual(tol)
	case formula.Boolean:
		return strings.EqualFold(excel, got.String())
	case formula.Error:
		return strings.Contains(excel, got.Err)
	}
	return excel == got.String()
}
