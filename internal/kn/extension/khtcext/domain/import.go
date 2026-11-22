package domain

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Trạng thái dòng của "Nạp Excel tổng hợp".
const (
	RowOK        = "ok"        // ghi nhận được
	RowOverwrite = "overwrite" // trùng chỉ tiêu và kỳ đã có giá trị → hỏi ghi đè
	RowError     = "error"     // không ghi nhận
)

// PeriodRef là kỳ KHTC dùng để tra cột "Kỳ".
type PeriodRef struct {
	ID        uuid.UUID
	Type      string
	Year      int
	Key       string
	Name      string
	Closed    bool // đã đóng hoặc đã hủy
	Cancelled bool
}

// IndicatorRef là chỉ tiêu dùng để tra cột "Chỉ tiêu" (mã hoặc tên).
type IndicatorRef struct {
	ID   uuid.UUID
	Code string
	Name string
	Unit string
}

// RawRow là một dòng đọc từ tệp (giữ nguyên chữ người dùng gõ).
type RawRow struct {
	Line       int    `json:"line"`
	Indicator  string `json:"indicator"`
	Period     string `json:"period"`
	Value      string `json:"value"`
	Unit       string `json:"unit"`
	Agency     string `json:"agency"`
	Document   string `json:"document"`
	SourceDate string `json:"source_date"`
}

// CheckedRow là kết quả kiểm một dòng.
type CheckedRow struct {
	RawRow
	Status        string     `json:"status"`
	Errors        []string   `json:"errors"`
	IndicatorID   *uuid.UUID `json:"indicator_id"`
	IndicatorName string     `json:"indicator_name"`
	PeriodID      *uuid.UUID `json:"period_id"`
	PeriodName    string     `json:"period_name"`
	Normalized    string     `json:"normalized_value"`
	Current       *string    `json:"current_value"`
}

// CheckResult là kết quả kiểm cả tệp.
type CheckResult struct {
	Rows       []CheckedRow `json:"rows"`
	Valid      int          `json:"valid"`
	Overwrites int          `json:"overwrites"`
	Errors     int          `json:"errors"`
}

var (
	parenRe    = regexp.MustCompile(`\s*\(.*\)\s*$`)
	slashYear  = regexp.MustCompile(`^(.*\S)\s*/\s*(\d{4})$`)
	leadZeroRe = regexp.MustCompile(`\b0+(\d)`)
)

// PeriodLabel chuẩn hóa nhãn kỳ để so khớp: bỏ phần "(từ ngày …)", "X/2026" → "X năm 2026", chữ thường, gộp khoảng trắng.
func PeriodLabel(s string) string {
	s = strings.ToLower(clean(s))
	s = parenRe.ReplaceAllString(s, "")
	if m := slashYear.FindStringSubmatch(s); m != nil {
		s = m[1] + " năm " + m[2]
	}
	s = leadZeroRe.ReplaceAllString(s, "$1")
	return clean(s)
}

// ShortName là tên ngắn của kỳ (bỏ phần trong ngoặc) để hiện và để điền mẫu Excel.
func ShortName(name string) string { return clean(parenRe.ReplaceAllString(clean(name), "")) }

// FigureKey là khóa chỉ tiêu × kỳ.
func FigureKey(period, indicator uuid.UUID) string { return period.String() + ":" + indicator.String() }

