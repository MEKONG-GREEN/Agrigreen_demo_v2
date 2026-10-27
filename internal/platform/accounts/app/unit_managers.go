package app

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// UnitManagerReader supplies a server-side unit-leader query without minting identity.
type UnitManagerReader struct{ Svc *Service }

var _ port.UnitManagers = UnitManagerReader{}

// maxUnitManagerUnits bounds one query (a plan period has at most 200 units).
const maxUnitManagerUnits = 200

func (r UnitManagerReader) UnitManagers(ctx context.Context, tid, node uuid.UUID, units []string) (map[string][]uuid.UUID, error) {
	if tid == uuid.Nil {
		return nil, tenant.ErrMissing
	}
	if p, ok := authz.FromContext(ctx); ok && p.TenantID != tid {
		return nil, port.ErrScopedDutyTenant
	}
	if contextual, err := tenant.FromContext(ctx); err == nil && contextual != tid {
		return nil, port.ErrScopedDutyTenant
	} else if err != nil && !errors.Is(err, tenant.ErrMissing) {
		return nil, err
	}
	want := make([]string, 0, len(units))
	for _, u := range units {
		u = strings.TrimSpace(u)
		if u == "" {
			return nil, port.ErrScopedDutyQuery
		}
		if !slices.Contains(want, u) {
			want = append(want, u)
		}
	}
	if node == uuid.Nil || len(want) == 0 || len(want) > maxUnitManagerUnits {
		return nil, port.ErrScopedDutyQuery
	}
	if r.Svc.DB == nil {
		return nil, ErrDependency
	}
	catalog := r.Svc.AttributeCatalog()
	if !catalog.Valid() {
		return nil, ErrDependency.WithDetail("danh mục thuộc tính chưa sẵn sàng")
	}
	at := r.Svc.now()
	var rows []store.UnitManagerCandidate
	err := tenant.WithTenantTxOptions(ctx, db.ForModule(r.Svc.DB, scopedDutyModule), tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		rows, err = store.UnitManagerCandidatesAt(ctx, tx, node, port.KNTinh, want, at)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := map[string][]uuid.UUID{}
	for _, c := range rows {
		caps, err := catalog.Capabilities(port.KNTinh, c.MgmtLevel, c.ProvinceGroup)
		if err != nil || !caps.Manager || caps.ForceReadOnly {
			// Thuộc tính không còn trong danh mục hoặc không phải lãnh đạo / nhóm chỉ xem: không nhận (đóng an toàn).
			continue
		}
		var of []string
		if c.InternalUnit != "" {
			of = []string{c.InternalUnit}
		} else {
			of = c.Departments
		}
		for _, u := range of {
			if slices.Contains(want, u) && !slices.Contains(out[u], c.UserRef) {
				out[u] = append(out[u], c.UserRef)
			}
		}
	}
	for u := range out {
		slices.SortFunc(out[u], func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	}
	return out, nil
}
