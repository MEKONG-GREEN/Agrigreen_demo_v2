package handler

import (
	"mime"
	"net/http"
	"strconv"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h Handler) registerPN215(rg *gin.RouterGroup) {
	g := rg.Group("/extension/khtc/departments/:department/program-progress")
	g.GET("/periods", h.pnPeriods215)
	g.GET("/:periodID", h.pnProgress215)
	g.GET("/:periodID/excel", h.pnExcel215)
}

func pnUUID215(raw string) (uuid.UUID, error) {
	if len(raw) != 36 {
		return uuid.Nil, app.ErrInvalid
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, app.ErrInvalid
	}
	return id, nil
}

func (h Handler) pnPeriods215(c *gin.Context) {
	raw := c.Query("year")
	if len(raw) != 4 {
		httpx.Fail(c, app.ErrInvalid.WithField("year", "Năm phải có 4 chữ số"))
		return
	}
	for i := range raw {
		if raw[i] < '0' || raw[i] > '9' {
			httpx.Fail(c, app.ErrInvalid.WithField("year", "Năm phải có 4 chữ số"))
			return
		}
	}
	year, err := strconv.Atoi(raw)
	if err != nil {
		httpx.Fail(c, app.ErrInvalid)
		return
	}
	after := uuid.Nil
	if raw = c.Query("after"); raw != "" {
		after, err = pnUUID215(raw)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
	}
	out, err := h.Svc.PNProgressPeriods215(c.Request.Context(), c.Param("department"), year, after)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}

func pnQuery215(c *gin.Context) (app.PNQuery215, bool) {
	q := app.PNQuery215{Level: c.Query("level")}
	for _, param := range []string{"snapshot_id", "program"} {
		if raw := c.Query(param); raw != "" {
			id, err := pnUUID215(raw)
			if err != nil {
				httpx.Fail(c, err)
				return q, false
			}
			if param == "snapshot_id" {
				q.SnapshotID = id
			} else {
				q.Program = id.String()
			}
		}
	}
	return q, true
}

func (h Handler) pnProgress215(c *gin.Context) {
	id, ok := path(c, "periodID")
	if !ok {
		return
	}
	q, ok := pnQuery215(c)
	if !ok {
		return
	}
	out, err := h.Svc.PNProgramProgress215(c.Request.Context(), c.Param("department"), id, q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}

func (h Handler) pnExcel215(c *gin.Context) {
	id, ok := path(c, "periodID")
	if !ok {
		return
	}
	q, ok := pnQuery215(c)
	if !ok {
		return
	}
	body, name, err := h.Svc.PNProgressExcel215(c.Request.Context(), c.Param("department"), id, q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", body)
}
