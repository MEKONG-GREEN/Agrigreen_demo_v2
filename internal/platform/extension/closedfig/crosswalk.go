package closedfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	rcdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// Bảng chuyển đổi kỳ nguồn → kỳ KHTC (ADR-0053 §6, D8: mỗi phòng một lịch tuần; KHTC Thứ Tư – Thứ Ba).
//
// port.CrosswalkV1 ("kn.crosswalk/v1;attribution=source_end"):
//   - mỗi kỳ nguồn thuộc ĐÚNG MỘT kỳ đích — kỳ đích chứa NGÀY CUỐI của kỳ nguồn (lịch đích, PeriodOf); không chia tỷ lệ;
//   - Sum = tổng decimal chính xác các ô số của mọi đơn vị × mọi kỳ nguồn quy vào (kỳ ngày → cộng đúng theo tuần KHTC);
//   - Last = tổng các ô của kỳ nguồn có ngày cuối muộn nhất (lũy kế của nguồn là chuẩn — D18); số tăng của tuần KHTC =
//     Increment(Last tuần trước, Last tuần này);
//   - Closed = mọi kỳ nguồn của biểu quy vào kỳ đích đã chốt và có số đã chốt; KeyClosed = như thế cho mọi biểu của
//     phòng (D17);
//   - Converted = có kỳ nguồn mà khoảng ngày khác khoảng kỳ đích; mỗi kỳ nguồn giữ danh tính (lịch, phiên bản, khóa gốc).
//
// Bất biến: Σ Sum trên mọi kỳ đích = Σ ô của mọi kỳ nguồn có số (không sai số làm tròn — decimal).

type groupKey struct{ dept, prov, form, key string }

type group struct {
	out      port.TargetFigure
	sum      byStatus
	last     byStatus
	lastEnd  time.Time
	hasLast  bool
	allClose bool
}

// byStatus: ô theo trạng thái xác nhận (D2) — confirmed | unconfirmed.
type byStatus map[string]map[string]decimal.Decimal

func (b byStatus) add(o byStatus) {
	for st, cells := range o {
		if b[st] == nil {
			b[st] = map[string]decimal.Decimal{}
		}
		for c, v := range cells {
			b[st][c] = b[st][c].Add(v)
		}
	}
}

func (b byStatus) total() map[string]decimal.Decimal {
	out := map[string]decimal.Decimal{}
	for _, cells := range b {
		for c, v := range cells {
			out[c] = out[c].Add(v)
		}
	}
	return out
}

