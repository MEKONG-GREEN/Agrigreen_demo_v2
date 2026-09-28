// Package channel: kênh gửi thông báo ra ngoài của pf/notify (ADR-0015).
//
//   - Pusher: đẩy thông điệp DỮ LIỆU tới thiết bị (FCM HTTP v1, hoặc ghi log ở Dev).
//     Chỉ mang notification_id + kind — dịch vụ đẩy nằm ngoài lãnh thổ (RB-7).
//   - Mailer: email tóm tắt chỉ đạo khẩn qua SMTP trong lãnh thổ.
//   - ZNS: Zalo ZNS — CHƯA bật (cần ADR riêng về dữ liệu SĐT, xem ADR-0015).
//
// Kênh trong app (hộp thư) luôn bật và không nằm ở đây.
package channel

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
)

// ErrTokenInvalid: nhà cung cấp báo token không còn hiệu lực (gỡ app, token hết hạn).
// Người gọi ngừng dùng token, không thử lại.
var ErrTokenInvalid = errors.New("channel: token đẩy không còn hiệu lực")

// Pusher gửi một thông điệp dữ liệu tới một thiết bị. Lỗi khác ErrTokenInvalid là
// lỗi tạm thời — người gọi thử lại sau.
type Pusher interface {
	Name() string
	Send(ctx context.Context, m domain.PushMessage) error
}

// LogPusher là kênh đẩy của Local/Dev: chỉ ghi log loại và mã thông báo, không gửi.
type LogPusher struct{ Log *slog.Logger }

// Name thực hiện Pusher.
func (LogPusher) Name() string { return "log" }

// Send thực hiện Pusher. Không ghi token (R-14).
func (p LogPusher) Send(ctx context.Context, m domain.PushMessage) error {
	if p.Log != nil {
		id := ""
		if m.NotificationID != nil {
			id = m.NotificationID.String()
		}
		p.Log.InfoContext(ctx, "đẩy (dev, không gửi)", "event", "notify.push", "kind", m.Kind, "notification_id", id,
			"platform", m.Platform, "outcome", "logged")
	}
	return nil
}

// Mailer gửi một email văn bản thuần.
type Mailer interface {
	Send(ctx context.Context, to []string, subject, body string) error
}

// ErrZNSDisabled: kênh Zalo ZNS chưa được bật (ADR-0015).
var ErrZNSDisabled = errors.New("channel: Zalo ZNS chưa được bật")

// ZNS gửi tin mẫu Zalo ZNS tới một chủ thể trong kho định danh. SĐT không bao
// giờ đi qua pf/notify: bản hiện thực tương lai phải để kho định danh (vùng vault)
// tra SĐT theo subjectRef và gọi Zalo — cần ADR riêng trước khi bật.
type ZNS interface {
	SendTemplate(ctx context.Context, subjectRef uuid.UUID, templateID string, params map[string]string) error
}

// DisabledZNS là bản mặc định: luôn trả ErrZNSDisabled.
type DisabledZNS struct{}

// SendTemplate thực hiện ZNS.
func (DisabledZNS) SendTemplate(context.Context, uuid.UUID, string, map[string]string) error {
	return ErrZNSDisabled
}
