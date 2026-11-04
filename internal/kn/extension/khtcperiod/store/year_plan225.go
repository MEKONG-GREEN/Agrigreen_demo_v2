package store

import (
	"context"
	"errors"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strconv"
)

// No list, expiry or status cutoff: even an old unknown dispatch fences this year.
func PlanYearReferences225(ctx context.Context, tx pgx.Tx, tid uuid.UUID, year int) (ext.KHTCPlanYearReferences, error) {
	var out ext.KHTCPlanYearReferences
	var unknown bool
	err := tx.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM pf_extension.khtc_period WHERE tenant_id=$1 AND plan_year=$2::smallint),
 EXISTS(SELECT 1 FROM pf_extension.khtc_adhoc_open_intent WHERE tenant_id=$1 AND plan->>'Year'=$3::text),
 EXISTS(SELECT 1 FROM pf_extension.khtc_adhoc_open_intent WHERE tenant_id=$1 AND NOT coalesce(jsonb_typeof(plan->'Year')='number' AND plan->>'Year' ~ '^(20[0-9]{2}|2100)$',false))`, tid, year, strconv.Itoa(year)).Scan(&out.HasPeriodMetadata, &out.HasOpenIntent, &unknown)
	if err != nil {
		return out, err
	}
	if unknown {
		return out, errors.New("unclassified legacy KHTC open owner year")
	}
	return out, nil
}
