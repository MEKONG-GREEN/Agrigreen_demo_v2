package app

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// BalanceRequest là yêu cầu cân đối một vụ (FN-09 B2–B5).
type BalanceRequest struct {
	Season  domain.Season
	IdemKey string
}

// BalanceOutcome là lần cân đối đã lưu.
type BalanceOutcome struct {
	Run      domain.BalanceRun
	Replayed bool
}

// ComputeBalance tính và LƯU cân đối cung–cầu của một vụ (chạy đồng bộ trong request).
//
// R-52: với ĐTK 1 (~1.200 HTX × vài chủng loại thí điểm) việc tính gồm một lượt
// đọc diện tích, một truy vấn đếm máy tại T và một lô INSERT — dự kiến dưới 2
// giây. Toàn bộ nghiệp vụ nằm ở RunBalance(ctx, principal, scope, req) — không
// phụ thuộc HTTP — để khi quy mô vượt ngưỡng, handler chỉ cần ghi job + outbox
// và trả 202; worker gọi đúng RunBalance với principal hệ thống.
func (s *Service) ComputeBalance(ctx context.Context, req BalanceRequest) (BalanceOutcome, error) {
	p, sc, err := s.caller(ctx, writeRoles)
	if err != nil {
		return BalanceOutcome{}, err
	}
	return s.RunBalance(ctx, p, sc, req)
}

// balanceInputs là mọi đầu vào của một lần cân đối, đã đọc xong.
type balanceInputs struct {
	season     domain.Season
	refDate    time.Time // ngày T (00:00 UTC mang ngày)
	pilot      []string
	types      []mdport.CatalogItem // chủng loại thuộc khâu thí điểm
	thresholds domain.Thresholds
	norms      map[string]*domain.ParamRef // theo chủng loại
	coops      []coopInfo
	areas      map[uuid.UUID]domain.SeasonArea
	counts     map[uuid.UUID]map[string]store.CountRow // HTX → chủng loại
}

// RunBalance là phần nghiệp vụ của cân đối, dùng chung cho request đồng bộ và worker.
func (s *Service) RunBalance(ctx context.Context, p authz.Principal, sc authz.Scope, req BalanceRequest) (BalanceOutcome, error) {
	if err := requireIdem(req.IdemKey); err != nil {
		return BalanceOutcome{}, err
	}
	item, err := s.season(ctx, req.Season)
	if err != nil {
		return BalanceOutcome{}, err
	}
	var prev *domain.BalanceRun
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.RunByIdem(ctx, tx, req.IdemKey)
		return e
	}); err != nil {
		return BalanceOutcome{}, err
	}
	if prev != nil {
		return BalanceOutcome{Run: *prev, Replayed: true}, nil
	}
	in, err := s.balanceInputs(ctx, p, sc, req.Season, item)
	if err != nil {
		return BalanceOutcome{}, err
	}
	run, results := computeRun(in)
	run.ID, run.TenantID, run.EventTime, run.ActorRef, run.IdemKey = ids.New(), p.TenantID, s.now(), p.ActorRef, req.IdemKey
	for _, r := range results {
		r.ID, r.RunID = ids.New(), run.ID
	}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.InsertRun(ctx, tx, &run); err != nil {
			return err
		}
		if err := store.InsertResults(ctx, tx, run, results); err != nil {
			return err
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: p.TenantID, Type: port.EventBalanceComputed, SchemaVersion: 1, OccurredAt: run.EventTime,
			TraceID: telemetry.TraceID(ctx), Producer: "pf/mechanization",
			Payload: port.BalanceComputedV1{
				RunID: run.ID.String(), SeasonCode: run.Season.Code, SeasonYear: run.Season.Year,
				ReferenceDate: run.ReferenceDate.Format(time.DateOnly), RuleVersion: run.RuleVersion, ResultCount: run.ResultCount,
				StatusCounts: run.StatusCounts, MissingNorms: run.MissingNorms, ComputedAt: run.EventTime.Format(time.RFC3339), Seq: run.Seq,
			},
		})
	})
	if err = classify(err); err != nil {
		return BalanceOutcome{}, err
	}
	return BalanceOutcome{Run: run}, nil
}

