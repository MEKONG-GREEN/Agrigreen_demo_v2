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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API vào nhóm /v1 (OIDC + Idempotency-Key cho POST).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/plots", h.bbox)
	rg.POST("/plots", h.create)
	rg.POST("/plots/overlap-check", h.overlapCheck)
	rg.GET("/plots/:ref", h.get)
	rg.POST("/plots/:ref/boundaries", h.correct)
	rg.POST("/plots/:ref/assign", h.assign)
	rg.POST("/plots/:ref/retire", h.retire)
	rg.GET("/cooperatives/:ref/plots", h.coopPlots)
	rg.GET("/geo/conflicts", h.conflicts)
	rg.POST("/geo/conflicts/:id/resolve", h.resolve)
	rg.GET("/geo/admin-boundaries/:code", h.adminBoundary)
}

// RegisterAdmin gắn route Admin API (MFA bắt buộc): nhập tệp.
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.POST("/geo/plots/import", h.importPlots)
	rg.POST("/geo/admin-boundaries/import", h.importAdmin)
}

func bindJSON(c *gin.Context, v any) bool {
	dec := json.NewDecoder(c.Request.Body)
	if err := dec.Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func pathRef(c *gin.Context, name string) (uuid.UUID, bool) {
	u, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, app.ErrPlotNotFound)
		return uuid.Nil, false
	}
	return u, true
}

// fieldCollector gom lỗi theo trường của một request.
type fieldCollector struct {
	p   *problem.Problem
	bad bool
}

func newCollector() *fieldCollector { return &fieldCollector{p: problem.Validation} }

func (f *fieldCollector) add(field, reason string) { f.p, f.bad = f.p.WithField(field, reason), true }

func (f *fieldCollector) ref(field string, s *string) uuid.UUID {
	if s == nil || strings.TrimSpace(*s) == "" {
		return uuid.Nil
	}
	u, err := ids.ParseRef(*s)
	if err != nil {
		f.add(field, "phải là UUID")
	}
	return u
}

func (f *fieldCollector) v7(field string, s *string) uuid.UUID {
	if s == nil || *s == "" {
		return uuid.Nil
	}
	u, err := ids.Parse(*s)
	if err != nil {
		f.add(field, "phải là UUIDv7")
	}
	return u
}

func (f *fieldCollector) at(field string, s *string) time.Time {
	if s == nil || *s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		f.add(field, "phải theo RFC 3339 có múi giờ")
	}
	return t
}

type boundaryReq struct {
	BoundaryID   *string          `json:"boundary_id"`
	Geometry     json.RawMessage  `json:"geometry"`
	GPSAccuracyM *decimal.Decimal `json:"gps_accuracy_m"`
	Source       string           `json:"source"`
	EventTime    *string          `json:"event_time"`
	DeviceID     *string          `json:"device_id"`
	// DeclaredAreaHa: diện tích khai báo để đối chiếu (HTX US-PLOT-02 AC-3).
	DeclaredAreaHa *decimal.Decimal `json:"declared_area_ha"`
	// ConfirmOverlap: xác nhận giữ nguyên chồng lấn cùng HTX/thửa nháp hoặc lệch diện tích (HTX US-PLOT-04 AC-2).
	ConfirmOverlap bool `json:"confirm_overlap"`
}

func (r boundaryReq) input(f *fieldCollector) app.BoundaryInput {
	return app.BoundaryInput{
		ID: f.v7("boundary_id", r.BoundaryID), Geometry: r.Geometry, GPSAccuracyM: r.GPSAccuracyM,
		Source: strings.TrimSpace(r.Source), EventTime: f.at("event_time", r.EventTime), DeviceID: r.DeviceID,
		DeclaredAreaHa: r.DeclaredAreaHa, Confirm: r.ConfirmOverlap,
	}
}

type createReq struct {
	boundaryReq
	PlotRef        *string `json:"plot_ref"`
	Name           string  `json:"name"`
	CommuneCode    string  `json:"commune_code"`
	CooperativeRef *string `json:"cooperative_ref"`
	HouseholdRef   *string `json:"household_ref"`
}

func writeResult(c *gin.Context, res app.WriteResult) {
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/plots/"+res.Plot.Ref.String())
	c.JSON(status, gin.H{
		"plot": plotFeature(res.Plot), "boundary": boundaryDTO(res.Boundary),
		"conflicts": conflictList(res.Conflicts), "flagged_overlaps": overlapDTOs(res.Flagged),
	})
}

