package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	contentport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Template là nội dung chung của một thông báo; người nhận gắn sau (For/ForAudience).
type Template struct {
	Kind, Severity, Title, Body, DeepLink string
	SubjectRef                            *uuid.UUID
	CreatedAt                             time.Time
}

// For dựng thông báo cá nhân.
func (t Template) For(recipient uuid.UUID) Notification {
	r := recipient
	return t.base(&r, Audience{})
}

// ForAudience dựng thông báo chủ đề.
func (t Template) ForAudience(a Audience) Notification { return t.base(nil, a) }

func (t Template) base(r *uuid.UUID, a Audience) Notification {
	return Notification{RecipientRef: r, Audience: a, Kind: t.Kind, Severity: t.Severity, Title: t.Title, Body: t.Body,
		DeepLink: t.DeepLink, SubjectRef: t.SubjectRef, CreatedAt: t.CreatedAt.UTC()}
}

// ParseTime đọc thời điểm RFC 3339 của payload; sai dạng → fallback (occurred_at của phong bì).
func ParseTime(s string, fallback time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return fallback.UTC()
}

// viTime hiển thị giờ Việt Nam dạng 15/09/2026 10:00 (không bị Redact che).
func viTime(t time.Time) string { return t.In(clock.Zone()).Format("02/01/2006 15:04") }

