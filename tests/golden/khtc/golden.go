// Package khtcgolden nạp bộ dữ liệu đối chứng (golden) liên phòng của KHTC (gói KHTCQA, 13/10/2026): quy đổi kỳ
// nguồn (Trồng trọt, CNTS, Báo giá) sang tuần KHTC và số KHTC tính tay cho các ca trong
// docs/uat/khtc-week-mapping.md. Dữ liệu nhúng vào gói (cases.json, crosswalk.csv) nên bên gọi không cần đường dẫn.
//
// Quy tắc (quyết định 13/10/2026, chờ PO ký): một kỳ nguồn tính vào tuần KHTC chứa NGÀY CUỐI của kỳ nguồn; trong kỳ =
// lũy kế nguồn − gốc, gốc = lũy kế ĐÃ ĐÓNG của tuần KHTC trước nếu tuần đó đã đóng, nếu không thì lũy kế hiện tại của
// tuần trước, chuỗi lũy kế nguồn mới (năm/vụ mới) → gốc 0; lũy kế nguồn là chuẩn; tuần KHTC "đã chốt" khi mọi kỳ nguồn
// quy về tuần đó đã chốt (không có kỳ nguồn → đã chốt).
//
// Chỉ dùng trong kiểm thử. Gói không import mã trong internal/ (kiểm thử quy đổi bằng hàm lịch kỳ W11.03 nằm ở
// internal/kn/extension/reportcycle/domain/khtc_golden_test.go); các gói sau nối dữ liệu này vào kiểm thử tích
// hợp của mình (W11.62–W11.66).
package khtcgolden

import (
	"bytes"
	"embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

//go:embed cases.json crosswalk.csv
var files embed.FS

// Dataset là toàn bộ bộ dữ liệu.
type Dataset struct {
	Version   string            `json:"version"`
	Status    string            `json:"status"`
	Rules     map[string]string `json:"rules"` // tên nguồn → thẻ quy tắc chia kỳ (reportcycle/domain.PeriodRule.Tag) hoặc "day"
	Cases     []Case            `json:"cases"`
	Crosswalk []CrosswalkRow    `json:"-"`
}

// Case là một ca; Kind: cumulative | price | htx | mapping (chỉ quy đổi, không có bước).
type Case struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	Source    string `json:"source"`
	Indicator string `json:"indicator"`
	Unit      string `json:"unit"`
	Steps     []Step `json:"steps"`
}

// Step là trạng thái tại một thời điểm và số KHTC kỳ vọng.
type Step struct {
	Name          string         `json:"name"`
	AsOf          string         `json:"as_of"` // giờ Việt Nam "YYYY-MM-DD HH:MM"
	SourcePeriods []SourcePeriod `json:"source_periods"`
	KHTCClosed    []Frozen       `json:"khtc_closed"`
	Boards        []Board        `json:"boards"`
	Cooperatives  []Cooperative  `json:"cooperatives"`
	Expected      []Expected     `json:"expected"`
	Diff          []DiffEntry    `json:"diff"`
}

// SourcePeriod là một kỳ nguồn như KHTC đọc được (lũy kế = ảnh chụp đã chốt gần nhất, W11.10).
type SourcePeriod struct {
	Key        string          `json:"key"`
	To         string          `json:"to"`
	Cumulative decimal.Decimal `json:"cumulative"`
	Closed     bool            `json:"closed"`
	Series     string          `json:"series"` // chuỗi lũy kế của nguồn (năm, vụ); đổi chuỗi → gốc 0
}

// Frozen là lũy kế của tuần KHTC đã đóng.
type Frozen struct {
	Week       string          `json:"week"`
	Cumulative decimal.Decimal `json:"cumulative"`
}

// Board là bảng giá ngày (Báo giá); Status: open | approving | approved | reopened.
type Board struct {
	Day     string      `json:"day"`
	Status  string      `json:"status"`
	Version int         `json:"version"`
	Tickets int         `json:"tickets"`
	Lines   []BoardLine `json:"lines"`
}

