package app

// Lịch báo giá, ngoại lệ, giờ khóa, hạn phản hồi, nhắc (W11.74 — BRD Báo giá v1.3 FN-AD-04.04, FN-AD-04.05). Cấu hình
// mặc định ở port.Profile.Schedule (gốc lắp ráp, không so tên ứng dụng — R-MOD-04); tenant ghi phiên bản mới ở
// pf_price.calendar_rule. Ghi: vai ManageBy của Profile (QTNV báo giá của Cấp Tỉnh thêm ở W11.72).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

var _ port.Calendar = (*Service)(nil)

// calendarDefaults là mặc định đã đọc của một Profile.
type calendarDefaults struct {
	schedule  domain.Schedule
	threshold domain.Threshold
	preview   int
}

// scheduleDefaults đọc cấu hình mặc định của Profile (trường rỗng → giá trị BRD). Sai → lỗi (gốc lắp ráp gọi lúc
// khởi động qua ValidateProfiles).
func scheduleDefaults(p port.Profile) (calendarDefaults, error) {
	d := p.Schedule
	out := calendarDefaults{preview: d.PreviewDays}
	if out.preview <= 0 {
		out.preview = 30
	}
	s := domain.Schedule{Weekdays: domain.WeekdaysOf(d.Weekdays...), Template: d.Template}
	if len(d.Weekdays) == 0 {
		s.Weekdays = domain.MonFri
	}
	if s.Template == "" {
		s.Template = domain.DefaultReminderTemplate
	}
	for _, f := range []struct {
		dst *domain.TimeOfDay
		src string
		def string
	}{{&s.LockAt, d.LockAt, "12:00"}, {&s.RespondBy, d.RespondBy, "13:30"}, {&s.Remind1, d.Remind1, "07:30"}, {&s.Remind2, d.Remind2, "11:00"}} {
		src := f.src
		if src == "" {
			src = f.def
		}
		t, err := domain.ParseTimeOfDay(src)
		if err != nil {
			return out, fmt.Errorf("price: cấu hình %q: %w", p.Name, err)
		}
		*f.dst = t
	}
	if len(d.Channels) == 0 {
		s.InApp, s.Email = true, true
	}
	for _, c := range d.Channels {
		switch c {
		case port.ChannelInApp:
			s.InApp = true
		case port.ChannelEmail:
			s.Email = true
		default:
			return out, fmt.Errorf("price: cấu hình %q: kênh %q", p.Name, c)
		}
	}
	if err := domain.ValidateSchedule(s); err != nil {
		return out, fmt.Errorf("price: cấu hình %q: %w", p.Name, err)
	}
	out.schedule = s
	params := domain.AlertParams{LookbackDays: d.LookbackDays, FlatPct: d.FlatPct, SurveyPct: d.SurveyPct}
	if params.LookbackDays == 0 {
		params.LookbackDays = 7
	}
	if params.FlatPct.IsZero() {
		params.FlatPct = decimal.NewFromInt(1)
	}
	if params.SurveyPct.IsZero() {
		params.SurveyPct = decimal.NewFromInt(40)
	}
	pct := d.ThresholdPct
	if pct.IsZero() {
		pct = decimal.NewFromInt(30)
	}
	out.threshold = domain.Threshold{Scope: domain.ThresholdScopeDefault, Pct: pct, Params: &params}
	if err := domain.ValidateThreshold(out.threshold); err != nil {
		return out, fmt.Errorf("price: cấu hình %q: %w", p.Name, err)
	}
	return out, nil
}

// ValidateProfiles kiểm cấu hình lịch/ngưỡng mặc định của mọi Profile (module.New gọi).
func ValidateProfiles(ps []port.Profile) error {
	var errs []error
	for _, p := range ps {
		if _, err := scheduleDefaults(p); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// calendarMapErr đổi lỗi quy tắc lịch sang problem.
func calendarMapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrRespondBeforeLock):
		return port.ErrRespondBeforeLock.WithDetail(err.Error())
	case errors.Is(err, domain.ErrRemindOrder):
		return port.ErrRemindOrder.WithDetail(err.Error())
	case errors.Is(err, domain.ErrTemplateVar):
		return port.ErrTemplateVar.WithDetail(err.Error())
	case errors.Is(err, domain.ErrExceptionMismatch):
		return port.ErrExceptionMismatch.WithDetail(err.Error())
	case errors.Is(err, domain.ErrDefaultThreshold):
		return port.ErrDefaultThreshold
	case errors.Is(err, domain.ErrInvalid):
		return problem.Validation.WithDetail(err.Error())
	}
	return err
}

// calendarIdem: khóa idem của sổ — Idempotency-Key HTTP + hậu tố (phát lại cùng khóa → trùng), vắng → mới.
func calendarIdem(idem, suffix string) string {
	if idem == "" {
		return "price:" + suffix + ":" + ids.New().String()
	}
	return idem + ":" + suffix
}

// calendarState là lịch của một bên dùng đọc trong một giao dịch.
type calendarState struct {
	defaults calendarDefaults
	versions []domain.Schedule
	history  []store.RuleRow
}

