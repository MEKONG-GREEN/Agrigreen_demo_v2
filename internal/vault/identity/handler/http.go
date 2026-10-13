// Package handler: HTTP của kho định danh (contracts/openapi/identity-vault.yaml).
//
// Chỉ phục vụ trong cmd/identity-vault, bề mặt Internal (mTLS, mục 11). Mục đích
// (R-42) lấy từ header X-Purpose, hoặc trường "purpose" trong thân, hoặc query
// ?purpose= với GET. Không có → 400 vault.purpose_required. Giá trị định danh
// chỉ đi trong thân, không bao giờ trong URL.
package handler

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Handler gói Service cho HTTP.
type Handler struct {
	Svc *app.Service
	// Idem là middleware Idempotency-Key (R-51) — chỉ gắn cho POST thay đổi trạng
	// thái (tạo, xóa). resolve/lookup/unwrap là đọc: KHÔNG lưu phản hồi của chúng
	// vào shared.idempotency_key vì phản hồi chứa định danh hoặc DEK (R-40).
	Idem gin.HandlerFunc
}

// Register gắn route vào nhóm /v1 đã có xác thực.
func (h Handler) Register(rg *gin.RouterGroup) {
	rg.POST("/subjects", h.Idem, h.create)
	rg.POST("/subjects/lookup", h.lookup)
	// W11.23: thẻ danh bạ theo lô (pf/contact) — POST vì thân mang danh sách ref; phản hồi no-store, không qua Idem.
	rg.POST("/subjects/cards", h.cards)
	rg.GET("/subjects/:ref/display", h.display)
	rg.POST("/subjects/:ref/resolve", h.resolve)
	rg.POST("/subjects/:ref/unwrap", h.unwrap)
	rg.POST("/subjects/:ref/erasure", h.Idem, h.erase)
	// W11.22 (vault 000004): hồ sơ nhân sự — sửa định danh, ghi/đọc ngăn có khóa riêng. Đọc ngăn là POST (thân mang mục
	// đích, phản hồi no-store) và KHÔNG qua Idem (phản hồi chứa định danh, R-40).
	rg.POST("/subjects/:ref/amend", h.Idem, h.amend)
	rg.POST("/subjects/:ref/sections/:section", h.Idem, h.putSection)
	rg.POST("/subjects/:ref/sections/:section/read", h.getSection)
	// W11.28: đọc hồ sơ nhân sự theo lô (báo cáo Sở, dashboard) — phản hồi no-store, không qua Idem (R-40).
	rg.POST("/subjects/batch-read", h.batchRead)
	rg.GET("/pubkeys", h.pubkeys)
	rg.GET("/pubkeys/households", h.householdPubkeys)
}

func purposeOf(c *gin.Context, body string) client.Purpose {
	if v := strings.TrimSpace(c.GetHeader(client.HeaderPurpose)); v != "" {
		return client.Purpose(v)
	}
	if body != "" {
		return client.Purpose(body)
	}
	return client.Purpose(c.Query("purpose"))
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func refOf(c *gin.Context) (uuid.UUID, bool) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil || ref == uuid.Nil {
		httpx.Fail(c, client.ProblemNotFound)
		return uuid.Nil, false
	}
	return ref, true
}

func (h Handler) create(c *gin.Context) {
	var req client.CreateRequest
	if !bind(c, &req) {
		return
	}
	out, err := h.Svc.Create(c.Request.Context(), purposeOf(c, req.Purpose), client.Identity{
		Kind: client.SubjectKind(req.Kind), FullName: req.FullName, Phone: req.Phone, NationalID: req.NationalID,
		Address: req.Address, AreaCode: req.AreaCode,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/subjects/"+out.Ref.String()+"/display")
	c.JSON(http.StatusCreated, client.CreatedResponse{Ref: out.Ref.String(), PublicKey: out.PublicKey})
}

func (h Handler) display(c *gin.Context) {
	ref, ok := refOf(c)
	if !ok {
		return
	}
	out, err := h.Svc.Display(c.Request.Context(), purposeOf(c, ""), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, client.DisplayResponse{Ref: ref.String(), Label: out.Label, MaskedPhone: out.MaskedPhone})
}

func (h Handler) resolve(c *gin.Context) {
	ref, ok := refOf(c)
	if !ok {
		return
	}
	var req client.PurposeRequest
	if c.Request.ContentLength != 0 && !bind(c, &req) {
		return
	}
	out, err := h.Svc.Resolve(c.Request.Context(), purposeOf(c, req.Purpose), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, client.IdentityResponse{
		Ref: ref.String(), Kind: string(out.Kind), FullName: out.FullName, Phone: out.Phone,
		NationalID: out.NationalID, Address: out.Address, AreaCode: out.AreaCode,
	})
}

func (h Handler) lookup(c *gin.Context) {
	var req client.LookupRequest
	if !bind(c, &req) {
		return
	}
	ref, err := h.Svc.Lookup(c.Request.Context(), purposeOf(c, req.Purpose), req.Value)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, client.LookupResponse{Ref: ref.String()})
}

func (h Handler) unwrap(c *gin.Context) {
	ref, ok := refOf(c)
	if !ok {
		return
	}
	var req client.UnwrapRequest
	if !bind(c, &req) {
		return
	}
	dek, err := h.Svc.Unwrap(c.Request.Context(), purposeOf(c, req.Purpose), ref, req.WrappedDEK)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, client.UnwrapResponse{DEK: dek})
	clear(dek)
}