// BoardLine là giá một mặt hàng của một xã báo giá.
type BoardLine struct {
	Point string          `json:"point"`
	Item  string          `json:"item"`
	Min   decimal.Decimal `json:"min"`
	Max   decimal.Decimal `json:"max"`
}

// Cooperative là một HTX tại ngày chốt.
type Cooperative struct {
	Code      string `json:"code"`
	Confirmed bool   `json:"confirmed"`
	Members   int    `json:"members"`
	CreatedBy string `json:"created_by"`
}

// PriceColumn là một cột "Ngày …" của Mẫu 1 A.III.
type PriceColumn struct {
	Day string          `json:"day"`
	Min decimal.Decimal `json:"min"`
	Max decimal.Decimal `json:"max"`
}

// Expected là một dòng số KHTC kỳ vọng của một tuần KHTC.
type Expected struct {
	Week          string           `json:"week"`
	SourceKeys    []string         `json:"source_keys"`
	NoSource      bool             `json:"no_source"`
	Frozen        bool             `json:"frozen"`
	SourceChanged bool             `json:"source_changed"`
	Reissued      *bool            `json:"reissued"`
	Closed        bool             `json:"closed"`
	Cumulative    *decimal.Decimal `json:"cumulative"`
	Base          *decimal.Decimal `json:"base"`
	BaseRef       string           `json:"base_ref"` // frozen:<tuần> | current:<tuần> | reset
	Increment     *decimal.Decimal `json:"increment"`
	YearToDate    *decimal.Decimal `json:"year_to_date"`

	Item         string        `json:"item"`
	ReferenceDay string        `json:"reference_day"`
	Columns      []PriceColumn `json:"columns"`
	Tickets      *int          `json:"tickets"`

	HTXTotal           *int `json:"htx_total"`
	HTXConfirmed       *int `json:"htx_confirmed"`
	HTXUnconfirmed     *int `json:"htx_unconfirmed"`
	MembersTotal       *int `json:"members_total"`
	MembersConfirmed   *int `json:"members_confirmed"`
	MembersUnconfirmed *int `json:"members_unconfirmed"`

	Note string `json:"note"`
}

// DiffEntry là một dòng lịch sử "Cập nhật số liệu".
type DiffEntry struct {
	Week  string          `json:"week"`
	Field string          `json:"field"` // cumulative | increment
	Old   decimal.Decimal `json:"old"`
	New   decimal.Decimal `json:"new"`
	By    string          `json:"by"`
}

// CrosswalkRow là một dòng crosswalk.csv: kỳ nguồn (khóa, ngày đầu, ngày cuối) → tuần KHTC.
type CrosswalkRow struct {
	CaseID, Source, SourceKey, From, To, KHTCWeek, KHTCFrom, KHTCTo string
}

// Load đọc và kiểm tra bộ dữ liệu nhúng.
func Load() (*Dataset, error) {
	raw, err := files.ReadFile("cases.json")
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var d Dataset
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("cases.json: %w", err)
	}
	if d.Crosswalk, err = loadCrosswalk(); err != nil {
		return nil, err
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

var crosswalkHeader = []string{"case_id", "source", "source_key", "from", "to", "khtc_week", "khtc_from", "khtc_to"}

func loadCrosswalk() ([]CrosswalkRow, error) {
	raw, err := files.ReadFile("crosswalk.csv")
	if err != nil {
		return nil, err
	}
	recs, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("crosswalk.csv: %w", err)
	}
	if len(recs) == 0 || !slices.Equal(recs[0], crosswalkHeader) {
		return nil, errors.New("crosswalk.csv: tiêu đề phải là " + strings.Join(crosswalkHeader, ","))
	}
	rows := make([]CrosswalkRow, 0, len(recs)-1)
	for _, r := range recs[1:] {
		rows = append(rows, CrosswalkRow{r[0], r[1], r[2], r[3], r[4], r[5], r[6], r[7]})
	}
	return rows, nil
}

// Case trả ca theo mã.
func (d *Dataset) Case(id string) (Case, bool) {
	for _, c := range d.Cases {
		if c.ID == id {
			return c, true
		}
	}
	return Case{}, false
}

