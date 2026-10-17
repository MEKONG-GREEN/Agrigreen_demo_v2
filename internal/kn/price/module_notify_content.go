package price

import (
	"context"
	"encoding/json"
	"fmt"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/google/uuid"
	"strings"
	"time"
)

// NotificationBuilders owns price content and recipient selection, using only read-only lookups.
func NotificationBuilders(directory priceport.PriceDesk) []notifyport.EventBuilderRegistration {
	return []notifyport.EventBuilderRegistration{{Events: []string{
		priceport.EventReminderIssued, priceport.EventAlertRaised, priceport.EventVerificationRequested,
		priceport.EventVerificationResponded, priceport.EventVerificationDecided, priceport.EventLockSummarized,
		priceport.EventBoardPublished, priceport.EventBoardReopened, priceport.EventTicketProxied,
	}, Build: func(ctx context.Context, tid uuid.UUID, event string, payload json.RawMessage, at time.Time) ([]notifyport.NotificationDraft, error) {
		messages, _, err := priceNotificationMessages(event, payload, at)
		if err != nil {
			return nil, err
		}
		var out []notifyport.NotificationDraft
		for _, message := range messages {
			var recipients []uuid.UUID
			if message.Tinh {
				recipients, err = priceNotificationProvince(ctx, tid, directory, message)
				if err != nil {
					return nil, err
				}
			}
			out = append(out, message.Notifications(recipients)...)
		}
		return out, nil
	}}}
}

func priceNotificationProvince(ctx context.Context, tid uuid.UUID, directory priceport.PriceDesk, m priceNotificationMessage) ([]uuid.UUID, error) {
	if directory == nil {
		return nil, nil
	}
	var out []uuid.UUID
	if m.Owner != nil && *m.Owner != uuid.Nil {
		out = append(out, *m.Owner)
	} else {
		all, err := directory.ProvinceOfficers(ctx, tid, m.Profile)
		if err != nil {
			return nil, err
		}
		out = append(out, all...)
	}
	stewards, err := directory.Stewards(ctx, tid, m.Profile)
	if err != nil {
		return nil, err
	}
	return priceNotificationRefs(append(out, stewards...)), nil
}

// Thông báo Báo giá nông sản (pf/price → pf/notify; W11.74 nhắc nhập giá, gói NOTIF — G1/D5: cảnh báo, xác minh, tổng
// hợp giờ khóa, duyệt / mở duyệt bảng giá). Tiêu đề/nội dung chỉ gồm tên xã (ĐVHC), ngày, mã mặt hàng, số đếm — không
// dữ liệu cá nhân; Redact che thêm chuỗi giống SĐT.

// Đường dẫn màn web-ops (apps/web-ops/src/app/kn/bao-gia/*).
const (
	priceNotificationReminderLink = "/kn/bao-gia/nhap"       // Nhập báo giá (FN-XA-03.04 B3)
	priceNotificationVerifyLink   = "/kn/bao-gia/xac-minh"   // Yêu cầu xác minh của Xã (FN-XA-03.05)
	priceNotificationReviewLink   = "/kn/bao-gia/kiem-duyet" // Kiểm duyệt & công bố của Cấp Tỉnh (FN-TI-02.03, 02.05)
	priceNotificationHistoryLink  = "/kn/bao-gia/lich-su"    // Lịch sử báo giá của Xã (FN-XA-03.03) — phiếu nhập thay (W11.77)
)

// priceNotificationProvinceRoles: vai của thông báo chủ đề Cấp Tỉnh khi không tra được người nhận cụ thể (không email).
var priceNotificationProvinceRoles = []string{authz.KNTinh, partyport.RoleKNProvinceAdmin}

// priceNotificationMessage là thông báo Báo giá dựng từ một sự kiện, CHƯA chọn người nhận Cấp Tỉnh (app tra notifyport.PriceDesk).
type priceNotificationMessage struct {
	Tpl     priceNotificationTemplate
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
	Topic *notifyport.DraftAudience
}

