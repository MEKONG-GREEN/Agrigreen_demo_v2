package handler

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func (h Handler) archive(c *gin.Context) {
	q := app.ArchiveQuery{Kind: c.Query("kind"), Search: c.Query("q"), Limit: 20}
	// Check bounded plain integer syntax before conversion, never exponent notation.
	parse := func(key string, fallback int) (int, bool) {
		raw := c.Query(key)
		if raw == "" {
			return fallback, true
		}
		if len(raw) > 6 {
			return 0, false
		}
		for _, ch := range raw {
			if ch < '0' || ch > '9' {
				return 0, false
			}
		}
		v, e := strconv.Atoi(raw)
		return v, e == nil
	}
	var ok bool
	if q.Year, ok = parse("year", 0); !ok {
		httpx.Fail(c, app.ErrInvalid)
		return
	}
	if q.Offset, ok = parse("offset", 0); !ok {
		httpx.Fail(c, app.ErrInvalid)
		return
	}
	if q.Limit, ok = parse("limit", 20); !ok {
		httpx.Fail(c, app.ErrInvalid)
		return
	}
	switch c.Query("versions") {
	case "", "false":
	case "true":
		q.AllVersions = true
	default:
		httpx.Fail(c, app.ErrInvalid)
		return
	}
	out, err := h.Svc.Archive(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}
