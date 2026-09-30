// Package handler: HTTP của pf/waterway (contracts/openapi/application.yaml và
// admin.yaml, mục "# --- pf/waterway ---").
package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API vào nhóm /v1 (đã có xác thực + idempotency).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/waterway/segments", h.layer)
	rg.GET("/waterway/routes", h.route)
	rg.GET("/waterway/networks/current", h.current)
	// Bề mặt đối tác (ADR-0020, G4-0): vai mkg_sync + cấp quyền waterway_routes.
	rg.GET("/partner/waterway/route", h.partnerRouteGet)
	rg.POST("/partner/waterway/route", h.partnerRoutePost)
}

// RegisterAdmin gắn route Admin API (MFA bắt buộc): vẽ/sửa mạng, dựng, phát hành.
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.POST("/waterway/segments", h.createSegment)
	rg.GET("/waterway/segments", h.segments)
	rg.GET("/waterway/segments/:ref", h.segment)
	rg.POST("/waterway/segments/:ref/revisions", h.reviseSegment)
	rg.POST("/waterway/structures", h.createStructure)
	rg.GET("/waterway/structures", h.structures)
	rg.POST("/waterway/structures/:ref/revisions", h.reviseStructure)
	rg.POST("/waterway/networks/builds", h.requestBuild)
	rg.GET("/waterway/networks/builds/:id", h.build)
	rg.GET("/waterway/networks", h.versions)
	rg.GET("/waterway/networks/:version", h.version)
	rg.POST("/waterway/networks/:version/publish", h.publish)
	rg.GET("/waterway/networks/:version/findings", h.findings)
}

func bindJSON(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func fieldErrs(errs []problem.FieldError) error {
	p := problem.Validation
	for _, e := range errs {
		p = p.WithField(e.Field, e.Reason)
	}
	return p
}

func pathUUID(c *gin.Context, name string, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}

func pathVersion(c *gin.Context) (int, bool) {
	n, err := strconv.Atoi(c.Param("version"))
	if err != nil || n <= 0 {
		httpx.Fail(c, app.ErrVersionNotFound)
		return 0, false
	}
	return n, true
}

func limit(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("limit"))
	return n
}

