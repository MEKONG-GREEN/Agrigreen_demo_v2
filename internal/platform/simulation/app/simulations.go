package app

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// DefaultSensitivityCodes là bộ tham số độ nhạy mặc định (FN-18, như prototype).
var DefaultSensitivityCodes = []string{
	"straw_to_paddy_ratio", "collectable_ratio", "loss_ratio", "collection_labour_price", "collection_consumable_price",
	"road_freight_rate", "waterway_freight_rate", "distance_correction", "peak_inventory_factor", "capex_other", "discount_rate_pct",
}

// SubmitInput là yêu cầu chạy mô phỏng.
type SubmitInput struct {
	ScenarioID       uuid.UUID
	Sensitivity      bool     // FN-18: chạy kèm độ nhạy
	SensitivityCodes []string // rỗng + Sensitivity → bộ mặc định
	Mode             string   // evaluate (mặc định) | select_hubs (THỬ NGHIỆM)
	MaxHubs          *int
	IdemKey          string
}

// Submit ghi yêu cầu mô phỏng (mục 12, R-52): simulation (queued) + dòng sổ
// simulation_requested (kiểm toán) + outbox pf.simulation.run.submitted trong MỘT
// giao dịch; handler trả 202 + /v1/jobs/{id}. Ảnh chụp dựng ở worker.
func (s *Service) Submit(ctx context.Context, in SubmitInput) (domain.Simulation, bool, error) {
	p, err := require(ctx)
	if err != nil {
		return domain.Simulation{}, false, err
	}
	if in.IdemKey == "" {
		return domain.Simulation{}, false, problem.IdempotencyRequired
	}
	opts := domain.SnapOptions{Mode: in.Mode, SensitivityCodes: []string{}, MaxHubs: in.MaxHubs}
	if opts.Mode == "" {
		opts.Mode = "evaluate"
	}
	if opts.Mode != "evaluate" && opts.Mode != "select_hubs" {
		return domain.Simulation{}, false, problem.Validation.WithField("mode", "evaluate | select_hubs")
	}
	if in.MaxHubs != nil && (*in.MaxHubs < 1 || opts.Mode != "select_hubs") {
		return domain.Simulation{}, false, problem.Validation.WithField("max_hubs", "chỉ dùng với mode = select_hubs, ≥ 1")
	}
	if in.Sensitivity || len(in.SensitivityCodes) > 0 {
		codes := in.SensitivityCodes
		if len(codes) == 0 {
			codes = DefaultSensitivityCodes
		}
		for _, c := range codes {
			d, ok := domain.ParamByCode(c)
			if !ok || d.ManagedElsewhere || d.Min == "" || d.Max == "" {
				return domain.Simulation{}, false, problem.Validation.WithField("sensitivity_codes", "tham số "+c+" không có min/max để phân tích độ nhạy")
			}
			if !slices.Contains(opts.SensitivityCodes, c) {
				opts.SensitivityCodes = append(opts.SensitivityCodes, c)
			}
		}
	}
	now := s.now()
	var sim domain.Simulation
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		prev, err := store.SimulationByIdemKey(ctx, tx, in.IdemKey)
		if err != nil {
			return err
		}
		if prev != nil {
			sim, replayed = *prev, true
			return nil
		}
		sc, err := store.ScenarioByID(ctx, tx, in.ScenarioID, true)
		if err != nil {
			return err
		}
		if sc == nil {
			return ErrScenarioNotFound
		}
		if len(sc.Hubs) == 0 {
			return ErrScenarioWithoutHubs
		}
		sim = domain.Simulation{ID: ids.New(), TenantID: p.TenantID, ScenarioID: sc.ID, Options: opts, RequestedBy: p.ActorRef,
			RequestedAt: now, IdemKey: in.IdemKey, Status: domain.StatusQueued}
		ch := store.Change{ID: ids.New(), TenantID: p.TenantID, ScenarioID: sc.ID, Kind: domain.ChangeSimulationRequested, SimulationID: &sim.ID,
			EventTime: now, ActorRef: p.ActorRef, IdemKey: "simulation:" + in.IdemKey, Payload: map[string]any{"simulation_id": sim.ID, "options": opts}}
		if err := store.AppendChange(ctx, tx, &ch); err != nil {
			return err
		}
		sim.ScenarioVersion = ch.Seq
		if err := store.InsertSimulation(ctx, tx, &sim); err != nil {
			return err
		}
		if err := store.TouchScenario(ctx, tx, sc.ID, ch.Seq, now); err != nil {
			return err
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: p.TenantID, Type: port.EventRunSubmitted, SchemaVersion: 1, OccurredAt: now,
			TraceID: telemetry.TraceID(ctx), Producer: port.Producer,
			Payload: port.RunSubmittedV1{SimulationID: sim.ID.String(), ScenarioID: sc.ID.String(), RequestedBy: p.ActorRef.String(), RequestedAt: ts(now)},
		})
	})
	if isUnique(err) {
		if rerr := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			prev, e := store.SimulationByIdemKey(ctx, tx, in.IdemKey)
			if prev != nil {
				sim = *prev
			}
			return e
		}); rerr == nil && sim.ID != uuid.Nil {
			return sim, true, nil
		}
	}
	return sim, replayed, err
}

// SimulationView là việc mô phỏng kèm mô tả ảnh chụp và cờ chính thức.
type SimulationView struct {
	Simulation domain.Simulation
	Snapshot   *domain.SnapshotMeta
	HasResult  bool
}

