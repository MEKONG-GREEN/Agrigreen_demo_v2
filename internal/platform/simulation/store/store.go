// Package store: SQL của pf/simulation, chỉ dùng pool vai simulation_app (R-05).
// Mọi hàm chạy trong giao dịch tenant (RLS) do app mở.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/domain"
)

func lock(ctx context.Context, tx pgx.Tx, parts ...string) error {
	key := "pf_simulation"
	for _, p := range parts {
		key += ":" + p
	}
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))", key)
	return err
}

func nextCode(ctx context.Context, tx pgx.Tx, tid uuid.UUID, table, prefix string) (string, error) {
	if err := lock(ctx, tx, tid.String(), table); err != nil {
		return "", err
	}
	var n int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM pf_simulation."+table).Scan(&n); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%04d", prefix, n+1), nil
}

// ---------------------------------------------------------------------------
// Hub ứng viên
// ---------------------------------------------------------------------------

const hubCols = `id, tenant_id, code, name, ST_Y(location), ST_X(location), unit_code, status, retired_reason, created_by, created_at, updated_at`

func scanHub(row pgx.Row) (domain.CandidateHub, error) {
	var h domain.CandidateHub
	err := row.Scan(&h.ID, &h.TenantID, &h.Code, &h.Name, &h.Location.Lat, &h.Location.Lng, &h.UnitCode, &h.Status, &h.RetiredReason,
		&h.CreatedBy, &h.CreatedAt, &h.UpdatedAt)
	return h, err
}

func oneHub(ctx context.Context, tx pgx.Tx, where string, args ...any) (*domain.CandidateHub, error) {
	h, err := scanHub(tx.QueryRow(ctx, "SELECT "+hubCols+" FROM pf_simulation.candidate_hub WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// HubByIdemKey tìm Hub đã tạo với idem_key.
func HubByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.CandidateHub, error) {
	return oneHub(ctx, tx, "idem_key = $1::text", key)
}

// HubByID đọc một Hub.
func HubByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.CandidateHub, error) {
	q := "id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneHub(ctx, tx, q, id)
}

// InsertHub ghi Hub mới; mã HUB-NNNN sinh tuần tự theo tenant.
func InsertHub(ctx context.Context, tx pgx.Tx, h *domain.CandidateHub, idem string) error {
	code, err := nextCode(ctx, tx, h.TenantID, "candidate_hub", "HUB")
	if err != nil {
		return err
	}
	h.Code = code
	_, err = tx.Exec(ctx, `INSERT INTO pf_simulation.candidate_hub
	    (tenant_id, id, code, name, location, unit_code, status, created_by, created_at, updated_at, idem_key)
	  VALUES ($1, $2, $3, $4, ST_SetSRID(ST_MakePoint($5, $6), 4326), $7, 'active', $8, $9, $9, $10)`,
		h.TenantID, h.ID, h.Code, h.Name, h.Location.Lng, h.Location.Lat, h.UnitCode, h.CreatedBy, h.CreatedAt.UTC(), idem)
	return err
}

// RetireHub ngừng dùng Hub kèm lý do (không xóa, RB-1).
func RetireHub(ctx context.Context, tx pgx.Tx, id uuid.UUID, reason string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_simulation.candidate_hub SET status = 'retired', retired_reason = $2, updated_at = $3 WHERE id = $1`,
		id, reason, at.UTC())
	return err
}

// ListHubs liệt kê Hub theo mã tăng dần; after = mã cuối trang trước (cursor).
func ListHubs(ctx context.Context, tx pgx.Tx, after string, limit int) ([]domain.CandidateHub, error) {
	rows, err := tx.Query(ctx, "SELECT "+hubCols+" FROM pf_simulation.candidate_hub WHERE code > $1 ORDER BY code LIMIT $2", after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.CandidateHub, error) { return scanHub(r) })
}

// ---------------------------------------------------------------------------
// Sổ kịch bản
// ---------------------------------------------------------------------------

// Change là một dòng sổ scenario_change.
type Change struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	ScenarioID       uuid.UUID
	Kind             string
	HubID            *uuid.UUID
	SimulationID     *uuid.UUID
	Payload          any
	EventTime        time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	IdemKey          string
	Seq              int64
	RecordedAt       time.Time
}

// StreamKey của một kịch bản.
func StreamKey(scenarioID uuid.UUID) string { return "scenario:" + scenarioID.String() }

// AppendChange ghi sổ; DB tính seq, prev_hash, row_hash (shared.fn_chain).
func AppendChange(ctx context.Context, tx pgx.Tx, c *Change) error {
	b, err := json.Marshal(c.Payload)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_simulation.scenario_change
	    (id, tenant_id, stream_key, scenario_id, kind, hub_id, simulation_id, payload, event_time, supersedes_id, correction_reason,
	     actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12, $13, $14) RETURNING seq, recorded_at`,
		c.ID, c.TenantID, StreamKey(c.ScenarioID), c.ScenarioID, c.Kind, c.HubID, c.SimulationID, string(b), c.EventTime.UTC(),
		c.SupersedesID, c.CorrectionReason, c.ActorRef, c.IdemKey, domain.RuleVersion).Scan(&c.Seq, &c.RecordedAt)
}

