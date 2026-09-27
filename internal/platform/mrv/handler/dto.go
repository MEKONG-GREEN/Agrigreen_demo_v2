package handler

import (
	"encoding/json"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/store"
)

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func tsPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

type fieldDTO struct {
	Key        string   `json:"key"`
	LabelVi    string   `json:"label_vi"`
	DataType   string   `json:"data_type"`
	Unit       *string  `json:"unit"`
	Required   bool     `json:"required"`
	Min        *string  `json:"min"`
	Max        *string  `json:"max"`
	MaxScale   *int     `json:"max_scale"`
	EnumValues []string `json:"enum_values"`
	HelpVi     *string  `json:"help_vi"`
	Position   int      `json:"position"`
	Reserved   bool     `json:"reserved"` // giá trị lấy từ cột riêng (plot_ref, season_code, gps, file_refs)
}

type catalogDTO struct {
	ID           string     `json:"id"`
	EvidenceType string     `json:"evidence_type"`
	Version      int        `json:"version"`
	Status       string     `json:"status"`
	Provisional  bool       `json:"provisional"`
	SourceNote   string     `json:"source_note"`
	Fields       []fieldDTO `json:"fields"`
	SetterRef    string     `json:"setter_ref"`
	ApproverRef  *string    `json:"approver_ref"`
	SubmittedAt  *string    `json:"submitted_at"`
	DecidedAt    *string    `json:"decided_at"`
	DecisionNote *string    `json:"decision_note"`
	CreatedAt    string     `json:"created_at"`
}

func toCatalogDTO(v domain.CatalogVersion) catalogDTO {
	d := catalogDTO{
		ID: v.ID.String(), EvidenceType: v.EvidenceType, Version: v.Version, Status: string(v.Status), Provisional: v.Provisional,
		SourceNote: v.SourceNote, Fields: make([]fieldDTO, 0, len(v.Fields)), SetterRef: v.SetterRef.String(),
		SubmittedAt: tsPtr(v.SubmittedAt), DecidedAt: tsPtr(v.DecidedAt), DecisionNote: v.DecisionNote, CreatedAt: ts(v.CreatedAt),
	}
	if v.ApproverRef != nil {
		s := v.ApproverRef.String()
		d.ApproverRef = &s
	}
	for _, f := range v.Fields {
		fd := fieldDTO{Key: f.Key, LabelVi: f.LabelVi, DataType: string(f.Type), Required: f.Required, MaxScale: f.MaxScale,
			EnumValues: f.EnumValues, Position: f.Position, Reserved: domain.IsReserved(f.Key)}
		if fd.EnumValues == nil {
			fd.EnumValues = []string{}
		}
		if f.Unit != "" {
			u := f.Unit
			fd.Unit = &u
		}
		if f.HelpVi != "" {
			h := f.HelpVi
			fd.HelpVi = &h
		}
		if f.Min != nil {
			s := f.Min.String()
			fd.Min = &s
		}
		if f.Max != nil {
			s := f.Max.String()
			fd.Max = &s
		}
		d.Fields = append(d.Fields, fd)
	}
	return d
}

type gpsDTO struct {
	Lat       string  `json:"lat" pii:"true"`
	Lon       string  `json:"lon" pii:"true"`
	AccuracyM *string `json:"accuracy_m"`
}

type evidenceDTO struct {
	ID               string                     `json:"id"`
	RootID           string                     `json:"root_id"`
	ChainVersion     int                        `json:"chain_version"`
	EvidenceType     string                     `json:"evidence_type"`
	CatalogVersion   int                        `json:"catalog_version"`
	AdminUnitCode    string                     `json:"admin_unit_code"`
	PlotRef          *string                    `json:"plot_ref"`
	HouseholdRef     *string                    `json:"household_ref"`
	SeasonCode       *string                    `json:"season_code"`
	Values           map[string]json.RawMessage `json:"values"`
	FileRefs         []domain.FileRef           `json:"file_refs"`
	GPS              *gpsDTO                    `json:"gps"`
	EventTime        string                     `json:"event_time"`
	RecordedAt       string                     `json:"recorded_at"`
	CapturedBy       string                     `json:"captured_by"`
	DeviceID         *string                    `json:"device_id"`
	SupersedesID     *string                    `json:"supersedes_id"`
	CorrectionReason *string                    `json:"correction_reason"`
	Seq              int64                      `json:"seq"`
	ReviewStatus     *string                    `json:"review_status,omitempty"`
}

