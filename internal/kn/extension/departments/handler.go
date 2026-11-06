// Package departments là bề mặt HTTP danh mục phòng ban (FN-AD-04.03, bước 2 đăng nhập FN-CH-00.01) và menu theo cấu
// hình (UR-KT-02) do chủ KN sở hữu (KNSPLIT-2 #118, #222). pf/party giữ dữ liệu, sổ pf_party.department_event, luật
// đơn vị/trạng thái/vai, bảng menu và nguồn đếm sử dụng; package này chỉ giữ binding route, đọc request và DTO, gọi qua
// partyport.DepartmentAdmin.
//
// Admin API (MFA): GET/POST /v1/departments, GET/PUT /v1/departments/{id}, POST /v1/departments/{id}/status|retire,
// GET /v1/departments/{id}/log, POST /v1/orgs/{ref}/departments/seed. Application API: GET /v1/departments,
// GET /v1/departments/{code}/modules, GET /v1/menu. Đường dẫn, thân JSON, mã lỗi và trạng thái HTTP giữ nguyên như
// trước khi chuyển (contracts/openapi/modules/extension/departments). Khu vực và cây tổ chức vẫn ở pf/party.
package departments

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ErrDependency: gốc lắp ráp chưa nối cổng quản trị phòng ban của pf/party — đóng, không trả dữ liệu.
var ErrDependency = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Chưa nối danh mục phòng ban của nền tảng")

// Handler gói partyport.DepartmentAdmin cho HTTP. Svc nil → mọi route trả 503 extension.dependency_unavailable.
type Handler struct{ Svc partyport.DepartmentAdmin }

// RegisterAdmin gắn route Admin API.
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.GET("/departments", h.adminList)
	rg.POST("/departments", h.create)
	rg.GET("/departments/:id", h.get)
	rg.PUT("/departments/:id", h.update)
	rg.POST("/departments/:id/status", h.status)
	rg.POST("/departments/:id/retire", h.retire)
	rg.GET("/departments/:id/log", h.log)
	rg.POST("/orgs/:ref/departments/seed", h.seed) // #222
}

// RegisterApp gắn route Application API (đọc).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/departments", h.appList)
	rg.GET("/departments/:code/modules", h.modules) // #222
	rg.GET("/menu", h.menu)                         // #222
}

// ---------------------------------------------------------------------------
// DTO (KNDepartment, KNDepartmentList, KNLogPage của hợp đồng OpenAPI)
// ---------------------------------------------------------------------------

type departmentDTO struct {
	ID           string  `json:"id"`
	OrgRef       string  `json:"org_ref"`
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	Description  *string `json:"description"`
	SortOrder    int     `json:"sort_order"`
	Status       string  `json:"status"`
	StatusLabel  string  `json:"status_label"`
	MenuProfile  string  `json:"menu_profile"`
	Retired      bool    `json:"retired"`
	Selectable   bool    `json:"selectable"`
	AccountCount *int    `json:"account_count"`
	RecordCount  *int    `json:"record_count"`
	UpdatedBy    string  `json:"updated_by"`
	UpdatedAt    string  `json:"updated_at"`
}

func toDepartmentDTO(d partyport.Department, u *partyport.Usage) departmentDTO {
	out := departmentDTO{ID: d.ID.String(), OrgRef: d.OrgRef.String(), Code: d.Code, Name: d.Name, Description: strPtr(d.Description),
		SortOrder: d.SortOrder, Status: d.Status, StatusLabel: partyport.DepartmentStatusLabel(d.Status), MenuProfile: d.MenuProfile,
		Retired: d.Retired, Selectable: d.Selectable(), UpdatedBy: d.UpdatedBy.String(), UpdatedAt: d.UpdatedAt.UTC().Format(time.RFC3339Nano)}
	if u != nil {
		a, r := u.Accounts, u.Records
		out.AccountCount, out.RecordCount = &a, &r
	}
	return out
}

type logEntryDTO struct {
	Seq          int64                   `json:"seq"`
	Event        string                  `json:"event"`
	ActorRef     string                  `json:"actor_ref"`
	ActorDisplay *string                 `json:"actor_display"`
	At           string                  `json:"at"`
	RecordedAt   string                  `json:"recorded_at"`
	Changes      []partyport.FieldChange `json:"changes"`
	Reason       *string                 `json:"reason"`
}

