package store

// SQL của báo cáo giá (W11.77, pf 20261013133657): dòng giá theo xã của phiên bản đã duyệt (board_point), đọc bảng giá
// đã duyệt theo khoảng ngày, khóa tháng thống kê phiếu (stat_month, stat_month_line). Giá qua text ↔ decimal (không float
// — R-CODE-01).

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/domain"
)

// BoardPointRow là giá một mặt hàng của một xã trong phiên bản đã duyệt.
type BoardPointRow struct {
	Day      time.Time
	Version  int
	Point    string
	Region   string
	Item     string
	Min, Max decimal.Decimal
	Mid      decimal.Decimal
	Adjusted bool
	Late     bool
}

// InsertBoardPoint ghi một dòng giá theo xã của phiên bản version.
func InsertBoardPoint(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, profile string, day time.Time, version int, r BoardPointRow,
	at time.Time, actor uuid.UUID, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.board_point (id, tenant_id, stream_key, profile, board_date, board_version, point_code,
	    region_code, item_code, price_min, price_max, price_mid, adjusted, late, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'bpoint:' || $3::text || ':' || $4 || ':v' || $5::int::text || ':' || $6::text || ':' || $8::text, $3, $4::date, $5,
	    $6, $7, $8, $9::numeric, $10::numeric, $11::numeric, $12, $13, $14, $15, $16, $17)`,
		id, tid, profile, dateOnly(day), version, r.Point, r.Region, r.Item, r.Min.String(), r.Max.String(), r.Mid.String(), r.Adjusted,
		r.Late, at.UTC(), actor, idem, domain.RuleVersion)
	return err
}

func decs(ss ...string) ([]decimal.Decimal, error) {
	out := make([]decimal.Decimal, len(ss))
	for i, s := range ss {
		d, err := decimal.NewFromString(s)
		if err != nil {
			return nil, err
		}
		out[i] = d
	}
	return out, nil
}

// BoardPointsBetween đọc dòng giá theo xã của MỌI phiên bản đã duyệt các ngày trong [from, to]; bên gọi chọn phiên bản
// hiện hành.
func BoardPointsBetween(ctx context.Context, tx pgx.Tx, profile string, from, to time.Time) ([]BoardPointRow, error) {
	rows, err := tx.Query(ctx, `SELECT board_date, board_version, point_code, region_code, item_code, price_min::text, price_max::text,
	    price_mid::text, adjusted, late FROM pf_price.board_point
	  WHERE profile = $1 AND board_date BETWEEN $2::date AND $3::date ORDER BY board_date, board_version, point_code, item_code`,
		profile, dateOnly(from), dateOnly(to))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (BoardPointRow, error) {
		var (
			x          BoardPointRow
			lo, hi, md string
		)
		if err := r.Scan(&x.Day, &x.Version, &x.Point, &x.Region, &x.Item, &lo, &hi, &md, &x.Adjusted, &x.Late); err != nil {
			return x, err
		}
		ds, err := decs(lo, hi, md)
		if err != nil {
			return x, err
		}
		x.Min, x.Max, x.Mid = ds[0], ds[1], ds[2]
		return x, nil
	})
}

// DatedBoardLine là một dòng bảng giá (toàn tỉnh) kèm ngày và phiên bản.
type DatedBoardLine struct {
	Day     time.Time
	Version int
	BoardLineRow
}

// BoardLinesBetween đọc dòng bảng giá toàn tỉnh của MỌI phiên bản đã duyệt các ngày trong [from, to].
func BoardLinesBetween(ctx context.Context, tx pgx.Tx, profile string, from, to time.Time) ([]DatedBoardLine, error) {
	rows, err := tx.Query(ctx, `SELECT board_date, board_version, item_code, item_name, group_code, unit_code, price_min::text,
	    price_max::text, price_avg::text, points_reported FROM pf_price.board_line
	  WHERE profile = $1 AND board_date BETWEEN $2::date AND $3::date ORDER BY board_date, board_version, item_code`,
		profile, dateOnly(from), dateOnly(to))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DatedBoardLine, error) {
		var (
			x           DatedBoardLine
			lo, hi, avg string
		)
		if err := r.Scan(&x.Day, &x.Version, &x.Item, &x.ItemName, &x.GroupCode, &x.UnitCode, &lo, &hi, &avg, &x.Points); err != nil {
			return x, err
		}
		ds, err := decs(lo, hi, avg)
		if err != nil {
			return x, err
		}
		x.Min, x.Max, x.Avg = ds[0], ds[1], ds[2]
		return x, nil
	})
}

// ApprovedWeeks đọc khóa tuần lưu lúc duyệt của các phiên bản trong [from, to] — khóa "YYYY-MM-DD:vN".
func ApprovedWeeks(ctx context.Context, tx pgx.Tx, profile string, from, to time.Time) (map[string]string, error) {
	out := map[string]string{}
	rows, err := tx.Query(ctx, `SELECT to_char(board_date, 'YYYY-MM-DD') || ':v' || board_version::text, coalesce(week_key, '')
	  FROM pf_price.board WHERE profile = $1 AND action = 'approve' AND board_date BETWEEN $2::date AND $3::date`,
		profile, dateOnly(from), dateOnly(to))
	if err != nil {
		return nil, err
	}
	var k, w string
	_, err = pgx.ForEachRow(rows, []any{&k, &w}, func() error {
		out[k] = w
		return nil
	})
	return out, err
}

// LatestPublished trả ngày báo giá gần nhất ≤ day (inclusive) hoặc < day có bảng giá đã duyệt (phiên bản ≥ 1).
func LatestPublished(ctx context.Context, tx pgx.Tx, profile string, day time.Time, inclusive bool) (time.Time, bool, error) {
	op := "<"
	if inclusive {
		op = "<="
	}
	rows, err := tx.Query(ctx, `SELECT board_date FROM (
	    SELECT DISTINCT ON (board_date) board_date, board_version FROM pf_price.board
	     WHERE profile = $1 AND board_date `+op+` $2::date ORDER BY board_date, seq DESC) b
	  WHERE board_version >= 1 ORDER BY board_date DESC LIMIT 1`, profile, dateOnly(day))
	if err != nil {
		return time.Time{}, false, err
	}
	ds, err := pgx.CollectRows(rows, pgx.RowTo[time.Time])
	if err != nil || len(ds) == 0 {
		return time.Time{}, false, err
	}
	return ds[0], true, nil
}

// ---- khóa tháng thống kê phiếu (Q-085) ----

func monthKey(m time.Time) string { return m.Format("2006-01") }

// LockStatMonth khóa (tenant, bên dùng, tháng) đến hết giao dịch.
func LockStatMonth(ctx context.Context, tx pgx.Tx, tid uuid.UUID, profile string, month time.Time) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_price.stat_month:' || $1::text || ':' || $2 || ':' || $3, 0))`,
		tid, profile, monthKey(month))
	return err
}

