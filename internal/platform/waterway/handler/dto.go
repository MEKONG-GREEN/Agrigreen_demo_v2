package handler

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func fmtTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := fmtTime(*t)
	return &s
}

func decPtr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func uuidPtr(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

// ---------------------------------------------------------------------------
// Hình học

type lineGeo struct {
	Type        string       `json:"type"`
	Coordinates [][2]float64 `json:"coordinates"`
}

func toLine(pts []domain.Point) lineGeo {
	g := lineGeo{Type: "LineString", Coordinates: make([][2]float64, len(pts))}
	for i, p := range pts {
		g.Coordinates[i] = [2]float64{p.Lng, p.Lat}
	}
	return g
}

func latLngLine(pts []port.LatLng) lineGeo {
	g := lineGeo{Type: "LineString", Coordinates: make([][2]float64, len(pts))}
	for i, p := range pts {
		g.Coordinates[i] = [2]float64{p.Lng, p.Lat}
	}
	return g
}

type location struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// ---------------------------------------------------------------------------
// Số đo: nhận số JSON hoặc chuỗi; sai định dạng → 422 với thông báo US-WATER-02 AC-2.

func parseNum(raw json.RawMessage, field string, errs *[]problem.FieldError) *decimal.Decimal {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, `"`) {
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			*errs = append(*errs, problem.FieldError{Field: field, Reason: domain.MsgPositiveNumbers})
			return nil
		}
		s = strings.TrimSpace(str)
		if s == "" {
			return nil
		}
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		*errs = append(*errs, problem.FieldError{Field: field, Reason: domain.MsgPositiveNumbers})
		return nil
	}
	return &d
}

type segmentReq struct {
	Name          string          `json:"name"`
	WaterwayClass string          `json:"waterway_class"`
	Status        string          `json:"status"`
	Geometry      *lineGeo        `json:"geometry"`
	WidthM        json.RawMessage `json:"width_m"`
	DepthM        json.RawMessage `json:"depth_m"`
	MaxLoadTons   json.RawMessage `json:"max_load_tons"`
	DataSource    string          `json:"data_source"`
	Note          *string         `json:"note"`
	EventTime     *time.Time      `json:"event_time"`
	// Chỉ cho bản sửa.
	ExpectedRevision int    `json:"expected_revision"`
	Reason           string `json:"reason"`
	// LengthM bị bỏ qua có chủ đích: chiều dài do hệ thống tính (US-WATER-02 AC-1).
	LengthM json.RawMessage `json:"length_m"`
}

func (r segmentReq) fields() (app.SegmentFields, []problem.FieldError) {
	var errs []problem.FieldError
	f := app.SegmentFields{Name: r.Name, Class: r.WaterwayClass, Status: r.Status, DataSource: r.DataSource, Note: r.Note, EventTime: r.EventTime}
	f.WidthM = parseNum(r.WidthM, "width_m", &errs)
	f.DepthM = parseNum(r.DepthM, "depth_m", &errs)
	f.MaxLoadTons = parseNum(r.MaxLoadTons, "max_load_tons", &errs)
	switch {
	case r.Geometry == nil:
		errs = append(errs, problem.FieldError{Field: "geometry", Reason: "bắt buộc (GeoJSON LineString)"})
	case r.Geometry.Type != "LineString":
		errs = append(errs, problem.FieldError{Field: "geometry", Reason: "phải là GeoJSON LineString"})
	default:
		for _, c := range r.Geometry.Coordinates {
			f.Points = append(f.Points, domain.Point{Lng: c[0], Lat: c[1]})
		}
	}
	return f, errs
}

type segmentDTO struct {
	SegmentRef       string  `json:"segment_ref"`
	ID               string  `json:"id"`
	Revision         int     `json:"revision"`
	Name             string  `json:"name"`
	WaterwayClass    string  `json:"waterway_class"`
	Status           string  `json:"status"`
	Geometry         lineGeo `json:"geometry"`
	LengthM          string  `json:"length_m"`
	WidthM           *string `json:"width_m"`
	DepthM           *string `json:"depth_m"`
	MaxLoadTons      *string `json:"max_load_tons"`
	DataSource       string  `json:"data_source"`
	Note             *string `json:"note"`
	EventTime        string  `json:"event_time"`
	RecordedAt       string  `json:"recorded_at"`
	SupersedesID     *string `json:"supersedes_id"`
	CorrectionReason *string `json:"correction_reason"`
	ActorRef         string  `json:"actor_ref"`
	Seq              int64   `json:"seq"`
}

