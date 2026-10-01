package form_test

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/form"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

func seed(t *testing.T, code string) port.FormDefinition {
	t.Helper()
	for _, s := range form.TrongTrotSeeds() {
		if s.Code == code {
			return s.Def
		}
	}
	t.Fatalf("không có mẫu %s", code)
	return port.FormDefinition{}
}

func issueCodes(r port.EvalResult) []string {
	var out []string
	for _, i := range r.Issues {
		out = append(out, i.Code+"@"+i.Letter)
	}
	return out
}

// TestFN_AD_04_06_SeedsValid: 14 mẫu khởi tạo đều hợp lệ; phần chưa biết có cờ chờ khách Q-01.
func TestFN_AD_04_06_SeedsValid(t *testing.T) {
	seeds := form.TrongTrotSeeds()
	if len(seeds) != 14 {
		t.Fatalf("muốn 14 mẫu, có %d", len(seeds))
	}
	want := []string{"T01", "T02", "T03", "T04", "T05", "T06", "T07", "M01", "V01", "V02", "V03", "V04", "V05", "V06"}
	for i, s := range seeds {
		if s.Code != want[i] || s.Def.Code != s.Code {
			t.Fatalf("thứ tự/mã mẫu sai: %s", s.Code)
		}
		if err := form.Validate(s.Def); err != nil {
			t.Fatalf("%s: %v", s.Code, err)
		}
		if s.Code != "T01" && len(s.Def.Pending) == 0 {
			t.Errorf("%s: phải ghi phần chờ khách", s.Code)
		}
		for _, p := range s.Def.Pending {
			if p.Question != "Q-01" {
				t.Errorf("%s: pending phải gắn Q-01", s.Code)
			}
		}
		for _, c := range s.Def.Columns {
			if c.Placeholder && !strings.Contains(c.Title, form.Pending) {
				t.Errorf("%s cột %s: placeholder phải có nhãn %q", s.Code, c.Letter, form.Pending)
			}
		}
	}
}

// TestFN_XA_03_02_T01Layout: T01 đúng cột A–O (BRD §8.2): A STT, B Khu vực, C Xã, D Tỉnh giao, E nhập,
// F–L theo kỳ, M lũy kế, N–O ngày; tiêu đề gộp F1:L1.
func TestFN_XA_03_02_T01Layout(t *testing.T) {
	d := seed(t, "T01")
	got := ""
	for _, c := range d.Columns {
		got += c.Letter + ":" + c.Kind[:2] + " "
	}
	want := "A:fi B:fi C:fi D:pr E:in F:pe G:pe H:pe I:pe J:pe K:pe L:pe M:fo N:in O:in "
	if got != want {
		t.Fatalf("bố cục T01:\n%s\nmuốn\n%s", got, want)
	}
	if d.Columns[12].Formula != "SUM(F#:L#)" || !slices.Contains(d.Header.Merges, "F1:L1") || !slices.Contains(d.Header.Merges, "A1:A2") {
		t.Fatalf("T01 công thức/gộp sai: %s %v", d.Columns[12].Formula, d.Header.Merges)
	}
	if d.Requirements != "Số xuống giống được nhập là số tăng thêm trong tuần, không nhập số lũy kế" {
		t.Fatal("T01 phải giữ “Yêu cầu biểu mẫu” nguyên văn")
	}
}

