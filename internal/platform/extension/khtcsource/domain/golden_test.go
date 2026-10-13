package domain_test

// Đối chứng KHTCQA (tests/golden/khtc — docs/uat/khtc-week-mapping.md) qua ĐÚNG đường của "Cập nhật số liệu": kỳ nguồn
// dạng hợp đồng closed_figures/v1 (port.ClosedFigure) → bảng chuyển đổi PFPUB (closedfig.Convert) → kỳ KHTC
// (closedfig.Assemble, như port KHTCSources.Periods) → số của khóa nguồn (domain.Evaluate) → lịch sử (domain.Diff).
// Mọi số so bằng decimal, chênh lệch phải bằng 0.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/closedfig"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	khtcgolden "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/golden/khtc"
)

// goldenSource: nguồn của ca → phòng, biểu, module, khóa nguồn (cách lấy theo chỉ tiêu: CNTS lũy kế từ đầu năm, Trồng
// trọt lũy kế vụ).
type goldenSource struct{ dept, form, module, key string }

var goldenSources = map[string]goldenSource{
	"cnts":          {"chan_nuoi_thuy_san", "BM-04", "pf/forms", "chan_nuoi_thuy_san:bm-04:luy_ke"},
	"trong_trot":    {"trong_trot", "PL01", "pf/extension", "season:trong_trot:pl01:lua_td:luy_ke"},
	"trong_trot_v1": {"trong_trot", "PL01", "pf/extension", "season:trong_trot:pl01:lua_td:luy_ke"},
}

func mustDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func sha(s string) *string {
	h := sha256.Sum256([]byte(s))
	x := hex.EncodeToString(h[:])
	return &x
}

func khtcTarget(t *testing.T, ds *khtcgolden.Dataset) (port.CrosswalkTarget, rc.Calendar) {
	t.Helper()
	cal, err := rc.CalendarOf(rc.DeptKeHoachTaiChinh, ds.Rules["khtc"])
	if err != nil {
		t.Fatal(err)
	}
	return port.CrosswalkTarget{Version: port.CrosswalkV1, CalendarID: cal.ID, RuleTag: cal.Rule.Tag(), PeriodType: rc.PeriodWeek}, cal
}

func weekSpan(t *testing.T, cal rc.Calendar, key string) rc.Span {
	t.Helper()
	s, err := rc.SpanOfKey(cal.Rule, rc.PeriodWeek, key, nil)
	if err != nil {
		t.Fatalf("tuần KHTC %s: %v", key, err)
	}
	return s
}

// figure dựng một kỳ nguồn đã chốt (dòng toàn tỉnh, ô "<chỉ tiêu>/<ô>") như view closed_figures/v1.
func figure(t *testing.T, ds *khtcgolden.Dataset, caseID, source string, p khtcgolden.SourcePeriod) port.ClosedFigure {
	t.Helper()
	gs := goldenSources[source]
	cal, err := rc.CalendarOf(gs.dept, ds.Rules[source])
	if err != nil {
		t.Fatal(err)
	}
	var from time.Time
	for _, r := range ds.CrosswalkOf(caseID) {
		if r.SourceKey == p.Key {
			from = mustDay(t, r.From)
		}
	}
	if from.IsZero() {
		t.Fatalf("%s: crosswalk thiếu kỳ nguồn %s", caseID, p.Key)
	}
	to := mustDay(t, p.To)
	ver := cal.Version
	ind := "" // CNTS: ô theo khóa trường; Trồng trọt: dòng theo chỉ tiêu × ô
	if gs.dept == "trong_trot" {
		ind = `"indicator":"LUA_TD",`
	}
	cells := `{"key":"tinh","unit":"tinh",` + ind + `"cells":{"luy_ke":"` + p.Cumulative.String() + `"},"confirmation":"confirmed"}`
	sn := int64(1)
	return port.ClosedFigure{DepartmentCode: gs.dept, CalendarID: cal.ID, CalendarVersion: &ver, PeriodType: rc.PeriodWeek, PeriodKey: p.Key,
		PeriodFrom: &from, PeriodTo: &to, SourceModule: gs.module, SourcePeriodID: uuid.NewSHA1(uuid.NameSpaceURL, []byte(caseID+p.Key)),
		FormCode: gs.form, FormClosed: p.Closed, PeriodClosed: p.Closed, KeyClosed: p.Closed, KeyClosedAll: p.Closed, ClosedAt: &to,
		SnapshotNo: &sn, Rows: json.RawMessage("[" + cells + "]"), Totals: json.RawMessage("[]"), ConfirmationStatus: port.ConfirmationConfirmed,
		FiguresHash: sha(p.Cumulative.String()), StatusHash: sha("tinh=confirmed")}
}

