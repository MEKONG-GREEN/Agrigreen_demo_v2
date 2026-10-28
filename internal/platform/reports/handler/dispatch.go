package handler

// HTTP "Báo cáo Chi cục" KN v7.7 (FN-TI-02.06) — contracts/openapi/application.yaml mục "pf/reports: báo cáo Chi cục",
// admin.yaml "dispatch-settings". Route Application cần header X-AG-Context (ngữ cảnh phiên P1).

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func (h Handler) registerDispatchApp(g *gin.RouterGroup) {
	g.GET("/dispatch-recipients", h.dispatchRecipients)
	g.POST("/dispatch-recipients", h.addDispatchRecipient)
	g.POST("/dispatch-recipients/:id/deactivate", h.deactivateDispatchRecipient)
	g.POST("/dispatches", h.createDispatch)
	g.GET("/dispatches", h.listDispatches)
	g.GET("/dispatches/:id", h.getDispatch)
	g.POST("/dispatches/:id/send", h.sendDispatch)
	g.GET("/dispatches/:id/files/:file/download", h.dispatchDownload)
}

func (h Handler) registerDispatchAdmin(g *gin.RouterGroup) {
	g.GET("/dispatch-settings", h.dispatchSettings)
	// A4: Admin thu hồi liên kết tải Chi cục, xem sổ truy cập liên kết.
	g.POST("/dispatches/:id/revoke-links", h.revokeDispatchLinks)
	g.GET("/dispatches/:id/link-access", h.dispatchLinkAccess)
}

// RegisterPublic gắn route CÔNG KHAI (không đăng nhập — mã thông báo trong đường dẫn là quyền truy cập): liên kết tải
// tệp Chi cục do api-platform phục vụ (quyết định A4, 03/10/2026).
func (h Handler) RegisterPublic(rg *gin.RouterGroup) {
	rg.GET("/report-links/:token", h.openDispatchLink)
}

func (h Handler) openDispatchLink(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	f, err := h.Svc.OpenDispatchLink(c.Request.Context(), c.Param("token"), c.ClientIP())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	defer func() { _ = f.Body.Close() }()
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, f.Size, f.ContentType, f.Body,
		map[string]string{"Content-Disposition": "attachment; filename*=UTF-8''" + url.PathEscape(f.Name)})
}

type revokeLinksReq struct {
	Reason string `json:"reason"`
}

func (h Handler) revokeDispatchLinks(c *gin.Context) {
	id, ok := pathID(c, app.ErrDispatchNotFound)
	if !ok {
		return
	}
	var req revokeLinksReq
	if !bind(c, &req) {
		return
	}
	n, err := h.Svc.RevokeDispatchLinks(c.Request.Context(), id, req.Reason, c.GetHeader("Idempotency-Key"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"dispatch_id": id, "revoked": n})
}

type linkAccessDTO struct {
	ID           uuid.UUID `json:"id"`
	LinkID       uuid.UUID `json:"link_id"`
	FileID       uuid.UUID `json:"file_id"`
	RecipientRef uuid.UUID `json:"recipient_ref"`
	Outcome      string    `json:"outcome"`
	IPHash       *string   `json:"ip_hash"`
	At           string    `json:"at"`
	Seq          int64     `json:"seq"`
}

func (h Handler) dispatchLinkAccess(c *gin.Context) {
	id, ok := pathID(c, app.ErrDispatchNotFound)
	if !ok {
		return
	}
	rows, err := h.Svc.DispatchLinkLog(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]linkAccessDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, linkAccessDTO{ID: r.ID, LinkID: r.LinkID, FileID: r.FileID, RecipientRef: r.RecipientRef, Outcome: r.Outcome,
			IPHash: r.IPHash, At: ts(r.EventTime), Seq: r.Seq})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type dispatchRecipientDTO struct {
	ID         uuid.UUID `json:"id"`
	Label      string    `json:"label"`
	Address    *string   `json:"address"`
	Masked     bool      `json:"masked"`
	Kind       string    `json:"kind"`
	IsDefault  bool      `json:"is_default"`
	Active     bool      `json:"active"`
	LastUsedAt *string   `json:"last_used_at"`
}

