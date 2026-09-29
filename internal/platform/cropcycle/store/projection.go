package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// CoopArea là một dòng coop_season_area.
type CoopArea struct {
	CooperativeRef   uuid.UUID
	SeasonCode       string
	SeasonYear       int
	AreaHa           decimal.Decimal
	Seasons          int
	OpenSeasons      int
	CompletedSeasons int
	UpdatedAt        time.Time
}

// RefreshCoopArea tính lại Σ diện tích vụ đang mở + đã hoàn thành của (HTX, vụ,
// năm) từ season_current (cùng giao dịch, R-21) và trả dòng mới cùng cờ đã đổi.
func RefreshCoopArea(ctx context.Context, tx pgx.Tx, tid, coop uuid.UUID, season string, year int) (CoopArea, bool, error) {
	var before *string
	err := tx.QueryRow(ctx, `SELECT area_ha::text || '/' || seasons || '/' || open_seasons || '/' || completed_seasons
	  FROM pf_cropcycle.coop_season_area WHERE cooperative_ref = $1::uuid AND season_code = $2::text AND season_year = $3::int FOR UPDATE`,
		coop, season, year).Scan(&before)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return CoopArea{}, false, err
	}
	var a CoopArea
	var area string
	err = tx.QueryRow(ctx, `INSERT INTO pf_cropcycle.coop_season_area
	    (tenant_id, cooperative_ref, season_code, season_year, area_ha, seasons, open_seasons, completed_seasons)
	  SELECT $1::uuid, $2::uuid, $3::text, $4::int,
	         coalesce(sum(area_ha) FILTER (WHERE status IN ('open', 'completed')), 0),
	         count(*) FILTER (WHERE status IN ('open', 'completed')),
	         count(*) FILTER (WHERE status = 'open'),
	         count(*) FILTER (WHERE status = 'completed')
	  FROM pf_cropcycle.season_current WHERE cooperative_ref = $2::uuid AND season_code = $3::text AND season_year = $4::int
	  ON CONFLICT (tenant_id, cooperative_ref, season_code, season_year) DO UPDATE SET
	    area_ha = EXCLUDED.area_ha, seasons = EXCLUDED.seasons, open_seasons = EXCLUDED.open_seasons,
	    completed_seasons = EXCLUDED.completed_seasons, updated_at = now()
	  RETURNING area_ha::text, seasons, open_seasons, completed_seasons, updated_at`,
		tid, coop, season, year).Scan(&area, &a.Seasons, &a.OpenSeasons, &a.CompletedSeasons, &a.UpdatedAt)
	if err != nil {
		return a, false, err
	}
	a.CooperativeRef, a.SeasonCode, a.SeasonYear = coop, season, year
	if a.AreaHa, err = decimal.NewFromString(area); err != nil {
		return a, false, err
	}
	after := area + "/" + itoa(a.Seasons) + "/" + itoa(a.OpenSeasons) + "/" + itoa(a.CompletedSeasons)
	return a, before == nil || *before != after, nil
}

