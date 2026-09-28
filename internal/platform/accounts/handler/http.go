// Package handler: HTTP của pf/accounts trên Admin API (contracts/openapi/admin.yaml,
// phần "# --- pf/accounts ---"). Mọi route ở bề mặt Admin: OIDC + MFA bắt buộc.
package handler

import (
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// MaxImportBytes giới hạn tệp nhập tài khoản (CSV/XLSX).
const MaxImportBytes = 4 << 20

// Handler gói app.Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterAdmin gắn route Admin API.
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.GET("/accounts/grants", h.grants)
	rg.GET("/accounts/users", h.list)
	rg.POST("/accounts/users", h.create)
	httpx.AllowBody(rg, http.MethodPost, "/accounts/users/import", MaxImportBytes)
	rg.POST("/accounts/users/import", h.importUsers)
	rg.POST("/accounts/users/bulk", h.bulk)
	rg.GET("/accounts/users/:id", h.get)
	rg.GET("/accounts/users/:id/events", h.events)
	rg.POST("/accounts/users/:id/lock", h.lock)
	rg.POST("/accounts/users/:id/unlock", h.unlock)
	rg.POST("/accounts/users/:id/resend-actions", h.resend)
	rg.POST("/accounts/users/:id/assignment", h.assign)
}

// ---------------------------------------------------------------------------
// DTO — trả cho quản trị viên; không ghi log (R-14).
// ---------------------------------------------------------------------------

type userDTO struct {
	ID              string   `json:"id"`
	Username        string   `json:"username" pii:"true"`
	Email           string   `json:"email" pii:"true"`
	FirstName       string   `json:"first_name" pii:"true"`
	LastName        string   `json:"last_name" pii:"true"`
	Phone           *string  `json:"phone" pii:"true"`
	Enabled         bool     `json:"enabled"`
	EmailVerified   bool     `json:"email_verified"`
	OrgRef          *string  `json:"org_ref"`
	Roles           []string `json:"roles"`
	LockCause       *string  `json:"lock_cause"`
	RequiredActions []string `json:"required_actions"`
	CreatedAt       *string  `json:"created_at"`
	Tracked         bool     `json:"tracked"`
}

func refPtr(u uuid.UUID) *string {
	if u == uuid.Nil {
		return nil
	}
	s := u.String()
	return &s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toUserDTO(v app.UserView) userDTO {
	u := v.User
	d := userDTO{
		ID: u.ID.String(), Username: u.Username, Email: u.Email, FirstName: u.FirstName, LastName: u.LastName,
		Phone: strPtr(u.Phone), Enabled: u.Enabled, EmailVerified: u.EmailVerified, OrgRef: refPtr(u.OrgRef),
		Roles: v.Roles, LockCause: strPtr(string(v.LockCause)), RequiredActions: u.RequiredActions, Tracked: v.Tracked,
	}
	if d.RequiredActions == nil {
		d.RequiredActions = []string{}
	}
	if !u.CreatedAt.IsZero() {
		s := u.CreatedAt.UTC().Format(time.RFC3339)
		d.CreatedAt = &s
	}
	return d
}

type accountDTO struct {
	UserRef   string   `json:"user_ref"`
	OrgRef    string   `json:"org_ref"`
	Roles     []string `json:"roles"`
	Enabled   bool     `json:"enabled"`
	LockCause *string  `json:"lock_cause"`
}

func toAccountDTO(a domain.Account) accountDTO {
	roles := a.Roles
	if roles == nil {
		roles = []string{}
	}
	return accountDTO{UserRef: a.UserRef.String(), OrgRef: a.OrgRef.String(), Roles: roles, Enabled: a.Enabled, LockCause: strPtr(string(a.LockCause))}
}

type eventDTO struct {
	Seq        int64          `json:"seq"`
	Action     string         `json:"action"`
	EventTime  string         `json:"event_time"`
	RecordedAt string         `json:"recorded_at"`
	ActorRef   string         `json:"actor_ref"`
	Reason     *string        `json:"reason"`
	Account    accountDTO     `json:"account"`
	Details    map[string]any `json:"details,omitempty"`
}

func toEventDTO(e domain.Event) eventDTO {
	return eventDTO{
		Seq: e.Seq, Action: string(e.Action), EventTime: e.EventTime.UTC().Format(time.RFC3339Nano),
		RecordedAt: e.RecordedAt.UTC().Format(time.RFC3339Nano), ActorRef: e.ActorRef.String(), Reason: strPtr(e.Reason),
		Account: toAccountDTO(e.Account), Details: e.Details,
	}
}

func bindJSON(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrUserNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func optRef(s *string, field string) (uuid.UUID, error) {
	if s == nil || *s == "" {
		return uuid.Nil, nil
	}
	u, err := uuid.Parse(*s)
	if err != nil || u == uuid.Nil {
		return uuid.Nil, problem.Validation.WithField(field, "phải là UUID")
	}
	return u, nil
}

func key(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

func (h Handler) grants(c *gin.Context) {
	p, err := authz.Require(c.Request.Context(), port.AdminRoles...)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	g := port.Grantable(p.Roles)
	if g == nil {
		g = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"grantable": g, "mfa_roles": port.MFARoles, "role_org_kinds": port.RoleOrgKinds})
}

type createReq struct {
	Username  string   `json:"username" pii:"true"`
	Email     string   `json:"email" pii:"true"`
	FirstName string   `json:"first_name" pii:"true"`
	LastName  string   `json:"last_name" pii:"true"`
	FullName  string   `json:"full_name" pii:"true"`
	Phone     string   `json:"phone" pii:"true"`
	OrgRef    *string  `json:"org_ref"`
	Roles     []string `json:"roles"`
}

func (h Handler) create(c *gin.Context) {
	var req createReq
	if !bindJSON(c, &req) {
		return
	}
	org, err := optRef(req.OrgRef, "org_ref")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	prof := domain.Profile{Username: req.Username, Email: req.Email, FirstName: req.FirstName, LastName: req.LastName, Phone: req.Phone}
	if req.FullName != "" && prof.FirstName == "" && prof.LastName == "" {
		prof.FirstName, prof.LastName = domain.SplitFullName(req.FullName)
	}
	res, err := h.Svc.Create(c.Request.Context(), app.CreateInput{Profile: prof, OrgRef: org, Roles: req.Roles, IdemKey: key(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/accounts/users/"+res.Account.UserRef.String())
	c.JSON(status, gin.H{
		"user":                  toUserDTO(app.UserView{User: res.User, Roles: res.Account.Roles, Tracked: true}),
		"seq":                   res.Event.Seq,
		"activation_email_sent": res.ActivationSent,
	})
}

func (h Handler) get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, err := h.Svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toUserDTO(v))
}

func (h Handler) events(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	evs, err := h.Svc.Events(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]eventDTO, 0, len(evs))
	for _, e := range evs {
		out = append(out, toEventDTO(e))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) list(c *gin.Context) {
	in := app.ListInput{}
	if s := c.Query("org_ref"); s != "" {
		org, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("org_ref", "phải là UUID"))
			return
		}
		in.OrgRef = org
	}
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		n, perr := strconv.Atoi(strings.TrimPrefix(string(b), "o:"))
		if err != nil || perr != nil || n < 0 || !strings.HasPrefix(string(b), "o:") {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		in.Offset = n
	}
	if s := c.Query("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			in.Limit = n
		}
	}
	page, err := h.Svc.List(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]userDTO, 0, len(page.Items))
	for _, it := range page.Items {
		items = append(items, toUserDTO(it))
	}
	resp := gin.H{"items": items, "next_cursor": nil}
	if page.Next != nil {
		resp["next_cursor"] = base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(*page.Next)))
	}
	c.JSON(http.StatusOK, resp)
}

