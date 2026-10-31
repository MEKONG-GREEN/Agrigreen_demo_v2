package app

// Chi tiết kỳ, bảng tiến độ phiếu + nguồn (B6), sửa hạn, đóng kỳ, hủy kỳ (B7), phiếu cơ sở ở cap/forms.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// FormRow là một dòng bảng "Tiến độ nộp phiếu" (một phòng).
type FormRow struct {
	Department      string
	DepartmentName  string
	View            domain.FormView
	LastSubmittedAt *time.Time
	ReviewedAt      *time.Time
	ReviewNote      string
	Late            bool
	NativeLate      *bool
	RequestDueAt    *time.Time
	RequestState    string
}

// SourceRow là một dòng bảng "Nguồn module".
type SourceRow struct {
	Ref    extport.KHTCSourceRef
	Known  bool // đọc được trạng thái chốt từ bộ quy đổi
	Closed bool
	At     *time.Time
}

// Counts là 4 thẻ đếm của chi tiết kỳ.
type Counts struct {
	Forms, FormsSent, Sources, SourcesClosed int
}

// Detail là kỳ KHTC kèm trạng thái chiếu, tiến độ phiếu và nguồn, sự kiện.
type Detail struct {
	Period      store.Period
	State       string
	DueAt       time.Time // hạn nộp hiệu lực (sau "Sửa hạn nộp")
	ExportDueAt time.Time
	Overdue     bool
	Counts      Counts
	Forms       []FormRow
	Sources     []SourceRow
	Events      []store.Event
	FormsStatus string // trạng thái kỳ native ("open", "closed", "cancelled"…); "" = không đọc được
	// CanClose/CloseForced/CanCancel: nút "Đóng kỳ" (thường/ngoại lệ có lý do), "Hủy kỳ" được bật.
	CanClose, CloseForced, CanCancel bool
	CloseLateDays                    int
}

func terminalOf(evs []store.Event) string {
	for _, e := range evs {
		switch e.Action {
		case store.ActionClosed:
			return domain.PeriodClosed
		case store.ActionCancelled:
			return domain.PeriodCancelled
		}
	}
	return ""
}

func deadlinesOf(p store.Period, evs []store.Event) (time.Time, time.Time) {
	due, exp := p.DueAt, p.ExportDueAt
	for _, e := range evs {
		if e.Action == store.ActionDeadline && e.DueAt != nil && e.ExportDueAt != nil {
			due, exp = *e.DueAt, *e.ExportDueAt
		}
	}
	return due, exp
}