// Convert quy đổi kỳ nguồn sang lịch đích t (hàm thuần). Kỳ nguồn không có ngày cuối (vụ, đột xuất) bị bỏ qua.
func Convert(figs []port.ClosedFigure, t port.CrosswalkTarget) ([]port.TargetFigure, error) {
	t, tc, err := normTarget(t)
	if err != nil {
		return nil, err
	}
	ver, rule, pt, cal, calVer := t.Version, tc.Rule, t.PeriodType, tc.ID, tc.Version
	groups := map[groupKey]*group{}
	var order []groupKey
	for _, f := range figs {
		if f.PeriodTo == nil {
			continue
		}
		end := dateOnly(*f.PeriodTo)
		span, err := rule.PeriodOf(pt, end, nil)
		if err != nil {
			return nil, fmt.Errorf("closedfig: kỳ đích của %s: %w", sourceKey(f.CalendarID, f.CalendarVersion, f.PeriodKey), err)
		}
		from := end
		if f.PeriodFrom != nil {
			from = dateOnly(*f.PeriodFrom)
		}
		ref := port.SourcePeriodRef{CalendarID: f.CalendarID, CalendarVersion: f.CalendarVersion, NativeKey: f.PeriodKey,
			PeriodType: f.PeriodType, From: from, To: end, Source: f.Source, SourceModule: f.SourceModule, SourcePeriodID: f.SourcePeriodID,
			FormCode: f.FormCode, Closed: f.FormClosed && f.Rows != nil, Converted: !from.Equal(span.From) || !end.Equal(span.To),
			ClosedAt: f.ClosedAt, SnapshotNo: f.SnapshotNo, FiguresHash: f.FiguresHash, StatusHash: f.StatusHash,
			ConfirmationStatus: f.ConfirmationStatus}
		k := groupKey{f.DepartmentCode, f.ProvinceCode, f.FormCode, span.Key}
		g := groups[k]
		if g == nil {
			g = &group{allClose: true, sum: byStatus{}, last: byStatus{},
				out: port.TargetFigure{RuleVersion: ver, DepartmentCode: f.DepartmentCode, ProvinceCode: f.ProvinceCode, FormCode: f.FormCode,
					TargetCalendarID: cal, TargetCalendarVersion: calVer, TargetRule: rule.Tag(), TargetType: pt, TargetKey: span.Key,
					TargetFrom: span.From, TargetTo: span.To}}
			groups[k] = g
			order = append(order, k)
		}
		g.out.Sources = append(g.out.Sources, ref)
		g.out.Converted = g.out.Converted || ref.Converted
		g.allClose = g.allClose && ref.Closed
		if f.Rows == nil {
			continue
		}
		cells, err := rowCells(f.Rows)
		if err != nil {
			return nil, fmt.Errorf("closedfig: rows của %s: %w", sourceKey(f.CalendarID, f.CalendarVersion, f.PeriodKey), err)
		}
		g.sum.add(cells)
		if !g.hasLast || end.After(g.lastEnd) {
			g.last, g.lastEnd, g.hasLast, g.out.LastSourceKey = cells, end, true, sourceKey(f.CalendarID, f.CalendarVersion, f.PeriodKey)
		}
	}
	// KeyClosed: mọi biểu của phòng quy vào cùng kỳ đích.
	keyClosed := map[groupKey]bool{}
	for k, g := range groups {
		kk := groupKey{k.dept, k.prov, "", k.key}
		if v, ok := keyClosed[kk]; ok {
			keyClosed[kk] = v && g.allClose
		} else {
			keyClosed[kk] = g.allClose
		}
	}
	out := make([]port.TargetFigure, 0, len(order))
	for _, k := range order {
		g := groups[k]
		sort.SliceStable(g.out.Sources, func(i, j int) bool { return g.out.Sources[i].To.Before(g.out.Sources[j].To) })
		g.out.Closed = g.allClose
		g.out.KeyClosed = keyClosed[groupKey{k.dept, k.prov, "", k.key}]
		g.out.Sum, g.out.Last = strs(g.sum.total()), strs(g.last.total())
		g.out.SumConfirmed, g.out.SumUnconfirmed = strs(g.sum[port.ConfirmationConfirmed]), strs(g.sum[port.ConfirmationUnconfirmed])
		g.out.LastConfirmed, g.out.LastUnconfirmed = strs(g.last[port.ConfirmationConfirmed]), strs(g.last[port.ConfirmationUnconfirmed])
		g.out.FiguresHash, g.out.StatusHash = combine(g.out.Sources, false), combine(g.out.Sources, true)
		out = append(out, g.out)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.DepartmentCode != b.DepartmentCode {
			return a.DepartmentCode < b.DepartmentCode
		}
		if a.ProvinceCode != b.ProvinceCode {
			return a.ProvinceCode < b.ProvinceCode
		}
		if !a.TargetFrom.Equal(b.TargetFrom) {
			return a.TargetFrom.Before(b.TargetFrom)
		}
		return a.FormCode < b.FormCode
	})
	return out, nil
}

// Increment trả số tăng cur − prev theo từng ô của cur (ô vắng ở prev coi là 0) — số tăng của tuần KHTC tính từ hai
// lũy kế của nguồn (Last). Ô không phải số bị bỏ.
func Increment(prev, cur map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range cur {
		c, err := decimal.NewFromString(v)
		if err != nil {
			continue
		}
		p := decimal.Zero
		if s, ok := prev[k]; ok {
			if d, err := decimal.NewFromString(s); err == nil {
				p = d
			}
		}
		out[k] = c.Sub(p).String()
	}
	return out
}

