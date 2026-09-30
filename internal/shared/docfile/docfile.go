// Package docfile kiểm tệp chứng từ tải lên trước khi ghi vào kho đối tượng (shared/objstore): giới hạn cỡ,
// nhận diện loại theo NỘI DUNG (không tin Content-Type client khai), SHA-256 do client khai phải khớp, tên hiển
// thị an toàn, và đọc phần "file" của multipart. Chỉ hạ tầng — không logic nghiệp vụ; module tự ánh xạ lỗi sang
// mã problem của mình và tự ghi siêu dữ liệu (cùng mẫu với tệp đính kèm pf/content, dùng được ở vùng mkg — R-03).
package docfile

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

// MaxBytes là cỡ tối đa một tệp chứng từ (15 MB, như pf/content).
const MaxBytes = 15 << 20

// Loại tệp được nhận: PDF và ảnh (biên bản, ePOD, e-bill chụp / quét).
const (
	TypePDF  = "application/pdf"
	TypeJPEG = "image/jpeg"
	TypePNG  = "image/png"
	TypeWebP = "image/webp"
)

// Lỗi kiểm tệp (module ánh xạ sang problem.* mã <module>.file_*).
var (
	ErrEmpty    = errors.New("docfile: tệp rỗng")
	ErrTooLarge = errors.New("docfile: tệp quá 15 MB")
	ErrType     = errors.New("docfile: chỉ nhận PDF, ảnh JPEG/PNG/WebP")
	ErrChecksum = errors.New("docfile: SHA-256 khai báo không khớp nội dung")
	ErrMissing  = errors.New("docfile: thiếu phần multipart \"file\"")
)

// Sniff nhận diện loại tệp theo nội dung.
func Sniff(b []byte) (string, bool) {
	if bytes.HasPrefix(b, []byte("%PDF-")) {
		return TypePDF, true
	}
	switch ct := http.DetectContentType(b); ct {
	case TypeJPEG, TypePNG, TypeWebP:
		return ct, true
	}
	return "", false
}

// CleanName giữ tên hiển thị an toàn: không đường dẫn, không ký tự điều khiển, tối đa 200 ký tự.
func CleanName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "." || name == "/" || name == "" {
		name = "tep"
	}
	if utf8.RuneCountInString(name) > 200 {
		name = string([]rune(name)[:200])
	}
	return name
}

// Checked là tệp đã qua kiểm.
type Checked struct {
	ContentType string
	SHA256      string // hex thường
	Size        int64
	FileName    string
}

// Check kiểm nội dung + tên + SHA-256 khai báo (bắt buộc — chống hỏng khi truyền, như pf/content).
func Check(data []byte, fileName, declaredSHA string) (Checked, error) {
	if len(data) == 0 {
		return Checked{}, ErrEmpty
	}
	if len(data) > MaxBytes {
		return Checked{}, ErrTooLarge
	}
	ct, ok := Sniff(data)
	if !ok {
		return Checked{}, ErrType
	}
	sum := objstore.SHA256Hex(data)
	if !strings.EqualFold(strings.TrimSpace(declaredSHA), sum) {
		return Checked{}, ErrChecksum
	}
	return Checked{ContentType: ct, SHA256: sum, Size: int64(len(data)), FileName: CleanName(fileName)}, nil
}

// ReadMultipart đọc phần "file" của multipart từ r (đã giới hạn thân bằng httpx.AllowBody), tối đa MaxBytes.
func ReadMultipart(r *http.Request) ([]byte, string, error) {
	_, fh, err := r.FormFile("file")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, "", ErrTooLarge
		}
		return nil, "", ErrMissing
	}
	return readHeader(fh)
}

func readHeader(fh *multipart.FileHeader) ([]byte, string, error) {
	if fh.Size > MaxBytes {
		return nil, "", ErrTooLarge
	}
	f, err := fh.Open()
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(b) > MaxBytes {
		return nil, "", ErrTooLarge
	}
	return b, fh.Filename, nil
}

// ObjectKey là khóa đối tượng theo nội dung: mkg/<module>/<tenant>/<sha256> — cùng nội dung ghi lặp an toàn
// (objstore.Put cùng băm trả bản cũ), không lộ tên tệp.
func ObjectKey(zonePrefix, tenant, sha string) string {
	return zonePrefix + "/" + tenant + "/" + sha
}

// WriteContent trả nội dung tệp (tải qua API có kiểm vai — không cấp URL có chữ ký ra ngoài): Content-Type đã
// nhận diện lúc tải, X-Content-SHA256 để client kiểm, không lưu đệm.
func WriteContent(w http.ResponseWriter, r io.Reader, contentType string, size int64, sha, name string) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	h.Set("X-Content-SHA256", sha)
	h.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, r)
}
