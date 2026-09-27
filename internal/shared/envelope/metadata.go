package envelope

import (
	"crypto/sha256"
	"encoding/hex"
)

// GPS là toạ độ chụp. Gắn với hộ thì là dữ liệu cá nhân — không log (R-14).
type GPS struct {
	Lat       float64 `json:"lat" pii:"true"`
	Lon       float64 `json:"lon" pii:"true"`
	AccuracyM float64 `json:"accuracy_m"`
}

// Metadata là phần JSON đi kèm bản mã (docs/dev/envelope-format.md). nonce và
// wrapped_dek mã hóa base64 chuẩn (encoding/json làm sẵn với []byte).
type Metadata struct {
	V                int    `json:"v"`
	Alg              string `json:"alg"`
	SubjectRef       string `json:"subject_ref"`
	Nonce            []byte `json:"nonce"`
	WrappedDEK       []byte `json:"wrapped_dek"`
	SHA256Ciphertext string `json:"sha256_ciphertext"`
	ContentType      string `json:"content_type"`
	CapturedAt       string `json:"captured_at"`
	GPS              *GPS   `json:"gps,omitempty"`
}

// NewMetadata dựng metadata phiên bản 1 cho một phong bì.
func NewMetadata(subjectRef string, s Sealed, contentType, capturedAt string) Metadata {
	return Metadata{
		V: Version, Alg: Alg, SubjectRef: subjectRef, Nonce: s.Nonce, WrappedDEK: s.WrappedDEK,
		SHA256Ciphertext: SHA256Hex(s.Ciphertext), ContentType: contentType, CapturedAt: capturedAt,
	}
}

// SHA256Hex là băm SHA-256 dạng hex của bản mã.
func SHA256Hex(ciphertext []byte) string {
	h := sha256.Sum256(ciphertext)
	return hex.EncodeToString(h[:])
}
