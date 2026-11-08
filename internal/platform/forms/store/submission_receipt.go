package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

type SubmissionReceipt struct {
	Profile      string
	PeriodID     uuid.UUID
	Unit         string
	Actor        uuid.UUID
	SubmissionID uuid.UUID
	Status       string
	SameRequest  bool
}

func InsertSubmissionReceipt(ctx context.Context, tx pgx.Tx, tid uuid.UUID, profile string, request json.RawMessage, key string, s Submission, status string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_forms.submission_receipt(id,tenant_id,stream_key,profile,period_id,unit_code,submission_id,status,request_content,event_time,actor_ref,idem_key,rule_version)
	 VALUES($1,$2,'submit:'||$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$3,'forms.submit/v1')`, ids.New(), tid, key, profile, s.PeriodID, s.Unit, s.ID, status, string(request), s.EventTime, s.Submitter)
	return err
}

func SubmissionReceiptByKey(ctx context.Context, tx pgx.Tx, key string, request json.RawMessage) (*SubmissionReceipt, error) {
	var r SubmissionReceipt
	err := tx.QueryRow(ctx, `SELECT profile,period_id,unit_code,actor_ref,submission_id,status,request_content=$2::jsonb FROM pf_forms.submission_receipt WHERE idem_key=$1`, key, string(request)).Scan(&r.Profile, &r.PeriodID, &r.Unit, &r.Actor, &r.SubmissionID, &r.Status, &r.SameRequest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}
