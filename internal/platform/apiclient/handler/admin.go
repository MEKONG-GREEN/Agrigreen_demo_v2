// Package handler: HTTP quản trị của pf/apiclient trên Admin API của api-platform (MFA bắt buộc —
// contracts/openapi/admin.yaml, mục "# --- pf/apiclient ---"). Bề mặt Public cho bên thứ ba ở
// package public (tiến trình cmd/api-public).
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Admin gói app.Service cho Admin API.
type Admin struct{ Svc *app.Service }

// Register gắn route Admin API.
func (h Admin) Register(rg *gin.RouterGroup) {
	rg.POST("/api-clients", h.proposeClient)
	rg.GET("/api-clients", h.listClients)
	rg.GET("/api-clients/:id", h.getClient)
	rg.POST("/api-clients/:id/approve", h.changeClient("approve"))
	rg.POST("/api-clients/:id/suspend", h.changeClient("suspend"))
	rg.POST("/api-clients/:id/reactivate", h.changeClient("reactivate"))
	rg.POST("/api-clients/:id/revoke", h.changeClient("revoke"))
	rg.GET("/api-clients/:id/keys", h.listKeys)
	rg.POST("/api-clients/:id/keys", h.issueKey)
	rg.POST("/api-clients/:id/keys/:key_id/rotate", h.rotateKey)
	rg.POST("/api-clients/:id/keys/:key_id/revoke", h.revokeKey)
	rg.GET("/api-clients/:id/subscriptions", h.listSubs)
	rg.POST("/api-clients/:id/subscriptions", h.proposeSub)
	rg.POST("/api-clients/:id/subscriptions/:sub_id/approve", h.changeSub("approve"))
	rg.POST("/api-clients/:id/subscriptions/:sub_id/revoke", h.changeSub("revoke"))
	rg.GET("/api-clients/:id/plans", h.listPlans)
	rg.POST("/api-clients/:id/plans", h.proposePlan)
	rg.POST("/api-clients/:id/plans/:version/approve", h.approvePlan)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func tsp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

func refp(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

type clientDTO struct {
	ID             string   `json:"id"`
	ClientTenantID string   `json:"client_tenant_id"`
	DisplayName    string   `json:"display_name"`
	Purpose        string   `json:"purpose"`
	ContactRef     string   `json:"contact_ref"`
	AuthMethods    []string `json:"auth_methods"`
	OAuthClientID  *string  `json:"oauth_client_id"`
	Status         string   `json:"status"`
	StatusReason   *string  `json:"status_reason"`
	ProposedBy     string   `json:"proposed_by"`
	ProposedAt     string   `json:"proposed_at"`
	ApprovedBy     *string  `json:"approved_by"`
	ApprovedAt     *string  `json:"approved_at"`
}

func toClient(c store.Client) clientDTO {
	return clientDTO{ID: c.ClientID.String(), ClientTenantID: c.ClientTenantID.String(), DisplayName: c.DisplayName, Purpose: c.Purpose,
		ContactRef: c.ContactRef.String(), AuthMethods: c.AuthMethods, OAuthClientID: c.OAuthClientID, Status: c.Status, StatusReason: c.StatusReason,
		ProposedBy: c.ProposedBy.String(), ProposedAt: ts(c.ProposedAt), ApprovedBy: refp(c.ApprovedBy), ApprovedAt: tsp(c.ApprovedAt)}
}

// keyDTO: KHÔNG có bí mật, KHÔNG có HMAC — chỉ prefix (an toàn để hiển thị và ghi log).
type keyDTO struct {
	ID           string  `json:"id"`
	Prefix       string  `json:"key_prefix"`
	Status       string  `json:"status"`
	IssuedAt     string  `json:"issued_at"`
	ExpiresAt    string  `json:"expires_at"`
	GraceUntil   *string `json:"grace_until"`
	RevokedAt    *string `json:"revoked_at"`
	RevokeReason *string `json:"revoke_reason"`
}

func toKey(k store.Key) keyDTO {
	return keyDTO{ID: k.KeyID.String(), Prefix: k.KeyPrefix, Status: k.Status, IssuedAt: ts(k.IssuedAt), ExpiresAt: ts(k.ExpiresAt),
		GraceUntil: tsp(k.GraceUntil), RevokedAt: tsp(k.RevokedAt), RevokeReason: k.RevokeReason}
}

type subDTO struct {
	ID           string   `json:"id"`
	Product      string   `json:"product"`
	Scopes       []string `json:"scopes"`
	Status       string   `json:"status"`
	ProposedBy   string   `json:"proposed_by"`
	ProposedAt   string   `json:"proposed_at"`
	ApprovedBy   *string  `json:"approved_by"`
	ApprovedAt   *string  `json:"approved_at"`
	RevokedAt    *string  `json:"revoked_at"`
	RevokeReason *string  `json:"revoke_reason"`
}

func toSub(s store.Subscription) subDTO {
	return subDTO{ID: s.SubscriptionID.String(), Product: s.Product, Scopes: s.Scopes, Status: s.Status, ProposedBy: s.ProposedBy.String(),
		ProposedAt: ts(s.ProposedAt), ApprovedBy: refp(s.ApprovedBy), ApprovedAt: tsp(s.ApprovedAt), RevokedAt: tsp(s.RevokedAt), RevokeReason: s.RevokeReason}
}

type planDTO struct {
	Version           int     `json:"version"`
	RequestsPerMinute int64   `json:"requests_per_minute"`
	DailyUnits        int64   `json:"daily_units"`
	MonthlyUnits      int64   `json:"monthly_units"`
	MaxExportRows     int64   `json:"max_export_rows"`
	EffectiveFrom     string  `json:"effective_from"`
	Status            string  `json:"status"`
	ProposedBy        string  `json:"proposed_by"`
	ProposedAt        string  `json:"proposed_at"`
	ApprovedBy        *string `json:"approved_by"`
	ApprovedAt        *string `json:"approved_at"`
}

func toPlan(p store.PlanRow) planDTO {
	return planDTO{Version: p.Version, RequestsPerMinute: p.RequestsPerMinute, DailyUnits: p.DailyUnits, MonthlyUnits: p.MonthlyUnits,
		MaxExportRows: p.MaxExportRows, EffectiveFrom: ts(p.EffectiveFrom), Status: p.Status, ProposedBy: p.ProposedBy.String(),
		ProposedAt: ts(p.ProposedAt), ApprovedBy: refp(p.ApprovedBy), ApprovedAt: tsp(p.ApprovedAt)}
}

func pathID(c *gin.Context, name string, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}

func bind(c *gin.Context, v any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ, không có trường lạ"))
		return false
	}
	return true
}

type reasonReq struct {
	Reason string `json:"reason"`
}

// bindReason đọc {"reason"} nếu có thân (approve/reactivate không cần thân).
func bindReason(c *gin.Context) (string, bool) {
	if c.Request.ContentLength == 0 {
		return "", true
	}
	var r reasonReq
	if !bind(c, &r) {
		return "", false
	}
	return r.Reason, true
}

type proposeClientReq struct {
	ClientTenantID string   `json:"client_tenant_id"`
	DisplayName    string   `json:"display_name"`
	Purpose        string   `json:"purpose"`
	ContactRef     string   `json:"contact_ref"`
	AuthMethods    []string `json:"auth_methods"`
	OAuthClientID  string   `json:"oauth_client_id"`
}

func (h Admin) proposeClient(c *gin.Context) {
	var req proposeClientReq
	if !bind(c, &req) {
		return
	}
	in := app.ProposeClientInput{DisplayName: req.DisplayName, Purpose: req.Purpose, AuthMethods: req.AuthMethods, OAuthClientID: req.OAuthClientID,
		IdemKey: idempotency.KeyFromContext(c.Request.Context())}
	in.ClientTenantID, _ = uuid.Parse(req.ClientTenantID)
	in.ContactRef, _ = uuid.Parse(req.ContactRef)
	out, err := h.Svc.ProposeClient(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/api-clients/"+out.ClientID.String())
	c.JSON(http.StatusCreated, toClient(out))
}

func (h Admin) listClients(c *gin.Context) {
	after := uuid.Nil
	if v := c.Query("cursor"); v != "" {
		u, err := uuid.Parse(v)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "con trỏ không hợp lệ"))
			return
		}
		after = u
	}
	limit := 100
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			httpx.Fail(c, problem.Validation.WithField("limit", "1–500"))
			return
		}
		limit = n
	}
	status := c.Query("status")
	switch port.ClientStatus(status) {
	case "", port.ClientProposed, port.ClientActive, port.ClientSuspended, port.ClientRevoked:
	default:
		httpx.Fail(c, problem.Validation.WithField("status", "proposed | active | suspended | revoked"))
		return
	}
	rows, err := h.Svc.ListClients(c.Request.Context(), status, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]clientDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, toClient(r))
	}
	var next *string
	if len(rows) == limit {
		s := rows[len(rows)-1].ClientID.String()
		next = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func (h Admin) getClient(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrClientNotFound)
	if !ok {
		return
	}
	out, err := h.Svc.GetClient(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toClient(out))
}

