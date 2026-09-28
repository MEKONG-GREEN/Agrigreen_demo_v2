package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ScheduleInput là yêu cầu lập lịch gửi báo cáo.
type ScheduleInput struct {
	Schedule domain.Schedule // TemplateKey, Format, UnitCode, Params, Frequency, Weekday, DayOfMonth, Hour, Recipients
	IdemKey  string
}

// CreateSchedule lập lịch gửi định kỳ (US-DASH-03). Phạm vi của người lập được
// giải và lưu lại; việc xuất theo lịch chỉ trong phạm vi đó.
func (s *Service) CreateSchedule(ctx context.Context, in ScheduleInput) (domain.Schedule, bool, error) {
	p, err := authz.Require(ctx, schedulers...)
	if err != nil {
		return domain.Schedule{}, false, err
	}
	if in.IdemKey == "" {
		return domain.Schedule{}, false, problem.IdempotencyRequired
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return domain.Schedule{}, false, err
	}
	if sc.Empty() {
		return domain.Schedule{}, false, ErrOutOfScope.WithDetail("tài khoản chưa gắn đơn vị công tác")
	}
	sch := in.Schedule
	if err := sch.Validate(); err != nil {
		return domain.Schedule{}, false, fieldProblem(err)
	}
	now := s.now()
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ScheduleByIdemKey(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			sch, replayed = *prev, true
			return nil
		}
		t, err := resolveTemplate(ctx, tx, strings.TrimSpace(sch.TemplateKey))
		if err != nil {
			return err
		}
		if t.Status != domain.TemplateActive {
			return ErrTemplateRetired
		}
		if !hasAny(p, rolesFor(t.Def.Source)) {
			return problem.Forbidden.WithDetail("vai của bạn không được xuất mẫu này")
		}
		if !slices.Contains(t.Def.Formats, sch.Format) {
			return ErrFormat.WithField("format", "mẫu hỗ trợ "+strings.Join(t.Def.Formats, ", "))
		}
		params := sch.Params
		params.UnitCode = strings.TrimSpace(sch.UnitCode)
		if params.Period == "" && domain.Sources[t.Def.Source].NeedsPeriod {
			params.Period = domain.MonthOf(now) // chỉ để kiểm; kỳ thật tính theo từng mốc
		}
		norm, err := s.normalizeParams(ctx, t.Def, params, sc, now)
		if err != nil {
			return err
		}
		norm.Period = ""
		sch.ID, sch.TenantID, sch.TemplateKey, sch.Params, sch.UnitCode = ids.New(), p.TenantID, t.Key, norm, norm.UnitCode
		sch.OwnerRef, sch.OwnerRoles, sch.ScopeAll, sch.ScopeUnits = p.ActorRef, p.Roles, sc.All, sc.Units
		sch.Active, sch.CreatedAt, sch.UpdatedAt, sch.IdemKey = true, now, now, in.IdemKey
		return store.InsertSchedule(ctx, tx, &sch)
	})
	return sch, replayed, err
}

// scheduleVisible: người lập, hoặc người có phạm vi bao trọn phạm vi của lịch.
func scheduleVisible(p authz.Principal, sc authz.Scope, s domain.Schedule) bool {
	if s.OwnerRef == p.ActorRef || sc.All {
		return true
	}
	if s.ScopeAll {
		return false
	}
	for _, u := range s.ScopeUnits {
		if !sc.Allows(u) {
			return false
		}
	}
	return len(s.ScopeUnits) > 0
}

// ListSchedules liệt kê lịch người gọi được xem.
func (s *Service) ListSchedules(ctx context.Context) ([]domain.Schedule, error) {
	p, err := authz.Require(ctx, schedulers...)
	if err != nil {
		return nil, err
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return nil, err
	}
	var all []domain.Schedule
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		all, e = store.ListSchedules(ctx, tx, false)
		return e
	}); err != nil {
		return nil, err
	}
	out := []domain.Schedule{}
	for _, x := range all {
		if scheduleVisible(p, sc, x) {
			out = append(out, x)
		}
	}
	return out, nil
}

// DeactivateSchedule ngừng lịch kèm lý do (không xóa — lịch sử chạy giữ nguyên).
func (s *Service) DeactivateSchedule(ctx context.Context, id uuid.UUID, reason string) (domain.Schedule, error) {
	p, err := authz.Require(ctx, schedulers...)
	if err != nil {
		return domain.Schedule{}, err
	}
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return domain.Schedule{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return domain.Schedule{}, err
	}
	var out domain.Schedule
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		x, err := store.ScheduleByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if x == nil || !scheduleVisible(p, sc, *x) {
			return ErrScheduleNotFound
		}
		if x.Active {
			if err := store.DeactivateSchedule(ctx, tx, id, reason, s.now()); err != nil {
				return err
			}
			x.Active, x.DeactivatedReason = false, &reason
		}
		out = *x
		return nil
	})
	return out, err
}

// ScheduleRuns trả các lần chạy của một lịch.
func (s *Service) ScheduleRuns(ctx context.Context, id uuid.UUID) ([]store.Run, error) {
	p, err := authz.Require(ctx, schedulers...)
	if err != nil {
		return nil, err
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return nil, err
	}
	var out []store.Run
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		x, err := store.ScheduleByID(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if x == nil || !scheduleVisible(p, sc, *x) {
			return ErrScheduleNotFound
		}
		out, err = store.RunsOf(ctx, tx, id)
		return err
	})
	return out, err
}