// CrosswalkOf trả các dòng quy đổi của một ca.
func (d *Dataset) CrosswalkOf(caseID string) []CrosswalkRow {
	var out []CrosswalkRow
	for _, r := range d.Crosswalk {
		if r.CaseID == caseID {
			out = append(out, r)
		}
	}
	return out
}

func (d *Dataset) khtcWeekOf(caseID, sourceKey string) (string, bool) {
	for _, r := range d.Crosswalk {
		if r.CaseID == caseID && r.SourceKey == sourceKey {
			return r.KHTCWeek, true
		}
	}
	return "", false
}

// Validate kiểm tra tính nhất quán số học của số tính tay (không thay kiểm thử quy đổi bằng hàm lịch kỳ):
// trong kỳ = lũy kế − gốc, gốc khớp base_ref, cờ đã chốt, lũy kế năm, giá min/max, số phiếu, tổng HTX, lịch sử sửa.
func (d *Dataset) Validate() error {
	if d.Rules["khtc"] == "" {
		return errors.New("thiếu rules.khtc")
	}
	ids := map[string]bool{}
	for _, c := range d.Cases {
		if ids[c.ID] {
			return fmt.Errorf("trùng mã ca %s", c.ID)
		}
		ids[c.ID] = true
		if _, ok := d.Rules[c.Source]; !ok && c.Kind != "htx" {
			return fmt.Errorf("%s: nguồn %q không có trong rules", c.ID, c.Source)
		}
	}
	for _, r := range d.Crosswalk {
		if !ids[r.CaseID] {
			return fmt.Errorf("crosswalk: ca %s không có", r.CaseID)
		}
		if _, ok := d.Rules[r.Source]; !ok {
			return fmt.Errorf("crosswalk %s/%s: nguồn %q không có trong rules", r.CaseID, r.SourceKey, r.Source)
		}
	}
	for _, c := range d.Cases {
		for i, s := range c.Steps {
			var prev *Step
			if i > 0 {
				prev = &c.Steps[i-1]
			}
			var err error
			switch c.Kind {
			case "cumulative":
				err = d.checkCumulative(c, s, prev)
			case "price":
				err = d.checkPrice(c, s)
			case "htx":
				err = checkHTX(s)
			default:
				err = fmt.Errorf("loại ca %q không có bước", c.Kind)
			}
			if err != nil {
				return fmt.Errorf("%s / %s: %w", c.ID, s.Name, err)
			}
		}
	}
	return nil
}

func frozenOf(s Step, week string) (decimal.Decimal, bool) {
	for _, f := range s.KHTCClosed {
		if f.Week == week {
			return f.Cumulative, true
		}
	}
	return decimal.Decimal{}, false
}

func expectedOf(s Step, week string) (Expected, bool) {
	for _, e := range s.Expected {
		if e.Week == week {
			return e, true
		}
	}
	return Expected{}, false
}

func weekYear(key string) (int, int, error) {
	y, w, ok := strings.Cut(key, "-W")
	yy, e1 := strconv.Atoi(y)
	ww, e2 := strconv.Atoi(w)
	if !ok || e1 != nil || e2 != nil {
		return 0, 0, fmt.Errorf("khóa tuần sai dạng %q", key)
	}
	return yy, ww, nil
}

