// Package app là mặt tiền của pf/cooperative: hiện thực các cổng của cooperative/port trên store.
// W3.2 store là store.Legacy (nguồn cũ của party/geo/cropcycle); mặt tiền thêm phần chung — nguồn
// chưa nối → port.ErrUnavailable, khóa rỗng không gọi nguồn, chuẩn hóa giới hạn trang và phân giải
// chủ thể (Resolve) từ các cổng đọc.
package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Giới hạn trang của Diary.BySeason.
const (
	DefaultDiaryLimit = 100
	MaxDiaryLimit     = 500
)

// Service là mặt tiền. Store có thể nối dần (Use* của module) trước khi phục vụ.
type Service struct {
	Store store.Legacy
}

var (
	_ port.Cooperatives = (*Service)(nil)
	_ port.Households   = (*Service)(nil)
	_ port.Plots        = (*Service)(nil)
	_ port.Seasons      = (*Service)(nil)
	_ port.Diary        = Diary{}
	_ port.Resolve      = (*Service)(nil)
)

func unavailable(what string) error { return fmt.Errorf("%w (%s)", port.ErrUnavailable, what) }

// ---- Cooperatives ----

// ByUnit thực hiện port.Cooperatives.
func (s *Service) ByUnit(ctx context.Context, units []string) ([]port.UnitCooperatives, error) {
	if s.Store.Cooperatives == nil {
		return nil, unavailable("cooperatives")
	}
	if units != nil && len(units) == 0 {
		return []port.UnitCooperatives{}, nil // danh sách rỗng (khác nil = mọi đơn vị): không thấy gì
	}
	return s.Store.Cooperatives.ByUnit(ctx, units)
}

// Active thực hiện port.Cooperatives.
func (s *Service) Active(ctx context.Context) ([]port.CooperativeSite, error) {
	if s.Store.Cooperatives == nil {
		return nil, unavailable("cooperatives")
	}
	return s.Store.Cooperatives.Active(ctx)
}

// Centroids thực hiện port.Cooperatives.
func (s *Service) Centroids(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]port.CoopCentroid, error) {
	if s.Store.Cooperatives == nil {
		return nil, unavailable("cooperatives")
	}
	if coops != nil && len(coops) == 0 {
		return map[uuid.UUID]port.CoopCentroid{}, nil
	}
	return s.Store.Cooperatives.Centroids(ctx, coops)
}

// ---- Households ----

// Membership thực hiện port.Households.
func (s *Service) Membership(ctx context.Context, ref uuid.UUID) (port.HouseholdMembership, error) {
	if s.Store.Households == nil {
		return port.HouseholdMembership{}, unavailable("households")
	}
	if ref == uuid.Nil {
		return port.HouseholdMembership{}, port.ErrHouseholdNotFound
	}
	return s.Store.Households.Membership(ctx, ref)
}

// InCommunes thực hiện port.Households.
func (s *Service) InCommunes(ctx context.Context, communes []string) ([]uuid.UUID, error) {
	if s.Store.Households == nil {
		return nil, unavailable("households")
	}
	if communes != nil && len(communes) == 0 {
		return []uuid.UUID{}, nil
	}
	return s.Store.Households.InCommunes(ctx, communes)
}

// ---- Plots ----

// Get thực hiện port.Plots.
func (s *Service) Get(ctx context.Context, ref uuid.UUID) (port.Plot, error) {
	if s.Store.Plots == nil {
		return port.Plot{}, unavailable("plots")
	}
	if ref == uuid.Nil {
		return port.Plot{}, port.ErrPlotNotFound
	}
	return s.Store.Plots.Get(ctx, ref)
}

// DistanceM thực hiện port.Plots.
func (s *Service) DistanceM(ctx context.Context, ref uuid.UUID, lon, lat decimal.Decimal) (decimal.Decimal, error) {
	if s.Store.Plots == nil {
		return decimal.Zero, unavailable("plots")
	}
	if ref == uuid.Nil {
		return decimal.Zero, port.ErrPlotNotFound
	}
	return s.Store.Plots.DistanceM(ctx, ref, lon, lat)
}

// Facts thực hiện port.Plots.
func (s *Service) Facts(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]port.Plot, error) {
	if s.Store.Plots == nil {
		return nil, unavailable("plots")
	}
	if len(refs) == 0 {
		return map[uuid.UUID]port.Plot{}, nil
	}
	return s.Store.Plots.Facts(ctx, refs)
}

// DigitizedByCommune thực hiện port.Plots.
func (s *Service) DigitizedByCommune(ctx context.Context, communes []string) ([]port.PlotStat, error) {
	if s.Store.Plots == nil {
		return nil, unavailable("plots")
	}
	if communes != nil && len(communes) == 0 {
		return []port.PlotStat{}, nil
	}
	return s.Store.Plots.DigitizedByCommune(ctx, communes)
}

// DigitizedByCooperative thực hiện port.Plots.
func (s *Service) DigitizedByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]port.PlotStat, error) {
	if s.Store.Plots == nil {
		return nil, unavailable("plots")
	}
	if coops != nil && len(coops) == 0 {
		return map[uuid.UUID]port.PlotStat{}, nil
	}
	return s.Store.Plots.DigitizedByCooperative(ctx, coops)
}

// ---- Seasons ----

// ActivityByCooperative thực hiện port.Seasons.
func (s *Service) ActivityByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]port.CoopActivity, error) {
	if s.Store.Seasons == nil {
		return nil, unavailable("seasons")
	}
	if coops != nil && len(coops) == 0 {
		return map[uuid.UUID]port.CoopActivity{}, nil
	}
	return s.Store.Seasons.ActivityByCooperative(ctx, coops)
}

