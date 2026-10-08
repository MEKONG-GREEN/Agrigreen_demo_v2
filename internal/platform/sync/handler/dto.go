package handler

import (
	"encoding/json"
	"time"

	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func tsPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

type deviceReq struct {
	DeviceID   string `json:"device_id"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
}

type deviceDTO struct {
	DeviceID     string  `json:"device_id"`
	RegisteredAt string  `json:"registered_at"`
	Platform     string  `json:"platform"`
	AppVersion   string  `json:"app_version"`
	Revoked      bool    `json:"revoked"`
	ActorRef     *string `json:"actor_ref,omitempty"`
	OrgRef       *string `json:"org_ref,omitempty"`
	LastSeenAt   *string `json:"last_seen_at,omitempty"`
	RevokedAt    *string `json:"revoked_at,omitempty"`
	RevokeReason *string `json:"revoke_reason,omitempty"`
}

func toDeviceDTO(d domain.Device, admin bool) deviceDTO {
	out := deviceDTO{DeviceID: d.ID.String(), RegisteredAt: ts(d.RegisteredAt), Platform: d.Platform, AppVersion: d.AppVersion, Revoked: d.Revoked()}
	if admin {
		a := d.ActorRef.String()
		out.ActorRef = &a
		if d.OrgRef != nil {
			o := d.OrgRef.String()
			out.OrgRef = &o
		}
		out.LastSeenAt, out.RevokedAt, out.RevokeReason = tsPtr(d.LastSeenAt), tsPtr(d.RevokedAt), d.RevokeReason
	}
	return out
}

type changeDTO struct {
	Entity  string          `json:"entity"`
	ID      string          `json:"id"`
	Version int64           `json:"version"`
	Op      string          `json:"op"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type pullDTO struct {
	Changes    []changeDTO `json:"changes"`
	NextCursor string      `json:"next_cursor"`
	HasMore    bool        `json:"has_more"`
	ServerTime string      `json:"server_time"`
	Reset      bool        `json:"reset"`
}

func toPullDTO(r app.PullResult) pullDTO {
	out := pullDTO{Changes: make([]changeDTO, 0, len(r.Changes)), NextCursor: r.NextCursor, HasMore: r.HasMore,
		ServerTime: r.ServerTime.UTC().Format(time.RFC3339), Reset: r.Reset}
	for _, c := range r.Changes {
		d := changeDTO{Entity: c.Entity, ID: c.ID.String(), Version: c.Version, Op: c.Op}
		if c.Op != port.OpTombstone {
			d.Data = c.Data
		}
		out.Changes = append(out.Changes, d)
	}
	return out
}

type recordReq struct {
	ID               string          `json:"id"`
	IdemKey          string          `json:"idem_key"`
	Entity           string          `json:"entity"`
	Op               string          `json:"op"`
	EventTime        string          `json:"event_time"`
	SupersedesID     *string         `json:"supersedes_id"`
	CorrectionReason *string         `json:"correction_reason"`
	Payload          json.RawMessage `json:"payload"`
}

type pushReq struct {
	DeviceID string      `json:"device_id"`
	Records  []recordReq `json:"records"`
}

type resultDTO struct {
	ID         string               `json:"id"`
	Status     string               `json:"status"`
	ConflictID *string              `json:"conflict_id,omitempty"`
	Code       string               `json:"code,omitempty"`
	Errors     []problem.FieldError `json:"errors,omitempty"`
}

func toResultDTO(r app.ResultItem) resultDTO {
	out := resultDTO{ID: r.ID, Status: r.Status, Code: r.Code, Errors: r.Errors}
	if r.ConflictID != nil {
		s := r.ConflictID.String()
		out.ConflictID = &s
	}
	if r.Status == port.StatusRejected && out.Errors == nil {
		out.Errors = []problem.FieldError{}
	}
	return out
}

type gpsDTO struct {
	Lat       float64  `json:"lat" pii:"true"`
	Lon       float64  `json:"lon" pii:"true"`
	AccuracyM *float64 `json:"accuracy_m,omitempty"`
}

type fileDTO struct {
	FileRef          string  `json:"file_ref"`
	SubjectRef       string  `json:"subject_ref"`
	V                int     `json:"v"`
	Alg              string  `json:"alg"`
	Nonce            []byte  `json:"nonce"`
	WrappedDEK       []byte  `json:"wrapped_dek"`
	SHA256Ciphertext string  `json:"sha256_ciphertext"`
	Size             int64   `json:"size"`
	ContentType      string  `json:"content_type"`
	CapturedAt       *string `json:"captured_at" pii:"true"`
	GPS              *gpsDTO `json:"gps"`
	// Bộ ba R-DB-07 (W6.5); null ở tệp cũ và tệp tải trước khi biết thực thể.
	SubjectModule *string `json:"subject_module"`
	SubjectType   *string `json:"subject_type"`
	SubjectID     *string `json:"subject_id"`
	CreatedAt     string  `json:"created_at"`
}

func toFileDTO(f evport.File) fileDTO {
	out := fileDTO{FileRef: f.Ref.String(), SubjectRef: f.SubjectRef.String(), V: f.V, Alg: f.Alg, Nonce: f.Nonce,
		WrappedDEK: f.WrappedDEK, SHA256Ciphertext: f.SHA256Ciphertext, Size: f.Size, ContentType: f.ContentType,
		CapturedAt: tsPtr(f.CapturedAt), CreatedAt: ts(f.CreatedAt)}
	if f.GPSLat != nil && f.GPSLon != nil {
		out.GPS = &gpsDTO{Lat: *f.GPSLat, Lon: *f.GPSLon, AccuracyM: f.GPSAccuracyM}
	}
	if sub := f.Subject; sub != nil {
		id := sub.ID.String()
		out.SubjectModule, out.SubjectType, out.SubjectID = &sub.Module, &sub.Type, &id
	}
	return out
}

type conflictDTO struct {
	ConflictID       string  `json:"conflict_id"`
	Entity           string  `json:"entity"`
	FirstRecordID    string  `json:"first_record_id"`
	DeviceID         string  `json:"device_id"`
	ReportedBy       string  `json:"reported_by"`
	OwnerOrgRef      *string `json:"owner_org_ref"`
	OpenedAt         string  `json:"opened_at"`
	DueAt            string  `json:"due_at"`
	Status           string  `json:"status"`
	Overdue          bool    `json:"overdue"`
	ResolvedAt       *string `json:"resolved_at"`
	ResolvedBy       *string `json:"resolved_by"`
	ResolutionNote   *string `json:"resolution_note"`
	ResolvedRecordID *string `json:"resolved_record_id"`
	// SLAStatus: on_track | due_soon | overdue | resolved_on_time | resolved_late (US-SYNC-03).
	SLAStatus string `json:"sla_status"`
}

func uuidStr[T interface{ String() string }](v *T) *string {
	if v == nil {
		return nil
	}
	s := (*v).String()
	return &s
}

func toConflictDTO(c domain.Conflict, now time.Time) conflictDTO {
	return conflictDTO{
		ConflictID: c.ID.String(), Entity: c.Entity, FirstRecordID: c.FirstRecordID.String(), DeviceID: c.DeviceID.String(),
		ReportedBy: c.ReportedBy.String(), OwnerOrgRef: uuidStr(c.OwnerOrgRef), OpenedAt: ts(c.OpenedAt), DueAt: ts(c.DueAt),
		Status: c.Status, Overdue: c.Overdue(now), ResolvedAt: tsPtr(c.ResolvedAt), ResolvedBy: uuidStr(c.ResolvedBy),
		ResolutionNote: c.ResolutionNote, ResolvedRecordID: uuidStr(c.ResolvedRecordID), SLAStatus: app.SLAStatus(c, now),
	}
}

type conflictRecordDTO struct {
	RecordID      *string `json:"record_id"`
	RecordIdemKey string  `json:"record_idem_key"`
	Entity        string  `json:"entity"`
	Op            string  `json:"op"`
	Status        string  `json:"status"`
	DeviceID      string  `json:"device_id"`
	ActorRef      string  `json:"actor_ref"`
	EventTime     string  `json:"event_time"`
	SupersedesID  *string `json:"supersedes_id"`
	PayloadSHA256 *string `json:"payload_sha256"`
}

type conflictVersionDTO struct {
	RecordID  string          `json:"record_id"`
	Role      string          `json:"role"`
	Label     string          `json:"label"`
	Current   bool            `json:"current"`
	EventTime string          `json:"event_time"`
	Data      json.RawMessage `json:"data"`
}

type conflictDetailDTO struct {
	conflictDTO
	Resolution        *string              `json:"resolution"`
	Records           []conflictRecordDTO  `json:"records"`
	Versions          []conflictVersionDTO `json:"versions"`
	VersionsAvailable bool                 `json:"versions_available"`
}

func toConflictDetailDTO(d app.ConflictDetail, now time.Time) conflictDetailDTO {
	out := conflictDetailDTO{conflictDTO: toConflictDTO(d.Conflict, now), Records: []conflictRecordDTO{}, Versions: []conflictVersionDTO{},
		VersionsAvailable: d.VersionsAvailable}
	if r := app.ResolutionOf(d.Conflict, d.Records); r != "" {
		out.Resolution = &r
	}
	for _, r := range d.Records {
		out.Records = append(out.Records, conflictRecordDTO{RecordID: uuidStr(r.RecordID), RecordIdemKey: r.RecordIdemKey, Entity: r.Entity,
			Op: r.Op, Status: r.Status, DeviceID: r.DeviceID.String(), ActorRef: r.ActorRef.String(), EventTime: ts(r.EventTime),
			SupersedesID: uuidStr(r.RecordSupersedesID), PayloadSHA256: r.PayloadSHA256})
	}
	for _, v := range d.Versions {
		out.Versions = append(out.Versions, conflictVersionDTO{RecordID: v.RecordID.String(), Role: v.Role, Label: v.Label, Current: v.Current,
			EventTime: ts(v.EventTime), Data: v.Data})
	}
	return out
}
