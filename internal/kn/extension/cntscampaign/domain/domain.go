// Package domain: quy tắc thuần của Chiến dịch điều tra mở rộng CNTS (W11.50 — BRD CNTS v3.7 FN-21, UR-TINH-26, UR-XA-14;
// D4 CN10). Một đợt = MỘT kỳ native cap/forms profile kn_cnts loại kỳ `adhoc` (đơn vị lá = xã), tách khỏi lũy kế báo cáo
// định kỳ (BR-02) và khỏi tổng quy đổi của KHTC. Trạng thái Nháp → Đang thu thập → Đã đóng (mở lại → Đang thu thập).
package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Hằng của đợt điều tra.
const (
	// Profile là profile cap/forms của phòng CNTS.
	Profile = "kn_cnts"
	// PeriodKind là loại kỳ native của đợt — loại `adhoc` không vào lũy kế (reportcycle) và tổng KHTC (closedfig).
	PeriodKind = "adhoc"
	// KeyPrefix là tiền tố khóa kỳ native của đợt ("DT-<yyyymmdd>-<6 hex>").
	KeyPrefix = "DT-"
	// PeriodRule là thẻ quy tắc chia kỳ ghi trên kỳ native (không theo lịch tuần của phòng).
	PeriodRule = "cnts.campaign/v1"
	// RuleVersion là phiên bản quy tắc ghi trên hai sổ (rule_version).
	RuleVersion = "cnts.campaign/v1"
	// Department là phòng ban sở hữu đợt.
	Department = "chan_nuoi_thuy_san"
)

// Trạng thái đợt (FN-21 BR-01).
const (
	StateDraft      = "draft"      // Nháp
	StateCollecting = "collecting" // Đang thu thập
	StateClosed     = "closed"     // Đã đóng
)

// Sự kiện của đợt.
const (
	ActPublished = "published"
	ActExtended  = "extended"
	ActClosed    = "closed"
	ActReopened  = "reopened"
	ActReminded  = "reminded"
)

// Lĩnh vực của đợt.
var Fields = []string{"chan_nuoi", "thu_y", "thuy_san", "khac"}

// Chế độ phạm vi (UR-TINH-26 "toàn bộ xã, Khu vực, danh sách xã").
const (
	ScopeProvince = "province"
	ScopeRegions  = "regions"
	ScopeCommunes = "communes"
)

// MaxUnits là số xã tối đa của một đợt (AC-01: 102 xã).
const MaxUnits = 2000

var (
	reCode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	reUnit = regexp.MustCompile(`^[0-9A-Za-z_-]{1,20}$`)
	reKey  = regexp.MustCompile(`^DT-[0-9]{8}-[0-9a-f]{6}$`)
)

// ErrInput là lỗi dữ liệu vào (trường + lý do).
type ErrInput struct{ Field, Msg string }

func (e ErrInput) Error() string { return e.Field + ": " + e.Msg }

// Draft là nội dung đợt (phiên bản Nháp).
type Draft struct {
	Name, Goal, Field string
	TemplateCode      string
	ScopeMode         string
	Regions           []string
	Units             []string // xã đã giải từ phạm vi
	From, To          time.Time
	DueAt             time.Time
	OwnerRef          *uuid.UUID
	RemindBeforeHours int
}

// Normalize cắt khoảng trắng, bỏ trùng xã/khu vực (giữ thứ tự).
func (d *Draft) Normalize() {
	d.Name, d.Goal, d.Field = strings.TrimSpace(d.Name), strings.TrimSpace(d.Goal), strings.TrimSpace(d.Field)
	d.TemplateCode, d.ScopeMode = strings.TrimSpace(d.TemplateCode), strings.TrimSpace(d.ScopeMode)
	if d.ScopeMode == "" {
		d.ScopeMode = ScopeCommunes
	}
	d.Regions, d.Units = dedup(d.Regions), dedup(d.Units)
}

