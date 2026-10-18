package domain

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Loại báo cáo hiện trường G1.
const (
	KindSowing     = "sowing_progress" // FR-01: diện tích đã sạ theo giống × vụ
	KindGrowthPest = "growth_pest"     // FR-02: giai đoạn sinh trưởng, sâu bệnh, mức độ, diện tích nhiễm
)

// Mức độ nhiễm (FR-02).
var Severities = []string{"nhe", "trung_binh", "nang"}

var (
	periodRe = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)
	codeRe   = regexp.MustCompile(`^[A-Za-z0-9_]{1,40}$`)
)

// MaxAreaHa chặn số nhập nhầm đơn vị (m² thay ha).
var MaxAreaHa = decimal.NewFromInt(100000)

// Report là một phiên bản báo cáo hiện trường (dòng sổ field_report).
type Report struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	Kind             string
	CommuneCode      string
	ProvinceCode     string
	CooperativeRef   uuid.UUID
	Period           string
	SeasonCode       string
	VarietyCode      string
	SownAreaHa       *decimal.Decimal
	SownOn           *time.Time
	GrowthStage      string
	PestCode         string
	Severity         string
	AffectedAreaHa   *decimal.Decimal
	Urgent           bool
	Note             string
	Version          int
	PreviousID       uuid.UUID
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	EventTime        time.Time
	RecordedAt       time.Time
	ActorRef         uuid.UUID
	DeviceID         *string
	IdemKey          string
	Seq              int64
	VersionKey       string
}

// PeriodOf trả kỳ báo cáo (tháng, giờ Việt Nam) chứa thời điểm t.
func PeriodOf(t time.Time, zone *time.Location) string { return t.In(zone).Format("2006-01") }

// ValidPeriod kiểm định dạng kỳ YYYY-MM.
func ValidPeriod(p string) bool { return periodRe.MatchString(p) }

// ComputeVersionKey là khóa phiên bản: các lần gửi cùng khóa là phiên bản của
// cùng một số liệu (chỉ phiên bản mới nhất được cộng vào tổng hợp).
//   - gieo sạ: xã × HTX × kỳ × vụ × giống
//   - sâu bệnh: mỗi lần quan sát là một số liệu riêng (khóa = id bản đầu tiên)
func (r Report) ComputeVersionKey(firstID uuid.UUID) string {
	if r.Kind == KindSowing {
		coop := "-"
		if r.CooperativeRef != uuid.Nil {
			coop = r.CooperativeRef.String()
		}
		return strings.Join([]string{"sowing", r.CommuneCode, coop, r.Period, r.SeasonCode, r.VarietyCode}, ":")
	}
	return "pest:" + firstID.String()
}

// StreamKey của một khóa phiên bản.
func ReportStreamKey(versionKey string) string { return "report:" + versionKey }

// Validate kiểm giá trị của báo cáo (không gồm danh mục — app/ tra masterdata).
// now dùng để chặn kỳ/ngày ở tương lai.
func (r *Report) Validate(now time.Time, zone *time.Location) error {
	r.CommuneCode = strings.TrimSpace(r.CommuneCode)
	r.Note = strings.TrimSpace(r.Note)
	switch {
	case r.CommuneCode == "":
		return fieldErr("commune_code", "bắt buộc")
	case !ValidPeriod(r.Period):
		return fieldErr("period", "định dạng YYYY-MM")
	case r.Period > PeriodOf(now, zone):
		return fieldErr("period", "không báo cáo cho kỳ tương lai")
	case len([]rune(r.Note)) > 2000:
		return fieldErr("note", "tối đa 2000 ký tự")
	case ContainsPII(r.Note):
		return FieldError{Field: "note", Err: ErrPIIInText}
	}
	switch r.Kind {
	case KindSowing:
		if r.CooperativeRef == uuid.Nil {
			return fieldErr("cooperative_ref", "bắt buộc với báo cáo gieo sạ (FR-01)")
		}
		if !codeOK(r.SeasonCode) {
			return fieldErr("season_code", "bắt buộc, mã vụ theo danh mục")
		}
		if strings.TrimSpace(r.VarietyCode) == "" {
			return fieldErr("variety_code", "bắt buộc, chọn từ danh mục giống (BR-01)")
		}
		if err := areaOK("sown_area_ha", r.SownAreaHa, true); err != nil {
			return err
		}
		if r.SownOn != nil && r.SownOn.After(now) {
			return fieldErr("sown_on", "không ở tương lai")
		}
		r.GrowthStage, r.PestCode, r.Severity, r.AffectedAreaHa = "", "", "", nil
	case KindGrowthPest:
		if !codeOK(r.GrowthStage) {
			return fieldErr("growth_stage", "bắt buộc, mã giai đoạn [A-Za-z0-9_]")
		}
		if strings.TrimSpace(r.PestCode) == "" {
			return fieldErr("pest_code", "bắt buộc, chọn từ danh mục sâu bệnh")
		}
		if !contains(Severities, r.Severity) {
			return fieldErr("severity", "phải là nhe, trung_binh hoặc nang")
		}
		if r.AffectedAreaHa != nil {
			if err := areaOK("affected_area_ha", r.AffectedAreaHa, false); err != nil {
				return err
			}
		}
		r.SeasonCode, r.VarietyCode, r.SownAreaHa, r.SownOn = "", "", nil, nil
	default:
		return fieldErr("kind", "phải là sowing_progress hoặc growth_pest")
	}
	return nil
}

func codeOK(s string) bool { return codeRe.MatchString(s) }

func areaOK(field string, d *decimal.Decimal, positive bool) error {
	switch {
	case d == nil:
		return fieldErr(field, "bắt buộc (ha, số thập phân)")
	case d.IsNegative() || (positive && d.IsZero()):
		return fieldErr(field, "phải lớn hơn 0")
	case d.GreaterThan(MaxAreaHa):
		return fieldErr(field, "vượt giới hạn hợp lý (ha)")
	case d.Exponent() < -4:
		return fieldErr(field, "tối đa 4 chữ số thập phân")
	}
	return nil
}

// ValidReason kiểm lý do bản ghi bù (≥ 10 ký tự).
func ValidReason(s *string) bool {
	return s != nil && len([]rune(strings.TrimSpace(*s))) >= 10
}
