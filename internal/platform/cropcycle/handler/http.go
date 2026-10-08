// Package handler: HTTP (Gin) của pf/cropcycle — van thu gom MKG ↔ HTX (lịch gom, bảng kê, phản hồi của HTX;
// luồng phản hồi cho MKG trên bề mặt đối tác). Route vụ/nhật ký/SOP (/v1/cropcycle/{sops,seasons,farm-logs,…},
// /v1/partner/season-harvest-dates) do pf/cooperative gắn từ W3.3 — đường dẫn không đổi.
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API vào nhóm /v1 (OIDC + Idempotency-Key cho POST).
func (h Handler) RegisterApp(rg *gin.RouterGroup) { h.registerCollection(rg) }

// RegisterAdmin: module không còn route Admin API (SOP, dựng lại bảng chiếu chuyển sang pf/cooperative).
func (h Handler) RegisterAdmin(*gin.RouterGroup) {}

func bindJSON(c *gin.Context, v any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func pathID(c *gin.Context, name string, notFound *problem.Problem) (uuid.UUID, bool) {
	u, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, notFound)
		return uuid.Nil, false
	}
	return u, true
}

// fields gom lỗi theo trường của một request.
type fields struct {
	p   *problem.Problem
	bad bool
}

func newFields() *fields { return &fields{p: problem.Validation} }

func (f *fields) add(field, reason string) { f.p, f.bad = f.p.WithField(field, reason), true }

func (f *fields) ref(field string, s *string) uuid.UUID {
	if s == nil || strings.TrimSpace(*s) == "" {
		return uuid.Nil
	}
	u, err := ids.ParseRef(strings.TrimSpace(*s))
	if err != nil {
		f.add(field, "phải là UUID")
	}
	return u
}

func cursor(c *gin.Context) (uuid.UUID, int, bool) {
	var after uuid.UUID
	if s := c.Query("cursor"); s != "" {
		u, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return uuid.Nil, 0, false
		}
		after = u
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	return after, limit, true
}

func qRef(c *gin.Context, f *fields, name string) uuid.UUID {
	s := c.Query(name)
	return f.ref(name, &s)
}

func next(n *uuid.UUID) *string {
	if n == nil {
		return nil
	}
	s := n.String()
	return &s
}

func created(c *gin.Context, replayed bool, body any) {
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, body)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }
