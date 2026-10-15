// Package app là phần cài đặt của pf/evidence (xem tài liệu gói gốc internal/platform/evidence).
package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// DBModule: giao dịch của module chạy SET LOCAL ROLE r_evidence (ADR-0028). Module mới không có vai đăng nhập
// cũ <module>_app: pool là vai đăng nhập của đơn vị (app_platform), là thành viên r_evidence.
var DBModule = db.Module("pf/evidence")

// PresignTTL là thời hạn URL tải bản mã cấp cho người xem.
const PresignTTL = 5 * time.Minute

// Mã lỗi (docs/errors.md). Mã chuyển từ pf/sync giữ tiền tố sync.* — hợp đồng HTTP của /v1/files không đổi.
var (
	ErrFileChecksum      = problem.New(http.StatusUnprocessableEntity, "sync.file_checksum_mismatch", "SHA-256 bản mã không khớp sha256_ciphertext")
	ErrFileRefTaken      = problem.New(http.StatusConflict, "sync.file_ref_taken", "file_ref đã dùng cho tệp có nội dung khác")
	ErrFileNotFound      = problem.New(http.StatusNotFound, "sync.file_not_found", "Không tìm thấy tệp minh chứng")
	ErrPurposeRequired   = problem.New(http.StatusBadRequest, "sync.purpose_required", "Thiếu mục đích xử lý (header X-Purpose)")
	ErrPurposeNotAllowed = problem.New(http.StatusForbidden, "sync.purpose_not_allowed", "Mục đích không được phép xem tệp minh chứng")
	ErrConsentMissing    = problem.New(http.StatusForbidden, "sync.consent_missing", "Chủ thể chưa đồng ý cho mục đích này")
	ErrDependency        = problem.New(http.StatusServiceUnavailable, "sync.dependency_unavailable", "Chưa nối phụ thuộc của module đồng bộ")
	// W6.5 (R-DB-07).
	ErrSubjectUnresolved = problem.New(http.StatusUnprocessableEntity, "evidence.subject_unresolved", "Module chủ không phân giải được chủ thể của tệp")
	ErrSubjectForbidden  = problem.New(http.StatusForbidden, "evidence.subject_forbidden", "Người gọi không được xem chủ thể của tệp minh chứng")
)

// ScopeResolver: phạm vi đơn vị hành chính của người gọi (pf/party hiện thực — interface tự khai để module
// năng lực không thêm cạnh vào chu trình party ↔ sync).
type ScopeResolver interface {
	Scope(ctx context.Context) (authz.Scope, error)
}

// ConsentChecker: đồng ý của chủ thể cho mục đích (pf/party hiện thực).
type ConsentChecker interface {
	HasConsent(ctx context.Context, subject uuid.UUID, purpose string) (bool, error)
}

// Unwrapper: kho định danh mở DEK theo mục đích (vault/identity client).
type Unwrapper interface {
	Unwrap(ctx context.Context, purpose client.Purpose, ref uuid.UUID, wrappedDEK []byte) ([]byte, error)
}

// Deps là phụ thuộc ngoài module, gốc lắp ráp truyền vào.
type Deps struct {
	Store     objstore.Store                // kho đối tượng chỉ chứa bản mã (R-40); nil → 503
	Vault     Unwrapper                     // nil → OpenContent 503
	Scope     ScopeResolver                 // nil → phạm vi rỗng: tệp có chủ thể không mở được (mặc định chặn)
	Consent   ConsentChecker                // tùy chọn
	Purposes  map[string]port.PurposePolicy // mục đích bổ sung, ràng buộc bộ ba chủ thể
	Resolvers map[string]port.Resolver      // subject_module → Resolver của module chủ
	Log       *slog.Logger
}

// Service cài đặt port.Files.
type Service struct {
	DB    tenant.Beginner // đã bọc db.ForModule(…, DBModule)
	Clock clock.Clock
	Deps
}

var _ port.Files = (*Service)(nil)