// periods chạy bảng chuyển đổi + dựng kỳ KHTC trên [from, to] cho các phòng depts.
func periods(t *testing.T, ds *khtcgolden.Dataset, figs []port.ClosedFigure, from, to time.Time, depts []string, price bool) []port.KHTCPeriod {
	t.Helper()
	tg, cal := khtcTarget(t, ds)
	conv, err := closedfig.Convert(figs, tg)
	if err != nil {
		t.Fatal(err)
	}
	return closedfig.Assemble(conv, tg, cal, from, to, depts, price, price)
}

func eq(t *testing.T, what string, got *decimal.Decimal, want *decimal.Decimal) {
	t.Helper()
	if want == nil {
		return
	}
	if got == nil || !got.Equal(*want) {
		t.Errorf("%s: có %v, cần %s (chênh lệch phải 0)", what, got, want)
	}
}

func nativeKeys(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, s := range ids {
		out = append(out, s[strings.LastIndex(s, ":")+1:])
	}
	return out
}

// cumulativeStep tính số mọi tuần kỳ vọng của một bước; trả số theo tuần (cho lịch sử).
func cumulativeStep(t *testing.T, ds *khtcgolden.Dataset, c khtcgolden.Case, s khtcgolden.Step) map[string]domain.Value {
	t.Helper()
	gs := goldenSources[c.Source]
	k, err := domain.ParseSourceKey(gs.key)
	if err != nil {
		t.Fatal(err)
	}
	_, cal := khtcTarget(t, ds)
	var figs []port.ClosedFigure
	for _, p := range s.SourcePeriods {
		figs = append(figs, figure(t, ds, c.ID, c.Source, p))
	}
	keys := []string{}
	for _, f := range s.KHTCClosed {
		keys = append(keys, f.Week)
	}
	for _, e := range s.Expected {
		keys = append(keys, e.Week)
	}
	slices.SortFunc(keys, func(a, b string) int { return weekSpan(t, cal, a).From.Compare(weekSpan(t, cal, b).From) })
	wFrom, wTo := weekSpan(t, cal, keys[0]).From, weekSpan(t, cal, keys[len(keys)-1]).To
	frozen := map[string]domain.Frozen{}
	for _, f := range s.KHTCClosed {
		fr := domain.Frozen{Cumulative: f.Cumulative}
		if f.Week == keys[0] {
			// Hạt giống lũy kế năm: tuần đã đóng đầu cửa sổ mang lũy kế năm = lũy kế (số liệu golden bắt đầu giữa năm;
			// chỉ so lũy kế năm ở ca có chuỗi theo năm).
			ytd := f.Cumulative
			fr.YearToDate = &ytd
		}
		frozen[f.Week] = fr
	}
	ps := periods(t, ds, figs, wFrom, wTo, []string{gs.dept}, false)
	out := map[string]domain.Value{}
	for _, e := range s.Expected {
		sp := weekSpan(t, cal, e.Week)
		v := domain.Evaluate(k, ps, frozen, sp.From, sp.To)
		out[e.Week] = v
		at := fmt.Sprintf("%s / %s / %s", c.ID, s.Name, e.Week)
		eq(t, at+" lũy kế", v.Cumulative, e.Cumulative)
		eq(t, at+" gốc", v.Base, e.Base)
		eq(t, at+" trong kỳ", v.Increment, e.Increment)
		eq(t, at+" lũy kế năm", v.YearToDate, e.YearToDate)
		if v.BaseRef != e.BaseRef {
			t.Errorf("%s: gốc lấy từ %q, cần %q", at, v.BaseRef, e.BaseRef)
		}
		if v.Closed != e.Closed || v.NoSource != e.NoSource || v.Frozen != e.Frozen || v.SourceChanged != e.SourceChanged {
			t.Errorf("%s: closed/no_source/frozen/source_changed = %v/%v/%v/%v, cần %v/%v/%v/%v", at, v.Closed, v.NoSource, v.Frozen,
				v.SourceChanged, e.Closed, e.NoSource, e.Frozen, e.SourceChanged)
		}
		if got := nativeKeys(v.SourceKeys); !slices.Equal(got, e.SourceKeys) && !(len(got) == 0 && len(e.SourceKeys) == 0) {
			t.Errorf("%s: kỳ nguồn %v, cần %v", at, got, e.SourceKeys)
		}
	}
	return out
}

