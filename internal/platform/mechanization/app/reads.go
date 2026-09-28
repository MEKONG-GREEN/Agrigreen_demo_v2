package app

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Mọi số liệu cân đối ở tệp này đọc từ KẾT QUẢ ĐÃ LƯU (FN-10 BR-04), không tính lại.

// Cấp tổng hợp (FN-08: tỉnh → xã → HTX).
const (
	LevelProvince    = "province"
	LevelCommune     = "commune"
	LevelCooperative = "cooperative"
)

// RunQuery chọn một lần cân đối: RunID, hoặc lần mới nhất của vụ (Season.Code rỗng = mới nhất mọi vụ).
type RunQuery struct {
	RunID  uuid.UUID
	Season domain.Season
}

func (s *Service) resolveRun(ctx context.Context, tx pgx.Tx, q RunQuery) (*domain.BalanceRun, error) {
	var run *domain.BalanceRun
	var err error
	if q.RunID != uuid.Nil {
		run, err = store.RunByID(ctx, tx, q.RunID)
	} else {
		run, err = store.LatestRun(ctx, tx, q.Season)
	}
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, ErrRunNotFound
	}
	return run, nil
}

func scopeFilter(sc authz.Scope, f store.ResultFilter) store.ResultFilter {
	f.All, f.Units = sc.All, sc.Units
	return f
}

// RunView là một lần cân đối kèm các dòng kết quả đã lưu.
type RunView struct {
	Run     domain.BalanceRun
	Results []domain.BalanceResult
}

// GetRun đọc một lần cân đối đã lưu và các dòng trong phạm vi của người gọi.
func (s *Service) GetRun(ctx context.Context, q RunQuery, f store.ResultFilter) (RunView, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return RunView{}, err
	}
	var out RunView
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		run, err := s.resolveRun(ctx, tx, q)
		if err != nil {
			return err
		}
		f.RunID = run.ID
		res, err := store.Results(ctx, tx, scopeFilter(sc, f))
		out = RunView{Run: *run, Results: res}
		return err
	})
	if out.Results == nil {
		out.Results = []domain.BalanceResult{}
	}
	return out, err
}

// ListRuns liệt kê các lần cân đối đã lưu của một vụ (hoặc mọi vụ).
func (s *Service) ListRuns(ctx context.Context, seasonCode string, year int) ([]domain.BalanceRun, error) {
	p, _, err := s.caller(ctx, readRoles)
	if err != nil {
		return nil, err
	}
	var out []domain.BalanceRun
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListRuns(ctx, tx, seasonCode, year, false)
		return e
	})
	if out == nil {
		out = []domain.BalanceRun{}
	}
	return out, err
}

// CoverageQuery là tham số bản đồ/bảng mức đáp ứng theo địa bàn (FN-08, FN-10 tab 1–2).
type CoverageQuery struct {
	RunQuery
	Level        string
	ProvinceCode string
	CommuneCode  string
	FarmStage    string
	MachineType  string
}

// CoverageItem là mức đáp ứng của một đơn vị (tỉnh, xã hoặc HTX).
type CoverageItem struct {
	UnitCode       string // mã tỉnh/xã; với HTX là mã xã
	CooperativeRef uuid.UUID
	// CooperativeCode: Mã HTX (CGH BR-01, FN-11) — chỉ ở cấp HTX (G-10).
	CooperativeCode string
	Name            string
	ProvinceCode    string
	CommuneCode     string
	Cooperatives    int
	AreaSource      string // chỉ ở cấp HTX: [App HTX]/[Nhập tay]
	AreaLocked      bool
	Overall         domain.Group
	Stages          map[string]domain.Group
}

// CoverageView là kết quả tổng hợp theo cấp.
type CoverageView struct {
	Run     domain.BalanceRun
	Level   string
	Summary domain.Group
	Stages  map[string]domain.Group
	Items   []CoverageItem
}

