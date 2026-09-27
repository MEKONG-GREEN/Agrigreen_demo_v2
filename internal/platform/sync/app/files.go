package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// PresignTTL là thời hạn URL tải bản mã cấp cho người xem.
const PresignTTL = 5 * time.Minute

// FilePurposes là mục đích được phép đọc tệp minh chứng (R-42).
var FilePurposes = map[client.Purpose]bool{client.PurposeMRVEvidence: true, client.PurposeFieldSupport: true}

// UploadInput là một tệp máy gửi lên: metadata phong bì và BẢN MÃ.
type UploadInput struct {
	DeviceHeader string
	FileRef      *string // tùy chọn: UUIDv7 máy sinh để bản ghi evidence ngoại tuyến tham chiếu trước khi tải
	Meta         envelope.Metadata
	Blob         []byte
}

// Upload lưu một tệp minh chứng đã mã hóa. Máy chủ KHÔNG giải mã (không có DEK):
// chỉ kiểm cấu trúc phong bì và SHA-256 bản mã, ghi bản mã vào kho đối tượng tại
// files/<tenant>/<file_ref> rồi ghi siêu dữ liệu. Gửi lại cùng file_ref, cùng bản
// mã → trả bản cũ (created = false); khác bản mã → sync.file_ref_taken.
func (s *Service) Upload(ctx context.Context, in UploadInput) (f domain.File, created bool, err error) {
	p, err := principal(ctx)
	if err != nil {
		return f, false, err
	}
	dev, err := s.checkDevice(ctx, p, in.DeviceHeader)
	if err != nil {
		return f, false, err
	}
	if s.Store == nil {
		return f, false, ErrDependency.WithDetail("chưa nối kho đối tượng")
	}
	now := s.Clock.Now()
	actual := envelope.SHA256Hex(in.Blob)
	f, errs := domain.ParseFileMeta(in.Meta, int64(len(in.Blob)), now)
	ref := ids.New()
	if in.FileRef != nil {
		r, err := ids.Parse(*in.FileRef)
		if err != nil {
			errs = append(errs, fieldErr("file_ref", "phải là UUIDv7"))
		}
		ref = r
	}
	if len(errs) > 0 {
		return f, false, validation(errs)
	}
	if f.SHA256Ciphertext != actual {
		return f, false, ErrFileChecksum
	}
	devID := dev.ID
	f.TenantID, f.Ref, f.UploadedBy, f.DeviceID = p.TenantID, ref, p.ActorRef, &devID
	f.ObjectKey = domain.ObjectKey(p.TenantID, ref)

	if prev, err := s.fileByRef(ctx, p, ref); err != nil {
		return f, false, err
	} else if prev != nil {
		if prev.SHA256Ciphertext != actual {
			return f, false, ErrFileRefTaken
		}
		return *prev, false, nil
	}
	_, err = s.Store.Put(ctx, f.ObjectKey, bytes.NewReader(in.Blob), int64(len(in.Blob)), actual, "application/octet-stream")
	switch {
	case errors.Is(err, objstore.ErrChecksumMismatch):
		return f, false, ErrFileChecksum
	case errors.Is(err, objstore.ErrExists):
		return f, false, ErrFileRefTaken
	case err != nil:
		return f, false, err
	}
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		prev, err := store.FileByRef(ctx, tx, ref)
		if err != nil {
			return err
		}
		if prev != nil { // hai lần tải song song cùng file_ref, cùng bản mã
			f = *prev
			return nil
		}
		created = true
		return store.InsertFile(ctx, tx, f)
	})
	if err == nil && created {
		f.CreatedAt = now
	}
	return f, created, err
}

func (s *Service) fileByRef(ctx context.Context, p authz.Principal, ref uuid.UUID) (*domain.File, error) {
	var f *domain.File
	err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		f, e = store.FileByRef(ctx, tx, ref)
		return e
	})
	return f, err
}

// FileView là siêu dữ liệu kèm URL tải bản mã có hạn.
type FileView struct {
	File      domain.File
	URL       string
	ExpiresAt time.Time
}

