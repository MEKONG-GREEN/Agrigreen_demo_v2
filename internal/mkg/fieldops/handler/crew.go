package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// registerCrews gắn route sổ tổ gom, phân công có năng lực, hạn cuộn, tham số (nghiệm thu G3).
func (h Handler) registerCrews(rg *gin.RouterGroup) {
	rg.GET("/fieldops/crews", h.listCrews)
	rg.POST("/fieldops/crews", h.createCrew)
	rg.GET("/fieldops/crews/:id", h.getCrew)
	rg.POST("/fieldops/crews/:id/revisions", h.reviseCrew)
	rg.POST("/fieldops/crews/:id/deactivate", h.crewStatus(false))
	rg.POST("/fieldops/crews/:id/reactivate", h.crewStatus(true))
	rg.GET("/fieldops/orders/:id/crew-suggestions", h.suggest)
	rg.GET("/fieldops/orders/:id/assignments", h.assignments)
	rg.GET("/fieldops/baling-deadlines", h.balingDeadlines)
	rg.POST("/fieldops/harvest-dates/refresh", h.refreshHarvest)
	rg.GET("/fieldops/params/:key", h.params)
	rg.POST("/fieldops/params/:key/versions", h.proposeParam)
	rg.POST("/fieldops/params/:key/versions/:version/decision", h.decideParam)
}

// ---------------------------------------------------------------------------
// Tổ gom
// ---------------------------------------------------------------------------

type crewReq struct {
	Code            string   `json:"code"`
	Name            string   `json:"name"`
	MemberRefs      []string `json:"member_refs"`
	CapacityUnit    string   `json:"capacity_unit"`
	CapacityPerDay  string   `json:"capacity_per_day"`
	ServiceCommunes []string `json:"service_communes"`
	ExpectedVersion *int     `json:"expected_version"`
	Reason          string   `json:"reason"`
}

func (h Handler) crewInput(c *gin.Context, full bool) (app.CrewInput, bool) {
	var req crewReq
	if !bind(c, &req) {
		return app.CrewInput{}, false
	}
	x := newV()
	in := app.CrewInput{ExpectedVersion: req.ExpectedVersion, Reason: req.Reason, IdemKey: idem(c)}
	if full {
		in.Spec = domain.CrewSpec{Code: req.Code, Name: req.Name, CapacityUnit: req.CapacityUnit, ServiceCommunes: req.ServiceCommunes}
		for _, m := range req.MemberRefs {
			in.Spec.MemberRefs = append(in.Spec.MemberRefs, x.uuid("member_refs", m))
		}
		d, err := decimal.NewFromString(req.CapacityPerDay)
		if err != nil {
			x.add("capacity_per_day", "chuỗi thập phân > 0, ví dụ \"1200\" hoặc \"4.5\"")
		}
		in.Spec.CapacityPerDay = d
	}
	return in, !x.fail(c)
}

func (h Handler) createCrew(c *gin.Context) {
	in, ok := h.crewInput(c, true)
	if !ok {
		return
	}
	r, replayed, err := h.Svc.CreateCrew(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "/v1/fieldops/crews/"+r.Crew.CrewID.String(), toCrewResult(r))
}

func (h Handler) reviseCrew(c *gin.Context) {
	id, ok := pathID(c, app.ErrCrewNotFound)
	if !ok {
		return
	}
	in, ok := h.crewInput(c, true)
	if !ok {
		return
	}
	r, replayed, err := h.Svc.ReviseCrew(c.Request.Context(), id, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "", toCrewResult(r))
}

func (h Handler) crewStatus(active bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c, app.ErrCrewNotFound)
		if !ok {
			return
		}
		in, ok := h.crewInput(c, false)
		if !ok {
			return
		}
		r, replayed, err := h.Svc.SetCrewStatus(c.Request.Context(), id, active, in)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		created(c, replayed, "", toCrewResult(r))
	}
}

