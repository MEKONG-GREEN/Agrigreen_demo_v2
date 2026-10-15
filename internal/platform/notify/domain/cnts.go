package domain

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"slices"
	"time"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// Thông báo cảnh báo và nhắc nộp biểu CNTS (W11.43, pf/extension → pf/notify). Tiêu đề là tóm tắt do pf/extension dựng
// (loại, biểu, kỳ, đơn vị) — không số liệu cá nhân; Redact che thêm. Đẩy FCM chỉ dữ liệu (ADR-0015).

// CNTSEvents là sự kiện consumer pf.notify.cnts xử lý.
var CNTSEvents = []string{extport.EventCNTSAlertChanged, extport.EventCNTSFormReminded, extport.EventCNTSDutyChanged}

// Đường dẫn màn của thông báo CNTS (web-ops).
const (
	CNTSAlertLink   = "/kn/cnts/canh-bao"
	CNTSPeriodsLink = "/kn/cnts/bao-cao-ky"
)

// CNTSNotifications dựng thông báo từ một sự kiện CNTS. Không có người nhận cụ thể → thông báo chủ đề (Cấp Tỉnh của tỉnh
// với cảnh báo; cán bộ của xã với nhắc nộp), không email.
func CNTSNotifications(eventType string, payload json.RawMessage, occurred time.Time) ([]Notification, error) {
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
		tpl := Template{Kind: port.KindCNTSDutyChanged, Severity: port.SeverityNormal, Title: "Lịch trực KSGM đã thay đổi",
			Body:     fmt.Sprintf("Lịch trực tháng %s đã cập nhật phiên bản %d. Mở lịch để xem ca của bạn.", month.Format("01/2006"), p.Version),
			DeepLink: "/kn/cnts/lich-truc", SubjectRef: ref(p.RosterID), CreatedAt: ParseTime(p.EventTime, occurred)}
		out := make([]Notification, 0, len(p.OfficerRefs))
		for _, r := range DedupRefs(p.OfficerRefs) {
			out = append(out, tpl.For(r))
		}
		return out, nil

	case extport.EventCNTSAlertChanged:
		var p extport.CNTSAlertChangedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("notify: payload %s: %w", eventType, err)
		}
		sev, lvl := port.SeverityNormal, "Cần chú ý"
		if p.Level == "urgent" {
			sev, lvl = port.SeverityUrgent, "Khẩn"
		}
		due := viTime(ParseTime(p.DueAt, occurred))
		tpl := Template{Severity: sev, DeepLink: CNTSAlertLink, SubjectRef: ref(p.AlertRef),
			CreatedAt: ParseTime(p.EventTime, occurred)}
		switch p.Action {
		case extport.CNTSAlertAssigned:
			tpl.Kind, tpl.Title = port.KindCNTSAlertAssigned, "Được giao xử lý cảnh báo"
			tpl.Body = fmt.Sprintf("[%s] %s — hạn xử lý %s.", lvl, p.Title, due)
		case extport.CNTSAlertOverdue:
			tpl.Kind, tpl.Severity, tpl.Title = port.KindCNTSAlertOverdue, port.SeverityUrgent, "Cảnh báo quá hạn xử lý"
			tpl.Body = fmt.Sprintf("[%s] %s — quá hạn %s.", lvl, p.Title, due)
		default:
			tpl.Kind, tpl.Title, tpl.Body = port.KindCNTSAlert, "Cảnh báo "+lvl, p.Title
		}
		out := make([]Notification, 0, len(p.RecipientRefs))
		for _, r := range DedupRefs(p.RecipientRefs) {
			n := tpl.For(r)
			n.Email = p.Level == "urgent" // Khẩn: thêm email (người có email đã xác minh)
			out = append(out, n)
		}
		if len(out) == 0 && p.ProvinceCode != nil && *p.ProvinceCode != "" {
			out = append(out, tpl.ForAudience(Audience{Units: []string{*p.ProvinceCode}, Roles: []string{partyport.RoleKNProvinceAdmin}}))
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
		tpl := Template{Kind: port.KindCNTSFormReminder, Severity: port.SeverityNormal, Title: title, Body: truncate(p.Message, 400),
			DeepLink: CNTSPeriodsLink, SubjectRef: ref(p.PeriodRef), CreatedAt: ParseTime(p.EventTime, occurred)}
		email := slices.Contains(p.Channels, "email")
		out := make([]Notification, 0, len(p.RecipientRefs))
		for _, r := range DedupRefs(p.RecipientRefs) {
			n := tpl.For(r)
			n.Email = email
			out = append(out, n)
		}
		if len(out) == 0 && p.CommuneCode != "" {
			out = append(out, tpl.ForAudience(Audience{Units: []string{p.CommuneCode}, Roles: []string{partyport.RoleKNOfficer}}))
		}
		return out, nil
	}
	return nil, nil
}
