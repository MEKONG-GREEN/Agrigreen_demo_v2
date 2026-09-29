package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/channel"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ErrPushRetry: còn lần gửi lỗi tạm thời — trả cho inbox để hàng đợi giao lại (tối
// đa shared/inbox.DeliveryLimit lần rồi vào DLQ).
var ErrPushRetry = errors.New("notify: còn lần gửi đẩy lỗi tạm thời")

// HandlePush là inbox.Handler của pf.notify.push.requested: gửi từng thiết bị của lô.
//
// Kết quả từng lần gửi được ghi bằng giao dịch RIÊNG trên pool của module (không
// phải tx của inbox) để khi trả lỗi cho hàng đợi giao lại, các thiết bị đã gửi
// thành công không bị gửi lặp. Một thiết bị lỗi không chặn thiết bị khác
// (HTX US-NEWS-03); token bị nhà cung cấp báo hỏng → ngừng dùng, không thử lại.
func (s *Service) HandlePush(ctx context.Context, _ pgx.Tx, env outbox.Envelope) error {
	if env.EventType != port.EventPushRequested {
		return nil
	}
	var p port.PushRequestedV1
	if err := decode(env, &p); err != nil {
		return err
	}
	if s.Pusher == nil {
		return fmt.Errorf("notify: %w: chưa cấu hình kênh đẩy", ErrDependency)
	}
	var jobs []domain.DeliveryJob
	if err := tenant.WithTenantTx(ctx, s.DB, env.TenantID, func(tx pgx.Tx) error {
		var e error
		jobs, e = store.OpenDeliveries(ctx, tx, p.BatchID)
		return e
	}); err != nil {
		return err
	}
	retry := false
	for _, j := range jobs {
		status, code := s.sendOne(ctx, j)
		if status == domain.DeliveryFailed && j.Attempts+1 < domain.MaxDeliveryAttempts {
			retry = true
		}
		err := tenant.WithTenantTx(ctx, s.DB, env.TenantID, func(tx pgx.Tx) error {
			if status == domain.DeliveryDropped && code != nil && *code == "token_invalid" {
				if _, err := store.DisableDevice(ctx, tx, j.DeviceID, domain.TokenDisabledInvalid, s.now()); err != nil {
					return err
				}
			}
			return store.FinishDelivery(ctx, tx, j.ID, status, code, s.now())
		})
		if err != nil {
			return err
		}
	}
	if retry {
		return ErrPushRetry
	}
	return nil
}

func (s *Service) sendOne(ctx context.Context, j domain.DeliveryJob) (string, *string) {
	code := func(c string) *string { return &c }
	if j.Token == nil {
		return domain.DeliveryDropped, code("no_active_token")
	}
	err := s.Pusher.Send(ctx, domain.PushMessage{Token: j.Token.Token, Platform: j.Token.Platform, Kind: j.Kind,
		NotificationID: j.NotificationID, Urgent: j.Urgent, Entities: j.Entities})
	switch {
	case err == nil:
		return domain.DeliverySent, nil
	case errors.Is(err, channel.ErrTokenInvalid):
		return domain.DeliveryDropped, code("token_invalid")
	}
	s.Log.WarnContext(ctx, "gửi đẩy lỗi tạm thời", "event", "notify.push", "channel", s.Pusher.Name(), "kind", j.Kind,
		"delivery_id", j.ID.String(), "attempt", j.Attempts+1, "outcome", "retry")
	return domain.DeliveryFailed, code("temporary")
}
