// Package handler: route /v1/extension/cnts/diary-cycles…, /diary-entries…, /diary-photos… (W11.48 — nhật ký nuôi
// BM-08/09/19: lứa, lần ghi, rà soát/xác thực, ảnh). Mọi route cần header X-AG-Context (ngữ cảnh phiên KN); thao tác ghi
// (POST) cần Idempotency-Key. Sửa là POST lên tài nguyên kèm base_version.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/cnts")
	g.GET("/diary-cycles", h.listCycles)
	g.POST("/diary-cycles", h.createCycle)
	g.GET("/diary-cycles/:ref", h.getCycle)
	g.POST("/diary-cycles/:ref", h.updateCycle)
	g.POST("/diary-cycles/:ref/close", h.closeCycle)
	g.POST("/diary-cycles/:ref/reopen", h.reopenCycle)
	g.GET("/diary-entries", h.listEntries)
	g.POST("/diary-entries", h.createEntry)
	g.GET("/diary-entries/:ref", h.getEntry)
	g.POST("/diary-entries/:ref", h.updateEntry)
	g.POST("/diary-entries/:ref/submit", h.simple(h.Svc.SubmitEntry))
	g.POST("/diary-entries/:ref/verify", h.simple(h.Svc.VerifyEntry))
	g.POST("/diary-entries/:ref/return", h.returnEntry)
	g.POST("/diary-entries/:ref/correct", h.correctEntry)
	g.POST("/diary-photos", h.uploadPhoto)
	g.GET("/diary-photos/:ref/content", h.photoContent)
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

type form struct {
	p   problem.Problem
	bad bool
}

func newForm() *form { return &form{p: *problem.Validation} }

func (f *form) fail(field, reason string) { f.p, f.bad = *f.p.WithField(field, reason), true }

func (f *form) ref(field string, s *string, v7 bool) *uuid.UUID {
	if s == nil || *s == "" {
		return nil
	}
	r, err := uuid.Parse(*s)
	if err != nil || (v7 && r.Version() != 7) {
		f.fail(field, map[bool]string{true: "phải là UUIDv7", false: "phải là UUID"}[v7])
		return nil
	}
	return &r
}

func (f *form) refs(field string, ss []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ss))
	for _, s := range ss {
		r, err := uuid.Parse(s)
		if err != nil {
			f.fail(field, "phải là UUID")
			return nil
		}
		out = append(out, r)
	}
	return out
}

func (f *form) date(field string, s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	d, err := time.Parse(time.DateOnly, *s)
	if err != nil {
		f.fail(field, "dạng YYYY-MM-DD")
		return nil
	}
	return &d
}

func (f *form) instant(field string, s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		f.fail(field, "dạng RFC 3339")
		return nil
	}
	return &t
}

func (f *form) dec(field string, s *string) *decimal.Decimal {
	if s == nil || *s == "" {
		return nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		f.fail(field, "số thập phân")
		return nil
	}
	return &d
}

func (f *form) done(c *gin.Context) bool {
	if f.bad {
		httpx.Fail(c, &f.p)
		return false
	}
	return true
}

func pathRef(c *gin.Context, nf error) (uuid.UUID, bool) {
	r, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, nf)
		return uuid.Nil, false
	}
	return r, true
}

func queryRef(c *gin.Context, f *form, key string) *uuid.UUID {
	v := c.Query(key)
	return f.ref(key, &v, false)
}

func nameOf(m map[uuid.UUID]string, r *uuid.UUID) any {
	if r == nil {
		return nil
	}
	if n, ok := m[*r]; ok && n != "" {
		return n
	}
	return nil
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

func optDec(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.String()
}

func refsOut(rs []uuid.UUID) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.String())
	}
	return out
}

func meta(m store.Meta, names map[uuid.UUID]string) gin.H {
	return gin.H{"version": m.Seq, "updated_at": m.RecordedAt.UTC().Format(time.RFC3339), "updated_by": m.ActorRef.String(),
		"updated_by_display": nameOf(names, &m.ActorRef)}
}

