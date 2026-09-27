package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
)

func decStr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
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

const versionCols = `id, tenant_id, param_key, version, value_num::text, value_text, unit, effective_from, source_doc,
  correction_reason, status, setter_ref, submitted_at, approver_ref, decided_at, decision_note, idem_key, created_at`

func scanVersion(row pgx.Row) (domain.ParamVersion, error) {
	var v domain.ParamVersion
	var num *string
	var status string
	err := row.Scan(&v.ID, &v.TenantID, &v.Key, &v.Version, &num, &v.Text, &v.Unit, &v.EffectiveFrom, &v.SourceDoc,
		&v.CorrectionReason, &status, &v.SetterRef, &v.SubmittedAt, &v.ApproverRef, &v.DecidedAt, &v.DecisionNote, &v.IdemKey, &v.CreatedAt)
	if err != nil {
		return v, err
	}
	v.Status = domain.ParamStatus(status)
	v.Value, err = parseDec(num)
	return v, err
}

func oneVersion(row pgx.Row) (*domain.ParamVersion, error) {
	v, err := scanVersion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// LockParamKey tuần tự hóa việc cấp số phiên bản và phê duyệt của cùng một khóa.
func LockParamKey(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_masterdata.param:' || $1::text || ':' || $2::text, 0))",
		tenantID.String(), key)
	return err
}

// NextVersion trả số phiên bản kế tiếp của khóa (gọi sau LockParamKey).
func NextVersion(ctx context.Context, tx pgx.Tx, key string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, "SELECT coalesce(max(version), 0) + 1 FROM pf_masterdata.param_version WHERE param_key = $1", key).Scan(&n)
	return n, err
}

// HasVersions cho biết khóa đã có phiên bản nào chưa.
func HasVersions(ctx context.Context, tx pgx.Tx, key string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pf_masterdata.param_version WHERE param_key = $1)", key).Scan(&ok)
	return ok, err
}

