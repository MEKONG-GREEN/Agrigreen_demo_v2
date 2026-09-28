// Package handler: HTTP của pf/mechanization — Application API (đọc, Cục KTHT và
// Admin) và Admin API (ghi) — contracts/openapi/application.yaml, admin.yaml,
// phần "# --- pf/mechanization ---".
package handler

import (
	"encoding/base64"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func refPtr(u uuid.UUID) *string {
	if u == uuid.Nil {
		return nil
	}
	s := u.String()
	return &s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func dateStr(t time.Time) string { return t.Format(time.DateOnly) }

func datePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := dateStr(*t)
	return &s
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// ---------------------------------------------------------------------------
// Chủ sở hữu
// ---------------------------------------------------------------------------

type ownerDTO struct {
	ID               string  `json:"id"`
	Code             string  `json:"code"`
	OwnerType        string  `json:"owner_type"`
	OwnerTypeLabel   string  `json:"owner_type_label"`
	CooperativeRef   *string `json:"cooperative_ref"`
	PersonRef        *string `json:"person_ref"`
	Active           bool    `json:"active"`
	StatusReason     *string `json:"status_reason"`
	Label            string  `json:"label" pii:"true"`
	MaskedPhone      string  `json:"masked_phone" pii:"true"`
	LabelUnavailable bool    `json:"label_unavailable"`
	Machines         int     `json:"machines"`
}

func toOwnerDTO(v app.OwnerView) ownerDTO {
	o := v.Owner
	return ownerDTO{
		ID: o.ID.String(), Code: o.Code, OwnerType: string(o.Type), OwnerTypeLabel: domain.OwnerTypeLabels[o.Type],
		CooperativeRef: refPtr(o.CooperativeRef), PersonRef: refPtr(o.PersonRef), Active: o.Active, StatusReason: strPtr(o.StatusReason),
		Label: v.Label, MaskedPhone: v.MaskedPhone, LabelUnavailable: v.LabelUnavailable, Machines: v.Machines,
	}
}

type ownerEventDTO struct {
	Seq              int64   `json:"seq"`
	Action           string  `json:"action"`
	EventTime        string  `json:"event_time"`
	RecordedAt       string  `json:"recorded_at"`
	ActorRef         string  `json:"actor_ref"`
	Reason           *string `json:"reason"`
	Active           bool    `json:"active"`
	CooperativeRef   *string `json:"cooperative_ref"`
	MachinesAffected int     `json:"machines_affected"`
}

func toOwnerEventDTO(e domain.OwnerEvent) ownerEventDTO {
	return ownerEventDTO{
		Seq: e.Seq, Action: string(e.Action), EventTime: ts(e.EventTime), RecordedAt: ts(e.Recorded), ActorRef: e.ActorRef.String(),
		Reason: strPtr(e.Reason), Active: e.Owner.Active, CooperativeRef: refPtr(e.Owner.CooperativeRef), MachinesAffected: e.MachinesAffected,
	}
}

// ---------------------------------------------------------------------------
// Máy
// ---------------------------------------------------------------------------

type machineDTO struct {
	Ref            string           `json:"ref"`
	Code           string           `json:"code"`
	MachineType    string           `json:"machine_type"`
	FarmStage      string           `json:"farm_stage"`
	CooperativeRef string           `json:"cooperative_ref"`
	OwnerID        string           `json:"owner_id"`
	Brand          *string          `json:"brand"`
	Model          *string          `json:"model"`
	SerialNo       *string          `json:"serial_no"`
	ChassisNo      *string          `json:"chassis_no"`
	YearMade       *int             `json:"year_made"`
	Fuel           *string          `json:"fuel"`
	PowerHP        *decimal.Decimal `json:"power_hp"`
	CapacityHa     *decimal.Decimal `json:"capacity_ha"`
	OwnedSince     string           `json:"owned_since"`
	Status         string           `json:"status"`
	StatusLabel    string           `json:"status_label"`
	StatusSource   string           `json:"status_source"`
	SourceLabel    string           `json:"source_label"`
	StatusAt       string           `json:"status_at"`
	Active         bool             `json:"active"`
	DeactivatedOn  *string          `json:"deactivated_on"`
	StatusReason   *string          `json:"status_reason"`
}

func toMachineDTO(m domain.Machine) machineDTO {
	return machineDTO{
		Ref: m.Ref.String(), Code: m.Code, MachineType: m.MachineType, FarmStage: m.FarmStage, CooperativeRef: m.CooperativeRef.String(),
		OwnerID: m.OwnerID.String(), Brand: strPtr(m.Brand), Model: strPtr(m.Model), SerialNo: strPtr(m.SerialNo),
		ChassisNo: strPtr(m.ChassisNo), YearMade: m.YearMade, Fuel: strPtr(m.Fuel), PowerHP: m.PowerHP, CapacityHa: m.CapacityHa,
		OwnedSince: dateStr(m.OwnedSince), Status: string(m.Status), StatusLabel: domain.StatusLabels[m.Status],
		StatusSource: m.StatusSource, SourceLabel: domain.SourceLabels[m.StatusSource], StatusAt: ts(m.StatusAt), Active: m.Active,
		DeactivatedOn: datePtr(m.DeactivatedOn), StatusReason: strPtr(m.StatusReason),
	}
}

type machineEventDTO struct {
	Seq        int64      `json:"seq"`
	Action     string     `json:"action"`
	Source     string     `json:"source"`
	EventTime  string     `json:"event_time"`
	RecordedAt string     `json:"recorded_at"`
	ActorRef   string     `json:"actor_ref"`
	Reason     *string    `json:"reason"`
	Machine    machineDTO `json:"machine"`
}

func toMachineEventDTO(e domain.MachineEvent) machineEventDTO {
	m := e.Machine
	m.StatusSource, m.StatusAt = e.Source, e.EventTime
	return machineEventDTO{
		Seq: e.Seq, Action: string(e.Action), Source: e.Source, EventTime: ts(e.EventTime), RecordedAt: ts(e.Recorded),
		ActorRef: e.ActorRef.String(), Reason: strPtr(e.Reason), Machine: toMachineDTO(m),
	}
}

// ---------------------------------------------------------------------------
// Diện tích vụ
// ---------------------------------------------------------------------------

type calendarDTO struct {
	Stage string `json:"stage"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type areaDTO struct {
	ID             string          `json:"id"`
	CooperativeRef string          `json:"cooperative_ref"`
	SeasonCode     string          `json:"season_code"`
	SeasonYear     int             `json:"season_year"`
	CommuneCode    *string         `json:"commune_code"`
	AreaHa         decimal.Decimal `json:"area_ha"`
	Source         string          `json:"source"`
	SourceLabel    string          `json:"source_label"`
	SourceAt       string          `json:"source_at"`
	Locked         bool            `json:"locked"`
	Calendar       []calendarDTO   `json:"calendar"`
}

var areaSourceLabels = map[string]string{domain.AreaFromAppHTX: "App HTX", domain.AreaManual: "Nhập tay"}

func toAreaDTO(a domain.SeasonArea) areaDTO {
	d := areaDTO{
		ID: a.ID.String(), CooperativeRef: a.CooperativeRef.String(), SeasonCode: a.Season.Code, SeasonYear: a.Season.Year,
		CommuneCode: strPtr(a.CommuneCode), AreaHa: a.AreaHa, Source: a.Source, SourceLabel: areaSourceLabels[a.Source],
		SourceAt: ts(a.SourceAt), Locked: a.Locked, Calendar: []calendarDTO{},
	}
	for _, e := range a.Calendar {
		d.Calendar = append(d.Calendar, calendarDTO{Stage: e.Stage, Start: dateStr(e.Start), End: dateStr(e.End)})
	}
	return d
}

type areaEventDTO struct {
	Seq        int64   `json:"seq"`
	Action     string  `json:"action"`
	EventTime  string  `json:"event_time"`
	RecordedAt string  `json:"recorded_at"`
	ActorRef   string  `json:"actor_ref"`
	Reason     *string `json:"reason"`
	Area       areaDTO `json:"area"`
}

func toAreaEventDTO(e domain.AreaEvent) areaEventDTO {
	a := e.Area
	a.SourceAt = e.EventTime
	return areaEventDTO{
		Seq: e.Seq, Action: string(e.Action), EventTime: ts(e.EventTime), RecordedAt: ts(e.Recorded), ActorRef: e.ActorRef.String(),
		Reason: strPtr(e.Reason), Area: toAreaDTO(a),
	}
}

// ---------------------------------------------------------------------------
// Cân đối
// ---------------------------------------------------------------------------

type paramDTO struct {
	Key       string          `json:"key"`
	Value     decimal.Decimal `json:"value"`
	Version   int             `json:"version"`
	SourceDoc string          `json:"source_doc"`
}

func toParamDTO(p *domain.ParamRef) *paramDTO {
	if p == nil {
		return nil
	}
	return &paramDTO{Key: p.Key, Value: p.Value, Version: p.Version, SourceDoc: p.SourceDoc}
}

type thresholdsDTO struct {
	Sufficient *paramDTO `json:"sufficient"`
	Attention  *paramDTO `json:"attention"`
	Surplus    *paramDTO `json:"surplus"`
}

type runDTO struct {
	ID            string         `json:"id"`
	SeasonCode    string         `json:"season_code"`
	SeasonYear    int            `json:"season_year"`
	SeasonKey     string         `json:"season_key"`
	ReferenceDate string         `json:"reference_date"`
	RuleVersion   string         `json:"rule_version"`
	PilotStages   []string       `json:"pilot_stages"`
	Thresholds    thresholdsDTO  `json:"thresholds"`
	ResultCount   int            `json:"result_count"`
	StatusCounts  map[string]int `json:"status_counts"`
	MissingNorms  []string       `json:"missing_norms"`
	ComputedAt    string         `json:"computed_at"`
	Seq           int64          `json:"seq"`
}

func toRunDTO(r domain.BalanceRun) runDTO {
	d := runDTO{
		ID: r.ID.String(), SeasonCode: r.Season.Code, SeasonYear: r.Season.Year, SeasonKey: r.Season.Key(),
		ReferenceDate: dateStr(r.ReferenceDate), RuleVersion: r.RuleVersion, PilotStages: r.PilotStages,
		Thresholds: thresholdsDTO{
			Sufficient: toParamDTO(r.Thresholds.Sufficient), Attention: toParamDTO(r.Thresholds.Attention), Surplus: toParamDTO(r.Thresholds.Surplus),
		},
		ResultCount: r.ResultCount, StatusCounts: r.StatusCounts, MissingNorms: r.MissingNorms, ComputedAt: ts(r.EventTime), Seq: r.Seq,
	}
	if d.PilotStages == nil {
		d.PilotStages = []string{}
	}
	if d.MissingNorms == nil {
		d.MissingNorms = []string{}
	}
	return d
}

type resultDTO struct {
	CooperativeRef  string           `json:"cooperative_ref"`
	CommuneCode     *string          `json:"commune_code"`
	ProvinceCode    *string          `json:"province_code"`
	FarmStage       string           `json:"farm_stage"`
	MachineType     string           `json:"machine_type"`
	AreaHa          *decimal.Decimal `json:"area_ha"`
	AreaSource      *string          `json:"area_source"`
	AreaSourceLabel string           `json:"area_source_label"`
	AreaLocked      bool             `json:"area_locked"`
	NormKey         string           `json:"norm_key"`
	Norm            *paramDTO        `json:"norm"`
	Required        *decimal.Decimal `json:"required"`
	RequiredWhole   *int             `json:"required_whole"`
	MachinesTotal   int              `json:"machines_total"`
	Capacity        int              `json:"capacity"`
	CoveragePct     *decimal.Decimal `json:"coverage_pct"`
	Gap             *int             `json:"gap"`
	Status          string           `json:"status"`
	StatusLabel     string           `json:"status_label"`
	Color           string           `json:"color"`
	Seq             int64            `json:"seq"`
}

func toResultDTO(r domain.BalanceResult) resultDTO {
	f := r.Figures
	label := areaSourceLabels[r.AreaSource]
	if r.AreaHa == nil {
		label = "Chưa có dữ liệu"
	}
	return resultDTO{
		CooperativeRef: r.CooperativeRef.String(), CommuneCode: strPtr(r.CommuneCode), ProvinceCode: strPtr(r.ProvinceCode),
		FarmStage: r.FarmStage, MachineType: r.MachineType, AreaHa: r.AreaHa, AreaSource: strPtr(r.AreaSource), AreaSourceLabel: label,
		AreaLocked: r.AreaLocked, NormKey: r.NormKey, Norm: toParamDTO(r.Norm), Required: f.Required, RequiredWhole: f.RequiredWhole,
		MachinesTotal: r.MachinesTotal, Capacity: r.Capacity, CoveragePct: f.CoveragePct, Gap: f.Gap, Status: string(f.Status),
		StatusLabel: domain.CoverageLabels[f.Status], Color: domain.CoverageColors[f.Status], Seq: r.Seq,
	}
}

type groupDTO struct {
	Required      decimal.Decimal  `json:"required"`
	RequiredWhole int              `json:"required_whole"`
	Capacity      int              `json:"capacity"`
	MachinesTotal int              `json:"machines_total"`
	CoveragePct   *decimal.Decimal `json:"coverage_pct"`
	Status        string           `json:"status"`
	StatusLabel   string           `json:"status_label"`
	Color         string           `json:"color"`
}

func toGroupDTO(g domain.Group) groupDTO {
	return groupDTO{
		Required: g.Required, RequiredWhole: g.RequiredWhole, Capacity: g.Capacity, MachinesTotal: g.MachinesTotal, CoveragePct: g.CoveragePct,
		Status: string(g.Status), StatusLabel: domain.CoverageLabels[g.Status], Color: domain.CoverageColors[g.Status],
	}
}

func toStagesDTO(m map[string]domain.Group) map[string]groupDTO {
	out := map[string]groupDTO{}
	for k, g := range m {
		out[k] = toGroupDTO(g)
	}
	return out
}

type coverageItemDTO struct {
	UnitCode       *string             `json:"unit_code"`
	CooperativeRef *string             `json:"cooperative_ref"`
	Name           string              `json:"name"`
	ProvinceCode   *string             `json:"province_code"`
	CommuneCode    *string             `json:"commune_code"`
	Cooperatives   int                 `json:"cooperatives"`
	AreaSource     *string             `json:"area_source"`
	AreaLocked     bool                `json:"area_locked"`
	Overall        groupDTO            `json:"overall"`
	Stages         map[string]groupDTO `json:"stages"`
	// G-10: Mã HTX và nhãn nguồn diện tích ([App HTX]/[Nhập tay]) — chỉ ở cấp HTX.
	CooperativeCode *string `json:"cooperative_code"`
	AreaSourceLabel *string `json:"area_source_label"`
}

func toCoverageItemDTO(it app.CoverageItem) coverageItemDTO {
	d := coverageItemDTO{
		UnitCode: strPtr(it.UnitCode), CooperativeRef: refPtr(it.CooperativeRef), Name: it.Name, ProvinceCode: strPtr(it.ProvinceCode),
		CommuneCode: strPtr(it.CommuneCode), Cooperatives: it.Cooperatives, AreaSource: strPtr(it.AreaSource), AreaLocked: it.AreaLocked,
		Overall: toGroupDTO(it.Overall), Stages: toStagesDTO(it.Stages), CooperativeCode: strPtr(it.CooperativeCode),
	}
	if it.AreaSource != "" {
		d.AreaSourceLabel = strPtr(areaSourceLabels[it.AreaSource])
	}
	return d
}

// ---------------------------------------------------------------------------
// Tiện ích
// ---------------------------------------------------------------------------

func parseCursor(c *gin.Context) (uuid.UUID, bool) {
	var after uuid.UUID
	if cur := c.Query("cursor"); cur != "" {
		b, err := base64.RawURLEncoding.DecodeString(cur)
		if err != nil || len(b) != 16 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return after, false
		}
		copy(after[:], b)
	}
	return after, true
}

func encodeCursor(next *uuid.UUID) any {
	if next == nil {
		return nil
	}
	return base64.RawURLEncoding.EncodeToString(next[:])
}

func queryLimit(c *gin.Context) int {
	if n, err := strconv.Atoi(c.Query("limit")); err == nil {
		return n
	}
	return 50
}

func pathID(c *gin.Context, name string, notFound error) (uuid.UUID, bool) {
	ref, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, notFound)
		return ref, false
	}
	return ref, true
}

func bindJSON(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

// fieldErrs gom lỗi theo trường khi đọc request.
type fieldErrs struct{ p *problem.Problem }

func (f *fieldErrs) add(field, reason string) {
	if f.p == nil {
		f.p = problem.Validation
	}
	f.p = f.p.WithField(field, reason)
}

func (f *fieldErrs) fail(c *gin.Context) bool {
	if f.p == nil {
		return false
	}
	httpx.Fail(c, f.p)
	return true
}

func (f *fieldErrs) ref(field, s string) uuid.UUID {
	if s == "" {
		return uuid.Nil
	}
	u, err := ids.ParseRef(s)
	if err != nil {
		f.add(field, "phải là UUID")
	}
	return u
}

func (f *fieldErrs) optRef(field string, s *string) *uuid.UUID {
	if s == nil {
		return nil
	}
	u := f.ref(field, *s)
	return &u
}

func (f *fieldErrs) date(field, s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		f.add(field, "phải có dạng YYYY-MM-DD")
		return nil
	}
	return &t
}

func (f *fieldErrs) optDate(field string, s *string) *time.Time {
	if s == nil {
		return nil
	}
	return f.date(field, *s)
}

func (f *fieldErrs) instant(field string, s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		f.add(field, "phải là thời điểm RFC 3339")
		return nil
	}
	return &t
}

// season đọc season_code + season_year từ query.
func (f *fieldErrs) season(c *gin.Context, required bool) domain.Season {
	s := domain.Season{Code: c.Query("season_code")}
	if y := c.Query("season_year"); y != "" {
		n, err := strconv.Atoi(y)
		if err != nil {
			f.add("season_year", "phải là số nguyên")
		}
		s.Year = n
	}
	if required && (s.Code == "" || s.Year == 0) {
		f.add("season_code", "bắt buộc season_code và season_year")
	}
	return s
}

// runQuery đọc run_id hoặc (season_code, season_year) từ query.
func (f *fieldErrs) runQuery(c *gin.Context) app.RunQuery {
	q := app.RunQuery{RunID: f.ref("run_id", c.Query("run_id")), Season: f.season(c, false)}
	if q.Season.Code != "" && q.Season.Year == 0 {
		f.add("season_year", "bắt buộc khi có season_code")
	}
	return q
}