func merge(a, b gin.H) gin.H {
	for k, v := range b {
		a[k] = v
	}
	return a
}

func status(replayed bool, created int) int {
	if replayed {
		return http.StatusOK
	}
	return created
}

type statusReq struct {
	BaseVersion *int64 `json:"base_version"`
	Reason      string `json:"reason"`
}

// ---------------------------------------------------------------------------
// Lứa
// ---------------------------------------------------------------------------

type cycleReq struct {
	ID               *string         `json:"id"`
	HouseholdRef     *string         `json:"household_ref"`
	BaseVersion      *int64          `json:"base_version"`
	Form             string          `json:"form"`
	Breed            string          `json:"breed"`
	ReceivedDate     *string         `json:"received_date"`
	InitialCount     int64           `json:"initial_count"`
	InitialWeightG   *string         `json:"initial_weight_g"`
	AgeDays          *int64          `json:"age_days"`
	Supplies         []domain.Supply `json:"supplies"`
	VaccinationBatch *string         `json:"vaccination_batch_ref"`
	Note             string          `json:"note"`
}

func (r cycleReq) input(c *gin.Context, f *form) app.CycleInput {
	in := app.CycleInput{Ref: f.ref("id", r.ID, true), BaseVersion: r.BaseVersion, IdemKey: idem(c)}
	in.Form, in.Breed, in.InitialCount, in.AgeDays, in.Supplies, in.Note = r.Form, r.Breed, r.InitialCount, r.AgeDays, r.Supplies, r.Note
	if d := f.date("received_date", r.ReceivedDate); d != nil {
		in.ReceivedDate = *d
	}
	in.InitialWeightG = f.dec("initial_weight_g", r.InitialWeightG)
	in.VaccinationBatch = f.ref("vaccination_batch_ref", r.VaccinationBatch, false)
	return in
}

func cycleOut(v app.CycleView, names map[uuid.UUID]string) gin.H {
	supplies := v.Supplies
	if supplies == nil {
		supplies = []domain.Supply{}
	}
	counts := v.Suggest.Counts
	if counts == nil {
		counts = map[string]int{}
	}
	var current any
	if v.Suggest.CurrentHeads != nil {
		current = *v.Suggest.CurrentHeads
	}
	var last any
	if v.Suggest.LastEntry != nil {
		last = *v.Suggest.LastEntry
	}
	m := merge(gin.H{"ref": v.Ref.String(), "event": v.Event, "status": v.Status, "household_ref": v.HouseholdRef.String(),
		"site_ref": v.SiteRef.String(), "project_ref": v.ProjectRef.String(), "commune_code": v.CommuneCode, "form": v.Form,
		"breed": v.Breed, "received_date": v.ReceivedDate.Format(time.DateOnly), "initial_count": v.InitialCount,
		"initial_weight_g": optDec(v.InitialWeightG), "age_days": v.AgeDays, "supplies": supplies,
		"vaccination_batch_ref": optRef(v.VaccinationBatch), "note": optStr(v.Note), "status_reason": optStr(v.StatusReason),
		"site_name": v.SiteName, "project_code": v.ProjectCode, "owner_label": optStr(v.OwnerLabel), "can_edit": v.CanEdit,
		"accounting": gin.H{"cost_seed": optDec(v.CostSeed), "cost_feed": optDec(v.CostFeed), "cost_depreciation": optDec(v.CostDepreciation),
			"cost_transport": optDec(v.CostTransport), "cost_vet": optDec(v.CostVet), "cost_probiotic": optDec(v.CostProbiotic),
			"revenue_heads": v.RevenueHeads, "revenue_output_kg": optDec(v.OutputKg), "revenue_price": optDec(v.Price),
			"revenue_amount": optDec(v.Revenue), "total_cost": v.TotalCost.String(), "profit": optDec(v.Profit), "unit_cost": optDec(v.UnitCost)},
		"suggest": gin.H{"feed_cost": v.Suggest.FeedCost.String(), "sold_heads": v.Suggest.SoldHeads, "sold_kg": v.Suggest.SoldKg.String(),
			"sales_amount": v.Suggest.SalesAmount.String(), "current_heads": current, "last_entry_date": last, "counts": counts},
	}, meta(v.Meta, names))
	if v.Versions != nil {
		vs := make([]gin.H, 0, len(v.Versions))
		for _, x := range v.Versions {
			vs = append(vs, merge(gin.H{"event": x.Event, "status_reason": optStr(x.StatusReason)}, meta(x.Meta, names)))
		}
		m["versions"] = vs
	}
	return m
}