type reasonReq struct {
	Reason string `json:"reason"`
}

func writeResult(c *gin.Context, res app.Result) {
	c.JSON(http.StatusOK, gin.H{"account": toAccountDTO(res.Account), "seq": res.Event.Seq, "replayed": res.Replayed})
}

func (h Handler) lock(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req reasonReq
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.Svc.Lock(c.Request.Context(), app.LockInput{UserID: id, Reason: req.Reason, IdemKey: key(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	writeResult(c, res)
}

func (h Handler) unlock(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req reasonReq
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.Svc.Unlock(c.Request.Context(), app.LockInput{UserID: id, Reason: req.Reason, IdemKey: key(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	writeResult(c, res)
}

type resendReq struct {
	Actions []string `json:"actions"`
}

func (h Handler) resend(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req resendReq
	// Thân rỗng được phép: gửi lại các required action đang chờ.
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return
	}
	res, err := h.Svc.Resend(c.Request.Context(), app.ResendInput{UserID: id, Actions: req.Actions, IdemKey: key(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"account": toAccountDTO(res.Account), "activation_email_sent": res.ActivationSent, "replayed": res.Replayed})
}

type assignReq struct {
	OrgRef *string  `json:"org_ref"`
	Roles  []string `json:"roles"`
}

func (h Handler) assign(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req assignReq
	if !bindJSON(c, &req) {
		return
	}
	org, err := optRef(req.OrgRef, "org_ref")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	res, err := h.Svc.Assign(c.Request.Context(), app.AssignInput{UserID: id, OrgRef: org, Roles: req.Roles, IdemKey: key(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	writeResult(c, res)
}

type bulkReq struct {
	Action  string   `json:"action"`
	UserIDs []string `json:"user_ids"`
	Reason  string   `json:"reason"`
}

func (h Handler) bulk(c *gin.Context) {
	var req bulkReq
	if !bindJSON(c, &req) {
		return
	}
	idsIn := make([]uuid.UUID, 0, len(req.UserIDs))
	for _, s := range req.UserIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("user_ids", "mỗi phần tử phải là UUID"))
			return
		}
		idsIn = append(idsIn, id)
	}
	items, err := h.Svc.Bulk(c.Request.Context(), app.BulkInput{Action: app.BulkAction(req.Action), UserIDs: idsIn, Reason: req.Reason, IdemKey: key(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(items))
	okN := 0
	for _, it := range items {
		if it.OK {
			okN++
		}
		out = append(out, gin.H{"user_id": it.UserID.String(), "ok": it.OK, "replayed": it.Replayed, "code": strPtr(it.Code), "detail": strPtr(it.Detail)})
	}
	c.JSON(http.StatusOK, gin.H{"succeeded": okN, "failed": len(items) - okN, "items": out})
}

const xlsxType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func (h Handler) importUsers(c *gin.Context) {
	mt, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	var format string
	switch mt {
	case "text/csv":
		format = app.FormatCSV
	case xlsxType:
		format = app.FormatXLSX
	default:
		httpx.Fail(c, app.ErrImportInvalid.WithDetail("Content-Type phải là text/csv hoặc "+xlsxType))
		return
	}
	res, err := h.Svc.Import(c.Request.Context(), format, io.LimitReader(c.Request.Body, MaxImportBytes+1), key(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows := make([]gin.H, 0, len(res.Rows))
	for _, r := range res.Rows {
		rows = append(rows, gin.H{"row": r.Row, "status": r.Status, "reason": strPtr(r.Reason), "user_id": refPtr(r.UserID), "activation_email_sent": r.ActivationSent})
	}
	c.JSON(http.StatusOK, gin.H{"created": res.Created, "rejected": res.Rejected, "rows": rows})
}
