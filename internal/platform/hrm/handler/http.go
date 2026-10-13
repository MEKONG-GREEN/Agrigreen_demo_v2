// Package handler: HTTP của pf/hrm (`dom/hrm`, ADR-0050) — Application API /v1/hrm/*. Hợp đồng ở
// contracts/openapi/application.yaml, mục "# --- pf/hrm ---". W11.21: danh mục nhân sự (FN-19) — mục danh mục, bảng hệ
// số có hiệu lực, tham số nâng bậc/quy đổi thành tích có phiên bản, cài dữ liệu mặc định.
package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/hrm/status", h.status)
	rg.POST("/hrm/defaults", h.defaults)
	rg.GET("/hrm/catalogs/:kind", h.list)
	rg.POST("/hrm/catalogs/:kind", h.create)
	rg.GET("/hrm/catalogs/:kind/:code/history", h.history)
	rg.POST("/hrm/catalogs/:kind/:code/revise", h.revise)
	rg.GET("/hrm/grade-scales", h.scales)
	rg.GET("/hrm/grade-scales/:grade/history", h.scaleHistory)
	rg.POST("/hrm/grade-scales/:grade", h.addScale)
	rg.GET("/hrm/rules/:key", h.rule)
	rg.POST("/hrm/rules/:key", h.addRule)
	h.registerRecords(rg) // W11.22: hồ sơ nhân sự (records.go)
	h.registerReports(rg) // W11.28: báo cáo Sở NS01–06, dashboard nhân sự (reports.go)
}

// decode đọc thân JSON, chặn trường lạ.
func decode(c *gin.Context, v any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("thân yêu cầu: "+err.Error()))
		return false
	}
	return true
}

// at đọc tham số ngày ?at=YYYY-MM-DD (vắng → zero = hôm nay).
func at(c *gin.Context) (time.Time, bool) {
	s := c.Query("at")
	if s == "" {
		return time.Time{}, true
	}
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("at phải là ngày YYYY-MM-DD"))
		return time.Time{}, false
	}
	return d, true
}

func dateOf(s, field string) (time.Time, error) {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, problem.Validation.WithField(field, "ngày YYYY-MM-DD")
	}
	return d, nil
}

type itemDTO struct {
	Kind        string          `json:"kind"`
	Code        string          `json:"code"`
	Name        string          `json:"name"`
	SortOrder   int             `json:"sort_order"`
	Active      bool            `json:"active"`
	Placeholder bool            `json:"placeholder"`
	Version     int64           `json:"version"`
	Attrs       json.RawMessage `json:"attrs"`
	RecordedAt  time.Time       `json:"recorded_at"`
}

func itemOut(it port.Item) itemDTO {
	attrs := it.Attrs
	if len(attrs) == 0 {
		attrs = json.RawMessage(`{}`)
	}
	return itemDTO{Kind: it.Kind, Code: it.Code, Name: it.Name, SortOrder: it.SortOrder, Active: it.Active, Placeholder: it.Placeholder,
		Version: it.Version, Attrs: attrs, RecordedAt: it.RecordedAt.UTC()}
}

func itemsOut(xs []port.Item) []itemDTO {
	out := make([]itemDTO, 0, len(xs))
	for _, x := range xs {
		out = append(out, itemOut(x))
	}
	return out
}

func (h Handler) status(c *gin.Context) {
	st, err := h.Svc.Status(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"can_manage": st.CanManage, "rule_version": st.RuleVersion, "day": st.Day.Format(time.DateOnly),
		"counts": st.Counts, "placeholders": st.Placeholders, "scales": st.Scales, "rules": st.Rules})
}

func (h Handler) defaults(c *gin.Context) {
	r, err := h.Svc.InstallDefaults(c.Request.Context(), idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"items": r.Items, "scales": r.Scales, "rules": r.Rules})
}

func (h Handler) list(c *gin.Context) {
	xs, err := h.Svc.List(c.Request.Context(), c.Param("kind"), c.Query("include_retired") == "true")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"kind": c.Param("kind"), "items": itemsOut(xs)})
}

func (h Handler) history(c *gin.Context) {
	xs, err := h.Svc.History(c.Request.Context(), c.Param("kind"), c.Param("code"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"versions": itemsOut(xs)})
}

type itemIn struct {
	Code      string       `json:"code"`
	Name      string       `json:"name"`
	SortOrder int          `json:"sort_order"`
	Active    *bool        `json:"active"`
	Attrs     domain.Attrs `json:"attrs"`
}

func (in itemIn) app() app.ItemInput {
	active := in.Active == nil || *in.Active
	return app.ItemInput{Code: in.Code, Name: in.Name, SortOrder: in.SortOrder, Active: active, Attrs: in.Attrs}
}

func written(c *gin.Context, r app.WriteResult, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": r.Code, "version": r.Version})
}

func (h Handler) create(c *gin.Context) {
	var in itemIn
	if !decode(c, &in) {
		return
	}
	r, err := h.Svc.Create(c.Request.Context(), c.Param("kind"), in.app(), idempotency.KeyFromContext(c.Request.Context()))
	written(c, r, err)
}

