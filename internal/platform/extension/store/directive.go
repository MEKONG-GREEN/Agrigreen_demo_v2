package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
)

// Target là một cán bộ nhận chỉ đạo (chụp lại lúc ban hành).
type Target struct {
	ActorRef     uuid.UUID
	UnitCode     string
	ProvinceCode string
}

// directiveCols đọc chỉ đạo kèm số đối tượng đã xác nhận và trạng thái của actor ($1).
const directiveCols = `d.directive_id, d.title, d.body, d.severity, d.audience_units, d.province_codes, d.issuer_ref,
  d.issued_at, d.expires_at, d.target_count,
  (SELECT count(*) FROM pf_extension.directive_ack a WHERE a.directive_id = d.directive_id)::int,
  (SELECT a.acked_at FROM pf_extension.directive_ack a WHERE a.directive_id = d.directive_id AND a.actor_ref = $1),
  EXISTS (SELECT 1 FROM pf_extension.directive_target t WHERE t.directive_id = d.directive_id AND t.actor_ref = $1),
  d.sync_seq`

func scanDirective(row pgx.Row) (domain.Directive, error) {
	var d domain.Directive
	err := row.Scan(&d.ID, &d.Title, &d.Body, &d.Severity, &d.AudienceUnits, &d.ProvinceCodes, &d.IssuerRef,
		&d.IssuedAt, &d.ExpiresAt, &d.TargetCount, &d.AckCount, &d.AckedByMe, &d.IsTarget, &d.SyncSeq)
	return d, err
}

// InsertDirective ghi chỉ đạo và danh sách đối tượng nhận.
func InsertDirective(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d domain.Directive, idemKey string, targets []Target) error {
	if _, err := tx.Exec(ctx, `INSERT INTO pf_extension.directive
	    (tenant_id, directive_id, title, body, severity, audience_units, province_codes, issuer_ref, issued_at,
	     expires_at, target_count, idem_key)
	  VALUES ($1, $2, $3, $4, $5, $6::text[], $7::text[], $8, $9, $10, $11, $12)`,
		tid, d.ID, d.Title, d.Body, d.Severity, units(d.AudienceUnits), units(d.ProvinceCodes), d.IssuerRef,
		d.IssuedAt.UTC(), d.ExpiresAt, len(targets), idemKey); err != nil {
		return err
	}
	for _, t := range targets {
		if _, err := tx.Exec(ctx, `INSERT INTO pf_extension.directive_target
		    (tenant_id, directive_id, actor_ref, unit_code, province_code, issued_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			tid, d.ID, t.ActorRef, t.UnitCode, t.ProvinceCode, d.IssuedAt.UTC()); err != nil {
			return err
		}
	}
	return nil
}

// DirectiveByIdemKey tìm chỉ đạo đã ban hành với khóa idempotency.
func DirectiveByIdemKey(ctx context.Context, tx pgx.Tx, actor uuid.UUID, key string) (*domain.Directive, error) {
	d, err := scanDirective(tx.QueryRow(ctx, "SELECT "+directiveCols+" FROM pf_extension.directive d WHERE d.idem_key = $2", actor, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// DirectiveByID đọc một chỉ đạo (góc nhìn của actor).
func DirectiveByID(ctx context.Context, tx pgx.Tx, actor, id uuid.UUID) (*domain.Directive, error) {
	d, err := scanDirective(tx.QueryRow(ctx, "SELECT "+directiveCols+" FROM pf_extension.directive d WHERE d.directive_id = $2", actor, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// DirectiveFilter lọc danh sách chỉ đạo: actor là đối tượng nhận, HOẶC chỉ đạo có
// tỉnh/đơn vị nhận nằm trong phạm vi (người giám sát).
type DirectiveFilter struct {
	Actor      uuid.UUID
	All        bool
	Units      []string
	OnlyMine   bool
	After      int64 // sync_seq
	Limit      int
	Descending bool
}

// ListDirectives liệt kê chỉ đạo theo sync_seq.
func ListDirectives(ctx context.Context, tx pgx.Tx, f DirectiveFilter) ([]domain.Directive, error) {
	order := "ASC"
	cmp := ">"
	if f.Descending {
		order, cmp = "DESC", "<"
	}
	after := f.After
	if f.Descending && after == 0 {
		after = 1<<62 - 1
	}
	rows, err := tx.Query(ctx, "SELECT "+directiveCols+` FROM pf_extension.directive d
	  WHERE d.sync_seq `+cmp+` $2
	    AND (EXISTS (SELECT 1 FROM pf_extension.directive_target t WHERE t.directive_id = d.directive_id AND t.actor_ref = $1)
	         OR (NOT $5::boolean AND ($3::boolean OR d.audience_units && $4::text[] OR d.province_codes && $4::text[])))
	  ORDER BY d.sync_seq `+order+` LIMIT $6`, f.Actor, after, f.All, units(f.Units), f.OnlyMine, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Directive
	for rows.Next() {
		d, err := scanDirective(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// IsTarget cho biết actor có trong danh sách nhận.
func IsTarget(ctx context.Context, tx pgx.Tx, id, actor uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_extension.directive_target
	    WHERE directive_id = $1 AND actor_ref = $2)`, id, actor).Scan(&ok)
	return ok, err
}

// InsertAck ghi xác nhận đã đọc; đã có → giữ lần đầu, trả thời điểm đã ghi.
func InsertAck(ctx context.Context, tx pgx.Tx, tid, id, actor uuid.UUID, at time.Time, device *string) (time.Time, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO pf_extension.directive_ack (tenant_id, directive_id, actor_ref, acked_at, device_id)
	    VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, tid, id, actor, at.UTC(), device); err != nil {
		return time.Time{}, err
	}
	var first time.Time
	err := tx.QueryRow(ctx, "SELECT acked_at FROM pf_extension.directive_ack WHERE directive_id = $1 AND actor_ref = $2", id, actor).Scan(&first)
	return first, err
}

// PendingTargets trả đối tượng chưa xác nhận của một chỉ đạo.
func PendingTargets(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]Target, error) {
	rows, err := tx.Query(ctx, `SELECT t.actor_ref, t.unit_code, t.province_code FROM pf_extension.directive_target t
	  WHERE t.directive_id = $1 AND NOT EXISTS (SELECT 1 FROM pf_extension.directive_ack a
	    WHERE a.directive_id = t.directive_id AND a.actor_ref = t.actor_ref)
	  ORDER BY t.unit_code, t.actor_ref`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Target
	for rows.Next() {
		var t Target
		if err := rows.Scan(&t.ActorRef, &t.UnitCode, &t.ProvinceCode); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
