// Package handler: HTTP (Gin) của pf/cropcycle — Application API (App HTX, KN
// chỉ đọc) và Admin API (soạn/phát hành SOP, dựng lại bảng chiếu).
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API vào nhóm /v1 (OIDC + Idempotency-Key cho POST).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/cropcycle/sops", h.listSOPs(false))
	rg.GET("/cropcycle/sops/:ref", h.getSOP(false))
	rg.GET("/cropcycle/sop-versions/:id", h.getVersion(false))
	rg.GET("/cropcycle/variety-bindings", h.bindings)

	rg.POST("/cropcycle/seasons", h.openSeason)
	rg.POST("/cropcycle/seasons/bulk-open", h.bulkOpen)
	rg.GET("/cropcycle/seasons", h.listSeasons)
	rg.GET("/cropcycle/seasons/:ref", h.getSeason)
	rg.POST("/cropcycle/seasons/:ref/corrections", h.correctSeason(false))
	rg.POST("/cropcycle/seasons/:ref/cancel", h.correctSeason(true))
	rg.POST("/cropcycle/seasons/:ref/harvest", h.declareHarvest)
	rg.GET("/cropcycle/plots/:ref/previous-config", h.previousConfig)

	rg.POST("/cropcycle/harvests/bulk", h.bulkHarvest)
	httpx.AllowBody(rg, http.MethodPost, "/cropcycle/harvests/import", app.MaxImportBytes)
	rg.POST("/cropcycle/harvests/import", h.importHarvests)
	rg.POST("/cropcycle/harvests/:id/corrections", h.correctHarvest)

	rg.POST("/cropcycle/farm-logs", h.recordLog)
	rg.POST("/cropcycle/farm-logs/batch", h.recordBatch)
	rg.GET("/cropcycle/farm-logs", h.listLogs)
	rg.GET("/cropcycle/farm-logs/:id", h.getLog)
	rg.POST("/cropcycle/farm-logs/:id/corrections", h.correctLog)
	rg.POST("/cropcycle/farm-logs/:id/reviews", h.reviewLog)

	rg.GET("/cropcycle/dashboard", h.dashboard)
	rg.GET("/cropcycle/stage-map", h.stageMap)
}

// RegisterAdmin gắn route Admin API (MFA bắt buộc): soạn, phát hành SOP; dựng lại bảng chiếu.
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.GET("/cropcycle/sops", h.listSOPs(true))
	rg.POST("/cropcycle/sops", h.createSOP)
	rg.GET("/cropcycle/sops/:ref", h.getSOP(true))
	rg.POST("/cropcycle/sops/:ref/versions", h.newVersion)
	rg.POST("/cropcycle/sops/:ref/clone", h.cloneSOP)
	rg.POST("/cropcycle/sops/:ref/retire", h.retireSOP)
	rg.GET("/cropcycle/sop-versions/:id", h.getVersion(true))
	rg.POST("/cropcycle/sop-versions/:id/steps", h.updateDraft)
	rg.POST("/cropcycle/sop-versions/:id/publish", h.publish)
	rg.POST("/cropcycle/variety-bindings", h.bind)
	rg.POST("/cropcycle/projections/rebuild", h.rebuild)
}

// ---------------------------------------------------------------------------
// Tiện ích
// ---------------------------------------------------------------------------

func bindJSON(c *gin.Context, v any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func pathID(c *gin.Context, name string, notFound *problem.Problem) (uuid.UUID, bool) {
	u, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, notFound)
		return uuid.Nil, false
	}
	return u, true
}

// fields gom lỗi theo trường của một request.
type fields struct {
	p   *problem.Problem
	bad bool
}

func newFields() *fields { return &fields{p: problem.Validation} }

func (f *fields) add(field, reason string) { f.p, f.bad = f.p.WithField(field, reason), true }

func (f *fields) ref(field string, s *string) uuid.UUID {
	if s == nil || strings.TrimSpace(*s) == "" {
		return uuid.Nil
	}
	u, err := ids.ParseRef(strings.TrimSpace(*s))
	if err != nil {
		f.add(field, "phải là UUID")
	}
	return u
}

func (f *fields) v7(field string, s *string) uuid.UUID {
	if s == nil || *s == "" {
		return uuid.Nil
	}
	u, err := ids.Parse(*s)
	if err != nil {
		f.add(field, "phải là UUIDv7")
	}
	return u
}

