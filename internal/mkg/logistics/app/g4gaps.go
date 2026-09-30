package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docfile"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// FilePurposeTripDocument là mục đích tệp của logistics (CHECK mkg_logistics.file_object.purpose).
const FilePurposeTripDocument = "trip_document"

// Phiên bản quy tắc (G4 đóng khoảng trống).
const (
	RuleFile         = "logistics.file.v1"          // ≤ 15 MB, PDF/JPEG/PNG/WebP theo nội dung, SHA-256 khai báo khớp
	RuleTripDocument = "logistics.trip_document.v1" // chứng từ chuyến có người ký + thời điểm ký (L-11)
)

// Mã lỗi tệp và chứng từ chuyến (docs/errors.md, mục mkg/logistics).
var (
	ErrFileTooLarge     = problem.New(413, "logistics.file_too_large", "Tệp chứng từ quá 15 MB")
	ErrFileType         = problem.New(415, "logistics.file_type", "Chỉ nhận tệp PDF hoặc ảnh JPEG / PNG / WebP (nhận diện theo nội dung)")
	ErrFileChecksum     = problem.New(422, "logistics.file_checksum", "SHA-256 khai báo không khớp nội dung tệp")
	ErrFileNotFound     = problem.New(404, "logistics.file_not_found", "Không tìm thấy tệp chứng từ (tải qua POST /v1/logistics/files trước)")
	ErrFileStoreAbsent  = problem.New(503, "logistics.file_store_unavailable", "Chưa cấu hình kho đối tượng trong lãnh thổ cho tệp chứng từ")
	ErrDocumentNotFound = problem.New(404, "logistics.trip_document_not_found", "Không tìm thấy chứng từ chuyến")
)

// FileProblem ánh xạ lỗi kiểm / đọc tệp sang mã logistics.file_*.
func FileProblem(err error) error {
	switch {
	case errors.Is(err, docfile.ErrTooLarge):
		return ErrFileTooLarge
	case errors.Is(err, docfile.ErrType):
		return ErrFileType
	case errors.Is(err, docfile.ErrChecksum), errors.Is(err, objstore.ErrChecksumMismatch):
		return ErrFileChecksum
	case errors.Is(err, docfile.ErrEmpty):
		return problem.Validation.WithField("file", "tệp rỗng")
	case errors.Is(err, docfile.ErrMissing):
		return problem.Validation.WithField("file", "thiếu phần multipart \"file\"")
	}
	return err
}

// UploadInput là một tệp chứng từ chuyến tải lên.
type UploadInput struct {
	Data     []byte
	FileName string
	SHA256   string
	IdemKey  string
}

// UploadFile lưu tệp chứng từ chuyến (ePOD / e-bill / biên bản) vào kho đối tượng trong lãnh thổ (ghi một lần,
// khóa theo băm nội dung) rồi ghi siêu dữ liệu chỉ ghi thêm; trả file_ref. Điều phối và tài xế tải được.
func (s *Service) UploadFile(ctx context.Context, in UploadInput) (store.File, bool, error) {
	p, err := authz.Require(ctx, RoleDispatcher, RoleDriver)
	if err != nil {
		return store.File{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return store.File{}, false, err
	}
	chk, err := docfile.Check(in.Data, in.FileName, in.SHA256)
	if err != nil {
		return store.File{}, false, FileProblem(err)
	}
	var prev *store.File
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.FileByIdem(ctx, tx, in.IdemKey)
		return e
	}); err != nil {
		return store.File{}, false, err
	}
	if prev != nil {
		if prev.SHA256 != chk.SHA256 {
			return store.File{}, false, problem.IdempotencyConflict
		}
		return *prev, true, nil
	}
	if s.Files == nil {
		return store.File{}, false, ErrFileStoreAbsent
	}
	key := docfile.ObjectKey("mkg/logistics", p.TenantID.String(), chk.SHA256)
	if _, err := s.Files.Put(ctx, key, bytes.NewReader(in.Data), chk.Size, chk.SHA256, chk.ContentType); err != nil {
		return store.File{}, false, FileProblem(err)
	}
	f := store.File{ID: ids.New(), Purpose: FilePurposeTripDocument, ObjectKey: key, SHA256: chk.SHA256, Size: chk.Size,
		ContentType: chk.ContentType, FileName: chk.FileName, UploadedBy: p.ActorRef, IdemKey: in.IdemKey, RuleVersion: RuleFile}
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error { return store.InsertFile(ctx, tx, p.TenantID, &f) },
		func(tx pgx.Tx) (bool, error) {
			x, err := store.FileByIdem(ctx, tx, in.IdemKey)
			if err != nil || x == nil {
				return false, err
			}
			f = *x
			return true, nil
		})
	return f, replayed, err
}

