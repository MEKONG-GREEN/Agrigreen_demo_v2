package handler

// HTTP KN v7.7 gói P1 (contracts/openapi/application.yaml "pf/accounts KN v7.7", admin.yaml cùng tên):
//
//	Application API: GET /v1/me/contexts, POST /v1/me/context, GET /v1/me/context
//	Admin API (MFA): POST /v1/accounts/kn-users; /v1/accounts/users/{id}/assignments[/{aid}[/revoke]];
//	                 PATCH /v1/accounts/users/{id}/profile; GET /v1/accounts/users/{id}/log;
//	                 POST /v1/accounts/support-sessions[/{id}/end]; GET /v1/accounts/support-log;
//	                 GET /v1/accounts/mgmt-levels
//
// Middleware: header X-AG-Context → authz.KNContext (mọi route của cả hai bề mặt).

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// RegisterKNApp gắn route ngữ cảnh phiên vào Application API.
func (h Handler) RegisterKNApp(rg *gin.RouterGroup) {
	rg.GET("/me/contexts", h.myContexts)
	rg.POST("/me/context", h.selectContext)
	rg.GET("/me/context", h.currentContext)
	h.registerDelegationsApp(rg)   // W11.07
	h.registerInternalUnitsApp(rg) // W11.07
}

// RegisterKNAdmin gắn route quản trị KN vào Admin API.
func (h Handler) RegisterKNAdmin(rg *gin.RouterGroup) {
	rg.GET("/accounts/mgmt-levels", h.mgmtLevels)
	rg.POST("/accounts/kn-users", h.createKNUser)
	rg.GET("/accounts/users/:id/assignments", h.listAssignments)
	rg.POST("/accounts/users/:id/assignments", h.grantAssignment)
	rg.PUT("/accounts/users/:id/assignments/:aid", h.changeAssignment)
	rg.POST("/accounts/users/:id/assignments/:aid/revoke", h.revokeAssignment)
	rg.PATCH("/accounts/users/:id/profile", h.updateProfile)
	rg.GET("/accounts/users/:id/log", h.userLog)
	rg.POST("/accounts/support-sessions", h.startSupport)
	rg.POST("/accounts/support-sessions/:id/end", h.endSupport)
	rg.GET("/accounts/support-log", h.supportLog)
	h.registerKNImport(rg)           // W11.02: nạp Excel nhiều vai trò, phiên/lịch sử đăng nhập
	h.registerAttrsAdmin(rg)         // W11.07
	h.registerInternalUnitsAdmin(rg) // W11.07
}

// ---------------------------------------------------------------------------
// Middleware ngữ cảnh phiên
// ---------------------------------------------------------------------------

// Route ghi được miễn lý do trong chế độ hỗ trợ: đổi ngữ cảnh, mở/thoát phiên hỗ trợ.
var reasonExempt = map[string]bool{
	"/v1/me/context":                        true,
	"/v1/accounts/support-sessions":         true,
	"/v1/accounts/support-sessions/:id/end": true,
}

// Route ghi được trong ngữ cảnh chỉ xem: đổi ngữ cảnh (chọn phòng ban khác).
var viewOnlyExempt = map[string]bool{"/v1/me/context": true}

func isWrite(m string) bool {
	return m == http.MethodPost || m == http.MethodPut || m == http.MethodPatch || m == http.MethodDelete
}

