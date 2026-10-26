package htxread

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/cropcyclesrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var (
	_                     cropcyclesrc.ReportData = (*Reader)(nil)
	productionSeasonCode                          = regexp.MustCompile(`^[A-Za-z0-9_]{1,40}$`)
	productionVarietyCode                         = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)
	errProductionQuery                            = errors.New("extension: invalid published production query")
)

// Production reads published production facts under the authenticated/job tenant.
// The query cannot select another tenant; sharing must be authorized separately.
func (r *Reader) Production(ctx context.Context, q cropcyclesrc.ProductionQuery) (cropcyclesrc.ProductionData, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return cropcyclesrc.ProductionData{}, err
	}
	if tid != q.TenantID || (q.CoopOnly && q.CooperativeRef == uuid.Nil) ||
		(q.SeasonCode != "" && !productionSeasonCode.MatchString(q.SeasonCode)) ||
		(q.VarietyCode != "" && !productionVarietyCode.MatchString(q.VarietyCode)) {
		return cropcyclesrc.ProductionData{}, errProductionQuery
	}
	out := cropcyclesrc.ProductionData{Rows: []cropcyclesrc.ProductionRow{}}
	err = r.read(ctx, func(tx pgx.Tx) error {
		rows, e := productionGroups(ctx, tx, q)
		if e != nil {
			return e
		}
		out.Rows = rows
		// Distinct households and ratios cannot be added across groups.
		totalQuery := q
		totalQuery.ByCooperative, totalQuery.BySeason, totalQuery.ByVariety = false, false, false
		totals, e := productionGroups(ctx, tx, totalQuery)
		if e != nil {
			return e
		}
		if len(totals) > 0 {
			out.Totals = totals[0].ProductionFigures
		}
		return nil
	})
	if err != nil {
		return cropcyclesrc.ProductionData{}, err
	}
	return out, nil
}

func productionGroups(ctx context.Context, tx pgx.Tx, q cropcyclesrc.ProductionQuery) ([]cropcyclesrc.ProductionRow, error) {
	cols := []string{"NULL::uuid", "''::text", "0::int", "''::text"}
	var group []string
	if q.ByCooperative {
		cols[0] = "production_cooperative_ref"
		group = append(group, cols[0])
	}
	if q.BySeason {
		cols[1], cols[2] = "production_season_code", "production_season_year"
		group = append(group, cols[2], cols[1])
	}
	if q.ByVariety {
		cols[3] = "production_variety_code"
		group = append(group, cols[3])
	}
	suffix := ""
	if len(group) > 0 {
		suffix = " GROUP BY " + strings.Join(group, ",") + " ORDER BY " + strings.Join(group, ",")
	}
	var coop *uuid.UUID
	if q.CooperativeRef != uuid.Nil {
		coop = &q.CooperativeRef
	}
	rows, err := tx.Query(ctx, "SELECT "+strings.Join(cols, ",")+`,
  count(*),count(*) FILTER(WHERE production_status='open'),
  count(*) FILTER(WHERE production_status='completed'),count(DISTINCT production_household_ref),
  coalesce(sum(production_area_ha),0)::text,
  coalesce(sum(production_area_ha) FILTER(WHERE actual_kg IS NOT NULL),0)::text,
  coalesce(sum(expected_kg),0)::text,coalesce(sum(actual_kg),0)::text,
  coalesce(sum(sop_steps),0),coalesce(sum(sop_steps_done),0)
  FROM pf_cooperative_pub.v1_seasons
  WHERE production_present AND production_status<>'cancelled'
  AND ($1::boolean OR (NOT $3::boolean AND production_commune_code=ANY($2::text[]))
    OR ($3::boolean AND production_cooperative_ref=$4::uuid))
  AND ($4::uuid IS NULL OR production_cooperative_ref=$4::uuid)
  AND ($5::text='' OR production_season_code=$5::text)
  AND ($6::int=0 OR production_season_year=$6::int)
  AND ($7::text='' OR production_variety_code=$7::text)`+suffix,
		q.Scope.All && !q.CoopOnly, q.Scope.Units, q.CoopOnly, coop, q.SeasonCode, q.SeasonYear, q.VarietyCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []cropcyclesrc.ProductionRow{}
	for rows.Next() {
		var v cropcyclesrc.ProductionRow
		var ref *uuid.UUID
		var area, harvested, expected, actual string
		if err := rows.Scan(&ref, &v.SeasonCode, &v.SeasonYear, &v.VarietyCode,
			&v.Seasons, &v.OpenSeasons, &v.CompletedSeasons, &v.Households,
			&area, &harvested, &expected, &actual, &v.SOPSteps, &v.SOPStepsDone); err != nil {
			return nil, err
		}
		if v.Seasons == 0 {
			continue
		}
		if ref != nil {
			v.CooperativeRef = *ref
		}
		if v.AreaHa, err = decimal.NewFromString(area); err != nil {
			return nil, err
		}
		if v.HarvestedAreaHa, err = decimal.NewFromString(harvested); err != nil {
			return nil, err
		}
		if v.ExpectedKg, err = mass.ParseKg(expected); err != nil {
			return nil, err
		}
		if v.ActualKg, err = mass.ParseKg(actual); err != nil {
			return nil, err
		}
		if v.HarvestedAreaHa.IsPositive() {
			yield := v.ActualKg.Decimal().Div(decimal.NewFromInt(1000)).Div(v.HarvestedAreaHa).Round(4)
			v.ActualYieldTPerHa = &yield
		}
		if v.SOPSteps > 0 {
			progress := decimal.NewFromInt(v.SOPStepsDone).Mul(decimal.NewFromInt(100)).Div(decimal.NewFromInt(v.SOPSteps)).Round(1)
			v.SOPProgressPct = &progress
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
