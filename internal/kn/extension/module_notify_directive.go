package extension

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/google/uuid"
	"strings"
	"time"
)

// DirectiveNotificationBuilders declares content while notify retains all writes.
func DirectiveNotificationBuilders(staff notifyport.StaffDirectory, units partyport.AdminUnits) []notifyport.EventBuilderRegistration {
	return []notifyport.EventBuilderRegistration{{Events: []string{extport.EventDirectiveIssued}, Plan: func(ctx context.Context, tenantID uuid.UUID, event string, payload json.RawMessage, occurred time.Time) (notifyport.NotificationPlan, error) {
		return sourceDirectivePlan(ctx, tenantID, event, payload, occurred, staff, units)
	}}}
}
func sourceDirectivePlan(ctx context.Context, tenantID uuid.UUID, event string, payload json.RawMessage, occurred time.Time, staff notifyport.StaffDirectory, unitsDirectory partyport.AdminUnits) (notifyport.NotificationPlan, error) {
	if event != extport.EventDirectiveIssued {
		return notifyport.NotificationPlan{}, nil
	}
	var p extport.DirectiveIssuedV1
	if err := json.Unmarshal(payload, &p); err != nil {
		return notifyport.NotificationPlan{}, fmt.Errorf("notify: payload %s hỏng: %w", event, err)
	}
	tpl := sourceDirectiveTemplate(p, occurred)
	targets := sourceNotificationRecipients(p.TargetRefs)
	units := sourceDirectiveUnits(p.AudienceUnits)
	if len(targets) == 0 && staff != nil {
		staff, err := staff.ActiveStaff(ctx, tenantID, units)
		if err != nil {
			return notifyport.NotificationPlan{}, err
		}
		for _, st := range staff {
			targets = append(targets, st.ActorRef)
		}
		targets = sourceNotificationRecipients(targets)
	}
	var ns []notifyport.NotificationDraft
	for _, r := range targets {
		ns = append(ns, tpl.For(r))
	}
	if len(ns) == 0 && len(units) > 0 {
		exp, err := sourceDirectiveExpandUnits(ctx, units, occurred, unitsDirectory)
		if err != nil {
			return notifyport.NotificationPlan{}, err
		}
		ns = append(ns, tpl.ForAudience(notifyport.DraftAudience{Units: exp, Roles: []string{partyport.RoleKNOfficer, partyport.RoleKNProvinceAdmin, partyport.RoleKNNationalAdmin}}))
	}

	var digests []notifyport.DigestDraft
	if p.Severity == notifyport.SeverityUrgent {
		digests = []notifyport.DigestDraft{{Reference: p.DirectiveID, AudienceUnits: units, IssuedAt: tpl.CreatedAt}}
	}
	return notifyport.NotificationPlan{Drafts: ns, Digests: digests}, nil
}

// NotificationDigest owns message and job metadata for the existing queue.
func NotificationDigest() notifyport.DigestRegistration {
	return notifyport.DigestRegistration{Events: []string{extport.EventDirectiveIssued}, Job: notifyport.DigestJobDefinition{Name: notifyport.JobEmailDigest, Every: time.Hour, Timeout: 10 * time.Minute, RunOnStart: false, LogMessage: "email tóm tắt chỉ đạo khẩn"}, Format: sourceDirectiveDigest}
}
func sourceDirectiveTemplate(p extport.DirectiveIssuedV1, occurred time.Time) sourceNotificationTemplate {
	sev, title := notifyport.SeverityNormal, "Chỉ đạo mới từ Trung tâm Khuyến nông"
	if p.Severity == notifyport.SeverityUrgent {
		sev, title = notifyport.SeverityUrgent, "Chỉ đạo KHẨN từ Trung tâm Khuyến nông"
	}
	return sourceNotificationTemplate{Kind: notifyport.KindDirective, Severity: sev, Title: title,
		Body:     "Mở để đọc nội dung chỉ đạo và xác nhận đã đọc.",
		DeepLink: "/extension/directives/" + p.DirectiveID.String(), SubjectRef: sourceNotificationRef(p.DirectiveID),
		CreatedAt: sourceNotificationTime(p.IssuedAt, occurred)}
}

func sourceDirectiveUnits(units []string) []string {
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

func sourceDirectiveExpandUnits(ctx context.Context, units []string, at time.Time, directory partyport.AdminUnits) ([]string, error) {
	if directory == nil {
		return units, nil
	}
	out := append([]string(nil), units...)
	for _, u := range units {
		desc, err := directory.Descendants(ctx, u, at)
		if errors.Is(err, partyport.ErrUnitNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, desc...)
	}
	return sourceDirectiveUnits(out), nil
}

func sourceDirectiveDigest(items []notifyport.DigestDraft) (subject, body string) {
	subject = fmt.Sprintf("[AgriGreen] %d chỉ đạo khẩn mới", len(items))
	var b strings.Builder
	b.WriteString("Tóm tắt chỉ đạo khẩn đã ban hành. Nội dung đầy đủ xem trong ứng dụng AgriGreen (đăng nhập).\n\n")
	for _, it := range items {
		fmt.Fprintf(&b, "- %s | vùng: %s | mở: /extension/directives/%s\n",
			sourceNotificationVietnamTime(it.IssuedAt), strings.Join(it.AudienceUnits, ", "), it.Reference)
	}
	b.WriteString("\nEmail tự động từ hệ thống, không trả lời thư này.\n")
	return subject, b.String()
}
