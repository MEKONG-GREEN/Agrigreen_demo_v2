package domain

// Bảng giá ngày có phiên bản (W11.76, pf 000151; BRD Báo giá nông sản v1.3 FN-TI-02.05 Duyệt, FN-TI-02.06 Mở duyệt,
// Mục II.3.3, II.4 CT-04). Thuần: trạng thái nhìn tại lúc đọc (khóa "Đang duyệt" hết hạn → trạng thái trước khóa), tổng
// hợp dòng bảng giá, giới hạn mở duyệt, tuần báo cáo mặc định (QĐ-6) khi gốc lắp ráp chưa nối lịch kỳ hệ thống.

import (
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Thao tác ghi sổ bảng giá (cột action của pf_price.board).
const (
	BoardLock         = "lock"          // "Duyệt bảng giá" → Đang duyệt (giữ khóa)
	BoardUnlock       = "unlock"        // hủy duyệt → trạng thái trước khóa
	BoardApprove      = "approve"       // xác nhận duyệt → Đã duyệt vN+1
	BoardReopen       = "reopen"        // Mở duyệt (lý do) → Đang mở duyệt
	BoardCancelReopen = "cancel_reopen" // Hủy mở duyệt (chưa sửa gì) → Đã duyệt vN
)

// BoardLockTTL là thời hạn khóa "Đang duyệt" (một cán bộ đang thao tác — BR-02 FN-TI-02.05). Hết hạn mà chưa xác nhận →
// khóa tự nhả (trạng thái trước khóa) để người khác duyệt được. Giả định dev (BRD không nêu thời hạn).
const BoardLockTTL = 10 * time.Minute

// BoardReopenDays mặc định: chỉ mở duyệt ngày báo giá trong 30 ngày gần nhất (BR-06 FN-TI-02.06 — "cần xác nhận", Q-086
// / Q-052; Profile.Board.ReopenDays đổi được).
const BoardReopenDays = 30

// Board là phiên bản hiện hành của bảng giá một ngày.
type Board struct {
	Seq        int64 // 0 = chưa có sổ (Chưa duyệt)
	Action     string
	Status     string
	BaseStatus string
	LockUntil  *time.Time
	Version    int // phiên bản đã duyệt hiện hành (0 = chưa duyệt)
	ApprovedAt *time.Time
	Reason     string
	Actor      uuid.UUID
	EventTime  time.Time
	RecordedAt time.Time // giờ ghi sổ (máy chủ CSDL) — mốc "đã sửa sau khi mở duyệt"
}

// View trả trạng thái nhìn tại now: khóa "Đang duyệt" hết hạn → trạng thái trước khóa; chưa có sổ → Chưa duyệt.
func (b Board) View(now time.Time) string {
	switch {
	case b.Seq == 0 || b.Status == "":
		return BoardOpen
	case b.Status == BoardApproving && b.LockUntil != nil && !now.Before(*b.LockUntil):
		return b.BaseStatus
	}
	return b.Status
}

// LockedBy trả người giữ khóa "Đang duyệt" còn hạn tại now (Nil nếu không ai giữ).
func (b Board) LockedBy(now time.Time) uuid.UUID {
	if b.View(now) == BoardApproving {
		return b.Actor
	}
	return uuid.Nil
}

// Published cho biết ngày đã có bảng giá duyệt (vN, kể cả đang mở duyệt / đang duyệt lại) — giá tham chiếu CT-02.
func (b Board) Published() bool { return b.Version >= 1 }

// ReopenWithin cho biết ngày báo giá day còn trong giới hạn mở duyệt (days ngày gần nhất tính tới today, BR-06).
func ReopenWithin(day, today time.Time, days int) bool {
	if days <= 0 {
		days = BoardReopenDays
	}
	return !day.Before(today.AddDate(0, 0, -days))
}

// BoardSource là một dòng giá tính vào bảng giá (phiếu Đã gửi / Nộp muộn, có giá, không bị loại; giá sau hiệu chỉnh).
type BoardSource struct {
	Point    string
	Item     string
	Min, Max decimal.Decimal
	Mid      decimal.Decimal
}

// BoardLine là một mặt hàng của bảng giá (CT-04): min / max toàn tỉnh, trung bình cộng P các xã, số xã có giá.
type BoardLine struct {
	Item   string
	Min    decimal.Decimal
	Max    decimal.Decimal
	Avg    decimal.Decimal
	Points int
}

// AggregateBoard tổng hợp dòng giá theo mặt hàng (CT-04), sắp theo mã mặt hàng. Mỗi xã một phiếu một ngày → mỗi mặt hàng
// tối đa một P mỗi xã; trung bình làm tròn 4 số lẻ.
func AggregateBoard(src []BoardSource) []BoardLine {
	type acc struct {
		line BoardLine
		sum  decimal.Decimal
		pts  map[string]bool
	}
	by := map[string]*acc{}
	for _, s := range src {
		a, ok := by[s.Item]
		if !ok {
			a = &acc{line: BoardLine{Item: s.Item, Min: s.Min, Max: s.Max}, pts: map[string]bool{}}
			by[s.Item] = a
		}
		if s.Min.LessThan(a.line.Min) {
			a.line.Min = s.Min
		}
		if s.Max.GreaterThan(a.line.Max) {
			a.line.Max = s.Max
		}
		a.sum = a.sum.Add(s.Mid)
		a.line.Points++
		a.pts[s.Point] = true
	}
	out := make([]BoardLine, 0, len(by))
	for _, a := range by {
		a.line.Avg = a.sum.DivRound(decimal.NewFromInt(int64(a.line.Points)), 4)
		a.line.Points = len(a.pts)
		out = append(out, a.line)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Item < out[j].Item })
	return out
}

// Week là tuần báo cáo chứa một ngày (lịch kỳ hệ thống W11.03).
type Week struct {
	Key  string // vd "2026-W45"
	Year int
	From time.Time
	To   time.Time
}

// DefaultWeekOf là tuần báo cáo theo quy tắc mặc định QĐ-6 (tuần Thứ Tư – Thứ Ba, số tuần theo năm của ngày cuối tuần,
// tuần 1 chứa 01/01) — chỉ dùng khi gốc lắp ráp chưa nối lịch kỳ hệ thống (port.Weeks). Cùng kết quả với
// extension/reportcycle DefaultRule.WeekOf.
func DefaultWeekOf(d time.Time) Week {
	d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	startOf := func(x time.Time) time.Time {
		return x.AddDate(0, 0, -((int(x.Weekday()) - int(time.Wednesday) + 7) % 7))
	}
	from := startOf(d)
	to := from.AddDate(0, 0, 6)
	y := to.Year()
	week1 := startOf(time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC))
	no := int(from.Sub(week1)/(24*time.Hour))/7 + 1
	return Week{Key: fmt.Sprintf("%d-W%02d", y, no), Year: y, From: from, To: to}
}
