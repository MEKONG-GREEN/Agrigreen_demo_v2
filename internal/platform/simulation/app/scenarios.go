package app

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func validLatLng(lat, lng float64) bool {
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180 && (lat != 0 || lng != 0)
}

func validName(n string) bool {
	l := utf8.RuneCountInString(strings.TrimSpace(n))
	return l >= 2 && l <= 200
}

// ---------------------------------------------------------------------------
// Hub ứng viên (FN-02/03)
// ---------------------------------------------------------------------------

// HubInput là yêu cầu tạo Hub ứng viên.
type HubInput struct {
	Name     string
	Lat, Lng float64
	UnitCode string
	IdemKey  string
}

// CreateHub tạo Hub ứng viên (idempotent theo Idempotency-Key).
func (s *Service) CreateHub(ctx context.Context, in HubInput) (domain.CandidateHub, bool, error) {
	p, err := require(ctx)
	if err != nil {
		return domain.CandidateHub{}, false, err
	}
	if in.IdemKey == "" {
		return domain.CandidateHub{}, false, problem.IdempotencyRequired
	}
	if !validName(in.Name) {
		return domain.CandidateHub{}, false, problem.Validation.WithField("name", "2–200 ký tự")
	}
	if !validLatLng(in.Lat, in.Lng) {
		return domain.CandidateHub{}, false, problem.Validation.WithField("location", "toạ độ WGS84 không hợp lệ")
	}
	now := s.now()
	h := domain.CandidateHub{ID: ids.New(), TenantID: p.TenantID, Name: strings.TrimSpace(in.Name), Location: port.LatLng{Lat: in.Lat, Lng: in.Lng},
		Status: "active", CreatedBy: p.ActorRef, CreatedAt: now, UpdatedAt: now}
	if u := strings.TrimSpace(in.UnitCode); u != "" {
		h.UnitCode = &u
	}
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		prev, err := store.HubByIdemKey(ctx, tx, in.IdemKey)
		if err != nil || prev != nil {
			if prev != nil {
				h, replayed = *prev, true
			}
			return err
		}
		return store.InsertHub(ctx, tx, &h, in.IdemKey)
	})
	return h, replayed, err
}

// HubPage là một trang Hub ứng viên.
type HubPage struct {
	Items []domain.CandidateHub
	Next  string
}

// ListHubs liệt kê Hub ứng viên theo mã (cursor = mã cuối trang).
func (s *Service) ListHubs(ctx context.Context, after string, limit int) (HubPage, error) {
	p, err := require(ctx)
	if err != nil {
		return HubPage{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var hubs []domain.CandidateHub
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		hubs, e = store.ListHubs(ctx, tx, after, limit+1)
		return e
	}); err != nil {
		return HubPage{}, err
	}
	pg := HubPage{Items: hubs}
	if len(hubs) > limit {
		pg.Items = hubs[:limit]
		pg.Next = hubs[limit-1].Code
	}
	return pg, nil
}

// ScenarioPage là một trang kịch bản.
type ScenarioPage struct {
	Items []domain.Scenario
	Next  string
}

// ListScenarios liệt kê kịch bản của tenant theo mã (cursor = mã cuối trang) — màn hình mô phỏng web.
func (s *Service) ListScenarios(ctx context.Context, after string, limit int) (ScenarioPage, error) {
	p, err := require(ctx)
	if err != nil {
		return ScenarioPage{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var items []domain.Scenario
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListScenarios(ctx, tx, after, limit+1)
		return e
	}); err != nil {
		return ScenarioPage{}, err
	}
	pg := ScenarioPage{Items: items}
	if len(items) > limit {
		pg.Items = items[:limit]
		pg.Next = items[limit-1].Code
	}
	return pg, nil
}

// RetireHub ngừng dùng Hub kèm lý do (không xóa — RB-1, ADR-0011).
func (s *Service) RetireHub(ctx context.Context, id uuid.UUID, reason string) (domain.CandidateHub, error) {
	p, err := require(ctx)
	if err != nil {
		return domain.CandidateHub{}, err
	}
	reason = strings.TrimSpace(reason)
	if l := utf8.RuneCountInString(reason); l < 10 || l > 500 {
		return domain.CandidateHub{}, problem.Validation.WithField("reason", "lý do 10–500 ký tự")
	}
	var h *domain.CandidateHub
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if h, e = store.HubByID(ctx, tx, id, true); e != nil || h == nil {
			return e
		}
		if h.Status == "retired" {
			return nil
		}
		if e = store.RetireHub(ctx, tx, id, reason, s.now()); e != nil {
			return e
		}
		h, e = store.HubByID(ctx, tx, id, false)
		return e
	})
	if err == nil && h == nil {
		return domain.CandidateHub{}, ErrHubNotFound
	}
	if err != nil {
		return domain.CandidateHub{}, err
	}
	return *h, nil
}

