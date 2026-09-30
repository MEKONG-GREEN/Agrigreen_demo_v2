package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ParamEvent là một dòng sổ param_version (đề xuất / duyệt / từ chối).
type ParamEvent struct {
	Meta
	Key           string
	VersionID     uuid.UUID
	VersionSeq    int
	Kind          string
	Value         decimal.Decimal
	EffectiveFrom time.Time
	Provisional   bool
	ProposerRef   uuid.UUID
	Reason        *string
}

// InsertParamEvent ghi sổ tham số và cập nhật bảng chiếu (cùng giao dịch).
func InsertParamEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *ParamEvent) error {
	err := tx.QueryRow(ctx, `INSERT INTO mkg_orders.param_version
	    (id, tenant_id, stream_key, param_key, version_id, version_seq, kind, value, effective_from, provisional, proposer_ref, reason,
	     event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, $9, $10, $11, $12, $13, $14, $15, $16)
	  RETURNING seq, recorded_at`,
		e.ID, tid, "param:"+e.Key, e.Key, e.VersionID, e.VersionSeq, e.Kind, e.Value.String(), e.EffectiveFrom, e.Provisional, e.ProposerRef,
		e.Reason, e.EventTime.UTC(), e.ActorRef, e.IdemKey, e.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
	if err != nil {
		return err
	}
	_, err = rebuildParams(ctx, tx, "p.version_id = $1", e.VersionID)
	return err
}

// projectParams dựng param_current từ sổ: dòng 'proposed' + quyết định (nếu có) của mỗi phiên bản.
func rebuildParams(ctx context.Context, tx pgx.Tx, where string, args ...any) (int64, error) {
	del := "DELETE FROM mkg_orders.param_current p"
	if where != "" {
		del += " WHERE " + where
	} else {
		where = "true"
	}
	if _, err := tx.Exec(ctx, del, args...); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO mkg_orders.param_current
	    (tenant_id, param_key, version_id, version_seq, value, effective_from, provisional, status, proposed_by, proposal_reason, decided_by,
	     decided_at, decision_reason, last_seq)
	  SELECT p.tenant_id, p.param_key, p.version_id, p.version_seq, p.value, p.effective_from, p.provisional, coalesce(d.kind, 'pending'),
	         p.proposer_ref, p.reason, d.actor_ref, d.event_time, d.reason, coalesce(d.seq, p.seq)
	  FROM mkg_orders.param_version p
	  LEFT JOIN mkg_orders.param_version d ON d.version_id = p.version_id AND d.kind <> 'proposed'
	  WHERE p.kind = 'proposed' AND `+where, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Param là bảng chiếu param_current.
type Param struct {
	Key            string
	VersionID      uuid.UUID
	VersionSeq     int
	Value          decimal.Decimal
	EffectiveFrom  time.Time
	Provisional    bool
	Status         string
	ProposedBy     uuid.UUID
	ProposalReason string
	DecidedBy      *uuid.UUID
	DecidedAt      *time.Time
	DecisionReason *string
}

const paramSelect = `param_key, version_id, version_seq, value::text, effective_from, provisional, status, proposed_by, proposal_reason,
  decided_by, decided_at, decision_reason`

func scanParam(row pgx.Row) (Param, error) {
	var p Param
	err := row.Scan(&p.Key, &p.VersionID, &p.VersionSeq, &p.Value, &p.EffectiveFrom, &p.Provisional, &p.Status, &p.ProposedBy,
		&p.ProposalReason, &p.DecidedBy, &p.DecidedAt, &p.DecisionReason)
	return p, err
}

// ParamEventByIdem trả phiên bản ghi với idem_key.
func ParamEventByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Param, error) {
	return noRows(scanParam(tx.QueryRow(ctx, "SELECT "+paramSelect+` FROM mkg_orders.param_current
	    WHERE version_id = (SELECT version_id FROM mkg_orders.param_version WHERE idem_key = $1 LIMIT 1)`, idem)))
}

// NextParamSeq trả số phiên bản kế tiếp của khóa (gọi sau Lock "param:<key>").
func NextParamSeq(ctx context.Context, tx pgx.Tx, key string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, "SELECT coalesce(max(version_seq), 0) + 1 FROM mkg_orders.param_current WHERE param_key = $1", key).Scan(&n)
	return n, err
}

// GetParam đọc một phiên bản; forUpdate khóa dòng.
func GetParam(ctx context.Context, tx pgx.Tx, key string, version uuid.UUID, forUpdate bool) (*Param, error) {
	q := "SELECT " + paramSelect + " FROM mkg_orders.param_current WHERE param_key = $1 AND version_id = $2"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return noRows(scanParam(tx.QueryRow(ctx, q, key, version)))
}

// Params liệt kê mọi phiên bản của khóa, mới nhất trước.
func Params(ctx context.Context, tx pgx.Tx, key string) ([]Param, error) {
	rows, err := tx.Query(ctx, "SELECT "+paramSelect+" FROM mkg_orders.param_current WHERE param_key = $1 ORDER BY version_seq DESC", key)
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
	return noRows(scanParam(tx.QueryRow(ctx, "SELECT "+paramSelect+` FROM mkg_orders.param_current
	    WHERE param_key = $1 AND status = 'approved' AND effective_from <= $2 ORDER BY effective_from DESC, version_seq DESC LIMIT 1`, key, day)))
}
