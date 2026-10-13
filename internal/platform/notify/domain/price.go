package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// Thông báo Báo giá nông sản (pf/price → pf/notify; W11.74 nhắc nhập giá, gói NOTIF — G1/D5: cảnh báo, xác minh, tổng
// hợp giờ khóa, duyệt / mở duyệt bảng giá). Tiêu đề/nội dung chỉ gồm tên xã (ĐVHC), ngày, mã mặt hàng, số đếm — không
// dữ liệu cá nhân; Redact che thêm chuỗi giống SĐT.

// Đường dẫn màn web-ops (apps/web-ops/src/app/kn/bao-gia/*).
const (
	PriceReminderLink = "/kn/bao-gia/nhap"       // Nhập báo giá (FN-XA-03.04 B3)
	PriceVerifyLink   = "/kn/bao-gia/xac-minh"   // Yêu cầu xác minh của Xã (FN-XA-03.05)
	PriceReviewLink   = "/kn/bao-gia/kiem-duyet" // Kiểm duyệt & công bố của Cấp Tỉnh (FN-TI-02.03, 02.05)
	PriceHistoryLink  = "/kn/bao-gia/lich-su"    // Lịch sử báo giá của Xã (FN-XA-03.03) — phiếu nhập thay (W11.77)
)

// PriceTinhRoles: vai của thông báo chủ đề Cấp Tỉnh khi không tra được người nhận cụ thể (không email).
var PriceTinhRoles = []string{authz.KNTinh, partyport.RoleKNProvinceAdmin}

// PriceMessage là thông báo Báo giá dựng từ một sự kiện, CHƯA chọn người nhận Cấp Tỉnh (app tra port.PriceDesk).
type PriceMessage struct {
	Tpl     Template
	Profile string
	// Xa: người nhận Cấp Xã cụ thể (cán bộ Xã phụ trách chính); rỗng = không có.
	Xa []uuid.UUID
	// Tinh: gửi cho Cấp Tỉnh — Owner (cán bộ được phân công xã; nil = mọi Cấp Tỉnh của bên dùng) + Quản trị nghiệp vụ.
	Tinh  bool
	Owner *uuid.UUID
	// Exclude: người gây ra sự việc (không tự báo cho mình).
	Exclude uuid.UUID
	Email   bool
	// Topic: thông báo chủ đề sẵn (nhắc nhập giá khi pf/price chưa có danh sách xã).
	Topic *Audience
}

func shortDay(day string) string {
	if t, err := time.Parse(time.DateOnly, day); err == nil {
		return t.Format("02/01/2006")
	}
	return day
}

func itemList(codes []string) string {
	const max = 10
	if len(codes) <= max {
		return strings.Join(codes, ", ")
	}
	return strings.Join(codes[:max], ", ") + fmt.Sprintf(" … (+%d)", len(codes)-max)
}

func refList(r *uuid.UUID) []uuid.UUID {
	if r == nil || *r == uuid.Nil {
		return nil
	}
	return []uuid.UUID{*r}
}

