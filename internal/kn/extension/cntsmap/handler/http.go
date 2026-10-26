// Package handler: route /v1/extension/cnts/map/… (W11.52 — bản đồ vùng nuôi, vùng dịch CNTS). Mọi route cần header
// X-AG-Context (ngữ cảnh phiên KN); thao tác ghi (POST) cần Idempotency-Key. Sửa là POST lên tài nguyên kèm
// base_version và lý do.
package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/cnts/map")
	g.GET("/layers", h.layers)
	g.GET("/zones", h.listZones)
	g.POST("/zones", h.createZone)
	g.POST("/zones/from-outbreak", h.fromOutbreak)
	g.GET("/zones/:ref", h.getZone)
	g.POST("/zones/:ref", h.updateZone)
	g.POST("/zones/:ref/retire", h.zoneStatus(true))
	g.POST("/zones/:ref/restore", h.zoneStatus(false))
	g.GET("/zones/:ref/contents", h.contents)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func bind(c *gin.Context, v any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("thân JSON không hợp lệ: "+err.Error()))
		return false
	}
	return true
}

func parseRef(field string, s *string, bad *problem.Problem) (*uuid.UUID, *problem.Problem) {
	if s == nil || *s == "" {
		return nil, bad
	}
	r, err := uuid.Parse(*s)
	if err != nil {
		if bad == nil {
			bad = problem.Validation
		}
		return nil, bad.WithField(field, "phải là UUID")
	}
	return &r, bad
}

func parseDate(field, s string, bad *problem.Problem) (*time.Time, *problem.Problem) {
	if s == "" {
		return nil, bad
	}
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		if bad == nil {
			bad = problem.Validation
		}
		return nil, bad.WithField(field, "dạng YYYY-MM-DD")
	}
	return &d, bad
}

func pathRef(c *gin.Context) (uuid.UUID, bool) {
	r, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrZoneNotFound)
		return uuid.Nil, false
	}
	return r, true
}

func optStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func optRef(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return u.String()
}

func status(replayed bool, created int) int {
	if replayed {
		return http.StatusOK
	}
	return created
}

func strs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func zoneCore(z store.Zone) gin.H {
	var center, radius, area any
	if z.Center != nil {
		center = gin.H{"lat": z.Center.Lat, "lng": z.Center.Lng}
	}
	if z.RadiusM != nil {
		radius = *z.RadiusM
	}
	if z.AreaHa != nil {
		area = z.AreaHa.String()
	}
	return gin.H{"ref": z.Ref.String(), "version": z.Seq, "event": z.Event, "status": domain.Status(z.Event),
		"province_code": z.Province, "communes": strs(z.Communes), "kind": z.Kind, "kind_label": domain.KindLabel[z.Kind], "name": z.Name,
		"draw_mode": z.Mode, "geometry": json.RawMessage(z.GeoJSON), "center": center, "radius_m": radius, "area_ha": area,
		"outbreak_ref": optRef(z.OutbreakRef), "parent_zone_ref": optRef(z.ParentRef), "species_code": optStr(z.Species),
		"disease_code": optStr(z.Disease), "effective_on": z.EffectiveOn.Format(time.DateOnly), "note": optStr(z.Note),
		"reason": optStr(z.Reason), "updated_at": z.RecordedAt.UTC().Format(time.RFC3339), "updated_by": z.ActorRef.String()}
}

func zoneOut(v app.ZoneView) gin.H {
	m := zoneCore(v.Zone)
	w := v.Warnings
	if w == nil {
		w = []string{}
	}
	m["can_edit"], m["warnings"] = v.CanEdit, w
	return m
}

func outbreakOut(o app.OutbreakView, names map[string]string) gin.H {
	m := gin.H{"ref": o.Ref.String(), "version": o.Version, "status": o.Status, "commune_code": o.Commune,
		"disease_code": o.Disease, "disease_name": optStr(names[knport.CatalogCNTSDisease+":"+o.Disease]),
		"species_code": o.Species, "species_name": optStr(names[knport.CatalogCNTSSpecies+":"+o.Species]),
		"lat": o.Location.Lat, "lng": o.Location.Lng, "as_of": o.AsOf.Format(time.DateOnly), "herd_size": o.HerdSize, "dead": o.Dead}
	if o.DistanceM != nil {
		m["distance_m"] = *o.DistanceM
	}
	return m
}