func (d *Dataset) checkCumulative(c Case, s Step, prev *Step) error {
	periods := map[string]SourcePeriod{}
	for _, p := range s.SourcePeriods {
		periods[p.Key] = p
	}
	var last *Expected
	for i := range s.Expected {
		e := s.Expected[i]
		if e.Cumulative == nil || e.Base == nil || e.Increment == nil {
			return fmt.Errorf("%s: thiếu cumulative/base/increment", e.Week)
		}
		if !e.Cumulative.Sub(*e.Base).Equal(*e.Increment) {
			return fmt.Errorf("%s: trong kỳ %s ≠ %s − %s", e.Week, e.Increment, e.Cumulative, e.Base)
		}
		// Gốc khớp base_ref.
		kind, ref, _ := strings.Cut(e.BaseRef, ":")
		switch kind {
		case "frozen":
			v, ok := frozenOf(s, ref)
			if !ok || !v.Equal(*e.Base) {
				return fmt.Errorf("%s: gốc %s không khớp tuần đã đóng %s", e.Week, e.Base, ref)
			}
		case "current":
			p, ok := expectedOf(s, ref)
			if !ok || p.Cumulative == nil || !p.Cumulative.Equal(*e.Base) {
				return fmt.Errorf("%s: gốc %s không khớp lũy kế hiện tại của %s", e.Week, e.Base, ref)
			}
			if _, closed := frozenOf(s, ref); closed {
				return fmt.Errorf("%s: %s đã đóng thì gốc phải là frozen", e.Week, ref)
			}
		case "reset":
			if !e.Base.IsZero() {
				return fmt.Errorf("%s: chuỗi mới thì gốc = 0", e.Week)
			}
		default:
			return fmt.Errorf("%s: base_ref %q", e.Week, e.BaseRef)
		}
		// Lũy kế khớp nguồn / số đã đóng; cờ đã chốt.
		switch {
		case e.Frozen:
			v, ok := frozenOf(s, e.Week)
			if !ok || !v.Equal(*e.Cumulative) {
				return fmt.Errorf("%s: tuần đã đóng phải giữ %s", e.Week, v)
			}
			changed := false
			for _, k := range e.SourceKeys {
				if p, ok := periods[k]; ok && !p.Cumulative.Equal(v) {
					changed = true
				}
			}
			if changed != e.SourceChanged {
				return fmt.Errorf("%s: source_changed = %v, tính được %v", e.Week, e.SourceChanged, changed)
			}
		case e.NoSource:
			if len(e.SourceKeys) != 0 || !e.Cumulative.Equal(*e.Base) || !e.Closed {
				return fmt.Errorf("%s: tuần không có kỳ nguồn: lũy kế = gốc, đã chốt", e.Week)
			}
		default:
			if len(e.SourceKeys) == 0 {
				return fmt.Errorf("%s: thiếu source_keys", e.Week)
			}
			var latest SourcePeriod
			closed := true
			for _, k := range e.SourceKeys {
				p, ok := periods[k]
				if !ok {
					return fmt.Errorf("%s: kỳ nguồn %s không có trong bước", e.Week, k)
				}
				if p.To > latest.To {
					latest = p
				}
				closed = closed && p.Closed
			}
			if !latest.Cumulative.Equal(*e.Cumulative) {
				return fmt.Errorf("%s: lũy kế %s ≠ lũy kế nguồn %s", e.Week, e.Cumulative, latest.Cumulative)
			}
			if closed != e.Closed {
				return fmt.Errorf("%s: closed = %v, tính được %v", e.Week, e.Closed, closed)
			}
		}
		for _, k := range e.SourceKeys {
			w, ok := d.khtcWeekOf(c.ID, k)
			if !ok || w != e.Week {
				return fmt.Errorf("%s: crosswalk của %s là %q", e.Week, k, w)
			}
		}
		// Lũy kế năm KHTC = Σ trong kỳ các tuần cùng năm.
		if e.YearToDate != nil {
			y, no, err := weekYear(e.Week)
			if err != nil {
				return err
			}
			if no == 1 && !e.YearToDate.Equal(*e.Increment) {
				return fmt.Errorf("%s: tuần 1 thì lũy kế năm = trong kỳ", e.Week)
			}
			if last != nil && last.YearToDate != nil {
				if py, _, _ := weekYear(last.Week); py == y && !last.YearToDate.Add(*e.Increment).Equal(*e.YearToDate) {
					return fmt.Errorf("%s: lũy kế năm %s ≠ %s + %s", e.Week, e.YearToDate, last.YearToDate, e.Increment)
				}
			}
		}
		last = &s.Expected[i]
	}
	for _, df := range s.Diff {
		if prev == nil {
			return errors.New("diff ở bước đầu")
		}
		now, ok1 := expectedOf(s, df.Week)
		was, ok2 := expectedOf(*prev, df.Week)
		if !ok1 || !ok2 {
			return fmt.Errorf("diff %s: tuần không có ở hai bước", df.Week)
		}
		pick := func(e Expected) *decimal.Decimal {
			if df.Field == "cumulative" {
				return e.Cumulative
			}
			return e.Increment
		}
		if !pick(was).Equal(df.Old) || !pick(now).Equal(df.New) || df.By == "" {
			return fmt.Errorf("diff %s %s: %s → %s không khớp hai bước", df.Week, df.Field, df.Old, df.New)
		}
	}
	return nil
}