// PriceMessages dựng thông báo Báo giá từ sự kiện của pf/price. Sự kiện không thuộc nhóm → (nil, false, nil).
func PriceMessages(eventType string, payload json.RawMessage, occurred time.Time) ([]PriceMessage, bool, error) {
	dec := func(v any) error {
		if err := json.Unmarshal(payload, v); err != nil {
			return fmt.Errorf("notify: payload %s: %w", eventType, err)
		}
		return nil
	}
	tpl := func(kind, link string, subject *uuid.UUID, title, body string) Template {
		return Template{Kind: kind, Severity: port.SeverityNormal, Title: title, Body: body, DeepLink: link, SubjectRef: subject,
			CreatedAt: occurred}
	}
	switch eventType {
	case priceport.EventReminderIssued:
		var p priceport.ReminderIssuedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		return priceReminder(p, occurred), true, nil
	case priceport.EventAlertRaised:
		var p priceport.AlertRaisedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		t := tpl(port.KindPriceAlert, PriceReviewLink, &p.TicketID,
			fmt.Sprintf("Cảnh báo biến động giá — %s, %s", p.PointName, shortDay(p.Day)),
			fmt.Sprintf("Xã %s gửi báo giá ngày %s có %d mặt hàng vượt ngưỡng (Tạm tính): %s. Vui lòng rà soát.",
				p.PointName, shortDay(p.Day), len(p.ItemCodes), itemList(p.ItemCodes)))
		return []PriceMessage{{Tpl: t, Profile: p.Profile, Tinh: true, Owner: p.OwnerRef, Email: true}}, true, nil
	case priceport.EventVerificationRequested:
		var p priceport.VerificationRequestedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		t := tpl(port.KindPriceVerifyRequest, PriceVerifyLink, &p.TicketID,
			fmt.Sprintf("Yêu cầu xác minh giá — %s, %s", p.PointName, shortDay(p.Day)),
			fmt.Sprintf("Cấp Tỉnh đề nghị xã kiểm tra lại %d mặt hàng (%s) của báo giá ngày %s. Hạn phản hồi: %s.",
				len(p.ItemCodes), itemList(p.ItemCodes), shortDay(p.Day), viTime(ParseTime(p.RespondBy, occurred))))
		return []PriceMessage{{Tpl: t, Profile: p.Profile, Xa: refList(p.OfficerRef), Exclude: p.RequesterRef, Email: true}}, true, nil
	case priceport.EventVerificationResponded:
		var p priceport.VerificationRespondedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		choice := "giữ giá đã gửi"
		if p.Choice == "propose" {
			choice = "đề xuất giá mới"
		}
		t := tpl(port.KindPriceVerifyReply, PriceReviewLink, &p.TicketID,
			fmt.Sprintf("Xã đã phản hồi xác minh — %s, %s", p.PointName, shortDay(p.Day)),
			fmt.Sprintf("Xã %s phản hồi mặt hàng %s (báo giá ngày %s): %s. Xem giải trình và chốt cảnh báo.",
				p.PointName, p.ItemCode, shortDay(p.Day), choice))
		return []PriceMessage{{Tpl: t, Profile: p.Profile, Tinh: true, Owner: p.OwnerRef, Exclude: p.ResponderRef, Email: true}}, true, nil
	case priceport.EventVerificationDecided:
		var p priceport.VerificationDecidedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		verb := map[string]string{priceport.DecisionKept: "giữ nguyên", priceport.DecisionExcluded: "loại khỏi bảng giá",
			priceport.DecisionReopened: "mở lại để rà soát", priceport.DecisionAdjusted: "hiệu chỉnh giá"}[p.Decision]
		if verb == "" {
			verb = p.Decision
		}
		t := tpl(port.KindPriceVerifyDecision, PriceVerifyLink, &p.TicketID,
			fmt.Sprintf("Kết quả xử lý báo giá — %s, %s", p.PointName, shortDay(p.Day)),
			fmt.Sprintf("Cấp Tỉnh đã %s: %s (báo giá ngày %s).", verb, itemList(p.ItemCodes), shortDay(p.Day)))
		return []PriceMessage{{Tpl: t, Profile: p.Profile, Xa: refList(p.OfficerRef), Exclude: p.DeciderRef, Email: true}}, true, nil
	case priceport.EventLockSummarized:
		var p priceport.LockSummarizedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		t := tpl(port.KindPriceLockSummary, PriceReviewLink, nil, fmt.Sprintf("Khóa nhập báo giá ngày %s", shortDay(p.Day)),
			fmt.Sprintf("Đã khóa nhập lúc %s. %d/%d xã gửi đúng hạn, %d nộp muộn, %d không nộp; %d cảnh báo chưa xử lý.",
				viTime(ParseTime(p.LockAt, occurred)), p.Submitted, p.Points, p.Late, p.Missed, p.OpenAlerts))
		return []PriceMessage{{Tpl: t, Profile: p.Profile, Tinh: true, Email: true}}, true, nil
	case priceport.EventBoardPublished:
		var p priceport.BoardPublishedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		title := fmt.Sprintf("Bảng giá ngày %s đã duyệt (v%d)", shortDay(p.BoardDate), p.Version)
		if p.Reapproval {
			title = fmt.Sprintf("Bảng giá ngày %s đã duyệt lại (v%d)", shortDay(p.BoardDate), p.Version)
		}
		t := tpl(port.KindPriceBoardApproved, PriceReviewLink, nil, title,
			fmt.Sprintf("%d mặt hàng, %d phiếu báo giá. Bảng giá đã công bố cho các cấp.", p.Items, p.Tickets))
		return []PriceMessage{{Tpl: t, Profile: p.Profile, Tinh: true}}, true, nil
	case priceport.EventBoardReopened:
		var p priceport.BoardReopenedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		t := tpl(port.KindPriceBoardReopened, PriceReviewLink, nil, fmt.Sprintf("Bảng giá ngày %s đang mở duyệt", shortDay(p.BoardDate)),
			fmt.Sprintf("Phiên bản v%d đang mở duyệt để sửa; các cấp vẫn thấy v%d (\"Đang cập nhật\") tới khi duyệt lại.", p.Version, p.Version))
		return []PriceMessage{{Tpl: t, Profile: p.Profile, Tinh: true, Exclude: p.ActorRef}}, true, nil
	case priceport.EventTicketProxied:
		var p priceport.TicketProxiedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		t := tpl(port.KindPriceProxyEntry, PriceHistoryLink, &p.TicketID,
			fmt.Sprintf("Cấp Tỉnh đã nhập thay báo giá — %s, %s", p.PointName, shortDay(p.Day)),
			fmt.Sprintf("Báo giá ngày %s của xã %s (%d mặt hàng) được Cấp Tỉnh nhập thay, gắn nhãn Nộp muộn.", shortDay(p.Day),
				p.PointName, p.Items))
		return []PriceMessage{{Tpl: t, Profile: p.Profile, Xa: refList(p.OfficerRef), Exclude: p.ActorRef}}, true, nil
	}
	return nil, false, nil
}

