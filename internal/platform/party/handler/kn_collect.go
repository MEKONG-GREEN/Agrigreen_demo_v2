package handler

// HTTP của KN v7.7 FN-XA-03.03 — HTX & nông hộ trên các xã phụ trách (hợp đồng P7 §1). Application API, bắt buộc
// header X-AG-Context (ngữ cảnh phiên P1):
//   GET /v1/kn/cooperatives, GET /v1/kn/cooperatives/{ref}, GET /v1/kn/cooperatives/{ref}/log.
// Ghi hồ sơ KN qua /v1/kn/producer-records (#67/#161); route ghi HTX legacy đã nghỉ (#160).
// Nông hộ của màn (/v1/kn/cooperatives/{ref}/households, /v1/kn/households/*) ở pf/cooperative (W3.3).

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// RegisterKNCollect gắn route FN-XA-03.03 vào nhóm Application API.
func (h KN) RegisterKNCollect(rg *gin.RouterGroup) {
	g := rg.Group("/kn")
	g.GET("/cooperatives", h.knListCoops)
	g.GET("/cooperatives/:ref", h.knGetCoop)
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
