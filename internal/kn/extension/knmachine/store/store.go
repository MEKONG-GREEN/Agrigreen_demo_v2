// Package store: SQL số máy theo loại của HTX ở kho KN — sổ pf_extension.kn_machine_count_event + bảng chiếu
// pf_extension.kn_machine_count (migration 20261111002853, KN-STANDALONE-5-TT-DATA-MAY #306) trên vai r_extension. Mọi hàm
// nhận pgx.Tx đã ở ngữ cảnh tenant (RLS).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knmachine/domain"
)

// Cooperative là HTX mà khai báo máy khóa theo: hồ sơ KN loại HTX, hoặc HTX cũ đã chép chưa có hồ sơ KN (Legacy).
type Cooperative struct {
	Ref         uuid.UUID
	Name        string
	CommuneCode string
	Active      bool
	Legacy      bool
}

// CooperativeOf đọc hồ sơ KN loại HTX theo mã; không có thì tìm khai báo đã chép còn giữ khóa HTX cũ
// (cooperative_ref = legacy_htx_ref). nil = không có.
func CooperativeOf(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Cooperative, error) {
	c := Cooperative{Ref: ref}
	var kind, event string
	var name *string
	err := tx.QueryRow(ctx, `SELECT kind, event, commune_code, name FROM pf_extension.kn_producer
	  WHERE producer_ref = $1::uuid ORDER BY seq DESC LIMIT 1`, ref).Scan(&kind, &event, &c.CommuneCode, &name)
	if err == nil {
		if kind != "cooperative" {
			return nil, nil
		}
		if name != nil {
			c.Name = *name
		}
		c.Active = event != "deactivated"
		return &c, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	err = tx.QueryRow(ctx, `SELECT commune_code FROM pf_extension.kn_machine_count
	  WHERE cooperative_ref = $1::uuid AND legacy_htx_ref = $1::uuid ORDER BY machine_type LIMIT 1`, ref).Scan(&c.CommuneCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.Active, c.Legacy = true, true
	return &c, nil
}

// CooperativeNames trả tên hiện hành của các hồ sơ KN loại HTX (mã không có hồ sơ thì vắng trong kết quả).
func CooperativeNames(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(refs) == 0 {
		return out, nil
	}
	ids := make([]string, len(refs))
	for i, r := range refs {
		ids[i] = r.String()
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (producer_ref) producer_ref, coalesce(name, '') FROM pf_extension.kn_producer
	  WHERE producer_ref = ANY($1::uuid[]) AND kind = 'cooperative' ORDER BY producer_ref, seq DESC`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref uuid.UUID
		var name string
		if err := rows.Scan(&ref, &name); err != nil {
			return nil, err
		}
		out[ref] = name
	}
	return out, rows.Err()
}

// Row là một dòng hiện hành (HTX × loại máy).
type Row struct {
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

// Event là một dòng sổ.
type Event struct {
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

// Stream là stream_key của khai báo một HTX.
func Stream(coop uuid.UUID) string { return "kn_machines:" + coop.String() }

const rowCols = `cooperative_ref, machine_type, quantity, coalesce(note, ''), commune_code, department_code, active, updated_at, updated_by`

func collect(rows pgx.Rows, err error) ([]Row, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Row{}
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.CooperativeRef, &r.MachineType, &r.Quantity, &r.Note, &r.CommuneCode, &r.DepartmentCode, &r.Active,
			&r.UpdatedAt, &r.UpdatedBy); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Of trả các dòng đang có của HTX; lock khóa các dòng (kể cả đã bỏ) để ghi trong cùng giao dịch.
func Of(ctx context.Context, tx pgx.Tx, coop uuid.UUID, lock bool) ([]Row, error) {
	q := "SELECT " + rowCols + " FROM pf_extension.kn_machine_count WHERE cooperative_ref = $1::uuid"
	if lock {
		q += " FOR UPDATE"
	} else {
		q += " AND active ORDER BY machine_type"
	}
	return collect(tx.Query(ctx, q, coop))
}

// Lock tuần tự hóa các lần lưu khai báo của một HTX (khóa tư vấn, tự nhả cuối giao dịch).
func Lock(ctx context.Context, tx pgx.Tx, coop uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.' || $1::text, 0))", Stream(coop))
	return err
}

// In trả mọi dòng đang có (active) của các xã (all = mọi xã của tenant), sắp theo xã, HTX, loại.
func In(ctx context.Context, tx pgx.Tx, all bool, communes []string) ([]Row, error) {
	if communes == nil {
		communes = []string{}
	}
	return collect(tx.Query(ctx, "SELECT "+rowCols+` FROM pf_extension.kn_machine_count
	    WHERE active AND ($1::boolean OR commune_code = ANY($2::text[]))
	    ORDER BY commune_code, cooperative_ref, machine_type`, all, communes))
}

// RequestRecorded cho biết đã có dòng sổ của lần lưu request_key (gọi lặp PUT cùng Idempotency-Key).
func RequestRecorded(ctx context.Context, tx pgx.Tx, key string) (bool, error) {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM pf_extension.kn_machine_count_event WHERE request_key = $1::text LIMIT 1`, key).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// InsertEvent ghi sổ; DB tính seq, chuỗi băm.
func InsertEvent(ctx context.Context, tx pgx.Tx, e *Event) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.kn_machine_count_event
	    (id, tenant_id, stream_key, cooperative_ref, commune_code, machine_type, action, quantity, prev_quantity, note, prev_note,
	     department_code, reason, request_key, on_behalf_of, support_session_id, support_reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::text, $7::text, $8::int, $9::int, $10::text, $11::text, $12::text,
	    $13::text, $14::text, $15::uuid, $16::uuid, $17::text, $18::timestamptz, $19::uuid, $20::text, $21::text)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, Stream(e.CooperativeRef), e.CooperativeRef, e.CommuneCode, e.MachineType, e.Action, e.Quantity,
		e.PrevQuantity, e.Note, e.PrevNote, e.DepartmentCode, e.Reason, e.RequestKey, e.OnBehalfOf, e.SupportSessionID, e.SupportReason,
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// Upsert ghi dòng hiện hành theo sự việc vừa ghi (R-21: cùng giao dịch với sổ). legacy_htx_ref của dòng đã chép giữ nguyên.
func Upsert(ctx context.Context, tx pgx.Tx, e Event) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.kn_machine_count
	    (tenant_id, cooperative_ref, machine_type, quantity, note, commune_code, department_code, active, updated_at, updated_by,
	     last_event_id, last_seq)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::int, $5::text, $6::text, $7::text, $8::boolean, $9::timestamptz, $10::uuid, $11::uuid, $12::bigint)
	  ON CONFLICT (tenant_id, cooperative_ref, machine_type) DO UPDATE SET quantity = EXCLUDED.quantity, note = EXCLUDED.note,
	    commune_code = EXCLUDED.commune_code, department_code = EXCLUDED.department_code, active = EXCLUDED.active,
	    updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by, last_event_id = EXCLUDED.last_event_id, last_seq = EXCLUDED.last_seq`,
		e.TenantID, e.CooperativeRef, e.MachineType, e.Quantity, e.Note, e.CommuneCode, e.DepartmentCode, e.Action == domain.Set,
		e.EventTime.UTC(), e.ActorRef, e.ID, e.Seq)
	return err
}

// Events trả sổ của HTX, seq > after, tăng dần.
func Events(ctx context.Context, tx pgx.Tx, coop uuid.UUID, after int64, limit int) ([]Event, error) {
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, cooperative_ref, commune_code, machine_type, action, quantity, prev_quantity, note,
	    prev_note, department_code, reason, request_key, on_behalf_of, support_session_id, support_reason, event_time, actor_ref,
	    idem_key, seq, recorded_at
	  FROM pf_extension.kn_machine_count_event
	  WHERE stream_key = $1::text AND seq > $2::bigint ORDER BY seq LIMIT $3::int`, Stream(coop), after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TenantID, &e.CooperativeRef, &e.CommuneCode, &e.MachineType, &e.Action, &e.Quantity, &e.PrevQuantity,
			&e.Note, &e.PrevNote, &e.DepartmentCode, &e.Reason, &e.RequestKey, &e.OnBehalfOf, &e.SupportSessionID, &e.SupportReason,
			&e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
