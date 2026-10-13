// Package handler: route /v1/extension/cnts/outbreaks…, /v1/extension/cnts/suspects… (W11.49 — Báo cáo dịch bệnh BM-10,
// báo nghi dịch). Mọi route cần header X-AG-Context (ngữ cảnh phiên KN); thao tác ghi cần Idempotency-Key.
package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/outbreak/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/outbreak/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/cnts")
	g.GET("/outbreaks", h.listOutbreaks)
	g.POST("/outbreaks", h.reportOutbreak)
	g.GET("/outbreak-points", h.points)
	g.GET("/outbreaks/:ref", h.getOutbreak)
	g.POST("/outbreaks/:ref/progress", h.updateOutbreak)
	g.POST("/outbreaks/:ref/close", h.closeOutbreak)
	g.POST("/outbreaks/:ref/reopen", h.reopenOutbreak)
	g.GET("/suspects", h.listSuspects)
	g.POST("/suspects", h.reportSuspect)
	g.GET("/suspects/:ref", h.getSuspect)
	g.POST("/suspects/:ref/confirm", h.confirmSuspect)
	g.POST("/suspects/:ref/dismiss", h.dismissSuspect)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func optTS(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ts(*t)
}

func optDay(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.DateOnly)
}

func optStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func optDec(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.String()
}

func optRef(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return u.String()
}

func refsOut(rs []uuid.UUID) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.String())
	}
	return out
}

func bind(c *gin.Context, v any) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func created(c *gin.Context, replayed bool, body any) {
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, body)
}

func actor(a app.Actor) gin.H { return gin.H{"ref": a.Ref.String(), "display": a.Display} }

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func pathRef(c *gin.Context) (uuid.UUID, bool) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("ref", "phải là UUID"))
		return uuid.Nil, false
	}
	return ref, true
}

func parseDay(field, s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, problem.Validation.WithField(field, "ngày dạng YYYY-MM-DD")
	}
	return &t, nil
}

func parseTS(field, s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, problem.Validation.WithField(field, "thời điểm RFC 3339")
	}
	return &t, nil
}

func locDTO(p *domain.Point, acc *decimal.Decimal, source string) any {
	if p == nil {
		return nil
	}
	return gin.H{"lat": p.Lat.String(), "lon": p.Lon.String(), "accuracy_m": optDec(acc), "source": optStr(source)}
}

// ---------------------------------------------------------------------------
// Ổ dịch
// ---------------------------------------------------------------------------

func obDTO(v app.OutbreakView) gin.H {
	return gin.H{
		"ref": v.Ref.String(), "version": v.Seq, "version_id": v.ID.String(), "event": v.Event, "status": v.Status,
		"commune_code": v.CommuneCode, "disease_code": v.DiseaseCode, "disease_name": v.DiseaseName, "species_code": v.SpeciesCode,
		"species_name": v.SpeciesName, "received_at": ts(v.ReceivedAt), "source": v.Source, "suspect_ref": optRef(v.SuspectRef),
		"household_ref": optRef(v.HouseholdRef), "locality": optStr(v.Locality),
		"location":  locDTO(v.Location, v.GPSAccuracyM, v.LocationSource),
		"herd_size": v.HerdSize, "herd_age_days": v.HerdAgeDays, "dead": v.Dead, "symptoms": v.Symptoms,
		"sample_date": optDay(v.SampleDate), "lab_name": optStr(v.LabName), "lab_result": optStr(v.LabResult),
		"culled_head": v.CulledHead, "culled_kg": optDec(v.CulledKg), "chemical_liters": optDec(v.ChemicalLiters),
		"buffer_note": optStr(v.BufferNote), "prevention_note": optStr(v.PreventionNote), "attachment_refs": refsOut(v.AttachmentRefs),
		"as_of": v.AsOf.Format(time.DateOnly), "progress_note": optStr(v.ProgressNote), "status_reason": v.StatusReason,
		"captured_at": optTS(v.CapturedAt), "recorded_at": ts(v.RecordedAt), "actor": actor(v.Actor),
	}
}

