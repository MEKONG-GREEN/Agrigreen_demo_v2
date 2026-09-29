// Package domain: quy tắc của pf/waterway — đoạn sông/kênh, công trình vượt
// sông, quy tắc phương tiện (suy tải trọng), dựng topology tất định và định
// tuyến phụ trợ. Không phụ thuộc hạ tầng; không time.Now() (R-12).
package domain

import (
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Trạng thái đoạn. Chỉ 'confirmed' vào đồ thị (FN-20 BR-04).
const (
	StatusDraft     = "draft"
	StatusConfirmed = "confirmed"
	StatusRetired   = "retired"
)

// Trạng thái công trình.
const (
	StructureActive  = "active"
	StructureRetired = "retired"
)

// Nguồn dữ liệu của đoạn/công trình.
const (
	SourceDigitised = "digitised"
	SourceOfficial  = "official"
	SourceSurvey    = "survey"
	SourceOSM       = "osm"
)

// Thông báo kiểm dữ liệu theo đúng AC-2 của GIS US-WATER-02/03.
const (
	MsgNameClassRequired = "Vui lòng nhập đầy đủ Tên tuyến và Phân loại."
	MsgPositiveNumbers   = "Chiều rộng, độ sâu và tải trọng tối đa phải là số lớn hơn 0."
)

// Point là một điểm WGS84 (kinh độ, vĩ độ).
type Point struct {
	Lng float64
	Lat float64
}

// Segment là một bản sửa của đoạn sông/kênh.
type Segment struct {
	ID               uuid.UUID
	SegmentRef       uuid.UUID
	Revision         int
	Name             string
	Class            string
	Status           string
	Points           []Point
	LengthM          decimal.Decimal // CSDL tính
	WidthM           *decimal.Decimal
	DepthM           *decimal.Decimal
	MaxLoadTons      *decimal.Decimal // khai báo
	DataSource       string
	Note             *string
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	Seq              int64
	RowHash          string
}

// Structure là một bản sửa của công trình vượt sông.
type Structure struct {
	ID               uuid.UUID
	StructureRef     uuid.UUID
	Revision         int
	Kind             string
	Name             string
	Status           string
	SegmentRef       uuid.UUID
	At               Point
	ClearanceHeightM *decimal.Decimal
	ClearanceWidthM  *decimal.Decimal
	DepthM           *decimal.Decimal
	SurveyDate       *time.Time
	DataSource       string
	Note             *string
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	Seq              int64
	RowHash          string
}

// ValidClass cho biết phân loại hợp lệ (GIS BR-35 + sông).
func ValidClass(c string) bool {
	switch c {
	case port.ClassMainCanal, port.ClassFieldCanal, port.ClassDitch, port.ClassRiver:
		return true
	}
	return false
}

// ValidSource cho biết nguồn dữ liệu hợp lệ.
func ValidSource(s string) bool {
	switch s {
	case SourceDigitised, SourceOfficial, SourceSurvey, SourceOSM:
		return true
	}
	return false
}

// ValidStructureKind cho biết loại công trình hợp lệ.
func ValidStructureKind(k string) bool {
	switch k {
	case port.StructureBridge, port.StructureCulvert, port.StructureLock, port.StructurePowerLine:
		return true
	}
	return false
}

// ValidatePoints kiểm hình học tuyến: ≥ 2 đỉnh phân biệt, WGS84 hợp lệ.
func ValidatePoints(field string, pts []Point) []problem.FieldError {
	if len(pts) < 2 {
		return []problem.FieldError{{Field: field, Reason: "Tuyến phải có tối thiểu 2 đỉnh."}}
	}
	for _, p := range pts {
		if math.IsNaN(p.Lat) || math.IsNaN(p.Lng) || math.IsInf(p.Lat, 0) || math.IsInf(p.Lng, 0) ||
			math.Abs(p.Lat) > 90 || math.Abs(p.Lng) > 180 {
			return []problem.FieldError{{Field: field, Reason: "Toạ độ không hợp lệ (phải theo hệ WGS84 / EPSG:4326)."}}
		}
	}
	distinct := 0
	for i := 1; i < len(pts); i++ {
		if pts[i] != pts[i-1] {
			distinct++
		}
	}
	if distinct == 0 {
		return []problem.FieldError{{Field: field, Reason: "Tuyến phải có tối thiểu 2 đỉnh phân biệt."}}
	}
	return nil
}

// PositiveOrNil kiểm số đo > 0 (nếu có) — US-WATER-02 AC-2.
func PositiveOrNil(field string, d *decimal.Decimal) []problem.FieldError {
	if d != nil && !d.IsPositive() {
		return []problem.FieldError{{Field: field, Reason: MsgPositiveNumbers}}
	}
	return nil
}

// ValidateSegment kiểm một bản sửa đoạn trước khi ghi (GIS US-WATER-02/03 AC-2).
func ValidateSegment(s Segment) []problem.FieldError {
	var errs []problem.FieldError
	if trimmed(s.Name) == "" || s.Class == "" {
		if trimmed(s.Name) == "" {
			errs = append(errs, problem.FieldError{Field: "name", Reason: MsgNameClassRequired})
		}
		if s.Class == "" {
			errs = append(errs, problem.FieldError{Field: "waterway_class", Reason: MsgNameClassRequired})
		}
	} else if !ValidClass(s.Class) {
		errs = append(errs, problem.FieldError{Field: "waterway_class", Reason: "Phân loại phải là kenh_chinh, noi_dong, muong hoặc song."})
	}
	if len([]rune(s.Name)) > 200 {
		errs = append(errs, problem.FieldError{Field: "name", Reason: "tối đa 200 ký tự"})
	}
	errs = append(errs, PositiveOrNil("width_m", s.WidthM)...)
	errs = append(errs, PositiveOrNil("depth_m", s.DepthM)...)
	errs = append(errs, PositiveOrNil("max_load_tons", s.MaxLoadTons)...)
	errs = append(errs, ValidatePoints("geometry", s.Points)...)
	switch s.Status {
	case StatusDraft, StatusConfirmed, StatusRetired:
	default:
		errs = append(errs, problem.FieldError{Field: "status", Reason: "phải là draft, confirmed hoặc retired"})
	}
	if !ValidSource(s.DataSource) {
		errs = append(errs, problem.FieldError{Field: "data_source", Reason: "phải là digitised, official, survey hoặc osm"})
	}
	if s.Note != nil && len([]rune(*s.Note)) > 1000 {
		errs = append(errs, problem.FieldError{Field: "note", Reason: "tối đa 1000 ký tự"})
	}
	return errs
}

// ValidateStructure kiểm một bản sửa công trình.
func ValidateStructure(s Structure) []problem.FieldError {
	var errs []problem.FieldError
	if trimmed(s.Name) == "" {
		errs = append(errs, problem.FieldError{Field: "name", Reason: "Công trình phải có tên."})
	}
	if !ValidStructureKind(s.Kind) {
		errs = append(errs, problem.FieldError{Field: "kind", Reason: "phải là cau, cong, au_thuyen hoặc duong_day_dien"})
	}
	if s.SegmentRef == uuid.Nil {
		errs = append(errs, problem.FieldError{Field: "segment_ref", Reason: "bắt buộc"})
	}
	errs = append(errs, PositiveOrNil("clearance_height_m", s.ClearanceHeightM)...)
	errs = append(errs, PositiveOrNil("clearance_width_m", s.ClearanceWidthM)...)
	errs = append(errs, PositiveOrNil("depth_m", s.DepthM)...)
	if math.Abs(s.At.Lat) > 90 || math.Abs(s.At.Lng) > 180 || math.IsNaN(s.At.Lat) || math.IsNaN(s.At.Lng) {
		errs = append(errs, problem.FieldError{Field: "location", Reason: "Toạ độ không hợp lệ (WGS84)."})
	}
	switch s.Status {
	case StructureActive, StructureRetired:
	default:
		errs = append(errs, problem.FieldError{Field: "status", Reason: "phải là active hoặc retired"})
	}
	if !ValidSource(s.DataSource) {
		errs = append(errs, problem.FieldError{Field: "data_source", Reason: "phải là digitised, official, survey hoặc osm"})
	}
	return errs
}

// ValidReason kiểm lý do sửa (bản ghi bù, RB-1).
func ValidReason(r string) bool { return len([]rune(trimmed(r))) >= 5 && len([]rune(r)) <= 500 }

func trimmed(s string) string { return strings.TrimSpace(s) }