// File đọc siêu dữ liệu tệp.
func (s *Service) File(ctx context.Context, id uuid.UUID) (store.File, error) {
	var out store.File
	err := s.read(ctx, func(tx pgx.Tx) error {
		f, err := store.FileByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if f == nil {
			return ErrFileNotFound
		}
		out = *f
		return nil
	})
	return out, err
}

// OpenFile mở nội dung tệp (tải qua API có kiểm vai). Người gọi Close.
func (s *Service) OpenFile(ctx context.Context, id uuid.UUID) (io.ReadCloser, store.File, error) {
	f, err := s.File(ctx, id)
	if err != nil {
		return nil, f, err
	}
	if s.Files == nil {
		return nil, f, ErrFileStoreAbsent
	}
	rc, _, err := s.Files.Get(ctx, f.ObjectKey)
	if errors.Is(err, objstore.ErrNotFound) {
		return nil, f, ErrFileNotFound
	}
	return rc, f, err
}

// DocumentInput là một chứng từ chuyến có người ký.
type DocumentInput struct {
	Kind             string // epod | ebill | minutes
	FileRef          uuid.UUID
	SignerRole       string // driver | consignee | hub_operator | dispatcher
	SignerRef        *uuid.UUID
	SignedAt         time.Time
	Note             string
	SupersedesID     *uuid.UUID
	CorrectionReason string
	DeviceID         *string
	IdemKey          string
}

var docKinds = map[string]bool{"epod": true, "ebill": true, "minutes": true}
var signerRoles = map[string]bool{"driver": true, "consignee": true, "hub_operator": true, "dispatcher": true}

