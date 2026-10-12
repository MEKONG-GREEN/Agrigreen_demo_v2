// Package handler: HTTP của pf/price (`cap/price`, ADR-0049) — Application API /v1/price/*. Hợp đồng ở
// contracts/openapi/application.yaml, mục "# --- pf/price ---". Bên dùng chọn bằng query `profile` (vắng → profile đầu
// tiên của gốc lắp ráp; KN = kn). Khung W11.71: trạng thái module + danh mục nền; route nghiệp vụ thêm ở W11.72–W11.76.
package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/price/status", h.status)
	rg.GET("/price/catalog", h.catalog)
	rg.POST("/price/catalog/defaults", h.defaults)
	h.registerCalendar(rg) // W11.74: lịch báo giá, giờ khóa, nhắc, ngưỡng (calendar.go)
}

type statusDTO struct {
	Profile     string `json:"profile"`
	Department  string `json:"department"`
	Level       string `json:"level"`
	CanManage   bool   `json:"can_manage"`
	CanSteward  bool   `json:"can_steward"` // W11.72: Quản trị nghiệp vụ báo giá
	RuleVersion string `json:"rule_version"`
	Day         string `json:"day"`
	Units       int    `json:"units"`
	Groups      int    `json:"groups"`
}

type entryDTO struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
	Version   int64  `json:"version"`
	ValidFrom string `json:"valid_from"`
}

type catalogDTO struct {
	Day     string     `json:"day"`
	Units   []entryDTO `json:"units"`
	Groups  []entryDTO `json:"groups"`
	Regions []entryDTO `json:"regions"` // W11.72 nhóm vùng
}

func (h Handler) status(c *gin.Context) {
	st, err := h.Svc.Status(c.Request.Context(), c.Query("profile"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, statusDTO{Profile: st.Profile.Name, Department: st.Profile.Department, Level: st.Level, CanManage: st.CanManage,
		CanSteward: st.CanSteward, RuleVersion: st.RuleVersion, Day: st.Day.Format(time.DateOnly), Units: st.Units, Groups: st.Groups})
}

func (h Handler) catalog(c *gin.Context) {
	var at time.Time
	if s := c.Query("at"); s != "" {
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithDetail("at phải là ngày YYYY-MM-DD"))
			return
		}
		at = d
	}
	v, err := h.Svc.CatalogAt(c.Request.Context(), c.Query("profile"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := catalogDTO{Day: v.Day.Format(time.DateOnly), Units: make([]entryDTO, 0, len(v.Units)), Groups: make([]entryDTO, 0, len(v.Groups)),
		Regions: make([]entryDTO, 0, len(v.Regions))}
	for _, u := range v.Units {
		out.Units = append(out.Units, entryDTO{Code: u.Code, Name: u.Name, SortOrder: u.SortOrder, Version: u.Version,
			ValidFrom: u.ValidFrom.Format(time.DateOnly)})
	}
	for _, g := range v.Groups {
		out.Groups = append(out.Groups, entryDTO{Code: g.Code, Name: g.Name, SortOrder: g.SortOrder, Version: g.Version,
			ValidFrom: g.ValidFrom.Format(time.DateOnly)})
	}
	for _, r := range v.Regions {
		out.Regions = append(out.Regions, entryDTO{Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, Version: r.Version,
			ValidFrom: r.ValidFrom.Format(time.DateOnly)})
	}
	c.JSON(http.StatusOK, out)
}

func (h Handler) defaults(c *gin.Context) {
	n, err := h.Svc.InstallDefaults(c.Request.Context(), c.Query("profile"), idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"written": n})
}