// TestFN_XA_03_02_T04ReachesHR: T04 từ K là 54 cây × 4 chỉ tiêu tới cột HR, tiêu đề 2 tầng nhóm cây.
func TestFN_XA_03_02_T04ReachesHR(t *testing.T) {
	d := seed(t, "T04")
	if last := d.Columns[len(d.Columns)-1].Letter; last != "HR" || len(d.Columns) != 226 {
		t.Fatalf("T04 phải kết thúc ở HR (226 cột), có %s (%d)", last, len(d.Columns))
	}
	if d.FreezeThrough != "J" || len(d.Groups) != 9 {
		t.Fatalf("T04 cố định A–J và 9 nhóm cây cho “Đi tới”: %s %d", d.FreezeThrough, len(d.Groups))
	}
	in := port.EvalInput{Mode: port.EvalSubmit, Rows: []port.EvalRow{{Commune: "30292", Region: "KV1",
		System: map[string]string{"D": "100"},
		Input:  map[string]string{"K": "10", "L": "8", "N": "40", "O": "5", "P": "2", "R": "6"}}}}
	r := form.Evaluate(d, in)
	if !r.Valid {
		t.Fatalf("T04 hợp lệ: %v", issueCodes(r))
	}
	c := r.Rows[0].Cells
	for l, want := range map[string]string{"E": "15", "F": "15", "G": "10", "I": "46", "H": "4.6", "J": "66.6666666667", "M": "5", "Q": "3"} {
		if c[l] != want {
			t.Errorf("T04 %s = %s, muốn %s", l, c[l], want)
		}
	}
}

// TestFN_XA_03_02_OnlyOpenPeriodEditable: PL 01 — chỉ cột của kỳ đang mở được nhập; cột cố định,
// Tỉnh giao, công thức không nhập tay; công thức Lũy kế tính lại ở server.
func TestFN_XA_03_02_OnlyOpenPeriodEditable(t *testing.T) {
	d := seed(t, "T01")
	in := port.EvalInput{Mode: port.EvalSubmit, OpenSlots: []int{3}, Rows: []port.EvalRow{{
		Commune: "30292", Region: "KV1",
		System: map[string]string{"A": "1", "B": "Khu vực 1", "C": "Phường Bình Đức", "D": "1200"},
		Input:  map[string]string{"E": "1100", "F": "", "H": "250.5", "N": "2026-10-15"},
	}}}
	r := form.Evaluate(d, in)
	if !r.Valid || r.Rows[0].Cells["M"] != "250.5" || r.Rows[0].Cells["D"] != "1200" {
		t.Fatalf("hợp lệ, M = 250.5: %v %v", issueCodes(r), r.Rows[0].Cells)
	}
	in.Rows[0].Input = map[string]string{"G": "10", "M": "999", "D": "5", "E": "abc", "N": "15/10/2026", "ZZ": "1"}
	r = form.Evaluate(d, in)
	got := issueCodes(r)
	for _, want := range []string{form.IssuePeriodClosed + "@G", form.IssueNotEditable + "@M", form.IssueNotEditable + "@D", form.IssueInvalidType + "@E", form.IssueInvalidType + "@N", form.IssueUnknownColumn + "@ZZ"} {
		if !slices.Contains(got, want) {
			t.Errorf("thiếu %s trong %v", want, got)
		}
	}
	if r.Valid {
		t.Fatal("lưới sai phải bị chặn gửi")
	}
}

// TestFN_XA_03_02_T03TraEqualsSown: ràng buộc T03 — tổng các trà = DT xuống giống PL 01 (liên biểu
// cùng xã cùng kỳ); vi phạm → báo tại ô J, không cho gửi; chưa có PL 01 → không áp dụng.
func TestFN_XA_03_02_T03TraEqualsSown(t *testing.T) {
	d := seed(t, "T03")
	row := port.EvalRow{Commune: "30292", Input: map[string]string{"E": "100", "F": "200.5", "G": "50"},
		XRefs: map[string]map[string]string{"T01": {"M": "350.5"}}}
	r := form.Evaluate(d, port.EvalInput{Mode: port.EvalSubmit, Rows: []port.EvalRow{row}})
	if !r.Valid || r.Rows[0].Cells["J"] != "350.5" || r.Rows[0].Cells["D"] != "350.5" {
		t.Fatalf("Σ trà = PL 01 phải hợp lệ: %v %v", issueCodes(r), r.Rows[0].Cells)
	}
	row.Input["G"] = "49"
	r = form.Evaluate(d, port.EvalInput{Mode: port.EvalSubmit, Rows: []port.EvalRow{row}})
	if r.Valid || !slices.Contains(issueCodes(r), form.IssueRuleFailed+"@J") || r.Issues[0].RuleID != "T03.tong_tra_bang_xg" {
		t.Fatalf("Σ trà ≠ PL 01 phải báo tại J: %v", issueCodes(r))
	}
	row.XRefs = nil
	r = form.Evaluate(d, port.EvalInput{Mode: port.EvalSubmit, Rows: []port.EvalRow{row}})
	if !r.Valid {
		t.Fatalf("chưa có PL 01 thì không áp dụng ràng buộc: %v", issueCodes(r))
	}
}

