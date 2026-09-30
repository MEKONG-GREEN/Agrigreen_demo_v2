package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// File là siêu dữ liệu một tệp chứng từ vùng mkg (mkg_logistics.file_object, chỉ ghi thêm — mkg 000021).
type File struct {
	ID          uuid.UUID
	Purpose     string
	ObjectKey   string
	SHA256      string
	Size        int64
	ContentType string
	FileName    string
	UploadedBy  uuid.UUID
	IdemKey     string
	RuleVersion string
	RecordedAt  time.Time
}

const fileCols = `id, purpose, object_key, sha256, size_bytes, content_type, file_name, uploaded_by, idem_key, rule_version, recorded_at`

func scanFile(r pgx.Row) (*File, error) {
	var f File
	err := r.Scan(&f.ID, &f.Purpose, &f.ObjectKey, &f.SHA256, &f.Size, &f.ContentType, &f.FileName, &f.UploadedBy, &f.IdemKey,
		&f.RuleVersion, &f.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// InsertFile ghi siêu dữ liệu tệp (trùng idem_key → lỗi unique, app/ phát lại).
func InsertFile(ctx context.Context, tx pgx.Tx, tid uuid.UUID, f *File) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_logistics.file_object (tenant_id, id, purpose, object_key, sha256, size_bytes, content_type,
	    file_name, uploaded_by, idem_key, rule_version) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING recorded_at`,
		tid, f.ID, f.Purpose, f.ObjectKey, f.SHA256, f.Size, f.ContentType, f.FileName, f.UploadedBy, f.IdemKey, f.RuleVersion).Scan(&f.RecordedAt)
}

// FileByID đọc siêu dữ liệu tệp; nil nếu không có (RLS: chỉ tenant hiện hành).
func FileByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*File, error) {
	return scanFile(tx.QueryRow(ctx, "SELECT "+fileCols+" FROM mkg_logistics.file_object WHERE id = $1", id))
}

// FileByIdem đọc tệp theo khóa idempotency của lần tải.
func FileByIdem(ctx context.Context, tx pgx.Tx, key string) (*File, error) {
	return scanFile(tx.QueryRow(ctx, "SELECT "+fileCols+" FROM mkg_logistics.file_object WHERE idem_key = $1", key))
}