// ChangeRow là dòng sổ đọc ra.
type ChangeRow struct {
	ID               uuid.UUID
	Seq              int64
	Kind             string
	HubID            *uuid.UUID
	SimulationID     *uuid.UUID
	Payload          json.RawMessage
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
}

// Changes đọc sổ của một kịch bản theo seq.
func Changes(ctx context.Context, tx pgx.Tx, scenarioID uuid.UUID) ([]ChangeRow, error) {
	rows, err := tx.Query(ctx, `SELECT id, seq, kind, hub_id, simulation_id, payload::text, event_time, recorded_at, supersedes_id,
	    correction_reason, actor_ref FROM pf_simulation.scenario_change WHERE stream_key = $1 ORDER BY seq`, StreamKey(scenarioID))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ChangeRow, error) {
		var c ChangeRow
		var p string
		err := r.Scan(&c.ID, &c.Seq, &c.Kind, &c.HubID, &c.SimulationID, &p, &c.EventTime, &c.RecordedAt, &c.SupersedesID, &c.CorrectionReason, &c.ActorRef)
		c.Payload = json.RawMessage(p)
		return c, err
	})
}

// ChangeByIdemKey tìm dòng sổ đã ghi với idem_key.
func ChangeByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*ChangeRow, error) {
	var c ChangeRow
	var p string
	err := tx.QueryRow(ctx, `SELECT id, seq, kind, hub_id, simulation_id, payload::text, event_time, recorded_at, supersedes_id,
	    correction_reason, actor_ref FROM pf_simulation.scenario_change WHERE idem_key = $1::text ORDER BY recorded_at LIMIT 1`, key).
		Scan(&c.ID, &c.Seq, &c.Kind, &c.HubID, &c.SimulationID, &p, &c.EventTime, &c.RecordedAt, &c.SupersedesID, &c.CorrectionReason, &c.ActorRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	c.Payload = json.RawMessage(p)
	return &c, err
}

// ---------------------------------------------------------------------------
// Kịch bản (bảng chiếu)
// ---------------------------------------------------------------------------

const scnCols = `id, tenant_id, code, name, plant_ref, ST_Y(plant_location), ST_X(plant_location), plant_annual_demand_tons,
  lifecycle_years, discounted, baseline_mode, baseline_manual_cost_per_ton, baseline_scenario_id, reference_year, version,
  created_by, created_at, updated_at`

func scanScenario(row pgx.Row) (domain.Scenario, error) {
	var s domain.Scenario
	err := row.Scan(&s.ID, &s.TenantID, &s.Code, &s.Name, &s.PlantRef, &s.PlantLocation.Lat, &s.PlantLocation.Lng, &s.PlantAnnualDemandTons,
		&s.LifecycleYears, &s.Discounted, &s.BaselineMode, &s.BaselineManualCostPerTon, &s.BaselineScenarioID, &s.ReferenceYear, &s.Version,
		&s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

// NextScenarioCode sinh mã KB-NNNN theo tenant.
func NextScenarioCode(ctx context.Context, tx pgx.Tx, tid uuid.UUID) (string, error) {
	return nextCode(ctx, tx, tid, "scenario", "KB")
}

// InsertScenario ghi bảng chiếu kịch bản.
func InsertScenario(ctx context.Context, tx pgx.Tx, s *domain.Scenario) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_simulation.scenario
	    (tenant_id, id, code, name, plant_ref, plant_location, plant_annual_demand_tons, lifecycle_years, discounted, baseline_mode,
	     baseline_manual_cost_per_ton, baseline_scenario_id, reference_year, version, created_by, created_at, updated_at)
	  VALUES ($1, $2, $3, $4, $5, ST_SetSRID(ST_MakePoint($6, $7), 4326), $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $17)`,
		s.TenantID, s.ID, s.Code, s.Name, s.PlantRef, s.PlantLocation.Lng, s.PlantLocation.Lat, s.PlantAnnualDemandTons, s.LifecycleYears,
		s.Discounted, s.BaselineMode, s.BaselineManualCostPerTon, s.BaselineScenarioID, s.ReferenceYear, s.Version, s.CreatedBy, s.CreatedAt.UTC())
	return err
}

// TouchScenario cập nhật version (seq cuối của sổ) và updated_at.
func TouchScenario(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_simulation.scenario SET version = $2, updated_at = $3 WHERE id = $1`, id, version, at.UTC())
	return err
}

// ScenarioByID đọc kịch bản kèm Hub đang gán (forUpdate khóa dòng kịch bản).
func ScenarioByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.Scenario, error) {
	q := "SELECT " + scnCols + " FROM pf_simulation.scenario WHERE id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	s, err := scanScenario(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	hubs, err := ScenarioHubs(ctx, tx, id, false)
	if err != nil {
		return nil, err
	}
	s.Hubs = hubs
	return &s, nil
}