func toSegment(s domain.Segment) segmentDTO {
	return segmentDTO{
		SegmentRef: s.SegmentRef.String(), ID: s.ID.String(), Revision: s.Revision, Name: s.Name, WaterwayClass: s.Class,
		Status: s.Status, Geometry: toLine(s.Points), LengthM: s.LengthM.StringFixed(2), WidthM: decPtr(s.WidthM),
		DepthM: decPtr(s.DepthM), MaxLoadTons: decPtr(s.MaxLoadTons), DataSource: s.DataSource, Note: s.Note,
		EventTime: fmtTime(s.EventTime), RecordedAt: fmtTime(s.RecordedAt), SupersedesID: uuidPtr(s.SupersedesID),
		CorrectionReason: s.CorrectionReason, ActorRef: s.ActorRef.String(), Seq: s.Seq,
	}
}

type structureReq struct {
	Kind             string          `json:"kind"`
	Name             string          `json:"name"`
	Status           string          `json:"status"`
	SegmentRef       string          `json:"segment_ref"`
	Location         *location       `json:"location"`
	ClearanceHeightM json.RawMessage `json:"clearance_height_m"`
	ClearanceWidthM  json.RawMessage `json:"clearance_width_m"`
	DepthM           json.RawMessage `json:"depth_m"`
	SurveyDate       string          `json:"survey_date"`
	DataSource       string          `json:"data_source"`
	Note             *string         `json:"note"`
	EventTime        *time.Time      `json:"event_time"`
	ExpectedRevision int             `json:"expected_revision"`
	Reason           string          `json:"reason"`
}

func (r structureReq) fields() (app.StructureFields, []problem.FieldError) {
	var errs []problem.FieldError
	f := app.StructureFields{Kind: r.Kind, Name: r.Name, Status: r.Status, DataSource: r.DataSource, Note: r.Note, EventTime: r.EventTime}
	if ref, err := uuid.Parse(r.SegmentRef); err == nil {
		f.SegmentRef = ref
	} else {
		errs = append(errs, problem.FieldError{Field: "segment_ref", Reason: "bắt buộc (uuid)"})
	}
	if r.Location == nil {
		errs = append(errs, problem.FieldError{Field: "location", Reason: "bắt buộc {lat, lng}"})
	} else {
		f.At = domain.Point{Lng: r.Location.Lng, Lat: r.Location.Lat}
	}
	f.ClearanceHeightM = parseNum(r.ClearanceHeightM, "clearance_height_m", &errs)
	f.ClearanceWidthM = parseNum(r.ClearanceWidthM, "clearance_width_m", &errs)
	f.DepthM = parseNum(r.DepthM, "depth_m", &errs)
	if r.SurveyDate != "" {
		d, err := time.Parse(time.DateOnly, r.SurveyDate)
		if err != nil {
			errs = append(errs, problem.FieldError{Field: "survey_date", Reason: "định dạng YYYY-MM-DD"})
		} else {
			f.SurveyDate = &d
		}
	}
	return f, errs
}

type structureDTO struct {
	StructureRef     string   `json:"structure_ref"`
	ID               string   `json:"id"`
	Revision         int      `json:"revision"`
	Kind             string   `json:"kind"`
	Name             string   `json:"name"`
	Status           string   `json:"status"`
	SegmentRef       string   `json:"segment_ref"`
	Location         location `json:"location"`
	ClearanceHeightM *string  `json:"clearance_height_m"`
	ClearanceWidthM  *string  `json:"clearance_width_m"`
	DepthM           *string  `json:"depth_m"`
	SurveyDate       *string  `json:"survey_date"`
	DataSource       string   `json:"data_source"`
	Note             *string  `json:"note"`
	EventTime        string   `json:"event_time"`
	RecordedAt       string   `json:"recorded_at"`
	SupersedesID     *string  `json:"supersedes_id"`
	CorrectionReason *string  `json:"correction_reason"`
	ActorRef         string   `json:"actor_ref"`
}

