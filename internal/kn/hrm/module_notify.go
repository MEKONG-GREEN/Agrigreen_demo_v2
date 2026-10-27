package hrm

// NOTIFY-HCTH-KN (#50): pf/hrm đăng ký loại thông báo, consumer và builder của mình vào registry chung của pf/notify. Nội
// dung và chính sách người nhận HCTH ở đây (chủ nghiệp vụ); pf/notify giữ định danh hộp thư, giao dịch inbox, che dữ liệu
// và kênh gửi (đóng an toàn — #127, #115). Gốc lắp ráp (cmd/api-platform/assembly/notify_consumers.go) nối các hàm này.
//
// Người nhận tra qua port.NotifyDirectory CHỈ ĐỌC, tenant lấy từ phong bì sự kiện; lời gọi đi qua txdeps.Get nên chạy
// NGOÀI giao dịch inbox (#143). Không tra được → lỗi (consumer NACK, giao lại); không có người nhận → không thông báo —
// không bao giờ mở rộng thành thông báo chủ đề toàn tenant theo vai realm.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

// Đường dẫn màn web-ops (apps/web-ops/src/app/kn/nhan-su/*).
const (
	notifyLinkPromotion = "/kn/nhan-su/nang-bac"       // Danh sách nâng bậc (FN-09)
	notifyLinkUnitPlan  = "/kn/nhan-su/ke-hoach-phong" // Kế hoạch của phòng (lãnh đạo đơn vị — FN-11)
)

var errNoNotifyDirectory = errors.New("hrm: chưa nối danh bạ người nhận thông báo")

// NotificationKinds khai loại thông báo pf/hrm sở hữu (token gạch dưới — hợp đồng #48).
func NotificationKinds() notifyport.KindRegistration {
	return notifyport.KindRegistration{Kinds: []string{port.KindPromotionDue, port.KindPlanReminder}}
}

// NotificationConsumers khai hàng đợi pf.notify.hrm cho sự kiện pf/hrm (không mang tenant hay hàm DB).
func NotificationConsumers() notifyport.ConsumerRegistration {
	return notifyport.ConsumerRegistration{Definitions: []notifyport.ConsumerDefinition{{
		Name: port.NotifyConsumerHRM, Order: port.NotifyConsumerHRMOrder,
		Events: []string{port.EventPromotionDueDetected, port.EventPlanReminded},
	}}}
}

// NotificationBuilders dựng thông báo HCTH. acc là cấu hình quyền của bên dùng (KN: assembly.KNHRMAccess): người nhận nâng
// bậc là vai ReadBy ở phòng ban chủ (Phòng HCTH) và vai ManageBy (Admin). dir nil → mọi sự kiện lỗi (không đoán người nhận).
func NotificationBuilders(acc port.Access, dir port.NotifyDirectory) []notifyport.EventBuilderRegistration {
	return []notifyport.EventBuilderRegistration{
		{Events: []string{port.EventPromotionDueDetected}, Build: func(ctx context.Context, tid uuid.UUID, event string, payload json.RawMessage, at time.Time) ([]notifyport.NotificationDraft, error) {
			return promotionDueDrafts(ctx, acc, dir, tid, payload, at)
		}},
		{Events: []string{port.EventPlanReminded}, Build: func(ctx context.Context, tid uuid.UUID, event string, payload json.RawMessage, at time.Time) ([]notifyport.NotificationDraft, error) {
			return planReminderDrafts(ctx, dir, tid, payload, at)
		}},
	}
}

func decodePayload(event string, payload json.RawMessage, v any) error {
	if err := json.Unmarshal(payload, v); err != nil {
		return fmt.Errorf("hrm: payload %s hỏng: %w", event, err)
	}
	return nil
}