const calendarHistoryLimit = 200

func (s *Service) calendarState(ctx context.Context, tx pgx.Tx, prof port.Profile) (calendarState, error) {
	d, err := scheduleDefaults(prof)
	if err != nil {
		return calendarState{}, err
	}
	rows, err := store.CalendarRules(ctx, tx, prof.Name, calendarHistoryLimit)
	if err != nil {
		return calendarState{}, err
	}
	st := calendarState{defaults: d, history: rows}
	for _, r := range rows {
		st.versions = append(st.versions, r.Schedule)
	}
	return st, nil
}

// planRange dựng kế hoạch các ngày [from, from+n).
func (s *Service) planRange(ctx context.Context, tx pgx.Tx, prof string, st calendarState, from time.Time, n int) ([]domain.DayPlan, error) {
	to := from.AddDate(0, 0, n-1)
	excs, err := store.Exceptions(ctx, tx, prof, from, to)
	if err != nil {
		return nil, err
	}
	byDay := map[time.Time]*domain.Exception{}
	for i := range excs {
		byDay[excs[i].Day] = &excs[i]
	}
	out := make([]domain.DayPlan, 0, n)
	for i := 0; i < n; i++ {
		d := from.AddDate(0, 0, i)
		out = append(out, domain.Plan(d, domain.ScheduleAt(st.versions, d, st.defaults.schedule), byDay[d]))
	}
	return out, nil
}

// Day (port.Calendar) trả kế hoạch ngày day của bên dùng cho tenant của ctx (không kiểm vai — bên gọi đã kiểm).
func (s *Service) Day(ctx context.Context, profile string, day time.Time) (port.DayPlan, error) {
	prof, ok := s.calendarProfile(profile)
	if !ok {
		return port.DayPlan{}, port.ErrProfileNotFound
	}
	var p domain.DayPlan
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		st, err := s.calendarState(ctx, tx, prof)
		if err != nil {
			return err
		}
		ps, err := s.planRange(ctx, tx, prof.Name, st, domain.BusinessDay(day), 1)
		if err != nil {
			return err
		}
		p = ps[0]
		return nil
	})
	if err != nil {
		return port.DayPlan{}, err
	}
	return port.DayPlan{Day: p.Day, Kind: p.Kind, Quote: p.Quote, Reason: p.Reason, LockAt: p.LockAt, RespondBy: p.RespondBy}, nil
}

func (s *Service) calendarProfile(name string) (port.Profile, bool) {
	if name == "" && len(s.order) > 0 {
		name = s.order[0]
	}
	p, ok := s.profiles[name]
	return p, ok
}

// CalendarView là màn "Kỳ báo giá & giờ khóa" + "Nhắc nhập giá".
type CalendarView struct {
	Profile    string
	Now        time.Time
	Today      domain.DayPlan
	Current    domain.Schedule   // lịch có hiệu lực hôm nay
	Upcoming   []domain.Schedule // phiên bản có ValidFrom > hôm nay (đã lên lịch)
	Earliest   time.Time         // ngày áp dụng sớm nhất của thay đổi mới
	Days       []domain.DayPlan  // "Lịch khóa nhập" PreviewDays ngày tới (từ hôm nay)
	Exceptions []domain.Exception
	History    []store.RuleRow
	CanManage  bool
}

// Calendar đọc lịch của bên dùng cho người gọi (mọi cấp của Profile đọc được — Xã cần biết giờ khóa).
func (s *Service) Calendar(ctx context.Context, profile string) (CalendarView, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return CalendarView{}, err
	}
	now := s.Clock.Now()
	today := domain.BusinessDay(now)
	v := CalendarView{Profile: prof.Name, Now: now, CanManage: c.steward}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		st, err := s.calendarState(ctx, tx, prof)
		if err != nil {
			return err
		}
		if v.Days, err = s.planRange(ctx, tx, prof.Name, st, today, st.defaults.preview); err != nil {
			return err
		}
		if v.Exceptions, err = store.Exceptions(ctx, tx, prof.Name, today, today.AddDate(1, 0, 0)); err != nil {
			return err
		}
		v.Today = v.Days[0]
		v.Current = domain.ScheduleAt(st.versions, today, st.defaults.schedule)
		for i := len(st.versions) - 1; i >= 0; i-- {
			if st.versions[i].ValidFrom.After(today) {
				v.Upcoming = append(v.Upcoming, st.versions[i])
			}
		}
		v.History = st.history
		v.Earliest = domain.EarliestValidFrom(v.Today)
		return nil
	})
	return v, err
}

