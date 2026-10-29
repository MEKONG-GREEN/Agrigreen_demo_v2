package app

// BF-NOTIFY-POOL (#155): gửi đẩy/email theo lô KHÔNG giữ giao dịch inbox (và không chờ kết nối thứ hai) trong lúc gọi ra
// ngoài. Consumer chạy vòng nhận–gửi–kết bên trong txdeps.Get (ngoài giao dịch inbox, cùng hợp đồng P0-POOL-SHARED-2 #143):
// mỗi lần gửi: giao dịch ngắn NHẬN (claim_id/claim_until) → gửi FCM / tra email + SMTP không giữ kết nối nào → giao dịch
// ngắn KẾT (kết quả + xóa nhận, chỉ khi nhận còn là của mình). Lần gửi đã có kết quả không gửi lại khi thông điệp được giao
// lại; giao lặp đồng thời chờ lượt đang giữ xong (không gửi song song); tiến trình chết → hết hạn giữ thì nhận lại.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DefaultDeliveryLease là hạn giữ một lần gửi: ngắn hơn hạn xử lý một thông điệp của consumer (inbox, 2 phút) để giao
// lại sau khi worker giữ chết kịp nhận lại; lời gọi ra ngoài dừng trước hạn giữ một biên (deliverySendMargin).
const DefaultDeliveryLease = time.Minute

// deliverySendMargin: lời gọi ra ngoài hết hạn sớm hơn hạn giữ một phần tư — worker khác chỉ nhận lại khi lượt này đã dừng.
func deliverySendMargin(lease time.Duration) time.Duration { return lease / 4 }

// deliveryPoll là chu kỳ chờ khi worker khác đang giữ lần gửi còn hạn.
const deliveryPoll = 500 * time.Millisecond

// deliveryWriteTimeout: hạn của giao dịch KẾT/TRẢ nhận (không theo ctx của lượt — lượt bị hủy vẫn ghi được kết quả).
const deliveryWriteTimeout = 30 * time.Second

// deliveryOutcome là kết quả vòng gửi của một lô (ngoài giao dịch inbox): còn lần lỗi tạm thời → thông điệp giao lại.
type deliveryOutcome struct{ retry bool }

// sendLoop mô tả bước của một kênh (đẩy / email) cho vòng nhận–gửi–kết.
type sendLoop[J any] struct {
	name    string
	list    func(ctx context.Context, tx pgx.Tx) ([]J, error)
	id      func(J) uuid.UUID
	claim   func(ctx context.Context, tx pgx.Tx, id, token uuid.UUID, now, until time.Time) (int, bool, error)
	send    func(ctx context.Context, j J) (string, *string)
	finish  func(ctx context.Context, tx pgx.Tx, j J, token uuid.UUID, status string, code *string, now time.Time) (bool, error)
	release func(ctx context.Context, tx pgx.Tx, id, token uuid.UUID) error
	// open: lần gửi còn mở (pending/failed còn lượt) — đọc lại sau khi mất nhận.
	open func(ctx context.Context, tx pgx.Tx, id uuid.UUID) (bool, error)
}

// runDeliveries gửi mọi lần gửi còn mở của lô; mỗi lần gửi tối đa một lần thử trong một lượt. Lần gửi đang do worker khác
// giữ còn hạn → chờ (poll) tới khi xong hoặc hết hạn giữ; ctx hết → lỗi (thông điệp giao lại).
func runDeliveries[J any](ctx context.Context, s *Service, tid uuid.UUID, l sendLoop[J]) (deliveryOutcome, error) {
	var out deliveryOutcome
	handled := map[uuid.UUID]bool{}
	for {
		var jobs []J
		if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
			var e error
			jobs, e = l.list(ctx, tx)
			return e
		}); err != nil {
			return out, err
		}
		held := false
		for _, j := range jobs {
			if handled[l.id(j)] {
				continue
			}
			retry, claimed, err := claimAndSend(ctx, s, tid, l, j)
			if err != nil {
				return out, err
			}
			if !claimed {
				held = true
				continue
			}
			handled[l.id(j)] = true
			out.retry = out.retry || retry
		}
		if !held {
			return out, nil
		}
		select {
		case <-ctx.Done():
			return out, fmt.Errorf("notify: %s: lần gửi đang do worker khác giữ: %w", l.name, ctx.Err())
		case <-time.After(deliveryPoll):
		}
	}
}

