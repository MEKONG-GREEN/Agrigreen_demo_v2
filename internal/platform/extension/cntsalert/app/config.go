package app

// Cấu hình nhắc nhở theo biểu (UR-TINH-08, Hình 17a), cấu hình xử lý theo loại cảnh báo (UR-TINH-13: hạn xử lý, mức
// Khẩn của môi trường nước), xem ngưỡng (tham số cnts.* — sửa qua quy trình tham số của pf/masterdata) và job nhắc nộp
// trước hạn / khi trễ / nhắc lặp + trễ liên tiếp (FN-13 BR-02, AC-06).

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/cntsalert/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/cntsalert/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

// FormConfigView là cấu hình nhắc của một biểu.
type FormConfigView struct {
	Code, Label string
	Reminder    domain.FormReminder
	Custom      bool // đã có phiên bản do Cấp Tỉnh lưu (false = mặc định BRD)
	Version     int64
	UpdatedAt   *time.Time
	UpdatedBy   *Actor
	SentToday   int // số lượt nhắc đã gửi hôm nay (FN-13 luồng phụ)
}

// AlertConfigView là cấu hình xử lý của một loại cảnh báo.
type AlertConfigView struct {
	Kind    domain.KindInfo
	Setting domain.AlertSetting
	Level   string
	Custom  bool
	Version int64
}

// Threshold là một ngưỡng đang hiệu lực (tham số có phiên bản).
type Threshold struct {
	Key, Label, Unit string
	Value            *decimal.Decimal // nil = chưa có giá trị đã duyệt
	Version          int
	EffectiveFrom    *time.Time
}

// ConfigView là màn "Cấu hình nhắc nhở" + "Ngưỡng".
type ConfigView struct {
	Forms      []FormConfigView
	Alerts     []AlertConfigView
	Thresholds []Threshold
}

var thresholdKeys = []struct{ Key, Label, Unit string }{
	{mdport.ParamCNTSHerdChangePct, "Tổng đàn biến động (BM-04) quá", "%"},
	{mdport.ParamCNTSFishingChangePct, "Khai thác thủy sản biến động (BM-17) quá", "%"},
	{mdport.ParamCNTSSlaughterSpikeFactor, "KSGM bất thường: lớn hơn n lần trung bình 4 tuần", "lần"},
	{mdport.ParamCNTSSlaughterSpikeMinDays, "KSGM bất thường: số ngày có số liệu tối thiểu", "ngày"},
	{mdport.ParamCNTSLookbackPeriods, "\"Kỳ gần nhất\" tìm lùi tối đa", "kỳ"},
	{mdport.IndicatorKey(mdport.ParamCNTSIndicatorMinPrefix, "PH", ""), "pH tối thiểu", ""},
	{mdport.IndicatorKey(mdport.ParamCNTSIndicatorMaxPrefix, "PH", ""), "pH tối đa", ""},
	{mdport.IndicatorKey(mdport.ParamCNTSIndicatorMinPrefix, "DO", ""), "Oxy hòa tan tối thiểu", "mg/l"},
	{mdport.ParamCNTSLocationDistanceM, "Vị trí lần ghi cách điểm quá", "m"},
	{mdport.ParamCNTSGPSAccuracyM, "Sai số GPS quá", "m"},
	{mdport.ParamCNTSLogOverdueDays, "Quá hạn ghi nhật ký thêm", "ngày"},
}

func (s *Service) configs(ctx context.Context, tx pgx.Tx) (map[string]store.Config, error) {
	cs, err := store.Configs(ctx, tx)
	out := map[string]store.Config{}
	for _, c := range cs {
		out[c.Scope+":"+c.Code] = c
	}
	return out, err
}

func reminderOf(cs map[string]store.Config, code string) (domain.FormReminder, *store.Config) {
	if c, ok := cs[domain.ScopeForm+":"+code]; ok {
		var r domain.FormReminder
		if json.Unmarshal(c.Settings, &r) == nil && len(r.Normalize()) == 0 {
			return r, &c
		}
	}
	return domain.DefaultFormReminder(code), nil
}

