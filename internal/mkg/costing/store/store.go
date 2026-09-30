// Package store: SQL của mkg/costing (schema mkg_costing, vai costing_app). Mọi hàm chạy trong
// giao dịch tenant đã mở (RLS). Sổ chỉ INSERT; bảng chiếu *_current cập nhật cùng giao dịch
// (R-21) và dựng lại được từ sổ (R-22). Số tiền / khối lượng đọc bằng ::text (không float, R-10).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// LockKey khóa tư vấn trong giao dịch cho một khóa nghiệp vụ (chuỗi sổ, kỳ, lệnh gom).
func LockKey(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 604571))", key)
	return err
}

func one[T any](row T, err error) (*T, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func kgPtr(s *string) (*mass.Kg, error) {
	if s == nil {
		return nil, nil
	}
	k, err := mass.ParseKg(*s)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func amountPtr(s *string) (*money.Amount, error) {
	if s == nil {
		return nil, nil
	}
	a, err := money.ParseAmount(*s)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func kgArg(k *mass.Kg) *string {
	if k == nil {
		return nil
	}
	s := k.String()
	return &s
}

func amountArg(a *money.Amount) *string {
	if a == nil {
		return nil
	}
	s := a.String()
	return &s
}

// pinsJSON trả jsonb dạng CHUỖI: ở QueryExecModeExec (PgBouncer) []byte bị mã hóa thành bytea.
func pinsJSON(p domain.Pins) string {
	if p == nil {
		p = domain.Pins{}
	}
	b, _ := json.Marshal(p)
	return string(b)
}

// ---------------------------------------------------------------------------
// Tham số có phiên bản
// ---------------------------------------------------------------------------

// ParamStream là chuỗi sổ của một khóa tham số.
func ParamStream(key string) string { return "param:" + key }

// ParamVersion là một phiên bản tham số kèm quyết định (nếu có).
type ParamVersion struct {
	ID            uuid.UUID
	Key           string
	Seq           int64
	Value         string
	Reason        string
	EffectiveFrom time.Time
	EventTime     time.Time
	RecordedAt    time.Time
	ProposedBy    uuid.UUID
	IdemKey       string
	Decision      *string
	DecidedBy     *uuid.UUID
	DecidedAt     *time.Time
	DecisionNote  *string
}

const paramCols = `v.id, v.param_key, v.seq, v.value, v.reason, v.effective_from, v.event_time, v.recorded_at, v.actor_ref, v.idem_key,
  d.decision, d.actor_ref, d.recorded_at, d.reason`

const paramFrom = ` FROM mkg_costing.param_version v
  LEFT JOIN mkg_costing.param_decision d ON d.version_id = v.id`

func scanParam(r pgx.Row) (ParamVersion, error) {
	var p ParamVersion
	err := r.Scan(&p.ID, &p.Key, &p.Seq, &p.Value, &p.Reason, &p.EffectiveFrom, &p.EventTime, &p.RecordedAt, &p.ProposedBy, &p.IdemKey,
		&p.Decision, &p.DecidedBy, &p.DecidedAt, &p.DecisionNote)
	return p, err
}

// InsertParamVersion ghi một phiên bản đề xuất (seq do DB tính = số phiên bản).
func InsertParamVersion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p *ParamVersion) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_costing.param_version
	  (id, tenant_id, stream_key, param_key, value, reason, effective_from, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING seq, recorded_at`,
		p.ID, tid, ParamStream(p.Key), p.Key, p.Value, p.Reason, p.EffectiveFrom, p.EventTime, p.ProposedBy, p.IdemKey, domain.RuleParams).
		Scan(&p.Seq, &p.RecordedAt)
}

// ParamVersionByIdem đọc phiên bản theo idem_key.
func ParamVersionByIdem(ctx context.Context, tx pgx.Tx, idem string) (*ParamVersion, error) {
	return one(scanParam(tx.QueryRow(ctx, "SELECT "+paramCols+paramFrom+" WHERE v.idem_key = $1", idem)))
}

// ParamVersionBySeq đọc phiên bản theo khóa + số.
func ParamVersionBySeq(ctx context.Context, tx pgx.Tx, key string, seq int64) (*ParamVersion, error) {
	return one(scanParam(tx.QueryRow(ctx, "SELECT "+paramCols+paramFrom+" WHERE v.param_key = $1 AND v.seq = $2", key, seq)))
}

// ParamVersions liệt kê phiên bản của một khóa (mới trước).
func ParamVersions(ctx context.Context, tx pgx.Tx, key string) ([]ParamVersion, error) {
	rows, err := tx.Query(ctx, "SELECT "+paramCols+paramFrom+" WHERE v.param_key = $1 ORDER BY v.seq DESC", key)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ParamVersion, error) { return scanParam(r) })
}

// PendingParam cho biết khóa có phiên bản chờ duyệt.
func PendingParam(ctx context.Context, tx pgx.Tx, key string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1`+paramFrom+` WHERE v.param_key = $1 AND d.id IS NULL)`, key).Scan(&ok)
	return ok, err
}

// ParamDecision là quyết định với một phiên bản.
type ParamDecision struct {
	ID          uuid.UUID
	Key         string
	VersionID   uuid.UUID
	VersionSeq  int64
	Decision    string
	Reason      *string
	ProposerRef uuid.UUID
	ActorRef    uuid.UUID
	EventTime   time.Time
	IdemKey     string
}

// InsertParamDecision ghi quyết định (CHECK actor_ref <> proposer_ref ở DB — R-56).
func InsertParamDecision(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d ParamDecision) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_costing.param_decision
	  (id, tenant_id, stream_key, param_key, version_id, version_seq, decision, reason, proposer_ref, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		d.ID, tid, ParamStream(d.Key), d.Key, d.VersionID, d.VersionSeq, d.Decision, d.Reason, d.ProposerRef, d.EventTime, d.ActorRef,
		d.IdemKey, domain.RuleParams)
	return ledger.Classify(err)
}

// ParamDecisionByIdem đọc (khóa, số phiên bản) của quyết định theo idem_key.
func ParamDecisionByIdem(ctx context.Context, tx pgx.Tx, idem string) (key string, seq int64, ok bool, err error) {
	err = tx.QueryRow(ctx, "SELECT param_key, version_seq FROM mkg_costing.param_decision WHERE idem_key = $1", idem).Scan(&key, &seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, false, nil
	}
	return key, seq, err == nil, err
}

// ParamAt trả tham số áp dụng cho sự việc tại eventTime, theo quyết định đã biết lúc asOf (R-13):
// phiên bản đã duyệt có effective_from ≤ eventTime và quyết định recorded_at ≤ asOf, mới nhất trước.
// Không có → mặc định #0 (tạm, chờ khách).
func ParamAt(ctx context.Context, tx pgx.Tx, key string, eventTime, asOf time.Time) (domain.ParamValue, error) {
	var v domain.ParamValue
	err := tx.QueryRow(ctx, `SELECT v.value, v.seq`+paramFrom+`
	  WHERE v.param_key = $1 AND d.decision = 'approved' AND v.effective_from <= $2 AND d.recorded_at <= $3
	  ORDER BY v.effective_from DESC, v.seq DESC LIMIT 1`, key, eventTime, asOf).Scan(&v.Value, &v.Seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Default(key), nil
	}
	v.Key = key
	return v, err
}
