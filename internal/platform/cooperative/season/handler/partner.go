package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/domain"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// harvestDatesDTO: ngày của vụ cho bên nhận (ADR-0020) — không nông hộ, không sản lượng.
type harvestDatesDTO struct {
	SeasonRef           string  `json:"season_ref"`
	PlotRef             string  `json:"plot_ref"`
	CooperativeRef      string  `json:"cooperative_ref"`
	CommuneCode         string  `json:"commune_code"`
	SeasonCode          string  `json:"season_code"`
	SeasonYear          int     `json:"season_year"`
	SowingDate          string  `json:"sowing_date"`
	ExpectedHarvestDate string  `json:"expected_harvest_date"`
	HarvestOpenDate     string  `json:"harvest_open_date"`
	Status              string  `json:"status"`
	CompletedAt         *string `json:"completed_at"`
	Cursor              int64   `json:"cursor"`
}

// partnerHarvestDates: GET /v1/partner/season-harvest-dates (cấp quyền season_harvest_dates).
func (h Handler) partnerHarvestDates(c *gin.Context) {
	grantor, err := partyport.ParseGrantor(c.Query("grantor"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var plot uuid.UUID
	if s := c.Query("plot_ref"); s != "" {
		if plot, err = uuid.Parse(s); err != nil {
			httpx.Fail(c, problem.Validation.WithField("plot_ref", "phải là UUID"))
			return
		}
	}
	var after int64
	if s := c.Query("after"); s != "" {
		if after, err = strconv.ParseInt(s, 10, 64); err != nil {
			httpx.Fail(c, problem.Validation.WithField("after", "phải là số nguyên ≥ 0"))
			return
		}
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	pg, err := h.Svc.PartnerHarvestDates(c.Request.Context(), grantor, plot, after, limit, c.Request.URL.RawQuery)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]harvestDatesDTO, 0, len(pg.Items))
	for _, v := range pg.Items {
		d := harvestDatesDTO{SeasonRef: v.SeasonRef.String(), PlotRef: v.PlotRef.String(), CooperativeRef: v.CooperativeRef.String(),
			CommuneCode: v.CommuneCode, SeasonCode: v.SeasonCode, SeasonYear: v.SeasonYear, SowingDate: domain.FormatDay(v.SowingDate),
			ExpectedHarvestDate: domain.FormatDay(v.ExpectedHarvestDate), HarvestOpenDate: domain.FormatDay(v.HarvestOpenDate), Status: v.Status,
			Cursor: v.SyncSeq}
		if v.CompletedAt != nil {
			s := v.CompletedAt.UTC().Format(time.RFC3339)
			d.CompletedAt = &s
		}
		items = append(items, d)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": pg.Next})
}
