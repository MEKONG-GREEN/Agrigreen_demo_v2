package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ScheduleJobResult là kết quả một lần chạy job lịch công tác của một tenant.
type ScheduleJobResult struct {
	Locked    int // lịch của tuần đã kết thúc được ghi sự việc "locked"
	Reminders int // lần nhắc đã phát (submit_due + review_overdue)
	Purged    int // tọa độ check-in quá hạn lưu đã xóa (W11.46, QĐ-8)
}

const (
	lockBatch  = 200
	purgeBatch = 1000
)

// RunScheduleJob là thân job "extension_work_schedule" (mỗi 15 phút, leader): với mọi tenant —
// (1) ghi sự việc "locked" cho lịch của tuần đã kết thúc (FN-TI-02.07: "lịch tuần đã kết thúc bị khóa"; việc
// chặn ghi không phụ thuộc job — app kiểm tuần theo đồng hồ);
// (2) nhắc cán bộ chưa gửi lịch tuần kế tiếp khi còn ≤ remind_before giờ tới hạn đăng ký;
// (3) nhắc người duyệt khi còn lịch chờ duyệt quá hạn duyệt (FN-TI-02.07 luồng phụ);
// (0) xóa tọa độ check-in quá hạn lưu (W11.46).
// Idempotent: mỗi (loại nhắc, tuần, phòng ban, tỉnh) chỉ một lần (bảng work_schedule_reminder), khóa idem
// "lock:<lịch>" cho sự việc khóa.
func (s *Service) RunScheduleJob(ctx context.Context, tenants TenantLister) (ScheduleJobResult, error) {
	now := s.now()
	tids, err := tenants(ctx, now)
	if err != nil {
		return ScheduleJobResult{}, err
	}
	var total ScheduleJobResult
	var errs []error
	for _, tid := range tids {
		r, err := s.ScheduleJobTenant(ctx, tid, now)
		total.Locked += r.Locked
		total.Reminders += r.Reminders
		total.Purged += r.Purged
		if err != nil {
			errs = append(errs, fmt.Errorf("tenant %s: %w", tid, err))
		}
	}
	return total, errors.Join(errs...)
}

// ScheduleJobTenant chạy job lịch công tác cho một tenant tại thời điểm now.
func (s *Service) ScheduleJobTenant(ctx context.Context, tid uuid.UUID, now time.Time) (ScheduleJobResult, error) {
	ctx = tenant.WithID(ctx, tid)
	var res ScheduleJobResult
	zone := clock.Zone()
	thisWeek := domain.WeekStartOf(now, zone)
	// (0) W11.46: xóa tọa độ check-in quá hạn lưu (bảng work_schedule_checkin_point, không phải sổ).
	l := now.In(zone)
	today := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
	for {
		var n int
		if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
			var e error
			n, e = store.PurgeCheckinPoints(ctx, tx, today, purgeBatch)
			return e
		}); err != nil {
			return res, err
		}
		res.Purged += n
		if n < purgeBatch {
			break
		}
	}
	for {
		n, err := s.lockEnded(ctx, tid, thisWeek, now)
		res.Locked += n
		if err != nil {
			return res, err
		}
		if n < lockBatch {
			break
		}
	}
	// Phạm vi cần xét: tỉnh có cán bộ trong danh bạ (Phòng Trồng trọt) + (phòng ban, tỉnh) còn lịch chờ duyệt.
	var provinces []string
	var pending map[store.ScheduleScope][]time.Time
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		if provinces, e = store.ActiveStaffProvinces(ctx, tx); e != nil {
			return e
		}
		pending, e = store.PendingScopes(ctx, tx, thisWeek)
		return e
	})
	if err != nil {
		return res, err
	}
	scopes := map[store.ScheduleScope]bool{}
	for _, p := range provinces {
		scopes[store.ScheduleScope{Department: partyport.DeptTrongTrot, Province: p}] = true
	}
	for k := range pending {
		scopes[k] = true
	}
	var errs []error
	next := thisWeek.AddDate(0, 0, 7)
	for sc := range scopes {
		st, err := s.scheduleSettings(ctx, sc.Department, sc.Province, now)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		// (2) Nhắc đăng ký tuần kế tiếp.
		due := st.SubmitDue(next)
		if !now.Before(due.Add(-time.Duration(st.RemindBefore)*time.Hour)) && now.Before(due) {
			ok, err := s.remindSubmit(ctx, tid, sc, next, due, now)
			if err != nil {
				errs = append(errs, err)
			} else if ok {
				res.Reminders++
			}
		}
		// (3) Nhắc người duyệt: lịch chờ duyệt quá hạn duyệt của tuần chưa kết thúc.
		for _, w := range pending[sc] {
			rdue := st.ReviewDue(w)
			if now.Before(rdue) || domain.WeekLocked(w, now, zone) {
				continue
			}
			ok, err := s.remindReview(ctx, tid, sc, w, rdue, now, st)
			if err != nil {
				errs = append(errs, err)
			} else if ok {
				res.Reminders++
			}
		}
	}
	return res, errors.Join(errs...)
}

