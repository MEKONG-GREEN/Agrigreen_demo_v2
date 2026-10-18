package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// Level của đơn vị xem dashboard.
const (
	LevelNational = "national"
	LevelProvince = "province"
	LevelCommune  = "commune"
)

// CellKey là một ô (tỉnh, xã) của dữ liệu tổng hợp.
type CellKey struct{ Province, Commune string }

// Cell là số liệu của một ô.
type Cell struct {
	SownHa       decimal.Decimal
	Cooperatives int64
	OpenTasks    int64
	Overdue      int64
	Targets      int64
	Acks         int64
	Reported     bool // xã có ít nhất một báo cáo hiện hành trong kỳ
	Expected     bool // xã có cán bộ phụ trách → phải báo cáo
}

// DashQuery là phạm vi tổng hợp.
type DashQuery struct {
	Level  string // national | province | commune
	Unit   string // mã tỉnh/xã; rỗng với national
	Period string
	From   time.Time // đầu kỳ (giờ Việt Nam) — lọc chỉ đạo theo thời điểm ban hành
	To     time.Time
	Now    time.Time
}

// filter trả điều kiện theo cấp; cột tỉnh là p, cột xã là c. Tham số $1 = unit.
func (q DashQuery) filter(p, c string) string {
	switch q.Level {
	case LevelProvince:
		return p + " = $1"
	case LevelCommune:
		return c + " = $1"
	}
	return "($1 = '' OR true)"
}

// Cells tính số liệu theo từng (tỉnh, xã) trong phạm vi. Mọi tổng ở tầng trên được
// cộng từ các ô này, nên toàn quốc luôn bằng tổng các tỉnh (US-DASH-01 AC-2).
func Cells(ctx context.Context, tx pgx.Tx, q DashQuery) (map[CellKey]*Cell, error) {
	out := map[CellKey]*Cell{}
	cell := func(p, c string) *Cell {
		k := CellKey{p, c}
		if out[k] == nil {
			out[k] = &Cell{SownHa: decimal.Zero}
		}
		return out[k]
	}

	// Báo cáo gieo sạ hiện hành của kỳ: diện tích, số HTX có báo cáo.
	rows, err := tx.Query(ctx, `SELECT province_code, commune_code, coalesce(sum(sown_area_ha), 0)::text,
	    count(DISTINCT cooperative_ref)
	  FROM pf_extension.field_report_current
	  WHERE `+q.filter("province_code", "commune_code")+` AND period = $2 AND report_kind = 'sowing_progress'
	  GROUP BY 1, 2`, q.Unit, q.Period)
	if err != nil {
		return nil, err
	}
	if err := scanEach(rows, func(r pgx.Rows) error {
		var p, c, sum string
		var coops int64
		if err := r.Scan(&p, &c, &sum, &coops); err != nil {
			return err
		}
		d, err := decimal.NewFromString(sum)
		if err != nil {
			return err
		}
		x := cell(p, c)
		x.SownHa, x.Cooperatives = d, coops
		return nil
	}); err != nil {
		return nil, err
	}

	// Xã đã báo cáo (mọi loại) trong kỳ.
	rows, err = tx.Query(ctx, `SELECT DISTINCT province_code, commune_code FROM pf_extension.field_report_current
	  WHERE `+q.filter("province_code", "commune_code")+` AND period = $2`, q.Unit, q.Period)
	if err != nil {
		return nil, err
	}
	if err := scanEach(rows, func(r pgx.Rows) error {
		var p, c string
		if err := r.Scan(&p, &c); err != nil {
			return err
		}
		cell(p, c).Reported = true
		return nil
	}); err != nil {
		return nil, err
	}

	// Xã phải báo cáo: có cán bộ cấp xã đang hoạt động trong danh bạ.
	rows, err = tx.Query(ctx, `SELECT DISTINCT province_code, unit_code FROM pf_extension.staff
	  WHERE active AND unit_code <> province_code AND `+q.filter("province_code", "unit_code"), q.Unit)
	if err != nil {
		return nil, err
	}
	if err := scanEach(rows, func(r pgx.Rows) error {
		var p, c string
		if err := r.Scan(&p, &c); err != nil {
			return err
		}
		cell(p, c).Expected = true
		return nil
	}); err != nil {
		return nil, err
	}

	// Nhiệm vụ đang mở / quá hạn (ảnh chụp hiện tại).
	rows, err = tx.Query(ctx, `SELECT province_code, commune_code,
	    count(*) FILTER (WHERE status <> 'resolved'),
	    count(*) FILTER (WHERE status <> 'resolved' AND due_at < $2)
	  FROM pf_extension.task_current WHERE `+q.filter("province_code", "commune_code")+` GROUP BY 1, 2`, q.Unit, q.Now.UTC())
	if err != nil {
		return nil, err
	}
	if err := scanEach(rows, func(r pgx.Rows) error {
		var p, c string
		var open, overdue int64
		if err := r.Scan(&p, &c, &open, &overdue); err != nil {
			return err
		}
		x := cell(p, c)
		x.OpenTasks, x.Overdue = open, overdue
		return nil
	}); err != nil {
		return nil, err
	}

	// Tỷ lệ xác nhận đã đọc của chỉ đạo ban hành trong kỳ.
	rows, err = tx.Query(ctx, `SELECT t.province_code, t.unit_code, count(*), count(a.actor_ref)
	  FROM pf_extension.directive_target t
	  LEFT JOIN pf_extension.directive_ack a ON a.directive_id = t.directive_id AND a.actor_ref = t.actor_ref
	  WHERE `+q.filter("t.province_code", "t.unit_code")+` AND t.issued_at >= $2 AND t.issued_at < $3
	  GROUP BY 1, 2`, q.Unit, q.From.UTC(), q.To.UTC())
	if err != nil {
		return nil, err
	}
	if err := scanEach(rows, func(r pgx.Rows) error {
		var p, c string
		var targets, acks int64
		if err := r.Scan(&p, &c, &targets, &acks); err != nil {
			return err
		}
		x := cell(p, c)
		x.Targets, x.Acks = targets, acks
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func scanEach(rows pgx.Rows, fn func(pgx.Rows) error) error {
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