func (h Admin) changeClient(op string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c, "id", app.ErrClientNotFound)
		if !ok {
			return
		}
		reason, ok := bindReason(c)
		if !ok {
			return
		}
		out, err := h.Svc.ChangeClient(c.Request.Context(), id, op, reason)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, toClient(out))
	}
}

func (h Admin) listKeys(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrClientNotFound)
	if !ok {
		return
	}
	rows, err := h.Svc.ListKeys(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]keyDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, toKey(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// issuedNoSecret là bản lưu để phát lại của phản hồi phát hành khóa — không có trường bí mật.
type issuedNoSecret struct {
	KeyID      string  `json:"key_id"`
	ClientID   string  `json:"client_id"`
	Prefix     string  `json:"key_prefix"`
	ExpiresAt  string  `json:"expires_at"`
	GraceUntil *string `json:"previous_key_grace_until,omitempty"`
}

// writeIssued trả bí mật MỘT lần (Cache-Control: no-store). Bản LƯU để phát lại Idempotency-Key
// (shared.idempotency_key, 48 giờ) KHÔNG có bí mật (R-14, R-92).
func writeIssued(c *gin.Context, k port.IssuedKey) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	stored := issuedNoSecret{KeyID: k.KeyID.String(), ClientID: k.ClientID.String(), Prefix: k.Prefix, ExpiresAt: ts(k.ExpiresAt), GraceUntil: tsp(k.GraceUntil)}
	if b, err := json.Marshal(stored); err == nil {
		idempotency.SetStoredBody(c, b)
	}
	c.Header("Location", "/v1/api-clients/"+k.ClientID.String()+"/keys")
	c.JSON(http.StatusCreated, k)
}

