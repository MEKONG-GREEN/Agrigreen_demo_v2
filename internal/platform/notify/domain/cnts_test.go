package domain

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

func TestCNTSNotifications_W1143(t *testing.T) {
	at := time.Date(2027, 10, 13, 3, 0, 0, 0, time.UTC)
	r1, r2 := uuid.New(), uuid.New()
	prov := "89"
	raw, _ := json.Marshal(extport.CNTSAlertChangedV1{AlertRef: uuid.New(), Action: extport.CNTSAlertRaised, Kind: "outbreak", Level: "urgent",
		Title: "Ổ dịch mới · xã 30001", ProvinceCode: &prov, DueAt: at.Format(time.RFC3339), RecipientRefs: []uuid.UUID{r1, r2, r1},
		EventTime: at.Format(time.RFC3339)})
	ns, err := CNTSNotifications(extport.EventCNTSAlertChanged, raw, at)
	if err != nil || len(ns) != 2 {
		t.Fatalf("2 người nhận (bỏ trùng): %d %v", len(ns), err)
	}
	for _, n := range ns {
		if n.Kind != port.KindCNTSAlert || n.Severity != port.SeverityUrgent || !n.Email || !n.Normalize() {
			t.Fatalf("cảnh báo Khẩn: %+v", n)
		}
	}
	// Không người nhận → thông báo chủ đề Cấp Tỉnh của tỉnh.
	raw, _ = json.Marshal(extport.CNTSAlertChangedV1{AlertRef: uuid.New(), Action: extport.CNTSAlertOverdue, Kind: "herd_change", Level: "attention",
		Title: "Tổng đàn", ProvinceCode: &prov, DueAt: at.Format(time.RFC3339), RecipientRefs: []uuid.UUID{}, EventTime: at.Format(time.RFC3339)})
	ns, _ = CNTSNotifications(extport.EventCNTSAlertChanged, raw, at)
	if len(ns) != 1 || !ns[0].Topic() || ns[0].Kind != port.KindCNTSAlertOverdue || ns[0].Email {
		t.Fatalf("chủ đề quá hạn: %+v", ns)
	}
	raw, _ = json.Marshal(extport.CNTSFormRemindedV1{PeriodRef: uuid.New(), TemplateCode: "CNTS-BM04", Slot: extport.CNTSReminderRepeat,
		CommuneCode: "30001", Message: "Nhắc nộp", Channels: []string{"in_app", "email"}, RecipientRefs: []uuid.UUID{r1}, EventTime: at.Format(time.RFC3339)})
	ns, _ = CNTSNotifications(extport.EventCNTSFormReminded, raw, at)
	if len(ns) != 1 || ns[0].Kind != port.KindCNTSFormReminder || !ns[0].Email || !ns[0].Normalize() {
		t.Fatalf("nhắc nộp kèm email: %+v", ns)
	}
}

// Captures only the pre-existing legacy implementation in phase A. Expected output is frozen after execution.
func TestCNTSLegacyRendererVectors(t *testing.T) {
	var inputs []struct {
		Name, Event, Payload string
		At                   time.Time
	}
	raw, err := os.ReadFile("../../../../tests/golden/kn-notify-cnts-inputs.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &inputs); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 46 {
		t.Fatalf("vector count: %d", len(inputs))
	}
	type result struct {
		Name   string
		Drafts []port.NotificationDraft
		Error  string
	}
	results := make([]result, 0, len(inputs))
	for _, in := range inputs {
		ns, err := CNTSNotifications(in.Event, json.RawMessage(in.Payload), in.At)
		var drafts []port.NotificationDraft
		if ns != nil {
			drafts = make([]port.NotificationDraft, len(ns))
		}
		for i, n := range ns {
			drafts[i] = port.NotificationDraft{RecipientRef: n.RecipientRef,
				Audience: port.DraftAudience{National: n.Audience.National, Units: n.Audience.Units, OrgRef: n.Audience.OrgRef, Roles: n.Audience.Roles},
				Kind:     n.Kind, Severity: n.Severity, Title: n.Title, Body: n.Body, DeepLink: n.DeepLink, SubjectRef: n.SubjectRef, CreatedAt: n.CreatedAt, Email: n.Email}
		}
		r := result{Name: in.Name, Drafts: drafts}
		if err != nil {
			r.Error = err.Error()
		}
		results = append(results, r)
	}
	data, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("OLD_CNTS=" + string(data))
}
