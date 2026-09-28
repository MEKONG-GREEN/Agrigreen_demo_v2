package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// RegisterApp gắn route chỉ đọc vào nhóm /v1 của Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/masterdata/admin-units", h.children)
	rg.GET("/masterdata/admin-units/:code", h.unit)
	rg.GET("/masterdata/admin-units/:code/tree", h.tree)
	rg.GET("/masterdata/admin-units/:code/current", h.current)
	rg.GET("/masterdata/catalogs/:catalog", h.listItems(false))
	rg.GET("/masterdata/catalogs/:catalog/:code", h.item)
	rg.GET("/masterdata/params/value", h.paramValue)
}

// RegisterAdmin gắn route quản trị vào nhóm /v1 của Admin API (MFA bắt buộc).
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.POST("/masterdata/seed", h.seed)
	rg.POST("/masterdata/admin-units/import", h.importUnits)
	rg.GET("/masterdata/catalogs/:catalog/items", h.listItems(true))
	rg.POST("/masterdata/catalogs/:catalog/items", h.createItem)
	rg.PUT("/masterdata/catalogs/:catalog/items/:code", h.updateItem)
	rg.POST("/masterdata/catalogs/:catalog/items/:code/deactivate", h.setActive(false))
	rg.POST("/masterdata/catalogs/:catalog/items/:code/reactivate", h.setActive(true))
	rg.GET("/masterdata/catalogs/:catalog/items/:code/history", h.history)
	rg.POST("/masterdata/params/versions", h.propose)
	rg.GET("/masterdata/params/versions", h.versions)
	rg.GET("/masterdata/params/versions/:id", h.version)
	rg.POST("/masterdata/params/versions/:id/submit", h.submit)
	rg.POST("/masterdata/params/versions/:id/approve", h.approve)
	rg.POST("/masterdata/params/versions/:id/reject", h.reject)
	h.registerRegistry(rg)
}

