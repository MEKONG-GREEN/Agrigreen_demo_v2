package app

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docfile"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mục đích tệp chứng từ của orders (CHECK mkg_orders.file_object.purpose).
const (
	FilePurposeReceiptReport    = "receipt_variance_report" // biên bản chênh lệch của phiếu nhận (proto createGoodsReceipt)
	FilePurposeDeliveryEvidence = "delivery_evidence"       // ePOD / biên bản / e-bill của giao nhận (O-07)
)

// RuleFile là phiên bản quy tắc kiểm tệp (cỡ ≤ 15 MB, PDF/JPEG/PNG/WebP theo nội dung, SHA-256 khai báo khớp).
const RuleFile = "orders.file.v1"

// Mã lỗi tệp (docs/errors.md, mục mkg/orders).
var (
	ErrFileTooLarge    = problem.New(413, "orders.file_too_large", "Tệp chứng từ quá 15 MB")
	ErrFileType        = problem.New(415, "orders.file_type", "Chỉ nhận tệp PDF hoặc ảnh JPEG / PNG / WebP (nhận diện theo nội dung)")
	ErrFileChecksum    = problem.New(422, "orders.file_checksum", "SHA-256 khai báo không khớp nội dung tệp")
	ErrFileNotFound    = problem.New(404, "orders.file_not_found", "Không tìm thấy tệp chứng từ")
	ErrFilePurpose     = problem.New(422, "orders.file_purpose", "Tệp chứng từ được tải cho mục đích khác (biên bản phiếu nhận ↔ chứng từ giao nhận)")
	ErrFileStoreAbsent = problem.New(503, "orders.file_store_unavailable", "Chưa cấu hình kho đối tượng trong lãnh thổ cho tệp chứng từ")
)

func fileProblem(err error) error {
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

// FileProblem ánh xạ lỗi đọc multipart (handler) sang mã orders.file_*.
func FileProblem(err error) error { return fileProblem(err) }

// UploadInput là một tệp chứng từ tải lên.
type UploadInput struct {
	Data     []byte
	FileName string
	SHA256   string // client khai, bắt buộc
	Purpose  string
	IdemKey  string
}

// UploadFile lưu tệp chứng từ vào kho đối tượng trong lãnh thổ (ghi một lần, khóa theo băm nội dung —
// cùng nội dung tải lặp an toàn) rồi ghi siêu dữ liệu chỉ ghi thêm. Trả file_ref để phiếu nhận (biên bản chênh
// lệch) và giao nhận (ePOD / biên bản / e-bill) tham chiếu. Gọi lặp cùng Idempotency-Key → trả bản cũ.
func (s *Service) UploadFile(ctx context.Context, in UploadInput) (store.File, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer, RoleApprover)
	if err != nil {
		return store.File{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return store.File{}, false, err
	}
	if in.Purpose != FilePurposeReceiptReport && in.Purpose != FilePurposeDeliveryEvidence {
		return store.File{}, false, problem.Validation.WithField("purpose", FilePurposeReceiptReport+" | "+FilePurposeDeliveryEvidence)
	}
	chk, err := docfile.Check(in.Data, in.FileName, in.SHA256)
	if err != nil {
		return store.File{}, false, fileProblem(err)
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
		if prev.SHA256 != chk.SHA256 || prev.Purpose != in.Purpose {
			return store.File{}, false, problem.IdempotencyConflict
		}
		return *prev, true, nil
	}
	if s.Files == nil {
		return store.File{}, false, ErrFileStoreAbsent
	}
	key := docfile.ObjectKey("mkg/orders", p.TenantID.String(), chk.SHA256)
	if _, err := s.Files.Put(ctx, key, bytes.NewReader(in.Data), chk.Size, chk.SHA256, chk.ContentType); err != nil {
		return store.File{}, false, fileProblem(err)
	}
	f := store.File{ID: ids.New(), Purpose: in.Purpose, ObjectKey: key, SHA256: chk.SHA256, Size: chk.Size, ContentType: chk.ContentType,
		FileName: chk.FileName, UploadedBy: p.ActorRef, IdemKey: in.IdemKey, RuleVersion: RuleFile}
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

// File đọc siêu dữ liệu tệp chứng từ.
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

// OpenFile mở nội dung tệp chứng từ (tải qua API có kiểm vai — không cấp URL có chữ ký ra ngoài). Người gọi Close.
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

// checkFileRef kiểm file_ref của chứng từ trong giao dịch ghi: tệp đã tải qua POST /v1/orders/files phải đúng
// mục đích. file_ref chưa đăng ký (tệp ở hệ thống khác / bản ghi trước G4) vẫn được nhận — tương thích ngược.
func checkFileRef(ctx context.Context, tx pgx.Tx, ref *uuid.UUID, purpose string) error {
	if ref == nil {
		return nil
	}
	f, err := store.FileByID(ctx, tx, *ref)
	if err != nil || f == nil {
		return err
	}
	if f.Purpose != purpose {
		return ErrFilePurpose
	}
	return nil
}