// balanceInputs đọc danh mục, tham số tại T, HTX, diện tích và số máy tại T.
func (s *Service) balanceInputs(ctx context.Context, p authz.Principal, sc authz.Scope, sn domain.Season, item *mdport.CatalogItem) (balanceInputs, error) {
	in := balanceInputs{season: sn, refDate: sn.StartDate(item.Attrs["start_month"]), norms: map[string]*domain.ParamRef{},
		areas: map[uuid.UUID]domain.SeasonArea{}, counts: map[uuid.UUID]map[string]store.CountRow{}}
	at := dayStart(in.refDate) // tham số tra theo thời điểm của vụ (mục 16), không theo hiện tại

	// FN-08 BR-06, FN-09 BR-01: chỉ các khâu bật cờ thí điểm.
	stages, err := s.Catalogs.List(ctx, mdport.CatalogFarmStage)
	if err != nil {
		return in, ErrDependency.WithDetail("không đọc được danh mục khâu").WithCause(err)
	}
	pilot := map[string]bool{}
	for _, st := range stages {
		if st.Active && st.Attrs["pilot"] == "true" {
			pilot[st.Code] = true
			in.pilot = append(in.pilot, st.Code)
		}
	}
	sort.Strings(in.pilot)
	types, err := s.Catalogs.List(ctx, mdport.CatalogMachineType)
	if err != nil {
		return in, ErrDependency.WithDetail("không đọc được danh mục chủng loại máy").WithCause(err)
	}
	for _, t := range types {
		if t.Active && pilot[t.Attrs["farm_stage"]] {
			in.types = append(in.types, t)
		}
	}
	sort.Slice(in.types, func(i, j int) bool { return in.types[i].Code < in.types[j].Code })

	// QT-02: ngưỡng có hiệu lực tại T.
	for _, th := range []struct {
		key string
		dst **domain.ParamRef
	}{
		{mdport.ParamCGHCoverageSufficient, &in.thresholds.Sufficient},
		{mdport.ParamCGHCoverageAttention, &in.thresholds.Attention},
		{mdport.ParamCGHCoverageSurplusFrom, &in.thresholds.Surplus},
	} {
		if *th.dst, err = s.paramAt(ctx, th.key, at); err != nil {
			return in, err
		}
	}
	// QT-01: định mức hiệu lực TẠI VỤ theo chủng loại × khâu (US-CFG-02 AC-2).
	for _, t := range in.types {
		if in.norms[t.Code], err = s.paramAt(ctx, mdport.NormKey(t.Code, t.Attrs["farm_stage"]), at); err != nil {
			return in, err
		}
	}

	var areas []domain.SeasonArea
	var machineCoops []uuid.UUID
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if areas, e = store.AreasOfSeason(ctx, tx, sn, uuid.Nil); e != nil {
			return e
		}
		machineCoops, e = store.MachineCoops(ctx, tx)
		return e
	}); err != nil {
		return in, err
	}
	seen := map[uuid.UUID]bool{}
	var refs []uuid.UUID
	for _, a := range areas {
		in.areas[a.CooperativeRef] = a
		if !seen[a.CooperativeRef] {
			seen[a.CooperativeRef] = true
			refs = append(refs, a.CooperativeRef)
		}
	}
	for _, c := range machineCoops {
		if !seen[c] {
			seen[c] = true
			refs = append(refs, c)
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].String() < refs[j].String() })
	var coopRefs []uuid.UUID
	for _, ref := range refs {
		ci, err := s.coop(ctx, ref, in.refDate)
		if err != nil {
			return in, err
		}
		// FN-08 BR-03, FN-03 BR-03: HTX vô hiệu hóa không tính cân đối.
		if ci == nil || !ci.Active || !visible(sc, ci) {
			continue
		}
		in.coops = append(in.coops, *ci)
		coopRefs = append(coopRefs, ref)
	}
	if len(coopRefs) == 0 {
		return in, nil
	}
	var counts []store.CountRow
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		counts, e = store.CountsAt(ctx, tx, in.refDate, dayStart(in.refDate.AddDate(0, 0, 1)), coopRefs)
		return e
	}); err != nil {
		return in, err
	}
	for _, c := range counts {
		if in.counts[c.CooperativeRef] == nil {
			in.counts[c.CooperativeRef] = map[string]store.CountRow{}
		}
		in.counts[c.CooperativeRef][c.MachineType] = c
	}
	return in, nil
}

// computeRun áp QT-01/QT-02 cho mọi HTX × chủng loại thí điểm — thuần, không I/O.
func computeRun(in balanceInputs) (domain.BalanceRun, []*domain.BalanceResult) {
	run := domain.BalanceRun{
		Season: in.season, ReferenceDate: in.refDate, PilotStages: in.pilot, Thresholds: in.thresholds,
		StatusCounts: map[string]int{}, MissingNorms: []string{}, RuleVersion: domain.BalanceRuleVersion,
	}
	if run.PilotStages == nil {
		run.PilotStages = []string{}
	}
	for _, c := range domain.AllCoverages {
		run.StatusCounts[string(c)] = 0
	}
	for _, t := range in.types {
		if in.norms[t.Code] == nil {
			run.MissingNorms = append(run.MissingNorms, mdport.NormKey(t.Code, t.Attrs["farm_stage"]))
		}
	}
	var results []*domain.BalanceResult
	for _, c := range in.coops {
		var area *decimal.Decimal
		a, hasArea := in.areas[c.Ref]
		if hasArea {
			v := a.AreaHa
			area = &v
		}
		for _, t := range in.types {
			stage := t.Attrs["farm_stage"]
			cnt := in.counts[c.Ref][t.Code]
			capacity := cnt.ByStatus[string(domain.StatusActive)]
			r := &domain.BalanceResult{
				Season: in.season, ReferenceDate: in.refDate, CooperativeRef: c.Ref, CommuneCode: c.CommuneCode, ProvinceCode: c.ProvinceCode,
				FarmStage: stage, MachineType: t.Code, AreaHa: area, NormKey: mdport.NormKey(t.Code, stage), Norm: in.norms[t.Code],
				MachinesTotal: cnt.Total, Capacity: capacity,
			}
			if hasArea {
				r.AreaSource, r.AreaLocked = a.Source, a.Locked
			}
			r.Figures = domain.ComputeBalance(domain.BalanceInput{
				AreaHa: area, Norm: r.Norm, MachinesTotal: cnt.Total, Capacity: capacity, Thresholds: in.thresholds,
			})
			run.StatusCounts[string(r.Figures.Status)]++
			results = append(results, r)
		}
	}
	run.ResultCount = len(results)
	return run, results
}
