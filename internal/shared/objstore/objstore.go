// Package objstore là cổng duy nhất tới kho đối tượng (ảnh minh chứng, tệp
// đính kèm, manifest neo chuỗi, gói xuất).
//
// Ràng buộc bắt buộc:
//
//   - CHỈ lưu bản mã (R-40). Ảnh và tệp có dữ liệu cá nhân phải được mã hóa
//     phong bì theo hộ (ADR-0004, docs/dev/envelope-format.md) TRƯỚC khi gọi
//     Put. Package này không mã hóa và không biết khóa. Ngoại lệ duy nhất là
//     tài liệu vận hành không có định danh, vd manifest neo chuỗi (chỉ chứa
//     tenant_id, stream_key, băm).
//   - Nhà cung cấp phải có hạ tầng và pháp nhân trong lãnh thổ (RB-7, ADR-0007):
//     MinIO ở Local/Dev, S3 trong lãnh thổ có khóa đối tượng ở Production.
//   - Ghi một lần: Put không ghi đè. Không có Delete — minh chứng không bao
//     giờ bị xóa qua ứng dụng; vòng đời (hủy khóa hộ, hết hạn lưu trữ) do chính
//     sách của bucket và quy trình xóa theo khóa hộ đảm nhiệm (mục 22).
//   - Put kiểm SHA-256 nội dung so với giá trị khai báo; sai → ErrChecksumMismatch.
//   - PresignGet chỉ cấp URL ngắn hạn (tối đa MaxPresignTTL). Không ghi URL có
//     chữ ký vào log (R-92).
package objstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// MaxPresignTTL là thời hạn tối đa của URL tải về có chữ ký.
const MaxPresignTTL = 15 * time.Minute

// Lỗi của package.
var (
	ErrNotFound         = errors.New("objstore: không có đối tượng")
	ErrExists           = errors.New("objstore: đối tượng đã tồn tại với nội dung khác")
	ErrChecksumMismatch = errors.New("objstore: SHA-256 nội dung không khớp giá trị khai báo")
	ErrInvalidKey       = errors.New("objstore: khóa đối tượng không hợp lệ")
	ErrInvalidTTL       = errors.New("objstore: thời hạn URL phải trong (0, 15 phút]")
)

// Info là siêu dữ liệu của một đối tượng.
type Info struct {
	Key         string
	Size        int64
	SHA256      string // hex thường, 64 ký tự
	ContentType string
}

// Store là kho đối tượng ghi một lần.
type Store interface {
	// Put ghi nội dung r (đúng size byte) dưới key. sha256Hex là băm nội dung
	// người gọi đã tính; không khớp → ErrChecksumMismatch và không có đối tượng
	// nào được tạo. key đã có cùng băm → trả Info cũ (gọi lặp an toàn); khác
	// băm → ErrExists.
	Put(ctx context.Context, key string, r io.Reader, size int64, sha256Hex, contentType string) (Info, error)
	// Get mở đối tượng để đọc. Người gọi phải Close.
	Get(ctx context.Context, key string) (io.ReadCloser, Info, error)
	// Head trả siêu dữ liệu, ErrNotFound nếu không có.
	Head(ctx context.Context, key string) (Info, error)
	// PresignGet cấp URL tải về có hạn ttl (0 < ttl <= MaxPresignTTL).
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// Khóa: chữ thường/hoa ASCII, số, và . _ - / ; không bắt đầu bằng /, không có
// đoạn rỗng, "." hay "..".
var keyRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)

// ValidateKey kiểm khóa đối tượng.
func ValidateKey(key string) error {
	if len(key) == 0 || len(key) > 512 || !keyRe.MatchString(key) {
		return fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "." || seg == ".." {
			return fmt.Errorf("%w: %q", ErrInvalidKey, key)
		}
	}
	return nil
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func normalizeSHA(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if !shaRe.MatchString(s) {
		return "", fmt.Errorf("objstore: sha256 phải là 64 ký tự hex")
	}
	return s, nil
}

func validateTTL(ttl time.Duration) error {
	if ttl <= 0 || ttl > MaxPresignTTL {
		return ErrInvalidTTL
	}
	return nil
}

// SHA256Hex tính băm hex của b — tiện cho người gọi và kiểm thử.
func SHA256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// FromEnv chọn hiện thực theo biến môi trường:
//
//	AG_S3_ENDPOINT, AG_S3_BUCKET, AG_S3_ACCESS_KEY, AG_S3_SECRET_KEY, AG_S3_USE_TLS  → S3 (MinIO/S3 trong lãnh thổ)
//	AG_OBJSTORE_DIR (+ AG_OBJSTORE_PRESIGN_SECRET, AG_OBJSTORE_BASE_URL)            → thư mục cục bộ (chỉ Local/Dev)
func FromEnv(ctx context.Context) (Store, error) {
	if os.Getenv("AG_S3_ENDPOINT") != "" {
		cfg, err := S3ConfigFromEnv()
		if err != nil {
			return nil, err
		}
		return NewS3(ctx, cfg)
	}
	if dir := os.Getenv("AG_OBJSTORE_DIR"); dir != "" {
		return NewLocal(dir, LocalOptions{
			PresignSecret: []byte(os.Getenv("AG_OBJSTORE_PRESIGN_SECRET")),
			BaseURL:       os.Getenv("AG_OBJSTORE_BASE_URL"),
		})
	}
	return nil, errors.New("objstore: chưa cấu hình AG_S3_ENDPOINT hoặc AG_OBJSTORE_DIR")
}
