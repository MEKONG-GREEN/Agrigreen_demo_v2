package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// registerCoopApp gắn route Application API của ranh giới HTX (GIS US-BND-02/03/07): đọc theo khung nhìn, ghi
// phiên bản, ngừng dùng / khôi phục.
func (h Handler) registerCoopApp(rg *gin.RouterGroup) {
	rg.GET("/geo/cooperative-boundaries", h.coopBBox)
	rg.POST("/geo/cooperative-boundaries", h.coopCreate)
	rg.GET("/geo/cooperative-boundaries/:ref", h.coopGet)
	rg.POST("/geo/cooperative-boundaries/:ref/versions", h.coopCorrect)
	rg.POST("/geo/cooperative-boundaries/:ref/retire", h.coopRetire)
	rg.POST("/geo/cooperative-boundaries/:ref/restore", h.coopRestore)
}

// registerCoopAdmin gắn route Admin API (MFA): nhập ranh giới HTX.
func (h Handler) registerCoopAdmin(rg *gin.RouterGroup) {
	httpx.AllowBody(rg, http.MethodPost, "/geo/cooperative-boundaries/import", MaxImportBody)
	rg.POST("/geo/cooperative-boundaries/import", h.coopImport)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

// refParam đọc UUID ở path; sai → notFound.
func refParam(c *gin.Context, name string, notFound *problem.Problem) (uuid.UUID, bool) {
	u, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, notFound)
		return uuid.Nil, false
	}
	return u, true
}

// bboxInput đọc bbox, zoom, limit của khung nhìn.
func bboxInput(c *gin.Context, defZoom int) (app.BBoxInput, bool) {
	parts := strings.Split(c.Query("bbox"), ",")
	if len(parts) != 4 {
		httpx.Fail(c, problem.Validation.WithField("bbox", "bắt buộc: minLon,minLat,maxLon,maxLat"))
		return app.BBoxInput{}, false
	}
	var v [4]float64
	for i, p := range parts {
		x, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("bbox", "phải là 4 số: minLon,minLat,maxLon,maxLat"))
			return app.BBoxInput{}, false
		}
		v[i] = x
	}
	zoom, err := intQuery(c, "zoom", defZoom)
	if err != nil {
		httpx.Fail(c, err)
		return app.BBoxInput{}, false
	}
	limit, err := intQuery(c, "limit", app.MaxViewportPlots)
	if err != nil {
		httpx.Fail(c, err)
		return app.BBoxInput{}, false
	}
	return app.BBoxInput{MinLon: v[0], MinLat: v[1], MaxLon: v[2], MaxLat: v[3], Zoom: zoom, Limit: limit}, true
}

func f7(x float64) string { return strconv.FormatFloat(x, 'f', 7, 64) }

func pointGeom(lon, lat float64) json.RawMessage {
	return json.RawMessage(`{"type":"Point","coordinates":[` + f7(lon) + "," + f7(lat) + "]}")
}

// ---------------------------------------------------------------------------
// Ranh giới HTX
// ---------------------------------------------------------------------------

func coopFeature(a store.CoopArea, marker json.RawMessage, markerSource string) gin.H {
	if marker == nil {
		marker, markerSource = pointGeom(a.MarkerLon, a.MarkerLat), "point_on_surface"
	}
	return gin.H{
		"type": "Feature", "id": a.CooperativeRef.String(), "geometry": json.RawMessage(a.GeoJSON),
		"properties": gin.H{
			"cooperative_ref": a.CooperativeRef.String(), "commune_code": a.CommuneCode, "status": a.Status,
			"status_reason": strOrNil(a.StatusReason), "version": a.CurrentVersion, "boundary_id": a.CurrentBoundaryID.String(),
			"area_ha": a.AreaHa.StringFixed(4), "source": a.Source, "revision": a.Revision, "marker": marker,
			"marker_source": markerSource, "created_at": ts(a.CreatedAt), "updated_at": ts(a.UpdatedAt),
		},
	}
}

func coopBoundaryDTO(b store.CoopBoundary) gin.H {
	m := gin.H{
		"boundary_id": b.ID.String(), "cooperative_ref": b.CooperativeRef.String(), "version": b.Version, "action": b.Action,
		"area_ha": b.AreaHa.StringFixed(4), "point_count": b.PointCount, "source": b.Source, "commune_code": b.CommuneCode,
		"event_time": ts(b.EventTime), "recorded_at": ts(b.RecordedAt), "supersedes_id": nil,
		"correction_reason": b.CorrectionReason, "actor_ref": b.ActorRef.String(), "device_id": b.DeviceID,
	}
	if b.SupersedesID != nil {
		m["supersedes_id"] = b.SupersedesID.String()
	}
	if b.GeoJSON != "" {
		m["geometry"] = json.RawMessage(b.GeoJSON)
	}
	return m
}

func (h Handler) coopBBox(c *gin.Context) {
	in, ok := bboxInput(c, 12)
	if !ok {
		return
	}
	l, err := h.Svc.CoopBoundariesInBBox(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, coopLayerDTO(l, in.Limit))
}

