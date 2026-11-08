package excel

import (
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
)

func TestPreflightAcceptsGeneratedXLSX(t *testing.T) {
	c, err := New(port.KHTCFormExcelSnapshot{ID: uuid.New(), TenantID: uuid.New(), PeriodID: uuid.New(), Department: "trong_trot", Sheets: []port.KHTCFormExcelSheet{{Key: "ct", Name: "CT", Kind: "grid", TemplateID: uuid.New(), TemplateVersion: 1, TemplateHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Columns: []port.KHTCFormExcelColumn{{Letter: "A", Title: "Nhập", Kind: "input", DataType: "text"}}, Rows: []port.KHTCFormExcelRow{{Code: "01"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := c.Generate(port.KHTCFormExcelContent{})
	if err != nil {
		t.Fatal(err)
	}
	if err := preflight(raw); err != nil {
		t.Fatal(err)
	}
}
