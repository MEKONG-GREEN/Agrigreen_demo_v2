package app

// Email theo từng người nhận (gói NOTIF): thông báo cá nhân có cờ Email → một dòng pf_notify.email_delivery cùng giao
// dịch với thông báo, phát pf.notify.email.requested; consumer pf.notify.email tra địa chỉ (port.EmailDirectory —
// pf/accounts/Keycloak) lúc gửi, gửi qua channel.Mailer (SMTP trong lãnh thổ; chỉ máy phát triển với AG_MAILER=log: LogMailer; chưa cấu hình: kênh tắt →
// bản gửi "dropped" mã channel_disabled:<lý do>). Không lưu địa chỉ,
// log chỉ ghi địa chỉ đã che. Idempotent: thông báo trùng không được ghi (chỉ mục sự kiện × người nhận), mỗi thông báo
// tối đa một lần gửi (UNIQUE), lần đã gửi không gửi lại khi lô được giao lại.

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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

// ErrEmailRetry: còn lần gửi email lỗi tạm thời — trả cho inbox để hàng đợi giao lại (tối đa MaxDeliveryAttempts lần).
var ErrEmailRetry = errors.New("notify: còn lần gửi email lỗi tạm thời")

// emailBatch gom lần gửi email của một sự kiện nguồn.
type emailBatch struct {
	id    uuid.UUID
	kind  string
	count int
}

// personalMailer là kênh gửi email theo người nhận (EmailMailer, không có → Mailer).
func (s *Service) personalMailer() channel.Mailer {
	if s.EmailMailer != nil {
		return s.EmailMailer
	}
	return s.Mailer
}

// emailEnabled checks queue/consumer dependencies, not transport availability.
// A non-nil disabled mailer must still reach the consumer's dropped audit (#127).
func (s *Service) emailEnabled() bool { return s.personalMailer() != nil && s.Emails != nil }

// queueEmail xếp email của thông báo n vừa ghi (cá nhân, cờ Email). Kênh email chưa cấu hình → bỏ qua (chỉ trong ứng dụng).
func (s *Service) queueEmail(ctx context.Context, tx pgx.Tx, tid uuid.UUID, b *emailBatch, n domain.Notification, now time.Time) error {
	if !n.Email || n.RecipientRef == nil || !s.emailEnabled() {
		return nil
	}
	ok, err := store.InsertEmailDelivery(ctx, tx, tid, ids.New(), b.id, n.ID, *n.RecipientRef, n.Kind, now)
	if err != nil || !ok {
		return err
	}
	b.kind = n.Kind
	b.count++
	return nil
}

// flushEmail phát pf.notify.email.requested cho lô (nếu có).
func (s *Service) flushEmail(ctx context.Context, tx pgx.Tx, tid uuid.UUID, b emailBatch, now time.Time, trace string) error {
	if b.count == 0 {
		return nil
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: port.EventEmailRequested, SchemaVersion: 1,
		OccurredAt: now, TraceID: trace, Producer: port.EventProducerName,
		Payload: port.EmailRequestedV1{BatchID: b.id, Kind: b.kind, Deliveries: b.count}})
}