func priceNotificationDay(day string) string {
	if t, err := time.Parse(time.DateOnly, day); err == nil {
		return t.Format("02/01/2006")
	}
	return day
}

func priceNotificationItems(codes []string) string {
	const max = 10
	if len(codes) <= max {
		return strings.Join(codes, ", ")
	}
	return strings.Join(codes[:max], ", ") + fmt.Sprintf(" … (+%d)", len(codes)-max)
}

func priceNotificationRefList(r *uuid.UUID) []uuid.UUID {
	if r == nil || *r == uuid.Nil {
		return nil
	}
	return []uuid.UUID{*r}
}

// priceNotificationMessages dựng thông báo Báo giá từ sự kiện của pf/price. Sự kiện không thuộc nhóm → (nil, false, nil).
func priceNotificationMessages(eventType string, payload json.RawMessage, occurred time.Time) ([]priceNotificationMessage, bool, error) {
	dec := func(v any) error {
		if err := json.Unmarshal(payload, v); err != nil {
			return fmt.Errorf("notify: payload %s: %w", eventType, err)
		}
		return nil
	}
	tpl := func(kind, link string, subject *uuid.UUID, title, body string) priceNotificationTemplate {
		return priceNotificationTemplate{Kind: kind, Severity: notifyport.SeverityNormal, Title: title, Body: body, DeepLink: link, SubjectRef: subject,
			CreatedAt: occurred}
	}
	switch eventType {
	case priceport.EventReminderIssued:
		var p priceport.ReminderIssuedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		return priceNotificationReminder(p, occurred), true, nil
	case priceport.EventAlertRaised:
		var p priceport.AlertRaisedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		t := tpl(notifyport.KindPriceAlert, priceNotificationReviewLink, &p.TicketID,
			fmt.Sprintf("Cảnh báo biến động giá — %s, %s", p.PointName, priceNotificationDay(p.Day)),
			fmt.Sprintf("Xã %s gửi báo giá ngày %s có %d mặt hàng vượt ngưỡng (Tạm tính): %s. Vui lòng rà soát.",
				p.PointName, priceNotificationDay(p.Day), len(p.ItemCodes), priceNotificationItems(p.ItemCodes)))
		return []priceNotificationMessage{{Tpl: t, Profile: p.Profile, Tinh: true, Owner: p.OwnerRef, Email: true}}, true, nil
	case priceport.EventVerificationRequested:
		var p priceport.VerificationRequestedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		t := tpl(notifyport.KindPriceVerifyRequest, priceNotificationVerifyLink, &p.TicketID,
			fmt.Sprintf("Yêu cầu xác minh giá — %s, %s", p.PointName, priceNotificationDay(p.Day)),
			fmt.Sprintf("Cấp Tỉnh đề nghị xã kiểm tra lại %d mặt hàng (%s) của báo giá ngày %s. Hạn phản hồi: %s.",
				len(p.ItemCodes), priceNotificationItems(p.ItemCodes), priceNotificationDay(p.Day), priceNotificationVietnamTime(priceNotificationTime(p.RespondBy, occurred))))
		return []priceNotificationMessage{{Tpl: t, Profile: p.Profile, Xa: priceNotificationRefList(p.OfficerRef), Exclude: p.RequesterRef, Email: true}}, true, nil
	case priceport.EventVerificationResponded:
		var p priceport.VerificationRespondedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		choice := "giữ giá đã gửi"
		if p.Choice == "propose" {
			choice = "đề xuất giá mới"
		}
		t := tpl(notifyport.KindPriceVerifyReply, priceNotificationReviewLink, &p.TicketID,
			fmt.Sprintf("Xã đã phản hồi xác minh — %s, %s", p.PointName, priceNotificationDay(p.Day)),
			fmt.Sprintf("Xã %s phản hồi mặt hàng %s (báo giá ngày %s): %s. Xem giải trình và chốt cảnh báo.",
				p.PointName, p.ItemCode, priceNotificationDay(p.Day), choice))
		return []priceNotificationMessage{{Tpl: t, Profile: p.Profile, Tinh: true, Owner: p.OwnerRef, Exclude: p.ResponderRef, Email: true}}, true, nil
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
		t := tpl(notifyport.KindPriceVerifyDecision, priceNotificationVerifyLink, &p.TicketID,
			fmt.Sprintf("Kết quả xử lý báo giá — %s, %s", p.PointName, priceNotificationDay(p.Day)),
			fmt.Sprintf("Cấp Tỉnh đã %s: %s (báo giá ngày %s).", verb, priceNotificationItems(p.ItemCodes), priceNotificationDay(p.Day)))
		return []priceNotificationMessage{{Tpl: t, Profile: p.Profile, Xa: priceNotificationRefList(p.OfficerRef), Exclude: p.DeciderRef, Email: true}}, true, nil
	case priceport.EventLockSummarized:
		var p priceport.LockSummarizedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		t := tpl(notifyport.KindPriceLockSummary, priceNotificationReviewLink, nil, fmt.Sprintf("Khóa nhập báo giá ngày %s", priceNotificationDay(p.Day)),
			fmt.Sprintf("Đã khóa nhập lúc %s. %d/%d xã gửi đúng hạn, %d nộp muộn, %d không nộp; %d cảnh báo chưa xử lý.",
				priceNotificationVietnamTime(priceNotificationTime(p.LockAt, occurred)), p.Submitted, p.Points, p.Late, p.Missed, p.OpenAlerts))
		return []priceNotificationMessage{{Tpl: t, Profile: p.Profile, Tinh: true, Email: true}}, true, nil
	case priceport.EventBoardPublished:
		var p priceport.BoardPublishedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		title := fmt.Sprintf("Bảng giá ngày %s đã duyệt (v%d)", priceNotificationDay(p.BoardDate), p.Version)
		if p.Reapproval {
			title = fmt.Sprintf("Bảng giá ngày %s đã duyệt lại (v%d)", priceNotificationDay(p.BoardDate), p.Version)
		}
		t := tpl(notifyport.KindPriceBoardApproved, priceNotificationReviewLink, nil, title,
			fmt.Sprintf("%d mặt hàng, %d phiếu báo giá. Bảng giá đã công bố cho các cấp.", p.Items, p.Tickets))
		return []priceNotificationMessage{{Tpl: t, Profile: p.Profile, Tinh: true}}, true, nil
	case priceport.EventBoardReopened:
		var p priceport.BoardReopenedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		t := tpl(notifyport.KindPriceBoardReopened, priceNotificationReviewLink, nil, fmt.Sprintf("Bảng giá ngày %s đang mở duyệt", priceNotificationDay(p.BoardDate)),
			fmt.Sprintf("Phiên bản v%d đang mở duyệt để sửa; các cấp vẫn thấy v%d (\"Đang cập nhật\") tới khi duyệt lại.", p.Version, p.Version))
		return []priceNotificationMessage{{Tpl: t, Profile: p.Profile, Tinh: true, Exclude: p.ActorRef}}, true, nil
	case priceport.EventTicketProxied:
		var p priceport.TicketProxiedV1
		if err := dec(&p); err != nil {
			return nil, true, err
		}
		t := tpl(notifyport.KindPriceProxyEntry, priceNotificationHistoryLink, &p.TicketID,
			fmt.Sprintf("Cấp Tỉnh đã nhập thay báo giá — %s, %s", p.PointName, priceNotificationDay(p.Day)),
			fmt.Sprintf("Báo giá ngày %s của xã %s (%d mặt hàng) được Cấp Tỉnh nhập thay, gắn nhãn Nộp muộn.", priceNotificationDay(p.Day),
				p.PointName, p.Items))
		return []priceNotificationMessage{{Tpl: t, Profile: p.Profile, Xa: priceNotificationRefList(p.OfficerRef), Exclude: p.ActorRef}}, true, nil
	}
	return nil, false, nil
}

