package handler

// HTTP của KN v7.7 FN-XA-03.04 — Máy móc HTX (hợp đồng P7 §2). Application API, bắt buộc header X-AG-Context:
//   GET /v1/kn/machine-counts?commune=, GET|PUT /v1/kn/machine-counts/{ref},
//   GET /v1/kn/machine-counts/{ref}/log.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// RegisterKNMachineCounts gắn route FN-XA-03.04 vào nhóm Application API.
func (h Handler) RegisterKNMachineCounts(rg *gin.RouterGroup) {
	g := rg.Group("/kn/machine-counts")
	g.GET("", h.knMachineOverview)
	g.GET("/:ref", h.knCoopMachines)
	g.PUT("/:ref", h.knPutMachines)
	g.GET("/:ref/log", h.knMachineLog)
}

type machineLineDTO struct {
	MachineType     string  `json:"machine_type"`
	MachineTypeName string  `json:"machine_type_name"`
	Quantity        int     `json:"quantity"`
	Note            *string `json:"note"`
	UpdatedAt       string  `json:"updated_at"`
	UpdatedBy       string  `json:"updated_by"`
}

func toLines(ls []app.MachineLine) []machineLineDTO {
	out := make([]machineLineDTO, 0, len(ls))
	for _, l := range ls {
		d := machineLineDTO{MachineType: l.MachineType, MachineTypeName: l.TypeName, Quantity: l.Quantity,
			UpdatedAt: l.UpdatedAt.UTC().Format(time.RFC3339Nano), UpdatedBy: l.UpdatedBy.String()}
		if l.Note != "" {
			n := l.Note
			d.Note = &n
		}
		out = append(out, d)
	}
	return out
}

func coopRef(c *gin.Context) (uuid.UUID, bool) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrCoopNotFound)
		return ref, false
	}
	return ref, true
}

func (h Handler) knMachineOverview(c *gin.Context) {
	out, err := h.Svc.MachineCountsOverview(c.Request.Context(), c.Query("commune"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	coops := make([]gin.H, 0, len(out.Cooperatives))
	for _, cm := range out.Cooperatives {
		coops = append(coops, gin.H{"cooperative_ref": cm.CooperativeRef.String(), "name": cm.Name, "commune_code": cm.CommuneCode,
			"lines": toLines(cm.Lines), "total": cm.Total})
	}
	totals := make([]gin.H, 0, len(out.CommuneTotals))
	for _, t := range out.CommuneTotals {
		totals = append(totals, gin.H{"commune_code": t.CommuneCode, "machine_type": t.MachineType, "machine_type_name": t.TypeName, "quantity": t.Quantity})
	}
	c.JSON(http.StatusOK, gin.H{"cooperatives": coops, "commune_totals": totals})
}

func writeCoopMachines(c *gin.Context, cm app.CoopMachines, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"cooperative_ref": cm.CooperativeRef.String(), "name": cm.Name, "commune_code": cm.CommuneCode,
		"lines": toLines(cm.Lines), "total": cm.Total})
}

func (h Handler) knCoopMachines(c *gin.Context) {
	ref, ok := coopRef(c)
	if !ok {
		return
	}
	cm, err := h.Svc.CoopMachineCounts(c.Request.Context(), ref)
	writeCoopMachines(c, cm, err)
}

type machineLineReq struct {
	MachineType string          `json:"machine_type"`
	Quantity    json.RawMessage `json:"quantity"`
	Note        *string         `json:"note"`
}

type putMachinesReq struct {
	Lines  []machineLineReq `json:"lines"`
	Reason string           `json:"reason"`
}

func (h Handler) knPutMachines(c *gin.Context) {
	ref, ok := coopRef(c)
	if !ok {
		return
	}
	var req putMachinesReq
	if !bindJSON(c, &req) {
		return
	}
	if req.Lines == nil {
		httpx.Fail(c, problem.Validation.WithField("lines", "bắt buộc (mảng rỗng = bỏ mọi dòng)"))
		return
	}
	ls := make([]domain.CountLine, 0, len(req.Lines))
	var bad *problem.Problem
	for i, l := range req.Lines {
		field := "lines[" + strconv.Itoa(i) + "].quantity"
		raw := bytes.TrimSpace(l.Quantity)
		// Số lượng phải là số JSON nguyên (không nhận chuỗi, null, số thập phân).
		if len(raw) == 0 || raw[0] == '"' || bytes.Equal(raw, []byte("null")) {
			if bad == nil {
				bad = app.ErrQuantityInvalid
			}
			bad = bad.WithField(field, domain.ErrQuantityNotInteger.Error())
			continue
		}
		q, err := domain.ParseQuantity(string(raw))
		if err != nil {
			if bad == nil {
				bad = app.ErrQuantityInvalid
			}
			bad = bad.WithField(field, err.Error())
			continue
		}
		note := ""
		if l.Note != nil {
			note = *l.Note
		}
		ls = append(ls, domain.CountLine{MachineType: l.MachineType, Quantity: q, Note: note})
	}
	if bad != nil {
		httpx.Fail(c, bad)
		return
	}
	cm, err := h.Svc.PutMachineCounts(c.Request.Context(), app.PutMachineCountsInput{CooperativeRef: ref, Lines: ls, Reason: req.Reason,
		Department: c.Query("department"), IdemKey: strings.TrimSpace(c.GetHeader("Idempotency-Key"))})
	writeCoopMachines(c, cm, err)
}

type fieldChangeDTO struct {
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

func (h Handler) knMachineLog(c *gin.Context) {
	ref, ok := coopRef(c)
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
	evs, err := h.Svc.MachineCountLog(c.Request.Context(), ref, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(evs))
	for _, e := range evs {
		q := e.Quantity
		changes := []fieldChangeDTO{{Field: "quantity", Old: intStr(e.PrevQuantity), New: intStr(&q)}}
		if e.Action == domain.CountRemoved {
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
			changes = append(changes, fieldChangeDTO{Field: "note", Old: oldNote, New: newNote})
		}
		reason := e.Reason
		if reason == nil {
			reason = e.SupportReason
		}
		items = append(items, gin.H{"seq": e.Seq, "event": e.Action, "machine_type": e.MachineType, "machine_type_name": e.TypeName,
			"actor_ref": e.ActorRef.String(), "actor_display": nil, "at": e.EventTime.UTC().Format(time.RFC3339Nano),
			"recorded_at": e.RecordedAt.UTC().Format(time.RFC3339Nano), "changes": changes, "reason": reason,
			"department_code": e.DepartmentCode})
	}
	var next any
	if len(evs) == limit {
		next = evs[len(evs)-1].Seq
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}
