// Package handler: HTTP thửa ruộng KN (Trồng trọt FN-XA-03.05) — Application API, bắt buộc header X-AG-Context:
//
//	GET|POST /v1/kn/plots, GET /v1/kn/plots/export, GET|PUT /v1/kn/plots/{ref}, POST /v1/kn/plots/{ref}/boundary,
//	GET /v1/kn/plots/{ref}/log
//
// KN-STANDALONE-5-TT-DATA (#271): route do chủ KN (pf/extension) đăng ký, dữ liệu ở pf_extension; giữ hợp đồng cũ
// (contracts/openapi/modules/extension/kn-plots) để web và field-app không đổi.
package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knplot/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knplot/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knplot/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gắn route thửa ruộng KN.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route vào nhóm Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/kn/plots", h.list)
	rg.GET("/kn/plots/export", h.export)
	rg.POST("/kn/plots", h.create)
	rg.GET("/kn/plots/:ref", h.get)
	rg.PUT("/kn/plots/:ref", h.update)
	rg.POST("/kn/plots/:ref/boundary", h.correct)
	rg.GET("/kn/plots/:ref/log", h.log)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func opt(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func dto(p store.Plot, labels map[uuid.UUID]string, withGeom bool) gin.H {
	d := p.Department
	if d == "" {
		d = domain.LegacyDepartment
	}
	m := gin.H{
		"ref": p.Ref.String(), "code": p.Code, "lot_name": opt(p.LotName), "commune_code": p.CommuneCode, "cooperative_ref": nil,
		"farmer": nil, "note": opt(p.Note), "area_ha": p.AreaHa.StringFixed(4), "declared_area_ha": nil, "area_deviation_pct": nil,
		"shape": opt(p.Shape), "department_code": d, "status": app.Status(p.Snapshot), "needs_review": false, "version": p.Version,
		"boundary_id": p.BoundaryID.String(), "updated_at": ts(p.UpdatedAt),
	}
	if p.CooperativeRef != nil {
		m["cooperative_ref"] = p.CooperativeRef.String()
	}
	if p.FarmerRef != nil {
		f := gin.H{"label": nil}
		if l, ok := labels[*p.FarmerRef]; ok {
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

func encodeCursor(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return base64.RawURLEncoding.EncodeToString(u[:])
}

func decodeCursor(c *gin.Context) (uuid.UUID, bool) {
	var after uuid.UUID
	cur := c.Query("cursor")
	if cur == "" {
		return after, true
	}
	b, err := base64.RawURLEncoding.DecodeString(cur)
	if err != nil || len(b) != 16 {
		httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
		return after, false
	}
	copy(after[:], b)
	return after, true
}

func limit(c *gin.Context, def int) (int, bool) {
	s := c.Query("limit")
	if s == "" {
		return def, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		httpx.Fail(c, problem.Validation.WithField("limit", "phải là số nguyên dương"))
		return 0, false
	}
	return n, true
}

func pathRef(c *gin.Context) (uuid.UUID, bool) {
	u, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrPlotNotFound)
		return u, false
	}
	return u, true
}

func optRef(field string, s *string) (*uuid.UUID, error) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil, nil
	}
	u, err := uuid.Parse(strings.TrimSpace(*s))
	if err != nil {
		return nil, problem.Validation.WithField(field, "phải là UUID")
	}
	return &u, nil
}

