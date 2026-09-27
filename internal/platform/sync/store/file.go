package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
)

// InsertFile ghi siêu dữ liệu tệp. Không có UPDATE: tệp ghi một lần.
func InsertFile(ctx context.Context, tx pgx.Tx, f domain.File) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_files.file
	    (tenant_id, file_ref, subject_ref, v, alg, nonce, wrapped_dek, sha256_ciphertext, size_bytes, content_type,
	     captured_at, gps, gps_accuracy_m, object_key, uploaded_by, device_id)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
	     CASE WHEN $12::float8 IS NULL THEN NULL ELSE ST_SetSRID(ST_MakePoint($13::float8, $12::float8), 4326) END,
	     $14::float8, $15, $16, $17)`,
		f.TenantID, f.Ref, f.SubjectRef, f.V, f.Alg, f.Nonce, f.WrappedDEK, f.SHA256Ciphertext, f.Size, f.ContentType,
		f.CapturedAt, f.GPSLat, f.GPSLon, f.GPSAccuracyM, f.ObjectKey, f.UploadedBy, f.DeviceID)
	return err
}

// FileByRef đọc siêu dữ liệu tệp.
func FileByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*domain.File, error) {
	var f domain.File
	err := tx.QueryRow(ctx, `SELECT tenant_id, file_ref, subject_ref, v, alg, nonce, wrapped_dek, sha256_ciphertext, size_bytes,
	    content_type, captured_at, ST_Y(gps), ST_X(gps), gps_accuracy_m::float8, object_key, uploaded_by, device_id, created_at
	  FROM pf_files.file WHERE file_ref = $1`, ref).
		Scan(&f.TenantID, &f.Ref, &f.SubjectRef, &f.V, &f.Alg, &f.Nonce, &f.WrappedDEK, &f.SHA256Ciphertext, &f.Size,
			&f.ContentType, &f.CapturedAt, &f.GPSLat, &f.GPSLon, &f.GPSAccuracyM, &f.ObjectKey, &f.UploadedBy, &f.DeviceID, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}