func (h Handler) writeCycle(c *gin.Context, v app.CycleView, err error, code int) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(status(v.Replayed, code), cycleOut(v, v.Names))
}

func (h Handler) listCycles(c *gin.Context) {
	f := newForm()
	q := app.CycleQuery{Household: queryRef(c, f, "household_ref"), Site: queryRef(c, f, "site_ref"), Project: queryRef(c, f, "project_ref"),
		Status: c.Query("status")}
	if !f.done(c) {
		return
	}
	l, err := h.Svc.ListCycles(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(l.Items))
	for _, v := range l.Items {
		items = append(items, cycleOut(v, l.Names))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "closed_count": l.ClosedCount, "avg_profit": optDec(l.AvgProfit),
		"can_create": l.CanCreateFor})
}

func (h Handler) createCycle(c *gin.Context) {
	var req cycleReq
	if !bind(c, &req) {
		return
	}
	f := newForm()
	in := req.input(c, f)
	if r := f.ref("household_ref", req.HouseholdRef, false); r != nil {
		in.HouseholdRef = *r
	} else if !f.bad {
		f.fail("household_ref", "bắt buộc")
	}
	if !f.done(c) {
		return
	}
	v, err := h.Svc.CreateCycle(c.Request.Context(), in)
	h.writeCycle(c, v, err, http.StatusCreated)
}

func (h Handler) getCycle(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrCycleNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetCycle(c.Request.Context(), ref)
	h.writeCycle(c, v, err, http.StatusOK)
}

func (h Handler) updateCycle(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrCycleNotFound)
	if !ok {
		return
	}
	var req cycleReq
	if !bind(c, &req) {
		return
	}
	req.ID, req.HouseholdRef = nil, nil
	f := newForm()
	in := req.input(c, f)
	if !f.done(c) {
		return
	}
	v, err := h.Svc.UpdateCycle(c.Request.Context(), ref, in)
	h.writeCycle(c, v, err, http.StatusOK)
}

type closeReq struct {
	BaseVersion      *int64  `json:"base_version"`
	CostSeed         *string `json:"cost_seed"`
	CostFeed         *string `json:"cost_feed"`
	CostDepreciation *string `json:"cost_depreciation"`
	CostTransport    *string `json:"cost_transport"`
	CostVet          *string `json:"cost_vet"`
	CostProbiotic    *string `json:"cost_probiotic"`
	RevenueHeads     *int64  `json:"revenue_heads"`
	RevenueOutputKg  *string `json:"revenue_output_kg"`
	RevenuePrice     *string `json:"revenue_price"`
	RevenueAmount    *string `json:"revenue_amount"`
}

func (h Handler) closeCycle(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrCycleNotFound)
	if !ok {
		return
	}
	var req closeReq
	if !bind(c, &req) {
		return
	}
	f := newForm()
	acc := domain.Accounting{CostSeed: f.dec("cost_seed", req.CostSeed), CostFeed: f.dec("cost_feed", req.CostFeed),
		CostDepreciation: f.dec("cost_depreciation", req.CostDepreciation), CostTransport: f.dec("cost_transport", req.CostTransport),
		CostVet: f.dec("cost_vet", req.CostVet), CostProbiotic: f.dec("cost_probiotic", req.CostProbiotic), RevenueHeads: req.RevenueHeads,
		OutputKg: f.dec("revenue_output_kg", req.RevenueOutputKg), Price: f.dec("revenue_price", req.RevenuePrice),
		Revenue: f.dec("revenue_amount", req.RevenueAmount)}
	if !f.done(c) {
		return
	}
	v, err := h.Svc.CloseCycle(c.Request.Context(), ref, req.BaseVersion, acc, idem(c))
	h.writeCycle(c, v, err, http.StatusOK)
}