func uuidCursor(c *gin.Context) (uuid.UUID, bool) {
	id, err := app.DecodeUUIDCursor(c.Query("cursor"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
		return id, false
	}
	return id, true
}

func intCursor(c *gin.Context) (int64, bool) {
	n, err := app.DecodeIntCursor(c.Query("cursor"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
		return 0, false
	}
	return n, true
}

func minLoad(c *gin.Context) (decimal.Decimal, bool) {
	s := c.Query("min_load_tons")
	if s == "" {
		return decimal.Zero, true
	}
	d, err := decimal.NewFromString(s)
	if err != nil || d.IsNegative() {
		httpx.Fail(c, problem.Validation.WithField("min_load_tons", "Tải trọng phải là số không âm."))
		return decimal.Zero, false
	}
	return d, true
}

func queryVersion(c *gin.Context) (int, bool) {
	s := c.Query("version")
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		httpx.Fail(c, problem.Validation.WithField("version", "phải là số nguyên dương"))
		return 0, false
	}
	return n, true
}

func parseFloats(s string, n int) ([]float64, bool) {
	parts := strings.Split(s, ",")
	if len(parts) != n {
		return nil, false
	}
	out := make([]float64, n)
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// ---------------------------------------------------------------------------
// Application API

// layer: GET /v1/waterway/segments?bbox=minLng,minLat,maxLng,maxLat&min_load_tons=&version= (GIS US-WATER-01).
func (h Handler) layer(c *gin.Context) {
	var box *store.BBox
	if s := c.Query("bbox"); s != "" {
		v, ok := parseFloats(s, 4)
		if !ok || v[0] > v[2] || v[1] > v[3] {
			httpx.Fail(c, problem.Validation.WithField("bbox", "định dạng minLng,minLat,maxLng,maxLat"))
			return
		}
		box = &store.BBox{MinLng: v[0], MinLat: v[1], MaxLng: v[2], MaxLat: v[3]}
	}
	ml, ok := minLoad(c)
	if !ok {
		return
	}
	ver, ok := queryVersion(c)
	if !ok {
		return
	}
	after, ok := uuidCursor(c)
	if !ok {
		return
	}
	res, err := h.Svc.SegmentLayer(c.Request.Context(), box, ml, ver, after, limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]layerSegmentDTO, 0, len(res.Items))
	for _, s := range res.Items {
		items = append(items, toLayerSegment(s))
	}
	body := gin.H{"items": items, "next_cursor": res.Next, "total": res.Total, "passable_count": res.PassableCount,
		"min_load_tons": ml.String(), "version_no": nil, "message": nil}
	if res.Version != nil {
		body["version_no"] = res.Version.VersionNo
	}
	if ml.IsPositive() && res.PassableCount == 0 {
		// GIS US-WATER-01 AC-2: không đánh dấu tuyến nào, báo không tìm thấy tuyến phù hợp.
		body["message"] = "Không tìm thấy tuyến sông/kênh phù hợp với tải trọng " + ml.String() + " tấn."
	}
	c.JSON(http.StatusOK, body)
}

// route: GET /v1/waterway/routes?from=lat,lng&to=lat,lng&min_load_tons=&version=.
func (h Handler) route(c *gin.Context) {
	from, ok1 := parseFloats(c.Query("from"), 2)
	to, ok2 := parseFloats(c.Query("to"), 2)
	if !ok1 || !ok2 {
		p := problem.Validation
		if !ok1 {
			p = p.WithField("from", "định dạng lat,lng")
		}
		if !ok2 {
			p = p.WithField("to", "định dạng lat,lng")
		}
		httpx.Fail(c, p)
		return
	}
	ml, ok := minLoad(c)
	if !ok {
		return
	}
	ver, ok := queryVersion(c)
	if !ok {
		return
	}
	r, err := h.Svc.Route(c.Request.Context(), port.LatLng{Lat: from[0], Lng: from[1]}, port.LatLng{Lat: to[0], Lng: to[1]}, ml, ver)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toRoute(r))
}

