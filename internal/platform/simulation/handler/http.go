// Package handler: HTTP của pf/simulation — Application API (contracts/openapi/
// application.yaml, mục "# --- pf/simulation ---") và Internal API cho optimizer.
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.POST("/candidate-hubs", h.createHub)
	rg.GET("/candidate-hubs", h.listHubs)
	rg.POST("/candidate-hubs/:id/retire", h.retireHub)
	rg.POST(`/scenarios\:compare`, h.compare)
	rg.POST("/scenarios", h.createScenario)
	rg.GET("/scenarios", h.listScenarios)
	rg.GET("/scenarios/:id", h.getScenario)
	rg.GET("/scenarios/:id/history", h.history)
	rg.POST("/scenarios/:id/hubs", h.attachHub)
	rg.POST("/scenarios/:id/hubs/:hub_id/detach", h.detachHub)
	rg.POST("/simulations", h.submit)
	rg.GET("/simulations/:id", h.getSimulation)
	rg.GET("/simulations/:id/result", h.getResult)
	rg.POST("/simulations/:id/mark-official", h.markOfficial)
	rg.GET("/jobs/:id", h.job)
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func param(c *gin.Context, name string, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func tsp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

// ---------------------------------------------------------------------------
// Hub ứng viên
// ---------------------------------------------------------------------------

type locationDTO struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type hubDTO struct {
	ID            string      `json:"id"`
	Code          string      `json:"code"`
	Name          string      `json:"name"`
	Location      locationDTO `json:"location"`
	UnitCode      *string     `json:"unit_code"`
	Status        string      `json:"status"`
	RetiredReason *string     `json:"retired_reason"`
	CreatedAt     string      `json:"created_at"`
}

func toHub(h domain.CandidateHub) hubDTO {
	return hubDTO{ID: h.ID.String(), Code: h.Code, Name: h.Name, Location: locationDTO{Lat: h.Location.Lat, Lng: h.Location.Lng},
		UnitCode: h.UnitCode, Status: h.Status, RetiredReason: h.RetiredReason, CreatedAt: ts(h.CreatedAt)}
}

type createHubReq struct {
	Name     string      `json:"name"`
	Location locationDTO `json:"location"`
	UnitCode string      `json:"unit_code"`
}

func (h Handler) createHub(c *gin.Context) {
	var req createHubReq
	if !bind(c, &req) {
		return
	}
	hub, replayed, err := h.Svc.CreateHub(c.Request.Context(), app.HubInput{Name: req.Name, Lat: req.Location.Lat, Lng: req.Location.Lng,
		UnitCode: req.UnitCode, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, toHub(hub))
}

func (h Handler) listHubs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	pg, err := h.Svc.ListHubs(c.Request.Context(), c.Query("cursor"), limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]hubDTO, 0, len(pg.Items))
	for _, x := range pg.Items {
		items = append(items, toHub(x))
	}
	var next *string
	if pg.Next != "" {
		next = &pg.Next
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

type reasonReq struct {
	Reason string `json:"reason"`
}

func (h Handler) retireHub(c *gin.Context) {
	id, ok := param(c, "id", app.ErrHubNotFound)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	hub, err := h.Svc.RetireHub(c.Request.Context(), id, req.Reason)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toHub(hub))
}

// ---------------------------------------------------------------------------
// Kịch bản
// ---------------------------------------------------------------------------

type plantDTO struct {
	PlantRef         *string          `json:"plant_ref"`
	Location         locationDTO      `json:"location"`
	AnnualDemandTons *decimal.Decimal `json:"annual_demand_tons"`
}

type scenarioHubDTO struct {
	ScenarioHubID      string           `json:"scenario_hub_id"`
	HubID              string           `json:"hub_id"`
	HubCode            string           `json:"hub_code"`
	ServiceRadiusKm    decimal.Decimal  `json:"service_radius_km"`
	DesignCapacityTons decimal.Decimal  `json:"design_capacity_tons"`
	BargePayloadTons   *decimal.Decimal `json:"barge_payload_tons"`
	ModeFieldHub       string           `json:"mode_field_hub"`
	ModeHubPlant       string           `json:"mode_hub_plant"`
	LandMode           string           `json:"land_mode"`
	SortOrder          int              `json:"sort_order"`
}

type scenarioDTO struct {
	ID                       string           `json:"id"`
	Code                     string           `json:"code"`
	Name                     string           `json:"name"`
	Plant                    plantDTO         `json:"plant"`
	LifecycleYears           int              `json:"lifecycle_years"`
	Discounted               bool             `json:"discounted"`
	BaselineMode             string           `json:"baseline_mode"`
	BaselineManualCostPerTon *decimal.Decimal `json:"baseline_manual_cost_per_ton"`
	BaselineScenarioID       *string          `json:"baseline_scenario_id"`
	ReferenceYear            int              `json:"reference_year"`
	Version                  int64            `json:"version"`
	Hubs                     []scenarioHubDTO `json:"hubs"`
	CreatedAt                string           `json:"created_at"`
	UpdatedAt                string           `json:"updated_at"`
}

func uuidStr(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

func toScenario(s domain.Scenario) scenarioDTO {
	d := scenarioDTO{ID: s.ID.String(), Code: s.Code, Name: s.Name,
		Plant:          plantDTO{PlantRef: uuidStr(s.PlantRef), Location: locationDTO{Lat: s.PlantLocation.Lat, Lng: s.PlantLocation.Lng}, AnnualDemandTons: s.PlantAnnualDemandTons},
		LifecycleYears: s.LifecycleYears, Discounted: s.Discounted, BaselineMode: s.BaselineMode, BaselineManualCostPerTon: s.BaselineManualCostPerTon,
		BaselineScenarioID: uuidStr(s.BaselineScenarioID), ReferenceYear: s.ReferenceYear, Version: s.Version, Hubs: []scenarioHubDTO{},
		CreatedAt: ts(s.CreatedAt), UpdatedAt: ts(s.UpdatedAt)}
	for _, x := range s.Hubs {
		d.Hubs = append(d.Hubs, scenarioHubDTO{ScenarioHubID: x.ID.String(), HubID: x.HubID.String(), HubCode: x.HubCode, ServiceRadiusKm: x.ServiceRadiusKm,
			DesignCapacityTons: x.DesignCapacityTons, BargePayloadTons: x.BargePayloadTons, ModeFieldHub: x.ModeFieldHub, ModeHubPlant: x.ModeHubPlant,
			LandMode: x.LandMode, SortOrder: x.SortOrder})
	}
	return d
}

type createScenarioReq struct {
	Name                     string           `json:"name"`
	Plant                    plantDTO         `json:"plant"`
	LifecycleYears           int              `json:"lifecycle_years"`
	Discounted               bool             `json:"discounted"`
	BaselineMode             string           `json:"baseline_mode"`
	BaselineManualCostPerTon *decimal.Decimal `json:"baseline_manual_cost_per_ton"`
	BaselineScenarioID       *string          `json:"baseline_scenario_id"`
	ReferenceYear            int              `json:"reference_year"`
}

func parseOptUUID(s *string, field string) (*uuid.UUID, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	u, err := uuid.Parse(*s)
	if err != nil {
		return nil, problem.Validation.WithField(field, "phải là UUID")
	}
	return &u, nil
}

func (h Handler) createScenario(c *gin.Context) {
	var req createScenarioReq
	if !bind(c, &req) {
		return
	}
	plantRef, err := parseOptUUID(req.Plant.PlantRef, "plant.plant_ref")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	baseRef, err := parseOptUUID(req.BaselineScenarioID, "baseline_scenario_id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	sc, replayed, err := h.Svc.CreateScenario(c.Request.Context(), app.ScenarioInput{Name: req.Name, PlantRef: plantRef, PlantLat: req.Plant.Location.Lat,
		PlantLng: req.Plant.Location.Lng, PlantAnnualDemandTons: req.Plant.AnnualDemandTons, LifecycleYears: req.LifecycleYears, Discounted: req.Discounted,
		BaselineMode: req.BaselineMode, BaselineManualCostPerTon: req.BaselineManualCostPerTon, BaselineScenarioID: baseRef,
		ReferenceYear: req.ReferenceYear, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, toScenario(sc))
}

func (h Handler) listScenarios(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	pg, err := h.Svc.ListScenarios(c.Request.Context(), c.Query("cursor"), limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]scenarioDTO, 0, len(pg.Items))
	for _, x := range pg.Items {
		items = append(items, toScenario(x))
	}
	var next *string
	if pg.Next != "" {
		next = &pg.Next
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func (h Handler) getScenario(c *gin.Context) {
	id, ok := param(c, "id", app.ErrScenarioNotFound)
	if !ok {
		return
	}
	sc, err := h.Svc.GetScenario(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toScenario(sc))
}

type changeDTO struct {
	ID               string          `json:"id"`
	Seq              int64           `json:"seq"`
	Kind             string          `json:"kind"`
	HubID            *string         `json:"hub_id"`
	SimulationID     *string         `json:"simulation_id"`
	Payload          json.RawMessage `json:"payload"`
	EventTime        string          `json:"event_time"`
	RecordedAt       string          `json:"recorded_at"`
	SupersedesID     *string         `json:"supersedes_id"`
	CorrectionReason *string         `json:"correction_reason"`
	ActorRef         string          `json:"actor_ref"`
}

func (h Handler) history(c *gin.Context) {
	id, ok := param(c, "id", app.ErrScenarioNotFound)
	if !ok {
		return
	}
	rows, err := h.Svc.History(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]changeDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, changeDTO{ID: r.ID.String(), Seq: r.Seq, Kind: r.Kind, HubID: uuidStr(r.HubID), SimulationID: uuidStr(r.SimulationID),
			Payload: r.Payload, EventTime: ts(r.EventTime), RecordedAt: ts(r.RecordedAt), SupersedesID: uuidStr(r.SupersedesID),
			CorrectionReason: r.CorrectionReason, ActorRef: r.ActorRef.String()})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type attachReq struct {
	HubID              string           `json:"hub_id"`
	ServiceRadiusKm    *decimal.Decimal `json:"service_radius_km"`
	DesignCapacityTons *decimal.Decimal `json:"design_capacity_tons"`
	BargePayloadTons   *decimal.Decimal `json:"barge_payload_tons"`
	ModeFieldHub       *string          `json:"mode_field_hub"`
	ModeHubPlant       *string          `json:"mode_hub_plant"`
	LandMode           *string          `json:"land_mode"`
}

func (h Handler) attachHub(c *gin.Context) {
	id, ok := param(c, "id", app.ErrScenarioNotFound)
	if !ok {
		return
	}
	var req attachReq
	if !bind(c, &req) {
		return
	}
	hubID, err := uuid.Parse(req.HubID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("hub_id", "phải là UUID"))
		return
	}
	sc, err := h.Svc.AttachHub(c.Request.Context(), id, hubID, domain.HubConfig{ServiceRadiusKm: req.ServiceRadiusKm, DesignCapacityTons: req.DesignCapacityTons,
		BargePayloadTons: req.BargePayloadTons, ModeFieldHub: req.ModeFieldHub, ModeHubPlant: req.ModeHubPlant, LandMode: req.LandMode}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toScenario(sc))
}

func (h Handler) detachHub(c *gin.Context) {
	id, ok := param(c, "id", app.ErrScenarioNotFound)
	if !ok {
		return
	}
	hubID, ok := param(c, "hub_id", app.ErrHubNotAttached)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	sc, err := h.Svc.DetachHub(c.Request.Context(), id, hubID, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toScenario(sc))
}

// ---------------------------------------------------------------------------
// Mô phỏng
// ---------------------------------------------------------------------------

type submitReq struct {
	ScenarioID       string   `json:"scenario_id"`
	Sensitivity      bool     `json:"sensitivity"`
	SensitivityCodes []string `json:"sensitivity_codes"`
	Mode             string   `json:"mode"`
	MaxHubs          *int     `json:"max_hubs"`
}

type snapshotDTO struct {
	ID                string `json:"id"`
	SHA256            string `json:"sha256"`
	ParamSetVersion   string `json:"param_set_version"`
	AllParamsApproved bool   `json:"all_params_approved"`
	NetworkVersion    string `json:"network_version"`
	AlgorithmVersion  string `json:"algorithm_version"`
	CooperativeCount  int    `json:"cooperative_count"`
	RouteCount        int    `json:"route_count"`
	CreatedAt         string `json:"created_at"`
}

type simulationDTO struct {
	ID               string             `json:"id"`
	ScenarioID       string             `json:"scenario_id"`
	ScenarioVersion  int64              `json:"scenario_version"`
	Status           string             `json:"status"`
	Options          domain.SnapOptions `json:"options"`
	RequestedBy      string             `json:"requested_by"`
	RequestedAt      string             `json:"requested_at"`
	CompletedAt      *string            `json:"completed_at"`
	ErrorCode        *string            `json:"error_code"`
	ErrorDetail      *string            `json:"error_detail"`
	Snapshot         *snapshotDTO       `json:"snapshot"`
	HasResult        bool               `json:"has_result"`
	Official         bool               `json:"official"`
	OfficialEligible bool               `json:"official_eligible"`
	OfficialAt       *string            `json:"official_at"`
	Links            map[string]string  `json:"links"`
}

func toSimulation(v app.SimulationView) simulationDTO {
	s := v.Simulation
	d := simulationDTO{ID: s.ID.String(), ScenarioID: s.ScenarioID.String(), ScenarioVersion: s.ScenarioVersion, Status: s.Status, Options: s.Options,
		RequestedBy: s.RequestedBy.String(), RequestedAt: ts(s.RequestedAt), CompletedAt: tsp(s.CompletedAt), ErrorCode: s.ErrorCode,
		ErrorDetail: s.ErrorDetail, HasResult: v.HasResult, Official: s.Official, OfficialEligible: v.OfficialEligible(), OfficialAt: tsp(s.OfficialAt),
		Links: map[string]string{"self": "/v1/simulations/" + s.ID.String(), "job": "/v1/jobs/" + s.ID.String(), "result": "/v1/simulations/" + s.ID.String() + "/result"}}
	if m := v.Snapshot; m != nil {
		d.Snapshot = &snapshotDTO{ID: m.ID.String(), SHA256: m.SHA256, ParamSetVersion: m.ParamSetVersion, AllParamsApproved: m.AllParamsApproved,
			NetworkVersion: m.NetworkVersion, AlgorithmVersion: m.AlgorithmVersion, CooperativeCount: m.CooperativeCount, RouteCount: m.RouteCount,
			CreatedAt: ts(m.CreatedAt)}
	}
	return d
}

func (h Handler) submit(c *gin.Context) {
	var req submitReq
	if !bind(c, &req) {
		return
	}
	sid, err := uuid.Parse(req.ScenarioID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("scenario_id", "phải là UUID"))
		return
	}
	sim, _, err := h.Svc.Submit(c.Request.Context(), app.SubmitInput{ScenarioID: sid, Sensitivity: req.Sensitivity, SensitivityCodes: req.SensitivityCodes,
		Mode: req.Mode, MaxHubs: req.MaxHubs, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	// R-52: việc dài → 202 + Location tới job.
	c.Header("Location", "/v1/jobs/"+sim.ID.String())
	c.JSON(http.StatusAccepted, toSimulation(app.SimulationView{Simulation: sim}))
}

func (h Handler) getSimulation(c *gin.Context) {
	id, ok := param(c, "id", app.ErrSimulationNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetSimulation(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toSimulation(v))
}

// job là /v1/jobs/{id} của mục 12 cho việc mô phỏng (id việc = id mô phỏng).
func (h Handler) job(c *gin.Context) {
	id, ok := param(c, "id", problem.NotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetSimulation(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	d := toSimulation(v)
	c.JSON(http.StatusOK, gin.H{"id": d.ID, "kind": "simulation", "status": d.Status, "error_code": d.ErrorCode, "requested_at": d.RequestedAt,
		"completed_at": d.CompletedAt, "links": d.Links})
}

func (h Handler) getResult(c *gin.Context) {
	id, ok := param(c, "id", app.ErrSimulationNotFound)
	if !ok {
		return
	}
	r, err := h.Svc.GetResult(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	d := toSimulation(r.View)
	var sens any
	if len(r.Sensitivity) > 0 {
		sens = r.Sensitivity
	}
	c.JSON(http.StatusOK, gin.H{
		"simulation_id": d.ID, "scenario_id": d.ScenarioID, "status": statusLabel(r.View), "official": d.Official,
		"official_eligible": d.OfficialEligible, "snapshot": d.Snapshot, "engine_version": r.Result.EngineVersion,
		"algorithm_version": r.Result.AlgorithmVersion, "result_sha256": r.Result.PayloadSHA256, "received_at": ts(r.Result.ReceivedAt),
		"result": r.Result.Payload, "sensitivity": sens,
	})
}

// statusLabel: "chinh_thuc" chỉ khi đã đánh dấu (và ảnh chụp đủ phê duyệt), còn lại "tham_khao".
func statusLabel(v app.SimulationView) string {
	if v.Simulation.Official && v.OfficialEligible() {
		return "chinh_thuc"
	}
	return "tham_khao"
}

func (h Handler) markOfficial(c *gin.Context) {
	id, ok := param(c, "id", app.ErrSimulationNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.MarkOfficial(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toSimulation(v))
}

type compareReq struct {
	SimulationIDs   []string `json:"simulation_ids"`
	IncludeBaseline *bool    `json:"include_baseline"`
}

type compareRowDTO struct {
	SimulationID           *string          `json:"simulation_id"`
	ScenarioID             *string          `json:"scenario_id"`
	Code                   string           `json:"code"`
	Name                   string           `json:"name"`
	Official               bool             `json:"official"`
	IsBaselineColumn       bool             `json:"is_baseline_column"`
	ParameterSetVersion    string           `json:"parameter_set_version"`
	LifecycleYears         int              `json:"lifecycle_years"`
	Discounted             bool             `json:"discounted"`
	HubCount               int              `json:"hub_count"`
	DeliveredTons          decimal.Decimal  `json:"delivered_tons"`
	Capex                  decimal.Decimal  `json:"capex"`
	OpexYear1              decimal.Decimal  `json:"opex_year1"`
	CollectionCost         decimal.Decimal  `json:"collection_cost"`
	TransportationCost     decimal.Decimal  `json:"transportation_cost"`
	WarehouseCost          decimal.Decimal  `json:"warehouse_cost"`
	HandlingCost           decimal.Decimal  `json:"handling_cost"`
	TotalLogisticsCost     decimal.Decimal  `json:"total_delivered_logistics_cost"`
	CostPerTon             *decimal.Decimal `json:"cost_per_ton"`
	TCO                    decimal.Decimal  `json:"tco"`
	TCOPerTon              *decimal.Decimal `json:"tco_per_ton"`
	LifecycleCostPerTon    *decimal.Decimal `json:"lifecycle_cost_per_ton"`
	ROIPct                 *decimal.Decimal `json:"roi_pct"`
	PaybackYears           *int             `json:"payback_years"`
	PaybackStatus          string           `json:"payback_status"`
	PlantDemandCoveragePct decimal.Decimal  `json:"plant_demand_coverage_pct"`
	ShortfallWarnings      int              `json:"shortfall_warnings"`
}

func (h Handler) compare(c *gin.Context) {
	var req compareReq
	if !bind(c, &req) {
		return
	}
	ids := make([]uuid.UUID, 0, len(req.SimulationIDs))
	for _, s := range req.SimulationIDs {
		u, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("simulation_ids", "phải là UUID"))
			return
		}
		ids = append(ids, u)
	}
	include := req.IncludeBaseline == nil || *req.IncludeBaseline
	cmp, err := h.Svc.Compare(c.Request.Context(), ids, include)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows := make([]compareRowDTO, 0, len(cmp.Rows))
	for _, r := range cmp.Rows {
		d := compareRowDTO{Code: r.ScenarioCode, Name: r.ScenarioName, Official: r.Official, IsBaselineColumn: r.IsBaselineColumn,
			ParameterSetVersion: r.ParameterSetVersion, LifecycleYears: r.LifecycleYears, Discounted: r.Discounted, HubCount: r.HubCount,
			DeliveredTons: r.DeliveredTons, Capex: r.Capex, OpexYear1: r.OpexYear1, CollectionCost: r.CollectionCost,
			TransportationCost: r.TransportationCost, WarehouseCost: r.WarehouseCost, HandlingCost: r.HandlingCost,
			TotalLogisticsCost: r.TotalLogisticsCost, CostPerTon: r.CostPerTon, TCO: r.TCO, TCOPerTon: r.TCOPerTon,
			LifecycleCostPerTon: r.LifecycleCostPerTon, ROIPct: r.ROIPct, PaybackYears: r.PaybackYears, PaybackStatus: r.PaybackStatus,
			PlantDemandCoveragePct: r.PlantDemandCoveragePct, ShortfallWarnings: r.ShortfallWarnings}
		if !r.IsBaselineColumn {
			d.SimulationID, d.ScenarioID = uuidStr(&r.SimulationID), uuidStr(&r.ScenarioID)
		}
		rows = append(rows, d)
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows, "sorted_by": cmp.SortedBy, "blocked": cmp.Blocked, "warnings": cmp.Warnings})
}
