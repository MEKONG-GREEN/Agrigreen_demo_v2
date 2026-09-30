package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// registerLists gắn danh sách phân trang theo con trỏ (G4 đóng khoảng trống): lô có giá thành, doanh thu carbon
// đã ghi, job đóng kỳ. Con trỏ = khóa cuối trang (next_cursor), mới nhất trước.
func (h Handler) registerLists(rg *gin.RouterGroup) {
	rg.GET(base+"/lots", h.lots)
	rg.GET(base+"/carbon-revenue", h.carbonList)
	rg.GET(base+"/period-close-jobs", h.jobs)
}

func carbonDTO(x store.Carbon) gin.H {
	return gin.H{"id": x.ID, "period_ref": x.PeriodRef, "period_code": x.PeriodCode, "revenue_amount": x.Revenue,
		"retained_amount": x.Split.Retained, "farmers_amount": x.Split.Farmers, "authority_amount": x.Split.Authority,
		"verifier_amount": x.Split.Verifier, "retained_pct": x.Split.RetainedPct.StringFixed(2), "farmers_pct": x.Split.FarmersPct.StringFixed(2),
		"authority_pct": x.Split.AuthorityPct.StringFixed(2), "verifier_pct": x.Split.VerifierPct.StringFixed(2), "param_pins": x.Pins,
		"reference": x.Reference, "label": domain.ProvisionalLabel, "rule_version": domain.RuleCarbonSplit, "event_time": x.EventTime,
		"recorded_at": x.RecordedAt, "actor_ref": x.ActorRef,
		"note": "Chỉ chia doanh thu carbon đã ghi; không tính tCO2e (pf/mrv, chờ C-1)"}
}

// cursorAndLimit đọc ?cursor=<uuid>&limit=.
func cursorAndLimit(c *gin.Context) (*uuid.UUID, int, bool) {
	before, err := parseUUIDPtr("cursor", strPtrQ(c, "cursor"))
	if err != nil {
		httpx.Fail(c, err)
		return nil, 0, false
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	return before, limit, true
}

func page(c *gin.Context, items any, next *uuid.UUID) {
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func (h Handler) lots(c *gin.Context) {
	before, limit, ok := cursorAndLimit(c)
	if !ok {
		return
	}
	vs, next, err := h.Svc.LotCosts(c.Request.Context(), before, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(vs))
	for _, v := range vs {
		items = append(items, lotCostDTO(v))
	}
	page(c, items, next)
}

func (h Handler) carbonList(c *gin.Context) {
	period, err := parseUUIDPtr("period_ref", strPtrQ(c, "period_ref"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	before, limit, ok := cursorAndLimit(c)
	if !ok {
		return
	}
	xs, next, err := h.Svc.CarbonRevenues(c.Request.Context(), period, before, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(xs))
	for _, x := range xs {
		items = append(items, carbonDTO(x))
	}
	page(c, items, next)
}

func (h Handler) jobs(c *gin.Context) {
	period, err := parseUUIDPtr("period_id", strPtrQ(c, "period_id"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	before, limit, ok := cursorAndLimit(c)
	if !ok {
		return
	}
	js, next, err := h.Svc.CloseJobs(c.Request.Context(), period, c.Query("status"), before, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(js))
	for _, j := range js {
		items = append(items, jobDTO(j))
	}
	page(c, items, next)
}
