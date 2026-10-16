package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

func rawJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// priceNotes dựng thông báo của một sự kiện pf/price với người nhận Cấp Tỉnh tinh.
func priceNotes(t *testing.T, typ string, v any, at time.Time, tinh []uuid.UUID) []Notification {
	t.Helper()
	ms, ok, err := PriceMessages(typ, rawJSON(v), at)
	if err != nil || !ok {
		t.Fatalf("%s: ok=%v err=%v", typ, ok, err)
	}
	var out []Notification
	for _, m := range ms {
		out = append(out, m.Notifications(tinh)...)
	}
	for i := range out {
		if !out[i].Normalize() {
			t.Fatalf("%s: thông báo không hợp lệ %+v", typ, out[i])
		}
	}
	return out
}

// W11.74 (Báo giá FN-XA-03.04): nhắc nhập giá → mỗi CTV một thông báo (loại riêng price_reminder — NOTIF, email theo
// cờ của lịch); không danh sách → chủ đề tới cán bộ xã; kênh trong ứng dụng tắt → không dựng (trừ gửi thử).
func TestPriceReminderNotifications_W1174(t *testing.T) {
	at := time.Date(2026, 10, 12, 0, 30, 0, 0, time.UTC)
	ctv := uuid.New()
	base := priceport.ReminderIssuedV1{Profile: "kn", Day: "2026-10-12", Round: 1, LockAt: "2026-10-12T05:00:00Z",
		Title: "Nhắc nhập giá lần 1 — 12/10/2026", Body: "Nhắc nhập giá ngày 12/10/2026 cho Xã A", InApp: true, Email: true,
		Communes: []string{"00001"}}

	p := base
	p.RecipientRefs = []uuid.UUID{ctv, ctv}
	ns := priceNotes(t, priceport.EventReminderIssued, p, at, nil)
	if len(ns) != 1 || *ns[0].RecipientRef != ctv || ns[0].Kind != port.KindPriceReminder || ns[0].DeepLink != PriceReminderLink ||
		!ns[0].Email {
		t.Fatalf("theo người nhận: %+v", ns)
	}
	ns = priceNotes(t, priceport.EventReminderIssued, base, at, nil)
	if len(ns) != 1 || !ns[0].Topic() || ns[0].Audience.Units[0] != "00001" || ns[0].Audience.Roles[0] != partyport.RoleKNOfficer || ns[0].Email {
		t.Fatalf("chủ đề theo xã (không email): %+v", ns)
	}
	p = base
	p.Communes = nil
	if ns = priceNotes(t, priceport.EventReminderIssued, p, at, nil); len(ns) != 1 || !ns[0].Audience.National {
		t.Fatalf("chủ đề toàn tenant: %+v", ns)
	}
	p.InApp = false
	if ns = priceNotes(t, priceport.EventReminderIssued, p, at, nil); len(ns) != 0 {
		t.Fatalf("tắt kênh trong ứng dụng: %+v", ns)
	}
	p.Test, p.RecipientRefs = true, []uuid.UUID{ctv}
	if ns = priceNotes(t, priceport.EventReminderIssued, p, at, nil); len(ns) != 1 {
		t.Fatalf("gửi thử: %+v", ns)
	}
}

