// Package handler: route hồ sơ KN riêng HTX / Nông hộ / Tổ hợp tác — KNSPLIT-2-KN-RECORDS (#67), nền TT-08/TT-09.
// Thay các route ghi legacy /v1/kn/cooperatives… (pf/party, pf/cooperative) cho màn KN. Mọi route cần header
// X-AG-Context; POST cần Idempotency-Key. Sửa là POST lên tài nguyên kèm base_version (như hộ mô hình CNTS).
package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knrecords/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knrecords/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// MaxBody là kích thước thân request tối đa (byte).
const MaxBody = 16 << 10

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/kn/producer-records", h.list)
	rg.POST("/kn/producer-records", h.create)
	rg.GET("/kn/producer-records/:ref", h.get)
	rg.POST("/kn/producer-records/:ref", h.update)
	rg.POST("/kn/producer-records/:ref/deactivate", h.status(true))
	rg.POST("/kn/producer-records/:ref/reactivate", h.status(false))
	rg.GET("/kn/producer-htx-candidates", h.htxCandidates)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func bind(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBody)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("thân JSON không hợp lệ"))
		return false
	}
	return true
}

func pathRef(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func queryRef(c *gin.Context, name string) (*uuid.UUID, bool) {
	s := c.Query(name)
	if s == "" {
		return nil, true
	}
	id, err := uuid.Parse(s)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, "không hợp lệ"))
		return nil, false
	}
	return &id, true
}