// TestKH_W1164_IncrementsFromCumulativesGolden: G01–G05B — lũy kế của nguồn (D18), trong kỳ = lũy kế − gốc (frozen /
// current / reset), tuần trống (M1), tuần đã đóng giữ số + "Nguồn đã thay đổi" (M2), lũy kế năm (M3), lịch sử G03 s3.
func TestKH_W1164_IncrementsFromCumulativesGolden(t *testing.T) {
	ds, err := khtcgolden.Load()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range ds.Cases {
		if c.Kind != "cumulative" {
			continue
		}
		var prev map[string]domain.Value
		for _, s := range c.Steps {
			cur := cumulativeStep(t, ds, c, s)
			n += len(s.Expected)
			if len(s.Diff) > 0 {
				// "Cập nhật số liệu" chạy ở từng kỳ tuần: lịch sử của kỳ = Diff(ảnh chụp trước của kỳ, ảnh chụp mới).
				var got []khtcgolden.DiffEntry
				for _, w := range slices.Sorted(maps.Keys(cur)) {
					for _, ch := range domain.Diff(map[string]domain.Value{"k": prev[w]}, map[string]domain.Value{"k": cur[w]}) {
						got = append(got, khtcgolden.DiffEntry{Week: w, Field: ch.Field, Old: *ch.Old, New: *ch.New, By: s.Diff[0].By})
					}
				}
				if len(got) != len(s.Diff) {
					t.Fatalf("%s / %s: lịch sử %+v, cần %+v", c.ID, s.Name, got, s.Diff)
				}
				for i, d := range s.Diff {
					g := got[i]
					if g.Week != d.Week || g.Field != d.Field || !g.Old.Equal(d.Old) || !g.New.Equal(d.New) {
						t.Errorf("%s: lịch sử dòng %d = %+v, cần %+v", c.ID, i, g, d)
					}
				}
			}
			prev = cur
		}
	}
	if n < 15 {
		t.Fatalf("chỉ đối chứng %d tuần", n)
	}
}

