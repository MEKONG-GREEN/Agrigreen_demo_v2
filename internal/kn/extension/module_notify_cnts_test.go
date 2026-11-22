package extension

// BF-CN-PUBLISH (#108, CN-07): kênh của cấu hình nhắc theo biểu được chuyển thành lựa chọn kênh tường minh của pf/notify
// (BF-NOTIFY-CNTS) — bỏ "Đẩy thông báo Mobile" thì không push; thông báo theo chủ đề không email; sự kiện cũ không kênh
// giữ hành vi mặc định.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

func renderReminder(t *testing.T, p extport.CNTSFormRemindedV1) []notifyport.NotificationDraft {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderCNTSNotifications(extport.EventCNTSFormReminded, raw, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCNTSReminderChannels_BF_CN_PUBLISH(t *testing.T) {
	r := uuid.New()
	base := extport.CNTSFormRemindedV1{PeriodRef: uuid.New(), Slot: extport.CNTSReminderBefore, CommuneCode: "30001", Message: "Nhắc nộp BM-04"}

	inApp := base
	inApp.Channels, inApp.ChannelsExplicit, inApp.RecipientRefs = []string{"in_app"}, true, []uuid.UUID{r}
	d := renderReminder(t, inApp)
	if len(d) != 1 || d[0].Channels == nil || !d[0].Channels.InApp || d[0].Channels.Push || d[0].Channels.Email || d[0].Email {
		t.Fatalf("chỉ hộp thư, không push: %+v", d)
	}

	all := base
	all.Channels, all.ChannelsExplicit, all.RecipientRefs = []string{"in_app", "push", "email"}, true, []uuid.UUID{r}
	d = renderReminder(t, all)
	if c := d[0].Channels; c == nil || !c.InApp || !c.Push || !c.Email {
		t.Fatalf("đủ ba kênh: %+v", d)
	}

	topic := base
	topic.Channels, topic.ChannelsExplicit = []string{"in_app", "push", "email"}, true
	d = renderReminder(t, topic)
	if len(d) != 1 || d[0].RecipientRef != nil || d[0].Channels == nil || d[0].Channels.Email || !d[0].Channels.Push {
		t.Fatalf("thông báo theo chủ đề không email: %+v", d)
	}

	legacy := base
	legacy.RecipientRefs = []uuid.UUID{r}
	if d = renderReminder(t, legacy); d[0].Channels != nil {
		t.Fatalf("sự kiện không kênh giữ hành vi cũ: %+v", d)
	}
}

// BF-NOTIFY-KN-ADAPTER (#121): sự kiện nhắc KHÔNG đánh dấu channels_explicit (sự kiện cũ trong outbox, nhắc đợt điều tra)
// giữ hành vi cũ dù có channels — hộp thư + push, email theo cờ (golden tests/golden/kn-notify-cnts.json); chỉ job nhắc
// theo cấu hình biểu (#108) đánh dấu tường minh.
func TestCNTSReminderLegacyChannelsKeepDefault_BF_NOTIFY_KN_ADAPTER(t *testing.T) {
	r := uuid.New()
	p := extport.CNTSFormRemindedV1{PeriodRef: uuid.New(), Slot: extport.CNTSReminderOverdue, CommuneCode: "30001", Message: "Nhắc nộp BM-04",
		Channels: []string{"in_app"}, RecipientRefs: []uuid.UUID{r}}
	d := renderReminder(t, p)
	if len(d) != 1 || d[0].Channels != nil || d[0].Email {
		t.Fatalf("sự kiện cũ chỉ in_app: phải giữ mặc định (Channels nil): %+v", d)
	}
	p.Channels = []string{"in_app", "email"}
	if d = renderReminder(t, p); d[0].Channels != nil || !d[0].Email {
		t.Fatalf("sự kiện cũ có email: cờ Email cũ, Channels nil: %+v", d)
	}
	raw, _ := json.Marshal(extport.CNTSFormRemindedV1{})
	if json.Valid(raw) && containsKey(raw, "channels_explicit") {
		t.Fatalf("payload không tường minh phải giữ nguyên dạng cũ (omitempty): %s", raw)
	}
}

func containsKey(raw []byte, key string) bool {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	_, ok := m[key]
	return ok
}

// Ổ dịch mới (#111 → cảnh báo Khẩn số 12, pf.extension.cnts_alert.changed level=urgent): thông báo Khẩn tới đúng người
// nhận cụ thể (hộp thư + push mặc định — không email-only — thêm email người có email đã xác minh); không người nhận →
// thông báo chủ đề Cấp Tỉnh của tỉnh, KHÔNG email; tiêu đề/nội dung chỉ số liệu tổng hợp (không người báo, không hộ).
func TestCNTSOutbreakUrgentAlertDrafts_BF_NOTIFY_KN_ADAPTER(t *testing.T) {
	r1, r2, reporter := uuid.New(), uuid.New(), uuid.New()
	prov := "89"
	title := "Ổ dịch mới · bệnh LMLM trên lon · xã 30001 · đàn 120 con, chết 3"
	p := extport.CNTSAlertChangedV1{AlertRef: uuid.New(), Action: "raised", Kind: "outbreak", Level: "urgent", Title: title,
		ProvinceCode: &prov, RecipientRefs: []uuid.UUID{r1, r2, r1, uuid.Nil}, DueAt: "2026-10-28T03:00:00Z", EventTime: "2026-10-28T02:00:00Z"}
	raw, _ := json.Marshal(p)
	d, err := renderCNTSNotifications(extport.EventCNTSAlertChanged, raw, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 2 {
		t.Fatalf("người nhận (bỏ trùng, bỏ Nil): %d", len(d))
	}
	for _, n := range d {
		if n.Severity != notifyport.SeverityUrgent || n.Kind != extport.KindCNTSAlert || n.Channels != nil || !n.Email ||
			n.DeepLink != cntsNotificationAlertLink || n.RecipientRef == nil || *n.RecipientRef == reporter {
			t.Fatalf("thông báo Khẩn ổ dịch: %+v", n)
		}
		if n.Body != title || n.Title != "Cảnh báo Khẩn" {
			t.Fatalf("nội dung ổ dịch: %q / %q", n.Title, n.Body)
		}
		if n.SubjectRef == nil || *n.SubjectRef != p.AlertRef {
			t.Fatalf("đối tượng thông báo phải là cảnh báo (không phải ổ dịch/hộ): %+v", n.SubjectRef)
		}
	}
	p.RecipientRefs = nil
	raw, _ = json.Marshal(p)
	d, err = renderCNTSNotifications(extport.EventCNTSAlertChanged, raw, time.Now())
	if err != nil || len(d) != 1 || d[0].RecipientRef != nil || d[0].Email || d[0].Audience.Units[0] != prov ||
		d[0].Severity != notifyport.SeverityUrgent {
		t.Fatalf("chủ đề Cấp Tỉnh, không email: %+v %v", d, err)
	}
	// Fail-closed: payload hỏng → lỗi (notify không gửi, giao lại), không bỏ qua.
	if _, err := renderCNTSNotifications(extport.EventCNTSAlertChanged, []byte(`{"recipient_refs":"x"}`), time.Now()); err == nil {
		t.Fatal("payload hỏng phải trả lỗi")
	}
	if _, err := renderCNTSNotifications(extport.EventCNTSFormReminded, []byte(`{"channels":7}`), time.Now()); err == nil {
		t.Fatal("payload nhắc hỏng phải trả lỗi")
	}
}