func principal(ctx context.Context) (authz.Principal, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Principal{}, problem.Unauthenticated
	}
	return p, nil
}

func validation(errs []problem.FieldError) error {
	v := problem.Validation
	for _, e := range errs {
		v = v.WithField(e.Field, e.Reason)
	}
	return v
}

// Attach lưu một tệp minh chứng đã mã hóa. Máy chủ KHÔNG giải mã (không có DEK): chỉ kiểm cấu trúc phong bì và
// SHA-256 bản mã, kiểm chủ thể qua Resolver của module chủ (R-DB-07: tồn tại, cùng tenant), ghi bản mã vào kho
// đối tượng tại files/<tenant>/<file_ref> rồi ghi siêu dữ liệu. Gửi lại cùng file_ref, cùng bản mã → bản cũ
// (created = false); khác bản mã → sync.file_ref_taken.
func (s *Service) Attach(ctx context.Context, in port.AttachInput) (f port.File, created bool, err error) {
	p, err := principal(ctx)
	if err != nil {
		return f, false, err
	}
	if s.Store == nil || s.DB == nil {
		return f, false, ErrDependency.WithDetail("chưa nối kho đối tượng")
	}
	now := s.Clock.Now()
	actual := envelope.SHA256Hex(in.Blob)
	f, errs := domain.ParseFileMeta(in.Meta, int64(len(in.Blob)), now)
	ref := ids.New()
	if in.FileRef != nil {
		r, err := ids.Parse(*in.FileRef)
		if err != nil {
			errs = append(errs, problem.FieldError{Field: "file_ref", Reason: "phải là UUIDv7"})
		}
		ref = r
	}
	if in.Subject != nil {
		errs = append(errs, domain.ValidateSubject(*in.Subject)...)
	}
	if len(errs) > 0 {
		return f, false, validation(errs)
	}
	if f.SHA256Ciphertext != actual {
		return f, false, ErrFileChecksum
	}
	if in.Subject != nil {
		if _, err := s.resolve(ctx, *in.Subject); err != nil {
			return f, false, err
		}
		sub := *in.Subject
		f.Subject = &sub
	}
	f.TenantID, f.Ref, f.UploadedBy, f.DeviceID = p.TenantID, ref, p.ActorRef, in.DeviceID
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

// resolve gọi Resolver của module chủ trong tenant của người gọi (ctx — middleware xác thực đặt). Module chủ lạ, chủ thể không có (hoặc ngoài
// tenant), loại không hỗ trợ → evidence.subject_unresolved; nguồn chưa nối → 503.
func (s *Service) resolve(ctx context.Context, sub port.Subject) (port.Resolved, error) {
	r := s.Resolvers[sub.Module]
	if r == nil {
		return port.Resolved{}, ErrSubjectUnresolved.WithDetail("module chủ " + sub.Module + " không khai Resolver")
	}
	res, err := r.Resolve(ctx, sub.Type, sub.ID) // tenant của người gọi: middleware xác thực đặt vào ctx
	switch {
	case err == nil:
		return res, nil
	case errors.Is(err, port.ErrSubjectNotFound):
		return port.Resolved{}, ErrSubjectUnresolved.WithDetail("không có " + sub.Type + " trong đơn vị").WithCause(err)
	case errors.Is(err, port.ErrSubjectTypeUnsupported):
		return port.Resolved{}, ErrSubjectUnresolved.WithDetail(sub.Module + " không cho tham chiếu loại " + sub.Type).WithCause(err)
	case errors.Is(err, port.ErrResolverUnavailable):
		return port.Resolved{}, ErrDependency.WithDetail("nguồn phân giải chủ thể của " + sub.Module + " chưa nối").WithCause(err)
	default:
		return port.Resolved{}, fmt.Errorf("evidence: phân giải %s/%s: %w", sub.Module, sub.Type, err)
	}
}

func (s *Service) fileByRef(ctx context.Context, p authz.Principal, ref uuid.UUID) (*port.File, error) {
	var f *port.File
	err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		f, e = store.FileByRef(ctx, tx, ref)
		return e
	})
	return f, err
}

