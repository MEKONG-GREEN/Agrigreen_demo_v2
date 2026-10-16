package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// Thông báo lịch công tác tuần (KN v7.7 P5, pf/extension → pf/notify). Không có nội dung lịch, chỉ tuần và kết
// quả; lý do "Yêu cầu chỉnh sửa" đi kèm theo FN-XA-03.08 AC (đã chặn SĐT/CCCD ở pf/extension, Redact che thêm).

func weekLabel(week string) string {
	w, err := time.Parse(time.DateOnly, week)
	if err != nil {
		return week
	}
	return w.Format("02/01") + "–" + w.AddDate(0, 0, 6).Format("02/01/2006")
}

// WorkScheduleNotifications dựng thông báo từ một sự kiện pf.extension.work_schedule.*.
func WorkScheduleNotifications(eventType string, payload json.RawMessage, occurred time.Time) ([]Notification, error) {
	switch eventType {
	case extport.EventWorkScheduleSubmitted:
		var p extport.WorkScheduleSubmittedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("notify: payload %s: %w", eventType, err)
		}
		title := "Lịch công tác chờ duyệt"
		if p.Amendment {
			title = "Lịch công tác điều chỉnh — chờ duyệt lại"
		}
		body := "Tuần " + weekLabel(p.WeekStart) + " — mở mục Lịch công tác để duyệt hoặc yêu cầu chỉnh sửa."
		if p.Late {
			body += " Đăng ký muộn."
		}
		tpl := Template{Kind: port.KindWorkScheduleSubmitted, Severity: port.SeverityNormal, Title: title, Body: body,
			DeepLink: "/extension/work-schedules/" + p.ScheduleID.String(), SubjectRef: ref(p.ScheduleID),
			CreatedAt: ParseTime(p.EventTime, occurred)}
		var out []Notification
		for _, r := range DedupRefs(p.ApproverRefs) {
			if r != p.OwnerRef {
				out = append(out, tpl.For(r))
			}
		}
		if len(out) == 0 && p.ProvinceCode != "" {
			// Chưa biết người duyệt (danh bạ trống): báo chủ đề cho Cấp Tỉnh của tỉnh.
			out = append(out, tpl.ForAudience(Audience{Units: []string{p.ProvinceCode}, Roles: []string{partyport.RoleKNProvinceAdmin}}))
		}
		return out, nil
	case extport.EventWorkScheduleReviewed:
		var p extport.WorkScheduleReviewedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("notify: payload %s: %w", eventType, err)
		}
		if p.Auto || p.OwnerRef == uuid.Nil || p.OwnerRef == p.ReviewerRef {
			return nil, nil // tự duyệt khi gửi (Q-16 "auto"): người gửi đã biết kết quả
		}
		title, body := "Lịch công tác đã được duyệt", "Lịch tuần "+weekLabel(p.WeekStart)+" đã được Cấp Tỉnh duyệt."
		if p.Decision == extport.ScheduleDecisionChangeRequested {
			title = "Lịch công tác cần chỉnh sửa"
			body = "Lịch tuần " + weekLabel(p.WeekStart) + " cần chỉnh sửa"
			if p.Reason != nil && *p.Reason != "" {
				body += ": " + *p.Reason
			}
			body += " — sửa và gửi lại."
		}
		tpl := Template{Kind: port.KindWorkScheduleReviewed, Severity: port.SeverityNormal, Title: title, Body: body,
			DeepLink: "/extension/work-schedules/" + p.ScheduleID.String(), SubjectRef: ref(p.ScheduleID),
			CreatedAt: ParseTime(p.EventTime, occurred)}
		return []Notification{tpl.For(p.OwnerRef)}, nil
	case extport.EventWorkScheduleReminded:
		var p extport.WorkScheduleRemindedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("notify: payload %s: %w", eventType, err)
		}
		due := viTime(ParseTime(p.DueAt, occurred))
		title := "Sắp hết hạn đăng ký lịch công tác"
		body := "Đăng ký và gửi duyệt lịch tuần " + weekLabel(p.WeekStart) + " trước " + due + "."
		link := "/extension/work-schedules/mine"
		if p.Kind == extport.ScheduleReminderReviewOverdue {
			title = "Lịch công tác chờ duyệt quá hạn"
			body = fmt.Sprintf("Còn %d lịch tuần %s chờ duyệt, hạn duyệt %s.", p.Pending, weekLabel(p.WeekStart), due)
			link = "/extension/work-schedules"
		}
		tpl := Template{Kind: port.KindWorkScheduleDue, Severity: port.SeverityNormal, Title: title, Body: body,
			DeepLink: link, CreatedAt: occurred}
		var out []Notification
		for _, r := range DedupRefs(p.RecipientRefs) {
			out = append(out, tpl.For(r))
		}
		return out, nil
	}
	return nil, nil
}
