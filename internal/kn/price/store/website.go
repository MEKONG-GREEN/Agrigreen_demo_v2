package store

import (
	"context"
	"errors"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WebsiteStatus reads acknowledgement metadata, without a URL or personal data.
func WebsiteStatus(ctx context.Context, tx pgx.Tx, profile string, day time.Time, version int) (domain.WebsiteStatus, error) {
	v := domain.WebsiteStatus{Status: domain.WebsiteNotConnected, Version: version}
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT event_time FROM pf_price.board_website_attempt
      WHERE profile=$1 AND board_date=$2::date AND board_version=$3 ORDER BY seq DESC LIMIT 1`, profile, dateOnly(day), version).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, nil
	}
	if err == nil {
		v.RequestedAt = &at
	}
	return v, err
}

// InsertWebsiteAttempt appends a disconnected acknowledgement; no delivery is queued.
func InsertWebsiteAttempt(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, profile string, day time.Time, version int,
	at time.Time, actor uuid.UUID, idem, trace string) error {
	var duplicate bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pf_price.board_website_attempt
      WHERE idem_key=$1 AND profile=$2 AND board_date=$3::date AND board_version=$4 AND actor_ref=$5)`,
		idem, profile, dateOnly(day), version, actor).Scan(&duplicate)
	if err != nil || duplicate {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_price.board_website_attempt
      (id,tenant_id,stream_key,profile,board_date,board_version,status,event_time,actor_ref,idem_key,rule_version,trace_id)
      VALUES($1,$2,'website:'||$3::text||':'||$4,$3,$4::date,$5,$6,$7,$8,$9,'kn-standalone-d2-v1',$10)`,
		id, tid, profile, dateOnly(day), version, domain.WebsiteNotConnected, at.UTC(), actor, idem, trace)
	return err
}
