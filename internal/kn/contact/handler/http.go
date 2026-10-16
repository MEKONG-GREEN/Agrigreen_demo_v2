// Package handler: HTTP của pf/contact (`cap/directory`, W11.23 — BRD HCTH v3.0 FN-13) — Application API /v1/contacts/*.
// Hợp đồng ở contracts/openapi/application.yaml, mục "# --- pf/contact ---".
//
// LỌC TRƯỜNG Ở API (BR-03): mỗi dòng được dựng bằng DTO RIÊNG của bộ trường người gọi — bộ "basic" (Cấp TW, Cấp Xã) chỉ có
// id, họ tên, năm sinh, SĐT; KHÔNG có khóa nào khác trên dây. Phản hồi chứa dữ liệu cá nhân: Cache-Control no-store.
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/contacts/access", h.access)
	rg.GET("/contacts/persons", h.persons)
	rg.POST("/contacts/persons", h.personCreate)
	rg.GET("/contacts/persons/:id", h.personGet)
	rg.POST("/contacts/persons/:id/revise", h.personRevise)
	rg.GET("/contacts/units", h.units)
	rg.POST("/contacts/units", h.unitCreate)
	rg.POST("/contacts/units/:id/revise", h.unitRevise)
}

func decode(c *gin.Context, v any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		// Không đưa nội dung lỗi của bộ giải mã (có thể chứa giá trị người dùng nhập — R-14).
		httpx.Fail(c, problem.Validation.WithDetail("thân yêu cầu không đúng dạng JSON của API"))
		return false
	}
	return true
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func noStore(c *gin.Context) { c.Header("Cache-Control", "no-store") }

func idParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		httpx.Fail(c, port.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

type accessDTO struct {
	FieldSet    string   `json:"field_set"`
	Fields      []string `json:"fields"`
	CanManage   bool     `json:"can_manage"`
	CanDetail   bool     `json:"can_detail"`
	StaffSource string   `json:"staff_source"`
}

func (h Handler) access(c *gin.Context) {
	a, err := h.Svc.MyAccess(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, accessDTO{FieldSet: string(a.Set), Fields: a.Fields, CanManage: a.CanManage, CanDetail: a.CanDetail,
		StaffSource: a.StaffSource})
}

// basicPersonDTO: bộ "basic" — KHÔNG thêm trường vào struct này (BR-03, kiểm thử TestContact_FieldFiltering).
type basicPersonDTO struct {
	ID        string `json:"id"`
	FullName  string `json:"full_name"`
	BirthYear string `json:"birth_year"`
	Phone     string `json:"phone"`
}

type fullPersonDTO struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	FullName   string `json:"full_name"`
	BirthYear  string `json:"birth_year"`
	Phone      string `json:"phone"`
	Title      string `json:"title"`
	Unit       string `json:"unit"`
	UnitCode   string `json:"unit_code,omitempty"`
	Level      string `json:"level"`
	LevelLabel string `json:"level_label"`
	Email      string `json:"email"`
	Hidden     bool   `json:"hidden"`
	Erased     bool   `json:"erased"`
}

func personDTO(set domain.FieldSet, p app.Person) any {
	if set != domain.SetFull {
		return basicPersonDTO{ID: p.ID.String(), FullName: p.FullName, BirthYear: p.BirthYear, Phone: p.Phone}
	}
	return fullPersonDTO{ID: p.ID.String(), Source: p.Source, FullName: p.FullName, BirthYear: p.BirthYear, Phone: p.Phone,
		Title: p.Title, Unit: p.Unit, UnitCode: p.UnitCode, Level: p.Level, LevelLabel: domain.LevelLabel(p.Level), Email: p.Email,
		Hidden: p.Hidden, Erased: p.Erased}
}

type personListDTO struct {
	FieldSet    string   `json:"field_set"`
	Fields      []string `json:"fields"`
	Total       int      `json:"total"`
	StaffSource string   `json:"staff_source"`
	Items       []any    `json:"items"`
}

func intQuery(c *gin.Context, name string) int {
	n, _ := strconv.Atoi(c.Query(name))
	return n
}

func (h Handler) persons(c *gin.Context) {
	r, err := h.Svc.ListPersons(c.Request.Context(), app.ListFilter{Q: c.Query("q"), Level: c.Query("level"), Unit: c.Query("unit"),
		Title: c.Query("title"), IncludeHidden: c.Query("include_hidden") == "true", Offset: intQuery(c, "offset"),
		Limit: intQuery(c, "limit")})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := personListDTO{FieldSet: string(r.Set), Fields: r.Set.Fields(), Total: r.Total, StaffSource: r.StaffSource, Items: []any{}}
	for _, p := range r.Items {
		out.Items = append(out.Items, personDTO(r.Set, p))
	}
	noStore(c)
	c.JSON(http.StatusOK, out)
}

