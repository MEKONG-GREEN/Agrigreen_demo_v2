package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscampaign/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscampaign/store"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func newID() uuid.UUID { return ids.New() }

// change chạy một thao tác chuyển trạng thái: giữ khóa tư vấn của đợt trong giao dịch pf_extension, kiểm trạng thái, gọi
// cap/forms (giao dịch riêng của pf/forms) rồi ghi sự kiện trong cùng giao dịch pf_extension. Gọi lặp cùng khóa
// idempotent → không làm lại (trả đợt hiện tại). Ghi sự kiện lỗi sau khi cap/forms đã đổi → bù (undo) nếu có.
func (s *Service) change(ctx context.Context, c caller, id uuid.UUID, act, idem string,
	forms func(cur Campaign) (store.Event, func(), error)) (Campaign, error) {
	if idem == "" {
		return Campaign{}, problem.IdempotencyRequired
	}
	key := "cnts_campaign:" + act + ":" + id.String() + ":" + idem
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, c.p.TenantID, id); err != nil {
			return err
		}
		if prev, err := store.EventByIdem(ctx, tx, key); err != nil || prev != nil {
			return err
		}
		v, err := store.Latest(ctx, tx, id)
		if err != nil {
			return err
		}
		if v == nil || !c.sees(*v) {
			return ErrNotFound
		}
		es, err := store.Events(ctx, tx, []uuid.UUID{id})
		if err != nil {
			return err
		}
		cur := build(*v, es[id])
		if !domain.CanApply(cur.State, act) {
			return ErrState.WithDetail("đợt đang ở trạng thái " + stateLabel(cur.State))
		}
		ev, undo, err := forms(cur)
		if err != nil {
			return err
		}
		ev.ID, ev.CampaignID, ev.Action, ev.ActorRef, ev.EventTime = newID(), id, act, c.p.ActorRef, s.now()
		if err := store.InsertEvent(ctx, tx, c.p.TenantID, &ev, key); err != nil {
			if undo != nil {
				undo()
			}
			return err
		}
		if act == domain.ActReminded && ev.Reminded != nil && *ev.Reminded > 0 {
			return s.emitReminders(ctx, tx, c, cur, ev.EventTime)
		}
		return nil
	})
	if err != nil {
		return Campaign{}, classify(err)
	}
	return s.get(ctx, c, id)
}

func stateLabel(st string) string {
	switch st {
	case domain.StateCollecting:
		return "Đang thu thập"
	case domain.StateClosed:
		return "Đã đóng"
	}
	return "Nháp"
}

// Publish "Phát hành" (B2): một kỳ native kn_cnts loại `adhoc` cho các xã của đợt; đợt chuyển Đang thu thập.
func (s *Service) Publish(ctx context.Context, id uuid.UUID, idem string) (Campaign, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Campaign{}, err
	}
	return s.change(ctx, c, id, domain.ActPublished, idem, func(cur Campaign) (store.Event, func(), error) {
		v := cur.Version
		if !v.DueAt.After(s.now()) {
			return store.Event{}, nil, problem.Validation.WithField("due_at", "hạn nộp đã qua — sửa nháp trước khi phát hành")
		}
		ref, err := s.Forms.LatestTemplate(ctx, domain.Profile, v.TemplateCode)
		if err != nil {
			if isNotFound(err) {
				return store.Event{}, nil, ErrTemplate.WithField("template_code", v.TemplateCode)
			}
			return store.Event{}, nil, err
		}
		key := domain.PeriodKey(v.CampaignID, v.From)
		zone := clock.Zone()
		from := time.Date(v.From.Year(), v.From.Month(), v.From.Day(), 0, 0, 0, 0, zone)
		to := time.Date(v.To.Year(), v.To.Month(), v.To.Day(), 0, 0, 0, 0, zone)
		pub, err := s.Forms.PublishBatch(ctx, formsport.BatchInput{Profile: domain.Profile, TemplateID: ref.ID, UnitKind: formsport.UnitCommune,
			Units: v.Units, Periods: []formsport.PeriodDef{{Label: v.Name, DueAt: v.DueAt, Kind: domain.PeriodKind, Key: key, From: from, To: to,
				Rule: domain.PeriodRule}}}, "cnts_campaign:"+v.CampaignID.String())
		if err != nil {
			return store.Event{}, nil, err
		}
		if len(pub) != 1 {
			return store.Event{}, nil, problem.Internal.WithDetail("cap/forms trả số kỳ khác 1")
		}
		pid := pub[0].ID
		seq, tv, due := v.Seq, ref.Version, v.DueAt
		undo := func() {
			_ = s.Control.CancelPeriod(ctx, domain.Profile, pid, "Hủy tự động: ghi phát hành đợt điều tra không thành công",
				"cnts_campaign:"+v.CampaignID.String()+":compensate")
		}
		return store.Event{Version: &seq, FormsPeriodRef: &pid, PeriodKey: &key, TemplateVersion: &tv, DueAt: &due}, undo, nil
	})
}

func isNotFound(err error) bool {
	var p *problem.Problem
	return errors.As(err, &p) && p.Code == formsport.ErrTemplateNotFound.Code
}

