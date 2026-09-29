package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// provisionalLabel là nhãn bắt buộc trên mọi số ước tính chưa chính thức.
const provisionalLabel = "Tạm — chờ khách ký phương pháp"

// registerG3App gắn route Application API G3.
func (h Handler) registerG3App(rg *gin.RouterGroup) {
	rg.GET("/mrv/completeness", h.completenessList)
	rg.GET("/mrv/seasons/:id/completeness", h.seasonCompleteness)
	rg.GET("/mrv/seasons/:id/comparison", h.seasonComparison)
	rg.POST("/mrv/comparisons", h.recordComparison)
	rg.GET("/mrv/estimates", h.estimates)
	rg.POST("/mrv/seasons/:id/estimate/recompute", h.recompute)
}

// registerG3Admin gắn route Admin API G3 (phương pháp có phê duyệt).
func (h Handler) registerG3Admin(rg *gin.RouterGroup) {
	rg.POST("/mrv/methods", h.proposeMethod)
	rg.GET("/mrv/methods", h.methods)
	rg.POST("/mrv/methods/:id/approve", func(c *gin.Context) { h.decideMethod(c, true) })
	rg.POST("/mrv/methods/:id/reject", func(c *gin.Context) { h.decideMethod(c, false) })
}

func optUUID(c *gin.Context, name string) (*uuid.UUID, bool) {
	s := c.Query(name)
	if s == "" {
		return nil, true
	}
	id, err := uuid.Parse(s)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, "phải là UUID"))
		return nil, false
	}
	return &id, true
}

