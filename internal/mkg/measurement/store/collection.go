package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// CollectionLink là một dòng sổ mkg_measurement.lot_collection (lô cân ↔ lệnh gom).
type CollectionLink struct {
	ID                 uuid.UUID
	Seq                int64
	LotRef             uuid.UUID
	CollectionOrderRef uuid.UUID
	Origin             string // session | manual
	SessionRef         *uuid.UUID
	EventTime          time.Time
	SupersedesID       *uuid.UUID
	CorrectionReason   *string
	ActorRef           uuid.UUID
	IdemKey            string
}

// Nguồn của liên kết.
const (
	LinkFromSession = "session"
	LinkManual      = "manual"
)

// InsertCollectionLink ghi liên kết vào sổ và bảng chiếu (cùng giao dịch, R-21).
// Trùng lệnh gom ở lô khác → lỗi 23505 lot_collection_current_order.
func InsertCollectionLink(ctx context.Context, tx pgx.Tx, tid uuid.UUID, l *CollectionLink) error {
	err := tx.QueryRow(ctx, `INSERT INTO mkg_measurement.lot_collection
	    (id, tenant_id, stream_key, lot_ref, collection_order_ref, origin, session_ref, event_time, supersedes_id, correction_reason,
	     actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING seq`,
		l.ID, tid, "collection:"+l.LotRef.String(), l.LotRef, l.CollectionOrderRef, l.Origin, l.SessionRef, l.EventTime.UTC(),
		l.SupersedesID, l.CorrectionReason, l.ActorRef, l.IdemKey, domain.RuleCollectionLink).Scan(&l.Seq)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO mkg_measurement.lot_collection_current (tenant_id, lot_ref, link_id, collection_order_ref, last_seq)
	  VALUES ($1, $2, $3, $4, $5)
	  ON CONFLICT (tenant_id, lot_ref) DO UPDATE SET link_id = EXCLUDED.link_id, collection_order_ref = EXCLUDED.collection_order_ref,
	    last_seq = EXCLUDED.last_seq, updated_at = now()`,
		tid, l.LotRef, l.ID, l.CollectionOrderRef, l.Seq)
	return err
}

const linkCols = `id, seq, lot_ref, collection_order_ref, origin, session_ref, event_time, supersedes_id, correction_reason, actor_ref, idem_key`

const linkColsL = `l.id, l.seq, l.lot_ref, l.collection_order_ref, l.origin, l.session_ref, l.event_time, l.supersedes_id,
  l.correction_reason, l.actor_ref, l.idem_key`

func scanLink(row pgx.Row) (*CollectionLink, error) {
	var l CollectionLink
	err := row.Scan(&l.ID, &l.Seq, &l.LotRef, &l.CollectionOrderRef, &l.Origin, &l.SessionRef, &l.EventTime, &l.SupersedesID,
		&l.CorrectionReason, &l.ActorRef, &l.IdemKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// CollectionOfLot đọc liên kết hiện hành của lô cân (nil = chưa có → quy ước đồng nhất).
func CollectionOfLot(ctx context.Context, tx pgx.Tx, lot uuid.UUID) (*CollectionLink, error) {
	return scanLink(tx.QueryRow(ctx, `SELECT `+linkColsL+` FROM mkg_measurement.lot_collection_current c
	    JOIN mkg_measurement.lot_collection l ON l.id = c.link_id AND l.tenant_id = c.tenant_id
	    WHERE c.lot_ref = $1`, lot))
}

// LotOfCollection đọc liên kết hiện hành trỏ tới lệnh gom (nil = chưa có).
func LotOfCollection(ctx context.Context, tx pgx.Tx, order uuid.UUID) (*CollectionLink, error) {
	return scanLink(tx.QueryRow(ctx, `SELECT `+linkColsL+` FROM mkg_measurement.lot_collection_current c
	    JOIN mkg_measurement.lot_collection l ON l.id = c.link_id AND l.tenant_id = c.tenant_id
	    WHERE c.collection_order_ref = $1`, order))
}

// CollectionLinkByIdem đọc dòng sổ theo idem_key (phát lại).
func CollectionLinkByIdem(ctx context.Context, tx pgx.Tx, key string) (*CollectionLink, error) {
	return scanLink(tx.QueryRow(ctx, `SELECT `+linkCols+` FROM mkg_measurement.lot_collection WHERE idem_key = $1`+ledger.NewestByIdem(""), key))
}

// CollectionOrderRefOf đọc lệnh gom liên kết tường minh của lô qua bảng chiếu (nil khi
// không có) — dùng được với vai ingest_mkg (chỉ SELECT bảng chiếu).
func CollectionOrderRefOf(ctx context.Context, tx pgx.Tx, lot uuid.UUID) (*uuid.UUID, error) {
	var o uuid.UUID
	err := tx.QueryRow(ctx, `SELECT collection_order_ref FROM mkg_measurement.lot_collection_current WHERE lot_ref = $1`, lot).Scan(&o)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}
