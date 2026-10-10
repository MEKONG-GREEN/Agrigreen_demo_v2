package handler

// Route KN v7.7 FN-XA-03.05 "Vẽ ranh giới thửa ruộng" (gói P7, contracts: scratchpad p7-contract §3). Mọi route cần
// header X-AG-Context (ngữ cảnh phiên P1); quyền theo vai trong ngữ cảnh (authz.RequireKN), không theo vai realm.

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
)

func (h Handler) registerKN(rg *gin.RouterGroup) {
	rg.GET("/kn/plots", h.knList)
	rg.GET("/kn/plots/export", h.knExport)
	rg.POST("/kn/plots", h.knCreate)
	rg.GET("/kn/plots/:ref", h.knGet)
	rg.PUT("/kn/plots/:ref", h.knUpdate)
	rg.POST("/kn/plots/:ref/boundary", h.knCorrect)
	rg.GET("/kn/plots/:ref/log", h.knLog)
}

func knPlotDTO(p store.Plot, labels map[uuid.UUID]string, withGeom bool) gin.H {
	if p.Department == "" {
		p.Department = store.LegacyKNDepartment // X-12: thửa có trước v7.7 đọc qua KN là Phòng Trồng trọt
	}
	m := gin.H{
		"ref": p.Ref.String(), "code": p.Code, "lot_name": strOrNil(p.LotName), "commune_code": p.CommuneCode,
		"cooperative_ref": refOrNil(p.CooperativeRef), "farmer": nil, "note": strOrNil(p.Note), "area_ha": p.AreaHa.StringFixed(4),
		"declared_area_ha": nil, "area_deviation_pct": nil, "shape": strOrNil(p.Shape), "department_code": strOrNil(p.Department),
		"status": p.Status, "needs_review": p.NeedsReview, "version": p.CurrentVersion, "boundary_id": p.CurrentBoundaryID.String(),
		"updated_at": ts(p.UpdatedAt),
	}
	if p.FarmerRef != uuid.Nil {
		f := gin.H{"label": nil}
		if l, ok := labels[p.FarmerRef]; ok {
			f["label"] = l
		}
		m["farmer"] = f
	}
	if p.DeclaredAreaHa != nil {
		m["declared_area_ha"] = p.DeclaredAreaHa.StringFixed(4)
		m["area_deviation_pct"] = domain.AreaDeviationPct(p.AreaHa, *p.DeclaredAreaHa).StringFixed(2)
	}
	if withGeom {
		m["geometry"] = json.RawMessage(p.GeoJSON)
	}
	return m
}

