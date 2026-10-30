package app_test

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/xuri/excelize/v2"
)

func TestW1168WordAndExcelUseFrozenSelectedFigures(t *testing.T) {
	d := wordDraft("adhoc", map[string]string{"place": "An Giang", "report_date": "2026-12-30", "recipient": "Sở Nông nghiệp"})
	d.Adhoc = &domain.AdhocSelection{Rows: []string{d.Data.Rows[1].Ref.String()}, Metrics: []string{}}
	d.Texts["noi_dung"] = "Báo cáo theo chỉ đạo."
	body, err := app.RenderXLSX(d, domain.Filter{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	text := ""
	for _, sheet := range file.GetSheetList() {
		rows, e := file.GetRows(sheet)
		if e != nil {
			t.Fatal(e)
		}
		for _, r := range rows {
			text += strings.Join(r, "|") + "\n"
		}
	}
	if !strings.Contains(text, "Mô hình lúa") || !strings.Contains(text, "Báo cáo theo chỉ đạo.") || strings.Contains(text, "Tập huấn") {
		t.Fatal("Excel did not use selected figures")
	}
	word, err := doctpl.RenderDOCX(app.WordTemplate("adhoc"), app.WordData(d, domain.Filter{}, 1))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(word), int64(len(word)))
	if err != nil {
		t.Fatal(err)
	}
	xml := ""
	for _, f := range archive.File {
		if f.Name == "word/document.xml" {
			r, e := f.Open()
			if e != nil {
				t.Fatal(e)
			}
			b, e := io.ReadAll(io.LimitReader(r, 4<<20))
			_ = r.Close()
			if e != nil {
				t.Fatal(e)
			}
			xml = string(b)
		}
	}
	if !strings.Contains(xml, "Mô hình lúa") || !strings.Contains(xml, "Báo cáo theo chỉ đạo.") || strings.Contains(xml, "Tập huấn") {
		t.Fatal("Word differs from selected frozen Excel")
	}
	d.Adhoc = nil
	if _, err := app.RenderXLSX(d, domain.Filter{}, 2); err == nil {
		t.Fatal("export without selection accepted")
	}
}
