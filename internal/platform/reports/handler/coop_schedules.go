package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// HTX US-DASH-03: lịch gửi báo cáo của quản lý HTX trên Application API.

type recipientDTO struct {
	ActorRef    string `json:"actor_ref"`
	DisplayName string `json:"display_name" pii:"true"`
	MaskedEmail string `json:"masked_email" pii:"true"`
}

func (h Handler) coopRecipients(c *gin.Context) {
	list, err := h.Svc.CoopRecipients(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]recipientDTO, 0, len(list))
	for _, r := range list {
		out = append(out, recipientDTO{ActorRef: r.ActorRef.String(), DisplayName: r.DisplayName, MaskedEmail: r.MaskedEmail})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// scheduleFromReq đọc thân lập lịch; ghi lỗi và trả false nếu sai.
func scheduleFromReq(c *gin.Context) (domain.Schedule, bool) {
	var req scheduleReq
	if !bind(c, &req) {
		return domain.Schedule{}, false
	}
	s := domain.Schedule{
		TemplateKey: req.TemplateKey, Format: req.Format, UnitCode: req.UnitCode, Params: req.Params, Frequency: req.Frequency,
		Weekday: req.Weekday, DayOfMonth: req.DayOfMonth, Hour: req.Hour,
	}
	for i, r := range req.Recipients {
		id, err := ids.ParseRef(r)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("recipients", "phần tử "+strconv.Itoa(i)+" phải là actor_ref (UUID)"))
			return s, false
		}
		s.Recipients = append(s.Recipients, id)
	}
	return s, true
}

func (h Handler) createCoopSchedule(c *gin.Context) {
	s, ok := scheduleFromReq(c)
	if !ok {
		return
	}
	out, replayed, err := h.Svc.CreateCoopSchedule(c.Request.Context(), app.ScheduleInput{Schedule: s, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/reports/schedules/"+out.ID.String())
	c.JSON(status, toSchedule(out))
}

func (h Handler) coopSchedules(c *gin.Context) {
	list, err := h.Svc.CoopSchedules(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]scheduleDTO, 0, len(list))
	for _, s := range list {
		out = append(out, toSchedule(s))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) coopDeactivate(c *gin.Context) {
	id, ok := pathID(c, app.ErrScheduleNotFound)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	s, err := h.Svc.DeactivateCoopSchedule(c.Request.Context(), id, req.Reason)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toSchedule(s))
}

func (h Handler) coopRuns(c *gin.Context) {
	id, ok := pathID(c, app.ErrScheduleNotFound)
	if !ok {
		return
	}
	list, err := h.Svc.CoopScheduleRuns(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, r := range list {
		out = append(out, toRun(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}