const shCols = `sh.id, sh.scenario_id, sh.hub_id, h.code, ST_Y(h.location), ST_X(h.location), sh.service_radius_km, sh.design_capacity_tons,
  sh.barge_payload_tons, sh.mode_field_hub, sh.mode_hub_plant, sh.land_mode, sh.sort_order, sh.active, sh.change_id`

func scanSH(row pgx.Row) (domain.ScenarioHub, error) {
	var h domain.ScenarioHub
	err := row.Scan(&h.ID, &h.ScenarioID, &h.HubID, &h.HubCode, &h.HubLocation.Lat, &h.HubLocation.Lng, &h.ServiceRadiusKm, &h.DesignCapacityTons,
		&h.BargePayloadTons, &h.ModeFieldHub, &h.ModeHubPlant, &h.LandMode, &h.SortOrder, &h.Active, &h.ChangeID)
	return h, err
}

// ScenarioHubs liệt kê cấu hình Hub của kịch bản (theo sort_order, mã Hub); all = cả Hub đã gỡ.
func ScenarioHubs(ctx context.Context, tx pgx.Tx, scenarioID uuid.UUID, all bool) ([]domain.ScenarioHub, error) {
	q := `SELECT ` + shCols + ` FROM pf_simulation.scenario_hub sh
	  JOIN pf_simulation.candidate_hub h ON h.tenant_id = sh.tenant_id AND h.id = sh.hub_id
	  WHERE sh.scenario_id = $1`
	if !all {
		q += " AND sh.active"
	}
	rows, err := tx.Query(ctx, q+" ORDER BY sh.sort_order, h.code", scenarioID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.ScenarioHub, error) { return scanSH(r) })
}