// claimAndSend nhận MỘT lần gửi (giao dịch riêng), gửi không giữ kết nối, rồi ghi kết quả (giao dịch riêng).
// claimed=false: lần gửi đã xong hoặc đang do worker khác giữ. retry=true: lần gửi còn mở sau lượt này (lỗi tạm thời
// còn lượt, hoặc mất nhận mà lần gửi chưa kết thúc) — thông điệp phải được giao lại, không ACK.
func claimAndSend[J any](ctx context.Context, s *Service, tid uuid.UUID, l sendLoop[J], j J) (retry, claimed bool, err error) {
	lease := DefaultDeliveryLease
	// Hạn lời gọi ra ngoài tính từ TRƯỚC giao dịch nhận (cùng mốc với claim_until = now + lease) — chờ kết nối lâu vẫn
	// không đẩy lời gọi quá hạn giữ (cùng bài học review #143 C2).
	sendBy := time.Now().Add(lease - deliverySendMargin(lease))
	token, now := ids.New(), s.now()
	var attempts int
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		attempts, claimed, e = l.claim(ctx, tx, l.id(j), token, now, now.Add(lease))
		return e
	}); err != nil || !claimed {
		return false, false, err
	}
	sctx, cancel := context.WithDeadline(ctx, sendBy)
	status, code := l.send(sctx, j)
	cancel()
	wctx, wcancel := context.WithTimeout(context.WithoutCancel(ctx), deliveryWriteTimeout)
	defer wcancel()
	if ctx.Err() != nil && status == domain.DeliveryFailed {
		// Lượt bị hủy (hết hạn xử lý thông điệp / dừng tiến trình) giữa lúc gửi: trả nhận, không tính lần thử.
		rerr := tenant.WithTenantTx(wctx, s.DB, tid, func(tx pgx.Tx) error { return l.release(wctx, tx, l.id(j), token) })
		return false, true, errors.Join(ctx.Err(), rerr)
	}
	var ok bool
	if err := tenant.WithTenantTx(wctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		ok, e = l.finish(wctx, tx, j, token, status, code, s.now())
		return e
	}); err != nil {
		return false, true, err
	}
	if !ok {
		// Mất nhận (hết hạn giữ, worker khác đã nhận lại): KHÔNG coi là xong (review #155 P1). Đọc lại trạng thái bền vững —
		// lần gửi đã kết thúc (sent/dropped/hết lượt) → xong; còn mở (worker kia có thể đã chết trước khi gửi/ghi) → báo
		// thử lại để inbox KHÔNG ACK và thông điệp được giao lại; lượt giao lại nhận lại khi hạn giữ của worker kia hết.
		var open bool
		if err := tenant.WithTenantTx(wctx, s.DB, tid, func(tx pgx.Tx) error {
			var e error
			open, e = l.open(wctx, tx, l.id(j))
			return e
		}); err != nil {
			return false, true, err
		}
		outcome := "claim_lost_done"
		if open {
			outcome = "claim_lost_retry"
		}
		s.Log.WarnContext(ctx, "không ghi kết quả gửi: nhận việc đã thuộc worker khác", "event", "notify."+l.name,
			"delivery_id", l.id(j).String(), "outcome", outcome)
		return open, true, nil
	}
	return status == domain.DeliveryFailed && attempts+1 < domain.MaxDeliveryAttempts, true, nil
}
