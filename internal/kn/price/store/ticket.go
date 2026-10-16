package store

// SQL của phiếu báo giá + xác minh (W11.75, pf 000148): sổ pf_price.ticket, ticket_line, price_alert. Đọc phiên bản mới
// nhất của mỗi chuỗi (DISTINCT ON stream_key … seq DESC). Số tiền, % đọc/ghi qua text ↔ decimal (không float — R-CODE-01).

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
)

// Ticket là một phiên bản đầu phiếu.
type Ticket struct {
	ID          uuid.UUID // ticket_id (ổn định)
	Seq         int64     // phiên bản
	PointCode   string
	Day         time.Time
	Status      string
	Source      string
	Op          string
	OfficerRef  uuid.UUID
	Reason      string
	SubmittedAt *time.Time
	EventTime   time.Time
	ActorRef    uuid.UUID
	RecordedAt  time.Time
}

// TicketFilter lọc phiếu theo phiên bản MỚI NHẤT (sửa ngày phiếu đổi quote_day ở phiên bản mới).
type TicketFilter struct {
	Point    string    // rỗng = mọi xã
	From, To time.Time // khoảng ngày báo giá (bao gồm hai đầu)
	IDs      []uuid.UUID
	Live     bool // bỏ phiếu Đã xóa
}

const ticketCols = `ticket_id, seq, point_code, quote_day, status, source, op, officer_ref, coalesce(reason, ''), submitted_at,
  event_time, actor_ref, recorded_at`

func scanTicket(r pgx.CollectableRow) (Ticket, error) {
	var (
		t   Ticket
		off *uuid.UUID
	)
	err := r.Scan(&t.ID, &t.Seq, &t.PointCode, &t.Day, &t.Status, &t.Source, &t.Op, &off, &t.Reason, &t.SubmittedAt, &t.EventTime,
		&t.ActorRef, &t.RecordedAt)
	if off != nil {
		t.OfficerRef = *off
	}
	return t, err
}

// LoadTickets đọc phiên bản mới nhất của các phiếu khớp bộ lọc, theo ngày, xã.
func LoadTickets(ctx context.Context, tx pgx.Tx, f TicketFilter) ([]Ticket, error) {
	var from, to any
	if !f.From.IsZero() {
		from = dateOnly(f.From)
	}
	if !f.To.IsZero() {
		to = dateOnly(f.To)
	}
	var idsArg any
	if f.IDs != nil {
		idsArg = uuidTexts(f.IDs)
	}
	rows, err := tx.Query(ctx, `SELECT `+ticketCols+` FROM (
	    SELECT DISTINCT ON (ticket_id) * FROM pf_price.ticket
	     WHERE ticket_id IN (SELECT ticket_id FROM pf_price.ticket
	                          WHERE ($1 = '' OR point_code = $1)
	                            AND ($2::date IS NULL OR quote_day >= $2::date) AND ($3::date IS NULL OR quote_day <= $3::date)
	                            AND ($4::text[] IS NULL OR ticket_id = ANY($4::text[]::uuid[])))
	     ORDER BY ticket_id, seq DESC) t
	  WHERE ($1 = '' OR point_code = $1)
	    AND ($2::date IS NULL OR quote_day >= $2::date) AND ($3::date IS NULL OR quote_day <= $3::date)
	    AND (NOT $5 OR status <> 'deleted')
	  ORDER BY quote_day, point_code, recorded_at`, f.Point, from, to, idsArg, f.Live)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanTicket)
}

// TicketHistory đọc mọi phiên bản của một phiếu (nhật ký).
func TicketHistory(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]Ticket, error) {
	rows, err := tx.Query(ctx, `SELECT `+ticketCols+` FROM pf_price.ticket WHERE ticket_id = $1 ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanTicket)
}

// LockPointTickets khóa sổ phiếu của một xã đến hết giao dịch: "mỗi xã một phiếu cho một ngày" (BR-03) kiểm trên cùng ảnh sổ.
func LockPointTickets(ctx context.Context, tx pgx.Tx, tid uuid.UUID, point string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_price.ticket:' || $1::text || ':' || $2::text, 0))`, tid, point)
	return err
}

