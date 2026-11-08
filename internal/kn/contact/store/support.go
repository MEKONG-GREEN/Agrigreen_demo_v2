package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// SupportRow keeps only a sealed payload, an irreversible retry fingerprint and server-owned references.
type SupportRow struct {
	ID                            uuid.UUID
	KeyHash, PayloadHash          []byte
	SubjectRef                    uuid.UUID
	Nonce, WrappedDEK, Ciphertext []byte
	CreatedAt                     time.Time
}

// SupportSummary is safe queue metadata; PII is loaded separately through the vault.
type SupportSummary struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	State     string    `json:"state"`
	Version   int64     `json:"version"`
}

// LockSupport serializes retries/CAS inside the same transaction without mutable state or granting UPDATE.
func LockSupport(ctx context.Context, tx pgx.Tx, binding string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "kn-support:"+binding)
	return err
}

func SupportByKey(ctx context.Context, tx pgx.Tx, key []byte) (SupportRow, bool, error) {
	var r SupportRow
	err := tx.QueryRow(ctx, `SELECT id, payload_hash FROM pf_contact.support_request WHERE key_hash=$1`, key).Scan(&r.ID, &r.PayloadHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, err
}

func InsertSupport(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r SupportRow) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_contact.support_request (tenant_id,id,key_hash,payload_hash,subject_ref,nonce,wrapped_dek,ciphertext) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, tid, r.ID, r.KeyHash, r.PayloadHash, r.SubjectRef, r.Nonce, r.WrappedDEK, r.Ciphertext)
	return err
}

func SupportByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (SupportRow, bool, error) {
	var r SupportRow
	err := tx.QueryRow(ctx, `SELECT id, subject_ref, nonce, wrapped_dek, ciphertext, created_at FROM pf_contact.support_request WHERE id=$1`, id).Scan(&r.ID, &r.SubjectRef, &r.Nonce, &r.WrappedDEK, &r.Ciphertext, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, err
}

const supportSummarySQL = `SELECT r.id,r.created_at,CASE e.event WHEN 'submitted' THEN 'new' ELSE 'resolved' END,e.seq
FROM pf_contact.support_request r JOIN LATERAL (
 SELECT event,seq FROM pf_contact.support_event WHERE request_id=r.id AND event IN ('submitted','resolved') ORDER BY seq DESC LIMIT 1
) e ON true`

func SupportCurrent(ctx context.Context, tx pgx.Tx, id uuid.UUID) (SupportSummary, bool, error) {
	var r SupportSummary
	err := tx.QueryRow(ctx, supportSummarySQL+` WHERE r.id=$1`, id).Scan(&r.ID, &r.CreatedAt, &r.State, &r.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, err
}

func ListSupport(ctx context.Context, tx pgx.Tx, state string, offset int) ([]SupportSummary, error) {
	rows, err := tx.Query(ctx, supportSummarySQL+` WHERE ($1='' OR CASE e.event WHEN 'submitted' THEN 'new' ELSE 'resolved' END=$1) ORDER BY r.created_at DESC,r.id DESC LIMIT 50 OFFSET $2`, state, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SupportSummary, error) {
		var r SupportSummary
		err := row.Scan(&r.ID, &r.CreatedAt, &r.State, &r.Version)
		return r, err
	})
}

func SupportResolutionReplay(ctx context.Context, tx pgx.Tx, key string) (int64, bool, error) {
	var version int64
	err := tx.QueryRow(ctx, `SELECT expected_version FROM pf_contact.support_event WHERE idem_key=$1 AND event='resolved' ORDER BY recorded_at DESC,id DESC LIMIT 1`, key).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return version, err == nil, err
}

// AppendSupportEvent writes audit and state atomically; only anonymous submission has a NULL actor.
func AppendSupportEvent(ctx context.Context, tx pgx.Tx, tid, id, actor uuid.UUID, event, key string, expected *int64, at time.Time) error {
	var request, who any
	stream := "support:" + id.String()
	if id != uuid.Nil {
		request = id
	} else {
		stream = "support-queue:" + actor.String()
	}
	if actor != uuid.Nil {
		who = actor
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_contact.support_event (id,tenant_id,stream_key,request_id,event,expected_version,event_time,actor_ref,idem_key,rule_version) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'kn.account_support/1')`, ids.New(), tid, stream, request, event, expected, at, who, key)
	return err
}
