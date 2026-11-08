// Package handler: route /v1/extension/cnts/livestock-households… — danh sách hộ chăn nuôi CNTS (CNTS-V4-HOUSEHOLD-LIST
// #251, BRD CNTS v4.0 FN-10). Mọi route cần header X-AG-Context (ngữ cảnh phiên KN); thao tác ghi (POST) cần
// Idempotency-Key. Sửa, check-in, vô hiệu hóa là POST lên tài nguyên kèm base_version.
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/cnts/livestock-households")
	g.GET("", h.list)
	g.POST("", h.create)
	g.GET("/:ref", h.get)
	g.POST("/:ref", h.update)
	g.POST("/:ref/checkin", h.checkin)
	g.POST("/:ref/deactivate", h.deactivate)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func bind(c *gin.Context, v any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("thân JSON không hợp lệ: "+err.Error()))
		return false
	}
	return true
}

func pathRef(c *gin.Context) (uuid.UUID, bool) {
	r, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrNotFound)
		return uuid.Nil, false
	}
	return r, true
}

type animalJSON struct {
	SpeciesCode string `json:"species_code"`
	HeadCount   int64  `json:"head_count"`
}

type checkinJSON struct {
	Lat        float64   `json:"lat"`
	Lon        float64   `json:"lon"`
	AccuracyM  string    `json:"accuracy_m"`
	Mocked     bool      `json:"mocked"`
	CapturedAt time.Time `json:"captured_at"`
}

func (c *checkinJSON) input() *app.CheckinInput {
	if c == nil {
		return nil
	}
	return &app.CheckinInput{Lat: c.Lat, Lon: c.Lon, AccuracyM: c.AccuracyM, Mocked: c.Mocked, CapturedAt: c.CapturedAt}
}

type householdReq struct {
	BaseVersion     *int64       `json:"base_version"`
	OwnerName       string       `json:"owner_name"`
	OwnerPhone      *string      `json:"owner_phone"`
	CommuneCode     string       `json:"commune_code"`
	Hamlet          string       `json:"hamlet"`
	LivestockType   string       `json:"livestock_type"`
	Animals         []animalJSON `json:"animals"`
	Checkin         *checkinJSON `json:"checkin"`
	ConfirmDistinct bool         `json:"confirm_distinct"`
}

func (r householdReq) input(c *gin.Context) app.Input {
	in := app.Input{Fields: domain.Fields{CommuneCode: r.CommuneCode, Hamlet: r.Hamlet, LivestockType: r.LivestockType},
		OwnerName: r.OwnerName, OwnerPhone: r.OwnerPhone, Checkin: r.Checkin.input(), ConfirmDistinct: r.ConfirmDistinct,
		BaseVersion: r.BaseVersion, IdemKey: idem(c)}
	for _, a := range r.Animals {
		in.Animals = append(in.Animals, domain.Animal{Species: a.SpeciesCode, Heads: a.HeadCount})
	}
	return in
}

func (h Handler) list(c *gin.Context) {
	q := app.Query{Commune: c.Query("commune"), Status: c.Query("status"), LivestockType: c.Query("livestock_type")}
	for key, dst := range map[string]*int{"limit": &q.Limit, "offset": &q.Offset} {
		if v := c.Query(key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || len(v) > 7 {
				httpx.Fail(c, problem.Validation.WithField(key, "số nguyên"))
				return
			}
			*dst = n
		}
	}
	p, err := h.Svc.List(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(p.Items))
	for _, v := range p.Items {
		items = append(items, out(v, false))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items, "total": p.Total, "limit": p.Limit, "offset": p.Offset})
}

func (h Handler) create(c *gin.Context) {
	var req householdReq
	if !bind(c, &req) {
		return
	}
	v, err := h.Svc.Create(c.Request.Context(), req.input(c))
	write(c, v, err, http.StatusCreated)
}

func (h Handler) get(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	v, err := h.Svc.Get(c.Request.Context(), ref)
	write(c, v, err, http.StatusOK)
}

func (h Handler) update(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req householdReq
	if !bind(c, &req) {
		return
	}
	if req.Checkin != nil {
		httpx.Fail(c, app.ErrInvalid.WithField("checkin", "đổi vị trí bằng check-in lại tại nhà hộ (POST …/checkin)"))
		return
	}
	v, err := h.Svc.Update(c.Request.Context(), ref, req.input(c))
	write(c, v, err, http.StatusOK)
}

