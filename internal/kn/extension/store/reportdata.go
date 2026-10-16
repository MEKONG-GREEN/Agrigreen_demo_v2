package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/extensionsrc"
)

// DataFilter là phạm vi đọc dữ liệu báo cáo (pf/reports đã giải phạm vi).
// Tham số $1 = All, $2 = Units, $3 = Unit.
type DataFilter struct {
	All   bool
	Units []string
	Unit  string
}

func (f DataFilter) args() []any { return []any{f.All, units(f.Units), f.Unit} }

// where trả điều kiện phạm vi + đơn vị cho cột tỉnh p và xã c.
func where(p, c string) string {
	return "($1::boolean OR " + c + " = ANY($2::text[]) OR " + p + " = ANY($2::text[])) AND ($3 = '' OR " + p + " = $3 OR " + c + " = $3)"
}

func decOf(s string) (decimal.Decimal, error) { return decimal.NewFromString(s) }

// SowingRows tổng hợp diện tích đã sạ hiện hành theo xã × vụ × giống của các kỳ.
func SowingRows(ctx context.Context, tx pgx.Tx, f DataFilter, periods []string) ([]extensionsrc.SowingRow, error) {
	rows, err := tx.Query(ctx, `SELECT c.province_code, c.commune_code, coalesce(r.season_code, ''), coalesce(r.variety_code, ''),
	    coalesce(sum(c.sown_area_ha), 0)::text, count(DISTINCT c.cooperative_ref), count(*)
	  FROM pf_extension.field_report_current c JOIN pf_extension.field_report r ON r.id = c.report_id
	  WHERE `+where("c.province_code", "c.commune_code")+` AND c.period = ANY($4::text[]) AND c.report_kind = 'sowing_progress'
	  GROUP BY 1, 2, 3, 4 ORDER BY 1, 2, 3, 4`, append(f.args(), periods)...)
	if err != nil {
		return nil, err
	}
	var out []extensionsrc.SowingRow
	err = scanEach(rows, func(r pgx.Rows) error {
		var x extensionsrc.SowingRow
		var sum string
		if err := r.Scan(&x.ProvinceCode, &x.CommuneCode, &x.SeasonCode, &x.VarietyCode, &sum, &x.Cooperatives, &x.Reports); err != nil {
			return err
		}
		var err error
		x.SownAreaHa, err = decOf(sum)
		out = append(out, x)
		return err
	})
	return out, err
}

// PestRows tổng hợp quan sát sâu bệnh hiện hành theo xã × loại × mức độ.
func PestRows(ctx context.Context, tx pgx.Tx, f DataFilter, periods []string) ([]extensionsrc.PestRow, error) {
	rows, err := tx.Query(ctx, `SELECT c.province_code, c.commune_code, coalesce(r.pest_code, ''), coalesce(r.severity, ''),
	    coalesce(sum(c.affected_area_ha), 0)::text, count(*), count(*) FILTER (WHERE r.urgent)
	  FROM pf_extension.field_report_current c JOIN pf_extension.field_report r ON r.id = c.report_id
	  WHERE `+where("c.province_code", "c.commune_code")+` AND c.period = ANY($4::text[]) AND c.report_kind = 'growth_pest'
	  GROUP BY 1, 2, 3, 4 ORDER BY 1, 2, 3, 4`, append(f.args(), periods)...)
	if err != nil {
		return nil, err
	}
	var out []extensionsrc.PestRow
	err = scanEach(rows, func(r pgx.Rows) error {
		var x extensionsrc.PestRow
		var sum string
		if err := r.Scan(&x.ProvinceCode, &x.CommuneCode, &x.PestCode, &x.Severity, &sum, &x.Observations, &x.Urgent); err != nil {
			return err
		}
		var err error
		x.AffectedAreaHa, err = decOf(sum)
		out = append(out, x)
		return err
	})
	return out, err
}