func (h Handler) getCrew(c *gin.Context) {
	id, ok := pathID(c, app.ErrCrewNotFound)
	if !ok {
		return
	}
	cr, hist, err := h.Svc.Crew(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	evs := make([]crewEventDTO, 0, len(hist))
	for _, e := range hist {
		evs = append(evs, toCrewEvent(e))
	}
	c.JSON(http.StatusOK, gin.H{"crew": toCrew(cr), "history": evs})
}

func (h Handler) listCrews(c *gin.Context) {
	after := ""
	if s := c.Query("cursor"); s != "" {
		after = s
	}
	pg, err := h.Svc.Crews(c.Request.Context(), c.Query("status"), after, limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]crewDTO, 0, len(pg.Items))
	for _, x := range pg.Items {
		items = append(items, toCrew(x))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": pg.Next})
}

type crewDTO struct {
	CrewID          uuid.UUID   `json:"crew_id"`
	Code            string      `json:"code"`
	Name            string      `json:"name"`
	MemberRefs      []uuid.UUID `json:"member_refs"`
	CapacityUnit    string      `json:"capacity_unit"`
	CapacityPerDay  string      `json:"capacity_per_day"`
	ServiceCommunes []string    `json:"service_communes"`
	Status          string      `json:"status"`
	Version         int         `json:"version"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

func toCrew(c store.Crew) crewDTO {
	communes := c.Spec.ServiceCommunes
	if communes == nil {
		communes = []string{}
	}
	return crewDTO{CrewID: c.CrewID, Code: c.Spec.Code, Name: c.Spec.Name, MemberRefs: c.Spec.MemberRefs, CapacityUnit: c.Spec.CapacityUnit,
		CapacityPerDay: c.Spec.CapacityPerDay.String(), ServiceCommunes: communes, Status: c.Status, Version: c.Version,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

type crewEventDTO struct {
	ID         uuid.UUID `json:"id"`
	Seq        int64     `json:"seq"`
	Kind       string    `json:"kind"`
	Version    int       `json:"version"`
	Crew       crewDTO   `json:"snapshot"`
	Reason     *string   `json:"reason"`
	ActorRef   uuid.UUID `json:"actor_ref"`
	EventTime  time.Time `json:"event_time"`
	RecordedAt time.Time `json:"recorded_at"`
}

func toCrewEvent(e store.CrewEvent) crewEventDTO {
	snap := toCrew(store.Crew{CrewID: e.CrewID, Spec: e.Spec, Status: e.Status, Version: e.Version, CreatedAt: e.EventTime, UpdatedAt: e.EventTime})
	return crewEventDTO{ID: e.ID, Seq: e.Seq, Kind: e.Kind, Version: e.Version, Crew: snap, Reason: e.Reason, ActorRef: e.ActorRef,
		EventTime: e.EventTime, RecordedAt: e.RecordedAt}
}

func toCrewResult(r app.CrewResult) map[string]any {
	return map[string]any{"crew": toCrew(r.Crew), "event": toCrewEvent(r.Event)}
}

// ---------------------------------------------------------------------------
// Phân công, gợi ý
// ---------------------------------------------------------------------------

type assignReq struct {
	CrewRef        string  `json:"crew_ref"`
	PlannedFrom    string  `json:"planned_from"`
	PlannedTo      string  `json:"planned_to"`
	ExpectedBales  *int    `json:"expected_bales"`
	ExpectedHa     *string `json:"expected_ha"`
	Mode           string  `json:"mode"`
	SuggestionRank *int    `json:"suggestion_rank"`
	OverrideReason string  `json:"override_reason"`
}

func (x *v) decPtr(field string, s *string) *decimal.Decimal {
	if s == nil || *s == "" {
		return nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		x.add(field, "chuỗi thập phân, ví dụ \"4.5\"")
		return nil
	}
	return &d
}

func (h Handler) assign(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	var req assignReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.AssignInput{Mode: req.Mode, SuggestionRank: req.SuggestionRank, OverrideReason: req.OverrideReason, IdemKey: idem(c),
		Workload: app.WorkloadInput{PlannedFrom: req.PlannedFrom, PlannedTo: req.PlannedTo, ExpectedBales: req.ExpectedBales}}
	in.CrewRef = x.uuid("crew_ref", req.CrewRef)
	in.Workload.ExpectedHa = x.decPtr("expected_ha", req.ExpectedHa)
	if x.fail(c) {
		return
	}
	r, replayed, err := h.Svc.Assign(c.Request.Context(), id, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	body := map[string]any{"order": toOrder(r.Order), "publications": toExports(r.Exports), "assignment": toAssignment(r.Assignment),
		"warnings": nonNil(r.Warnings)}
	if r.Event.ID != uuid.Nil {
		body["event"] = toOrderEvent(r.Event)
	} else {
		body["event"] = nil
	}
	created(c, replayed, "", body)
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

type assignmentDTO struct {
	ID             uuid.UUID `json:"id"`
	OrderID        uuid.UUID `json:"order_id"`
	OrderEventID   uuid.UUID `json:"order_event_id"`
	CrewRef        uuid.UUID `json:"crew_ref"`
	CrewVersion    int       `json:"crew_version"`
	CapacityUnit   string    `json:"capacity_unit"`
	CapacityPerDay string    `json:"capacity_per_day"`
	PlannedFrom    string    `json:"planned_from"`
	PlannedTo      string    `json:"planned_to"`
	ExpectedQty    string    `json:"expected_qty"`
	DailyLoad      string    `json:"daily_load"`
	PeakLoad       string    `json:"peak_load"`
	OverCapacity   bool      `json:"over_capacity"`
	OverrideReason *string   `json:"override_reason"`
	Mode           string    `json:"mode"`
	SuggestionRank *int      `json:"suggestion_rank"`
	RuleVersion    string    `json:"rule_version"`
	ActorRef       uuid.UUID `json:"actor_ref"`
	EventTime      time.Time `json:"event_time"`
}

func toAssignment(a store.Assignment) *assignmentDTO {
	if a.ID == uuid.Nil {
		return nil
	}
	return &assignmentDTO{ID: a.ID, OrderID: a.OrderID, OrderEventID: a.OrderEventID, CrewRef: a.CrewID, CrewVersion: a.CrewVersion,
		CapacityUnit: a.CapacityUnit, CapacityPerDay: a.CapacityPerDay.String(), PlannedFrom: a.PlannedFrom.Format(dateLayout),
		PlannedTo: a.PlannedTo.Format(dateLayout), ExpectedQty: a.ExpectedQty.String(), DailyLoad: a.DailyLoad.String(),
		PeakLoad: a.PeakLoad.String(), OverCapacity: a.OverCapacity, OverrideReason: a.OverrideReason, Mode: a.Mode,
		SuggestionRank: a.SuggestionRank, RuleVersion: a.RuleVersion, ActorRef: a.ActorRef, EventTime: a.EventTime}
}

func (h Handler) assignments(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	xs, err := h.Svc.Assignments(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]*assignmentDTO, 0, len(xs))
	for _, a := range xs {
		items = append(items, toAssignment(a))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type suggestionDTO struct {
	Rank           *int      `json:"rank"`
	CrewRef        uuid.UUID `json:"crew_ref"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	CapacityUnit   string    `json:"capacity_unit"`
	CapacityPerDay string    `json:"capacity_per_day"`
	Local          bool      `json:"local"`
	DailyLoad      *string   `json:"daily_load"`
	PeakLoad       *string   `json:"peak_load"`
	LoadRatio      *string   `json:"load_ratio"`
	OverDay        *string   `json:"over_day"`
	Unfit          *string   `json:"unfit"`
}

func toSuggestion(s app.SuggestView) suggestionDTO {
	d := suggestionDTO{CrewRef: s.CrewID, Code: s.Code, Name: s.Crew.Spec.Name, CapacityUnit: s.Crew.Spec.CapacityUnit,
		CapacityPerDay: s.Crew.Spec.CapacityPerDay.String(), Local: s.Local}
	if s.Rank > 0 {
		r := s.Rank
		d.Rank = &r
	}
	if s.Unfit != "" {
		u := s.Unfit
		d.Unfit = &u
	}
	if s.Workload != nil {
		dl := s.Workload.DailyLoad().String()
		d.DailyLoad = &dl
	}
	if s.Unfit != domain.UnfitUnit {
		pk, lr := s.Capacity.PeakLoad.String(), s.LoadRatio.String()
		d.PeakLoad, d.LoadRatio = &pk, &lr
	}
	if s.Capacity.OverDay != nil {
		od := s.Capacity.OverDay.Format(dateLayout)
		d.OverDay = &od
	}
	return d
}

func (h Handler) suggest(c *gin.Context) {
	id, ok := pathID(c, app.ErrOrderNotFound)
	if !ok {
		return
	}
	x := newV()
	in := app.WorkloadInput{PlannedFrom: c.Query("planned_from"), PlannedTo: c.Query("planned_to")}
	if s := c.Query("expected_bales"); s != "" {
		d := x.decPtr("expected_bales", &s)
		if d != nil {
			if !d.Equal(d.Truncate(0)) || !d.IsPositive() {
				x.add("expected_bales", "số nguyên > 0")
			} else {
				n := int(d.IntPart())
				in.ExpectedBales = &n
			}
		}
	}
	if s := c.Query("expected_ha"); s != "" {
		in.ExpectedHa = x.decPtr("expected_ha", &s)
	}
	if x.fail(c) {
		return
	}
	r, err := h.Svc.Suggest(c.Request.Context(), id, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	fit := make([]suggestionDTO, 0, len(r.Suggestions))
	for _, s := range r.Suggestions {
		fit = append(fit, toSuggestion(s))
	}
	unfit := make([]suggestionDTO, 0, len(r.Unfit))
	for _, s := range r.Unfit {
		unfit = append(unfit, toSuggestion(s))
	}
	var top any
	if len(fit) > 0 {
		top = fit[0]
	}
	c.JSON(http.StatusOK, gin.H{"order_id": r.Order.OrderID, "commune_code": r.Order.CommuneCode, "planned_from": r.From.Format(dateLayout),
		"planned_to": r.To.Format(dateLayout), "suggested": top, "suggestions": fit, "unfit": unfit, "inactive_crews": r.Inactive,
		"rule_version": domain.RuleAssignment})
}

// ---------------------------------------------------------------------------
// Hạn cuộn (FM-02), ngày gặt
// ---------------------------------------------------------------------------

type balingDTO struct {
	OrderID      uuid.UUID  `json:"order_id"`
	PlotRef      uuid.UUID  `json:"plot_ref"`
	HTXRef       uuid.UUID  `json:"htx_ref"`
	CrewRef      *uuid.UUID `json:"crew_ref"`
	OrderStatus  string     `json:"order_status"`
	HarvestCheck string     `json:"harvest_check"`
	HarvestDate  *string    `json:"harvest_date"`
	HarvestOpen  *string    `json:"harvest_open_date"`
	SeasonRef    *uuid.UUID `json:"season_ref"`
	BaledOn      *string    `json:"baled_on"`
	Deadline     *string    `json:"deadline"`
	DaysOnField  *int       `json:"days_on_field"`
	Status       string     `json:"status"`
	Overdue      bool       `json:"overdue"`
	Flagged      bool       `json:"flagged"`
}

func datePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(dateLayout)
	return &s
}

func toBaling(b app.BalingItem) balingDTO {
	check := domain.HarvestUnknown
	if b.Row.HarvestCheck != nil {
		check = *b.Row.HarvestCheck
	}
	d := balingDTO{OrderID: b.Row.OrderID, PlotRef: b.Row.PlotRef, HTXRef: b.Row.HTXRef, CrewRef: b.Row.CrewRef, OrderStatus: b.Row.OrderStatus,
		HarvestCheck: check, SeasonRef: b.Row.SeasonRef, Deadline: datePtr(b.Baling.Deadline),
		DaysOnField: b.Baling.DaysOnField, Status: b.Baling.Status, Overdue: b.Baling.Status == domain.BalingOverdue, Flagged: b.Baling.Flagged()}
	if check == domain.HarvestOK {
		d.HarvestDate, d.HarvestOpen = datePtr(b.Row.HarvestDate), datePtr(b.Row.HarvestOpen)
	}
	if b.Row.BaledAt != nil {
		day := domain.Day(*b.Row.BaledAt)
		d.BaledOn = datePtr(&day)
	}
	return d
}

func toBalings(xs []app.BalingItem) []balingDTO {
	out := make([]balingDTO, 0, len(xs))
	for _, x := range xs {
		out = append(out, toBaling(x))
	}
	return out
}

var balingStatuses = map[string]bool{"": true, "flagged": true, domain.BalingHarvestUnknown: true, domain.BalingNotHarvested: true,
	domain.BalingPending: true, domain.BalingOverdue: true, domain.BalingOnTime: true, domain.BalingLate: true}

func (h Handler) balingDeadlines(c *gin.Context) {
	cur, ok := decodeCursor(c, 2)
	if !ok {
		return
	}
	x := newV()
	var order *uuid.UUID
	if s := c.Query("order_id"); s != "" {
		order = x.uuidPtr("order_id", &s)
	}
	status := c.Query("status")
	if !balingStatuses[status] {
		x.add("status", "flagged | overdue | baled_late | pending | not_harvested | baled_on_time | harvest_unknown")
	}
	if x.fail(c) {
		return
	}
	pg, err := h.Svc.BalingDeadlines(c.Request.Context(), order, status, [2]uuid.UUID{cur[0], cur[1]}, limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var next *string
	if pg.Next != nil {
		s := encodeCursor(pg.Next[0], pg.Next[1])
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": toBalings(pg.Items), "next_cursor": next, "baling_deadline_days": pg.Days,
		"param_version": pg.ParamVersion, "provisional": pg.Provisional, "today": pg.Today.Format(dateLayout),
		"rule_version": domain.RuleBalingDeadline})
}

func (h Handler) refreshHarvest(c *gin.Context) {
	r, err := h.Svc.RefreshHarvest(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, r)
}

// ---------------------------------------------------------------------------
// Tham số có phiên bản
// ---------------------------------------------------------------------------

type paramDTO struct {
	Key            string     `json:"param_key"`
	VersionID      uuid.UUID  `json:"version_id"`
	VersionSeq     int        `json:"version_seq"`
	Value          string     `json:"value"`
	EffectiveFrom  string     `json:"effective_from"`
	Provisional    bool       `json:"provisional"`
	Status         string     `json:"status"`
	ProposedBy     uuid.UUID  `json:"proposed_by"`
	ProposalReason string     `json:"proposal_reason"`
	DecidedBy      *uuid.UUID `json:"decided_by"`
	DecidedAt      *time.Time `json:"decided_at"`
	DecisionReason *string    `json:"decision_reason"`
}

func toParam(p store.Param) paramDTO {
	return paramDTO{Key: p.Key, VersionID: p.VersionID, VersionSeq: p.VersionSeq, Value: p.Value.String(),
		EffectiveFrom: p.EffectiveFrom.Format(dateLayout), Provisional: p.Provisional, Status: p.Status, ProposedBy: p.ProposedBy,
		ProposalReason: p.ProposalReason, DecidedBy: p.DecidedBy, DecidedAt: p.DecidedAt, DecisionReason: p.DecisionReason}
}

func (h Handler) params(c *gin.Context) {
	v, err := h.Svc.Params(c.Request.Context(), c.Param("key"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]paramDTO, 0, len(v.Versions))
	for _, p := range v.Versions {
		items = append(items, toParam(p))
	}
	c.JSON(http.StatusOK, gin.H{"param_key": v.Key, "effective_value": v.Effective, "param_version": v.ParamVersion,
		"provisional": v.Provisional, "default_value": v.Default, "versions": items})
}

type paramReq struct {
	Value         string `json:"value"`
	EffectiveFrom string `json:"effective_from"`
	Provisional   *bool  `json:"provisional"`
	Reason        string `json:"reason"`
	Decision      string `json:"decision"`
}

func (h Handler) proposeParam(c *gin.Context) {
	var req paramReq
	if !bind(c, &req) {
		return
	}
	val, err := decimal.NewFromString(req.Value)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("value", "chuỗi số, ví dụ \"3\""))
		return
	}
	prov := true // mặc định: tạm, chờ khách
	if req.Provisional != nil {
		prov = *req.Provisional
	}
	r, replayed, err := h.Svc.ProposeParam(c.Request.Context(), app.ParamInput{Key: c.Param("key"), Value: val,
		EffectiveFrom: req.EffectiveFrom, Provisional: prov, Reason: req.Reason, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "", gin.H{"param": toParam(r.Param)})
}

func (h Handler) decideParam(c *gin.Context) {
	ver, err := uuid.Parse(c.Param("version"))
	if err != nil {
		httpx.Fail(c, app.ErrParamNotFound)
		return
	}
	var req paramReq
	if !bind(c, &req) {
		return
	}
	if req.Decision != "approved" && req.Decision != "rejected" {
		httpx.Fail(c, problem.Validation.WithField("decision", "approved | rejected"))
		return
	}
	r, replayed, err := h.Svc.DecideParam(c.Request.Context(), c.Param("key"), ver, req.Decision == "approved", req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "", gin.H{"param": toParam(r.Param)})
}
