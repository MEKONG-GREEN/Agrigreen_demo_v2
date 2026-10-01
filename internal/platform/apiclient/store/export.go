package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ExportFilters là ảnh chụp bộ lọc bất biến của một việc xuất (cột filters).
type ExportFilters struct {
	SeasonCode   string `json:"season_code,omitempty"`
	ProvinceCode string `json:"province_code,omitempty"`
	FromMonth    string `json:"from_month,omitempty"`
	ToMonth      string `json:"to_month,omitempty"`
}

// ExportJob là một dòng export_job.
type ExportJob struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	ClientID        uuid.UUID
	ClientTenantID  uuid.UUID
	DataTenantID    uuid.UUID
	Product         string
	ContractVersion int
	Format          string
	Filters         ExportFilters
	KApplied        int
	MaxRows         int64
	RequestID       uuid.UUID
	RequestHash     string
	IdemKey         string
	RequestedAt     time.Time
	Status          string
	Rows            *int64
	SHA256          *string
	ErrorCode       *string
	CompletedAt     *time.Time
}

const jobCols = `id, tenant_id, client_id, client_tenant_id, data_tenant_id, product, contract_version, format, filters, k_applied, max_rows,
  request_id, request_hash, idem_key, requested_at, status, rows_count, sha256, error_code, completed_at`

func scanJob(row pgx.Row) (*ExportJob, error) {
	var j ExportJob
	var raw []byte
	err := row.Scan(&j.ID, &j.TenantID, &j.ClientID, &j.ClientTenantID, &j.DataTenantID, &j.Product, &j.ContractVersion, &j.Format, &raw,
		&j.KApplied, &j.MaxRows, &j.RequestID, &j.RequestHash, &j.IdemKey, &j.RequestedAt, &j.Status, &j.Rows, &j.SHA256, &j.ErrorCode, &j.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &j.Filters); err != nil {
		return nil, err
	}
	return &j, nil
}

// InsertJob ghi việc xuất (queued).
func InsertJob(ctx context.Context, tx pgx.Tx, j ExportJob) error {
	raw, err := json.Marshal(j.Filters)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_apiclient.export_job
	    (id, tenant_id, client_id, client_tenant_id, data_tenant_id, product, contract_version, format, filters, k_applied, max_rows,
	     request_id, request_hash, idem_key, requested_at, status)
	  VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::text, $7::int, $8::text, $9::jsonb, $10::int, $11::bigint,
	     $12::uuid, $13::text, $14::text, $15::timestamptz, 'queued')`,
		j.ID, j.TenantID, j.ClientID, j.ClientTenantID, j.DataTenantID, j.Product, j.ContractVersion, j.Format, string(raw), j.KApplied, j.MaxRows,
		j.RequestID, j.RequestHash, j.IdemKey, j.RequestedAt.UTC())
	return err
}

// JobByIdem đọc việc xuất theo idem_key; nil nếu không có.
func JobByIdem(ctx context.Context, tx pgx.Tx, key string) (*ExportJob, error) {
	return scanJob(tx.QueryRow(ctx, "SELECT "+jobCols+" FROM pf_apiclient.export_job WHERE idem_key = $1::text", key))
}

// JobByID đọc (và khóa nếu lock) việc xuất; nil nếu không có.
func JobByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*ExportJob, error) {
	q := "SELECT " + jobCols + " FROM pf_apiclient.export_job WHERE id = $1::uuid"
	if lock {
		q += " FOR UPDATE"
	}
	return scanJob(tx.QueryRow(ctx, q, id))
}

// CompleteJob chuyển việc xuất sang completed.
func CompleteJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, rows int64, sha string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_apiclient.export_job SET status = 'completed', rows_count = $2::bigint, sha256 = $3::text,
	    completed_at = $4::timestamptz WHERE id = $1::uuid`, id, rows, sha, at.UTC())
	return err
}

// FailJob chuyển việc xuất sang failed với mã lỗi.
func FailJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, code string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_apiclient.export_job SET status = 'failed', error_code = $2::text, completed_at = $3::timestamptz
	    WHERE id = $1::uuid`, id, code, at.UTC())
	return err
}

// ExportFile là tệp kết quả của một việc xuất.
type ExportFile struct {
	TenantID    uuid.UUID
	JobID       uuid.UUID
	ObjectKey   string
	SHA256      string
	SizeBytes   int64
	Rows        int64
	ContentType string
}

// InsertFile ghi tệp kết quả.
func InsertFile(ctx context.Context, tx pgx.Tx, f ExportFile) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.export_file (tenant_id, job_id, object_key, sha256, size_bytes, rows_count, content_type)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::text, $5::bigint, $6::bigint, $7::text)`,
		f.TenantID, f.JobID, f.ObjectKey, f.SHA256, f.SizeBytes, f.Rows, f.ContentType)
	return err
}

// FileOf đọc tệp kết quả; nil nếu không có.
func FileOf(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) (*ExportFile, error) {
	var f ExportFile
	err := tx.QueryRow(ctx, `SELECT tenant_id, job_id, object_key, sha256, size_bytes, rows_count, content_type
	    FROM pf_apiclient.export_file WHERE job_id = $1::uuid`, jobID).
		Scan(&f.TenantID, &f.JobID, &f.ObjectKey, &f.SHA256, &f.SizeBytes, &f.Rows, &f.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}
