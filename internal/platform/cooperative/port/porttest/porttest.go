// Package porttest là bản giả trong bộ nhớ của các cổng đọc pf/cooperative (Memory) cho kiểm thử
// của module khác, cùng bộ kiểm hợp đồng (RunContract) mà mọi hiện thực cổng phải qua: mặt tiền
// trên Memory, mặt tiền trên port cũ (cmd/internal/coopwire, W3.2) và store pf_coop (W3.3).
package porttest

import (
	"context"
	"slices"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
)

// Seed là dữ liệu mẫu của một tenant.
type Seed struct {
	Tenant    uuid.UUID
	Units     []port.UnitCooperatives
	Sites     []port.CooperativeSite
	Centroids []port.CoopCentroid
	Members   []port.HouseholdMembership
	// PlotHouseholds: xã → nông hộ có thửa đang dùng.
	PlotHouseholds map[string][]uuid.UUID
	Plots          []port.Plot
	CommuneStats   []port.PlotStat
	CoopStats      []port.PlotStat
	Activity       []port.CoopActivity
	Areas          []port.CoopSeasonArea
	Supply         []port.CoopSeasonSupply
	Production     port.ProductionData
	Diary          []port.DiaryEntry
}

// Memory cài đặt port.Cooperatives, Households, Plots, Seasons trên Seed (Diary qua MemoryDiary).
// Ngữ nghĩa lọc như port cũ: danh sách nil = tất cả, rỗng = không gì.
type Memory struct{ S Seed }

var (
	_ port.Cooperatives = Memory{}
	_ port.Households   = Memory{}
	_ port.Plots        = Memory{}
	_ port.Seasons      = Memory{}
	_ port.Diary        = MemoryDiary{}
)

func in[T comparable](xs []T, x T) bool { return xs == nil || slices.Contains(xs, x) }

// ByUnit thực hiện port.Cooperatives.
func (m Memory) ByUnit(_ context.Context, units []string) ([]port.UnitCooperatives, error) {
	out := []port.UnitCooperatives{}
	for _, u := range m.S.Units {
		if in(units, u.UnitCode) {
			out = append(out, u)
		}
	}
	return out, nil
}

// Active thực hiện port.Cooperatives.
func (m Memory) Active(context.Context) ([]port.CooperativeSite, error) {
	return append([]port.CooperativeSite{}, m.S.Sites...), nil
}

// Centroids thực hiện port.Cooperatives.
func (m Memory) Centroids(_ context.Context, coops []uuid.UUID) (map[uuid.UUID]port.CoopCentroid, error) {
	out := map[uuid.UUID]port.CoopCentroid{}
	for _, c := range m.S.Centroids {
		if in(coops, c.CooperativeRef) {
			out[c.CooperativeRef] = c
		}
	}
	return out, nil
}

// Membership thực hiện port.Households.
func (m Memory) Membership(_ context.Context, ref uuid.UUID) (port.HouseholdMembership, error) {
	for _, h := range m.S.Members {
		if h.Ref == ref {
			return h, nil
		}
	}
	return port.HouseholdMembership{}, port.ErrHouseholdNotFound
}