// OfficialEligible: FN-01 BR-04 — chỉ khi mọi tham số của ảnh chụp đã phê duyệt.
func (v SimulationView) OfficialEligible() bool {
	return v.Snapshot != nil && v.Snapshot.AllParamsApproved && v.Simulation.Status == domain.StatusCompleted
}

func (s *Service) view(ctx context.Context, tx pgx.Tx, id uuid.UUID) (SimulationView, error) {
	sim, err := store.SimulationByID(ctx, tx, id, false)
	if err != nil {
		return SimulationView{}, err
	}
	if sim == nil {
		return SimulationView{}, ErrSimulationNotFound
	}
	v := SimulationView{Simulation: *sim}
	if v.Snapshot, err = store.SnapshotMetaBySimulation(ctx, tx, id); err != nil {
		return v, err
	}
	r, err := store.ResultBySimulation(ctx, tx, id)
	v.HasResult = r != nil
	return v, err
}

// GetSimulation đọc trạng thái việc mô phỏng (cũng là /v1/jobs/{id}).
func (s *Service) GetSimulation(ctx context.Context, id uuid.UUID) (SimulationView, error) {
	p, err := require(ctx)
	if err != nil {
		return SimulationView{}, err
	}
	var v SimulationView
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		v, e = s.view(ctx, tx, id)
		return e
	})
	return v, err
}

// ResultView là kết quả đã lưu (bất biến) kèm trạng thái chính thức.
type ResultView struct {
	View        SimulationView
	Result      store.ResultRow
	Sensitivity json.RawMessage
}

// GetResult đọc kết quả; chưa có → simulation.result_not_ready.
func (s *Service) GetResult(ctx context.Context, id uuid.UUID) (ResultView, error) {
	p, err := require(ctx)
	if err != nil {
		return ResultView{}, err
	}
	var out ResultView
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		v, e := s.view(ctx, tx, id)
		if e != nil {
			return e
		}
		r, e := store.ResultBySimulation(ctx, tx, id)
		if e != nil {
			return e
		}
		if r == nil {
			return ErrResultNotReady.WithDetail("trạng thái: " + v.Simulation.Status)
		}
		out = ResultView{View: v, Result: *r}
		out.Sensitivity, e = store.Sensitivity(ctx, tx, id)
		return e
	})
	return out, err
}

// MarkOfficial đánh dấu kết quả "Chính thức" — FN-01 BR-04: chỉ khi mọi tham số
// đã dùng trong ảnh chụp đến từ phiên bản ĐÃ PHÊ DUYỆT. Đánh dấu lặp idempotent.
func (s *Service) MarkOfficial(ctx context.Context, id uuid.UUID, idem string) (SimulationView, error) {
	p, err := require(ctx)
	if err != nil {
		return SimulationView{}, err
	}
	var v SimulationView
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		sim, e := store.SimulationByID(ctx, tx, id, true)
		if e != nil {
			return e
		}
		if sim == nil {
			return ErrSimulationNotFound
		}
		if v, e = s.view(ctx, tx, id); e != nil {
			return e
		}
		if sim.Official {
			return nil
		}
		if sim.Status != domain.StatusCompleted {
			return ErrResultNotReady.WithDetail("trạng thái: " + sim.Status)
		}
		if !v.OfficialEligible() {
			var snap []byte
			var sha string
			if snap, sha, e = store.SnapshotPayload(ctx, tx, id); e != nil {
				return e
			}
			pr := ErrParamsUnapproved
			if s2, _, de := domain.DecodeSnapshot(snap, sha); de == nil {
				pr = pr.With("unapproved_parameters", s2.Params.Unapproved)
			}
			return pr
		}
		now := s.now()
		ch := store.Change{ID: ids.New(), TenantID: p.TenantID, ScenarioID: sim.ScenarioID, Kind: domain.ChangeMarkedOfficial, SimulationID: &id,
			EventTime: now, ActorRef: p.ActorRef, IdemKey: "official:" + id.String(), Payload: map[string]any{"simulation_id": id}}
		if e := store.AppendChange(ctx, tx, &ch); e != nil {
			return e
		}
		if e := store.MarkOfficial(ctx, tx, id, p.ActorRef, now); e != nil {
			return e
		}
		v, e = s.view(ctx, tx, id)
		return e
	})
	return v, err
}

// Compare dựng bảng so sánh FN-14 từ các kết quả đã lưu (đồng bộ, < 2 giây).
func (s *Service) Compare(ctx context.Context, simIDs []uuid.UUID, includeBaseline bool) (domain.Comparison, error) {
	p, err := require(ctx)
	if err != nil {
		return domain.Comparison{}, err
	}
	if len(simIDs) < 1 || len(simIDs) > 20 {
		return domain.Comparison{}, problem.Validation.WithField("simulation_ids", "1–20 mô phỏng")
	}
	var sums map[uuid.UUID]domain.ResultSummary
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		sums, e = store.Summaries(ctx, tx, simIDs)
		return e
	}); err != nil {
		return domain.Comparison{}, err
	}
	var results []domain.ResultSummary
	var missing []string
	seen := map[uuid.UUID]bool{}
	for _, id := range simIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if r, ok := sums[id]; ok {
			results = append(results, r)
		} else {
			missing = append(missing, id.String())
		}
	}
	return domain.Compare(results, missing, len(seen), includeBaseline), nil
}