// CurrentStatMonth đọc phiên bản hiện hành của khóa tháng (Seq = 0 nếu chưa khóa lần nào).
func CurrentStatMonth(ctx context.Context, tx pgx.Tx, profile string, month time.Time) (domain.StatMonth, error) {
	rows, err := tx.Query(ctx, `SELECT seq, status, stat_version, coalesce(reason, ''), coalesce(rows_count, 0), actor_ref, event_time
	  FROM pf_price.stat_month WHERE profile = $1 AND month_start = $2::date ORDER BY seq DESC LIMIT 1`, profile, dateOnly(month))
	if err != nil {
		return domain.StatMonth{}, err
	}
	ms, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.StatMonth, error) {
		var m domain.StatMonth
		err := r.Scan(&m.Seq, &m.Status, &m.Version, &m.Reason, &m.Rows, &m.Actor, &m.EventTime)
		return m, err
	})
	if err != nil || len(ms) == 0 {
		return domain.StatMonth{}, err
	}
	return ms[0], nil
}

// InsertStatMonth ghi một phiên bản khóa tháng (close: rows = số dòng ảnh chụp; reopen: reason bắt buộc).
func InsertStatMonth(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, profile string, month time.Time, action, status string, version int,
	reason string, rows *int, at time.Time, actor uuid.UUID, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.stat_month (id, tenant_id, stream_key, profile, month_start, action, status, stat_version,
	    reason, rows_count, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'statmonth:' || $3::text || ':' || to_char($4::date, 'YYYY-MM'), $3, $4::date, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		id, tid, profile, dateOnly(month), action, status, version, nullText(reason), rows, at.UTC(), actor, idem, domain.RuleVersion)
	return err
}

// InsertStatLine ghi một dòng ảnh chụp của phiên bản khóa version.
func InsertStatLine(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, profile string, month time.Time, version int, r domain.StatRow,
	at time.Time, actor uuid.UUID, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.stat_month_line (id, tenant_id, stream_key, profile, month_start, stat_version, officer_ref,
	    point_code, region_code, days_required, on_time, late, missed, deleted, adjusted_lines, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'statline:' || $3::text || ':' || to_char($4::date, 'YYYY-MM') || ':v' || $5::int::text || ':' || $6::uuid::text
	    || ':' || $7::text, $3, $4::date, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		id, tid, profile, dateOnly(month), version, r.Officer, r.Point, r.Region, r.Required, r.OnTime, r.Late, r.Missed, r.Deleted,
		r.AdjustedLines, at.UTC(), actor, idem, domain.RuleVersion)
	return err
}

// LoadStatLines đọc ảnh chụp của phiên bản khóa version.
func LoadStatLines(ctx context.Context, tx pgx.Tx, profile string, month time.Time, version int) ([]domain.StatRow, error) {
	rows, err := tx.Query(ctx, `SELECT officer_ref, point_code, region_code, days_required, on_time, late, missed, deleted, adjusted_lines
	  FROM pf_price.stat_month_line WHERE profile = $1 AND month_start = $2::date AND stat_version = $3 ORDER BY point_code, officer_ref`,
		profile, dateOnly(month), version)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.StatRow, error) {
		var x domain.StatRow
		err := r.Scan(&x.Officer, &x.Point, &x.Region, &x.Required, &x.OnTime, &x.Late, &x.Missed, &x.Deleted, &x.AdjustedLines)
		return x, err
	})
}