// Coverage tổng hợp mức đáp ứng đã lưu theo tỉnh → xã → HTX, kèm màu trạng thái.
// Mức của một đơn vị là mức của khâu thấp nhất (domain.Summarize).
func (s *Service) Coverage(ctx context.Context, q CoverageQuery) (CoverageView, error) {
	if q.Level == "" {
		q.Level = LevelProvince
	}
	if q.Level != LevelProvince && q.Level != LevelCommune && q.Level != LevelCooperative {
		return CoverageView{}, problem.Validation.WithField("level", "province | commune | cooperative")
	}
	rv, err := s.GetRun(ctx, q.RunQuery, store.ResultFilter{
		ProvinceCode: q.ProvinceCode, CommuneCode: q.CommuneCode, FarmStage: q.FarmStage, MachineType: q.MachineType,
	})
	if err != nil {
		return CoverageView{}, err
	}
	return s.coverageOf(ctx, rv, q.Level), nil
}

func (s *Service) coverageOf(ctx context.Context, rv RunView, level string) CoverageView {
	th := rv.Run.Thresholds
	type bucket struct {
		item  CoverageItem
		rows  []domain.ResultRow
		coops map[uuid.UUID]bool
	}
	buckets := map[string]*bucket{}
	var order []string
	all := make([]domain.ResultRow, 0, len(rv.Results))
	for _, r := range rv.Results {
		all = append(all, r.Row())
		key := r.ProvinceCode
		switch level {
		case LevelCommune:
			key = r.CommuneCode
		case LevelCooperative:
			key = r.CooperativeRef.String()
		}
		b, ok := buckets[key]
		if !ok {
			b = &bucket{coops: map[uuid.UUID]bool{}, item: CoverageItem{UnitCode: key, ProvinceCode: r.ProvinceCode}}
			switch level {
			case LevelCommune:
				b.item.CommuneCode = r.CommuneCode
			case LevelCooperative:
				b.item.UnitCode, b.item.CooperativeRef, b.item.CommuneCode = r.CommuneCode, r.CooperativeRef, r.CommuneCode
				b.item.AreaSource, b.item.AreaLocked = r.AreaSource, r.AreaLocked
			}
			buckets[key] = b
			order = append(order, key)
		}
		b.rows = append(b.rows, r.Row())
		b.coops[r.CooperativeRef] = true
	}
	out := CoverageView{Run: rv.Run, Level: level, Items: []CoverageItem{}}
	out.Summary, out.Stages = domain.Summarize(all, th)
	names := map[string]string{}
	for _, k := range order {
		b := buckets[k]
		b.item.Overall, b.item.Stages = domain.Summarize(b.rows, th)
		b.item.Cooperatives = len(b.coops)
		b.item.Name = s.unitName(ctx, level, b.item, rv.Run.ReferenceDate, names)
		if level == LevelCooperative {
			b.item.CooperativeCode = s.coopCode(ctx, b.item.CooperativeRef, names)
		}
		out.Items = append(out.Items, b.item)
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		if out.Items[i].ProvinceCode != out.Items[j].ProvinceCode {
			return out.Items[i].ProvinceCode < out.Items[j].ProvinceCode
		}
		return out.Items[i].UnitCode+out.Items[i].CooperativeRef.String() < out.Items[j].UnitCode+out.Items[j].CooperativeRef.String()
	})
	return out
}

// unitName trả tên hiển thị của đơn vị (lỗi tra cứu → rỗng, không chặn báo cáo).
func (s *Service) unitName(ctx context.Context, level string, it CoverageItem, at time.Time, cache map[string]string) string {
	key := level + ":" + it.UnitCode + it.CooperativeRef.String()
	if n, ok := cache[key]; ok {
		return n
	}
	name := ""
	switch level {
	case LevelCooperative:
		if o, err := s.Orgs.Get(ctx, it.CooperativeRef); err == nil {
			name = o.Name
		}
	default:
		if it.UnitCode != "" {
			if u, err := s.unitAt(ctx, it.UnitCode, at); err == nil && u != nil {
				name = u.Name
			}
		}
	}
	cache[key] = name
	return name
}