// TestKH_FN_TI_02_05_PriceRangeApprovedBoards: G02B — giá min/max toàn tỉnh của bảng giá đã duyệt (D22), hai ngày đã
// duyệt gần nhất không sau ngày chốt, số phiếu chỉ đếm ngày đã duyệt, "Tạm tính" khi còn ngày chưa duyệt.
func TestKH_FN_TI_02_05_PriceRangeApprovedBoards(t *testing.T) {
	ds, err := khtcgolden.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, ok := ds.Case("G02B")
	if !ok {
		t.Fatal("thiếu G02B")
	}
	_, cal := khtcTarget(t, ds)
	for _, s := range c.Steps {
		var boards []closedfig.PriceBoardRow
		var tickets []closedfig.PriceTicketRow
		for _, b := range s.Boards {
			day := mustDay(t, b.Day)
			w := weekSpan(t, cal, mustWeekKey(t, cal, day))
			approved := b.Status == "approved"
			n := 0
			if approved {
				n = b.Tickets
				// Dòng bảng giá của view là mức toàn tỉnh mỗi mặt hàng: min/max các xã báo giá (pf/price tổng hợp).
				agg := map[string][2]decimal.Decimal{}
				var items []string
				for _, l := range b.Lines {
					a, seen := agg[l.Item]
					if !seen {
						a = [2]decimal.Decimal{l.Min, l.Max}
						items = append(items, l.Item)
					}
					a[0], a[1] = decimal.Min(a[0], l.Min), decimal.Max(a[1], l.Max)
					agg[l.Item] = a
				}
				for _, it := range items {
					boards = append(boards, closedfig.PriceBoardRow{BoardDate: day, BoardVersion: int64(b.Version), ApprovedAt: day, ItemCode: it,
						PriceMin: agg[it][0].String(), PriceMax: agg[it][1].String(), PointsReported: len(b.Lines)})
				}
			}
			tickets = append(tickets, closedfig.PriceTicketRow{WeekKey: w.Key, WeekFrom: w.From, WeekTo: w.To, PointCode: "D" + b.Day,
				TicketsCollected: n, DaysScheduled: 1, DaysApproved: map[bool]int{true: 1}[approved], WeekClosed: approved})
		}
		figs := closedfig.PriceFigures("", boards, tickets)
		for _, e := range s.Expected {
			sp := weekSpan(t, cal, e.Week)
			ps := periods(t, ds, figs, sp.From, sp.To, []string{closedfig.PriceDepartment}, true)
			kb, _ := domain.ParseSourceKey("thong_tin_truyen_thong:price-board:" + strings.ToLower(e.Item))
			kt, _ := domain.ParseSourceKey("thong_tin_truyen_thong:price-tickets:tickets_collected")
			v := domain.Evaluate(kb, ps, nil, sp.From, mustDay(t, e.ReferenceDay))
			if len(v.PriceDays) != len(e.Columns) {
				t.Fatalf("%s: %d cột, cần %d: %+v", s.Name, len(v.PriceDays), len(e.Columns), v.PriceDays)
			}
			for i, col := range e.Columns {
				g := v.PriceDays[i]
				if g.Day != col.Day || !g.Min.Equal(col.Min) || !g.Max.Equal(col.Max) {
					t.Errorf("%s cột %d: %+v, cần %+v", s.Name, i, g, col)
				}
			}
			last := e.Columns[len(e.Columns)-1]
			eq(t, s.Name+" giá từ", v.Min, &last.Min)
			eq(t, s.Name+" giá đến", v.Max, &last.Max)
			tk := domain.Evaluate(kt, ps, nil, sp.From, sp.To)
			want := decimal.NewFromInt(int64(*e.Tickets))
			eq(t, s.Name+" số phiếu", tk.Sum, &want)
			if v.Closed != e.Closed || tk.Closed != e.Closed || v.Fallback {
				t.Errorf("%s: closed giá/phiếu %v/%v (cần %v), fallback %v", s.Name, v.Closed, tk.Closed, e.Closed, v.Fallback)
			}
		}
	}
}

