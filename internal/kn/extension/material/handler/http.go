// Package handler: route /v1/kn/materials… (KN v7.7 FN-XA-03.06). Mọi route cần header X-AG-Context.
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
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/material/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/material/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/kn/materials", h.list)
	rg.GET("/kn/materials/summary", h.summary)
	rg.POST("/kn/materials", h.record)
	rg.POST("/kn/materials/:id/correct", h.correct)
	rg.GET("/kn/materials/:id/log", h.log)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func dto(v app.View) gin.H {
	var sup any
	if v.SupersedesID != nil {
		sup = v.SupersedesID.String()
	}
	return gin.H{
		"id": v.ID.String(), "commune_code": v.CommuneCode, "recorded_on": v.RecordedOn.Format(time.DateOnly),
		"material_code": v.MaterialCode, "material_name": v.MaterialName, "quantity": v.Quantity.String(), "unit": v.Unit,
		"note": v.Note, "department_code": v.DepartmentCode, "actor_ref": v.ActorRef.String(), "actor_display": v.ActorDisplay,
		"recorded_at": ts(v.RecordedAt), "supersedes_id": sup, "correction_reason": v.CorrectionReason,
	}
}

func dtos(vs []app.View) []gin.H {
	out := make([]gin.H, 0, len(vs))
	for _, v := range vs {
		out = append(out, dto(v))
	}
	return out
}

func bind(c *gin.Context, v any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

type lineReq struct {
	MaterialCode string           `json:"material_code"`
	Quantity     *decimal.Decimal `json:"quantity"`
	Note         string           `json:"note"`
}

type recordReq struct {
	CommuneCode string    `json:"commune_code"`
	RecordedOn  string    `json:"recorded_on"`
	Lines       []lineReq `json:"lines"`
}

func (h Handler) record(c *gin.Context) {
	var req recordReq
	if !bind(c, &req) {
		return
	}
	in := app.RecordInput{CommuneCode: req.CommuneCode, RecordedOn: req.RecordedOn, Department: c.Query("department"),
		IdemKey: idempotency.KeyFromContext(c.Request.Context())}
	for _, l := range req.Lines {
		in.Lines = append(in.Lines, app.Line{MaterialCode: l.MaterialCode, Quantity: l.Quantity, Note: l.Note})
	}
	items, replayed, err := h.Svc.Record(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"items": dtos(items)})
}

type correctReq struct {
	Quantity *decimal.Decimal `json:"quantity"`
	Note     *string          `json:"note"`
	Reason   string           `json:"reason"`
}

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h Handler) correct(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req correctReq
	if !bind(c, &req) {
		return
	}
	v, replayed, err := h.Svc.Correct(c.Request.Context(), app.CorrectInput{ID: id, Quantity: req.Quantity, Note: req.Note, Reason: req.Reason,
		IdemKey: idempotency.KeyFromContext(c.Request.Context())})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, dto(v))
}

// cursor = base64url("YYYY-MM-DD_<uuid>") của dòng cuối trang trước.
func encodeCursor(on *time.Time, id uuid.UUID) any {
	if on == nil {
		return nil
	}
	return base64.RawURLEncoding.EncodeToString([]byte(on.Format(time.DateOnly) + "_" + id.String()))
}

func decodeCursor(s string) (*time.Time, uuid.UUID, error) {
	if s == "" {
		return nil, uuid.Nil, nil
	}
	bad := problem.Validation.WithField("cursor", "không hợp lệ")
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, uuid.Nil, bad
	}
	d, idPart, ok := strings.Cut(string(b), "_")
	if !ok {
		return nil, uuid.Nil, bad
	}
	t, err := time.Parse(time.DateOnly, d)
	if err != nil {
		return nil, uuid.Nil, bad
	}
	id, err := uuid.Parse(idPart)
	if err != nil {
		return nil, uuid.Nil, bad
	}
	return &t, id, nil
}

func query(c *gin.Context) (app.Query, error) {
	q := app.Query{Commune: c.Query("commune"), From: c.Query("from"), To: c.Query("to"), MaterialCode: c.Query("material_code"),
		Department: c.Query("department")}
	if l := c.Query("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil {
			return q, problem.Validation.WithField("limit", "phải là số nguyên")
		}
		q.Limit = n
	}
	var err error
	q.AfterOn, q.AfterID, err = decodeCursor(c.Query("cursor"))
	return q, err
}

func (h Handler) list(c *gin.Context) {
	q, err := query(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	pg, err := h.Svc.List(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": dtos(pg.Items), "next_cursor": encodeCursor(pg.NextOn, pg.NextID)})
}

func summaryDTO(rs []store.SummaryRow) []gin.H {
	out := make([]gin.H, 0, len(rs))
	for _, r := range rs {
		out = append(out, gin.H{"commune_code": r.CommuneCode, "material_code": r.MaterialCode, "material_name": r.MaterialName,
			"unit": r.Unit, "quantity": r.Quantity.String(), "records": r.Records})
	}
	return out
}

func (h Handler) summary(c *gin.Context) {
	q, err := query(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rs, err := h.Svc.Summary(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": summaryDTO(rs)})
}

func (h Handler) log(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	items, err := h.Svc.Log(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, gin.H{"id": it.ID.String(), "event": it.Event, "actor_ref": it.ActorRef.String(), "actor_display": it.ActorDisplay,
			"at": ts(it.At), "changes": it.Changes, "reason": it.Reason})
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "next_cursor": nil})
}