func toRecipient(r store.DispatchRecipient, masked bool) dispatchRecipientDTO {
	return dispatchRecipientDTO{ID: r.ID, Label: r.Label, Address: r.Address, Masked: masked, Kind: r.Kind, IsDefault: r.IsDefault, Active: r.Active,
		LastUsedAt: tsp(r.LastUsedAt)}
}

func (h Handler) dispatchRecipients(c *gin.Context) {
	rs, masked, err := h.Svc.ListDispatchRecipients(c.Request.Context(), c.Query("province"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]dispatchRecipientDTO, 0, len(rs))
	for _, r := range rs {
		items = append(items, toRecipient(r, masked))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type recipientReq struct {
	Label     string `json:"label"`
	Address   string `json:"address"`
	Kind      string `json:"kind"`
	IsDefault *bool  `json:"is_default"`
}

func (h Handler) addDispatchRecipient(c *gin.Context) {
	var req recipientReq
	if !bind(c, &req) {
		return
	}
	def := true
	if req.IsDefault != nil {
		def = *req.IsDefault
	}
	r, err := h.Svc.AddDispatchRecipient(c.Request.Context(), app.RecipientInput{Label: req.Label, Address: req.Address, Kind: req.Kind, IsDefault: def}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toRecipient(r, false))
}

type reasonBody struct {
	Reason string `json:"reason"`
}

func (h Handler) deactivateDispatchRecipient(c *gin.Context) {
	id, ok := pathID(c, app.ErrRecipientNotFound)
	if !ok {
		return
	}
	var req reasonBody
	if !bind(c, &req) {
		return
	}
	r, err := h.Svc.DeactivateDispatchRecipient(c.Request.Context(), id, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toRecipient(r, false))
}

func (h Handler) dispatchDTO(v app.DispatchView) gin.H {
	d := v.Dispatch
	files := make([]gin.H, 0, len(v.Files))
	for _, f := range v.Files {
		files = append(files, gin.H{"id": f.ID, "format": f.Format, "form_code": f.FormCode, "file_name": f.FileName, "sha256": f.SHA256,
			"size_bytes": f.SizeBytes, "created_at": ts(f.CreatedAt)})
	}
	logs := make([]gin.H, 0, len(v.Events))
	for _, e := range v.Events {
		logs = append(logs, gin.H{"event": "dispatch." + e.Action, "actor_ref": e.ActorRef, "at": ts(e.EventTime), "on_behalf_of": e.OnBehalfOf,
			"detail": json.RawMessage(e.Payload)})
	}
	return gin.H{"dispatch": h.dispatchRow(d), "files": files, "log": logs}
}

func (h Handler) dispatchRow(d store.Dispatch) gin.H {
	// Review #128: "Gửi lại" khi "Đang gửi" quá hạn nhận việc — máy chủ tính, web không tự đoán hạn.
	rs := h.Svc.DispatchResendState(d)
	missing := d.Missing
	if missing == nil {
		missing = []string{}
	}
	return gin.H{"id": d.ID, "department_code": d.Department, "province_code": d.Province, "period_id": d.PeriodID, "period_label": d.PeriodLabel,
		"run_ids": d.RunIDs, "forms": d.Forms, "formats": d.Formats, "provisional": d.Provisional, "missing_communes": missing, "status": d.Status,
		"recipient_ids": nonNilIDs(d.Recipients), "cc_ids": nonNilIDs(d.CC), "subject": d.Subject, "body": d.Body, "link_only": d.LinkOnly,
		"attempts": d.Attempts, "last_error": d.LastError, "sent_at": tsp(d.SentAt), "created_by": d.CreatedBy, "created_at": ts(d.CreatedAt),
		"updated_at": ts(d.UpdatedAt), "can_resend": rs.CanSend, "resumable_at": tsp(rs.ResumableAt)}
}

func nonNilIDs(v []uuid.UUID) []uuid.UUID {
	if v == nil {
		return []uuid.UUID{}
	}
	return v
}

type dispatchReq struct {
	PeriodID  string   `json:"period_id"`
	FormCodes []string `json:"form_codes"`
	Formats   []string `json:"formats"`
}

func (h Handler) createDispatch(c *gin.Context) {
	var req dispatchReq
	if !bind(c, &req) {
		return
	}
	pid, err := uuid.Parse(req.PeriodID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("period_id", "bắt buộc, UUID"))
		return
	}
	v, err := h.Svc.CreateDispatch(c.Request.Context(), app.DispatchInput{PeriodID: pid, FormCodes: req.FormCodes, Formats: req.Formats}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, h.dispatchDTO(v))
}

func (h Handler) listDispatches(c *gin.Context) {
	var pid, before *uuid.UUID
	if v := c.Query("period_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("period_id", "phải là UUID"))
			return
		}
		pid = &id
	}
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		if err != nil || len(b) != 16 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		var id uuid.UUID
		copy(id[:], b)
		before = &id
	}
	lim, _ := strconv.Atoi(c.Query("limit"))
	if lim <= 0 || lim > 200 {
		lim = 50
	}
	ds, err := h.Svc.ListDispatches(c.Request.Context(), c.Query("province"), pid, before, lim+1)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var next any
	if len(ds) > lim {
		ds = ds[:lim]
		last := ds[len(ds)-1].ID
		next = base64.RawURLEncoding.EncodeToString(last[:])
	}
	items := make([]gin.H, 0, len(ds))
	for _, d := range ds {
		items = append(items, h.dispatchRow(d))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func (h Handler) getDispatch(c *gin.Context) {
	id, ok := pathID(c, app.ErrDispatchNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetDispatch(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, h.dispatchDTO(v))
}

type sendReq struct {
	RecipientIDs []string `json:"recipient_ids"`
	CCIDs        []string `json:"cc_ids"`
	Subject      string   `json:"subject"`
	Body         string   `json:"body"`
}

func parseIDs(ss []string, field string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(ss))
	for _, s := range ss {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, problem.Validation.WithField(field, "phải là UUID")
		}
		out = append(out, id)
	}
	return out, nil
}

