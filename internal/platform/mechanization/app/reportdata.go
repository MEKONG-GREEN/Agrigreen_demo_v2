package app

import (
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/mechanizationsrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ReportData hiện thực port.ReportData (FN-10): đọc KẾT QUẢ ĐÃ LƯU của một lần
// cân đối, lọc theo phạm vi pf/reports truyền vào. Không đọc diện tích, máy hay
// định mức hiện tại — số trên tệp xuất đúng bằng số đã lưu (BR-04).
type ReportData struct{ Svc *Service }

var _ mechanizationsrc.ReportData = ReportData{}

var errNoTenant = errors.New("mechanization: truy vấn báo cáo thiếu tenant")

// ErrSameSeason: tab đối chiếu chọn hai vụ trùng nhau (FN-10 luồng ngoại lệ).
var ErrSameSeason = problem.Validation.WithField("season_b", "Vui lòng chọn hai vụ khác nhau để đối chiếu")

func runInfo(r domain.BalanceRun) mechanizationsrc.RunInfo {
	mn := append([]string{}, r.MissingNorms...)
	return mechanizationsrc.RunInfo{
		RunID: r.ID, SeasonCode: r.Season.Code, SeasonYear: r.Season.Year, ReferenceDate: r.ReferenceDate,
		ComputedAt: r.EventTime, RuleVersion: r.RuleVersion, MissingNorms: mn, ThresholdsComplete: r.Thresholds.Complete(),
	}
}

// saved đọc lần cân đối và các dòng đã lưu trong phạm vi.
func (d ReportData) saved(ctx context.Context, q mechanizationsrc.ReportQuery) (*domain.BalanceRun, []domain.BalanceResult, error) {
	if q.TenantID == uuid.Nil {
		return nil, nil, errNoTenant
	}
	var run *domain.BalanceRun
	var res []domain.BalanceResult
	err := d.Svc.tx(ctx, q.TenantID, func(tx pgx.Tx) error {
		var err error
		run, err = d.Svc.resolveRun(ctx, tx, RunQuery{RunID: q.RunID, Season: domain.Season{Code: q.SeasonCode, Year: q.SeasonYear}})
		if err != nil {
			return err
		}
		if q.Scope.Empty() {
			return nil
		}
		res, err = store.Results(ctx, tx, store.ResultFilter{
			RunID: run.ID, All: q.Scope.All, Units: q.Scope.Units, ProvinceCode: q.ProvinceCode, CommuneCode: q.CommuneCode,
			MachineType: q.MachineType,
		})
		return err
	})
	return run, res, err
}

func figuresOf(g domain.Group) mechanizationsrc.Figures {
	f := mechanizationsrc.Figures{Capacity: g.Capacity, CoveragePct: g.CoveragePct, Status: string(g.Status), StatusLabel: domain.CoverageLabels[g.Status]}
	if g.Status != domain.CoverageNoData && g.Status != domain.CoverageNoNorm {
		req, whole := g.Required, g.RequiredWhole
		f.Required, f.RequiredWhole = &req, &whole
	}
	return f
}

type groupKey struct{ Province, Commune, Stage, Type string }

func less(a, b groupKey) bool {
	switch {
	case a.Province != b.Province:
		return a.Province < b.Province
	case a.Commune != b.Commune:
		return a.Commune < b.Commune
	case a.Stage != b.Stage:
		return a.Stage < b.Stage
	}
	return a.Type < b.Type
}

// groups gom các dòng đã lưu theo đơn vị × khâu × chủng loại; số liệu nhóm dùng
// đúng quy tắc tổng hợp của bản đồ (domain.Summarize) với ngưỡng ĐÃ LƯU của lần cân đối.
func groups(run domain.BalanceRun, res []domain.BalanceResult, level string) (map[groupKey]mechanizationsrc.Figures, map[groupKey]int, []groupKey) {
	rows := map[groupKey][]domain.ResultRow{}
	coops := map[groupKey]map[uuid.UUID]bool{}
	for _, r := range res {
		k := groupKey{Province: r.ProvinceCode, Stage: r.FarmStage, Type: r.MachineType}
		if level == LevelCommune {
			k.Commune = r.CommuneCode
		}
		rows[k] = append(rows[k], r.Row())
		if coops[k] == nil {
			coops[k] = map[uuid.UUID]bool{}
		}
		coops[k][r.CooperativeRef] = true
	}
	figs := map[groupKey]mechanizationsrc.Figures{}
	counts := map[groupKey]int{}
	keys := make([]groupKey, 0, len(rows))
	for k, rs := range rows {
		overall, _ := domain.Summarize(rs, run.Thresholds)
		figs[k], counts[k] = figuresOf(overall), len(coops[k])
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return less(keys[i], keys[j]) })
	return figs, counts, keys
}

func level(l string) (string, error) {
	switch l {
	case "", LevelProvince:
		return LevelProvince, nil
	case LevelCommune:
		return LevelCommune, nil
	}
	return "", problem.Validation.WithField("level", "province | commune")
}

