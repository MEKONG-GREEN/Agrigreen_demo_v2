package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

const jobCols = `id, tenant_id, status, filters::text, requested_by, requested_at, idem_key, object_key, package_sha256,
  snapshot_sha256, evidence_count, error_code, completed_at`

func scanJob(row pgx.Row) (domain.ExportJob, error) {
	var j domain.ExportJob
	var status, filters string
	err := row.Scan(&j.ID, &j.TenantID, &status, &filters, &j.RequestedBy, &j.RequestedAt, &j.IdemKey, &j.ObjectKey,
		&j.PackageSHA256, &j.SnapshotSHA256, &j.EvidenceCount, &j.ErrorCode, &j.CompletedAt)
	if err != nil {
		return j, err
	}
	j.Status = domain.ExportStatus(status)
	err = json.Unmarshal([]byte(filters), &j.Filters)
	return j, err
}

func oneJob(ctx context.Context, tx pgx.Tx, where string, args ...any) (*domain.ExportJob, error) {
	j, err := scanJob(tx.QueryRow(ctx, "SELECT "+jobCols+" FROM pf_mrv.export_job WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// JobByIdemKey tìm job đã tạo với idem_key.
func JobByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.ExportJob, error) {
	return oneJob(ctx, tx, "idem_key = $1::text", key)
}

// JobByID đọc một job; forUpdate khóa dòng (worker).
func JobByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.ExportJob, error) {
	q := "id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneJob(ctx, tx, q, id)
}

// InsertJob ghi job; requested_at (mốc ảnh chụp) do DB đặt.
func InsertJob(ctx context.Context, tx pgx.Tx, j *domain.ExportJob) error {
	f, err := json.Marshal(j.Filters)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_mrv.export_job (id, tenant_id, status, filters, requested_by, idem_key)
	  VALUES ($1, $2, 'queued', $3::jsonb, $4, $5) RETURNING requested_at`,
		j.ID, j.TenantID, string(f), j.RequestedBy, j.IdemKey).Scan(&j.RequestedAt)
}

// CompleteJob đánh dấu job xong.
func CompleteJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, key, pkgSHA, snapSHA string, count int, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_mrv.export_job SET status = 'completed', object_key = $2, package_sha256 = $3,
	    snapshot_sha256 = $4, evidence_count = $5, completed_at = $6, attempts = attempts + 1 WHERE id = $1`,
		id, key, pkgSHA, snapSHA, count, at.UTC())
	return err
}

// FailJob đánh dấu job hỏng vĩnh viễn với mã lỗi.
func FailJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, code string) error {
	_, err := tx.Exec(ctx, "UPDATE pf_mrv.export_job SET status = 'failed', error_code = $2, attempts = attempts + 1 WHERE id = $1", id, code)
	return err
}

// PackageRow là dòng sổ gói đã xuất.
type PackageRow struct {
	ID             uuid.UUID
	ExportID       uuid.UUID
	ObjectKey      string
	PackageSHA256  string
	SnapshotSHA256 string
	SizeBytes      int64
	EvidenceCount  int
	AsOf           time.Time
	ActorRef       uuid.UUID
	EventTime      time.Time
	Seq            int64
	RecordedAt     time.Time
}

// InsertPackage ghi sổ gói đã xuất (bất biến). idem_key 'export:<id>' bảo đảm
// một gói cho mỗi yêu cầu.
func InsertPackage(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p *PackageRow) error {
	p.ID = ids.New()
	return tx.QueryRow(ctx, `INSERT INTO pf_mrv.export_package
	    (id, tenant_id, stream_key, export_id, object_key, package_sha256, snapshot_sha256, size_bytes, evidence_count,
	     as_of, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'exports', $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING seq, recorded_at`,
		p.ID, tid, p.ExportID, p.ObjectKey, p.PackageSHA256, p.SnapshotSHA256, p.SizeBytes, p.EvidenceCount,
		p.AsOf.UTC(), p.EventTime.UTC(), p.ActorRef, "export:"+p.ExportID.String(), domain.RuleVersion).Scan(&p.Seq, &p.RecordedAt)
}

