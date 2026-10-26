package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ChannelCounts đếm lần gửi của một kênh (đẩy/email) của tenant từ một mốc — cho GET /v1/notify/delivery-health
// (P0-NOTIFY-FAILCLOSED). Disabled là số bản gửi "dropped" vì kênh tắt (last_error channel_disabled:<lý do>).
// LastDisabledAt là thời điểm THỬ của lần bỏ vì kênh tắt gần nhất (finished_at, ghi cùng Finish*Delivery) để so được
// với LastSentAt (sent_at thật) — không dùng created_at (lúc vào hàng đợi; lô cũ có thể được thử muộn sau khi kênh tắt).
type ChannelCounts struct {
	Sent, Failed, Dropped, Pending, Disabled int
	LastDisabledCode                         *string
	LastDisabledAt, LastSentAt               *time.Time
}

// Bảng lần gửi theo kênh (hằng — không nhận tên bảng từ bên ngoài).
const (
	TablePushDelivery  = "pf_notify.push_delivery"
	TableEmailDelivery = "pf_notify.email_delivery"
)

const (
	healthCountsHead = `SELECT
	    count(*) FILTER (WHERE status = 'sent'),
	    count(*) FILTER (WHERE status = 'failed'),
	    count(*) FILTER (WHERE status = 'dropped'),
	    count(*) FILTER (WHERE status = 'pending'),
	    count(*) FILTER (WHERE status = 'dropped' AND last_error LIKE 'channel\_disabled%'),
	    max(sent_at)
	  FROM `
	// Mốc của một bản gửi: lần thử/kết quả gần nhất; bản chưa thử (pending) hoặc dòng cũ trước cột finished_at → created_at.
	healthCountsTail = ` WHERE coalesce(finished_at, created_at) >= $1`
	healthLastHead   = `SELECT last_error, coalesce(finished_at, created_at) FROM `
	healthLastTail   = ` WHERE coalesce(finished_at, created_at) >= $1 AND status = 'dropped' AND last_error LIKE 'channel\_disabled%'
	  ORDER BY coalesce(finished_at, created_at) DESC, id DESC LIMIT 1`
)

// healthSQL: câu truy vấn theo bảng (hằng, ghép lúc biên dịch).
var healthSQL = map[string][2]string{
	TablePushDelivery:  {healthCountsHead + TablePushDelivery + healthCountsTail, healthLastHead + TablePushDelivery + healthLastTail},
	TableEmailDelivery: {healthCountsHead + TableEmailDelivery + healthCountsTail, healthLastHead + TableEmailDelivery + healthLastTail},
}

// DeliveryHealth đếm lần gửi của bảng table (TablePushDelivery|TableEmailDelivery) có lần thử (hoặc tạo, nếu chưa thử)
// từ since, theo RLS tenant.
func DeliveryHealth(ctx context.Context, tx pgx.Tx, table string, since time.Time) (ChannelCounts, error) {
	q, ok := healthSQL[table]
	if !ok {
		return ChannelCounts{}, fmt.Errorf("store: bảng lần gửi không hợp lệ %q", table)
	}
	var c ChannelCounts
	if err := tx.QueryRow(ctx, q[0], since.UTC()).Scan(&c.Sent, &c.Failed, &c.Dropped, &c.Pending, &c.Disabled, &c.LastSentAt); err != nil {
		return ChannelCounts{}, err
	}
	if c.Disabled == 0 {
		return c, nil
	}
	var code string
	var at time.Time
	if err := tx.QueryRow(ctx, q[1], since.UTC()).Scan(&code, &at); err != nil {
		return ChannelCounts{}, err
	}
	c.LastDisabledCode, c.LastDisabledAt = &code, &at
	return c, nil
}
