package domain

import (
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Giới hạn tệp minh chứng.
const (
	MaxFileBytes = 15 << 20 // 15 MiB bản mã mỗi tệp (docs/dev/sync-contract.md)
	MinFileBytes = envelope.TagSize
	// MaxUploadBody là giới hạn thân multipart của POST /v1/files (bản mã + metadata + biên).
	MaxUploadBody = MaxFileBytes + 256<<10
)

// Kiểu nội dung (của BẢN RÕ) được nhận.
var contentTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/webp": true, "image/heic": true,
	"application/pdf": true, "video/mp4": true,
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// File là siêu dữ liệu một tệp minh chứng đã mã hóa phong bì. Nội dung chỉ nằm ở
// kho đối tượng dưới dạng bản mã (R-40).
type File struct {
	TenantID         uuid.UUID
	Ref              uuid.UUID
	SubjectRef       uuid.UUID
	V                int
	Alg              string
	Nonce            []byte
	WrappedDEK       []byte
	SHA256Ciphertext string
	Size             int64
	ContentType      string
	// CapturedAt và GPS là dữ liệu cá nhân khi gắn với hộ: không log, không đưa vào sự kiện.
	CapturedAt   *time.Time `pii:"true"`
	GPSLat       *float64   `pii:"true"`
	GPSLon       *float64   `pii:"true"`
	GPSAccuracyM *float64
	ObjectKey    string
	UploadedBy   uuid.UUID
	DeviceID     *uuid.UUID
	CreatedAt    time.Time
}

// ObjectKey là khóa đối tượng files/<tenant>/<file_ref> — không chứa định danh.
func ObjectKey(tenantID, ref uuid.UUID) string {
	return "files/" + tenantID.String() + "/" + ref.String()
}

// ParseFileMeta kiểm metadata phong bì (docs/dev/envelope-format.md) và kích thước
// bản mã. Trả File chưa có tenant/ref/người tải; băm bản mã do app so.
func ParseFileMeta(m envelope.Metadata, size int64, now time.Time) (File, []problem.FieldError) {
	var errs []problem.FieldError
	bad := func(field, reason string) { errs = append(errs, problem.FieldError{Field: field, Reason: reason}) }
	f := File{V: m.V, Alg: m.Alg, Nonce: m.Nonce, WrappedDEK: m.WrappedDEK, Size: size, ContentType: m.ContentType}
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
	case size < MinFileBytes:
		bad("blob", "bản mã quá ngắn")
	case size > MaxFileBytes:
		bad("blob", "vượt 15 MB")
	}
	return f, errs
}
