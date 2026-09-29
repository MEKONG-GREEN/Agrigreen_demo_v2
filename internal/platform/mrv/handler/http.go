// Package handler: HTTP của pf/mrv (contracts/openapi/application.yaml và
// admin.yaml, mục "# --- pf/mrv ---").
package handler

import (
	"encoding/base64"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API vào nhóm /v1 (đã có xác thực + idempotency).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/mrv/catalog", h.approvedCatalog)
	rg.GET("/mrv/evidence-types", h.evidenceTypes)
	rg.POST("/mrv/evidence", h.capture)
	rg.GET("/mrv/evidence", h.list)
	rg.GET("/mrv/evidence/:id", h.get)
	rg.POST("/mrv/evidence/:id/reviews", h.review)
	rg.POST("/mrv/exports", h.requestExport)
	rg.GET("/mrv/exports/:id", h.export)
	rg.GET("/mrv/exports/:id/package", h.packageURL)
	rg.GET("/mrv/exports/:id/verify", h.verify)
	h.registerG3App(rg)
}

// RegisterAdmin gắn route Admin API (MFA bắt buộc): danh mục trường có phê duyệt.
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.POST("/mrv/catalog/seed", h.seed)
	rg.POST("/mrv/catalog/versions", h.propose)
	rg.GET("/mrv/catalog/versions", h.versions)
	rg.GET("/mrv/catalog/versions/:id", h.version)
	rg.POST("/mrv/catalog/versions/:id/submit", h.submit)
	rg.POST("/mrv/catalog/versions/:id/approve", h.approve)
	rg.POST("/mrv/catalog/versions/:id/reject", h.reject)
	h.registerG3Admin(rg)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func bindJSON(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func pathID(c *gin.Context, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}

func cursor(c *gin.Context) (uuid.UUID, int, bool) {
	var after uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		if err != nil || len(b) != 16 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return after, 0, false
		}
		copy(after[:], b)
	}
	limit := 50
	if s := c.Query("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			limit = n
		}
	}
	return after, limit, true
}

func nextCursor(next *uuid.UUID) any {
	if next == nil {
		return nil
	}
	return base64.RawURLEncoding.EncodeToString(next[:])
}

func created(replayed bool) int {
	if replayed {
		return http.StatusOK
	}
	return http.StatusCreated
}

// ---------------------------------------------------------------------------
// Danh mục (Application API: chỉ bản đã duyệt)

func (h Handler) approvedCatalog(c *gin.Context) {
	version := 0
	if s := c.Query("version"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			httpx.Fail(c, problem.Validation.WithField("version", "phải là số nguyên dương"))
			return
		}
		version = n
	}
	v, err := h.Svc.ApprovedCatalog(c.Request.Context(), c.Query("evidence_type"), version)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toCatalogDTO(v))
}

type evidenceTypeDTO struct {
	EvidenceType string  `json:"evidence_type"`
	CatalogID    string  `json:"catalog_id"`
	Version      int     `json:"version"`
	Provisional  bool    `json:"provisional"`
	SourceNote   string  `json:"source_note"`
	FieldCount   int     `json:"field_count"`
	ApprovedAt   *string `json:"approved_at"`
}