// Dashboard là FN-11: 4 thẻ chỉ số và bảng "HTX cần ưu tiên".
type Dashboard struct {
	Run             *domain.BalanceRun
	TotalMachines   int // máy đang hoạt động (chưa vô hiệu hóa) trong phạm vi
	TotalCoops      int // HTX có trong lần cân đối
	CoopsSufficient int // HTX đủ máy (Đủ hoặc Thừa)
	CoopsShortage   int // HTX thiếu máy
	CoopsByStatus   map[string]int
	Priority        []CoverageItem // HTX mức đáp ứng thấp nhất trước
}

// PriorityLimit là số dòng của bảng "HTX cần ưu tiên".
const PriorityLimit = 10

// GetDashboard dựng dashboard từ lần cân đối đã lưu mới nhất của vụ (hoặc của mọi vụ).
func (s *Service) GetDashboard(ctx context.Context, q RunQuery) (Dashboard, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return Dashboard{}, err
	}
	d := Dashboard{CoopsByStatus: map[string]int{}, Priority: []CoverageItem{}}
	for _, c := range domain.AllCoverages {
		d.CoopsByStatus[string(c)] = 0
	}
	var rv *RunView
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		run, err := s.resolveRun(ctx, tx, q)
		if errors.Is(err, ErrRunNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		res, err := store.Results(ctx, tx, scopeFilter(sc, store.ResultFilter{RunID: run.ID}))
		rv = &RunView{Run: *run, Results: res}
		return err
	})
	if err != nil {
		return Dashboard{}, err
	}
	var coops []uuid.UUID
	if rv != nil {
		d.Run = &rv.Run
		cv := s.coverageOf(ctx, *rv, LevelCooperative)
		d.TotalCoops = len(cv.Items)
		var ranked []CoverageItem
		for _, it := range cv.Items {
			coops = append(coops, it.CooperativeRef)
			d.CoopsByStatus[string(it.Overall.Status)]++
			switch it.Overall.Status {
			case domain.CoverageSufficient, domain.CoverageSurplus:
				d.CoopsSufficient++
			case domain.CoverageShortage:
				d.CoopsShortage++
			}
			if it.Overall.CoveragePct != nil {
				ranked = append(ranked, it)
			}
		}
		sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Overall.CoveragePct.LessThan(*ranked[j].Overall.CoveragePct) })
		if len(ranked) > PriorityLimit {
			ranked = ranked[:PriorityLimit]
		}
		d.Priority = append(d.Priority, ranked...)
	}
	if sc.All || len(coops) > 0 {
		if sc.All {
			coops = nil // mọi HTX
		}
		err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			d.TotalMachines, e = store.CountActive(ctx, tx, coops)
			return e
		})
	}
	return d, err
}

// TrendPoint là một vụ trong xu hướng nhiều năm (FN-11 B3, AC-02).
type TrendPoint struct {
	Run           domain.BalanceRun
	Summary       domain.Group
	Stages        map[string]domain.Group
	CoopsByStatus map[string]int
}

// Trend trả xu hướng cân đối qua các vụ đã lưu (lần tính mới nhất của mỗi vụ), lọc theo mã vụ và khâu.
func (s *Service) Trend(ctx context.Context, seasonCode, stage string) ([]TrendPoint, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return nil, err
	}
	var views []RunView
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		runs, err := store.ListRuns(ctx, tx, seasonCode, 0, true)
		if err != nil {
			return err
		}
		for _, r := range runs {
			res, err := store.Results(ctx, tx, scopeFilter(sc, store.ResultFilter{RunID: r.ID, FarmStage: stage}))
			if err != nil {
				return err
			}
			views = append(views, RunView{Run: r, Results: res})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := []TrendPoint{}
	for _, v := range views {
		cv := s.coverageOf(ctx, v, LevelProvince)
		tp := TrendPoint{Run: v.Run, Summary: cv.Summary, Stages: cv.Stages, CoopsByStatus: map[string]int{}}
		byCoop := map[uuid.UUID][]domain.ResultRow{}
		for _, r := range v.Results {
			byCoop[r.CooperativeRef] = append(byCoop[r.CooperativeRef], r.Row())
		}
		for _, rows := range byCoop {
			g, _ := domain.Summarize(rows, v.Run.Thresholds)
			tp.CoopsByStatus[string(g.Status)]++
		}
		out = append(out, tp)
	}
	return out, nil
}