func toStructure(s domain.Structure) structureDTO {
	var survey *string
	if s.SurveyDate != nil {
		d := s.SurveyDate.Format(time.DateOnly)
		survey = &d
	}
	return structureDTO{
		StructureRef: s.StructureRef.String(), ID: s.ID.String(), Revision: s.Revision, Kind: s.Kind, Name: s.Name, Status: s.Status,
		SegmentRef: s.SegmentRef.String(), Location: location{Lat: s.At.Lat, Lng: s.At.Lng}, ClearanceHeightM: decPtr(s.ClearanceHeightM),
		ClearanceWidthM: decPtr(s.ClearanceWidthM), DepthM: decPtr(s.DepthM), SurveyDate: survey, DataSource: s.DataSource, Note: s.Note,
		EventTime: fmtTime(s.EventTime), RecordedAt: fmtTime(s.RecordedAt), SupersedesID: uuidPtr(s.SupersedesID),
		CorrectionReason: s.CorrectionReason, ActorRef: s.ActorRef.String(),
	}
}

// ---------------------------------------------------------------------------
// Dựng và phiên bản

type buildDTO struct {
	ID               string             `json:"id"`
	Status           string             `json:"status"`
	RequestedBy      string             `json:"requested_by"`
	RequestedAt      string             `json:"requested_at"`
	NetworkVersionNo *int               `json:"network_version_no"`
	Reused           *bool              `json:"reused"`
	ErrorCode        *string            `json:"error_code"`
	CompletedAt      *string            `json:"completed_at"`
	Params           domain.BuildParams `json:"params"`
}

func toBuild(j domain.BuildJob) buildDTO {
	return buildDTO{ID: j.ID.String(), Status: j.Status, RequestedBy: j.RequestedBy.String(), RequestedAt: fmtTime(j.RequestedAt),
		NetworkVersionNo: j.VersionNo, Reused: j.Reused, ErrorCode: j.ErrorCode, CompletedAt: fmtTimePtr(j.CompletedAt), Params: j.Params}
}

type versionDTO struct {
	ID               string             `json:"id"`
	VersionNo        int                `json:"version_no"`
	Status           string             `json:"status"`
	SegmentSetSHA256 string             `json:"segment_set_sha256"`
	GraphSHA256      string             `json:"graph_sha256"`
	RuleVersion      string             `json:"rule_version"`
	AsOf             string             `json:"as_of"`
	SegmentCount     int                `json:"segment_count"`
	StructureCount   int                `json:"structure_count"`
	VertexCount      int                `json:"vertex_count"`
	EdgeCount        int                `json:"edge_count"`
	ComponentCount   int                `json:"component_count"`
	FindingCount     int                `json:"finding_count"`
	BuildID          string             `json:"build_id"`
	BuiltBy          string             `json:"built_by"`
	BuiltAt          string             `json:"built_at"`
	PublishedBy      *string            `json:"published_by"`
	PublishedAt      *string            `json:"published_at"`
	EffectiveFrom    *string            `json:"effective_from"`
	SupersededAt     *string            `json:"superseded_at"`
	Params           domain.BuildParams `json:"params"`
}

func toVersion(v domain.NetworkVersion) versionDTO {
	return versionDTO{
		ID: v.ID.String(), VersionNo: v.VersionNo, Status: v.Status, SegmentSetSHA256: v.SegmentSetSHA256, GraphSHA256: v.GraphSHA256,
		RuleVersion: v.RuleVersion, AsOf: fmtTime(v.AsOf), SegmentCount: v.SegmentCount, StructureCount: v.StructureCount,
		VertexCount: v.VertexCount, EdgeCount: v.EdgeCount, ComponentCount: v.ComponentCount, FindingCount: v.FindingCount,
		BuildID: v.BuildJobID.String(), BuiltBy: v.BuiltBy.String(), BuiltAt: fmtTime(v.BuiltAt), PublishedBy: uuidPtr(v.PublishedBy),
		PublishedAt: fmtTimePtr(v.PublishedAt), EffectiveFrom: fmtTimePtr(v.EffectiveFrom), SupersededAt: fmtTimePtr(v.SupersededAt),
		Params: v.Params,
	}
}

type findingDTO struct {
	ID          int64    `json:"id"`
	Kind        string   `json:"kind"`
	SegmentRefs []string `json:"segment_refs"`
	Location    location `json:"location"`
	GapM        *string  `json:"gap_m"`
	AutoFixed   bool     `json:"auto_fixed"`
	Message     string   `json:"message"`
}