func (h Handler) reopenCycle(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrCycleNotFound)
	if !ok {
		return
	}
	var req statusReq
	if !bind(c, &req) {
		return
	}
	v, err := h.Svc.ReopenCycle(c.Request.Context(), ref, req.BaseVersion, req.Reason, idem(c))
	h.writeCycle(c, v, err, http.StatusOK)
}

// ---------------------------------------------------------------------------
// Lần ghi
// ---------------------------------------------------------------------------

type locationReq struct {
	Lat          float64 `json:"lat"`
	Lng          float64 `json:"lng"`
	AccuracyM    *string `json:"accuracy_m"`
	MockLocation bool    `json:"mock_location"`
}

type entryReq struct {
	ID          *string           `json:"id"`
	CycleRef    *string           `json:"cycle_ref"`
	BaseVersion *int64            `json:"base_version"`
	Submit      bool              `json:"submit"`
	CapturedAt  *string           `json:"captured_at"`
	EntryDate   *string           `json:"entry_date"`
	EntryKind   string            `json:"entry_kind"`
	Metrics     map[string]string `json:"metrics"`
	Tables      domain.Tables     `json:"tables"`
	PhotoRefs   []string          `json:"photo_refs"`
	Location    *locationReq      `json:"location"`
	Reason      string            `json:"reason"`
}

func (r entryReq) input(c *gin.Context, f *form) app.EntryInput {
	in := app.EntryInput{Ref: f.ref("id", r.ID, true), BaseVersion: r.BaseVersion, Submit: r.Submit, Kind: r.EntryKind,
		Metrics: r.Metrics, Tables: r.Tables, Reason: r.Reason, IdemKey: idem(c)}
	in.CapturedAt = f.instant("captured_at", r.CapturedAt)
	in.EntryDate = f.date("entry_date", r.EntryDate)
	in.Photos = f.refs("photo_refs", r.PhotoRefs)
	if r.Location != nil {
		in.Fix = &domain.Fix{Lat: r.Location.Lat, Lng: r.Location.Lng, AccuracyM: f.dec("location.accuracy_m", r.Location.AccuracyM),
			Mock: r.Location.MockLocation}
	}
	if in.Metrics == nil {
		in.Metrics = map[string]string{}
	}
	return in
}

func checksOut(cs []domain.Check) []domain.Check {
	if cs == nil {
		return []domain.Check{}
	}
	return cs
}

