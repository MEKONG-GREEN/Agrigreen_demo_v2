package app

import (
	"context"

	"github.com/google/uuid"

	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
)

// UploadInput là một tệp máy gửi lên: metadata phong bì và BẢN MÃ.
type UploadInput struct {
	DeviceHeader string
	FileRef      *string // tùy chọn: UUIDv7 máy sinh để bản ghi evidence ngoại tuyến tham chiếu trước khi tải
	Meta         envelope.Metadata
	Blob         []byte
	Subject      *evport.Subject // tùy chọn: bộ ba R-DB-07 (W6.5)
}

// Upload kiểm thiết bị của người gọi rồi giao tệp cho pf/evidence (cap/evidence, W6.5) — module sở hữu
// pf_files: phong bì, băm bản mã, chủ thể, kho đối tượng.
func (s *Service) Upload(ctx context.Context, in UploadInput) (evport.File, bool, error) {
	p, err := principal(ctx)
	if err != nil {
		return evport.File{}, false, err
	}
	dev, err := s.checkDevice(ctx, p, in.DeviceHeader)
	if err != nil {
		return evport.File{}, false, err
	}
	if s.Files == nil {
		return evport.File{}, false, ErrDependency.WithDetail("chưa nối pf/evidence")
	}
	devID := dev.ID
	return s.Files.Attach(ctx, evport.AttachInput{FileRef: in.FileRef, Meta: in.Meta, Blob: in.Blob, DeviceID: &devID, Subject: in.Subject})
}

// GetFile: Open(purpose) của pf/evidence — siêu dữ liệu và URL tải BẢN MÃ có hạn.
func (s *Service) GetFile(ctx context.Context, ref uuid.UUID, purpose string) (evport.Opened, error) {
	if s.Files == nil {
		return evport.Opened{}, ErrDependency.WithDetail("chưa nối pf/evidence")
	}
	return s.Files.Open(ctx, ref, purpose)
}

// ContentFile: OpenContent(purpose) của pf/evidence — bản rõ chỉ trong bộ nhớ request.
func (s *Service) ContentFile(ctx context.Context, ref uuid.UUID, purpose string) (string, []byte, error) {
	if s.Files == nil {
		return "", nil, ErrDependency.WithDetail("chưa nối pf/evidence")
	}
	return s.Files.OpenContent(ctx, ref, purpose)
}
