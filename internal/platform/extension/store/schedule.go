package store

// KN v7.7 P5 — lịch công tác tuần (migration pf 000075).

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

const scheduleCols = `schedule_id, owner_ref, owner_level, department_code, province_code, commune_codes, week_start, status,
  version, coalesce(approved_version, 0), amendment, late, item_count, conflict_count, first_submitted_at, submitted_at,
  reviewed_at, reviewer_ref, coalesce(last_reason, ''), locked_at, created_by, created_at, updated_by, updated_at,
  last_event_id, last_seq`

func scanSchedule(row pgx.Row) (domain.Schedule, error) {
	var s domain.Schedule
	var reviewer *uuid.UUID
	err := row.Scan(&s.ID, &s.OwnerRef, &s.OwnerLevel, &s.Department, &s.ProvinceCode, &s.Communes, &s.WeekStart, &s.Status,
		&s.Version, &s.ApprovedVersion, &s.Amendment, &s.Late, &s.ItemCount, &s.ConflictCount, &s.FirstSubmitted, &s.SubmittedAt,
		&s.ReviewedAt, &reviewer, &s.LastReason, &s.LockedAt, &s.CreatedBy, &s.CreatedAt, &s.UpdatedBy, &s.UpdatedAt,
		&s.LastEventID, &s.LastSeq)
	s.ReviewerRef = refOrNil(reviewer)
	s.WeekStart = dateUTC(s.WeekStart)
	if s.Communes == nil {
		s.Communes = []string{}
	}
	return s, err
}

func dateUTC(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func oneSchedule(row pgx.Row) (*domain.Schedule, error) {
	s, err := scanSchedule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ScheduleByID đọc lịch; forUpdate khóa dòng.
func ScheduleByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.Schedule, error) {
	q := "SELECT " + scheduleCols + " FROM pf_extension.work_schedule_current WHERE schedule_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneSchedule(tx.QueryRow(ctx, q, id))
}

// ScheduleByOwnerWeek đọc lịch của người đăng ký × phòng ban × cấp × tuần.
func ScheduleByOwnerWeek(ctx context.Context, tx pgx.Tx, owner uuid.UUID, dept, level string, week time.Time, forUpdate bool) (*domain.Schedule, error) {
	q := "SELECT " + scheduleCols + ` FROM pf_extension.work_schedule_current
	  WHERE owner_ref = $1 AND department_code = $2 AND owner_level = $3 AND week_start = $4::date`
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneSchedule(tx.QueryRow(ctx, q, owner, dept, level, week.Format(time.DateOnly)))
}

// LockScheduleKey khóa tư vấn theo (người, phòng ban, cấp, tuần) để hai lần tạo đồng thời không vỡ khóa duy nhất.
func LockScheduleKey(ctx context.Context, tx pgx.Tx, owner uuid.UUID, dept, level string, week time.Time) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 75))",
		owner.String()+"|"+dept+"|"+level+"|"+week.Format(time.DateOnly))
	return err
}

