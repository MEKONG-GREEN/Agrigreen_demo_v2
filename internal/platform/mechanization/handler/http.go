package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
)

// Handler gói app.Service cho HTTP.
type Handler struct{ Svc *app.Service }

const base = "/mechanization"

// RegisterApp gắn route Application API — chỉ đọc (Cục KTHT: cgh_viewer; Admin: cgh_admin).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group(base)
	g.GET("/owners", h.listOwners)
	g.GET("/owners/:id", h.getOwner)
	g.GET("/machines", h.listMachines)
	g.GET("/machines/:ref", h.getMachine)
	g.GET("/cooperatives/:ref/machine-count", h.machineCount)
	g.GET("/cooperatives/:ref/panel", h.panel)
	g.GET("/season-areas", h.listAreas)
	g.GET("/balance-runs", h.listRuns)
	g.GET("/balance-runs/:id", h.getRun)
	g.GET("/coverage", h.coverage)
	g.GET("/dashboard", h.dashboard)
	g.GET("/trend", h.trend)
	h.registerForecastApp(g)
	h.registerAreaSyncApp(g) // CGH FN-07: nhật ký đồng bộ, độ tươi
}

// RegisterAdmin gắn route Admin API (MFA bắt buộc ở router) — chỉ cgh_admin ghi.
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	g := rg.Group(base)
	g.POST("/owners", h.createOwner)
	g.POST("/owners/:id/deactivate", h.deactivateOwner)
	g.POST("/owners/:id/reactivate", h.reactivateOwner)
	g.GET("/owners/:id/events", h.ownerEvents)
	g.POST("/machines", h.createMachine)
	g.POST("/machines/import", h.importMachines)
	g.POST("/machines/:ref/update", h.updateMachine)
	g.POST("/machines/:ref/status", h.changeStatus)
	g.POST("/machines/:ref/deactivate", h.deactivateMachine)
	g.POST("/machines/:ref/reactivate", h.reactivateMachine)
	g.GET("/machines/:ref/events", h.machineEvents)
	g.POST("/season-areas", h.recordArea)
	g.POST("/season-areas/:id/lock", h.lockArea)
	g.POST("/season-areas/:id/unlock", h.unlockArea)
	g.GET("/season-areas/:id/events", h.areaEvents)
	g.POST("/balance-runs", h.computeBalance)
	h.registerForecastAdmin(g)
}

