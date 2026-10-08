package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API vào nhóm /v1 (OIDC + Idempotency-Key cho POST). Thửa, xung đột, ranh giới
// HTX, /v1/kn/plots và /v1/partner/plots do pf/cooperative phục vụ (W3.3).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/geo/admin-boundaries/:code", h.adminBoundary)
	h.registerG1(rg)
	h.registerG2App(rg)
}

// MaxImportBody là giới hạn thân request của đường nhập tệp (kho/Hub).
const MaxImportBody = 10 << 20

// RegisterAdmin gắn route Admin API (MFA bắt buộc): nhập tệp.
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.POST("/geo/admin-boundaries/import", h.importAdmin)
	h.registerG2Admin(rg)
}

func bindJSON(c *gin.Context, v any) bool {
	dec := json.NewDecoder(c.Request.Body)
	if err := dec.Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

// fieldCollector gom lỗi theo trường của một request.
type fieldCollector struct {
	p   *problem.Problem
	bad bool
}

func newCollector() *fieldCollector { return &fieldCollector{p: problem.Validation} }

func (f *fieldCollector) add(field, reason string) { f.p, f.bad = f.p.WithField(field, reason), true }

func (f *fieldCollector) ref(field string, s *string) uuid.UUID {
	if s == nil || strings.TrimSpace(*s) == "" {
		return uuid.Nil
	}
	u, err := ids.ParseRef(*s)
	if err != nil {
		f.add(field, "phải là UUID")
	}
	return u
}

func (f *fieldCollector) v7(field string, s *string) uuid.UUID {
	if s == nil || *s == "" {
		return uuid.Nil
	}
	u, err := ids.Parse(*s)
	if err != nil {
		f.add(field, "phải là UUIDv7")
	}
	return u
}

func (f *fieldCollector) at(field string, s *string) time.Time {
	if s == nil || *s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		f.add(field, "phải theo RFC 3339 có múi giờ")
	}
	return t
}

func intQuery(c *gin.Context, name string, def int) (int, error) {
	s := c.Query(name)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, problem.Validation.WithField(name, "phải là số nguyên")
	}
	return n, nil
}

func (h Handler) adminBoundary(c *gin.Context) {
	zoom, err := intQuery(c, "zoom", 12)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	b, err := h.Svc.GetAdminBoundary(c.Request.Context(), c.Param("code"), zoom)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"type": "Feature", "id": b.UnitCode, "geometry": json.RawMessage(b.GeoJSON),
		"properties": gin.H{"code": b.UnitCode, "level": b.Level, "name": b.Name, "source": b.Source,
			"area_ha": b.AreaHa.StringFixed(4), "imported_at": ts(b.ImportedAt)},
	})
}

// ---------------------------------------------------------------------------
// Admin API
// ---------------------------------------------------------------------------

func (h Handler) importAdmin(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	dry := c.Query("dry_run") == "true"
	res, err := h.Svc.ImportAdminBoundaries(c.Request.Context(), app.AdminImportInput{Data: raw, Source: c.Query("source"), DryRun: dry})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	feats := make([]gin.H, 0, len(res.Features))
	for _, r := range res.Features {
		feats = append(feats, gin.H{
			"index": r.Index, "code": strOrNil(r.UnitCode), "name": strOrNil(r.Name), "level": strOrNil(r.Level),
			"status": r.Status, "replaced": r.Replaced, "error_code": strOrNil(r.Code), "reason": strOrNil(r.Reason),
			"area_ha": decOrNil(r.AreaHa),
		})
	}
	c.JSON(http.StatusOK, gin.H{"dry_run": res.DryRun, "accepted": res.Accepted, "rejected": res.Rejected, "features": feats})
}
