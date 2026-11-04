package store

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type CloseReservation211 struct {
	Operation, Period, Event, Actor uuid.UUID
	Action                          string
}

func Reservation211(ctx context.Context, tx pgx.Tx, op uuid.UUID) (*CloseReservation211, error) {
	var r CloseReservation211
	err := tx.QueryRow(ctx, `SELECT operation_id,period_id,close_event_id,actor_ref,action FROM pf_extension.khtc_close_reservation211 WHERE operation_id=$1 ORDER BY seq DESC LIMIT 1`, op).Scan(&r.Operation, &r.Period, &r.Event, &r.Actor, &r.Action)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}
func ReserveEvent211(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r CloseReservation211, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_close_reservation211(id,tenant_id,stream_key,operation_id,period_id,close_event_id,action,actor_ref,event_time,idem_key,rule_version) VALUES($1,$2,'khtc_close_reservation211:'||$3::uuid::text,$3,$4,$5,$6,$7,$8,'close-reservation211:'||$3::uuid::text||':'||$6,'khtc.close-reservation/v1')`, ids.New(), tid, r.Operation, r.Period, r.Event, r.Action, r.Actor, at)
	return err
}
func CloseReserved211(ctx context.Context, tx pgx.Tx, tid, pid uuid.UUID) (bool, error) {
	var b bool
	err := tx.QueryRow(ctx, `SELECT pf_extension.fn_khtc_close_reserved211($1,$2)`, tid, pid).Scan(&b)
	return b, err
}