func placeOut(p store.Place, names map[string]string) gin.H {
	var scale any
	if p.Scale != nil {
		scale = p.Scale.String()
	}
	m := gin.H{"kind": p.Kind, "ref": p.Ref.String(), "site_ref": p.SiteRef.String(), "project_ref": p.ProjectRef.String(),
		"project_name": optStr(p.ProjectName), "commune_code": p.Commune, "name": p.Name, "model_type_code": p.ModelType,
		"model_type_name": optStr(names[knport.CatalogCNTSModelType+":"+p.ModelType]), "scale": scale,
		"scale_unit": optStr(p.ScaleUnit), "lat": p.Location.Lat, "lng": p.Location.Lng}
	if p.DistanceM != nil {
		m["distance_m"] = *p.DistanceM
	}
	return m
}

func (h Handler) layers(c *gin.Context) {
	l, err := h.Svc.Layers(c.Request.Context(), c.Query("date"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	zones := make([]gin.H, 0, len(l.Zones))
	for _, z := range l.Zones {
		zones = append(zones, zoneOut(z))
	}
	obs := make([]gin.H, 0, len(l.Outbreaks))
	for _, o := range l.Outbreaks {
		obs = append(obs, outbreakOut(o, l.Catalog))
	}
	places := make([]gin.H, 0, len(l.Places))
	for _, p := range l.Places {
		places = append(places, placeOut(p, l.Catalog))
	}
	plots := make([]gin.H, 0, len(l.PlotStats))
	for _, p := range l.PlotStats {
		plots = append(plots, gin.H{"commune_code": p.CommuneCode, "plots": p.Plots, "area_ha": p.AreaHa.String()})
	}
	c.JSON(http.StatusOK, gin.H{"date": l.Day.Format(time.DateOnly), "can_draw": l.CanDraw, "zones": zones, "outbreaks": obs,
		"places": places, "plot_stats": plots, "plots_ready": l.PlotsReady})
}

func (h Handler) listZones(c *gin.Context) {
	var bad *problem.Problem
	ob := c.Query("outbreak_ref")
	ref, bad := parseRef("outbreak_ref", &ob, bad)
	if bad != nil {
		httpx.Fail(c, bad)
		return
	}
	list, canDraw, err := h.Svc.ListZones(c.Request.Context(), app.ZoneQuery{Day: c.Query("date"), Kind: c.Query("kind"),
		OutbreakRef: ref, WithRetired: c.Query("with_retired") == "true"})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, v := range list {
		items = append(items, zoneOut(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "can_draw": canDraw})
}

func (h Handler) getZone(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	d, err := h.Svc.GetZone(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	m := zoneOut(d.ZoneView)
	vers := make([]gin.H, 0, len(d.Versions))
	for _, v := range d.Versions {
		x := zoneCore(v)
		x["actor_display"] = optStr(d.Names[v.ActorRef])
		vers = append(vers, x)
	}
	m["versions"] = vers
	c.JSON(http.StatusOK, m)
}

type pointReq struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type zoneReq struct {
	ID            *string         `json:"id"`
	BaseVersion   *int64          `json:"base_version"`
	Reason        string          `json:"reason"`
	ProvinceCode  *string         `json:"province_code"`
	Kind          string          `json:"kind"`
	Name          string          `json:"name"`
	Communes      []string        `json:"communes"`
	DrawMode      string          `json:"draw_mode"`
	Geometry      json.RawMessage `json:"geometry"`
	Center        *pointReq       `json:"center"`
	RadiusM       *float64        `json:"radius_m"`
	OutbreakRef   *string         `json:"outbreak_ref"`
	ParentZoneRef *string         `json:"parent_zone_ref"`
	SpeciesCode   *string         `json:"species_code"`
	DiseaseCode   *string         `json:"disease_code"`
	EffectiveOn   *string         `json:"effective_on"`
	Note          *string         `json:"note"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (r zoneReq) input(c *gin.Context) (app.ZoneInput, *problem.Problem) {
	var bad *problem.Problem
	in := app.ZoneInput{BaseVersion: r.BaseVersion, Reason: r.Reason, Province: deref(r.ProvinceCode), IdemKey: idem(c),
		Zone: domain.Zone{Kind: r.Kind, Name: r.Name, Communes: r.Communes, Mode: r.DrawMode, Geometry: r.Geometry, RadiusM: r.RadiusM,
			Species: deref(r.SpeciesCode), Disease: deref(r.DiseaseCode), Note: deref(r.Note)}}
	if r.Center != nil {
		in.Center = &domain.Point{Lat: r.Center.Lat, Lng: r.Center.Lng}
	}
	in.Ref, bad = parseRef("id", r.ID, bad)
	if in.Ref != nil && in.Ref.Version() != 7 {
		bad = problem.Validation.WithField("id", "phải là UUIDv7")
	}
	in.OutbreakRef, bad = parseRef("outbreak_ref", r.OutbreakRef, bad)
	in.ParentRef, bad = parseRef("parent_zone_ref", r.ParentZoneRef, bad)
	in.EffectiveOn, bad = parseDate("effective_on", deref(r.EffectiveOn), bad)
	return in, bad
}

func (h Handler) createZone(c *gin.Context) {
	var r zoneReq
	if !bind(c, &r) {
		return
	}
	in, bad := r.input(c)
	if bad != nil {
		httpx.Fail(c, bad)
		return
	}
	v, err := h.Svc.CreateZone(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(status(v.Replayed, http.StatusCreated), zoneOut(v))
}

func (h Handler) updateZone(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var r zoneReq
	if !bind(c, &r) {
		return
	}
	r.ID = nil
	in, bad := r.input(c)
	if bad != nil {
		httpx.Fail(c, bad)
		return
	}
	in.Ref = &ref
	v, err := h.Svc.UpdateZone(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, zoneOut(v))
}

type statusReq struct {
	BaseVersion *int64 `json:"base_version"`
	Reason      string `json:"reason"`
	EffectiveOn string `json:"effective_on"`
}

func (h Handler) zoneStatus(retire bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ref, ok := pathRef(c)
		if !ok {
			return
		}
		var r statusReq
		if !bind(c, &r) {
			return
		}
		on, bad := parseDate("effective_on", r.EffectiveOn, nil)
		if bad != nil {
			httpx.Fail(c, bad)
			return
		}
		v, err := h.Svc.SetZoneStatus(c.Request.Context(), ref, retire, r.BaseVersion, r.Reason, on, idem(c))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, zoneOut(v))
	}
}

type fromOutbreakReq struct {
	OutbreakRef string             `json:"outbreak_ref"`
	Radii       map[string]float64 `json:"radii"`
	EffectiveOn string             `json:"effective_on"`
	Note        *string            `json:"note"`
}

func (h Handler) fromOutbreak(c *gin.Context) {
	var r fromOutbreakReq
	if !bind(c, &r) {
		return
	}
	ref, bad := parseRef("outbreak_ref", &r.OutbreakRef, nil)
	if ref == nil && bad == nil {
		bad = problem.Validation.WithField("outbreak_ref", "bắt buộc")
	}
	on, bad := parseDate("effective_on", r.EffectiveOn, bad)
	if bad != nil {
		httpx.Fail(c, bad)
		return
	}
	vs, err := h.Svc.ZonesFromOutbreak(c.Request.Context(), app.OutbreakZonesInput{OutbreakRef: *ref, Radii: r.Radii, EffectiveOn: on,
		Note: deref(r.Note), IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(vs))
	replayed := false
	for _, v := range vs {
		items = append(items, zoneOut(v))
		replayed = v.Replayed
	}
	c.JSON(status(replayed, http.StatusCreated), gin.H{"items": items})
}

func (h Handler) contents(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	z, err := h.Svc.Contents(c.Request.Context(), ref, c.Query("date"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	places := make([]gin.H, 0, len(z.Places))
	sites, households := 0, 0
	for _, p := range z.Places {
		places = append(places, placeOut(p, z.Catalog))
		if p.Kind == "site" {
			sites++
		} else {
			households++
		}
	}
	obs := make([]gin.H, 0, len(z.Outbreaks))
	for _, o := range z.Outbreaks {
		obs = append(obs, outbreakOut(o, z.Catalog))
	}
	c.JSON(http.StatusOK, gin.H{"date": z.Day.Format(time.DateOnly), "zone": zoneOut(z.Zone), "places": places, "outbreaks": obs,
		"site_count": sites, "household_count": households, "outbreak_count": len(obs)})
}