// ScenarioHub đọc một cấu hình Hub × kịch bản (kể cả đã gỡ).
func ScenarioHub(ctx context.Context, tx pgx.Tx, scenarioID, hubID uuid.UUID) (*domain.ScenarioHub, error) {
	h, err := scanSH(tx.QueryRow(ctx, `SELECT `+shCols+` FROM pf_simulation.scenario_hub sh
	  JOIN pf_simulation.candidate_hub h ON h.tenant_id = sh.tenant_id AND h.id = sh.hub_id
	  WHERE sh.scenario_id = $1 AND sh.hub_id = $2`, scenarioID, hubID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// NextSortOrder trả thứ tự kế tiếp của Hub trong kịch bản.
func NextSortOrder(ctx context.Context, tx pgx.Tx, scenarioID uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT coalesce(max(sort_order), 0) + 1 FROM pf_simulation.scenario_hub WHERE scenario_id = $1`, scenarioID).Scan(&n)
	return n, err
}

// UpsertScenarioHub ghi trạng thái cấu hình Hub × kịch bản (bảng chiếu).
func UpsertScenarioHub(ctx context.Context, tx pgx.Tx, tid uuid.UUID, h domain.ScenarioHub, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_simulation.scenario_hub
	    (tenant_id, scenario_id, hub_id, id, service_radius_km, design_capacity_tons, barge_payload_tons, mode_field_hub, mode_hub_plant,
	     land_mode, sort_order, active, change_id, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	  ON CONFLICT (tenant_id, scenario_id, hub_id) DO UPDATE SET service_radius_km = EXCLUDED.service_radius_km,
	    design_capacity_tons = EXCLUDED.design_capacity_tons, barge_payload_tons = EXCLUDED.barge_payload_tons,
	    mode_field_hub = EXCLUDED.mode_field_hub, mode_hub_plant = EXCLUDED.mode_hub_plant, land_mode = EXCLUDED.land_mode,
	    sort_order = EXCLUDED.sort_order, active = EXCLUDED.active, change_id = EXCLUDED.change_id, updated_at = EXCLUDED.updated_at`,
		tid, h.ScenarioID, h.HubID, h.ID, h.ServiceRadiusKm, h.DesignCapacityTons, h.BargePayloadTons, h.ModeFieldHub, h.ModeHubPlant,
		h.LandMode, h.SortOrder, h.Active, h.ChangeID, at.UTC())
	return err
}

// ---------------------------------------------------------------------------
// Việc mô phỏng
// ---------------------------------------------------------------------------

const simCols = `id, tenant_id, scenario_id, scenario_version, options::text, requested_by, requested_at, idem_key, status, snapshot_id,
  error_code, error_detail, completed_at, official, official_by, official_at`

func scanSim(row pgx.Row) (domain.Simulation, error) {
	var s domain.Simulation
	var opts string
	err := row.Scan(&s.ID, &s.TenantID, &s.ScenarioID, &s.ScenarioVersion, &opts, &s.RequestedBy, &s.RequestedAt, &s.IdemKey, &s.Status,
		&s.SnapshotID, &s.ErrorCode, &s.ErrorDetail, &s.CompletedAt, &s.Official, &s.OfficialBy, &s.OfficialAt)
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal([]byte(opts), &s.Options)
}

func oneSim(ctx context.Context, tx pgx.Tx, where string, args ...any) (*domain.Simulation, error) {
	s, err := scanSim(tx.QueryRow(ctx, "SELECT "+simCols+" FROM pf_simulation.simulation WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SimulationByIdemKey tìm việc đã tạo với idem_key.
func SimulationByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Simulation, error) {
	return oneSim(ctx, tx, "idem_key = $1::text", key)
}

// SimulationByID đọc một việc; forUpdate khóa dòng.
func SimulationByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.Simulation, error) {
	q := "id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneSim(ctx, tx, q, id)
}

// InsertSimulation ghi việc mô phỏng ở trạng thái queued.
func InsertSimulation(ctx context.Context, tx pgx.Tx, s *domain.Simulation) error {
	opts, err := json.Marshal(s.Options)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_simulation.simulation
	    (tenant_id, id, scenario_id, scenario_version, options, requested_by, requested_at, idem_key, status)
	  VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, 'queued')`,
		s.TenantID, s.ID, s.ScenarioID, s.ScenarioVersion, string(opts), s.RequestedBy, s.RequestedAt.UTC(), s.IdemKey)
	return err
}

// SetRunning ghim ảnh chụp và chuyển sang running.
func SetRunning(ctx context.Context, tx pgx.Tx, id, snapshotID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE pf_simulation.simulation SET status = 'running', snapshot_id = $2 WHERE id = $1`, id, snapshotID)
	return err
}

// Finish chuyển việc sang completed/failed.
func Finish(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, code, detail *string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_simulation.simulation SET status = $2, error_code = $3, error_detail = $4, completed_at = $5 WHERE id = $1`,
		id, status, code, detail, at.UTC())
	return err
}

// MarkOfficial đánh dấu kết quả "Chính thức".
func MarkOfficial(ctx context.Context, tx pgx.Tx, id, by uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_simulation.simulation SET official = true, official_by = $2, official_at = $3 WHERE id = $1`, id, by, at.UTC())
	return err
}

// ---------------------------------------------------------------------------
// Ảnh chụp
// ---------------------------------------------------------------------------