// Middleware dựng authz.KNContext từ header X-AG-Context (gap §4.5): vai, phòng ban, phạm vi giải ở
// server; context lạ/của người khác/hết hạn/bị thu hồi → 403, không bao giờ âm thầm bỏ qua header.
// Chế độ hỗ trợ (FN-AD-04.07): thao tác ghi bắt buộc X-AG-Reason và ghi nhật ký trước + sau khi chạy.
func (h Handler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := strings.TrimSpace(c.GetHeader(authz.ContextHeader))
		if raw == "" {
			c.Next()
			return
		}
		ctx := c.Request.Context()
		p, ok := authz.FromContext(ctx)
		if !ok {
			httpx.Fail(c, problem.Unauthenticated)
			return
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Fail(c, app.ErrContextInvalid)
			return
		}
		kc, err := h.Svc.ResolveContext(ctx, id)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		// W11.07: ngữ cảnh chỉ xem (Lãnh đạo đơn vị ở phòng ban khác, Ban Giám đốc) không ghi được ở BẤT KỲ route
		// nào — chặn tập trung, không trông vào từng module (đổi ngữ cảnh vẫn được).
		if kc.ViewOnly && isWrite(c.Request.Method) && !viewOnlyExempt[c.FullPath()] {
			httpx.Fail(c, app.ErrContextReadOnly)
			return
		}
		audited := kc.Support != nil && isWrite(c.Request.Method) && !reasonExempt[c.FullPath()]
		if audited {
			reason, err := url.PathUnescape(c.GetHeader(authz.ReasonHeader))
			if err != nil || utf8.RuneCountInString(strings.TrimSpace(reason)) < domain.MinKNReason || utf8.RuneCountInString(reason) > 500 {
				httpx.Fail(c, app.ErrSupportReason)
				return
			}
			kc.Support.Reason = strings.TrimSpace(reason)
		}
		p.KN = kc
		binding, err := h.Sessions.ResolveSelected(ctx, p)
		if err != nil {
			httpx.Fail(c, app.ErrContextInvalid.WithCause(err))
			return
		}
		p.SelectedSession = &binding
		// Bind both the existing context and the complete registered owner projection.
		// Owner payload may carry current grants absent from the compatibility context.
		stamp, err := binding.AuthorizationStamp()
		if err != nil {
			httpx.Fail(c, app.ErrContextInvalid.WithCause(err))
			return
		}
		scope, err := json.Marshal(struct {
			Context *authz.KNContext
			Owner   []byte
		}{kc, stamp})
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		ctx = idempotency.WithAuthorizationScope(ctx, scope)
		c.Request = c.Request.WithContext(authz.WithPrincipal(ctx, p))
		if !audited {
			c.Next()
			return
		}
		params := map[string]any{}
		for _, prm := range c.Params {
			params[prm.Key] = prm.Value
		}
		in := app.SupportActionInput{RequestID: ids.New(), Phase: "attempted", Function: c.Request.Method + " " + c.FullPath(),
			Content: map[string]any{"path_params": params}}
		if err := h.Svc.LogSupportAction(c.Request.Context(), in); err != nil {
			httpx.Fail(c, err) // không ghi được nhật ký → không chạy thao tác hỗ trợ
			return
		}
		c.Next()
		in.Phase, in.Status = "completed", c.Writer.Status()
		if err := h.Svc.LogSupportAction(c.Request.Context(), in); err != nil {
			_ = c.Error(err) // dòng attempted đã có; lỗi ghi completed vào log máy chủ
		}
	}
}

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type communeDTO struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type deptRefDTO struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Status      string  `json:"status"`
	ReadOnly    bool    `json:"read_only"` // W11.07: chọn được ở chế độ chỉ xem
}

type assignmentDTO struct {
	AssignmentID  string       `json:"assignment_id"`
	UserRef       string       `json:"user_ref"`
	Role          string       `json:"role"`
	OrgRef        *string      `json:"org_ref"`
	OrgName       string       `json:"org_name"`
	MgmtLevel     *string      `json:"mgmt_level"`
	MgmtLevelCode *string      `json:"mgmt_level_code"`
	Communes      []communeDTO `json:"communes"`
	Departments   []deptRefDTO `json:"departments"`
	ValidFrom     string       `json:"valid_from"`
	ValidTo       *string      `json:"valid_to"`
	Active        bool         `json:"active"`
	CreatedBy     string       `json:"created_by"`
	CreatedAt     string       `json:"created_at"`
	UpdatedBy     string       `json:"updated_by"`
	UpdatedAt     string       `json:"updated_at"`
	Attributes    attrsDTO     `json:"attributes"` // W11.07
}

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func communesDTO(cs []app.CommuneView) []communeDTO {
	out := make([]communeDTO, 0, len(cs))
	for _, c := range cs {
		out = append(out, communeDTO{Code: c.Code, Name: c.Name})
	}
	return out
}

