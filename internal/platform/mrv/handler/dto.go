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
	Urgent           *bool                      `json:"urgent,omitempty"`
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
	u := l.Urgent
	d.Urgent = &u
	return d
}

// alertDTO là một cảnh báo MRV (sổ pf_mrv.alert).
type alertDTO struct {
	ID            string            `json:"id"`
	Kind          string            `json:"kind"`
	Level         int               `json:"level"`
	Severity      string            `json:"severity"`
	Label         string            `json:"label"`
	AdminUnitCode string            `json:"admin_unit_code"`
	SubjectType   string            `json:"subject_type"`
	SubjectID     string            `json:"subject_id"`
	SubjectRootID string            `json:"subject_root_id"`
	PlotRef       *string           `json:"plot_ref"`
	SeasonRef     *string           `json:"season_ref"`
	Detail        json.RawMessage   `json:"detail"`
	Params        []domain.ParamUse `json:"params"`
	RuleVersion   string            `json:"rule_version"`
	EventTime     string            `json:"event_time"`
	RecordedAt    string            `json:"recorded_at"`
	Seq           int64             `json:"seq"`
}

var alertLabels = map[string]string{
	"pest_urgent": "Cảnh báo khẩn", "awd_no_infrastructure": "Ghi AWD nhưng vùng chưa đủ hạ tầng",
	"review_overdue": "Hồ sơ quá hạn xử lý",
}

func toAlertDTO(a store.AlertRow) alertDTO {
	d := alertDTO{
		ID: a.ID.String(), Kind: a.Kind, Level: a.Level, Severity: a.Severity, Label: alertLabels[a.Kind],
		AdminUnitCode: a.AdminUnitCode, SubjectType: a.SubjectType, SubjectID: a.SubjectID.String(),
		SubjectRootID: a.SubjectRootID.String(), Detail: a.Detail, Params: a.Params, RuleVersion: a.RuleVersion,
		EventTime: ts(a.EventTime), RecordedAt: ts(a.RecordedAt), Seq: a.Seq,
	}
	if a.Kind == "review_overdue" {
		d.Label += map[int]string{1: " — đã báo người duyệt", 2: " — đã leo thang lên quản trị TTKN tỉnh"}[a.Level]
	}
	if a.PlotRef != nil {
		s := a.PlotRef.String()
		d.PlotRef = &s
	}
	if a.SeasonRef != nil {
		s := a.SeasonRef.String()
		d.SeasonRef = &s
	}
	if d.Params == nil {
		d.Params = []domain.ParamUse{}
	}
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
	// Urgent: phiên bản đang xem mang nhãn "Cảnh báo khẩn" (KN US-MRV-04).
	Urgent bool       `json:"urgent"`
	Alerts []alertDTO `json:"alerts"`
}

func toViewDTO(v app.EvidenceView) evidenceViewDTO {
	d := evidenceViewDTO{
		Evidence: toEvidenceDTO(v.Evidence), HeadID: v.HeadID.String(), IsCurrent: v.HeadID == v.Evidence.ID,
		ReviewStatus: string(v.ReviewStatus), Chain: make([]evidenceDTO, 0, len(v.Chain)), Reviews: make([]reviewDTO, 0, len(v.Reviews)),
		Alerts: make([]alertDTO, 0, len(v.Alerts)),
	}
	for _, a := range v.Alerts {
		d.Alerts = append(d.Alerts, toAlertDTO(a))
		if a.Kind == "pest_urgent" && a.SubjectID == v.Evidence.ID {
			d.Urgent = true
		}
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
