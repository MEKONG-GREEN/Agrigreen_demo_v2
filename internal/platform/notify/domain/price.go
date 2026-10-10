package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
)

// Nhắc nhập giá của Báo giá nông sản (W11.74, pf/price → pf/notify; BRD FN-XA-03.04). Tiêu đề/nội dung do pf/price dựng
// từ mẫu của Cấp Tỉnh (biến {ngày} {xã} {giờ khóa}); Redact che thêm chuỗi giống SĐT.

// PriceReminderLink là đường dẫn mở màn Nhập báo giá (FN-XA-03.04 B3) — màn của W11.75.
const PriceReminderLink = "/kn/bao-gia/nhap-gia"

// PriceReminderNotifications dựng thông báo từ pf.price.reminder.issued: có người nhận → mỗi người một thông báo;
// không → thông báo chủ đề tới cán bộ xã (vai kn_officer) của các xã trong communes, rỗng = toàn tenant. Kênh trong
// ứng dụng tắt (in_app = false) → không dựng thông báo (email theo từng người nhận chưa có ở pf/notify — W11.74 ghi rõ).
func PriceReminderNotifications(payload json.RawMessage, occurred time.Time) ([]Notification, error) {
	var p priceport.ReminderIssuedV1
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("notify: payload %s: %w", priceport.EventReminderIssued, err)
	}
	if !p.InApp && !p.Test {
		return nil, nil
	}
	tpl := Template{Kind: port.KindPriceReminder, Severity: port.SeverityNormal, Title: p.Title, Body: p.Body,
		DeepLink: PriceReminderLink, CreatedAt: occurred}
	refs := DedupRefs(p.RecipientRefs)
	if len(refs) > 0 {
		out := make([]Notification, 0, len(refs))
		for _, r := range refs {
			out = append(out, tpl.For(r))
		}
		return out, nil
	}
	a := Audience{Roles: []string{partyport.RoleKNOfficer}}
	if units := DedupUnits(p.Communes); len(units) > 0 {
		a.Units = units
	} else {
		a.National = true
	}
	return []Notification{tpl.ForAudience(a)}, nil
}
