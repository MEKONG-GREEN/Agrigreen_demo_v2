package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
)

// W11.74 (Báo giá FN-XA-03.04): nhắc nhập giá → mỗi CTV một thông báo; không danh sách → chủ đề tới cán bộ xã; kênh
// trong ứng dụng tắt → không dựng (trừ gửi thử).
func TestPriceReminderNotifications_W1174(t *testing.T) {
	at := time.Date(2026, 10, 12, 0, 30, 0, 0, time.UTC)
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	ctv := uuid.New()
	base := priceport.ReminderIssuedV1{Profile: "kn", Day: "2026-10-12", Round: 1, LockAt: "2026-10-12T05:00:00Z",
		Title: "Nhắc nhập giá lần 1 — 12/10/2026", Body: "Nhắc nhập giá ngày 12/10/2026 cho Xã A", InApp: true, Communes: []string{"00001"}}

	p := base
	p.RecipientRefs = []uuid.UUID{ctv, ctv}
	ns, err := PriceReminderNotifications(raw(p), at)
	if err != nil || len(ns) != 1 || *ns[0].RecipientRef != ctv || ns[0].Kind != port.KindPriceReminder || !ns[0].Normalize() ||
		ns[0].DeepLink != PriceReminderLink {
		t.Fatalf("theo người nhận: %+v %v", ns, err)
	}

	ns, err = PriceReminderNotifications(raw(base), at)
	if err != nil || len(ns) != 1 || !ns[0].Topic() || ns[0].Audience.Units[0] != "00001" || ns[0].Audience.Roles[0] != partyport.RoleKNOfficer {
		t.Fatalf("chủ đề theo xã: %+v %v", ns, err)
	}
	p = base
	p.Communes = nil
	if ns, _ = PriceReminderNotifications(raw(p), at); len(ns) != 1 || !ns[0].Audience.National {
		t.Fatalf("chủ đề toàn tenant: %+v", ns)
	}
	p.InApp = false
	if ns, _ = PriceReminderNotifications(raw(p), at); len(ns) != 0 {
		t.Fatalf("tắt kênh trong ứng dụng: %+v", ns)
	}
	p.Test, p.RecipientRefs = true, []uuid.UUID{ctv}
	if ns, _ = PriceReminderNotifications(raw(p), at); len(ns) != 1 {
		t.Fatalf("gửi thử: %+v", ns)
	}
}