// UpsertSchedule ghi bảng chiếu sau một sự việc.
func UpsertSchedule(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s domain.Schedule) error {
	var approved *int
	if s.ApprovedVersion > 0 {
		v := s.ApprovedVersion
		approved = &v
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.work_schedule_current
	    (tenant_id, schedule_id, owner_ref, owner_level, department_code, province_code, commune_codes, week_start, status,
	     version, approved_version, amendment, late, item_count, conflict_count, first_submitted_at, submitted_at,
	     reviewed_at, reviewer_ref, last_reason, locked_at, created_by, created_at, updated_by, updated_at, last_event_id, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23,
	          $24, $25, $26, $27)
	  ON CONFLICT (tenant_id, schedule_id) DO UPDATE SET
	    commune_codes = EXCLUDED.commune_codes, status = EXCLUDED.status, version = EXCLUDED.version,
	    approved_version = EXCLUDED.approved_version, amendment = EXCLUDED.amendment, late = EXCLUDED.late,
	    item_count = EXCLUDED.item_count, conflict_count = EXCLUDED.conflict_count,
	    first_submitted_at = EXCLUDED.first_submitted_at, submitted_at = EXCLUDED.submitted_at,
	    reviewed_at = EXCLUDED.reviewed_at, reviewer_ref = EXCLUDED.reviewer_ref, last_reason = EXCLUDED.last_reason,
	    locked_at = EXCLUDED.locked_at, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at,
	    last_event_id = EXCLUDED.last_event_id, last_seq = EXCLUDED.last_seq`,
		tid, s.ID, s.OwnerRef, s.OwnerLevel, s.Department, s.ProvinceCode, units(s.Communes), s.WeekStart.Format(time.DateOnly),
		s.Status, s.Version, approved, s.Amendment, s.Late, s.ItemCount, s.ConflictCount, s.FirstSubmitted, s.SubmittedAt,
		s.ReviewedAt, nilIfZero(s.ReviewerRef), nilIfEmpty(s.LastReason), s.LockedAt, s.CreatedBy, s.CreatedAt.UTC(),
		s.UpdatedBy, s.UpdatedAt.UTC(), s.LastEventID, s.LastSeq)
	return err
}

const scheduleEventCols = `id, tenant_id, schedule_id, version, action, status_after, owner_ref, owner_level, department_code,
  province_code, commune_codes, week_start, item_count, late, coalesce(reason, ''), coalesce(note, ''), event_time,
  recorded_at, actor_ref, device_id, idem_key, seq`

func scanScheduleEvent(row pgx.Row) (domain.ScheduleEvent, error) {
	var e domain.ScheduleEvent
	err := row.Scan(&e.ID, &e.TenantID, &e.ScheduleID, &e.Version, &e.Action, &e.StatusAfter, &e.OwnerRef, &e.OwnerLevel,
		&e.Department, &e.Province, &e.Communes, &e.WeekStart, &e.ItemCount, &e.Late, &e.Reason, &e.Note, &e.EventTime,
		&e.RecordedAt, &e.ActorRef, &e.DeviceID, &e.IdemKey, &e.Seq)
	e.WeekStart = dateUTC(e.WeekStart)
	return e, err
}

// InsertScheduleEvent ghi sổ sự việc; DB tính seq, prev_hash, row_hash.
func InsertScheduleEvent(ctx context.Context, tx pgx.Tx, e *domain.ScheduleEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.work_schedule_event
	    (id, tenant_id, stream_key, schedule_id, version, action, status_after, owner_ref, owner_level, department_code,
	     province_code, commune_codes, week_start, item_count, late, reason, note, event_time, actor_ref, device_id, idem_key,
	     rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::date, $14, $15, $16, $17, $18, $19, $20, $21, $22)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, domain.ScheduleStreamKey(e.ScheduleID), e.ScheduleID, e.Version, e.Action, e.StatusAfter, e.OwnerRef,
		e.OwnerLevel, e.Department, e.Province, units(e.Communes), e.WeekStart.Format(time.DateOnly), e.ItemCount, e.Late,
		nilIfEmpty(e.Reason), nilIfEmpty(e.Note), e.EventTime.UTC(), e.ActorRef, e.DeviceID, e.IdemKey, domain.RuleVersion).
		Scan(&e.Seq, &e.RecordedAt)
}

// ScheduleEventByIdem tìm sự việc đã ghi với idem_key (gọi lặp, hàng đợi ngoại tuyến phát lại).
func ScheduleEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*domain.ScheduleEvent, error) {
	e, err := scanScheduleEvent(tx.QueryRow(ctx, "SELECT "+scheduleEventCols+" FROM pf_extension.work_schedule_event WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ScheduleEvents trả mọi sự việc của lịch theo seq tăng dần.
func ScheduleEvents(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]domain.ScheduleEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+scheduleEventCols+" FROM pf_extension.work_schedule_event WHERE schedule_id = $1 ORDER BY seq", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ScheduleEvent
	for rows.Next() {
		e, err := scanScheduleEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// InsertScheduleItems ghi ảnh chụp mục của một phiên bản (sổ work_schedule_item). idemPrefix + vị trí là
// idem_key của từng dòng.
func InsertScheduleItems(ctx context.Context, tx pgx.Tx, tid, scheduleID uuid.UUID, version int, items []domain.ScheduleItem,
	actor uuid.UUID, at time.Time, idemPrefix string, newID func() uuid.UUID) error {
	for i := range items {
		it := &items[i]
		it.RowID, it.Version = newID(), version
		_, err := tx.Exec(ctx, `INSERT INTO pf_extension.work_schedule_item
		    (id, tenant_id, stream_key, schedule_id, version, item_id, position, day, session, start_time, end_time, content,
		     commune_code, cooperative_ref, location, participants, note, supersedes_id, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9, $10::time, $11::time, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)`,
			it.RowID, tid, domain.ScheduleStreamKey(scheduleID), scheduleID, version, it.ItemID, it.Position, it.DayString(),
			it.Session, nilIfEmpty(it.Start), nilIfEmpty(it.End), it.Content, nilIfEmpty(it.CommuneCode), nilIfZero(it.CooperativeRef),
			nilIfEmpty(it.Location), nilIfEmpty(it.Participants), nilIfEmpty(it.Note), it.SupersedesID, at.UTC(), actor,
			idemPrefix+":"+strconv.Itoa(i), domain.RuleVersion)
		if err != nil {
			return err
		}
	}
	return nil
}

const scheduleItemCols = `i.id, i.item_id, i.version, i.position, i.day, i.session, coalesce(to_char(i.start_time, 'HH24:MI'), ''),
  coalesce(to_char(i.end_time, 'HH24:MI'), ''), i.content, coalesce(i.commune_code, ''), i.cooperative_ref, coalesce(i.location, ''),
  coalesce(i.participants, ''), coalesce(i.note, ''), i.supersedes_id`

func scanScheduleItem(row pgx.Row) (domain.ScheduleItem, error) {
	var it domain.ScheduleItem
	var coop *uuid.UUID
	err := row.Scan(&it.RowID, &it.ItemID, &it.Version, &it.Position, &it.Day, &it.Session, &it.Start, &it.End, &it.Content,
		&it.CommuneCode, &coop, &it.Location, &it.Participants, &it.Note, &it.SupersedesID)
	it.CooperativeRef = refOrNil(coop)
	it.Day = dateUTC(it.Day)
	return it, err
}

// ScheduleItemsOf trả mục của các (lịch, phiên bản); khóa kết quả theo lịch.
func ScheduleItemsOf(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, versions []int) (map[uuid.UUID][]domain.ScheduleItem, error) {
	out := map[uuid.UUID][]domain.ScheduleItem{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT i.schedule_id, `+scheduleItemCols+`
	  FROM pf_extension.work_schedule_item i
	  JOIN unnest($1::uuid[], $2::int[]) AS k(schedule_id, version) ON k.schedule_id = i.schedule_id AND k.version = i.version
	  ORDER BY i.schedule_id, i.position`, uuidStrings(ids), intStrings(versions))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sid uuid.UUID
		var it domain.ScheduleItem
		var coop *uuid.UUID
		if err := rows.Scan(&sid, &it.RowID, &it.ItemID, &it.Version, &it.Position, &it.Day, &it.Session, &it.Start, &it.End,
			&it.Content, &it.CommuneCode, &coop, &it.Location, &it.Participants, &it.Note, &it.SupersedesID); err != nil {
			return nil, err
		}
		it.CooperativeRef = refOrNil(coop)
		it.Day = dateUTC(it.Day)
		out[sid] = append(out[sid], it)
	}
	return out, rows.Err()
}

