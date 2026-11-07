package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// WeeklyRecord is a program snapshot and its audit metadata.
type WeeklyRecord struct {
	Seq    int64             `json:"version"`
	Book   domain.WeeklyBook `json:"-"`
	WeekID string            `json:"week_id"`
	Action string            `json:"action"`
	Actor  uuid.UUID         `json:"actor"`
	At     time.Time         `json:"at"`
	Hash   []byte            `json:"-"`
}

// LockWeekly serializes all writes, including initialization, per tenant/profile/program.
func LockWeekly(ctx context.Context, tx pgx.Tx, tid uuid.UUID, profile, program string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_price.weekly:' || $1::text || ':' || $2 || ':' || $3,0))`, tid, profile, program)
	return err
}

// LoadWeekly loads the latest snapshot, or the result of an already authorized idempotent command.
func LoadWeekly(ctx context.Context, tx pgx.Tx, profile, program, idem string) (WeeklyRecord, error) {
	var r WeeklyRecord
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT seq,payload,week_id::text,action,actor_ref,event_time,request_hash
	 FROM pf_price.weekly_program WHERE profile=$1 AND program=$2 AND ($3='' OR idem_key=$3) ORDER BY seq DESC LIMIT 1`, profile, program, idem).
		Scan(&r.Seq, &raw, &r.WeekID, &r.Action, &r.Actor, &r.At, &r.Hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(raw, &r.Book)
	return r, err
}

// InsertWeekly appends an audited snapshot through the shared ledger trigger.
func InsertWeekly(ctx context.Context, tx pgx.Tx, tid uuid.UUID, profile, program, weekID, action, idem string, actor uuid.UUID, at time.Time, hash []byte, book domain.WeeklyBook) (WeeklyRecord, error) {
	r := WeeklyRecord{Book: book, WeekID: weekID, Action: action, Actor: actor, At: at, Hash: hash}
	raw, err := json.Marshal(book)
	if err != nil {
		return r, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO pf_price.weekly_program
	 (id,tenant_id,stream_key,profile,program,week_id,action,payload,request_hash,event_time,actor_ref,idem_key,rule_version)
	 VALUES ($1,$2,'weekly:' || $3 || ':' || $4,$3,$4,$5,$6,$7,$8,$9,$10,$11,'price.weekly.v2') RETURNING seq`,
		ids.New(), tid, profile, program, weekID, action, raw, hash, at.UTC(), actor, idem).Scan(&r.Seq)
	return r, err
}

// WeeklyHistory returns audit metadata without loading each historical payload.
func WeeklyHistory(ctx context.Context, tx pgx.Tx, profile, program string) ([]WeeklyRecord, error) {
	rows, err := tx.Query(ctx, `SELECT seq,week_id::text,action,actor_ref,event_time FROM pf_price.weekly_program
	 WHERE profile=$1 AND program=$2 ORDER BY seq DESC`, profile, program)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (WeeklyRecord, error) {
		var r WeeklyRecord
		err := row.Scan(&r.Seq, &r.WeekID, &r.Action, &r.Actor, &r.At)
		return r, err
	})
}
