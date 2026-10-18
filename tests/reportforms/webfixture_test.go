package reportformstest

// Tệp mẫu cho web-ops (P3b): backend giả lập của web dùng ĐÚNG định nghĩa 14 mẫu khởi tạo và bộ
// ca tính/thao tác vàng do engine Go sinh ra, để bản port TS của bộ tính (chỉ dùng trong chế độ
// giả lập) không lệch với máy chủ. Lệch → test này hỏng; sinh lại:
//
//	AG_UPDATE_WEB_FIXTURES=1 go test ./tests/reportforms -run TestWebFixtures
//
// rồi chạy Vitest (src/api/mock/forms/engine.test.ts so bản TS với ca vàng).

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

const webFixtureDir = "../../apps/web-ops/src/api/mock/forms/fixtures"

type webSeed struct {
	Code       string              `json:"code"`
	Name       string              `json:"name"`
	EntryLevel string              `json:"entry_level"`
	ReportType string              `json:"report_type"`
	Definition port.FormDefinition `json:"definition"`
}

type evalCase struct {
	Name   string                `json:"name"`
	Code   string                `json:"code"`
	Input  port.EvalInput        `json:"input"`
	Result port.EvalResult       `json:"result"`
	Totals []port.TotalRowResult `json:"totals"`
}

type opsCase struct {
	Name      string               `json:"name"`
	Code      string               `json:"code"`
	Ops       []reports.FormOp     `json:"ops"`
	ErrorCode string               `json:"error_code,omitempty"`
	ErrorAt   *int                 `json:"error_at,omitempty"`
	Result    *port.FormDefinition `json:"result,omitempty"`
}

func row(commune, region string, input map[string]string) port.EvalRow {
	return port.EvalRow{Commune: commune, Region: region, Input: input}
}

func goldenEvalCases(t *testing.T) []evalCase {
	open := []int{3}
	cases := []evalCase{
		{Name: "T01 tuần 3 mở, lũy kế, dòng tổng", Code: "T01", Input: port.EvalInput{Mode: port.EvalDraft, OpenSlots: open, Rows: []port.EvalRow{
			{Commune: "91301", Region: "KV883", Input: map[string]string{"E": "126", "F": "28", "G": "38", "H": "38.5"}, System: map[string]string{"B": "Long Xuyên", "C": "Mỹ Hòa Hưng", "D": "126"}},
			{Commune: "91302", Region: "KV883", Input: map[string]string{"E": "80,25", "H": "12.125", "I": "5", "N": "2026-09-01"}, System: map[string]string{"D": "90"}},
			row("91310", "KV884", map[string]string{"H": "abc", "O": "16/11/2026"}),
		}}},
		{Name: "T01 gửi: bắt buộc bỏ qua, kỳ đóng", Code: "T01", Input: port.EvalInput{Mode: port.EvalSubmit, OpenSlots: []int{1, 2}, Rows: []port.EvalRow{
			row("91301", "KV883", map[string]string{"F": "1", "G": "2.5", "M": "9"}),
		}}},
		{Name: "T04 hai cây, năng suất, tỷ lệ", Code: "T04", Input: port.EvalInput{Mode: port.EvalDraft, Rows: []port.EvalRow{
			{Commune: "91301", Region: "KV883", Input: map[string]string{"K": "0.5", "L": "0.5", "N": "4", "O": "6.5", "P": "9", "R": "162", "HO": "3", "HP": "1", "HR": "7"}, System: map[string]string{"D": "60"}},
			{Commune: "91302", Region: "KV883", Input: map[string]string{"K": "10", "L": "0", "N": "0"}, System: map[string]string{"D": "0"}},
		}}},
		{Name: "T07 khối 4 cột, % so PL 01", Code: "T07", Input: port.EvalInput{Mode: port.EvalDraft, OpenSlots: []int{2}, Rows: []port.EvalRow{
			{Commune: "91301", Region: "KV883", Input: map[string]string{"I": "12.5", "J": "6.2"}, XRefs: map[string]map[string]string{"T01": {"M": "104.5"}}},
		}}},
		{Name: "T03 ràng buộc liên biểu", Code: "T03", Input: port.EvalInput{Mode: port.EvalDraft, Rows: []port.EvalRow{
			{Commune: "91301", Region: "KV883", Input: map[string]string{"E": "10", "F": "20.5"}, XRefs: map[string]map[string]string{"T01": {"M": "40"}}},
			{Commune: "91302", Region: "KV883", Input: map[string]string{"E": "40"}, XRefs: map[string]map[string]string{"T01": {"M": "40"}}},
		}}},
		{Name: "T05 dòng chỉ tiêu", Code: "T05", Input: port.EvalInput{Mode: port.EvalDraft, Rows: []port.EvalRow{
			{Commune: "91301", Region: "KV883", Indicator: "SB01", Input: map[string]string{"E": "1.5", "F": "2", "G": "0.25"}},
			{Commune: "91301", Region: "KV883", Indicator: "SB02", Input: map[string]string{"E": "3"}},
			{Commune: "91302", Region: "KV884", Indicator: "SB01", Input: map[string]string{"E": "1"}},
			{Commune: "91302", Region: "KV884", Indicator: "XX99", Input: map[string]string{}},
		}}},
	}
	for i := range cases {
		def := seed(t, cases[i].Code)
		cases[i].Result = reports.EvaluateForm(def, cases[i].Input)
		cases[i].Totals = reports.FormTotals(def, cases[i].Result.Rows)
		if cases[i].Totals == nil {
			cases[i].Totals = []port.TotalRowResult{}
		}
	}
	return cases
}

