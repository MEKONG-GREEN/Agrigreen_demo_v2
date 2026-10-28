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

// DefaultLease là hạn ý định thao tác của đợt: đủ cho các lời gọi cap/forms của một yêu cầu HTTP; tiến trình chết giữa
// chừng → hết hạn thì thao tác khác (và sửa nháp) chạy được.
const DefaultLease = 2 * time.Minute

// maxLeaseMargin: biên an toàn tối đa giữa hạn lời gọi cap/forms và hạn ý định.
const maxLeaseMargin = 15 * time.Second

// cleanupTimeout: hạn của bù ở cap/forms và bỏ ý định sau lỗi (chạy cả khi yêu cầu đã hủy).
const cleanupTimeout = 30 * time.Second

func (s *Service) lease() time.Duration {
	if s.Lease >= time.Second {
		return s.Lease.Truncate(time.Second) // hạn DB tính bằng giây nguyên
	}
	return DefaultLease
}

// step là kết quả lời gọi cap/forms (chạy NGOÀI giao dịch) của một thao tác.
type step struct {
	ev store.Event
	// undo bù ở cap/forms khi ghi sự kiện đợt lỗi (phát hành: hủy kỳ native vừa tạo). nil → không bù.
	undo func(context.Context)
	// emit ghi thêm trong giao dịch ghi sự kiện (outbox nhắc nộp). nil → không.
	emit func(ctx context.Context, tx pgx.Tx, at time.Time) error
}

// current đọc trạng thái hiện tại của đợt trong giao dịch (ErrNotFound nếu không có / người gọi không thấy).
func (s *Service) current(ctx context.Context, tx pgx.Tx, c caller, id uuid.UUID) (Campaign, error) {
	v, err := store.Latest(ctx, tx, id)
	if err != nil {
		return Campaign{}, err
	}
	if v == nil || !c.sees(*v) {
		return Campaign{}, ErrNotFound
	}
	es, err := store.Events(ctx, tx, []uuid.UUID{id})
	if err != nil {
		return Campaign{}, err
	}
	// Đợt đã phát hành: phiên bản mới nhất là phiên bản đã phát hành (trigger chặn phiên bản sau phát hành).
	return build(*v, es[id]), nil
}