// NOTIF: người nhận đúng của từng sự kiện vòng đời — Xã nhận yêu cầu / kết quả; Cấp Tỉnh (đã tra: người được phân
// công / mọi Cấp Tỉnh + QTNV) nhận cảnh báo, phản hồi, tổng hợp, bảng giá; người gây ra sự việc không tự nhận; không
// tra được Cấp Tỉnh → chủ đề vai Cấp Tỉnh, không email.
func TestPriceLifecycleRecipients_NOTIF(t *testing.T) {
	at := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	ctv, owner, steward, other := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ticket := uuid.New()

	ns := priceNotes(t, priceport.EventAlertRaised, priceport.AlertRaisedV1{Profile: "kn", Day: "2026-10-12", PointCode: "XBG0001",
		PointName: "Mỹ Chánh", TicketID: ticket, ItemCodes: []string{"DUA_LEO"}, OwnerRef: &owner}, at, []uuid.UUID{owner, steward, owner})
	if len(ns) != 2 || ns[0].Kind != port.KindPriceAlert || !ns[0].Email || *ns[0].RecipientRef != owner || *ns[1].RecipientRef != steward ||
		*ns[0].SubjectRef != ticket || !strings.Contains(ns[0].Title, "Mỹ Chánh") || !strings.Contains(ns[0].Body, "DUA_LEO") {
		t.Fatalf("cảnh báo → Cấp Tỉnh phụ trách + QTNV: %+v", ns)
	}
	if ns = priceNotes(t, priceport.EventAlertRaised, priceport.AlertRaisedV1{Profile: "kn", Day: "2026-10-12", PointName: "A",
		TicketID: ticket, ItemCodes: []string{"X"}}, at, nil); len(ns) != 1 || !ns[0].Topic() || ns[0].Email ||
		!ns[0].Audience.National || len(ns[0].Audience.Roles) == 0 {
		t.Fatalf("không tra được Cấp Tỉnh → chủ đề, không email: %+v", ns)
	}

	ns = priceNotes(t, priceport.EventVerificationRequested, priceport.VerificationRequestedV1{Profile: "kn", Day: "2026-10-12",
		PointName: "Mỹ Chánh", TicketID: ticket, ItemCodes: []string{"B", "A"}, RespondBy: "2026-10-12T06:30:00Z",
		OfficerRef: &ctv, RequesterRef: owner}, at, nil)
	if len(ns) != 1 || *ns[0].RecipientRef != ctv || ns[0].Kind != port.KindPriceVerifyRequest || !ns[0].Email ||
		ns[0].DeepLink != PriceVerifyLink || !strings.Contains(ns[0].Body, "13:30") {
		t.Fatalf("yêu cầu xác minh → CTV: %+v", ns)
	}
	if ns = priceNotes(t, priceport.EventVerificationRequested, priceport.VerificationRequestedV1{Profile: "kn", Day: "2026-10-12",
		PointName: "A", TicketID: ticket, ItemCodes: []string{"A"}, RequesterRef: owner}, at, nil); len(ns) != 0 {
		t.Fatalf("xã chưa có cán bộ Xã → không ai nhận: %+v", ns)
	}

	ns = priceNotes(t, priceport.EventVerificationResponded, priceport.VerificationRespondedV1{Profile: "kn", Day: "2026-10-12",
		PointName: "A", TicketID: ticket, ItemCode: "A", Choice: "propose", OwnerRef: nil, ResponderRef: ctv}, at,
		[]uuid.UUID{owner, other, steward, ctv})
	if len(ns) != 3 || ns[0].Kind != port.KindPriceVerifyReply {
		t.Fatalf("phản hồi → mọi Cấp Tỉnh + QTNV, bỏ người phản hồi: %+v", ns)
	}

	ns = priceNotes(t, priceport.EventVerificationDecided, priceport.VerificationDecidedV1{Profile: "kn", Day: "2026-10-12",
		PointName: "A", TicketID: ticket, ItemCodes: []string{"A"}, Decision: priceport.DecisionExcluded, OfficerRef: &ctv, DeciderRef: owner}, at, nil)
	if len(ns) != 1 || *ns[0].RecipientRef != ctv || ns[0].Kind != port.KindPriceVerifyDecision || !strings.Contains(ns[0].Body, "loại khỏi bảng giá") {
		t.Fatalf("kết quả → CTV: %+v", ns)
	}

	ns = priceNotes(t, priceport.EventLockSummarized, priceport.LockSummarizedV1{Profile: "kn", Day: "2026-10-12",
		LockAt: "2026-10-12T05:00:00Z", Points: 10, Submitted: 7, Late: 1, Missed: 2, OpenAlerts: 3}, at, []uuid.UUID{owner, steward})
	if len(ns) != 2 || ns[0].Kind != port.KindPriceLockSummary || !ns[0].Email ||
		!strings.Contains(ns[0].Body, "12:00") || !strings.Contains(ns[0].Body, "7/10") || !strings.Contains(ns[0].Body, "3 cảnh báo") {
		t.Fatalf("tổng hợp 12:00 → Cấp Tỉnh: %+v", ns)
	}

	ns = priceNotes(t, priceport.EventBoardPublished, priceport.BoardPublishedV1{Profile: "kn", BoardDate: "2026-10-12", Version: 2,
		Items: 40, Tickets: 9, Reapproval: true}, at, []uuid.UUID{owner})
	if len(ns) != 1 || ns[0].Kind != port.KindPriceBoardApproved || ns[0].Email || !strings.Contains(ns[0].Title, "duyệt lại (v2)") {
		t.Fatalf("bảng giá duyệt → chuông Cấp Tỉnh: %+v", ns)
	}
	ns = priceNotes(t, priceport.EventBoardReopened, priceport.BoardReopenedV1{Profile: "kn", BoardDate: "2026-10-12", Version: 1,
		ActorRef: owner}, at, []uuid.UUID{owner, steward})
	if len(ns) != 1 || *ns[0].RecipientRef != steward || ns[0].Kind != port.KindPriceBoardReopened {
		t.Fatalf("mở duyệt → Cấp Tỉnh khác người mở: %+v", ns)
	}
	if _, ok, _ := PriceMessages("pf.price.other.event", nil, at); ok {
		t.Fatal("sự kiện lạ phải bỏ qua")
	}
}