func bindJSON(c *gin.Context, v any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func (h Handler) list(c *gin.Context) {
	coop, err := optRef("cooperative_ref", strPtr(c.Query("cooperative_ref")))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	after, ok := decodeCursor(c)
	if !ok {
		return
	}
	n, ok := limit(c, 100)
	if !ok {
		return
	}
	pg, err := h.Svc.List(c.Request.Context(), app.Query{Commune: c.Query("commune"), Coop: coop, After: after, Limit: n})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(pg.Items))
	for _, p := range pg.Items {
		items = append(items, dto(p, pg.Labels, false))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (h Handler) get(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	p, labels, err := h.Svc.Get(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, dto(p, labels, true))
}

func (h Handler) write(c *gin.Context, res app.Result) {
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/kn/plots/"+res.Plot.Ref.String())
	out := dto(res.Plot, h.Svc.FarmerLabels(c.Request.Context(), res.Plot), true)
	out["boundary"] = gin.H{"boundary_id": res.Plot.BoundaryID.String(), "plot_ref": res.Plot.Ref.String(), "version": res.Plot.Version,
		"area_ha": res.Plot.AreaHa.StringFixed(4), "commune_code": res.Plot.CommuneCode}
	c.JSON(status, out)
}

type createReq struct {
	PlotRef        *string         `json:"plot_ref"`
	CommuneCode    string          `json:"commune_code"`
	CooperativeRef *string         `json:"cooperative_ref"`
	FarmerName     string          `json:"farmer_name" pii:"true"`
	LotName        string          `json:"lot_name"`
	Note           string          `json:"note"`
	DeclaredAreaHa json.RawMessage `json:"declared_area_ha"`
	Shape          string          `json:"shape"`
	Geometry       json.RawMessage `json:"geometry"`
}

func (h Handler) create(c *gin.Context) {
	var req createReq
	if !bindJSON(c, &req) {
		return
	}
	plotRef, err := optRef("plot_ref", req.PlotRef)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	coop, err := optRef("cooperative_ref", req.CooperativeRef)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	declared, why := domain.ParseDeclared(req.DeclaredAreaHa)
	if why != "" {
		httpx.Fail(c, problem.Validation.WithField("declared_area_ha", why))
		return
	}
	in := app.CreateInput{CommuneCode: req.CommuneCode, CooperativeRef: coop, FarmerName: req.FarmerName, LotName: req.LotName,
		Note: req.Note, DeclaredAreaHa: declared, Shape: req.Shape, Geometry: req.Geometry, Department: c.Query("department"),
		IdemKey: idempotency.KeyFromContext(c.Request.Context()), Source: domain.SourceWeb}
	if plotRef != nil {
		in.PlotRef = *plotRef
	}
	res, err := h.Svc.Create(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.write(c, res)
}

type correctReq struct {
	SupersedesBoundaryID *string         `json:"supersedes_boundary_id"`
	Geometry             json.RawMessage `json:"geometry"`
	Shape                string          `json:"shape"`
	Reason               string          `json:"reason"`
}

func (h Handler) correct(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req correctReq
	if !bindJSON(c, &req) {
		return
	}
	sup, err := optRef("supersedes_boundary_id", req.SupersedesBoundaryID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if sup == nil {
		httpx.Fail(c, problem.Validation.WithField("supersedes_boundary_id", "bắt buộc"))
		return
	}
	res, err := h.Svc.Correct(c.Request.Context(), app.CorrectInput{PlotRef: ref, SupersedesID: *sup, Geometry: req.Geometry, Shape: req.Shape,
		Reason: req.Reason, IdemKey: idempotency.KeyFromContext(c.Request.Context()), Source: domain.SourceWeb})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.write(c, res)
}

// update: trường vắng = giữ nguyên; null/"" = xóa (lot_name bắt buộc khác rỗng).
func (h Handler) update(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req map[string]json.RawMessage
	if !bindJSON(c, &req) {
		return
	}
	in := app.UpdateInput{PlotRef: ref, IdemKey: strings.TrimSpace(c.GetHeader(idempotency.Header))}
	bad := problem.Validation
	failed := false
	str := func(field string) *string {
		raw, ok := req[field]
		if !ok {
			return nil
		}
		var s *string
		if err := json.Unmarshal(raw, &s); err != nil {
			bad, failed = bad.WithField(field, "phải là chuỗi"), true
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
			p, err := uuid.Parse(*s)
			if err != nil {
				bad, failed = bad.WithField("cooperative_ref", "phải là UUID"), true
			}
			u = p
		}
		in.CooperativeRef = &u
	}
	if raw, ok := req["declared_area_ha"]; ok {
		d, why := domain.ParseDeclared(raw)
		switch {
		case why != "":
			bad, failed = bad.WithField("declared_area_ha", why), true
		case d == nil:
			in.ClearDeclared = true
		default:
			in.DeclaredAreaHa = d
		}
	}
	if failed {
		httpx.Fail(c, bad)
		return
	}
	res, err := h.Svc.Update(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, dto(res.Plot, h.Svc.FarmerLabels(c.Request.Context(), res.Plot), true))
}

func (h Handler) log(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	after, ok := decodeCursor(c)
	if !ok {
		return
	}
	n, ok := limit(c, 50)
	if !ok {
		return
	}
	pg, err := h.Svc.History(c.Request.Context(), ref, after, n)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(pg.Items))
	for _, it := range pg.Items {
		items = append(items, gin.H{"id": it.ID.String(), "event": it.Event, "actor_ref": it.ActorRef.String(), "actor_display": it.ActorDisplay,
			"at": ts(it.At), "changes": it.Changes, "reason": it.Reason})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
}

func (h Handler) export(c *gin.Context) {
	f, err := h.Svc.Export(c.Request.Context(), c.Query("commune"), strings.ToLower(strings.TrimSpace(c.Query("format"))))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+f.Name+`"`)
	c.Data(http.StatusOK, f.ContentType, f.Body)
}
