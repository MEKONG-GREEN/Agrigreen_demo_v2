package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// SnapshotPath / ResultPath là đường Internal API optimizer dùng (mTLS).
func SnapshotPath(id uuid.UUID) string {
	return "/internal/v1/simulations/" + id.String() + "/snapshot"
}

// ResultPath là đường nộp kết quả.
func ResultPath(id uuid.UUID) string { return "/internal/v1/simulations/" + id.String() + "/result" }

// BuildRequest là đầu vào dựng ảnh chụp (không phụ thuộc DB — kiểm thử với cổng giả).
type BuildRequest struct {
	TenantID     uuid.UUID
	Simulation   domain.Simulation
	Scenario     domain.Scenario
	BaselineCost *string // Cost/Ton của kịch bản baseline (baseline_mode = scenario), nếu có kết quả
	SnapshotID   uuid.UUID
}

// BuildSnapshot dựng ảnh chụp đầu vào qua ba cổng (ParamSet, SupplySource, Router):
// giải tham số tại thời điểm yêu cầu, lấy nguồn cung, xét bán kính FN-05, định
// tuyến MỌI chặng engine có thể cần (không cache — kết quả nằm trong ảnh chụp),
// rồi bỏ mọi tên/toạ độ (RB-7).
func (s *Service) BuildSnapshot(ctx context.Context, r BuildRequest) (domain.Snapshot, error) {
	if s.Supply == nil {
		return domain.Snapshot{}, ErrDependency.WithDetail("chưa nối nguồn cung HTX × vụ (SupplySource)")
	}
	at := r.Simulation.RequestedAt
	params, err := s.resolveParams(ctx, r.TenantID, at)
	if err != nil {
		return domain.Snapshot{}, err
	}
	supply, err := s.Supply.Supply(ctx, r.TenantID, port.SupplyQuery{ReferenceYear: r.Scenario.ReferenceYear, At: at})
	if err != nil {
		return domain.Snapshot{}, err
	}
	hubs := make([]domain.HubSpec, 0, len(r.Scenario.Hubs))
	snapHubs := make([]domain.SnapHub, 0, len(r.Scenario.Hubs))
	for _, h := range r.Scenario.Hubs {
		sh := domain.SnapHub{ScenarioHubID: h.ID.String(), HubRef: h.HubID.String(), Code: h.HubCode, SortOrder: h.SortOrder,
			ServiceRadiusKm: h.ServiceRadiusKm.String(), DesignCapacityTons: h.DesignCapacityTons.String(),
			ModeFieldHub: h.ModeFieldHub, ModeHubPlant: h.ModeHubPlant, LandMode: h.LandMode}
		if h.BargePayloadTons != nil {
			v := h.BargePayloadTons.String()
			sh.BargePayloadTons = &v
		}
		snapHubs = append(snapHubs, sh)
		hubs = append(hubs, domain.HubSpec{Snap: sh, HubRef: h.HubID, Location: h.HubLocation, RadiusKm: h.ServiceRadiusKm})
	}
	plan := domain.PlanRoutes(hubs, r.Scenario.PlantLocation, supply.Cooperatives)
	if len(plan.Cooperatives) == 0 {
		return domain.Snapshot{}, ErrNoSupply
	}
	network := domain.NoNetwork
	results := make([]port.RouteResult, len(plan.Needs))
	if s.Router != nil {
		if network, err = s.Router.NetworkVersion(ctx, r.TenantID); err != nil {
			return domain.Snapshot{}, err
		}
		if network == "" {
			network = domain.NoNetwork
		}
		reqs := make([]port.RouteRequest, len(plan.Needs))
		for i, n := range plan.Needs {
			reqs[i] = port.RouteRequest{From: n.FromLoc, To: n.ToLoc, Mode: n.Mode}
		}
		if results, err = s.Router.Route(ctx, r.TenantID, network, reqs); err != nil {
			return domain.Snapshot{}, err
		}
		if len(results) != len(reqs) {
			return domain.Snapshot{}, fmt.Errorf("simulation: router trả %d kết quả cho %d chặng", len(results), len(reqs))
		}
	}
	routes := make([]domain.SnapRoute, len(plan.Needs))
	for i, n := range plan.Needs {
		routes[i] = domain.ToRoute(n, results[i], params.CorrectionFactor(n.Mode))
	}
	coops, seasons, obs, err := domain.SupplyToSnapshot(plan.Cooperatives, supply.Seasons, supply.Observations)
	if err != nil {
		return domain.Snapshot{}, ErrSupplyInvalid.WithDetail(err.Error())
	}
	sc := r.Scenario
	snap := domain.Snapshot{
		SchemaVersion: domain.SnapshotSchemaVersion, SnapshotID: r.SnapshotID.String(), SimulationID: r.Simulation.ID.String(),
		TenantRef: r.TenantID.String(), AlgorithmVersion: domain.AlgorithmVersion, NetworkVersion: network, CreatedAt: domain.FormatTime(s.now()),
		Scenario: domain.SnapScenario{ScenarioID: sc.ID.String(), Code: sc.Code, LifecycleYears: sc.LifecycleYears, Discounted: sc.Discounted,
			BaselineMode: sc.BaselineMode, BaselineScenarioCostPerTon: r.BaselineCost},
		Hubs: snapHubs, Cooperatives: coops, Seasons: seasons, HarvestObservations: obs, Radius: plan.Radius, Routes: routes,
		Params: params, Options: r.Simulation.Options,
	}
	if snap.Radius == nil {
		snap.Radius = []domain.SnapRadius{}
	}
	if snap.Options.SensitivityCodes == nil {
		snap.Options.SensitivityCodes = []string{}
	}
	if sc.BaselineManualCostPerTon != nil {
		v := sc.BaselineManualCostPerTon.String()
		snap.Scenario.BaselineManualCostPerTon = &v
	}
	if sc.BaselineScenarioID != nil {
		v := sc.BaselineScenarioID.String()
		snap.Scenario.BaselineScenarioID = &v
	}
	if sc.PlantRef != nil {
		v := sc.PlantRef.String()
		snap.Scenario.Plant.PlantRef = &v
	}
	if sc.PlantAnnualDemandTons != nil {
		v := sc.PlantAnnualDemandTons.String()
		snap.Scenario.Plant.AnnualDemandTons = &v
	}
	return snap, nil
}