// project dựng Detail từ sổ + tiến độ kỳ native + trạng thái nguồn.
func (s *Service) project(ctx context.Context, c caller, p store.Period, evs []store.Event, srcs []store.Source, names map[string]string, requests map[string]*store.AdhocRequest) Detail {
	now := s.now()
	d := Detail{Period: p, Events: evs}
	d.DueAt, d.ExportDueAt = deadlinesOf(p, evs)
	d.Overdue = now.After(d.DueAt)
	term := terminalOf(evs)
	prog, perr := s.Control.Progress(ctx, domain.ProfileKHTC, p.FormsPeriodRef)
	byUnit := map[string]formsport.UnitProgress{}
	if perr == nil {
		d.FormsStatus = prog.Status
		for _, u := range prog.Units {
			byUnit[u.Unit] = u
		}
	}
	f := domain.Facts{Forms: len(p.Departments), Overdue: d.Overdue}
	var drafts map[string]int64
	if s.Drafts != nil && term == "" {
		drafts, _ = s.Drafts(ctx, p.ID) // lỗi đọc nháp không chặn bảng tiến độ (chỉ mất nhãn "Lưu nháp")
	}
	for _, dept := range p.Departments {
		row := FormRow{Department: dept, DepartmentName: names[dept]}
		u, ok := byUnit[dept]
		switch {
		case !ok:
			row.View = domain.FormView{State: domain.FormUnknown}
		default:
			n, hasDraft := drafts[dept]
			overdue := d.Overdue
			if request := requests[dept]; request != nil {
				overdue = now.After(request.DueAt)
			}
			row.View = domain.FormStateOf(domain.FormFacts{Versions: u.Versions, Decision: u.Decision, Overdue: overdue, Terminal: term,
				Max: p.MaxSubmissions, Draft: hasDraft && n == u.Versions})
			row.LastSubmittedAt, row.ReviewedAt, row.ReviewNote, row.Late = u.LastSubmittedAt, u.ReviewedAt, u.ReviewNote, u.Late
		}
		projectAdhocRequest(&row, requests[dept], u, term)
		if u.Versions > 0 {
			f.AnySent = true
		}
		if row.View.CountsAsSent {
			f.FormsSent++
		}
		d.Forms = append(d.Forms, row)
	}
	refs := make([]extport.KHTCSourceRef, 0, len(srcs))
	for _, sr := range srcs {
		ref := extport.KHTCSourceRef{Department: sr.Department, Source: sr.Source, CalendarID: sr.CalendarID, CalendarVersion: sr.CalendarVersion,
			NativeKey: sr.NativeKey}
		if sr.Label != nil {
			ref.Label = *sr.Label
		}
		refs = append(refs, ref)
		d.Sources = append(d.Sources, SourceRow{Ref: ref})
	}
	if s.Crosswalk != nil && len(refs) > 0 {
		if states, err := s.Crosswalk.States(ctx, refs); err == nil && len(states) == len(refs) {
			for i, st := range states {
				d.Sources[i].Known, d.Sources[i].Closed, d.Sources[i].At = true, st.Closed, st.ClosedAt
			}
		}
	}
	f.Sources = len(d.Sources)
	for _, sr := range d.Sources {
		if sr.Closed {
			f.SourcesClosed++
		}
	}
	d.State = domain.PeriodStateOf(term, f)
	d.Counts = Counts{Forms: f.Forms, FormsSent: f.FormsSent, Sources: f.Sources, SourcesClosed: f.SourcesClosed}
	if c.k.Role != "" && !c.k.ReadOnly() {
		d.CloseLateDays = s.closeLateDays(ctx, now)
		if dc, err := domain.CanClose(d.State, d.DueAt, now, d.CloseLateDays, "đóng kỳ — kiểm tra nút"); err == nil {
			d.CanClose, d.CloseForced = true, dc.Forced
		}
		_, err := domain.CanCancel(d.State, false, "hủy kỳ — kiểm tra nút")
		d.CanCancel = err == nil
		if s.Reports != nil {
			final, e := s.Reports.HasFinal(ctx, p.ID.String())
			d.CanCancel = d.CanCancel && e == nil && !final
		}
	}
	return d
}

func (s *Service) load(ctx context.Context, c caller, id uuid.UUID) (store.Period, []store.Event, []store.Source, error) {
	var p *store.Period
	var evs map[uuid.UUID][]store.Event
	var srcs []store.Source
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if p, e = store.PeriodByID(ctx, tx, id); e != nil || p == nil {
			return e
		}
		if evs, e = store.Events(ctx, tx, []uuid.UUID{id}); e != nil {
			return e
		}
		srcs, e = store.Sources(ctx, tx, id)
		return e
	})
	if err != nil {
		return store.Period{}, nil, nil, err
	}
	if p == nil {
		return store.Period{}, nil, nil, ErrNotFound
	}
	return *p, evs[id], srcs, nil
}

// Get đọc chi tiết kỳ (B6).
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Detail, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return Detail{}, err
	}
	p, evs, srcs, err := s.load(ctx, c, id)
	if err != nil {
		return Detail{}, err
	}
	if p.Type == domain.TypeAdhoc && !adhocReader(c) {
		return Detail{}, problem.Forbidden
	}
	requests, err := s.activeRequests(ctx, c, p)
	if err != nil {
		return Detail{}, err
	}
	names, _, _ := s.unitDepartments(ctx, c.k)
	return s.project(ctx, c, p, evs, srcs, names, requests), nil
}

