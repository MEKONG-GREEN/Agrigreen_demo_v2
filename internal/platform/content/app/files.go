package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

const docxType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// sniffType nhận diện loại tệp theo nội dung (không tin Content-Type do client khai).
// Cho phép: PDF, Word (.docx), ảnh JPEG/PNG/WebP, video MP4 (KN FR-12: video/hình/PDF).
func sniffType(b []byte, fileName string) (string, bool) {
	switch domain.FileKind(b) {
	case "pdf":
		return "application/pdf", true
	case "zip":
		if strings.EqualFold(path.Ext(fileName), ".docx") {
			return docxType, true
		}
		return "", false
	}
	switch ct := http.DetectContentType(b); ct {
	case "image/jpeg", "image/png", "image/webp", "video/mp4":
		return ct, true
	}
	return "", false
}

// cleanFileName giữ tên hiển thị an toàn (không đường dẫn, không ký tự điều khiển).
func cleanFileName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, name)
	if name == "." || name == "/" || name == "" {
		name = "tep"
	}
	if utf8.RuneCountInString(name) > 200 {
		name = string([]rune(name)[:200])
	}
	return name
}

// UploadInput là một tệp đính kèm tải lên.
type UploadInput struct {
	Data     []byte
	FileName string
	SHA256   string // do client khai; bắt buộc
	IdemKey  string
}

// Upload lưu tệp đính kèm của tài liệu vào kho đối tượng (ghi một lần, khóa theo
// băm nội dung) và ghi siêu dữ liệu. Tài liệu không phải dữ liệu cá nhân nên
// lưu bản rõ (ngoại lệ R-40 cho tài liệu công khai); vẫn kiểm SHA-256.
func (s *Service) Upload(ctx context.Context, ref uuid.UUID, in UploadInput) (store.AttachmentRow, bool, error) {
	var row store.AttachmentRow
	p, err := authz.Require(ctx, editorRoles...)
	if err != nil {
		return row, false, err
	}
	if in.IdemKey == "" {
		return row, false, problem.IdempotencyRequired
	}
	if s.Store == nil {
		return row, false, ErrDependency.WithDetail("chưa cấu hình kho đối tượng")
	}
	if len(in.Data) == 0 {
		return row, false, problem.Validation.WithField("file", "tệp rỗng")
	}
	if len(in.Data) > domain.MaxAttachmentBytes {
		return row, false, ErrFileTooLarge.WithDetail("tối đa 15 MB mỗi tệp")
	}
	ct, ok := sniffType(in.Data, in.FileName)
	if !ok {
		return row, false, ErrFileType.WithDetail("chỉ nhận PDF, Word (.docx), ảnh JPEG/PNG/WebP, video MP4")
	}
	sum := objstore.SHA256Hex(in.Data)
	if !strings.EqualFold(strings.TrimSpace(in.SHA256), sum) {
		return row, false, ErrChecksum
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return row, false, err
	}
	// Kiểm quyền trước khi ghi vào kho.
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		d, err := s.loadForEdit(ctx, tx, p, sc, ref)
		if err != nil {
			return err
		}
		if d.Status == domain.StatusRetired {
			return ErrInvalidTransition.WithDetail("tài liệu đã gỡ")
		}
		return nil
	})
	if err != nil {
		return row, false, err
	}
	key := "content/" + p.TenantID.String() + "/" + ref.String() + "/" + sum
	if _, err := s.Store.Put(ctx, key, bytes.NewReader(in.Data), int64(len(in.Data)), sum, ct); err != nil {
		if errors.Is(err, objstore.ErrChecksumMismatch) {
			return row, false, ErrChecksum
		}
		return row, false, err
	}
	row = store.AttachmentRow{
		ID: ids.New(), DocumentRef: ref, Key: key, SHA256: sum, Size: int64(len(in.Data)), ContentType: ct,
		FileName: cleanFileName(in.FileName), UploadedBy: p.ActorRef,
	}
	var replayed bool
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		var err error
		replayed, err = store.InsertAttachment(ctx, tx, p.TenantID, &row, in.IdemKey)
		return err
	})
	return row, replayed, err
}

// Extract trích nội dung từ tệp Word (.docx) hoặc PDF có lớp chữ để điền sẵn bản nháp (US-LIB-03, US-CMS-02).
// Không lưu gì: người soạn xem lại, sửa rồi tạo bản nháp — không bao giờ tự xuất bản.
func (s *Service) Extract(ctx context.Context, data []byte) (domain.Extracted, error) {
	if _, err := authz.Require(ctx, editorRoles...); err != nil {
		return domain.Extracted{}, err
	}
	if len(data) == 0 {
		return domain.Extracted{}, problem.Validation.WithField("file", "tệp rỗng")
	}
	if len(data) > domain.MaxAttachmentBytes {
		return domain.Extracted{}, ErrFileTooLarge
	}
	switch domain.FileKind(data) {
	case "zip", "pdf", "ole":
	default:
		return domain.Extracted{}, ErrFileType.WithDetail("Chỉ hỗ trợ file Word hoặc PDF trong giới hạn dung lượng cho phép.")
	}
	x, err := domain.Extract(data)
	var xe *domain.ExtractError
	switch {
	case errors.As(err, &xe) && errors.Is(err, domain.ErrExtractUnsupported):
		return x, ErrExtractUnsupported.WithDetail(xe.Msg)
	case errors.Is(err, domain.ErrExtractUnsupported):
		return x, ErrExtractUnsupported
	case errors.Is(err, domain.ErrExtractFailed):
		return x, ErrExtractFailed
	case err != nil:
		return x, err
	}
	x.Body = domain.Sanitize(x.Body, x.BodyFormat)
	return x, nil
}