func (h Handler) revise(c *gin.Context) {
	var in itemIn
	if !decode(c, &in) {
		return
	}
	if in.Code != "" {
		httpx.Fail(c, problem.Validation.WithField("code", "không đổi được mã — mã ở đường dẫn"))
		return
	}
	r, err := h.Svc.Update(c.Request.Context(), c.Param("kind"), c.Param("code"), in.app(), idempotency.KeyFromContext(c.Request.Context()))
	written(c, r, err)
}

type scaleDTO struct {
	GradeCode    string    `json:"grade_code"`
	ValidFrom    string    `json:"valid_from"`
	Steps        int       `json:"steps"`
	Coefficients []string  `json:"coefficients"` // chuỗi thập phân 2 chữ số (R-CODE-01)
	HoldMonths   int       `json:"hold_months"`
	Note         string    `json:"note"`
	Placeholder  bool      `json:"placeholder"`
	Version      int64     `json:"version"`
	RecordedAt   time.Time `json:"recorded_at"`
}

func scaleOut(r store.ScaleRow) scaleDTO {
	cs := make([]string, len(r.Coefficients))
	for i, x := range r.Coefficients {
		cs[i] = x.StringFixed(2)
	}
	return scaleDTO{GradeCode: r.GradeCode, ValidFrom: r.ValidFrom.Format(time.DateOnly), Steps: len(cs), Coefficients: cs,
		HoldMonths: r.HoldMonths, Note: r.Note, Placeholder: r.Placeholder, Version: r.Version, RecordedAt: r.RecordedAt.UTC()}
}

func (h Handler) scales(c *gin.Context) {
	d, ok := at(c)
	if !ok {
		return
	}
	day, xs, err := h.Svc.Scales(c.Request.Context(), d)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	type row struct {
		Grade itemDTO   `json:"grade"`
		Scale *scaleDTO `json:"scale"`
	}
	out := make([]row, 0, len(xs))
	for _, x := range xs {
		r := row{Grade: itemOut(x.Grade)}
		if x.Scale != nil {
			s := scaleOut(*x.Scale)
			r.Scale = &s
		}
		out = append(out, r)
	}
	c.JSON(http.StatusOK, gin.H{"day": day.Format(time.DateOnly), "grades": out})
}

func (h Handler) scaleHistory(c *gin.Context) {
	xs, err := h.Svc.ScaleHistory(c.Request.Context(), c.Param("grade"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]scaleDTO, 0, len(xs))
	for _, x := range xs {
		out = append(out, scaleOut(x))
	}
	c.JSON(http.StatusOK, gin.H{"versions": out})
}

type scaleIn struct {
	ValidFrom    string            `json:"valid_from"`
	Coefficients []decimal.Decimal `json:"coefficients"`
	HoldMonths   int               `json:"hold_months"`
	Note         string            `json:"note"`
}

func (h Handler) addScale(c *gin.Context) {
	var in scaleIn
	if !decode(c, &in) {
		return
	}
	d, err := dateOf(in.ValidFrom, "valid_from")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	r, err := h.Svc.AddScale(c.Request.Context(), domain.GradeScale{GradeCode: c.Param("grade"), ValidFrom: d, Coefficients: in.Coefficients,
		HoldMonths: in.HoldMonths, Note: in.Note}, idempotency.KeyFromContext(c.Request.Context()))
	written(c, r, err)
}

type ruleDTO struct {
	Key         string          `json:"key"`
	ValidFrom   string          `json:"valid_from"`
	Payload     json.RawMessage `json:"payload"`
	Note        string          `json:"note"`
	Placeholder bool            `json:"placeholder"`
	Version     int64           `json:"version"`
	RecordedAt  time.Time       `json:"recorded_at"`
}

func ruleOut(r store.RuleRow) ruleDTO {
	return ruleDTO{Key: r.Key, ValidFrom: r.ValidFrom.Format(time.DateOnly), Payload: r.Payload, Note: r.Note, Placeholder: r.Placeholder,
		Version: r.Version, RecordedAt: r.RecordedAt.UTC()}
}

func (h Handler) rule(c *gin.Context) {
	d, ok := at(c)
	if !ok {
		return
	}
	v, err := h.Svc.Rule(c.Request.Context(), c.Param("key"), d)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var cur *ruleDTO
	if v.Current != nil {
		r := ruleOut(*v.Current)
		cur = &r
	}
	vs := make([]ruleDTO, 0, len(v.Versions))
	for _, x := range v.Versions {
		vs = append(vs, ruleOut(x))
	}
	c.JSON(http.StatusOK, gin.H{"key": v.Key, "day": v.Day.Format(time.DateOnly), "current": cur, "versions": vs})
}

type ruleIn struct {
	ValidFrom string          `json:"valid_from"`
	Payload   json.RawMessage `json:"payload"`
	Note      string          `json:"note"`
}

func (h Handler) addRule(c *gin.Context) {
	var in ruleIn
	if !decode(c, &in) {
		return
	}
	d, err := dateOf(in.ValidFrom, "valid_from")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	r, err := h.Svc.AddRule(c.Request.Context(), domain.RuleVersionInput{Key: domain.RuleKey(c.Param("key")), ValidFrom: d, Payload: in.Payload,
		Note: in.Note}, idempotency.KeyFromContext(c.Request.Context()))
	written(c, r, err)
}