func (f *fields) at(field string, s *string) time.Time {
	if s == nil || *s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		f.add(field, "phải theo RFC 3339 có múi giờ")
	}
	return t
}

func (f *fields) day(field string, s *string) *time.Time {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	d, err := domain.ParseDay(*s)
	if err != nil {
		f.add(field, "phải có dạng YYYY-MM-DD")
		return nil
	}
	return &d
}

func (f *fields) kg(field, s string) mass.Kg {
	k, err := mass.ParseKg(strings.TrimSpace(s))
	if err != nil {
		f.add(field, "khối lượng kg dạng chuỗi decimal, tối đa 3 chữ số lẻ")
	}
	return k
}

func cursor(c *gin.Context) (uuid.UUID, int, bool) {
	var after uuid.UUID
	if s := c.Query("cursor"); s != "" {
		u, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return uuid.Nil, 0, false
		}
		after = u
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	return after, limit, true
}

func qRef(c *gin.Context, f *fields, name string) uuid.UUID {
	s := c.Query(name)
	return f.ref(name, &s)
}

func qInt(c *gin.Context, f *fields, name string) int {
	s := c.Query(name)
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		f.add(name, "phải là số nguyên")
	}
	return n
}

func next(n *uuid.UUID) *string {
	if n == nil {
		return nil
	}
	s := n.String()
	return &s
}

func created(c *gin.Context, replayed bool, body any) {
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, body)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

// gpsReq là vị trí gửi lên; lon/lat là số thập phân (chuỗi hoặc số JSON).
type gpsReq struct {
	Lon       *decimal.Decimal `json:"lon"`
	Lat       *decimal.Decimal `json:"lat"`
	AccuracyM *decimal.Decimal `json:"accuracy_m"`
	Source    string           `json:"source"`
}

func (g *gpsReq) gps() domain.GPS {
	if g == nil {
		return domain.GPS{}
	}
	return domain.GPS{Lon: g.Lon, Lat: g.Lat, AccuracyM: g.AccuracyM, Source: strings.TrimSpace(g.Source)}
}

// ---------------------------------------------------------------------------
// SOP
// ---------------------------------------------------------------------------

func (h Handler) listSOPs(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		after, limit, ok := cursor(c)
		if !ok {
			return
		}
		pg, err := h.Svc.ListSOPs(c.Request.Context(), app.SOPListInput{Status: c.Query("status"), Admin: admin, After: after, Limit: limit})
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		items := make([]templateDTO, 0, len(pg.Items))
		for _, t := range pg.Items {
			items = append(items, templateOut(t))
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next(pg.Next)})
	}
}

func (h Handler) getSOP(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ref, ok := pathID(c, "ref", app.ErrSOPNotFound)
		if !ok {
			return
		}
		v, err := h.Svc.GetSOP(c.Request.Context(), ref, admin)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, sopOut(v))
	}
}

func (h Handler) getVersion(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c, "id", app.ErrSOPNotFound)
		if !ok {
			return
		}
		v, t, err := h.Svc.GetSOPVersion(c.Request.Context(), id, admin)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"version": versionOut(v), "template": templateOut(t)})
	}
}

func (h Handler) bindings(c *gin.Context) {
	bs, err := h.Svc.Bindings(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]bindingDTO, 0, len(bs))
	for _, b := range bs {
		items = append(items, bindingOut(b))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type createSOPReq struct {
	Code           string        `json:"code"`
	Name           string        `json:"name"`
	Crop           string        `json:"crop"`
	Scope          string        `json:"scope"`
	CooperativeRef *string       `json:"cooperative_ref"`
	Standard       string        `json:"standard"`
	Description    string        `json:"description"`
	Steps          []domain.Step `json:"steps"`
	ChangeNote     string        `json:"change_note"`
}

func (h Handler) createSOP(c *gin.Context) {
	var req createSOPReq
	if !bindJSON(c, &req) {
		return
	}
	f := newFields()
	in := app.CreateSOPInput{Code: req.Code, Name: req.Name, Crop: req.Crop, Scope: req.Scope, CooperativeRef: f.ref("cooperative_ref", req.CooperativeRef),
		Standard: req.Standard, Description: req.Description, Steps: req.Steps, ChangeNote: req.ChangeNote}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	v, err := h.Svc.CreateSOP(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/cropcycle/sops/"+v.Template.Ref.String())
	c.JSON(http.StatusCreated, sopOut(v))
}

type versionReq struct {
	Steps      *[]domain.Step `json:"steps"`
	ChangeNote string         `json:"change_note"`
}

func (h Handler) newVersion(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrSOPNotFound)
	if !ok {
		return
	}
	var req versionReq
	if !bindJSON(c, &req) {
		return
	}
	in := app.NewVersionInput{TemplateRef: ref, CopySteps: req.Steps == nil, ChangeNote: req.ChangeNote}
	if req.Steps != nil {
		in.Steps = *req.Steps
	}
	v, err := h.Svc.NewVersion(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, sopOut(v))
}

func (h Handler) updateDraft(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrSOPNotFound)
	if !ok {
		return
	}
	var req versionReq
	if !bindJSON(c, &req) {
		return
	}
	if req.Steps == nil {
		httpx.Fail(c, problem.Validation.WithField("steps", "bắt buộc"))
		return
	}
	v, err := h.Svc.UpdateDraft(c.Request.Context(), id, *req.Steps, req.ChangeNote)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, sopOut(v))
}

