package app

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ErrForecastNotFound: không có lần chạy dự báo.
var ErrForecastNotFound = problem.New(http.StatusNotFound, "mechanization.forecast_not_found", "Không tìm thấy kết quả dự báo đã lưu")

// ForecastRequest là yêu cầu chạy dự báo nhu cầu cho một vụ (FN-09).
type ForecastRequest struct {
	Season  domain.Season
	IdemKey string
}

// ForecastOutcome là lần chạy dự báo đã lưu.
type ForecastOutcome struct {
	Run      domain.ForecastRun
	Replayed bool
}

// RunForecast tính và LƯU dự báo nhu cầu máy cho vụ Season (cgh.forecast/v1 —
// phương pháp tạm, cần DCRD xác nhận): diện tích dự báo = diện tích cùng vụ năm
// trước × (1 + tăng trưởng TB tối đa 3 vụ, chặn ±20 %); số máy cần theo định mức
// hiệu lực tại ngày bắt đầu vụ dự báo. Kết quả là sổ bất biến; chạy lại tạo lần mới.
func (s *Service) RunForecast(ctx context.Context, req ForecastRequest) (ForecastOutcome, error) {
	p, sc, err := s.caller(ctx, writeRoles)
	if err != nil {
		return ForecastOutcome{}, err
	}
	if err := requireIdem(req.IdemKey); err != nil {
		return ForecastOutcome{}, err
	}
	item, err := s.season(ctx, req.Season)
	if err != nil {
		return ForecastOutcome{}, err
	}
	var prev *domain.ForecastRun
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.ForecastRunByIdem(ctx, tx, req.IdemKey)
		return e
	}); err != nil {
		return ForecastOutcome{}, err
	}
	if prev != nil {
		return ForecastOutcome{Run: *prev, Replayed: true}, nil
	}
	// Danh mục, định mức tại ngày bắt đầu vụ dự báo, HTX có máy (dùng chung với cân đối).
	in, err := s.balanceInputs(ctx, p, sc, req.Season, item)
	if err != nil {
		return ForecastOutcome{}, err
	}
	// Diện tích cùng vụ các năm Y−1 … Y−(3+1).
	history := map[uuid.UUID]map[int]decimal.Decimal{}
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		for i := 1; i <= domain.ForecastMaxHistory+1; i++ {
			areas, err := store.AreasOfSeason(ctx, tx, domain.Season{Code: req.Season.Code, Year: req.Season.Year - i}, uuid.Nil)
			if err != nil {
				return err
			}
			for _, a := range areas {
				if history[a.CooperativeRef] == nil {
					history[a.CooperativeRef] = map[int]decimal.Decimal{}
				}
				history[a.CooperativeRef][a.Season.Year] = a.AreaHa
			}
		}
		return nil
	}); err != nil {
		return ForecastOutcome{}, err
	}
	coops := map[uuid.UUID]coopInfo{}
	for _, c := range in.coops {
		coops[c.Ref] = c
	}
	var extra []uuid.UUID
	for ref := range history {
		if _, ok := coops[ref]; ok {
			continue
		}
		ci, err := s.coop(ctx, ref, in.refDate)
		if err != nil {
			return ForecastOutcome{}, err
		}
		if ci == nil || !ci.Active || !visible(sc, ci) {
			continue
		}
		coops[ref] = *ci
		extra = append(extra, ref)
	}
	if len(extra) > 0 {
		var counts []store.CountRow
		if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			counts, e = store.CountsAt(ctx, tx, in.refDate, dayStart(in.refDate.AddDate(0, 0, 1)), extra)
			return e
		}); err != nil {
			return ForecastOutcome{}, err
		}
		for _, c := range counts {
			if in.counts[c.CooperativeRef] == nil {
				in.counts[c.CooperativeRef] = map[string]store.CountRow{}
			}
			in.counts[c.CooperativeRef][c.MachineType] = c
		}
	}
	now := s.now()
	run := domain.ForecastRun{
		ID: ids.New(), TenantID: p.TenantID, Season: req.Season, ReferenceDate: in.refDate, PilotStages: in.pilot,
		Method: domain.DefaultForecastMethod(), EventTime: now, ActorRef: p.ActorRef, IdemKey: req.IdemKey,
		RuleVersion: domain.ForecastRuleVersion, MissingNorms: []string{},
	}
	for _, t := range in.types {
		if in.norms[t.Code] == nil {
			run.MissingNorms = append(run.MissingNorms, mdport.NormKey(t.Code, t.Attrs["farm_stage"]))
		}
	}
	refs := make([]uuid.UUID, 0, len(coops))
	for r := range coops {
		refs = append(refs, r)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].String() < refs[j].String() })
	var results []*domain.ForecastResult
	for _, ref := range refs {
		c := coops[ref]
		proj := domain.ProjectArea(history[ref], req.Season.Year)
		inputs := domain.ForecastInputs{History: map[string]string{}, Rates: map[string]string{}, Capped: proj.Capped,
			NormAt: in.refDate.Format(time.DateOnly)}
		for y, a := range history[ref] {
			inputs.History[strconv.Itoa(y)] = a.String()
		}
		for y, r := range proj.Rates {
			inputs.Rates[y] = r.String()
		}
		if proj.RawGrowth != nil {
			g := proj.RawGrowth.String()
			inputs.RawGrowth = &g
		}
		var growthPct *decimal.Decimal
		if proj.Base != nil {
			g := proj.Growth.Mul(decimal.NewFromInt(100)).Round(4)
			growthPct = &g
		}
		for _, t := range in.types {
			stage := t.Attrs["farm_stage"]
			capacity := in.counts[ref][t.Code].ByStatus[string(domain.StatusActive)]
			r := &domain.ForecastResult{
				ID: ids.New(), RunID: run.ID, Season: req.Season, CooperativeRef: ref, CommuneCode: c.CommuneCode, ProvinceCode: c.ProvinceCode,
				FarmStage: stage, MachineType: t.Code, BaseAreaHa: proj.Base, GrowthPct: growthPct, ForecastAreaHa: proj.Forecast,
				NormKey: mdport.NormKey(t.Code, stage), Norm: in.norms[t.Code], CapacityNow: capacity, Inputs: inputs,
			}
			r.Required, r.RequiredWhole, r.Gap, r.Status = domain.ForecastRequired(proj.Forecast, r.Norm, capacity)
			results = append(results, r)
		}
	}
	run.ResultCount = len(results)
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.InsertForecastRun(ctx, tx, &run); err != nil {
			return err
		}
		return store.InsertForecastResults(ctx, tx, run, results)
	})
	if err = classify(err); err != nil {
		return ForecastOutcome{}, err
	}
	return ForecastOutcome{Run: run}, nil
}