type departmentReq struct {
	OrgRef      string `json:"org_ref"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
	Status      string `json:"status"`
	MenuProfile string `json:"menu_profile"`
}

type statusReq struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type reasonReq struct {
	Reason string `json:"reason"`
}

// ---------------------------------------------------------------------------
// Tiện ích
// ---------------------------------------------------------------------------

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func bindJSON(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

// written: 201 lần đầu, 200 khi gọi lặp cùng Idempotency-Key được use case phát lại.
func written(c *gin.Context, replayed bool, body any) {
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, body)
}

func idemKey(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

// svc trả cổng đã nối; chưa nối → 503 và false.
func (h Handler) svc(c *gin.Context) (partyport.DepartmentAdmin, bool) {
	if h.Svc == nil {
		httpx.Fail(c, ErrDependency)
		return nil, false
	}
	return h.Svc, true
}

// pathID đọc :id; không phải UUID → 404 party.department_not_found (như id không có trong tenant).
func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, partyport.ErrDepartmentMissing)
		return uuid.Nil, false
	}
	return id, true
}

func optRefQuery(c *gin.Context, name string) (uuid.UUID, bool) {
	s := c.Query(name)
	if s == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(s)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, "phải là UUID"))
		return uuid.Nil, false
	}
	return id, true
}

func limitOf(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("limit"))
	return n
}

func clampLimit(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}

func afterSeq(c *gin.Context) (int64, bool) {
	s := c.Query("cursor")
	if s == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		httpx.Fail(c, problem.Validation.WithField("cursor", "phải là số seq ≥ 0"))
		return 0, false
	}
	return n, true
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

func (h Handler) list(c *gin.Context, admin bool) {
	svc, ok := h.svc(c)
	if !ok {
		return
	}
	unit, ok := optRefQuery(c, "org_ref")
	if !ok {
		return
	}
	vs, resolved, err := svc.ListDepartments(c.Request.Context(), unit, c.Query("include_retired") == "true", admin)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]departmentDTO, 0, len(vs))
	for _, v := range vs {
		items = append(items, toDepartmentDTO(v.Department, v.Usage))
	}
	c.JSON(http.StatusOK, gin.H{"org_ref": resolved.String(), "items": items})
}

func (h Handler) adminList(c *gin.Context) { h.list(c, true) }
func (h Handler) appList(c *gin.Context)   { h.list(c, false) }

func (h Handler) create(c *gin.Context) {
	svc, ok := h.svc(c)
	if !ok {
		return
	}
	var req departmentReq
	if !bindJSON(c, &req) {
		return
	}
	in := partyport.DepartmentInput{Code: req.Code, Name: req.Name, Description: req.Description, SortOrder: req.SortOrder,
		Status: req.Status, MenuProfile: req.MenuProfile}
	if ref, err := uuid.Parse(req.OrgRef); err == nil {
		in.OrgRef = ref
	}
	d, replayed, err := svc.CreateDepartment(c.Request.Context(), in, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/departments/"+d.ID.String())
	written(c, replayed, toDepartmentDTO(d, nil))
}

func (h Handler) get(c *gin.Context) {
	svc, ok := h.svc(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, err := svc.GetDepartment(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toDepartmentDTO(v.Department, v.Usage))
}

func (h Handler) update(c *gin.Context) {
	svc, ok := h.svc(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req departmentReq
	if !bindJSON(c, &req) {
		return
	}
	d, err := svc.UpdateDepartment(c.Request.Context(), id, partyport.DepartmentInput{Name: req.Name, Description: req.Description,
		SortOrder: req.SortOrder, MenuProfile: req.MenuProfile})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toDepartmentDTO(d, nil))
}

func (h Handler) status(c *gin.Context) {
	svc, ok := h.svc(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req statusReq
	if !bindJSON(c, &req) {
		return
	}
	res, err := svc.ChangeDepartmentStatus(c.Request.Context(), id, req.Status, req.Reason, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	warnings := res.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	written(c, res.Replayed, gin.H{"department": toDepartmentDTO(res.Department, res.Usage), "warnings": warnings})
}

func (h Handler) retire(c *gin.Context) {
	svc, ok := h.svc(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req reasonReq
	if !bindJSON(c, &req) {
		return
	}
	d, replayed, err := svc.RetireDepartment(c.Request.Context(), id, req.Reason, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	written(c, replayed, toDepartmentDTO(d, nil))
}

func (h Handler) log(c *gin.Context) {
	svc, ok := h.svc(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	after, ok := afterSeq(c)
	if !ok {
		return
	}
	es, err := svc.DepartmentLog(c.Request.Context(), id, after, limitOf(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]logEntryDTO, 0, len(es))
	for _, e := range es {
		items = append(items, logEntryDTO{Seq: e.Seq, Event: e.Event, ActorRef: e.ActorRef.String(), ActorDisplay: strPtr(e.ActorDisplay),
			At: e.At.UTC().Format(time.RFC3339Nano), RecordedAt: e.RecordedAt.UTC().Format(time.RFC3339Nano), Changes: e.Changes, Reason: e.Reason})
	}
	var next *int64
	if n := len(es); n > 0 && n == clampLimit(limitOf(c)) {
		s := es[n-1].Seq
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}