func (d *Dataset) checkPrice(c Case, s Step) error {
	for _, e := range s.Expected {
		var approved []Board
		tickets, all := 0, true
		for _, b := range s.Boards {
			inWeek := false
			if w, ok := d.khtcWeekOf(c.ID, b.Day); ok && w == e.Week {
				inWeek = true
			}
			if b.Status == "approved" {
				if b.Day <= e.ReferenceDay {
					approved = append(approved, b)
				}
				if inWeek {
					tickets += b.Tickets
				}
			} else if inWeek {
				all = false
			}
		}
		slices.SortFunc(approved, func(a, b Board) int { return strings.Compare(a.Day, b.Day) })
		if len(approved) > 2 {
			approved = approved[len(approved)-2:]
		}
		if len(approved) != len(e.Columns) {
			return fmt.Errorf("%s: %d cột, có %d ngày đã duyệt", e.Week, len(e.Columns), len(approved))
		}
		for i, col := range e.Columns {
			b := approved[i]
			if col.Day != b.Day {
				return fmt.Errorf("%s: cột %d là %s, ngày đã duyệt gần nhất là %s", e.Week, i+1, col.Day, b.Day)
			}
			var lo, hi *decimal.Decimal
			for _, l := range b.Lines {
				if l.Item != e.Item {
					continue
				}
				if lo == nil || l.Min.LessThan(*lo) {
					lo = &l.Min
				}
				if hi == nil || l.Max.GreaterThan(*hi) {
					hi = &l.Max
				}
			}
			if lo == nil || !lo.Equal(col.Min) || !hi.Equal(col.Max) {
				return fmt.Errorf("%s ngày %s: min/max kỳ vọng %s–%s", e.Week, col.Day, col.Min, col.Max)
			}
		}
		if e.Tickets == nil || *e.Tickets != tickets {
			return fmt.Errorf("%s: số phiếu đã duyệt trong tuần = %d", e.Week, tickets)
		}
		if e.Closed != all {
			return fmt.Errorf("%s: closed = %v, tính được %v", e.Week, e.Closed, all)
		}
	}
	return nil
}

func checkHTX(s Step) error {
	var n, nc, m, mc int
	for _, h := range s.Cooperatives {
		n++
		m += h.Members
		if h.Confirmed {
			nc++
			mc += h.Members
		}
	}
	for _, e := range s.Expected {
		if e.HTXTotal == nil || e.HTXConfirmed == nil || e.HTXUnconfirmed == nil || e.MembersTotal == nil || e.MembersConfirmed == nil || e.MembersUnconfirmed == nil {
			return fmt.Errorf("%s: thiếu số HTX", e.Week)
		}
		if *e.HTXTotal != *e.HTXConfirmed+*e.HTXUnconfirmed || *e.MembersTotal != *e.MembersConfirmed+*e.MembersUnconfirmed {
			return fmt.Errorf("%s: tổng ≠ đã xác nhận + chưa xác nhận", e.Week)
		}
		if e.Frozen {
			continue
		}
		if *e.HTXTotal != n || *e.HTXConfirmed != nc || *e.MembersTotal != m || *e.MembersConfirmed != mc {
			return fmt.Errorf("%s: kỳ vọng %d/%d HTX, %d/%d thành viên; danh sách cho %d/%d, %d/%d", e.Week,
				*e.HTXTotal, *e.HTXConfirmed, *e.MembersTotal, *e.MembersConfirmed, n, nc, m, mc)
		}
	}
	return nil
}
