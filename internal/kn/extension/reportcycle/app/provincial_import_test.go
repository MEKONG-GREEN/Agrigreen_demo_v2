package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"

	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

func pvForm(code string, cols ...repport.FormColumn) repport.FormVersion {
	return repport.FormVersion{TemplateID: uuid.New(), Code: code, Definition: repport.FormDefinition{Columns: cols}}
}

func readPV(t *testing.T, csv string) *tabular.Table {
	t.Helper()
	tb, err := tabular.ReadBytes([]byte(csv), tabular.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

// UR-TI-02 / FN-TI-02.02 Bước 4: nạp Tỉnh giao từ tệp (W11.02).
func TestFN_TI_02_02_ProvincialImport(t *testing.T) {
	t01 := pvForm("T01",
		repport.FormColumn{Letter: "C", Title: "Tên", Kind: "input", DType: "text"},
		repport.FormColumn{Letter: "E", Title: "Diện tích giao", Kind: repport.ColumnProvincial, DType: repport.DTypeNumber},
		repport.FormColumn{Letter: "F", Title: "Số HTX giao", Kind: repport.ColumnProvincial, DType: repport.DTypeInteger})
	t04 := pvForm("T04", repport.FormColumn{Letter: "D", Title: "Kế hoạch", Kind: repport.ColumnProvincial, DType: repport.DTypeNumber})
	csv := "Mã xã,Tên xã,T01 E (ha),T01_F,T04 cột D,Ghi chú\n" +
		"30280,Mỹ Hòa Hưng,12.50,3,7,x\n" +
		"30281,Bình Đức,-1,,,\n" +
		"30282,Mỹ Thới,,2.5,,\n" +
		"30280,trùng,1,1,1,\n" +
		",thiếu,1,,,\n" +
		"30283,Mỹ Khánh,,,1 000,\n"
	res, err := parseProvincial(readPV(t, csv), []repport.FormVersion{t01, t04})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Columns) != 3 || len(res.Ignored) != 1 || res.Ignored[0] != "Ghi chú" {
		t.Fatalf("cột: %+v bỏ qua %v", res.Columns, res.Ignored)
	}
	if res.Accepted != 2 || res.Rejected != 4 {
		t.Fatalf("đếm: %+v", res.Rows)
	}
	if got := res.Values[t01.TemplateID]["30280"]; got["E"] != "12.5" || got["F"] != "3" {
		t.Fatalf("T01 30280: %v", got)
	}
	if got := res.Values[t04.TemplateID]["30283"]["D"]; got != "1000" {
		t.Fatalf("T04 30283: %q", got)
	}
	for _, r := range res.Rows {
		if r.Status == PVRowRejected && (r.Reason == "" || strings.Contains(r.Reason, "-1")) {
			t.Fatalf("lý do dòng không được rỗng hay chứa giá trị nhập: %+v", r)
		}
	}
	// Một biểu: tiêu đề chỉ chữ cột cũng nhận.
	one, err := parseProvincial(readPV(t, "ma_xa,E,cot_f\n30280,1,2\n"), []repport.FormVersion{t01})
	if err != nil || one.Values[t01.TemplateID]["30280"]["F"] != "2" {
		t.Fatalf("một biểu: %+v %v", one, err)
	}
	// Sai mẫu: thiếu Mã xã, không có cột Tỉnh giao nào, cột trùng.
	for _, bad := range []string{"T01 E\n1\n", "Mã xã,Khác\n1,2\n", "Mã xã,T01 E,T01_E_dien_tich\n1,2,3\n"} {
		if _, err := parseProvincial(readPV(t, bad), []repport.FormVersion{t01, t04}); problem.From(err).Code != ErrProvincialImportInvalid.Code {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	// Ô công thức trong .xlsx → cả tệp bị từ chối (tabular).
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, "Tinh giao", []string{"Mã xã", "T01 E"}, [][]string{{"30280", "5"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := parseProvincial(readPV(t, "Mã xã,T01 E\n"), []repport.FormVersion{t01}); err != nil {
		t.Fatalf("tệp không dòng dữ liệu vẫn hợp lệ: %v", err)
	}
	tb, err := tabular.ReadBytes(buf.Bytes(), tabular.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := parseProvincial(tb, []repport.FormVersion{t01}); err != nil || res.Values[t01.TemplateID]["30280"]["E"] != "5" {
		t.Fatalf("xlsx: %+v %v", res, err)
	}
}