// HandleEmail là inbox.Handler của pf.notify.email.requested: gửi từng email của lô. Như HandlePush (BF-NOTIFY-POOL
// #155): đọc lô, tra địa chỉ (pf/accounts), gửi SMTP và ghi kết quả chạy NGOÀI giao dịch inbox, mỗi lần gửi nhận → gửi →
// kết bằng giao dịch ngắn riêng, nên khi hàng đợi giao lại, email đã gửi không gửi lặp. Không có email → dropped
// (no_email), không thử lại; lỗi tra cứu / máy chủ thư → failed, thử lại tới MaxDeliveryAttempts.
func (s *Service) HandleEmail(ctx context.Context, _ pgx.Tx, env outbox.Envelope) error {
	if env.EventType != port.EventEmailRequested {
		return nil
	}
	var p port.EmailRequestedV1
	if err := decode(env, &p); err != nil {
		return err
	}
	if !s.emailEnabled() {
		return fmt.Errorf("notify: %w: chưa cấu hình kênh email / danh bạ thư", ErrDependency)
	}
	tid, batch := env.TenantID, p.BatchID
	res, err := txdeps.Get(ctx, "notify.email:"+env.EventID.String(), func(ctx context.Context) (deliveryOutcome, error) {
		return runDeliveries(ctx, s, tid, sendLoop[domain.EmailJob]{
			name: "email",
			list: func(ctx context.Context, tx pgx.Tx) ([]domain.EmailJob, error) {
				return store.OpenEmailDeliveries(ctx, tx, batch)
			},
			id:    func(j domain.EmailJob) uuid.UUID { return j.ID },
			claim: store.ClaimEmailDelivery,
			send:  func(ctx context.Context, j domain.EmailJob) (string, *string) { return s.sendEmail(ctx, tid, j) },
			finish: func(ctx context.Context, tx pgx.Tx, j domain.EmailJob, token uuid.UUID, status string, code *string, now time.Time) (bool, error) {
				return store.FinishEmailDelivery(ctx, tx, j.ID, token, status, code, now)
			},
			release: store.ReleaseEmailDelivery,
			open:    store.EmailDeliveryOpen,
		})
	})
	if err != nil {
		return err
	}
	if res.retry {
		return ErrEmailRetry
	}
	return nil
}

func (s *Service) sendEmail(ctx context.Context, tid uuid.UUID, j domain.EmailJob) (string, *string) {
	code := func(c string) *string { return &c }
	// P0-NOTIFY-FAILCLOSED: kênh email tắt (chưa cấu hình SMTP / URL liên kết) → bỏ, ghi lý do; không tra địa chỉ, không
	// thử lại, không bao giờ "sent".
	if reason, off := channel.DisabledReason(s.personalMailer()); off {
		s.Log.WarnContext(ctx, "kênh email chưa cấu hình — không gửi, chỉ trong ứng dụng", "event", "notify.email", "kind", j.Kind,
			"delivery_id", j.ID.String(), "reason", reason, "outcome", "dropped")
		return domain.DeliveryDropped, code(channel.DisabledCode(reason))
	}
	addr, err := s.Emails.EmailOf(ctx, tid, j.RecipientRef)
	switch {
	case errors.Is(err, port.ErrNoEmail) || (err == nil && addr == ""):
		s.Log.InfoContext(ctx, "người nhận chưa có email — chỉ gửi trong ứng dụng", "event", "notify.email", "kind", j.Kind,
			"delivery_id", j.ID.String(), "outcome", "dropped")
		return domain.DeliveryDropped, code(domain.EmailErrNoAddress)
	case err != nil:
		s.Log.WarnContext(ctx, "tra email người nhận lỗi tạm thời", "event", "notify.email", "kind", j.Kind,
			"delivery_id", j.ID.String(), "attempt", j.Attempts+1, "outcome", "retry")
		return domain.DeliveryFailed, code(domain.EmailErrLookup)
	}
	subject, body := domain.EmailMessage(j, s.LinkBase)
	if err := s.personalMailer().Send(ctx, []string{addr}, subject, body); errors.Is(err, channel.ErrChannelDisabled) {
		return domain.DeliveryDropped, code(channel.CodeChannelDisabled)
	} else if err != nil {
		s.Log.WarnContext(ctx, "gửi email lỗi tạm thời", "event", "notify.email", "kind", j.Kind, "to", domain.MaskEmail(addr),
			"delivery_id", j.ID.String(), "attempt", j.Attempts+1, "outcome", "retry")
		return domain.DeliveryFailed, code(domain.EmailErrSend)
	}
	s.Log.InfoContext(ctx, "đã gửi email thông báo", "event", "notify.email", "kind", j.Kind, "to", domain.MaskEmail(addr),
		"delivery_id", j.ID.String(), "outcome", "sent")
	return domain.DeliverySent, nil
}
