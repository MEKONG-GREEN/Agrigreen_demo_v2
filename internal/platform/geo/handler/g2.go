package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// registerG2App gắn route Application API của G2: đọc lớp nền (ranh giới HTX,
// kho/Hub + widget sức chứa, đường bộ, lớp gộp), ghi ranh giới HTX, đọc log GPS.
func (h Handler) registerG2App(rg *gin.RouterGroup) {
	rg.GET("/geo/layers", h.layers)
	rg.GET("/geo/cooperative-boundaries", h.coopBBox)
	rg.POST("/geo/cooperative-boundaries", h.coopCreate)
	rg.GET("/geo/cooperative-boundaries/:ref", h.coopGet)
	rg.POST("/geo/cooperative-boundaries/:ref/versions", h.coopCorrect)
	rg.POST("/geo/cooperative-boundaries/:ref/retire", h.coopRetire)
	rg.POST("/geo/cooperative-boundaries/:ref/restore", h.coopRestore)
	rg.GET("/geo/facilities", h.facilities)
	rg.GET("/geo/facilities/:ref", h.facilityGet)
	rg.GET("/geo/hubs/:ref/capacity", h.hubCapacity)
	rg.GET("/geo/capacity-widgets", h.capacityWidgets)
	rg.GET("/geo/roads", h.roads)
	rg.GET("/geo/roads/:ref", h.roadGet)
	rg.GET("/geo/gps-fixes", h.gpsFixes)
	// HTX US-GPS-03 AC-1: log chỉ đọc — mọi yêu cầu sửa/xóa bị từ chối kèm thông báo.
	for _, m := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rg.Handle(m, "/geo/gps-fixes/:id", h.gpsReadOnly)
	}
}

