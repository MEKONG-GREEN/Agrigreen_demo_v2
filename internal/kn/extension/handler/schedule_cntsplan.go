package handler

// BF-CN-WEEKLY (PRD UP-06): kế hoạch tuần BM-05 Phần II đã đăng ký → Lịch công tác chờ duyệt
// (contracts/openapi/modules/extension/cnts-weekly, "/v1/extension/cnts/weekly-plan/schedule").

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type planScheduleReq struct {
	PeriodID string `json:"period_id"`
	Unit     string `json:"unit"`
}

func (h Handler) scheduleFromPlan(c *gin.Context) {
	var req planScheduleReq
	if !bind(c, &req) {
		return
	}
	pid, err := uuid.Parse(req.PeriodID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("period_id", "phải là UUID"))
		return
	}
	if req.Unit == "" {
		httpx.Fail(c, problem.Validation.WithField("unit", "bắt buộc"))
		return
	}
	d, err := h.Svc.ScheduleFromCNTSPlan(c.Request.Context(), pid, req.Unit, idem(c))
	h.writeDetail(c, d, err, http.StatusCreated)
}