// ---------------------------------------------------------------------------
// Kịch bản
// ---------------------------------------------------------------------------

// ScenarioInput là yêu cầu tạo kịch bản.
type ScenarioInput struct {
	Name                     string
	PlantRef                 *uuid.UUID
	PlantLat, PlantLng       float64
	PlantAnnualDemandTons    *decimal.Decimal
	LifecycleYears           int // 0 → tham số #47
	Discounted               bool
	BaselineMode             string
	BaselineManualCostPerTon *decimal.Decimal
	BaselineScenarioID       *uuid.UUID
	ReferenceYear            int
	IdemKey                  string
}

// CreateScenario tạo kịch bản: dòng sổ "created" + bảng chiếu, cùng giao dịch.
func (s *Service) CreateScenario(ctx context.Context, in ScenarioInput) (domain.Scenario, bool, error) {
	p, err := require(ctx)
	if err != nil {
		return domain.Scenario{}, false, err
	}
	if in.IdemKey == "" {
		return domain.Scenario{}, false, problem.IdempotencyRequired
	}
	switch {
	case !validName(in.Name):
		return domain.Scenario{}, false, problem.Validation.WithField("name", "2–200 ký tự")
	case !validLatLng(in.PlantLat, in.PlantLng):
		return domain.Scenario{}, false, problem.Validation.WithField("plant.location", "toạ độ nhà máy WGS84 không hợp lệ")
	case in.PlantAnnualDemandTons != nil && !in.PlantAnnualDemandTons.IsPositive():
		return domain.Scenario{}, false, problem.Validation.WithField("plant.annual_demand_tons", "phải lớn hơn 0")
	case in.ReferenceYear < 2000 || in.ReferenceYear > 2100:
		return domain.Scenario{}, false, problem.Validation.WithField("reference_year", "năm vụ tham chiếu 2000–2100")
	case in.LifecycleYears < 0 || in.LifecycleYears > 50:
		return domain.Scenario{}, false, problem.Validation.WithField("lifecycle_years", "1–50 năm")
	}
	if in.BaselineMode == "" {
		in.BaselineMode = domain.BaselineNoHub
	}
	switch in.BaselineMode {
	case domain.BaselineNoHub:
		in.BaselineManualCostPerTon, in.BaselineScenarioID = nil, nil
	case domain.BaselineManual:
		if in.BaselineManualCostPerTon == nil || !in.BaselineManualCostPerTon.IsPositive() {
			return domain.Scenario{}, false, problem.Validation.WithField("baseline_manual_cost_per_ton", "bắt buộc, > 0 khi baseline_mode = manual")
		}
		in.BaselineScenarioID = nil
	case domain.BaselineScenario:
		if in.BaselineScenarioID == nil {
			return domain.Scenario{}, false, problem.Validation.WithField("baseline_scenario_id", "bắt buộc khi baseline_mode = scenario")
		}
		in.BaselineManualCostPerTon = nil
	default:
		return domain.Scenario{}, false, problem.Validation.WithField("baseline_mode", "no_hub | manual | scenario")
	}
	now := s.now()
	if in.LifecycleYears == 0 {
		params, err := s.resolveParams(ctx, p.TenantID, now)
		if err != nil {
			return domain.Scenario{}, false, err
		}
		in.LifecycleYears = int(paramOr(params, "lifecycle_years", 10).IntPart())
	}
	sc := domain.Scenario{ID: ids.New(), TenantID: p.TenantID, Name: strings.TrimSpace(in.Name), PlantRef: in.PlantRef,
		PlantLocation: port.LatLng{Lat: in.PlantLat, Lng: in.PlantLng}, PlantAnnualDemandTons: in.PlantAnnualDemandTons,
		LifecycleYears: in.LifecycleYears, Discounted: in.Discounted, BaselineMode: in.BaselineMode,
		BaselineManualCostPerTon: in.BaselineManualCostPerTon, BaselineScenarioID: in.BaselineScenarioID, ReferenceYear: in.ReferenceYear,
		CreatedBy: p.ActorRef, CreatedAt: now, UpdatedAt: now}
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ChangeByIdemKey(ctx, tx, in.IdemKey); err != nil || prev != nil {
			if prev == nil {
				return err
			}
			got, err := s.loadScenario(ctx, tx, prevScenarioID(prev))
			if err == nil {
				sc, replayed = got, true
			}
			return err
		}
		if sc.BaselineScenarioID != nil {
			other, err := store.ScenarioByID(ctx, tx, *sc.BaselineScenarioID, false)
			if err != nil {
				return err
			}
			if other == nil {
				return ErrBaselineScenario.WithField("baseline_scenario_id", "không có kịch bản này")
			}
		}
		code, err := store.NextScenarioCode(ctx, tx, p.TenantID)
		if err != nil {
			return err
		}
		sc.Code = code
		ch := store.Change{ID: ids.New(), TenantID: p.TenantID, ScenarioID: sc.ID, Kind: domain.ChangeCreated, EventTime: now,
			ActorRef: p.ActorRef, IdemKey: in.IdemKey, Payload: scenarioPayload(sc)}
		if err := store.AppendChange(ctx, tx, &ch); err != nil {
			return err
		}
		sc.Version = ch.Seq
		return store.InsertScenario(ctx, tx, &sc)
	})
	return sc, replayed, err
}

