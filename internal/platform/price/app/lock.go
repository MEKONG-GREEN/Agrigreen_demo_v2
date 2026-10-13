package app

// Job lịch báo giá "price_calendar" (W11.74 — BRD FN-AD-04.04 AC-01 "sai lệch ≤ 1 phút", FN-XA-03.04 BR-03): chạy
// mỗi phút ở leader của cmd/worker (shared/leader + shared/schedule, giờ VN). Mỗi (tenant, bên dùng, ngày, loại) đúng
// một lần — sổ pf_price.calendar_run (CHECK seq = 1 + idem_key):
//   - lock    : khi now ≥ giờ khóa của kỳ báo giá hôm nay — ghi mốc khóa thực tế (độ trễ = event_time − scheduled_at).
//     Việc CHẶN ghi không phụ thuộc job: port.DayPlan.Locked tính theo đồng hồ (W11.75 kiểm khi lưu phiếu), job chỉ ghi
//     sự việc cho nhật ký và cho bước "phiếu chưa gửi → Không nộp" của W11.75.
//   - remind1 : giờ nhắc 1 ≤ now < giờ khóa; remind2: giờ nhắc 2 ≤ now < giờ khóa, chỉ xã chưa gửi (BR-01). Ngày nghỉ,
//     ngày ngoài lịch không nhắc (BR-02). Mỗi lần nhắc phát pf.price.reminder.issued (outbox, cùng giao dịch) → pf/notify.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// CalendarJobActor là actor_ref của dòng sổ do job lịch ghi (không phải người).
var CalendarJobActor = uuid.MustParse("00000000-0000-0000-0000-000000011740")

// CalendarTenantLister trả tenant cần chạy job lịch.
type CalendarTenantLister func(ctx context.Context) ([]uuid.UUID, error)

// CalendarTenants dùng pool vai ag_ops_jobs (BYPASSRLS, chỉ cột tenant_id của pf_price.unit, pf_price.calendar_rule —
// pf 000139): tenant đã bật Báo giá (đã ghi danh mục nền hoặc cấu hình lịch).
func CalendarTenants(pool *pgxpool.Pool) CalendarTenantLister {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, `SELECT tenant_id FROM pf_price.unit UNION SELECT tenant_id FROM pf_price.calendar_rule`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
}

// CalendarJobResult là kết quả một lần chạy job.
type CalendarJobResult struct {
	Locked    int // mốc khóa đã ghi
	Reminders int // lần nhắc đã ghi
	Events    int // sự kiện nhắc đã phát
}

func (r *CalendarJobResult) add(o CalendarJobResult) {
	r.Locked += o.Locked
	r.Reminders += o.Reminders
	r.Events += o.Events
}

// RunCalendarJob chạy job cho mọi tenant tại đồng hồ của Service.
func (s *Service) RunCalendarJob(ctx context.Context, tenants CalendarTenantLister) (CalendarJobResult, error) {
	tids, err := tenants(ctx)
	if err != nil {
		return CalendarJobResult{}, err
	}
	now := s.Clock.Now()
	var total CalendarJobResult
	var errs []error
	for _, tid := range tids {
		r, err := s.CalendarJobTenant(ctx, tid, now)
		total.add(r)
		if err != nil {
			errs = append(errs, fmt.Errorf("tenant %s: %w", tid, err))
		}
	}
	return total, errors.Join(errs...)
}

// CalendarJobTenant chạy job cho một tenant tại now (mọi bên dùng). Idempotent.
func (s *Service) CalendarJobTenant(ctx context.Context, tid uuid.UUID, now time.Time) (CalendarJobResult, error) {
	var total CalendarJobResult
	var errs []error
	for _, name := range s.order {
		r, err := s.calendarJobProfile(ctx, tid, s.profiles[name], now)
		total.add(r)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	return total, errors.Join(errs...)
}

func (s *Service) calendarJobProfile(ctx context.Context, tid uuid.UUID, prof port.Profile, now time.Time) (CalendarJobResult, error) {
	today := domain.BusinessDay(now)
	var plan domain.DayPlan
	var done map[string]bool
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		st, err := s.calendarState(ctx, tx, prof)
		if err != nil {
			return err
		}
		ps, err := s.planRange(ctx, tx, prof.Name, st, today, 1)
		if err != nil {
			return err
		}
		plan = ps[0]
		done, err = store.RunKinds(ctx, tx, prof.Name, today)
		return err
	})
	if err != nil {
		return CalendarJobResult{}, err
	}
	var res CalendarJobResult
	var errs []error
	for _, kind := range plan.DueRuns(now) {
		if done[kind] {
			continue
		}
		var r CalendarJobResult
		var err error
		if kind == domain.RunLock {
			r, err = s.recordLock(ctx, tid, prof, plan, now)
		} else {
			r, err = s.remind(ctx, tid, prof, plan, kind, now)
		}
		if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
			continue // bản chạy khác vừa ghi
		}
		res.add(r)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", kind, err))
		}
	}
	return res, errors.Join(errs...)
}

func runIdem(profile string, day time.Time, kind string) string {
	return "price:run:" + profile + ":" + day.Format(time.DateOnly) + ":" + kind
}