// SeasonAreas đọc coop_season_area của HTX (season rỗng / year 0 = mọi).
func SeasonAreas(ctx context.Context, tx pgx.Tx, coop uuid.UUID, season string, year int) ([]CoopArea, error) {
	rows, err := tx.Query(ctx, `SELECT cooperative_ref, season_code, season_year, area_ha::text, seasons, open_seasons, completed_seasons, updated_at
	  FROM pf_cropcycle.coop_season_area
	  WHERE cooperative_ref = $1::uuid AND ($2::text = '' OR season_code = $2::text) AND ($3::int = 0 OR season_year = $3::int)
	  ORDER BY season_year, season_code`, coop, season, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CoopArea
	for rows.Next() {
		var a CoopArea
		var area string
		if err := rows.Scan(&a.CooperativeRef, &a.SeasonCode, &a.SeasonYear, &area, &a.Seasons, &a.OpenSeasons, &a.CompletedSeasons, &a.UpdatedAt); err != nil {
			return nil, err
		}
		if a.AreaHa, err = decimal.NewFromString(area); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RefreshYield tính lại yield_summary của một vụ từ season_current, SOP đã ghim,
// nhật ký ĐÃ DUYỆT (HTX US-LOG-05 AC-2) và khai báo sản lượng hiện hành.
func RefreshYield(ctx context.Context, tx pgx.Tx, season uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_cropcycle.yield_summary
	    (tenant_id, season_ref, plot_ref, household_ref, cooperative_ref, commune_code, season_code, season_year, variety_code,
	     status, area_ha, expected_yield_t_per_ha, expected_kg, harvest_id, actual_kg, actual_yield_t_per_ha, harvest_date,
	     sop_steps, sop_mandatory_steps, sop_steps_done, sop_mandatory_done, approved_logs, pending_logs)
	  SELECT s.tenant_id, s.season_ref, s.plot_ref, s.household_ref, s.cooperative_ref, s.commune_code, s.season_code, s.season_year,
	         s.variety_code, s.status, s.area_ha, s.expected_yield_t_per_ha,
	         CASE WHEN s.expected_yield_t_per_ha IS NULL THEN NULL ELSE round(s.area_ha * s.expected_yield_t_per_ha * 1000, 3) END,
	         s.harvest_id, h.mass_kg, h.yield_t_per_ha, h.harvest_date,
	         v.step_count,
	         (SELECT count(*) FROM jsonb_array_elements(v.steps) e WHERE (e->>'mandatory')::boolean),
	         (SELECT count(DISTINCT (e->>'seq')::int) FROM jsonb_array_elements(v.steps) e
	            WHERE EXISTS (SELECT 1 FROM pf_cropcycle.farm_log_current l WHERE l.season_ref = s.season_ref
	                            AND l.status = 'approved' AND l.sop_step_seq = (e->>'seq')::int)),
	         (SELECT count(DISTINCT (e->>'seq')::int) FROM jsonb_array_elements(v.steps) e
	            WHERE (e->>'mandatory')::boolean AND EXISTS (SELECT 1 FROM pf_cropcycle.farm_log_current l
	                            WHERE l.season_ref = s.season_ref AND l.status = 'approved' AND l.sop_step_seq = (e->>'seq')::int)),
	         (SELECT count(*) FROM pf_cropcycle.farm_log_current l WHERE l.season_ref = s.season_ref AND l.status = 'approved'),
	         (SELECT count(*) FROM pf_cropcycle.farm_log_current l WHERE l.season_ref = s.season_ref AND l.status = 'pending')
	  FROM pf_cropcycle.season_current s
	  JOIN pf_cropcycle.sop_version v ON v.tenant_id = s.tenant_id AND v.id = s.sop_version_id
	  LEFT JOIN pf_cropcycle.harvest_declaration h ON h.tenant_id = s.tenant_id AND h.id = s.harvest_id
	  WHERE s.season_ref = $1::uuid
	  ON CONFLICT (tenant_id, season_ref) DO UPDATE SET
	    plot_ref = EXCLUDED.plot_ref, household_ref = EXCLUDED.household_ref, cooperative_ref = EXCLUDED.cooperative_ref,
	    commune_code = EXCLUDED.commune_code, season_code = EXCLUDED.season_code, season_year = EXCLUDED.season_year,
	    variety_code = EXCLUDED.variety_code, status = EXCLUDED.status, area_ha = EXCLUDED.area_ha,
	    expected_yield_t_per_ha = EXCLUDED.expected_yield_t_per_ha, expected_kg = EXCLUDED.expected_kg,
	    harvest_id = EXCLUDED.harvest_id, actual_kg = EXCLUDED.actual_kg, actual_yield_t_per_ha = EXCLUDED.actual_yield_t_per_ha,
	    harvest_date = EXCLUDED.harvest_date, sop_steps = EXCLUDED.sop_steps, sop_mandatory_steps = EXCLUDED.sop_mandatory_steps,
	    sop_steps_done = EXCLUDED.sop_steps_done, sop_mandatory_done = EXCLUDED.sop_mandatory_done,
	    approved_logs = EXCLUDED.approved_logs, pending_logs = EXCLUDED.pending_logs, updated_at = now()`, season)
	return err
}

// YieldRow là một dòng yield_summary.
type YieldRow struct {
	SeasonRef         uuid.UUID
	Status            string
	AreaHa            decimal.Decimal
	ExpectedKg        *decimal.Decimal
	ActualKg          *decimal.Decimal
	ActualYield       *decimal.Decimal
	SOPSteps          int
	SOPMandatory      int
	SOPStepsDone      int
	SOPMandatoryDone  int
	ApprovedLogs      int
	PendingLogs       int
	ExpectedYieldTPHa *decimal.Decimal
}

// YieldOf đọc yield_summary của một vụ.
func YieldOf(ctx context.Context, tx pgx.Tx, season uuid.UUID) (*YieldRow, error) {
	var y YieldRow
	var area string
	var exp, act, ay, ey *string
	err := tx.QueryRow(ctx, `SELECT season_ref, status, area_ha::text, expected_kg::text, actual_kg::text, actual_yield_t_per_ha::text,
	    sop_steps, sop_mandatory_steps, sop_steps_done, sop_mandatory_done, approved_logs, pending_logs, expected_yield_t_per_ha::text
	  FROM pf_cropcycle.yield_summary WHERE season_ref = $1::uuid`, season).Scan(&y.SeasonRef, &y.Status, &area, &exp, &act, &ay,
		&y.SOPSteps, &y.SOPMandatory, &y.SOPStepsDone, &y.SOPMandatoryDone, &y.ApprovedLogs, &y.PendingLogs, &ey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if y.AreaHa, err = decimal.NewFromString(area); err != nil {
		return nil, err
	}
	for _, p := range []struct {
		s *string
		d **decimal.Decimal
	}{{exp, &y.ExpectedKg}, {act, &y.ActualKg}, {ay, &y.ActualYield}, {ey, &y.ExpectedYieldTPHa}} {
		if *p.d, err = parseDecPtr(p.s); err != nil {
			return nil, err
		}
	}
	return &y, nil
}

// Activity là số vụ mở và nhật ký chờ duyệt theo HTX.
type Activity struct {
	CooperativeRef uuid.UUID
	OpenSeasons    int64
	PendingLogs    int64
}

// ActivityByCooperative đếm vụ mở / nhật ký chờ duyệt (coops rỗng + all = mọi HTX).
func ActivityByCooperative(ctx context.Context, tx pgx.Tx, all bool, coops []uuid.UUID) ([]Activity, error) {
	rows, err := tx.Query(ctx, `WITH s AS (
	    SELECT cooperative_ref, count(*) AS n FROM pf_cropcycle.season_current
	    WHERE status = 'open' AND ($1::boolean OR cooperative_ref = ANY($2::uuid[])) GROUP BY cooperative_ref),
	  l AS (
	    SELECT cooperative_ref, count(*) AS n FROM pf_cropcycle.farm_log_current
	    WHERE status = 'pending' AND ($1::boolean OR cooperative_ref = ANY($2::uuid[])) GROUP BY cooperative_ref)
	  SELECT coalesce(s.cooperative_ref, l.cooperative_ref), coalesce(s.n, 0), coalesce(l.n, 0)
	  FROM s FULL JOIN l ON l.cooperative_ref = s.cooperative_ref`, all, uuids(coops))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Activity
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.CooperativeRef, &a.OpenSeasons, &a.PendingLogs); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DashFilter lọc bảng điều hành (HTX US-DASH-01).
type DashFilter struct {
	Scope          Filter
	CooperativeRef uuid.UUID
	SeasonCode     string
	SeasonYear     int
	VarietyCode    string
}

// DashGroup là một nhóm tổng hợp của bảng điều hành.
type DashGroup struct {
	Key              string // mã giống / "<vụ>:<năm>" / cooperative_ref; rỗng = tổng
	Seasons          int64
	OpenSeasons      int64
	CompletedSeasons int64
	Households       int64
	AreaHa           decimal.Decimal
	ExpectedKg       decimal.Decimal
	ActualKg         decimal.Decimal
	HarvestedAreaHa  decimal.Decimal
	SOPSteps         int64
	SOPStepsDone     int64
	ApprovedLogs     int64
	PendingLogs      int64
}

// Dashboard tổng hợp yield_summary theo nhóm: group = "" (tổng), "variety",
// "season", "cooperative". Vụ bị hủy không tính.
func Dashboard(ctx context.Context, tx pgx.Tx, f DashFilter, group string) ([]DashGroup, error) {
	key := "''"
	switch group {
	case "variety":
		key = "y.variety_code"
	case "season":
		key = "y.season_code || ':' || y.season_year"
	case "cooperative":
		key = "y.cooperative_ref::text"
	}
	args := append(f.Scope.args(), nilIfZero(f.CooperativeRef), f.SeasonCode, f.SeasonYear, f.VarietyCode)
	rows, err := tx.Query(ctx, `SELECT `+key+` AS k, count(*), count(*) FILTER (WHERE y.status = 'open'),
	    count(*) FILTER (WHERE y.status = 'completed'), count(DISTINCT y.household_ref),
	    coalesce(sum(y.area_ha), 0)::text, coalesce(sum(y.expected_kg), 0)::text, coalesce(sum(y.actual_kg), 0)::text,
	    coalesce(sum(y.area_ha) FILTER (WHERE y.actual_kg IS NOT NULL), 0)::text,
	    coalesce(sum(y.sop_steps), 0), coalesce(sum(y.sop_steps_done), 0), coalesce(sum(y.approved_logs), 0), coalesce(sum(y.pending_logs), 0)
	  FROM pf_cropcycle.yield_summary y
	  WHERE y.status <> 'cancelled' AND `+scopeSQL("y", 1)+`
	    AND ($5::uuid IS NULL OR y.cooperative_ref = $5::uuid) AND ($6::text = '' OR y.season_code = $6::text)
	    AND ($7::int = 0 OR y.season_year = $7::int) AND ($8::text = '' OR y.variety_code = $8::text)
	  GROUP BY k ORDER BY k`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DashGroup
	for rows.Next() {
		var g DashGroup
		var area, exp, act, harv string
		if err := rows.Scan(&g.Key, &g.Seasons, &g.OpenSeasons, &g.CompletedSeasons, &g.Households, &area, &exp, &act, &harv,
			&g.SOPSteps, &g.SOPStepsDone, &g.ApprovedLogs, &g.PendingLogs); err != nil {
			return nil, err
		}
		for _, p := range []struct {
			s string
			d *decimal.Decimal
		}{{area, &g.AreaHa}, {exp, &g.ExpectedKg}, {act, &g.ActualKg}, {harv, &g.HarvestedAreaHa}} {
			if *p.d, err = decimal.NewFromString(p.s); err != nil {
				return nil, err
			}
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ClearProjections xóa mọi bảng chiếu của tenant trước khi dựng lại từ sổ (R-22).
// Bảng chiếu không phải sổ; sổ không bị chạm.
func ClearProjections(ctx context.Context, tx pgx.Tx) error {
	for _, t := range []string{"yield_summary", "coop_season_area", "farm_log_current", "season_current"} {
		if _, err := tx.Exec(ctx, "DELETE FROM pf_cropcycle."+t); err != nil {
			return err
		}
	}
	return nil
}

// CoopSeasonKeys trả mọi (HTX, vụ, năm) có vụ trong season_current.
func CoopSeasonKeys(ctx context.Context, tx pgx.Tx) ([]CoopArea, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT cooperative_ref, season_code, season_year FROM pf_cropcycle.season_current`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CoopArea
	for rows.Next() {
		var a CoopArea
		if err := rows.Scan(&a.CooperativeRef, &a.SeasonCode, &a.SeasonYear); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AllSeasonRefs trả mọi season_ref trong season_current.
func AllSeasonRefs(ctx context.Context, tx pgx.Tx) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT season_ref FROM pf_cropcycle.season_current ORDER BY season_ref`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var u uuid.UUID
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
