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
)

// Handler xử lý một sự kiện trong giao dịch tenant đã mở. Handler thường ghi
// kết quả và Enqueue sự kiện tiếp theo trên cùng tx.
type Handler func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error

// Process chạy h đúng một lần cho (event_id, consumer). Trả processed=false nếu
// sự kiện đã được consumer này xử lý trước đó.
func Process(ctx context.Context, db tenant.Beginner, consumer string, env outbox.Envelope, h Handler) (processed bool, err error) {
	if consumer == "" {
		return false, errors.New("inbox: thiếu tên consumer")
	}
	err = tenant.WithTenantTx(ctx, db, env.TenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO shared.inbox (event_id, consumer, tenant_id, event_type)
		  VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, env.EventID, consumer, env.TenantID, env.EventType)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil // đã xử lý
		}
		processed = true
		// Handler chạy trong giao dịch của tenant env.TenantID: ngữ cảnh mang cùng tenant để cổng của module khác
		// (tham số masterdata, tổng hợp KN…) đọc được (KN P8: consumer gửi Chi cục lỗi "tenant: thiếu tenant_id").
		return h(tenant.WithID(ctx, env.TenantID), tx, env)
	})
	if err != nil {
		processed = false
	}
	return processed, err
}