// PriceEvents là sự kiện pf/price mà PriceMessages xử lý (consumer pf.notify.price).
var PriceEvents = []string{
	priceport.EventReminderIssued, priceport.EventAlertRaised, priceport.EventVerificationRequested,
	priceport.EventVerificationResponded, priceport.EventVerificationDecided, priceport.EventLockSummarized,
	priceport.EventBoardPublished, priceport.EventBoardReopened, priceport.EventTicketProxied,
}

// priceReminder: nhắc nhập giá (W11.74): có người nhận → mỗi người một thông báo (email theo cờ của lịch); không →
// thông báo chủ đề tới cán bộ xã (vai kn_officer) của các xã trong communes, rỗng = toàn tenant. Kênh trong ứng dụng tắt
// và không phải gửi thử → không dựng thông báo.
func priceReminder(p priceport.ReminderIssuedV1, occurred time.Time) []PriceMessage {
	if !p.InApp && !p.Test {
		return nil
	}
	t := Template{Kind: port.KindPriceReminder, Severity: port.SeverityNormal, Title: p.Title, Body: p.Body,
		DeepLink: PriceReminderLink, CreatedAt: occurred}
	if rs := DedupRefs(p.RecipientRefs); len(rs) > 0 {
		return []PriceMessage{{Tpl: t, Profile: p.Profile, Xa: rs, Email: p.Email}}
	}
	a := Audience{Roles: []string{partyport.RoleKNOfficer}}
	if units := DedupUnits(p.Communes); len(units) > 0 {
		a.Units = units
	} else {
		a.National = true
	}
	return []PriceMessage{{Tpl: t, Profile: p.Profile, Topic: &a}}
}

// PriceTinhAudience là thông báo chủ đề Cấp Tỉnh (dự phòng khi không tra được người nhận cụ thể).
func PriceTinhAudience() Audience {
	return Audience{National: true, Roles: append([]string(nil), PriceTinhRoles...)}
}

// Notifications dựng thông báo của m với người nhận Cấp Tỉnh đã tra (tinh: người được phân công hoặc mọi Cấp Tỉnh, cộng
// Quản trị nghiệp vụ). m.Tinh mà tinh rỗng → thêm một thông báo chủ đề Cấp Tỉnh (không email). Bỏ trùng, bỏ người gây
// ra sự việc; cờ email chỉ trên thông báo cá nhân.
func (m PriceMessage) Notifications(tinh []uuid.UUID) []Notification {
	if m.Topic != nil {
		return []Notification{m.Tpl.ForAudience(*m.Topic)}
	}
	rs := append(append([]uuid.UUID(nil), m.Xa...), tinh...)
	if !m.Tinh {
		rs = m.Xa
	}
	var out []Notification
	for _, r := range DedupRefs(rs) {
		if r == m.Exclude {
			continue
		}
		n := m.Tpl.For(r)
		n.Email = m.Email
		out = append(out, n)
	}
	if m.Tinh && len(tinh) == 0 {
		out = append(out, m.Tpl.ForAudience(PriceTinhAudience()))
	}
	return out
}