func ref(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

// DirectiveTemplate: chỉ đạo mới (KN US-TASK-03). Tiêu đề/nội dung chỉ đạo KHÔNG nằm
// trong sự kiện — người nhận mở đường dẫn để đọc và xác nhận.
func DirectiveTemplate(p extport.DirectiveIssuedV1, occurred time.Time) Template {
	sev, title := port.SeverityNormal, "Chỉ đạo mới từ Trung tâm Khuyến nông"
	if p.Severity == port.SeverityUrgent {
		sev, title = port.SeverityUrgent, "Chỉ đạo KHẨN từ Trung tâm Khuyến nông"
	}
	return Template{Kind: port.KindDirective, Severity: sev, Title: title,
		Body:     "Mở để đọc nội dung chỉ đạo và xác nhận đã đọc.",
		DeepLink: "/extension/directives/" + p.DirectiveID.String(), SubjectRef: ref(p.DirectiveID),
		CreatedAt: ParseTime(p.IssuedAt, occurred)}
}

// AlertTemplate: cảnh báo khẩn (HTX US-NEWS-03) — tiêu đề và tóm tắt là nội dung công bố.
func AlertTemplate(p contentport.AlertPublishedV1, occurred time.Time) Template {
	id, _ := uuid.Parse(p.DocumentRef)
	return Template{Kind: port.KindAlert, Severity: port.SeverityUrgent, Title: p.Title, Body: p.Summary,
		DeepLink: "/documents/" + id.String(), SubjectRef: ref(id), CreatedAt: ParseTime(p.PublishedAt, occurred)}
}

var docLabel = map[string]string{
	"procedure": "Quy trình kỹ thuật", "guide": "Hướng dẫn", "news": "Tin tức", "supplement": "Hướng dẫn địa phương",
}

// DocumentTemplate: tài liệu/tin mới hoặc cập nhật (US-LIB-06).
func DocumentTemplate(p contentport.DocumentPublishedV1, occurred time.Time) Template {
	id, _ := uuid.Parse(p.DocumentRef)
	label := docLabel[p.Kind]
	if label == "" {
		label = "Tài liệu"
	}
	title, body := label+" mới: "+p.Title, "Đã xuất bản. Có thể tải về để xem ngoại tuyến."
	if !p.FirstVersion {
		title, body = label+" cập nhật: "+p.Title, fmt.Sprintf("Đã cập nhật lên phiên bản %d.", p.VersionNo)
	}
	return Template{Kind: port.KindDocument, Severity: port.SeverityNormal, Title: title, Body: body,
		DeepLink: "/documents/" + id.String(), SubjectRef: ref(id), CreatedAt: ParseTime(p.PublishedAt, occurred)}
}

var priorityLabel = map[string]string{"low": "thấp", "normal": "bình thường", "high": "cao", "urgent": "khẩn"}

// TaskEscalatedTemplate: nhiệm vụ leo thang lên TTKN tỉnh (KN US-TASK-02 AC-2).
func TaskEscalatedTemplate(p extport.TaskEscalatedV1, occurred time.Time) Template {
	title := "Nhiệm vụ quá hạn SLA đã leo thang lên tỉnh"
	if p.Reason == extport.EscalationReasonManual {
		title = "Nhiệm vụ được chuyển tiếp lên tỉnh"
	}
	sev := port.SeverityNormal
	if p.Priority == "urgent" || p.Priority == "high" {
		sev = port.SeverityUrgent
	}
	body := fmt.Sprintf("Xã %s, ưu tiên %s, hạn %s.", p.CommuneCode, priorityLabel[p.Priority],
		viTime(ParseTime(p.DueAt, occurred)))
	return Template{Kind: port.KindTaskEscalated, Severity: sev, Title: title, Body: body,
		DeepLink: "/extension/tasks/" + p.TaskID.String(), SubjectRef: ref(p.TaskID), CreatedAt: ParseTime(p.EventTime, occurred)}
}

// HouseholdRegisteredTemplate: nông hộ mới đăng ký từ App HTX (KN US-HTX-05) — không
// nhập lại tay; hồ sơ mang đúng mã định danh chung household_ref. Không có PII.
func HouseholdRegisteredTemplate(p partyport.HouseholdRegisteredV1, occurred time.Time) Template {
	return Template{Kind: port.KindHouseholdRegistered, Severity: port.SeverityNormal, Title: "Nông hộ mới đăng ký từ App HTX",
		Body:     fmt.Sprintf("Xã %s: hồ sơ nông hộ đã đồng bộ vào App Khuyến nông, không cần nhập lại.", p.CommuneCode),
		DeepLink: "/households/" + p.HouseholdRef.String(), SubjectRef: ref(p.HouseholdRef), CreatedAt: ParseTime(p.EventTime, occurred)}
}

var membershipLabel = map[string]string{
	partyport.MembershipJoined: "vào HTX", partyport.MembershipLeft: "rời HTX", partyport.MembershipTransferred: "chuyển HTX",
}

// MembershipChangedTemplate: nông hộ vào/rời/chuyển HTX (KN US-HTX-05).
func MembershipChangedTemplate(p partyport.HouseholdMembershipChangedV1, occurred time.Time) Template {
	label := membershipLabel[p.Change]
	if label == "" {
		label = "đổi thành viên HTX"
	}
	return Template{Kind: port.KindHouseholdMembershipChanged, Severity: port.SeverityNormal, Title: "Nông hộ trong địa bàn " + label,
		Body:     fmt.Sprintf("Xã %s, hiệu lực từ %s.", p.CommuneCode, p.EffectiveDate),
		DeepLink: "/households/" + p.HouseholdRef.String(), SubjectRef: ref(p.HouseholdRef), CreatedAt: ParseTime(p.EventTime, occurred)}
}

// TaskAssignedTemplate: nhiệm vụ tự sinh từ yêu cầu hỗ trợ của nông dân (KN US-TASK-01).
func TaskAssignedTemplate(p extport.TaskCreatedV1, occurred time.Time) Template {
	title := "Nhiệm vụ hỗ trợ mới từ App HTX"
	if p.AssigneeRef == nil {
		title = "Yêu cầu hỗ trợ chưa có cán bộ nhận — cần phân công"
	}
	sev := port.SeverityNormal
	if p.Priority == "urgent" || p.Priority == "high" {
		sev = port.SeverityUrgent
	}
	body := fmt.Sprintf("Xã %s, ưu tiên %s, hạn %s.", p.CommuneCode, priorityLabel[p.Priority], viTime(ParseTime(p.DueAt, occurred)))
	return Template{Kind: port.KindTaskAssigned, Severity: sev, Title: title, Body: body,
		DeepLink: "/extension/tasks/" + p.TaskID.String(), SubjectRef: ref(p.TaskID), CreatedAt: ParseTime(p.CreatedAt, occurred)}
}

// ConflictTemplate: xung đột đồng bộ chờ xử lý (C-8).
func ConflictTemplate(p syncport.ConflictOpenedV1, occurred time.Time) Template {
	return Template{Kind: port.KindSyncConflict, Severity: port.SeverityNormal, Title: "Xung đột đồng bộ cần xử lý",
		Body:     fmt.Sprintf("Dữ liệu %s gửi từ thiết bị hiện trường xung đột; hạn xử lý %s.", p.Entity, viTime(ParseTime(p.DueAt, occurred))),
		DeepLink: "/sync/conflicts/" + p.ConflictID.String(), SubjectRef: ref(p.ConflictID), CreatedAt: ParseTime(p.OpenedAt, occurred)}
}

// OrgDeactivatedTemplate: tổ chức bị vô hiệu hóa — không đưa mã số thuế hay lý do vào thông báo.
func OrgDeactivatedTemplate(p partyport.OrgDeactivatedV1, occurred time.Time) Template {
	return Template{Kind: port.KindOrgDeactivated, Severity: port.SeverityNormal, Title: "Một tổ chức trong địa bàn đã bị vô hiệu hóa",
		Body:     fmt.Sprintf("Tài khoản thuộc tổ chức đã bị khóa; %d hộ thành viên bị ảnh hưởng.", p.ActiveHouseholds),
		DeepLink: "/orgs/" + p.OrgRef.String(), SubjectRef: ref(p.OrgRef), CreatedAt: ParseTime(p.EventTime, occurred)}
}
