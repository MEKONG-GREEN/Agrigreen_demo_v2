package handler

// HTTP xã báo giá + phân công (W11.72, pf 000145; BRD Báo giá v1.3 FN-AD-04.02, FN-AD-04.01) — Application API
// /v1/price/regions*, points*, admin-units, officers. Hợp đồng ở contracts/openapi/application.yaml, mục "# --- pf/price ---"
// (W11.72).

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// PointHandler gói Service (xã báo giá) cho HTTP.
type PointHandler struct{ Svc *app.Service }

// RegisterApp gắn route Application API của xã báo giá.
func (h PointHandler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/price/regions", h.listRegions)
	rg.POST("/price/regions", h.createRegion)
	rg.POST("/price/regions/:code/revise", h.reviseRegion)
	rg.POST("/price/regions/:code/retire", h.retireRegion)
	rg.GET("/price/points", h.listPoints)
	rg.POST("/price/points", h.createPoint)
	rg.GET("/price/points/:code", h.getPoint)
	rg.POST("/price/points/:code/revise", h.revisePoint)
	rg.POST("/price/points/:code/retire", h.retirePoint)
	rg.POST("/price/points/:code/officers", h.assignOfficer)
	rg.GET("/price/admin-units", h.adminUnits)
	rg.GET("/price/officers", h.officers)
}

func reply(c *gin.Context, status int, r app.WriteResult, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(status, writeDTO{Code: r.Code, Version: r.Version, ValidFrom: r.ValidFrom.Format(time.DateOnly), Written: r.Written})
}

func refPtr(u uuid.UUID) *string {
	if u == uuid.Nil {
		return nil
	}
	s := u.String()
	return &s
}

// ---- nhóm vùng ----

type regionDTO struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
	Version   int64  `json:"version"`
	ValidFrom string `json:"valid_from"`
	Status    string `json:"status"`
	Points    int    `json:"points"`
}

func (h PointHandler) listRegions(c *gin.Context) {
	at, ok := queryDay(c)
	if !ok {
		return
	}
	rs, err := h.Svc.ListRegions(c.Request.Context(), c.Query("profile"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]regionDTO, 0, len(rs))
	for _, r := range rs {
		out = append(out, regionDTO{Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, Version: r.Version,
			ValidFrom: r.ValidFrom.Format(time.DateOnly), Status: r.Status, Points: r.Points})
	}
	c.JSON(http.StatusOK, gin.H{"regions": out})
}

type regionReq struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

func (h PointHandler) createRegion(c *gin.Context) {
	var req regionReq
	if !bind(c, &req) {
		return
	}
	r, err := h.Svc.CreateRegion(c.Request.Context(), c.Query("profile"), app.RegionInput(req), idem(c))
	reply(c, http.StatusCreated, r, err)
}

func (h PointHandler) reviseRegion(c *gin.Context) {
	var req regionReq
	if !bind(c, &req) {
		return
	}
	req.Code = ""
	r, err := h.Svc.ReviseRegion(c.Request.Context(), c.Query("profile"), c.Param("code"), app.RegionInput(req), idem(c))
	reply(c, http.StatusCreated, r, err)
}

func (h PointHandler) retireRegion(c *gin.Context) {
	r, err := h.Svc.RetireRegion(c.Request.Context(), c.Query("profile"), c.Param("code"), idem(c))
	reply(c, http.StatusCreated, r, err)
}

// ---- xã báo giá ----

type pointDTO struct {
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	AdminUnitCode  string   `json:"admin_unit_code"`
	AdminUnitName  string   `json:"admin_unit_name"`
	AdminUnitValid bool     `json:"admin_unit_valid"`
	Successors     []string `json:"successor_codes"`
	RegionCode     string   `json:"region_code"`
	RegionName     string   `json:"region_name"`
	State          string   `json:"state"`
	Version        int64    `json:"version"`
	ValidFrom      string   `json:"valid_from"`
	Since          *string  `json:"since"`
	ValidTo        *string  `json:"valid_to"`
	Note           *string  `json:"note"`
	OfficerRef     *string  `json:"officer_ref"`
	OfficerName    *string  `json:"officer_name"`
	OfficerSince   *string  `json:"officer_since"`
	OwnerRef       *string  `json:"owner_ref"`
	OwnerName      *string  `json:"owner_name"`
	OwnerSince     *string  `json:"owner_since"`
	ItemCount      int      `json:"item_count"`
}