func strp(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

// ---------------------------------------------------------------------------
// DTO

type seasonDTO struct {
	SeasonRef   string  `json:"season_ref"`
	PlotRef     string  `json:"plot_ref"`
	CommuneCode string  `json:"commune_code"`
	SeasonCode  string  `json:"season_code"`
	SeasonYear  int     `json:"season_year"`
	Status      string  `json:"status"`
	SowingDate  *string `json:"sowing_date"`
}

func toSeasonDTO(s store.SeasonRow) seasonDTO {
	d := seasonDTO{SeasonRef: s.SeasonRef.String(), PlotRef: s.PlotRef.String(), CommuneCode: s.CommuneCode,
		SeasonCode: s.SeasonCode, SeasonYear: s.SeasonYear, Status: s.Status}
	if s.SowingDate != nil {
		v := s.SowingDate.Format("2006-01-02")
		d.SowingDate = &v
	}
	return d
}

type estimateDTO struct {
	ID                   string                 `json:"id"`
	SeasonRef            string                 `json:"season_ref"`
	PlotRef              string                 `json:"plot_ref"`
	CommuneCode          string                 `json:"commune_code"`
	SeasonCode           string                 `json:"season_code"`
	RuleVersion          string                 `json:"rule_version"`
	InputSHA256          string                 `json:"input_sha256"`
	CountedTonnes        string                 `json:"counted_tonnes"`
	AvoidedBurningKgCO2e string                 `json:"avoided_burning_kgco2e"`
	TransportKgCO2e      *string                `json:"transport_kgco2e"`
	TransportStatus      string                 `json:"transport_status"`
	Inputs               []domain.EstimateInput `json:"inputs"`
	Params               []domain.ParamUse      `json:"params"`
	Status               string                 `json:"status"` // provisional | official
	Official             bool                   `json:"official"`
	Label                string                 `json:"label"`
	MethodVersionID      *string                `json:"method_version_id"`
	ComputedAt           string                 `json:"computed_at"`
	RecordedAt           string                 `json:"recorded_at"`
	SupersedesID         *string                `json:"supersedes_id"`
	Seq                  int64                  `json:"seq"`
}

func toEstimateDTO(e store.EstimateRow) estimateDTO {
	d := estimateDTO{
		ID: e.ID.String(), SeasonRef: e.SeasonRef.String(), PlotRef: e.PlotRef.String(), CommuneCode: e.CommuneCode,
		SeasonCode: e.SeasonCode, RuleVersion: e.RuleVersion, InputSHA256: e.InputSHA256, CountedTonnes: e.CountedTonnes.StringFixed(3),
		AvoidedBurningKgCO2e: e.AvoidedKg.StringFixed(3), TransportStatus: e.TransportStatus, Inputs: e.Inputs, Params: e.Params,
		Status: "provisional", Official: e.Official, Label: provisionalLabel, MethodVersionID: strp(e.MethodVersionID),
		ComputedAt: ts(e.EventTime), RecordedAt: ts(e.RecordedAt), SupersedesID: strp(e.SupersedesID), Seq: e.Seq,
	}
	if e.Official {
		d.Status, d.Label = "official", "Chính thức — phương pháp và tham số đã duyệt"
	}
	if e.TransportKg != nil {
		t := e.TransportKg.StringFixed(3)
		d.TransportKgCO2e = &t
	}
	if d.Inputs == nil {
		d.Inputs = []domain.EstimateInput{}
	}
	return d
}

type itemDTO struct {
	Key            string `json:"key"`
	Source         string `json:"source"`
	CatalogVersion *int   `json:"catalog_version"`
	Status         string `json:"status"`
	Count          int    `json:"count"`
	ApprovedCount  int    `json:"approved_count"`
}

type anomalyDTO struct {
	NoRecord      bool    `json:"no_record"`
	LastRecordAt  *string `json:"last_record_at"`
	DaysSince     int     `json:"days_since"`
	ThresholdDays int     `json:"threshold_days"`
}

type completenessDTO struct {
	Season           seasonDTO    `json:"season"`
	Items            []itemDTO    `json:"items"`
	CountsToEstimate bool         `json:"counts_toward_estimate"`
	Anomaly          anomalyDTO   `json:"anomaly"`
	Estimate         *estimateDTO `json:"estimate"`
}

func toCompletenessDTO(c app.Completeness) completenessDTO {
	d := completenessDTO{Season: toSeasonDTO(c.Season), Items: make([]itemDTO, 0, len(c.Items)), CountsToEstimate: c.CountsToEstimate,
		Anomaly: anomalyDTO{NoRecord: c.Anomaly.NoRecord, LastRecordAt: tsPtr(c.Anomaly.LastRecordAt), DaysSince: c.Anomaly.DaysSince,
			ThresholdDays: c.Anomaly.ThresholdDays}}
	for _, it := range c.Items {
		d.Items = append(d.Items, itemDTO(it))
	}
	if c.Estimate != nil {
		e := toEstimateDTO(*c.Estimate)
		d.Estimate = &e
	}
	return d
}

type comparisonDTO struct {
	ID               string           `json:"id"`
	EvidenceID       string           `json:"evidence_id"`
	LogRef           string           `json:"log_ref"`
	LogID            string           `json:"log_id"`
	SeasonRef        string           `json:"season_ref"`
	Result           string           `json:"result"`
	Note             *string          `json:"note"`
	FileRefs         []domain.FileRef `json:"file_refs"`
	ComparedBy       string           `json:"compared_by"`
	EventTime        string           `json:"event_time"`
	RecordedAt       string           `json:"recorded_at"`
	SupersedesID     *string          `json:"supersedes_id"`
	CorrectionReason *string          `json:"correction_reason"`
}

func toComparisonDTO(c store.ComparisonRow) comparisonDTO {
	files := c.FileRefs
	if files == nil {
		files = []domain.FileRef{}
	}
	return comparisonDTO{ID: c.ID.String(), EvidenceID: c.EvidenceID.String(), LogRef: c.LogRef.String(), LogID: c.LogID.String(),
		SeasonRef: c.SeasonRef.String(), Result: c.Result, Note: c.Note, FileRefs: files, ComparedBy: c.ActorRef.String(),
		EventTime: ts(c.EventTime), RecordedAt: ts(c.RecordedAt), SupersedesID: strp(c.SupersedesID), CorrectionReason: c.CorrectionReason}
}

type officerDTO struct {
	EvidenceID   string `json:"evidence_id"`
	RootID       string `json:"root_id"`
	EvidenceType string `json:"evidence_type"`
	ActivityCode string `json:"activity_code"`
	EventTime    string `json:"event_time"`
	ReviewStatus string `json:"review_status"`
}

func toOfficerDTO(o domain.OfficerRecord) officerDTO {
	return officerDTO{EvidenceID: o.EvidenceID.String(), RootID: o.RootID.String(), EvidenceType: o.EvidenceType,
		ActivityCode: o.ActivityCode, EventTime: ts(o.EventTime), ReviewStatus: string(o.ReviewStatus)}
}

// ---------------------------------------------------------------------------

func (h Handler) completenessList(c *gin.Context) {
	after, limit, ok := cursor(c)
	if !ok {
		return
	}
	plot, ok := optUUID(c, "plot_ref")
	if !ok {
		return
	}
	pg, err := h.Svc.CompletenessList(c.Request.Context(), app.CompletenessFilter{
		AdminUnitCode: c.Query("admin_unit_code"), SeasonCode: c.Query("season_code"), PlotRef: plot,
		AnomalyOnly: c.Query("anomaly") == "true", After: after, Limit: limit,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]completenessDTO, 0, len(pg.Items))
	for _, it := range pg.Items {
		items = append(items, toCompletenessDTO(it))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(pg.Next)})
}