// TestFN_XA_03_02_RequiredAndRange: bắt buộc chỉ kiểm khi gửi (nháp bỏ qua); giới hạn của mẫu.
func TestFN_XA_03_02_RequiredAndRange(t *testing.T) {
	d := seed(t, "T01")
	d.Columns[4].Required = true
	minV := "0"
	d.Columns[4].Min = &minV
	in := port.EvalInput{Mode: port.EvalDraft, Rows: []port.EvalRow{{Commune: "x1", Input: map[string]string{}}}}
	if r := form.Evaluate(d, in); !r.Valid {
		t.Fatalf("nháp bỏ kiểm bắt buộc: %v", issueCodes(r))
	}
	in.Mode = port.EvalSubmit
	if r := form.Evaluate(d, in); r.Valid || !slices.Contains(issueCodes(r), form.IssueRequired+"@E") {
		t.Fatalf("gửi phải kiểm bắt buộc: %v", issueCodes(r))
	}
	in.Rows[0].Input["E"] = "-1"
	if r := form.Evaluate(d, in); !slices.Contains(issueCodes(r), form.IssueOutOfRange+"@E") {
		t.Fatalf("giới hạn của mẫu: %v", issueCodes(r))
	}
}

// TestFN_TI_02_05_TotalsWeighted: dòng tổng Khu vực/Toàn tỉnh cộng các xã; năng suất trên dòng
// tổng = tổng sản lượng / tổng diện tích (bình quân gia quyền), không cộng năng suất.
func TestFN_TI_02_05_TotalsWeighted(t *testing.T) {
	d := seed(t, "T04")
	rows := []port.EvalRow{
		{Commune: "c1", Region: "KV1", System: map[string]string{"D": "10"}, Input: map[string]string{"L": "10", "N": "50"}},
		{Commune: "c2", Region: "KV1", System: map[string]string{"D": "30"}, Input: map[string]string{"L": "30", "N": "90"}},
		{Commune: "c3", Region: "KV2", Input: map[string]string{"L": "5", "N": "10"}},
	}
	r := form.Evaluate(d, port.EvalInput{Mode: port.EvalDraft, Rows: rows})
	tot := form.Totals(d, r.Rows)
	if len(tot) != 3 {
		t.Fatalf("muốn 2 khu vực + toàn tỉnh, có %d", len(tot))
	}
	kv1 := tot[0]
	if kv1.Key != "region:KV1" || kv1.Members != 2 || kv1.Cells["L"] != "40" || kv1.Cells["N"] != "140" || kv1.Cells["M"] != "3.5" || kv1.Cells["D"] != "40" || kv1.Cells["B"] != "Cộng KV1" {
		t.Fatalf("dòng Khu vực sai: %+v", kv1)
	}
	all := tot[2]
	if all.Key != "province" || all.Members != 3 || all.Cells["H"] != "3.3333333333" || all.Cells["I"] != "150" || all.Cells["B"] != "Toàn tỉnh" {
		t.Fatalf("dòng Toàn tỉnh sai: %+v", all)
	}
}

