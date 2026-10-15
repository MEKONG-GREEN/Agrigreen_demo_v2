// Package handler: route /v1/extension/cnts/duty-rosters…, /v1/extension/cnts/my-duties, /v1/extension/cnts/vaccination-
// batches… (W11.46 — lịch trực KSGM BM-03, lịch tiêm phòng BM-07). Mọi route cần header X-AG-Context (ngữ cảnh phiên
// KN); thao tác ghi (POST) cần Idempotency-Key.
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/cntscal/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/cntscal/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/cntscal/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/cnts")
	g.GET("/duty-rosters/:month", h.getRoster)
	g.POST("/duty-rosters/:month", h.saveRoster)
	g.GET("/my-duties", h.myDuties)
	g.GET("/vaccination-batches", h.listBatches)
	g.POST("/vaccination-batches", h.createBatch)
	g.GET("/vaccination-batches/:ref", h.getBatch)
	g.POST("/vaccination-batches/:ref/doses", h.updateDoses)
	g.POST("/vaccination-batches/:ref/cancel", h.cancelBatch)
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

func nameOf(m map[uuid.UUID]string, r uuid.UUID) any {
	if n, ok := m[r]; ok && n != "" {
		return n
	}
	return nil
}

func optTS(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func optStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func optRef(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return u.String()
}

// ---------------------------------------------------------------------------
// Lịch trực KSGM
// ---------------------------------------------------------------------------

var shiftOut = map[string]string{domain.ShiftDay: "N", domain.ShiftNight: "D", domain.ShiftBoth: "ND"}

func cellsOut(cs []domain.Cell) []gin.H {
	out := make([]gin.H, 0, len(cs))
	for _, x := range cs {
		out = append(out, gin.H{"facility_ref": x.Facility, "officer_ref": x.Officer, "day": x.Day, "shift": shiftOut[x.Shift]})
	}
	return out
}

func rosterOut(v app.RosterView) gin.H {
	facs := make([]gin.H, 0, len(v.Facilities))
	for _, f := range v.Facilities {
		facs = append(facs, gin.H{"ref": f.Ref, "code": f.Code, "name": f.Name, "commune_code": f.CommuneCode, "kind": optStr(f.Kind)})
	}
	offs := make([]gin.H, 0, len(v.Officers))
	for _, o := range v.Officers {
		fr := make([]string, 0, len(o.Scope.Facilities))
		for _, f := range o.Scope.Facilities {
			fr = append(fr, f.String())
		}
		offs = append(offs, gin.H{"ref": o.Ref, "display": nameOf(v.Names, o.Ref), "commune_codes": o.Communes, "facility_refs": fr})
	}
	warns := make([]gin.H, 0, len(v.Warnings))
	for _, w := range v.Warnings {
		var f, o any
		if w.Facility != uuid.Nil {
			f = w.Facility
		}
		if w.Officer != uuid.Nil {
			o = w.Officer
		}
		days := w.Days
		if days == nil {
			days = []int{}
		}
		warns = append(warns, gin.H{"code": w.Code, "message": w.Message, "facility_ref": f, "officer_ref": o, "days": days})
	}
	vers := make([]gin.H, 0, len(v.Versions))
	for _, x := range v.Versions {
		vers = append(vers, gin.H{"version": x.Version, "source": x.Source, "cell_count": x.CellCount, "note": optStr(x.Note),
			"at": x.At.UTC().Format(time.RFC3339), "actor_ref": x.ActorRef, "actor_display": nameOf(v.Names, x.ActorRef)})
	}
	notified := make([]string, 0, len(v.Notified))
	for _, r := range v.Notified {
		notified = append(notified, r.String())
	}
	var by any
	if v.UpdatedBy != nil {
		by = *v.UpdatedBy
	}
	return gin.H{"month": domain.MonthKey(v.Month), "province_code": optStr(v.Province), "legacy": v.Legacy, "days_in_month": domain.DaysIn(v.Month), "version": v.Version,
		"source": optStr(v.Source), "note": optStr(v.Note), "cells": cellsOut(v.Cells), "facilities": facs, "officers": offs,
		"warnings": warns, "updated_at": optTS(v.UpdatedAt), "updated_by": by, "versions": vers, "can_edit": v.CanEdit,
		"notified_officer_refs": notified}
}

func (h Handler) getRoster(c *gin.Context) {
	v, err := h.Svc.GetRoster(c.Request.Context(), c.Param("month"), c.Query("province_code"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, rosterOut(v))
}

type cellReq struct {
	FacilityRef string `json:"facility_ref"`
	OfficerRef  string `json:"officer_ref"`
	Day         int    `json:"day"`
	Shift       string `json:"shift"`
}

type rosterReq struct {
	ProvinceCode string    `json:"province_code"`
	BaseVersion  *int64    `json:"base_version"`
	Cells        []cellReq `json:"cells"`
	Source       string    `json:"source"`
	Note         string    `json:"note"`
}

func (h Handler) saveRoster(c *gin.Context) {
	var req rosterReq
	if !bind(c, &req) {
		return
	}
	v := problem.Validation
	bad := false
	cells := make([]domain.Cell, 0, len(req.Cells))
	for i, x := range req.Cells {
		f, err1 := uuid.Parse(x.FacilityRef)
		o, err2 := uuid.Parse(x.OfficerRef)
		if err1 != nil || err2 != nil {
			v, bad = v.WithField("cells["+strconv.Itoa(i)+"]", "facility_ref, officer_ref phải là UUID"), true
			continue
		}
		cells = append(cells, domain.Cell{Facility: f, Officer: o, Day: x.Day, Shift: x.Shift})
	}
	if bad {
		httpx.Fail(c, v)
		return
	}
	out, err := h.Svc.SaveRoster(c.Request.Context(), app.SaveRosterInput{Month: c.Param("month"), ProvinceCode: req.ProvinceCode, BaseVersion: req.BaseVersion,
		Cells: cells, Source: req.Source, Note: req.Note, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if out.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, rosterOut(out))
}

func (h Handler) myDuties(c *gin.Context) {
	m, err := h.Svc.GetMyDuties(c.Request.Context(), c.Query("month"), c.Query("province_code"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(m.Items))
	for _, d := range m.Items {
		cols := make([]gin.H, 0, len(d.Colleagues))
		for _, r := range d.Colleagues {
			cols = append(cols, gin.H{"ref": r, "display": nameOf(m.Names, r)})
		}
		items = append(items, gin.H{"date": d.Date.Format(time.DateOnly), "shift": shiftOut[d.Shift],
			"facility": gin.H{"ref": d.Facility.Ref, "code": optStr(d.Facility.Code), "name": optStr(d.Facility.Name),
				"commune_code": optStr(d.Facility.CommuneCode)}, "colleagues": cols})
	}
	c.JSON(http.StatusOK, gin.H{"month": domain.MonthKey(m.Month), "version": m.Version, "items": items})
}

// ---------------------------------------------------------------------------
// Lịch tiêm phòng
// ---------------------------------------------------------------------------

func doseOut(d domain.Dose, today time.Time, labels map[string]string) gin.H {
	var actual any
	if d.Actual != nil {
		actual = d.Actual.Format(time.DateOnly)
	}
	return gin.H{"vaccine_code": d.Vaccine, "vaccine_name": optStr(labels["cnts_vaccine:"+d.Vaccine]), "shot": d.Shot,
		"age_days": d.AgeDays, "expected_date": d.Expected.Format(time.DateOnly), "actual_date": actual, "note": optStr(d.Note),
		"overdue": d.Overdue(today)}
}

func batchCore(b store.Batch, today time.Time, labels map[string]string) gin.H {
	doses := make([]gin.H, 0, len(b.Doses))
	for _, d := range b.Doses {
		doses = append(doses, doseOut(d, today, labels))
	}
	var head any
	if b.HeadCount != nil {
		head = *b.HeadCount
	}
	return gin.H{"ref": b.Ref, "version": b.Seq, "event": b.Event, "commune_code": b.CommuneCode, "model_type_code": b.ModelType,
		"model_type_name": optStr(labels["cnts_model_type:"+b.ModelType]), "breed_code": optStr(b.Breed),
		"breed_name": optStr(labels["cnts_breed:"+b.Breed]), "schedule_code": b.Schedule,
		"schedule_name":       optStr(labels["cnts_vaccination_schedule:"+b.Schedule]),
		"schedule_valid_from": b.ScheduleValidFrom.Format(time.DateOnly), "household_ref": optRef(b.HouseholdRef),
		"locality": optStr(b.Locality), "received_date": b.Received.Format(time.DateOnly), "age_at_receipt": b.AgeAtReceipt,
		"head_count": head, "doses": doses, "note": optStr(b.Note), "status_reason": optStr(b.StatusReason),
		"updated_at": b.EventTime.UTC().Format(time.RFC3339), "updated_by": b.ActorRef}
}

func batchOut(v app.BatchView, today time.Time, labels map[string]string) gin.H {
	m := batchCore(v.Batch, today, labels)
	m["status"], m["overdue_count"], m["can_plan"], m["can_record"] = v.Status, v.OverdueCount, v.CanPlan, v.CanRecord
	m["next_dose"] = nil
	if v.Next != nil {
		m["next_dose"] = doseOut(*v.Next, today, labels)
	}
	return m
}

func parseDay(v *problem.Problem, bad *bool, field, s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		*v, *bad = *v.WithField(field, "định dạng YYYY-MM-DD"), true
		return nil
	}
	return &t
}

func (h Handler) listBatches(c *gin.Context) {
	v, bad := *problem.Validation, false
	q := app.BatchQuery{Commune: c.Query("commune_code"), Status: c.Query("status"),
		From: parseDay(&v, &bad, "received_from", c.Query("received_from")), To: parseDay(&v, &bad, "received_to", c.Query("received_to"))}
	if s := c.Query("household_ref"); s != "" {
		r, err := uuid.Parse(s)
		if err != nil {
			v, bad = *v.WithField("household_ref", "phải là UUID"), true
		}
		q.Household = &r
	}
	if s := c.Query("limit"); s != "" {
		q.Limit, _ = strconv.Atoi(s)
	}
	if bad {
		httpx.Fail(c, &v)
		return
	}
	list, labels, err := h.Svc.ListBatches(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	t := h.Svc.Today()
	items := make([]gin.H, 0, len(list))
	for _, b := range list {
		items = append(items, batchOut(b, t, labels))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) getBatch(c *gin.Context) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrBatchNotFound)
		return
	}
	v, err := h.Svc.GetBatch(c.Request.Context(), ref)
	h.writeBatch(c, v, err, http.StatusOK)
}

func (h Handler) writeBatch(c *gin.Context, v app.BatchView, err error, created int) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	t := h.Svc.Today()
	m := batchOut(v, t, v.Labels)
	vers := make([]gin.H, 0, len(v.Versions))
	for i := len(v.Versions) - 1; i >= 0; i-- {
		b := v.Versions[i]
		x := batchCore(b, t, v.Labels)
		x["actor_display"] = nameOf(v.Names, b.ActorRef)
		vers = append(vers, x)
	}
	m["versions"] = vers
	status := created
	if v.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, m)
}