// TenantLister trả các tenant có lịch gửi đang dùng.
type TenantLister func(ctx context.Context) ([]uuid.UUID, error)

// ScheduleTenants dùng pool vai ag_outbox_publisher (chỉ SELECT hai cột
// tenant_id, active của pf_reports.schedule — migration 000013).
func ScheduleTenants(pool *pgxpool.Pool) TenantLister {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, "SELECT DISTINCT tenant_id FROM pf_reports.schedule WHERE active")
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
}

// RunSchedules là thân job lịch gửi (worker, chỉ leader): với mỗi lịch đang dùng,
// mốc gần nhất ≤ bây giờ chưa chạy → tạo MỘT việc xuất (qua điểm kiểm soát DLP).
// Chạy lặp an toàn: khóa chính (lịch, mốc) của schedule_run.
func (s *Service) RunSchedules(ctx context.Context, tenants TenantLister) (int, error) {
	tids, err := tenants(ctx)
	if err != nil {
		return 0, err
	}
	fired := 0
	var errs []error
	for _, tid := range tids {
		n, err := s.FireDue(ctx, tid)
		fired += n
		if err != nil {
			errs = append(errs, fmt.Errorf("tenant %s: %w", tid, err))
		}
	}
	return fired, errors.Join(errs...)
}

// FireDue chạy các mốc đến hạn của một tenant; trả số việc xuất đã tạo.
func (s *Service) FireDue(ctx context.Context, tid uuid.UUID) (int, error) {
	ctx = tenant.WithID(ctx, tid) // tham số pf/masterdata tra theo tenant trong context
	now := s.now()
	var list []domain.Schedule
	if err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		list, e = store.ListSchedules(ctx, tx, true)
		return e
	}); err != nil {
		return 0, err
	}
	fired := 0
	var errs []error
	for _, sch := range list {
		slotAt, slot := sch.Slot(now)
		if slotAt.Before(sch.CreatedAt) {
			continue // mốc trước khi lập lịch — không gửi bù
		}
		ok, err := s.fire(ctx, sch.ID, tid, slotAt, slot, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("lịch %s mốc %s: %w", sch.ID, slot, err))
			continue
		}
		if ok {
			fired++
		}
	}
	return fired, errors.Join(errs...)
}

// fire chạy một mốc trong một giao dịch (khóa dòng lịch để tuần tự hóa).
func (s *Service) fire(ctx context.Context, id, tid uuid.UUID, slotAt time.Time, slot string, now time.Time) (bool, error) {
	created := false
	lg := s.log().With("event", "reports.schedule.fire", "tenant_id", tid, "schedule_id", id, "slot", slot)
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		sch, err := store.ScheduleByID(ctx, tx, id, true)
		if err != nil || sch == nil || !sch.Active {
			return err
		}
		run := store.Run{ScheduleID: id, SlotKey: slot, SlotAt: slotAt, FiredAt: now}
		skip := func(reason string) error {
			run.Status, run.SkipReason = "skipped", &reason
			_, err := store.ClaimRun(ctx, tx, tid, run)
			lg.Warn("bỏ mốc lịch gửi", "reason", reason, "outcome", "skipped")
			return err
		}
		if exists, err := store.RunExists(ctx, tx, id, slot); err != nil || exists {
			return err
		}
		t, err := resolveTemplate(ctx, tx, sch.TemplateKey)
		if errors.Is(err, ErrTemplateNotFound) || (err == nil && t.Status != domain.TemplateActive) {
			return skip("mẫu báo cáo đã ngừng dùng")
		}
		if err != nil {
			return err
		}
		params := sch.Params
		if domain.Sources[t.Def.Source].NeedsPeriod {
			params.Period = sch.ReportPeriod(slotAt)
		}
		sid := sch.ID
		job := domain.Job{
			ID: ids.New(), TenantID: tid, TemplateKey: t.Key, TemplateVersion: t.Version, Def: t.Def, Format: sch.Format, Params: params,
			ScopeAll: sch.ScopeAll, ScopeUnits: sch.ScopeUnits, RequestedBy: sch.OwnerRef, RequestedAt: now, Trigger: domain.TriggerSchedule,
			ScheduleID: &sid, ScheduleSlot: &slot, IdemKey: "schedule:" + sch.ID.String() + ":" + slot, Status: domain.JobQueued,
		}
		err = s.egress(ctx, tx, Egress{Kind: EgressScheduled, TenantID: tid, ActorRef: sch.OwnerRef, TemplateKey: t.Key, Format: sch.Format, JobID: job.ID})
		if errors.Is(err, ErrQuotaExceeded) {
			return skip("vượt hạn mức xuất trong ngày của người lập lịch")
		}
		if err != nil {
			return err
		}
		if err := s.enqueueJob(ctx, tx, &job); err != nil {
			return err
		}
		run.Status, run.JobID = "queued", &job.ID
		created, err = store.ClaimRun(ctx, tx, tid, run)
		if err == nil && !created {
			return errors.New("reports: mốc lịch đã có lần chạy")
		}
		lg.Info("tạo việc xuất theo lịch", "job_id", job.ID, "outcome", "ok")
		return err
	})
	return created, err
}