func toFinding(f store.FindingRow) findingDTO {
	refs := make([]string, len(f.SegmentRefs))
	for i, r := range f.SegmentRefs {
		refs[i] = r.String()
	}
	return findingDTO{ID: f.ID, Kind: f.Kind, SegmentRefs: refs, Location: location{Lat: f.At.Lat, Lng: f.At.Lng}, GapM: f.GapM,
		AutoFixed: f.AutoFixed, Message: f.Message}
}

// ---------------------------------------------------------------------------
// Application API

type layerSegmentDTO struct {
	SegmentRef          string  `json:"segment_ref"`
	Revision            int     `json:"revision"`
	Name                string  `json:"name"`
	WaterwayClass       string  `json:"waterway_class"`
	DataSource          string  `json:"data_source"`
	LengthM             string  `json:"length_m"`
	WidthM              *string `json:"width_m"`
	DepthM              *string `json:"depth_m"`
	DeclaredMaxLoadTons *string `json:"declared_max_load_tons"`
	MaxLoadTons         *string `json:"max_load_tons"`
	Certain             bool    `json:"certain"`
	Limiting            string  `json:"limiting"`
	LimitingStructure   *string `json:"limiting_structure_ref"`
	Passable            bool    `json:"passable"`
	Geometry            lineGeo `json:"geometry"`
}

func toLayerSegment(s app.SegmentView) layerSegmentDTO {
	return layerSegmentDTO{
		SegmentRef: s.SegmentRef.String(), Revision: s.Revision, Name: s.Name, WaterwayClass: s.Class, DataSource: s.DataSource,
		LengthM: s.LengthM.StringFixed(2), WidthM: decPtr(s.WidthM), DepthM: decPtr(s.DepthM), DeclaredMaxLoadTons: decPtr(s.DeclaredTons),
		MaxLoadTons: decPtr(s.MaxLoadTons), Certain: s.Certain, Limiting: s.Limiting, LimitingStructure: uuidPtr(s.StructureRef),
		Passable: s.Passable, Geometry: toLine(s.Points),
	}
}

// SourceLabels là nhãn nguồn tính hiển thị (Simulation FN-04 BR-04).
var SourceLabels = map[string]string{
	port.SourceRouted:    "Định tuyến thực tế",
	port.SourceDigitised: "Định tuyến trên tuyến số hóa nội bộ",
	port.SourceHaversine: "Haversine hiệu chỉnh",
}

type limitingDTO struct {
	MaxLoadTons  *string `json:"max_load_tons"`
	Certain      bool    `json:"certain"`
	Reason       string  `json:"reason"`
	SegmentRef   string  `json:"segment_ref"`
	StructureRef *string `json:"structure_ref"`
}

func toLimiting(l *port.Limiting) *limitingDTO {
	if l == nil {
		return nil
	}
	return &limitingDTO{MaxLoadTons: decPtr(l.MaxLoadTons), Certain: l.Certain, Reason: l.Reason, SegmentRef: l.SegmentRef.String(),
		StructureRef: uuidPtr(l.StructureRef)}
}

type routeDTO struct {
	VersionNo   *int         `json:"version_no"`
	Found       bool         `json:"found"`
	DistanceKm  string       `json:"distance_km"`
	AccessKm    string       `json:"access_km"`
	Source      string       `json:"source"`
	SourceLabel string       `json:"source_label"`
	Geometry    *lineGeo     `json:"geometry"`
	SegmentRefs []string     `json:"segment_refs"`
	Limiting    *limitingDTO `json:"limiting"`
	Reason      *string      `json:"reason"`
}

func toRoute(r port.Route) routeDTO {
	out := routeDTO{Found: r.Found, DistanceKm: r.DistanceKm.StringFixed(3), Source: r.Source, SourceLabel: SourceLabels[r.Source],
		Limiting: toLimiting(r.Limiting), SegmentRefs: []string{}, AccessKm: "0.000"}
	if r.VersionNo > 0 {
		n := r.VersionNo
		out.VersionNo = &n
	}
	if len(r.Legs) == 1 {
		l := r.Legs[0]
		out.AccessKm = l.AccessKm.StringFixed(3)
		if l.Found {
			g := latLngLine(l.Path)
			out.Geometry = &g
		}
		for _, s := range l.SegmentRefs {
			out.SegmentRefs = append(out.SegmentRefs, s.String())
		}
		if l.Reason != "" {
			reason := l.Reason
			out.Reason = &reason
		}
	}
	return out
}