func (h Handler) publish(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrSOPNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.PublishSOP(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, sopOut(v))
}

type cloneReq struct {
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	Scope          string  `json:"scope"`
	CooperativeRef *string `json:"cooperative_ref"`
}

func (h Handler) cloneSOP(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrSOPNotFound)
	if !ok {
		return
	}
	var req cloneReq
	if !bindJSON(c, &req) {
		return
	}
	f := newFields()
	in := app.CloneInput{SourceRef: ref, Code: req.Code, Name: req.Name, Scope: req.Scope, CooperativeRef: f.ref("cooperative_ref", req.CooperativeRef)}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	v, err := h.Svc.CloneSOP(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/cropcycle/sops/"+v.Template.Ref.String())
	c.JSON(http.StatusCreated, sopOut(v))
}

type reasonReq struct {
	Reason string `json:"reason"`
}

func (h Handler) retireSOP(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrSOPNotFound)
	if !ok {
		return
	}
	var req reasonReq
	if !bindJSON(c, &req) {
		return
	}
	v, err := h.Svc.RetireSOP(c.Request.Context(), ref, req.Reason)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, sopOut(v))
}

type bindReq struct {
	VarietyCode string  `json:"variety_code"`
	TemplateRef *string `json:"template_ref"`
	Note        string  `json:"note"`
}

func (h Handler) bind(c *gin.Context) {
	var req bindReq
	if !bindJSON(c, &req) {
		return
	}
	f := newFields()
	t := f.ref("template_ref", req.TemplateRef)
	if t == uuid.Nil {
		f.add("template_ref", "bắt buộc")
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	b, err := h.Svc.BindVariety(c.Request.Context(), req.VarietyCode, t, req.Note)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, bindingOut(b))
}

func (h Handler) rebuild(c *gin.Context) {
	r, err := h.Svc.RebuildProjections(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"seasons": r.Seasons, "farm_logs": r.Logs, "coop_season_areas": r.CoopAreas})
}

// ---------------------------------------------------------------------------
// Vụ
// ---------------------------------------------------------------------------

type openReq struct {
	ID           *string `json:"id"`
	SeasonRef    *string `json:"season_ref"`
	PlotRef      *string `json:"plot_ref"`
	SeasonCode   string  `json:"season_code"`
	SeasonYear   int     `json:"season_year"`
	VarietyCode  string  `json:"variety_code"`
	SowingDate   *string `json:"sowing_date"`
	SOPVersionID *string `json:"sop_version_id"`
	CopyPrevious bool    `json:"copy_previous"`
	GPS          *gpsReq `json:"gps"`
	EventTime    *string `json:"event_time"`
	DeviceID     *string `json:"device_id"`
}