// authorize là Open(purpose): vai xem minh chứng (cùng vai kho định danh đòi khi mở DEK), mục đích bắt buộc và
// được phép, người gọi thấy được chủ thể (Resolver của module chủ + phạm vi), đồng ý của hộ. Mọi quyết định sau
// khi thấy tệp — cấp hay từ chối — ghi một dòng sổ pf_files.access_log; ghi sổ lỗi → không cấp (fail-closed).
func (s *Service) authorize(ctx context.Context, ref uuid.UUID, purpose, op string) (port.File, error) {
	p, err := authz.Require(ctx, client.RoleEvidenceViewer)
	if err != nil {
		return port.File{}, err
	}
	switch {
	case purpose == "":
		return port.File{}, ErrPurposeRequired
	case !domain.Purposes[purpose] && s.Purposes[purpose].SubjectModule == "":
		return port.File{}, ErrPurposeNotAllowed
	}
	if s.DB == nil {
		return port.File{}, ErrDependency.WithDetail("chưa nối pool của pf/evidence")
	}
	f, err := s.fileByRef(ctx, p, ref)
	if err != nil {
		return port.File{}, err
	}
	if f == nil {
		return port.File{}, ErrFileNotFound
	}
	d, err := s.decide(ctx, p, *f, purpose)
	if err != nil {
		return port.File{}, err
	}
	if err := s.logAccess(ctx, p, *f, op, purpose, d.outcome); err != nil {
		return port.File{}, err
	}
	if d.deny != nil {
		return port.File{}, d.deny
	}
	return *f, nil
}

// decision là kết quả ghi sổ và lỗi trả người gọi (deny nil = cấp).
type decision struct {
	outcome string
	deny    error
}

// decide quyết định cấp hay từ chối; lỗi trả về là lỗi hạ tầng (không ghi sổ). Tệp cũ chưa có bộ ba (trước pf 000123) chỉ kiểm
// vai, mục đích, đồng ý như trước W6.5.
func (s *Service) decide(ctx context.Context, p authz.Principal, f port.File, purpose string) (decision, error) {
	needsConsent := true
	if policy, ok := s.Purposes[purpose]; ok {
		if f.Subject == nil || policy.SubjectModule == "" || policy.SubjectType == "" ||
			f.Subject.Module != policy.SubjectModule || f.Subject.Type != policy.SubjectType {
			return decision{domain.OutcomeDeniedSubject, ErrPurposeNotAllowed}, nil
		}
		needsConsent = policy.RequiresConsent
	}
	if f.Subject != nil {
		res, err := s.resolve(ctx, *f.Subject)
		var pb *problem.Problem
		switch {
		case errors.As(err, &pb) && pb.Code == ErrSubjectUnresolved.Code:
			return decision{domain.OutcomeDeniedSubject, ErrSubjectForbidden.WithDetail("chủ thể của tệp không còn phân giải được")}, nil
		case err != nil:
			return decision{}, err
		}
		sc, err := s.scope(ctx, p)
		if err != nil {
			return decision{}, err
		}
		if !domain.Visible(sc, res) {
			return decision{domain.OutcomeDeniedScope, ErrSubjectForbidden}, nil
		}
	}
	if s.Consent != nil && needsConsent {
		ok, err := s.Consent.HasConsent(ctx, f.SubjectRef, purpose)
		if err != nil {
			return decision{}, err
		}
		if !ok {
			return decision{domain.OutcomeDeniedConsent, ErrConsentMissing}, nil
		}
	}
	return decision{outcome: domain.OutcomeGranted}, nil
}

