package store

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ProductionGroup là một dòng báo cáo sản lượng HTX (HTX US-DASH-02): chiều phân
// nhóm không chọn để trống (CooperativeRef = uuid.Nil, SeasonCode = "", SeasonYear =
// 0, VarietyCode = ""). Số hộ đếm phân biệt TRONG nhóm nên không cộng được giữa các nhóm.
type ProductionGroup struct {
	CooperativeRef uuid.UUID
	SeasonCode     string
	SeasonYear     int
	VarietyCode    string
	DashGroup
}

// ProductionGroups tổng hợp yield_summary (vụ chưa hủy) theo các chiều đã chọn
// (HTX / vụ + năm / giống) với cùng bộ lọc và phạm vi của bảng điều hành
// (Dashboard): cùng một nguồn nên tệp xuất khớp số trên màn hình (US-DASH-02 AC-1).
func ProductionGroups(ctx context.Context, tx pgx.Tx, f DashFilter, byCoop, bySeason, byVariety bool) ([]ProductionGroup, error) {
	cols := []string{"NULL::uuid", "''::text", "0::int", "''::text"}
	var group []string
	if byCoop {
		cols[0] = "y.cooperative_ref"
		group = append(group, "y.cooperative_ref")
	}
	if bySeason {
		cols[1], cols[2] = "y.season_code", "y.season_year"
		group = append(group, "y.season_year", "y.season_code")
	}
	if byVariety {
		cols[3] = "y.variety_code"
		group = append(group, "y.variety_code")
	}
	groupBy := ""
	if len(group) > 0 {
		groupBy = " GROUP BY " + strings.Join(group, ", ") + " ORDER BY " + strings.Join(group, ", ")
	}
	args := append(f.Scope.args(), nilIfZero(f.CooperativeRef), f.SeasonCode, f.SeasonYear, f.VarietyCode)
	rows, err := tx.Query(ctx, `SELECT `+strings.Join(cols, ", ")+`, count(*), count(*) FILTER (WHERE y.status = 'open'),
	    count(*) FILTER (WHERE y.status = 'completed'), count(DISTINCT y.household_ref),
	    coalesce(sum(y.area_ha), 0)::text, coalesce(sum(y.expected_kg), 0)::text, coalesce(sum(y.actual_kg), 0)::text,
	    coalesce(sum(y.area_ha) FILTER (WHERE y.actual_kg IS NOT NULL), 0)::text,
	    coalesce(sum(y.sop_steps), 0), coalesce(sum(y.sop_steps_done), 0), coalesce(sum(y.approved_logs), 0), coalesce(sum(y.pending_logs), 0)
	  FROM pf_cropcycle.yield_summary y
	  WHERE y.status <> 'cancelled' AND `+scopeSQL("y", 1)+`
	    AND ($5::uuid IS NULL OR y.cooperative_ref = $5::uuid) AND ($6::text = '' OR y.season_code = $6::text)
	    AND ($7::int = 0 OR y.season_year = $7::int) AND ($8::text = '' OR y.variety_code = $8::text)`+groupBy, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProductionGroup
	for rows.Next() {
		var g ProductionGroup
		var coop *uuid.UUID
		var area, exp, act, harv string
		if err := rows.Scan(&coop, &g.SeasonCode, &g.SeasonYear, &g.VarietyCode, &g.Seasons, &g.OpenSeasons, &g.CompletedSeasons,
			&g.Households, &area, &exp, &act, &harv, &g.SOPSteps, &g.SOPStepsDone, &g.ApprovedLogs, &g.PendingLogs); err != nil {
			return nil, err
		}
		g.CooperativeRef = deref(coop)
		for _, p := range []struct {
			s string
			d *decimal.Decimal
		}{{area, &g.AreaHa}, {exp, &g.ExpectedKg}, {act, &g.ActualKg}, {harv, &g.HarvestedAreaHa}} {
			if *p.d, err = decimal.NewFromString(p.s); err != nil {
				return nil, err
			}
		}
		// Không nhóm (tổng) trên tập rỗng vẫn trả một dòng số 0 — bỏ để "không có dữ liệu" rõ ràng.
		if len(group) == 0 && g.Seasons == 0 {
			continue
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