// Coverage: tab "Tổng hợp theo khu vực".
func (d ReportData) Coverage(ctx context.Context, q mechanizationsrc.ReportQuery) (mechanizationsrc.CoverageData, error) {
	lv, err := level(q.Level)
	if err != nil {
		return mechanizationsrc.CoverageData{}, err
	}
	run, res, err := d.saved(ctx, q)
	if err != nil {
		return mechanizationsrc.CoverageData{}, err
	}
	out := mechanizationsrc.CoverageData{Run: runInfo(*run), Rows: []mechanizationsrc.CoverageRow{}}
	figs, counts, keys := groups(*run, res, lv)
	for _, k := range keys {
		out.Rows = append(out.Rows, mechanizationsrc.CoverageRow{
			ProvinceCode: k.Province, CommuneCode: k.Commune, FarmStage: k.Stage, MachineType: k.Type, Cooperatives: counts[k], Figures: figs[k],
		})
	}
	return out, nil
}

// Shortage: tab "Thiếu hụt theo HTX" — đúng các dòng đã lưu có gap < 0.
func (d ReportData) Shortage(ctx context.Context, q mechanizationsrc.ReportQuery) (mechanizationsrc.ShortageData, error) {
	run, res, err := d.saved(ctx, q)
	if err != nil {
		return mechanizationsrc.ShortageData{}, err
	}
	out := mechanizationsrc.ShortageData{Run: runInfo(*run), Rows: []mechanizationsrc.ShortageRow{}}
	orgs := map[uuid.UUID][2]string{}
	for _, r := range res {
		if r.Figures.Gap == nil || *r.Figures.Gap >= 0 {
			continue
		}
		info, ok := orgs[r.CooperativeRef]
		if !ok && d.Svc.Orgs != nil {
			if o, err := d.Svc.Orgs.Get(ctx, r.CooperativeRef); err == nil {
				info = [2]string{o.Code, o.Name}
			}
			orgs[r.CooperativeRef] = info
		}
		out.Rows = append(out.Rows, mechanizationsrc.ShortageRow{
			ResultID: r.ID, CooperativeRef: r.CooperativeRef, CooperativeCode: info[0], CooperativeName: info[1],
			ProvinceCode: r.ProvinceCode, CommuneCode: r.CommuneCode, FarmStage: r.FarmStage, MachineType: r.MachineType,
			Capacity: r.Capacity, Required: r.Figures.Required, RequiredWhole: r.Figures.RequiredWhole, Shortage: -*r.Figures.Gap,
			CoveragePct: r.Figures.CoveragePct, Status: string(r.Figures.Status), AreaSource: r.AreaSource,
		})
	}
	sort.SliceStable(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		if a.ProvinceCode != b.ProvinceCode {
			return a.ProvinceCode < b.ProvinceCode
		}
		if a.CommuneCode != b.CommuneCode {
			return a.CommuneCode < b.CommuneCode
		}
		if a.CooperativeCode != b.CooperativeCode {
			return a.CooperativeCode < b.CooperativeCode
		}
		return a.MachineType < b.MachineType
	})
	return out, nil
}

// SeasonCompare: tab "Đối chiếu giữa các vụ" — hai lần cân đối đã lưu (mới nhất
// của Vụ A và Vụ B); chênh lệch = B − A.
func (d ReportData) SeasonCompare(ctx context.Context, q mechanizationsrc.CompareQuery) (mechanizationsrc.CompareData, error) {
	lv, err := level(q.Level)
	if err != nil {
		return mechanizationsrc.CompareData{}, err
	}
	if q.SeasonCode == "" || q.SeasonCodeB == "" {
		return mechanizationsrc.CompareData{}, problem.Validation.WithField("season", "chọn Vụ A và Vụ B")
	}
	if q.SeasonCode == q.SeasonCodeB && q.SeasonYear == q.SeasonYearB {
		return mechanizationsrc.CompareData{}, ErrSameSeason
	}
	qa := q.ReportQuery
	qa.RunID = uuid.Nil
	runA, resA, err := d.saved(ctx, qa)
	if err != nil {
		return mechanizationsrc.CompareData{}, err
	}
	qb := qa
	qb.SeasonCode, qb.SeasonYear = q.SeasonCodeB, q.SeasonYearB
	runB, resB, err := d.saved(ctx, qb)
	if err != nil {
		return mechanizationsrc.CompareData{}, err
	}
	fa, _, ka := groups(*runA, resA, lv)
	fb, _, kb := groups(*runB, resB, lv)
	seen := map[groupKey]bool{}
	var keys []groupKey
	for _, k := range append(ka, kb...) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return less(keys[i], keys[j]) })
	out := mechanizationsrc.CompareData{RunA: runInfo(*runA), RunB: runInfo(*runB), Rows: []mechanizationsrc.CompareRow{}}
	for _, k := range keys {
		row := mechanizationsrc.CompareRow{ProvinceCode: k.Province, CommuneCode: k.Commune, FarmStage: k.Stage, MachineType: k.Type}
		if a, ok := fa[k]; ok {
			row.A = &a
		}
		if b, ok := fb[k]; ok {
			row.B = &b
		}
		if row.A != nil && row.B != nil {
			diff := row.B.Capacity - row.A.Capacity
			row.CapacityDiff = &diff
			if row.A.CoveragePct != nil && row.B.CoveragePct != nil {
				p := row.B.CoveragePct.Sub(*row.A.CoveragePct)
				row.PctDiff = &p
			}
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}
