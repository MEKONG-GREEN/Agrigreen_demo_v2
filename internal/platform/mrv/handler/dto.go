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

// catalogDTO là phiên bản danh mục trả qua API — cùng dạng với payload entity
// đồng bộ mrv_catalog (app.CatalogDoc).
type catalogDTO = app.CatalogDoc

func toCatalogDTO(v domain.CatalogVersion) catalogDTO { return app.CatalogDocOf(v) }

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
