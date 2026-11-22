package app

// NOTIFY-HCTH-KN (#50, W11.27 FN-11; prototype "Nhắc phòng chưa lập"): Phòng HCTH/Admin (phiên có quyền GHI kế hoạch, cấp
// Trung tâm) nhắc các đơn vị nội bộ CHƯA gửi kế hoạch của kỳ đang mở. Sổ plan_reminder và sự kiện pf.hrm.plan.reminded ghi
// CÙNG giao dịch; pf/notify gửi lãnh đạo các đơn vị đó tại Nút của phiên người nhắc (builder module_notify.go) — không gửi
// toàn tenant theo vai realm, không gửi đơn vị đã gửi.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// PlanReminderUnit là một đơn vị được nhắc.
type PlanReminderUnit struct {
	Unit     string
	UnitName string
}

// PlanReminder là kết quả một lần nhắc.
type PlanReminder struct {
	ID         uuid.UUID
	Year       int
	PeriodRef  uuid.UUID
	Units      []PlanReminderUnit
	DueAt      time.Time
	RemindedAt time.Time
}

// RemindPlan nhắc các đơn vị chưa gửi kế hoạch năm year. Kỳ đã chốt/hủy → hrm.plan_closed; mọi đơn vị đã gửi →
// hrm.plan_all_submitted (không ghi sổ, không phát sự kiện). Gọi lại cùng idem (request chạy lại) trả lần nhắc đã ghi, không
// phát thêm sự kiện.
func (s *Service) RemindPlan(ctx context.Context, year int, idem string) (PlanReminder, error) {
	c, err := s.planCaller(ctx)
	if err != nil {
		return PlanReminder{}, err
	}
	if err := c.needCenter(true); err != nil {
		return PlanReminder{}, err
	}
	row, err := s.planPeriod(ctx, c.p.TenantID, year)
	if err != nil {
		return PlanReminder{}, err
	}
	pr, err := s.Plans.Control.Progress(ctx, port.PlanProfile, row.PeriodRef)
	if err != nil {
		return PlanReminder{}, err
	}
	if pr.Status == "closed" || pr.Status == "cancelled" {
		return PlanReminder{}, port.ErrPlanClosed
	}
	if c.unitRef == uuid.Nil {
		// Người nhận tra theo Nút của phiên — thiếu Nút thì không suy rộng ra toàn tenant (đóng an toàn).
		return PlanReminder{}, problem.Forbidden.WithDetail("nhắc kế hoạch: phiên chưa gắn Nút đơn vị")
	}
	var units []string
	for _, u := range pr.Units {
		if u.Versions == 0 {
			units = append(units, u.Unit)
		}
	}
	if len(units) == 0 {
		return PlanReminder{}, port.ErrPlanAllSubmitted
	}
	names := map[string]string{}
	if s.Rec != nil && s.Rec.Units != nil {
		us, err := s.Rec.Units.InternalUnits(ctx, c.unitRef)
		if err != nil {
			return PlanReminder{}, err
		}
		for _, u := range us {
			names[u.Code] = u.Name
		}
	}
	if idem == "" {
		idem = "hrm-plan-remind:" + ids.New().String()
	}
	now := s.Clock.Now()
	x := store.PlanReminderRow{ID: ids.New(), PeriodRef: row.PeriodRef, Year: year, NodeRef: c.unitRef, Units: units, Actor: c.p.ActorRef,
		RecordedAt: now}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.InsertPlanReminder(ctx, tx, c.p.TenantID, x, now, idem); err != nil {
			return err
		}
		return enqueueEvent(ctx, tx, c.p.TenantID, now, port.EventPlanReminded, port.PlanRemindedV1{ReminderID: x.ID, Year: year,
			PeriodRef: row.PeriodRef, NodeRef: c.unitRef, Units: units, DueAt: row.DueAt.UTC().Format(time.RFC3339), ActorRef: c.p.ActorRef})
	})
	if isUnique(err) {
		// Request chạy lại cùng khóa: trả lần nhắc đã ghi (sự kiện đã phát cùng giao dịch đó).
		var old *store.PlanReminderRow
		if e := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) (e error) {
			old, e = store.PlanReminderByIdem(ctx, tx, idem)
			return e
		}); e != nil {
			return PlanReminder{}, e
		}
		if old == nil {
			return PlanReminder{}, classify(err)
		}
		x, err = *old, nil
	}
	if err != nil {
		return PlanReminder{}, err
	}
	out := PlanReminder{ID: x.ID, Year: x.Year, PeriodRef: x.PeriodRef, DueAt: row.DueAt, RemindedAt: x.RecordedAt}
	for _, u := range x.Units {
		n := names[u]
		if n == "" {
			n = u
		}
		out.Units = append(out.Units, PlanReminderUnit{Unit: u, UnitName: n})
	}
	return out, nil
}
