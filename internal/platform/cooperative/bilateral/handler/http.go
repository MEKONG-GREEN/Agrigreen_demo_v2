// Package handler: HTTP phía HTX của luồng song phương (W5.6 — ADR-0034; pf/cooperative). Application API (App HTX,
// vai coop_manager, đơn vị công tác = Nút HTX):
//
//	GET  /v1/bilateral/pairs                          cặp của HTX người gọi
//	POST /v1/bilateral/pairs                          lập cặp với tenant đối tác {counterparty_tenant_id}
//	POST /v1/bilateral/pairs/{pair_id}/terminate      chấm dứt cặp {reason}
//	GET  /v1/bilateral/pairs/{pair_id}/records        bản ghi của cặp (?after=<seq>&limit=)
//	POST /v1/bilateral/confirmations                  xác nhận / phản đối số cuộn (bản ghi htx_roll_confirmation v1)
//
// Hợp đồng song phương còn proposed (A7, C-12): bật production chờ C-12 — route chỉ gắn khi gốc lắp ráp bật
// (cooperative.Module.UseBilateral).
package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/bilateral/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói app.Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API (nhóm /v1 đã xác thực, có middleware Idempotency-Key).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/bilateral/pairs", h.pairs)
	rg.POST("/bilateral/pairs", h.establish)
	rg.POST("/bilateral/pairs/:pair_id/terminate", h.terminate)
	rg.GET("/bilateral/pairs/:pair_id/records", h.records)
	rg.POST("/bilateral/confirmations", h.confirm)
}

func pairDTO(p app.Pair) gin.H {
	out := gin.H{"pair_id": p.PairID.String(), "htx_ref": p.HTXRef.String(), "counterparty_tenant_id": p.Counterparty.String(),
		"active": p.Active, "since": nil, "terminated_at": nil}
	if p.Active {
		out["since"] = p.Since.UTC().Format(time.RFC3339Nano)
	}
	if p.TerminatedAt != nil {
		out["terminated_at"] = p.TerminatedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

func recordDTO(r app.Record) gin.H {
	return gin.H{"id": r.ID.String(), "pair_id": r.PairID.String(), "seq": r.Seq, "contract": r.Contract + ".v" + strconv.Itoa(r.ContractVersion),
		"entity_key": r.EntityKey, "author_side": r.AuthorSide, "channel": r.Channel, "record": r.Payload, "payload_sha256": r.PayloadSHA256,
		"recorded_at": r.RecordedAt.UTC().Format(time.RFC3339Nano)}
}

func (h Handler) pairs(c *gin.Context) {
	ps, err := h.Svc.Pairs(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(ps))
	for _, p := range ps {
		items = append(items, pairDTO(p))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) establish(c *gin.Context) {
	var req struct {
		CounterpartyTenantID string `json:"counterparty_tenant_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("thân JSON không hợp lệ"))
		return
	}
	cp, err := uuid.Parse(req.CounterpartyTenantID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("counterparty_tenant_id", "UUID"))
		return
	}
	p, err := h.Svc.EstablishPair(c.Request.Context(), cp)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, pairDTO(p))
}

func pairParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("pair_id"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("pair_id", "UUID"))
		return uuid.Nil, false
	}
	return id, true
}

func (h Handler) terminate(c *gin.Context) {
	id, ok := pairParam(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("thân JSON không hợp lệ"))
		return
	}
	p, err := h.Svc.TerminatePair(c.Request.Context(), id, req.Reason)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, pairDTO(p))
}

func (h Handler) records(c *gin.Context) {
	id, ok := pairParam(c)
	if !ok {
		return
	}
	var after int64
	if v := c.Query("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			httpx.Fail(c, problem.Validation.WithField("after", "số nguyên ≥ 0"))
			return
		}
		after = n
	}
	limit := 100
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > app.MaxPageLimit {
			httpx.Fail(c, problem.Validation.WithField("limit", "1–1000"))
			return
		}
		limit = n
	}
	rs, err := h.Svc.Records(c.Request.Context(), id, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(rs))
	next := after
	for _, r := range rs {
		items = append(items, recordDTO(r))
		next = r.Seq
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_after": next})
}

func (h Handler) confirm(c *gin.Context) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 64<<10))
	if err != nil || !json.Valid(raw) {
		httpx.Fail(c, problem.Validation.WithDetail("thân JSON không hợp lệ"))
		return
	}
	r, created, err := h.Svc.Confirm(c.Request.Context(), raw)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	c.JSON(status, recordDTO(r))
}
