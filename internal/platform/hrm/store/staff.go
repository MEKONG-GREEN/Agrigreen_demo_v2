package store

// W11.22 (pf 000150): SQL của hồ sơ nhân sự — sổ staff, staff_event, record_access, perm_rule. Không PII.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/domain"
)

// StaffRow là một phiên bản đầu hồ sơ.
type StaffRow struct {
	ID            uuid.UUID
	StaffID       uuid.UUID
	Code          string
	Type          domain.StaffType
	SubjectRef    uuid.UUID
	AccountRef    *uuid.UUID
	Change        string
	ChangedFields []string
	Seq           int64
	RecordedAt    time.Time
	Actor         uuid.UUID
}

const staffCols = `id, staff_id, code, staff_type, subject_ref, account_ref, change, changed_fields, seq, recorded_at, actor_ref`

func scanStaff(r pgx.CollectableRow) (StaffRow, error) {
	var x StaffRow
	var t string
	err := r.Scan(&x.ID, &x.StaffID, &x.Code, &t, &x.SubjectRef, &x.AccountRef, &x.Change, &x.ChangedFields, &x.Seq, &x.RecordedAt, &x.Actor)
	x.Type = domain.StaffType(t)
	return x, err
}

// CurrentStaff trả phiên bản mới nhất của mọi hồ sơ, sắp theo mã.
func CurrentStaff(ctx context.Context, tx pgx.Tx) ([]StaffRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+staffCols+` FROM (
	    SELECT DISTINCT ON (staff_id) `+staffCols+` FROM pf_hrm.staff ORDER BY staff_id, seq DESC) v ORDER BY code`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanStaff)
}

// StaffHistory trả mọi phiên bản đầu hồ sơ của staffID, cũ trước; rỗng nếu không có.
func StaffHistory(ctx context.Context, tx pgx.Tx, staffID uuid.UUID) ([]StaffRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+staffCols+` FROM pf_hrm.staff WHERE staff_id = $1 ORDER BY seq`, staffID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanStaff)
}

// StaffBySubject trả hồ sơ (phiên bản mới nhất) gắn chủ thể vault ref; ok = false nếu không có.
func StaffBySubject(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (StaffRow, bool, error) {
	rows, err := tx.Query(ctx, `SELECT `+staffCols+` FROM pf_hrm.staff WHERE subject_ref = $1 ORDER BY seq DESC LIMIT 1`, ref)
	if err != nil {
		return StaffRow{}, false, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanStaff)
	if errors.Is(err, pgx.ErrNoRows) {
		return StaffRow{}, false, nil
	}
	return x, err == nil, err
}

// LockCodes khóa cấp mã hồ sơ của tenant trong giao dịch (mã tự sinh không trùng — HS-01).
func LockCodes(ctx context.Context, tx pgx.Tx, tid uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_hrm.staff_code:' || $1::text, 0))`, tid)
	return err
}

// MaxCodeNumber trả số lớn nhất đã cấp cho tiền tố (VC/HD/XA); 0 nếu chưa có.
func MaxCodeNumber(ctx context.Context, tx pgx.Tx, prefix string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT coalesce(max(substring(code from 4)::int), 0) FROM pf_hrm.staff WHERE code LIKE $1 || '-%'`, prefix).Scan(&n)
	return n, err
}

// InsertStaff ghi một phiên bản đầu hồ sơ.
func InsertStaff(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x StaffRow, at time.Time, idem string) (int64, error) {
	if x.ChangedFields == nil {
		x.ChangedFields = []string{}
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_hrm.staff (id, tenant_id, stream_key, staff_id, code, staff_type, subject_ref, account_ref,
	    change, changed_fields, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'staff:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING seq`,
		x.ID, tid, x.StaffID, x.Code, string(x.Type), x.SubjectRef, x.AccountRef, x.Change, x.ChangedFields, at.UTC(), x.Actor, idem,
		domain.RuleVersion).Scan(&seq)
	return seq, err
}

const eventCols = `id, kind, action, valid_from, date_precision, valid_to, payload, supersedes_id, coalesce(correction_reason, ''), seq,
  recorded_at, actor_ref`

// EventRow là một dòng sổ quá trình kèm hồ sơ.
type EventRow struct {
	StaffID uuid.UUID
	domain.Event
}

func scanEvent(r pgx.CollectableRow) (EventRow, error) {
	var x EventRow
	var kind, action, prec string
	var payload []byte
	err := r.Scan(&x.StaffID, &x.ID, &kind, &action, &x.ValidFrom, &prec, &x.ValidTo, &payload, &x.Supersedes, &x.Reason, &x.Seq,
		&x.RecordedAt, &x.Actor)
	if err != nil {
		return x, err
	}
	x.Kind, x.Precision, x.Void = domain.EventKind(kind), domain.Precision(prec), action == "void"
	x.Payload = map[string]string{}
	return x, json.Unmarshal(payload, &x.Payload)
}

// Events trả mọi dòng sổ quá trình (staffID Nil = mọi hồ sơ của tenant), theo hồ sơ rồi seq.
func Events(ctx context.Context, tx pgx.Tx, staffID uuid.UUID) ([]EventRow, error) {
	rows, err := tx.Query(ctx, `SELECT staff_id, `+eventCols+` FROM pf_hrm.staff_event
	  WHERE ($1::uuid IS NULL OR staff_id = $1) ORDER BY staff_id, seq`, nilable(staffID))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanEvent)
}