// W11.77 (FN-TI-02.09 hậu điều kiện): Cấp Tỉnh nhập thay → cán bộ Xã phụ trách nhận chuông (không email), không gửi cho
// người nhập thay; xã chưa có cán bộ Xã → không ai nhận.
func TestPriceProxyEntryNotifies_W1177(t *testing.T) {
	at := time.Date(2026, 10, 8, 5, 30, 0, 0, time.UTC)
	officer, tinh := uuid.New(), uuid.New()
	ns := priceNotes(t, priceport.EventTicketProxied, priceport.TicketProxiedV1{Profile: "kn", Day: "2026-10-08", PointCode: "XBG0003",
		PointName: "Hà Tiên", TicketID: uuid.New(), Items: 38, OfficerRef: &officer, ActorRef: tinh}, at, []uuid.UUID{tinh})
	if len(ns) != 1 || *ns[0].RecipientRef != officer || ns[0].Kind != port.KindPriceProxyEntry || ns[0].Email ||
		ns[0].DeepLink != PriceHistoryLink || !strings.Contains(ns[0].Title, "Hà Tiên") || !strings.Contains(ns[0].Body, "Nộp muộn") {
		t.Fatalf("nhập thay → cán bộ Xã: %+v", ns)
	}
	if ns = priceNotes(t, priceport.EventTicketProxied, priceport.TicketProxiedV1{Profile: "kn", Day: "2026-10-08", PointCode: "XBG0003",
		PointName: "Hà Tiên", TicketID: uuid.New(), Items: 38, ActorRef: tinh}, at, nil); len(ns) != 0 {
		t.Fatalf("chưa có cán bộ Xã → không ai nhận: %+v", ns)
	}
}

// NOTIF: log chỉ ghi email đã che; email dựng từ tiêu đề/nội dung thông báo kèm liên kết web.
func TestMaskEmailAndMessage_NOTIF(t *testing.T) {
	for in, want := range map[string]string{
		"nguyen.van.a@mekong.vn": "ng•••@mekong.vn",
		"ab@x.vn":                "a•••@x.vn",
		" a@x.vn ":               "a•••@x.vn",
		"không-phải-email":       "•••",
		"@x.vn":                  "•••",
		"a@":                     "•••",
	} {
		if got := MaskEmail(in); got != want {
			t.Errorf("MaskEmail(%q) = %q, muốn %q", in, got, want)
		}
	}
	sub, body := EmailMessage(EmailJob{Title: "Yêu cầu xác minh", Body: "Nội dung", DeepLink: PriceVerifyLink}, "https://kn.example.vn/")
	if sub != "[AgriGreen] Yêu cầu xác minh" || !strings.Contains(body, "https://kn.example.vn/kn/bao-gia/xac-minh") {
		t.Fatalf("email: %q %q", sub, body)
	}
}