// List liệt kê kỳ (năm kế hoạch year, 0 = mọi năm), mới trước, kèm trạng thái và tiến độ nộp.
func (s *Service) List(ctx context.Context, year, limit int) ([]Detail, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var ps []store.Period
	var evs map[uuid.UUID][]store.Event
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if ps, e = store.PeriodsForReader(ctx, tx, year, limit, adhocReader(c)); e != nil {
			return e
		}
		idl := make([]uuid.UUID, 0, len(ps))
		for _, p := range ps {
			idl = append(idl, p.ID)
		}
		evs, e = store.Events(ctx, tx, idl)
		return e
	})
	if err != nil {
		return nil, err
	}
	names, _, _ := s.unitDepartments(ctx, c.k)
	out := make([]Detail, 0, len(ps))
	for _, p := range ps {
		requests, err := s.activeRequests(ctx, c, p)
		if err != nil {
			return nil, err
		}
		d := s.project(ctx, c, p, evs[p.ID], nil, names, requests)
		d.Sources = nil
		out = append(out, d)
	}
	return out, nil
}

// DeadlineInput là "Sửa hạn nộp": hạn nộp, hạn xuất mới, lý do (tùy chọn).
type DeadlineInput struct {
	DueAt, ExportDueAt time.Time
	Reason             string
}

func (s *Service) appendEvent(ctx context.Context, c caller, e store.Event, idem string) error {
	e.ID, e.ActorRef, e.EventTime = ids.New(), c.p.ActorRef, s.now()
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { return store.InsertEvent(ctx, tx, c.p.TenantID, &e, idem) })
	return classify(err)
}

func eventIdem(idem, action string, id uuid.UUID) string {
	if idem == "" {
		idem = ids.New().String()
	}
	return "khtc:" + action + ":" + id.String() + ":" + idem
}