func (h Handler) erase(c *gin.Context) {
	ref, ok := refOf(c)
	if !ok {
		return
	}
	var req client.ErasureRequest
	if !bind(c, &req) {
		return
	}
	if err := h.Svc.Erase(c.Request.Context(), ref, req.Reason); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, client.ErasureResponse{Ref: ref.String(), Erased: true})
}

// householdPubkeys: GET /v1/pubkeys/households?ref=…&ref=… — khóa công khai theo ref nông hộ.
func (h Handler) householdPubkeys(c *gin.Context) {
	var refs []uuid.UUID
	for _, a := range c.QueryArray("ref") {
		for _, part := range strings.Split(a, ",") {
			if part = strings.TrimSpace(part); part == "" {
				continue
			}
			u, err := uuid.Parse(part)
			if err != nil {
				httpx.Fail(c, problem.Validation.WithField("ref", "phải là UUID"))
				return
			}
			refs = append(refs, u)
		}
	}
	items, err := h.Svc.HouseholdPubKeys(c.Request.Context(), refs)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	resp := client.PubKeysResponse{Items: make([]client.PubKeyItem, 0, len(items))}
	for _, it := range items {
		resp.Items = append(resp.Items, client.PubKeyItem{Ref: it.Ref.String(), AreaCode: it.AreaCode, PublicKey: it.PublicKey})
	}
	c.JSON(http.StatusOK, resp)
}

func (h Handler) pubkeys(c *gin.Context) {
	var areas []string
	for _, a := range c.QueryArray("area") {
		for _, part := range strings.Split(a, ",") {
			if part = strings.TrimSpace(part); part != "" {
				areas = append(areas, part)
			}
		}
	}
	var after uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		if err != nil || len(b) != 16 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		copy(after[:], b)
	}
	limit := 1000
	if s := c.Query("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			limit = n
		}
	}
	items, next, err := h.Svc.PubKeysPage(c.Request.Context(), areas, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	resp := client.PubKeysResponse{Items: make([]client.PubKeyItem, 0, len(items))}
	for _, it := range items {
		resp.Items = append(resp.Items, client.PubKeyItem{Ref: it.Ref.String(), AreaCode: it.AreaCode, PublicKey: it.PublicKey})
	}
	if next != nil {
		s := base64.RawURLEncoding.EncodeToString(next[:])
		resp.NextCursor = &s
	}
	c.JSON(http.StatusOK, resp)
}

func (h Handler) amend(c *gin.Context) {
	ref, ok := refOf(c)
	if !ok {
		return
	}
	var req client.AmendRequest
	if !bind(c, &req) {
		return
	}
	if err := h.Svc.Amend(c.Request.Context(), purposeOf(c, req.Purpose), ref,
		client.Identity{FullName: req.FullName, Phone: req.Phone, Address: req.Address}); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, client.WriteResponse{Ref: ref.String()})
}

func (h Handler) putSection(c *gin.Context) {
	ref, ok := refOf(c)
	if !ok {
		return
	}
	var req client.SectionRequest
	if !bind(c, &req) {
		return
	}
	if err := h.Svc.PutSection(c.Request.Context(), purposeOf(c, req.Purpose), ref, c.Param("section"), req.Fields); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, client.WriteResponse{Ref: ref.String()})
}

func (h Handler) getSection(c *gin.Context) {
	ref, ok := refOf(c)
	if !ok {
		return
	}
	var req client.PurposeRequest
	if c.Request.ContentLength != 0 && !bind(c, &req) {
		return
	}
	section := c.Param("section")
	out, err := h.Svc.GetSection(c.Request.Context(), purposeOf(c, req.Purpose), ref, section)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, client.SectionResponse{Ref: ref.String(), Section: section, Fields: out})
}

func (h Handler) cards(c *gin.Context) {
	var req client.CardsRequest
	if !bind(c, &req) {
		return
	}
	if len(req.Refs) > client.MaxCardRefs {
		httpx.Fail(c, problem.Validation.WithField("refs", "tối đa 200 chủ thể mỗi lần"))
		return
	}
	refs := make([]uuid.UUID, 0, len(req.Refs))
	for _, r := range req.Refs {
		ref, err := uuid.Parse(r)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("refs", "mã tham chiếu không hợp lệ"))
			return
		}
		refs = append(refs, ref)
	}
	out, err := h.Svc.Cards(c.Request.Context(), purposeOf(c, req.Purpose), refs, req.Fields)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]client.CardItem, 0, len(out))
	for _, x := range out {
		items = append(items, client.CardItem{Ref: x.Ref.String(), FullName: x.FullName, Phone: x.Phone, BirthYear: x.BirthYear,
			Email: x.Email, Erased: x.Erased})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, client.CardsResponse{Items: items})
}