func (h Handler) openSeason(c *gin.Context) {
	var req openReq
	if !bindJSON(c, &req) {
		return
	}
	f := newFields()
	in := app.OpenInput{EventID: f.v7("id", req.ID), SeasonRef: f.v7("season_ref", req.SeasonRef), PlotRef: f.ref("plot_ref", req.PlotRef),
		SeasonCode: req.SeasonCode, SeasonYear: req.SeasonYear, VarietyCode: req.VarietyCode, SOPVersionID: f.ref("sop_version_id", req.SOPVersionID),
		CopyPrevious: req.CopyPrevious, GPS: req.GPS.gps(), EventTime: f.at("event_time", req.EventTime), DeviceID: req.DeviceID,
		Channel: app.ChannelApp, IdemKey: idem(c)}
	if in.PlotRef == uuid.Nil {
		f.add("plot_ref", "bắt buộc")
	}
	if d := f.day("sowing_date", req.SowingDate); d != nil {
		in.SowingDate = *d
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	r, err := h.Svc.OpenSeason(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/cropcycle/seasons/"+r.Season.SeasonRef.String())
	created(c, r.Replayed, gin.H{"season": seasonOut(r.Season), "event": seasonEventOut(r.Event)})
}

type bulkOpenReq struct {
	PlotRefs     []string `json:"plot_refs"`
	SeasonCode   string   `json:"season_code"`
	SeasonYear   int      `json:"season_year"`
	VarietyCode  string   `json:"variety_code"`
	SowingDate   *string  `json:"sowing_date"`
	SOPVersionID *string  `json:"sop_version_id"`
}

func (h Handler) bulkOpen(c *gin.Context) {
	var req bulkOpenReq
	if !bindJSON(c, &req) {
		return
	}
	f := newFields()
	in := app.BulkOpenInput{SeasonCode: req.SeasonCode, SeasonYear: req.SeasonYear, VarietyCode: req.VarietyCode,
		SOPVersionID: f.ref("sop_version_id", req.SOPVersionID), IdemKey: idem(c)}
	for i := range req.PlotRefs {
		in.PlotRefs = append(in.PlotRefs, f.ref("plot_refs", &req.PlotRefs[i]))
	}
	if d := f.day("sowing_date", req.SowingDate); d != nil {
		in.SowingDate = *d
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	items, err := h.Svc.BulkOpen(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(items))
	opened := 0
	for _, it := range items {
		row := gin.H{"plot_ref": it.PlotRef.String(), "status": it.Status}
		if it.Status == "opened" {
			opened++
			row["season_ref"] = it.SeasonRef.String()
		} else {
			row["code"], row["reason"] = it.Code, it.Reason
		}
		out = append(out, row)
	}
	c.JSON(http.StatusOK, gin.H{"opened": opened, "skipped": len(items) - opened, "items": out})
}

func (h Handler) listSeasons(c *gin.Context) {
	after, limit, ok := cursor(c)
	if !ok {
		return
	}
	f := newFields()
	in := app.ListSeasonsInput{CooperativeRef: qRef(c, f, "cooperative_ref"), PlotRef: qRef(c, f, "plot_ref"), Status: c.Query("status"),
		SeasonCode: c.Query("season_code"), SeasonYear: qInt(c, f, "season_year"), VarietyCode: c.Query("variety_code"), After: after, Limit: limit}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	pg, err := h.Svc.ListSeasons(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]seasonDTO, 0, len(pg.Items))
	for _, s := range pg.Items {
		items = append(items, seasonOut(s))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next(pg.Next)})
}

func (h Handler) getSeason(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrSeasonNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.GetSeason(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, seasonDetailOut(d))
}

type correctSeasonReq struct {
	ID               *string `json:"id"`
	SupersedesID     *string `json:"supersedes_id"`
	CorrectionReason string  `json:"correction_reason"`
	Reason           string  `json:"reason"`
	VarietyCode      *string `json:"variety_code"`
	SowingDate       *string `json:"sowing_date"`
	SOPVersionID     *string `json:"sop_version_id"`
}

func (h Handler) correctSeason(cancel bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ref, ok := pathID(c, "ref", app.ErrSeasonNotFound)
		if !ok {
			return
		}
		var req correctSeasonReq
		if !bindJSON(c, &req) {
			return
		}
		f := newFields()
		in := app.CorrectSeasonInput{EventID: f.v7("id", req.ID), SeasonRef: ref, SupersedesID: f.ref("supersedes_id", req.SupersedesID),
			Reason: req.CorrectionReason, Cancel: cancel, Channel: app.ChannelApp, IdemKey: idem(c)}
		if cancel && in.Reason == "" {
			in.Reason = req.Reason
		}
		if in.SupersedesID == uuid.Nil {
			f.add("supersedes_id", "bắt buộc: sự kiện hiện hành của vụ (current_event_id)")
		}
		if !cancel {
			in.VarietyCode, in.SowingDate = req.VarietyCode, f.day("sowing_date", req.SowingDate)
			if req.SOPVersionID != nil {
				v := f.ref("sop_version_id", req.SOPVersionID)
				in.SOPVersionID = &v
			}
		}
		if f.bad {
			httpx.Fail(c, f.p)
			return
		}
		r, err := h.Svc.CorrectSeason(c.Request.Context(), in)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		created(c, r.Replayed, gin.H{"season": seasonOut(r.Season), "event": seasonEventOut(r.Event)})
	}
}

func (h Handler) previousConfig(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrPlotNotFound)
	if !ok {
		return
	}
	p, err := h.Svc.PreviousSeasonConfig(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"season_ref": p.SeasonRef.String(), "season_code": p.SeasonCode, "season_year": p.SeasonYear,
		"variety_code": p.VarietyCode, "sop_template_ref": p.SOPTemplate.String(), "sop_version_id": p.SOPVersionID.String(),
		"sop_version_no": p.SOPVersionNo})
}

// ---------------------------------------------------------------------------
// Sản lượng
// ---------------------------------------------------------------------------

type harvestReq struct {
	ID             *string          `json:"id"`
	HarvestDate    *string          `json:"harvest_date"`
	MassKg         string           `json:"mass_kg"`
	MoisturePct    *decimal.Decimal `json:"moisture_pct"`
	ConfirmAnomaly bool             `json:"confirm_anomaly"`
	GPS            *gpsReq          `json:"gps"`
	EventTime      *string          `json:"event_time"`
	DeviceID       *string          `json:"device_id"`
}

func (h Handler) declareHarvest(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrSeasonNotFound)
	if !ok {
		return
	}
	var req harvestReq
	if !bindJSON(c, &req) {
		return
	}
	f := newFields()
	in := app.HarvestInput{ID: f.v7("id", req.ID), SeasonRef: ref, MassKg: f.kg("mass_kg", req.MassKg), MoisturePct: req.MoisturePct,
		ConfirmAnomaly: req.ConfirmAnomaly, GPS: req.GPS.gps(), EventTime: f.at("event_time", req.EventTime), DeviceID: req.DeviceID,
		Source: app.SourceApp, IdemKey: idem(c)}
	if d := f.day("harvest_date", req.HarvestDate); d != nil {
		in.HarvestDate = *d
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	r, err := h.Svc.DeclareHarvest(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, r.Replayed, gin.H{"harvest": harvestOut(r.Harvest), "season": seasonOut(r.Season)})
}

type correctHarvestReq struct {
	harvestReq
	CorrectionReason string `json:"correction_reason"`
}

func (h Handler) correctHarvest(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrHarvestNotFound)
	if !ok {
		return
	}
	var req correctHarvestReq
	if !bindJSON(c, &req) {
		return
	}
	f := newFields()
	in := app.CorrectHarvestInput{ID: f.v7("id", req.ID), SupersedesID: id, Reason: req.CorrectionReason, HarvestDate: f.day("harvest_date", req.HarvestDate),
		MassKg: f.kg("mass_kg", req.MassKg), MoisturePct: req.MoisturePct, ConfirmAnomaly: req.ConfirmAnomaly,
		EventTime: f.at("event_time", req.EventTime), DeviceID: req.DeviceID, IdemKey: idem(c)}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	r, err := h.Svc.CorrectHarvest(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, r.Replayed, gin.H{"harvest": harvestOut(r.Harvest), "season": seasonOut(r.Season)})
}

type bulkHarvestRow struct {
	SeasonRef      string  `json:"season_ref"`
	PlotCode       string  `json:"plot_code"`
	HarvestDate    string  `json:"harvest_date"`
	MassKg         *string `json:"mass_kg"`
	MoisturePct    *string `json:"moisture_pct"`
	ConfirmAnomaly bool    `json:"confirm_anomaly"`
}

type bulkHarvestReq struct {
	CooperativeRef *string          `json:"cooperative_ref"`
	Rows           []bulkHarvestRow `json:"rows"`
}

func bulkResult(c *gin.Context, r app.BulkHarvestResult) {
	rows := make([]gin.H, 0, len(r.Rows))
	for _, x := range r.Rows {
		row := gin.H{"row": x.Line, "status": x.Status}
		if x.Code != "" {
			row["code"], row["reason"] = x.Code, x.Reason
		}
		if x.NeedsConfirm {
			row["needs_confirm"] = true
		}
		if x.SeasonRef != uuid.Nil {
			row["season_ref"] = x.SeasonRef.String()
		}
		if x.HarvestID != uuid.Nil {
			row["harvest_id"] = x.HarvestID.String()
		}
		rows = append(rows, row)
	}
	c.JSON(http.StatusOK, gin.H{"accepted": r.Accepted, "duplicate": r.Duplicate, "rejected": r.Rejected, "rows": rows})
}

