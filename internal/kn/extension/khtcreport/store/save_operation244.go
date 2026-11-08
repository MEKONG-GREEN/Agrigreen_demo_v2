package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SaveOperation is written in the draft's transaction, never after an ACK.
type SaveOperation struct {
	OperationID   uuid.UUID
	PeriodID      uuid.UUID
	RequestKey    string
	AuthorityHash string
	Applied       domain.SavedDraft
}

func (v SaveOperation) Receipt() domain.SaveReceipt {
	applied := v.Applied
	return domain.SaveReceipt{OperationID: v.OperationID, PeriodID: v.PeriodID, Outcome: "applied", Applied: &applied}
}
func LockSaveOperation(ctx context.Context, tx pgx.Tx, tid, operation uuid.UUID, key string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('khtc_report_save_operation:'||$1::uuid::text||':'||$2::uuid::text,0))`, tid, operation)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('khtc_report_save_key:'||$1::uuid::text||':'||$2::text,0))`, tid, key)
	return err
}

const saveOperationCols = `operation_id,period_id,request_key,authority_hash,draft_id,base_draft_id,base_version,draft_version,request_hash,actor_ref,event_time,changes`

func scanSaveOperation(row pgx.Row) (*SaveOperation, error) {
	var v SaveOperation
	var changes []byte
	err := row.Scan(&v.OperationID, &v.PeriodID, &v.RequestKey, &v.AuthorityHash, &v.Applied.DraftID, &v.Applied.BaseDraftID, &v.Applied.BaseVersion, &v.Applied.Version, &v.Applied.RequestHash, &v.Applied.ActorRef, &v.Applied.At, &changes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(changes, &v.Applied.Changes); err != nil {
		return nil, err
	}
	return &v, nil
}
func SaveOperationByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*SaveOperation, error) {
	return scanSaveOperation(tx.QueryRow(ctx, `SELECT `+saveOperationCols+` FROM pf_extension.khtc_report_save_operation WHERE operation_id=$1 ORDER BY seq DESC LIMIT 1`, id))
}
func SaveOperationByKey(ctx context.Context, tx pgx.Tx, key string) (*SaveOperation, error) {
	return scanSaveOperation(tx.QueryRow(ctx, `SELECT `+saveOperationCols+` FROM pf_extension.khtc_report_save_operation WHERE request_key=$1 ORDER BY seq DESC LIMIT 1`, key))
}
func SaveOperations(ctx context.Context, tx pgx.Tx, period uuid.UUID) ([]SaveOperation, error) {
	rows, err := tx.Query(ctx, `SELECT `+saveOperationCols+` FROM pf_extension.khtc_report_save_operation WHERE period_id=$1 ORDER BY draft_version DESC LIMIT 50`, period)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SaveOperation, error) {
		v, e := scanSaveOperation(row)
		if e != nil {
			return SaveOperation{}, e
		}
		return *v, nil
	})
}
func InsertSaveOperation(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v SaveOperation) error {
	changes, err := json.Marshal(v.Applied.Changes)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_extension.khtc_report_save_operation
 (id,tenant_id,stream_key,operation_id,period_id,request_key,request_hash,authority_hash,draft_id,base_draft_id,base_version,draft_version,changes,actor_ref,event_time,idem_key,rule_version)
 VALUES($1,$2,'khtc_report_save_operation:'||$1::uuid::text,$1,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,'khtc:report:save-operation:'||$1::uuid::text,'report-save-receipt/v1')`,
		v.OperationID, tid, v.PeriodID, v.RequestKey, v.Applied.RequestHash, v.AuthorityHash, v.Applied.DraftID, v.Applied.BaseDraftID, v.Applied.BaseVersion, v.Applied.Version, string(changes), v.Applied.ActorRef, v.Applied.At)
	return err
}