func (h Handler) create(c *gin.Context) {
	var req createReq
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	in := app.CreateInput{
		PlotRef: f.v7("plot_ref", req.PlotRef), Name: req.Name, CommuneCode: req.CommuneCode,
		CooperativeRef: f.ref("cooperative_ref", req.CooperativeRef), HouseholdRef: f.ref("household_ref", req.HouseholdRef),
		Boundary: req.input(f), IdemKey: idempotency.KeyFromContext(c.Request.Context()),
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	res, err := h.Svc.CreatePlot(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	writeResult(c, res)
}

type correctReq struct {
	boundaryReq
	SupersedesID     *string `json:"supersedes_id"`
	CorrectionReason string  `json:"correction_reason"`
}

func (h Handler) correct(c *gin.Context) {
	ref, ok := pathRef(c, "ref")
	if !ok {
		return
	}
	var req correctReq
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	in := app.CorrectInput{
		PlotRef: ref, SupersedesID: f.ref("supersedes_id", req.SupersedesID), Reason: req.CorrectionReason,
		Boundary: req.input(f), IdemKey: idempotency.KeyFromContext(c.Request.Context()),
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	res, err := h.Svc.CorrectBoundary(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	writeResult(c, res)
}

type assignReq struct {
	CooperativeRef *string `json:"cooperative_ref"`
	HouseholdRef   *string `json:"household_ref"`
	Reason         string  `json:"reason"`
	ConfirmOverlap bool    `json:"confirm_overlap"`
}

func attrResult(c *gin.Context, res app.AttrResult) {
	c.JSON(http.StatusOK, gin.H{"plot": plotFeature(res.Plot), "conflicts": conflictList(res.Conflicts), "event": gin.H{
		"id": res.Event.ID.String(), "action": res.Event.Action, "seq": res.Event.Seq, "event_time": ts(res.Event.EventTime),
		"reason": strOrNil(res.Event.Reason), "actor_ref": res.Event.ActorRef.String(),
	}})
}

func (h Handler) assign(c *gin.Context) {
	ref, ok := pathRef(c, "ref")
	if !ok {
		return
	}
	var req assignReq
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	in := app.AssignInput{
		PlotRef: ref, CooperativeRef: f.ref("cooperative_ref", req.CooperativeRef),
		HouseholdRef: f.ref("household_ref", req.HouseholdRef), Reason: req.Reason, ConfirmOverlap: req.ConfirmOverlap,
		IdemKey: idempotency.KeyFromContext(c.Request.Context()),
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	res, err := h.Svc.Assign(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	attrResult(c, res)
}

func (h Handler) retire(c *gin.Context) {
	ref, ok := pathRef(c, "ref")
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.Svc.Retire(c.Request.Context(), app.RetireInput{PlotRef: ref, Reason: req.Reason, IdemKey: idempotency.KeyFromContext(c.Request.Context())})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	attrResult(c, res)
}

func (h Handler) overlapCheck(c *gin.Context) {
	var req struct {
		Geometry       json.RawMessage  `json:"geometry"`
		CommuneCode    string           `json:"commune_code"`
		CooperativeRef *string          `json:"cooperative_ref"`
		ExcludePlotRef *string          `json:"exclude_plot_ref"`
		DeclaredAreaHa *decimal.Decimal `json:"declared_area_ha"`
	}
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	in := app.OverlapCheckInput{
		Geometry: req.Geometry, CommuneCode: req.CommuneCode, CooperativeRef: f.ref("cooperative_ref", req.CooperativeRef),
		ExcludePlotRef: f.ref("exclude_plot_ref", req.ExcludePlotRef), DeclaredAreaHa: req.DeclaredAreaHa,
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	out, err := h.Svc.CheckOverlap(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"area_ha": out.AreaHa.StringFixed(4), "point_count": out.PointCount, "tolerance_pct": out.Tolerance.String(),
		"blocking": overlapDTOs(out.Blocking), "needs_confirm": overlapDTOs(out.NeedsConfirm),
		"deviation_pct": decOrNil(out.DeviationPct), "deviation_needs_confirm": out.DeviationNeedsConfirm,
		"can_save":         len(out.Blocking) == 0,
		"requires_confirm": len(out.Blocking) == 0 && (len(out.NeedsConfirm) > 0 || out.DeviationNeedsConfirm),
	})
}

func intQuery(c *gin.Context, name string, def int) (int, error) {
	s := c.Query(name)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, problem.Validation.WithField(name, "phải là số nguyên")
	}
	return n, nil
}

func (h Handler) bbox(c *gin.Context) {
	parts := strings.Split(c.Query("bbox"), ",")
	if len(parts) != 4 {
		httpx.Fail(c, problem.Validation.WithField("bbox", "bắt buộc: minLon,minLat,maxLon,maxLat"))
		return
	}
	var v [4]float64
	for i, p := range parts {
		x, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("bbox", "phải là 4 số: minLon,minLat,maxLon,maxLat"))
			return
		}
		v[i] = x
	}
	zoom, err := intQuery(c, "zoom", 17)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	limit, err := intQuery(c, "limit", app.MaxViewportPlots)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	pg, err := h.Svc.PlotsInBBox(c.Request.Context(), app.BBoxInput{MinLon: v[0], MinLat: v[1], MaxLon: v[2], MaxLat: v[3], Zoom: zoom, Limit: limit})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"type": "FeatureCollection", "features": featureCollection(pg.Items), "truncated": pg.Truncated, "limit": limit})
}

func (h Handler) get(c *gin.Context) {
	ref, ok := pathRef(c, "ref")
	if !ok {
		return
	}
	d, err := h.Svc.GetPlot(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	versions := make([]gin.H, 0, len(d.Boundaries))
	for _, b := range d.Boundaries {
		versions = append(versions, boundaryDTO(b))
	}
	c.JSON(http.StatusOK, gin.H{"plot": plotFeature(d.Plot), "versions": versions, "conflicts": conflictList(d.Conflicts)})
}

func (h Handler) coopPlots(c *gin.Context) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, problem.NotFound)
		return
	}
	after, err := decodeCursor(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	limit, err := intQuery(c, "limit", 200)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	pg, err := h.Svc.PlotsOfCooperative(c.Request.Context(), ref, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"type": "FeatureCollection", "features": featureCollection(pg.Items), "next_cursor": encodeCursor(pg.Next)})
}