func (h Admin) issueKey(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrClientNotFound)
	if !ok {
		return
	}
	out, err := h.Svc.IssueKey(c.Request.Context(), id, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	writeIssued(c, out)
}

func (h Admin) rotateKey(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrClientNotFound)
	if !ok {
		return
	}
	kid, ok := pathID(c, "key_id", app.ErrKeyNotFound)
	if !ok {
		return
	}
	out, err := h.Svc.RotateKey(c.Request.Context(), id, kid, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	writeIssued(c, out)
}

func (h Admin) revokeKey(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrClientNotFound)
	if !ok {
		return
	}
	kid, ok := pathID(c, "key_id", app.ErrKeyNotFound)
	if !ok {
		return
	}
	var r reasonReq
	if !bind(c, &r) {
		return
	}
	out, err := h.Svc.RevokeKey(c.Request.Context(), id, kid, r.Reason)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toKey(out))
}

func (h Admin) listSubs(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrClientNotFound)
	if !ok {
		return
	}
	rows, err := h.Svc.ListSubscriptions(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]subDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, toSub(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type proposeSubReq struct {
	Product string   `json:"product"`
	Scopes  []string `json:"scopes"`
}

func (h Admin) proposeSub(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrClientNotFound)
	if !ok {
		return
	}
	var req proposeSubReq
	if !bind(c, &req) {
		return
	}
	out, err := h.Svc.ProposeSubscription(c.Request.Context(), id, req.Product, req.Scopes, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toSub(out))
}

func (h Admin) changeSub(op string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c, "id", app.ErrClientNotFound)
		if !ok {
			return
		}
		sid, ok := pathID(c, "sub_id", app.ErrSubscriptionNotFound)
		if !ok {
			return
		}
		reason, ok := bindReason(c)
		if !ok {
			return
		}
		out, err := h.Svc.ChangeSubscription(c.Request.Context(), id, sid, op, reason)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, toSub(out))
	}
}

func (h Admin) listPlans(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrClientNotFound)
	if !ok {
		return
	}
	rows, err := h.Svc.ListPlans(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]planDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, toPlan(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "defaults": gin.H{"requests_per_minute": port.DefaultRequestsPerMinute,
		"daily_units": port.DefaultDailyUnits, "monthly_units": port.DefaultMonthlyUnits, "max_export_rows": port.DefaultMaxExportRows}})
}

type proposePlanReq struct {
	RequestsPerMinute int64  `json:"requests_per_minute"`
	DailyUnits        int64  `json:"daily_units"`
	MonthlyUnits      int64  `json:"monthly_units"`
	MaxExportRows     int64  `json:"max_export_rows"`
	EffectiveFrom     string `json:"effective_from"`
}

func (h Admin) proposePlan(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrClientNotFound)
	if !ok {
		return
	}
	var req proposePlanReq
	if !bind(c, &req) {
		return
	}
	in := port.Plan{RequestsPerMinute: req.RequestsPerMinute, DailyUnits: req.DailyUnits, MonthlyUnits: req.MonthlyUnits, MaxExportRows: req.MaxExportRows}
	if req.EffectiveFrom != "" {
		t, err := time.Parse(time.RFC3339, req.EffectiveFrom)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("effective_from", "RFC 3339"))
			return
		}
		in.EffectiveFrom = t
	}
	out, err := h.Svc.ProposePlan(c.Request.Context(), id, in, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toPlan(out))
}

func (h Admin) approvePlan(c *gin.Context) {
	id, ok := pathID(c, "id", app.ErrClientNotFound)
	if !ok {
		return
	}
	v, err := strconv.Atoi(c.Param("version"))
	if err != nil || v < 1 {
		httpx.Fail(c, app.ErrPlanNotFound)
		return
	}
	out, err := h.Svc.ApprovePlan(c.Request.Context(), id, v)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toPlan(out))
}