func created(replayed bool) int {
	if replayed {
		return http.StatusOK
	}
	return http.StatusCreated
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

// ---------------------------------------------------------------------------
// Chủ sở hữu
// ---------------------------------------------------------------------------

type createOwnerReq struct {
	OwnerType      string `json:"owner_type"`
	CooperativeRef string `json:"cooperative_ref"`
	Name           string `json:"name" pii:"true"`
	Phone          string `json:"phone" pii:"true"`
}

func (h Handler) createOwner(c *gin.Context) {
	var req createOwnerReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	t, ok := domain.ParseOwnerType(req.OwnerType)
	if !ok {
		fe.add("owner_type", "Loại chủ sở hữu phải là một trong: Thành viên HTX / HTX / Doanh nghiệp / Đơn vị khác")
	}
	coop := fe.ref("cooperative_ref", req.CooperativeRef)
	if fe.fail(c) {
		return
	}
	out, err := h.Svc.CreateOwner(c.Request.Context(), app.CreateOwnerInput{
		Type: t, CooperativeRef: coop, Identity: app.OwnerIdentity{Name: req.Name, Phone: req.Phone}, IdemKey: idem(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1"+base+"/owners/"+out.Owner.ID.String())
	c.JSON(created(out.Replayed), gin.H{"owner": toOwnerDTO(app.OwnerView{Owner: out.Owner}), "seq": out.Event.Seq})
}

type ownerStatusReq struct {
	Reason        string  `json:"reason"`
	WithMachines  bool    `json:"with_machines"`
	DeactivatedOn *string `json:"deactivated_on"`
}

func (h Handler) ownerStatus(c *gin.Context, deactivate bool) {
	id, ok := pathID(c, "id", app.ErrOwnerNotFound)
	if !ok {
		return
	}
	var req ownerStatusReq
	if c.Request.ContentLength != 0 && !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	in := app.OwnerStatusInput{ID: id, Reason: req.Reason, WithMachines: req.WithMachines, IdemKey: idem(c)}
	in.DeactivatedOn = fe.optDate("deactivated_on", req.DeactivatedOn)
	if fe.fail(c) {
		return
	}
	fn := h.Svc.ReactivateOwner
	if deactivate {
		fn = h.Svc.DeactivateOwner
	}
	out, err := fn(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	ms := make([]machineDTO, 0, len(out.Machines))
	for _, m := range out.Machines {
		ms = append(ms, toMachineDTO(m))
	}
	c.JSON(http.StatusOK, gin.H{"owner": toOwnerDTO(app.OwnerView{Owner: out.Owner}), "event": toOwnerEventDTO(out.Event), "deactivated_machines": ms})
}

func (h Handler) deactivateOwner(c *gin.Context) { h.ownerStatus(c, true) }
func (h Handler) reactivateOwner(c *gin.Context) { h.ownerStatus(c, false) }

func (h Handler) listOwners(c *gin.Context) {
	after, ok := parseCursor(c)
	if !ok {
		return
	}
	var fe fieldErrs
	q := app.OwnerQuery{
		CooperativeRef: fe.ref("cooperative_ref", c.Query("cooperative_ref")), Type: c.Query("owner_type"),
		IncludeInactive: c.Query("include_inactive") == "true", After: after, Limit: queryLimit(c),
	}
	if fe.fail(c) {
		return
	}
	pg, err := h.Svc.ListOwners(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]ownerDTO, 0, len(pg.Items))
	for _, v := range pg.Items {
		items = append(items, toOwnerDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
}

func (h Handler) getOwner(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrOwnerNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetOwner(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toOwnerDTO(v))
}

func (h Handler) ownerEvents(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrOwnerNotFound)
	if !ok {
		return
	}
	evs, err := h.Svc.OwnerHistory(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]ownerEventDTO, 0, len(evs))
	for _, e := range evs {
		items = append(items, toOwnerEventDTO(e))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ---------------------------------------------------------------------------
// Máy
// ---------------------------------------------------------------------------

type machineReq struct {
	CooperativeRef *string          `json:"cooperative_ref"`
	MachineType    *string          `json:"machine_type"`
	FarmStage      *string          `json:"farm_stage"` // bị từ chối: khâu tự gán theo chủng loại
	OwnerID        *string          `json:"owner_id"`
	Brand          *string          `json:"brand"`
	Model          *string          `json:"model"`
	SerialNo       *string          `json:"serial_no"`
	ChassisNo      *string          `json:"chassis_no"`
	YearMade       *int             `json:"year_made"`
	Fuel           *string          `json:"fuel"`
	PowerHP        *decimal.Decimal `json:"power_hp"`
	CapacityHa     *decimal.Decimal `json:"capacity_ha"`
	OwnedSince     *string          `json:"owned_since"`
	Status         *string          `json:"status"`
}

func val(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (h Handler) createMachine(c *gin.Context) {
	var req machineReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	if req.FarmStage != nil {
		fe.add("farm_stage", domain.ErrStageDerived.Error())
	}
	in := app.MachineInput{
		CooperativeRef: fe.ref("cooperative_ref", val(req.CooperativeRef)), MachineType: val(req.MachineType),
		OwnerID: fe.ref("owner_id", val(req.OwnerID)), Brand: val(req.Brand), Model: val(req.Model), SerialNo: val(req.SerialNo),
		ChassisNo: val(req.ChassisNo), YearMade: req.YearMade, Fuel: val(req.Fuel), PowerHP: req.PowerHP, CapacityHa: req.CapacityHa,
		IdemKey: idem(c),
	}
	if d := fe.date("owned_since", val(req.OwnedSince)); d != nil {
		in.OwnedSince = *d
	}
	if req.Status != nil {
		st, ok := domain.ParseStatus(*req.Status)
		if !ok {
			fe.add("status", domain.ErrStatus.Error())
		}
		in.Status = st
	}
	if fe.fail(c) {
		return
	}
	out, err := h.Svc.CreateMachine(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1"+base+"/machines/"+out.Machine.Ref.String())
	c.JSON(created(out.Replayed), gin.H{"machine": toMachineDTO(out.Machine), "seq": out.Event.Seq})
}

func (h Handler) updateMachine(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrMachineNotFound)
	if !ok {
		return
	}
	var req machineReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	if req.FarmStage != nil {
		fe.add("farm_stage", domain.ErrStageDerived.Error())
	}
	if req.Status != nil {
		fe.add("status", "đổi tình trạng qua POST /machines/{ref}/status")
	}
	in := app.MachinePatch{
		Ref: ref, CooperativeRef: fe.optRef("cooperative_ref", req.CooperativeRef), MachineType: req.MachineType,
		OwnerID: fe.optRef("owner_id", req.OwnerID), Brand: req.Brand, Model: req.Model, SerialNo: req.SerialNo, ChassisNo: req.ChassisNo,
		YearMade: req.YearMade, Fuel: req.Fuel, PowerHP: req.PowerHP, CapacityHa: req.CapacityHa,
		OwnedSince: fe.optDate("owned_since", req.OwnedSince), IdemKey: idem(c),
	}
	if fe.fail(c) {
		return
	}
	writeMachine(c)(h.Svc.UpdateMachine(c.Request.Context(), in))
}

func writeMachine(c *gin.Context) func(app.MachineResult, error) {
	return func(out app.MachineResult, err error) {
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"machine": toMachineDTO(out.Machine), "event": toMachineEventDTO(out.Event)})
	}
}

type statusReq struct {
	Status      string  `json:"status"`
	EffectiveAt *string `json:"effective_at"`
}

func (h Handler) changeStatus(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrMachineNotFound)
	if !ok {
		return
	}
	var req statusReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	st, ok := domain.ParseStatus(req.Status)
	if !ok {
		fe.add("status", domain.ErrStatus.Error())
	}
	at := fe.instant("effective_at", req.EffectiveAt)
	if fe.fail(c) {
		return
	}
	writeMachine(c)(h.Svc.ChangeStatus(c.Request.Context(), app.StatusInput{Ref: ref, Status: st, EffectiveAt: at, IdemKey: idem(c)}))
}

type machineActiveReq struct {
	DeactivatedOn *string `json:"deactivated_on"`
	Reason        string  `json:"reason"`
}

func (h Handler) machineActive(c *gin.Context, deactivate bool) {
	ref, ok := pathID(c, "ref", app.ErrMachineNotFound)
	if !ok {
		return
	}
	var req machineActiveReq
	if c.Request.ContentLength != 0 && !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	in := app.MachineStatusChange{Ref: ref, Reason: req.Reason, IdemKey: idem(c), DeactivatedOn: fe.optDate("deactivated_on", req.DeactivatedOn)}
	if fe.fail(c) {
		return
	}
	fn := h.Svc.ReactivateMachine
	if deactivate {
		fn = h.Svc.DeactivateMachine
	}
	writeMachine(c)(fn(c.Request.Context(), in))
}

func (h Handler) deactivateMachine(c *gin.Context) { h.machineActive(c, true) }
func (h Handler) reactivateMachine(c *gin.Context) { h.machineActive(c, false) }

type importRowDTO struct {
	Row        int     `json:"row"`
	Status     string  `json:"status"`
	Reason     *string `json:"reason"`
	MachineRef *string `json:"machine_ref"`
	Code       *string `json:"code"`
}

func (h Handler) importMachines(c *gin.Context) {
	var fe fieldErrs
	coop := fe.ref("cooperative_ref", c.Query("cooperative_ref"))
	if fe.fail(c) {
		return
	}
	out, err := h.Svc.ImportMachines(c.Request.Context(), app.ImportInput{
		CooperativeRef: coop, Mode: c.Query("mode"), Body: c.Request.Body, IdemKey: idem(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows := make([]importRowDTO, 0, len(out.Rows))
	for _, r := range out.Rows {
		rows = append(rows, importRowDTO{Row: r.Row, Status: r.Status, Reason: strPtr(r.Reason), MachineRef: refPtr(r.MachineRef), Code: strPtr(r.Code)})
	}
	deact := make([]machineDTO, 0, len(out.Deactivated))
	for _, m := range out.Deactivated {
		deact = append(deact, toMachineDTO(m))
	}
	c.JSON(http.StatusOK, gin.H{
		"mode": out.Mode, "created": out.Created, "updated": out.Updated, "reactivated": out.Reactivated, "unchanged": out.Unchanged,
		"rejected": out.Rejected, "rows": rows, "deactivated": deact,
	})
}

func (h Handler) listMachines(c *gin.Context) {
	after, ok := parseCursor(c)
	if !ok {
		return
	}
	var fe fieldErrs
	q := app.MachineQuery{
		CooperativeRef: fe.ref("cooperative_ref", c.Query("cooperative_ref")), OwnerID: fe.ref("owner_id", c.Query("owner_id")),
		FarmStage: c.Query("farm_stage"), MachineType: c.Query("machine_type"), Status: c.Query("status"),
		IncludeInactive: c.Query("include_inactive") == "true", After: after, Limit: queryLimit(c),
	}
	if fe.fail(c) {
		return
	}
	pg, err := h.Svc.ListMachines(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]machineDTO, 0, len(pg.Items))
	for _, m := range pg.Items {
		items = append(items, toMachineDTO(m))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
}

func (h Handler) getMachine(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrMachineNotFound)
	if !ok {
		return
	}
	m, err := h.Svc.GetMachine(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toMachineDTO(m))
}

func (h Handler) machineEvents(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrMachineNotFound)
	if !ok {
		return
	}
	evs, err := h.Svc.MachineHistory(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]machineEventDTO, 0, len(evs))
	for _, e := range evs {
		items = append(items, toMachineEventDTO(e))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type stageCountDTO struct {
	FarmStage   string         `json:"farm_stage"`
	MachineType string         `json:"machine_type"`
	Total       int            `json:"total"`
	ByStatus    map[string]int `json:"by_status"`
}

func (h Handler) machineCount(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrCoopInvalid)
	if !ok {
		return
	}
	var fe fieldErrs
	at := fe.date("at", c.Query("at"))
	if at == nil && c.Query("at") == "" {
		fe.add("at", "bắt buộc, dạng YYYY-MM-DD")
	}
	if fe.fail(c) {
		return
	}
	out, err := h.Svc.MachineCountAt(c.Request.Context(), ref, *at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	by := make([]stageCountDTO, 0, len(out.ByStage))
	for _, s := range out.ByStage {
		by = append(by, stageCountDTO{FarmStage: s.FarmStage, MachineType: s.MachineType, Total: s.Total, ByStatus: s.ByStatus})
	}
	c.JSON(http.StatusOK, gin.H{"cooperative_ref": ref.String(), "at": dateStr(out.At), "total": out.Total, "active": out.Active, "by_stage": by})
}

type panelMachineDTO struct {
	machineDTO
	OwnerCode        string `json:"owner_code"`
	OwnerLabel       string `json:"owner_label" pii:"true"`
	OwnerMaskedPhone string `json:"owner_masked_phone" pii:"true"`
}

func (h Handler) panel(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrCoopInvalid)
	if !ok {
		return
	}
	var fe fieldErrs
	q := fe.runQuery(c)
	if fe.fail(c) {
		return
	}
	pn, err := h.Svc.CooperativePanel(c.Request.Context(), ref, q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	ms := make([]panelMachineDTO, 0, len(pn.Machines))
	for _, m := range pn.Machines {
		ms = append(ms, panelMachineDTO{machineDTO: toMachineDTO(m.Machine), OwnerCode: m.OwnerCode, OwnerLabel: m.OwnerLabel, OwnerMaskedPhone: m.OwnerMaskedPhone})
	}
	body := gin.H{
		"cooperative_ref": pn.CooperativeRef.String(), "name": pn.Name, "commune_code": strPtr(pn.CommuneCode),
		"province_code": strPtr(pn.ProvinceCode), "machines": ms, "coverage": nil, "run": nil,
	}
	if len(ms) == 0 {
		body["note"] = "Chưa có máy"
	}
	if pn.Coverage != nil {
		body["coverage"] = toCoverageItemDTO(*pn.Coverage)
		body["run"] = toRunDTO(*pn.Run)
	}
	c.JSON(http.StatusOK, body)
}

// ---------------------------------------------------------------------------
// Diện tích vụ
// ---------------------------------------------------------------------------

type areaReq struct {
	CooperativeRef string           `json:"cooperative_ref"`
	SeasonCode     string           `json:"season_code"`
	SeasonYear     int              `json:"season_year"`
	AreaHa         *decimal.Decimal `json:"area_ha"`
	Calendar       []calendarDTO    `json:"calendar"`
	Lock           bool             `json:"lock"`
	Source         string           `json:"source"` // chỉ nhận "manual"; App HTX đi qua port.AreaFeed
}

func (h Handler) recordArea(c *gin.Context) {
	var req areaReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	if req.Source != "" && req.Source != domain.AreaManual {
		fe.add("source", "nhập tay chỉ nhận source = manual; dữ liệu App HTX đến qua đồng bộ (FN-07)")
	}
	in := app.AreaInput{
		CooperativeRef: fe.ref("cooperative_ref", req.CooperativeRef), Season: domain.Season{Code: req.SeasonCode, Year: req.SeasonYear},
		AreaHa: req.AreaHa, Lock: req.Lock, IdemKey: idem(c),
	}
	for i, e := range req.Calendar {
		start := fe.date("calendar["+itoa(i)+"].start", e.Start)
		end := fe.date("calendar["+itoa(i)+"].end", e.End)
		if start != nil && end != nil {
			in.Calendar = append(in.Calendar, domain.CalendarEntry{Stage: e.Stage, Start: *start, End: *end})
		}
	}
	if fe.fail(c) {
		return
	}
	out, err := h.Svc.RecordArea(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(created(out.Replayed), gin.H{"area": toAreaDTO(out.Area), "event": toAreaEventDTO(out.Event)})
}

type lockReq struct {
	Reason string `json:"reason"`
}

func (h Handler) lock(c *gin.Context, lock bool) {
	id, ok := pathID(c, "id", app.ErrAreaNotFound)
	if !ok {
		return
	}
	var req lockReq
	if c.Request.ContentLength != 0 && !bindJSON(c, &req) {
		return
	}
	fn := h.Svc.UnlockArea
	if lock {
		fn = h.Svc.LockArea
	}
	out, err := fn(c.Request.Context(), app.LockInput{ID: id, Reason: req.Reason, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"area": toAreaDTO(out.Area), "event": toAreaEventDTO(out.Event)})
}

func (h Handler) lockArea(c *gin.Context)   { h.lock(c, true) }
func (h Handler) unlockArea(c *gin.Context) { h.lock(c, false) }

func (h Handler) listAreas(c *gin.Context) {
	var fe fieldErrs
	q := app.AreaQuery{Season: fe.season(c, true), CooperativeRef: fe.ref("cooperative_ref", c.Query("cooperative_ref"))}
	if fe.fail(c) {
		return
	}
	list, err := h.Svc.ListAreas(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]areaDTO, 0, len(list))
	for _, a := range list {
		items = append(items, toAreaDTO(a))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) areaEvents(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrAreaNotFound)
	if !ok {
		return
	}
	evs, err := h.Svc.AreaHistory(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]areaEventDTO, 0, len(evs))
	for _, e := range evs {
		items = append(items, toAreaEventDTO(e))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ---------------------------------------------------------------------------
// Cân đối, báo cáo, dashboard
// ---------------------------------------------------------------------------

type balanceReq struct {
	SeasonCode string `json:"season_code"`
	SeasonYear int    `json:"season_year"`
}

func (h Handler) computeBalance(c *gin.Context) {
	var req balanceReq
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.Svc.ComputeBalance(c.Request.Context(), app.BalanceRequest{
		Season: domain.Season{Code: req.SeasonCode, Year: req.SeasonYear}, IdemKey: idem(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1"+base+"/balance-runs/"+out.Run.ID.String())
	c.JSON(created(out.Replayed), gin.H{"run": toRunDTO(out.Run)})
}

func (h Handler) listRuns(c *gin.Context) {
	var fe fieldErrs
	s := fe.season(c, false)
	if fe.fail(c) {
		return
	}
	runs, err := h.Svc.ListRuns(c.Request.Context(), s.Code, s.Year)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]runDTO, 0, len(runs))
	for _, r := range runs {
		items = append(items, toRunDTO(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) getRun(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrRunNotFound)
	if !ok {
		return
	}
	var fe fieldErrs
	f := store.ResultFilter{
		ProvinceCode: c.Query("province_code"), CommuneCode: c.Query("commune_code"),
		CooperativeRef: fe.ref("cooperative_ref", c.Query("cooperative_ref")), FarmStage: c.Query("farm_stage"), MachineType: c.Query("machine_type"),
	}
	if fe.fail(c) {
		return
	}
	rv, err := h.Svc.GetRun(c.Request.Context(), app.RunQuery{RunID: id}, f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]resultDTO, 0, len(rv.Results))
	for _, r := range rv.Results {
		items = append(items, toResultDTO(r))
	}
	c.JSON(http.StatusOK, gin.H{"run": toRunDTO(rv.Run), "results": items})
}

func (h Handler) coverage(c *gin.Context) {
	var fe fieldErrs
	q := app.CoverageQuery{
		RunQuery: fe.runQuery(c), Level: c.Query("level"), ProvinceCode: c.Query("province_code"), CommuneCode: c.Query("commune_code"),
		FarmStage: c.Query("farm_stage"), MachineType: c.Query("machine_type"),
	}
	if fe.fail(c) {
		return
	}
	cv, err := h.Svc.Coverage(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]coverageItemDTO, 0, len(cv.Items))
	for _, it := range cv.Items {
		items = append(items, toCoverageItemDTO(it))
	}
	c.JSON(http.StatusOK, gin.H{
		"run": toRunDTO(cv.Run), "level": cv.Level, "summary": toGroupDTO(cv.Summary), "stages": toStagesDTO(cv.Stages), "items": items,
		"legend": legend(cv.Run.Thresholds),
	})
}

type legendDTO struct {
	Status string `json:"status"`
	Label  string `json:"label"`
	Color  string `json:"color"`
}

// legend là chú giải màu theo ngưỡng của lần cân đối (FN-08 BR-01).
func legend(t domain.Thresholds) []legendDTO {
	out := make([]legendDTO, 0, len(domain.AllCoverages))
	for _, s := range domain.AllCoverages {
		label := domain.CoverageLabels[s]
		if t.Complete() {
			switch s {
			case domain.CoverageShortage:
				label += " (<" + t.Attention.Value.String() + "%)"
			case domain.CoverageAttention:
				label += " (" + t.Attention.Value.String() + "–<" + t.Sufficient.Value.String() + "%)"
			case domain.CoverageSufficient:
				label += " (≥" + t.Sufficient.Value.String() + "%)"
			case domain.CoverageSurplus:
				label += " (≥" + t.Surplus.Value.String() + "%)"
			}
		}
		out = append(out, legendDTO{Status: string(s), Label: label, Color: domain.CoverageColors[s]})
	}
	return out
}

func (h Handler) dashboard(c *gin.Context) {
	var fe fieldErrs
	q := fe.runQuery(c)
	if fe.fail(c) {
		return
	}
	d, err := h.Svc.GetDashboard(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	pr := make([]coverageItemDTO, 0, len(d.Priority))
	for _, it := range d.Priority {
		pr = append(pr, toCoverageItemDTO(it))
	}
	body := gin.H{
		"kpis": gin.H{
			"total_machines": d.TotalMachines, "total_cooperatives": d.TotalCoops,
			"cooperatives_sufficient": d.CoopsSufficient, "cooperatives_shortage": d.CoopsShortage,
		},
		"cooperatives_by_status": d.CoopsByStatus, "priority": pr, "run": nil,
	}
	if d.Run != nil {
		body["run"] = toRunDTO(*d.Run)
	}
	c.JSON(http.StatusOK, body)
}

func (h Handler) trend(c *gin.Context) {
	pts, err := h.Svc.Trend(c.Request.Context(), c.Query("season_code"), c.Query("farm_stage"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(pts))
	for _, p := range pts {
		items = append(items, gin.H{
			"run": toRunDTO(p.Run), "summary": toGroupDTO(p.Summary), "stages": toStagesDTO(p.Stages), "cooperatives_by_status": p.CoopsByStatus,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func itoa(i int) string { return strconv.Itoa(i) }
