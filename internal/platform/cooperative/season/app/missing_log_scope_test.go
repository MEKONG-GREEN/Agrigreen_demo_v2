package app

import (
	"context"
	"errors"
	"testing"

	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
)

type missingSeasonPlots struct {
	port.Plots
	get func(context.Context, uuid.UUID) (port.Plot, error)
}

func (p missingSeasonPlots) Get(ctx context.Context, ref uuid.UUID) (port.Plot, error) {
	return p.get(ctx, ref)
}

func TestMissingLogSeasonRequiresWriteProofWithoutExistenceOracle(t *testing.T) {
	tid, coop, household, plot := ids.New(), ids.New(), ids.New(), ids.New()
	own := port.Plot{Ref: plot, CooperativeRef: coop, HouseholdRef: household, OwnerTenant: tid}
	manager := access{kind: accCoop, p: authz.Principal{TenantID: tid, OrgRef: coop}}
	farmer := access{kind: accFarmer, p: authz.Principal{TenantID: tid, OrgRef: coop}, households: []uuid.UUID{household}}
	for _, a := range []access{manager, farmer} {
		for _, tc := range []struct {
			name   string
			change func(*port.Plot)
			lookup error
			want   error
		}{
			{"own unopened", nil, nil, ErrSeasonNotOpen},
			{"hidden", nil, port.ErrPlotNotFound, ErrOutOfScope},
			{"wrong reference", func(p *port.Plot) { p.Ref = ids.New() }, nil, ErrOutOfScope},
			{"foreign owner tenant", func(p *port.Plot) { p.OwnerTenant = ids.New() }, nil, ErrOutOfScope},
			{"no cooperative", func(p *port.Plot) { p.CooperativeRef = uuid.Nil }, nil, ErrOutOfScope},
		} {
			t.Run(tc.name, func(t *testing.T) {
				candidate := own
				if tc.change != nil {
					tc.change(&candidate)
				}
				calls := 0
				s := &Service{Plots: missingSeasonPlots{get: func(context.Context, uuid.UUID) (port.Plot, error) { calls++; return candidate, tc.lookup }}}
				err := s.missingLogSeason(context.Background(), a, uuid.Nil, plot)
				if !errors.Is(err, tc.want) || calls != 1 {
					t.Fatalf("err=%v want=%v calls=%d", err, tc.want, calls)
				}
			})
		}
		t.Run("missing season ref does not trust a supplied plot", func(t *testing.T) {
			s := &Service{Plots: missingSeasonPlots{get: func(context.Context, uuid.UUID) (port.Plot, error) {
				t.Fatal("looked up unrelated client plot")
				return own, nil
			}}}
			if err := s.missingLogSeason(context.Background(), a, ids.New(), plot); !errors.Is(err, ErrOutOfScope) {
				t.Fatal(err)
			}
		})
	}
	t.Run("other cooperative", func(t *testing.T) {
		candidate := own
		candidate.CooperativeRef = ids.New()
		s := &Service{Plots: missingSeasonPlots{get: func(context.Context, uuid.UUID) (port.Plot, error) { return candidate, nil }}}
		if err := s.missingLogSeason(context.Background(), manager, uuid.Nil, plot); !errors.Is(err, ErrOutOfScope) {
			t.Fatal(err)
		}
	})
	t.Run("other household", func(t *testing.T) {
		candidate := own
		candidate.HouseholdRef = ids.New()
		s := &Service{Plots: missingSeasonPlots{get: func(context.Context, uuid.UUID) (port.Plot, error) { return candidate, nil }}}
		if err := s.missingLogSeason(context.Background(), farmer, uuid.Nil, plot); !errors.Is(err, ErrOutOfScope) {
			t.Fatal(err)
		}
	})
	t.Run("dependency closes", func(t *testing.T) {
		sentinel := errors.New("plot dependency unavailable")
		s := &Service{Plots: missingSeasonPlots{get: func(context.Context, uuid.UUID) (port.Plot, error) { return port.Plot{}, sentinel }}}
		if err := s.missingLogSeason(context.Background(), manager, uuid.Nil, plot); !errors.Is(err, sentinel) {
			t.Fatalf("lost dependency failure: %v", err)
		}
		if err := (&Service{}).missingLogSeason(context.Background(), manager, uuid.Nil, plot); err == nil {
			t.Fatal("missing plot port allowed")
		}
	})
	t.Run("joined lookup closes before plot port", func(t *testing.T) {
		b := &nativeScopeDB{}
		s := &Service{Plots: missingSeasonPlots{get: func(context.Context, uuid.UUID) (port.Plot, error) {
			t.Fatal("plot port called inside joined tx")
			return own, nil
		}}}
		ctx := shareddb.JoinTx(context.Background(), &nativeScopeTxFake{b: b})
		if err := s.missingLogSeason(ctx, manager, uuid.Nil, plot); err == nil {
			t.Fatal("joined lookup allowed")
		}
	})
	t.Run("admin classification retained", func(t *testing.T) {
		s := &Service{}
		a := access{kind: accAll}
		if err := s.missingLogSeason(context.Background(), a, uuid.Nil, plot); !errors.Is(err, ErrSeasonNotOpen) {
			t.Fatal(err)
		}
		if err := s.missingLogSeason(context.Background(), a, ids.New(), uuid.Nil); !errors.Is(err, ErrSeasonNotFound) {
			t.Fatal(err)
		}
	})
}
