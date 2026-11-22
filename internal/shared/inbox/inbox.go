// Package inbox bảo đảm mỗi consumer xử lý một sự kiện đúng một lần về hiệu
// quả (R-61), dù hàng đợi giao lặp.
//
// Cách làm: trong cùng giao dịch với kết quả xử lý, INSERT (event_id, consumer)
// vào shared.inbox. Đã có → bỏ qua. ACK chỉ sau commit.
package inbox

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

// Handler xử lý một sự kiện trong giao dịch tenant đã mở. Handler thường ghi
// kết quả và Enqueue sự kiện tiếp theo trên cùng tx.
//
// P0-POOL-SHARED-2 (#143): ctx của Handler mang bộ nhớ txdeps (niêm phong khi đang trong giao dịch). I/O ngoài (port
// module khác tự mở giao dịch trên cùng pool, kho đối tượng, SMTP, HTTP) KHÔNG gọi trực tiếp trong tx mà qua
// txdeps.Get(ctx, khóa, load): lần đầu Get trả txdeps.ErrNeed → Process hủy giao dịch (inbox chưa ghi), chạy load NGOÀI
// giao dịch, rồi chạy lại Handler trong giao dịch MỚI — lần này Get trả kết quả đã nhớ và Handler chỉ ghi kết quả + inbox +
// outbox cùng commit (R-61). Handler có thể chạy nhiều lần cho một thông điệp: đặt lại mọi biến ngoài ở đầu, không tác
// dụng phụ ngoài DB ngoài load của Get.
type Handler func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error

// MaxTxAttempts: số lần chạy thân giao dịch inbox tối đa cho một thông điệp (lần đầu thường thiếu dữ liệu ngoài; handler
// có nhiều tầng đọc phụ thuộc nhau như cảnh báo CNTS cần vài lần). Quá → txdeps.ErrExhausted, thông điệp giao lại.
const MaxTxAttempts = 10

// Process chạy h đúng một lần (về hiệu quả) cho (event_id, consumer). Trả processed=false nếu
// sự kiện đã được consumer này xử lý trước đó.
func Process(ctx context.Context, db tenant.Beginner, consumer string, env outbox.Envelope, h Handler) (processed bool, err error) {
	if consumer == "" {
		return false, errors.New("inbox: thiếu tên consumer")
	}
	// Bộ nhớ txdeps MỚI cho mỗi thông điệp; ngữ cảnh mang tenant của phong bì cả khi nạp ngoài giao dịch.
	// Handler chạy trong giao dịch của tenant env.TenantID: ngữ cảnh mang cùng tenant để cổng của module khác
	// (tham số masterdata, tổng hợp KN…) đọc được (KN P8: consumer gửi Chi cục lỗi "tenant: thiếu tenant_id").
	hctx := tenant.WithID(txdeps.With(ctx), env.TenantID)
	err = txdeps.RunMax(hctx, MaxTxAttempts, func(body func(pgx.Tx) error) error {
		return tenant.WithTenantTx(ctx, db, env.TenantID, body)
	}, func(tx pgx.Tx) error {
		processed = false
		tag, err := tx.Exec(ctx, `INSERT INTO shared.inbox (event_id, consumer, tenant_id, event_type)
		  VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, env.EventID, consumer, env.TenantID, env.EventType)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil // đã xử lý
		}
		processed = true
		return h(hctx, tx, env)
	})
	if err != nil {
		processed = false
	}
	return processed, err
}
