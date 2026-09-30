package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/domain"
)

// ParamEvent là một dòng sổ param_version (đề xuất / duyệt / từ chối).
type ParamEvent struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Seq           int64
	Key           string
	VersionID     uuid.UUID
	VersionSeq    int
	Kind          string
	Value         decimal.Decimal
	EffectiveFrom time.Time
	Provisional   bool
	ProposerRef   uuid.UUID
	Reason        *string
	EventTime     time.Time
	RecordedAt    time.Time
	ActorRef      uuid.UUID
	IdemKey       string
}

// InsertParamEvent ghi sổ tham số.
func InsertParamEvent(ctx context.Context, tx pgx.Tx, e *ParamEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_logistics.param_version
	    (id, tenant_id, stream_key, param_key, version_id, version_seq, kind, value, effective_from, provisional, proposer_ref, reason,
	     event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, $9, $10, $11, $12, $13, $14, $15, $16)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, "param:"+e.Key, e.Key, e.VersionID, e.VersionSeq, e.Kind, e.Value.String(), e.EffectiveFrom, e.Provisional,
		e.ProposerRef, e.Reason, e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.RuleParam).Scan(&e.Seq, &e.RecordedAt)
}

// ParamEventByIdem tìm sự kiện tham số theo idem_key.
func ParamEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*ParamEvent, error) {
	var e ParamEvent
	var v string
	err := tx.QueryRow(ctx, `SELECT id, tenant_id, seq, param_key, version_id, version_seq, kind, value::text, effective_from, provisional,
	    proposer_ref, reason, event_time, recorded_at, actor_ref, idem_key FROM mkg_logistics.param_version WHERE idem_key = $1`, key).Scan(
		&e.ID, &e.TenantID, &e.Seq, &e.Key, &e.VersionID, &e.VersionSeq, &e.Kind, &v, &e.EffectiveFrom, &e.Provisional, &e.ProposerRef,
		&e.Reason, &e.EventTime, &e.RecordedAt, &e.ActorRef, &e.IdemKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Value, err = decimal.NewFromString(v)
	return &e, err
}

// Param là bảng chiếu param_current.
type Param struct {
	Key            string
	VersionID      uuid.UUID
	VersionSeq     int
	Value          decimal.Decimal
	EffectiveFrom  time.Time
	Provisional    bool
	Status         string // pending | approved | rejected
	ProposedBy     uuid.UUID
	ProposalReason string
	DecidedBy      *uuid.UUID
	DecidedAt      *time.Time
	DecisionReason *string
	UpdatedAt      time.Time
}

const paramCols = `param_key, version_id, version_seq, value::text, effective_from, provisional, status, proposed_by, proposal_reason,
  decided_by, decided_at, decision_reason, updated_at`

func scanParam(row pgx.Row) (Param, error) {
	var p Param
	var v string
	err := row.Scan(&p.Key, &p.VersionID, &p.VersionSeq, &v, &p.EffectiveFrom, &p.Provisional, &p.Status, &p.ProposedBy,
		&p.ProposalReason, &p.DecidedBy, &p.DecidedAt, &p.DecisionReason, &p.UpdatedAt)
	if err != nil {
		return p, err
	}
	p.Value, err = decimal.NewFromString(v)
	return p, err
}

// LockParamKey tuần tự hóa đề xuất của một khóa tham số trong tenant.
func LockParamKey(ctx context.Context, tx pgx.Tx, tid uuid.UUID, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "mkg_logistics.param:"+tid.String()+":"+key)
	return err
}

// NextParamSeq trả số phiên bản kế tiếp của khóa.
func NextParamSeq(ctx context.Context, tx pgx.Tx, key string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, "SELECT coalesce(max(version_seq), 0) + 1 FROM mkg_logistics.param_current WHERE param_key = $1", key).Scan(&n)
	return n, err
}

// GetParam đọc một phiên bản tham số; forUpdate khóa dòng.
func GetParam(ctx context.Context, tx pgx.Tx, key string, version uuid.UUID, forUpdate bool) (*Param, error) {
	q := "SELECT " + paramCols + " FROM mkg_logistics.param_current WHERE param_key = $1 AND version_id = $2"
	if forUpdate {
		q += " FOR UPDATE"
	}
	p, err := scanParam(tx.QueryRow(ctx, q, key, version))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ApplyParamEvent cập nhật bảng chiếu tham số.
func ApplyParamEvent(ctx context.Context, tx pgx.Tx, e ParamEvent) error {
	if e.Kind == "proposed" {
		_, err := tx.Exec(ctx, `INSERT INTO mkg_logistics.param_current
		    (tenant_id, param_key, version_id, version_seq, value, effective_from, provisional, status, proposed_by, proposal_reason)
		  VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, 'pending', $8, $9)`,
			e.TenantID, e.Key, e.VersionID, e.VersionSeq, e.Value.String(), e.EffectiveFrom, e.Provisional, e.ProposerRef, e.Reason)
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE mkg_logistics.param_current SET status = $3, decided_by = $4, decided_at = $5, decision_reason = $6,
	    updated_at = now() WHERE param_key = $1 AND version_id = $2`,
		e.Key, e.VersionID, e.Kind, e.ActorRef, e.EventTime.UTC(), e.Reason)
	return err
}

// Params liệt kê mọi phiên bản của khóa, mới nhất trước.
func Params(ctx context.Context, tx pgx.Tx, key string) ([]Param, error) {
	rows, err := tx.Query(ctx, "SELECT "+paramCols+" FROM mkg_logistics.param_current WHERE param_key = $1 ORDER BY version_seq DESC", key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Param
	for rows.Next() {
		p, err := scanParam(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// EffectiveParam trả phiên bản ĐÃ DUYỆT hiệu lực vào ngày day; nil = chưa có (dùng mặc định tạm).
func EffectiveParam(ctx context.Context, tx pgx.Tx, key string, day time.Time) (*Param, error) {
	p, err := scanParam(tx.QueryRow(ctx, "SELECT "+paramCols+` FROM mkg_logistics.param_current
	    WHERE param_key = $1 AND status = 'approved' AND effective_from <= $2 ORDER BY effective_from DESC, version_seq DESC LIMIT 1`, key, day))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}