// InsertSnapshot ghi ảnh chụp bất biến.
func InsertSnapshot(ctx context.Context, tx pgx.Tx, tid, scenarioID uuid.UUID, m domain.SnapshotMeta, payload []byte) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_simulation.input_snapshot
	    (tenant_id, id, simulation_id, scenario_id, payload, sha256, param_set_version, all_params_approved, network_version,
	     algorithm_version, cooperative_count, route_count, created_at)
	  VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11, $12, $13)`,
		tid, m.ID, m.SimulationID, scenarioID, string(payload), m.SHA256, m.ParamSetVersion, m.AllParamsApproved, m.NetworkVersion,
		m.AlgorithmVersion, m.CooperativeCount, m.RouteCount, m.CreatedAt.UTC())
	return err
}

// SnapshotMetaBySimulation đọc mô tả ảnh chụp của một việc.
func SnapshotMetaBySimulation(ctx context.Context, tx pgx.Tx, simID uuid.UUID) (*domain.SnapshotMeta, error) {
	var m domain.SnapshotMeta
	err := tx.QueryRow(ctx, `SELECT id, simulation_id, sha256, param_set_version, all_params_approved, network_version, algorithm_version,
	    cooperative_count, route_count, created_at FROM pf_simulation.input_snapshot WHERE simulation_id = $1`, simID).
		Scan(&m.ID, &m.SimulationID, &m.SHA256, &m.ParamSetVersion, &m.AllParamsApproved, &m.NetworkVersion, &m.AlgorithmVersion,
			&m.CooperativeCount, &m.RouteCount, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &m, err
}

// SnapshotPayload đọc payload (jsonb) và băm đã lưu.
func SnapshotPayload(ctx context.Context, tx pgx.Tx, simID uuid.UUID) ([]byte, string, error) {
	var p, sha string
	err := tx.QueryRow(ctx, `SELECT payload::text, sha256 FROM pf_simulation.input_snapshot WHERE simulation_id = $1`, simID).Scan(&p, &sha)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	return []byte(p), sha, err
}

// ---------------------------------------------------------------------------
// Kết quả
// ---------------------------------------------------------------------------

// ResultRow là kết quả đã lưu.
type ResultRow struct {
	SimulationID     uuid.UUID
	SnapshotID       uuid.UUID
	SnapshotSHA256   string
	Payload          json.RawMessage
	PayloadSHA256    string
	EngineVersion    string
	AlgorithmVersion string
	OfficialEligible bool
	ReceivedAt       time.Time
}

// InsertResult ghi kết quả bất biến và cột tóm tắt.
func InsertResult(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r ResultRow, s domain.ResultSummary) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_simulation.result
	    (tenant_id, simulation_id, snapshot_id, snapshot_sha256, payload, payload_sha256, engine_version, algorithm_version, official_eligible,
	     parameter_set_version, lifecycle_years, discounted, hub_count, delivered_tons, capex, opex_year1, collection_cost, transportation_cost,
	     warehouse_cost, handling_cost, total_logistics_cost, cost_per_ton, tco, tco_per_ton, lifecycle_cost_per_ton, roi_pct, payback_years,
	     payback_status, plant_annual_demand_tons, plant_demand_coverage_pct, shortfall_warnings, baseline_available, baseline_delivered_tons,
	     baseline_collection_cost, baseline_transportation_cost, baseline_total_cost, baseline_cost_per_ton, received_at)
	  VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26,
	          $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38)`,
		tid, r.SimulationID, r.SnapshotID, r.SnapshotSHA256, string(r.Payload), r.PayloadSHA256, r.EngineVersion, r.AlgorithmVersion,
		r.OfficialEligible, s.ParameterSetVersion, s.LifecycleYears, s.Discounted, s.HubCount, s.DeliveredTons, s.Capex, s.OpexYear1,
		s.CollectionCost, s.TransportationCost, s.WarehouseCost, s.HandlingCost, s.TotalLogisticsCost, s.CostPerTon, s.TCO, s.TCOPerTon,
		s.LifecycleCostPerTon, s.ROIPct, s.PaybackYears, s.PaybackStatus, s.PlantAnnualDemandTons, s.PlantDemandCoveragePct,
		s.ShortfallWarnings, s.BaselineAvailable, s.BaselineDeliveredTons, s.BaselineCollection, s.BaselineTransportation,
		s.BaselineTotalCost, s.BaselineCostPerTon, r.ReceivedAt.UTC())
	return err
}

