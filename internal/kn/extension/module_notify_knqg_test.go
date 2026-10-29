package extension

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

// W11.53b: nội dung thông báo biểu dự án KNQG — một thông báo cá nhân mỗi người nhận (bỏ trùng, bỏ Nil), đúng loại, đường
// dẫn mở thẳng biểu; mẫu/hành động không thông báo → lỗi (consumer NACK, không đoán).
func TestRenderKNQGNotifications_W1153b(t *testing.T) {
	doc, a, b := uuid.New(), uuid.New(), uuid.New()
	at := time.Date(2027, 10, 13, 3, 0, 0, 0, time.UTC)
	mk := func(form, period, act string, recips ...uuid.UUID) json.RawMessage {
		raw, _ := json.Marshal(extport.CNTSKNQGChangedV1{DocID: uuid.New(), DocRef: doc, ProjectRef: uuid.New(), ProjectCode: "KNQG-01",
			ProjectName: "Nuôi gà an toàn sinh học", ProvinceCode: "89", Form: form, Period: period, Action: act, Status: "sent",
			RecipientRefs: recips, ActorRef: uuid.New(), Department: "chan_nuoi_thuy_san", EventTime: at.Format(time.RFC3339)})
		return raw
	}
	cases := []struct {
		form, period, act, kind, title string
	}{
		{"07", "2027", extport.CNTSKNQGSent, extport.KindCNTSKNQGSent, "Mẫu 07/NTKQ-DA chờ Cấp TW xác nhận"},
		{"07", "2027", extport.CNTSKNQGConfirmed, extport.KindCNTSKNQGReviewed, "Mẫu 07/NTKQ-DA đã được Cấp TW xác nhận"},
		{"07", "2027", extport.CNTSKNQGReturned, extport.KindCNTSKNQGReviewed, "Mẫu 07/NTKQ-DA bị Cấp TW trả lại"},
		{"01", "2027-09", extport.CNTSKNQGSubmitted, extport.KindCNTSKNQGSubmitted, "Mẫu 01/TĐ-DA chờ duyệt"},
	}
	for _, c := range cases {
		got, err := renderKNQGNotifications(extport.EventCNTSKNQGChanged, mk(c.form, c.period, c.act, a, b, a, uuid.Nil), at.Add(time.Minute))
		if err != nil || len(got) != 2 {
			t.Fatalf("%s: %v %d", c.act, err, len(got))
		}
		for i, n := range got {
			want := []uuid.UUID{a, b}[i]
			if n.RecipientRef == nil || *n.RecipientRef != want || n.Kind != c.kind || n.Title != c.title || !n.CreatedAt.Equal(at) ||
				n.DeepLink != "/kn/cnts/du-an-mau-bieu?doc="+doc.String() || n.SubjectRef == nil || *n.SubjectRef != doc || n.Email {
				t.Fatalf("%s: %+v", c.act, n)
			}
		}
		if c.form == "01" && !strings.Contains(got[0].Body, "tháng 09/2027") || c.form == "07" && !strings.Contains(got[0].Body, "năm 2027") {
			t.Fatalf("kỳ: %s", got[0].Body)
		}
	}
	for _, bad := range []json.RawMessage{mk("05", "", extport.CNTSKNQGSent, a), mk("07", "2027", "approve", a), json.RawMessage(`{`)} {
		if _, err := renderKNQGNotifications(extport.EventCNTSKNQGChanged, bad, at); err == nil {
			t.Fatalf("phải lỗi: %s", bad)
		}
	}
	if got, err := renderKNQGNotifications(extport.EventCNTSKNQGChanged, mk("07", "2027", extport.CNTSKNQGSent), at); err != nil || len(got) != 0 {
		t.Fatalf("không người nhận: %v %d", err, len(got))
	}
}
