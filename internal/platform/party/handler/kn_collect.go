package handler

// HTTP của KN v7.7 FN-XA-03.03 — HTX & nông hộ trên các xã phụ trách (hợp đồng P7 §1). Application API, bắt buộc
// header X-AG-Context (ngữ cảnh phiên P1):
//   GET  /v1/kn/cooperatives, POST /v1/kn/cooperatives, GET|PUT /v1/kn/cooperatives/{ref},
//   POST /v1/kn/cooperatives/{ref}/deactivate|reactivate, GET /v1/kn/cooperatives/{ref}/log.
// Nông hộ của màn (/v1/kn/cooperatives/{ref}/households, /v1/kn/households/*) ở pf/cooperative (W3.3).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
)

// RegisterKNCollect gắn route FN-XA-03.03 vào nhóm Application API.
func (h KN) RegisterKNCollect(rg *gin.RouterGroup) {
	g := rg.Group("/kn")
	g.GET("/cooperatives", h.knListCoops)
	g.POST("/cooperatives", h.knCreateCoop)
	g.GET("/cooperatives/:ref", h.knGetCoop)
	g.PUT("/cooperatives/:ref", h.knUpdateCoop)
	g.POST("/cooperatives/:ref/deactivate", h.knDeactivateCoop)
	g.POST("/cooperatives/:ref/reactivate", h.knReactivateCoop)
	g.GET("/cooperatives/:ref/log", h.knCoopLog)
	// Nông hộ (/kn/cooperatives/{ref}/households, /kn/households/*) do pf/cooperative phục vụ (W3.3).
}

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type knPersonDTO struct {
	Label       string  `json:"label" pii:"true"`
	MaskedPhone *string `json:"masked_phone" pii:"true"`
}

// unavailableLabel hiển thị khi kho định danh không trả nhãn (đã xóa định danh hoặc lỗi).
const unavailableLabel = "Không hiển thị được"

func toPersonDTO(p app.KNPerson) knPersonDTO {
	if p.Unavailable {
		return knPersonDTO{Label: unavailableLabel}
	}
	return knPersonDTO{Label: p.Label, MaskedPhone: strPtr(p.MaskedPhone)}
}

type knCoopDTO struct {
	Ref            string       `json:"ref"`
	Code           string       `json:"code"`
	Name           string       `json:"name"`
	CommuneCode    string       `json:"commune_code"`
	AreaHa         *string      `json:"area_ha"`
	Representative *knPersonDTO `json:"representative"`
	HouseholdCount int          `json:"household_count"`
	Active         bool         `json:"active"`
	StatusReason   *string      `json:"status_reason"`
	TaxCode        *string      `json:"tax_code"`
	HeadOffice     *string      `json:"head_office"`
	UpdatedAt      string       `json:"updated_at"`
	// D2 (ADR-0052): confirmed | unconfirmed + nhãn "Đã xác nhận" | "Chưa xác nhận".
	ConfirmationStatus string `json:"confirmation_status"`
	ConfirmationLabel  string `json:"confirmation_label"`
}

func toKNCoopDTO(c app.KNCooperative) knCoopDTO {
	out := knCoopDTO{Ref: c.Ref.String(), Code: c.Code, Name: c.Name, CommuneCode: c.AdminUnitCode, HouseholdCount: c.HouseholdCount,
		Active: c.Active, StatusReason: strPtr(c.StatusReason), TaxCode: strPtr(c.Profile.TaxCode), HeadOffice: strPtr(c.Profile.Address),
		UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339Nano)}
	out.ConfirmationStatus, out.ConfirmationLabel = confirmation(c.Profile.Confirmed)
	if c.Profile.AreaHa != nil {
		s := c.Profile.AreaHa.String()
		out.AreaHa = &s
	}
	if c.Representative != nil {
		d := toPersonDTO(*c.Representative)
		out.Representative = &d
	}
	return out
}

// ---------------------------------------------------------------------------
// Tiện ích
// ---------------------------------------------------------------------------

// decimalText đọc trường số thập phân gửi dạng chuỗi hoặc số JSON. nil = không gửi; "" = null (xóa).
func decimalText(f *fieldErrs, field string, raw json.RawMessage) *string {
	if raw == nil {
		return nil
	}
	t := bytes.TrimSpace(raw)
	empty := ""
	if bytes.Equal(t, []byte("null")) {
		return &empty
	}
	var s string
	if len(t) > 0 && t[0] == '"' {
		if err := json.Unmarshal(t, &s); err != nil {
			f.add(field, "phải là số thập phân")
			return nil
		}
	} else {
		s = string(t)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return &empty
	}
	if _, err := decimal.NewFromString(s); err != nil {
		f.add(field, "phải là số thập phân")
		return nil
	}
	return &s
}

func decimalValue(f *fieldErrs, field string, raw json.RawMessage) *decimal.Decimal {
	s := decimalText(f, field, raw)
	if s == nil || *s == "" {
		return nil
	}
	d, _ := decimal.NewFromString(*s)
	return &d
}

