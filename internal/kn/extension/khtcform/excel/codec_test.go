package excel_test

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/excel"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

func snapshot() port.KHTCFormExcelSnapshot {
	return port.KHTCFormExcelSnapshot{
		ID: uuid.MustParse("019a3fb0-0000-7000-8000-000000000001"), TenantID: uuid.MustParse("019a3fb0-0000-7000-8000-000000000002"),
		PeriodID: uuid.MustParse("019a3fb0-0000-7000-8000-000000000003"), Department: "trong_trot",
		Sheets: []port.KHTCFormExcelSheet{{Key: "program_iii", Name: "CT III", Kind: "grid", TemplateID: uuid.MustParse("019a3fb0-0000-7000-8000-000000000004"), TemplateVersion: 7,
			TemplateHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AllowAdjustments: true,
			Columns: []port.KHTCFormExcelColumn{
				{Letter: "A", Title: "Hạng mục", Kind: "fixed", DataType: "text"},
				{Letter: "B", Title: "KH năm", Kind: "from_plan", DataType: "number"},
				{Letter: "C", Title: "Thực hiện trong kỳ", Kind: "input", DataType: "number", Required: true, NonNegative: true},
				{Letter: "D", Title: "Lũy kế", Kind: "formula", DataType: "number", FormulaMode: "server_value"},
				{Letter: "E", Title: "Nội dung", Kind: "input", DataType: "text", MaxLength: 100},
			}, Rows: []port.KHTCFormExcelRow{
				{Code: "III.01", SystemValues: map[string]string{"A": "Tên trùng", "B": "100", "D": "0"}},
				{Code: "III.02", SystemValues: map[string]string{"A": "Tên trùng", "B": "200", "D": "0"}},
			}}},
		Periods: []port.KHTCFormExcelPeriod{{ID: uuid.MustParse("019a3fb0-0000-7000-8000-000000000005"), Name: "Tuần đã khóa"}},
	}
}

func content() port.KHTCFormExcelContent {
	return port.KHTCFormExcelContent{Rows: map[string]map[string]map[string]string{"program_iii": {
		"III.01": {"C": "1", "E": "=literal text"}, "III.02": {"C": "2"},
	}}}
}

func changeWorkbook(t *testing.T, raw []byte, change func(*excelize.File)) []byte {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	change(f)
	out, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestRoundTripMatchesItemCodeAndKeepsUnsavedValues(t *testing.T) {
	codec, err := excel.New(snapshot())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := codec.Generate(content())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	raw = changeWorkbook(t, raw, func(f *excelize.File) {
		// A name and position cannot identify a row: both names are identical.
		for _, edit := range []struct{ cell, value string }{{"F4", "III.02"}, {"C4", "20"}, {"F5", "III.01"}, {"C5", "10"}, {"B5", "999"}} {
			if err := f.SetCellStr("CT III", edit.cell, edit.value); err != nil {
				t.Fatal(err)
			}
		}
	})
	preview, err := codec.Preview(raw)
	if err != nil {
		t.Fatal(err)
	}
	report := preview.Result()
	if report.ValidRows != 2 || len(report.Issues) == 0 {
		t.Fatalf("expected two valid rows with ignored locked-cell warnings: %#v", report)
	}
	merged, err := codec.Merge(content(), preview, "replace")
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.Rows["program_iii"]["III.01"]["C"]; got != "10" {
		t.Fatalf("III.01 must follow code, got %q", got)
	}
	if got := merged.Rows["program_iii"]["III.02"]["C"]; got != "20" {
		t.Fatalf("III.02 must follow code, got %q", got)
	}
	if _, exists := merged.Rows["program_iii"]["III.01"]["B"]; exists {
		t.Fatal("locked system value entered editable content")
	}
}

func TestInvalidRowDoesNotExcludeValidRows(t *testing.T) {
	codec, err := excel.New(snapshot())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := codec.Generate(content())
	if err != nil {
		t.Fatal(err)
	}
	raw = changeWorkbook(t, raw, func(f *excelize.File) {
		if err := f.SetCellStr("CT III", "C4", "1e100000"); err != nil {
			t.Fatal(err)
		}
	})
	preview, err := codec.Preview(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := preview.Result().ValidRows; got != 1 {
		t.Fatalf("valid rows = %d, want 1", got)
	}
	merged, err := codec.Merge(port.KHTCFormExcelContent{}, preview, "replace")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := merged.Rows["program_iii"]["III.01"]; exists {
		t.Fatal("invalid numeric row imported")
	}
	if got := merged.Rows["program_iii"]["III.02"]["C"]; got != "2" {
		t.Fatalf("valid row lost: %q", got)
	}
}