// HandleRunSubmitted là inbox.Handler của worker cho pf.simulation.run.submitted
// (consumer port.SnapshotConsumer, pool vai simulation_app): dựng ảnh chụp bất
// biến, ghim vào việc mô phỏng, phát pf.simulation.job.requested — cùng giao dịch
// với inbox (R-61). Lỗi nghiệp vụ vĩnh viễn → việc hỏng + job.completed(failed).
func (s *Service) HandleRunSubmitted(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	var pl port.RunSubmittedV1
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		return fmt.Errorf("simulation: payload %s hỏng: %w", env.EventType, err)
	}
	id, err := uuid.Parse(pl.SimulationID)
	if err != nil {
		return fmt.Errorf("simulation: simulation_id không hợp lệ: %w", err)
	}
	sim, err := store.SimulationByID(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if sim == nil || sim.Status != domain.StatusQueued {
		return nil // giao lặp: đã dựng ảnh chụp
	}
	sc, err := store.ScenarioByID(ctx, tx, sim.ScenarioID, false)
	if err != nil {
		return err
	}
	if sc == nil {
		return s.fail(ctx, tx, *sim, ErrScenarioNotFound, env.TraceID)
	}
	if len(sc.Hubs) == 0 {
		return s.fail(ctx, tx, *sim, ErrScenarioWithoutHubs, env.TraceID)
	}
	req := BuildRequest{TenantID: env.TenantID, Simulation: *sim, Scenario: *sc, SnapshotID: ids.New()}
	if sc.BaselineMode == domain.BaselineScenario && sc.BaselineScenarioID != nil {
		c, err := store.LatestCostPerTon(ctx, tx, *sc.BaselineScenarioID)
		if err != nil {
			return err
		}
		if c != nil {
			v := c.String()
			req.BaselineCost = &v
		}
	}
	snap, err := s.BuildSnapshot(ctx, req)
	var p *problem.Problem
	if errors.As(err, &p) && (p.Status < 500 || p.Code == ErrDependency.Code) {
		return s.fail(ctx, tx, *sim, p, env.TraceID)
	}
	if err != nil {
		return err
	}
	raw, sha, err := snap.Canonical()
	if err != nil {
		return err
	}
	now := s.now()
	meta := domain.SnapshotMeta{ID: req.SnapshotID, SimulationID: sim.ID, SHA256: sha, ParamSetVersion: snap.Params.SetVersion,
		AllParamsApproved: snap.Params.AllApproved, NetworkVersion: snap.NetworkVersion, AlgorithmVersion: snap.AlgorithmVersion,
		CooperativeCount: len(snap.Cooperatives), RouteCount: len(snap.Routes), CreatedAt: now}
	if err := store.InsertSnapshot(ctx, tx, env.TenantID, sim.ScenarioID, meta, raw); err != nil {
		return err
	}
	if err := store.SetRunning(ctx, tx, sim.ID, meta.ID); err != nil {
		return err
	}
	s.log().Info("đã dựng ảnh chụp mô phỏng", "event", "simulation.snapshot", "tenant_id", env.TenantID, "simulation_id", sim.ID,
		"cooperatives", meta.CooperativeCount, "routes", meta.RouteCount, "all_params_approved", meta.AllParamsApproved, "outcome", "ok")
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: env.TenantID, Type: port.EventJobRequested, SchemaVersion: 1, OccurredAt: now, TraceID: env.TraceID,
		Producer: port.Producer,
		Payload: port.JobRequestedV1{SimulationID: sim.ID.String(), SnapshotID: meta.ID.String(), SnapshotSHA256: sha,
			SnapshotPath: SnapshotPath(sim.ID), ResultPath: ResultPath(sim.ID), AlgorithmVersion: snap.AlgorithmVersion,
			Mode: snap.Options.Mode, RequestedAt: ts(sim.RequestedAt)},
	})
}

