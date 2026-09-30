// Package handler: HTTP của pf/extdata (contracts/openapi/application.yaml, mục pf/extdata).
package handler

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API (mọi người dùng đã xác thực; không vai riêng).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/extdata/sources", h.sources)
	rg.GET("/extdata/:source/point", h.point)
	rg.GET("/extdata/:source/tiles/:z/:x/:y", h.tile)
}

type sourceDTO struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	InTerritory bool   `json:"in_territory"`
	ADR         string `json:"adr,omitempty"`
	CellDeg     string `json:"cell_deg,omitempty"`
	MaxZoom     *int   `json:"max_zoom,omitempty"`
	TTLSeconds  int64  `json:"ttl_seconds"`
	Fake        bool   `json:"fake"`
}

func (h Handler) sources(c *gin.Context) {
	list, err := h.Svc.Sources(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]sourceDTO, 0, len(list))
	for _, s := range list {
		d := sourceDTO{ID: s.ID, Kind: string(s.Kind), InTerritory: s.InTerritory, ADR: s.ADR, TTLSeconds: int64(s.TTL / time.Second), Fake: s.Fake}
		if s.Kind.Point() {
			d.CellDeg = s.CellDeg.String()
		} else {
			z := s.MaxZoom
			d.MaxZoom = &z
		}
		items = append(items, d)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func maxAge(exp, now time.Time) string {
	s := int64(math.Max(0, exp.Sub(now).Seconds()))
	return "private, max-age=" + strconv.FormatInt(s, 10)
}

func (h Handler) point(c *gin.Context) {
	lat, lon := c.Query("lat"), c.Query("lon")
	if lat == "" || lon == "" {
		p := problem.Validation
		if lat == "" {
			p = p.WithField("lat", "bắt buộc")
		}
		if lon == "" {
			p = p.WithField("lon", "bắt buộc")
		}
		httpx.Fail(c, p)
		return
	}
	a, err := h.Svc.Point(c.Request.Context(), c.Param("source"), lat, lon)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", maxAge(a.Expires, h.Svc.Clock.Now()))
	c.JSON(http.StatusOK, gin.H{
		"source": a.Source.ID, "kind": string(a.Source.Kind),
		"cell":       gin.H{"lat": a.Cell.Lat.String(), "lon": a.Cell.Lon.String(), "size_deg": a.Cell.SizeDeg.String()},
		"fetched_at": a.FetchedAt.UTC().Format(time.RFC3339), "cached": a.Cached, "data": a.Data,
	})
}

func (h Handler) tile(c *gin.Context) {
	y := c.Param("y")
	if i := strings.IndexByte(y, '.'); i >= 0 { // 12/3/4.png
		y = y[:i]
	}
	zi, e1 := strconv.Atoi(c.Param("z"))
	xi, e2 := strconv.Atoi(c.Param("x"))
	yi, e3 := strconv.Atoi(y)
	if e1 != nil || e2 != nil || e3 != nil {
		httpx.Fail(c, problem.Validation.WithField("tile", "z/x/y phải là số nguyên"))
		return
	}
	a, err := h.Svc.Tile(c.Request.Context(), c.Param("source"), zi, xi, yi)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", maxAge(a.Expires, h.Svc.Clock.Now()))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, a.Tile.ContentType, a.Tile.Body)
}
