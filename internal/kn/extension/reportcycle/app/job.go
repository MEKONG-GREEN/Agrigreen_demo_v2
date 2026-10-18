package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// TenantLister trả tenant có kỳ báo cáo (job vòng đời báo cáo).
type TenantLister func(ctx context.Context) ([]uuid.UUID, error)

// PeriodTenants dựng bộ tìm tenant từ pool vai ag_ops_jobs (chỉ đọc (tenant_id, status) của report_period).
func PeriodTenants(pool *pgxpool.Pool) TenantLister {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, `SELECT DISTINCT tenant_id FROM pf_extension.report_period`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
}

// JobResult là kết quả một lượt job.
type JobResult struct {
	Closed    int // kỳ tự chốt
	Reclosed  int // kỳ tự chốt lại sau hạn mở
	Reminders int // lần tự nhắc
}

// RunJob chạy một lượt cho mọi tenant (worker, leader). Idempotent: kỳ đã chốt không chốt lại; nhắc tự động
// không lặp trong cùng cửa sổ.
func (s *Service) RunJob(ctx context.Context, tenants TenantLister) (JobResult, error) {
	tids, err := tenants(ctx)
	if err != nil {
		return JobResult{}, err
	}
	var total JobResult
	var errs []error
	for _, tid := range tids {
		r, err := s.JobTenant(ctx, tid, s.now())
		total.Closed += r.Closed
		total.Reclosed += r.Reclosed
		total.Reminders += r.Reminders
		if err != nil {
			errs = append(errs, err)
		}
	}
	return total, errors.Join(errs...)
}

// JobTenant: tự chốt kỳ quá giờ chốt (kèm tổng hợp "Hệ thống"), tự chốt lại xã/biểu hết hạn mở, tự nhắc trước hạn
// và khi trễ hạn theo tham số (Q-15, tạm).
func (s *Service) JobTenant(ctx context.Context, tid uuid.UUID, now time.Time) (JobResult, error) {
	// Cổng của module khác (khu vực P2, engine mẫu P3, cán bộ phụ trách P1) đọc tenant từ ngữ cảnh: job không có request nên
	// phải gắn tenant trước khi chốt/chốt lại/tổng hợp (lỗi P8: "tenant: thiếu tenant_id" khi chốt lại tự động trên gốc lắp ráp thật).
	ctx = tenant.WithID(ctx, tid)
	var out JobResult
	var due []store.Period
	if err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		due, err = store.DuePeriods(ctx, tx, now, 200)
		return err
	}); err != nil {
		return out, err
	}
	for _, p := range due {
		var cons consolidateOut
		err := s.tx(ctx, tid, func(tx pgx.Tx) error {
			lp, err := store.PeriodByID(ctx, tx, p.ID, true)
			if err != nil || lp == nil || lp.Status != "open" {
				return err
			}
			b, err := store.BatchByID(ctx, tx, lp.BatchID, false)
			if err != nil {
				return err
			}
			cons, err = s.closeTx(ctx, tx, tid, *lp, *b, port.SystemActor, nil, "auto", "auto-close:"+lp.ID.String(), now)
			return err
		})
		if err != nil {
			return out, err
		}
		s.afterConsolidate(cons)
		out.Closed++
	}
	var expired []store.Task
	if err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		expired, err = store.ExpiredReopens(ctx, tx, now, 5000)
		return err
	}); err != nil {
		return out, err
	}
	byPeriod := map[uuid.UUID][]uuid.UUID{}
	var order []uuid.UUID
	for _, t := range expired {
		if _, ok := byPeriod[t.PeriodID]; !ok {
			order = append(order, t.PeriodID)
		}
		byPeriod[t.PeriodID] = append(byPeriod[t.PeriodID], t.ID)
	}
	for _, pid := range order {
		var cons consolidateOut
		err := s.tx(ctx, tid, func(tx pgx.Tx) error {
			p, err := store.PeriodByID(ctx, tx, pid, true)
			if err != nil || p == nil {
				return err
			}
			b, err := store.BatchByID(ctx, tx, p.BatchID, false)
			if err != nil {
				return err
			}
			tasks, err := store.TasksOfPeriod(ctx, tx, pid, nil, true)
			if err != nil {
				return err
			}
			cons, err = s.reclose(ctx, tx, tid, *p, *b, tasks, byPeriod[pid], port.SystemActor, nil, "auto-reclose:"+pid.String()+":"+ts(now), now)
			return err
		})
		if err != nil {
			return out, err
		}
		s.afterConsolidate(cons)
		out.Reclosed++
	}
	n, err := s.autoRemind(ctx, tid, now)
	out.Reminders = n
	return out, err
}

