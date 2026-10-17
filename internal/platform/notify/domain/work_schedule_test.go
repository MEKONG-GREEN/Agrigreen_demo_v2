package domain

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

// KN v7.7 FN-XA-03.08 AC: Cán bộ xã nhận thông báo kết quả duyệt kèm lý do; FN-TI-02.07: người duyệt nhận lịch
// chờ duyệt; nhắc hạn đăng ký / quá hạn duyệt.
func TestFN_XA_03_08_WorkScheduleNotifications(t *testing.T) {
	at := time.Date(2026, 9, 15, 3, 0, 0, 0, time.UTC)
	owner, head, sid := uuid.New(), uuid.New(), uuid.New()
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

	reason := "Bổ sung địa điểm, gọi 0912345678 nếu cần"
	ns, err := WorkScheduleNotifications(extport.EventWorkScheduleReviewed, raw(extport.WorkScheduleReviewedV1{
		ScheduleID: sid, OwnerRef: owner, ReviewerRef: head, WeekStart: "2026-09-21", Version: 2,
		Decision: extport.ScheduleDecisionChangeRequested, Reason: &reason, EventTime: at.Format(time.RFC3339),
	}), at)
	if err != nil || len(ns) != 1 || *ns[0].RecipientRef != owner || ns[0].Kind != port.KindWorkScheduleReviewed {
		t.Fatalf("kết quả duyệt → người đăng ký: %+v %v", ns, err)
	}
	n := ns[0]
	if !n.Normalize() || !strings.Contains(n.Body, "Bổ sung địa điểm") || strings.Contains(n.Body, "0912345678") ||
		!strings.Contains(n.Body, "21/09–27/09/2026") || n.DeepLink != "/extension/work-schedules/"+sid.String() {
		t.Fatalf("thông báo kèm lý do (SĐT bị che), tuần, đường dẫn: %+v", n)
	}
	if ns, _ := WorkScheduleNotifications(extport.EventWorkScheduleReviewed, raw(extport.WorkScheduleReviewedV1{
		ScheduleID: sid, OwnerRef: owner, ReviewerRef: uuid.New(), Decision: extport.ScheduleDecisionApproved, Auto: true,
	}), at); len(ns) != 0 {
		t.Fatal("tự duyệt khi gửi (Q-16 auto): không báo lại người gửi")
	}

	ns, _ = WorkScheduleNotifications(extport.EventWorkScheduleSubmitted, raw(extport.WorkScheduleSubmittedV1{
		ScheduleID: sid, OwnerRef: owner, ProvinceCode: "89", WeekStart: "2026-09-21", Amendment: true, Late: true,
		ApproverRefs: []uuid.UUID{head, head, owner},
	}), at)
	if len(ns) != 1 || *ns[0].RecipientRef != head || !strings.Contains(ns[0].Title, "điều chỉnh") || !strings.Contains(ns[0].Body, "muộn") {
		t.Fatalf("gửi duyệt → người duyệt (bỏ trùng, bỏ người gửi): %+v", ns)
	}
	ns, _ = WorkScheduleNotifications(extport.EventWorkScheduleSubmitted, raw(extport.WorkScheduleSubmittedV1{
		ScheduleID: sid, OwnerRef: owner, ProvinceCode: "89", WeekStart: "2026-09-21", ApproverRefs: []uuid.UUID{},
	}), at)
	if len(ns) != 1 || ns[0].RecipientRef != nil || len(ns[0].Audience.Units) != 1 || ns[0].Audience.Units[0] != "89" {
		t.Fatalf("chưa biết người duyệt → chủ đề Cấp Tỉnh của tỉnh: %+v", ns)
	}

	ns, _ = WorkScheduleNotifications(extport.EventWorkScheduleReminded, raw(extport.WorkScheduleRemindedV1{
		Kind: extport.ScheduleReminderReviewOverdue, ProvinceCode: "89", WeekStart: "2026-09-21", DueAt: "2026-09-20T17:00:00Z",
		RecipientRefs: []uuid.UUID{head}, Pending: 3,
	}), at)
	if len(ns) != 1 || ns[0].Kind != port.KindWorkScheduleDue || !strings.Contains(ns[0].Body, "Còn 3 lịch") || !ns[0].Normalize() {
		t.Fatalf("nhắc quá hạn duyệt: %+v", ns)
	}
}

// Captures the unchanged legacy implementations in phase A; the owner only reads the frozen results.
func TestWorkflowLegacyRendererVectors(t *testing.T) {
	var inputs []struct {
		Name, Group, Event, Payload string
		At                          time.Time
	}
	raw, err := os.ReadFile("../../../../tests/golden/kn-notify-workflow-inputs.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &inputs); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 77 {
		t.Fatalf("vector count %d", len(inputs))
	}
	type result struct {
		Name   string
		Drafts []port.NotificationDraft
		Error  string
	}
	results := make([]result, 0, len(inputs))
	for _, in := range inputs {
		var ns []Notification
		var err error
		if in.Group == "work" {
			ns, err = WorkScheduleNotifications(in.Event, json.RawMessage(in.Payload), in.At)
		} else {
			ns, err = ReportCycleNotifications(in.Event, json.RawMessage(in.Payload), in.At)
		}
		var drafts []port.NotificationDraft
		if ns != nil {
			drafts = make([]port.NotificationDraft, len(ns))
		}
		for i, n := range ns {
			drafts[i] = port.NotificationDraft{RecipientRef: n.RecipientRef, Audience: port.DraftAudience{National: n.Audience.National, Units: n.Audience.Units, OrgRef: n.Audience.OrgRef, Roles: n.Audience.Roles}, Kind: n.Kind, Severity: n.Severity, Title: n.Title, Body: n.Body, DeepLink: n.DeepLink, SubjectRef: n.SubjectRef, CreatedAt: n.CreatedAt, Email: n.Email}
		}
		out := result{Name: in.Name, Drafts: drafts}
		if err != nil {
			out.Error = err.Error()
		}
		results = append(results, out)
	}
	data, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("OLD_WORKFLOW=" + string(data))
}
