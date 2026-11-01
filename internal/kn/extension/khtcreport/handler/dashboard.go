package handler

import (
	pa "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
	"strconv"
)

func (h Handler) RegisterDashboard(rg *gin.RouterGroup, periods *pa.Service) {
	rg.GET("/extension/khtc/dashboard", func(c *gin.Context) {
		q := app.DashboardQuery{Department: c.Query("department"), Field: c.Query("field")}
		for _, x := range []struct {
			name   string
			target *int
		}{{"year", &q.Year}, {"quarter", &q.Quarter}} {
			if raw := c.Query(x.name); raw != "" {
				n, e := strconv.Atoi(raw)
				if e != nil || len(raw) > 4 || (x.name == "year" && (n < 2000 || n > 2100)) || (x.name == "quarter" && (n < 1 || n > 4)) {
					httpx.Fail(c, problem.Validation.WithField(x.name, "số nguyên hợp lệ"))
					return
				}
				*x.target = n
			}
		}
		d, e := h.Svc.Dashboard(c.Request.Context(), q, periods)
		if e != nil {
			httpx.Fail(c, e)
			return
		}
		c.JSON(200, d)
	})
}
