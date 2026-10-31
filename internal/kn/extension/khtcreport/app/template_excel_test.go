package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

func TestFixedWorkbookAllFivePreserveNumbersAndInertText(t *testing.T) {
	for _, key := range templatedomain.Keys {
		t.Run(key, func(t *testing.T) {
			draft, source := fixedFixture(key)
			draft.Data.Period.Name = "Kỳ đã đóng băng"
			planned, percent := "10", "70"
			draft.Data.Rows = []domain.ProgramRow{{Ref: uuid.New(), Kind: "item", Name: "Mô hình đã chụp", InPeriod: "7", YearToDate: "7", Planned: &planned, Percent: &percent, Unit: "ha", Department: "trong_trot", Content: "Nội dung nguồn"}}
			definition := templatedomain.Seed(key)
			definition.Nodes = append(definition.Nodes, templatedomain.Node{ID: "excel_memo", Parent: definition.Nodes[0].ID, Order: 100, Title: "Mục Excel bổ sung", Repeat: "none", Body: "{van}", TextSlot: true, Bindings: []templatedomain.Binding{{Variable: "van", Source: "text", Field: "narrative"}}})
			draft.Texts["excel_memo"] = "=HYPERLINK(\"https://example.invalid\",\"giữ nguyên văn\")"
			raw, hash, err := definition.BytesHash()
			if err != nil {
				t.Fatal(err)
			}
			source.Definition, source.Hash = raw, hash
			pinned, err := selectFixedTemplate(context.Background(), &fixedProvider{selected: source}, draft)
			if err != nil {
				t.Fatal(err)
			}
			base, err := RenderXLSX(draft, domain.Filter{}, 2)
			if err != nil {
				t.Fatal(err)
			}
			body, err := fixedWorkbook(base, pinned, draft, "", domain.Filter{}, 2)
			if err != nil {
				t.Fatal(err)
			}
			file, err := excelize.OpenReader(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			if got := file.GetSheetList()[0]; got != "Báo cáo" {
				t.Fatalf("main sheet %s", got)
			}
			for _, sheet := range []string{"Báo cáo", "Chi tiết số liệu", "Số liệu nguồn", "Mốc dữ liệu"} {
				if i, e := file.GetSheetIndex(sheet); e != nil || i < 0 {
					t.Fatal("missing sheet", sheet, e)
				}
			}
			rows, err := file.GetRows("Báo cáo")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for i, row := range rows {
				for j, value := range row {
					if value == draft.Texts["excel_memo"] {
						found = true
						cell, _ := excelize.CoordinatesToCellName(j+1, i+1)
						formula, e := file.GetCellFormula("Báo cáo", cell)
						if e != nil || formula != "" {
							t.Fatal("template became formula", formula, e)
						}
					}
				}
			}
			if !found {
				t.Fatal("compiled text missing")
			}
			detailSheet := "Chi tiết số liệu"
			if key == "mau_3" || key == "mau_4" || key == "mau_5" {
				detailSheet = "Số liệu chương trình"
			}
			details, _ := file.GetRows(detailSheet)
			found = false
			for i, row := range details {
				if len(row) > 5 && row[1] == "Mô hình đã chụp" {
					cell, _ := excelize.CoordinatesToCellName(6, i+1)
					kind, e := file.GetCellType(detailSheet, cell)
					if e != nil || kind == excelize.CellTypeSharedString || kind == excelize.CellTypeInlineString {
						t.Fatal("numeric source became text", cell, kind, e)
					}
					found = true
				}
			}
			if !found {
				t.Fatal("numeric program detail lost")
			}
			provenance, _ := file.GetRows("Mốc dữ liệu")
			encoded := []string{}
			for _, row := range provenance {
				encoded = append(encoded, strings.Join(row, "|"))
			}
			if !strings.Contains(strings.Join(encoded, "\n"), pinned.Hash) {
				t.Fatal("pinned provenance missing")
			}
			old, err := fixedWorkbook(base, nil, draft, "", domain.Filter{}, 2)
			if err != nil || !bytes.Equal(base, old) {
				t.Fatal("historical nil bytes changed", err)
			}
			bad := *pinned
			bad.Hash = "mismatch"
			if _, err = fixedWorkbook(base, &bad, draft, "", domain.Filter{}, 2); err == nil {
				t.Fatal("bad hash fell back")
			}
		})
	}
}