func (s *Service) lockEnded(ctx context.Context, tid uuid.UUID, thisWeek, now time.Time) (int, error) {
	n := 0
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		list, err := store.EndedUnlocked(ctx, tx, thisWeek, lockBatch)
		if err != nil {
			return err
		}
		for _, sc := range list {
			ev := s.scheduleEvent(tid, sc, domain.ScheduleActLocked, SystemActor, now, "lock:"+sc.ID.String())
			if err := store.InsertScheduleEvent(ctx, tx, &ev); err != nil {
				return err
			}
			at := now
			sc.LockedAt, sc.LastEventID, sc.LastSeq = &at, ev.ID, ev.Seq
			if err := store.UpsertSchedule(ctx, tx, tid, sc); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
		return 0, nil // bản chạy khác vừa khóa
	}
	return n, err
}

func (s *Service) remindSubmit(ctx context.Context, tid uuid.UUID, sc store.ScheduleScope, week, due, now time.Time) (bool, error) {
	offs, err := s.knOfficers(ctx, tid, sc.Department, sc.Province)
	if err != nil {
		return false, err
	}
	var list []domain.Schedule
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		list, e = store.SchedulesOfWeek(ctx, tx, sc.Department, sc.Province, week)
		return e
	}); err != nil {
		return false, err
	}
	sent := map[uuid.UUID]bool{}
	for _, l := range list {
		if l.FirstSubmitted != nil {
			sent[l.OwnerRef] = true
		}
	}
	recipients := []uuid.UUID{}
	for _, o := range offs {
		if !sent[o.ActorRef] {
			recipients = append(recipients, o.ActorRef)
		}
	}
	if len(recipients) == 0 {
		return false, nil
	}
	return s.remind(ctx, tid, port.ScheduleReminderSubmitDue, sc, week, due, now, recipients, len(recipients))
}

func (s *Service) remindReview(ctx context.Context, tid uuid.UUID, sc store.ScheduleScope, week, due, now time.Time, st ScheduleSettings) (bool, error) {
	var list []domain.Schedule
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		list, e = store.SchedulesOfWeek(ctx, tx, sc.Department, sc.Province, week)
		return e
	}); err != nil {
		return false, err
	}
	pendingN := 0
	for _, l := range list {
		if l.Status == domain.SchedulePending {
			pendingN++
		}
	}
	if pendingN == 0 {
		return false, nil
	}
	recipients, err := s.approvers(ctx, tid, sc.Department, sc.Province, uuid.Nil, st)
	if err != nil {
		return false, err
	}
	return s.remind(ctx, tid, port.ScheduleReminderReviewOverdue, sc, week, due, now, recipients, pendingN)
}

func (s *Service) remind(ctx context.Context, tid uuid.UUID, kind string, sc store.ScheduleScope, week, due, now time.Time,
	recipients []uuid.UUID, count int) (bool, error) {
	sent := false
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		ok, err := store.InsertScheduleReminder(ctx, tx, tid, ids.New(), kind, week, sc.Department, sc.Province, len(recipients), now)
		if err != nil || !ok {
			return err
		}
		sent = true
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: tid, Type: port.EventWorkScheduleReminded, SchemaVersion: 1, OccurredAt: now,
			TraceID: telemetry.TraceID(ctx), Producer: port.EventProducerName,
			Payload: port.WorkScheduleRemindedV1{Kind: kind, DepartmentCode: sc.Department, ProvinceCode: sc.Province,
				WeekStart: week.Format(time.DateOnly), DueAt: due.UTC().Format(time.RFC3339), RecipientRefs: recipients, Pending: count},
		})
	})
	return sent, err
}

// ScheduleTenants dùng pool vai ag_ops_jobs (BYPASSRLS, chỉ đọc cột tenant_id/week_start/locked_at của
// work_schedule_current và tenant_id/active của staff — migration 000075) để tìm tenant cần chạy job lịch.
func ScheduleTenants(pool *pgxpool.Pool) TenantLister {
	return func(ctx context.Context, _ time.Time) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, `SELECT tenant_id FROM pf_extension.work_schedule_current WHERE locked_at IS NULL
		  UNION SELECT tenant_id FROM pf_extension.staff WHERE active`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, rows.Err()
	}
}
