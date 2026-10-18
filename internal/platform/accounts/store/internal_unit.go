package store

// W11.07 (migration pf 000142): sổ + bảng chiếu đơn vị nội bộ. Hàm nhận pgx.Tx đã ở ngữ cảnh tenant.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// UnitEvent là một dòng sổ internal_unit_event; U là trạng thái SAU sự việc.
type UnitEvent struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Action    string
	U         port.InternalUnit
	Before    []byte
	After     []byte
	Reason    *string
	EventTime time.Time
	ActorRef  uuid.UUID
	IdemKey   string
	Seq       int64
}

// InsertUnitEvent ghi sổ đơn vị nội bộ; seq do DB tính.
func InsertUnitEvent(ctx context.Context, tx pgx.Tx, e *UnitEvent) error {
	u := e.U
	return tx.QueryRow(ctx, `INSERT INTO pf_accounts.internal_unit_event
	    (id, tenant_id, stream_key, unit_id, org_ref, code, kind, action, before, after, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10::jsonb, $11, $12, $13, $14, $15) RETURNING seq`,
		e.ID, e.TenantID, "iunit:"+u.ID.String(), u.ID, u.OrgRef, u.Code, u.Kind, e.Action, jsonOrNil(e.Before), string(e.After),
		e.Reason, e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.InternalUnitRuleVersion).Scan(&e.Seq)
}

// UnitEventByIdem trả unit_id của dòng sổ đã ghi với idem_key (ok = false nếu chưa có).
func UnitEventByIdem(ctx context.Context, tx pgx.Tx, key string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, "SELECT unit_id FROM pf_accounts.internal_unit_event WHERE idem_key = $1"+ledger.NewestByIdem(""), key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}

// UnitLogRow là một dòng khung Log của đơn vị nội bộ.
type UnitLogRow struct {
	Seq       int64
	Action    string
	Before    []byte
	After     []byte
	Reason    *string
	EventTime time.Time
	ActorRef  uuid.UUID
}

// UnitEvents trả sổ của một đơn vị nội bộ, seq > after, tăng dần.
func UnitEvents(ctx context.Context, tx pgx.Tx, id uuid.UUID, after int64, limit int) ([]UnitLogRow, error) {
	rows, err := tx.Query(ctx, `SELECT seq, action, before, after, reason, event_time, actor_ref FROM pf_accounts.internal_unit_event
	  WHERE stream_key = $1 AND seq > $2 ORDER BY seq LIMIT $3`, "iunit:"+id.String(), after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UnitLogRow{}
	for rows.Next() {
		var r UnitLogRow
		if err := rows.Scan(&r.Seq, &r.Action, &r.Before, &r.After, &r.Reason, &r.EventTime, &r.ActorRef); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

const unitCols = `id, org_ref, code, kind, title, coalesce(department_code, ''), sort_order, active, updated_by, updated_at`

func scanUnit(row pgx.Row) (*port.InternalUnit, error) {
	var u port.InternalUnit
	err := row.Scan(&u.ID, &u.OrgRef, &u.Code, &u.Kind, &u.Name, &u.DepartmentCode, &u.SortOrder, &u.Active, &u.UpdatedBy, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// InternalUnitByID đọc (và khóa nếu lock) một đơn vị nội bộ; nil nếu không có.
func InternalUnitByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*port.InternalUnit, error) {
	q := "SELECT " + unitCols + " FROM pf_accounts.internal_unit WHERE id = $1"
	if lock {
		q += " FOR UPDATE"
	}
	return scanUnit(tx.QueryRow(ctx, q, id))
}

// InternalUnitByCode đọc đơn vị nội bộ của đơn vị theo mã (kể cả đã ngừng); nil nếu không có.
func InternalUnitByCode(ctx context.Context, tx pgx.Tx, orgRef uuid.UUID, code string) (*port.InternalUnit, error) {
	return scanUnit(tx.QueryRow(ctx, "SELECT "+unitCols+" FROM pf_accounts.internal_unit WHERE org_ref = $1 AND code = $2", orgRef, code))
}

// ListInternalUnits trả đơn vị nội bộ của đơn vị theo thứ tự hiển thị.
func ListInternalUnits(ctx context.Context, tx pgx.Tx, orgRef uuid.UUID, includeInactive bool) ([]port.InternalUnit, error) {
	rows, err := tx.Query(ctx, "SELECT "+unitCols+` FROM pf_accounts.internal_unit
	    WHERE org_ref = $1 AND ($2::boolean OR active) ORDER BY sort_order, code`, orgRef, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []port.InternalUnit{}
	for rows.Next() {
		u, err := scanUnit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// PutInternalUnit ghi bảng chiếu đơn vị nội bộ (cùng giao dịch với sổ, R-21).
func PutInternalUnit(ctx context.Context, tx pgx.Tx, e UnitEvent) error {
	u := e.U
	_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.internal_unit
	    (tenant_id, id, org_ref, code, kind, title, department_code, sort_order, active, last_event_id, last_seq, updated_by, created_at, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13)
	  ON CONFLICT (tenant_id, id) DO UPDATE SET kind = EXCLUDED.kind, title = EXCLUDED.title, department_code = EXCLUDED.department_code,
	    sort_order = EXCLUDED.sort_order, active = EXCLUDED.active, last_event_id = EXCLUDED.last_event_id, last_seq = EXCLUDED.last_seq,
	    updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		e.TenantID, u.ID, u.OrgRef, u.Code, u.Kind, u.Name, strPtr(u.DepartmentCode), u.SortOrder, u.Active, e.ID, e.Seq, e.ActorRef,
		e.EventTime.UTC())
	return err
}