// RecordDocument ghi chứng từ chuyến ePOD / e-bill / biên bản (L-11, proto ePOD): tệp đã tải (file_ref của
// logistics, băm chép vào sổ), người ký là *_ref (không tên), thời điểm ký. Điều phối hoặc tài xế được giao; chuyến
// không bị hủy. Sửa sai = bản bù supersedes_id + lý do (RB-1), không sửa dòng cũ.
func (s *Service) RecordDocument(ctx context.Context, trip uuid.UUID, in DocumentInput) (store.TripDocument, bool, error) {
	p, err := authz.Require(ctx, RoleDispatcher, RoleDriver)
	if err != nil {
		return store.TripDocument{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return store.TripDocument{}, false, err
	}
	switch {
	case !docKinds[in.Kind]:
		return store.TripDocument{}, false, problem.Validation.WithField("kind", "epod | ebill | minutes")
	case !signerRoles[in.SignerRole]:
		return store.TripDocument{}, false, problem.Validation.WithField("signer_role", "driver | consignee | hub_operator | dispatcher")
	case in.FileRef == uuid.Nil:
		return store.TripDocument{}, false, problem.Validation.WithField("file_ref", "bắt buộc — tải tệp qua POST /v1/logistics/files")
	case in.SignedAt.IsZero():
		return store.TripDocument{}, false, problem.Validation.WithField("signed_at", "bắt buộc, RFC 3339")
	case in.SignedAt.After(s.Clock.Now().Add(5 * time.Minute)):
		return store.TripDocument{}, false, problem.Validation.WithField("signed_at", "không được ở tương lai")
	case len([]rune(in.Note)) > 500:
		return store.TripDocument{}, false, problem.Validation.WithField("note", "tối đa 500 ký tự")
	}
	var reason *string
	if in.SupersedesID != nil {
		r, err := domain.CheckReason("correction_reason", in.CorrectionReason, true)
		if err != nil {
			return store.TripDocument{}, false, mapErr(err)
		}
		reason = &r
	}
	var out store.TripDocument
	replay := func(tx pgx.Tx) (bool, error) {
		d, err := store.TripDocumentByIdem(ctx, tx, in.IdemKey)
		if err != nil || d == nil {
			return false, err
		}
		out = *d
		return true, nil
	}
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, err := loadTrip(ctx, tx, p.TenantID, trip)
		if err != nil {
			return err
		}
		if !p.Has(RoleDispatcher) && !assignedTo(t, p) {
			return ErrNotAssignedDriver
		}
		if t.Status == port.TripCancelled {
			return ErrTripClosed
		}
		f, err := store.FileByID(ctx, tx, in.FileRef)
		if err != nil {
			return err
		}
		if f == nil {
			return ErrFileNotFound
		}
		if in.SupersedesID != nil {
			prev, err := store.TripDocumentByID(ctx, tx, *in.SupersedesID)
			if err != nil {
				return err
			}
			if prev == nil || prev.TripID != trip {
				return ErrDocumentNotFound
			}
		}
		d := store.TripDocument{ID: ids.New(), TenantID: p.TenantID, TripID: trip, Kind: in.Kind, FileRef: f.ID, FileSHA256: f.SHA256,
			SignerRole: in.SignerRole, SignerRef: in.SignerRef, SignedAt: in.SignedAt, Note: strPtr(in.Note), EventTime: s.Clock.Now(),
			SupersedesID: in.SupersedesID, CorrectionReason: reason, ActorRef: p.ActorRef, DeviceID: in.DeviceID, IdemKey: in.IdemKey,
			RuleVersion: RuleTripDocument}
		if err := insert(store.InsertTripDocument(ctx, tx, &d)); err != nil {
			return err
		}
		out = d
		return nil
	}, replay)
	if err == nil && replayed && out.TripID != trip {
		return store.TripDocument{}, false, problem.IdempotencyConflict
	}
	return out, replayed, err
}

// Documents liệt kê chứng từ của chuyến (điều phối, tài xế được giao).
func (s *Service) Documents(ctx context.Context, trip uuid.UUID) ([]store.TripDocument, error) {
	p, _ := authz.FromContext(ctx)
	var out []store.TripDocument
	err := s.read(ctx, func(tx pgx.Tx) error {
		t, err := store.GetTrip(ctx, tx, trip, false)
		if err != nil {
			return err
		}
		if t == nil || (DriverOnly(p) && !assignedTo(t, p)) {
			return ErrTripNotFound
		}
		out, err = store.TripDocuments(ctx, tx, trip)
		return err
	})
	return out, err
}

// VesselVersions liệt kê lịch sử phiên bản danh mục của một phương tiện (điều phối), version tăng dần; con trỏ
// = version cuối trang.
func (s *Service) VesselVersions(ctx context.Context, vessel uuid.UUID, afterVersion, limit int) ([]store.VesselEvent, *int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []store.VesselEvent
	err := s.read(ctx, func(tx pgx.Tx) error {
		v, err := store.GetVessel(ctx, tx, vessel, false)
		if err != nil {
			return err
		}
		if v == nil {
			return ErrVesselNotFound
		}
		out, err = store.VesselVersions(ctx, tx, vessel, afterVersion, limit+1)
		return err
	}, RoleDispatcher)
	var next *int
	if len(out) > limit {
		out = out[:limit]
		n := out[limit-1].Version
		next = &n
	}
	return out, next, err
}