// ChangeDeadline "Sửa hạn nộp" — chỉ kỳ chưa đóng/hủy. Đổi hạn của kỳ native cap/forms trước (sổ phụ period_due: cờ
// "Nộp trễ" của lần gửi, Trễ hạn, hạn trên thiết bị theo hạn mới), rồi ghi sự kiện ở sổ KHTC; ghi sổ KHTC lỗi → trả hạn
// native về hạn cũ (bù như "Mở kỳ").
func (s *Service) ChangeDeadline(ctx context.Context, id uuid.UUID, in DeadlineInput, idem string) (Detail, error) {
	c, err := s.periodWriter(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	d, err := s.Get(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	if domain.CanChangeDeadline(d.State) != nil {
		return Detail{}, ErrTerminal
	}
	if in.DueAt.IsZero() {
		return Detail{}, problem.Validation.WithField("due_at", "bắt buộc")
	}
	if in.ExportDueAt.IsZero() {
		in.ExportDueAt = d.ExportDueAt
		if in.ExportDueAt.Before(in.DueAt) {
			in.ExportDueAt = in.DueAt
		}
	}
	if err := domain.ValidateDeadlines(d.Period.Cutoff, in.DueAt, in.ExportDueAt, zone()); err != nil {
		return Detail{}, inputErr(err)
	}
	e := store.Event{PeriodID: id, Action: store.ActionDeadline, DueAt: ptrTime(in.DueAt.UTC()), ExportDueAt: ptrTime(in.ExportDueAt.UTC())}
	if why, ok := domain.ValidReason(in.Reason); ok {
		e.Reason = &why
	}
	reason := ""
	if e.Reason != nil {
		reason = *e.Reason
	}
	nativeKey := eventIdem(idem, "forms-due", id)
	nativeCtx := ctx
	if d.Period.Type == domain.TypeAdhoc {
		hash, e := formsport.OperationHash(struct {
			Due    time.Time
			Reason string
		}{in.DueAt.UTC(), strings.TrimSpace(reason)})
		if e != nil {
			return Detail{}, e
		}
		nativeCtx = BindAdhocOperation(ctx, s.DB, c.k, id, formsport.Operation{Action: formsport.OperationChangeDue, Profile: domain.ProfileKHTC, PeriodID: d.Period.FormsPeriodRef, PayloadHash: hash, IdemKey: nativeKey})
	}
	if err := s.Control.ChangeDue(nativeCtx, domain.ProfileKHTC, d.Period.FormsPeriodRef, in.DueAt, reason, nativeKey); err != nil {
		return Detail{}, err
	}
	if err := s.appendEvent(ctx, c, e, eventIdem(idem, "deadline", id)); err != nil {
		// Bù: trả hạn kỳ native về hạn hiệu lực cũ (best effort — lỗi bù không che lỗi gốc).
		undoReason := "Hoàn tác: ghi sổ KHTC không thành công"
		undoKey := eventIdem(idem, "forms-due-undo", id)
		undoCtx := ctx
		if d.Period.Type == domain.TypeAdhoc {
			hash, _ := formsport.OperationHash(struct {
				Due    time.Time
				Reason string
			}{d.DueAt.UTC(), undoReason})
			undoCtx = BindAdhocOperation(ctx, s.DB, c.k, id, formsport.Operation{Action: formsport.OperationChangeDue, Profile: domain.ProfileKHTC, PeriodID: d.Period.FormsPeriodRef, PayloadHash: hash, IdemKey: undoKey})
		}
		_ = s.Control.ChangeDue(undoCtx, domain.ProfileKHTC, d.Period.FormsPeriodRef, d.DueAt, undoReason, undoKey)
		return Detail{}, err
	}
	return s.Get(ctx, id)
}

func ptrTime(t time.Time) *time.Time { return &t }

// Close "Đóng kỳ" (BR-DL-15): chốt kỳ native (phiếu chưa gửi → Không gửi, đã gửi/yêu cầu bổ sung → Đã khóa, ảnh chụp đóng
// băng) rồi ghi sự kiện đóng. Gọi lại sau lỗi giữa chừng: kỳ native đã chốt → chỉ ghi sự kiện.
func (s *Service) Close(ctx context.Context, id uuid.UUID, reason, idem string) (Detail, error) {
	c, err := s.periodWriter(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	d, err := s.Get(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	dc, err := domain.CanClose(d.State, d.DueAt, s.now(), s.closeLateDays(ctx, s.now()), reason)
	switch {
	case errors.Is(err, domain.ErrTerminal):
		return Detail{}, ErrTerminal
	case errors.Is(err, domain.ErrReasonRequired):
		return Detail{}, problem.Validation.WithField("reason", "đóng kỳ khi còn thiếu dữ liệu: lý do bắt buộc, 10–500 ký tự")
	case err != nil:
		return Detail{}, ErrNotClosable.WithDetail("kỳ đang " + domain.PeriodLabels[d.State] + ": " + err.Error())
	}
	if s.CancelGuard != nil {
		// Đang hủy kỳ (ý định còn hạn): không chốt kỳ native ở cap/forms — trigger cũng chặn sự kiện 'closed', kiểm trước để
		// không để lại kỳ native đã chốt mà sổ KHTC chưa đóng (review #143 N6).
		pending, err := s.CancelGuard.CancelPending(ctx, id)
		if err != nil {
			return Detail{}, ErrDependency.WithDetail("trạng thái hủy kỳ lỗi").WithCause(err)
		}
		if pending {
			return Detail{}, ErrCancelling
		}
	}
	nativeKey := eventIdem(idem, "forms-close", id)
	nativeCtx := ctx
	if d.Period.Type == domain.TypeAdhoc {
		nativeCtx = BindAdhocOperation(ctx, s.DB, c.k, id, formsport.Operation{Action: formsport.OperationClose, Profile: domain.ProfileKHTC, PeriodID: d.Period.FormsPeriodRef, IdemKey: nativeKey})
	}
	if err := s.Control.ClosePeriod(nativeCtx, domain.ProfileKHTC, d.Period.FormsPeriodRef, nativeKey); err != nil &&
		!errors.Is(err, formsport.ErrPeriodClosed) {
		return Detail{}, err
	}
	st := d.State
	e := store.Event{PeriodID: id, Action: store.ActionClosed, Forced: dc.Forced, State: &st}
	if dc.Reason != "" {
		why := dc.Reason
		e.Reason = &why
	}
	if err := s.appendEvent(ctx, c, e, eventIdem(idem, "close", id)); err != nil {
		return Detail{}, err
	}
	return s.Get(ctx, id)
}

// Cancel "Hủy kỳ" (BR-DL-18): kỳ Đã mở/Đang thu thập/Trễ hạn/Đủ dữ liệu, báo cáo chưa có Bản cuối, lý do bắt buộc. Hủy kỳ
// native (phiếu gỡ khỏi module các phòng, số liệu không vào ảnh chụp) rồi ghi sự kiện hủy. Kỳ đã hủy không tính khi
// kiểm mở trùng — mở lại đúng kỳ đó được.
//
// P0-POOL-SHARED-2 (#143, bàn giao từ #139): KHÔNG giữ giao dịch quanh lời gọi module khác. Đọc kỳ, Bản cuối và kiểm điều
// kiện ngoài giao dịch; CancelGuard (khtcreport) ghi Ý ĐỊNH HỦY bền trong giao dịch ngắn giữ khóa khtc_snapshot (trigger chặn
// Bản cuối/đóng kỳ trong lúc còn hạn), hủy kỳ native ở cap/forms NGOÀI giao dịch, rồi ghi sự kiện hủy + bỏ ý định trong
// giao dịch ngắn thứ hai. cap/forms lỗi → bỏ ý định, không ghi gì ở sổ KHTC.
func (s *Service) Cancel(ctx context.Context, id uuid.UUID, reason, idem string) (Detail, error) {
	c, err := s.periodWriter(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	d, err := s.Get(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	hasFinal := false
	if s.Reports != nil {
		if hasFinal, err = s.Reports.HasFinal(ctx, id.String()); err != nil {
			return Detail{}, ErrDependency.WithDetail("trạng thái bản báo cáo lỗi").WithCause(err)
		}
	}
	why, err := domain.CanCancel(d.State, hasFinal, reason)
	switch {
	case errors.Is(err, domain.ErrTerminal):
		return Detail{}, ErrTerminal
	case errors.Is(err, domain.ErrReasonRequired):
		return Detail{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	case err != nil:
		return Detail{}, ErrNotCancellable.WithDetail(err.Error())
	}
	var token uuid.UUID
	cctx := ctx // lời gọi cap/forms: có hạn trước khi ý định hủy hết hạn (review #143 C1)
	if s.CancelGuard != nil {
		var deadline time.Time
		token, deadline, err = s.CancelGuard.BeginCancel(ctx, id)
		if err == nil {
			var cancel context.CancelFunc
			cctx, cancel = context.WithDeadline(ctx, deadline)
			defer cancel()
		}
		switch {
		case errors.Is(err, domain.ErrHasFinal):
			return Detail{}, ErrNotCancellable.WithDetail(err.Error())
		case errors.Is(err, domain.ErrCancelling):
			return Detail{}, ErrCancelling
		case err != nil:
			return Detail{}, err
		}
	}
	abort := func(cause error) error {
		if s.CancelGuard == nil {
			return cause
		}
		actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if aerr := s.CancelGuard.AbortCancel(actx, id, token); aerr != nil {
			return errors.Join(cause, aerr)
		}
		return cause
	}
	if err := s.Control.CancelPeriod(cancelNativeContext(cctx, s.DB, c.k, d.Period, why, eventIdem(idem, "forms-cancel", id)), domain.ProfileKHTC, d.Period.FormsPeriodRef, why, eventIdem(idem, "forms-cancel", id)); err != nil &&
		!errors.Is(err, formsport.ErrPeriodCancelled) {
		if ctx.Err() == nil && cctx.Err() != nil {
			err = ErrDependency.WithDetail("hủy kỳ ở cap/forms chậm quá hạn giữ kỳ — thử lại").WithCause(err)
		}
		return Detail{}, abort(err)
	}
	st := d.State
	event := store.Event{PeriodID: id, Action: store.ActionCancelled, Reason: &why, State: &st}
	if s.CancelGuard != nil {
		event.ID, event.ActorRef, event.EventTime = ids.New(), c.p.ActorRef, s.now()
		write := func(tx pgx.Tx) error {
			return store.InsertEvent(ctx, tx, c.p.TenantID, &event, eventIdem(idem, "cancel", id))
		}
		err := s.CancelGuard.FinishCancel(ctx, id, token, write)
		if errors.Is(err, domain.ErrCancelLost) {
			return s.recoverLostCancel(ctx, id, d.Period.FormsPeriodRef, write)
		}
		if err != nil {
			return Detail{}, abort(classify(err))
		}
		return s.Get(ctx, id)
	}
	if err := s.appendEvent(ctx, c, event, eventIdem(idem, "cancel", id)); err != nil {
		return Detail{}, err
	}
	return s.Get(ctx, id)
}

// recoverLostCancel (review #143 C1): cap/forms ĐÃ hủy kỳ native nhưng ý định hủy đã hết hạn trước khi ghi sự kiện hủy
// (lời gọi/giao dịch chậm quá hạn). Bù: nhận ý định MỚI (BeginCancel kiểm Bản cuối dưới khóa khtc_snapshot) rồi ghi sự
// kiện hủy — nhất quán khi trong lúc đó chưa ai đánh dấu Bản cuối. Đã có Bản cuối (cap/forms hủy, KHTC có Bản cuối — không
// tự hoàn tác được vì hủy ở cap/forms là trạng thái cuối) → log lỗi extension.khtc.cancel_intent_lost để đối soát, trả
// 409 extension.khtc_period_cancel_interrupted. Lần hủy khác đang giữ ý định → 409 cancelling (lần đó sẽ ghi sự kiện).
func (s *Service) recoverLostCancel(ctx context.Context, id, formsRef uuid.UUID, write func(pgx.Tx) error) (Detail, error) {
	lg := slog.Default().With("event", "extension.khtc.cancel_intent_lost", "period_id", id, "forms_period_ref", formsRef)
	token, _, err := s.CancelGuard.BeginCancel(ctx, id)
	if err == nil {
		if err = s.CancelGuard.FinishCancel(ctx, id, token, write); err != nil {
			actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			err = errors.Join(err, s.CancelGuard.AbortCancel(actx, id, token))
		}
	}
	switch {
	case err == nil:
		lg.WarnContext(ctx, "hủy kỳ KHTC: ý định hủy hết hạn sau khi cap/forms đã hủy — đã nhận lại ý định và ghi sự kiện hủy", "outcome", "recovered")
		return s.Get(ctx, id)
	case errors.Is(err, domain.ErrCancelling):
		lg.WarnContext(ctx, "hủy kỳ KHTC: ý định hủy hết hạn, lần hủy khác đang giữ ý định", "outcome", "deferred")
		return Detail{}, ErrCancelling
	case errors.Is(err, domain.ErrHasFinal):
		lg.ErrorContext(ctx, "hủy kỳ KHTC: cap/forms đã hủy kỳ native nhưng báo cáo đã có Bản cuối — cần đối soát", "outcome", "alert")
		return Detail{}, ErrCancelInterrupted.WithCause(err)
	default:
		lg.ErrorContext(ctx, "hủy kỳ KHTC: cap/forms đã hủy kỳ native nhưng chưa ghi được sự kiện hủy — thử Hủy kỳ lại", "outcome", "alert", "error", err.Error())
		return Detail{}, classify(err)
	}
}

// ---------------------------------------------------------------------------
// Phiếu cơ sở ở cap/forms
// ---------------------------------------------------------------------------

// BaseFields là trường của biểu cap/forms "Báo cáo cho KHTC" (KHTC-PHIEU). Phiên bản 2 (W11.63): lưới hạng mục và mục lời
// văn theo mẫu phiếu từng phòng nằm ở bảng số liệu pf_extension.khtc_form_sheet; bài nộp mang mã bảng đã niêm phong
// (trường FieldSheet — profile kn_khtc có Validator của W11.63 bắt buộc). Bốn ô lời văn của phiên bản 1 (W11.62) giữ
// để kỳ cũ đọc được, không còn bắt buộc.
var BaseFields = []map[string]any{
	{"key": FieldSheet, "label": "Bảng số liệu (hệ thống)", "type": "text", "required": false, "group": "Hệ thống"},
	{"key": "ket_qua", "label": "Kết quả thực hiện trong kỳ", "type": "text", "required": false, "group": "Báo cáo cho KHTC"},
	{"key": "kho_khan", "label": "Khó khăn, vướng mắc", "type": "text", "required": false, "group": "Báo cáo cho KHTC"},
	{"key": "nhiem_vu_ky_toi", "label": "Nhiệm vụ kỳ tới", "type": "text", "required": false, "group": "Báo cáo cho KHTC"},
	{"key": "kien_nghi", "label": "Kiến nghị, đề xuất", "type": "text", "required": false, "group": "Báo cáo cho KHTC"},
}

// TemplateState là phiếu cơ sở đã cài (Version 0 = chưa cài).
type TemplateState struct {
	Code    string
	Version int64
	Title   string
}

// Template đọc phiên bản phiếu cơ sở đã cài.
func (s *Service) Template(ctx context.Context) (TemplateState, error) {
	if _, err := s.reader(ctx); err != nil {
		return TemplateState{}, err
	}
	ref, err := s.Forms.LatestTemplate(ctx, domain.ProfileKHTC, domain.TemplateCode)
	if errors.Is(err, formsport.ErrTemplateNotFound) {
		return TemplateState{Code: domain.TemplateCode}, nil
	}
	if err != nil {
		return TemplateState{}, err
	}
	return TemplateState{Code: ref.Code, Version: ref.Version, Title: ref.Title}, nil
}

// FieldSheet là trường mã bảng số liệu (khớp khtcform/domain.FieldSheet — W11.63).
const FieldSheet = "bang_so_lieu"

// InstallTemplate cài phiếu cơ sở (vai define_by của profile kn_khtc: Admin KN, reports_admin — cap/forms kiểm). Đã cài
// phiên bản có trường bảng số liệu → giữ nguyên; phiên bản 1 (W11.62, chưa có trường) → cài phiên bản mới cùng mã.
func (s *Service) InstallTemplate(ctx context.Context, idem string) (TemplateState, error) {
	if s.Forms == nil {
		return TemplateState{}, ErrDependency.WithDetail("chưa nối cap/forms cho Kỳ KHTC")
	}
	ref, err := s.Forms.LatestTemplate(ctx, domain.ProfileKHTC, domain.TemplateCode)
	if err == nil && bytes.Contains(ref.Fields, []byte(`"`+FieldSheet+`"`)) {
		return TemplateState{Code: ref.Code, Version: ref.Version, Title: ref.Title}, nil
	}
	if err != nil && !errors.Is(err, formsport.ErrTemplateNotFound) {
		return TemplateState{}, err
	}
	raw, err := json.Marshal(BaseFields)
	if err != nil {
		return TemplateState{}, err
	}
	if idem == "" {
		idem = ids.New().String()
	}
	ref, err = s.Forms.DefineTemplate(ctx, formsport.TemplateDef{Profile: domain.ProfileKHTC, Code: domain.TemplateCode,
		Title: "Báo cáo cho KHTC — phiếu cơ sở", Fields: raw}, "khtc:template:"+idem)
	if err != nil {
		return TemplateState{}, err
	}
	return TemplateState{Code: ref.Code, Version: ref.Version, Title: ref.Title}, nil
}
