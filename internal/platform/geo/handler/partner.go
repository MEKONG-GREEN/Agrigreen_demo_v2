package handler

// Bề mặt đối tác /v1/partner/plots (ADR-0020): danh bạ thửa cho tài khoản dịch vụ tenant bên nhận.

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// partnerPlotDTO: hình học, diện tích, xã, HTX, trạng thái — không nông hộ, không tên thửa.
type partnerPlotDTO struct {
	Ref            string          `json:"ref"`
	Code           string          `json:"code"`
	CommuneCode    string          `json:"commune_code"`
	CooperativeRef *string         `json:"cooperative_ref"`
	Status         string          `json:"status"`
	AreaHa         string          `json:"area_ha"`
	Geometry       json.RawMessage `json:"geometry"`
	Revision       int64           `json:"revision"`
	UpdatedAt      string          `json:"updated_at"`
}

func toPartnerPlot(p store.PartnerPlot) partnerPlotDTO {
	d := partnerPlotDTO{Ref: p.Ref.String(), Code: p.Code, CommuneCode: p.CommuneCode, Status: p.Status, AreaHa: p.AreaHa, Geometry: p.Geometry,
		Revision: p.Revision, UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339Nano)}
	if p.CooperativeRef != nil {
		s := p.CooperativeRef.String()
		d.CooperativeRef = &s
	}
	return d
}

func partnerGrantor(c *gin.Context) (uuid.UUID, bool) {
	g, err := partyport.ParseGrantor(c.Query("grantor"))
	if err != nil {
		httpx.Fail(c, err)
		return uuid.Nil, false
	}
	return g, true
}

func optRef(c *gin.Context, name string) (uuid.UUID, bool) {
	s := c.Query(name)
	if s == "" {
		return uuid.Nil, true
	}
	u, err := uuid.Parse(s)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, "phải là UUID"))
		return uuid.Nil, false
	}
	return u, true
}

func (h Handler) partnerPlots(c *gin.Context) {
	grantor, ok := partnerGrantor(c)
	if !ok {
		return
	}
	coop, ok := optRef(c, "cooperative_ref")
	if !ok {
		return
	}
	after, ok := optRef(c, "cursor")
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	pg, err := h.Svc.PartnerPlots(c.Request.Context(), grantor, coop, after, limit, c.Request.URL.RawQuery)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]partnerPlotDTO, 0, len(pg.Items))
	for _, p := range pg.Items {
		items = append(items, toPartnerPlot(p))
	}
	var next *string
	if pg.Next != nil {
		s := pg.Next.String()
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func (h Handler) partnerPlot(c *gin.Context) {
	grantor, ok := partnerGrantor(c)
	if !ok {
		return
	}
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrPlotNotFound)
		return
	}
	p, err := h.Svc.PartnerPlot(c.Request.Context(), grantor, ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toPartnerPlot(p))
}