func (h Handler) conflicts(c *gin.Context) {
	after, err := decodeCursor(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	limit, err := intQuery(c, "limit", 50)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	pg, err := h.Svc.ListConflicts(c.Request.Context(), app.ConflictQuery{Status: c.Query("status"), After: after, Limit: limit})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": conflictList(pg.Items), "next_cursor": encodeCursor(pg.Next)})
}

func (h Handler) resolve(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrConflictNotFound)
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.Svc.ResolveConflict(c.Request.Context(), id, req.Note)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, conflictDTO(out))
}

func (h Handler) adminBoundary(c *gin.Context) {
	zoom, err := intQuery(c, "zoom", 12)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	b, err := h.Svc.GetAdminBoundary(c.Request.Context(), c.Param("code"), zoom)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"type": "Feature", "id": b.UnitCode, "geometry": json.RawMessage(b.GeoJSON),
		"properties": gin.H{"code": b.UnitCode, "level": b.Level, "name": b.Name, "source": b.Source,
			"area_ha": b.AreaHa.StringFixed(4), "imported_at": ts(b.ImportedAt)},
	})
}

// ---------------------------------------------------------------------------
// Admin API
// ---------------------------------------------------------------------------

func (h Handler) importPlots(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	f := newCollector()
	coopQ := c.Query("cooperative_ref")
	in := app.ImportInput{
		Format: c.Query("format"), FileName: c.Query("file_name"), Data: raw, CommuneCode: c.Query("commune_code"),
		CooperativeRef: f.ref("cooperative_ref", &coopQ), Source: c.Query("source"), ConfirmOverlap: c.Query("confirm_overlap") == "true",
		IdemKey: idempotency.KeyFromContext(c.Request.Context()),
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	res, err := h.Svc.ImportPlots(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	feats := make([]gin.H, 0, len(res.Features))
	for _, r := range res.Features {
		feats = append(feats, gin.H{
			"index": r.Index, "name": strOrNil(r.Name), "status": r.Status, "code": strOrNil(r.Code), "reason": strOrNil(r.Reason),
			"plot_ref": refOrNil(r.PlotRef), "plot_code": strOrNil(r.PlotCode), "area_ha": decOrNil(r.AreaHa),
			"flagged_overlaps": overlapDTOs(r.Overlaps),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"format": res.Format, "accepted": res.Accepted, "flagged": res.Flagged, "duplicate": res.Duplicate,
		"rejected": res.Rejected, "features": feats,
	})
}

func (h Handler) importAdmin(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	dry := c.Query("dry_run") == "true"
	res, err := h.Svc.ImportAdminBoundaries(c.Request.Context(), app.AdminImportInput{Data: raw, Source: c.Query("source"), DryRun: dry})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	feats := make([]gin.H, 0, len(res.Features))
	for _, r := range res.Features {
		feats = append(feats, gin.H{
			"index": r.Index, "code": strOrNil(r.UnitCode), "name": strOrNil(r.Name), "level": strOrNil(r.Level),
			"status": r.Status, "replaced": r.Replaced, "error_code": strOrNil(r.Code), "reason": strOrNil(r.Reason),
			"area_ha": decOrNil(r.AreaHa),
		})
	}
	c.JSON(http.StatusOK, gin.H{"dry_run": res.DryRun, "accepted": res.Accepted, "rejected": res.Rejected, "features": feats})
}
