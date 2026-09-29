package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
)

// ValveExport là một dòng sổ xuất của van công bố (mkg_hub.valve_export).
type ValveExport struct {
	ID              uuid.UUID // = event_id trên shared.outbox
	TenantID        uuid.UUID
	Seq             int64
	Contract        string
	ContractVersion int
	EventType       string
	SchemaVersion   int
	SubjectRef      uuid.UUID
	Payload         json.RawMessage // ĐÚNG byte đã kiểm theo hợp đồng
	EventTime       time.Time
	RecordedAt      time.Time
	ActorRef        uuid.UUID
	IdemKey         string
}

// ValveStream là khóa chuỗi băm: thứ tự theo hợp đồng và hub.
func ValveStream(contract string, subject uuid.UUID) string {
	return "valve:" + contract + ":" + subject.String()
}

// InsertValveExport ghi sổ xuất kèm băm SHA-256 của payload.
func InsertValveExport(ctx context.Context, tx pgx.Tx, v *ValveExport) error {
	sum := sha256.Sum256(v.Payload)
	return tx.QueryRow(ctx, `INSERT INTO mkg_hub.valve_export
	    (id, tenant_id, stream_key, contract, contract_version, event_type, schema_version, subject_ref, payload,
	     payload_sha256, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12, $13, $14) RETURNING seq, recorded_at`,
		v.ID, v.TenantID, ValveStream(v.Contract, v.SubjectRef), v.Contract, v.ContractVersion, v.EventType, v.SchemaVersion,
		v.SubjectRef, string(v.Payload), sum[:], v.EventTime.UTC(), v.ActorRef, v.IdemKey, domain.RuleValve).Scan(&v.Seq, &v.RecordedAt)
}

const valveCols = `id, tenant_id, seq, contract, contract_version, event_type, schema_version, subject_ref, payload::text,
  event_time, recorded_at, actor_ref, idem_key`

func scanValve(row pgx.Row) (ValveExport, error) {
	var v ValveExport
	var payload string
	err := row.Scan(&v.ID, &v.TenantID, &v.Seq, &v.Contract, &v.ContractVersion, &v.EventType, &v.SchemaVersion, &v.SubjectRef,
		&payload, &v.EventTime, &v.RecordedAt, &v.ActorRef, &v.IdemKey)
	v.Payload = json.RawMessage(payload)
	return v, err
}

// ValveByIdem tìm bản ghi xuất với idem_key.
func ValveByIdem(ctx context.Context, tx pgx.Tx, key string) (*ValveExport, error) {
	v, err := scanValve(tx.QueryRow(ctx, "SELECT "+valveCols+" FROM mkg_hub.valve_export WHERE idem_key = $1", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ValveExports liệt kê bản ghi xuất của một hub theo hợp đồng, mới nhất trước.
func ValveExports(ctx context.Context, tx pgx.Tx, contract string, subject uuid.UUID, limit int) ([]ValveExport, error) {
	rows, err := tx.Query(ctx, "SELECT "+valveCols+` FROM mkg_hub.valve_export
	    WHERE contract = $1 AND subject_ref = $2 ORDER BY seq DESC LIMIT $3`, contract, subject, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ValveExport
	for rows.Next() {
		v, err := scanValve(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