// registerG2Admin gắn route Admin API của G2 (MFA): nhập ranh giới HTX, quản lý
// kho/Hub và sức chứa (BR-52: chỉ Admin nhập tay), vẽ/sửa đường bộ.
func (h Handler) registerG2Admin(rg *gin.RouterGroup) {
	httpx.AllowBody(rg, http.MethodPost, "/geo/cooperative-boundaries/import", MaxImportBody)
	rg.POST("/geo/cooperative-boundaries/import", h.coopImport)
	rg.POST("/geo/facilities", h.facilityCreate)
	httpx.AllowBody(rg, http.MethodPost, "/geo/facilities/import", MaxImportBody)
	rg.POST("/geo/facilities/import", h.facilityImport)
	rg.POST("/geo/facilities/:ref/versions", h.facilityUpdate)
	rg.POST("/geo/facilities/:ref/retire", h.facilityRetire)
	rg.POST("/geo/facilities/:ref/restore", h.facilityRestore)
	rg.POST("/geo/facilities/:ref/capacity", h.capacityRecord(domain.SubjectFacility))
	rg.POST("/geo/facilities/:ref/capacity/release", h.capacityRelease(domain.SubjectFacility))
	rg.POST("/geo/hubs/:ref/capacity", h.capacityRecord(domain.SubjectHub))
	rg.POST("/geo/hubs/:ref/capacity/release", h.capacityRelease(domain.SubjectHub))
	rg.POST("/geo/capacity/import", h.capacityImport)
	rg.POST("/geo/roads", h.roadCreate)
	rg.POST("/geo/roads/:ref/versions", h.roadCorrect)
	rg.POST("/geo/roads/:ref/retire", h.roadRetire)
	rg.POST("/geo/roads/:ref/restore", h.roadRestore)
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

func decQuery(c *gin.Context, name string) (*decimal.Decimal, bool) {
	s := strings.TrimSpace(c.Query(name))
	if s == "" {
		return nil, false
	}
	d, err := decimal.NewFromString(strings.ReplaceAll(s, ",", "."))
	if err != nil {
		return nil, true
	}
	return &d, false
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

// ---------------------------------------------------------------------------
// Kho / Hub / nhà máy + sức chứa
// ---------------------------------------------------------------------------

func decStrOrNil(d *decimal.Decimal, places int32) any {
	if d == nil {
		return nil
	}
	return d.StringFixed(places)
}

func latestDTO(l store.CapacityLatest) gin.H {
	m := gin.H{"source": l.Source, "snapshot_id": l.SnapshotID.String(), "as_of": ts(l.AsOf),
		"capacity": l.Capacity.StringFixed(3), "used": l.Used.StringFixed(3), "unit": l.Unit, "released": l.Released,
		"released_at": nil}
	if l.ReleasedAt != nil {
		m["released_at"] = ts(*l.ReleasedAt)
	}
	return m
}

func capacityDTO(v app.CapacityView) gin.H {
	m := gin.H{
		"capacity": decStrOrNil(v.Capacity, 3), "used": decStrOrNil(v.Used, 3), "unit": strOrNil(v.Unit),
		"source": strOrNil(v.Source), "as_of": nil, "snapshot_id": refOrNil(v.SnapshotID),
		"fill_pct": decStrOrNil(v.FillPct, 1), "class": v.Class, "sync_differs": v.SyncDiffers, "pending_sync": nil,
	}
	if v.AsOf != nil {
		m["as_of"] = ts(*v.AsOf)
	}
	if v.Pending != nil {
		m["pending_sync"] = latestDTO(*v.Pending)
	}
	latest := make([]gin.H, 0, len(v.Latest))
	for _, l := range v.Latest {
		latest = append(latest, latestDTO(l))
	}
	m["by_source"] = latest
	return m
}

func snapshotDTO(s store.CapacitySnapshot) gin.H {
	m := gin.H{
		"id": s.ID.String(), "subject_kind": s.SubjectKind, "subject_ref": s.SubjectRef.String(), "seq": s.Seq, "action": s.Action,
		"source": s.Source, "as_of": ts(s.AsOf), "capacity": decStrOrNil(s.Capacity, 3), "used": decStrOrNil(s.Used, 3),
		"unit": strOrNil(s.Unit), "recorded_at": ts(s.RecordedAt), "supersedes_id": nil, "correction_reason": s.CorrectionReason,
		"actor_ref": s.ActorRef.String(),
	}
	if s.SupersedesID != nil {
		m["supersedes_id"] = s.SupersedesID.String()
	}
	return m
}

func facilityItemDTO(it app.FacilityItem) gin.H {
	var geom json.RawMessage
	if it.GeoJSON != "" {
		geom = json.RawMessage(it.GeoJSON)
	}
	return gin.H{
		"type": "Feature", "id": it.Ref.String(), "geometry": pointGeom(it.Lon, it.Lat),
		"properties": gin.H{
			"origin": it.Origin, "ref": it.Ref.String(), "kind": it.Kind, "code": it.Code, "name": it.Name,
			"province_code": strOrNil(it.ProvinceCode), "commune_code": strOrNil(it.CommuneCode),
			"nominal_capacity": decStrOrNil(it.Nominal, 3), "unit": it.Unit, "status": it.Status, "version": it.Version,
			"footprint": geom, "capacity": capacityDTO(it.Capacity),
		},
	}
}

func facilityDTO(f store.Facility) gin.H {
	var geom json.RawMessage
	if f.GeoJSON != "" {
		geom = json.RawMessage(f.GeoJSON)
	}
	return gin.H{
		"type": "Feature", "id": f.Ref.String(), "geometry": pointGeom(f.Lon, f.Lat),
		"properties": gin.H{
			"origin": app.OriginTenant, "ref": f.Ref.String(), "kind": f.Kind, "code": f.Code, "name": f.Name,
			"province_code": f.ProvinceCode, "commune_code": strOrNil(f.CommuneCode), "nominal_capacity": f.Capacity.StringFixed(3),
			"unit": f.Unit, "status": f.Status, "status_reason": strOrNil(f.StatusReason), "version": f.CurrentVersion,
			"current_event_id": f.CurrentEventID.String(), "area_ha": decOrNil(f.AreaHa), "footprint": geom,
			"revision": f.Revision, "created_at": ts(f.CreatedAt), "updated_at": ts(f.UpdatedAt),
		},
	}
}

func facilityEventDTO(e store.FacilityEvent) gin.H {
	m := gin.H{
		"id": e.ID.String(), "facility_ref": e.FacilityRef.String(), "version": e.Version, "action": e.Action, "kind": e.Kind,
		"code": e.Code, "name": e.Name, "lon": f7(e.Lon), "lat": f7(e.Lat), "has_polygon": e.HasPolygon,
		"province_code": e.ProvinceCode, "commune_code": strOrNil(e.CommuneCode), "capacity": e.Capacity.StringFixed(3),
		"unit": e.Unit, "status": e.Status, "event_time": ts(e.EventTime), "recorded_at": ts(e.RecordedAt),
		"supersedes_id": nil, "correction_reason": e.CorrectionReason, "actor_ref": e.ActorRef.String(),
	}
	if e.SupersedesID != nil {
		m["supersedes_id"] = e.SupersedesID.String()
	}
	return m
}

func (h Handler) facilityListInput(c *gin.Context) (app.FacilityListInput, bool) {
	bb, ok := bboxInput(c, 12)
	if !ok {
		return app.FacilityListInput{}, false
	}
	return app.FacilityListInput{BBoxInput: bb, Kind: c.Query("kind"), IncludeRetired: c.Query("include_retired") == "true"}, true
}

func facilityPageDTO(pg app.FacilityPage, limit int) gin.H {
	feats := make([]gin.H, 0, len(pg.Items))
	for _, it := range pg.Items {
		feats = append(feats, facilityItemDTO(it))
	}
	if limit == 0 {
		limit = app.MaxViewportPlots
	}
	return gin.H{"type": "FeatureCollection", "features": feats, "truncated": pg.Truncated, "limit": limit,
		"thresholds": gin.H{"warn_pct": pg.WarnPct.String(), "full_pct": pg.FullPct.String()}}
}

func (h Handler) facilities(c *gin.Context) {
	in, ok := h.facilityListInput(c)
	if !ok {
		return
	}
	pg, err := h.Svc.FacilitiesInBBox(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, facilityPageDTO(pg, in.Limit))
}

// capacityWidgets: GIS US-HUB-01 — marker kèm % và lớp màu, gọn hơn lớp cơ sở.
func (h Handler) capacityWidgets(c *gin.Context) {
	in, ok := h.facilityListInput(c)
	if !ok {
		return
	}
	pg, err := h.Svc.CapacityWidgets(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(pg.Items))
	for _, it := range pg.Items {
		v := it.Capacity
		items = append(items, gin.H{
			"origin": it.Origin, "ref": it.Ref.String(), "kind": it.Kind, "code": it.Code, "name": it.Name,
			"lon": f7(it.Lon), "lat": f7(it.Lat), "capacity": decStrOrNil(v.Capacity, 3), "used": decStrOrNil(v.Used, 3),
			"free": freeOf(v), "unit": strOrNil(v.Unit), "fill_pct": decStrOrNil(v.FillPct, 1), "class": v.Class,
			"source": strOrNil(v.Source), "sync_differs": v.SyncDiffers,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "truncated": pg.Truncated,
		"thresholds": gin.H{"warn_pct": pg.WarnPct.String(), "full_pct": pg.FullPct.String()}})
}

func freeOf(v app.CapacityView) any {
	if v.Capacity == nil || v.Used == nil {
		return nil
	}
	f := decimal.Max(decimal.Zero, v.Capacity.Sub(*v.Used))
	return f.StringFixed(3)
}

func (h Handler) facilityGet(c *gin.Context) {
	ref, ok := refParam(c, "ref", app.ErrFacilityNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.GetFacility(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	events := make([]gin.H, 0, len(d.Events))
	for _, e := range d.Events {
		events = append(events, facilityEventDTO(e))
	}
	hist := make([]gin.H, 0, len(d.History))
	for _, s := range d.History {
		hist = append(hist, snapshotDTO(s))
	}
	c.JSON(http.StatusOK, gin.H{"facility": facilityDTO(d.Facility), "events": events, "capacity": capacityDTO(d.Capacity),
		"capacity_history": hist})
}

func (h Handler) hubCapacity(c *gin.Context) {
	ref, ok := refParam(c, "ref", app.ErrHubNotFound)
	if !ok {
		return
	}
	v, hist, err := h.Svc.SubjectCapacity(c.Request.Context(), domain.SubjectHub, ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(hist))
	for _, s := range hist {
		items = append(items, snapshotDTO(s))
	}
	c.JSON(http.StatusOK, gin.H{"hub_ref": ref.String(), "capacity": capacityDTO(v), "capacity_history": items})
}

type facilityReq struct {
	Ref              *string         `json:"ref"`
	Kind             *string         `json:"kind"`
	Code             *string         `json:"code"`
	Name             *string         `json:"name"`
	Point            json.RawMessage `json:"point"`
	Polygon          json.RawMessage `json:"polygon"`
	RemovePolygon    bool            `json:"remove_polygon"`
	ProvinceCode     *string         `json:"province_code"`
	CommuneCode      *string         `json:"commune_code"`
	Capacity         json.RawMessage `json:"capacity"`
	Unit             *string         `json:"unit"`
	EventTime        *string         `json:"event_time"`
	DeviceID         *string         `json:"device_id"`
	SupersedesID     *string         `json:"supersedes_id"`
	CorrectionReason string          `json:"correction_reason"`
}

func (r facilityReq) input(f *fieldCollector, c *gin.Context) app.FacilityInput {
	capacity, bad := app.ParseDecimalJSON(r.Capacity)
	return app.FacilityInput{
		Ref: f.v7("ref", r.Ref), Kind: r.Kind, Code: r.Code, Name: r.Name, Point: r.Point,
		Polygon: r.Polygon, RemovePoly: r.RemovePolygon, ProvinceCode: r.ProvinceCode, CommuneCode: r.CommuneCode,
		Capacity: capacity, CapacityBad: bad, Unit: r.Unit, EventTime: f.at("event_time", r.EventTime), DeviceID: r.DeviceID,
		SupersedesID: f.ref("supersedes_id", r.SupersedesID), Reason: r.CorrectionReason, IdemKey: idem(c),
	}
}

func facilityWriteResult(c *gin.Context, res app.FacilityWriteResult, created bool) {
	status := http.StatusOK
	if created && !res.Replayed {
		status = http.StatusCreated
	}
	c.Header("Location", "/v1/geo/facilities/"+res.Facility.Ref.String())
	c.JSON(status, gin.H{"facility": facilityDTO(res.Facility), "event": facilityEventDTO(res.Event)})
}

func (h Handler) facilityCreate(c *gin.Context) {
	var req facilityReq
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	in := req.input(f, c)
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	res, err := h.Svc.CreateFacility(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	facilityWriteResult(c, res, true)
}

func (h Handler) facilityUpdate(c *gin.Context) {
	ref, ok := refParam(c, "ref", app.ErrFacilityNotFound)
	if !ok {
		return
	}
	var req facilityReq
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	in := req.input(f, c)
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	res, err := h.Svc.UpdateFacility(c.Request.Context(), ref, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	facilityWriteResult(c, res, true)
}

func (h Handler) facilityStatus(c *gin.Context, fn func(*gin.Context, app.FacilityStatusInput) (app.FacilityWriteResult, error)) {
	ref, ok := refParam(c, "ref", app.ErrFacilityNotFound)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !bindJSON(c, &req) {
		return
	}
	res, err := fn(c, app.FacilityStatusInput{Ref: ref, Reason: req.Reason, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	facilityWriteResult(c, res, false)
}

func (h Handler) facilityRetire(c *gin.Context) {
	h.facilityStatus(c, func(c *gin.Context, in app.FacilityStatusInput) (app.FacilityWriteResult, error) {
		return h.Svc.RetireFacility(c.Request.Context(), in)
	})
}

func (h Handler) facilityRestore(c *gin.Context) {
	h.facilityStatus(c, func(c *gin.Context, in app.FacilityStatusInput) (app.FacilityWriteResult, error) {
		return h.Svc.RestoreFacility(c.Request.Context(), in)
	})
}

func (h Handler) facilityImport(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	res, err := h.Svc.ImportFacilities(c.Request.Context(), app.FacilityImportInput{
		Format: c.Query("format"), FileName: c.Query("file_name"), Data: raw, Kind: c.Query("kind"),
		ProvinceCode: c.Query("province_code"), CommuneCode: c.Query("commune_code"), Unit: c.Query("unit"), IdemKey: idem(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	feats := make([]gin.H, 0, len(res.Features))
	for _, r := range res.Features {
		feats = append(feats, gin.H{"index": r.Index, "name": strOrNil(r.Name), "status": r.Status, "code": strOrNil(r.Code),
			"reason": strOrNil(r.Reason), "facility_ref": refOrNil(r.FacilityRef), "facility_code": strOrNil(r.FacilityCode)})
	}
	c.JSON(http.StatusOK, gin.H{"format": res.Format, "accepted": res.Accepted, "duplicate": res.Duplicate,
		"rejected": res.Rejected, "features": feats})
}

type capacityReq struct {
	Capacity         json.RawMessage `json:"capacity"`
	Used             json.RawMessage `json:"used"`
	Unit             string          `json:"unit"`
	AsOf             *string         `json:"as_of"`
	SupersedesID     *string         `json:"supersedes_id"`
	CorrectionReason string          `json:"correction_reason"`
}

func (h Handler) capacityRecord(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		nf := app.ErrFacilityNotFound
		if kind == domain.SubjectHub {
			nf = app.ErrHubNotFound
		}
		ref, ok := refParam(c, "ref", nf)
		if !ok {
			return
		}
		var req capacityReq
		if !bindJSON(c, &req) {
			return
		}
		f := newCollector()
		capacity, bad := app.ParseDecimalJSON(req.Capacity)
		used, badUsed := app.ParseDecimalJSON(req.Used)
		if badUsed {
			f.add("used", "phải là số")
		}
		in := app.CapacityInput{
			SubjectKind: kind, SubjectRef: ref, Capacity: capacity, CapacityBad: bad, Used: used, Unit: strings.TrimSpace(req.Unit),
			AsOf: f.at("as_of", req.AsOf), SupersedesID: f.ref("supersedes_id", req.SupersedesID), Reason: req.CorrectionReason,
			IdemKey: idem(c),
		}
		if f.bad {
			httpx.Fail(c, f.p)
			return
		}
		res, err := h.Svc.RecordCapacity(c.Request.Context(), in)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		status := http.StatusCreated
		if res.Replayed {
			status = http.StatusOK
		}
		c.JSON(status, gin.H{"snapshot": snapshotDTO(res.Snapshot), "capacity": capacityDTO(res.Capacity)})
	}
}

func (h Handler) capacityRelease(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		nf := app.ErrFacilityNotFound
		if kind == domain.SubjectHub {
			nf = app.ErrHubNotFound
		}
		ref, ok := refParam(c, "ref", nf)
		if !ok {
			return
		}
		var req struct {
			Reason string `json:"reason"`
		}
		if !bindJSON(c, &req) {
			return
		}
		res, err := h.Svc.ReleaseCapacity(c.Request.Context(), app.ReleaseInput{SubjectKind: kind, SubjectRef: ref, Reason: req.Reason, IdemKey: idem(c)})
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"snapshot": snapshotDTO(res.Snapshot), "capacity": capacityDTO(res.Capacity)})
	}
}

func (h Handler) capacityImport(c *gin.Context) {
	var req struct {
		Items []struct {
			FacilityCode string          `json:"facility_code"`
			HubRef       *string         `json:"hub_ref"`
			Capacity     json.RawMessage `json:"capacity"`
			Used         json.RawMessage `json:"used"`
			Unit         string          `json:"unit"`
			AsOf         *string         `json:"as_of"`
		} `json:"items"`
	}
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	items := make([]app.CapacityImportItem, 0, len(req.Items))
	for i, it := range req.Items {
		capacity, bad := app.ParseDecimalJSON(it.Capacity)
		used, badUsed := app.ParseDecimalJSON(it.Used)
		if badUsed {
			f.add("items["+strconv.Itoa(i)+"].used", "phải là số")
		}
		items = append(items, app.CapacityImportItem{
			FacilityCode: it.FacilityCode, HubRef: f.ref("items["+strconv.Itoa(i)+"].hub_ref", it.HubRef), Capacity: capacity,
			CapacityBad: bad, Used: used, Unit: strings.TrimSpace(it.Unit), AsOf: f.at("items["+strconv.Itoa(i)+"].as_of", it.AsOf),
		})
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	rows, err := h.Svc.ImportCapacities(c.Request.Context(), items, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(rows))
	counts := map[string]int{app.FeatureAccepted: 0, app.FeatureDuplicate: 0, app.FeatureRejected: 0}
	for _, r := range rows {
		counts[r.Status]++
		out = append(out, gin.H{"index": r.Index, "status": r.Status, "code": strOrNil(r.Code), "reason": strOrNil(r.Reason),
			"snapshot_id": refOrNil(r.SnapshotID)})
	}
	c.JSON(http.StatusOK, gin.H{"accepted": counts[app.FeatureAccepted], "duplicate": counts[app.FeatureDuplicate],
		"rejected": counts[app.FeatureRejected], "items": out})
}

// ---------------------------------------------------------------------------
// Đường bộ
// ---------------------------------------------------------------------------

func roadFeature(r store.Road) gin.H {
	return gin.H{
		"type": "Feature", "id": r.Ref.String(), "geometry": json.RawMessage(r.GeoJSON),
		"properties": gin.H{
			"ref": r.Ref.String(), "name": r.Name, "road_class": r.RoadClass, "max_load_tons": r.MaxLoadTons.StringFixed(3),
			"length_m": r.LengthM.StringFixed(2), "province_code": r.ProvinceCode, "status": r.Status,
			"status_reason": strOrNil(r.StatusReason), "version": r.CurrentVersion, "segment_id": r.CurrentSegmentID.String(),
			"revision": r.Revision, "created_at": ts(r.CreatedAt), "updated_at": ts(r.UpdatedAt),
		},
	}
}

func roadSegmentDTO(s store.RoadSegment) gin.H {
	m := gin.H{
		"segment_id": s.ID.String(), "road_ref": s.RoadRef.String(), "version": s.Version, "action": s.Action, "name": s.Name,
		"road_class": s.RoadClass, "max_load_tons": s.MaxLoadTons.StringFixed(3), "length_m": s.LengthM.StringFixed(2),
		"point_count": s.PointCount, "province_code": s.ProvinceCode, "event_time": ts(s.EventTime),
		"recorded_at": ts(s.RecordedAt), "supersedes_id": nil, "correction_reason": s.CorrectionReason,
		"actor_ref": s.ActorRef.String(),
	}
	if s.SupersedesID != nil {
		m["supersedes_id"] = s.SupersedesID.String()
	}
	if s.GeoJSON != "" {
		m["geometry"] = json.RawMessage(s.GeoJSON)
	}
	return m
}

func roadPageDTO(pg app.RoadPage, limit int) gin.H {
	feats := make([]gin.H, 0, len(pg.Items))
	for _, r := range pg.Items {
		feats = append(feats, roadFeature(r))
	}
	if limit == 0 {
		limit = app.MaxViewportPlots
	}
	return gin.H{"type": "FeatureCollection", "features": feats, "truncated": pg.Truncated, "limit": limit,
		"matched": len(pg.Items), "none_matched": pg.NoneMatched, "notice": strOrNil(pg.Notice)}
}

func (h Handler) roads(c *gin.Context) {
	bb, ok := bboxInput(c, 17)
	if !ok {
		return
	}
	minLoad, bad := decQuery(c, "min_load_tons")
	pg, err := h.Svc.RoadsInBBox(c.Request.Context(), app.RoadQueryInput{
		BBoxInput: bb, MinLoadTons: minLoad, MinLoadBad: bad, RoadClass: c.Query("road_class"),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, roadPageDTO(pg, bb.Limit))
}

func (h Handler) roadGet(c *gin.Context) {
	ref, ok := refParam(c, "ref", app.ErrRoadNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.GetRoad(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	versions := make([]gin.H, 0, len(d.Versions))
	for _, v := range d.Versions {
		versions = append(versions, roadSegmentDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"road": roadFeature(d.Road), "versions": versions})
}

type roadReq struct {
	Ref              *string         `json:"ref"`
	Name             *string         `json:"name"`
	RoadClass        *string         `json:"road_class"`
	MaxLoadTons      json.RawMessage `json:"max_load_tons"`
	Geometry         json.RawMessage `json:"geometry"`
	ProvinceCode     *string         `json:"province_code"`
	EventTime        *string         `json:"event_time"`
	DeviceID         *string         `json:"device_id"`
	SupersedesID     *string         `json:"supersedes_id"`
	CorrectionReason string          `json:"correction_reason"`
}

func (r roadReq) input(f *fieldCollector, c *gin.Context) app.RoadInput {
	load, bad := app.ParseDecimalJSON(r.MaxLoadTons)
	return app.RoadInput{
		Ref: f.v7("ref", r.Ref), Name: r.Name, RoadClass: r.RoadClass, MaxLoadTons: load, MaxLoadBad: bad, Geometry: r.Geometry,
		ProvinceCode: r.ProvinceCode, EventTime: f.at("event_time", r.EventTime), DeviceID: r.DeviceID,
		SupersedesID: f.ref("supersedes_id", r.SupersedesID), Reason: r.CorrectionReason, IdemKey: idem(c),
	}
}

func roadWriteResult(c *gin.Context, res app.RoadWriteResult, created bool) {
	status := http.StatusOK
	if created && !res.Replayed {
		status = http.StatusCreated
	}
	c.Header("Location", "/v1/geo/roads/"+res.Road.Ref.String())
	c.JSON(status, gin.H{"road": roadFeature(res.Road), "segment": roadSegmentDTO(res.Segment)})
}

func (h Handler) roadCreate(c *gin.Context) {
	var req roadReq
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	in := req.input(f, c)
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	res, err := h.Svc.CreateRoad(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	roadWriteResult(c, res, true)
}

func (h Handler) roadCorrect(c *gin.Context) {
	ref, ok := refParam(c, "ref", app.ErrRoadNotFound)
	if !ok {
		return
	}
	var req roadReq
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	in := req.input(f, c)
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	res, err := h.Svc.CorrectRoad(c.Request.Context(), ref, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	roadWriteResult(c, res, true)
}

func (h Handler) roadStatus(c *gin.Context, fn func(*gin.Context, app.RoadStatusInput) (app.RoadWriteResult, error)) {
	ref, ok := refParam(c, "ref", app.ErrRoadNotFound)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !bindJSON(c, &req) {
		return
	}
	res, err := fn(c, app.RoadStatusInput{Ref: ref, Reason: req.Reason, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	roadWriteResult(c, res, false)
}

func (h Handler) roadRetire(c *gin.Context) {
	h.roadStatus(c, func(c *gin.Context, in app.RoadStatusInput) (app.RoadWriteResult, error) {
		return h.Svc.RetireRoad(c.Request.Context(), in)
	})
}

func (h Handler) roadRestore(c *gin.Context) {
	h.roadStatus(c, func(c *gin.Context, in app.RoadStatusInput) (app.RoadWriteResult, error) {
		return h.Svc.RestoreRoad(c.Request.Context(), in)
	})
}

// ---------------------------------------------------------------------------
// Lớp gộp
// ---------------------------------------------------------------------------

func (h Handler) layers(c *gin.Context) {
	bb, ok := bboxInput(c, 12)
	if !ok {
		return
	}
	want := map[string]bool{}
	for _, l := range strings.Split(c.DefaultQuery("layers", "cooperatives,plots,facilities,roads"), ",") {
		if l = strings.TrimSpace(l); l != "" {
			want[l] = true
		}
	}
	minLoad, bad := decQuery(c, "min_load_tons")
	out, err := h.Svc.Layers(c.Request.Context(), app.LayersInput{BBoxInput: bb, Layers: want, MinLoadTons: minLoad, MinLoadBad: bad})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	res := gin.H{"zoom": bb.Zoom, "plot_min_zoom": out.PlotMinZoom, "plots_notice": strOrNil(out.PlotsNotice),
		"cooperatives": nil, "plots": nil, "facilities": nil, "roads": nil}
	if out.Cooperatives != nil {
		res["cooperatives"] = coopLayerDTO(*out.Cooperatives, bb.Limit)
	}
	if out.Plots != nil {
		res["plots"] = gin.H{"type": "FeatureCollection", "features": featureCollection(out.Plots.Items), "truncated": out.Plots.Truncated}
	}
	if out.Facilities != nil {
		res["facilities"] = facilityPageDTO(*out.Facilities, bb.Limit)
	}
	if out.Roads != nil {
		res["roads"] = roadPageDTO(*out.Roads, bb.Limit)
	}
	c.JSON(http.StatusOK, res)
}

// ---------------------------------------------------------------------------
// Log GPS (HTX US-GPS-03)
// ---------------------------------------------------------------------------

func gpsDTO(g store.GpsFix) gin.H {
	var point any
	if g.Lon != nil && g.Lat != nil {
		point = pointGeom(*g.Lon, *g.Lat)
	}
	return gin.H{
		"id": g.ID.String(), "seq": g.Seq, "actor_ref": g.ActorRef.String(), "device_id": strOrNil(g.DeviceID),
		"purpose": g.Purpose, "target_ref": refOrNil(g.TargetRef), "plot_ref": refOrNil(g.PlotRef),
		"commune_code": strOrNil(g.CommuneCode), "cooperative_ref": refOrNil(g.CooperativeRef), "point": point,
		"accuracy_m": decStrOrNil(g.AccuracyM, 2), "source": g.Source, "distance_to_plot_m": decStrOrNil(g.DistanceToPlotM, 1),
		"captured_at": ts(g.EventTime), "recorded_at": ts(g.RecordedAt),
	}
}

func (h Handler) gpsFixes(c *gin.Context) {
	f := newCollector()
	q := func(name string) uuid.UUID {
		v := c.Query(name)
		return f.ref(name, &v)
	}
	in := app.GpsQueryInput{ActorRef: q("actor_ref"), TargetRef: q("target_ref"), PlotRef: q("plot_ref"), Purpose: c.Query("purpose")}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	var err error
	if in.Before, err = decodeCursor(c); err != nil {
		httpx.Fail(c, err)
		return
	}
	if in.Limit, err = intQuery(c, "limit", 50); err != nil {
		httpx.Fail(c, err)
		return
	}
	pg, err := h.Svc.ListGpsFixes(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(pg.Items))
	for _, g := range pg.Items {
		items = append(items, gpsDTO(g))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
}

func (h Handler) gpsReadOnly(c *gin.Context) {
	c.Header("Allow", http.MethodGet)
	httpx.Fail(c, app.ErrGpsLogReadOnly.WithDetail(domain.MsgGpsReadOnly))
}