func toPointDTO(v app.PointView) pointDTO {
	succ := v.Successors
	if succ == nil {
		succ = []string{}
	}
	d := pointDTO{Code: v.Code, Name: v.Name, AdminUnitCode: v.AdminUnitCode, AdminUnitName: v.AdminUnitName,
		AdminUnitValid: v.AdminUnitValid, Successors: succ, RegionCode: v.RegionCode, RegionName: v.RegionName, State: v.State,
		Version: v.Version, ValidFrom: v.ValidFrom.Format(time.DateOnly), Since: dateStr(v.Since), ValidTo: dateStr(v.ValidTo),
		Note: strPtr(v.Note), OfficerRef: refPtr(v.Officer), OfficerName: strPtr(v.OfficerName), OwnerRef: refPtr(v.Owner),
		OwnerName: strPtr(v.OwnerName), ItemCount: v.ItemCount}
	if v.Officer != uuid.Nil {
		d.OfficerSince = dateStr(v.OfficerSince)
	}
	if v.Owner != uuid.Nil {
		d.OwnerSince = dateStr(v.OwnerSince)
	}
	return d
}

func (h PointHandler) listPoints(c *gin.Context) {
	at, ok := queryDay(c)
	if !ok {
		return
	}
	vs, err := h.Svc.ListPoints(c.Request.Context(), c.Query("profile"), app.PointFilter{At: at, Region: c.Query("region"),
		State: c.Query("state"), Mine: c.Query("mine") == "true"})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]pointDTO, 0, len(vs))
	for _, v := range vs {
		out = append(out, toPointDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"points": out})
}

type pointVersionDTO struct {
	Version       int64   `json:"version"`
	ValidFrom     string  `json:"valid_from"`
	Status        string  `json:"status"`
	AdminUnitCode string  `json:"admin_unit_code"`
	Name          string  `json:"name"`
	RegionCode    string  `json:"region_code"`
	Note          *string `json:"note"`
	EventTime     string  `json:"event_time"`
	ActorRef      string  `json:"actor_ref"`
}

type officerVersionDTO struct {
	Role       string  `json:"role"`
	OfficerRef *string `json:"officer_ref"`
	Name       *string `json:"name"`
	ValidFrom  string  `json:"valid_from"`
	Version    int64   `json:"version"`
	EventTime  string  `json:"event_time"`
	ActorRef   string  `json:"actor_ref"`
}

