// Package domain: quy tắc thuần của pf/evidence — phong bì tệp, khóa đối tượng, bộ ba chủ thể, mục đích,
// quyết định phạm vi khi mở tệp.
package domain

import (
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// MaxClockSkew: captured_at không được vượt quá now + 5 phút (như event_time của pf/sync).
const MaxClockSkew = 5 * time.Minute

// RuleVersion của sổ truy cập pf_files.access_log.
const RuleVersion = "evidence.access/v1"

// Purposes là mục đích được phép đọc tệp minh chứng (R-42).
var Purposes = map[string]bool{string(client.PurposeMRVEvidence): true, string(client.PurposeFieldSupport): true}

// Thao tác và kết quả quyết định mở tệp ghi vào sổ truy cập (CHECK của pf_files.access_log, pf 000123).
const (
	OpMetadata = "metadata"
	OpContent  = "content"

	OutcomeGranted       = "granted"
	OutcomeDeniedSubject = "denied_subject" // chủ thể không phân giải được (không có, ngoài tenant, module chủ lạ)
	OutcomeDeniedScope   = "denied_scope"   // người gọi không thấy chủ thể
	OutcomeDeniedConsent = "denied_consent" // hộ chưa đồng ý (hoặc đã rút) cho mục đích
)

// Kiểu nội dung (của BẢN RÕ) được nhận.
var contentTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/webp": true, "image/heic": true,
	"application/pdf": true, "video/mp4": true,
}

var (
	shaRe         = regexp.MustCompile(`^[0-9a-f]{64}$`)
	subjectModRe  = regexp.MustCompile(`^(pf|mkg)/[a-z][a-z0-9]{1,39}$`)
	subjectTypeRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
)

// ObjectKey là khóa đối tượng files/<tenant>/<file_ref> — không chứa định danh.
func ObjectKey(tenantID, ref uuid.UUID) string {
	return "files/" + tenantID.String() + "/" + ref.String()
}

// ValidateSubject kiểm dạng bộ ba (khớp CHECK của pf 000123). Tồn tại và cùng tenant do Resolver của module chủ.
func ValidateSubject(s port.Subject) []problem.FieldError {
	var errs []problem.FieldError
	if !subjectModRe.MatchString(s.Module) {
		errs = append(errs, problem.FieldError{Field: "subject_module", Reason: "phải là tên module, vd pf/cooperative"})
	}
	if !subjectTypeRe.MatchString(s.Type) {
		errs = append(errs, problem.FieldError{Field: "subject_type", Reason: "chữ thường, số, gạch dưới (2–40 ký tự)"})
	}
	if s.ID == uuid.Nil {
		errs = append(errs, problem.FieldError{Field: "subject_id", Reason: "bắt buộc, phải là UUID"})
	}
	return errs
}

// Visible cho biết người gọi có phạm vi sc thấy chủ thể r: phạm vi toàn quốc, qua cấp phép chia sẻ, hoặc cùng
// đơn vị hành chính. Chủ thể không có mã đơn vị chỉ phạm vi toàn quốc thấy (mặc định chặn).
func Visible(sc authz.Scope, r port.Resolved) bool {
	switch {
	case sc.All, r.ViaShareGrant:
		return true
	case r.CommuneCode == "":
		return false
	default:
		return sc.Allows(r.CommuneCode)
	}
}

// ParseFileMeta kiểm metadata phong bì (docs/dev/envelope-format.md) và kích thước bản mã. Trả File chưa có
// tenant/ref/người tải; băm bản mã do app so.
func ParseFileMeta(m envelope.Metadata, size int64, now time.Time) (port.File, []problem.FieldError) {
	var errs []problem.FieldError
	bad := func(field, reason string) { errs = append(errs, problem.FieldError{Field: field, Reason: reason}) }
	f := port.File{V: m.V, Alg: m.Alg, Nonce: m.Nonce, WrappedDEK: m.WrappedDEK, Size: size, ContentType: m.ContentType}
	if m.V != envelope.Version {
		bad("meta.v", "chỉ hỗ trợ phiên bản 1")
	}
	if m.Alg != envelope.Alg {
		bad("meta.alg", "phải là "+envelope.Alg)
	}
	if s, err := ids.ParseRef(m.SubjectRef); err != nil {
		bad("meta.subject_ref", "bắt buộc, phải là UUID")
	} else {
		f.SubjectRef = s
	}
	if len(m.Nonce) != envelope.NonceSize {
		bad("meta.nonce", "phải là base64 của 12 byte")
	}
	if len(m.WrappedDEK) != envelope.WrappedDEKLen {
		bad("meta.wrapped_dek", "phải là base64 của 80 byte (crypto_box_seal)")
	}
	if !shaRe.MatchString(m.SHA256Ciphertext) {
		bad("meta.sha256_ciphertext", "phải là 64 ký tự hex thường")
	}
	f.SHA256Ciphertext = m.SHA256Ciphertext
	if !contentTypes[m.ContentType] {
		bad("meta.content_type", "kiểu nội dung không được nhận")
	}
	if m.CapturedAt != "" {
		t, err := time.Parse(time.RFC3339Nano, m.CapturedAt)
		switch {
		case err != nil:
			bad("meta.captured_at", "phải theo RFC 3339 có múi giờ")
		case t.After(now.Add(MaxClockSkew)):
			bad("meta.captured_at", "không được ở tương lai quá 5 phút")
		default:
			tu := t.UTC()
			f.CapturedAt = &tu
		}
	}
	if g := m.GPS; g != nil {
		if g.Lat < -90 || g.Lat > 90 || g.Lon < -180 || g.Lon > 180 || g.AccuracyM < 0 {
			bad("meta.gps", "toạ độ ngoài miền hợp lệ")
		} else {
			lat, lon, acc := g.Lat, g.Lon, g.AccuracyM
			f.GPSLat, f.GPSLon, f.GPSAccuracyM = &lat, &lon, &acc
		}
	}
	switch {
	case size < port.MinFileBytes:
		bad("blob", "bản mã quá ngắn")
	case size > port.MaxFileBytes:
		bad("blob", "vượt 15 MB")
	}
	return f, errs
}