type historyDTO struct {
	Version       int64     `json:"version"`
	Change        string    `json:"change"`
	ChangedFields []string  `json:"changed_fields"`
	At            time.Time `json:"at"`
	ActorRef      string    `json:"actor_ref"`
}

type personDetailDTO struct {
	fullPersonDTO
	Organization string       `json:"organization"`
	CanEdit      bool         `json:"can_edit"`
	History      []historyDTO `json:"history"`
}

func detailDTO(d app.Detail) personDetailDTO {
	out := personDetailDTO{fullPersonDTO: personDTO(domain.SetFull, d.Person).(fullPersonDTO), Organization: d.Organization,
		CanEdit: d.CanEdit, History: []historyDTO{}}
	for _, v := range d.History {
		f := v.ChangedFields
		if f == nil {
			f = []string{}
		}
		out.History = append(out.History, historyDTO{Version: v.Version, Change: v.Change, ChangedFields: f, At: v.At,
			ActorRef: v.Actor.String()})
	}
	return out
}

func (h Handler) personGet(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	d, err := h.Svc.GetPerson(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	noStore(c)
	c.JSON(http.StatusOK, detailDTO(d))
}

type personReq struct {
	FullName     *string `json:"full_name"`
	Phone        *string `json:"phone"`
	BirthYear    *string `json:"birth_year"`
	Email        *string `json:"email"`
	Level        *string `json:"level"`
	Organization *string `json:"organization"`
	Title        *string `json:"title"`
	Hidden       *bool   `json:"hidden"`
}

func (r personReq) in() app.PersonInput {
	return app.PersonInput{FullName: r.FullName, Phone: r.Phone, BirthYear: r.BirthYear, Email: r.Email, Level: r.Level,
		Organization: r.Organization, Title: r.Title, Hidden: r.Hidden}
}

func (h Handler) personCreate(c *gin.Context) {
	var req personReq
	if !decode(c, &req) {
		return
	}
	d, err := h.Svc.CreatePerson(c.Request.Context(), req.in(), idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	noStore(c)
	c.JSON(http.StatusCreated, detailDTO(d))
}

func (h Handler) personRevise(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req personReq
	if !decode(c, &req) {
		return
	}
	d, err := h.Svc.RevisePerson(c.Request.Context(), id, req.in(), idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	noStore(c)
	c.JSON(http.StatusOK, detailDTO(d))
}

// basicUnitDTO: thẻ Đơn vị của bộ "basic" — tên, cấp, SĐT (FN-13 B3).
type basicUnitDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Level      string `json:"level"`
	LevelLabel string `json:"level_label"`
	Phone      string `json:"phone"`
}

type fullUnitDTO struct {
	basicUnitDTO
	Email   string  `json:"email"`
	Address string  `json:"address"`
	OrgRef  *string `json:"org_ref"`
	Hidden  bool    `json:"hidden"`
}

func unitDTO(set domain.FieldSet, u app.Unit) any {
	b := basicUnitDTO{ID: u.ID.String(), Name: u.Name, Level: u.Level, LevelLabel: domain.LevelLabel(u.Level), Phone: u.Phone}
	if set != domain.SetFull {
		return b
	}
	out := fullUnitDTO{basicUnitDTO: b, Email: u.Email, Address: u.Address, Hidden: u.Hidden}
	if u.OrgRef != nil {
		s := u.OrgRef.String()
		out.OrgRef = &s
	}
	return out
}

func (h Handler) units(c *gin.Context) {
	r, err := h.Svc.ListUnits(c.Request.Context(), c.Query("q"), c.Query("level"), c.Query("include_hidden") == "true")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := []any{}
	for _, u := range r.Items {
		items = append(items, unitDTO(r.Set, u))
	}
	c.JSON(http.StatusOK, gin.H{"field_set": string(r.Set), "items": items})
}

type unitReq struct {
	Name    string  `json:"name"`
	Level   string  `json:"level"`
	Phone   *string `json:"phone"`
	Email   *string `json:"email"`
	Address *string `json:"address"`
	OrgRef  *string `json:"org_ref"`
	Hidden  *bool   `json:"hidden"`
}

func (h Handler) saveUnit(c *gin.Context, id uuid.UUID, status int) {
	var req unitReq
	if !decode(c, &req) {
		return
	}
	w := app.UnitWrite{In: domain.UnitInput{Name: req.Name, Level: req.Level, Phone: req.Phone, Email: req.Email, Address: req.Address},
		Hidden: req.Hidden}
	if req.OrgRef != nil && *req.OrgRef != "" {
		o, err := uuid.Parse(*req.OrgRef)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("org_ref", "UUID"))
			return
		}
		w.OrgRef = &o
	}
	u, err := h.Svc.SaveUnit(c.Request.Context(), id, w, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(status, unitDTO(domain.SetFull, u))
}

func (h Handler) unitCreate(c *gin.Context) { h.saveUnit(c, uuid.Nil, http.StatusCreated) }

func (h Handler) unitRevise(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	h.saveUnit(c, id, http.StatusOK)
}
