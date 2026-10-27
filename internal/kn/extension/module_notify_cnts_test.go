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
	inApp.Channels, inApp.RecipientRefs = []string{"in_app"}, []uuid.UUID{r}
	d := renderReminder(t, inApp)
	if len(d) != 1 || d[0].Channels == nil || !d[0].Channels.InApp || d[0].Channels.Push || d[0].Channels.Email || d[0].Email {
		t.Fatalf("chỉ hộp thư, không push: %+v", d)
	}

	all := base
	all.Channels, all.RecipientRefs = []string{"in_app", "push", "email"}, []uuid.UUID{r}
	d = renderReminder(t, all)
	if c := d[0].Channels; c == nil || !c.InApp || !c.Push || !c.Email {
		t.Fatalf("đủ ba kênh: %+v", d)
	}

	topic := base
	topic.Channels = []string{"in_app", "push", "email"}
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
