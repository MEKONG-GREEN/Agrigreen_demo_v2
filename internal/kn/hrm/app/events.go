package app

// NOTIFY-HCTH-KN (#50): sự kiện outbox của pf/hrm (port.Event*). Mọi sự kiện ghi trong CÙNG giao dịch với dòng sổ gây ra nó
// (R-61): giao dịch hủy → không có sự kiện; outbox-publisher và consumer pf.notify.hrm thử lại tới khi giao được (consumer
// idempotent theo event_id). Payload chỉ số đếm, mã kỳ/đơn vị, mã tham chiếu — không PII.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// enqueueEvent ghi một sự kiện của pf/hrm vào outbox trong tx.
func enqueueEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, at time.Time, typ string, payload any) error {
	return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: typ, SchemaVersion: 1, OccurredAt: at.UTC(),
		TraceID: telemetry.TraceID(ctx), Producer: port.EventProducerName, Payload: payload})
}
