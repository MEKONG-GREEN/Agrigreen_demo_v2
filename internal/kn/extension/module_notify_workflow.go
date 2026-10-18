package extension

import (
	"encoding/json"
	"fmt"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/google/uuid"
	"time"
)

// WorkflowNotificationRenderers declares seven report-cycle/work-schedule source events.
func WorkflowNotificationRenderers() []notifyport.EventRendererRegistration {
	return []notifyport.EventRendererRegistration{
		{Events: []string{extport.EventWorkScheduleSubmitted, extport.EventWorkScheduleReviewed, extport.EventWorkScheduleReminded}, Render: renderWorkScheduleNotifications},
		{Events: []string{extport.EventReportBatchIssued, extport.EventReportTaskReminded, extport.EventSubmissionAdjustmentRequested, extport.EventPeriodReopened}, Render: renderReportCycleNotifications},
	}
}

// Thông báo lịch công tác tuần (KN v7.7 P5, pf/extension → pf/notify). Không có nội dung lịch, chỉ tuần và kết
// quả; lý do "Yêu cầu chỉnh sửa" đi kèm theo FN-XA-03.08 AC (đã chặn SĐT/CCCD ở pf/extension, Redact che thêm).

func workflowNotificationWeekLabel(week string) string {
	w, err := time.Parse(time.DateOnly, week)
	if err != nil {
		return week
	}
	return w.Format("02/01") + "–" + w.AddDate(0, 0, 6).Format("02/01/2006")
}

// renderWorkScheduleNotifications dựng thông báo từ một sự kiện pf.extension.work_schedule.*.
func renderWorkScheduleNotifications(eventType string, payload json.RawMessage, occurred time.Time) ([]notifyport.NotificationDraft, error) {
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
		body := "Tuần " + workflowNotificationWeekLabel(p.WeekStart) + " — mở mục Lịch công tác để duyệt hoặc yêu cầu chỉnh sửa."
		if p.Late {
			body += " Đăng ký muộn."
		}
		tpl := sourceNotificationTemplate{Kind: extport.KindWorkScheduleSubmitted, Severity: notifyport.SeverityNormal, Title: title, Body: body,
			DeepLink: "/extension/work-schedules/" + p.ScheduleID.String(), SubjectRef: sourceNotificationRef(p.ScheduleID),
			CreatedAt: sourceNotificationTime(p.EventTime, occurred)}
		var out []notifyport.NotificationDraft
		for _, r := range sourceNotificationRecipients(p.ApproverRefs) {
			if r != p.OwnerRef {
				out = append(out, tpl.For(r))
			}
		}
		if len(out) == 0 && p.ProvinceCode != "" {
			// Chưa biết người duyệt (danh bạ trống): báo chủ đề cho Cấp Tỉnh của tỉnh.
			out = append(out, tpl.ForAudience(notifyport.DraftAudience{Units: []string{p.ProvinceCode}, Roles: []string{partyport.RoleKNProvinceAdmin}}))
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
		title, body := "Lịch công tác đã được duyệt", "Lịch tuần "+workflowNotificationWeekLabel(p.WeekStart)+" đã được Cấp Tỉnh duyệt."
		if p.Decision == extport.ScheduleDecisionChangeRequested {
			title = "Lịch công tác cần chỉnh sửa"
			body = "Lịch tuần " + workflowNotificationWeekLabel(p.WeekStart) + " cần chỉnh sửa"
			if p.Reason != nil && *p.Reason != "" {
				body += ": " + *p.Reason
			}
			body += " — sửa và gửi lại."
		}
		tpl := sourceNotificationTemplate{Kind: extport.KindWorkScheduleReviewed, Severity: notifyport.SeverityNormal, Title: title, Body: body,
			DeepLink: "/extension/work-schedules/" + p.ScheduleID.String(), SubjectRef: sourceNotificationRef(p.ScheduleID),
			CreatedAt: sourceNotificationTime(p.EventTime, occurred)}
		return []notifyport.NotificationDraft{tpl.For(p.OwnerRef)}, nil
	case extport.EventWorkScheduleReminded:
		var p extport.WorkScheduleRemindedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("notify: payload %s: %w", eventType, err)
		}
		due := sourceNotificationVietnamTime(sourceNotificationTime(p.DueAt, occurred))
		title := "Sắp hết hạn đăng ký lịch công tác"
		body := "Đăng ký và gửi duyệt lịch tuần " + workflowNotificationWeekLabel(p.WeekStart) + " trước " + due + "."
		link := "/extension/work-schedules/mine"
		if p.Kind == extport.ScheduleReminderReviewOverdue {
			title = "Lịch công tác chờ duyệt quá hạn"
			body = fmt.Sprintf("Còn %d lịch tuần %s chờ duyệt, hạn duyệt %s.", p.Pending, workflowNotificationWeekLabel(p.WeekStart), due)
			link = "/extension/work-schedules"
		}
		tpl := sourceNotificationTemplate{Kind: extport.KindWorkScheduleDue, Severity: notifyport.SeverityNormal, Title: title, Body: body,
			DeepLink: link, CreatedAt: occurred}
		var out []notifyport.NotificationDraft
		for _, r := range sourceNotificationRecipients(p.RecipientRefs) {
			out = append(out, tpl.For(r))
		}
		return out, nil
	}
	return nil, nil
}

