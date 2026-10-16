package domain

import (
	"encoding/json"
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