type batchReq struct {
	ID            *string `json:"id"`
	CommuneCode   string  `json:"commune_code"`
	ModelTypeCode string  `json:"model_type_code"`
	BreedCode     *string `json:"breed_code"`
	ScheduleCode  *string `json:"schedule_code"`
	HouseholdRef  *string `json:"household_ref"`
	Locality      *string `json:"locality"`
	ReceivedDate  string  `json:"received_date"`
	AgeAtReceipt  int     `json:"age_at_receipt"`
	HeadCount     *int    `json:"head_count"`
	Note          *string `json:"note"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (h Handler) createBatch(c *gin.Context) {
	var req batchReq
	if !bind(c, &req) {
		return
	}
	v, bad := *problem.Validation, false
	in := app.CreateBatchInput{CommuneCode: req.CommuneCode, ModelType: req.ModelTypeCode, Breed: deref(req.BreedCode),
		Schedule: deref(req.ScheduleCode), Locality: deref(req.Locality), AgeAtReceipt: req.AgeAtReceipt, HeadCount: req.HeadCount,
		Note: deref(req.Note), IdemKey: idem(c)}
	if req.ID != nil && *req.ID != "" {
		r, err := uuid.Parse(*req.ID)
		if err != nil || r.Version() != 7 {
			v, bad = *v.WithField("id", "phải là UUIDv7"), true
		}
		in.Ref = &r
	}
	if req.HouseholdRef != nil && *req.HouseholdRef != "" {
		r, err := uuid.Parse(*req.HouseholdRef)
		if err != nil {
			v, bad = *v.WithField("household_ref", "phải là UUID"), true
		}
		in.HouseholdRef = &r
	}
	if d := parseDay(&v, &bad, "received_date", req.ReceivedDate); d != nil {
		in.Received = *d
	} else if !bad {
		v, bad = *v.WithField("received_date", "bắt buộc"), true
	}
	if bad {
		httpx.Fail(c, &v)
		return
	}
	out, err := h.Svc.CreateBatch(c.Request.Context(), in)
	h.writeBatch(c, out, err, http.StatusCreated)
}

type doseReq struct {
	VaccineCode  string  `json:"vaccine_code"`
	Shot         int     `json:"shot"`
	ExpectedDate *string `json:"expected_date"`
	ActualDate   *string `json:"actual_date"`
	ClearActual  bool    `json:"clear_actual"`
	Note         *string `json:"note"`
}

type dosesReq struct {
	BaseVersion *int64    `json:"base_version"`
	Doses       []doseReq `json:"doses"`
	CapturedAt  *string   `json:"captured_at"`
}

func (h Handler) updateDoses(c *gin.Context) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrBatchNotFound)
		return
	}
	var req dosesReq
	if !bind(c, &req) {
		return
	}
	v, bad := *problem.Validation, false
	in := app.DoseChangeInput{Ref: ref, BaseVersion: req.BaseVersion, IdemKey: idem(c)}
	for i, d := range req.Doses {
		f := "doses[" + strconv.Itoa(i) + "]."
		u := domain.DoseUpdate{Vaccine: d.VaccineCode, Shot: d.Shot, ClearActual: d.ClearActual, Note: d.Note,
			Expected: parseDay(&v, &bad, f+"expected_date", deref(d.ExpectedDate)), Actual: parseDay(&v, &bad, f+"actual_date", deref(d.ActualDate))}
		in.Updates = append(in.Updates, u)
	}
	if req.CapturedAt != nil {
		t, err := time.Parse(time.RFC3339, *req.CapturedAt)
		if err != nil {
			v, bad = *v.WithField("captured_at", "RFC 3339"), true
		}
		in.CapturedAt = &t
	}
	if bad {
		httpx.Fail(c, &v)
		return
	}
	out, err := h.Svc.UpdateDoses(c.Request.Context(), in)
	h.writeBatch(c, out, err, http.StatusOK)
}

type cancelReq struct {
	BaseVersion *int64 `json:"base_version"`
	Reason      string `json:"reason"`
}

func (h Handler) cancelBatch(c *gin.Context) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrBatchNotFound)
		return
	}
	var req cancelReq
	if !bind(c, &req) {
		return
	}
	out, err := h.Svc.CancelBatch(c.Request.Context(), ref, req.BaseVersion, req.Reason, idem(c))
	h.writeBatch(c, out, err, http.StatusOK)
}