func (s *Service) autoRemind(ctx context.Context, tid uuid.UUID, now time.Time) (int, error) {
	before := s.paramInt(ctx, port.ParamReminderBeforeHours, 24)
	every := s.paramInt(ctx, port.ParamReminderOverdueEveryHours, 24)
	if before <= 0 && every <= 0 {
		return 0, nil
	}
	sent := 0
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		ps, err := store.OpenPeriodsOf(ctx, tx, 1000)
		if err != nil {
			return err
		}
		for _, p := range ps {
			var mode string
			var since time.Time
			switch {
			case before > 0 && now.Before(p.DueAt) && !now.Before(p.DueAt.Add(-time.Duration(before)*time.Hour)):
				mode, since = port.ReminderAutoBefore, p.DueAt.Add(-time.Duration(before)*time.Hour)
			case every > 0 && !now.Before(p.DueAt):
				mode, since = port.ReminderAutoOverdue, now.Add(-time.Duration(every)*time.Hour)
			default:
				continue
			}
			done, err := store.RemindedSince(ctx, tx, p.ID, mode, since)
			if err != nil {
				return err
			}
			pid := p.ID
			tasks, err := store.ListTasks(ctx, tx, store.TaskFilter{PeriodID: &pid})
			if err != nil {
				return err
			}
			var todo []store.Task
			for _, t := range tasks {
				if !done[t.ID] {
					todo = append(todo, t)
				}
			}
			rem, _, _ := pickReminder(todo, p, nil, now)
			if len(rem.TaskIDs) == 0 {
				continue
			}
			rem.ID, rem.BatchID, rem.PeriodID, rem.Mode, rem.Message = ids.New(), p.BatchID, p.ID, mode, autoMessage(mode, p)
			rem.Department, rem.EventTime, rem.ActorRef = p.DepartmentCode, now, port.SystemActor
			rem.IdemKey = "auto-remind:" + p.ID.String() + ":" + mode + ":" + ts(now)
			if err := s.recordReminder(ctx, tx, tid, &rem, p); err != nil {
				return err
			}
			sent++
		}
		return nil
	})
	return sent, err
}

// ---------------------------------------------------------------------------
// Cổng cho pf/party (P2): bảng Ảnh hưởng (kỳ chưa chốt), số bản ghi theo phòng ban
// ---------------------------------------------------------------------------

// OrgImpact hiện thực party/port.OrgImpactSource — dòng "Kỳ báo cáo chưa chốt" của bảng Ảnh hưởng (FN-AD-04.02).
type OrgImpact struct{ Svc *Service }

var _ partyport.OrgImpactSource = OrgImpact{}

// ImpactKind trả tên dòng.
func (OrgImpact) ImpactKind() string { return partyport.ImpactOpenPeriods }

// OrgImpact đếm kỳ chưa chốt có công việc ở các xã của nhánh Nút.
func (o OrgImpact) OrgImpact(ctx context.Context, _ uuid.UUID, communes []string) (int, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return 0, nil
	}
	if len(communes) == 0 {
		return 0, nil
	}
	var n int
	err := o.Svc.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var err error
		n, err = store.OpenPeriodsForCommunes(ctx, tx, communes)
		return err
	})
	return n, err
}

// DepartmentUsage hiện thực party/port.DepartmentUsage — số công việc báo cáo của phòng ban (chặn "xóa" phòng ban
// đã có dữ liệu, FN-AD-04.03).
type DepartmentUsage struct{ Svc *Service }

var _ partyport.DepartmentUsage = DepartmentUsage{}

// DepartmentUsage đếm bản ghi.
func (d DepartmentUsage) DepartmentUsage(ctx context.Context, _ uuid.UUID, code string) (partyport.Usage, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return partyport.Usage{}, nil
	}
	var n int
	err := d.Svc.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var err error
		n, err = store.DepartmentRecords(ctx, tx, code)
		return err
	})
	return partyport.Usage{Records: n}, err
}

var _ = domain.TaskClosed
