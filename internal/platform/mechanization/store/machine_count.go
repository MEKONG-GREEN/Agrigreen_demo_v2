package store

// KN v7.7 FN-XA-03.04 (migration pf 000078): sổ coop_machine_count_event + bảng chiếu coop_machine_count.
// Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
)

// MachineCountRow là một dòng hiện hành (HTX × loại máy).
type MachineCountRow struct {
	CooperativeRef uuid.UUID
	MachineType    string
	Quantity       int
	Note           string
	CommuneCode    string
	DepartmentCode string
	Active         bool
	UpdatedAt      time.Time
	UpdatedBy      uuid.UUID
}

// MachineCountEvent là một dòng sổ.
type MachineCountEvent struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	CooperativeRef   uuid.UUID
	CommuneCode      string
	MachineType      string
	Action           string
	Quantity         int
	PrevQuantity     *int
	Note             *string
	PrevNote         *string
	DepartmentCode   string
	Reason           *string
	RequestKey       string
	OnBehalfOf       *uuid.UUID
	SupportSessionID *uuid.UUID
	SupportReason    *string
	EventTime        time.Time
	ActorRef         uuid.UUID
	IdemKey          string
	Seq              int64
	RecordedAt       time.Time
}

// MachineCountStream là stream_key của khai báo một HTX.
func MachineCountStream(coop uuid.UUID) string { return "coop_machines:" + coop.String() }

const machineCountCols = `cooperative_ref, machine_type, quantity, coalesce(note, ''), commune_code, department_code, active, updated_at, updated_by`

func scanMachineCount(row pgx.Row) (MachineCountRow, error) {
	var r MachineCountRow
	err := row.Scan(&r.CooperativeRef, &r.MachineType, &r.Quantity, &r.Note, &r.CommuneCode, &r.DepartmentCode, &r.Active, &r.UpdatedAt, &r.UpdatedBy)
	return r, err
}

func collectCounts(rows pgx.Rows, err error) ([]MachineCountRow, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MachineCountRow{}
	for rows.Next() {
		r, err := scanMachineCount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MachineCountsOf trả các dòng đang có của HTX; lock khóa các dòng (kể cả đã bỏ) để ghi trong cùng giao dịch.
func MachineCountsOf(ctx context.Context, tx pgx.Tx, coop uuid.UUID, lock bool) ([]MachineCountRow, error) {
	q := "SELECT " + machineCountCols + " FROM pf_mechanization.coop_machine_count WHERE cooperative_ref = $1"
	if lock {
		q += " FOR UPDATE"
	} else {
		q += " AND active ORDER BY machine_type"
	}
	return collectCounts(tx.Query(ctx, q, coop))
}

// LockCoopMachines tuần tự hóa các lần lưu khai báo của một HTX (khóa tư vấn, tự nhả cuối giao dịch).
func LockCoopMachines(ctx context.Context, tx pgx.Tx, coop uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_mechanization.coop_machine_count:' || $1::text, 0))", coop)
	return err
}

// MachineCountsIn trả mọi dòng đang có (active) của các xã (all = mọi xã của tenant), sắp theo xã, HTX, loại.
func MachineCountsIn(ctx context.Context, tx pgx.Tx, all bool, communes []string) ([]MachineCountRow, error) {
	if communes == nil {
		communes = []string{}
	}
	return collectCounts(tx.Query(ctx, "SELECT "+machineCountCols+` FROM pf_mechanization.coop_machine_count
	    WHERE active AND ($1::boolean OR commune_code = ANY($2::text[]))
	    ORDER BY commune_code, cooperative_ref, machine_type`, all, communes))
}

// CoopMachineTotal trả tổng số máy đang khai báo của HTX.
func CoopMachineTotal(ctx context.Context, tx pgx.Tx, coop uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT coalesce(sum(quantity), 0) FROM pf_mechanization.coop_machine_count
	    WHERE cooperative_ref = $1 AND active`, coop).Scan(&n)
	return n, err
}

// RequestRecorded cho biết đã có dòng sổ của lần lưu request_key (gọi lặp PUT cùng Idempotency-Key).
func RequestRecorded(ctx context.Context, tx pgx.Tx, key string) (uuid.UUID, bool, error) {
	var coop uuid.UUID
	err := tx.QueryRow(ctx, `SELECT cooperative_ref FROM pf_mechanization.coop_machine_count_event WHERE request_key = $1 LIMIT 1`, key).Scan(&coop)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return coop, err == nil, err
}

// InsertMachineCountEvent ghi sổ; DB tính seq, chuỗi băm.
func InsertMachineCountEvent(ctx context.Context, tx pgx.Tx, e *MachineCountEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_mechanization.coop_machine_count_event
	    (id, tenant_id, stream_key, cooperative_ref, commune_code, machine_type, action, quantity, prev_quantity, note, prev_note,
	     department_code, reason, request_key, on_behalf_of, support_session_id, support_reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21) RETURNING seq, recorded_at`,
		e.ID, e.TenantID, MachineCountStream(e.CooperativeRef), e.CooperativeRef, e.CommuneCode, e.MachineType, e.Action, e.Quantity,
		e.PrevQuantity, e.Note, e.PrevNote, e.DepartmentCode, e.Reason, e.RequestKey, e.OnBehalfOf, e.SupportSessionID, e.SupportReason,
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.MachineCountRuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// UpsertMachineCount ghi dòng hiện hành theo sự kiện vừa ghi (R-21: cùng giao dịch với sổ).
func UpsertMachineCount(ctx context.Context, tx pgx.Tx, e MachineCountEvent) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_mechanization.coop_machine_count
	    (tenant_id, cooperative_ref, machine_type, quantity, note, commune_code, department_code, active, updated_at, updated_by,
	     last_event_id, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	  ON CONFLICT (tenant_id, cooperative_ref, machine_type) DO UPDATE SET quantity = EXCLUDED.quantity, note = EXCLUDED.note,
	    commune_code = EXCLUDED.commune_code, department_code = EXCLUDED.department_code, active = EXCLUDED.active,
	    updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by, last_event_id = EXCLUDED.last_event_id, last_seq = EXCLUDED.last_seq`,
		e.TenantID, e.CooperativeRef, e.MachineType, e.Quantity, e.Note, e.CommuneCode, e.DepartmentCode, e.Action == domain.CountSet,
		e.EventTime.UTC(), e.ActorRef, e.ID, e.Seq)
	return err
}

const machineCountEventCols = `id, tenant_id, cooperative_ref, commune_code, machine_type, action, quantity, prev_quantity, note, prev_note,
  department_code, reason, request_key, on_behalf_of, support_session_id, support_reason, event_time, actor_ref, idem_key, seq, recorded_at`

// MachineCountEvents trả sổ của HTX, seq > after, tăng dần.
func MachineCountEvents(ctx context.Context, tx pgx.Tx, coop uuid.UUID, after int64, limit int) ([]MachineCountEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+machineCountEventCols+` FROM pf_mechanization.coop_machine_count_event
	    WHERE stream_key = $1 AND seq > $2 ORDER BY seq LIMIT $3`, MachineCountStream(coop), after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MachineCountEvent{}
	for rows.Next() {
		var e MachineCountEvent
		if err := rows.Scan(&e.ID, &e.TenantID, &e.CooperativeRef, &e.CommuneCode, &e.MachineType, &e.Action, &e.Quantity, &e.PrevQuantity,
			&e.Note, &e.PrevNote, &e.DepartmentCode, &e.Reason, &e.RequestKey, &e.OnBehalfOf, &e.SupportSessionID, &e.SupportReason,
			&e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
