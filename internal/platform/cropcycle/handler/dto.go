package handler

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
)

func ref(u uuid.UUID) *string {
	if u == uuid.Nil {
		return nil
	}
	s := u.String()
	return &s
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func tsPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

func dec(d *decimal.Decimal, places int32) *string {
	if d == nil {
		return nil
	}
	s := d.StringFixed(places)
	return &s
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---------------------------------------------------------------------------
// SOP
// ---------------------------------------------------------------------------

type templateDTO struct {
	Ref               string  `json:"ref"`
	Code              string  `json:"code"`
	Name              string  `json:"name"`
	Crop              string  `json:"crop"`
	Scope             string  `json:"scope"`
	CooperativeRef    *string `json:"cooperative_ref"`
	Standard          *string `json:"standard"`
	Description       *string `json:"description"`
	Status            string  `json:"status"`
	StatusReason      *string `json:"status_reason"`
	SourceTemplateRef *string `json:"source_template_ref"`
	LatestVersion     int     `json:"latest_version"`
	PublishedVersion  *int    `json:"published_version"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

func templateOut(t store.Template) templateDTO {
	return templateDTO{Ref: t.Ref.String(), Code: t.Code, Name: t.Name, Crop: t.Crop, Scope: t.Scope, CooperativeRef: ref(t.CooperativeRef),
		Standard: strOrNil(t.Standard), Description: strOrNil(t.Description), Status: t.Status, StatusReason: strOrNil(t.StatusReason),
		SourceTemplateRef: ref(t.SourceTemplateRef), LatestVersion: t.LatestVersion, PublishedVersion: t.PublishedVersion,
		CreatedAt: ts(t.CreatedAt), UpdatedAt: ts(t.UpdatedAt)}
}

type versionDTO struct {
	ID                string        `json:"id"`
	TemplateRef       string        `json:"template_ref"`
	VersionNo         int           `json:"version_no"`
	Status            string        `json:"status"`
	Steps             []domain.Step `json:"steps"`
	HarvestDayOffset  *int          `json:"harvest_day_offset"`
	HarvestWindowDays *int          `json:"harvest_window_days"`
	SourceVersionRef  *string       `json:"source_version_ref"`
	ChangeNote        *string       `json:"change_note"`
	AuthoredBy        string        `json:"authored_by"`
	AuthoredAt        string        `json:"authored_at"`
	PublishedBy       *string       `json:"published_by"`
	PublishedAt       *string       `json:"published_at"`
}

func versionOut(v store.Version) versionDTO {
	steps := v.Steps
	if steps == nil {
		steps = []domain.Step{}
	}
	for i := range steps {
		if steps[i].EvidenceKinds == nil {
			steps[i].EvidenceKinds = []string{}
		}
	}
	return versionDTO{ID: v.ID.String(), TemplateRef: v.TemplateRef.String(), VersionNo: v.VersionNo, Status: v.Status, Steps: steps,
		HarvestDayOffset: v.HarvestDayOffset, HarvestWindowDays: v.HarvestWindowDays, SourceVersionRef: ref(v.SourceVersionRef),
		ChangeNote: strOrNil(v.ChangeNote), AuthoredBy: v.AuthoredBy.String(), AuthoredAt: ts(v.AuthoredAt), PublishedBy: ref(v.PublishedBy),
		PublishedAt: tsPtr(v.PublishedAt)}
}

type sopDTO struct {
	templateDTO
	Versions []versionDTO `json:"versions"`
}

func sopOut(v app.SOPView) sopDTO {
	out := sopDTO{templateDTO: templateOut(v.Template), Versions: make([]versionDTO, 0, len(v.Versions))}
	for _, x := range v.Versions {
		out.Versions = append(out.Versions, versionOut(x))
	}
	return out
}

type bindingDTO struct {
	VarietyCode string  `json:"variety_code"`
	TemplateRef string  `json:"template_ref"`
	Note        *string `json:"note"`
	BoundBy     string  `json:"bound_by"`
	BoundAt     string  `json:"bound_at"`
}

func bindingOut(b store.Binding) bindingDTO {
	return bindingDTO{VarietyCode: b.VarietyCode, TemplateRef: b.TemplateRef.String(), Note: strOrNil(b.Note), BoundBy: b.BoundBy.String(), BoundAt: ts(b.BoundAt)}
}

// ---------------------------------------------------------------------------
// Vụ
// ---------------------------------------------------------------------------

type seasonDTO struct {
	SeasonRef           string  `json:"season_ref"`
	PlotRef             string  `json:"plot_ref"`
	PlotCode            string  `json:"plot_code"`
	HouseholdRef        *string `json:"household_ref"`
	CooperativeRef      string  `json:"cooperative_ref"`
	CommuneCode         string  `json:"commune_code"`
	SeasonCode          string  `json:"season_code"`
	SeasonYear          int     `json:"season_year"`
	VarietyCode         string  `json:"variety_code"`
	SOPTemplateRef      string  `json:"sop_template_ref"`
	SOPVersionID        string  `json:"sop_version_id"`
	SOPVersionNo        int     `json:"sop_version_no"`
	SowingDate          string  `json:"sowing_date"`
	ExpectedHarvestDate string  `json:"expected_harvest_date"`
	HarvestOpenDate     string  `json:"harvest_open_date"`
	AreaHa              string  `json:"area_ha"`
	ExpectedYieldTPerHa *string `json:"expected_yield_t_per_ha"`
	Status              string  `json:"status"`
	CurrentEventID      string  `json:"current_event_id"`
	Version             int64   `json:"version"`
	LocationStatus      string  `json:"location_status"`
	GPSSource           string  `json:"gps_source"`
	OnBehalf            bool    `json:"on_behalf"`
	OpenedBy            string  `json:"opened_by"`
	OpenedAt            string  `json:"opened_at"`
	CopiedFromSeasonRef *string `json:"copied_from_season_ref"`
	HarvestID           *string `json:"harvest_id"`
	CompletedAt         *string `json:"completed_at"`
	UpdatedAt           string  `json:"updated_at"`
}

func seasonOut(s store.Season) seasonDTO {
	return seasonDTO{SeasonRef: s.SeasonRef.String(), PlotRef: s.PlotRef.String(), PlotCode: s.PlotCode, HouseholdRef: ref(s.HouseholdRef),
		CooperativeRef: s.CooperativeRef.String(), CommuneCode: s.CommuneCode, SeasonCode: s.SeasonCode, SeasonYear: s.SeasonYear,
		VarietyCode: s.VarietyCode, SOPTemplateRef: s.SOPTemplateRef.String(), SOPVersionID: s.SOPVersionID.String(), SOPVersionNo: s.SOPVersionNo,
		SowingDate: domain.FormatDay(s.SowingDate), ExpectedHarvestDate: domain.FormatDay(s.ExpectedHarvestDate),
		HarvestOpenDate: domain.FormatDay(s.HarvestOpenDate), AreaHa: s.AreaHa.StringFixed(4), ExpectedYieldTPerHa: dec(s.ExpectedYield, 4),
		Status: s.Status, CurrentEventID: s.CurrentEventID.String(), Version: s.Version, LocationStatus: s.LocationStatus, GPSSource: s.GPSSource,
		OnBehalf: s.OnBehalf, OpenedBy: s.OpenedBy.String(), OpenedAt: ts(s.OpenedAt), CopiedFromSeasonRef: ref(s.CopiedFrom),
		HarvestID: ref(s.HarvestID), CompletedAt: tsPtr(s.CompletedAt), UpdatedAt: ts(s.UpdatedAt)}
}

type seasonEventDTO struct {
	ID               string  `json:"id"`
	Seq              int64   `json:"seq"`
	Action           string  `json:"action"`
	VarietyCode      string  `json:"variety_code"`
	SOPVersionID     string  `json:"sop_version_id"`
	SowingDate       string  `json:"sowing_date"`
	LocationStatus   string  `json:"location_status"`
	GPSSource        string  `json:"gps_source"`
	DistanceM        *string `json:"distance_m"`
	OnBehalf         bool    `json:"on_behalf"`
	Channel          string  `json:"channel"`
	ActorRef         string  `json:"actor_ref"`
	EventTime        string  `json:"event_time"`
	RecordedAt       string  `json:"recorded_at"`
	SupersedesID     *string `json:"supersedes_id"`
	CorrectionReason *string `json:"correction_reason"`
	HarvestID        *string `json:"harvest_id"`
}

func seasonEventOut(e store.SeasonEvent) seasonEventDTO {
	var sup *string
	if e.SupersedesID != nil {
		sup = ref(*e.SupersedesID)
	}
	return seasonEventDTO{ID: e.ID.String(), Seq: e.Seq, Action: e.Action, VarietyCode: e.VarietyCode, SOPVersionID: e.SOPVersionID.String(),
		SowingDate: domain.FormatDay(e.SowingDate), LocationStatus: e.LocationStatus, GPSSource: e.GPS.Source, DistanceM: dec(e.DistanceM, 1),
		OnBehalf: e.OnBehalf, Channel: e.Channel, ActorRef: e.ActorRef.String(), EventTime: ts(e.EventTime), RecordedAt: ts(e.RecordedAt),
		SupersedesID: sup, CorrectionReason: e.CorrectionReason, HarvestID: ref(e.HarvestID)}
}

type progressDTO struct {
	SOPSteps          int     `json:"sop_steps"`
	SOPMandatorySteps int     `json:"sop_mandatory_steps"`
	SOPStepsDone      int     `json:"sop_steps_done"`
	SOPMandatoryDone  int     `json:"sop_mandatory_done"`
	ApprovedLogs      int     `json:"approved_logs"`
	PendingLogs       int     `json:"pending_logs"`
	ExpectedKg        *string `json:"expected_kg"`
	ActualKg          *string `json:"actual_kg"`
	ActualYieldTPerHa *string `json:"actual_yield_t_per_ha"`
}

type seasonDetailDTO struct {
	seasonDTO
	Events   []seasonEventDTO `json:"events"`
	Steps    []domain.Step    `json:"steps"`
	Progress *progressDTO     `json:"progress"`
	Harvest  *harvestDTO      `json:"harvest"`
}

func seasonDetailOut(d app.SeasonDetail) seasonDetailDTO {
	out := seasonDetailDTO{seasonDTO: seasonOut(d.Season), Events: make([]seasonEventDTO, 0, len(d.Events)), Steps: d.Steps}
	if out.Steps == nil {
		out.Steps = []domain.Step{}
	}
	for _, e := range d.Events {
		out.Events = append(out.Events, seasonEventOut(e))
	}
	if y := d.Yield; y != nil {
		out.Progress = &progressDTO{SOPSteps: y.SOPSteps, SOPMandatorySteps: y.SOPMandatory, SOPStepsDone: y.SOPStepsDone,
			SOPMandatoryDone: y.SOPMandatoryDone, ApprovedLogs: y.ApprovedLogs, PendingLogs: y.PendingLogs,
			ExpectedKg: dec(y.ExpectedKg, 3), ActualKg: dec(y.ActualKg, 3), ActualYieldTPerHa: dec(y.ActualYield, 4)}
	}
	if d.Harvest != nil {
		h := harvestOut(*d.Harvest)
		out.Harvest = &h
	}
	return out
}

// ---------------------------------------------------------------------------
// Nhật ký
// ---------------------------------------------------------------------------

type gpsDTO struct {
	Lon       *string `json:"lon"`
	Lat       *string `json:"lat"`
	AccuracyM *string `json:"accuracy_m"`
	Source    string  `json:"source"`
}

func gpsOut(g domain.GPS) gpsDTO {
	return gpsDTO{Lon: dec(g.Lon, 7), Lat: dec(g.Lat, 7), AccuracyM: dec(g.AccuracyM, 2), Source: g.Source}
}

type inputDTO struct {
	Name string `json:"name"`
	Qty  string `json:"qty"`
	Unit string `json:"unit"`
}

type logDTO struct {
	ID               string     `json:"id"`
	LogRef           string     `json:"log_ref"`
	Version          int64      `json:"version"`
	SeasonRef        string     `json:"season_ref"`
	PlotRef          string     `json:"plot_ref"`
	HouseholdRef     *string    `json:"household_ref"`
	CooperativeRef   string     `json:"cooperative_ref"`
	CommuneCode      string     `json:"commune_code"`
	ActivityCode     string     `json:"activity_code"`
	SOPStepSeq       *int       `json:"sop_step_seq"`
	EventTime        string     `json:"event_time"`
	GPS              gpsDTO     `json:"gps"`
	LocationStatus   string     `json:"location_status"`
	DistanceM        *string    `json:"distance_m"`
	MachineCodes     []string   `json:"machine_codes"`
	Inputs           []inputDTO `json:"inputs"`
	Note             *string    `json:"note"`
	FileRefs         []string   `json:"file_refs"`
	BatchRef         *string    `json:"batch_ref"`
	OnBehalf         bool       `json:"on_behalf"`
	Channel          string     `json:"channel"`
	AuthorRef        string     `json:"author_ref"`
	RecordedAt       string     `json:"recorded_at"`
	SupersedesID     *string    `json:"supersedes_id"`
	CorrectionReason *string    `json:"correction_reason"`
	BecameCurrent    bool       `json:"became_current"`
}

func logOut(l store.FarmLog) logDTO {
	machines := l.MachineCodes
	if machines == nil {
		machines = []string{}
	}
	inputs := make([]inputDTO, 0, len(l.Inputs))
	for _, x := range l.Inputs {
		inputs = append(inputs, inputDTO{Name: x.Name, Qty: x.Qty.String(), Unit: x.Unit})
	}
	files := make([]string, 0, len(l.FileRefs))
	for _, f := range l.FileRefs {
		files = append(files, f.String())
	}
	var sup *string
	if l.SupersedesID != nil {
		sup = ref(*l.SupersedesID)
	}
	return logDTO{ID: l.ID.String(), LogRef: l.LogRef.String(), Version: l.Version, SeasonRef: l.SeasonRef.String(), PlotRef: l.PlotRef.String(),
		HouseholdRef: ref(l.HouseholdRef), CooperativeRef: l.CooperativeRef.String(), CommuneCode: l.CommuneCode, ActivityCode: l.ActivityCode,
		SOPStepSeq: l.SOPStepSeq, EventTime: ts(l.EventTime), GPS: gpsOut(l.GPS), LocationStatus: l.LocationStatus, DistanceM: dec(l.DistanceM, 1),
		MachineCodes: machines, Inputs: inputs, Note: strOrNil(l.Note), FileRefs: files, BatchRef: ref(l.BatchRef), OnBehalf: l.OnBehalf,
		Channel: l.Channel, AuthorRef: l.ActorRef.String(), RecordedAt: ts(l.RecordedAt), SupersedesID: sup, CorrectionReason: l.CorrectionReason,
		BecameCurrent: l.BecameCurrent}
}

type logStateDTO struct {
	ID             string  `json:"id"`
	LogRef         string  `json:"log_ref"`
	Version        int64   `json:"version"`
	SeasonRef      string  `json:"season_ref"`
	PlotRef        string  `json:"plot_ref"`
	HouseholdRef   *string `json:"household_ref"`
	CooperativeRef string  `json:"cooperative_ref"`
	ActivityCode   string  `json:"activity_code"`
	SOPStepSeq     *int    `json:"sop_step_seq"`
	EventTime      string  `json:"event_time"`
	LocationStatus string  `json:"location_status"`
	GPSSource      string  `json:"gps_source"`
	DistanceM      *string `json:"distance_m"`
	AuthorRef      string  `json:"author_ref"`
	OnBehalf       bool    `json:"on_behalf"`
	BatchRef       *string `json:"batch_ref"`
	Status         string  `json:"status"`
	SupersededBy   *string `json:"superseded_by"`
	ReviewNote     *string `json:"review_note"`
	ReviewedBy     *string `json:"reviewed_by"`
	ReviewedAt     *string `json:"reviewed_at"`
}

func stateOut(s store.LogState) logStateDTO {
	return logStateDTO{ID: s.ID.String(), LogRef: s.LogRef.String(), Version: s.Version, SeasonRef: s.SeasonRef.String(), PlotRef: s.PlotRef.String(),
		HouseholdRef: ref(s.HouseholdRef), CooperativeRef: s.CooperativeRef.String(), ActivityCode: s.ActivityCode, SOPStepSeq: s.SOPStepSeq,
		EventTime: ts(s.EventTime), LocationStatus: s.LocationStatus, GPSSource: s.GPSSource, DistanceM: dec(s.DistanceM, 1),
		AuthorRef: s.AuthorRef.String(), OnBehalf: s.OnBehalf, BatchRef: ref(s.BatchRef), Status: s.Status, SupersededBy: ref(s.SupersededBy),
		ReviewNote: strOrNil(s.ReviewNote), ReviewedBy: ref(s.ReviewedBy), ReviewedAt: tsPtr(s.ReviewedAt)}
}

// ---------------------------------------------------------------------------
// Sản lượng
// ---------------------------------------------------------------------------

type harvestDTO struct {
	ID               string  `json:"id"`
	SeasonRef        string  `json:"season_ref"`
	PlotRef          string  `json:"plot_ref"`
	CooperativeRef   string  `json:"cooperative_ref"`
	HarvestDate      string  `json:"harvest_date"`
	MassKg           string  `json:"mass_kg"`
	AreaHa           string  `json:"area_ha"`
	YieldTPerHa      string  `json:"yield_t_per_ha"`
	MoisturePct      *string `json:"moisture_pct"`
	AnomalyKind      *string `json:"anomaly_kind"`
	AnomalyThreshold *string `json:"anomaly_threshold_t_per_ha"`
	AnomalyConfirmed bool    `json:"anomaly_confirmed"`
	Source           string  `json:"source"`
	LocationStatus   string  `json:"location_status"`
	GPSSource        string  `json:"gps_source"`
	OnBehalf         bool    `json:"on_behalf"`
	ActorRef         string  `json:"actor_ref"`
	EventTime        string  `json:"event_time"`
	RecordedAt       string  `json:"recorded_at"`
	SupersedesID     *string `json:"supersedes_id"`
	CorrectionReason *string `json:"correction_reason"`
	VarietyCode      string  `json:"variety_code"`
	SeasonCode       string  `json:"season_code"`
	SeasonYear       int     `json:"season_year"`
	HouseholdRef     *string `json:"household_ref"`
	DistanceM        *string `json:"distance_m"`
}

func harvestOut(h store.Harvest) harvestDTO {
	var sup *string
	if h.SupersedesID != nil {
		sup = ref(*h.SupersedesID)
	}
	return harvestDTO{ID: h.ID.String(), SeasonRef: h.SeasonRef.String(), PlotRef: h.PlotRef.String(), CooperativeRef: h.CooperativeRef.String(),
		HarvestDate: domain.FormatDay(h.HarvestDate), MassKg: h.MassKg.String(), AreaHa: h.AreaHa.StringFixed(4), YieldTPerHa: h.YieldTPerHa.StringFixed(4),
		MoisturePct: dec(h.MoisturePct, 2), AnomalyKind: strOrNil(h.AnomalyKind), AnomalyThreshold: dec(h.AnomalyThreshold, 4),
		AnomalyConfirmed: h.AnomalyConfirmed, Source: h.Source, LocationStatus: h.LocationStatus, GPSSource: h.GPS.Source, OnBehalf: h.OnBehalf,
		ActorRef: h.ActorRef.String(), EventTime: ts(h.EventTime), RecordedAt: ts(h.RecordedAt), SupersedesID: sup,
		CorrectionReason: h.CorrectionReason, VarietyCode: h.VarietyCode, SeasonCode: h.SeasonCode, SeasonYear: h.SeasonYear,
		HouseholdRef: ref(h.HouseholdRef), DistanceM: dec(h.DistanceM, 1)}
}

// ---------------------------------------------------------------------------
// Bảng điều hành
// ---------------------------------------------------------------------------

type dashGroupDTO struct {
	Key               *string `json:"key,omitempty"`
	Seasons           int64   `json:"seasons"`
	OpenSeasons       int64   `json:"open_seasons"`
	CompletedSeasons  int64   `json:"completed_seasons"`
	Households        int64   `json:"households"`
	AreaHa            string  `json:"area_ha"`
	ExpectedKg        string  `json:"expected_kg"`
	ActualKg          string  `json:"actual_kg"`
	HarvestedAreaHa   string  `json:"harvested_area_ha"`
	ActualYieldTPerHa *string `json:"actual_yield_t_per_ha"`
	SOPProgressPct    *string `json:"sop_progress_pct"`
	ApprovedLogs      int64   `json:"approved_logs"`
	PendingLogs       int64   `json:"pending_logs"`
}

func dashOut(g app.DashGroup, withKey bool) dashGroupDTO {
	d := dashGroupDTO{Seasons: g.Seasons, OpenSeasons: g.OpenSeasons, CompletedSeasons: g.CompletedSeasons, Households: g.Households,
		AreaHa: g.AreaHa.StringFixed(4), ExpectedKg: g.ExpectedKg.StringFixed(3), ActualKg: g.ActualKg.StringFixed(3),
		HarvestedAreaHa: g.HarvestedAreaHa.StringFixed(4), ActualYieldTPerHa: dec(g.ActualYieldTPerHa, 4), SOPProgressPct: dec(g.SOPProgressPct, 1),
		ApprovedLogs: g.ApprovedLogs, PendingLogs: g.PendingLogs}
	if withKey {
		k := g.Key
		d.Key = &k
	}
	return d
}
