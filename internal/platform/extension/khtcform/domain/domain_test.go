package domain_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcform/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
)

func dec(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }

// TestKH_FN_AD_04_02_SeededFormsPerDepartment: 4 mẫu nạp sẵn, mỗi mẫu một phòng nhập (Bảng FN-PN-03.02); lưới A–M có đủ 5
// loại cột, trong đó 2 loại mới "Từ Kế hoạch năm" (C, D) và "Lũy kế kỳ trước" (E); cột G "Điều chỉnh kỳ trước" cạnh cột F
// "Thực hiện trong kỳ"; HCTH không có tab chương trình, chỉ lời văn; mục lời văn đầu bắt buộc.
func TestKH_FN_AD_04_02_SeededFormsPerDepartment(t *testing.T) {
	seeds := domain.Seeds()
	depts := []string{}
	for _, s := range seeds {
		depts = append(depts, s.Department)
		if s.Version != 1 || s.Code == "" || len(s.Sections) == 0 || !s.Sections[0].Required {
			t.Fatalf("%s: mã/phiên bản/mục lời văn", s.Code)
		}
	}
	if !slices.Equal(depts, []string{domain.DeptTTTT, domain.DeptTT, domain.DeptCNTS, domain.DeptHCTH}) {
		t.Fatalf("phòng nhập: %v", depts)
	}
	tt := domain.TemplateFor(domain.DeptTT)
	got := ""
	for i, c := range tt.Columns {
		if c.Letter != string(rune('A'+i)) {
			t.Fatalf("chữ cái cột không liên tục: %s", c.Letter)
		}
		got += c.Letter + ":" + c.Kind + " "
	}
	want := "A:fixed B:fixed C:from_plan D:from_plan E:prior_cumulative F:input G:formula H:formula I:formula J:input K:input L:input M:input "
	if got != want {
		t.Fatalf("bố cục:\n%s\nmuốn\n%s", got, want)
	}
	if f, _ := tt.Column("F"); f.Input != domain.InDone || !f.NonNegative {
		t.Fatal("F phải là Thực hiện trong kỳ, không âm")
	}
	if g, _ := tt.Column("G"); g.Formula != domain.FxAdjust {
		t.Fatal("G phải là Điều chỉnh kỳ trước")
	}
	if h := domain.TemplateFor(domain.DeptHCTH); len(h.Columns) != 0 || h.Sections[0].Key != "cchc" {
		t.Fatal("HCTH chỉ lời văn, mục CCHC")
	}
	if g := domain.TemplateFor("phong_khac"); g.Code != "KH-CHUNG" {
		t.Fatal("phòng chưa có mẫu → mẫu chung")
	}
}

type planFx struct {
	plan                         port.KHTCPlan
	ct3, ct7, item, mod, stopped uuid.UUID
}

func samplePlan() planFx {
	f := planFx{ct3: uuid.New(), ct7: uuid.New(), item: uuid.New(), mod: uuid.New(), stopped: uuid.New()}
	g3 := uuid.New()
	ref := func(u uuid.UUID) *uuid.UUID { return &u }
	stop := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	f.plan = port.KHTCPlan{Year: 2026, Entries: []port.KHTCPlanEntry{
		{Ref: f.ct7, Tab: "central_project", Kind: "program", Name: "DA nuôi tôm"},
		{Ref: uuid.New(), Tab: "central_project", Kind: "item", ParentRef: ref(f.ct7), Name: "Điểm tôm", Department: domain.DeptCNTS, Tracking: "tracking"},
		{Ref: f.ct3, Tab: "programs", Kind: "program", Roman: "III", ProgramRef: ref(uuid.New())},
		{Ref: g3, Tab: "programs", Kind: "group", ParentRef: ref(f.ct3), Name: "Mô hình"},
		{Ref: f.item, Tab: "programs", Kind: "item", ParentRef: ref(g3), Name: "Mô hình lúa", UnitCode: "ha", PlannedQty: dec("100"),
			Department: domain.DeptTT, Tracking: "tracking"},
		{Ref: f.mod, Tab: "programs", Kind: "item", ParentRef: ref(g3), Name: "Diện tích gieo sạ", UnitCode: "ha", PlannedQty: dec("5000"),
			Department: domain.DeptTT, DataSource: "module", SourceKey: "trong_trot.consolidation", Tracking: "tracking"},
		{Ref: f.stopped, Tab: "programs", Kind: "item", ParentRef: ref(g3), Name: "Đã ngừng", Department: domain.DeptTT, Tracking: "stopped",
			StoppedFrom: &stop},
	}}
	return f
}

