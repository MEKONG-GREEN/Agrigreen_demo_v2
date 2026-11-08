package excel_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/excel"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

func mustCodec(t *testing.T, s port.KHTCFormExcelSnapshot) *excel.Codec {
	t.Helper()
	c, err := excel.New(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func mustWorkbook(t *testing.T, c *excel.Codec, values port.KHTCFormExcelContent) []byte {
	t.Helper()
	raw, err := c.Generate(values)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func mustPreview(t *testing.T, c *excel.Codec, raw []byte) port.KHTCFormExcelPreview {
	t.Helper()
	p, err := c.Preview(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func put(t *testing.T, f *excelize.File, sheet, ref, value string) {
	t.Helper()
	if err := f.SetCellStr(sheet, ref, value); err != nil {
		t.Fatal(err)
	}
}
func hasIssue(p port.KHTCFormExcelPreview, rule, severity string) bool {
	for _, issue := range p.Result().Issues {
		if issue.Code == rule && issue.Severity == severity {
			return true
		}
	}
	return false
}

func TestGeneratorKeepsLettersVisibleCodesAndLiteralText(t *testing.T) {
	c := mustCodec(t, snapshot())
	raw := mustWorkbook(t, c, content())
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	for ref, expected := range map[string]string{"C2": "C", "C3": "Thực hiện trong kỳ", "F3": "Mã hạng mục", "F4": "III.01", "D4": "0", "E4": "=literal text"} {
		value, err := f.GetCellValue("CT III", ref)
		if err != nil || value != expected {
			t.Fatalf("%s = %q, %v; want %q", ref, value, err, expected)
		}
		formula, err := f.GetCellFormula("CT III", ref)
		if err != nil || formula != "" {
			t.Fatalf("value-only projection emitted formula at %s", ref)
		}
	}
	visible, err := f.GetColVisible("CT III", "F")
	if err != nil || !visible {
		t.Fatal("the only item-code column must be visible")
	}
}

func TestInsertedRowsAndNarrativeUseStableIdentities(t *testing.T) {
	s := snapshot()
	s.Sheets = append(s.Sheets, port.KHTCFormExcelSheet{Key: "words", Name: "Lời văn", Kind: "narrative", TemplateID: uuid.MustParse("019a3fb0-0000-7000-8000-000000000009"), TemplateVersion: 2, TemplateHash: strings.Repeat("b", 64), Columns: []port.KHTCFormExcelColumn{{Letter: "A", Title: "Mục", Kind: "fixed", DataType: "text"}, {Letter: "C", Title: "Nội dung", Kind: "input", DataType: "text", Required: true}}, Rows: []port.KHTCFormExcelRow{{Code: "section_one", SystemValues: map[string]string{"A": "Tên trùng"}}, {Code: "section_two", SystemValues: map[string]string{"A": "Tên trùng"}}}})
	values := content()
	values.Rows["words"] = map[string]map[string]string{"section_one": {"C": "Một"}, "section_two": {"C": "Hai"}}
	c := mustCodec(t, s)
	raw := mustWorkbook(t, c, values)
	raw = changeWorkbook(t, raw, func(f *excelize.File) {
		if err := f.InsertRows("CT III", 4, 1); err != nil {
			t.Fatal(err)
		}
		put(t, f, "CT III", "C5", "7.123456")
		put(t, f, "Lời văn", "D4", "section_two")
		put(t, f, "Lời văn", "C4", "Nội dung 2")
		put(t, f, "Lời văn", "D5", "section_one")
		put(t, f, "Lời văn", "C5", "Nội dung 1")
	})
	p := mustPreview(t, c, raw)
	if p.Result().ValidRows != 4 {
		t.Fatalf("report: %#v", p.Result())
	}
	merged, err := c.Merge(port.KHTCFormExcelContent{}, p, "replace")
	if err != nil {
		t.Fatal(err)
	}
	if merged.Rows["program_iii"]["III.01"]["C"] != "7.123456" || merged.Rows["words"]["section_one"]["C"] != "Nội dung 1" || merged.Rows["words"]["section_two"]["C"] != "Nội dung 2" {
		t.Fatalf("identity mapping: %#v", merged)
	}
}

func TestLockedChangesAndFormulasAreIgnoredWithoutEvaluation(t *testing.T) {
	s := snapshot()
	s.Sheets[0].Rows[1].Locked = []string{"C"}
	s.Sheets[0].Rows[1].SystemValues["C"] = "42"
	s.Sheets[0].Rows[1].AdjustmentLocked = true
	values := content()
	delete(values.Rows["program_iii"]["III.02"], "C")
	c := mustCodec(t, s)
	raw := mustWorkbook(t, c, values)
	raw = changeWorkbook(t, raw, func(f *excelize.File) {
		put(t, f, "CT III", "C5", "-999")
		if err := f.SetCellFormula("CT III", "D4", `1/0`); err != nil {
			t.Fatal(err)
		}
		put(t, f, "CT III", "A4", "Changed display name")
	})
	p := mustPreview(t, c, raw)
	if p.Result().ValidRows != 2 || !hasIssue(p, "locked", "ignored") {
		t.Fatalf("report: %#v", p.Result())
	}
	merged, err := c.Merge(port.KHTCFormExcelContent{}, p, "replace")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := merged.Rows["program_iii"]["III.02"]["C"]; ok {
		t.Fatal("module input was imported")
	}
	if _, ok := merged.Rows["program_iii"]["III.01"]["D"]; ok {
		t.Fatal("uploaded formula was imported")
	}
}

func TestEditableFormulasExcludeOnlyTheirRow(t *testing.T) {
	for _, cell := range []string{"C4", "E4"} {
		t.Run(cell, func(t *testing.T) {
			c := mustCodec(t, snapshot())
			raw := mustWorkbook(t, c, content())
			raw = changeWorkbook(t, raw, func(f *excelize.File) {
				if err := f.SetCellFormula("CT III", cell, `HYPERLINK("https://invalid.example/","x")`); err != nil {
					t.Fatal(err)
				}
			})
			p := mustPreview(t, c, raw)
			if p.Result().ValidRows != 1 || !hasIssue(p, "formula", "error") {
				t.Fatalf("report: %#v", p.Result())
			}
		})
	}
}

func TestNumericBoundsBeforeParsingAndExactPrecision(t *testing.T) {
	for _, value := range []string{"1e100000", "1e-100000", strings.Repeat("9", 10000), "1000000000000000", "0.0000001", "1,2", "NaN", "+1", "-1"} {
		t.Run(value[:min(len(value), 24)], func(t *testing.T) {
			c := mustCodec(t, snapshot())
			raw := changeWorkbook(t, mustWorkbook(t, c, content()), func(f *excelize.File) { put(t, f, "CT III", "C4", value) })
			p := mustPreview(t, c, raw)
			if p.Result().ValidRows != 1 {
				t.Fatalf("hostile numeric row imported: %q", value[:min(len(value), 24)])
			}
		})
	}
	for _, value := range []string{"999999999999999.123456", "0.000001", "-0.000000"} {
		t.Run(value, func(t *testing.T) {
			c := mustCodec(t, snapshot())
			raw := changeWorkbook(t, mustWorkbook(t, c, content()), func(f *excelize.File) { put(t, f, "CT III", "C4", value) })
			p := mustPreview(t, c, raw)
			merged, err := c.Merge(port.KHTCFormExcelContent{}, p, "replace")
			if err != nil || merged.Rows["program_iii"]["III.01"]["C"] != value {
				t.Fatalf("decimal precision lost: %#v, %v", merged, err)
			}
		})
	}
	s := snapshot()
	s.Sheets[0].Columns[2].DataType = "integer"
	c := mustCodec(t, s)
	for _, value := range []string{"1000000000", "1.0", "1e1"} {
		raw := changeWorkbook(t, mustWorkbook(t, c, content()), func(f *excelize.File) { put(t, f, "CT III", "C4", value) })
		if !hasIssue(mustPreview(t, c, raw), "integer", "error") {
			t.Fatalf("integer bound accepted %q", value)
		}
	}
}

func TestRequiredChoiceAndUnicodeLength(t *testing.T) {
	s := snapshot()
	s.Sheets[0].Columns[4].Required = true
	s.Sheets[0].Columns[4].MaxLength = 3
	s.Sheets[0].Columns[4].Choices = []string{"đạt", "tốt"}
	c := mustCodec(t, s)
	for _, tc := range []struct{ value, rule string }{{"", "required"}, {"khá", "choice"}, {"đạtt", "length"}} {
		t.Run(tc.rule, func(t *testing.T) {
			values := content()
			values.Rows["program_iii"]["III.01"]["E"] = tc.value
			values.Rows["program_iii"]["III.02"]["E"] = "đạt"
			p := mustPreview(t, c, mustWorkbook(t, c, values))
			if p.Result().ValidRows != 1 || !hasIssue(p, tc.rule, "error") {
				t.Fatalf("report: %#v", p.Result())
			}
		})
	}
	values := content()
	values.Rows["program_iii"]["III.01"]["E"] = "tốt"
	values.Rows["program_iii"]["III.02"]["E"] = "đạt"
	if p := mustPreview(t, c, mustWorkbook(t, c, values)); p.Result().ValidRows != 2 {
		t.Fatal("valid Unicode choices rejected")
	}
}

func TestDuplicateAndUnknownCodesCannotEnterTheEditor(t *testing.T) {
	c := mustCodec(t, snapshot())
	raw := mustWorkbook(t, c, content())
	duplicate := changeWorkbook(t, raw, func(f *excelize.File) { put(t, f, "CT III", "F5", "III.01") })
	p := mustPreview(t, c, duplicate)
	if p.Result().ValidRows != 0 || !hasIssue(p, "duplicate_code", "error") {
		t.Fatalf("duplicate rows survived: %#v", p.Result())
	}
	unknown := changeWorkbook(t, raw, func(f *excelize.File) { put(t, f, "CT III", "F4", "UPLOADED PRIVATE CONTENT") })
	p = mustPreview(t, c, unknown)
	if p.Result().ValidRows != 1 || !hasIssue(p, "item_code", "error") {
		t.Fatal("unknown code imported")
	}
	for _, i := range p.Result().Issues {
		if strings.Contains(i.ItemCode+i.Message, "PRIVATE") {
			t.Fatal("raw unknown value leaked into diagnostic")
		}
	}
}

func TestWrongWorkbookIdentityOrShapeFailsWholeFile(t *testing.T) {
	c := mustCodec(t, snapshot())
	raw := mustWorkbook(t, c, content())
	for _, tc := range []struct {
		name   string
		change func(*excelize.File)
	}{
		{"renamed sheet", func(f *excelize.File) {
			if err := f.SetSheetName("CT III", "Wrong"); err != nil {
				t.Fatal(err)
			}
		}},
		{"extra sheet", func(f *excelize.File) {
			if _, err := f.NewSheet("Extra"); err != nil {
				t.Fatal(err)
			}
		}},
		{"renamed column", func(f *excelize.File) { put(t, f, "CT III", "C3", "Changed") }},
		{"changed letter", func(f *excelize.File) { put(t, f, "CT III", "C2", "D") }},
		{"wrong sheet key", func(f *excelize.File) { put(t, f, "CT III", "C1", "other_program") }},
		{"changed fingerprint", func(f *excelize.File) { put(t, f, "CT III", "B1", strings.Repeat("0", 64)) }},
		{"extra column", func(f *excelize.File) { put(t, f, "CT III", "G4", "extra") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Preview(changeWorkbook(t, raw, tc.change)); !errors.Is(err, excel.ErrWorkbook) {
				t.Fatalf("wrong shape accepted: %v", err)
			}
		})
	}
	for _, change := range []func(*port.KHTCFormExcelSnapshot){func(s *port.KHTCFormExcelSnapshot) { s.PeriodID = uuid.New() }, func(s *port.KHTCFormExcelSnapshot) { s.Department = "hanh_chinh_tong_hop" }, func(s *port.KHTCFormExcelSnapshot) { s.Sheets[0].TemplateVersion++ }} {
		s := snapshot()
		change(&s)
		if _, err := mustCodec(t, s).Preview(raw); !errors.Is(err, excel.ErrWorkbook) {
			t.Fatalf("cross-context workbook accepted: %v", err)
		}
	}
}

type forgedPreview struct{ report port.KHTCFormExcelValidation }

func (f forgedPreview) Result() port.KHTCFormExcelValidation { return f.report }

func TestMergeKeepReplaceOpaquePreviewAndDetachedState(t *testing.T) {
	s := snapshot()
	c := mustCodec(t, s)
	raw := mustWorkbook(t, c, content())
	p := mustPreview(t, c, raw)
	existing := content()
	existing.Rows["program_iii"]["III.01"]["C"] = "9"
	existing.Rows["program_iii"]["III.01"]["E"] = ""
	kept, err := c.Merge(existing, p, "keep")
	if err != nil {
		t.Fatal(err)
	}
	if kept.Rows["program_iii"]["III.01"]["C"] != "9" || kept.Rows["program_iii"]["III.01"]["E"] != "=literal text" {
		t.Fatalf("keep semantics: %#v", kept)
	}
	kept.Rows["program_iii"]["III.01"]["C"] = "MUTATED OUTPUT"
	if existing.Rows["program_iii"]["III.01"]["C"] != "9" {
		t.Fatal("merge aliases existing data")
	}
	report := p.Result()
	report.SnapshotHash = "forged"
	report.Sheets[0].Valid = 0
	report.Issues = append(report.Issues, port.KHTCFormExcelIssue{Message: "forged"})
	replaced, err := c.Merge(existing, p, "replace")
	if err != nil || replaced.Rows["program_iii"]["III.01"]["C"] != "1" {
		t.Fatalf("reported patch changed merge: %#v, %v", replaced, err)
	}
	if p.Result().SnapshotHash != c.SnapshotHash() || p.Result().Sheets[0].Valid != 2 {
		t.Fatal("Result aliases private state")
	}
	for _, other := range []port.KHTCFormExcelPreview{forgedPreview{p.Result()}, mustPreview(t, mustCodec(t, snapshot()), raw)} {
		if _, err := c.Merge(existing, other, "replace"); !errors.Is(err, excel.ErrPreview) {
			t.Fatalf("foreign preview trusted: %v", err)
		}
	}
	if _, err := c.Merge(existing, p, "unexpected"); !errors.Is(err, excel.ErrPreview) {
		t.Fatal("unknown merge mode accepted")
	}
	s.Sheets[0].Rows[0].SystemValues["B"] = "MUTATED OWNER MAP"
	s.Sheets[0].Columns[0].Title = "MUTATED OWNER SLICE"
	if _, err := c.Preview(raw); err != nil {
		t.Fatalf("New did not pin snapshot: %v", err)
	}
	for i := range raw {
		raw[i] = 0
	}
	if _, err := c.Merge(existing, p, "replace"); err != nil {
		t.Fatalf("Preview aliases upload bytes: %v", err)
	}
}

func TestAdjustmentsRequireAllowedPeriodReasonAndPreservePartialRows(t *testing.T) {
	s := snapshot()
	c := mustCodec(t, s)
	values := content()
	period := s.Periods[0].ID
	values.Adjustments = []port.KHTCFormExcelAdjustment{{Sheet: "program_iii", ItemCode: "III.01", PeriodID: period, Amount: "-1.123456", Reason: "Điều chỉnh số đã khóa"}, {Sheet: "program_iii", ItemCode: "III.02", PeriodID: period, Amount: "1", Reason: ""}}
	p := mustPreview(t, c, mustWorkbook(t, c, values))
	if p.Result().ValidRows != 3 || !hasIssue(p, "reason", "error") {
		t.Fatalf("report: %#v", p.Result())
	}
	existing := content()
	existing.Adjustments = []port.KHTCFormExcelAdjustment{{Sheet: "program_iii", ItemCode: "III.01", PeriodID: period, Amount: "9", Reason: "Đang có"}}
	kept, err := c.Merge(existing, p, "keep")
	if err != nil || len(kept.Adjustments) != 1 || kept.Adjustments[0].Amount != "9" {
		t.Fatalf("keep adjustments: %#v %v", kept, err)
	}
	replaced, err := c.Merge(existing, p, "replace")
	if err != nil || len(replaced.Adjustments) != 1 || replaced.Adjustments[0].Amount != "-1.123456" {
		t.Fatalf("replace adjustments: %#v %v", replaced, err)
	}
	for _, tc := range []struct {
		amount       string
		period       uuid.UUID
		reason, rule string
	}{{"0", period, "Lý do", "zero"}, {"1e100000", period, "Lý do", "number"}, {"1", uuid.New(), "Lý do", "period"}, {"1", period, strings.Repeat("a", 501), "reason"}} {
		t.Run(tc.rule, func(t *testing.T) {
			values.Adjustments = []port.KHTCFormExcelAdjustment{{Sheet: "program_iii", ItemCode: "III.01", PeriodID: tc.period, Amount: tc.amount, Reason: tc.reason}}
			if !hasIssue(mustPreview(t, c, mustWorkbook(t, c, values)), tc.rule, "error") {
				t.Fatal("invalid adjustment accepted")
			}
		})
	}
}

func rewriteZIP(t *testing.T, raw []byte, edit func(string, []byte) []byte, additions map[string][]byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, part := range zr.File {
		r, err := part.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if edit != nil {
			body = edit(part.Name, body)
		}
		w, err := zw.Create(part.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range additions {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestHostilePackagesAndEarlyResourceBounds(t *testing.T) {
	c := mustCodec(t, snapshot())
	raw := mustWorkbook(t, c, content())
	for _, tc := range []struct {
		name  string
		raw   []byte
		limit bool
	}{
		{"over upload bytes", make([]byte, excel.MaxWorkbookBytes+1), true},
		{"legacy OLE", []byte{0xd0, 0xcf, 0x11, 0xe0}, false},
		{"macro", rewriteZIP(t, raw, nil, map[string][]byte{"xl/vbaProject.bin": {1, 2}}), false},
		{"duplicate ZIP", rewriteZIP(t, raw, nil, map[string][]byte{"xl/workbook.xml": []byte("<workbook/>")}), false},
		{"case duplicate ZIP", rewriteZIP(t, raw, nil, map[string][]byte{"XL/WORKBOOK.XML": []byte("<workbook/>")}), false},
		{"path traversal", rewriteZIP(t, raw, nil, map[string][]byte{"../outside.xml": []byte("<x/>")}), false},
		{"zip bomb", rewriteZIP(t, raw, nil, map[string][]byte{"xl/oversize.xml": bytes.Repeat([]byte("a"), excel.MaxPartBytes+1)}), true},
		{"entity", rewriteZIP(t, raw, func(name string, b []byte) []byte {
			if name == "xl/workbook.xml" {
				return append([]byte(`<!DOCTYPE workbook [<!ENTITY payload "expanded">]>`), b...)
			}
			return b
		}, nil), false},
		{"external relationship", rewriteZIP(t, raw, nil, map[string][]byte{"xl/worksheets/_rels/sheet1.xml.rels": []byte(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="hyperlink" Target="https://invalid.example/" TargetMode="External"/></Relationships>`)}), false},
		{"huge row", rewriteZIP(t, raw, func(name string, b []byte) []byte {
			if name == "xl/worksheets/sheet1.xml" {
				return bytes.Replace(b, []byte(`r="4"`), []byte(`r="1048576"`), 1)
			}
			return b
		}, nil), true},
		{"huge cell", rewriteZIP(t, raw, func(name string, b []byte) []byte {
			if name == "xl/worksheets/sheet1.xml" {
				return bytes.Replace(b, []byte(`r="C4"`), []byte(`r="XFD4"`), 1)
			}
			return b
		}, nil), true},
		{"duplicate XML cell", rewriteZIP(t, raw, func(name string, b []byte) []byte {
			if name == "xl/worksheets/sheet1.xml" {
				return bytes.Replace(b, []byte(`</row>`), []byte(`<c r="A1" t="inlineStr"><is><t>duplicate</t></is></c></row>`), 1)
			}
			return b
		}, nil), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Preview(tc.raw)
			expected := excel.ErrWorkbook
			if tc.limit {
				expected = excel.ErrLimit
			}
			if !errors.Is(err, expected) {
				t.Fatalf("expected %v, got %v", expected, err)
			}
		})
	}
	oversized := changeWorkbook(t, raw, func(f *excelize.File) { put(t, f, "CT III", "E4", strings.Repeat("a", excel.MaxCellBytes+1)) })
	if _, err := c.Preview(oversized); !errors.Is(err, excel.ErrLimit) {
		t.Fatalf("oversized cell accepted: %v", err)
	}
}

func TestSnapshotBoundsAndUnsupportedFormulaAreExplicit(t *testing.T) {
	for _, change := range []func(*port.KHTCFormExcelSnapshot){func(s *port.KHTCFormExcelSnapshot) { s.ID = uuid.Nil }, func(s *port.KHTCFormExcelSnapshot) { s.Sheets[0].TemplateHash = "not a hash" }, func(s *port.KHTCFormExcelSnapshot) { s.Sheets[0].TemplateVersion = 0 }, func(s *port.KHTCFormExcelSnapshot) { s.Sheets[0].Rows = append(s.Sheets[0].Rows, s.Sheets[0].Rows[0]) }, func(s *port.KHTCFormExcelSnapshot) { s.Sheets = append(s.Sheets, s.Sheets[0]) }, func(s *port.KHTCFormExcelSnapshot) { s.Sheets[0].Columns[1].Letter = "A" }} {
		s := snapshot()
		change(&s)
		if _, err := excel.New(s); !errors.Is(err, excel.ErrSnapshot) {
			t.Fatalf("malformed owner snapshot accepted: %v", err)
		}
	}
	for _, change := range []func(*port.KHTCFormExcelSnapshot){func(s *port.KHTCFormExcelSnapshot) { s.Sheets[0].Columns[3].FormulaMode = "excel_expression" }, func(s *port.KHTCFormExcelSnapshot) { s.Sheets[0].Columns[3].FormulaMode = "" }, func(s *port.KHTCFormExcelSnapshot) { s.Sheets[0].Rows[0].SystemValues["D"] = "SUM(C4)" }} {
		s := snapshot()
		change(&s)
		if _, err := excel.New(s); !errors.Is(err, excel.ErrFormulaProjection) {
			t.Fatalf("unsupported formula silently converted: %v", err)
		}
	}
}

func TestErrorExportAndConcurrentCallsDoNotShareMutableState(t *testing.T) {
	c := mustCodec(t, snapshot())
	raw := changeWorkbook(t, mustWorkbook(t, c, content()), func(f *excelize.File) { put(t, f, "CT III", "C4", "-2") })
	p := mustPreview(t, c, raw)
	errorsXLSX, err := c.ErrorWorkbook(p)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(errorsXLSX))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	rows, err := f.GetRows("Dòng cần chú ý")
	if err != nil || len(rows) < 2 {
		t.Fatalf("error export missing rows: %v", err)
	}
	for _, row := range rows {
		for _, value := range row {
			if strings.Contains(value, "-2") {
				t.Fatal("raw uploaded value copied to diagnostic export")
			}
		}
	}
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			p, err := c.Preview(raw)
			if err != nil {
				failures <- err
				return
			}
			if _, err = c.Merge(content(), p, "keep"); err != nil {
				failures <- err
			}
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
}
