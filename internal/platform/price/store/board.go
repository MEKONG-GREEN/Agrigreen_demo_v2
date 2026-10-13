package store

// SQL của bảng giá ngày có phiên bản (W11.76, pf 000151): sổ pf_price.board (trạng thái), board_line (dòng của một phiên
// bản đã duyệt), board_ticket (phiếu thuộc một phiên bản). Giá đọc/ghi qua text ↔ decimal (không float — R-CODE-01).

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/domain"
)

// BoardWrite là một phiên bản trạng thái bảng giá cần ghi. Week* / DaysScheduled / LinesCount / TicketsCount chỉ có ở
// thao tác approve.
type BoardWrite struct {
	Profile       string
	Day           time.Time
	Action        string
	Status        string
	BaseStatus    string
	LockUntil     *time.Time
	Version       int
	ApprovedAt    *time.Time
	Reason        string
	WeekKey       string
	WeekYear      int
	WeekFrom      time.Time
	WeekTo        time.Time
	DaysScheduled int
	LinesCount    int
	TicketsCount  int
}

// LockBoard khóa bảng giá (profile, ngày) của tenant đến hết giao dịch — mọi thao tác duyệt / mở duyệt tuần tự.
func LockBoard(ctx context.Context, tx pgx.Tx, tid uuid.UUID, profile string, day time.Time) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_price.board:' || $1::text || ':' || $2 || ':' || $3, 0))`,
		tid, profile, dateOnly(day))
	return err
}

const boardCols = `board_date, seq, action, status, coalesce(base_status, ''), lock_until, board_version, approved_at,
  coalesce(reason, ''), actor_ref, event_time, recorded_at`

func scanBoard(r pgx.CollectableRow) (boardRow, error) {
	var b boardRow
	err := r.Scan(&b.Day, &b.B.Seq, &b.B.Action, &b.B.Status, &b.B.BaseStatus, &b.B.LockUntil, &b.B.Version, &b.B.ApprovedAt, &b.B.Reason,
		&b.B.Actor, &b.B.EventTime, &b.B.RecordedAt)
	return b, err
}

type boardRow struct {
	Day time.Time
	B   domain.Board
}

// CurrentBoard đọc phiên bản hiện hành của bảng giá ngày day (Seq = 0 nếu chưa có sổ).
func CurrentBoard(ctx context.Context, tx pgx.Tx, profile string, day time.Time) (domain.Board, error) {
	rows, err := tx.Query(ctx, `SELECT `+boardCols+` FROM pf_price.board WHERE profile = $1 AND board_date = $2::date
	  ORDER BY seq DESC LIMIT 1`, profile, dateOnly(day))
	if err != nil {
		return domain.Board{}, err
	}
	rs, err := pgx.CollectRows(rows, scanBoard)
	if err != nil || len(rs) == 0 {
		return domain.Board{}, err
	}
	return rs[0].B, nil
}

// BoardsBetween đọc phiên bản hiện hành của bảng giá các ngày trong [from, to] (ngày chưa có sổ vắng).
func BoardsBetween(ctx context.Context, tx pgx.Tx, profile string, from, to time.Time) (map[time.Time]domain.Board, error) {
	rows, err := tx.Query(ctx, `SELECT `+boardCols+` FROM (
	    SELECT DISTINCT ON (board_date) * FROM pf_price.board WHERE profile = $1 AND board_date BETWEEN $2::date AND $3::date
	     ORDER BY board_date, seq DESC) b`, profile, dateOnly(from), dateOnly(to))
	if err != nil {
		return nil, err
	}
	rs, err := pgx.CollectRows(rows, scanBoard)
	if err != nil {
		return nil, err
	}
	out := make(map[time.Time]domain.Board, len(rs))
	for _, r := range rs {
		out[r.Day] = r.B
	}
	return out, nil
}

// BoardHistory đọc mọi phiên bản trạng thái bảng giá của ngày (nhật ký duyệt / mở duyệt).
func BoardHistory(ctx context.Context, tx pgx.Tx, profile string, day time.Time) ([]domain.Board, error) {
	rows, err := tx.Query(ctx, `SELECT `+boardCols+` FROM pf_price.board WHERE profile = $1 AND board_date = $2::date ORDER BY seq`,
		profile, dateOnly(day))
	if err != nil {
		return nil, err
	}
	rs, err := pgx.CollectRows(rows, scanBoard)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Board, len(rs))
	for i, r := range rs {
		out[i] = r.B
	}
	return out, nil
}