func (h Handler) current(c *gin.Context) {
	v, err := h.Svc.CurrentPublished(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if v == nil {
		httpx.Fail(c, app.ErrVersionNotFound.WithDetail("chưa có phiên bản mạng được phát hành"))
		return
	}
	c.JSON(http.StatusOK, toVersion(*v))
}

// ---------------------------------------------------------------------------
// Admin API: đoạn

func (h Handler) createSegment(c *gin.Context) {
	var req segmentReq
	if !bindJSON(c, &req) {
		return
	}
	f, errs := req.fields()
	if len(errs) > 0 {
		httpx.Fail(c, fieldErrs(errs))
		return
	}
	s, err := h.Svc.CreateSegment(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/waterway/segments/"+s.SegmentRef.String())
	c.JSON(http.StatusCreated, toSegment(s))
}

func (h Handler) reviseSegment(c *gin.Context) {
	ref, ok := pathUUID(c, "ref", app.ErrSegmentNotFound)
	if !ok {
		return
	}
	var req segmentReq
	if !bindJSON(c, &req) {
		return
	}
	f, errs := req.fields()
	if len(errs) > 0 {
		httpx.Fail(c, fieldErrs(errs))
		return
	}
	s, err := h.Svc.ReviseSegment(c.Request.Context(), ref, f, app.Revision{ExpectedRevision: req.ExpectedRevision, Reason: req.Reason})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toSegment(s))
}

func (h Handler) segment(c *gin.Context) {
	ref, ok := pathUUID(c, "ref", app.ErrSegmentNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.Segment(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	hist := make([]segmentDTO, 0, len(d.History))
	for _, s := range d.History {
		hist = append(hist, toSegment(s))
	}
	c.JSON(http.StatusOK, gin.H{"head": toSegment(d.Head), "history": hist})
}

func (h Handler) segments(c *gin.Context) {
	after, ok := uuidCursor(c)
	if !ok {
		return
	}
	pg, err := h.Svc.Segments(c.Request.Context(), c.Query("status"), after, limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]segmentDTO, 0, len(pg.Items))
	for _, s := range pg.Items {
		items = append(items, toSegment(s))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": pg.Next})
}

// ---------------------------------------------------------------------------
// Admin API: công trình

func (h Handler) createStructure(c *gin.Context) {
	var req structureReq
	if !bindJSON(c, &req) {
		return
	}
	f, errs := req.fields()
	if len(errs) > 0 {
		httpx.Fail(c, fieldErrs(errs))
		return
	}
	s, err := h.Svc.CreateStructure(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toStructure(s))
}

func (h Handler) reviseStructure(c *gin.Context) {
	ref, ok := pathUUID(c, "ref", app.ErrStructureNotFound)
	if !ok {
		return
	}
	var req structureReq
	if !bindJSON(c, &req) {
		return
	}
	f, errs := req.fields()
	if len(errs) > 0 {
		httpx.Fail(c, fieldErrs(errs))
		return
	}
	s, err := h.Svc.ReviseStructure(c.Request.Context(), ref, f, app.Revision{ExpectedRevision: req.ExpectedRevision, Reason: req.Reason})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toStructure(s))
}

func (h Handler) structures(c *gin.Context) {
	after, ok := uuidCursor(c)
	if !ok {
		return
	}
	var seg *uuid.UUID
	if s := c.Query("segment_ref"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("segment_ref", "uuid không hợp lệ"))
			return
		}
		seg = &id
	}
	pg, err := h.Svc.Structures(c.Request.Context(), seg, after, limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]structureDTO, 0, len(pg.Items))
	for _, s := range pg.Items {
		items = append(items, toStructure(s))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": pg.Next})
}

// ---------------------------------------------------------------------------
// Admin API: dựng và phát hành

func (h Handler) requestBuild(c *gin.Context) {
	j, _, err := h.Svc.RequestBuild(c.Request.Context(), idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/waterway/networks/builds/"+j.ID.String())
	c.JSON(http.StatusAccepted, toBuild(j))
}

func (h Handler) build(c *gin.Context) {
	id, ok := pathUUID(c, "id", app.ErrBuildNotFound)
	if !ok {
		return
	}
	j, err := h.Svc.Build(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toBuild(j))
}

func (h Handler) versions(c *gin.Context) {
	before, ok := intCursor(c)
	if !ok {
		return
	}
	items, next, err := h.Svc.Versions(c.Request.Context(), int(before), limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]versionDTO, 0, len(items))
	for _, v := range items {
		out = append(out, toVersion(v))
	}
	var cur *string
	if next != nil {
		s := app.EncodeIntCursor(int64(*next))
		cur = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "next_cursor": cur})
}

func (h Handler) version(c *gin.Context) {
	no, ok := pathVersion(c)
	if !ok {
		return
	}
	v, err := h.Svc.Version(c.Request.Context(), no)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toVersion(v))
}

func (h Handler) publish(c *gin.Context) {
	no, ok := pathVersion(c)
	if !ok {
		return
	}
	v, err := h.Svc.Publish(c.Request.Context(), no)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toVersion(v))
}

func (h Handler) findings(c *gin.Context) {
	no, ok := pathVersion(c)
	if !ok {
		return
	}
	after, ok := intCursor(c)
	if !ok {
		return
	}
	items, next, err := h.Svc.Findings(c.Request.Context(), no, c.Query("kind"), after, limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]findingDTO, 0, len(items))
	for _, f := range items {
		out = append(out, toFinding(f))
	}
	var cur *string
	if next != nil {
		s := app.EncodeIntCursor(*next)
		cur = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "next_cursor": cur})
}
