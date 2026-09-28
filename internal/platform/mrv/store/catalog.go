// Package store: truy cập DB của pf/mrv, chỉ qua pool của vai mrv_app (R-05).
// Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant (tenant.WithTenantTx).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

const versionCols = `id, tenant_id, evidence_type, version, status, provisional, source_note, setter_ref,
  approver_ref, submitted_at, decided_at, decision_note, idem_key, created_at`

func scanVersion(row pgx.Row) (domain.CatalogVersion, error) {
	var v domain.CatalogVersion
	var status string
	err := row.Scan(&v.ID, &v.TenantID, &v.EvidenceType, &v.Version, &status, &v.Provisional, &v.SourceNote, &v.SetterRef,
		&v.ApproverRef, &v.SubmittedAt, &v.DecidedAt, &v.DecisionNote, &v.IdemKey, &v.CreatedAt)
	v.Status = domain.CatalogStatus(status)
	return v, err
}

func oneVersion(ctx context.Context, tx pgx.Tx, where string, args ...any) (*domain.CatalogVersion, error) {
	v, err := scanVersion(tx.QueryRow(ctx, "SELECT "+versionCols+" FROM pf_mrv.catalog_version WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if v.Fields, err = fieldsOf(ctx, tx, v.ID); err != nil {
		return nil, err
	}
	return &v, nil
}

// LockEvidenceType tuần tự hóa việc cấp số phiên bản của một loại minh chứng.
func LockEvidenceType(ctx context.Context, tx pgx.Tx, tid uuid.UUID, evidenceType string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))", "mrv.catalog:"+tid.String()+":"+evidenceType)
	return err
}

// NextVersion trả số phiên bản kế tiếp của một loại minh chứng.
func NextVersion(ctx context.Context, tx pgx.Tx, evidenceType string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, "SELECT coalesce(max(version), 0) + 1 FROM pf_mrv.catalog_version WHERE evidence_type = $1::text", evidenceType).Scan(&n)
	return n, err
}

// HasVersion cho biết loại minh chứng đã có phiên bản danh mục nào chưa.
func HasVersion(ctx context.Context, tx pgx.Tx, evidenceType string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pf_mrv.catalog_version WHERE evidence_type = $1::text)", evidenceType).Scan(&ok)
	return ok, err
}