func (h Handler) bulkHarvest(c *gin.Context) {
	var req bulkHarvestReq
	if !bindJSON(c, &req) {
		return
	}
	f := newFields()
	coop := f.ref("cooperative_ref", req.CooperativeRef)
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	rows := make([]app.HarvestRow, 0, len(req.Rows))
	for i, r := range req.Rows {
		row := app.HarvestRow{Line: i + 1, SeasonRef: r.SeasonRef, PlotCode: r.PlotCode, HarvestDate: r.HarvestDate, Confirm: r.ConfirmAnomaly}
		if r.MassKg != nil {
			row.MassKg = *r.MassKg
		}
		if r.MoisturePct != nil {
			row.MoisturePct = *r.MoisturePct
		}
		rows = append(rows, row)
	}
	r, err := h.Svc.BulkHarvest(c.Request.Context(), coop, rows, app.SourceWeb, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	bulkResult(c, r)
}

func (h Handler) importHarvests(c *gin.Context) {
	f := newFields()
	coop := qRef(c, f, "cooperative_ref")
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	r, err := h.Svc.ImportHarvests(c.Request.Context(), coop, c.Request.Body, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	bulkResult(c, r)
}

// ---------------------------------------------------------------------------
// Nhật ký
// ---------------------------------------------------------------------------

type inputReq struct {
	Name string          `json:"name"`
	Qty  decimal.Decimal `json:"qty"`
	Unit string          `json:"unit"`
}

type contentReq struct {
	ActivityCode string     `json:"activity_code"`
	SOPStepSeq   *int       `json:"sop_step_seq"`
	MachineCodes []string   `json:"machine_codes"`
	Inputs       []inputReq `json:"inputs"`
	Note         string     `json:"note"`
	FileRefs     []string   `json:"file_refs"`
}

func (r contentReq) content(f *fields) app.LogContent {
	c := app.LogContent{ActivityCode: r.ActivityCode, SOPStepSeq: r.SOPStepSeq, MachineCodes: r.MachineCodes, Note: r.Note}
	for _, x := range r.Inputs {
		c.Inputs = append(c.Inputs, domain.Input{Name: x.Name, Qty: x.Qty, Unit: x.Unit})
	}
	for i := range r.FileRefs {
		c.FileRefs = append(c.FileRefs, f.ref("file_refs", &r.FileRefs[i]))
	}
	return c
}

type logReq struct {
	contentReq
	ID        *string `json:"id"`
	SeasonRef *string `json:"season_ref"`
	PlotRef   *string `json:"plot_ref"`
	GPS       *gpsReq `json:"gps"`
	EventTime *string `json:"event_time"`
	DeviceID  *string `json:"device_id"`
}

func (h Handler) recordLog(c *gin.Context) {
	var req logReq
	if !bindJSON(c, &req) {
		return
	}
	f := newFields()
	in := app.LogInput{ID: f.v7("id", req.ID), SeasonRef: f.ref("season_ref", req.SeasonRef), PlotRef: f.ref("plot_ref", req.PlotRef),
		LogContent: req.content(f), EventTime: f.at("event_time", req.EventTime), GPS: req.GPS.gps(), DeviceID: req.DeviceID,
		Channel: app.ChannelApp, IdemKey: idem(c)}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	r, err := h.Svc.RecordLog(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/cropcycle/farm-logs/"+r.Log.ID.String())
	created(c, r.Replayed, gin.H{"log": logOut(r.Log), "state": stateOut(r.State)})
}

type batchReq struct {
	contentReq
	ID        *string  `json:"id"`
	PlotRefs  []string `json:"plot_refs"`
	GPS       *gpsReq  `json:"gps"`
	EventTime *string  `json:"event_time"`
}

func (h Handler) recordBatch(c *gin.Context) {
	var req batchReq
	if !bindJSON(c, &req) {
		return
	}
	f := newFields()
	in := app.BatchInput{ID: f.v7("id", req.ID), LogContent: req.content(f), EventTime: f.at("event_time", req.EventTime), GPS: req.GPS.gps(),
		Channel: app.ChannelWeb, IdemKey: idem(c)}
	for i := range req.PlotRefs {
		in.PlotRefs = append(in.PlotRefs, f.ref("plot_refs", &req.PlotRefs[i]))
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	r, err := h.Svc.RecordBatch(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	lines := make([]logDTO, 0, len(r.Lines))
	for _, l := range r.Lines {
		lines = append(lines, logOut(l))
	}
	skipped := make([]gin.H, 0, len(r.Batch.Skipped))
	for _, s := range r.Batch.Skipped {
		skipped = append(skipped, gin.H{"plot_ref": s.PlotRef.String(), "code": s.Code, "reason": s.Reason})
	}
	created(c, r.Replayed, gin.H{"batch_ref": r.Batch.ID.String(), "requested": r.Batch.Requested, "accepted": r.Batch.Accepted,
		"skipped": skipped, "lines": lines})
}

func (h Handler) listLogs(c *gin.Context) {
	after, limit, ok := cursor(c)
	if !ok {
		return
	}
	f := newFields()
	in := app.ListLogsInput{CooperativeRef: qRef(c, f, "cooperative_ref"), SeasonRef: qRef(c, f, "season_ref"), Status: c.Query("status"),
		After: after, Limit: limit}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	pg, err := h.Svc.ListLogs(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]logStateDTO, 0, len(pg.Items))
	for _, s := range pg.Items {
		items = append(items, stateOut(s))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next(pg.Next)})
}

func (h Handler) getLog(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrLogNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.GetLog(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	versions := make([]logDTO, 0, len(d.Versions))
	for _, v := range d.Versions {
		versions = append(versions, logOut(v))
	}
	c.JSON(http.StatusOK, gin.H{"log": logOut(d.Log), "state": stateOut(d.State), "versions": versions})
}

type correctLogReq struct {
	contentReq
	ID               *string `json:"id"`
	CorrectionReason string  `json:"correction_reason"`
	GPS              *gpsReq `json:"gps"`
	EventTime        *string `json:"event_time"`
	DeviceID         *string `json:"device_id"`
}

func (h Handler) correctLog(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrLogNotFound)
	if !ok {
		return
	}
	var req correctLogReq
	if !bindJSON(c, &req) {
		return
	}
	f := newFields()
	in := app.CorrectLogInput{ID: f.v7("id", req.ID), SupersedesID: id, Reason: req.CorrectionReason, LogContent: req.content(f),
		DeviceID: req.DeviceID, Channel: app.ChannelApp, IdemKey: idem(c)}
	if req.EventTime != nil {
		t := f.at("event_time", req.EventTime)
		in.EventTime = &t
	}
	if req.GPS != nil {
		g := req.GPS.gps()
		in.GPS = &g
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	r, err := h.Svc.CorrectLog(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, r.Replayed, gin.H{"log": logOut(r.Log), "state": stateOut(r.State)})
}

type reviewReq struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

func (h Handler) reviewLog(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrLogNotFound)
	if !ok {
		return
	}
	var req reviewReq
	if !bindJSON(c, &req) {
		return
	}
	r, err := h.Svc.ReviewLog(c.Request.Context(), app.ReviewInput{LogID: id, Decision: req.Decision, Note: req.Note, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, r.Replayed, gin.H{"review_id": r.Review.ID.String(), "decision": r.Review.Decision, "state": stateOut(r.State)})
}

// ---------------------------------------------------------------------------
// Bảng điều hành
// ---------------------------------------------------------------------------

func (h Handler) dashboard(c *gin.Context) {
	f := newFields()
	in := app.DashboardInput{SeasonCode: c.Query("season_code"), SeasonYear: qInt(c, f, "season_year"),
		CooperativeRef: qRef(c, f, "cooperative_ref"), VarietyCode: c.Query("variety_code")}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	d, err := h.Svc.Dashboard(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	group := func(gs []app.DashGroup) []dashGroupDTO {
		out := make([]dashGroupDTO, 0, len(gs))
		for _, g := range gs {
			out = append(out, dashOut(g, true))
		}
		return out
	}
	c.JSON(http.StatusOK, gin.H{"totals": dashOut(d.Totals, false), "by_variety": group(d.ByVariety), "by_season": group(d.BySeason),
		"by_cooperative": group(d.ByCooperative)})
}