func metricsOut(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func entryCore(e store.Entry, names map[uuid.UUID]string) gin.H {
	l := e.Location
	return merge(gin.H{"ref": e.Ref.String(), "event": e.Event, "status": e.Status, "cycle_ref": e.CycleRef.String(),
		"household_ref": e.HouseholdRef.String(), "site_ref": e.SiteRef.String(), "project_ref": e.ProjectRef.String(),
		"commune_code": e.CommuneCode, "form": e.Form, "entry_kind": e.Kind, "entry_date": e.EntryDate.Format(time.DateOnly),
		"captured_at": e.CapturedAt.UTC().Format(time.RFC3339), "metrics": metricsOut(e.Metrics), "tables": e.Tables,
		"photo_refs": refsOut(e.Photos), "checks": checksOut(e.Checks),
		"location": gin.H{"has_fix": l.HasFix, "accuracy_m": optDec(l.AccuracyM), "distance_m": optDec(l.DistanceM),
			"no_target_point": l.NoTargetPoint, "far": l.Far, "weak_accuracy": l.WeakAccuracy, "mock_location": l.MockDevice,
			"mock_suspected": l.MockSuspected, "fix_stored": e.FixStored},
		"author_ref": e.AuthorRef.String(), "author_display": nameOf(names, &e.AuthorRef), "reviewer_ref": optRef(e.ReviewerRef),
		"reviewer_display": nameOf(names, e.ReviewerRef), "status_reason": optStr(e.StatusReason)}, meta(e.Meta, names))
}

func entryOut(v app.EntryView, names map[uuid.UUID]string) gin.H {
	m := entryCore(v.Entry, names)
	m["site_name"], m["project_code"], m["breed"], m["owner_label"] = v.SiteName, v.ProjectCode, optStr(v.Breed), optStr(v.OwnerLabel)
	m["editable_until"] = v.EditableUntil.Format(time.DateOnly)
	m["can_edit"], m["can_submit"], m["can_review"], m["can_correct"] = v.CanEdit, v.CanSubmit, v.CanReview, v.CanCorrect
	if v.Versions == nil {
		return m
	}
	vs := make([]gin.H, 0, len(v.Versions))
	for _, x := range v.Versions {
		vs = append(vs, merge(gin.H{"event": x.Event, "status": x.Status, "status_reason": optStr(x.StatusReason),
			"metrics": metricsOut(x.Metrics), "checks": checksOut(x.Checks)}, meta(x.Meta, names)))
	}
	m["versions"] = vs
	m["fix"] = nil
	if v.Fix != nil {
		m["fix"] = gin.H{"lat": v.Fix.Lat, "lng": v.Fix.Lng, "accuracy_m": optDec(v.Fix.AccuracyM),
			"captured_at": v.Fix.CapturedAt.UTC().Format(time.RFC3339)}
	}
	m["target"] = nil
	if v.Target != nil {
		m["target"] = gin.H{"lat": v.Target.Lat, "lng": v.Target.Lng}
	}
	ps := make([]gin.H, 0, len(v.PhotoMeta))
	for _, p := range v.PhotoMeta {
		ps = append(ps, photoOut(p))
	}
	m["photos"] = ps
	m["previous"] = nil
	if v.Previous != nil {
		m["previous"] = gin.H{"ref": v.Previous.Ref.String(), "entry_date": v.Previous.EntryDate.Format(time.DateOnly),
			"metrics": metricsOut(v.Previous.Metrics)}
	}
	ws := make([]gin.H, 0, len(v.Weights))
	for _, w := range v.Weights {
		ws = append(ws, gin.H{"date": w.Date.Format(time.DateOnly), "weight_avg_g": w.Weight.String()})
	}
	m["weights"] = ws
	return m
}

func (h Handler) writeEntry(c *gin.Context, v app.EntryView, err error, code int) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(status(v.Replayed, code), entryOut(v, v.Names))
}

func (h Handler) listEntries(c *gin.Context) {
	f := newForm()
	q := app.EntryQuery{Cycle: queryRef(c, f, "cycle_ref"), Household: queryRef(c, f, "household_ref"), Project: queryRef(c, f, "project_ref"),
		Status: c.Query("status"), Review: c.Query("review") == "true", Flagged: c.Query("flagged") == "true"}
	if !f.done(c) {
		return
	}
	l, err := h.Svc.ListEntries(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(l.Items))
	for _, v := range l.Items {
		items = append(items, entryOut(v, l.Names))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) createEntry(c *gin.Context) {
	var req entryReq
	if !bind(c, &req) {
		return
	}
	f := newForm()
	in := req.input(c, f)
	if r := f.ref("cycle_ref", req.CycleRef, false); r != nil {
		in.CycleRef = *r
	} else if !f.bad {
		f.fail("cycle_ref", "bắt buộc")
	}
	if !f.done(c) {
		return
	}
	v, err := h.Svc.CreateEntry(c.Request.Context(), in)
	h.writeEntry(c, v, err, http.StatusCreated)
}

func (h Handler) getEntry(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrEntryNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetEntry(c.Request.Context(), ref)
	h.writeEntry(c, v, err, http.StatusOK)
}

// entryBody đọc thân sửa/điều chỉnh (không có mã, lứa, thời điểm, vị trí — chỉ có ở lần tạo).
func entryBody(c *gin.Context) (app.EntryInput, bool) {
	var req entryReq
	if !bind(c, &req) {
		return app.EntryInput{}, false
	}
	f := newForm()
	if req.ID != nil || req.CycleRef != nil || req.CapturedAt != nil || req.Location != nil {
		f.fail("location", "mã, lứa, thời điểm và vị trí lúc ghi không sửa được")
	}
	in := req.input(c, f)
	return in, f.done(c)
}

func (h Handler) updateEntry(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrEntryNotFound)
	if !ok {
		return
	}
	in, ok := entryBody(c)
	if !ok {
		return
	}
	v, err := h.Svc.UpdateEntry(c.Request.Context(), ref, in)
	h.writeEntry(c, v, err, http.StatusOK)
}