// ResultBySimulation đọc kết quả đã lưu.
func ResultBySimulation(ctx context.Context, tx pgx.Tx, simID uuid.UUID) (*ResultRow, error) {
	var r ResultRow
	var p string
	err := tx.QueryRow(ctx, `SELECT simulation_id, snapshot_id, snapshot_sha256, payload::text, payload_sha256, engine_version,
	    algorithm_version, official_eligible, received_at FROM pf_simulation.result WHERE simulation_id = $1`, simID).
		Scan(&r.SimulationID, &r.SnapshotID, &r.SnapshotSHA256, &p, &r.PayloadSHA256, &r.EngineVersion, &r.AlgorithmVersion,
			&r.OfficialEligible, &r.ReceivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	r.Payload = json.RawMessage(p)
	return &r, err
}

// Summaries đọc cột tóm tắt của các kết quả (so sánh FN-14), theo thứ tự ids.
func Summaries(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]domain.ResultSummary, error) {
	strs := make([]string, len(ids)) // chuỗi: chạy được ở QueryExecModeExec (PgBouncer)
	for i, id := range ids {
		strs[i] = id.String()
	}
	rows, err := tx.Query(ctx, `SELECT r.simulation_id, s.scenario_id, sc.code, sc.name, s.official, r.parameter_set_version, r.lifecycle_years,
	    r.discounted, r.hub_count, r.delivered_tons, r.capex, r.opex_year1, r.collection_cost, r.transportation_cost, r.warehouse_cost,
	    r.handling_cost, r.total_logistics_cost, r.cost_per_ton, r.tco, r.tco_per_ton, r.lifecycle_cost_per_ton, r.roi_pct, r.payback_years,
	    r.payback_status, r.plant_annual_demand_tons, r.plant_demand_coverage_pct, r.shortfall_warnings, r.baseline_available,
	    r.baseline_delivered_tons, r.baseline_collection_cost, r.baseline_transportation_cost, r.baseline_total_cost, r.baseline_cost_per_ton
	  FROM pf_simulation.result r
	  JOIN pf_simulation.simulation s ON s.tenant_id = r.tenant_id AND s.id = r.simulation_id
	  JOIN pf_simulation.scenario sc ON sc.tenant_id = s.tenant_id AND sc.id = s.scenario_id
	  WHERE r.simulation_id = ANY($1::uuid[])`, strs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]domain.ResultSummary{}
	for rows.Next() {
		var s domain.ResultSummary
		if err := rows.Scan(&s.SimulationID, &s.ScenarioID, &s.ScenarioCode, &s.ScenarioName, &s.Official, &s.ParameterSetVersion,
			&s.LifecycleYears, &s.Discounted, &s.HubCount, &s.DeliveredTons, &s.Capex, &s.OpexYear1, &s.CollectionCost, &s.TransportationCost,
			&s.WarehouseCost, &s.HandlingCost, &s.TotalLogisticsCost, &s.CostPerTon, &s.TCO, &s.TCOPerTon, &s.LifecycleCostPerTon, &s.ROIPct,
			&s.PaybackYears, &s.PaybackStatus, &s.PlantAnnualDemandTons, &s.PlantDemandCoveragePct, &s.ShortfallWarnings, &s.BaselineAvailable,
			&s.BaselineDeliveredTons, &s.BaselineCollection, &s.BaselineTransportation, &s.BaselineTotalCost, &s.BaselineCostPerTon); err != nil {
			return nil, err
		}
		out[s.SimulationID] = s
	}
	return out, rows.Err()
}

// LatestCostPerTon trả Cost/Ton của kết quả mới nhất của một kịch bản (baseline "Kịch bản Hub khác").
func LatestCostPerTon(ctx context.Context, tx pgx.Tx, scenarioID uuid.UUID) (*decimal.Decimal, error) {
	var v *decimal.Decimal
	err := tx.QueryRow(ctx, `SELECT r.cost_per_ton FROM pf_simulation.result r
	  JOIN pf_simulation.simulation s ON s.tenant_id = r.tenant_id AND s.id = r.simulation_id
	  WHERE s.scenario_id = $1 ORDER BY r.received_at DESC LIMIT 1`, scenarioID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// InsertSensitivity ghi kết quả độ nhạy bất biến.
func InsertSensitivity(ctx context.Context, tx pgx.Tx, tid, simID uuid.UUID, payload []byte, sha string, s domain.SensitivitySummary, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_simulation.sensitivity_result (tenant_id, simulation_id, payload, payload_sha256, high_sensitivity, swing_pct, received_at)
	  VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7)`, tid, simID, string(payload), sha, s.HighSensitivity, s.SwingPct, at.UTC())
	return err
}

// Sensitivity đọc kết quả độ nhạy (nil nếu không chạy).
func Sensitivity(ctx context.Context, tx pgx.Tx, simID uuid.UUID) (json.RawMessage, error) {
	var p string
	err := tx.QueryRow(ctx, `SELECT payload::text FROM pf_simulation.sensitivity_result WHERE simulation_id = $1`, simID).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return json.RawMessage(p), err
}