// InsertVersion ghi phiên bản đề xuất và sổ định nghĩa trường trong cùng giao dịch.
func InsertVersion(ctx context.Context, tx pgx.Tx, v *domain.CatalogVersion, now time.Time) error {
	err := tx.QueryRow(ctx, `INSERT INTO pf_mrv.catalog_version
	    (id, tenant_id, evidence_type, version, status, provisional, source_note, field_count, setter_ref, idem_key)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING created_at`,
		v.ID, v.TenantID, v.EvidenceType, v.Version, string(v.Status), v.Provisional, v.SourceNote, len(v.Fields), v.SetterRef, v.IdemKey).
		Scan(&v.CreatedAt)
	if err != nil {
		return err
	}
	for i := range v.Fields {
		f := &v.Fields[i]
		f.Position = i + 1
		var unit, help *string
		if f.Unit != "" {
			unit = &f.Unit
		}
		if f.HelpVi != "" {
			help = &f.HelpVi
		}
		var enum *string
		if len(f.EnumValues) > 0 {
			b, _ := json.Marshal(f.EnumValues)
			s := string(b)
			enum = &s
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pf_mrv.field_def
		    (id, tenant_id, stream_key, catalog_version_id, evidence_type, catalog_version, position, field_key, label_vi,
		     data_type, unit, required, min_value, max_value, max_scale, enum_values, help_vi, event_time, actor_ref,
		     idem_key, rule_version)
		  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::numeric, $14::numeric, $15, $16::jsonb, $17, $18,
		     $19, $20, $21)`,
			ids.New(), v.TenantID, "catalog:"+v.EvidenceType, v.ID, v.EvidenceType, v.Version, f.Position, f.Key, f.LabelVi,
			string(f.Type), unit, f.Required, decStr(f.Min), decStr(f.Max), f.MaxScale, enum, help, now.UTC(), v.SetterRef,
			"field:"+v.ID.String()+":"+f.Key, domain.RuleVersion); err != nil {
			return err
		}
	}
	return nil
}

func decStr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func fieldsOf(ctx context.Context, tx pgx.Tx, versionID uuid.UUID) ([]domain.FieldDef, error) {
	rows, err := tx.Query(ctx, `SELECT field_key, label_vi, data_type, coalesce(unit, ''), required, min_value::text,
	    max_value::text, max_scale, enum_values::text, coalesce(help_vi, ''), position
	  FROM pf_mrv.field_def WHERE catalog_version_id = $1 ORDER BY position`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.FieldDef
	for rows.Next() {
		var f domain.FieldDef
		var typ string
		var minS, maxS, enum *string
		if err := rows.Scan(&f.Key, &f.LabelVi, &typ, &f.Unit, &f.Required, &minS, &maxS, &f.MaxScale, &enum, &f.HelpVi, &f.Position); err != nil {
			return nil, err
		}
		f.Type = domain.DataType(typ)
		if f.Min, err = parseDec(minS); err != nil {
			return nil, err
		}
		if f.Max, err = parseDec(maxS); err != nil {
			return nil, err
		}
		if enum != nil {
			if err := json.Unmarshal([]byte(*enum), &f.EnumValues); err != nil {
				return nil, err
			}
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func parseDec(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// VersionByIdemKey tìm phiên bản đã ghi với idem_key (gọi lặp).
func VersionByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.CatalogVersion, error) {
	return oneVersion(ctx, tx, "idem_key = $1", key)
}

// VersionByID đọc một phiên bản; forUpdate khóa dòng.
func VersionByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.CatalogVersion, error) {
	q := "id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneVersion(ctx, tx, q, id)
}

// Approved trả phiên bản ĐÃ DUYỆT (version > 0) hoặc bản đã duyệt mới nhất (version = 0).
func Approved(ctx context.Context, tx pgx.Tx, evidenceType string, version int) (*domain.CatalogVersion, error) {
	if version > 0 {
		return oneVersion(ctx, tx, "evidence_type = $1::text AND version = $2::int AND status = 'approved'", evidenceType, version)
	}
	return oneVersion(ctx, tx, "evidence_type = $1::text AND status = 'approved' ORDER BY version DESC LIMIT 1", evidenceType)
}

// ListVersions liệt kê phiên bản (mọi trạng thái), cursor theo id (UUIDv7).
func ListVersions(ctx context.Context, tx pgx.Tx, evidenceType, status string, after uuid.UUID, limit int) ([]domain.CatalogVersion, error) {
	rows, err := tx.Query(ctx, "SELECT "+versionCols+` FROM pf_mrv.catalog_version
	    WHERE ($1::text = '' OR evidence_type = $1::text) AND ($2::text = '' OR status = $2::text) AND id > $3
	    ORDER BY id LIMIT $4`, evidenceType, status, after, limit)
	if err != nil {
		return nil, err
	}
	var out []domain.CatalogVersion
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Fields, err = fieldsOf(ctx, tx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// MarkSubmitted chuyển nháp → đã gửi duyệt.
func MarkSubmitted(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, "UPDATE pf_mrv.catalog_version SET status = 'submitted', submitted_at = $2 WHERE id = $1", id, at.UTC())
	return err
}

// MarkDecided ghi quyết định duyệt/từ chối.
func MarkDecided(ctx context.Context, tx pgx.Tx, id uuid.UUID, status domain.CatalogStatus, approver uuid.UUID, at time.Time, note *string) error {
	_, err := tx.Exec(ctx, `UPDATE pf_mrv.catalog_version SET status = $2, approver_ref = $3, decided_at = $4, decision_note = $5
	  WHERE id = $1`, id, string(status), approver, at.UTC(), note)
	return err
}

// ApprovedSummary là bản đã duyệt mới nhất của một loại minh chứng, không kèm
// định nghĩa trường (chỉ số trường).
type ApprovedSummary struct {
	Version    domain.CatalogVersion
	FieldCount int
}

// ApprovedLatest trả bản đã duyệt mới nhất (số phiên bản lớn nhất) của mỗi loại
// minh chứng, theo loại.
func ApprovedLatest(ctx context.Context, tx pgx.Tx) ([]ApprovedSummary, error) {
	rows, err := tx.Query(ctx, "SELECT DISTINCT ON (evidence_type) "+versionCols+`, field_count FROM pf_mrv.catalog_version
	    WHERE status = 'approved' ORDER BY evidence_type, version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApprovedSummary
	for rows.Next() {
		var r ApprovedSummary
		v := &r.Version
		var status string
		if err := rows.Scan(&v.ID, &v.TenantID, &v.EvidenceType, &v.Version, &status, &v.Provisional, &v.SourceNote, &v.SetterRef,
			&v.ApproverRef, &v.SubmittedAt, &v.DecidedAt, &v.DecisionNote, &v.IdemKey, &v.CreatedAt, &r.FieldCount); err != nil {
			return nil, err
		}
		v.Status = domain.CatalogStatus(status)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ApprovedSyncRow là bản đã duyệt mới nhất của một loại minh chứng kèm con trỏ
// đồng bộ: thời điểm duyệt tính bằng micro giây Unix (bất biến sau khi duyệt).
type ApprovedSyncRow struct {
	Version domain.CatalogVersion
	Cursor  int64
}

// ApprovedSince trả bản đã duyệt mới nhất của mỗi loại minh chứng có thời điểm
// duyệt (micro giây Unix) > after, theo thời điểm duyệt rồi theo loại, kèm định
// nghĩa trường. Số dòng tối đa bằng số loại minh chứng (nhỏ) nên không phân trang ở SQL.
func ApprovedSince(ctx context.Context, tx pgx.Tx, after int64) ([]ApprovedSyncRow, error) {
	rows, err := tx.Query(ctx, `SELECT id, cur FROM (
	    SELECT DISTINCT ON (evidence_type) id, evidence_type,
	           (extract(epoch FROM decided_at) * 1000000)::bigint AS cur
	      FROM pf_mrv.catalog_version WHERE status = 'approved'
	     ORDER BY evidence_type, version DESC) t
	  WHERE cur > $1 ORDER BY cur, evidence_type`, after)
	if err != nil {
		return nil, err
	}
	type ref struct {
		id  uuid.UUID
		cur int64
	}
	var refs []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.id, &r.cur); err != nil {
			rows.Close()
			return nil, err
		}
		refs = append(refs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ApprovedSyncRow, 0, len(refs))
	for _, r := range refs {
		v, err := VersionByID(ctx, tx, r.id, false)
		if err != nil {
			return nil, err
		}
		if v != nil {
			out = append(out, ApprovedSyncRow{Version: *v, Cursor: r.cur})
		}
	}
	return out, nil
}