// Config đọc cấu hình nhắc, xử lý cảnh báo và ngưỡng (Admin, Cấp Tỉnh, Cấp Xã đọc).
func (s *Service) Config(ctx context.Context) (ConfigView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return ConfigView{}, err
	}
	now := s.now()
	var cs map[string]store.Config
	var sent map[string]int
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if cs, e = s.configs(ctx, tx); e != nil {
			return e
		}
		d := today(now)
		start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, clock.Zone())
		sent, e = store.RemindersSince(ctx, tx, start)
		return e
	})
	if err != nil {
		return ConfigView{}, err
	}
	var refs []uuid.UUID
	for _, x := range cs {
		refs = append(refs, x.ActorRef)
	}
	name := s.actors(ctx, dedupRefs(refs))
	var v ConfigView
	for _, f := range domain.Forms {
		r, cfg := reminderOf(cs, f.Code)
		fv := FormConfigView{Code: f.Code, Label: f.Label, Reminder: r, SentToday: sent[f.Code]}
		if cfg != nil {
			at, by := cfg.EventTime, name(cfg.ActorRef)
			fv.Custom, fv.Version, fv.UpdatedAt, fv.UpdatedBy = true, cfg.Seq, &at, &by
		}
		v.Forms = append(v.Forms, fv)
	}
	for _, k := range domain.Kinds {
		set := domain.DefaultAlertSetting(k.Code)
		av := AlertConfigView{Kind: k}
		if x, ok := cs[domain.ScopeAlert+":"+k.Code]; ok {
			var st domain.AlertSetting
			if json.Unmarshal(x.Settings, &st) == nil && len(st.Normalize(k.Code)) == 0 {
				set, av.Custom, av.Version = st, true, x.Seq
			}
		}
		av.Setting, av.Level = set, domain.LevelFor(k.Code, set)
		v.Alerts = append(v.Alerts, av)
	}
	for _, t := range thresholdKeys {
		th := Threshold{Key: t.Key, Label: t.Label, Unit: t.Unit}
		if s.Params != nil {
			if pv, err := s.Params.At(ctx, t.Key, now); err == nil {
				val, ef := pv.Value, pv.EffectiveFrom
				th.Value, th.Version, th.EffectiveFrom = &val, pv.Version, &ef
				if pv.Unit != "" {
					th.Unit = pv.Unit
				}
			}
		}
		v.Thresholds = append(v.Thresholds, th)
	}
	return v, nil
}

func (s *Service) putConfig(ctx context.Context, scope, code string, settings any, idem string) (ConfigView, error) {
	c, err := s.manager(ctx)
	if err != nil {
		return ConfigView{}, err
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return ConfigView{}, err
	}
	onBehalf, support := c.support()
	if idem == "" {
		idem = "config:" + ids.New().String()
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		_, err := store.InsertConfig(ctx, tx, c.p.TenantID, newID(), scope, code, raw, s.now(), c.p.ActorRef, onBehalf, support, idem)
		return err
	})
	if err != nil && !dup(err) {
		return ConfigView{}, err
	}
	return s.Config(ctx)
}

func configProblem(es domain.Errors) error {
	p := ErrConfigInvalid
	for _, e := range es {
		p = p.WithField(e.Field, e.Reason)
	}
	return p
}

// PutFormReminder lưu phiên bản mới cấu hình nhắc của biểu (Cấp Tỉnh, Admin).
func (s *Service) PutFormReminder(ctx context.Context, code string, r domain.FormReminder, idem string) (ConfigView, error) {
	if !domain.IsForm(code) {
		return ConfigView{}, ErrConfigInvalid.WithField("code", "biểu kỳ CNTS không có")
	}
	if es := r.Normalize(); len(es) > 0 {
		return ConfigView{}, configProblem(es)
	}
	return s.putConfig(ctx, domain.ScopeForm, code, r, idem)
}

// PutAlertSetting lưu phiên bản mới cấu hình xử lý của loại cảnh báo (Cấp Tỉnh, Admin).
func (s *Service) PutAlertSetting(ctx context.Context, kind string, a domain.AlertSetting, idem string) (ConfigView, error) {
	if es := a.Normalize(kind); len(es) > 0 {
		return ConfigView{}, configProblem(es)
	}
	return s.putConfig(ctx, domain.ScopeAlert, kind, a, idem)
}

// ---------------------------------------------------------------------------
// Job nhắc nộp + trễ liên tiếp
// ---------------------------------------------------------------------------

