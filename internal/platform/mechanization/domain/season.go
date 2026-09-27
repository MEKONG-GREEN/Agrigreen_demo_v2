package domain

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Season là một vụ cụ thể: mã vụ trong danh mục season × năm bắt đầu vụ.
// Vd Đông Xuân bắt đầu tháng 11/2025 là {DX, 2025}.
type Season struct {
	Code string
	Year int
}

// Key là khóa hiển thị "DX-2025".
func (s Season) Key() string { return s.Code + "-" + strconv.Itoa(s.Year) }

// StartDate là ngày bắt đầu vụ (thời điểm T của cân đối): ngày 1 của tháng
// start_month (thuộc tính danh mục season) trong năm của vụ; thiếu → 01/01.
// Trả thời điểm 00:00 UTC mang ngày nghiệp vụ (chỉ dùng phần ngày).
func (s Season) StartDate(startMonth string) time.Time {
	m, err := strconv.Atoi(startMonth)
	if err != nil || m < 1 || m > 12 {
		m = 1
	}
	return time.Date(s.Year, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
}

// ValidYear kiểm năm vụ.
func ValidYear(y int) bool { return y >= 2000 && y <= 2100 }

// CalendarEntry là lịch một khâu trong vụ theo vùng (FN-06).
type CalendarEntry struct {
	Stage string    `json:"stage"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Lỗi lịch mùa vụ.
var (
	ErrCalendarOrder = errors.New("ngày kết thúc phải sau ngày bắt đầu")
	ErrArea          = errors.New("diện tích phải là số không âm")
)

// ValidateCalendar kiểm lịch: kết thúc sau bắt đầu; cùng khâu không chồng lấn
// (FN-06 luồng ngoại lệ). Trả FieldError với chỉ số dòng.
func ValidateCalendar(entries []CalendarEntry) error {
	byStage := map[string][]int{}
	for i, e := range entries {
		if !e.End.After(e.Start) {
			return FieldError{fmt.Sprintf("calendar[%d].end", i), ErrCalendarOrder}
		}
		byStage[e.Stage] = append(byStage[e.Stage], i)
	}
	for stage, idx := range byStage {
		sort.Slice(idx, func(a, b int) bool { return entries[idx[a]].Start.Before(entries[idx[b]].Start) })
		for k := 1; k < len(idx); k++ {
			prev, cur := entries[idx[k-1]], entries[idx[k]]
			if !cur.Start.After(prev.End) {
				return FieldError{fmt.Sprintf("calendar[%d].start", idx[k]), fmt.Errorf(
					"khâu %s đã có lịch từ %s–%s, không thể thêm khoảng %s–%s chồng lấn", stage,
					prev.Start.Format("02/01"), prev.End.Format("02/01"), cur.Start.Format("02/01"), cur.End.Format("02/01"))}
			}
		}
	}
	return nil
}

// SeasonArea là diện tích canh tác theo vụ của một HTX kèm nhãn nguồn và cờ khóa.
type SeasonArea struct {
	ID             uuid.UUID
	CooperativeRef uuid.UUID
	Season         Season
	CommuneCode    string
	AreaHa         decimal.Decimal
	Source         string // app_htx | manual
	Locked         bool
	Calendar       []CalendarEntry
	SourceAt       time.Time
	LastSeq        int64
}

// Nguồn diện tích (QT-03).
const (
	AreaFromAppHTX = "app_htx"
	AreaManual     = "manual"
)

// Lý do không áp bản ghi đồng bộ.
const ReasonLocked = "bản ghi đã được Admin khóa — giữ giá trị nhập tay (QT-03 mục 3)"

// ResolveArea áp QT-03 cho một giá trị diện tích mới:
//  1. App HTX được ưu tiên khi có dữ liệu hợp lệ;
//  2. thiếu App HTX thì dùng giá trị nhập tay;
//  3. App HTX có lại dữ liệu → ưu tiên trở lại, TRỪ bản ghi Admin đã khóa;
//  4. cả hai đều không có → "Chưa có dữ liệu" (không có bản ghi).
//
// Nhập tay của Admin luôn được áp (Admin hiệu chỉnh khi App HTX trễ/thiếu).
// Trả apply = false kèm lý do khi phải giữ nguyên giá trị hiện tại.
func ResolveArea(cur *SeasonArea, incomingSource string) (apply bool, reason string) {
	if cur == nil || incomingSource == AreaManual {
		return true, ""
	}
	if cur.Locked {
		return false, ReasonLocked
	}
	return true, ""
}