func dedup(xs []string) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x != "" && !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// Validate kiểm nội dung đợt (sau khi đã giải phạm vi thành danh sách xã).
func (d Draft) Validate() error {
	switch {
	case d.Name == "" || utf8.RuneCountInString(d.Name) > 200:
		return ErrInput{"name", "bắt buộc, tối đa 200 ký tự"}
	case utf8.RuneCountInString(d.Goal) > 2000:
		return ErrInput{"goal", "tối đa 2000 ký tự"}
	case !slices.Contains(Fields, d.Field):
		return ErrInput{"field", strings.Join(Fields, " | ")}
	case !reCode.MatchString(d.TemplateCode):
		return ErrInput{"template_code", "mã biểu điều tra (cap/forms) bắt buộc"}
	case d.ScopeMode != ScopeProvince && d.ScopeMode != ScopeRegions && d.ScopeMode != ScopeCommunes:
		return ErrInput{"scope_mode", "province | regions | communes"}
	case d.ScopeMode == ScopeRegions && len(d.Regions) == 0:
		return ErrInput{"regions", "chọn ít nhất một khu vực"}
	case d.ScopeMode != ScopeRegions && len(d.Regions) > 0:
		return ErrInput{"regions", "chỉ dùng với phạm vi theo khu vực"}
	case len(d.Units) == 0:
		return ErrInput{"communes", "phạm vi không có xã nào"}
	case len(d.Units) > MaxUnits:
		return ErrInput{"communes", fmt.Sprintf("tối đa %d xã", MaxUnits)}
	case d.From.IsZero() || d.To.IsZero():
		return ErrInput{"from_date", "thời gian thu thập bắt buộc"}
	case d.To.Before(d.From):
		return ErrInput{"to_date", "đến ngày ≥ từ ngày"}
	case d.To.Sub(d.From) > 366*24*time.Hour:
		return ErrInput{"to_date", "đợt tối đa một năm"}
	case d.DueAt.IsZero():
		return ErrInput{"due_at", "hạn nộp bắt buộc"}
	case d.RemindBeforeHours < 0 || d.RemindBeforeHours > 168:
		return ErrInput{"remind_before_hours", "0–168 giờ"}
	}
	for _, u := range d.Units {
		if !reUnit.MatchString(u) {
			return ErrInput{"communes", "mã xã không hợp lệ: " + u}
		}
	}
	return nil
}

// PeriodKey là khóa kỳ native của đợt: "DT-<ngày đầu yyyymmdd>-<6 hex cuối mã đợt>" (≤ 20 ký tự — cap/forms).
func PeriodKey(id uuid.UUID, from time.Time) string {
	h := strings.ReplaceAll(id.String(), "-", "")
	return KeyPrefix + from.Format("20060102") + "-" + h[len(h)-6:]
}

// IsCampaignKey cho biết khóa kỳ native là của một đợt điều tra.
func IsCampaignKey(key string) bool { return reKey.MatchString(key) }

// StateOf trả trạng thái đợt từ chuỗi sự kiện (theo thứ tự ghi): sự kiện chuyển trạng thái cuối.
func StateOf(actions []string) string {
	st := StateDraft
	for _, a := range actions {
		switch a {
		case ActPublished, ActReopened:
			st = StateCollecting
		case ActClosed:
			st = StateClosed
		}
	}
	return st
}

// CanApply cho biết sự kiện act hợp lệ ở trạng thái st (khớp trigger fn_cnts_campaign_transition).
func CanApply(st, act string) bool {
	switch act {
	case ActPublished:
		return st == StateDraft
	case ActExtended, ActClosed, ActReminded:
		return st == StateCollecting
	case ActReopened:
		return st == StateClosed
	}
	return false
}

// Reason kiểm lý do gia hạn/mở lại: 10–500 ký tự.
func Reason(s string) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n >= 10 && n <= 500
}