// change chạy một thao tác chuyển trạng thái (BF-CNTS-CAMPAIGN-POOL, #156) KHÔNG giữ giao dịch DB quanh lời gọi cap/forms
// (cap/forms mở giao dịch riêng trên CÙNG pool — giữ kết nối trong khi chờ kết nối thứ hai làm cạn pool):
//  1. giao dịch ngắn giữ khóa tư vấn của đợt: gọi lặp cùng khóa idempotent → trả đợt hiện tại; kiểm thấy đợt, trạng thái;
//     ghi ý định thao tác (cnts_campaign_intent, hạn giờ DB) — ý định khác còn hạn → 409 ErrBusy; commit;
//  2. call: cap/forms NGOÀI giao dịch, hạn lời gọi = thời điểm TRƯỚC giao dịch 1 + hạn ý định − biên;
//  3. giao dịch ngắn giữ khóa: bỏ ý định của token (hết hạn → nhận lại nếu không ai giữ; người khác giữ → ErrInterrupted,
//     không ghi gì), kiểm lại trạng thái, phiên bản, số sự kiện; ghi sự kiện đợt (+ emit) cùng giao dịch.
//
// Lỗi ở 2 → bỏ ý định. Lỗi ở 3 → bù ở cap/forms (undo) rồi bỏ ý định. Trong lúc ý định còn hạn, trigger chặn sửa nháp và
// mọi sự kiện đợt khác — thay khóa giữ suốt của bản cũ.
func (s *Service) change(ctx context.Context, c caller, id uuid.UUID, act, idem string,
	call func(ctx context.Context, cur Campaign) (step, error)) (Campaign, error) {
	if idem == "" {
		return Campaign{}, problem.IdempotencyRequired
	}
	key := "cnts_campaign:" + act + ":" + id.String() + ":" + idem
	lease := s.lease()
	start := time.Now()
	token := newID()
	var cur Campaign
	done := false
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, c.p.TenantID, id); err != nil {
			return err
		}
		if prev, err := store.EventByIdem(ctx, tx, key); err != nil || prev != nil {
			done = prev != nil
			return err
		}
		var err error
		if cur, err = s.current(ctx, tx, c, id); err != nil {
			return err
		}
		if !domain.CanApply(cur.State, act) {
			return ErrState.WithDetail("đợt đang ở trạng thái " + stateLabel(cur.State))
		}
		claimed, err := store.Claim(ctx, tx, c.p.TenantID, id, token, act, lease)
		if err != nil {
			return err
		}
		if !claimed {
			return ErrBusy
		}
		return nil
	})
	if err != nil {
		return Campaign{}, classify(err)
	}
	if done {
		return s.get(ctx, c, id)
	}

	callCtx, cancel := context.WithDeadline(ctx, start.Add(lease-min(lease/4, maxLeaseMargin)))
	st, err := call(callCtx, cur)
	expired := errors.Is(callCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	cancel()
	if err != nil {
		s.release(ctx, c, id, token)
		if expired {
			return Campaign{}, ErrDependency.WithDetail("cap/forms không phản hồi kịp hạn thao tác — thử lại").WithCause(err)
		}
		return Campaign{}, err
	}

	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, c.p.TenantID, id); err != nil {
			return err
		}
		released, err := store.Release(ctx, tx, c.p.TenantID, id, token, true)
		if err != nil {
			return err
		}
		if !released {
			// Ý định của token đã hết hạn trước khi ghi (giao dịch này chờ lâu): nhận lại nếu không ai đang giữ — kiểm đợt
			// không đổi ngay dưới đây (ai đó đã chen vào thì trạng thái/phiên bản/sự kiện đã khác → ErrState, bù).
			// Người khác đang giữ ý định còn hạn → ErrInterrupted, không ghi gì.
			reclaimed, err := store.Claim(ctx, tx, c.p.TenantID, id, token, act, lease)
			if err != nil {
				return err
			}
			if !reclaimed {
				return ErrInterrupted
			}
			if _, err := store.Release(ctx, tx, c.p.TenantID, id, token, true); err != nil {
				return err
			}
		}
		now, err := s.current(ctx, tx, c, id)
		if err != nil {
			return err
		}
		// Phòng thủ: ý định giữ suốt nên không ai đổi được đợt; vẫn kiểm trước khi ghi.
		if !domain.CanApply(now.State, act) || now.Version.Seq != cur.Version.Seq || len(now.Events) != len(cur.Events) {
			return ErrState.WithDetail("đợt vừa đổi trong lúc thao tác — tải lại rồi thử lại")
		}
		ev := st.ev
		ev.ID, ev.CampaignID, ev.Action, ev.ActorRef, ev.EventTime = newID(), id, act, c.p.ActorRef, s.now()
		if err := store.InsertEvent(ctx, tx, c.p.TenantID, &ev, key); err != nil {
			return err
		}
		if st.emit != nil {
			return st.emit(ctx, tx, ev.EventTime)
		}
		return nil
	})
	if err != nil {
		if st.undo != nil {
			uctx, ucancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
			st.undo(uctx)
			ucancel()
		}
		s.release(ctx, c, id, token)
		return Campaign{}, classify(err)
	}
	return s.get(ctx, c, id)
}

