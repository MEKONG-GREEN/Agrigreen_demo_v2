package store

import (
	"context"
	"encoding/json"
	"errors"
	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AdhocRequest struct {
	Indicators                                           []pd.AdhocIndicator
	ID, PeriodID, ActorRef                               uuid.UUID
	Department, Instruction, PayloadHash, IdemKey, State string
	DueAt, At                                            time.Time
	BaseAttempt, Seq                                     int64
	NativeSubmissionRef                                  *uuid.UUID
	ReturnApplied                                        *bool
}

const adhocSelect = `SELECT r.id,r.period_id,r.department,r.instruction,r.indicators::text,r.due_at,r.payload_hash,r.idem_key,r.actor_ref,r.event_time,r.base_attempt,r.seq,
 coalesce(e.action,'intent'),e.native_submission_ref,e.return_applied
 FROM pf_extension.khtc_adhoc_request r
 LEFT JOIN LATERAL(SELECT x.action,x.native_submission_ref,x.return_applied FROM pf_extension.khtc_adhoc_request_event x
 WHERE x.tenant_id=r.tenant_id AND x.request_id=r.id ORDER BY x.seq DESC LIMIT 1)e ON true `

func scanAdhocRequest(row pgx.Row) (*AdhocRequest, error) {
	var r AdhocRequest
	var definitions string
	err := row.Scan(&r.ID, &r.PeriodID, &r.Department, &r.Instruction, &definitions, &r.DueAt, &r.PayloadHash, &r.IdemKey, &r.ActorRef, &r.At, &r.BaseAttempt, &r.Seq, &r.State, &r.NativeSubmissionRef, &r.ReturnApplied)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err == nil {
		err = json.Unmarshal([]byte(definitions), &r.Indicators)
	}
	return &r, err
}
func AdhocRequestByKey(ctx context.Context, tx pgx.Tx, period uuid.UUID, key string) (*AdhocRequest, error) {
	return scanAdhocRequest(tx.QueryRow(ctx, adhocSelect+` WHERE r.period_id=$1 AND r.idem_key=$2 ORDER BY r.seq DESC LIMIT 1`, period, key))
}
func AdhocRequestByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*AdhocRequest, error) {
	return scanAdhocRequest(tx.QueryRow(ctx, adhocSelect+` WHERE r.id=$1 ORDER BY r.seq DESC LIMIT 1`, id))
}
func LatestAdhocRequest(ctx context.Context, tx pgx.Tx, period uuid.UUID, dept string) (*AdhocRequest, error) {
	return scanAdhocRequest(tx.QueryRow(ctx, adhocSelect+` WHERE r.period_id=$1 AND r.department=$2 ORDER BY r.seq DESC LIMIT 1`, period, dept))
}
func AdhocRequests(ctx context.Context, tx pgx.Tx, period uuid.UUID, dept string) ([]AdhocRequest, error) {
	rows, err := tx.Query(ctx, adhocSelect+` WHERE r.period_id=$1 AND ($2='' OR r.department=$2) ORDER BY r.department,r.seq DESC LIMIT 200`, period, dept)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdhocRequest{}
	for rows.Next() {
		r, e := scanAdhocRequest(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// Same lock identity as khtcreport/store.Lock and the SQL barriers.
func LockAdhocRequest(ctx context.Context, tx pgx.Tx, tid, period uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('khtc_snapshot:'||$1::uuid::text||':'||$2::uuid::text,0))`, tid, period)
	return err
}
func InsertAdhocRequest(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *AdhocRequest) error {
	definitions, err := json.Marshal(r.Indicators)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_adhoc_request
 (id,tenant_id,stream_key,period_id,department,instruction,indicators,due_at,payload_hash,base_attempt,actor_ref,event_time,idem_key,rule_version)
 VALUES($1,$2,'khtc_adhoc_request:'||$3::uuid::text||':'||$4,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12,'khtc-adhoc/v1') RETURNING seq`,
		r.ID, tid, r.PeriodID, r.Department, r.Instruction, string(definitions), r.DueAt, r.PayloadHash, r.BaseAttempt, r.ActorRef, r.At, r.IdemKey).Scan(&r.Seq)
}

type AdhocRequestEvent struct {
	ID, RequestID, PeriodID, ActorRef uuid.UUID
	Action, Reason, IdemKey           string
	NativeSubmissionRef               *uuid.UUID
	ReturnApplied                     *bool
	At                                time.Time
}

func InsertAdhocRequestEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e AdhocRequestEvent) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_adhoc_request_event
 (id,tenant_id,stream_key,request_id,period_id,action,native_submission_ref,return_applied,reason,actor_ref,event_time,idem_key,rule_version)
 VALUES($1,$2,'khtc_adhoc_request_event:'||$3::uuid::text,$3,$4,$5,$6,$7,$8,$9,$10,$11,'khtc-adhoc/v1')`,
		e.ID, tid, e.RequestID, e.PeriodID, e.Action, e.NativeSubmissionRef, e.ReturnApplied, e.Reason, e.ActorRef, e.At, e.IdemKey)
	return err
}

func ActiveAdhocRequest(ctx context.Context, tx pgx.Tx, period uuid.UUID, dept string) (*AdhocRequest, error) {
	return scanAdhocRequest(tx.QueryRow(ctx, adhocSelect+` WHERE r.period_id=$1 AND r.department=$2 AND coalesce(e.action,'intent') NOT IN('failed','cancelled') ORDER BY r.seq DESC LIMIT 1`, period, dept))
}

// Inbox scope and terminal filters precede LIMIT so unrelated tasks cannot starve a department.
func AdhocRequestInbox(ctx context.Context, tx pgx.Tx, dept string) ([]AdhocRequest, error) {
	rows, err := tx.Query(ctx, adhocSelect+` WHERE r.department=$1 AND coalesce(e.action,'intent') IN('intent','issued')
 AND NOT EXISTS(SELECT 1 FROM pf_extension.khtc_period_event t WHERE t.tenant_id=r.tenant_id AND t.period_id=r.period_id AND t.action IN('closed','cancelled'))
 ORDER BY r.due_at,r.id LIMIT 100`, dept)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdhocRequest{}
	for rows.Next() {
		r, e := scanAdhocRequest(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}