func strp(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func refp(p *uuid.UUID) any {
	if p == nil {
		return nil
	}
	return p.String()
}

func personDTO(p app.Person) any {
	if p.Unavailable {
		return gin.H{"label": "Không hiển thị được", "masked_phone": nil}
	}
	var phone any
	if p.MaskedPhone != "" {
		phone = p.MaskedPhone
	}
	return gin.H{"label": p.Label, "masked_phone": phone}
}

func dto(v app.View) gin.H {
	var area any
	if v.AreaHa != nil {
		area = v.AreaHa.String()
	}
	display := v.Person.Label
	if v.Name != nil {
		display = *v.Name
	}
	out := gin.H{
		"ref": v.Ref.String(), "kind": v.Kind, "kind_label": domain.KindLabel(v.Kind), "commune_code": v.CommuneCode,
		"name": strp(v.Name), "display_name": display, "parent_ref": refp(v.ParentRef), "parent_name": strp(v.ParentName),
		"parent_kind": strp(v.ParentKind), "htx_ref": refp(v.HTXRef), "htx_name": strp(v.HTXName), "area_ha": area,
		"tax_code": strp(v.TaxCode), "address": strp(v.Address), "member_count": v.MemberCount, "status": v.Status,
		"status_reason": strp(v.StatusReason), "version": v.Seq, "can_edit": v.CanEdit, "updated_at": ts(v.RecordedAt),
	}
	if v.Kind == domain.KindHousehold {
		out["holder"], out["representative"] = personDTO(v.Person), nil
	} else {
		out["representative"], out["holder"] = personDTO(v.Person), nil
	}
	return out
}

func detailDTO(v app.View) gin.H {
	out := dto(v)
	log := make([]gin.H, 0, len(v.Versions))
	for i := len(v.Versions) - 1; i >= 0; i-- {
		x := v.Versions[i]
		var name any
		if n, ok := v.Names[x.ActorRef]; ok && n != "" {
			name = n
		}
		log = append(log, gin.H{"version": x.Seq, "event": x.Event, "at": ts(x.RecordedAt), "actor_ref": x.ActorRef.String(),
			"actor_display": name, "status_reason": strp(x.StatusReason)})
	}
	out["log"] = log
	return out
}

func (h Handler) list(c *gin.Context) {
	q := app.Query{Kind: c.Query("kind"), Commune: c.Query("commune"), Status: c.Query("status"), Q: c.Query("q")}
	var ok bool
	if q.Parent, ok = queryRef(c, "parent_ref"); !ok {
		return
	}
	if q.HTX, ok = queryRef(c, "htx_ref"); !ok {
		return
	}
	if l := c.Query("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > app.MaxLimit {
			httpx.Fail(c, problem.Validation.WithField("limit", "số nguyên 1–200"))
			return
		}
		q.Limit = n
	}
	if cur, ok := queryRef(c, "cursor"); !ok {
		return
	} else if cur != nil {
		q.After = *cur
	}
	pg, err := h.Svc.List(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(pg.Items))
	for _, v := range pg.Items {
		items = append(items, dto(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": refp(pg.Next)})
}

func (h Handler) get(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	v, err := h.Svc.Get(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, detailDTO(v))
}

type personReq struct {
	FullName string `json:"full_name"`
	Phone    string `json:"phone"`
}

type recordReq struct {
	Kind           string          `json:"kind"`
	CommuneCode    string          `json:"commune_code"`
	Name           *string         `json:"name"`
	AreaHa         json.RawMessage `json:"area_ha"`
	TaxCode        *string         `json:"tax_code"`
	Address        *string         `json:"address"`
	Representative *personReq      `json:"representative"`
	Holder         *personReq      `json:"holder"`
	ParentRef      *string         `json:"parent_ref"`
	HTXRef         *string         `json:"htx_ref"`
	BaseVersion    *int64          `json:"base_version"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// areaText: số JSON hoặc chuỗi → chuỗi gốc để domain kiểm cú pháp thập phân thường; null/vắng → rỗng.
func areaText(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", true
	}
	if len(raw) > domain.MaxAreaInput+2 {
		return "", false
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return "", false
		}
		return s, true
	}
	var n json.Number
	if json.Unmarshal(raw, &n) != nil {
		return "", false
	}
	return n.String(), true
}

func (r recordReq) input(c *gin.Context) (app.Input, bool) {
	in := app.Input{Record: domain.Record{Kind: r.Kind, CommuneCode: r.CommuneCode, Name: deref(r.Name), TaxCode: deref(r.TaxCode),
		Address: deref(r.Address)}, BaseVersion: r.BaseVersion, IdemKey: idem(c)}
	area, ok := areaText(r.AreaHa)
	if !ok {
		httpx.Fail(c, problem.Validation.WithField("area_ha", domain.ErrArea.Error()))
		return in, false
	}
	in.AreaRaw = area
	switch {
	case r.Representative != nil && r.Holder != nil:
		httpx.Fail(c, problem.Validation.WithField("holder", "gửi representative (HTX, THT) HOẶC holder (Nông hộ)"))
		return in, false
	case r.Representative != nil:
		in.Person = &domain.Person{FullName: r.Representative.FullName, Phone: r.Representative.Phone}
	case r.Holder != nil:
		in.Person = &domain.Person{FullName: r.Holder.FullName, Phone: r.Holder.Phone}
	}
	for _, x := range []struct {
		name string
		s    *string
		dst  **uuid.UUID
	}{{"parent_ref", r.ParentRef, &in.ParentRef}, {"htx_ref", r.HTXRef, &in.HTXRef}} {
		if x.s == nil || *x.s == "" {
			continue
		}
		id, err := uuid.Parse(*x.s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField(x.name, "không hợp lệ"))
			return in, false
		}
		*x.dst = &id
	}
	return in, true
}

func (h Handler) create(c *gin.Context) {
	var req recordReq
	if !bind(c, &req) {
		return
	}
	if req.BaseVersion != nil {
		httpx.Fail(c, problem.Validation.WithField("base_version", "chỉ dùng khi sửa"))
		return
	}
	in, ok := req.input(c)
	if !ok {
		return
	}
	v, err := h.Svc.Create(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	code := http.StatusCreated
	if v.Replayed {
		code = http.StatusOK
	}
	c.JSON(code, detailDTO(v))
}

func (h Handler) update(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req recordReq
	if !bind(c, &req) {
		return
	}
	in, ok := req.input(c)
	if !ok {
		return
	}
	v, err := h.Svc.Update(c.Request.Context(), ref, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, detailDTO(v))
}

type statusReq struct {
	Reason      string `json:"reason"`
	BaseVersion *int64 `json:"base_version"`
}

func (h Handler) status(deactivate bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ref, ok := pathRef(c)
		if !ok {
			return
		}
		var req statusReq
		if !bind(c, &req) {
			return
		}
		v, err := h.Svc.SetStatus(c.Request.Context(), ref, deactivate, req.BaseVersion, req.Reason, idem(c))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, detailDTO(v))
	}
}

func (h Handler) htxCandidates(c *gin.Context) {
	out, err := h.Svc.PublishedHTXs(c.Request.Context(), c.Query("commune"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(out))
	for _, o := range out {
		items = append(items, gin.H{"htx_ref": o.Ref.String(), "name": o.Name, "commune_code": o.CommuneCode, "linked_ref": refp(o.LinkedRef)})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