// priceNotificationReminder: nhắc nhập giá (W11.74): có người nhận → mỗi người một thông báo (email theo cờ của lịch); không →
// thông báo chủ đề tới cán bộ xã (vai kn_officer) của các xã trong communes, rỗng = toàn tenant. Kênh trong ứng dụng tắt
// và không phải gửi thử → không dựng thông báo.
func priceNotificationReminder(p priceport.ReminderIssuedV1, occurred time.Time) []priceNotificationMessage {
	if !p.InApp && !p.Test {
		return nil
	}
	t := priceNotificationTemplate{Kind: notifyport.KindPriceReminder, Severity: notifyport.SeverityNormal, Title: p.Title, Body: p.Body,
		DeepLink: priceNotificationReminderLink, CreatedAt: occurred}
	if rs := priceNotificationRefs(p.RecipientRefs); len(rs) > 0 {
		return []priceNotificationMessage{{Tpl: t, Profile: p.Profile, Xa: rs, Email: p.Email}}
	}
	a := notifyport.DraftAudience{Roles: []string{partyport.RoleKNOfficer}}
	if units := priceNotificationUnits(p.Communes); len(units) > 0 {
		a.Units = units
	} else {
		a.National = true
	}
	return []priceNotificationMessage{{Tpl: t, Profile: p.Profile, Topic: &a}}
}