// intStrings: mảng số gửi dạng chuỗi (chế độ tương thích PgBouncer không mã hóa []int; uuidStrings ở roster.go).
func intStrings(n []int) []string {
	out := make([]string, len(n))
	for i, x := range n {
		out[i] = strconv.Itoa(x)
	}
	return out
}

// ScheduleItems trả mục của một phiên bản.
func ScheduleItems(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int) ([]domain.ScheduleItem, error) {
	rows, err := tx.Query(ctx, "SELECT "+scheduleItemCols+` FROM pf_extension.work_schedule_item i
	  WHERE i.schedule_id = $1 AND i.version = $2 ORDER BY i.position`, id, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ScheduleItem
	for rows.Next() {
		it, err := scanScheduleItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ScheduleFilter lọc danh sách lịch. Owner ≠ Nil → chỉ lịch của người đó (Cấp Xã); All → mọi phòng ban/tỉnh (Admin).
type ScheduleFilter struct {
	All        bool
	Department string
	Province   string
	Owner      uuid.UUID
	Week       time.Time
	Status     string
	Commune    string
	OwnerLevel string
	After      uuid.UUID
	Limit      int
}

// ListSchedules liệt kê lịch theo bộ lọc, phân trang theo schedule_id.
func ListSchedules(ctx context.Context, tx pgx.Tx, f ScheduleFilter) ([]domain.Schedule, error) {
	rows, err := tx.Query(ctx, "SELECT "+scheduleCols+` FROM pf_extension.work_schedule_current
	  WHERE week_start = $1::date
	    AND ($2::boolean OR (department_code = $3 AND province_code = $4))
	    AND ($5::uuid IS NULL OR owner_ref = $5)
	    AND ($6 = '' OR status = $6)
	    AND ($7 = '' OR $7 = ANY(commune_codes))
	    AND ($8 = '' OR owner_level = $8)
	    AND schedule_id > $9
	  ORDER BY schedule_id LIMIT $10`,
		f.Week.Format(time.DateOnly), f.All, f.Department, f.Province, nilIfZero(f.Owner), f.Status, f.Commune, f.OwnerLevel,
		f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ScheduleStatusCounts đếm lịch theo trạng thái của tuần trong phạm vi.
func ScheduleStatusCounts(ctx context.Context, tx pgx.Tx, f ScheduleFilter) (map[string]int, error) {
	rows, err := tx.Query(ctx, `SELECT status, count(*) FROM pf_extension.work_schedule_current
	  WHERE week_start = $1::date AND ($2::boolean OR (department_code = $3 AND province_code = $4))
	    AND ($5::uuid IS NULL OR owner_ref = $5)
	  GROUP BY status`, f.Week.Format(time.DateOnly), f.All, f.Department, f.Province, nilIfZero(f.Owner))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}

// PendingOpenWeeks đếm lịch chờ duyệt của các tuần từ fromWeek trở đi trong phòng ban × tỉnh, trừ lịch của
// người gọi (số đếm menu "Lịch công tác" của Cấp Tỉnh).
func PendingOpenWeeks(ctx context.Context, tx pgx.Tx, dept, province string, fromWeek time.Time, except uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.work_schedule_current
	  WHERE department_code = $1 AND province_code = $2 AND status = 'pending' AND week_start >= $3::date AND owner_ref <> $4`,
		dept, province, fromWeek.Format(time.DateOnly), except).Scan(&n)
	return n, err
}

// SchedulesOfWeek trả mọi lịch của phòng ban × tỉnh trong tuần (tab Chưa đăng ký, job nhắc).
func SchedulesOfWeek(ctx context.Context, tx pgx.Tx, dept, province string, week time.Time) ([]domain.Schedule, error) {
	return ListSchedules(ctx, tx, ScheduleFilter{Department: dept, Province: province, Week: week, Limit: 100000})
}

// ScheduleScope là một (phòng ban, tỉnh) có lịch.
type ScheduleScope struct {
	Department string
	Province   string
}

// PendingScopes trả (phòng ban, tỉnh, tuần) còn lịch chờ duyệt của các tuần chưa kết thúc.
func PendingScopes(ctx context.Context, tx pgx.Tx, fromWeek time.Time) (map[ScheduleScope][]time.Time, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT department_code, province_code, week_start FROM pf_extension.work_schedule_current
	  WHERE status = 'pending' AND week_start >= $1::date`, fromWeek.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[ScheduleScope][]time.Time{}
	for rows.Next() {
		var k ScheduleScope
		var w time.Time
		if err := rows.Scan(&k.Department, &k.Province, &w); err != nil {
			return nil, err
		}
		out[k] = append(out[k], dateUTC(w))
	}
	return out, rows.Err()
}

// EndedUnlocked trả lịch của các tuần đã kết thúc (trước currentWeek) chưa ghi sự việc "locked".
func EndedUnlocked(ctx context.Context, tx pgx.Tx, currentWeek time.Time, limit int) ([]domain.Schedule, error) {
	rows, err := tx.Query(ctx, "SELECT "+scheduleCols+` FROM pf_extension.work_schedule_current
	  WHERE week_start < $1::date AND locked_at IS NULL ORDER BY schedule_id LIMIT $2 FOR UPDATE SKIP LOCKED`,
		currentWeek.Format(time.DateOnly), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// InsertScheduleReminder ghi một lần nhắc; false nếu (loại, tuần, phòng ban, tỉnh) đã nhắc.
func InsertScheduleReminder(ctx context.Context, tx pgx.Tx, tid, id uuid.UUID, kind string, week time.Time, dept, province string,
	recipients int, at time.Time) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO pf_extension.work_schedule_reminder
	    (tenant_id, id, kind, week_start, department_code, province_code, recipients, sent_at)
	  VALUES ($1, $2, $3, $4::date, $5, $6, $7, $8) ON CONFLICT (tenant_id, kind, week_start, department_code, province_code) DO NOTHING`,
		tid, id, kind, week.Format(time.DateOnly), dept, province, recipients, at.UTC())
	return tag.RowsAffected() == 1, err
}

// ActiveStaffProvinces trả các tỉnh có cán bộ đang hoạt động trong danh bạ (nguồn dự phòng khi chưa có P1).
func ActiveStaffProvinces(ctx context.Context, tx pgx.Tx) ([]string, error) {
	rows, err := tx.Query(ctx, "SELECT DISTINCT province_code FROM pf_extension.staff WHERE active ORDER BY 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProvinceStaff trả cán bộ đang hoạt động của một tỉnh trong danh bạ (actor_ref, đơn vị) — không trả tên.
func ProvinceStaff(ctx context.Context, tx pgx.Tx, province string) ([][2]string, []uuid.UUID, error) {
	rows, err := tx.Query(ctx, "SELECT actor_ref, unit_code, province_code FROM pf_extension.staff WHERE active AND province_code = $1 ORDER BY actor_ref", province)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var units [][2]string
	var refs []uuid.UUID
	for rows.Next() {
		var ref uuid.UUID
		var unit, prov string
		if err := rows.Scan(&ref, &unit, &prov); err != nil {
			return nil, nil, err
		}
		refs = append(refs, ref)
		units = append(units, [2]string{unit, prov})
	}
	return units, refs, rows.Err()
}
