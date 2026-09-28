package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// InboundByConflict đọc mọi dòng sổ nhận gắn với một xung đột (các bản ghi máy
// gửi lên của cùng xung đột), theo thời điểm ghi nhận. Không có payload.
func InboundByConflict(ctx context.Context, tx pgx.Tx, conflict uuid.UUID) ([]Inbound, error) {
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, batch_key, record_index, record_id, record_idem_key, entity, op,
	    status, code, conflict_id, record_supersedes_id, payload_sha256, event_time, actor_ref, device_id
	  FROM pf_sync.inbound WHERE conflict_id = $1 ORDER BY recorded_at, seq`, conflict)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Inbound
	for rows.Next() {
		var r Inbound
		var dev string
		if err := rows.Scan(&r.ID, &r.TenantID, &r.BatchKey, &r.Index, &r.RecordID, &r.RecordIdemKey, &r.Entity, &r.Op,
			&r.Status, &r.Code, &r.ConflictID, &r.RecordSupersedesID, &r.PayloadSHA256, &r.EventTime, &r.ActorRef, &dev); err != nil {
			return nil, err
		}
		r.DeviceID, _ = uuid.Parse(dev)
		out = append(out, r)
	}
	return out, rows.Err()
}