func counts(cs []app.Count) []gin.H {
	out := make([]gin.H, 0, len(cs))
	for _, c := range cs {
		out = append(out, gin.H{"key": c.Key, "label": c.Label, "count": c.N})
	}
	return out
}

func query(c *gin.Context) (app.OutbreakQuery, error) {
	q := app.OutbreakQuery{Status: c.Query("status"), Commune: c.Query("commune"), DiseaseCode: c.Query("disease")}
	var err error
	if q.From, err = parseDay("from", c.Query("from")); err != nil {
		return q, err
	}
	if q.To, err = parseDay("to", c.Query("to")); err != nil {
		return q, err
	}
	return q, nil
}

func (h Handler) listOutbreaks(c *gin.Context) {
	q, err := query(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	res, err := h.Svc.ListOutbreaks(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(res.Items))
	for _, v := range res.Items {
		items = append(items, obDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "by_status": counts(res.ByStatus), "by_disease": counts(res.ByDisease)})
}

// points trả GeoJSON FeatureCollection (WGS84, [kinh độ, vĩ độ]) cho lớp ổ dịch của bản đồ (FN-18 — W11.52).
func (h Handler) points(c *gin.Context) {
	q, err := query(c)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	vs, err := h.Svc.OutbreakPoints(c.Request.Context(), q, c.Query("include_closed") == "true")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	features := make([]gin.H, 0, len(vs))
	for _, v := range vs {
		lon, _ := v.Location.Lon.Float64() // GeoJSON là số; tọa độ không phải số liệu tiền/khối lượng
		lat, _ := v.Location.Lat.Float64()
		features = append(features, gin.H{
			"type": "Feature", "id": v.Ref.String(),
			"geometry": gin.H{"type": "Point", "coordinates": []float64{lon, lat}},
			"properties": gin.H{"ref": v.Ref.String(), "status": v.Status, "version": v.Seq, "commune_code": v.CommuneCode,
				"disease_code": v.DiseaseCode, "disease_name": v.DiseaseName, "species_code": v.SpeciesCode, "species_name": v.SpeciesName,
				"received_at": ts(v.ReceivedAt), "as_of": v.AsOf.Format(time.DateOnly), "herd_size": v.HerdSize, "dead": v.Dead,
				"accuracy_m": optDec(v.GPSAccuracyM)},
		})
	}
	c.Header("Content-Type", "application/geo+json")
	c.JSON(http.StatusOK, gin.H{"type": "FeatureCollection", "features": features})
}

func (h Handler) getOutbreak(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	vs, err := h.Svc.GetOutbreak(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	versions := make([]gin.H, 0, len(vs))
	for _, v := range vs {
		versions = append(versions, obDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"current": versions[0], "versions": versions})
}

type locationReq struct {
	Lat       decimal.Decimal  `json:"lat"`
	Lon       decimal.Decimal  `json:"lon"`
	AccuracyM *decimal.Decimal `json:"accuracy_m"`
	Source    string           `json:"source"`
}

type outbreakReq struct {
	BaseVersion    *int64           `json:"base_version"`
	CommuneCode    string           `json:"commune_code"`
	DiseaseCode    string           `json:"disease_code"`
	SpeciesCode    string           `json:"species_code"`
	ReceivedAt     string           `json:"received_at"`
	Source         string           `json:"source"`
	HouseholdRef   *uuid.UUID       `json:"household_ref"`
	Locality       string           `json:"locality"`
	Location       *locationReq     `json:"location"`
	HerdSize       int64            `json:"herd_size"`
	HerdAgeDays    *int64           `json:"herd_age_days"`
	Dead           int64            `json:"dead"`
	Symptoms       string           `json:"symptoms"`
	SampleDate     string           `json:"sample_date"`
	LabName        string           `json:"lab_name"`
	LabResult      string           `json:"lab_result"`
	CulledHead     *int64           `json:"culled_head"`
	CulledKg       *decimal.Decimal `json:"culled_kg"`
	ChemicalLiters *decimal.Decimal `json:"chemical_liters"`
	BufferNote     string           `json:"buffer_note"`
	PreventionNote string           `json:"prevention_note"`
	AttachmentRefs []uuid.UUID      `json:"attachment_refs"`
	AsOf           string           `json:"as_of"`
	ProgressNote   string           `json:"progress_note"`
	CapturedAt     string           `json:"captured_at"`
}

func (r outbreakReq) outbreak() (domain.Outbreak, error) {
	o := domain.Outbreak{CommuneCode: r.CommuneCode, DiseaseCode: r.DiseaseCode, SpeciesCode: r.SpeciesCode, Source: r.Source,
		HouseholdRef: r.HouseholdRef, Locality: r.Locality, HerdSize: r.HerdSize, HerdAgeDays: r.HerdAgeDays, Dead: r.Dead,
		Symptoms: r.Symptoms, LabName: r.LabName, LabResult: r.LabResult, CulledHead: r.CulledHead, CulledKg: r.CulledKg,
		ChemicalLiters: r.ChemicalLiters, BufferNote: r.BufferNote, PreventionNote: r.PreventionNote,
		AttachmentRefs: r.AttachmentRefs, ProgressNote: r.ProgressNote}
	if r.Location != nil {
		o.Location = &domain.Point{Lat: r.Location.Lat, Lon: r.Location.Lon}
		o.GPSAccuracyM, o.LocationSource = r.Location.AccuracyM, r.Location.Source
	}
	recv, err := parseTS("received_at", r.ReceivedAt)
	if err != nil {
		return o, err
	}
	if recv != nil {
		o.ReceivedAt = *recv
	}
	if o.SampleDate, err = parseDay("sample_date", r.SampleDate); err != nil {
		return o, err
	}
	asOf, err := parseDay("as_of", r.AsOf)
	if err != nil {
		return o, err
	}
	if asOf != nil {
		o.AsOf = *asOf
	}
	o.CapturedAt, err = parseTS("captured_at", r.CapturedAt)
	return o, err
}

func (h Handler) reportOutbreak(c *gin.Context) {
	var req outbreakReq
	if !bind(c, &req) {
		return
	}
	o, err := req.outbreak()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	v, replayed, err := h.Svc.ReportOutbreak(c.Request.Context(), app.OutbreakInput{Outbreak: o, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, obDTO(v))
}

func (h Handler) updateOutbreak(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req outbreakReq
	if !bind(c, &req) {
		return
	}
	o, err := req.outbreak()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	v, replayed, err := h.Svc.UpdateOutbreak(c.Request.Context(), ref, app.OutbreakInput{Outbreak: o, BaseVersion: req.BaseVersion, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, obDTO(v))
}

type statusReq struct {
	BaseVersion *int64 `json:"base_version"`
	Reason      string `json:"reason"`
}

func (h Handler) setStatus(c *gin.Context, fn func(*gin.Context, uuid.UUID, app.StatusInput) (app.OutbreakView, bool, error)) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req statusReq
	if !bind(c, &req) {
		return
	}
	v, replayed, err := fn(c, ref, app.StatusInput{BaseVersion: req.BaseVersion, Reason: req.Reason, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, obDTO(v))
}

func (h Handler) closeOutbreak(c *gin.Context) {
	h.setStatus(c, func(c *gin.Context, ref uuid.UUID, in app.StatusInput) (app.OutbreakView, bool, error) {
		return h.Svc.CloseOutbreak(c.Request.Context(), ref, in)
	})
}

func (h Handler) reopenOutbreak(c *gin.Context) {
	h.setStatus(c, func(c *gin.Context, ref uuid.UUID, in app.StatusInput) (app.OutbreakView, bool, error) {
		return h.Svc.ReopenOutbreak(c.Request.Context(), ref, in)
	})
}

// ---------------------------------------------------------------------------
// Báo nghi dịch
// ---------------------------------------------------------------------------

func susDTO(v app.SuspectView) gin.H {
	return gin.H{
		"ref": v.Ref.String(), "version": v.Seq, "version_id": v.ID.String(), "status": v.Event, "commune_code": v.CommuneCode,
		"reporter_kind": v.ReporterKind, "observed_at": ts(v.ObservedAt), "species_code": optStr(v.SpeciesCode),
		"disease_code": optStr(v.DiseaseCode), "household_ref": optRef(v.HouseholdRef), "locality": optStr(v.Locality),
		"location": locDTO(v.Location, v.GPSAccuracyM, ""), "affected": v.Affected, "dead": v.Dead, "description": v.Description,
		"attachment_refs": refsOut(v.AttachmentRefs), "outbreak_ref": optRef(v.OutbreakRef), "status_reason": v.StatusReason,
		"captured_at": optTS(v.CapturedAt), "recorded_at": ts(v.RecordedAt), "actor": actor(v.Actor),
	}
}

func (h Handler) listSuspects(c *gin.Context) {
	vs, err := h.Svc.ListSuspects(c.Request.Context(), c.Query("status"), c.Query("commune"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(vs))
	for _, v := range vs {
		items = append(items, susDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) getSuspect(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	vs, err := h.Svc.GetSuspect(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	versions := make([]gin.H, 0, len(vs))
	for _, v := range vs {
		versions = append(versions, susDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"current": versions[0], "versions": versions})
}

type suspectReq struct {
	CommuneCode    string       `json:"commune_code"`
	ReporterKind   string       `json:"reporter_kind"`
	ObservedAt     string       `json:"observed_at"`
	SpeciesCode    string       `json:"species_code"`
	DiseaseCode    string       `json:"disease_code"`
	HouseholdRef   *uuid.UUID   `json:"household_ref"`
	Locality       string       `json:"locality"`
	Location       *locationReq `json:"location"`
	Affected       *int64       `json:"affected"`
	Dead           *int64       `json:"dead"`
	Description    string       `json:"description"`
	AttachmentRefs []uuid.UUID  `json:"attachment_refs"`
	CapturedAt     string       `json:"captured_at"`
}

func (h Handler) reportSuspect(c *gin.Context) {
	var req suspectReq
	if !bind(c, &req) {
		return
	}
	s := domain.Suspect{CommuneCode: req.CommuneCode, ReporterKind: req.ReporterKind, SpeciesCode: req.SpeciesCode,
		DiseaseCode: req.DiseaseCode, HouseholdRef: req.HouseholdRef, Locality: req.Locality, Affected: req.Affected, Dead: req.Dead,
		Description: req.Description, AttachmentRefs: req.AttachmentRefs}
	if req.Location != nil {
		s.Location = &domain.Point{Lat: req.Location.Lat, Lon: req.Location.Lon}
		s.GPSAccuracyM = req.Location.AccuracyM
	}
	obs, err := parseTS("observed_at", req.ObservedAt)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if obs != nil {
		s.ObservedAt = *obs
	}
	if s.CapturedAt, err = parseTS("captured_at", req.CapturedAt); err != nil {
		httpx.Fail(c, err)
		return
	}
	v, replayed, err := h.Svc.ReportSuspect(c.Request.Context(), app.SuspectInput{Suspect: s, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, susDTO(v))
}

func (h Handler) confirmSuspect(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req outbreakReq
	if !bind(c, &req) {
		return
	}
	o, err := req.outbreak()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	v, replayed, err := h.Svc.ConfirmSuspect(c.Request.Context(), ref, app.ConfirmInput{BaseVersion: req.BaseVersion, Outbreak: o, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, obDTO(v))
}

func (h Handler) dismissSuspect(c *gin.Context) {
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req statusReq
	if !bind(c, &req) {
		return
	}
	v, replayed, err := h.Svc.DismissSuspect(c.Request.Context(), ref, app.StatusInput{BaseVersion: req.BaseVersion, Reason: req.Reason, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, susDTO(v))
}