func promotionDueDrafts(ctx context.Context, acc port.Access, dir port.NotifyDirectory, tid uuid.UUID, payload json.RawMessage, at time.Time) ([]notifyport.NotificationDraft, error) {
	var p port.PromotionDueDetectedV1
	if err := decodePayload(port.EventPromotionDueDetected, payload, &p); err != nil {
		return nil, err
	}
	if p.ScanID == uuid.Nil || p.Day == "" {
		return nil, fmt.Errorf("hrm: payload %s thiếu scan_id/day", port.EventPromotionDueDetected)
	}
	if dir == nil {
		return nil, errNoNotifyDirectory
	}
	var rs []uuid.UUID
	for _, role := range acc.ReadBy {
		xs, err := roleHolders(ctx, dir, tid, role, acc.Department)
		if err != nil {
			return nil, err
		}
		rs = append(rs, xs...)
	}
	for _, role := range acc.ManageBy {
		xs, err := roleHolders(ctx, dir, tid, role, "")
		if err != nil {
			return nil, err
		}
		rs = append(rs, xs...)
	}
	day := p.Day
	if d, err := time.Parse(time.DateOnly, p.Day); err == nil {
		day = d.Format("02/01/2006")
	}
	var parts []string
	for _, w := range p.Windows {
		parts = append(parts, fmt.Sprintf("%d tháng: %d", w.Months, w.Count))
	}
	body := "Đến hạn NLTX, TNVK chưa có quyết định — " + strings.Join(parts, "; ") + "."
	extra := []struct {
		label string
		n     int
	}{{"Quá hạn chưa xử lý", p.Overdue}, {"Chưa đủ điều kiện", p.Fail}, {"Cần xem xét", p.Review}, {"Thiếu mốc xếp bậc", p.Missing}}
	for _, x := range extra {
		if x.n > 0 {
			body += fmt.Sprintf(" %s: %d.", x.label, x.n)
		}
	}
	sev := notifyport.SeverityNormal
	if p.Overdue > 0 || p.Fail > 0 {
		sev = notifyport.SeverityUrgent
	}
	subject := p.ScanID
	tpl := notifyport.NotificationDraft{Kind: port.KindPromotionDue, Severity: sev, Title: "Kết quả tính mốc nâng bậc lương ngày " + day,
		Body: body + " Mở danh sách nâng bậc để kiểm tra điều kiện và xuất danh sách.", DeepLink: notifyLinkPromotion, SubjectRef: &subject,
		CreatedAt: at.UTC()}
	return draftsFor(tpl, rs, uuid.Nil), nil
}

func planReminderDrafts(ctx context.Context, dir port.NotifyDirectory, tid uuid.UUID, payload json.RawMessage, at time.Time) ([]notifyport.NotificationDraft, error) {
	var p port.PlanRemindedV1
	if err := decodePayload(port.EventPlanReminded, payload, &p); err != nil {
		return nil, err
	}
	if p.NodeRef == uuid.Nil || p.PeriodRef == uuid.Nil || len(p.Units) == 0 {
		return nil, fmt.Errorf("hrm: payload %s thiếu node_ref/period_ref/units", port.EventPlanReminded)
	}
	if dir == nil {
		return nil, errNoNotifyDirectory
	}
	units := slices.Clone(p.Units)
	key := "hrm.notify.managers:" + tid.String() + ":" + p.NodeRef.String() + ":" + strings.Join(units, ",")
	byUnit, err := txdeps.Get(ctx, key, func(ctx context.Context) (map[string][]uuid.UUID, error) {
		return dir.UnitManagers(ctx, tid, p.NodeRef, units)
	})
	if err != nil {
		return nil, err
	}
	var rs []uuid.UUID
	for _, u := range units {
		rs = append(rs, byUnit[u]...)
	}
	due := p.DueAt
	if t, err := time.Parse(time.RFC3339, p.DueAt); err == nil {
		due = t.In(clock.Zone()).Format("02/01/2006")
	}
	subject := p.PeriodRef
	tpl := notifyport.NotificationDraft{Kind: port.KindPlanReminder, Severity: notifyport.SeverityNormal,
		Title:    fmt.Sprintf("Nhắc lập kế hoạch nhân lực năm %d", p.Year),
		Body:     fmt.Sprintf("Phòng HCTH nhắc đơn vị chưa gửi kế hoạch nhân lực năm %d. Hạn gửi: %s.", p.Year, due),
		DeepLink: notifyLinkUnitPlan, SubjectRef: &subject, CreatedAt: at.UTC()}
	return draftsFor(tpl, rs, p.ActorRef), nil
}

func roleHolders(ctx context.Context, dir port.NotifyDirectory, tid uuid.UUID, role, dept string) ([]uuid.UUID, error) {
	return txdeps.Get(ctx, "hrm.notify.roles:"+tid.String()+":"+role+":"+dept, func(ctx context.Context) ([]uuid.UUID, error) {
		return dir.RoleHolders(ctx, tid, role, dept)
	})
}

// draftsFor: mỗi người nhận một thông báo cá nhân, bỏ Nil, bỏ trùng, bỏ người gây ra sự việc (exclude).
func draftsFor(tpl notifyport.NotificationDraft, refs []uuid.UUID, exclude uuid.UUID) []notifyport.NotificationDraft {
	seen := map[uuid.UUID]bool{uuid.Nil: true, exclude: true}
	var out []notifyport.NotificationDraft
	for _, r := range refs {
		if seen[r] {
			continue
		}
		seen[r] = true
		d := tpl
		ref := r
		d.RecipientRef = &ref
		out = append(out, d)
	}
	return out
}