// InsertTicket ghi một phiên bản đầu phiếu; seq, chuỗi băm do trigger fn_chain đặt.
func InsertTicket(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, t Ticket, at time.Time, actor uuid.UUID, idem string) (int64, error) {
	var off any
	if t.OfficerRef != uuid.Nil {
		off = t.OfficerRef
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.ticket (id, tenant_id, stream_key, ticket_id, point_code, quote_day, status, source,
	    op, officer_ref, reason, submitted_at, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'ticket:' || $3::uuid::text, $3::uuid, $4, $5::date, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING seq`,
		id, tid, t.ID, t.PointCode, dateOnly(t.Day), t.Status, t.Source, t.Op, off, nullText(t.Reason), t.SubmittedAt, at.UTC(), actor,
		idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// Line là một phiên bản dòng giá.
type Line struct {
	TicketID  uuid.UUID
	Seq       int64
	Line      domain.Line
	AdjMin    *decimal.Decimal
	AdjMax    *decimal.Decimal
	AdjReason string
	EventTime time.Time
	ActorRef  uuid.UUID
}

// uuidTexts chuyển mã thành text[] (SQL ép ::uuid[]) — pgx không tự mã hóa []uuid.UUID.
func uuidTexts(xs []uuid.UUID) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = x.String()
	}
	return out
}

func decPtr(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func decArg(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.String()
}

func scanLine(r pgx.CollectableRow) (Line, error) {
	var (
		l                Line
		lo, hi, alo, ahi *string
		e1, e2, e3, e4   error
		err              error
	)
	if err = r.Scan(&l.TicketID, &l.Line.ItemCode, &l.Seq, &lo, &hi, &l.Line.NoGoods, &l.Line.Note, &alo, &ahi, &l.AdjReason, &l.EventTime,
		&l.ActorRef); err != nil {
		return l, err
	}
	l.Line.Min, e1 = decPtr(lo)
	l.Line.Max, e2 = decPtr(hi)
	l.AdjMin, e3 = decPtr(alo)
	l.AdjMax, e4 = decPtr(ahi)
	for _, e := range []error{e1, e2, e3, e4} {
		if e != nil {
			return l, e
		}
	}
	return l, nil
}

const lineCols = `ticket_id, item_code, seq, min_price::text, max_price::text, no_goods, coalesce(note, ''), adj_min::text, adj_max::text,
  coalesce(adj_reason, ''), event_time, actor_ref`

// LoadLines đọc phiên bản mới nhất của mọi dòng giá các phiếu ids.
func LoadLines(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) ([]Line, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT `+lineCols+` FROM (
	    SELECT DISTINCT ON (ticket_id, item_code) * FROM pf_price.ticket_line WHERE ticket_id = ANY($1::text[]::uuid[])
	     ORDER BY ticket_id, item_code, seq DESC) l ORDER BY ticket_id, item_code`, uuidTexts(ids))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanLine)
}

// LineHistory đọc mọi phiên bản dòng giá của một phiếu (nhật ký, lịch sử hiệu chỉnh).
func LineHistory(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]Line, error) {
	rows, err := tx.Query(ctx, `SELECT `+lineCols+` FROM pf_price.ticket_line WHERE ticket_id = $1 ORDER BY item_code, seq`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanLine)
}

// InsertLine ghi một phiên bản dòng giá.
func InsertLine(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, l Line, at time.Time, actor uuid.UUID, idem string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.ticket_line (id, tenant_id, stream_key, ticket_id, item_code, min_price, max_price,
	    no_goods, note, adj_min, adj_max, adj_reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'line:' || $3::uuid::text || ':' || $4::text, $3::uuid, $4::text, $5::numeric, $6::numeric, $7, $8, $9::numeric, $10::numeric,
	    $11, $12, $13, $14, $15) RETURNING seq`,
		id, tid, l.TicketID, l.Line.ItemCode, decArg(l.Line.Min), decArg(l.Line.Max), l.Line.NoGoods, nullText(l.Line.Note),
		decArg(l.AdjMin), decArg(l.AdjMax), nullText(l.AdjReason), at.UTC(), actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// HasPricedLine cho biết mặt hàng đã có giá trên một phiếu đã gửi chính thức (W11.73 ItemHooks.HasPrice — khóa ĐVT…).
func HasPricedLine(ctx context.Context, tx pgx.Tx, item string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_price.ticket_line l
	    WHERE l.item_code = $1 AND l.min_price IS NOT NULL
	      AND EXISTS (SELECT 1 FROM pf_price.ticket t WHERE t.ticket_id = l.ticket_id AND t.status IN ('submitted', 'late')))`, item).Scan(&ok)
	return ok, err
}

