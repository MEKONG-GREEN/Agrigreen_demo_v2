package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// registerSupport gắn route yêu cầu hỗ trợ (HTX US-SUPPORT-01, #313 — chuyển từ pf/extension, đường dẫn của nông dân
// giữ nguyên): nông dân gửi/xem của hộ mình; quản lý HTX/quản trị xem danh sách.
func (h Handler) registerSupport(rg *gin.RouterGroup) {
	rg.POST("/support-requests", h.submitSupport)
	rg.GET("/support-requests", h.listMySupport)
	rg.GET("/cropcycle/support-requests", h.listSupportForManagers)
}

type supportReq struct {
	ID *string `json:"id"` // UUIDv7 sinh trên máy (tùy chọn)
	app.SupportPayload
}

func (h Handler) submitSupport(c *gin.Context) {
	var req supportReq
	if !bindJSON(c, &req) {
		return
	}
	in, err := req.ToInput()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if req.ID != nil && *req.ID != "" {
		id, err := uuid.Parse(*req.ID)
		if err != nil || id.Version() != 7 {
			httpx.Fail(c, problem.Validation.WithField("id", "phải là UUIDv7"))
			return
		}
		in.ID = &id
	}
	in.IdemKey = idem(c)
	res, err := h.Svc.SubmitSupport(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, res.Replayed, gin.H{"support_request": json.RawMessage(app.SupportData(res.Request)), "replayed": res.Replayed})
}

// listMySupport: cursor là id (base64url) như trước khi chuyển module.
func (h Handler) listMySupport(c *gin.Context) {
	var before uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		u, perr := uuid.ParseBytes(b)
		if err != nil || perr != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		before = u
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	pg, err := h.Svc.ListMySupport(c.Request.Context(), before, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]json.RawMessage, 0, len(pg.Items))
	for _, r := range pg.Items {
		items = append(items, app.SupportData(r))
	}
	resp := gin.H{"items": items, "next_cursor": nil}
	if pg.Next != nil {
		resp["next_cursor"] = base64.RawURLEncoding.EncodeToString([]byte(pg.Next.String()))
	}
	c.JSON(http.StatusOK, resp)
}

// supportItemDTO là một yêu cầu hỗ trợ trong danh sách của quản lý HTX. Không trả toạ độ: chỉ trạng thái GPS (nhãn
// "Thiếu vị trí GPS") và độ chính xác; ảnh là mã tệp, tải qua pf/files theo quyền riêng.
type supportItemDTO struct {
	ID               string   `json:"id"`
	HouseholdRef     string   `json:"household_ref"`
	PlotRef          *string  `json:"plot_ref"`
	CooperativeRef   string   `json:"cooperative_ref"`
	CommuneCode      string   `json:"commune_code"`
	ProvinceCode     string   `json:"province_code"`
	Category         string   `json:"category"`
	Urgency          string   `json:"urgency"`
	Description      string   `json:"description"`
	PhotoRefs        []string `json:"photo_refs"`
	PhotoCount       int      `json:"photo_count"`
	GPSMissing       bool     `json:"gps_missing"`
	GPSAccuracyM     *string  `json:"gps_accuracy_m"`
	Labels           []string `json:"labels"`
	EventTime        string   `json:"event_time"`
	ReceivedAt       string   `json:"received_at"`
	LegacyTaskStatus *string  `json:"legacy_task_status"`
}

func toSupportItem(r domain.SupportRequest) supportItemDTO {
	photos := make([]string, 0, len(r.PhotoRefs))
	for _, p := range r.PhotoRefs {
		photos = append(photos, p.String())
	}
	labels := []string{}
	if r.GPSMissing() {
		labels = append(labels, domain.LabelGPSMissing)
	}
	d := supportItemDTO{
		ID: r.ID.String(), HouseholdRef: r.HouseholdRef.String(), CooperativeRef: r.CooperativeRef.String(),
		CommuneCode: r.CommuneCode, ProvinceCode: r.ProvinceCode, Category: r.Category, Urgency: r.Urgency,
		Description: r.Description, PhotoRefs: photos, PhotoCount: len(photos), GPSMissing: r.GPSMissing(), Labels: labels,
		EventTime: ts(r.EventTime), ReceivedAt: ts(r.ReceivedAt),
	}
	if r.PlotRef != uuid.Nil {
		s := r.PlotRef.String()
		d.PlotRef = &s
	}
	if r.GPSAccuracyM != nil {
		s := r.GPSAccuracyM.String()
		d.GPSAccuracyM = &s
	}
	if r.LegacyTaskStatus != "" {
		s := r.LegacyTaskStatus
		d.LegacyTaskStatus = &s
	}
	return d
}

// listSupportForManagers: GET /v1/cropcycle/support-requests — quản lý HTX (HTX mình) và quản trị (toàn tenant).
func (h Handler) listSupportForManagers(c *gin.Context) {
	before, limit, ok := cursor(c)
	if !ok {
		return
	}
	f := newFields()
	q := app.SupportQuery{Cooperative: qRef(c, f, "cooperative_ref"), Category: c.Query("category"), Urgency: c.Query("urgency"),
		CommuneCode: c.Query("commune_code"), Before: before, Limit: limit}
	switch c.Query("gps_missing") {
	case "":
	case "true", "false":
		v := c.Query("gps_missing") == "true"
		q.GPSMissing = &v
	default:
		f.add("gps_missing", "phải là true hoặc false")
	}
	if f.bad {
		httpx.Fail(c, f.p)
		return
	}
	pg, err := h.Svc.ListSupportForManagers(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]supportItemDTO, 0, len(pg.Items))
	for _, r := range pg.Items {
		items = append(items, toSupportItem(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next(pg.Next)})
}
