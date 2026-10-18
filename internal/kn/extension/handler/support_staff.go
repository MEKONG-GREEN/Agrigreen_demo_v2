package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// staffSupportDTO là một yêu cầu hỗ trợ trong danh sách của cán bộ (SupportRequestStaff).
// Không trả toạ độ: chỉ trạng thái GPS (nhãn "Thiếu vị trí GPS"); ảnh là mã tệp, tải
// qua pf/files theo quyền riêng.
type staffSupportDTO struct {
	ID             string   `json:"id"`
	HouseholdRef   string   `json:"household_ref"`
	PlotRef        *string  `json:"plot_ref"`
	CooperativeRef string   `json:"cooperative_ref"`
	CommuneCode    string   `json:"commune_code"`
	ProvinceCode   string   `json:"province_code"`
	Category       string   `json:"category"`
	Urgency        string   `json:"urgency"`
	Description    string   `json:"description"`
	PhotoRefs      []string `json:"photo_refs"`
	PhotoCount     int      `json:"photo_count"`
	GPSMissing     bool     `json:"gps_missing"`
	GPSAccuracyM   *string  `json:"gps_accuracy_m"`
	Labels         []string `json:"labels"`
	EventTime      string   `json:"event_time"`
	ReceivedAt     string   `json:"received_at"`
	AssignmentRule string   `json:"assignment_rule"`
	Task           taskDTO  `json:"task"`
}

func toStaffSupport(it app.StaffSupportItem, task taskDTO) staffSupportDTO {
	r := it.Request
	photos := make([]string, 0, len(r.PhotoRefs))
	for _, p := range r.PhotoRefs {
		photos = append(photos, p.String())
	}
	labels := []string{}
	if r.GPSMissing() {
		labels = append(labels, domain.LabelGPSMissing)
	}
	return staffSupportDTO{
		ID: r.ID.String(), HouseholdRef: r.HouseholdRef.String(), PlotRef: ref(r.PlotRef), CooperativeRef: r.CooperativeRef.String(),
		CommuneCode: r.CommuneCode, ProvinceCode: r.ProvinceCode, Category: r.Category, Urgency: r.Urgency, Description: r.Description,
		PhotoRefs: photos, PhotoCount: len(photos), GPSMissing: r.GPSMissing(), GPSAccuracyM: decp(r.GPSAccuracyM), Labels: labels,
		EventTime: ts(r.EventTime), ReceivedAt: ts(r.ReceivedAt), AssignmentRule: r.AssignmentRule, Task: task,
	}
}

// listStaffSupport: GET /v1/extension/support-requests — danh sách cho quản lý HTX và
// cán bộ khuyến nông (HTX US-SUPPORT-01, KN US-TASK-01/03).
func (h Handler) listStaffSupport(c *gin.Context) {
	before, ok := uuidCursor(c)
	if !ok {
		return
	}
	q := app.StaffSupportQuery{
		TaskStatus: c.Query("task_status"), Category: c.Query("category"), Urgency: c.Query("urgency"),
		CommuneCode: c.Query("commune_code"), Before: before, Limit: limitOf(c),
	}
	if s := c.Query("cooperative_ref"); s != "" {
		coop, err := ids.ParseRef(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("cooperative_ref", "phải là UUID"))
			return
		}
		q.Cooperative = coop
	}
	switch c.Query("gps_missing") {
	case "":
	case "true":
		v := true
		q.GPSMissing = &v
	case "false":
		v := false
		q.GPSMissing = &v
	default:
		httpx.Fail(c, problem.Validation.WithField("gps_missing", "phải là true hoặc false"))
		return
	}
	pg, err := h.Svc.ListSupportForStaff(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	now := h.Svc.Clock.Now()
	names := h.Svc.NamesFor(c.Request.Context(), pg.SupportTasks())
	items := make([]staffSupportDTO, 0, len(pg.Items))
	for _, it := range pg.Items {
		items = append(items, toStaffSupport(it, withNames(toTask(it.Task, now), it.Task, names)))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextUUID(pg.Next)})
}