// fail đánh dấu việc hỏng vĩnh viễn: dòng sổ + trạng thái + job.completed(failed).
func (s *Service) fail(ctx context.Context, tx pgx.Tx, sim domain.Simulation, p *problem.Problem, trace string) error {
	now := s.now()
	code := p.Code
	detail := p.Detail
	if len(detail) > 500 {
		detail = detail[:500]
	}
	var dptr *string
	if strings.TrimSpace(detail) != "" {
		dptr = &detail
	}
	s.log().Warn("việc mô phỏng hỏng", "event", "simulation.failed", "tenant_id", sim.TenantID, "simulation_id", sim.ID, "error_code", code, "outcome", "rejected")
	ch := store.Change{ID: ids.New(), TenantID: sim.TenantID, ScenarioID: sim.ScenarioID, Kind: domain.ChangeSimulationFailed, SimulationID: &sim.ID,
		EventTime: now, ActorRef: sim.RequestedBy, IdemKey: "failed:" + sim.ID.String(), Payload: map[string]any{"error_code": code}}
	if err := store.AppendChange(ctx, tx, &ch); err != nil {
		return err
	}
	if err := store.Finish(ctx, tx, sim.ID, domain.StatusFailed, &code, dptr, now); err != nil {
		return err
	}
	return enqueueCompleted(ctx, tx, sim, domain.StatusFailed, &code, nil, false, now, trace)
}

func enqueueCompleted(ctx context.Context, tx pgx.Tx, sim domain.Simulation, status string, code, sha *string, eligible bool, at time.Time, trace string) error {
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: sim.TenantID, Type: port.EventJobCompleted, SchemaVersion: 1, OccurredAt: at, TraceID: trace,
		Producer: port.Producer,
		Payload: port.JobCompletedV1{SimulationID: sim.ID.String(), ScenarioID: sim.ScenarioID.String(), RequestedBy: sim.RequestedBy.String(),
			Status: status, ErrorCode: code, ResultSHA256: sha, OfficialEligible: eligible, CompletedAt: ts(at)},
	})
}