// priceNotificationProvinceAudience là thông báo chủ đề Cấp Tỉnh (dự phòng khi không tra được người nhận cụ thể).
func priceNotificationProvinceAudience() notifyport.DraftAudience {
	return notifyport.DraftAudience{National: true, Roles: append([]string(nil), priceNotificationProvinceRoles...)}
}

// Notifications dựng thông báo của m với người nhận Cấp Tỉnh đã tra (tinh: người được phân công hoặc mọi Cấp Tỉnh, cộng
// Quản trị nghiệp vụ). m.Tinh mà tinh rỗng → thêm một thông báo chủ đề Cấp Tỉnh (không email). Bỏ trùng, bỏ người gây
// ra sự việc; cờ email chỉ trên thông báo cá nhân.
func (m priceNotificationMessage) Notifications(tinh []uuid.UUID) []notifyport.NotificationDraft {
	if m.Topic != nil {
		return []notifyport.NotificationDraft{m.Tpl.ForAudience(*m.Topic)}
	}
	rs := append(append([]uuid.UUID(nil), m.Xa...), tinh...)
	if !m.Tinh {
		rs = m.Xa
	}
	var out []notifyport.NotificationDraft
	for _, r := range priceNotificationRefs(rs) {
		if r == m.Exclude {
			continue
		}
		n := m.Tpl.For(r)
		n.Email = m.Email
		out = append(out, n)
	}
	if m.Tinh && len(tinh) == 0 {
		out = append(out, m.Tpl.ForAudience(priceNotificationProvinceAudience()))
	}
	return out
}

type priceNotificationTemplate struct {
	Kind, Severity, Title, Body, DeepLink string
	SubjectRef                            *uuid.UUID
	CreatedAt                             time.Time
}

func (t priceNotificationTemplate) For(recipient uuid.UUID) notifyport.NotificationDraft {
	r := recipient
	return t.base(&r, notifyport.DraftAudience{})
}
func (t priceNotificationTemplate) ForAudience(a notifyport.DraftAudience) notifyport.NotificationDraft {
	return t.base(nil, a)
}
func (t priceNotificationTemplate) base(r *uuid.UUID, a notifyport.DraftAudience) notifyport.NotificationDraft {
	return notifyport.NotificationDraft{RecipientRef: r, Audience: a, Kind: t.Kind, Severity: t.Severity, Title: t.Title, Body: t.Body, DeepLink: t.DeepLink, SubjectRef: t.SubjectRef, CreatedAt: t.CreatedAt.UTC()}
}
func priceNotificationRefs(refs []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		if r == uuid.Nil || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}
func priceNotificationUnits(units []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(units))
	for _, u := range units {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}
func priceNotificationTime(s string, fallback time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return fallback.UTC()
}
func priceNotificationVietnamTime(t time.Time) string {
	return t.In(clock.Zone()).Format("02/01/2006 15:04")
}