func goldenOpsCases(t *testing.T) []opsCase {
	col := func(c port.FormColumn) *port.FormColumn { return &c }
	str := func(s string) *string { return &s }
	cases := []opsCase{
		{Name: "chèn cột trước F: dải SUM nở, vùng gộp dời", Code: "T01", Ops: []reports.FormOp{{Op: reports.FormOpInsertColumn, At: "F", Side: "before", Column: col(port.FormColumn{Title: "Ghi chú", Kind: port.ColumnInput, DType: port.DTypeText})}}},
		{Name: "chèn cột sau O + công thức tham chiếu", Code: "T01", Ops: []reports.FormOp{
			{Op: reports.FormOpInsertColumn, At: "O", Side: "after", Column: col(port.FormColumn{Title: "Tỷ lệ", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "%", Formula: "IF(D#=0,0,M#/D#*100)"})},
			{Op: reports.FormOpSetHeaderCell, Row: 1, Letter: "P", Text: "TỶ LỆ"},
			{Op: reports.FormOpMerge, Range: "P1:P2"},
		}},
		{Name: "xóa cột E (không ai tham chiếu)", Code: "T01", Ops: []reports.FormOp{{Op: reports.FormOpDeleteColumn, Letter: "E"}}},
		{Name: "xóa cột D đang được công thức tham chiếu → lỗi", Code: "T04", Ops: []reports.FormOp{{Op: reports.FormOpDeleteColumn, Letter: "D"}}},
		{Name: "xóa cột theo kỳ lẻ → lỗi", Code: "T01", Ops: []reports.FormOp{{Op: reports.FormOpDeleteColumn, Letter: "H"}}},
		{Name: "đổi cột thành công thức sai cú pháp → lỗi", Code: "T01", Ops: []reports.FormOp{{Op: reports.FormOpUpdateColumn, Letter: "E", Column: col(port.FormColumn{Title: "X", Kind: port.ColumnFormula, DType: port.DTypeNumber, Formula: "SUM(D#:"})}}},
		{Name: "công thức tham chiếu cột không tồn tại → lỗi", Code: "T01", Ops: []reports.FormOp{{Op: reports.FormOpUpdateColumn, Letter: "E", Column: col(port.FormColumn{Title: "X", Kind: port.ColumnFormula, DType: port.DTypeNumber, Formula: "ZZ#*2"})}}},
		{Name: "công thức vòng → lỗi", Code: "T01", Ops: []reports.FormOp{{Op: reports.FormOpUpdateColumn, Letter: "E", Column: col(port.FormColumn{Title: "X", Kind: port.ColumnFormula, DType: port.DTypeNumber, Formula: "M#+1"})}, {Op: reports.FormOpUpdateColumn, Letter: "M", Column: col(port.FormColumn{Title: "LŨY KẾ", Kind: port.ColumnFormula, DType: port.DTypeNumber, Formula: "SUM(F#:L#)+E#"})}}},
		{Name: "thêm dòng tiêu đề trên cùng, tách, gộp lại", Code: "T01", Ops: []reports.FormOp{
			{Op: reports.FormOpInsertHeaderRow, Row: 1, Side: "before"},
			{Op: reports.FormOpSetHeaderCell, Row: 1, Letter: "A", Text: "PHỤ LỤC 01"},
			{Op: reports.FormOpMerge, Range: "A1:O1"},
			{Op: reports.FormOpUnmerge, Range: "F2:L2"},
		}},
		{Name: "xóa dòng tiêu đề 2", Code: "T07", Ops: []reports.FormOp{{Op: reports.FormOpDeleteHeaderRow, Row: 2}}},
		{Name: "gộp chồng vùng có sẵn → lỗi", Code: "T01", Ops: []reports.FormOp{{Op: reports.FormOpMerge, Range: "E1:G1"}}},
		{Name: "dòng chỉ tiêu, ràng buộc, meta, nhóm, dòng tổng, khối kỳ", Code: "T05", Ops: []reports.FormOp{
			{Op: reports.FormOpAddIndicator, Indicator: &port.Indicator{Code: "SB00", Label: "Rầy nâu"}, After: "SB01"},
			{Op: reports.FormOpDeleteIndicator, Code: "SB20"},
			{Op: reports.FormOpSetRule, Rule: &port.FormRule{ID: "T05.nang", Expr: "G#<=H#", Message: "Mức nặng không vượt tổng", Severity: port.SeverityWarning, Letters: []string{"G"}}},
			{Op: reports.FormOpSetMeta, Meta: &reports.FormMeta{Title: str("SÂU BỆNH HẠI LÚA"), FreezeThrough: str("c"), Requirements: str("Nhập diện tích nhiễm theo mức")}},
			{Op: reports.FormOpSetGroups, Groups: []port.ColumnGroup{{ID: "muc", Title: "Theo mức", From: "E", To: "G"}}},
			{Op: reports.FormOpSetTotals, Totals: []port.TotalRowDef{{Kind: port.TotalProvince, Label: "Toàn tỉnh", LabelLetter: "C"}}},
		}},
		{Name: "đổi tiêu đề mẫu của khối kỳ", Code: "T01", Ops: []reports.FormOp{{Op: reports.FormOpSetPeriodBlock, Block: &port.PeriodBlock{ID: "xg", FirstLetter: "F", Width: 1, Slots: 7, TitlePattern: "Tuần thứ {n}"}}}},
		{Name: "tăng số kỳ 7 → 8 (chèn trước khe cuối)", Code: "T01", Ops: []reports.FormOp{
			{Op: reports.FormOpSetPeriodBlock, Block: &port.PeriodBlock{ID: "xg", FirstLetter: "F", Width: 1, Slots: 8, TitlePattern: "Tuần {n}"}},
			{Op: reports.FormOpInsertColumn, At: "L", Side: "before", Column: col(port.FormColumn{Key: "xg_tuan_7", Title: "Tuần 7", Kind: port.ColumnPerPeriod, DType: port.DTypeNumber, Unit: "ha", PeriodBlock: "xg", PeriodSlot: 7})},
			{Op: reports.FormOpUpdateColumn, Letter: "M", Column: col(port.FormColumn{Key: "xg_tuan_8", Title: "Tuần 8", Kind: port.ColumnPerPeriod, DType: port.DTypeNumber, Unit: "ha", PeriodBlock: "xg", PeriodSlot: 8})},
		}},
		{Name: "giảm số kỳ 7 → 6", Code: "T01", Ops: []reports.FormOp{
			{Op: reports.FormOpSetPeriodBlock, Block: &port.PeriodBlock{ID: "xg", FirstLetter: "F", Width: 1, Slots: 6, TitlePattern: "Tuần {n}"}},
			{Op: reports.FormOpUpdateColumn, Letter: "L", Column: col(port.FormColumn{Title: "Tuần 7", Kind: port.ColumnInput, DType: port.DTypeNumber})},
			{Op: reports.FormOpDeleteColumn, Letter: "L"},
		}},
		{Name: "thao tác không hỗ trợ → lỗi", Code: "T01", Ops: []reports.FormOp{{Op: "explode"}}},
	}
	for i := range cases {
		d, err := reports.ApplyForm(seed(t, cases[i].Code), cases[i].Ops)
		if err != nil {
			var oe *reports.FormOpError
			var ve *reports.FormValidationError
			switch {
			case errors.As(err, &oe):
				cases[i].ErrorCode, cases[i].ErrorAt = oe.Code, &oe.Index
			case errors.As(err, &ve):
				cases[i].ErrorCode = ve.Code()
			default:
				t.Fatalf("%s: lỗi lạ %v", cases[i].Name, err)
			}
			continue
		}
		cases[i].Result = &d
	}
	return cases
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestWebFixtures (FN-AD-04.06, P3b): tệp mẫu của backend giả lập web khớp engine Go.
func TestWebFixtures(t *testing.T) {
	seeds := []webSeed{}
	for _, s := range extension.ReportFormSeeds().Templates {
		seeds = append(seeds, webSeed{Code: s.Code, Name: s.Name, EntryLevel: s.EntryLevel, ReportType: s.ReportType, Definition: s.Def})
	}
	files := map[string][]byte{
		"seed.json":        marshal(t, seeds),
		"eval-golden.json": marshal(t, goldenEvalCases(t)),
		"ops-golden.json":  marshal(t, goldenOpsCases(t)),
	}
	update := os.Getenv("AG_UPDATE_WEB_FIXTURES") == "1"
	for name, want := range files {
		p := filepath.Join(webFixtureDir, name)
		if update {
			if err := os.MkdirAll(webFixtureDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, want, 0o644); err != nil { //nolint:gosec // tệp mẫu của repo
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(p) //nolint:gosec // đường dẫn cố định trong repo
		if err != nil {
			t.Fatalf("%s: %v (sinh bằng AG_UPDATE_WEB_FIXTURES=1)", name, err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
			t.Errorf("%s lệch engine Go — sinh lại bằng AG_UPDATE_WEB_FIXTURES=1 rồi chạy Vitest", name)
		}
	}
}
