package extension

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"slices"
	"time"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// CNTSNotificationRenderers owns CNTS content. The shared capability owns inbox and delivery.
func CNTSNotificationRenderers() []notifyport.EventRendererRegistration {
	return []notifyport.EventRendererRegistration{{Events: []string{
		extport.EventCNTSAlertChanged, extport.EventCNTSFormReminded, extport.EventCNTSDutyChanged,
	}, Render: renderCNTSNotifications}}
}

// Thông báo cảnh báo và nhắc nộp biểu CNTS (W11.43, pf/extension → pf/notify). Tiêu đề là tóm tắt do pf/extension dựng
// (loại, biểu, kỳ, đơn vị) — không số liệu cá nhân; Redact che thêm. Đẩy FCM chỉ dữ liệu (ADR-0015).

// Đường dẫn màn của thông báo CNTS (web-ops).
const (
	cntsNotificationAlertLink   = "/kn/cnts/canh-bao"
	cntsNotificationPeriodsLink = "/kn/cnts/bao-cao-ky"
)

// renderCNTSNotifications dựng thông báo từ một sự kiện CNTS. Không có người nhận cụ thể → thông báo chủ đề (Cấp Tỉnh của tỉnh
// với cảnh báo; cán bộ của xã với nhắc nộp), không email.
func renderCNTSNotifications(eventType string, payload json.RawMessage, occurred time.Time) ([]notifyport.NotificationDraft, error) {
	switch eventType {
	case extport.EventCNTSDutyChanged:
		var p extport.CNTSDutyChangedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("notify: payload %s: %w", eventType, err)
		}
		if p.RosterID == uuid.Nil || p.Version < 1 {
			return nil, fmt.Errorf("notify: lịch trực thiếu mã hoặc phiên bản")
		}
		month, err := time.Parse("2006-01", p.Month)
		if err != nil {
			return nil, fmt.Errorf("notify: tháng lịch trực: %w", err)
		}
		tpl := sourceNotificationTemplate{Kind: extport.KindCNTSDutyChanged, Severity: notifyport.SeverityNormal, Title: "Lịch trực KSGM đã thay đổi",
			Body:     fmt.Sprintf("Lịch trực tháng %s đã cập nhật phiên bản %d. Mở lịch để xem ca của bạn.", month.Format("01/2006"), p.Version),
			DeepLink: "/kn/cnts/lich-truc", SubjectRef: sourceNotificationRef(p.RosterID), CreatedAt: sourceNotificationTime(p.EventTime, occurred)}
		out := make([]notifyport.NotificationDraft, 0, len(p.OfficerRefs))
		for _, r := range sourceNotificationRecipients(p.OfficerRefs) {
			out = append(out, tpl.For(r))
		}
		return out, nil

	case extport.EventCNTSAlertChanged:
		var p extport.CNTSAlertChangedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("notify: payload %s: %w", eventType, err)
		}
		sev, lvl := notifyport.SeverityNormal, "Cần chú ý"
		if p.Level == "urgent" {
			sev, lvl = notifyport.SeverityUrgent, "Khẩn"
		}
		due := sourceNotificationVietnamTime(sourceNotificationTime(p.DueAt, occurred))
		tpl := sourceNotificationTemplate{Severity: sev, DeepLink: cntsNotificationAlertLink, SubjectRef: sourceNotificationRef(p.AlertRef),
			CreatedAt: sourceNotificationTime(p.EventTime, occurred)}
		switch p.Action {
		case extport.CNTSAlertAssigned:
			tpl.Kind, tpl.Title = extport.KindCNTSAlertAssigned, "Được giao xử lý cảnh báo"
			tpl.Body = fmt.Sprintf("[%s] %s — hạn xử lý %s.", lvl, p.Title, due)
		case extport.CNTSAlertOverdue:
			tpl.Kind, tpl.Severity, tpl.Title = extport.KindCNTSAlertOverdue, notifyport.SeverityUrgent, "Cảnh báo quá hạn xử lý"
			tpl.Body = fmt.Sprintf("[%s] %s — quá hạn %s.", lvl, p.Title, due)
		default:
			tpl.Kind, tpl.Title, tpl.Body = extport.KindCNTSAlert, "Cảnh báo "+lvl, p.Title
		}
		out := make([]notifyport.NotificationDraft, 0, len(p.RecipientRefs))
		for _, r := range sourceNotificationRecipients(p.RecipientRefs) {
			n := tpl.For(r)
			n.Email = p.Level == "urgent" // Khẩn: thêm email (người có email đã xác minh)
			out = append(out, n)
		}
		if len(out) == 0 && p.ProvinceCode != nil && *p.ProvinceCode != "" {
			out = append(out, tpl.ForAudience(notifyport.DraftAudience{Units: []string{*p.ProvinceCode}, Roles: []string{partyport.RoleKNProvinceAdmin}}))
		}
		return out, nil
	case extport.EventCNTSFormReminded:
		var p extport.CNTSFormRemindedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("notify: payload %s: %w", eventType, err)
		}
		title := "Nhắc nộp báo cáo kỳ CNTS"
		switch p.Slot {
		case extport.CNTSReminderOverdue:
			title = "Báo cáo kỳ CNTS đã trễ hạn"
		case extport.CNTSReminderRepeat:
			title = "Nhắc lại: báo cáo kỳ CNTS chưa gửi"
		}
		tpl := sourceNotificationTemplate{Kind: extport.KindCNTSFormReminder, Severity: notifyport.SeverityNormal, Title: title, Body: sourceNotificationTruncate(p.Message, 400),
			DeepLink: cntsNotificationPeriodsLink, SubjectRef: sourceNotificationRef(p.PeriodRef), CreatedAt: sourceNotificationTime(p.EventTime, occurred)}
		email := slices.Contains(p.Channels, "email")
		out := make([]notifyport.NotificationDraft, 0, len(p.RecipientRefs))
		for _, r := range sourceNotificationRecipients(p.RecipientRefs) {
			n := tpl.For(r)
			n.Email = email
			out = append(out, n)
		}
		if len(out) == 0 && p.CommuneCode != "" {
			out = append(out, tpl.ForAudience(notifyport.DraftAudience{Units: []string{p.CommuneCode}, Roles: []string{partyport.RoleKNOfficer}}))
		}
		return out, nil
	}
	return nil, nil
}