func coopLayerDTO(l app.CoopLayer, limit int) gin.H {
	feats := make([]gin.H, 0, len(l.Items)+len(l.Unbounded))
	for _, a := range l.Items {
		var marker json.RawMessage
		src := ""
		if m, ok := l.Markers[a.CooperativeRef]; ok {
			marker, src = pointGeom(m.Lon.InexactFloat64(), m.Lat.InexactFloat64()), "party_location"
		}
		feats = append(feats, coopFeature(a, marker, src))
	}
	for _, m := range l.Unbounded {
		feats = append(feats, gin.H{
			"type": "Feature", "id": m.Ref.String(), "geometry": pointGeom(m.Lon.InexactFloat64(), m.Lat.InexactFloat64()),
			"properties": gin.H{"cooperative_ref": m.Ref.String(), "code": m.Code, "name": m.Name, "commune_code": m.CommuneCode,
				"status": nil, "boundary_id": nil, "marker_source": "party_location"},
		})
	}
	if limit == 0 {
		limit = app.MaxViewportPlots
	}
	return gin.H{"type": "FeatureCollection", "features": feats, "truncated": l.Truncated, "limit": limit}
}

type coopShapeReq struct {
	BoundaryID *string         `json:"boundary_id"`
	Geometry   json.RawMessage `json:"geometry"`
	Source     string          `json:"source"`
	EventTime  *string         `json:"event_time"`
	DeviceID   *string         `json:"device_id"`
}

func (r coopShapeReq) input(f *fieldCollector) app.CoopShapeInput {
	return app.CoopShapeInput{
		ID: f.v7("boundary_id", r.BoundaryID), Geometry: r.Geometry, Source: strings.TrimSpace(r.Source),
		EventTime: f.at("event_time", r.EventTime), DeviceID: r.DeviceID,
	}
}

func coopWriteResult(c *gin.Context, res app.CoopWriteResult, created bool) {
	status := http.StatusOK
	if created && !res.Replayed {
		status = http.StatusCreated
	}
	c.Header("Location", "/v1/geo/cooperative-boundaries/"+res.Area.CooperativeRef.String())
	c.JSON(status, gin.H{"cooperative": coopFeature(res.Area, nil, ""), "boundary": coopBoundaryDTO(res.Boundary)})
}

func (h Handler) coopCreate(c *gin.Context) {
	var req struct {
		coopShapeReq
		CooperativeRef *string `json:"cooperative_ref"`
		CommuneCode    string  `json:"commune_code"`
	}
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	in := app.CoopBoundaryInput{
		CooperativeRef: f.ref("cooperative_ref", req.CooperativeRef), CommuneCode: req.CommuneCode, Shape: req.input(f), IdemKey: idem(c),
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	res, err := h.Svc.CreateCoopBoundary(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	coopWriteResult(c, res, true)
}

func (h Handler) coopCorrect(c *gin.Context) {
	ref, ok := refParam(c, "ref", app.ErrCoopBoundaryNotFound)
	if !ok {
		return
	}
	var req struct {
		coopShapeReq
		SupersedesID     *string `json:"supersedes_id"`
		CorrectionReason string  `json:"correction_reason"`
	}
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	in := app.CoopCorrectInput{
		CooperativeRef: ref, SupersedesID: f.ref("supersedes_id", req.SupersedesID), Reason: req.CorrectionReason,
		Shape: req.input(f), IdemKey: idem(c),
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	res, err := h.Svc.CorrectCoopBoundary(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	coopWriteResult(c, res, true)
}

func (h Handler) coopStatus(c *gin.Context, fn func(*gin.Context, app.CoopStatusInput) (app.CoopWriteResult, error)) {
	ref, ok := refParam(c, "ref", app.ErrCoopBoundaryNotFound)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !bindJSON(c, &req) {
		return
	}
	res, err := fn(c, app.CoopStatusInput{CooperativeRef: ref, Reason: req.Reason, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	coopWriteResult(c, res, false)
}

func (h Handler) coopRetire(c *gin.Context) {
	h.coopStatus(c, func(c *gin.Context, in app.CoopStatusInput) (app.CoopWriteResult, error) {
		return h.Svc.RetireCoopBoundary(c.Request.Context(), in)
	})
}

func (h Handler) coopRestore(c *gin.Context) {
	h.coopStatus(c, func(c *gin.Context, in app.CoopStatusInput) (app.CoopWriteResult, error) {
		return h.Svc.RestoreCoopBoundary(c.Request.Context(), in)
	})
}

func (h Handler) coopGet(c *gin.Context) {
	ref, ok := refParam(c, "ref", app.ErrCoopBoundaryNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.GetCoopBoundary(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	versions := make([]gin.H, 0, len(d.Versions))
	for _, v := range d.Versions {
		versions = append(versions, coopBoundaryDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"cooperative": coopFeature(d.Area, nil, ""), "versions": versions})
}

func (h Handler) coopImport(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	res, err := h.Svc.ImportCoopBoundaries(c.Request.Context(), app.CoopImportInput{
		Format: c.Query("format"), FileName: c.Query("file_name"), Data: raw, Reason: c.Query("reason"), IdemKey: idem(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	feats := make([]gin.H, 0, len(res.Features))
	for _, r := range res.Features {
		feats = append(feats, gin.H{
			"index": r.Index, "name": strOrNil(r.Name), "status": r.Status, "replaced": r.Replaced, "code": strOrNil(r.Code),
			"reason": strOrNil(r.Reason), "cooperative_ref": refOrNil(r.CooperativeRef), "boundary_id": refOrNil(r.BoundaryID),
			"version": r.Version, "area_ha": decOrNil(r.AreaHa),
		})
	}
	c.JSON(http.StatusOK, gin.H{"format": res.Format, "accepted": res.Accepted, "duplicate": res.Duplicate,
		"rejected": res.Rejected, "features": feats})
}