// Alert là một phiên bản cảnh báo biến động.
type Alert struct {
	TicketID    uuid.UUID
	ItemCode    string
	Seq         int64
	Status      string
	Direction   string
	Ref         decimal.Decimal
	Price       decimal.Decimal
	ChangePct   decimal.Decimal
	Threshold   decimal.Decimal
	Message     string
	RespondBy   *time.Time
	Response    string
	ProposedMin *decimal.Decimal
	ProposedMax *decimal.Decimal
	Explanation string
	EventTime   time.Time
	ActorRef    uuid.UUID
}

const alertCols = `ticket_id, item_code, seq, status, direction, ref_price::text, price::text, change_pct::text, threshold_pct::text,
  coalesce(message, ''), respond_by, coalesce(response, ''), proposed_min::text, proposed_max::text, coalesce(explanation, ''),
  event_time, actor_ref`

func scanAlert(r pgx.CollectableRow) (Alert, error) {
	var (
		a                Alert
		ref, p, pct, thr string
		pmin, pmax       *string
		err              error
	)
	if err = r.Scan(&a.TicketID, &a.ItemCode, &a.Seq, &a.Status, &a.Direction, &ref, &p, &pct, &thr, &a.Message, &a.RespondBy, &a.Response,
		&pmin, &pmax, &a.Explanation, &a.EventTime, &a.ActorRef); err != nil {
		return a, err
	}
	if a.Ref, err = decimal.NewFromString(ref); err != nil {
		return a, err
	}
	if a.Price, err = decimal.NewFromString(p); err != nil {
		return a, err
	}
	if a.ChangePct, err = decimal.NewFromString(pct); err != nil {
		return a, err
	}
	if a.Threshold, err = decimal.NewFromString(thr); err != nil {
		return a, err
	}
	if a.ProposedMin, err = decPtr(pmin); err != nil {
		return a, err
	}
	a.ProposedMax, err = decPtr(pmax)
	return a, err
}

// LoadAlerts đọc MỌI phiên bản cảnh báo của các phiếu ids (phiên bản mới nhất = trạng thái; phiên bản cũ = trao đổi
// xác minh), theo phiếu, mặt hàng, seq.
func LoadAlerts(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) ([]Alert, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT `+alertCols+` FROM pf_price.price_alert WHERE ticket_id = ANY($1::text[]::uuid[])
	  ORDER BY ticket_id, item_code, seq`, uuidTexts(ids))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanAlert)
}

// InsertAlert ghi một phiên bản cảnh báo.
func InsertAlert(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, a Alert, at time.Time, actor uuid.UUID, idem string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.price_alert (id, tenant_id, stream_key, ticket_id, item_code, status, direction,
	    ref_price, price, change_pct, threshold_pct, message, respond_by, response, proposed_min, proposed_max, explanation, event_time,
	    actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'alert:' || $3::uuid::text || ':' || $4::text, $3::uuid, $4::text, $5, $6, $7::numeric, $8::numeric, $9::numeric, $10::numeric,
	    $11, $12, $13, $14::numeric, $15::numeric, $16, $17, $18, $19, $20) RETURNING seq`,
		id, tid, a.TicketID, a.ItemCode, a.Status, a.Direction, a.Ref.String(), a.Price.String(), a.ChangePct.String(), a.Threshold.String(),
		nullText(a.Message), a.RespondBy, nullText(a.Response), decArg(a.ProposedMin), decArg(a.ProposedMax), nullText(a.Explanation),
		at.UTC(), actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}