func (h Handler) correctEntry(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrEntryNotFound)
	if !ok {
		return
	}
	in, ok := entryBody(c)
	if !ok {
		return
	}
	v, err := h.Svc.CorrectEntry(c.Request.Context(), ref, in)
	h.writeEntry(c, v, err, http.StatusOK)
}

func (h Handler) returnEntry(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrEntryNotFound)
	if !ok {
		return
	}
	var req statusReq
	if !bind(c, &req) {
		return
	}
	v, err := h.Svc.ReturnEntry(c.Request.Context(), ref, req.BaseVersion, req.Reason, idem(c))
	h.writeEntry(c, v, err, http.StatusOK)
}

type baseReq struct {
	BaseVersion *int64 `json:"base_version"`
}

// simple là thao tác chỉ cần base_version (gửi, xác thực).
func (h Handler) simple(fn func(context.Context, uuid.UUID, *int64, string) (app.EntryView, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		ref, ok := pathRef(c, app.ErrEntryNotFound)
		if !ok {
			return
		}
		var req baseReq
		if !bind(c, &req) {
			return
		}
		v, err := fn(c.Request.Context(), ref, req.BaseVersion, idem(c))
		h.writeEntry(c, v, err, http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// Ảnh
// ---------------------------------------------------------------------------

func photoOut(p store.Photo) gin.H {
	var captured any
	if p.CapturedAt != nil {
		captured = p.CapturedAt.UTC().Format(time.RFC3339)
	}
	return gin.H{"file_ref": p.FileRef.String(), "household_ref": p.HouseholdRef.String(), "kind": p.Kind, "content_type": p.ContentType,
		"size_bytes": p.Size, "captured_at": captured, "uploaded_by": p.UploadedBy.String(), "uploaded_at": p.UploadedAt.UTC().Format(time.RFC3339)}
}

func (h Handler) uploadPhoto(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, domain.MaxPhotoBytes+64<<10)
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		httpx.Fail(c, app.ErrPhotoInvalid.WithField("file", "thiếu tệp ảnh hoặc quá 500 KB"))
		return
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(io.LimitReader(file, domain.MaxPhotoBytes+1))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Fail(c, app.ErrPhotoInvalid.WithField("file", "quá 500 KB"))
			return
		}
		httpx.Fail(c, err)
		return
	}
	f := newForm()
	hh, id, captured := c.Request.FormValue("household_ref"), c.Request.FormValue("id"), c.Request.FormValue("captured_at")
	in := app.PhotoInput{Ref: f.ref("id", &id, true), Kind: c.Request.FormValue("kind"), CapturedAt: f.instant("captured_at", &captured),
		Content: content}
	if r := f.ref("household_ref", &hh, false); r != nil {
		in.HouseholdRef = *r
	} else if !f.bad {
		f.fail("household_ref", "bắt buộc")
	}
	if !f.done(c) {
		return
	}
	p, err := h.Svc.UploadPhoto(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, photoOut(p))
}

func (h Handler) photoContent(c *gin.Context) {
	ref, ok := pathRef(c, app.ErrPhotoNotFound)
	if !ok {
		return
	}
	ct, body, err := h.Svc.OpenPhoto(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, ct, body)
}
