// Package handler: route hồ sơ KN riêng /v1/kn/machine-profiles… (TT-10) và /v1/kn/plot-profiles… (TT-11), gói
// BF-SRV-KN-ATTRS (#119). Mọi route cần header X-AG-Context.
package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knattrs/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knattrs/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/kn/machine-profiles/:cooperative_ref", h.getMachines)
	rg.PUT("/kn/machine-profiles/:cooperative_ref", h.putMachines)
	rg.GET("/kn/plot-profiles", h.listCrops)
	rg.GET("/kn/plot-profiles/:plot_ref", h.getPlot)
	rg.PUT("/kn/plot-profiles/:plot_ref", h.putPlot)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func bind(c *gin.Context, v any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func pathRef(c *gin.Context, name string, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}

func machinesDTO(p app.MachineProfile) gin.H {
	items := make([]gin.H, 0, len(p.Items))
	for _, m := range p.Items {
		items = append(items, gin.H{"machine_type": m.MachineType, "machine_type_name": m.TypeName, "year_made": m.YearMade,
			"updated_at": ts(m.RecordedAt), "actor_ref": m.ActorRef.String()})
	}
	return gin.H{"cooperative_ref": p.CooperativeRef.String(), "commune_code": p.CommuneCode, "department_code": p.Department, "items": items}
}

func (h Handler) getMachines(c *gin.Context) {
	ref, ok := pathRef(c, "cooperative_ref", app.ErrCoopNotFound)
	if !ok {
		return
	}
	p, err := h.Svc.GetMachines(c.Request.Context(), ref, c.Query("department"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, machinesDTO(p))
}

type machineLineReq struct {
	MachineType string          `json:"machine_type"`
	YearMade    json.RawMessage `json:"year_made"`
}

type machinesReq struct {
	Lines *[]machineLineReq `json:"lines"`
}

// yearText: số JSON → chuỗi để kiểm nguyên/phạm vi; null/vắng → nil (bắt buộc); kiểu khác → chuỗi không phải số.
func yearText(raw json.RawMessage) *string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var n json.Number
	if raw[0] != '"' && json.Unmarshal(raw, &n) == nil {
		s := n.String()
		return &s
	}
	s := "không phải số"
	return &s
}

func (h Handler) putMachines(c *gin.Context) {
	ref, ok := pathRef(c, "cooperative_ref", app.ErrCoopNotFound)
	if !ok {
		return
	}
	var req machinesReq
	if !bind(c, &req) {
		return
	}
	if req.Lines == nil {
		httpx.Fail(c, problem.Validation.WithField("lines", "bắt buộc (mảng rỗng = bỏ toàn bộ năm sản xuất)"))
		return
	}
	lines := make([]app.MachineYearInput, 0, len(*req.Lines))
	for _, l := range *req.Lines {
		lines = append(lines, app.MachineYearInput{MachineType: l.MachineType, YearMade: yearText(l.YearMade)})
	}
	p, err := h.Svc.PutMachines(c.Request.Context(), ref, c.Query("department"), lines)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, machinesDTO(p))
}

func cropDTO(cr store.PlotCrop) gin.H {
	return gin.H{"plot_ref": cr.PlotRef.String(), "commune_code": cr.CommuneCode, "department_code": cr.Department, "crop_code": cr.CropCode,
		"crop_name": cr.CropName, "updated_at": ts(cr.RecordedAt), "actor_ref": cr.ActorRef.String()}
}

func plotDTO(p app.PlotProfile) gin.H {
	out := gin.H{"plot_ref": p.PlotRef.String(), "commune_code": p.CommuneCode, "department_code": p.Department, "status": p.Status,
		"crop_code": nil, "crop_name": nil, "updated_at": nil, "actor_ref": nil}
	if p.Crop != nil {
		out["crop_code"], out["crop_name"], out["updated_at"], out["actor_ref"] = p.Crop.CropCode, p.Crop.CropName, ts(p.Crop.RecordedAt), p.Crop.ActorRef.String()
	}
	return out
}

func (h Handler) getPlot(c *gin.Context) {
	ref, ok := pathRef(c, "plot_ref", app.ErrPlotNotFound)
	if !ok {
		return
	}
	p, err := h.Svc.GetPlot(c.Request.Context(), ref, c.Query("department"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, plotDTO(p))
}

type plotReq struct {
	CropCode *string `json:"crop_code"`
}

func (h Handler) putPlot(c *gin.Context) {
	ref, ok := pathRef(c, "plot_ref", app.ErrPlotNotFound)
	if !ok {
		return
	}
	var req plotReq
	if !bind(c, &req) {
		return
	}
	crop := ""
	if req.CropCode != nil {
		crop = *req.CropCode
	}
	p, err := h.Svc.PutPlot(c.Request.Context(), ref, c.Query("department"), crop)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, plotDTO(p))
}

func (h Handler) listCrops(c *gin.Context) {
	q := app.CropQuery{Commune: c.Query("commune"), Department: c.Query("department")}
	if l := c.Query("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("limit", "phải là số nguyên"))
			return
		}
		q.Limit = n
	}
	if cur := c.Query("cursor"); cur != "" {
		id, err := uuid.Parse(cur)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		q.After = id
	}
	pg, err := h.Svc.ListCrops(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(pg.Items))
	for _, cr := range pg.Items {
		items = append(items, cropDTO(cr))
	}
	var next any
	if pg.Next != nil {
		next = pg.Next.String()
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}