func (s *Service) recordLock(ctx context.Context, tid uuid.UUID, prof port.Profile, plan domain.DayPlan, now time.Time) (CalendarJobResult, error) {
	note := ""
	if lag := now.Sub(plan.LockAt); lag > time.Minute {
		note = fmt.Sprintf("ghi muộn %s so với giờ khóa (worker không chạy đúng giờ); kỳ vẫn khóa đúng %s theo đồng hồ",
			lag.Truncate(time.Second), plan.Schedule.LockAt)
	}
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		if err := store.InsertRun(ctx, tx, store.CalMeta{ID: ids.New(), Tenant: tid, EventTime: now, Actor: CalendarJobActor,
			Idem: runIdem(prof.Name, plan.Day, domain.RunLock)}, prof.Name,
			store.Run{Day: plan.Day, Kind: domain.RunLock, ScheduledAt: plan.LockAt, Note: note}); err != nil {
			return err
		}
		if s.LockSummary == nil {
			return nil
		}
		// NOTIF: thông báo tổng hợp 12:00 cho Cấp Tỉnh (FN-TI-02.03 B4) — cùng giao dịch với dòng nhật ký (đúng một lần).
		sum, err := s.LockSummary(ctx, tx, plan.Day)
		if err != nil {
			return err
		}
		return enqueue(ctx, tx, tid, now, port.EventLockSummarized, port.LockSummarizedV1{Profile: prof.Name, Day: dayStr(plan.Day),
			LockAt: plan.LockAt.UTC().Format(time.RFC3339), Points: sum.Points, Submitted: sum.Submitted, Late: sum.Late,
			Missed: sum.Missed, OpenAlerts: sum.OpenAlerts})
	})
	if err != nil {
		return CalendarJobResult{}, err
	}
	return CalendarJobResult{Locked: 1}, nil
}

// remind gửi nhắc lần 1/2: người nhận từ ReminderTargets (W11.72/W11.75); chưa nối → một thông báo chủ đề tới cán bộ
// xã của tenant. Lần 2 không còn xã chưa gửi → chỉ ghi nhật ký (0 người nhận).
func (s *Service) remind(ctx context.Context, tid uuid.UUID, prof port.Profile, plan domain.DayPlan, kind string, now time.Time) (CalendarJobResult, error) {
	round := 1
	scheduled := plan.Remind1
	if kind == domain.RunRemind2 {
		round, scheduled = 2, plan.Remind2
	}
	var targets []port.ReminderTarget
	topic := s.Reminders == nil
	if !topic {
		var err error
		if targets, err = s.Reminders.Targets(ctx, tid, prof.Name, plan.Day, round); err != nil {
			return CalendarJobResult{}, err
		}
	} else {
		targets = []port.ReminderTarget{{}}
	}
	sched := plan.Schedule
	title := fmt.Sprintf("Nhắc nhập giá lần %d — %s", round, plan.Day.Format("02/01/2006"))
	recipients, events := 0, 0
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		for _, t := range targets {
			if !topic && len(t.RecipientRefs) == 0 {
				continue
			}
			p := port.ReminderIssuedV1{Profile: prof.Name, Day: plan.Day.Format(time.DateOnly), Round: round,
				LockAt: plan.LockAt.Format(time.RFC3339), Title: title,
				Body:  domain.RenderReminder(sched.Template, plan.Day, t.CommuneName, sched.LockAt),
				InApp: sched.InApp, Email: sched.Email, Communes: []string{}, RecipientRefs: t.RecipientRefs}
			if p.RecipientRefs == nil {
				p.RecipientRefs = []uuid.UUID{}
			}
			if t.CommuneCode != "" {
				p.Communes = []string{t.CommuneCode}
			}
			if err := s.enqueueReminder(ctx, tx, tid, now, p); err != nil {
				return err
			}
			recipients += len(t.RecipientRefs)
			events++
		}
		note := ""
		switch {
		case topic:
			note = "thông báo chủ đề tới cán bộ xã của tenant (chưa có danh sách xã báo giá)"
		case events == 0:
			note = "không còn xã cần nhắc"
		}
		n := len(targets)
		if topic {
			n = 0
		}
		return store.InsertRun(ctx, tx, store.CalMeta{ID: ids.New(), Tenant: tid, EventTime: now, Actor: CalendarJobActor,
			Idem: runIdem(prof.Name, plan.Day, kind)}, prof.Name,
			store.Run{Day: plan.Day, Kind: kind, ScheduledAt: scheduled, Recipients: recipients, Targets: n, Note: note})
	})
	if err != nil {
		return CalendarJobResult{}, err
	}
	return CalendarJobResult{Reminders: 1, Events: events}, nil
}

func (s *Service) enqueueReminder(ctx context.Context, tx pgx.Tx, tid uuid.UUID, now time.Time, p port.ReminderIssuedV1) error {
	return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: port.EventReminderIssued, SchemaVersion: 1,
		OccurredAt: now, TraceID: telemetry.TraceID(ctx), Producer: port.EventProducerName, Payload: p})
}

// SendTestReminder "Gửi thử" (FN-AD-04.05 AC-02): thông báo mẫu của lịch hiện hành tới tài khoản đang đăng nhập.
// Không ghi nhật ký job. Vai ManageBy.
func (s *Service) SendTestReminder(ctx context.Context, profile string) (string, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return "", err
	}
	if !c.steward {
		return "", problem.Forbidden.WithDetail("chỉ vai quản trị nghiệp vụ báo giá gửi thử được")
	}
	now := s.Clock.Now()
	today := domain.BusinessDay(now)
	var body string
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		st, err := s.calendarState(ctx, tx, prof)
		if err != nil {
			return err
		}
		sched := domain.ScheduleAt(st.versions, today, st.defaults.schedule)
		body = domain.RenderReminder(sched.Template, today, "", sched.LockAt)
		return s.enqueueReminder(ctx, tx, c.tenant, now, port.ReminderIssuedV1{Profile: prof.Name, Day: today.Format(time.DateOnly),
			Round: 1, LockAt: sched.LockAt.On(today).Format(time.RFC3339), Title: "[Gửi thử] Nhắc nhập giá", Body: body,
			Communes: []string{}, RecipientRefs: []uuid.UUID{c.ref}, InApp: true, Email: sched.Email, Test: true})
	})
	return body, err
}