// InsertVersion ghi một phiên bản mới (nháp, hoặc đã duyệt khi nạp dữ liệu nền).
func InsertVersion(ctx context.Context, tx pgx.Tx, v *domain.ParamVersion) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_masterdata.param_version
	    (id, tenant_id, param_key, version, value_num, value_text, unit, effective_from, source_doc, correction_reason,
	     status, setter_ref, submitted_at, approver_ref, decided_at, decision_note, idem_key)
	  VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	  RETURNING created_at`,
		v.ID, v.TenantID, v.Key, v.Version, decStr(v.Value), v.Text, v.Unit, v.EffectiveFrom.UTC(), v.SourceDoc, v.CorrectionReason,
		string(v.Status), v.SetterRef, v.SubmittedAt, v.ApproverRef, v.DecidedAt, v.DecisionNote, v.IdemKey).Scan(&v.CreatedAt)
}

// VersionByID đọc một phiên bản; forUpdate khóa dòng.
func VersionByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.ParamVersion, error) {
	q := "SELECT " + versionCols + " FROM pf_masterdata.param_version WHERE id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneVersion(tx.QueryRow(ctx, q, id))
}

// VersionByIdemKey tìm phiên bản đã đề xuất với idem_key (gọi lặp).
func VersionByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.ParamVersion, error) {
	return oneVersion(tx.QueryRow(ctx, "SELECT "+versionCols+" FROM pf_masterdata.param_version WHERE idem_key = $1", key))
}

// ListVersions liệt kê phiên bản theo id (UUIDv7), sau after (cursor). key rỗng = mọi khóa.
func ListVersions(ctx context.Context, tx pgx.Tx, key string, status string, after uuid.UUID, limit int) ([]domain.ParamVersion, error) {
	rows, err := tx.Query(ctx, "SELECT "+versionCols+` FROM pf_masterdata.param_version
	    WHERE ($1 = '' OR param_key = $1) AND ($2 = '' OR status = $2) AND id > $3 ORDER BY id LIMIT $4`, key, status, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ParamVersion
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// MarkSubmitted chuyển nháp → đã gửi duyệt.
func MarkSubmitted(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, "UPDATE pf_masterdata.param_version SET status = 'submitted', submitted_at = $2 WHERE id = $1", id, at.UTC())
	return err
}

// MarkDecided ghi quyết định phê duyệt/từ chối (trigger chặn chuyển trạng thái sai).
func MarkDecided(ctx context.Context, tx pgx.Tx, id uuid.UUID, status domain.ParamStatus, approver uuid.UUID, at time.Time, note *string) error {
	_, err := tx.Exec(ctx, `UPDATE pf_masterdata.param_version
	    SET status = $2, approver_ref = $3, decided_at = $4, decision_note = $5 WHERE id = $1`,
		id, string(status), approver, at.UTC(), note)
	return err
}

const approvedCols = `id, param_key, version, param_version_id, value_num::text, value_text, unit, effective_from, source_doc,
  setter_ref, actor_ref, event_time, supersedes_id, correction_reason, idem_key, seq, recorded_at`

func oneApproved(row pgx.Row) (*domain.Approved, error) {
	var a domain.Approved
	var num *string
	err := row.Scan(&a.ID, &a.Key, &a.Version, &a.ParamVersionID, &num, &a.Text, &a.Unit, &a.EffectiveFrom, &a.SourceDoc,
		&a.SetterRef, &a.ApproverRef, &a.EventTime, &a.SupersedesID, &a.CorrectionReason, &a.IdemKey, &a.Seq, &a.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if a.Value, err = parseDec(num); err != nil {
		return nil, err
	}
	return &a, nil
}

// ApprovedAt trả phiên bản đã duyệt có hiệu lực tại thời điểm at: effective_from
// lớn nhất ≤ at; cùng effective_from thì bản duyệt sau (seq lớn hơn — bản ghi bù) thắng.
func ApprovedAt(ctx context.Context, tx pgx.Tx, key string, at time.Time) (*domain.Approved, error) {
	return oneApproved(tx.QueryRow(ctx, "SELECT "+approvedCols+` FROM pf_masterdata.param_approved
	    WHERE param_key = $1 AND effective_from <= $2 ORDER BY effective_from DESC, seq DESC LIMIT 1`, key, at.UTC()))
}

// ApprovedSameEffective trả bản duyệt mới nhất của khóa có đúng effective_from (để làm bản ghi bù).
func ApprovedSameEffective(ctx context.Context, tx pgx.Tx, key string, eff time.Time) (*domain.Approved, error) {
	return oneApproved(tx.QueryRow(ctx, "SELECT "+approvedCols+` FROM pf_masterdata.param_approved
	    WHERE param_key = $1 AND effective_from = $2 ORDER BY seq DESC LIMIT 1`, key, eff.UTC()))
}

// ApprovedByIdemKey tìm dòng sổ đã ghi với idem_key.
func ApprovedByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Approved, error) {
	return oneApproved(tx.QueryRow(ctx, "SELECT "+approvedCols+" FROM pf_masterdata.param_approved WHERE idem_key = $1", key))
}

// InsertApproved ghi sổ phiên bản đã duyệt; DB tính seq, chuỗi băm, recorded_at.
func InsertApproved(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, a *domain.Approved) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_masterdata.param_approved
	    (id, tenant_id, stream_key, param_key, version, param_version_id, value_num, value_text, unit, effective_from,
	     source_doc, setter_ref, event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	  RETURNING seq, recorded_at`,
		a.ID, tenantID, domain.ParamStreamKey(a.Key), a.Key, a.Version, a.ParamVersionID, decStr(a.Value), a.Text, a.Unit,
		a.EffectiveFrom.UTC(), a.SourceDoc, a.SetterRef, a.EventTime.UTC(), a.SupersedesID, a.CorrectionReason, a.ApproverRef,
		a.IdemKey, domain.ParamRuleVersion).Scan(&a.Seq, &a.RecordedAt)
}