// TestFN_XA_03_02_IndicatorRows: T05 — mỗi xã × mỗi đối tượng một dòng; dòng tổng theo đối tượng.
func TestFN_XA_03_02_IndicatorRows(t *testing.T) {
	d := seed(t, "T05")
	in := port.EvalInput{Mode: port.EvalSubmit, Rows: []port.EvalRow{
		{Commune: "c1", Region: "KV1", Indicator: "SB01", Input: map[string]string{"E": "1", "G": "2"}},
		{Commune: "c2", Region: "KV1", Indicator: "SB01", Input: map[string]string{"F": "3"}},
		{Commune: "c1", Region: "KV1", Indicator: "XX"},
	}}
	r := form.Evaluate(d, in)
	if r.Valid || len(r.Rows) != 2 || r.Rows[0].Cells["H"] != "3" || !strings.Contains(r.Rows[0].Cells["D"], "Đối tượng sâu bệnh 1") {
		t.Fatalf("dòng chỉ tiêu: %v %v", issueCodes(r), r.Rows)
	}
	tot := form.Totals(d, r.Rows)
	var sb01 port.TotalRowResult
	for _, x := range tot {
		if x.Key == "province/SB01" {
			sb01 = x
		}
	}
	if sb01.Cells["H"] != "6" || sb01.Members != 2 {
		t.Fatalf("tổng theo đối tượng: %+v", sb01)
	}
}