// ForecastView là một lần chạy dự báo kèm dòng kết quả trong phạm vi.
type ForecastView struct {
	Run     domain.ForecastRun
	Results []domain.ForecastResult
	// Tổng theo khâu và toàn bộ (diện tích dự báo tính một lần mỗi HTX).
	TotalForecastAreaHa decimal.Decimal
	RequiredByStage     map[string]decimal.Decimal
}

// ListForecasts liệt kê lần chạy dự báo đã lưu.
func (s *Service) ListForecasts(ctx context.Context, seasonCode string, year int) ([]domain.ForecastRun, error) {
	p, _, err := s.caller(ctx, readRoles)
	if err != nil {
		return nil, err
	}
	var out []domain.ForecastRun
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListForecastRuns(ctx, tx, seasonCode, year)
		return e
	})
	if out == nil {
		out = []domain.ForecastRun{}
	}
	return out, err
}

// GetForecast đọc một lần chạy dự báo đã lưu (không tính lại) và các dòng trong phạm vi.
func (s *Service) GetForecast(ctx context.Context, id uuid.UUID, f store.ResultFilter) (ForecastView, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return ForecastView{}, err
	}
	var v ForecastView
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		run, err := store.ForecastRunByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if run == nil {
			return ErrForecastNotFound
		}
		v.Run = *run
		f.RunID = id
		v.Results, err = store.ForecastResults(ctx, tx, scopeFilter(sc, f))
		return err
	})
	if err != nil {
		return ForecastView{}, err
	}
	if v.Results == nil {
		v.Results = []domain.ForecastResult{}
	}
	v.RequiredByStage = map[string]decimal.Decimal{}
	seen := map[uuid.UUID]bool{}
	for _, r := range v.Results {
		if r.ForecastAreaHa != nil && !seen[r.CooperativeRef] {
			seen[r.CooperativeRef] = true
			v.TotalForecastAreaHa = v.TotalForecastAreaHa.Add(*r.ForecastAreaHa)
		}
		if r.Required != nil {
			v.RequiredByStage[r.FarmStage] = v.RequiredByStage[r.FarmStage].Add(*r.Required)
		}
	}
	return v, nil
}
