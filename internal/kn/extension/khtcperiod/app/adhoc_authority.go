package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Admin support is scoped to the new adhoc workflow. Native forms still authorizes
// the unchanged canonical session; no role is synthesized or granted here.
func (s *Service) openingWriter(ctx context.Context, in OpenInput) (caller, error) {
	if in.Request.Type == domain.TypeAdhoc {
		c, err := s.reader(ctx)
		if err != nil {
			return c, err
		}
		if c.k.Role == ext.SessionRoleAdmin && !c.k.ReadOnly() {
			if !c.p.MFA {
				return c, problem.MFARequired
			}
			return c, nil
		}
	}
	return s.writer(ctx)
}

func (s *Service) periodWriter(ctx context.Context, id uuid.UUID) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	if c.k.Role == ext.SessionRoleAdmin && !c.k.ReadOnly() {
		if !c.p.MFA {
			return c, problem.MFARequired
		}
		var p *store.Period
		if err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; p, e = store.PeriodByID(ctx, tx, id); return e }); err != nil {
			return c, err
		}
		if p != nil && p.Type == domain.TypeAdhoc {
			return c, nil
		}
	}
	return s.writer(ctx)
}
