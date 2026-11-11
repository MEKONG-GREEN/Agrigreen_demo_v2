// Package handler: route Trồng trọt FN-XA-03.04 — Máy móc HTX do chủ KN phục vụ (KN-STANDALONE-5-TT-DATA-MAY #306):
//
//	GET /v1/kn/machine-counts?commune=, GET|PUT /v1/kn/machine-counts/{ref}, GET /v1/kn/machine-counts/{ref}/log.
//
// {ref} là mã hồ sơ KN loại HTX (/v1/kn/producer-records). Mọi route cần header X-AG-Context. DTO giữ nguyên như trước khi
// chuyển kho để web và field-app không đổi.
package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knmachine/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knmachine/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// MaxBody là kích thước thân request tối đa (byte): 100 dòng × ghi chú 500 ký tự.
const MaxBody = 256 << 10

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/kn/machine-counts")
	g.GET("", h.overview)
	g.GET("/:ref", h.get)
	g.PUT("/:ref", h.put)
	g.GET("/:ref/log", h.log)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

type lineDTO struct {
	MachineType     string  `json:"machine_type"`
	MachineTypeName string  `json:"machine_type_name"`
	Quantity        int     `json:"quantity"`
	Note            *string `json:"note"`
	UpdatedAt       string  `json:"updated_at"`
	UpdatedBy       string  `json:"updated_by"`
}

func toLines(ls []app.Line) []lineDTO {
	out := make([]lineDTO, 0, len(ls))
	for _, l := range ls {
		d := lineDTO{MachineType: l.MachineType, MachineTypeName: l.TypeName, Quantity: l.Quantity, UpdatedAt: ts(l.UpdatedAt), UpdatedBy: l.UpdatedBy.String()}
		if l.Note != "" {
			n := l.Note
			d.Note = &n
		}
		out = append(out, d)
	}
	return out
}

func coopDTO(cm app.CoopMachines) gin.H {
	return gin.H{"cooperative_ref": cm.CooperativeRef.String(), "name": cm.Name, "commune_code": cm.CommuneCode, "lines": toLines(cm.Lines), "total": cm.Total}
}

func pathRef(c *gin.Context) (uuid.UUID, bool) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrCoopNotFound)
		return ref, false
	}
	return ref, true
}

func (h Handler) overview(c *gin.Context) {
	out, err := h.Svc.Overview(c.Request.Context(), c.Query("commune"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	coops := make([]gin.H, 0, len(out.Cooperatives))
	for _, cm := range out.Cooperatives {
		coops = append(coops, coopDTO(cm))
	}
	totals := make([]gin.H, 0, len(out.CommuneTotals))
	for _, t := range out.CommuneTotals {
		totals = append(totals, gin.H{"commune_code": t.CommuneCode, "machine_type": t.MachineType, "machine_type_name": t.TypeName, "quantity": t.Quantity})
	}
	c.JSON(http.StatusOK, gin.H{"cooperatives": coops, "commune_totals": totals})
}

func (h Handler) get(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	cm, err := h.Svc.Get(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, coopDTO(cm))
}

type lineReq struct {
	MachineType string          `json:"machine_type"`
	Quantity    json.RawMessage `json:"quantity"`
	Note        *string         `json:"note"`
}

type putReq struct {
	Lines  []lineReq `json:"lines"`
	Reason string    `json:"reason"`
}

func (h Handler) put(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req putReq
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBody)
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return
	}
	if req.Lines == nil {
		httpx.Fail(c, problem.Validation.WithField("lines", "bắt buộc (mảng rỗng = bỏ mọi dòng)"))
		return
	}
	ls := make([]domain.Line, 0, len(req.Lines))
	var bad *problem.Problem
	for i, l := range req.Lines {
		raw := bytes.TrimSpace(l.Quantity)
		// Số lượng phải là số JSON nguyên (không nhận chuỗi, null, số thập phân, số mũ) — kiểm cú pháp trước khi parse.
		q, err := domain.ParseQuantity(string(raw))
		if len(raw) == 0 || raw[0] == '"' || bytes.Equal(raw, []byte("null")) {
			err = domain.ErrQuantityNotInteger
		}
		if err != nil {
			if bad == nil {
				bad = app.ErrQuantityInvalid
			}
			bad = bad.WithField("lines["+strconv.Itoa(i)+"].quantity", err.Error())
			continue
		}
		note := ""
		if l.Note != nil {
			note = *l.Note
		}
		ls = append(ls, domain.Line{MachineType: l.MachineType, Quantity: q, Note: note})
	}
	if bad != nil {
		httpx.Fail(c, bad)
		return
	}
	cm, err := h.Svc.Put(c.Request.Context(), app.PutInput{CooperativeRef: ref, Lines: ls, Reason: req.Reason,
		Department: c.Query("department"), IdemKey: strings.TrimSpace(c.GetHeader("Idempotency-Key"))})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, coopDTO(cm))
}

type changeDTO struct {
	Field string  `json:"field"`
	Old   *string `json:"old"`
	New   *string `json:"new"`
}

func intStr(p *int) *string {
	if p == nil {
		return nil
	}
	s := strconv.Itoa(*p)
	return &s
}

func (h Handler) log(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var after int64
	if s := c.Query("cursor"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 0 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "phải là số seq ≥ 0"))
			return
		}
		after = n
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	evs, err := h.Svc.Log(c.Request.Context(), ref, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(evs))
	for _, e := range evs {
		q := e.Quantity
		changes := []changeDTO{{Field: "quantity", Old: intStr(e.PrevQuantity), New: intStr(&q)}}
		if e.Action == domain.Removed {
			changes[0].New = nil
		}
		var oldNote, newNote *string
		if e.PrevNote != nil && *e.PrevNote != "" {
			oldNote = e.PrevNote
		}
		if e.Note != nil && *e.Note != "" {
			newNote = e.Note
		}
		if (oldNote == nil) != (newNote == nil) || (oldNote != nil && *oldNote != *newNote) {
			changes = append(changes, changeDTO{Field: "note", Old: oldNote, New: newNote})
		}
		reason := e.Reason
		if reason == nil {
			reason = e.SupportReason
		}
		items = append(items, gin.H{"seq": e.Seq, "event": e.Action, "machine_type": e.MachineType, "machine_type_name": e.TypeName,
			"actor_ref": e.ActorRef.String(), "actor_display": nil, "at": ts(e.EventTime), "recorded_at": ts(e.RecordedAt),
			"changes": changes, "reason": reason, "department_code": e.DepartmentCode})
	}
	var next any
	if len(evs) == limit {
		next = evs[len(evs)-1].Seq
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}
