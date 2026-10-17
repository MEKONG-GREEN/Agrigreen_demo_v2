package extension

import (
	"context"
	"encoding/json"
	"fmt"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/google/uuid"
	"time"
)

// TaskNotificationBuilders supplies task content and recipient policy through a read-only directory.
func TaskNotificationBuilders(directory notifyport.StaffDirectory) []notifyport.EventBuilderRegistration {
	return []notifyport.EventBuilderRegistration{{Events: []string{extport.EventTaskCreated, extport.EventTaskEscalated},
		Build: func(ctx context.Context, tenantID uuid.UUID, event string, payload json.RawMessage, occurred time.Time) ([]notifyport.NotificationDraft, error) {
			return taskNotificationDrafts(ctx, tenantID, event, payload, occurred, directory)
		}}}
}
func taskNotificationDrafts(ctx context.Context, tenantID uuid.UUID, event string, payload json.RawMessage, occurred time.Time, directory notifyport.StaffDirectory) ([]notifyport.NotificationDraft, error) {
	switch event {
	case extport.EventTaskEscalated:
		var p extport.TaskEscalatedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("notify: payload %s hỏng: %w", event, err)
		}
		tpl := taskNotificationEscalatedTemplate(p, occurred)
		var recipients []uuid.UUID
		if p.AssigneeRef != nil {
			recipients = append(recipients, *p.AssigneeRef)
		}
		admins := 0
		if directory != nil && p.ProvinceCode != "" {
			staff, err := directory.ActiveStaff(ctx, tenantID, []string{p.ProvinceCode})
			if err != nil {
				return nil, err
			}
			for _, st := range staff {
				if st.ProvinceLevel() && st.ProvinceCode == p.ProvinceCode {
					recipients = append(recipients, st.ActorRef)
					admins++
				}
			}
		}
		var ns []notifyport.NotificationDraft
		for _, r := range sourceNotificationRecipients(recipients) {
			ns = append(ns, tpl.For(r))
		}
		if admins == 0 && p.ProvinceCode != "" {
			ns = append(ns, tpl.ForAudience(notifyport.DraftAudience{Units: []string{p.ProvinceCode}, Roles: []string{partyport.RoleKNProvinceAdmin}}))
		}
		return ns, nil
	case extport.EventTaskCreated:
		var p extport.TaskCreatedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("notify: payload %s hỏng: %w", event, err)
		}
		tpl := taskNotificationAssignedTemplate(p, occurred)
		if p.AssigneeRef != nil && *p.AssigneeRef != uuid.Nil {
			return []notifyport.NotificationDraft{tpl.For(*p.AssigneeRef)}, nil
		}
		unit := p.ProvinceCode
		if unit == "" {
			unit = p.CommuneCode
		}
		return []notifyport.NotificationDraft{tpl.ForAudience(notifyport.DraftAudience{Units: []string{unit}, Roles: []string{partyport.RoleKNProvinceAdmin}})}, nil
	}
	return nil, nil
}

var taskNotificationPriorityLabel = map[string]string{"low": "thấp", "normal": "bình thường", "high": "cao", "urgent": "khẩn"}

// taskNotificationEscalatedTemplate: nhiệm vụ leo thang lên TTKN tỉnh (KN US-TASK-02 AC-2).
func taskNotificationEscalatedTemplate(p extport.TaskEscalatedV1, occurred time.Time) sourceNotificationTemplate {
	title := "Nhiệm vụ quá hạn SLA đã leo thang lên tỉnh"
	if p.Reason == extport.EscalationReasonManual {
		title = "Nhiệm vụ được chuyển tiếp lên tỉnh"
	}
	sev := notifyport.SeverityNormal
	if p.Priority == "urgent" || p.Priority == "high" {
		sev = notifyport.SeverityUrgent
	}
	body := fmt.Sprintf("Xã %s, ưu tiên %s, hạn %s.", p.CommuneCode, taskNotificationPriorityLabel[p.Priority],
		sourceNotificationVietnamTime(sourceNotificationTime(p.DueAt, occurred)))
	return sourceNotificationTemplate{Kind: notifyport.KindTaskEscalated, Severity: sev, Title: title, Body: body,
		DeepLink: "/extension/tasks/" + p.TaskID.String(), SubjectRef: sourceNotificationRef(p.TaskID), CreatedAt: sourceNotificationTime(p.EventTime, occurred)}
}

// taskNotificationAssignedTemplate: nhiệm vụ tự sinh từ yêu cầu hỗ trợ của nông dân (KN US-TASK-01).
func taskNotificationAssignedTemplate(p extport.TaskCreatedV1, occurred time.Time) sourceNotificationTemplate {
	title := "Nhiệm vụ hỗ trợ mới từ App HTX"
	if p.AssigneeRef == nil {
		title = "Yêu cầu hỗ trợ chưa có cán bộ nhận — cần phân công"
	}
	sev := notifyport.SeverityNormal
	if p.Priority == "urgent" || p.Priority == "high" {
		sev = notifyport.SeverityUrgent
	}
	body := fmt.Sprintf("Xã %s, ưu tiên %s, hạn %s.", p.CommuneCode, taskNotificationPriorityLabel[p.Priority], sourceNotificationVietnamTime(sourceNotificationTime(p.DueAt, occurred)))
	return sourceNotificationTemplate{Kind: notifyport.KindTaskAssigned, Severity: sev, Title: title, Body: body,
		DeepLink: "/extension/tasks/" + p.TaskID.String(), SubjectRef: sourceNotificationRef(p.TaskID), CreatedAt: sourceNotificationTime(p.CreatedAt, occurred)}
}
