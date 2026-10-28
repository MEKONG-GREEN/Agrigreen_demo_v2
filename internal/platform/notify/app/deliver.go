package app

import (
	"context"
	"errors"
	"fmt"

	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/channel"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

// ErrPushRetry: còn lần gửi lỗi tạm thời — trả cho inbox để hàng đợi giao lại (tối
// đa shared/inbox.DeliveryLimit lần rồi vào DLQ).
var ErrPushRetry = errors.New("notify: còn lần gửi đẩy lỗi tạm thời")

// HandlePush là inbox.Handler của pf.notify.push.requested: gửi từng thiết bị của lô.
//
// BF-NOTIFY-POOL (#155): đọc lô, gửi FCM và ghi kết quả chạy NGOÀI giao dịch inbox (txdeps.Get → runDeliveries, hợp đồng
// #143) — trước đây handler giữ giao dịch inbox rồi chờ kết nối thứ hai trên cùng pool. Mỗi lần gửi: nhận (giao dịch
// ngắn) → gửi → kết (giao dịch ngắn), nên khi trả lỗi cho hàng đợi giao lại, các thiết bị đã gửi thành công không bị gửi
// lặp. Một thiết bị lỗi không chặn thiết bị khác (HTX US-NEWS-03); token bị nhà cung cấp báo hỏng → ngừng dùng, không
// thử lại. Thân giao dịch inbox chỉ ghi nhận kết quả (inbox cùng commit, R-61).
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
	tid, batch := env.TenantID, p.BatchID
	res, err := txdeps.Get(ctx, "notify.push:"+env.EventID.String(), func(ctx context.Context) (deliveryOutcome, error) {
		return runDeliveries(ctx, s, tid, s.pushLoop(batch))
	})
	if err != nil {
		return err
	}
	if res.retry {
		return ErrPushRetry
	}
	return nil
}

// pushLoop là các bước nhận–gửi–kết của kênh đẩy cho lô batch.
func (s *Service) pushLoop(batch uuid.UUID) sendLoop[domain.DeliveryJob] {
	return sendLoop[domain.DeliveryJob]{
		name: "push",
		list: func(ctx context.Context, tx pgx.Tx) ([]domain.DeliveryJob, error) {
			return store.OpenDeliveries(ctx, tx, batch)
		},
		id:    func(j domain.DeliveryJob) uuid.UUID { return j.ID },
		claim: store.ClaimDelivery,
		send:  s.sendOne,
		finish: func(ctx context.Context, tx pgx.Tx, j domain.DeliveryJob, token uuid.UUID, status string, code *string, now time.Time) (bool, error) {
			ok, err := store.FinishDelivery(ctx, tx, j.ID, token, status, code, now)
			if err != nil || !ok {
				return ok, err
			}
			if status == domain.DeliveryDropped && code != nil && *code == "token_invalid" {
				if _, err := store.DisableDevice(ctx, tx, j.DeviceID, domain.TokenDisabledInvalid, now); err != nil {
					return false, err
				}
			}
			return true, nil
		},
		release: store.ReleaseDelivery,
		open:    store.DeliveryOpen,
	}
}

func (s *Service) sendOne(ctx context.Context, j domain.DeliveryJob) (string, *string) {
	code := func(c string) *string { return &c }
	if j.Token == nil {
		return domain.DeliveryDropped, code("no_active_token")
	}
	// P0-NOTIFY-FAILCLOSED: kênh đẩy tắt (chưa cấu hình FCM) → bỏ, ghi lý do; không thử lại, không bao giờ "sent".
	if reason, off := channel.DisabledReason(s.Pusher); off {
		s.Log.WarnContext(ctx, "kênh đẩy chưa cấu hình — không gửi", "event", "notify.push", "kind", j.Kind,
			"delivery_id", j.ID.String(), "reason", reason, "outcome", "dropped")
		return domain.DeliveryDropped, code(channel.DisabledCode(reason))
	}
	err := s.Pusher.Send(ctx, domain.PushMessage{Token: j.Token.Token, Platform: j.Token.Platform, Kind: j.Kind,
		NotificationID: j.NotificationID, Urgent: j.Urgent, Entities: j.Entities})
	switch {
	case err == nil:
		return domain.DeliverySent, nil
	case errors.Is(err, channel.ErrTokenInvalid):
		return domain.DeliveryDropped, code("token_invalid")
	case errors.Is(err, channel.ErrChannelDisabled):
		return domain.DeliveryDropped, code(channel.CodeChannelDisabled)
	}
	s.Log.WarnContext(ctx, "gửi đẩy lỗi tạm thời", "event", "notify.push", "channel", s.Pusher.Name(), "kind", j.Kind,
		"delivery_id", j.ID.String(), "attempt", j.Attempts+1, "outcome", "retry")
	return domain.DeliveryFailed, code("temporary")
}
