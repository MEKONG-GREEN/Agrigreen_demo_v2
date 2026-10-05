package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Tra theo thời điểm và chuỗi kế thừa (R-DB-09, ADR-0032 mục 5) — port.AdminUnitHistory.
// Nguồn quan hệ: pf_masterdata.admin_unit_succession (pf 000089); phiên bản đóng chưa
// có dòng nào (ghi thẳng trước 000089) suy từ successor_codes (domain.LegacyEdges).

var _ port.AdminUnitHistory = UnitsPort{}

// edgesIn trả hàm đọc quan hệ kế thừa trong giao dịch tx.
func edgesIn(ctx context.Context, tx pgx.Tx, at domain.FindFunc) domain.EdgesFunc {
	return func(u domain.Unit) ([]port.Succession, error) {
		rows, err := store.SuccessionsFrom(ctx, tx, u.Code, u.ValidFrom)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return domain.LegacyEdges(u, at)
		}
		return domain.PreferOfficial(rows), nil
	}
}

func unitAtIn(ctx context.Context, tx pgx.Tx) domain.FindFunc {
	return func(c string, d time.Time) (*domain.Unit, error) { return store.UnitAt(ctx, tx, c, d) }
}

// toPortSuccession đổi ngày (UTC 00:00 của domain) sang 00:00 giờ Việt Nam như ToPortUnit.
func toPortSuccession(s port.Succession) port.Succession {
	s.PredecessorValidFrom = domain.DayStart(s.PredecessorValidFrom)
	s.SuccessorValidFrom = domain.DayStart(s.SuccessorValidFrom)
	s.EffectiveOn = domain.DayStart(s.EffectiveOn)
	return s
}

func (s *Service) successorsAt(ctx context.Context, tid uuid.UUID, code string, from, to time.Time) ([]port.Succession, error) {
	f, t := domain.DayOf(from), domain.DayOf(to)
	if t.Before(f) {
		return nil, problem.Validation.WithField("to", domain.ErrRollBackward.Error())
	}
	var out []port.Succession
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		at := unitAtIn(ctx, tx)
		start, err := at(code, f)
		if err != nil {
			return err
		}
		if start == nil {
			return ErrUnitNotFound
		}
		out, err = domain.SuccessorChain(*start, t, at, edgesIn(ctx, tx, at))
		return err
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i] = toPortSuccession(out[i])
	}
	return out, nil
}

func (s *Service) rollForward(ctx context.Context, tid uuid.UUID, code string, atTime, target time.Time) ([]port.RolledUnit, error) {
	a, t := domain.DayOf(atTime), domain.DayOf(target)
	if t.Before(a) {
		return nil, problem.Validation.WithField("target", domain.ErrRollBackward.Error())
	}
	var out []port.RolledUnit
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		at := unitAtIn(ctx, tx)
		start, err := at(code, a)
		if err != nil {
			return err
		}
		if start == nil {
			return ErrUnitNotFound
		}
		out, err = domain.RollForward(*start, t, at, edgesIn(ctx, tx, at))
		return err
	})
	if errors.Is(err, port.ErrUnitNotFound) {
		return nil, ErrUnitNotFound
	}
	return out, err
}

// AdminUnitAt thực hiện port.AdminUnitHistory (cùng nghĩa Get).
func (p UnitsPort) AdminUnitAt(ctx context.Context, code string, at time.Time) (port.AdminUnit, error) {
	return p.Get(ctx, code, at)
}

// SuccessorsAt thực hiện port.AdminUnitHistory.
func (p UnitsPort) SuccessorsAt(ctx context.Context, code string, from, to time.Time) ([]port.Succession, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	return p.Svc.successorsAt(ctx, tid, code, from, to)
}

// RollForward thực hiện port.AdminUnitHistory.
func (p UnitsPort) RollForward(ctx context.Context, code string, at, target time.Time) ([]port.RolledUnit, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	return p.Svc.rollForward(ctx, tid, code, at, target)
}