func prevScenarioID(c *store.ChangeRow) uuid.UUID {
	var v struct {
		ScenarioID uuid.UUID `json:"scenario_id"`
	}
	_ = jsonUnmarshal(c.Payload, &v)
	return v.ScenarioID
}

func scenarioPayload(sc domain.Scenario) map[string]any {
	return map[string]any{
		"scenario_id": sc.ID, "code": sc.Code, "name": sc.Name, "plant_ref": sc.PlantRef,
		"plant_location":           map[string]float64{"lat": sc.PlantLocation.Lat, "lng": sc.PlantLocation.Lng},
		"plant_annual_demand_tons": sc.PlantAnnualDemandTons, "lifecycle_years": sc.LifecycleYears, "discounted": sc.Discounted,
		"baseline_mode": sc.BaselineMode, "baseline_manual_cost_per_ton": sc.BaselineManualCostPerTon,
		"baseline_scenario_id": sc.BaselineScenarioID, "reference_year": sc.ReferenceYear,
	}
}

func (s *Service) loadScenario(ctx context.Context, tx pgx.Tx, id uuid.UUID) (domain.Scenario, error) {
	sc, err := store.ScenarioByID(ctx, tx, id, false)
	if err != nil {
		return domain.Scenario{}, err
	}
	if sc == nil {
		return domain.Scenario{}, ErrScenarioNotFound
	}
	return *sc, nil
}

// GetScenario đọc kịch bản kèm Hub đang gán.
func (s *Service) GetScenario(ctx context.Context, id uuid.UUID) (domain.Scenario, error) {
	p, err := require(ctx)
	if err != nil {
		return domain.Scenario{}, err
	}
	var sc domain.Scenario
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		sc, e = s.loadScenario(ctx, tx, id)
		return e
	})
	return sc, err
}

// History đọc sổ thay đổi của kịch bản (kiểm toán, bản ghi bù).
func (s *Service) History(ctx context.Context, id uuid.UUID) ([]store.ChangeRow, error) {
	p, err := require(ctx)
	if err != nil {
		return nil, err
	}
	var out []store.ChangeRow
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if _, e := s.loadScenario(ctx, tx, id); e != nil {
			return e
		}
		var e error
		out, e = store.Changes(ctx, tx, id)
		return e
	})
	return out, err
}