// BR-DL-21, BR-DL-10: tab = CT có hạng mục của phòng (không gắn cứng mã), hạng mục ngừng trước kỳ không còn, hạng mục nguồn
// module không có ô nhập.
func TestKH_FN_PN_03_02_RowsFromPlan(t *testing.T) {
	f := samplePlan()
	names := map[uuid.UUID]string{*f.plan.Entries[2].ProgramRef: "CT KN trồng trọt"}
	tabs := domain.BuildTabs(f.plan, domain.DeptTT, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), names)
	if len(tabs) != 1 || tabs[0].Label != "CT III: CT KN trồng trọt" || len(tabs[0].Rows) != 2 {
		t.Fatalf("tab: %+v", tabs)
	}
	if !tabs[0].Rows[1].ModuleSourced || tabs[0].Rows[0].ModuleSourced {
		t.Fatal("nguồn module")
	}
	// Kỳ bắt đầu trước ngày ngừng (quý III) vẫn có hạng mục đã ngừng từ 01/10.
	q := domain.BuildTabs(f.plan, domain.DeptTT, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), names)
	if len(q[0].Rows) != 3 {
		t.Fatalf("quý chứa ngày ngừng: %d dòng", len(q[0].Rows))
	}
	if c := domain.BuildTabs(f.plan, domain.DeptCNTS, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), nil); len(c) != 1 ||
		c[0].Label != "Dự án TW: DA nuôi tôm" {
		t.Fatalf("CNTS: %+v", c)
	}
}

func eval(f planFx, sh domain.Sheet, prior map[uuid.UUID]decimal.Decimal, adj map[uuid.UUID]domain.AdjPeriod, submit bool) domain.Result {
	tabs := domain.BuildTabs(f.plan, domain.DeptTT, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), nil)
	return domain.Evaluate(domain.Input{Template: domain.TemplateFor(domain.DeptTT), Rows: domain.Rows(tabs), Grid: true, Prior: prior,
		Periods: adj, PlanYear: 2026, Sheet: sh.Normalize(), Submit: submit})
}

func rules(r domain.Result) []string {
	var out []string
	for _, i := range r.Issues {
		out = append(out, i.Rule+"/"+i.Severity+"@"+i.Letter)
	}
	return out
}