func nilable(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

// InsertEvent ghi một dòng quá trình (thêm, thay — supersedes + lý do, hoặc hủy).
func InsertEvent(ctx context.Context, tx pgx.Tx, tid, staffID uuid.UUID, e domain.Event, at time.Time, idem string) (int64, error) {
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return 0, err
	}
	action := "add"
	if e.Void {
		action = "void"
	}
	var reason *string
	if e.Supersedes != nil {
		reason = &e.Reason
	}
	var seq int64
	err = tx.QueryRow(ctx, `INSERT INTO pf_hrm.staff_event (id, tenant_id, stream_key, staff_id, kind, action, valid_from, date_precision,
	    valid_to, payload, supersedes_id, correction_reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'staff:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12, $13, $14, $15) RETURNING seq`,
		e.ID, tid, staffID, string(e.Kind), action, e.ValidFrom, string(e.Precision), e.ValidTo, string(payload), e.Supersedes, reason,
		at.UTC(), e.Actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// Access là một dòng nhật ký xem/xuất.
type Access struct {
	ID         uuid.UUID
	Action     string
	StaffID    *uuid.UUID
	StaffIDs   []uuid.UUID
	ItemCount  int
	Scope      string
	Query      map[string]string
	Actor      uuid.UUID
	RecordedAt time.Time
	Seq        int64
}

// InsertAccess ghi một dòng nhật ký xem/xuất (chuỗi theo người xem).
func InsertAccess(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a Access, at time.Time) error {
	q, err := json.Marshal(a.Query)
	if err != nil {
		return err
	}
	if a.Query == nil {
		q = []byte("{}")
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_hrm.record_access (id, tenant_id, stream_key, action, staff_id, staff_ids, item_count, scope, query,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'actor:' || $3::uuid::text, $4, $5, $6::text[]::uuid[], $7, $8, $9::jsonb, $10, $3::uuid, 'access:' || $1::uuid::text, $11)`,
		a.ID, tid, a.Actor, a.Action, a.StaffID, uuidStrs(a.StaffIDs), a.ItemCount, a.Scope, string(q), at.UTC(), domain.RuleVersion)
	return err
}

// AccessOf trả nhật ký xem/xuất có hồ sơ staffID (xem trực tiếp hoặc nằm trong lần xuất), mới trước, tối đa limit.
func AccessOf(ctx context.Context, tx pgx.Tx, staffID uuid.UUID, limit int) ([]Access, error) {
	rows, err := tx.Query(ctx, `SELECT id, action, staff_id, coalesce(staff_ids, '{}')::text[], item_count, scope, query, actor_ref, recorded_at, seq
	  FROM pf_hrm.record_access WHERE staff_id = $1 OR staff_ids @> ARRAY[$1::uuid] ORDER BY recorded_at DESC, id DESC LIMIT $2`, staffID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Access, error) {
		var a Access
		var q []byte
		var sids []string
		if err := r.Scan(&a.ID, &a.Action, &a.StaffID, &sids, &a.ItemCount, &a.Scope, &q, &a.Actor, &a.RecordedAt, &a.Seq); err != nil {
			return a, err
		}
		for _, x := range sids {
			u, err := uuid.Parse(x)
			if err != nil {
				return a, err
			}
			a.StaffIDs = append(a.StaffIDs, u)
		}
		a.Query = map[string]string{}
		_ = json.Unmarshal(q, &a.Query)
		return a, nil
	})
}

// uuidStrs: nil → NULL (không phải mảng rỗng).
func uuidStrs(xs []uuid.UUID) []string {
	if xs == nil {
		return nil
	}
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.String())
	}
	return out
}

// PermRow là phiên bản mới nhất của một dòng ma trận phân quyền.
type PermRow struct {
	Kind, Grantee string
	Perms         json.RawMessage
	Note          string
	Version       int64
	RecordedAt    time.Time
	Actor         uuid.UUID
}

// PermRules trả phiên bản mới nhất của mọi dòng ma trận (history = true: mọi phiên bản, cũ trước).
func PermRules(ctx context.Context, tx pgx.Tx, history bool) ([]PermRow, error) {
	q := `SELECT grantee_kind, grantee, perms, coalesce(note, ''), seq, recorded_at, actor_ref FROM (
	    SELECT DISTINCT ON (grantee_kind, grantee) * FROM pf_hrm.perm_rule ORDER BY grantee_kind, grantee, seq DESC) v
	  ORDER BY grantee_kind, grantee`
	if history {
		q = `SELECT grantee_kind, grantee, perms, coalesce(note, ''), seq, recorded_at, actor_ref FROM pf_hrm.perm_rule ORDER BY recorded_at, id`
	}
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PermRow, error) {
		var x PermRow
		err := r.Scan(&x.Kind, &x.Grantee, &x.Perms, &x.Note, &x.Version, &x.RecordedAt, &x.Actor)
		return x, err
	})
}

// InsertPerm ghi một phiên bản dòng ma trận.
func InsertPerm(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, kind, grantee string, perms []byte, note string, at time.Time,
	actor uuid.UUID, idem string) (int64, error) {
	var n *string
	if note != "" {
		n = &note
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_hrm.perm_rule (id, tenant_id, stream_key, grantee_kind, grantee, perms, note, event_time,
	    actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'perm:' || $3::text || ':' || $4::text, $3::text, $4::text, $5::jsonb, $6, $7, $8, $9, $10) RETURNING seq`,
		id, tid, kind, grantee, string(perms), n, at.UTC(), actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}