// InCommunes thực hiện port.Households.
func (m Memory) InCommunes(_ context.Context, communes []string) ([]uuid.UUID, error) {
	out := []uuid.UUID{}
	for c, hh := range m.S.PlotHouseholds {
		if in(communes, c) {
			for _, h := range hh {
				if !slices.Contains(out, h) {
					out = append(out, h)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out, nil
}

// Get thực hiện port.Plots.
func (m Memory) Get(_ context.Context, ref uuid.UUID) (port.Plot, error) {
	for _, p := range m.S.Plots {
		if p.Ref == ref {
			return p, nil
		}
	}
	return port.Plot{}, port.ErrPlotNotFound
}

// DistanceM thực hiện port.Plots: bản giả trả 0 cho mọi thửa có (không có hình học).
func (m Memory) DistanceM(ctx context.Context, ref uuid.UUID, _, _ decimal.Decimal) (decimal.Decimal, error) {
	if _, err := m.Get(ctx, ref); err != nil {
		return decimal.Zero, err
	}
	return decimal.Zero, nil
}

// Facts thực hiện port.Plots.
func (m Memory) Facts(_ context.Context, refs []uuid.UUID) (map[uuid.UUID]port.Plot, error) {
	out := map[uuid.UUID]port.Plot{}
	for _, p := range m.S.Plots {
		if slices.Contains(refs, p.Ref) {
			out[p.Ref] = p
		}
	}
	return out, nil
}

// DigitizedByCommune thực hiện port.Plots.
func (m Memory) DigitizedByCommune(_ context.Context, communes []string) ([]port.PlotStat, error) {
	out := []port.PlotStat{}
	for _, s := range m.S.CommuneStats {
		if in(communes, s.CommuneCode) {
			out = append(out, s)
		}
	}
	return out, nil
}

// DigitizedByCooperative thực hiện port.Plots (như pf/geo: coops rỗng hoặc nil → rỗng).
func (m Memory) DigitizedByCooperative(_ context.Context, coops []uuid.UUID) (map[uuid.UUID]port.PlotStat, error) {
	out := map[uuid.UUID]port.PlotStat{}
	for _, s := range m.S.CoopStats {
		if slices.Contains(coops, s.CooperativeRef) {
			out[s.CooperativeRef] = s
		}
	}
	return out, nil
}

// ActivityByCooperative thực hiện port.Seasons.
func (m Memory) ActivityByCooperative(_ context.Context, coops []uuid.UUID) (map[uuid.UUID]port.CoopActivity, error) {
	out := map[uuid.UUID]port.CoopActivity{}
	for _, a := range m.S.Activity {
		if in(coops, a.CooperativeRef) {
			out[a.CooperativeRef] = a
		}
	}
	return out, nil
}

// Areas thực hiện port.Seasons.
func (m Memory) Areas(_ context.Context, coop uuid.UUID, seasonCode string, year int) ([]port.CoopSeasonArea, error) {
	out := []port.CoopSeasonArea{}
	for _, a := range m.S.Areas {
		if a.CooperativeRef == coop && (seasonCode == "" || a.SeasonCode == seasonCode) && (year == 0 || a.SeasonYear == year) {
			out = append(out, a)
		}
	}
	return out, nil
}

// Supply thực hiện port.Seasons.
func (m Memory) Supply(_ context.Context, year int) ([]port.CoopSeasonSupply, error) {
	out := []port.CoopSeasonSupply{}
	for _, s := range m.S.Supply {
		if s.SeasonYear == year {
			out = append(out, s)
		}
	}
	return out, nil
}

// Production thực hiện port.Seasons (trả nguyên Seed.Production).
func (m Memory) Production(context.Context, port.ProductionQuery) (port.ProductionData, error) {
	return m.S.Production, nil
}

// HarvestDates thực hiện port.Seasons: bản giả không giữ ngày thu hoạch.
func (m Memory) HarvestDates(context.Context, []uuid.UUID) ([]port.SeasonHarvestDate, error) {
	return []port.SeasonHarvestDate{}, nil
}

// MemoryDiary cài đặt port.Diary trên Seed.Diary.
type MemoryDiary struct{ S Seed }

// Get thực hiện port.Diary.
func (d MemoryDiary) Get(_ context.Context, ref uuid.UUID) (port.DiaryEntry, error) {
	for _, e := range d.S.Diary {
		if e.LogRef == ref {
			return e, nil
		}
	}
	return port.DiaryEntry{}, port.ErrDiaryEntryNotFound
}

// BySeason thực hiện port.Diary.
func (d MemoryDiary) BySeason(_ context.Context, season, after uuid.UUID, limit int) ([]port.DiaryEntry, error) {
	var out []port.DiaryEntry
	for _, e := range d.S.Diary {
		if e.SeasonRef == season && (after == uuid.Nil || e.LogRef.String() > after.String()) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LogRef.String() < out[j].LogRef.String() })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