func mustWeekKey(t *testing.T, cal rc.Calendar, day time.Time) string {
	t.Helper()
	s, err := cal.Rule.PeriodOf(rc.PeriodWeek, day, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s.Key
}

// TestKH_W1164_UnconfirmedHTXSplitGolden: G06 — HTX "Chưa xác nhận" vào tổng, tách đã/chưa xác nhận; nhận HTX chỉ đổi
// nhãn (tổng giữ); tuần đã gửi giữ số (Frozen).
func TestKH_W1164_UnconfirmedHTXSplitGolden(t *testing.T) {
	ds, err := khtcgolden.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, _ := ds.Case("G06")
	_, cal := khtcTarget(t, ds)
	kh, _ := domain.ParseSourceKey("cum:hop_tac_xa:htx-tong:so_htx")
	km, _ := domain.ParseSourceKey("cum:hop_tac_xa:htx-tong:thanh_vien")
	frozenH, frozenM := map[string]domain.Frozen{}, map[string]domain.Frozen{} // số đã giữ theo khóa nguồn
	var figs []port.ClosedFigure
	for _, s := range c.Steps {
		day := mustDay(t, s.AsOf[:10])
		var rows []map[string]any
		for _, h := range s.Cooperatives {
			st := port.ConfirmationUnconfirmed
			if h.Confirmed {
				st = port.ConfirmationConfirmed
			}
			rows = append(rows, map[string]any{"key": h.Code, "unit": h.Code, "unit_kind": port.UnitKindCooperative, "confirmation": st,
				"cells": map[string]string{"so_htx": "1", "thanh_vien": fmt.Sprint(h.Members)}})
		}
		raw, _ := json.Marshal(rows)
		ver := rc.CalendarVersionDefault
		figs = append(figs, port.ClosedFigure{DepartmentCode: "hop_tac_xa", CalendarID: "hop_tac_xa", CalendarVersion: &ver, PeriodType: rc.PeriodDay,
			PeriodKey: s.AsOf[:10], PeriodFrom: &day, PeriodTo: &day, SourceModule: "pf/cooperative", FormCode: "HTX-TONG", FormClosed: true,
			PeriodClosed: true, KeyClosed: true, KeyClosedAll: true, Rows: raw, Totals: json.RawMessage("[]")})
		for _, e := range s.Expected {
			sp := weekSpan(t, cal, e.Week)
			ps := periods(t, ds, figs, weekSpan(t, cal, s.Expected[0].Week).From, sp.To, []string{"hop_tac_xa"}, false)
			vh, vm := domain.Evaluate(kh, ps, frozenH, sp.From, sp.To), domain.Evaluate(km, ps, frozenM, sp.From, sp.To)
			at := s.Name + " " + e.Week
			check := func(what string, total, unconf *decimal.Decimal, wt, wc, wu int) {
				conf := total.Sub(*unconf)
				if !total.Equal(decimal.NewFromInt(int64(wt))) || !conf.Equal(decimal.NewFromInt(int64(wc))) || !unconf.Equal(decimal.NewFromInt(int64(wu))) {
					t.Errorf("%s %s: %s = %s + %s, cần %d = %d + %d", at, what, total, conf, unconf, wt, wc, wu)
				}
			}
			check("HTX", vh.Cumulative, vh.Unconfirmed, *e.HTXTotal, *e.HTXConfirmed, *e.HTXUnconfirmed)
			check("thành viên", vm.Cumulative, vm.Unconfirmed, *e.MembersTotal, *e.MembersConfirmed, *e.MembersUnconfirmed)
			if vh.Frozen != e.Frozen || vh.Closed != e.Closed {
				t.Errorf("%s: frozen/closed %v/%v", at, vh.Frozen, vh.Closed)
			}
			if !e.Frozen {
				// Kỳ đã gửi: giữ số của ảnh chụp (Q10 — không phát hành lại).
				frozenH[e.Week] = domain.Frozen{Cumulative: *vh.Cumulative, Unconfirmed: vh.Unconfirmed}
				frozenM[e.Week] = domain.Frozen{Cumulative: *vm.Cumulative, Unconfirmed: vm.Unconfirmed}
			}
		}
	}
}

// TestKH_FN_TI_02_03_AttachSourcePeriodsGolden: mọi dòng crosswalk.csv (trừ Báo giá — ở ca giá) — kỳ nguồn nằm trong
// danh sách kỳ nguồn của đúng tuần KHTC chứa ngày cuối (Q3).
func TestKH_FN_TI_02_03_AttachSourcePeriodsGolden(t *testing.T) {
	ds, err := khtcgolden.Load()
	if err != nil {
		t.Fatal(err)
	}
	_, cal := khtcTarget(t, ds)
	n := 0
	for _, r := range ds.Crosswalk {
		gs, ok := goldenSources[r.Source]
		if !ok {
			continue
		}
		f := figure(t, ds, r.CaseID, r.Source, khtcgolden.SourcePeriod{Key: r.SourceKey, To: r.To, Cumulative: decimal.NewFromInt(1), Closed: true})
		sp := weekSpan(t, cal, r.KHTCWeek)
		if sp.From.Format(time.DateOnly) != r.KHTCFrom || sp.To.Format(time.DateOnly) != r.KHTCTo {
			t.Fatalf("%s: tuần KHTC %s = %s–%s", r.CaseID, r.KHTCWeek, sp.From, sp.To)
		}
		k, _ := domain.ParseSourceKey(gs.key)
		ps := periods(t, ds, []port.ClosedFigure{f}, sp.From.AddDate(0, 0, -7), sp.To.AddDate(0, 0, 7), []string{gs.dept}, false)
		v := domain.Evaluate(k, ps, nil, sp.From, sp.To)
		if !slices.Equal(nativeKeys(v.SourceKeys), []string{r.SourceKey}) {
			t.Errorf("%s %s → %s: kỳ nguồn của tuần %v", r.CaseID, r.SourceKey, r.KHTCWeek, v.SourceKeys)
		}
		n++
	}
	if n < 20 {
		t.Fatalf("chỉ %d dòng", n)
	}
}