// authorizeFile kiểm vai, mục đích và đồng ý trước khi cấp tệp. Vai là vai xem
// minh chứng của kho định danh — cùng vai kho sẽ đòi khi mở DEK.
func (s *Service) authorizeFile(ctx context.Context, ref uuid.UUID, purpose client.Purpose) (authz.Principal, domain.File, error) {
	p, err := authz.Require(ctx, client.RoleEvidenceViewer)
	if err != nil {
		return p, domain.File{}, err
	}
	switch {
	case purpose == "":
		return p, domain.File{}, ErrPurposeRequired
	case !FilePurposes[purpose]:
		return p, domain.File{}, ErrPurposeNotAllowed
	}
	f, err := s.fileByRef(ctx, p, ref)
	if err != nil {
		return p, domain.File{}, err
	}
	if f == nil {
		return p, domain.File{}, ErrFileNotFound
	}
	if s.Consent != nil {
		ok, err := s.Consent.HasConsent(ctx, f.SubjectRef, string(purpose))
		if err != nil {
			return p, domain.File{}, err
		}
		if !ok {
			return p, domain.File{}, ErrConsentMissing
		}
	}
	return p, *f, nil
}

func (s *Service) accessLog(ctx context.Context, p authz.Principal, op string, ref uuid.UUID, purpose client.Purpose) {
	// Chỉ mã tham chiếu và mục đích — không toạ độ, không thời điểm chụp (R-14).
	s.Log.Info("truy cập tệp minh chứng", "event", "files.access", "op", op, "file_ref", ref, "purpose", string(purpose),
		"tenant_id", p.TenantID, "actor_ref", p.ActorRef, "trace_id", telemetry.TraceID(ctx))
}

// GetFile trả siêu dữ liệu và URL tải BẢN MÃ có hạn 5 phút. Giải mã để hiển thị
// đi qua ContentFile (mở DEK bằng kho định danh, có mục đích).
func (s *Service) GetFile(ctx context.Context, ref uuid.UUID, purpose client.Purpose) (FileView, error) {
	p, f, err := s.authorizeFile(ctx, ref, purpose)
	if err != nil {
		return FileView{}, err
	}
	if s.Store == nil {
		return FileView{}, ErrDependency.WithDetail("chưa nối kho đối tượng")
	}
	url, err := s.Store.PresignGet(ctx, f.ObjectKey, PresignTTL)
	if err != nil {
		return FileView{}, err
	}
	s.accessLog(ctx, p, "metadata", ref, purpose)
	return FileView{File: f, URL: url, ExpiresAt: s.Clock.Now().Add(PresignTTL)}, nil
}

// ContentFile mở tệp để hiển thị: kho định danh mở DEK (bắt buộc mục đích, ghi
// sổ truy cập ở kho), bản rõ chỉ tồn tại trong bộ nhớ của request này rồi trả thẳng
// cho người xem. Không ghi bản rõ ra đĩa, không lưu đệm.
func (s *Service) ContentFile(ctx context.Context, ref uuid.UUID, purpose client.Purpose) (contentType string, plaintext []byte, err error) {
	p, f, err := s.authorizeFile(ctx, ref, purpose)
	if err != nil {
		return "", nil, err
	}
	if s.Store == nil || s.Vault == nil {
		return "", nil, ErrDependency.WithDetail("chưa nối kho đối tượng hoặc kho định danh")
	}
	rc, _, err := s.Store.Get(ctx, f.ObjectKey)
	if err != nil {
		return "", nil, err
	}
	ct, err := io.ReadAll(io.LimitReader(rc, domain.MaxFileBytes+1))
	_ = rc.Close()
	if err != nil {
		return "", nil, err
	}
	if envelope.SHA256Hex(ct) != f.SHA256Ciphertext {
		return "", nil, errors.New("sync: bản mã trong kho đối tượng không khớp băm đã ghi")
	}
	dek, err := s.Vault.Unwrap(ctx, purpose, f.SubjectRef, f.WrappedDEK)
	if err != nil {
		return "", nil, err
	}
	defer clear(dek)
	pt, err := envelope.DecryptWithDEK(dek, f.Nonce, ct)
	if err != nil {
		return "", nil, err
	}
	s.accessLog(ctx, p, "content", ref, purpose)
	return f.ContentType, pt, nil
}