// remindWindow: kỳ có hạn trong [now − 45 ngày, now + 8 ngày] (nhắc trước hạn tối đa 7 ngày; trễ liên tiếp ≤ 12 kỳ tuần
// không cần đủ — cửa sổ đủ cho 3 kỳ tuần, 45 kỳ ngày).
const (
	remindBack  = 45 * 24 * time.Hour
	remindAhead = 8 * 24 * time.Hour
)

func accepting(status string) bool { return status == "open" || status == "reopened" }

// Remind gửi nhắc nộp đến hạn và sinh cảnh báo trễ liên tiếp cho tenant tại now. Idempotent (cnts_reminder_sent, khóa
// chống trùng của cảnh báo).
func (s *Service) Remind(ctx context.Context, tid uuid.UUID, now time.Time) (int, int, error) {
	if s.Feed == nil {
		return 0, 0, nil
	}
	ps, err := s.Feed.DuePeriods(ctx, tid, ProfileCNTS, now.Add(-remindBack), now.Add(remindAhead))
	if err != nil {
		return 0, 0, err
	}
	loc := clock.Zone()
	sent, streaks := 0, 0
	err = s.tx(ctx, tid, func(tx pgx.Tx) error {
		cs, err := s.configs(ctx, tx)
		if err != nil {
			return err
		}
		officers := map[string][]uuid.UUID{} // xã → cán bộ xã
		commune := func(code string) []uuid.UUID {
			if v, ok := officers[code]; ok {
				return v
			}
			rs := &resolver{s: s, tid: tid, province: s.provinceOf(ctx, code, now)}
			v := rs.resolve(ctx, []string{domain.RecipCommune}, code, uuid.Nil)
			officers[code] = v
			return v
		}
		// 1. Nhắc nộp.
		for _, p := range ps {
			if !accepting(p.Status) || !domain.IsForm(p.TemplateCode) {
				continue
			}
			cfg, _ := reminderOf(cs, p.TemplateCode)
			slots := reminderSlots(cfg, p.DueAt, now, loc)
			if len(slots) == 0 {
				continue
			}
			for _, u := range p.Units {
				if u.Versions > 0 || (u.Status != "not_submitted" && u.Status != "overdue") {
					continue
				}
				for _, slot := range slots {
					recips := commune(u.Parent)
					ok, err := store.MarkReminder(ctx, tx, tid, p.ID, u.Unit, slot, p.TemplateCode, len(recips), now)
					if err != nil {
						return err
					}
					if !ok {
						continue
					}
					if err := emitReminded(ctx, tx, tid, p, u, slot, cfg, recips, now, loc); err != nil {
						return err
					}
					sent++
				}
			}
		}
		// 2. Trễ liên tiếp (UR-TINH-08: từ 2 kỳ BM-04/BM-17/BM-05, 3 kỳ BM-01 → người phụ trách biểu, Lãnh đạo phòng).
		n, err := s.lateStreaks(ctx, tx, tid, ps, cs, now)
		streaks = n
		return err
	})
	return sent, streaks, err
}

// reminderSlots trả các lượt nhắc đến giờ tại now cho kỳ có hạn due.
func reminderSlots(cfg domain.FormReminder, due, now time.Time, loc *time.Location) []string {
	var out []string
	if cfg.BeforeDueHours > 0 && !now.Before(due.Add(-time.Duration(cfg.BeforeDueHours)*time.Hour)) && now.Before(due) {
		out = append(out, port.CNTSReminderBefore)
	}
	if cfg.OnOverdue && !now.Before(due) && now.Before(due.Add(24*time.Hour)) {
		out = append(out, port.CNTSReminderOverdue)
	}
	if t, ok := cfg.RepeatTime(now, loc); ok && !now.Before(t) {
		dueDay := due.In(loc).Format(time.DateOnly)
		day := now.In(loc).Format(time.DateOnly)
		if day > dueDay {
			out = append(out, "repeat:"+day)
		}
	}
	return out
}