func (h Handler) knList(c *gin.Context) {
	f := newCollector()
	coop := f.ref("cooperative_ref", strPtrQ(c.Query("cooperative_ref")))
	after, err := decodeCursor(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	limit, err := intQuery(c, "limit", 100)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	pg, err := h.Svc.KNListPlots(c.Request.Context(), app.KNPlotQuery{Commune: c.Query("commune"), CooperativeRef: coop, After: after, Limit: limit})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(pg.Items))
	for _, p := range pg.Items {
		items = append(items, knPlotDTO(p, pg.Labels, false))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
}

func strPtrQ(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (h Handler) knGet(c *gin.Context) {
	ref, ok := pathRef(c, "ref")
	if !ok {
		return
	}
	p, labels, err := h.Svc.KNGetPlot(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, knPlotDTO(p, labels, true))
}

type knCreateReq struct {
	PlotRef        *string          `json:"plot_ref"`
	CommuneCode    string           `json:"commune_code"`
	CooperativeRef *string          `json:"cooperative_ref"`
	FarmerName     string           `json:"farmer_name" pii:"true"`
	LotName        string           `json:"lot_name"`
	Note           string           `json:"note"`
	DeclaredAreaHa *decimal.Decimal `json:"declared_area_ha"`
	Shape          string           `json:"shape"`
	Geometry       json.RawMessage  `json:"geometry"`
}

func knWrite(c *gin.Context, h Handler, res app.WriteResult) {
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/kn/plots/"+res.Plot.Ref.String())
	out := knPlotDTO(res.Plot, h.Svc.FarmerLabels(c.Request.Context(), res.Plot), true)
	out["boundary"] = boundaryDTO(res.Boundary)
	c.JSON(status, out)
}

func (h Handler) knCreate(c *gin.Context) {
	var req knCreateReq
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	in := app.KNCreateInput{
		PlotRef: f.v7("plot_ref", req.PlotRef), CommuneCode: req.CommuneCode, CooperativeRef: f.ref("cooperative_ref", req.CooperativeRef),
		FarmerName: req.FarmerName, LotName: req.LotName, Note: req.Note, DeclaredAreaHa: req.DeclaredAreaHa, Shape: req.Shape,
		Geometry: req.Geometry, Department: c.Query("department"), IdemKey: idempotency.KeyFromContext(c.Request.Context()),
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	res, err := h.Svc.KNCreatePlot(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	knWrite(c, h, res)
}

type knCorrectReq struct {
	SupersedesBoundaryID *string         `json:"supersedes_boundary_id"`
	Geometry             json.RawMessage `json:"geometry"`
	Shape                string          `json:"shape"`
	Reason               string          `json:"reason"`
}

func (h Handler) knCorrect(c *gin.Context) {
	ref, ok := pathRef(c, "ref")
	if !ok {
		return
	}
	var req knCorrectReq
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	in := app.KNCorrectInput{PlotRef: ref, SupersedesID: f.ref("supersedes_boundary_id", req.SupersedesBoundaryID), Geometry: req.Geometry,
		Shape: req.Shape, Reason: req.Reason, IdemKey: idempotency.KeyFromContext(c.Request.Context())}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	res, err := h.Svc.KNCorrectBoundary(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	knWrite(c, h, res)
}

// knUpdateReq: trường vắng = giữ nguyên; null/"" = xóa (lot_name bắt buộc khác rỗng).
type knUpdateReq map[string]json.RawMessage

func (h Handler) knUpdate(c *gin.Context) {
	ref, ok := pathRef(c, "ref")
	if !ok {
		return
	}
	var req knUpdateReq
	if !bindJSON(c, &req) {
		return
	}
	f := newCollector()
	in := app.KNUpdateInput{PlotRef: ref, IdemKey: strings.TrimSpace(c.GetHeader(idempotency.Header))}
	str := func(field string) *string {
		raw, ok := req[field]
		if !ok {
			return nil
		}
		var s *string
		if err := json.Unmarshal(raw, &s); err != nil {
			f.add(field, "phải là chuỗi")
			return nil
		}
		if s == nil {
			e := ""
			return &e
		}
		return s
	}
	in.LotName, in.Note, in.FarmerName = str("lot_name"), str("note"), str("farmer_name")
	if r := str("reason"); r != nil {
		in.Reason = *r
	}
	if s := str("cooperative_ref"); s != nil {
		u := uuid.Nil
		if *s != "" {
			u = f.ref("cooperative_ref", s)
		}
		in.CooperativeRef = &u
	}
	if raw, ok := req["declared_area_ha"]; ok {
		if string(raw) == "null" {
			in.ClearDeclared = true
		} else {
			var d decimal.Decimal
			if err := json.Unmarshal(raw, &d); err != nil {
				f.add("declared_area_ha", "phải là số thập phân")
			} else {
				in.DeclaredAreaHa = &d
			}
		}
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	res, err := h.Svc.KNUpdatePlot(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, knPlotDTO(res.Plot, h.Svc.FarmerLabels(c.Request.Context(), res.Plot), true))
}

func (h Handler) knLog(c *gin.Context) {
	ref, ok := pathRef(c, "ref")
	if !ok {
		return
	}
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
	pg, err := h.Svc.KNPlotLog(c.Request.Context(), ref, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(pg.Items))
	for _, it := range pg.Items {
		items = append(items, gin.H{"id": it.ID.String(), "event": it.Event, "actor_ref": it.ActorRef.String(), "actor_display": it.ActorDisplay,
			"at": it.At.UTC().Format(time.RFC3339Nano), "changes": it.Changes, "reason": it.Reason})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
}

func (h Handler) knExport(c *gin.Context) {
	file, err := h.Svc.KNExport(c.Request.Context(), c.Query("commune"), strings.ToLower(strings.TrimSpace(c.Query("format"))))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+file.Name+`"`)
	c.Data(http.StatusOK, file.ContentType, file.Body)
}