func decimalQuery(c *gin.Context, f *fieldErrs, name string) *decimal.Decimal {
	s := strings.TrimSpace(c.Query(name))
	if s == "" {
		return nil
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		f.add(name, "phải là số thập phân")
		return nil
	}
	return &d
}

// putKey là Idempotency-Key của PUT (middleware chỉ áp cho POST — app chống lặp bằng idem_key của sổ).
func putKey(c *gin.Context) string { return strings.TrimSpace(c.GetHeader("Idempotency-Key")) }

func writeKNLog(c *gin.Context, pg app.KNLogPage, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]logEntryDTO, 0, len(pg.Items))
	for _, e := range pg.Items {
		items = append(items, logEntryDTO{Seq: e.Seq, Event: e.Event, ActorRef: e.ActorRef.String(), ActorDisplay: strPtr(e.ActorDisplay),
			At: e.At.UTC().Format(time.RFC3339Nano), RecordedAt: e.RecordedAt.UTC().Format(time.RFC3339Nano), Changes: e.Changes, Reason: e.Reason})
	}
	var next any
	if pg.Next != nil {
		next = *pg.Next
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

// ---------------------------------------------------------------------------
// HTX
// ---------------------------------------------------------------------------

func (h KN) knListCoops(c *gin.Context) {
	after, ok := parseCursor(c)
	if !ok {
		return
	}
	var fe fieldErrs
	q := app.KNCoopQuery{Commune: c.Query("commune"), Status: c.Query("status"), Q: c.Query("q"),
		AreaMin: decimalQuery(c, &fe, "area_min"), AreaMax: decimalQuery(c, &fe, "area_max"), After: after, Limit: queryLimit(c),
		Confirmation: c.Query("confirmation")}
	if fe.fail(c) {
		return
	}
	pg, err := h.Svc.ListKNCooperatives(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]knCoopDTO, 0, len(pg.Items))
	for _, it := range pg.Items {
		items = append(items, toKNCoopDTO(it))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
}

func (h KN) knGetCoop(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	out, err := h.Svc.GetKNCooperative(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toKNCoopDTO(out))
}

type knRepReq struct {
	FullName string `json:"full_name" pii:"true"`
	Phone    string `json:"phone" pii:"true"`
}

func (r *knRepReq) toApp() *app.Representative {
	if r == nil {
		return nil
	}
	return &app.Representative{FullName: r.FullName, Phone: r.Phone}
}

type knCreateCoopReq struct {
	Name           string          `json:"name"`
	CommuneCode    string          `json:"commune_code"`
	AreaHa         json.RawMessage `json:"area_ha"`
	TaxCode        string          `json:"tax_code"`
	HeadOffice     string          `json:"head_office"`
	Representative *knRepReq       `json:"representative"`
}

func (h KN) knCreateCoop(c *gin.Context) {
	var req knCreateCoopReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	area := decimalValue(&fe, "area_ha", req.AreaHa)
	if fe.fail(c) {
		return
	}
	out, replayed, err := h.Svc.CreateKNCooperative(c.Request.Context(), app.KNCreateCoopInput{
		Name: req.Name, CommuneCode: req.CommuneCode, AreaHa: area, TaxCode: req.TaxCode, HeadOffice: req.HeadOffice,
		Representative: req.Representative.toApp(), IdemKey: idempotency.KeyFromContext(c.Request.Context()),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, toKNCoopDTO(out))
}

type knUpdateCoopReq struct {
	Name           *string         `json:"name"`
	AreaHa         json.RawMessage `json:"area_ha"`
	TaxCode        *string         `json:"tax_code"`
	HeadOffice     *string         `json:"head_office"`
	Representative *knRepReq       `json:"representative"`
	Reason         string          `json:"reason"`
}

func (h KN) knUpdateCoop(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req knUpdateCoopReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	area := decimalText(&fe, "area_ha", req.AreaHa)
	if fe.fail(c) {
		return
	}
	out, err := h.Svc.UpdateKNCooperative(c.Request.Context(), app.KNUpdateCoopInput{
		Ref: ref, Name: req.Name, AreaHa: area, TaxCode: req.TaxCode, HeadOffice: req.HeadOffice, Representative: req.Representative.toApp(),
		Reason: req.Reason, IdemKey: putKey(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toKNCoopDTO(out))
}

type knReasonReq struct {
	Reason string `json:"reason"`
}

func (h KN) knDeactivateCoop(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req knReasonReq
	if !bindJSON(c, &req) {
		return
	}
	out, w, _, err := h.Svc.DeactivateKNCooperative(c.Request.Context(), ref, req.Reason, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"cooperative": toKNCoopDTO(out),
		"warnings": gin.H{"households": w.Households, "plots": w.Plots, "machines": w.Machines}})
}

func (h KN) knReactivateCoop(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req knReasonReq
	if !bindJSON(c, &req) {
		return
	}
	out, _, err := h.Svc.ReactivateKNCooperative(c.Request.Context(), ref, req.Reason, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toKNCoopDTO(out))
}

func (h KN) knCoopLog(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	after, err := app.ParseLogCursor(c.Query("cursor"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	pg, err := h.Svc.CooperativeLog(c.Request.Context(), ref, after, limitOf(c))
	writeKNLog(c, pg, err)
}