// Extend "Gia hạn": hạn nộp mới (sau hạn hiện hành), lý do bắt buộc (UR-TINH-26).
func (s *Service) Extend(ctx context.Context, id uuid.UUID, due time.Time, reason, idem string) (Campaign, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Campaign{}, err
	}
	why, ok := domain.Reason(reason)
	if !ok {
		return Campaign{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	return s.change(ctx, c, id, domain.ActExtended, idem, func(cur Campaign) (store.Event, func(), error) {
		if !due.After(cur.DueAt) {
			return store.Event{}, nil, problem.Validation.WithField("due_at", "hạn mới phải sau hạn hiện hành")
		}
		if err := s.Control.ChangeDue(ctx, domain.Profile, *cur.Period.FormsPeriodRef, due, why, "cnts_campaign:extend:"+idem); err != nil {
			return store.Event{}, nil, err
		}
		return store.Event{DueAt: &due, Reason: &why}, nil, nil
	})
}

// Close "Đóng đợt" (B5): chốt kỳ native (ảnh chụp đóng băng) — xã chưa gửi chuyển "Không nộp", không gửi được nữa.
func (s *Service) Close(ctx context.Context, id uuid.UUID, idem string) (Campaign, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Campaign{}, err
	}
	return s.change(ctx, c, id, domain.ActClosed, idem, func(cur Campaign) (store.Event, func(), error) {
		if err := s.Control.ClosePeriod(ctx, domain.Profile, *cur.Period.FormsPeriodRef, "cnts_campaign:close:"+idem); err != nil {
			return store.Event{}, nil, err
		}
		return store.Event{}, nil, nil
	})
}

// Reopen "Mở lại đợt": chỉ Lãnh đạo phòng (quyền mo_lai_ky), lý do và thời hạn (hạn nộp mới) bắt buộc (FN-21 BR-01).
func (s *Service) Reopen(ctx context.Context, id uuid.UUID, until time.Time, reason, idem string) (Campaign, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Campaign{}, err
	}
	if !c.k.Can(extport.SessionPowerReopenPeriod) {
		return Campaign{}, errNotLeader
	}
	ro, ok := s.Control.(Reopener)
	if !ok {
		return Campaign{}, ErrDependency.WithDetail("cap/forms chưa có thao tác mở lại kỳ cho module nghiệp vụ")
	}
	why, ok := domain.Reason(reason)
	if !ok {
		return Campaign{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	if !until.After(s.now()) {
		return Campaign{}, problem.Validation.WithField("due_at", "thời hạn mở lại phải sau hiện tại")
	}
	return s.change(ctx, c, id, domain.ActReopened, idem, func(cur Campaign) (store.Event, func(), error) {
		pid := *cur.Period.FormsPeriodRef
		if err := ro.ReopenPeriod(ctx, domain.Profile, pid, why, "cnts_campaign:reopen:"+idem); err != nil {
			return store.Event{}, nil, err
		}
		if err := s.Control.ChangeDue(ctx, domain.Profile, pid, until, why, "cnts_campaign:reopen-due:"+idem); err != nil {
			return store.Event{}, nil, err
		}
		return store.Event{DueAt: &until, Reason: &why}, nil, nil
	})
}

// Remind "Nhắc xã chưa nộp" (B4): một thông báo cho cán bộ xã của mỗi xã chưa gửi (sự kiện nhắc nộp biểu CNTS — pf/notify
// gửi tới cán bộ xã của xã khi không có người nhận cụ thể).
func (s *Service) Remind(ctx context.Context, id uuid.UUID, idem string) (Campaign, int, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Campaign{}, 0, err
	}
	n := 0
	out, err := s.change(ctx, c, id, domain.ActReminded, idem, func(cur Campaign) (store.Event, func(), error) {
		p, err := s.Control.Progress(ctx, domain.Profile, *cur.Period.FormsPeriodRef)
		if err != nil {
			return store.Event{}, nil, err
		}
		for _, u := range p.Units {
			if pending(u) {
				n++
			}
		}
		if n == 0 {
			return store.Event{}, nil, problem.Validation.WithDetail("mọi xã đã gửi — không có xã cần nhắc")
		}
		return store.Event{Reminded: &n}, nil, nil
	})
	return out, n, err
}

func pending(u formsport.UnitProgress) bool {
	return u.Versions == 0 && (u.Status == "not_submitted" || u.Status == "overdue")
}

// emitReminders ghi sự kiện nhắc nộp (outbox, cùng giao dịch với sự kiện reminded của đợt).
func (s *Service) emitReminders(ctx context.Context, tx pgx.Tx, c caller, cur Campaign, now time.Time) error {
	p, err := s.Control.Progress(ctx, domain.Profile, *cur.Period.FormsPeriodRef)
	if err != nil {
		return err
	}
	slot := extport.CNTSReminderBefore
	if !now.Before(cur.DueAt) {
		slot = extport.CNTSReminderOverdue
	}
	loc := clock.Zone()
	msg := "Đợt điều tra \"" + cur.Version.Name + "\": xã chưa gửi biểu — hạn nộp " + cur.DueAt.In(loc).Format("15:04 02/01/2006")
	for _, u := range p.Units {
		if !pending(u) {
			continue
		}
		pl := extport.CNTSFormRemindedV1{PeriodRef: p.ID, TemplateCode: p.TemplateCode, PeriodLabel: p.Label, Unit: u.Unit, CommuneCode: u.Parent,
			Slot: slot, DueAt: cur.DueAt.UTC().Format(time.RFC3339), Message: msg, Channels: []string{}, RecipientRefs: []uuid.UUID{},
			Department: domain.Department, EventTime: now.UTC().Format(time.RFC3339)}
		if pl.CommuneCode == "" {
			pl.CommuneCode = u.Unit
		}
		if err := outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: c.p.TenantID, Type: extport.EventCNTSFormReminded, SchemaVersion: 1,
			OccurredAt: now, Producer: extport.EventProducerName, Payload: pl}); err != nil {
			return err
		}
	}
	return nil
}
