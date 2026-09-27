package domain

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// GeoPoint là toạ độ WGS84 lúc thu. Dữ liệu cá nhân khi gắn với hộ
// (docs/dev/envelope-format.md) — không đưa vào log hay thông báo lỗi.
type GeoPoint struct {
	Lat       decimal.Decimal  `pii:"true"`
	Lon       decimal.Decimal  `pii:"true"`
	AccuracyM *decimal.Decimal // sai số GPS (m)
}

// Valid kiểm phạm vi toạ độ.
func (g GeoPoint) Valid() bool {
	return g.Lat.GreaterThanOrEqual(decimal.NewFromInt(-90)) && g.Lat.LessThanOrEqual(decimal.NewFromInt(90)) &&
		g.Lon.GreaterThanOrEqual(decimal.NewFromInt(-180)) && g.Lon.LessThanOrEqual(decimal.NewFromInt(180)) &&
		(g.AccuracyM == nil || (!g.AccuracyM.IsNegative() && g.AccuracyM.LessThan(decimal.NewFromInt(1000000))))
}

// FileRef tham chiếu một tệp minh chứng đã mã hóa phong bì do module tệp lưu
// (ADR-0004). pf/mrv không bao giờ thấy bản rõ; sha256 là băm ciphertext.
type FileRef struct {
	FileRef uuid.UUID `json:"file_ref"`
	SHA256  string    `json:"sha256"`
}

// Evidence là một phiên bản của một minh chứng (sổ pf_mrv.evidence).
type Evidence struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	RootID           uuid.UUID // bản gốc của chuỗi sửa sai
	ChainVersion     int       // 1 = bản gốc; bản ghi bù tăng dần
	EvidenceType     string
	CatalogVersion   int // phiên bản danh mục đã ghim lúc thu
	AdminUnitCode    string
	PlotRef          *uuid.UUID
	HouseholdRef     *uuid.UUID
	SeasonCode       *string
	Values           map[string]json.RawMessage
	FileRefs         []FileRef
	GPS              *GeoPoint
	EventTime        time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID // người thu
	DeviceID         *string
	IdemKey          string
	Seq              int64
	RecordedAt       time.Time
	RowHash          []byte
}

// StreamKey của sổ minh chứng: một chuỗi băm cho mỗi xã.
func StreamKey(adminUnitCode string) string { return "unit:" + adminUnitCode }

var (
	unitCodeRe = regexp.MustCompile(`^[0-9A-Z][0-9A-Z.-]{0,39}$`)
	sha256Re   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ValidUnitCode kiểm mã đơn vị hành chính.
func ValidUnitCode(s string) bool { return unitCodeRe.MatchString(s) }

// Giới hạn bản ghi.
const (
	MaxFileRefs       = 20
	maxClockSkew      = 10 * time.Minute
	MinCorrectionLen  = 10
	maxCorrectionLen  = 1000
	maxSeasonCodeLen  = 40
	maxEventTimeYears = 5
)

// ValidateEnvelope kiểm phần không phụ thuộc danh mục của bản ghi. now là thời
// điểm máy chủ (clock.Clock ở app/).
func (e Evidence) ValidateEnvelope(now time.Time) []problem.FieldError {
	var errs []problem.FieldError
	add := func(f, r string) { errs = append(errs, problem.FieldError{Field: f, Reason: r}) }
	if e.ID == uuid.Nil || e.ID.Version() != 7 {
		add("id", "phải là UUIDv7")
	}
	if !evidenceTypeRe.MatchString(e.EvidenceType) {
		add("evidence_type", "3–40 ký tự a-z, 0-9, _")
	}
	if !unitCodeRe.MatchString(e.AdminUnitCode) {
		add("admin_unit_code", "mã đơn vị hành chính không hợp lệ")
	}
	if e.SeasonCode != nil {
		if n := utf8.RuneCountInString(*e.SeasonCode); n < 1 || n > maxSeasonCodeLen {
			add("season_code", "1–40 ký tự")
		}
	}
	switch {
	case e.EventTime.IsZero():
		add("event_time", "bắt buộc")
	case e.EventTime.After(now.Add(maxClockSkew)):
		add("event_time", "không được ở tương lai")
	case e.EventTime.Before(now.AddDate(-maxEventTimeYears, 0, 0)):
		add("event_time", "quá xa trong quá khứ")
	}
	if len(e.FileRefs) > MaxFileRefs {
		add("file_refs", "tối đa 20 tệp")
	}
	seen := map[uuid.UUID]bool{}
	for _, f := range e.FileRefs {
		if f.FileRef == uuid.Nil || seen[f.FileRef] {
			add("file_refs", "file_ref rỗng hoặc trùng")
		}
		seen[f.FileRef] = true
		if !sha256Re.MatchString(f.SHA256) {
			add("file_refs", "sha256 phải là 64 ký tự hex thường (băm ciphertext)")
		}
	}
	if e.GPS != nil && !e.GPS.Valid() {
		// Không đưa giá trị toạ độ vào thông báo lỗi (R-14).
		add("gps", "toạ độ ngoài phạm vi hợp lệ")
	}
	if e.DeviceID != nil && (len(*e.DeviceID) == 0 || len(*e.DeviceID) > 100) {
		add("device_id", "1–100 ký tự")
	}
	if (e.SupersedesID == nil) != (e.CorrectionReason == nil) {
		add("correction_reason", "bản ghi bù cần cả supersedes_id và correction_reason")
	}
	if e.CorrectionReason != nil {
		if n := utf8.RuneCountInString(strings.TrimSpace(*e.CorrectionReason)); n < MinCorrectionLen || n > maxCorrectionLen {
			add("correction_reason", "10–1000 ký tự")
		}
	}
	return errs
}

// Subject trả phần được kiểm theo danh mục.
func (e Evidence) Subject() Subject {
	return Subject{Values: e.Values, PlotRef: e.PlotRef, SeasonCode: e.SeasonCode, GPS: e.GPS, FileRefs: e.FileRefs}
}

// ReviewStatus là trạng thái duyệt của bản hiện hành.
type ReviewStatus string

// Các trạng thái duyệt.
const (
	ReviewPending  ReviewStatus = "pending"
	ReviewAccepted ReviewStatus = "accepted" // "Đạt chuẩn"
	ReviewRejected ReviewStatus = "rejected" // "Yêu cầu kiểm tra lại"
)

// Current là bảng chiếu bản hiện hành của một chuỗi minh chứng.
type Current struct {
	RootID        uuid.UUID
	HeadID        uuid.UUID
	ChainVersion  int
	EvidenceType  string
	AdminUnitCode string
	CapturedBy    uuid.UUID
	ReviewStatus  ReviewStatus
	ReviewID      *uuid.UUID
}

// Review là một quyết định duyệt (sổ pf_mrv.evidence_review).
type Review struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	EvidenceID uuid.UUID
	RootID     uuid.UUID
	Decision   ReviewStatus // accepted | rejected
	Reason     string
	EventTime  time.Time
	ActorRef   uuid.UUID // người duyệt
	IdemKey    string
	Seq        int64
	RecordedAt time.Time
}

// ValidateReview kiểm quyết định duyệt.
func ValidateReview(decision ReviewStatus, reason string) []problem.FieldError {
	var errs []problem.FieldError
	if decision != ReviewAccepted && decision != ReviewRejected {
		errs = append(errs, problem.FieldError{Field: "decision", Reason: "phải là accepted hoặc rejected"})
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(reason)); n < 5 || n > 1000 {
		errs = append(errs, problem.FieldError{Field: "reason", Reason: "bắt buộc, 5–1000 ký tự"})
	}
	return errs
}