func toEvidenceDTO(e domain.Evidence) evidenceDTO {
	d := evidenceDTO{
		ID: e.ID.String(), RootID: e.RootID.String(), ChainVersion: e.ChainVersion, EvidenceType: e.EvidenceType,
		CatalogVersion: e.CatalogVersion, AdminUnitCode: e.AdminUnitCode, SeasonCode: e.SeasonCode, Values: e.Values,
		FileRefs: e.FileRefs, EventTime: ts(e.EventTime), RecordedAt: ts(e.RecordedAt), CapturedBy: e.ActorRef.String(),
		DeviceID: e.DeviceID, CorrectionReason: e.CorrectionReason, Seq: e.Seq,
	}
	if d.FileRefs == nil {
		d.FileRefs = []domain.FileRef{}
	}
	if e.PlotRef != nil {
		s := e.PlotRef.String()
		d.PlotRef = &s
	}
	if e.HouseholdRef != nil {
		s := e.HouseholdRef.String()
		d.HouseholdRef = &s
	}
	if e.SupersedesID != nil {
		s := e.SupersedesID.String()
		d.SupersedesID = &s
	}
	if e.GPS != nil {
		g := &gpsDTO{Lat: e.GPS.Lat.String(), Lon: e.GPS.Lon.String()}
		if e.GPS.AccuracyM != nil {
			a := e.GPS.AccuracyM.String()
			g.AccuracyM = &a
		}
		d.GPS = g
	}
	return d
}

func toListedDTO(l store.Listed) evidenceDTO {
	d := toEvidenceDTO(l.Evidence)
	s := string(l.ReviewStatus)
	d.ReviewStatus = &s
	return d
}

type reviewDTO struct {
	ID          string `json:"id"`
	EvidenceID  string `json:"evidence_id"`
	RootID      string `json:"root_id"`
	Decision    string `json:"decision"`
	Reason      string `json:"reason"`
	ReviewerRef string `json:"reviewer_ref"`
	EventTime   string `json:"event_time"`
	RecordedAt  string `json:"recorded_at"`
	Seq         int64  `json:"seq"`
}

func toReviewDTO(r domain.Review) reviewDTO {
	return reviewDTO{
		ID: r.ID.String(), EvidenceID: r.EvidenceID.String(), RootID: r.RootID.String(), Decision: string(r.Decision),
		Reason: r.Reason, ReviewerRef: r.ActorRef.String(), EventTime: ts(r.EventTime), RecordedAt: ts(r.RecordedAt), Seq: r.Seq,
	}
}

type evidenceViewDTO struct {
	Evidence     evidenceDTO   `json:"evidence"`
	HeadID       string        `json:"head_id"`
	IsCurrent    bool          `json:"is_current"`
	ReviewStatus string        `json:"review_status"`
	Chain        []evidenceDTO `json:"chain"`
	Reviews      []reviewDTO   `json:"reviews"`
}

func toViewDTO(v app.EvidenceView) evidenceViewDTO {
	d := evidenceViewDTO{
		Evidence: toEvidenceDTO(v.Evidence), HeadID: v.HeadID.String(), IsCurrent: v.HeadID == v.Evidence.ID,
		ReviewStatus: string(v.ReviewStatus), Chain: make([]evidenceDTO, 0, len(v.Chain)), Reviews: make([]reviewDTO, 0, len(v.Reviews)),
	}
	for _, e := range v.Chain {
		d.Chain = append(d.Chain, toEvidenceDTO(e))
	}
	for _, r := range v.Reviews {
		d.Reviews = append(d.Reviews, toReviewDTO(r))
	}
	return d
}

type exportDTO struct {
	ID             string               `json:"id"`
	Status         string               `json:"status"`
	Filters        domain.ExportFilters `json:"filters"`
	RequestedBy    string               `json:"requested_by"`
	RequestedAt    string               `json:"requested_at"`
	ObjectKey      *string              `json:"object_key"`
	PackageSHA256  *string              `json:"package_sha256"`
	SnapshotSHA256 *string              `json:"snapshot_sha256"`
	EvidenceCount  *int                 `json:"evidence_count"`
	ErrorCode      *string              `json:"error_code"`
	CompletedAt    *string              `json:"completed_at"`
}

func toExportDTO(j domain.ExportJob) exportDTO {
	return exportDTO{
		ID: j.ID.String(), Status: string(j.Status), Filters: j.Filters, RequestedBy: j.RequestedBy.String(),
		RequestedAt: ts(j.RequestedAt), ObjectKey: j.ObjectKey, PackageSHA256: j.PackageSHA256, SnapshotSHA256: j.SnapshotSHA256,
		EvidenceCount: j.EvidenceCount, ErrorCode: j.ErrorCode, CompletedAt: tsPtr(j.CompletedAt),
	}
}