// release bỏ ý định của token sau lỗi (giao dịch riêng, chạy cả khi yêu cầu đã hủy). Token không còn giữ ý định → không có
// gì để bỏ. Lỗi bỏ qua: ý định tự hết hạn.
func (s *Service) release(ctx context.Context, c caller, id, token uuid.UUID) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	_ = s.tx(rctx, c.p.TenantID, func(tx pgx.Tx) error {
		_, err := store.Release(rctx, tx, c.p.TenantID, id, token, false)
		return err
	})
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
	return s.change(ctx, c, id, domain.ActPublished, idem, func(ctx context.Context, cur Campaign) (step, error) {
		v := cur.Version
		if !v.DueAt.After(s.now()) {
			return step{}, problem.Validation.WithField("due_at", "hạn nộp đã qua — sửa nháp trước khi phát hành")
		}
		ref, err := s.Forms.LatestTemplate(ctx, domain.Profile, v.TemplateCode)
		if err != nil {
			if isNotFound(err) {
				return step{}, ErrTemplate.WithField("template_code", v.TemplateCode)
			}
			return step{}, err
		}
		key := domain.PeriodKey(v.CampaignID, v.From)
		zone := clock.Zone()
		from := time.Date(v.From.Year(), v.From.Month(), v.From.Day(), 0, 0, 0, 0, zone)
		to := time.Date(v.To.Year(), v.To.Month(), v.To.Day(), 0, 0, 0, 0, zone)
		pub, err := s.Forms.PublishBatch(ctx, formsport.BatchInput{Profile: domain.Profile, TemplateID: ref.ID, UnitKind: formsport.UnitCommune,
			Units: v.Units, Periods: []formsport.PeriodDef{{Label: v.Name, DueAt: v.DueAt, Kind: domain.PeriodKind, Key: key, From: from, To: to,
				Rule: domain.PeriodRule}}}, "cnts_campaign:"+v.CampaignID.String())
		if err != nil {
			return step{}, err
		}
		if len(pub) != 1 {
			return step{}, problem.Internal.WithDetail("cap/forms trả số kỳ khác 1")
		}
		pid := pub[0].ID
		seq, tv, due := v.Seq, ref.Version, v.DueAt
		// Bù: khóa idempotent của PublishBatch cố định theo đợt nên kỳ native này chỉ của lần phát hành này — hủy an toàn
		// kể cả khi ý định đã mất.
		undo := func(ctx context.Context) {
			_ = s.Control.CancelPeriod(ctx, domain.Profile, pid, "Hủy tự động: ghi phát hành đợt điều tra không thành công",
				"cnts_campaign:"+v.CampaignID.String()+":compensate")
		}
		return step{ev: store.Event{Version: &seq, FormsPeriodRef: &pid, PeriodKey: &key, TemplateVersion: &tv, DueAt: &due}, undo: undo}, nil
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
	return s.change(ctx, c, id, domain.ActExtended, idem, func(ctx context.Context, cur Campaign) (step, error) {
		if !due.After(cur.DueAt) {
			return step{}, problem.Validation.WithField("due_at", "hạn mới phải sau hạn hiện hành")
		}
		if err := s.Control.ChangeDue(ctx, domain.Profile, *cur.Period.FormsPeriodRef, due, why, "cnts_campaign:extend:"+idem); err != nil {
			return step{}, err
		}
		return step{ev: store.Event{DueAt: &due, Reason: &why}}, nil
	})
}

// Close "Đóng đợt" (B5): chốt kỳ native (ảnh chụp đóng băng) — xã chưa gửi chuyển "Không nộp", không gửi được nữa.
func (s *Service) Close(ctx context.Context, id uuid.UUID, idem string) (Campaign, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Campaign{}, err
	}
	return s.change(ctx, c, id, domain.ActClosed, idem, func(ctx context.Context, cur Campaign) (step, error) {
		if err := s.Control.ClosePeriod(ctx, domain.Profile, *cur.Period.FormsPeriodRef, "cnts_campaign:close:"+idem); err != nil {
			return step{}, err
		}
		return step{}, nil
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
	return s.change(ctx, c, id, domain.ActReopened, idem, func(ctx context.Context, cur Campaign) (step, error) {
		pid := *cur.Period.FormsPeriodRef
		if err := ro.ReopenPeriod(ctx, domain.Profile, pid, why, "cnts_campaign:reopen:"+idem); err != nil {
			return step{}, err
		}
		if err := s.Control.ChangeDue(ctx, domain.Profile, pid, until, why, "cnts_campaign:reopen-due:"+idem); err != nil {
			return step{}, err
		}
		return step{ev: store.Event{DueAt: &until, Reason: &why}}, nil
	})
}

// Remind "Nhắc xã chưa nộp" (B4): một thông báo cho cán bộ xã của mỗi xã chưa gửi (sự kiện nhắc nộp biểu CNTS — pf/notify
// gửi tới cán bộ xã của xã khi không có người nhận cụ thể). Tiến độ đọc MỘT lần ngoài giao dịch; sự kiện "reminded" và các
// sự kiện nhắc (outbox) ghi cùng giao dịch theo đúng ảnh tiến độ đó.
func (s *Service) Remind(ctx context.Context, id uuid.UUID, idem string) (Campaign, int, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Campaign{}, 0, err
	}
	n := 0
	out, err := s.change(ctx, c, id, domain.ActReminded, idem, func(ctx context.Context, cur Campaign) (step, error) {
		p, err := s.Control.Progress(ctx, domain.Profile, *cur.Period.FormsPeriodRef)
		if err != nil {
			return step{}, err
		}
		for _, u := range p.Units {
			if pending(u) {
				n++
			}
		}
		if n == 0 {
			return step{}, problem.Validation.WithDetail("mọi xã đã gửi — không có xã cần nhắc")
		}
		emit := func(ctx context.Context, tx pgx.Tx, at time.Time) error {
			return s.emitReminders(ctx, tx, c, cur, p, at)
		}
		return step{ev: store.Event{Reminded: &n}, emit: emit}, nil
	})
	return out, n, err
}

func pending(u formsport.UnitProgress) bool {
	return u.Versions == 0 && (u.Status == "not_submitted" || u.Status == "overdue")
}

// emitReminders ghi sự kiện nhắc nộp (outbox, cùng giao dịch với sự kiện reminded của đợt) theo tiến độ p đã đọc.
func (s *Service) emitReminders(ctx context.Context, tx pgx.Tx, c caller, cur Campaign, p formsport.PeriodProgress, now time.Time) error {
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
