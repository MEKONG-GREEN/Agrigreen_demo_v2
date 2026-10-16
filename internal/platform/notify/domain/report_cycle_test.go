package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

// KN v7.7 FN-TI-02.02/02.03/02.04: thông báo vòng đời báo cáo tới đúng cán bộ phụ trách — một thông báo mỗi cán bộ mỗi
// đợt (không tràn mỗi công việc), nhắc thủ công/tự động khác kiểu, yêu cầu điều chỉnh mang lý do.
func TestFN_TI_02_ReportCycleNotifications(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	at := time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)
	issued, _ := json.Marshal(extport.ReportBatchIssuedV1{BatchID: uuid.NewString(), SeasonLabel: "Thu Đông 2026", PeriodCount: 2, FirstDueAt: "2026-09-15T10:00:00Z",
		Assignees: []extport.TaskAssigneesV1{{CommuneCode: "x1", OfficerRefs: []string{a.String()}, TaskCount: 4},
			{CommuneCode: "x2", OfficerRefs: []string{a.String(), b.String()}, TaskCount: 4}}})
	ns, err := ReportCycleNotifications(extport.EventReportBatchIssued, issued, at)
	if err != nil || len(ns) != 2 || ns[0].Kind != port.KindReportTaskAssigned || *ns[0].RecipientRef != a {
		t.Fatalf("mỗi cán bộ một thông báo: %+v %v", ns, err)
	}
	if ns[0].Body != "Thu Đông 2026 — 8 công việc (2 kỳ), hạn kỳ đầu 15/09/2026 17:00." {
		t.Fatalf("nội dung: %q", ns[0].Body)
	}
	rem, _ := json.Marshal(extport.ReportTaskRemindedV1{Mode: extport.ReminderAutoBefore, PeriodLabel: "Tuần 1", Message: "Sắp đến hạn",
		RecipientRefs: []string{a.String(), a.String()}})
	if ns, _ := ReportCycleNotifications(extport.EventReportTaskReminded, rem, at); len(ns) != 1 || ns[0].Kind != port.KindReportTaskDue {
		t.Fatalf("tự nhắc: %+v", ns)
	}
	man, _ := json.Marshal(extport.ReportTaskRemindedV1{Mode: extport.ReminderManual, PeriodLabel: "Tuần 1", Message: "Nộp ngay", RecipientRefs: []string{b.String()}})
	if ns, _ := ReportCycleNotifications(extport.EventReportTaskReminded, man, at); len(ns) != 1 || ns[0].Kind != port.KindReportReminder {
		t.Fatalf("nhắc thủ công: %+v", ns)
	}
	adj, _ := json.Marshal(extport.SubmissionAdjustmentRequestedV1{TaskID: uuid.NewString(), FormCode: "T01", PeriodLabel: "Tuần 1", CommuneCode: "x1",
		Reason: "Số tuần 1 lệch", RecipientRefs: []string{a.String()}})
	if ns, _ := ReportCycleNotifications(extport.EventSubmissionAdjustmentRequested, adj, at); len(ns) != 1 || ns[0].Kind != port.KindReportAdjustmentRequested ||
		ns[0].Body != "T01 · Tuần 1 · xã x1: Số tuần 1 lệch" {
		t.Fatalf("yêu cầu điều chỉnh kèm lý do: %+v", ns)
	}
	for _, n := range ns {
		if !n.Normalize() {
			t.Fatal("thông báo hợp lệ")
		}
	}
}
