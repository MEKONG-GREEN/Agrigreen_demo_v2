package handler

// HTTP nông hộ của KN v7.7 FN-XA-03.03 (hợp đồng P7 §1). Application API, bắt buộc header X-AG-Context (ngữ cảnh
// phiên P1): GET|POST /v1/kn/cooperatives/{ref}/households, PUT /v1/kn/households/{ref},
// GET /v1/kn/households/{ref}/log. Phần HTX của màn ở pf/party.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// registerKN gắn route nông hộ của FN-XA-03.03 vào nhóm Application API (bắt buộc header X-AG-Context).
func (h Handler) registerKN(rg *gin.RouterGroup) {
	g := rg.Group("/kn")
	g.GET("/cooperatives/:ref/households", h.knListHouseholds)
	g.POST("/cooperatives/:ref/households", h.knCreateHousehold)
	g.PUT("/households/:ref", h.knUpdateHousehold)
	g.GET("/households/:ref/log", h.knHouseholdLog)
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

type knHouseholdDTO struct {
	Ref              string      `json:"ref"`
	Holder           knPersonDTO `json:"holder"`
	CultivatedAreaHa *string     `json:"cultivated_area_ha"`
	Active           bool        `json:"active"`
	SinceDate        *string     `json:"since_date"`
}

func toKNHouseholdDTO(h app.KNHousehold) knHouseholdDTO {
	out := knHouseholdDTO{Ref: h.Ref.String(), Holder: toPersonDTO(h.Holder), Active: h.Active}
	if h.CultivatedAreaHa != nil {
		s := h.CultivatedAreaHa.String()
		out.CultivatedAreaHa = &s
	}
	if h.MemberSince != nil {
		s := h.MemberSince.Format(time.DateOnly)
		out.SinceDate = &s
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
// Nông hộ
// ---------------------------------------------------------------------------

func (h Handler) knListHouseholds(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	after, ok := parseCursor(c)
	if !ok {
		return
	}
	pg, err := h.Svc.ListKNHouseholds(c.Request.Context(), ref, after, queryLimit(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]knHouseholdDTO, 0, len(pg.Items))
	for _, it := range pg.Items {
		items = append(items, toKNHouseholdDTO(it))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(pg.Next)})
}

type knCreateHouseholdReq struct {
	FullName         string          `json:"full_name" pii:"true"`
	Phone            string          `json:"phone" pii:"true"`
	CultivatedAreaHa json.RawMessage `json:"cultivated_area_ha"`
}

func (h Handler) knCreateHousehold(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req knCreateHouseholdReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	area := decimalValue(&fe, "cultivated_area_ha", req.CultivatedAreaHa)
	if strings.TrimSpace(req.FullName) == "" {
		fe.add("full_name", "bắt buộc")
	}
	if strings.TrimSpace(req.Phone) == "" {
		fe.add("phone", "bắt buộc")
	}
	if fe.fail(c) {
		return
	}
	out, err := h.Svc.CreateKNHousehold(c.Request.Context(), app.KNCreateHouseholdInput{
		CooperativeRef: ref, FullName: req.FullName, Phone: req.Phone, CultivatedAreaHa: area,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toKNHouseholdDTO(out))
}

type knUpdateHouseholdReq struct {
	CultivatedAreaHa json.RawMessage `json:"cultivated_area_ha"`
	Reason           string          `json:"reason"`
}

func (h Handler) knUpdateHousehold(c *gin.Context) {
	ref, ok := householdRef(c)
	if !ok {
		return
	}
	var req knUpdateHouseholdReq
	if !bindJSON(c, &req) {
		return
	}
	var fe fieldErrs
	area := decimalText(&fe, "cultivated_area_ha", req.CultivatedAreaHa)
	if fe.fail(c) {
		return
	}
	if area == nil {
		httpx.Fail(c, problem.Validation.WithField("cultivated_area_ha", "bắt buộc (null = xóa giá trị)"))
		return
	}
	out, err := h.Svc.UpdateKNHousehold(c.Request.Context(), ref, area, req.Reason, putKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toKNHouseholdDTO(out))
}

func (h Handler) knHouseholdLog(c *gin.Context) {
	ref, ok := householdRef(c)
	if !ok {
		return
	}
	after, err := app.ParseLogCursor(c.Query("cursor"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	pg, err := h.Svc.HouseholdLog(c.Request.Context(), ref, after, limitOf(c))
	writeKNLog(c, pg, err)
}

// logEntryDTO là một dòng khung Log (kn-v7.7-plan §1.5).
type logEntryDTO struct {
	Seq          int64                `json:"seq"`
	Event        string               `json:"event"`
	ActorRef     string               `json:"actor_ref"`
	ActorDisplay *string              `json:"actor_display"`
	At           string               `json:"at"`
	RecordedAt   string               `json:"recorded_at"`
	Changes      []domain.FieldChange `json:"changes"`
	Reason       *string              `json:"reason"`
}
