package handler_test

import (
	"testing"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// KN v7.7 FN-TI-02.02 AC / FN-TI-02.03 AC: phát hành → cán bộ xã phụ trách nhận "có công việc mới"; nhắc của Cấp Tỉnh
// tới đúng người (kiểu mới ghi được qua ràng buộc kind của pf 000073); giao lặp không trùng; cán bộ khác không nhận.
func TestFN_TI_02_03_ReportCycleInbox(t *testing.T) {
	f := newFixture(t)
	org := ids.New()
	a := f.userIn(f.tenant, org, authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	b := f.userIn(f.tenant, org, authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	issued := f.event(f.tenant, extport.EventReportBatchIssued, extport.ReportBatchIssuedV1{BatchID: ids.New().String(), DepartmentCode: "trong_trot",
		ProvinceCode: "89", ReportType: "weekly", SeasonLabel: "Thu Đông 2026", FormCodes: []string{"T01"}, PeriodCount: 2, TaskCount: 2,
		FirstDueAt: "2026-09-15T10:00:00Z", Assignees: []extport.TaskAssigneesV1{{CommuneCode: ward1, OfficerRefs: []string{a.actor.String()}, TaskCount: 2}},
		IssuedBy: ids.New().String(), EventTime: "2026-09-07T01:00:00Z"})
	f.process(port.ConsumerReportCycle, issued)
	f.process(port.ConsumerReportCycle, issued)
	f.process(port.ConsumerReportCycle, f.event(f.tenant, extport.EventReportTaskReminded, extport.ReportTaskRemindedV1{ReminderID: ids.New().String(),
		BatchID: ids.New().String(), PeriodID: ids.New().String(), PeriodLabel: "Tuần 1", Mode: extport.ReminderManual, Message: "Đề nghị nộp trước 17:00",
		DueAt: "2026-09-15T10:00:00Z", Communes: []string{ward1}, RecipientRefs: []string{a.actor.String()}, ActorRef: ids.New().String(),
		EventTime: "2026-09-14T01:00:00Z"}))
	in := a.inbox()
	kinds := map[any]bool{}
	for _, n := range in {
		kinds[n["kind"]] = true
	}
	if len(in) != 2 || !kinds[port.KindReportTaskAssigned] || !kinds[port.KindReportReminder] {
		t.Fatalf("cán bộ phụ trách: %v", in)
	}
	if len(b.inbox()) != 0 {
		t.Fatal("cán bộ không phụ trách không nhận")
	}
}