func toAssignmentDTO(v app.AssignmentView) assignmentDTO {
	d := assignmentDTO{AssignmentID: v.ID.String(), UserRef: v.UserRef.String(), Role: v.Role, OrgRef: refPtr(v.OrgRef), OrgName: v.OrgName,
		MgmtLevel: strPtr(v.MgmtLevelLabel), MgmtLevelCode: strPtr(v.MgmtLevel), Communes: communesDTO(v.CommuneViews),
		Departments: []deptRefDTO{}, ValidFrom: rfc(v.ValidFrom), Active: v.Active, CreatedBy: v.CreatedBy.String(), CreatedAt: rfc(v.CreatedAt),
		UpdatedBy: v.UpdatedBy.String(), UpdatedAt: rfc(v.UpdatedAt)}
	if v.ValidTo != nil {
		s := rfc(*v.ValidTo)
		d.ValidTo = &s
	}
	for _, x := range v.DepartmentRefs {
		d.Departments = append(d.Departments, deptRefDTO{Code: x.Code, Name: x.Name, Description: strPtr(x.Description), Status: x.Status,
			ReadOnly: x.ReadOnly})
	}
	d.Attributes = toAttrsDTO(v.Attrs)
	return d
}

type contextDTO struct {
	ContextID        string       `json:"context_id"`
	AssignmentID     *string      `json:"assignment_id"`
	Role             string       `json:"role"`
	DepartmentCode   *string      `json:"department_code"`
	DepartmentName   *string      `json:"department_name"`
	OrgRef           *string      `json:"org_ref"`
	OrgName          string       `json:"org_name"`
	Communes         []communeDTO `json:"communes"`
	DefaultRoute     string       `json:"default_route"`
	ExpiresAt        string       `json:"expires_at"`
	SupportSessionID *string      `json:"support_session_id"`
	OnBehalfOf       *string      `json:"on_behalf_of"`
	// W11.07: phân quyền trong vai của ngữ cảnh (powers chỉ đầy đủ ở GET /v1/me/context).
	ReadOnly         bool     `json:"read_only"`
	MgmtLevelCode    *string  `json:"mgmt_level_code"`
	Manager          bool     `json:"manager"`
	ProvinceGroup    *string  `json:"province_group"`
	Duties           []string `json:"duties"`
	InternalUnitCode *string  `json:"internal_unit_code"`
	Powers           []string `json:"powers"`
}

func toContextDTO(v app.ContextView) contextDTO {
	d := contextDTO{ContextID: v.ContextID.String(), AssignmentID: refPtr(v.AssignmentID), Role: v.Role, OrgRef: refPtr(v.OrgRef),
		OrgName: v.OrgName, Communes: communesDTO(v.Communes), DefaultRoute: v.DefaultRoute, ExpiresAt: rfc(v.ExpiresAt),
		SupportSessionID: refPtr(v.SupportID), OnBehalfOf: refPtr(v.OnBehalfOf), ReadOnly: v.ReadOnly, MgmtLevelCode: strPtr(v.MgmtLevel),
		Manager: v.Manager, ProvinceGroup: strPtr(v.ProvinceGroup), Duties: nonNilStr(v.Duties), InternalUnitCode: strPtr(v.InternalUnit),
		Powers: nonNilStr(v.Powers)}
	if v.Department != nil {
		d.DepartmentCode, d.DepartmentName = strPtr(v.Department.Code), strPtr(v.Department.Name)
	}
	return d
}

type warningDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func warningsDTO(ws []app.Warning) []warningDTO {
	out := make([]warningDTO, 0, len(ws))
	for _, w := range ws {
		out = append(out, warningDTO(w))
	}
	return out
}

// ---------------------------------------------------------------------------
// Application API
// ---------------------------------------------------------------------------

func (h Handler) myContexts(c *gin.Context) {
	vs, err := h.Svc.MyContexts(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]assignmentDTO, 0, len(vs))
	for _, v := range vs {
		items = append(items, toAssignmentDTO(v))
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, gin.H{"assignments": items})
}

type selectReq struct {
	AssignmentID   string  `json:"assignment_id"`
	DepartmentCode *string `json:"department_code"`
}