// ProgramRows đọc tiến độ Đề án hiện hành theo HTX × vụ.
func ProgramRows(ctx context.Context, tx pgx.Tx, f DataFilter, season string, year int) ([]extensionsrc.ProgramRow, error) {
	rows, err := tx.Query(ctx, `SELECT province_code, commune_code, cooperative_ref, season_code, season_year,
	    registered_area_ha::text, deploying_area_ha::text, completed_area_ha::text, coalesce(approved_report_id = report_id, false)
	  FROM pf_extension.program_report_current
	  WHERE `+where("province_code", "commune_code")+` AND ($4 = '' OR season_code = $4) AND ($5 = 0 OR season_year = $5)
	  ORDER BY province_code, commune_code, cooperative_ref, season_year, season_code`, append(f.args(), season, year)...)
	if err != nil {
		return nil, err
	}
	var out []extensionsrc.ProgramRow
	err = scanEach(rows, func(r pgx.Rows) error {
		var x extensionsrc.ProgramRow
		var reg, dep, comp string
		if err := r.Scan(&x.ProvinceCode, &x.CommuneCode, &x.CooperativeRef, &x.SeasonCode, &x.SeasonYear, &reg, &dep, &comp, &x.Approved); err != nil {
			return err
		}
		var err error
		if x.RegisteredHa, err = decOf(reg); err != nil {
			return err
		}
		if x.DeployingHa, err = decOf(dep); err != nil {
			return err
		}
		x.CompletedHa, err = decOf(comp)
		out = append(out, x)
		return err
	})
	return out, err
}

// TaskRows đếm nhiệm vụ theo xã trong [from, to); đang mở/quá hạn tính tại at.
func TaskRows(ctx context.Context, tx pgx.Tx, f DataFilter, from, to, at time.Time) ([]extensionsrc.TaskRow, error) {
	rows, err := tx.Query(ctx, `SELECT province_code, commune_code,
	    count(*) FILTER (WHERE created_at >= $4 AND created_at < $5),
	    count(*) FILTER (WHERE resolved_at >= $4 AND resolved_at < $5),
	    count(*) FILTER (WHERE resolved_at >= $4 AND resolved_at < $5 AND resolved_at <= due_at),
	    count(*) FILTER (WHERE escalated_at >= $4 AND escalated_at < $5),
	    count(*) FILTER (WHERE status <> 'resolved'),
	    count(*) FILTER (WHERE status <> 'resolved' AND due_at < $6)
	  FROM pf_extension.task_current WHERE `+where("province_code", "commune_code")+`
	  GROUP BY 1, 2 ORDER BY 1, 2`, append(f.args(), from.UTC(), to.UTC(), at.UTC())...)
	if err != nil {
		return nil, err
	}
	var out []extensionsrc.TaskRow
	err = scanEach(rows, func(r pgx.Rows) error {
		var x extensionsrc.TaskRow
		if err := r.Scan(&x.ProvinceCode, &x.CommuneCode, &x.Opened, &x.Resolved, &x.OnTime, &x.Escalated, &x.Open, &x.Overdue); err != nil {
			return err
		}
		out = append(out, x)
		return nil
	})
	return out, err
}

// UnitPair là một (tỉnh, xã).
type UnitPair struct{ Province, Commune string }

// ExpectedCommunes trả các xã phải báo cáo (có cán bộ cấp xã đang hoạt động) trong phạm vi.
func ExpectedCommunes(ctx context.Context, tx pgx.Tx, f DataFilter) ([]UnitPair, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT province_code, unit_code FROM pf_extension.staff
	  WHERE active AND unit_code <> province_code AND `+where("province_code", "unit_code")+` ORDER BY 1, 2`, f.args()...)
	if err != nil {
		return nil, err
	}
	return pairs(rows)
}

// ReportedCommunes trả các xã đã có báo cáo hiện hành (mọi loại) trong các kỳ.
func ReportedCommunes(ctx context.Context, tx pgx.Tx, f DataFilter, periods []string) ([]UnitPair, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT province_code, commune_code FROM pf_extension.field_report_current
	  WHERE `+where("province_code", "commune_code")+` AND period = ANY($4::text[]) ORDER BY 1, 2`, append(f.args(), periods)...)
	if err != nil {
		return nil, err
	}
	return pairs(rows)
}

// FullyClosedProvinces trả các tỉnh đã chốt MỌI kỳ trong periods.
func FullyClosedProvinces(ctx context.Context, tx pgx.Tx, periods []string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT province_code FROM pf_extension.period_close WHERE period = ANY($1::text[])
	  GROUP BY 1 HAVING count(DISTINCT period) = cardinality($1::text[])`, periods)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	err = scanEach(rows, func(r pgx.Rows) error {
		var p string
		if err := r.Scan(&p); err != nil {
			return err
		}
		out[p] = true
		return nil
	})
	return out, err
}

func pairs(rows pgx.Rows) ([]UnitPair, error) {
	var out []UnitPair
	err := scanEach(rows, func(r pgx.Rows) error {
		var p UnitPair
		if err := r.Scan(&p.Province, &p.Commune); err != nil {
			return err
		}
		out = append(out, p)
		return nil
	})
	return out, err
}
