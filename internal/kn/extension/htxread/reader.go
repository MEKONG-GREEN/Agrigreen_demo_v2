// Package htxread reads versioned publications under the KN module role.
// It never calls the HTX implementation or reads a private source table.
package htxread

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

type Reader struct{ db tenant.Beginner }

func New(pool tenant.Beginner) *Reader {
	return &Reader{db: db.ForModule(pool, db.Module("pf/extension"))}
}

var (
	_ port.CooperativeStatistics = (*Reader)(nil)
	_ port.PlotStatistics        = (*Reader)(nil)
	_ port.HTXConfirmations      = (*Reader)(nil)
)

func (r *Reader) read(ctx context.Context, fn func(pgx.Tx) error) error {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return err
	}
	return tenant.WithTenantTxOptions(ctx, r.db, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, fn)
}

// ByUnit retains active organizations without a profile, with zero missing member counts.
// Both sides are publications and the join includes tenant_id even when references coincide.
func (r *Reader) ByUnit(ctx context.Context, units []string) ([]port.UnitCooperatives, error) {
	out := []port.UnitCooperatives{}
	err := r.read(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT o.admin_unit_code,count(*)::bigint,
    coalesce(sum(c.member_households),0)::bigint
    FROM pf_party_pub.v1_org_tree o
    LEFT JOIN pf_cooperative_pub.v1_cooperatives c
      ON c.tenant_id=o.tenant_id AND c.org_ref=o.org_ref
    WHERE o.kind='cooperative' AND o.active AND coalesce(o.admin_unit_code,'')<>''
      AND ($1::boolean OR o.admin_unit_code=ANY($2::text[]))
    GROUP BY o.admin_unit_code ORDER BY o.admin_unit_code`, units == nil, units)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v port.UnitCooperatives
			if err := rows.Scan(&v.UnitCode, &v.Cooperatives, &v.Households); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Reader) DigitizedByCommune(ctx context.Context, units []string) ([]port.CommunePlots, error) {
	out := []port.CommunePlots{}
	err := r.read(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT commune_code,count(*)::bigint,
    coalesce(sum(area_ha),0)::numeric(14,4)
    FROM pf_cooperative_pub.v1_plots
    WHERE status<>'retired' AND ($1::boolean OR commune_code=ANY($2::text[]))
    GROUP BY commune_code ORDER BY commune_code`, units == nil, units)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v port.CommunePlots
			if err := rows.Scan(&v.CommuneCode, &v.Plots, &v.AreaHa); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Reader) Confirmations(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]string, error) {
	return r.confirmations(ctx, refs, nil)
}

// ConfirmationsAt uses the supplied instant, including zero; it never substitutes now().
// The caller converts the M6 reporting date to the last instant of that day in Vietnam.
func (r *Reader) ConfirmationsAt(ctx context.Context, refs []uuid.UUID, at time.Time) (map[uuid.UUID]string, error) {
	return r.confirmations(ctx, refs, &at)
}

func (r *Reader) confirmations(ctx context.Context, refs []uuid.UUID, cutoff *time.Time) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(refs))
	keys := make([]string, 0, len(refs))
	for _, ref := range refs {
		out[ref] = port.ConfirmationUnconfirmed
		keys = append(keys, ref.String())
	}
	err := r.read(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT org_ref FROM pf_cooperative_pub.v1_confirmations
    WHERE org_ref=ANY($1::text[]::uuid[])
    AND ($2::timestamptz IS NULL OR claimed_at<=$2)`, keys, cutoff)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ref uuid.UUID
			if err := rows.Scan(&ref); err != nil {
				return err
			}
			out[ref] = port.ConfirmationConfirmed
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

var _ port.PlotDirectory = (*Reader)(nil)

// PlotLocation đọc xã/HTX/trạng thái của một thửa từ publication; RLS của người gọi ẩn thửa ngoài phạm vi.
func (r *Reader) PlotLocation(ctx context.Context, plotRef uuid.UUID) (port.PlotLocation, bool, error) {
	var out port.PlotLocation
	found := false
	err := r.read(ctx, func(tx pgx.Tx) error {
		var coop *uuid.UUID
		e := tx.QueryRow(ctx, `SELECT plot_ref,commune_code,cooperative_ref,status
    FROM pf_cooperative_pub.v1_plots WHERE plot_ref=$1::uuid`, plotRef).Scan(&out.PlotRef, &out.CommuneCode, &coop, &out.Status)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		if coop != nil {
			out.CooperativeRef = *coop
		}
		found = true
		return nil
	})
	if err != nil {
		return port.PlotLocation{}, false, err
	}
	return out, found, nil
}