// TestFN_AD_04_06_DesignerOps: thêm/xóa cột dời công thức như Excel; xóa cột đang được công thức
// tham chiếu → reports.formula_invalid_ref, không lưu; thêm dòng tiêu đề, gộp/tách ô.
func TestFN_AD_04_06_DesignerOps(t *testing.T) {
	d := seed(t, "T01")
	newCol := port.FormColumn{Title: "Ghi chú", Kind: port.ColumnInput, DType: port.DTypeText}
	out, err := form.Apply(d, []form.Op{{Op: form.OpInsertColumn, At: "E", Side: "after", Column: &newCol}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Columns[5].Title != "Ghi chú" || out.Columns[13].Formula != "SUM(G#:M#)" || out.PeriodBlocks[0].FirstLetter != "G" || !slices.Contains(out.Header.Merges, "G1:M1") {
		t.Fatalf("chèn cột sau E: %s %s %v", out.Columns[13].Formula, out.PeriodBlocks[0].FirstLetter, out.Header.Merges)
	}
	back, err := form.Apply(out, []form.Op{{Op: form.OpDeleteColumn, Letter: "F"}})
	if err != nil {
		t.Fatal(err)
	}
	if back.Columns[12].Formula != "SUM(F#:L#)" || len(back.Columns) != 15 {
		t.Fatalf("xóa lại cột: %s", back.Columns[12].Formula)
	}
	// Xóa cột E mà một công thức tham chiếu → lỗi tham chiếu.
	f := port.FormColumn{Title: "Tỷ lệ", Kind: port.ColumnFormula, DType: port.DTypeNumber, Formula: "IF(E#=0,0,M#/E#*100)"}
	withRatio, err := form.Apply(d, []form.Op{{Op: form.OpInsertColumn, At: "O", Side: "after", Column: &f}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = form.Apply(withRatio, []form.Op{{Op: form.OpDeleteColumn, Letter: "E"}})
	var oe *form.OpError
	if !errors.As(err, &oe) || oe.Code != form.CodeFormulaInvalidRef {
		t.Fatalf("xóa cột được tham chiếu phải lỗi formula_invalid_ref: %v", err)
	}
	// Công thức tham chiếu ô không tồn tại → lỗi, không lưu.
	bad := port.FormColumn{Title: "Sai", Kind: port.ColumnFormula, DType: port.DTypeNumber, Formula: "ZZ#+1"}
	_, err = form.Apply(d, []form.Op{{Op: form.OpInsertColumn, At: "O", Side: "after", Column: &bad}})
	var ve *form.ValidationError
	if !errors.As(err, &ve) || ve.Code() != form.CodeFormulaInvalidRef {
		t.Fatalf("tham chiếu không tồn tại: %v", err)
	}
	// Dòng tiêu đề + gộp/tách.
	out, err = form.Apply(d, []form.Op{
		{Op: form.OpInsertHeaderRow, Row: 1, Side: "before"},
		{Op: form.OpSetHeaderCell, Row: 1, Letter: "A", Text: "BIỂU PL 01"},
		{Op: form.OpMerge, Range: "A1:O1"},
		{Op: form.OpUnmerge, Range: "A1:O1"},
		{Op: form.OpMerge, Range: "A1:E1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Header.Rows != 3 || !slices.Contains(out.Header.Merges, "F2:L2") || !slices.Contains(out.Header.Merges, "A2:A3") || !slices.Contains(out.Header.Merges, "A1:E1") {
		t.Fatalf("dòng tiêu đề: %d %v", out.Header.Rows, out.Header.Merges)
	}
	if _, err := form.Apply(d, []form.Op{{Op: form.OpMerge, Range: "E1:G1"}}); err == nil {
		t.Fatal("gộp chồng vùng gộp có sẵn phải lỗi")
	}
	// Vòng tham chiếu.
	cyc := form.Clone(d)
	cyc.Columns[12].Formula = "SUM(F#:L#)+P#"
	cyc.Columns = append(cyc.Columns, port.FormColumn{Letter: "P", Title: "x", Kind: port.ColumnFormula, DType: port.DTypeNumber, Formula: "M#"})
	if err := form.Validate(cyc); !errors.As(err, &ve) || ve.Code() != form.CodeFormulaCycle {
		t.Fatalf("vòng tham chiếu: %v", err)
	}
}

// TestFN_TI_02_02_LayoutPeriodLabels: tiêu đề cột theo kỳ đặt theo các kỳ đã chia; khe thừa inactive.
func TestFN_TI_02_02_LayoutPeriodLabels(t *testing.T) {
	d := form.Layout(seed(t, "T01"), form.LayoutOptions{PeriodLabels: []string{"Tuần 36", "Tuần 37"}, Season: "THU ĐÔNG 2026"})
	if d.Columns[5].Title != "Tuần 36" || d.Columns[6].Title != "Tuần 37" || !d.Columns[7].Inactive || d.Columns[5].Inactive {
		t.Fatalf("nhãn kỳ: %+v", d.Columns[5:8])
	}
	found := false
	for _, c := range d.Header.Cells {
		found = found || c.Letter == "F" && c.Row == 1 && c.Text == "DIỆN TÍCH XUỐNG GIỐNG LÚA THU ĐÔNG 2026 THEO TUẦN (ha)"
	}
	if !found || d.Title != "TIẾN ĐỘ XUỐNG GIỐNG LÚA THU ĐÔNG 2026" {
		t.Fatalf("thay {season}: %v", d.Title)
	}
}

// TestFN_AD_04_06_SchemaContract: mọi trường JSON của định nghĩa (seed T01, T04, T07) có trong
// contracts/schemas/form-template.v1.json — hợp đồng lưới cho web/app không lệch mã.
func TestFN_AD_04_06_SchemaContract(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/schemas/form-template.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var sc struct {
		Defs map[string]map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &sc); err != nil {
		t.Fatal(err)
	}
	props := func(def string) map[string]any { p, _ := sc.Defs[def]["properties"].(map[string]any); return p }
	check := func(what string, obj map[string]any, allowed map[string]any) {
		for k := range obj {
			if _, ok := allowed[k]; !ok {
				t.Errorf("%s: trường %q không có trong lược đồ", what, k)
			}
		}
	}
	for _, code := range []string{"T01", "T04", "T07", "T05"} {
		b, _ := json.Marshal(seed(t, code))
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		check(code, m, props("FormDefinition"))
		for _, c := range m["columns"].([]any) {
			check(code+" cột", c.(map[string]any), props("FormColumn"))
		}
		for _, c := range m["header"].(map[string]any)["cells"].([]any) {
			check(code+" ô tiêu đề", c.(map[string]any), props("FormHeaderCell"))
		}
		for _, r := range m["rules"].([]any) {
			check(code+" ràng buộc", r.(map[string]any), props("FormRule"))
		}
	}
}
