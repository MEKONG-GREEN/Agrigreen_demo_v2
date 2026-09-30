package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// Ticket là một phiếu cân nhận từ mkg.measurement.weigh_ticket.recorded (HUB_IN / CUSTOMER_IN).
type Ticket struct {
	Meta
	TicketID           uuid.UUID
	LotRef             uuid.UUID
	Point              string
	Route              string
	Source             string
	MassKg             decimal.Decimal
	SupersedesTicketID *uuid.UUID
	TicketTime         time.Time
	SourceRule         *string
}

// InsertTicket ghi sổ phiếu cân (idem 'ticket:<ticket_id>').
func InsertTicket(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t *Ticket) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_orders.ticket_event
	    (id, tenant_id, stream_key, ticket_id, lot_ref, point, route, source, mass_kg, supersedes_ticket_id, ticket_time, source_rule,
	     event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10, $11, $12, $13, $14, $15, $16)
	  RETURNING seq, recorded_at`,
		t.ID, tid, "lot:"+t.LotRef.String(), t.TicketID, t.LotRef, t.Point, t.Route, t.Source, t.MassKg.String(), t.SupersedesTicketID,
		t.TicketTime.UTC(), t.SourceRule, t.EventTime.UTC(), t.ActorRef, t.IdemKey, t.RuleVersion).Scan(&t.Seq, &t.RecordedAt)
}

// TicketSeen cho biết phiếu đã được ghi (giao lặp).
func TicketSeen(ctx context.Context, tx pgx.Tx, ticket uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM mkg_orders.ticket_event WHERE ticket_id = $1)", ticket).Scan(&ok)
	return ok, err
}

// LotTicket là phiếu hiệu lực của (lô, điểm cân).
type LotTicket struct {
	LotRef             uuid.UUID
	Point              string
	TicketID           uuid.UUID
	Route              string
	MassKg             decimal.Decimal
	SupersedesTicketID *uuid.UUID
	TicketTime         time.Time
	LastSeq            int64
}

// GetLotTicket đọc phiếu hiệu lực; nil = chưa có.
func GetLotTicket(ctx context.Context, tx pgx.Tx, lot uuid.UUID, point string) (*LotTicket, error) {
	var l LotTicket
	err := tx.QueryRow(ctx, `SELECT lot_ref, point, ticket_id, route, mass_kg::text, supersedes_ticket_id, ticket_time, last_seq
	  FROM mkg_orders.lot_ticket_current WHERE lot_ref = $1 AND point = $2`, lot, point).Scan(
		&l.LotRef, &l.Point, &l.TicketID, &l.Route, &l.MassKg, &l.SupersedesTicketID, &l.TicketTime, &l.LastSeq)
	return noRows(l, err)
}

// TicketByID đọc phiếu đã nhận theo mã phiếu.
func TicketByID(ctx context.Context, tx pgx.Tx, ticket uuid.UUID) (*Ticket, error) {
	var t Ticket
	err := tx.QueryRow(ctx, `SELECT id, seq, ticket_id, lot_ref, point, route, source, mass_kg::text, supersedes_ticket_id, ticket_time
	  FROM mkg_orders.ticket_event WHERE ticket_id = $1 LIMIT 1`, ticket).Scan(
		&t.ID, &t.Seq, &t.TicketID, &t.LotRef, &t.Point, &t.Route, &t.Source, &t.MassKg, &t.SupersedesTicketID, &t.TicketTime)
	return noRows(t, err)
}

// ApplyTicket cập nhật phiếu hiệu lực của (lô, điểm) theo quy tắc: chưa có → nhận; phiếu mới bù phiếu
// hiện hành → nhận; phiếu hiện hành bù phiếu mới (tới muộn) → bỏ; không quan hệ bù → phiếu có thời điểm
// cân muộn hơn thắng (cân lại). Trả true khi phiếu mới trở thành hiệu lực.
func ApplyTicket(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t Ticket) (bool, *LotTicket, error) {
	cur, err := GetLotTicket(ctx, tx, t.LotRef, t.Point)
	if err != nil {
		return false, nil, err
	}
	if !TicketWins(cur, t) {
		return false, cur, nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO mkg_orders.lot_ticket_current
	    (tenant_id, lot_ref, point, ticket_id, route, mass_kg, supersedes_ticket_id, ticket_time, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8, $9)
	  ON CONFLICT (tenant_id, lot_ref, point) DO UPDATE SET ticket_id = EXCLUDED.ticket_id, route = EXCLUDED.route,
	    mass_kg = EXCLUDED.mass_kg, supersedes_ticket_id = EXCLUDED.supersedes_ticket_id, ticket_time = EXCLUDED.ticket_time,
	    last_seq = EXCLUDED.last_seq`,
		tid, t.LotRef, t.Point, t.TicketID, t.Route, t.MassKg.String(), t.SupersedesTicketID, t.TicketTime.UTC(), t.Seq)
	return true, cur, err
}

// TicketWins là quy tắc thay phiếu hiệu lực (dùng cho ghi thường và dựng lại).
func TicketWins(cur *LotTicket, t Ticket) bool {
	switch {
	case cur == nil:
		return true
	case cur.TicketID == t.TicketID:
		return false
	case t.SupersedesTicketID != nil && *t.SupersedesTicketID == cur.TicketID:
		return true
	case cur.SupersedesTicketID != nil && *cur.SupersedesTicketID == t.TicketID:
		return false
	}
	return t.TicketTime.After(cur.TicketTime)
}

// RebuildTickets dựng lại lot_ticket_current bằng phát lại ticket_event theo thứ tự ghi (R-22).
func RebuildTickets(ctx context.Context, tx pgx.Tx, tid uuid.UUID) (int64, error) {
	if _, err := tx.Exec(ctx, "DELETE FROM mkg_orders.lot_ticket_current"); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT id, seq, ticket_id, lot_ref, point, route, source, mass_kg::text, supersedes_ticket_id, ticket_time
	  FROM mkg_orders.ticket_event ORDER BY recorded_at, lot_ref, seq`)
	if err != nil {
		return 0, err
	}
	var ts []Ticket
	for rows.Next() {
		var t Ticket
		if err := rows.Scan(&t.ID, &t.Seq, &t.TicketID, &t.LotRef, &t.Point, &t.Route, &t.Source, &t.MassKg, &t.SupersedesTicketID,
			&t.TicketTime); err != nil {
			rows.Close()
			return 0, err
		}
		ts = append(ts, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, t := range ts {
		if _, _, err := ApplyTicket(ctx, tx, tid, t); err != nil {
			return 0, err
		}
	}
	var n int64
	err = tx.QueryRow(ctx, "SELECT count(*) FROM mkg_orders.lot_ticket_current").Scan(&n)
	return n, err
}