// InsertBoard ghi một phiên bản trạng thái bảng giá; seq, chuỗi băm do trigger fn_chain đặt.
func InsertBoard(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, b BoardWrite, at time.Time, actor uuid.UUID, idem string) (int64, error) {
	var (
		wk, wf, wt     any
		wy, ds, lc, tc any
		base           = nullText(b.BaseStatus)
		seq            int64
	)
	if b.WeekKey != "" {
		wk, wy, wf, wt = b.WeekKey, b.WeekYear, dateOnly(b.WeekFrom), dateOnly(b.WeekTo)
		ds, lc, tc = b.DaysScheduled, b.LinesCount, b.TicketsCount
	}
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.board (id, tenant_id, stream_key, profile, board_date, action, status, base_status,
	    lock_until, board_version, approved_at, reason, week_key, week_year, week_from, week_to, days_scheduled, lines_count,
	    tickets_count, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'board:' || $3::text || ':' || $4, $3, $4::date, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::date, $15::date,
	    $16, $17, $18, $19, $20, $21, $22) RETURNING seq`,
		id, tid, b.Profile, dateOnly(b.Day), b.Action, b.Status, base, b.LockUntil, b.Version, b.ApprovedAt, nullText(b.Reason),
		wk, wy, wf, wt, ds, lc, tc, at.UTC(), actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// BoardLineRow là một dòng của phiên bản bảng giá đã duyệt.
type BoardLineRow struct {
	Item      string
	ItemName  string
	GroupCode string
	UnitCode  string
	Min, Max  decimal.Decimal
	Avg       decimal.Decimal
	Points    int
}

// InsertBoardLine ghi một dòng của phiên bản version.
func InsertBoardLine(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, profile string, day time.Time, version int, l BoardLineRow,
	at time.Time, actor uuid.UUID, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.board_line (id, tenant_id, stream_key, profile, board_date, board_version, item_code,
	    item_name, group_code, unit_code, price_min, price_max, price_avg, points_reported, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'bline:' || $3::text || ':' || $4 || ':v' || $5::int::text || ':' || $6::text, $3, $4::date, $5, $6, $7, $8, $9,
	    $10::numeric, $11::numeric, $12::numeric, $13, $14, $15, $16, $17)`,
		id, tid, profile, dateOnly(day), version, l.Item, l.ItemName, l.GroupCode, l.UnitCode, l.Min.String(), l.Max.String(),
		l.Avg.String(), l.Points, at.UTC(), actor, idem, domain.RuleVersion)
	return err
}

// LoadBoardLines đọc dòng của phiên bản version, theo nhóm rồi mã mặt hàng.
func LoadBoardLines(ctx context.Context, tx pgx.Tx, profile string, day time.Time, version int) ([]BoardLineRow, error) {
	rows, err := tx.Query(ctx, `SELECT item_code, item_name, group_code, unit_code, price_min::text, price_max::text, price_avg::text,
	    points_reported FROM pf_price.board_line WHERE profile = $1 AND board_date = $2::date AND board_version = $3
	  ORDER BY item_code`, profile, dateOnly(day), version)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (BoardLineRow, error) {
		var (
			l           BoardLineRow
			lo, hi, avg string
			e1, e2, e3  error
		)
		if err := r.Scan(&l.Item, &l.ItemName, &l.GroupCode, &l.UnitCode, &lo, &hi, &avg, &l.Points); err != nil {
			return l, err
		}
		l.Min, e1 = decimal.NewFromString(lo)
		l.Max, e2 = decimal.NewFromString(hi)
		l.Avg, e3 = decimal.NewFromString(avg)
		for _, e := range []error{e1, e2, e3} {
			if e != nil {
				return l, e
			}
		}
		return l, nil
	})
}

// BoardTicketRow là một phiếu thuộc phiên bản bảng giá.
type BoardTicketRow struct {
	TicketID    uuid.UUID
	TicketSeq   int64
	PointCode   string
	CommuneCode string
	Late        bool
}

// InsertBoardTicket ghi một phiếu thuộc phiên bản version.
func InsertBoardTicket(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, profile string, day time.Time, version int, t BoardTicketRow,
	at time.Time, actor uuid.UUID, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.board_ticket (id, tenant_id, stream_key, profile, board_date, board_version, ticket_id,
	    ticket_seq, point_code, commune_code, late, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'bticket:' || $3::text || ':' || $4 || ':v' || $5::int::text || ':' || $6::uuid::text, $3, $4::date, $5, $6::uuid,
	    $7, $8, $9, $10, $11, $12, $13, $14)`,
		id, tid, profile, dateOnly(day), version, t.TicketID, t.TicketSeq, t.PointCode, t.CommuneCode, t.Late, at.UTC(), actor, idem,
		domain.RuleVersion)
	return err
}

// DayChangedSince cho biết số liệu ngày day đã đổi sau thời điểm since: phiên bản phiếu (kể cả phiếu chuyển ngày đi / tới),
// dòng giá hoặc cảnh báo của phiếu thuộc ngày — "Hủy mở duyệt khi chưa sửa gì" (FN-TI-02.06 luồng phụ).
func DayChangedSince(ctx context.Context, tx pgx.Tx, day, since time.Time) (bool, error) {
	var changed bool
	err := tx.QueryRow(ctx, `WITH t AS (SELECT DISTINCT ticket_id FROM pf_price.ticket WHERE quote_day = $1::date)
	  SELECT EXISTS (SELECT 1 FROM pf_price.ticket x WHERE x.ticket_id IN (SELECT ticket_id FROM t) AND x.recorded_at > $2)
	      OR EXISTS (SELECT 1 FROM pf_price.ticket_line x WHERE x.ticket_id IN (SELECT ticket_id FROM t) AND x.recorded_at > $2)
	      OR EXISTS (SELECT 1 FROM pf_price.price_alert x WHERE x.ticket_id IN (SELECT ticket_id FROM t) AND x.recorded_at > $2)`,
		dateOnly(day), since.UTC()).Scan(&changed)
	return changed, err
}