func (h Handler) sendDispatch(c *gin.Context) {
	id, ok := pathID(c, app.ErrDispatchNotFound)
	if !ok {
		return
	}
	var req sendReq
	if !bind(c, &req) {
		return
	}
	to, err := parseIDs(req.RecipientIDs, "recipient_ids")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	cc, err := parseIDs(req.CCIDs, "cc_ids")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	v, err := h.Svc.SendDispatch(c.Request.Context(), id, app.SendInput{RecipientIDs: to, CCIDs: cc, Subject: req.Subject, Body: req.Body}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, h.dispatchDTO(v))
}

func (h Handler) dispatchDownload(c *gin.Context) {
	id, ok := pathID(c, app.ErrDispatchNotFound)
	if !ok {
		return
	}
	fid, err := uuid.Parse(c.Param("file"))
	if err != nil {
		httpx.Fail(c, app.ErrDispatchNotFound)
		return
	}
	u, err := h.Svc.DispatchFileURL(c.Request.Context(), id, fid)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": u, "expires_in_seconds": int(app.DownloadTTL.Seconds())})
}

func (h Handler) dispatchSettings(c *gin.Context) {
	if _, err := authz.Require(c.Request.Context(), authz.RolePlatformAdmin, "reports_admin"); err != nil {
		httpx.Fail(c, err)
		return
	}
	g := h.Svc.GSheet
	c.JSON(http.StatusOK, gin.H{"gsheet_enabled": g.Enabled, "gsheet_adr": g.ADR, "gsheet_share_mode": g.ShareMode,
		"gsheet_connected": h.Svc.GSheetPub != nil, "smtp_configured": h.Svc.Mailer != nil})
}
