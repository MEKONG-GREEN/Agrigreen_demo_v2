// Package store là SQL của pf/workflow (schema pf_workflow, pf 000118). Ghi chỉ qua hai hàm DB
// pf_workflow.submit / pf_workflow.decide — chạy được trong giao dịch của module gọi (vai r_masterdata,
// r_party… chỉ có EXECUTE). Đọc chạy trong giao dịch của chính module (vai r_workflow).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Request là một dòng pf_workflow.request.
type Request struct {
	ID          uuid.UUID
	Kind        string
	Subject     uuid.UUID
	Requester   uuid.UUID
	Beneficiary *uuid.UUID
	Seq         int64
	Replayed    bool
}

// Decision là một dòng pf_workflow.decision.
type Decision struct {
	ID        uuid.UUID
	RequestID uuid.UUID
	Outcome   string
	Decider   uuid.UUID
	At        time.Time
	Replayed  bool
}

func nullText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Submit gọi pf_workflow.submit (idempotent theo kind + subject).
func Submit(ctx context.Context, tx pgx.Tx, id uuid.UUID, kind string, subject, requester uuid.UUID, beneficiary *uuid.UUID,
	actor uuid.UUID, summary string, at time.Time, ruleVersion string) (Request, error) {
	r := Request{Kind: kind, Subject: subject}
	err := tx.QueryRow(ctx, `SELECT o_request_id, o_requester_ref, o_beneficiary_ref, o_seq, o_replayed
	    FROM pf_workflow.submit($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		id, kind, subject, requester, beneficiary, actor, nullText(summary), at.UTC(), ruleVersion).
		Scan(&r.ID, &r.Requester, &r.Beneficiary, &r.Seq, &r.Replayed)
	return r, err
}

// Decide gọi pf_workflow.decide (lập bù yêu cầu nếu chưa có, rồi ghi quyết định).
func Decide(ctx context.Context, tx pgx.Tx, requestID, decisionID uuid.UUID, kind string, subject, requester uuid.UUID,
	beneficiary *uuid.UUID, decider uuid.UUID, outcome, note string, at time.Time, ruleVersion string) (Decision, error) {
	var d Decision
	err := tx.QueryRow(ctx, `SELECT o_decision_id, o_request_id, o_outcome, o_decider_ref, o_event_time, o_replayed
	    FROM pf_workflow.decide($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		requestID, decisionID, kind, subject, requester, beneficiary, decider, outcome, nullText(note), at.UTC(), ruleVersion).
		Scan(&d.ID, &d.RequestID, &d.Outcome, &d.Decider, &d.At, &d.Replayed)
	return d, err
}

// RequestBySubject đọc yêu cầu của một đối tượng (nil nếu chưa có) và quyết định của nó (nil nếu chưa có).
func RequestBySubject(ctx context.Context, tx pgx.Tx, kind string, subject uuid.UUID) (*Request, *Decision, error) {
	var r Request
	err := tx.QueryRow(ctx, `SELECT id, kind, subject_ref, requester_ref, beneficiary_ref, seq
	    FROM pf_workflow.request WHERE kind = $1 AND subject_ref = $2`, kind, subject).
		Scan(&r.ID, &r.Kind, &r.Subject, &r.Requester, &r.Beneficiary, &r.Seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var d Decision
	err = tx.QueryRow(ctx, `SELECT id, request_id, outcome, decider_ref, event_time
	    FROM pf_workflow.decision WHERE request_id = $1`, r.ID).
		Scan(&d.ID, &d.RequestID, &d.Outcome, &d.Decider, &d.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return &r, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &r, &d, nil
}
