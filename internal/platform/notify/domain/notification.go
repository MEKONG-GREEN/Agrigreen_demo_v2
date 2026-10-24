// Package domain: thông báo, người nhận theo chủ đề, dựng thông báo từ sự kiện
// của module khác. Không phụ thuộc hạ tầng, không đọc đồng hồ (R-12).
package domain

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// Audience là người nhận theo chủ đề (khi không có danh sách người nhận chính xác).
// Người xem khớp chủ đề nếu có một trong Roles (rỗng = mọi vai) VÀ:
//   - OrgRef khác nil: đơn vị công tác của người xem đúng OrgRef; hoặc
//   - National: phạm vi của người xem khác rỗng; hoặc
//   - Units: phạm vi của người xem KHÔNG phải toàn quốc và giao với Units.
//
// Người xem phạm vi toàn quốc (All) chỉ nhận chủ đề National — không bị tràn mọi
// thông báo cấp xã (US-LIB-06 "không gửi tràn toàn hệ thống").
type Audience struct {
	National bool
	Units    []string
	OrgRef   *uuid.UUID
	Roles    []string
}

// Empty cho biết chủ đề không nhắm ai.
func (a Audience) Empty() bool { return !a.National && len(a.Units) == 0 && a.OrgRef == nil }

// Viewer là người xem hộp thư.
type Viewer struct {
	ActorRef uuid.UUID
	OrgRef   uuid.UUID
	Roles    []string
	Scope    authz.Scope
}

// Matches là quy tắc khớp chủ đề (bản Go của điều kiện SQL ở store).
func (a Audience) Matches(v Viewer) bool {
	if len(a.Roles) > 0 && !slices.ContainsFunc(a.Roles, func(r string) bool { return slices.Contains(v.Roles, r) }) {
		return false
	}
	switch {
	case a.OrgRef != nil:
		return v.OrgRef != uuid.Nil && *a.OrgRef == v.OrgRef
	case a.National:
		return !v.Scope.Empty()
	case v.Scope.All:
		return false
	}
	return slices.ContainsFunc(a.Units, func(u string) bool { return slices.Contains(v.Scope.Units, u) })
}

// Notification là một thông báo trong hộp thư.
type Notification struct {
	ID              uuid.UUID
	RecipientRef    *uuid.UUID // nil = thông báo chủ đề
	Audience        Audience
	Kind            string
	Severity        string
	Title           string // không chứa dữ liệu cá nhân
	Body            string // không chứa dữ liệu cá nhân
	DeepLink        string // đường dẫn trong app, vd /extension/directives/<id>
	SubjectRef      *uuid.UUID
	SourceEventID   uuid.UUID
	SourceEventType string
	CreatedAt       time.Time
	ReadAt          *time.Time
	SyncSeq         int64
	// Email: gửi thêm email tới người nhận (chỉ thông báo có RecipientRef; không lưu trong bảng notification — lần gửi
	// xếp ở pf_notify.email_delivery, gói NOTIF).
	Email bool
	// Source-selected transport settings. Legacy/native notifications retain
	// zero-value behavior. SuppressInApp is persisted for every inbox read path.
	SuppressInApp bool
	SuppressPush  bool
	// RequireEmail requires queue dependencies, not successful external delivery.
	// A disabled adapter is still handled/audited by the email consumer (#127).
	RequireEmail bool
}

// Topic cho biết thông báo theo chủ đề.
func (n Notification) Topic() bool { return n.RecipientRef == nil }

// Urgent cho biết thông báo khẩn (thông điệp đẩy ưu tiên cao).
func (n Notification) Urgent() bool { return n.Severity == "urgent" }

var deepLinkRe = regexp.MustCompile(`^/[A-Za-z0-9/_.-]{1,200}$`)

// Giới hạn độ dài (khớp CHECK của migration).
const (
	MaxTitle = 300
	MaxBody  = 2000
)

// Normalize cắt độ dài, che chuỗi giống SĐT/CCCD (phòng thủ R-14) và kiểm đường dẫn.
// Trả false nếu thông báo không hợp lệ (lỗi lắp ráp của bộ dựng).
func (n *Notification) Normalize() bool {
	n.Title = truncate(Redact(strings.TrimSpace(n.Title)), MaxTitle)
	n.Body = truncate(Redact(strings.TrimSpace(n.Body)), MaxBody)
	if n.Severity == "" {
		n.Severity = "normal"
	}
	return n.Title != "" && deepLinkRe.MatchString(n.DeepLink) && n.Kind != "" &&
		(n.RecipientRef != nil || !n.Audience.Empty())
}

// phoneLikeRe: chuỗi số có thể xen dấu cách/chấm/gạch, 9 chữ số trở lên — SĐT, CCCD.
var phoneLikeRe = regexp.MustCompile(`\+?\d(?:[\d .-]*\d)?`)

// Redact thay chuỗi giống SĐT/CCCD (≥ 9 chữ số) bằng "***". Tiêu đề/nội dung thông
// báo lấy từ sự kiện đã không có PII; đây là lớp phòng thủ thứ hai.
func Redact(s string) string {
	return phoneLikeRe.ReplaceAllStringFunc(s, func(m string) string {
		digits := 0
		for _, r := range m {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		if digits >= 9 {
			return "***"
		}
		return m
	})
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

// DedupRefs bỏ trùng và bỏ uuid.Nil, giữ thứ tự.
func DedupRefs(refs []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		if r == uuid.Nil || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

// DedupUnits bỏ trùng và mã rỗng, giữ thứ tự.
func DedupUnits(units []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(units))
	for _, u := range units {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}
