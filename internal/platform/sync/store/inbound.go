package store

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
)

// Inbound là một dòng sổ nhận bản ghi chiều lên. Không có payload — chỉ băm.
type Inbound struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	DeviceID           uuid.UUID
	BatchKey           string
	Index              int
	RecordID           *uuid.UUID
	RecordIdemKey      string
	Entity             string
	Op                 string
	Status             string
	Code               *string
	ConflictID         *uuid.UUID
	RecordSupersedesID *uuid.UUID
	PayloadSHA256      *string
	EventTime          time.Time
	ActorRef           uuid.UUID
}

// StreamKey của sổ inbound: một chuỗi băm mỗi thiết bị.
func StreamKey(device uuid.UUID) string { return "device:" + device.String() }

// LedgerIdemKey là idem_key của dòng sổ: duy nhất theo (lô, vị trí).
func LedgerIdemKey(batch string, index int) string {
	return "in:" + batch + ":" + strconv.Itoa(index)
}

// InsertInbound ghi một dòng sổ; DB tính seq và chuỗi băm. Trùng idem_key của
// sổ (lô đã ghi ở lần chạy trước) → lỗi ledger_idem_pkey, gọi ledger.Classify.
func InsertInbound(ctx context.Context, tx pgx.Tx, r Inbound) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_sync.inbound
	    (id, tenant_id, stream_key, batch_key, record_index, record_id, record_idem_key, entity, op, status, code,
	     conflict_id, record_supersedes_id, payload_sha256, event_time, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`,
		r.ID, r.TenantID, StreamKey(r.DeviceID), r.BatchKey, r.Index, r.RecordID,
		domain.Truncate(r.RecordIdemKey, domain.MaxIdemKeyLen), domain.Truncate(r.Entity, 40), domain.Truncate(r.Op, 20),
		r.Status, r.Code, r.ConflictID, r.RecordSupersedesID, r.PayloadSHA256, r.EventTime.UTC(), r.ActorRef,
		r.DeviceID.String(), LedgerIdemKey(r.BatchKey, r.Index), domain.RuleVersion)
	return err
}

// WrittenKeys trả tập idem_key (do máy sinh) trong keys đã được một Sink ghi
// nhận (accepted, duplicate hoặc conflict) ở lần gửi trước.
func WrittenKeys(ctx context.Context, tx pgx.Tx, keys []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT record_idem_key FROM pf_sync.inbound
	  WHERE record_idem_key = ANY($1::text[]) AND status IN ($2, $3, $4)`,
		keys, port.StatusAccepted, port.StatusDuplicate, port.StatusConflict)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// InboundByDevice đọc sổ của một thiết bị theo seq (kiểm toán, kiểm thử).
func InboundByDevice(ctx context.Context, tx pgx.Tx, device uuid.UUID) ([]Inbound, error) {
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, batch_key, record_index, record_id, record_idem_key, entity, op,
	    status, code, conflict_id, record_supersedes_id, payload_sha256, event_time, actor_ref
	  FROM pf_sync.inbound WHERE stream_key = $1 ORDER BY seq`, StreamKey(device))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Inbound
	for rows.Next() {
		r := Inbound{DeviceID: device}
		if err := rows.Scan(&r.ID, &r.TenantID, &r.BatchKey, &r.Index, &r.RecordID, &r.RecordIdemKey, &r.Entity, &r.Op,
			&r.Status, &r.Code, &r.ConflictID, &r.RecordSupersedesID, &r.PayloadSHA256, &r.EventTime, &r.ActorRef); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