// PanelMachine là một máy trong panel HTX kèm nhãn chủ sở hữu đã che.
type PanelMachine struct {
	domain.Machine
	OwnerCode        string
	OwnerLabel       string `pii:"true"`
	OwnerMaskedPhone string `pii:"true"`
}

// Panel là panel chi tiết HTX trên bản đồ (FN-08 B5, US-MAP-02).
type Panel struct {
	CooperativeRef uuid.UUID
	Name           string
	CommuneCode    string
	ProvinceCode   string
	Machines       []PanelMachine // máy đang hoạt động (chưa vô hiệu hóa), theo khâu
	Coverage       *CoverageItem  // mức đáp ứng đã lưu gần nhất của HTX (nil = chưa có)
	Run            *domain.BalanceRun
}

// CooperativePanel dựng panel HTX: máy theo khâu + tình trạng kèm nhãn nguồn, chủ sở hữu (nhãn đã che), mức đáp ứng.
func (s *Service) CooperativePanel(ctx context.Context, coop uuid.UUID, q RunQuery) (Panel, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return Panel{}, err
	}
	ci, err := s.coop(ctx, coop, s.now())
	if err != nil {
		return Panel{}, err
	}
	if ci == nil || !visible(sc, ci) {
		return Panel{}, ErrCoopInvalid.WithDetail("không có HTX trong phạm vi")
	}
	pn := Panel{CooperativeRef: ci.Ref, Name: ci.Name, CommuneCode: ci.CommuneCode, ProvinceCode: ci.ProvinceCode, Machines: []PanelMachine{}}
	var machines []domain.Machine
	var owners map[uuid.UUID]domain.Owner
	var rv *RunView
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if machines, e = store.ListMachines(ctx, tx, store.MachineFilter{CooperativeRef: coop, Limit: 10000}); e != nil {
			return e
		}
		idList := make([]uuid.UUID, 0, len(machines))
		for _, m := range machines {
			idList = append(idList, m.OwnerID)
		}
		if owners, e = store.OwnersByIDs(ctx, tx, idList); e != nil {
			return e
		}
		run, e := s.resolveRun(ctx, tx, q)
		if errors.Is(e, ErrRunNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		res, e := store.Results(ctx, tx, store.ResultFilter{RunID: run.ID, All: true, CooperativeRef: coop})
		rv = &RunView{Run: *run, Results: res}
		return e
	})
	if err != nil {
		return Panel{}, err
	}
	sort.SliceStable(machines, func(i, j int) bool {
		if machines[i].FarmStage != machines[j].FarmStage {
			return machines[i].FarmStage < machines[j].FarmStage
		}
		return machines[i].Code < machines[j].Code
	})
	labels := map[uuid.UUID]PanelMachine{}
	for _, m := range machines {
		pm := PanelMachine{Machine: m}
		if o, ok := owners[m.OwnerID]; ok {
			if cached, ok := labels[o.ID]; ok {
				pm.OwnerCode, pm.OwnerLabel, pm.OwnerMaskedPhone = cached.OwnerCode, cached.OwnerLabel, cached.OwnerMaskedPhone
			} else {
				pm.OwnerCode = o.Code
				pm.OwnerLabel, pm.OwnerMaskedPhone, _ = s.label(ctx, o, ci.Name)
				labels[o.ID] = pm
			}
		}
		pn.Machines = append(pn.Machines, pm)
	}
	if rv != nil && len(rv.Results) > 0 {
		cv := s.coverageOf(ctx, *rv, LevelCooperative)
		if len(cv.Items) > 0 {
			it := cv.Items[0]
			pn.Coverage, pn.Run = &it, &rv.Run
		}
	}
	return pn, nil
}