// AttachHub gán Hub vào kịch bản hoặc sửa cấu hình (FN-02 BR-03). Trường không gửi
// giữ giá trị đang lưu, rồi mới tới mặc định #13 (bán kính), #14 (công suất),
// #52 (sà lan nhỏ). Ghi sổ hub_attached/hub_updated + bảng chiếu cùng giao dịch.
func (s *Service) AttachHub(ctx context.Context, scenarioID, hubID uuid.UUID, cfg domain.HubConfig, idem string) (domain.Scenario, error) {
	p, err := require(ctx)
	if err != nil {
		return domain.Scenario{}, err
	}
	if idem == "" {
		return domain.Scenario{}, problem.IdempotencyRequired
	}
	now := s.now()
	params, err := s.resolveParams(ctx, p.TenantID, now)
	if err != nil {
		return domain.Scenario{}, err
	}
	var out domain.Scenario
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, e := store.ChangeByIdemKey(ctx, tx, idem); e != nil || prev != nil {
			if e != nil {
				return e
			}
			var le error
			out, le = s.loadScenario(ctx, tx, scenarioID)
			return le
		}
		sc, e := store.ScenarioByID(ctx, tx, scenarioID, true)
		if e != nil {
			return e
		}
		if sc == nil {
			return ErrScenarioNotFound
		}
		hub, e := store.HubByID(ctx, tx, hubID, false)
		if e != nil {
			return e
		}
		if hub == nil {
			return ErrHubNotFound
		}
		cur, e := store.ScenarioHub(ctx, tx, scenarioID, hubID)
		if e != nil {
			return e
		}
		kind := domain.ChangeHubUpdated
		base := domain.ScenarioHub{}
		if cur != nil && cur.Active {
			base = *cur
		} else {
			if hub.Status != "active" {
				return ErrHubRetired
			}
			kind = domain.ChangeHubAttached
			barge := paramOr(params, "barge_small_payload_tons", 1000)
			base = domain.ScenarioHub{ID: ids.New(), ScenarioID: scenarioID, HubID: hubID, ServiceRadiusKm: paramOr(params, "default_service_radius_km", 30),
				DesignCapacityTons: paramOr(params, "default_design_capacity_tons", 60000), BargePayloadTons: &barge,
				ModeFieldHub: "auto", ModeHubPlant: "auto", LandMode: "mua"}
			if cur != nil { // gán lại sau khi gỡ: giữ id cấu hình, thứ tự mới
				base.ID = cur.ID
			}
			if base.SortOrder, e = store.NextSortOrder(ctx, tx, scenarioID); e != nil {
				return e
			}
		}
		next, verr := cfg.Apply(base)
		if verr != nil {
			return problem.Validation.WithDetail(verr.Error())
		}
		next.Active = true
		ch := store.Change{ID: ids.New(), TenantID: p.TenantID, ScenarioID: scenarioID, Kind: kind, HubID: &hubID, EventTime: now,
			ActorRef: p.ActorRef, IdemKey: idem, Payload: hubPayload(next, hub.Code)}
		if e := store.AppendChange(ctx, tx, &ch); e != nil {
			return e
		}
		next.ChangeID = ch.ID
		if e := store.UpsertScenarioHub(ctx, tx, p.TenantID, next, now); e != nil {
			return e
		}
		if e := store.TouchScenario(ctx, tx, scenarioID, ch.Seq, now); e != nil {
			return e
		}
		out, e = s.loadScenario(ctx, tx, scenarioID)
		return e
	})
	return out, err
}

func hubPayload(h domain.ScenarioHub, code string) map[string]any {
	return map[string]any{
		"scenario_hub_id": h.ID, "hub_id": h.HubID, "hub_code": code, "service_radius_km": h.ServiceRadiusKm,
		"design_capacity_tons": h.DesignCapacityTons, "barge_payload_tons": h.BargePayloadTons, "mode_field_hub": h.ModeFieldHub,
		"mode_hub_plant": h.ModeHubPlant, "land_mode": h.LandMode, "sort_order": h.SortOrder,
	}
}

// DetachHub gỡ Hub khỏi kịch bản bằng BẢN GHI BÙ (supersedes_id trỏ bản ghi đã
// tạo cấu hình hiện hành, kèm lý do) — không xóa (RB-1).
func (s *Service) DetachHub(ctx context.Context, scenarioID, hubID uuid.UUID, reason, idem string) (domain.Scenario, error) {
	p, err := require(ctx)
	if err != nil {
		return domain.Scenario{}, err
	}
	if idem == "" {
		return domain.Scenario{}, problem.IdempotencyRequired
	}
	reason = strings.TrimSpace(reason)
	if l := utf8.RuneCountInString(reason); l < 10 || l > 500 {
		return domain.Scenario{}, problem.Validation.WithField("reason", "lý do 10–500 ký tự")
	}
	now := s.now()
	var out domain.Scenario
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, e := store.ChangeByIdemKey(ctx, tx, idem); e != nil || prev != nil {
			if e != nil {
				return e
			}
			var le error
			out, le = s.loadScenario(ctx, tx, scenarioID)
			return le
		}
		sc, e := store.ScenarioByID(ctx, tx, scenarioID, true)
		if e != nil {
			return e
		}
		if sc == nil {
			return ErrScenarioNotFound
		}
		cur, e := store.ScenarioHub(ctx, tx, scenarioID, hubID)
		if e != nil {
			return e
		}
		if cur == nil || !cur.Active {
			return ErrHubNotAttached
		}
		sup := cur.ChangeID
		ch := store.Change{ID: ids.New(), TenantID: p.TenantID, ScenarioID: scenarioID, Kind: domain.ChangeHubDetached, HubID: &hubID,
			EventTime: now, ActorRef: p.ActorRef, IdemKey: idem, SupersedesID: &sup, CorrectionReason: &reason,
			Payload: map[string]any{"scenario_hub_id": cur.ID, "hub_id": hubID}}
		if e := store.AppendChange(ctx, tx, &ch); e != nil {
			return e
		}
		cur.Active, cur.ChangeID = false, ch.ID
		if e := store.UpsertScenarioHub(ctx, tx, p.TenantID, *cur, now); e != nil {
			return e
		}
		if e := store.TouchScenario(ctx, tx, scenarioID, ch.Seq, now); e != nil {
			return e
		}
		out, e = s.loadScenario(ctx, tx, scenarioID)
		return e
	})
	return out, err
}