func bindJSON(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Đơn vị hành chính
// ---------------------------------------------------------------------------

func (h Handler) children(c *gin.Context) {
	at, err := h.at(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	after, err := decodeCursor(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	pg, err := h.Svc.Children(c.Request.Context(), c.Query("parent"), at, after, limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]unitDTO, 0, len(pg.Items))
	for _, u := range pg.Items {
		items = append(items, toUnitDTO(u))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
}

func (h Handler) unit(c *gin.Context) {
	at, err := h.at(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	u, err := h.Svc.GetUnit(c.Request.Context(), c.Param("code"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toUnitDTO(u))
}

func (h Handler) tree(c *gin.Context) {
	at, err := h.at(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	us, err := h.Svc.Tree(c.Request.Context(), c.Param("code"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, buildTree(us))
}

func (h Handler) current(c *gin.Context) {
	at, err := h.at(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	codes, err := h.Svc.CurrentCodes(c.Request.Context(), c.Param("code"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": c.Param("code"), "at": at.UTC().Format(time.RFC3339), "current_codes": codes})
}

func (h Handler) importUnits(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	res, err := h.Svc.ImportUnits(c.Request.Context(), raw)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h Handler) seed(c *gin.Context) {
	res, err := h.Svc.SeedForCaller(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// ---------------------------------------------------------------------------
// Danh mục
// ---------------------------------------------------------------------------

func (h Handler) listItems(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		after, err := decodeCursor(c)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		incl := c.Query("include_inactive") == "true" || (admin && c.Query("include_inactive") != "false")
		pg, err := h.Svc.ListItems(c.Request.Context(), c.Param("catalog"), incl, after, limit(c))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		items := make([]itemDTO, 0, len(pg.Items))
		for _, it := range pg.Items {
			items = append(items, toItemDTO(it))
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
	}
}

func (h Handler) item(c *gin.Context) {
	it, err := h.Svc.GetItem(c.Request.Context(), c.Param("catalog"), c.Param("code"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toItemDTO(it))
}

type itemReq struct {
	Code  string            `json:"code"`
	Name  string            `json:"name"`
	Attrs map[string]string `json:"attrs"`
}

func (h Handler) createItem(c *gin.Context) {
	var req itemReq
	if !bindJSON(c, &req) {
		return
	}
	in := domain.Item{Catalog: c.Param("catalog"), Code: req.Code, Name: req.Name, Attrs: req.Attrs}
	it, replayed, err := h.Svc.CreateItem(c.Request.Context(), in, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/masterdata/catalogs/"+it.Catalog+"/items/"+it.Code)
	c.JSON(status, toItemDTO(it))
}

func (h Handler) updateItem(c *gin.Context) {
	var req itemReq
	if !bindJSON(c, &req) {
		return
	}
	if req.Code != "" && req.Code != c.Param("code") {
		httpx.Fail(c, problem.Validation.WithField("code", "không đổi được mã; bỏ trống hoặc trùng mã trên đường dẫn"))
		return
	}
	in := domain.Item{Catalog: c.Param("catalog"), Code: c.Param("code"), Name: req.Name, Attrs: req.Attrs}
	it, err := h.Svc.UpdateItem(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toItemDTO(it))
}

type reasonReq struct {
	Reason string `json:"reason"`
}

func (h Handler) setActive(active bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req reasonReq
		if !bindJSON(c, &req) {
			return
		}
		it, _, err := h.Svc.SetActive(c.Request.Context(), c.Param("catalog"), c.Param("code"), active, req.Reason,
			idempotency.KeyFromContext(c.Request.Context()))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, toItemDTO(it))
	}
}

func (h Handler) history(c *gin.Context) {
	var after int64
	if cur, err := decodeCursor(c); err != nil {
		httpx.Fail(c, err)
		return
	} else if cur != "" {
		n, err := strconv.ParseInt(cur, 10, 64)
		if err != nil || n < 0 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		after = n
	}
	pg, err := h.Svc.History(c.Request.Context(), c.Param("catalog"), c.Param("code"), after, limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]changeDTO, 0, len(pg.Items))
	for _, ch := range pg.Items {
		items = append(items, toChangeDTO(ch))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
}

// ---------------------------------------------------------------------------
// Tham số
// ---------------------------------------------------------------------------

func (h Handler) paramValue(c *gin.Context) {
	at, err := h.at(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	a, err := h.Svc.ParamAt(c.Request.Context(), c.Query("key"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toParamValueDTO(a))
}

type proposeReq struct {
	Key              string          `json:"key"`
	Value            json.RawMessage `json:"value"` // chuỗi hoặc số JSON — đọc thẳng thành decimal, không qua float (R-10)
	Text             *string         `json:"text"`
	Unit             string          `json:"unit"`
	EffectiveFrom    string          `json:"effective_from"`
	SourceDoc        string          `json:"source_doc"`
	CorrectionReason *string         `json:"correction_reason"`
}

func parseDecimal(raw json.RawMessage) (*decimal.Decimal, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return nil, true
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if json.Unmarshal(raw, &str) != nil {
			return nil, false
		}
		s = strings.TrimSpace(str)
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return nil, false
	}
	return &d, true
}

func (h Handler) propose(c *gin.Context) {
	var req proposeReq
	if !bindJSON(c, &req) {
		return
	}
	in := app.ProposeInput{Key: req.Key, Text: req.Text, Unit: req.Unit, SourceDoc: req.SourceDoc,
		CorrectionReason: req.CorrectionReason, IdemKey: idempotency.KeyFromContext(c.Request.Context())}
	v := problem.Validation
	bad := false
	if d, ok := parseDecimal(req.Value); !ok {
		v, bad = v.WithField("value", "phải là số thập phân (chuỗi hoặc số JSON)"), true
	} else {
		in.Value = d
	}
	if t, err := domain.ParseInstant(req.EffectiveFrom); err != nil {
		v, bad = v.WithField("effective_from", "phải theo RFC 3339 hoặc YYYY-MM-DD"), true
	} else {
		in.EffectiveFrom = t
	}
	if bad {
		httpx.Fail(c, v)
		return
	}
	out, replayed, err := h.Svc.Propose(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.Header("Location", "/v1/masterdata/params/versions/"+out.ID.String())
	c.JSON(status, toVersionDTO(out))
}

func versionID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrVersionNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h Handler) version(c *gin.Context) {
	id, ok := versionID(c)
	if !ok {
		return
	}
	v, err := h.Svc.Version(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	d := []versionDTO{toVersionDTO(v)}
	h.withNames(c.Request.Context(), d)
	c.JSON(http.StatusOK, d[0])
}

func (h Handler) versions(c *gin.Context) {
	var after uuid.UUID
	if cur, err := decodeCursor(c); err != nil {
		httpx.Fail(c, err)
		return
	} else if cur != "" {
		id, err := uuid.Parse(cur)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		after = id
	}
	pg, err := h.Svc.Versions(c.Request.Context(), c.Query("key"), c.Query("status"), after, limit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]versionDTO, 0, len(pg.Items))
	for _, v := range pg.Items {
		items = append(items, toVersionDTO(v))
	}
	h.withNames(c.Request.Context(), items)
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
}

func (h Handler) submit(c *gin.Context) {
	id, ok := versionID(c)
	if !ok {
		return
	}
	v, err := h.Svc.Submit(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toVersionDTO(v))
}

type decisionReq struct {
	Note *string `json:"note"`
}

func (h Handler) approve(c *gin.Context) {
	id, ok := versionID(c)
	if !ok {
		return
	}
	var req decisionReq
	if c.Request.ContentLength != 0 && !bindJSON(c, &req) {
		return
	}
	v, a, err := h.Svc.Approve(c.Request.Context(), id, req.Note)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"version": toVersionDTO(v), "approved": toParamValueDTO(a)})
}

func (h Handler) reject(c *gin.Context) {
	id, ok := versionID(c)
	if !ok {
		return
	}
	var req decisionReq
	if !bindJSON(c, &req) {
		return
	}
	note := ""
	if req.Note != nil {
		note = *req.Note
	}
	v, err := h.Svc.Reject(c.Request.Context(), id, note)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toVersionDTO(v))
}