// PackagesOf trả mọi dòng sổ gói của một yêu cầu xuất (mong đúng một).
func PackagesOf(ctx context.Context, tx pgx.Tx, exportID uuid.UUID) ([]PackageRow, error) {
	rows, err := tx.Query(ctx, `SELECT id, export_id, object_key, package_sha256, snapshot_sha256, size_bytes, evidence_count,
	    as_of, actor_ref, event_time, seq, recorded_at FROM pf_mrv.export_package WHERE export_id = $1 ORDER BY seq`, exportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PackageRow
	for rows.Next() {
		var p PackageRow
		if err := rows.Scan(&p.ID, &p.ExportID, &p.ObjectKey, &p.PackageSHA256, &p.SnapshotSHA256, &p.SizeBytes, &p.EvidenceCount,
			&p.AsOf, &p.ActorRef, &p.EventTime, &p.Seq, &p.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SnapshotRow là bản hiện hành tại as_of của một chuỗi, kèm quyết định duyệt tại as_of.
type SnapshotRow struct {
	Evidence domain.Evidence
	Review   *domain.Review
}

// SnapshotAt dựng tập minh chứng của ảnh chụp: với mỗi chuỗi, phiên bản mới nhất
// đã ghi tới as_of, lọc theo bộ lọc đã giải phạm vi, cùng quyết định duyệt mới
// nhất tới as_of. Chỉ đọc sổ (không đọc bảng chiếu) nên dựng lại được về sau.
func SnapshotAt(ctx context.Context, tx pgx.Tx, f domain.ExportFilters, asOf time.Time) ([]SnapshotRow, error) {
	var from, to *time.Time
	if f.EventFrom != "" {
		t, err := time.Parse(time.RFC3339Nano, f.EventFrom)
		if err != nil {
			return nil, err
		}
		from = &t
	}
	if f.EventTo != "" {
		t, err := time.Parse(time.RFC3339Nano, f.EventTo)
		if err != nil {
			return nil, err
		}
		to = &t
	}
	units := f.AdminUnitCodes
	if units == nil {
		units = []string{}
	}
	rows, err := tx.Query(ctx, `WITH heads AS (
	    SELECT DISTINCT ON (root_id) * FROM pf_mrv.evidence
	    WHERE recorded_at <= $1::timestamptz
	    ORDER BY root_id, chain_version DESC)
	  SELECT `+evidenceCols+`, r.id, r.decision, r.reason, r.event_time, r.actor_ref, r.recorded_at, r.seq
	  FROM heads e
	  LEFT JOIN LATERAL (SELECT x.id, x.decision, x.reason, x.event_time, x.actor_ref, x.recorded_at, x.seq
	                     FROM pf_mrv.evidence_review x
	                     WHERE x.evidence_id = e.id AND x.recorded_at <= $1::timestamptz
	                     ORDER BY x.recorded_at DESC, x.seq DESC LIMIT 1) r ON true
	  WHERE ($2::text = '' OR e.evidence_type = $2::text)
	    AND ($3::bool OR e.admin_unit_code = ANY($4::text[]))
	    AND ($5::text = '' OR e.season_code = $5::text)
	    AND ($6::timestamptz IS NULL OR e.event_time >= $6::timestamptz)
	    AND ($7::timestamptz IS NULL OR e.event_time < $7::timestamptz)
	    AND ($8::text = '' OR coalesce(r.decision, 'pending') = $8::text)
	  ORDER BY e.root_id`, asOf.UTC(), f.EvidenceType, f.AllUnits, units, f.SeasonCode, from, to, f.ReviewStatus)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SnapshotRow
	for rows.Next() {
		var rid *uuid.UUID
		var dec, reason *string
		var rt, rrec *time.Time
		var ractor *uuid.UUID
		var rseq *int64
		e, err := scanEvidence(rows, &rid, &dec, &reason, &rt, &ractor, &rrec, &rseq)
		if err != nil {
			return nil, err
		}
		row := SnapshotRow{Evidence: e}
		if rid != nil {
			row.Review = &domain.Review{ID: *rid, EvidenceID: e.ID, RootID: e.RootID, Decision: domain.ReviewStatus(*dec),
				Reason: *reason, EventTime: *rt, ActorRef: *ractor, RecordedAt: *rrec, Seq: *rseq}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// HexHash đổi row_hash sang hex.
func HexHash(b []byte) string { return hex.EncodeToString(b) }

// ProductJobs trả job xuất của tenant (sản phẩm dữ liệu mrv_package_status, G5) theo id (UUIDv7 —
// thứ tự thời gian) sau after, lọc trạng thái (rỗng = mọi trạng thái), tối đa limit.
func ProductJobs(ctx context.Context, tx pgx.Tx, status string, after uuid.UUID, limit int) ([]domain.ExportJob, error) {
	rows, err := tx.Query(ctx, "SELECT "+jobCols+` FROM pf_mrv.export_job
	  WHERE ($1::text = '' OR status = $1::text) AND id > $2::uuid ORDER BY id LIMIT $3::int`, status, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ExportJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