// Thông báo vòng đời báo cáo KN v7.7 (gói P4). Không có số liệu báo cáo, chỉ biểu, kỳ, hạn và lý do/nội dung do Cấp
// Tỉnh soạn (Redact che SĐT/CCCD nếu lỡ nhập). Đẩy FCM chỉ dữ liệu (ADR-0015): app kéo nội dung qua API.

func workflowNotificationRefs(ss []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ss))
	for _, s := range ss {
		if u, err := uuid.Parse(s); err == nil && u != uuid.Nil {
			out = append(out, u)
		}
	}
	return sourceNotificationRecipients(out)
}

// renderReportCycleNotifications dựng thông báo cá nhân tới cán bộ phụ trách từ một sự kiện vòng đời báo cáo.
func renderReportCycleNotifications(eventType string, payload json.RawMessage, occurred time.Time) ([]notifyport.NotificationDraft, error) {
	bad := func(err error) ([]notifyport.NotificationDraft, error) {
		return nil, fmt.Errorf("notify: payload %s: %w", eventType, err)
	}
	var tpl sourceNotificationTemplate
	var recips []uuid.UUID
	switch eventType {
	case extport.EventReportBatchIssued:
		var p extport.ReportBatchIssuedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return bad(err)
		}
		due := sourceNotificationVietnamTime(sourceNotificationTime(p.FirstDueAt, occurred))
		at := sourceNotificationTime(p.EventTime, occurred)
		var out []notifyport.NotificationDraft
		seen := map[uuid.UUID]int{}
		for _, a := range p.Assignees {
			for _, r := range workflowNotificationRefs(a.OfficerRefs) {
				seen[r] += a.TaskCount
			}
		}
		for _, a := range p.Assignees {
			for _, r := range workflowNotificationRefs(a.OfficerRefs) {
				n, ok := seen[r]
				if !ok {
					continue
				}
				delete(seen, r)
				t := sourceNotificationTemplate{Kind: extport.KindReportTaskAssigned, Severity: notifyport.SeverityNormal, Title: "Có công việc nộp báo cáo mới",
					Body:     fmt.Sprintf("%s — %d công việc (%d kỳ), hạn kỳ đầu %s.", p.SeasonLabel, n, p.PeriodCount, due),
					DeepLink: "/kn/nhiem-vu/cong-viec", SubjectRef: workflowNotificationParseRef(p.BatchID), CreatedAt: at}
				out = append(out, t.For(r))
			}
		}
		return out, nil
	case extport.EventReportTaskReminded:
		var p extport.ReportTaskRemindedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return bad(err)
		}
		kind, title := extport.KindReportReminder, "Cấp Tỉnh nhắc nộp báo cáo"
		if p.Mode != extport.ReminderManual {
			kind, title = extport.KindReportTaskDue, "Nhắc hạn nộp báo cáo"
		}
		tpl = sourceNotificationTemplate{Kind: kind, Severity: notifyport.SeverityNormal, Title: title, Body: p.PeriodLabel + ": " + sourceNotificationTruncate(p.Message, 400),
			DeepLink: "/kn/nhiem-vu/cong-viec", SubjectRef: workflowNotificationParseRef(p.PeriodID), CreatedAt: sourceNotificationTime(p.EventTime, occurred)}
		recips = workflowNotificationRefs(p.RecipientRefs)
	case extport.EventSubmissionAdjustmentRequested:
		var p extport.SubmissionAdjustmentRequestedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return bad(err)
		}
		tpl = sourceNotificationTemplate{Kind: extport.KindReportAdjustmentRequested, Severity: notifyport.SeverityNormal, Title: "Yêu cầu điều chỉnh báo cáo",
			Body: p.FormCode + " · " + p.PeriodLabel + " · xã " + p.CommuneCode + ": " + sourceNotificationTruncate(p.Reason, 400), DeepLink: "/kn/nhiem-vu/cong-viec/" + p.TaskID,
			SubjectRef: workflowNotificationParseRef(p.TaskID), CreatedAt: sourceNotificationTime(p.EventTime, occurred)}
		recips = workflowNotificationRefs(p.RecipientRefs)
	case extport.EventPeriodReopened:
		var p extport.PeriodReopenedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return bad(err)
		}
		tpl = sourceNotificationTemplate{Kind: extport.KindReportPeriodReopened, Severity: notifyport.SeverityNormal, Title: "Kỳ báo cáo được mở lại",
			Body:     fmt.Sprintf("%s mở lại đến %s: %s", p.Label, sourceNotificationVietnamTime(sourceNotificationTime(p.ReopenUntil, occurred)), sourceNotificationTruncate(p.Reason, 300)),
			DeepLink: "/kn/nhiem-vu/cong-viec", SubjectRef: workflowNotificationParseRef(p.PeriodID), CreatedAt: sourceNotificationTime(p.EventTime, occurred)}
		recips = workflowNotificationRefs(p.RecipientRefs)
	default:
		return nil, nil
	}
	out := make([]notifyport.NotificationDraft, 0, len(recips))
	for _, r := range recips {
		out = append(out, tpl.For(r))
	}
	return out, nil
}

func workflowNotificationParseRef(s string) *uuid.UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return sourceNotificationRef(u)
}