func (h Handler) seasonCompleteness(c *gin.Context) {
	id, ok := pathID(c, app.ErrSeasonNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.SeasonCompleteness(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toCompletenessDTO(v))
}

func (h Handler) seasonComparison(c *gin.Context) {
	id, ok := pathID(c, app.ErrSeasonNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.SeasonComparison(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	officer := make([]officerDTO, 0, len(v.Officer))
	for _, o := range v.Officer {
		officer = append(officer, toOfficerDTO(o))
	}
	lines := make([]gin.H, 0, len(v.Lines))
	for _, l := range v.Lines {
		rec := make([]comparisonDTO, 0, len(l.Recorded))
		for _, r := range l.Recorded {
			rec = append(rec, toComparisonDTO(r))
		}
		var sug *officerDTO
		if l.Officer != nil {
			o := toOfficerDTO(*l.Officer)
			sug = &o
		}
		lines = append(lines, gin.H{
			"log_ref": l.Log.LogRef.String(), "log_id": l.Log.LogID.String(), "log_version": l.Log.LogVersion,
			"activity_code": l.Log.ActivityCode, "log_event_time": ts(l.Log.LogEventTime), "location_status": l.Log.LocationStatus,
			"htx_review_decision": l.Log.Decision, "catalog_version": l.Log.CatalogVersion, "suggested": l.Suggested,
			"suggested_officer_record": sug, "result": l.FinalState, "confirmed": l.Confirmed, "comparisons": rec,
		})
	}
	c.JSON(http.StatusOK, gin.H{"season": toSeasonDTO(v.Season), "window_days": v.WindowDays, "lines": lines, "officer_records": officer})
}

type compareReq struct {
	SeasonRef        string            `json:"season_ref"`
	LogRef           string            `json:"log_ref"`
	EvidenceID       string            `json:"evidence_id"`
	Result           string            `json:"result"`
	Note             *string           `json:"note"`
	FileRefs         []app.FileRefWire `json:"file_refs"`
	SupersedesID     *string           `json:"supersedes_id"`
	CorrectionReason *string           `json:"correction_reason"`
}

func (h Handler) recordComparison(c *gin.Context) {
	var req compareReq
	if !bindJSON(c, &req) {
		return
	}
	var errs []problem.FieldError
	parse := func(field, s string) uuid.UUID {
		id, err := ids.ParseRef(s)
		if err != nil {
			errs = append(errs, problem.FieldError{Field: field, Reason: "phải là UUID"})
		}
		return id
	}
	in := app.CompareInput{SeasonRef: parse("season_ref", req.SeasonRef), LogRef: parse("log_ref", req.LogRef),
		EvidenceID: parse("evidence_id", req.EvidenceID), Result: req.Result, Note: req.Note, CorrectionReason: req.CorrectionReason,
		IdemKey: idem(c)}
	if req.SupersedesID != nil {
		id := parse("supersedes_id", *req.SupersedesID)
		in.SupersedesID = &id
	}
	for _, f := range req.FileRefs {
		in.FileRefs = append(in.FileRefs, domain.FileRef{FileRef: parse("file_refs", f.FileRef), SHA256: strings.ToLower(strings.TrimSpace(f.SHA256))})
	}
	if len(errs) > 0 {
		p := problem.Validation
		for _, e := range errs {
			p = p.WithField(e.Field, e.Reason)
		}
		httpx.Fail(c, p)
		return
	}
	r, replayed, err := h.Svc.RecordComparison(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(created(replayed), toComparisonDTO(r))
}

func (h Handler) estimates(c *gin.Context) {
	after, limit, ok := cursor(c)
	if !ok {
		return
	}
	season, ok := optUUID(c, "season_ref")
	if !ok {
		return
	}
	pg, err := h.Svc.Estimates(c.Request.Context(), app.EstimateFilter{SeasonRef: season, AdminUnitCode: c.Query("admin_unit_code"),
		After: after, Limit: limit})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]estimateDTO, 0, len(pg.Items))
	for _, e := range pg.Items {
		items = append(items, toEstimateDTO(e))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(pg.Next)})
}

func (h Handler) recompute(c *gin.Context) {
	id, ok := pathID(c, app.ErrSeasonNotFound)
	if !ok {
		return
	}
	e, err := h.Svc.RecomputeEstimate(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if e == nil {
		c.JSON(http.StatusOK, gin.H{"estimate": nil})
		return
	}
	d := toEstimateDTO(*e)
	c.JSON(http.StatusOK, gin.H{"estimate": d})
}

// ---------------------------------------------------------------------------
// Admin: phương pháp

type methodDTO struct {
	ID           string            `json:"id"`
	RuleVersion  string            `json:"rule_version"`
	Version      int               `json:"version"`
	Status       string            `json:"status"`
	Params       []domain.ParamPin `json:"params"`
	SourceNote   string            `json:"source_note"`
	SetterRef    string            `json:"setter_ref"`
	ApproverRef  *string           `json:"approver_ref"`
	DecidedAt    *string           `json:"decided_at"`
	DecisionNote *string           `json:"decision_note"`
	CreatedAt    string            `json:"created_at"`
}

func toMethodDTO(m store.MethodRow) methodDTO {
	params := m.Params
	if params == nil {
		params = []domain.ParamPin{}
	}
	return methodDTO{ID: m.ID.String(), RuleVersion: m.RuleVersion, Version: m.Version, Status: m.Status, Params: params,
		SourceNote: m.SourceNote, SetterRef: m.SetterRef.String(), ApproverRef: strp(m.ApproverRef), DecidedAt: tsPtr(m.DecidedAt),
		DecisionNote: m.DecisionNote, CreatedAt: ts(m.CreatedAt)}
}

func (h Handler) proposeMethod(c *gin.Context) {
	var in app.MethodInput
	if !bindJSON(c, &in) {
		return
	}
	in.IdemKey = idem(c)
	m, replayed, err := h.Svc.ProposeMethod(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(created(replayed), toMethodDTO(m))
}

func (h Handler) methods(c *gin.Context) {
	after, limit, ok := cursor(c)
	if !ok {
		return
	}
	pg, err := h.Svc.Methods(c.Request.Context(), after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]methodDTO, 0, len(pg.Items))
	for _, m := range pg.Items {
		items = append(items, toMethodDTO(m))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(pg.Next)})
}

func (h Handler) decideMethod(c *gin.Context, approve bool) {
	id, ok := pathID(c, app.ErrMethodNotFound)
	if !ok {
		return
	}
	var req decisionReq
	if c.Request.ContentLength != 0 && !bindJSON(c, &req) {
		return
	}
	m, err := h.Svc.DecideMethod(c.Request.Context(), id, approve, req.Note)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toMethodDTO(m))
}
