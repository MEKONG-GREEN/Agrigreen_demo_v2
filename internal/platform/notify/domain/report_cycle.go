package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

// Thông báo vòng đời báo cáo KN v7.7 (gói P4). Không có số liệu báo cáo, chỉ biểu, kỳ, hạn và lý do/nội dung do Cấp
// Tỉnh soạn (Redact che SĐT/CCCD nếu lỡ nhập). Đẩy FCM chỉ dữ liệu (ADR-0015): app kéo nội dung qua API.

// ReportCycleEvents là sự kiện vòng đời báo cáo mà consumer pf.notify.report_cycle xử lý.
var ReportCycleEvents = []string{extport.EventReportBatchIssued, extport.EventReportTaskReminded, extport.EventSubmissionAdjustmentRequested,
	extport.EventPeriodReopened}

func refsOf(ss []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ss))
	for _, s := range ss {
		if u, err := uuid.Parse(s); err == nil && u != uuid.Nil {
			out = append(out, u)
		}
	}
	return DedupRefs(out)
}

// ReportCycleNotifications dựng thông báo cá nhân tới cán bộ phụ trách từ một sự kiện vòng đời báo cáo.
func ReportCycleNotifications(eventType string, payload json.RawMessage, occurred time.Time) ([]Notification, error) {
	bad := func(err error) ([]Notification, error) {
		return nil, fmt.Errorf("notify: payload %s: %w", eventType, err)
	}
	var tpl Template
	var recips []uuid.UUID
	switch eventType {
	case extport.EventReportBatchIssued:
		var p extport.ReportBatchIssuedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return bad(err)
		}
		due := viTime(ParseTime(p.FirstDueAt, occurred))
		at := ParseTime(p.EventTime, occurred)
		var out []Notification
		seen := map[uuid.UUID]int{}
		for _, a := range p.Assignees {
			for _, r := range refsOf(a.OfficerRefs) {
				seen[r] += a.TaskCount
			}
		}
		for _, a := range p.Assignees {
			for _, r := range refsOf(a.OfficerRefs) {
				n, ok := seen[r]
				if !ok {
					continue
				}
				delete(seen, r)
				t := Template{Kind: port.KindReportTaskAssigned, Severity: port.SeverityNormal, Title: "Có công việc nộp báo cáo mới",
					Body:     fmt.Sprintf("%s — %d công việc (%d kỳ), hạn kỳ đầu %s.", p.SeasonLabel, n, p.PeriodCount, due),
					DeepLink: "/kn/nhiem-vu/cong-viec", SubjectRef: parseRef(p.BatchID), CreatedAt: at}
				out = append(out, t.For(r))
			}
		}
		return out, nil
	case extport.EventReportTaskReminded:
		var p extport.ReportTaskRemindedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return bad(err)
		}
		kind, title := port.KindReportReminder, "Cấp Tỉnh nhắc nộp báo cáo"
		if p.Mode != extport.ReminderManual {
			kind, title = port.KindReportTaskDue, "Nhắc hạn nộp báo cáo"
		}
		tpl = Template{Kind: kind, Severity: port.SeverityNormal, Title: title, Body: p.PeriodLabel + ": " + truncate(p.Message, 400),
			DeepLink: "/kn/nhiem-vu/cong-viec", SubjectRef: parseRef(p.PeriodID), CreatedAt: ParseTime(p.EventTime, occurred)}
		recips = refsOf(p.RecipientRefs)
	case extport.EventSubmissionAdjustmentRequested:
		var p extport.SubmissionAdjustmentRequestedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return bad(err)
		}
		tpl = Template{Kind: port.KindReportAdjustmentRequested, Severity: port.SeverityNormal, Title: "Yêu cầu điều chỉnh báo cáo",
			Body: p.FormCode + " · " + p.PeriodLabel + " · xã " + p.CommuneCode + ": " + truncate(p.Reason, 400), DeepLink: "/kn/nhiem-vu/cong-viec/" + p.TaskID,
			SubjectRef: parseRef(p.TaskID), CreatedAt: ParseTime(p.EventTime, occurred)}
		recips = refsOf(p.RecipientRefs)
	case extport.EventPeriodReopened:
		var p extport.PeriodReopenedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return bad(err)
		}
		tpl = Template{Kind: port.KindReportPeriodReopened, Severity: port.SeverityNormal, Title: "Kỳ báo cáo được mở lại",
			Body:     fmt.Sprintf("%s mở lại đến %s: %s", p.Label, viTime(ParseTime(p.ReopenUntil, occurred)), truncate(p.Reason, 300)),
			DeepLink: "/kn/nhiem-vu/cong-viec", SubjectRef: parseRef(p.PeriodID), CreatedAt: ParseTime(p.EventTime, occurred)}
		recips = refsOf(p.RecipientRefs)
	default:
		return nil, nil
	}
	out := make([]Notification, 0, len(recips))
	for _, r := range recips {
		out = append(out, tpl.For(r))
	}
	return out, nil
}

func parseRef(s string) *uuid.UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return ref(u)
}