func (h PointHandler) getPoint(c *gin.Context) {
	at, ok := queryDay(c)
	if !ok {
		return
	}
	d, err := h.Svc.GetPoint(c.Request.Context(), c.Query("profile"), c.Param("code"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	vs := make([]pointVersionDTO, 0, len(d.Versions))
	for _, v := range d.Versions {
		vs = append(vs, pointVersionDTO{Version: v.Version, ValidFrom: v.ValidFrom.Format(time.DateOnly), Status: v.Status,
			AdminUnitCode: v.AdminUnitCode, Name: v.Name, RegionCode: v.RegionCode, Note: strPtr(v.Note),
			EventTime: v.EventTime.UTC().Format(time.RFC3339), ActorRef: v.ActorRef.String()})
	}
	os := make([]officerVersionDTO, 0, len(d.Officers))
	for _, o := range d.Officers {
		os = append(os, officerVersionDTO{Role: o.Role, OfficerRef: refPtr(o.OfficerRef), Name: strPtr(o.Name),
			ValidFrom: o.ValidFrom.Format(time.DateOnly), Version: o.Version, EventTime: o.EventTime.UTC().Format(time.RFC3339),
			ActorRef: o.ActorRef.String()})
	}
	c.JSON(http.StatusOK, gin.H{"point": toPointDTO(d.PointView), "versions": vs, "officers": os})
}

type pointReq struct {
	AdminUnitCode string `json:"admin_unit_code"`
	RegionCode    string `json:"region_code"`
	ValidFrom     string `json:"valid_from"`
	Note          string `json:"note"`
	Reason        string `json:"reason"`
}

func (r pointReq) input() (app.PointInput, error) {
	from, err := day(r.ValidFrom, "valid_from")
	if err != nil {
		return app.PointInput{}, err
	}
	note := r.Note
	if note == "" {
		note = r.Reason
	}
	return app.PointInput{AdminUnitCode: r.AdminUnitCode, RegionCode: r.RegionCode, ValidFrom: from, Note: note}, nil
}

func (h PointHandler) pointWrite(c *gin.Context, fn func(in app.PointInput) (app.WriteResult, error)) {
	var req pointReq
	if !bind(c, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	r, err := fn(in)
	reply(c, http.StatusCreated, r, err)
}

func (h PointHandler) createPoint(c *gin.Context) {
	h.pointWrite(c, func(in app.PointInput) (app.WriteResult, error) {
		return h.Svc.CreatePoint(c.Request.Context(), c.Query("profile"), in, idem(c))
	})
}

func (h PointHandler) revisePoint(c *gin.Context) {
	h.pointWrite(c, func(in app.PointInput) (app.WriteResult, error) {
		return h.Svc.RevisePoint(c.Request.Context(), c.Query("profile"), c.Param("code"), in, idem(c))
	})
}

func (h PointHandler) retirePoint(c *gin.Context) {
	h.pointWrite(c, func(in app.PointInput) (app.WriteResult, error) {
		return h.Svc.RetirePoint(c.Request.Context(), c.Query("profile"), c.Param("code"), in, idem(c))
	})
}

type officerReq struct {
	Role       string  `json:"role"`
	OfficerRef *string `json:"officer_ref"`
	ValidFrom  string  `json:"valid_from"`
}

func (h PointHandler) assignOfficer(c *gin.Context) {
	var req officerReq
	if !bind(c, &req) {
		return
	}
	from, err := day(req.ValidFrom, "valid_from")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	ref := uuid.Nil
	if req.OfficerRef != nil && *req.OfficerRef != "" {
		if ref, err = uuid.Parse(*req.OfficerRef); err != nil || ref == uuid.Nil {
			httpx.Fail(c, problem.Validation.WithDetail("officer_ref phải là UUID"))
			return
		}
	}
	r, err := h.Svc.AssignOfficer(c.Request.Context(), c.Query("profile"), c.Param("code"), req.Role, ref, from, idem(c))
	reply(c, http.StatusCreated, r, err)
}

// ---- ĐVHC, danh bạ cán bộ ----

type adminUnitDTO struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	ParentCode *string `json:"parent_code"`
	ValidFrom  string  `json:"valid_from"`
	ValidTo    *string `json:"valid_to"`
	TakenBy    *string `json:"taken_by"`
}

func (h PointHandler) adminUnits(c *gin.Context) {
	at, ok := queryDay(c)
	if !ok {
		return
	}
	us, err := h.Svc.ListAdminUnits(c.Request.Context(), c.Query("profile"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]adminUnitDTO, 0, len(us))
	for _, u := range us {
		d := adminUnitDTO{Code: u.Code, Name: u.Name, ParentCode: strPtr(u.ParentCode), ValidFrom: u.ValidFrom.Format(time.DateOnly),
			TakenBy: strPtr(u.TakenBy)}
		if u.ValidTo != nil {
			d.ValidTo = dateStr(*u.ValidTo)
		}
		out = append(out, d)
	}
	c.JSON(http.StatusOK, gin.H{"admin_units": out})
}

type officerDTO struct {
	Ref      string   `json:"ref"`
	Name     string   `json:"name"`
	Level    string   `json:"level"`
	Communes []string `json:"communes"`
}

func (h PointHandler) officers(c *gin.Context) {
	os, wired, err := h.Svc.ListOfficers(c.Request.Context(), c.Query("profile"), c.Query("level"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]officerDTO, 0, len(os))
	for _, o := range os {
		cs := o.Communes
		if cs == nil {
			cs = []string{}
		}
		out = append(out, officerDTO{Ref: o.Ref.String(), Name: o.Name, Level: o.Level, Communes: cs})
	}
	c.JSON(http.StatusOK, gin.H{"officers": out, "directory": wired})
}