func (h Handler) evidenceTypes(c *gin.Context) {
	ts, err := h.Svc.EvidenceTypes(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]evidenceTypeDTO, 0, len(ts))
	for _, t := range ts {
		items = append(items, evidenceTypeDTO{
			EvidenceType: t.EvidenceType, CatalogID: t.CatalogID.String(), Version: t.Version, Provisional: t.Provisional,
			SourceNote: t.SourceNote, FieldCount: t.FieldCount, ApprovedAt: tsPtr(t.DecidedAt),
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ---------------------------------------------------------------------------
// Minh chứng

func (h Handler) capture(c *gin.Context) {
	var w app.CapturePayload
	if !bindJSON(c, &w) {
		return
	}
	in, errs := w.Parse()
	if w.EventTime == "" {
		errs = append(errs, problem.FieldError{Field: "event_time", Reason: "bắt buộc"})
	}
	if len(errs) > 0 {
		p := problem.Validation
		for _, e := range errs {
			p = p.WithField(e.Field, e.Reason)
		}
		httpx.Fail(c, p)
		return
	}
	in.IdemKey = idem(c)
	e, replayed, err := h.Svc.CaptureHTTP(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/mrv/evidence/"+e.ID.String())
	c.JSON(created(replayed), toEvidenceDTO(e))
}

func (h Handler) list(c *gin.Context) {
	after, limit, ok := cursor(c)
	if !ok {
		return
	}
	pg, err := h.Svc.List(c.Request.Context(), app.ListInput{
		EvidenceType: c.Query("evidence_type"), AdminUnitCode: c.Query("admin_unit_code"),
		ReviewStatus: c.Query("review_status"), After: after, Limit: limit,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]evidenceDTO, 0, len(pg.Items))
	for _, it := range pg.Items {
		items = append(items, toListedDTO(it))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(pg.Next)})
}

func (h Handler) get(c *gin.Context) {
	id, ok := pathID(c, app.ErrEvidenceNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toViewDTO(v))
}

type reviewReq struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func (h Handler) review(c *gin.Context) {
	id, ok := pathID(c, app.ErrEvidenceNotFound)
	if !ok {
		return
	}
	var req reviewReq
	if !bindJSON(c, &req) {
		return
	}
	r, replayed, err := h.Svc.Review(c.Request.Context(), app.ReviewInput{
		EvidenceID: id, Decision: domain.ReviewStatus(req.Decision), Reason: req.Reason, IdemKey: idem(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(created(replayed), toReviewDTO(r))
}

// ---------------------------------------------------------------------------
// Xuất gói (mục 12: 202 + job)

type exportReq struct {
	Filters domain.ExportFilters `json:"filters"`
}

func (h Handler) requestExport(c *gin.Context) {
	var req exportReq
	if !bindJSON(c, &req) {
		return
	}
	req.Filters.AllUnits = false // do máy chủ giải từ phạm vi, không nhận từ client
	j, _, err := h.Svc.RequestExport(c.Request.Context(), req.Filters, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/mrv/exports/"+j.ID.String())
	c.JSON(http.StatusAccepted, toExportDTO(j))
}

func (h Handler) export(c *gin.Context) {
	id, ok := pathID(c, app.ErrExportNotFound)
	if !ok {
		return
	}
	j, err := h.Svc.Export(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toExportDTO(j))
}

func (h Handler) packageURL(c *gin.Context) {
	id, ok := pathID(c, app.ErrExportNotFound)
	if !ok {
		return
	}
	l, err := h.Svc.PackageURL(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"export_id": l.Job.ID.String(), "url": l.URL, "expires_at": ts(l.ExpiresAt),
		"package_sha256": l.Job.PackageSHA256, "snapshot_sha256": l.Job.SnapshotSHA256,
	})
}

func (h Handler) verify(c *gin.Context) {
	id, ok := pathID(c, app.ErrExportNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.VerifyExport(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"export_id": v.Job.ID.String(), "as_of": ts(v.Job.RequestedAt), "snapshot_sha256": v.Stored,
		"rebuilt_sha256": v.Rebuilt, "match": v.Match,
	})
}

// ---------------------------------------------------------------------------
// Admin: danh mục trường có phê duyệt (R-56)

func (h Handler) seed(c *gin.Context) {
	res, err := h.Svc.SeedG1(c.Request.Context(), idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(res))
	for _, r := range res {
		item := gin.H{"evidence_type": r.EvidenceType, "created": r.Created, "version": nil}
		if r.Version.ID != uuid.Nil {
			item["version"] = toCatalogDTO(r.Version)
		}
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) propose(c *gin.Context) {
	var in app.ProposeInput
	if !bindJSON(c, &in) {
		return
	}
	in.IdemKey = idem(c)
	v, replayed, err := h.Svc.Propose(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/mrv/catalog/versions/"+v.ID.String())
	c.JSON(created(replayed), toCatalogDTO(v))
}

func (h Handler) versions(c *gin.Context) {
	after, limit, ok := cursor(c)
	if !ok {
		return
	}
	pg, err := h.Svc.Versions(c.Request.Context(), c.Query("evidence_type"), c.Query("status"), after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]catalogDTO, 0, len(pg.Items))
	for _, v := range pg.Items {
		items = append(items, toCatalogDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(pg.Next)})
}

func (h Handler) version(c *gin.Context) {
	id, ok := pathID(c, app.ErrCatalogNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.Version(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toCatalogDTO(v))
}

func (h Handler) submit(c *gin.Context) {
	id, ok := pathID(c, app.ErrCatalogNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.Submit(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toCatalogDTO(v))
}

type decisionReq struct {
	Note *string `json:"note"`
}

func (h Handler) decide(c *gin.Context, approve bool) {
	id, ok := pathID(c, app.ErrCatalogNotFound)
	if !ok {
		return
	}
	var req decisionReq
	if c.Request.ContentLength != 0 && !bindJSON(c, &req) {
		return
	}
	v, err := h.Svc.Decide(c.Request.Context(), id, approve, req.Note)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toCatalogDTO(v))
}

func (h Handler) approve(c *gin.Context) { h.decide(c, true) }

func (h Handler) reject(c *gin.Context) { h.decide(c, false) }
