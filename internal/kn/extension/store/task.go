// Package store: truy cập DB của pf/extension, chỉ qua pool của vai
// extension_app (R-05). Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant.
//
// Pool chạy ở chế độ tương thích PgBouncer (QueryExecModeExec): numeric gửi và
// đọc dạng chuỗi, jsonb gửi dạng chuỗi.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

func nilIfZero(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func refOrNil(u *uuid.UUID) uuid.UUID {
	if u == nil {
		return uuid.Nil
	}
	return *u
}

func decPtr(d *decimal.Decimal) *string {
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

func units(u []string) []string {
	if u == nil {
		return []string{}
	}
	return u
}

const taskCols = `task_id, commune_code, province_code, cooperative_ref, household_ref, plot_ref, category, description,
  priority, source, status, assignee_ref, created_by, created_at, sla_hours::text, due_at, escalated_at,
  coalesce(escalated_unit, ''), escalation_org_ref, resolved_at, coalesce(last_note, ''), last_seq, sync_seq, updated_at`

func scanTask(row pgx.Row) (domain.Task, error) {
	var t domain.Task
	var coop, hh, plot, assignee, escOrg *uuid.UUID
	var status, sla string
	err := row.Scan(&t.ID, &t.CommuneCode, &t.ProvinceCode, &coop, &hh, &plot, &t.Category, &t.Description,
		&t.Priority, &t.Source, &status, &assignee, &t.CreatedBy, &t.CreatedAt, &sla, &t.DueAt, &t.EscalatedAt,
		&t.EscalatedUnit, &escOrg, &t.ResolvedAt, &t.LastNote, &t.LastSeq, &t.SyncSeq, &t.UpdatedAt)
	if err != nil {
		return t, err
	}
	t.CooperativeRef, t.HouseholdRef, t.PlotRef = refOrNil(coop), refOrNil(hh), refOrNil(plot)
	t.AssigneeRef, t.EscalationOrgRef = refOrNil(assignee), refOrNil(escOrg)
	t.Status = domain.Status(status)
	t.SLAHours, err = decimal.NewFromString(sla)
	return t, err
}

// TaskByID đọc trạng thái hiện tại; forUpdate khóa dòng để ghi trong cùng giao dịch.
func TaskByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.Task, error) {
	q := "SELECT " + taskCols + " FROM pf_extension.task_current WHERE task_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	t, err := scanTask(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// InsertTask ghi bảng hiện tại khi tạo.
func InsertTask(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t domain.Task, ev domain.Event) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.task_current
	    (tenant_id, task_id, commune_code, province_code, cooperative_ref, household_ref, plot_ref, category, description,
	     priority, source, status, assignee_ref, created_by, created_at, sla_hours, due_at, escalated_unit,
	     escalation_org_ref, last_event_id, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::numeric, $17, $18, $19, $20, $21)`,
		tid, t.ID, t.CommuneCode, t.ProvinceCode, nilIfZero(t.CooperativeRef), nilIfZero(t.HouseholdRef), nilIfZero(t.PlotRef),
		t.Category, t.Description, t.Priority, t.Source, string(t.Status), nilIfZero(t.AssigneeRef), t.CreatedBy,
		t.CreatedAt.UTC(), t.SLAHours.String(), t.DueAt.UTC(), nilIfEmpty(t.EscalatedUnit), nilIfZero(t.EscalationOrgRef),
		ev.ID, ev.Seq)
	return err
}

// UpdateTask ghi trạng thái sau sự việc và lấy con trỏ đồng bộ mới.
func UpdateTask(ctx context.Context, tx pgx.Tx, t domain.Task, ev domain.Event) error {
	_, err := tx.Exec(ctx, `UPDATE pf_extension.task_current SET
	    status = $2, assignee_ref = $3, due_at = $4, escalated_at = $5, escalated_unit = $6, escalation_org_ref = $7,
	    resolved_at = $8, last_note = $9, last_event_id = $10, last_seq = $11,
	    sync_seq = nextval('pf_extension.sync_seq'), updated_at = now()
	  WHERE task_id = $1`,
		t.ID, string(t.Status), nilIfZero(t.AssigneeRef), t.DueAt.UTC(), t.EscalatedAt, nilIfEmpty(t.EscalatedUnit),
		nilIfZero(t.EscalationOrgRef), t.ResolvedAt, nilIfEmpty(t.LastNote), ev.ID, ev.Seq)
	return err
}

const eventCols = `id, tenant_id, task_id, action, status_after, assignee_ref, due_at, coalesce(note, ''),
  coalesce(escalated_unit, ''), escalation_org_ref, coalesce(snapshot::text, ''), event_time, recorded_at,
  actor_ref, device_id, idem_key, seq`

func scanEvent(row pgx.Row) (domain.Event, error) {
	var e domain.Event
	var action, status, snap string
	var assignee, escOrg *uuid.UUID
	err := row.Scan(&e.ID, &e.TenantID, &e.TaskID, &action, &status, &assignee, &e.DueAt, &e.Note, &e.EscalatedUnit,
		&escOrg, &snap, &e.EventTime, &e.RecordedAt, &e.ActorRef, &e.DeviceID, &e.IdemKey, &e.Seq)
	if err != nil {
		return e, err
	}
	e.Action, e.StatusAfter = domain.Action(action), domain.Status(status)
	e.AssigneeRef, e.EscalationOrgRef = refOrNil(assignee), refOrNil(escOrg)
	if snap != "" {
		if err := json.Unmarshal([]byte(snap), &e.Snapshot); err != nil {
			return e, err
		}
	}
	return e, nil
}

// InsertEvent ghi sổ; DB tính seq, prev_hash, row_hash.
func InsertEvent(ctx context.Context, tx pgx.Tx, e *domain.Event) error {
	var snap *string
	if e.Snapshot != nil {
		b, err := json.Marshal(e.Snapshot)
		if err != nil {
			return err
		}
		s := string(b)
		snap = &s
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.task_event
	    (id, tenant_id, stream_key, task_id, action, status_after, assignee_ref, due_at, note, escalated_unit,
	     escalation_org_ref, snapshot, event_time, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14, $15, $16, $17)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, domain.StreamKey(e.TaskID), e.TaskID, string(e.Action), string(e.StatusAfter),
		nilIfZero(e.AssigneeRef), e.DueAt.UTC(), nilIfEmpty(e.Note), nilIfEmpty(e.EscalatedUnit), nilIfZero(e.EscalationOrgRef),
		snap, e.EventTime.UTC(), e.ActorRef, e.DeviceID, e.IdemKey, domain.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// EventByIdemKey tìm sự việc đã ghi với idem_key (gọi lặp / bản ghi đồng bộ trùng).
func EventByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Event, error) {
	e, err := scanEvent(tx.QueryRow(ctx, "SELECT "+eventCols+" FROM pf_extension.task_event WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// EventByID tìm sự việc theo id.
func EventByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.Event, error) {
	e, err := scanEvent(tx.QueryRow(ctx, "SELECT "+eventCols+" FROM pf_extension.task_event WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// TaskEvents trả lịch sử của một nhiệm vụ theo thứ tự.
func TaskEvents(ctx context.Context, tx pgx.Tx, taskID uuid.UUID) ([]domain.Event, error) {
	rows, err := tx.Query(ctx, "SELECT "+eventCols+" FROM pf_extension.task_event WHERE task_id = $1 ORDER BY seq", taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// TaskFilter lọc danh sách nhiệm vụ.
type TaskFilter struct {
	All         bool
	Units       []string
	Status      string
	Priority    string
	AssigneeRef uuid.UUID
	Cooperative uuid.UUID // coop_manager: chỉ nhiệm vụ của HTX mình
	Overdue     bool
	Now         time.Time
	After       uuid.UUID
	Limit       int
}

// ListTasks liệt kê theo phạm vi (xã hoặc tỉnh thuộc scope), phân trang theo task_id (R-53).
func ListTasks(ctx context.Context, tx pgx.Tx, f TaskFilter) ([]domain.Task, error) {
	rows, err := tx.Query(ctx, "SELECT "+taskCols+` FROM pf_extension.task_current
	  WHERE ($1::boolean OR commune_code = ANY($2::text[]) OR province_code = ANY($2::text[]))
	    AND ($3 = '' OR status = $3)
	    AND ($4 = '' OR priority = $4)
	    AND ($5::uuid IS NULL OR assignee_ref = $5)
	    AND (NOT $6::boolean OR (status <> 'resolved' AND due_at < $7))
	    AND task_id > $8
	    AND ($10::uuid IS NULL OR cooperative_ref = $10)
	  ORDER BY task_id LIMIT $9`,
		f.All, units(f.Units), f.Status, f.Priority, nilIfZero(f.AssigneeRef), f.Overdue, f.Now.UTC(), f.After, f.Limit,
		nilIfZero(f.Cooperative))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// OverdueUnescalated trả nhiệm vụ quá hạn chưa hoàn thành, chưa leo thang, khóa dòng
// (SKIP LOCKED: hai lần chạy chồng nhau không xử lý cùng dòng).
func OverdueUnescalated(ctx context.Context, tx pgx.Tx, now time.Time, limit int) ([]domain.Task, error) {
	rows, err := tx.Query(ctx, "SELECT "+taskCols+` FROM pf_extension.task_current
	  WHERE status <> 'resolved' AND escalated_at IS NULL AND due_at < $1
	  ORDER BY due_at LIMIT $2 FOR UPDATE SKIP LOCKED`, now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TasksChangedSince trả nhiệm vụ trong phạm vi (xã/tỉnh) hoặc giao cho actor có sync_seq > after.
func TasksChangedSince(ctx context.Context, tx pgx.Tx, all bool, unitCodes []string, actor uuid.UUID, after int64, limit int) ([]domain.Task, error) {
	rows, err := tx.Query(ctx, "SELECT "+taskCols+` FROM pf_extension.task_current
	  WHERE sync_seq > $1
	    AND ($2::boolean OR commune_code = ANY($3::text[]) OR province_code = ANY($3::text[]) OR assignee_ref = $4)
	  ORDER BY sync_seq LIMIT $5`, after, all, units(unitCodes), actor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
