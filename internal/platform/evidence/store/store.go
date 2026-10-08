// Package store: SQL của pf/evidence trên schema pf_files (pf 000010, pf 000123). Giao dịch do app mở qua
// db.ForModule (vai r_evidence) và tenant.WithTenantTx (RLS).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
)

// InsertFile ghi siêu dữ liệu tệp. Không có UPDATE: tệp ghi một lần.
func InsertFile(ctx context.Context, tx pgx.Tx, f port.File) error {
	var sm, st *string
	var sid *uuid.UUID
	if f.Subject != nil {
		sm, st, sid = &f.Subject.Module, &f.Subject.Type, &f.Subject.ID
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_files.file
	    (tenant_id, file_ref, subject_ref, v, alg, nonce, wrapped_dek, sha256_ciphertext, size_bytes, content_type,
	     captured_at, gps, gps_accuracy_m, object_key, uploaded_by, device_id, subject_module, subject_type, subject_id)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
	     CASE WHEN $12::float8 IS NULL THEN NULL ELSE ST_SetSRID(ST_MakePoint($13::float8, $12::float8), 4326) END,
	     $14::float8, $15, $16, $17, $18, $19, $20)`,
		f.TenantID, f.Ref, f.SubjectRef, f.V, f.Alg, f.Nonce, f.WrappedDEK, f.SHA256Ciphertext, f.Size, f.ContentType,
		f.CapturedAt, f.GPSLat, f.GPSLon, f.GPSAccuracyM, f.ObjectKey, f.UploadedBy, f.DeviceID, sm, st, sid)
	return err
}

// FileByRef đọc siêu dữ liệu tệp; không có (hoặc tenant khác — RLS) → nil.
func FileByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*port.File, error) {
	var f port.File
	var sm, st *string
	var sid *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT tenant_id, file_ref, subject_ref, v, alg, nonce, wrapped_dek, sha256_ciphertext, size_bytes,
	    content_type, captured_at, ST_Y(gps), ST_X(gps), gps_accuracy_m::float8, object_key, uploaded_by, device_id, created_at,
	    subject_module, subject_type, subject_id
	  FROM pf_files.file WHERE file_ref = $1`, ref).
		Scan(&f.TenantID, &f.Ref, &f.SubjectRef, &f.V, &f.Alg, &f.Nonce, &f.WrappedDEK, &f.SHA256Ciphertext, &f.Size,
			&f.ContentType, &f.CapturedAt, &f.GPSLat, &f.GPSLon, &f.GPSAccuracyM, &f.ObjectKey, &f.UploadedBy, &f.DeviceID, &f.CreatedAt,
			&sm, &st, &sid)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if sm != nil && st != nil && sid != nil {
		f.Subject = &port.Subject{Module: *sm, Type: *st, ID: *sid}
	}
	return &f, nil
}

// Access là một dòng sổ truy cập pf_files.access_log (chỉ mã tham chiếu, mục đích, kết quả — R-14).
type Access struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	FileRef     uuid.UUID
	Op          string
	Purpose     string
	Outcome     string
	Subject     *port.Subject
	TraceID     *string
	EventTime   time.Time
	ActorRef    uuid.UUID
	RuleVersion string
	// Seq, RecordedAt: do fn_chain/DB đặt.
	Seq        int64
	RecordedAt time.Time
}

// InsertAccess ghi một quyết định mở tệp vào sổ chỉ ghi thêm (chuỗi băm theo tệp 'file:<ref>').
func InsertAccess(ctx context.Context, tx pgx.Tx, a *Access) error {
	var sm, st *string
	var sid *uuid.UUID
	if a.Subject != nil {
		sm, st, sid = &a.Subject.Module, &a.Subject.Type, &a.Subject.ID
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_files.access_log
	    (id, tenant_id, stream_key, file_ref, op, purpose, outcome, subject_module, subject_type, subject_id,
	     trace_id, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	  RETURNING seq, recorded_at`,
		a.ID, a.TenantID, "file:"+a.FileRef.String(), a.FileRef, a.Op, a.Purpose, a.Outcome, sm, st, sid,
		a.TraceID, a.EventTime.UTC(), a.ActorRef, "open:"+a.ID.String(), a.RuleVersion).Scan(&a.Seq, &a.RecordedAt)
}

// AccessByFile đọc sổ truy cập của một tệp theo seq (kiểm toán, kiểm thử).
func AccessByFile(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Access, error) {
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, file_ref, op, purpose, outcome, subject_module, subject_type, subject_id,
	    trace_id, event_time, actor_ref, rule_version, seq, recorded_at
	  FROM pf_files.access_log WHERE file_ref = $1 ORDER BY seq`, ref)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Access, error) {
		var a Access
		var sm, st *string
		var sid *uuid.UUID
		err := r.Scan(&a.ID, &a.TenantID, &a.FileRef, &a.Op, &a.Purpose, &a.Outcome, &sm, &st, &sid,
			&a.TraceID, &a.EventTime, &a.ActorRef, &a.RuleVersion, &a.Seq, &a.RecordedAt)
		if sm != nil && st != nil && sid != nil {
			a.Subject = &port.Subject{Module: *sm, Type: *st, ID: *sid}
		}
		return a, err
	})
}
