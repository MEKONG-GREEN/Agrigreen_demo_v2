package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Period là kỳ báo cáo FR-05: tháng (YYYY-MM), quý (YYYY-Qn) hoặc năm (YYYY).
type Period struct {
	Code   string   // như người dùng gửi, đã chuẩn hóa
	Kind   string   // month | quarter | year
	Months []string // các kỳ tháng YYYY-MM thuộc kỳ
	Label  string   // "Tháng 09/2026", "Quý III/2026", "Năm 2026"
}

var (
	monthRe   = regexp.MustCompile(`^([0-9]{4})-(0[1-9]|1[0-2])$`)
	quarterRe = regexp.MustCompile(`^([0-9]{4})-Q([1-4])$`)
	yearRe    = regexp.MustCompile(`^([0-9]{4})$`)
)

// ErrPeriod: kỳ sai định dạng.
var ErrPeriod = errors.New("kỳ phải là YYYY-MM, YYYY-Qn hoặc YYYY")

var roman = []string{"I", "II", "III", "IV"}

// ParsePeriod đọc kỳ báo cáo.
func ParsePeriod(s string) (Period, error) {
	switch {
	case monthRe.MatchString(s):
		m := monthRe.FindStringSubmatch(s)
		return Period{Code: s, Kind: "month", Months: []string{s}, Label: "Tháng " + m[2] + "/" + m[1]}, nil
	case quarterRe.MatchString(s):
		m := quarterRe.FindStringSubmatch(s)
		q, _ := strconv.Atoi(m[2])
		p := Period{Code: s, Kind: "quarter", Label: "Quý " + roman[q-1] + "/" + m[1]}
		for i := 1; i <= 3; i++ {
			p.Months = append(p.Months, fmt.Sprintf("%s-%02d", m[1], (q-1)*3+i))
		}
		return p, nil
	case yearRe.MatchString(s):
		p := Period{Code: s, Kind: "year", Label: "Năm " + s}
		for i := 1; i <= 12; i++ {
			p.Months = append(p.Months, fmt.Sprintf("%s-%02d", s, i))
		}
		return p, nil
	}
	return Period{}, ErrPeriod
}

// MonthOf là kỳ tháng (giờ Việt Nam) chứa t.
func MonthOf(t time.Time) string { return t.In(clock.Zone()).Format("2006-01") }

// PreviousMonth là kỳ tháng liền trước kỳ chứa t (giờ Việt Nam).
func PreviousMonth(t time.Time) string {
	v := t.In(clock.Zone())
	first := time.Date(v.Year(), v.Month(), 1, 0, 0, 0, 0, clock.Zone())
	return first.AddDate(0, -1, 0).Format("2006-01")
}