// CheckRows kiểm các dòng của tệp nạp (B3): chỉ tiêu phải có trong danh mục; kỳ phải có, chưa đóng; giá trị số; nguồn
// (cơ quan, văn bản hoặc kênh, ngày) bắt buộc. current: giá trị hiệu lực hiện có theo FigureKey (trùng → "ghi đè").
// Hai dòng cùng chỉ tiêu × kỳ trong một tệp → dòng sau lỗi.
func CheckRows(rows []RawRow, periods []PeriodRef, indicators []IndicatorRef, current map[string]string, now time.Time) CheckResult {
	byLabel := map[string][]PeriodRef{}
	for _, p := range periods {
		if p.Cancelled {
			continue
		}
		labels := map[string]bool{PeriodLabel(p.Name): true}
		if p.Key != "" {
			labels[strings.ToLower(p.Key)] = true
		}
		for l := range labels {
			byLabel[l] = append(byLabel[l], p)
		}
	}
	byCode, byName := map[string]IndicatorRef{}, map[string]IndicatorRef{}
	for _, in := range indicators {
		byCode[strings.ToLower(in.Code)] = in
		byName[NameKey(in.Name)] = in
	}
	out := CheckResult{Rows: make([]CheckedRow, 0, len(rows))}
	seen := map[string]int{}
	for _, r := range rows {
		c := CheckedRow{RawRow: r, Errors: []string{}}
		bad := func(msg string) { c.Errors = append(c.Errors, msg) }
		ind, ok := byCode[strings.ToLower(strings.TrimSpace(r.Indicator))]
		if !ok {
			ind, ok = byName[NameKey(r.Indicator)]
		}
		switch {
		case strings.TrimSpace(r.Indicator) == "":
			bad("thiếu chỉ tiêu")
		case !ok:
			bad("chỉ tiêu không có trong danh mục: " + clean(r.Indicator))
		default:
			id := ind.ID
			c.IndicatorID, c.IndicatorName = &id, ind.Name
		}
		cands := byLabel[PeriodLabel(r.Period)]
		if len(cands) == 0 {
			cands = byLabel[strings.ToLower(strings.TrimSpace(r.Period))]
		}
		switch {
		case strings.TrimSpace(r.Period) == "":
			bad("thiếu kỳ")
		case len(cands) == 0:
			bad("kỳ không có hoặc chưa mở: " + clean(r.Period))
		case len(cands) > 1:
			bad("kỳ không duy nhất, ghi mã kỳ (vd 2026-W39): " + clean(r.Period))
		case cands[0].Closed:
			bad("kỳ đã đóng — chỉ xem: " + ShortName(cands[0].Name))
		default:
			id := cands[0].ID
			c.PeriodID, c.PeriodName = &id, ShortName(cands[0].Name)
		}
		if v, err := ParseValue(r.Value); err != nil {
			bad("giá trị: " + err.Error())
		} else {
			c.Normalized = v.String()
		}
		if badText(clean(r.Unit), 50) {
			bad("ĐVT tối đa 50 ký tự")
		}
		if c.Unit = clean(r.Unit); c.Unit == "" && ok {
			c.Unit = ind.Unit
		}
		if _, errs := validSource(r.Agency, r.Document, r.SourceDate, now); len(errs) > 0 {
			for _, e := range errs {
				bad(e.Reason)
			}
		}
		if c.IndicatorID != nil && c.PeriodID != nil {
			k := FigureKey(*c.PeriodID, *c.IndicatorID)
			if line, dup := seen[k]; dup {
				bad("trùng chỉ tiêu và kỳ với dòng " + strconv.Itoa(line))
			} else {
				seen[k] = r.Line
			}
			if cur, has := current[k]; has {
				v := cur
				c.Current = &v
			}
		}
		switch {
		case len(c.Errors) > 0:
			c.Status = RowError
			out.Errors++
		case c.Current != nil:
			c.Status = RowOverwrite
			out.Overwrites++
		default:
			c.Status = RowOK
			out.Valid++
		}
		out.Rows = append(out.Rows, c)
	}
	return out
}

// Template là tên mẫu báo cáo dùng số liệu của loại kỳ ("Dùng trong").
func Template(periodType string) string {
	switch periodType {
	case "week":
		return "Mẫu 1 — Báo cáo tuần"
	case "month":
		return "Mẫu 2 — Báo cáo tháng"
	case "quarter":
		return "Mẫu 3 — Báo cáo quý"
	case "year":
		return "Mẫu 4 — Báo cáo năm"
	case "program":
		return "Mẫu 5 — Tiến độ chương trình"
	case "adhoc":
		return "Báo cáo phát sinh"
	}
	return ""
}