func (h Handler) selectContext(c *gin.Context) {
	var req selectReq
	if !bindJSON(c, &req) {
		return
	}
	id, err := uuid.Parse(req.AssignmentID)
	if err != nil {
		httpx.Fail(c, app.ErrAssignmentNotFound)
		return
	}
	dept := ""
	if req.DepartmentCode != nil {
		dept = *req.DepartmentCode
	}
	v, err := h.Svc.SelectContext(c.Request.Context(), id, dept, key(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if v.Replayed {
		status = http.StatusOK
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(status, toContextDTO(v))
}

func (h Handler) currentContext(c *gin.Context) {
	v, err := h.Svc.CurrentContext(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, toContextDTO(v))
}

// ---------------------------------------------------------------------------
// Admin API
// ---------------------------------------------------------------------------

func (h Handler) mgmtLevels(c *gin.Context) {
	if _, err := authz.Require(c.Request.Context(), port.AdminRoles...); err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(port.MgmtLevels))
	for _, l := range port.MgmtLevels {
		items = append(items, gin.H{"code": l.Code, "label": l.Label, "manager": l.Manager, "reads_all_departments": l.ReadsAllDepartments})
	}
	// W11.07: danh mục phân quyền trong vai (Q-059) — cùng phiên bản, cùng trạng thái tạm.
	c.JSON(http.StatusOK, gin.H{"items": items, "provisional": true, "version": port.MgmtCatalogVersion,
		"province_groups": catalogDTO(port.ProvinceGroups), "duties": catalogDTO(port.Duties), "powers": catalogDTO(port.Powers),
		"delegation_max_days": domain.DelegationMaxDays})
}

type assignmentReq struct {
	Role        string   `json:"role"`
	OrgRef      *string  `json:"org_ref"`
	Departments []string `json:"department_codes"`
	MgmtLevel   string   `json:"mgmt_level"`
	Communes    []string `json:"commune_codes"`
	ValidFrom   *string  `json:"valid_from"`
	ValidTo     *string  `json:"valid_to"`
	Reason      string   `json:"reason"`
}

func (r assignmentReq) input() (domain.AssignmentInput, error) {
	org, err := optRef(r.OrgRef, "org_ref")
	if err != nil {
		return domain.AssignmentInput{}, err
	}
	in := domain.AssignmentInput{Role: r.Role, OrgRef: org, Departments: r.Departments, MgmtLevel: r.MgmtLevel, Communes: r.Communes}
	if r.ValidFrom != nil && *r.ValidFrom != "" {
		t, err := time.Parse(time.RFC3339, *r.ValidFrom)
		if err != nil {
			return in, problem.Validation.WithField("valid_from", "RFC 3339")
		}
		in.ValidFrom = t
	}
	if r.ValidTo != nil && *r.ValidTo != "" {
		t, err := time.Parse(time.RFC3339, *r.ValidTo)
		if err != nil {
			return in, problem.Validation.WithField("valid_to", "RFC 3339")
		}
		in.ValidTo = &t
	}
	return in, nil
}

func (h Handler) views(c *gin.Context, as []port.Assignment) ([]assignmentDTO, bool) {
	vs, err := h.Svc.Views(c.Request.Context(), as)
	if err != nil {
		httpx.Fail(c, err)
		return nil, false
	}
	out := make([]assignmentDTO, 0, len(vs))
	for _, v := range vs {
		out = append(out, toAssignmentDTO(v))
	}
	return out, true
}

type knUserReq struct {
	FullName    string          `json:"full_name" pii:"true"`
	Email       string          `json:"email" pii:"true"`
	Phone       string          `json:"phone" pii:"true"`
	Assignments []assignmentReq `json:"assignments"`
}

func (h Handler) createKNUser(c *gin.Context) {
	var req knUserReq
	if !bindJSON(c, &req) {
		return
	}
	first, last := domain.SplitFullName(req.FullName)
	in := app.KNCreateInput{Profile: domain.Profile{Email: req.Email, FirstName: first, LastName: last, Phone: req.Phone}, IdemKey: key(c)}
	for _, a := range req.Assignments {
		x, err := a.input()
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		in.Assignments = append(in.Assignments, x)
	}
	res, err := h.Svc.CreateKNUser(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	asgs, ok := h.views(c, res.Assignments)
	if !ok {
		return
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/accounts/users/"+res.User.ID.String())
	c.JSON(status, gin.H{"user": toUserDTO(app.UserView{User: res.User, Roles: []string{port.RoleKNUser}, Tracked: true}),
		"assignments": asgs, "activation_email_sent": res.ActivationSent, "warnings": warningsDTO(res.Warnings)})
}

func pathRef(c *gin.Context, name string, nf error) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, nf)
		return uuid.Nil, false
	}
	return id, true
}

func (h Handler) listAssignments(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	as, err := h.Svc.ListAssignments(c.Request.Context(), id, c.Query("include_revoked") == "true")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items, ok := h.views(c, as)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) writeAsg(c *gin.Context, res app.AssignmentResult, err error, created bool) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items, ok := h.views(c, []port.Assignment{res.Assignment})
	if !ok {
		return
	}
	status := http.StatusOK
	if created && !res.Replayed {
		status = http.StatusCreated
	}
	c.JSON(status, gin.H{"assignment": items[0], "seq": res.Seq, "replayed": res.Replayed, "warnings": warningsDTO(res.Warnings)})
}

