package app

// Ảnh nhật ký (NT-13 ảnh cân mẫu, ảnh bộ test; NC-16): máy chủ nhận ảnh đã nén (≤ 500 KB), MÃ HÓA PHONG BÌ bằng khóa công
// khai của CHỦ HỘ ở kho định danh (xóa định danh chủ hộ = ảnh không còn đọc được), lưu bản mã qua pf/evidence với bộ ba
// chủ thể ('pf/extension', 'cnts_household', hộ) — không lưu ảnh/base64 trong CSDL KN. Mở ảnh: pf/evidence OpenContent
// với mục đích extension.diary (kiểm vai vault_evidence_viewer, chủ thể qua FileResolver dưới đây, ghi sổ truy cập).

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/store"
	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// PhotoInput là một ảnh tải lên.
type PhotoInput struct {
	Ref          *uuid.UUID // UUIDv7 máy khách sinh (tùy chọn — tải lại an toàn)
	HouseholdRef uuid.UUID
	Kind         string
	CapturedAt   *time.Time
	Content      []byte
}

// photoTypes là kiểu ảnh nhận (đoán từ nội dung, không tin tên tệp).
var photoTypes = []string{"image/jpeg", "image/png", "image/webp"}

// UploadPhoto mã hóa và lưu ảnh nhật ký của hộ.
func (s *Service) UploadPhoto(ctx context.Context, in PhotoInput) (store.Photo, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return store.Photo{}, err
	}
	if err := c.checkWrite(); err != nil {
		return store.Photo{}, err
	}
	if s.Files == nil || s.Vault == nil {
		return store.Photo{}, ErrDependency
	}
	if !slices.Contains(domain.PhotoKinds, in.Kind) {
		return store.Photo{}, ErrPhotoInvalid.WithField("kind", "weighing, test_kit, flock hoặc other")
	}
	if len(in.Content) == 0 || len(in.Content) > domain.MaxPhotoBytes {
		return store.Photo{}, ErrPhotoInvalid.WithField("file", "ảnh 1 byte – 500 KB (nén trước khi gửi)")
	}
	ct := http.DetectContentType(in.Content)
	if !slices.Contains(photoTypes, ct) {
		return store.Photo{}, ErrPhotoInvalid.WithField("file", "chỉ nhận ảnh JPEG, PNG, WebP")
	}
	if in.CapturedAt != nil && in.CapturedAt.After(s.now().Add(domain.FutureSkew)) {
		return store.Photo{}, ErrPhotoInvalid.WithField("captured_at", "không ở tương lai")
	}
	ref := ids.New()
	if in.Ref != nil {
		ref = *in.Ref
	}
	var person uuid.UUID
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		// Mã ảnh đã có (dù cùng hộ): không lưu lại. Trước đây cùng hộ thì đi tiếp với person rỗng → 409 "chưa có khóa"
		// sai lệch (#172). Bấm lại đúng ảnh cũ thì lớp Idempotency-Key đã phát lại trước khi tới đây.
		p, err := store.PhotoByRef(ctx, tx, ref)
		if err != nil {
			return err
		}
		if p != nil {
			return ErrPhotoInvalid.WithField("id", "mã ảnh đã dùng")
		}
		hc, err := s.loadHousehold(ctx, tx, c, in.HouseholdRef)
		if err != nil {
			return err
		}
		if hc == nil {
			return problem.NotFound.WithField("household_ref", "không có hộ mô hình")
		}
		if !c.keeper(hc) || !hc.open() {
			return problem.Forbidden.WithDetail("chỉ cán bộ theo dõi điểm / người ghi nhật ký của hộ tải ảnh")
		}
		person = hc.h.PersonRef
		return nil
	}); err != nil {
		return store.Photo{}, err
	}
	keys, err := s.Vault.HouseholdPubKeys(ctx, []uuid.UUID{person})
	if err != nil {
		return store.Photo{}, err
	}
	if len(keys) != 1 || len(keys[0].PublicKey) != envelope.KeySize {
		return store.Photo{}, ErrPhotoNoKey
	}
	var pub [envelope.KeySize]byte
	copy(pub[:], keys[0].PublicKey)
	sealed, err := envelope.Seal(in.Content, &pub)
	if err != nil {
		return store.Photo{}, err
	}
	captured := ""
	if in.CapturedAt != nil {
		captured = in.CapturedAt.UTC().Format(time.RFC3339)
	}
	fileRef := ref.String()
	f, _, err := s.Files.Attach(ctx, evport.AttachInput{FileRef: &fileRef, Meta: envelope.NewMetadata(person.String(), sealed, ct, captured),
		Blob: sealed.Ciphertext, Subject: &evport.Subject{Module: SubjectModule, Type: SubjectType, ID: in.HouseholdRef}})
	if err != nil {
		return store.Photo{}, err
	}
	out := store.Photo{FileRef: f.Ref, HouseholdRef: in.HouseholdRef, Kind: in.Kind, ContentType: ct, Size: len(in.Content),
		CapturedAt: in.CapturedAt, UploadedBy: c.me()}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.InsertPhoto(ctx, tx, c.p.TenantID, out); err != nil {
			return err
		}
		p, err := store.PhotoByRef(ctx, tx, f.Ref)
		if err == nil && p != nil {
			out = *p
		}
		return err
	})
	return out, err
}