// FN-TI-02.04: đúng 3 quy tắc — yêu cầu của mẫu (chặn), số âm phải có lý do (chặn), lũy kế > 100% KH (cảnh báo).
func TestKH_FN_TI_02_04_Rules(t *testing.T) {
	f := samplePlan()
	w40 := uuid.New()
	adj := map[uuid.UUID]domain.AdjPeriod{w40: {ID: w40, Name: "Tuần 40", Year: 2026}}
	prior := map[uuid.UUID]decimal.Decimal{f.item: decimal.RequireFromString("95")}
	// Hợp lệ: lũy kế 95 + 3 + 1 = 99 (99%).
	ok := domain.Sheet{Cells: map[string]domain.CellInput{f.item.String(): {Done: "3", People: "25"}},
		Adjustments: []domain.Adjustment{{EntryRef: f.item.String(), PeriodID: w40.String(), Amount: "1", Reason: "Bổ sung lớp tuần 40"}},
		Texts:       map[string]string{"cong_tac_khac": "Tập huấn 3 lớp"}}
	r := eval(f, ok, prior, adj, true)
	if !r.Valid() || len(r.Issues) != 0 {
		t.Fatalf("hợp lệ: %v", rules(r))
	}
	cells := r.Rows[f.item].Cells
	if cells["E"] != "95" || cells["G"] != "1" || cells["H"] != "99" || cells["I"] != "99" || cells["D"] != "100" || cells["C"] != "ha" {
		t.Fatalf("ô hệ thống: %v", cells)
	}
	// (3) vượt KH → cảnh báo, vẫn gửi.
	over := ok
	over.Cells = map[string]domain.CellInput{f.item.String(): {Done: "10.5"}}
	r = eval(f, over, prior, adj, true)
	if !r.Valid() || !slices.Equal(rules(r), []string{"over_plan/warning@H"}) || r.Rows[f.item].Cells["I"] != "106.5" {
		t.Fatalf("vượt KH: %v %v", rules(r), r.Rows[f.item].Cells)
	}
	// (1) mẫu: F âm, thiếu mục bắt buộc, điều chỉnh thiếu kỳ; nguồn module có ô nhập. (2) điều chỉnh âm thiếu lý do.
	bad := domain.Sheet{Cells: map[string]domain.CellInput{f.item.String(): {Done: "-2"}, f.mod.String(): {Done: "7"}},
		Adjustments: []domain.Adjustment{{EntryRef: f.item.String(), Amount: "2", Reason: "x"},
			{EntryRef: f.item.String(), PeriodID: w40.String(), Amount: "-4"}}}
	r = eval(f, bad, prior, adj, true)
	got := rules(r)
	for _, w := range []string{"template/error@F", "template/error@", "negative_reason/error@G"} {
		if !slices.Contains(got, w) {
			t.Fatalf("thiếu %s trong %v", w, got)
		}
	}
	if r.Valid() {
		t.Fatal("phải chặn gửi")
	}
	// Nháp: không kiểm mục bắt buộc.
	if r := eval(f, domain.Sheet{}, nil, nil, false); len(r.Issues) != 0 {
		t.Fatalf("nháp trống: %v", rules(r))
	}
	// Kỳ điều chỉnh không thuộc danh sách kỳ đã khóa → lỗi mẫu.
	foreign := ok
	foreign.Adjustments = []domain.Adjustment{{EntryRef: f.item.String(), PeriodID: uuid.NewString(), Amount: "1", Reason: "abc"}}
	if r := eval(f, foreign, prior, adj, true); r.Valid() {
		t.Fatal("kỳ điều chỉnh lạ phải chặn")
	}
}

// BR-DL-05, BR-DL-12: điều chỉnh cho kỳ năm trước lưu vết nhưng không cộng lũy kế năm nay; phần cộng vào lũy kế của bảng
// đã gửi = Thực hiện trong kỳ + điều chỉnh cùng năm.
func TestKH_FN_PN_03_02_PriorYearAdjustmentNotCounted(t *testing.T) {
	f := samplePlan()
	w52 := uuid.New()
	adj := map[uuid.UUID]domain.AdjPeriod{w52: {ID: w52, Name: "Tuần 52 năm 2025", Year: 2025}}
	sh := domain.Sheet{Cells: map[string]domain.CellInput{f.item.String(): {Done: "4"}},
		Adjustments: []domain.Adjustment{{EntryRef: f.item.String(), PeriodID: w52.String(), PeriodYear: 2025, Amount: "-3", Reason: "Ghi thừa năm 2025"}},
		Texts:       map[string]string{"cong_tac_khac": "x"}}
	r := eval(f, sh, nil, adj, true)
	if !r.Valid() || r.Rows[f.item].Cells["H"] != "4" || r.Rows[f.item].Cells["G"] != "" {
		t.Fatalf("năm trước: %v %v", rules(r), r.Rows[f.item].Cells)
	}
	c := domain.Contribution(sh.Normalize(), 2026)
	if !c[f.item].Equal(decimal.NewFromInt(4)) {
		t.Fatalf("phần cộng lũy kế: %v", c)
	}
	_, h1, _ := sh.Normalize().Hash()
	_, h2, _ := sh.Normalize().Hash()
	if string(h1) != string(h2) {
		t.Fatal("băm phải ổn định")
	}
}

// BR-DL-06: phiếu tháng/quý/năm chỉ lời văn.
func TestKH_FN_PN_03_02_MonthTextOnly(t *testing.T) {
	f := samplePlan()
	sh := domain.Sheet{Cells: map[string]domain.CellInput{f.item.String(): {Done: "1"}}, Texts: map[string]string{"cong_tac_khac": "x"}}
	r := domain.Evaluate(domain.Input{Template: domain.TemplateFor(domain.DeptTT), Grid: false, PlanYear: 2026, Sheet: sh.Normalize(), Submit: true})
	if r.Valid() {
		t.Fatal("phiếu tháng có ô số phải chặn")
	}
}