// scope là phạm vi người gọi, cùng quy tắc pf/sync: request mang ngữ cảnh KN (X-AG-Context, pf/accounts giải ở
// server) → phạm vi của ngữ cảnh; còn lại ScopeResolver; chưa nối → phạm vi rỗng (mặc định chặn).
func (s *Service) scope(ctx context.Context, p authz.Principal) (authz.Scope, error) {
	if p.KN != nil {
		return p.KN.Scope, nil
	}
	if s.Scope == nil {
		return authz.Scope{}, nil
	}
	return s.Scope.Scope(ctx)
}

func (s *Service) logAccess(ctx context.Context, p authz.Principal, f port.File, op, purpose, outcome string) error {
	a := store.Access{ID: ids.New(), TenantID: p.TenantID, FileRef: f.Ref, Op: op, Purpose: purpose, Outcome: outcome,
		Subject: f.Subject, EventTime: s.Clock.Now(), ActorRef: p.ActorRef, RuleVersion: domain.RuleVersion}
	if tid := telemetry.TraceID(ctx); tid != "" {
		a.TraceID = &tid
	}
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error { return store.InsertAccess(ctx, tx, &a) }); err != nil {
		return fmt.Errorf("evidence: ghi sổ truy cập: %w", err)
	}
	// Chỉ mã tham chiếu, mục đích, kết quả — không toạ độ, không thời điểm chụp (R-14).
	if s.Log != nil {
		s.Log.Info("truy cập tệp minh chứng", "event", "files.access", "op", op, "file_ref", f.Ref, "purpose", purpose,
			"outcome", outcome, "tenant_id", p.TenantID, "actor_ref", p.ActorRef, "trace_id", a.TraceID)
	}
	return nil
}

// Open trả siêu dữ liệu và URL tải BẢN MÃ có hạn 5 phút. Giải mã để hiển thị đi qua OpenContent.
func (s *Service) Open(ctx context.Context, ref uuid.UUID, purpose string) (port.Opened, error) {
	if s.Store == nil {
		return port.Opened{}, ErrDependency.WithDetail("chưa nối kho đối tượng")
	}
	f, err := s.authorize(ctx, ref, purpose, domain.OpMetadata)
	if err != nil {
		return port.Opened{}, err
	}
	url, err := s.Store.PresignGet(ctx, f.ObjectKey, PresignTTL)
	if err != nil {
		return port.Opened{}, err
	}
	return port.Opened{File: f, URL: url, ExpiresAt: s.Clock.Now().Add(PresignTTL)}, nil
}

// OpenContent mở tệp để hiển thị: kho định danh mở DEK (bắt buộc mục đích, ghi sổ truy cập ở kho), bản rõ chỉ
// tồn tại trong bộ nhớ của request này rồi trả thẳng cho người xem. Không ghi bản rõ ra đĩa, không lưu đệm.
func (s *Service) OpenContent(ctx context.Context, ref uuid.UUID, purpose string) (string, []byte, error) {
	if s.Store == nil || s.Vault == nil {
		return "", nil, ErrDependency.WithDetail("chưa nối kho đối tượng hoặc kho định danh")
	}
	f, err := s.authorize(ctx, ref, purpose, domain.OpContent)
	if err != nil {
		return "", nil, err
	}
	rc, _, err := s.Store.Get(ctx, f.ObjectKey)
	if err != nil {
		return "", nil, err
	}
	ct, err := io.ReadAll(io.LimitReader(rc, port.MaxFileBytes+1))
	_ = rc.Close()
	if err != nil {
		return "", nil, err
	}
	if envelope.SHA256Hex(ct) != f.SHA256Ciphertext {
		return "", nil, errors.New("evidence: bản mã trong kho đối tượng không khớp băm đã ghi")
	}
	dek, err := s.Vault.Unwrap(ctx, client.Purpose(purpose), f.SubjectRef, f.WrappedDEK)
	if err != nil {
		return "", nil, err
	}
	defer clear(dek)
	pt, err := envelope.DecryptWithDEK(dek, f.Nonce, ct)
	if err != nil {
		return "", nil, err
	}
	return f.ContentType, pt, nil
}