// OpenPhoto trả nội dung ảnh (bản rõ chỉ trong bộ nhớ của request) cho người ghi được / người rà soát được / Admin.
func (s *Service) OpenPhoto(ctx context.Context, ref uuid.UUID) (string, []byte, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return "", nil, err
	}
	if s.Files == nil {
		return "", nil, ErrDependency
	}
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		ok, err := s.photoAllowed(ctx, tx, c, ref)
		if err == nil && !ok {
			err = ErrPhotoNotFound
		}
		return err
	}); err != nil {
		return "", nil, err
	}
	return s.Files.OpenContent(ctx, ref, Purpose)
}

// photoAllowed: ảnh có trong tenant, người gọi thấy hộ và là người ghi được / người rà soát được / Admin.
func (s *Service) photoAllowed(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID) (bool, error) {
	p, err := store.PhotoByRef(ctx, tx, ref)
	if err != nil || p == nil {
		return false, err
	}
	hc, err := s.loadHousehold(ctx, tx, c, p.HouseholdRef)
	if err != nil || hc == nil {
		return false, err
	}
	return c.privileged(hc), nil
}

// ---------------------------------------------------------------------------
// Resolver cho pf/evidence (R-DB-07): chủ thể ('pf/extension', 'cnts_household', hộ mô hình).
// ---------------------------------------------------------------------------

// FileResolver là Resolver của pf/extension cho pf/evidence (ảnh nhật ký CNTS).
type FileResolver struct{ S *Service }

var (
	_ evport.Resolver       = FileResolver{}
	_ evport.FileAuthorizer = FileResolver{}
)

// Resolve: hộ có trong tenant; người gọi ghi được / rà soát được → ViaShareGrant; không → chỉ mã xã (evidence kiểm phạm vi).
func (r FileResolver) Resolve(ctx context.Context, subjectType string, id uuid.UUID) (evport.Resolved, error) {
	if subjectType != SubjectType {
		return evport.Resolved{}, evport.ErrSubjectTypeUnsupported
	}
	if r.S == nil || r.S.DB == nil {
		return evport.Resolved{}, evport.ErrResolverUnavailable
	}
	c, err := r.S.reader(ctx)
	if err != nil {
		return evport.Resolved{}, evport.ErrSubjectNotFound
	}
	var out evport.Resolved
	err = r.S.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		hc, err := r.S.loadHousehold(ctx, tx, c, id)
		if err != nil {
			return err
		}
		if hc == nil {
			return evport.ErrSubjectNotFound
		}
		out = evport.Resolved{CommuneCode: hc.h.CommuneCode, ViaShareGrant: c.privileged(hc)}
		return nil
	})
	return out, err
}

// AuthorizeFile kiểm lại ảnh cụ thể: có trong sổ ảnh nhật ký, cùng hộ của bộ ba, người gọi có quyền xem ảnh của hộ.
func (r FileResolver) AuthorizeFile(ctx context.Context, f evport.File) (bool, error) {
	if r.S == nil || r.S.DB == nil {
		return false, evport.ErrResolverUnavailable
	}
	if f.Subject == nil || f.Subject.Module != SubjectModule || f.Subject.Type != SubjectType {
		return false, nil
	}
	c, err := r.S.reader(ctx)
	if err != nil {
		return false, nil //nolint:nilerr // người gọi không có ngữ cảnh KN: từ chối tệp
	}
	allowed := false
	err = r.S.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		p, err := store.PhotoByRef(ctx, tx, f.Ref)
		if err != nil || p == nil || p.HouseholdRef != f.Subject.ID {
			return err
		}
		allowed, err = r.S.photoAllowed(ctx, tx, c, f.Ref)
		return err
	})
	if errors.Is(err, ErrPhotoNotFound) {
		return false, nil
	}
	return allowed, err
}