type checkinReq struct {
	BaseVersion *int64       `json:"base_version"`
	Checkin     *checkinJSON `json:"checkin"`
	Reason      string       `json:"reason"`
}

func (h Handler) checkin(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req checkinReq
	if !bind(c, &req) {
		return
	}
	if req.Checkin == nil {
		httpx.Fail(c, app.ErrNeedCheckin.WithField("checkin", "chưa có vị trí GPS"))
		return
	}
	v, err := h.Svc.CheckinAt(c.Request.Context(), ref, req.BaseVersion, *req.Checkin.input(), req.Reason, idem(c))
	write(c, v, err, http.StatusOK)
}

type reasonReq struct {
	BaseVersion *int64 `json:"base_version"`
	Reason      string `json:"reason"`
}

func (h Handler) deactivate(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req reasonReq
	if !bind(c, &req) {
		return
	}
	v, err := h.Svc.Deactivate(c.Request.Context(), ref, req.BaseVersion, req.Reason, idem(c))
	write(c, v, err, http.StatusOK)
}

func write(c *gin.Context, v app.View, err error, status int) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if v.Replayed {
		status = http.StatusOK
	}
	if c.Request.Method == http.MethodPost {
		// Chỉ cache metadata cho phát lại: tên đầy đủ phải được đọc lại qua vault có audit.
		// Không sửa phản hồi/cached payload đã có trước gói này.
		cached := out(v, true)
		cached["owner_name"], cached["updated_by_name"] = nil, nil
		for _, row := range cached["history"].([]gin.H) {
			row["actor_name"] = nil
			for _, item := range row["changes"].([]gin.H) {
				if item["field"] == domain.FieldOwnerName {
					item["old"], item["new"] = app.HistoryNameUnavailable, app.HistoryNameUnavailable
				}
			}
		}
		body, err := json.Marshal(cached)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		idempotency.SetStoredBody(c, body)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, out(v, true))
}

func optStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func out(v app.View, detail bool) gin.H {
	animals := make([]gin.H, 0, len(v.Animals))
	for _, a := range v.Animals {
		animals = append(animals, gin.H{"species_code": a.Species, "species_name": optStr(v.SpeciesNames[a.Species]), "head_count": a.Heads})
	}
	var loc any
	if v.Location != nil {
		loc = gin.H{"lat": v.Location.Lat, "lon": v.Location.Lon, "accuracy_m": v.Location.AccuracyM.String(),
			"mocked": v.Location.Mocked, "captured_at": v.Location.CapturedAt.UTC().Format(time.RFC3339)}
	}
	m := gin.H{
		"id": v.Ref.String(), "code": v.Code, "version": v.Seq, "status": v.Status, "event": v.Event,
		"commune_code": v.CommuneCode, "commune_name": optStr(v.CommuneName), "hamlet": optStr(v.Hamlet),
		"livestock_type": v.LivestockType, "animals": animals, "location": loc,
		"owner_label": optStr(v.OwnerLabel), "owner_masked_phone": optStr(v.OwnerPhone),
		"can_edit": v.CanEdit, "can_deactivate": v.CanDeact,
		"updated_at": v.EventTime.UTC().Format(time.RFC3339), "reason": optStr(v.Reason),
	}
	if !detail {
		return m
	}
	m["owner_name"] = optStr(v.OwnerName)
	m["updated_by_name"] = optStr(v.ActorName)
	warnings := v.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	m["warnings"] = warnings
	hist := make([]gin.H, 0, len(v.History))
	for i := len(v.History) - 1; i >= 0; i-- { // mới trước
		ch := v.History[i]
		items := make([]gin.H, 0, len(ch.Items))
		for _, it := range ch.Items {
			items = append(items, gin.H{"field": it.Field, "old": it.Old, "new": it.New})
		}
		hist = append(hist, gin.H{"version": ch.Seq, "event": ch.Event, "actor_ref": ch.ActorRef.String(),
			"actor_name": optStr(ch.ActorName), "at": ch.At.UTC().Format(time.RFC3339), "reason": optStr(ch.Reason), "changes": items})
	}
	m["history"] = hist
	return m
}