func (h Handler) grantAssignment(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req assignmentReq
	if !bindJSON(c, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	res, err := h.Svc.GrantAssignment(c.Request.Context(), id, in, req.Reason, key(c))
	h.writeAsg(c, res, err, true)
}

// headerKey: Idempotency-Key cho PUT/PATCH (middleware chỉ bắt với POST); vắng → khóa mới.
func headerKey(c *gin.Context) string {
	if k := strings.TrimSpace(c.GetHeader("Idempotency-Key")); k != "" {
		return k
	}
	return ids.New().String()
}

func (h Handler) changeAssignment(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	aid, ok := pathRef(c, "aid", app.ErrAssignmentNotFound)
	if !ok {
		return
	}
	var req assignmentReq
	if !bindJSON(c, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	res, err := h.Svc.ChangeAssignment(c.Request.Context(), id, aid, in, req.Reason, headerKey(c))
	h.writeAsg(c, res, err, false)
}

func (h Handler) revokeAssignment(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	aid, ok := pathRef(c, "aid", app.ErrAssignmentNotFound)
	if !ok {
		return
	}
	var req reasonReq
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.Svc.RevokeAssignment(c.Request.Context(), id, aid, req.Reason, key(c))
	h.writeAsg(c, res, err, false)
}

type profileReq struct {
	FullName *string `json:"full_name" pii:"true"`
	Email    *string `json:"email" pii:"true"`
	Phone    *string `json:"phone" pii:"true"`
	Reason   string  `json:"reason"`
}

func (h Handler) updateProfile(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req profileReq
	if !bindJSON(c, &req) {
		return
	}
	pc, replayed, err := h.Svc.UpdateProfile(c.Request.Context(), id, app.ProfilePatch{FullName: req.FullName, Email: req.Email, Phone: req.Phone,
		Reason: req.Reason, IdemKey: headerKey(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	v, err := h.Svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": toUserDTO(v), "changed_fields": pc.Fields, "seq": pc.Seq, "replayed": replayed})
}

type logChangeDTO struct {
	Field string  `json:"field"`
	Old   *string `json:"old"`
	New   *string `json:"new"`
}

type userLogDTO struct {
	Seq          int64          `json:"seq"`
	Event        string         `json:"event"`
	EntityRef    *string        `json:"entity_ref"`
	ActorRef     string         `json:"actor_ref"`
	ActorDisplay *string        `json:"actor_display"`
	At           string         `json:"at"`
	RecordedAt   string         `json:"recorded_at"`
	Changes      []logChangeDTO `json:"changes"`
	Reason       *string        `json:"reason"`
}

func clamp(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}

func (h Handler) userLog(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var after int64
	if s := c.Query("cursor"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 0 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		after = n
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	limit = clamp(limit)
	es, err := h.Svc.UserLog(c.Request.Context(), id, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]userLogDTO, 0, len(es))
	for _, e := range es {
		d := userLogDTO{Seq: e.Seq, Event: e.Event, EntityRef: refPtr(e.EntityRef), ActorRef: e.ActorRef.String(), ActorDisplay: strPtr(e.ActorDisplay),
			At: rfc(e.At), RecordedAt: rfc(e.RecordedAt), Changes: []logChangeDTO{}, Reason: e.Reason}
		for _, ch := range e.Changes {
			d.Changes = append(d.Changes, logChangeDTO(ch))
		}
		items = append(items, d)
	}
	var next *int64 // số như khung Log của P2 (KNLogPage)
	if n := len(es); n > 0 && n == limit {
		s := es[n-1].Seq
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

type supportReq struct {
	TargetAssignmentID *string  `json:"target_assignment_id"`
	Role               string   `json:"role"`
	OrgRef             *string  `json:"org_ref"`
	Communes           []string `json:"commune_codes"`
	DepartmentCode     string   `json:"department_code"`
	Reason             string   `json:"reason"`
}

func (h Handler) startSupport(c *gin.Context) {
	var req supportReq
	if !bindJSON(c, &req) {
		return
	}
	ta, err := optRef(req.TargetAssignmentID, "target_assignment_id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	org, err := optRef(req.OrgRef, "org_ref")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	v, err := h.Svc.StartSupport(c.Request.Context(), app.SupportInput{TargetAssignment: ta, Role: req.Role, OrgRef: org, Communes: req.Communes,
		Department: req.DepartmentCode, Reason: req.Reason, IdemKey: key(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if v.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, toContextDTO(v))
}

func (h Handler) endSupport(c *gin.Context) {
	id, ok := pathRef(c, "id", app.ErrSupportNotFound)
	if !ok {
		return
	}
	s, err := h.Svc.EndSupport(c.Request.Context(), id, key(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"support_session_id": s.ID.String(), "ended_at": timeStr(s.EndedAt)})
}

type supportLogDTO struct {
	ID                string         `json:"id"`
	SupportSessionID  string         `json:"support_session_id"`
	ContextID         string         `json:"context_id"`
	RequestID         string         `json:"request_id"`
	Phase             string         `json:"phase"`
	ActorRef          string         `json:"actor_ref"`
	ActorDisplay      *string        `json:"actor_display"`
	OnBehalfOf        *string        `json:"on_behalf_of"`
	OnBehalfOfDisplay *string        `json:"on_behalf_of_display"`
	ActedRole         string         `json:"acted_role"`
	DepartmentCode    string         `json:"department_code"`
	Function          string         `json:"function"`
	Content           map[string]any `json:"content"`
	StatusCode        *int           `json:"status_code"`
	Reason            string         `json:"reason"`
	TraceID           *string        `json:"trace_id"`
	At                string         `json:"at"`
}

func (h Handler) supportLog(c *gin.Context) {
	var session uuid.UUID
	if s := c.Query("support_session_id"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("support_session_id", "phải là UUID"))
			return
		}
		session = id
	}
	var beforeAt *time.Time
	var beforeID uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		at, id, found := strings.Cut(string(b), "|")
		t, terr := time.Parse(time.RFC3339Nano, at)
		u, uerr := uuid.Parse(id)
		if err != nil || !found || terr != nil || uerr != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		beforeAt, beforeID = &t, u
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	limit = clamp(limit)
	rows, err := h.Svc.SupportLog(c.Request.Context(), session, beforeAt, beforeID, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]supportLogDTO, 0, len(rows))
	for _, r := range rows {
		d := supportLogDTO{ID: r.ID.String(), SupportSessionID: r.SessionID.String(), ContextID: r.ContextID.String(), RequestID: r.RequestID.String(),
			Phase: r.Phase, ActorRef: r.ActorRef.String(), ActorDisplay: strPtr(r.ActorDisplay), OnBehalfOf: refPtr(r.OnBehalfOf),
			OnBehalfOfDisplay: strPtr(r.OnBehalfOfDisplay), ActedRole: r.ActedRole, DepartmentCode: r.Department, Function: r.Function,
			StatusCode: r.Status, Reason: r.Reason, TraceID: strPtr(r.TraceID), At: rfc(r.EventTime)}
		_ = json.Unmarshal(r.Content, &d.Content)
		items = append(items, d)
	}
	var next *string
	if n := len(rows); n > 0 && n == limit {
		s := base64.RawURLEncoding.EncodeToString([]byte(rows[n-1].RecordedAt.UTC().Format(time.RFC3339Nano) + "|" + rows[n-1].ID.String()))
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}