// SaveSchedule ghi một phiên bản lịch mới (FN-AD-04.04 B1–B2, FN-AD-04.05). Ngày áp dụng ≥ ngày áp dụng sớm nhất
// (không quá khứ; hôm nay là ngày báo giá → từ ngày mai). Trả phiên bản đã ghi.
func (s *Service) SaveSchedule(ctx context.Context, profile, idem string, in domain.Schedule) (domain.Schedule, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return domain.Schedule{}, err
	}
	if !c.steward {
		return domain.Schedule{}, problem.Forbidden.WithDetail("chỉ vai quản trị nghiệp vụ báo giá cấu hình được lịch")
	}
	if err := domain.ValidateSchedule(in); err != nil {
		return domain.Schedule{}, calendarMapErr(err)
	}
	now := s.Clock.Now()
	today := domain.BusinessDay(now)
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		st, err := s.calendarState(ctx, tx, prof)
		if err != nil {
			return err
		}
		ps, err := s.planRange(ctx, tx, prof.Name, st, today, 1)
		if err != nil {
			return err
		}
		if earliest := domain.EarliestValidFrom(ps[0]); in.ValidFrom.Before(earliest) {
			return port.ErrValidFrom.WithDetail("ngày áp dụng sớm nhất " + earliest.Format(time.DateOnly))
		}
		in.Version, err = store.InsertCalendarRule(ctx, tx, store.CalMeta{ID: ids.New(), Tenant: c.tenant, EventTime: now, Actor: c.ref,
			Idem: calendarIdem(idem, "calendar")}, prof.Name, in)
		return err
	})
	if err != nil {
		return domain.Schedule{}, calendarClassify(err)
	}
	return in, nil
}

// calendarClassify: idem trùng (phát lại) → 409; còn lại như classify.
func calendarClassify(err error) error {
	if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
		return problem.Conflict.WithDetail("thao tác đã được ghi (Idempotency-Key trùng)").WithCause(err)
	}
	return classify(err)
}

// AddException thêm ngày nghỉ / ngày báo giá bổ sung (FN-AD-04.04 B3). Chỉ ngày từ ngày mai (ngày đã có phiếu gửi
// không thêm ngày nghỉ được — phiếu ở W11.75; ngày đã qua/hôm nay chặn để không đổi kỳ đang chạy).
func (s *Service) AddException(ctx context.Context, profile, idem string, e domain.Exception) (domain.Exception, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return e, err
	}
	if !c.steward {
		return e, problem.Forbidden.WithDetail("chỉ vai quản trị nghiệp vụ báo giá cấu hình được lịch ngoại lệ")
	}
	if err := domain.ValidateException(e); err != nil {
		return e, calendarMapErr(err)
	}
	now := s.Clock.Now()
	if !e.Day.After(domain.BusinessDay(now)) {
		return e, port.ErrValidFrom.WithDetail("ngày ngoại lệ phải từ ngày mai")
	}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		have, err := store.Exceptions(ctx, tx, prof.Name, e.Day, e.Day)
		if err != nil {
			return err
		}
		if len(have) > 0 {
			return port.ErrExceptionExists.WithDetail(have[0].Kind + ": " + have[0].Reason)
		}
		st, err := s.calendarState(ctx, tx, prof)
		if err != nil {
			return err
		}
		if err := domain.CheckException(e, domain.ScheduleAt(st.versions, e.Day, st.defaults.schedule)); err != nil {
			return calendarMapErr(err)
		}
		e.Version, err = store.InsertException(ctx, tx, store.CalMeta{ID: ids.New(), Tenant: c.tenant, EventTime: now, Actor: c.ref,
			Idem: calendarIdem(idem, "exception")}, prof.Name, e, "active")
		return err
	})
	if err != nil {
		return e, calendarClassify(err)
	}
	return e, nil
}

// RemoveException xóa ngoại lệ của ngày (phiên bản 'retired'; chỉ ngày từ ngày mai).
func (s *Service) RemoveException(ctx context.Context, profile, idem string, day time.Time) error {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return err
	}
	if !c.steward {
		return problem.Forbidden.WithDetail("chỉ vai quản trị nghiệp vụ báo giá cấu hình được lịch ngoại lệ")
	}
	now := s.Clock.Now()
	if !day.After(domain.BusinessDay(now)) {
		return port.ErrValidFrom.WithDetail("chỉ xóa được ngoại lệ từ ngày mai")
	}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		have, err := store.Exceptions(ctx, tx, prof.Name, day, day)
		if err != nil {
			return err
		}
		if len(have) == 0 {
			return port.ErrExceptionNotFound
		}
		_, err = store.InsertException(ctx, tx, store.CalMeta{ID: ids.New(), Tenant: c.tenant, EventTime: now, Actor: c.ref,
			Idem: calendarIdem(idem, "exception-retire")}, prof.Name, have[0], "retired")
		return err
	})
	return calendarClassify(err)
}

// CalendarRuns đọc nhật ký job (khóa, nhắc) trong [from, to] — "Lịch sử gửi nhắc", mốc khóa thực tế.
func (s *Service) CalendarRuns(ctx context.Context, profile string, from, to time.Time) ([]store.Run, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	if to.Before(from) || to.Sub(from) > 92*24*time.Hour {
		return nil, problem.Validation.WithDetail("khoảng ngày tối đa 92 ngày, from ≤ to")
	}
	var out []store.Run
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var e error
		out, e = store.Runs(ctx, tx, prof.Name, from, to)
		return e
	})
	return out, err
}