// Areas thực hiện port.Seasons.
func (s *Service) Areas(ctx context.Context, coop uuid.UUID, seasonCode string, year int) ([]port.CoopSeasonArea, error) {
	if s.Store.Seasons == nil {
		return nil, unavailable("seasons")
	}
	return s.Store.Seasons.Areas(ctx, coop, seasonCode, year)
}

// Supply thực hiện port.Seasons.
func (s *Service) Supply(ctx context.Context, year int) ([]port.CoopSeasonSupply, error) {
	if s.Store.Seasons == nil {
		return nil, unavailable("seasons")
	}
	return s.Store.Seasons.Supply(ctx, year)
}

// Production thực hiện port.Seasons.
func (s *Service) Production(ctx context.Context, q port.ProductionQuery) (port.ProductionData, error) {
	if s.Store.Seasons == nil {
		return port.ProductionData{}, unavailable("seasons")
	}
	return s.Store.Seasons.Production(ctx, q)
}

// HarvestDates thực hiện port.Seasons.
func (s *Service) HarvestDates(ctx context.Context, plotRefs []uuid.UUID) ([]port.SeasonHarvestDate, error) {
	if s.Store.Seasons == nil {
		return nil, unavailable("seasons")
	}
	if len(plotRefs) == 0 {
		return []port.SeasonHarvestDate{}, nil
	}
	return s.Store.Seasons.HarvestDates(ctx, plotRefs)
}

// ---- Diary ----

// Diary là port.Diary của mặt tiền (tách khỏi Service vì Get trùng tên với port.Plots.Get).
type Diary struct{ Svc *Service }

// Get thực hiện port.Diary.
func (d Diary) Get(ctx context.Context, ref uuid.UUID) (port.DiaryEntry, error) {
	if d.Svc == nil || d.Svc.Store.Diary == nil {
		return port.DiaryEntry{}, unavailable("diary")
	}
	if ref == uuid.Nil {
		return port.DiaryEntry{}, port.ErrDiaryEntryNotFound
	}
	return d.Svc.Store.Diary.Get(ctx, ref)
}

// BySeason thực hiện port.Diary.
func (d Diary) BySeason(ctx context.Context, season, after uuid.UUID, limit int) ([]port.DiaryEntry, error) {
	if d.Svc == nil || d.Svc.Store.Diary == nil {
		return nil, unavailable("diary")
	}
	return d.Svc.Store.Diary.BySeason(ctx, season, after, ClampDiaryLimit(limit))
}

// ClampDiaryLimit chuẩn hóa giới hạn trang của Diary.BySeason.
func ClampDiaryLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultDiaryLimit
	case limit > MaxDiaryLimit:
		return MaxDiaryLimit
	}
	return limit
}

// ---- Resolve ----

// Resolve thực hiện port.Resolve từ các cổng đọc. Tenant lấy từ context (bắt buộc). W3.2 phân giải
// cooperative (HTX đang hoạt động), household, plot, diary_entry; season và harvest chờ store
// riêng (W3.3) → port.ErrSubjectTypeUnsupported.
func (s *Service) Resolve(ctx context.Context, t port.SubjectType, id uuid.UUID) (port.Subject, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return port.Subject{}, err
	}
	sub := port.Subject{Type: t, ID: id, TenantID: tid}
	switch t {
	case port.SubjectCooperative, port.SubjectHousehold, port.SubjectPlot, port.SubjectDiaryEntry:
	default:
		return port.Subject{}, fmt.Errorf("%w: %q", port.ErrSubjectTypeUnsupported, t)
	}
	if id == uuid.Nil {
		return port.Subject{}, port.ErrSubjectNotFound
	}
	switch t {
	case port.SubjectCooperative:
		sites, err := s.Active(ctx)
		if err != nil {
			return port.Subject{}, err
		}
		for _, c := range sites {
			if c.Ref == id {
				sub.NodeRef, sub.CommuneCode, sub.Status = c.Ref, c.CommuneCode, "active"
				return sub, nil
			}
		}
		return port.Subject{}, port.ErrSubjectNotFound
	case port.SubjectHousehold:
		m, err := s.Membership(ctx, id)
		if errors.Is(err, port.ErrHouseholdNotFound) {
			return port.Subject{}, port.ErrSubjectNotFound
		}
		if err != nil {
			return port.Subject{}, err
		}
		sub.NodeRef, sub.CommuneCode, sub.HouseholdRef, sub.Status = m.CooperativeRef, m.CommuneCode, m.Ref, "inactive"
		if m.Active {
			sub.Status = "active"
		}
		return sub, nil
	case port.SubjectPlot:
		p, err := s.Get(ctx, id)
		if errors.Is(err, port.ErrPlotNotFound) {
			return port.Subject{}, port.ErrSubjectNotFound
		}
		if err != nil {
			return port.Subject{}, err
		}
		sub.NodeRef, sub.CommuneCode, sub.HouseholdRef, sub.Status = p.CooperativeRef, p.CommuneCode, p.HouseholdRef, p.Status
		return sub, nil
	default: // port.SubjectDiaryEntry
		e, err := Diary{Svc: s}.Get(ctx, id)
		if errors.Is(err, port.ErrDiaryEntryNotFound) {
			return port.Subject{}, port.ErrSubjectNotFound
		}
		if err != nil {
			return port.Subject{}, err
		}
		sub.NodeRef, sub.CommuneCode, sub.HouseholdRef, sub.Status = e.CooperativeRef, e.CommuneCode, e.HouseholdRef, e.Status
		return sub, nil
	}
}
