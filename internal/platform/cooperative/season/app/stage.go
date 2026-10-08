package app

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// StageMapLimit là số thửa tối đa một lần đọc (GIS BR-25: ≤ 1.000 đối tượng mỗi khung nhìn).
const StageMapLimit = 1000

// StageMapInput lọc lớp giai đoạn mùa vụ (GIS US-SEASON-01, US-YIELD-01).
type StageMapInput struct {
	Date           *time.Time // ngày tra (mặc định hôm nay, giờ VN)
	CooperativeRef uuid.UUID
	CommuneCode    string
	Limit          int
}

// StagePlot là giai đoạn hiện tại của một thửa.
type StagePlot struct {
	store.Season
	Stage         string
	DaysToHarvest int
	Ripening      bool
	ExpectedTons  *decimal.Decimal // năng suất dự kiến App HTX × diện tích; nil = App HTX chưa gửi năng suất dự kiến
}

// StageCoop là tổng hợp theo HTX để tô polygon vùng HTX.
type StageCoop struct {
	CooperativeRef       uuid.UUID
	CommuneCode          string
	DominantStage        string
	StageAreaHa          map[string]decimal.Decimal
	PlotCount            int
	RipeningPlotCount    int
	RipeningAreaHa       decimal.Decimal
	RipeningExpectedTons *decimal.Decimal
	EarliestRipening     *time.Time
}

// StageMap là lớp giai đoạn mùa vụ theo phạm vi của người gọi.
type StageMap struct {
	Date         time.Time
	Plots        []StagePlot
	Cooperatives []StageCoop
	Truncated    bool
}

// StageMap đọc giai đoạn mùa vụ theo thửa và theo HTX (GIS US-SEASON-01 / FN-13) kèm vùng chín rộ trong
// 1–3 ngày tới và sản lượng dự kiến App HTX đã gửi (GIS US-YIELD-01 / FN-14, BR-48 — không tự dự báo).
// Phạm vi như bảng điều hành: quản lý HTX chỉ HTX mình, KN theo xã, nông dân theo hộ.
func (s *Service) StageMap(ctx context.Context, in StageMapInput) (StageMap, error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return StageMap{}, err
	}
	if a.kind == accCoop && in.CooperativeRef != uuid.Nil && in.CooperativeRef != a.p.OrgRef {
		return StageMap{}, ErrOutOfScope
	}
	day := s.today()
	if in.Date != nil {
		if d := domain.DaysBetween(day, *in.Date); d < -366 || d > 366 {
			return StageMap{}, problem.Validation.WithField("date", "trong vòng một năm so với hôm nay")
		}
		day = *in.Date
	}
	limit := in.Limit
	if limit <= 0 || limit > StageMapLimit {
		limit = StageMapLimit
	}
	var rows []store.Season
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.StageSeasons(ctx, tx, a.filter(), in.CooperativeRef, in.CommuneCode, domain.AddDays(day, -domain.DoneWindowDays), limit+1)
		return e
	})
	if err != nil {
		return StageMap{}, err
	}
	out := StageMap{Date: day}
	if len(rows) > limit {
		rows, out.Truncated = rows[:limit], true
	}
	coops := map[uuid.UUID]*StageCoop{}
	var order []uuid.UUID
	for _, r := range rows {
		// Vụ hoàn thành sau ngày tra (tra quá khứ) vẫn đang mở tại ngày đó.
		status := r.Status
		if status == "completed" && r.CompletedAt != nil && businessDay(*r.CompletedAt).After(day) {
			status = "open"
		}
		// Vụ mở sau ngày tra chưa tồn tại tại ngày đó.
		if businessDay(r.OpenedAt).After(day) {
			continue
		}
		stage := domain.SeasonStage(status, r.SowingDate, r.HarvestOpenDate, r.ExpectedHarvestDate, day)
		if stage == "" {
			continue
		}
		p := StagePlot{Season: r, Stage: stage, DaysToHarvest: domain.DaysBetween(day, r.ExpectedHarvestDate),
			Ripening: domain.Ripening(status, r.ExpectedHarvestDate, day)}
		if r.ExpectedYield != nil {
			t := r.ExpectedYield.Mul(r.AreaHa).Round(3)
			p.ExpectedTons = &t
		}
		out.Plots = append(out.Plots, p)
		c := coops[r.CooperativeRef]
		if c == nil {
			c = &StageCoop{CooperativeRef: r.CooperativeRef, CommuneCode: r.CommuneCode, StageAreaHa: map[string]decimal.Decimal{}}
			coops[r.CooperativeRef] = c
			order = append(order, r.CooperativeRef)
		}
		c.PlotCount++
		c.StageAreaHa[stage] = c.StageAreaHa[stage].Add(r.AreaHa)
		if p.Ripening {
			c.RipeningPlotCount++
			c.RipeningAreaHa = c.RipeningAreaHa.Add(r.AreaHa)
			if p.ExpectedTons != nil {
				sum := *p.ExpectedTons
				if c.RipeningExpectedTons != nil {
					sum = sum.Add(*c.RipeningExpectedTons)
				}
				c.RipeningExpectedTons = &sum
			}
			if c.EarliestRipening == nil || r.ExpectedHarvestDate.Before(*c.EarliestRipening) {
				d := r.ExpectedHarvestDate
				c.EarliestRipening = &d
			}
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i].String() < order[j].String() })
	for _, ref := range order {
		c := coops[ref]
		c.DominantStage = dominantStage(c.StageAreaHa)
		out.Cooperatives = append(out.Cooperatives, *c)
	}
	return out, nil
}

// dominantStage là giai đoạn có diện tích lớn nhất; bằng nhau → giai đoạn sớm hơn trong thứ tự chú giải.
func dominantStage(area map[string]decimal.Decimal) string {
	best, bestArea := "", decimal.Decimal{}
	for _, st := range domain.Stages {
		if a, ok := area[st]; ok && (best == "" || a.GreaterThan(bestArea)) {
			best, bestArea = st, a
		}
	}
	return best
}
