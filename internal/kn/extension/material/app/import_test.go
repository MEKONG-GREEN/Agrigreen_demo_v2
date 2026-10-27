package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

var importCatalog = []mdport.CatalogItem{
	{Catalog: mdport.CatalogMaterial, Code: "PHAN_URE", Name: "Phân Urê", Attrs: map[string]string{"unit": "kg"}, Active: true},
	{Catalog: mdport.CatalogMaterial, Code: "GIONG_BAO", Name: "Giống lúa (bao)", Attrs: map[string]string{"unit": "bao"}, Active: true},
	{Catalog: mdport.CatalogMaterial, Code: "CU", Name: "Thuốc cũ", Attrs: map[string]string{"unit": "chai"}, Active: false},
	{Catalog: mdport.CatalogMaterial, Code: "KHONG_DVT", Name: "Không ĐVT", Attrs: map[string]string{}, Active: true},
}

var importToday = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func readCSV(t *testing.T, s string) *tabular.Table {
	t.Helper()
	tb, err := tabular.ReadBytes([]byte(s), tabular.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

func TestParseImportTable_LinesAndReasons(t *testing.T) {
	csv := strings.Join([]string{
		"Vật tư,Số lượng,ĐVT,Thời điểm,Ghi chú",
		"phan_ure,\"1.234,5\",kg,30/09/2026,Vụ Đông Xuân", // mã không phân biệt hoa thường, số kiểu VN
		"Giống lúa (bao),10,,2026-09-29,",                 // theo tên danh mục
		"Giống lúa (bao),2.5,,,",                          // ĐVT đếm được → số nguyên
		"Thuốc cũ,1,,,",                                   // ngừng dùng
		"KHONG_DVT,1,,,",                                  // mục chưa có ĐVT không nhận
		"PHAN_URE,-1,,,",                                  // số âm
		"PHAN_URE,5,lít,,",                                // ĐVT khác danh mục
		"PHAN_URE,5,,02/10/2026,",                         // tương lai
		"PHAN_URE,abc,,,Gọi 0912345678",                   // không phải số + PII trong ghi chú
		",,,,ghi chú lẻ",                                  // thiếu cả vật tư và số lượng
	}, "\n")
	res, err := ParseImportTable(readCSV(t, csv), importCatalog, importToday)
	if err != nil {
		t.Fatal(err)
	}
	if res.Accepted != 2 || res.Rejected != 8 || len(res.Lines) != 10 {
		t.Fatalf("đếm: %+v", res)
	}
	l0 := res.Lines[0]
	if l0.Row != 2 || l0.MaterialCode != "PHAN_URE" || l0.Unit != "kg" || l0.Quantity != "1234.5" || l0.RecordedOn != "2026-09-30" || l0.Note != "Vụ Đông Xuân" || l0.Status != ImportAccepted {
		t.Fatalf("dòng 1: %+v", l0)
	}
	if l1 := res.Lines[1]; l1.MaterialCode != "GIONG_BAO" || l1.Quantity != "10" || l1.RecordedOn != "2026-09-29" {
		t.Fatalf("dòng 2: %+v", l1)
	}
	wantReason := map[int]string{
		2: "số nguyên", 3: "không có trong danh mục", 4: "không có trong danh mục", 5: "không được âm",
		6: "đơn vị tính khác danh mục", 7: "tương lai", 8: "không phải số", 9: "thiếu vật tư",
	}
	for i, frag := range wantReason {
		l := res.Lines[i]
		if l.Status != ImportRejected || !strings.Contains(l.Reason, frag) {
			t.Fatalf("dòng %d: muốn lý do chứa %q, nhận %+v", i+1, frag, l)
		}
	}
	// Dòng bị loại vẫn giữ vật tư nhận ra (để sửa trong bảng); lý do không lặp lại giá trị nhập.
	if l := res.Lines[2]; l.MaterialCode != "GIONG_BAO" || l.Quantity != "" {
		t.Fatalf("dòng bị loại giữ vật tư: %+v", l)
	}
	if l := res.Lines[8]; !strings.Contains(l.Reason, "ghi chú") || strings.Contains(l.Reason, "0912345678") || l.Note != "" || strings.Contains(l.Reason, "abc") {
		t.Fatalf("PII/giá trị nhập trong lý do: %+v", l)
	}
	if l := res.Lines[9]; !strings.Contains(l.Reason, "thiếu số lượng") {
		t.Fatalf("thiếu số lượng: %+v", l)
	}
}

func TestParseImportTable_MissingColumns(t *testing.T) {
	_, err := ParseImportTable(readCSV(t, "Ghi chú,Thời điểm\nx,2026-09-01\n"), importCatalog, importToday)
	var p *problem.Problem
	if !errors.As(err, &p) || p.Code != "extension.material_import_invalid" || !strings.Contains(p.Detail, "Vật tư, Số lượng") {
		t.Fatalf("thiếu cột: %v", err)
	}
}