func emitReminded(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p formsport.PeriodProgress, u formsport.UnitProgress, slot string,
	cfg domain.FormReminder, recips []uuid.UUID, now time.Time, loc *time.Location) error {
	kind := slot
	if len(slot) > 7 && slot[:7] == "repeat:" {
		kind = port.CNTSReminderRepeat
	}
	if recips == nil {
		recips = []uuid.UUID{}
	}
	pl := port.CNTSFormRemindedV1{PeriodRef: p.ID, TemplateCode: p.TemplateCode, PeriodLabel: p.Label, Unit: u.Unit, CommuneCode: u.Parent,
		Slot: kind, DueAt: p.DueAt.UTC().Format(time.RFC3339), Message: cfg.Render(domain.FormLabel(p.TemplateCode), p.Label, p.DueAt, loc),
		Channels: cfg.Channels, RecipientRefs: recips, Department: domain.Department, EventTime: now.UTC().Format(time.RFC3339)}
	return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: port.EventCNTSFormReminded, SchemaVersion: 1, OccurredAt: now,
		Producer: port.EventProducerName, Payload: pl})
}

type unitPeriod struct {
	p    formsport.PeriodProgress
	u    formsport.UnitProgress
	late bool
}

// lateStreaks sinh cảnh báo trễ liên tiếp: đơn vị trễ (chưa gửi khi quá hạn, hoặc gửi sau hạn) ở n kỳ liên tiếp mới nhất
// đã quá hạn của cùng biểu. Chống trùng theo kỳ đầu của chuỗi trễ.
func (s *Service) lateStreaks(ctx context.Context, tx pgx.Tx, tid uuid.UUID, ps []formsport.PeriodProgress, cs map[string]store.Config,
	now time.Time) (int, error) {
	series := map[string][]unitPeriod{} // biểu|đơn vị → kỳ đã quá hạn
	for _, p := range ps {
		if !domain.IsForm(p.TemplateCode) || now.Before(p.DueAt) {
			continue
		}
		for _, u := range p.Units {
			late := u.Late || u.Versions == 0
			series[p.TemplateCode+"|"+u.Unit] = append(series[p.TemplateCode+"|"+u.Unit], unitPeriod{p: p, u: u, late: late})
		}
	}
	n := 0
	keys := make([]string, 0, len(series))
	for k := range series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		xs := series[k]
		sort.Slice(xs, func(i, j int) bool { return xs[i].p.DueAt.After(xs[j].p.DueAt) })
		late := make([]bool, len(xs))
		for i, x := range xs {
			late[i] = x.late
		}
		cfg, _ := reminderOf(cs, xs[0].p.TemplateCode)
		streak := domain.LateStreak(late)
		if cfg.LateStreak <= 0 || streak < cfg.LateStreak {
			continue
		}
		first, last := xs[streak-1], xs[0]
		province := s.provinceOf(ctx, last.u.Parent, now)
		rs := &resolver{s: s, tid: tid, province: province}
		kind, _ := domain.KindOf(domain.KindLateStreak)
		subjKind := "commune"
		if last.u.Parent != "" && last.u.Parent != last.u.Unit {
			subjKind = "facility"
		}
		scope := first.p.PeriodKey
		if scope == "" {
			scope = first.p.ID.String()
		}
		pid := last.p.ID
		labels := make([]string, 0, streak)
		for i := streak - 1; i >= 0; i-- {
			labels = append(labels, xs[i].p.Label)
		}
		r := Raise{Kind: domain.KindLateStreak, Subject: last.u.Unit, SubjectKind: subjKind, Scope: scope, CommuneCode: last.u.Parent,
			ProvinceCode: province, TemplateCode: last.p.TemplateCode, PeriodKey: last.p.PeriodKey, PeriodRef: &pid,
			Title:      kind.Label + " · " + domain.FormLabel(last.p.TemplateCode) + " · " + itoa(int64(streak)) + " kỳ · xã " + last.u.Parent,
			Recipients: rs.resolve(ctx, kind.Recipients, last.u.Parent, uuid.Nil), At: now,
			Extra: map[string]any{"streak": streak, "threshold": cfg.LateStreak, "periods": labels,
				"reason": "Trễ hạn " + itoa(int64(streak)) + " kỳ liên tiếp (ngưỡng " + itoa(int64(cfg.LateStreak)) + " kỳ)"}}
		if _, created, err := s.raise(ctx, tx, tid, r); err != nil {
			return n, err
		} else if created {
			n++
		}
	}
	return n, nil
}