// sourceKey là danh tính kỳ nguồn "<calendar_id>:v<n>:<native_key>" (D8 — reportcycle/domain.CalendarRef);
// kỳ không theo lịch tuần (phiên bản nil) → "<calendar_id>:-:<native_key>".
func sourceKey(id string, v *int, key string) string {
	if v == nil {
		return id + ":-:" + key
	}
	return rcdomain.CalendarRef(id, *v) + ":" + key
}

// normTarget điền mặc định của lịch đích và trả lịch D8 của nó: thẻ quy tắc rỗng → quy tắc v2 mặc định (Thứ Tư – Thứ Ba)
// → lịch (KHTC, 2); thẻ mang ";cal=<phòng>:v<n>" → đúng lịch đó (reportcycle/domain.CalendarOf).
func normTarget(t port.CrosswalkTarget) (port.CrosswalkTarget, rcdomain.Calendar, error) {
	if t.Version == "" {
		t.Version = port.CrosswalkV1
	}
	if t.Version != port.CrosswalkV1 {
		return t, rcdomain.Calendar{}, fmt.Errorf("closedfig: phiên bản bảng chuyển đổi không hỗ trợ: %s", t.Version)
	}
	if t.PeriodType == "" {
		t.PeriodType = rcdomain.PeriodWeek
	}
	if t.CalendarID == "" {
		t.CalendarID = partyport.DeptKeHoachTaiChinh
	}
	if t.RuleTag == "" {
		t.RuleTag = rcdomain.DefaultRule.Tag()
	}
	c, err := rcdomain.CalendarOf(t.CalendarID, t.RuleTag)
	if err != nil {
		return t, c, fmt.Errorf("closedfig: lịch đích: %w", err)
	}
	t.CalendarID, t.RuleTag = c.ID, c.Rule.Tag()
	return t, c, nil
}

// CellKey là khóa ô của Sum/Last: "<chỉ tiêu>/<ô>" khi dòng có chỉ tiêu, không thì "<ô>".
func CellKey(indicator, cell string) string {
	if indicator == "" {
		return cell
	}
	return indicator + "/" + cell
}

// rowCells cộng các ô số của mọi dòng đơn vị (rows của hợp đồng closed_figures/v1), tách theo trạng thái xác nhận
// (confirmation vắng = confirmed — dòng trước D2).
func rowCells(raw json.RawMessage) (byStatus, error) {
	var rows []struct {
		Indicator    *string           `json:"indicator"`
		Cells        map[string]string `json:"cells"`
		Confirmation string            `json:"confirmation"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	out := byStatus{}
	for _, r := range rows {
		ind := ""
		if r.Indicator != nil {
			ind = *r.Indicator
		}
		st := port.ConfirmationConfirmed
		if r.Confirmation == port.ConfirmationUnconfirmed {
			st = port.ConfirmationUnconfirmed
		}
		if out[st] == nil {
			out[st] = map[string]decimal.Decimal{}
		}
		for c, v := range r.Cells {
			d, err := decimal.NewFromString(strings.TrimSpace(v))
			if err != nil {
				continue
			}
			k := CellKey(ind, c)
			out[st][k] = out[st][k].Add(d)
		}
	}
	return out, nil
}

// combine gộp băm của các kỳ nguồn (theo thứ tự) thành một băm sha256 hex: danh tính kỳ nguồn + băm số (status=false)
// hoặc băm trạng thái xác nhận (status=true). Kỳ chưa có số góp "-".
func combine(refs []port.SourcePeriodRef, status bool) string {
	h := sha256.New()
	for _, r := range refs {
		v := r.FiguresHash
		if status {
			v = r.StatusHash
		}
		x := "-"
		if v != nil {
			x = *v
		}
		_, _ = fmt.Fprintf(h, "%s:%s:%s:%s;", r.SourceModule, sourceKey(r.CalendarID, r.CalendarVersion, r.NativeKey), r.FormCode, x)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func strs(m map[string]decimal.Decimal) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v.String()
	}
	return out
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
