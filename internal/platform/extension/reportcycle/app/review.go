package app

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/store"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// selfActAs chặn người đã NHẬP THAY (chế độ hỗ trợ, on_behalf_of) tự rà soát/xác nhận/chốt bản của chính mình
// (gap §4.6 maker-checker, R-56).
func selfActAs(ctx context.Context, tx pgx.Tx, a actor, taskIDs []uuid.UUID) error {
	if len(taskIDs) == 0 {
		return nil
	}
	n, err := store.ActAsSubmissionsBy(ctx, tx, a.p.ActorRef, taskIDs)
	if err != nil {
		return err
	}
	if n > 0 {
		return problem.SelfApproval.WithDetail("Không rà soát, xác nhận hoặc chốt báo cáo do chính mình nhập thay (chế độ hỗ trợ)")
	}
	return nil
}

// ReviewResult là công việc sau rà soát.
type ReviewResult struct {
	Task TaskView
}

func (s *Service) reviewable(ctx context.Context, tx pgx.Tx, a actor, taskID uuid.UUID) (*store.Task, *store.Period, error) {
	t, err := s.taskInScope(ctx, tx, a, taskID, true)
	if err != nil {
		return nil, nil, err
	}
	if t.SubmittedSubmissionID == nil {
		return nil, nil, ErrNotSubmitted
	}
	p, err := store.PeriodByID(ctx, tx, t.PeriodID, false)
	if err != nil {
		return nil, nil, err
	}
	return t, p, selfActAs(ctx, tx, a, []uuid.UUID{t.ID})
}

// RequestAdjustment yêu cầu xã điều chỉnh, LÝ DO BẮT BUỘC (FN-TI-02.04). Cấp Tỉnh không sửa số gốc; kỳ đã chốt
// → phải mở lại kỳ trước.
func (s *Service) RequestAdjustment(ctx context.Context, taskID uuid.UUID, reason, idem string) (ReviewResult, error) {
	a, err := s.writer(ctx, authz.KNTinh)
	if err != nil {
		return ReviewResult{}, err
	}
	why, ok := domain.Reason(reason, 2000)
	if !ok {
		return ReviewResult{}, problem.Validation.WithField("reason", "bắt buộc, 10–2000 ký tự")
	}
	now := s.now()
	var out ReviewResult
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		t, p, err := s.reviewable(ctx, tx, a, taskID)
		if err != nil {
			return err
		}
		st := domain.TaskState{Status: t.Status, DueAt: t.DueAt, PeriodClosed: p.Status == "closed", ReopenedUntil: t.ReopenedUntil}
		if !st.Open(now) {
			return ErrAdjustmentAfterClose
		}
		if err := store.InsertReview(ctx, tx, a.p.TenantID, store.Review{ID: ids.New(), TaskID: t.ID, SubmissionID: *t.SubmittedSubmissionID,
			Action: "adjustment_requested", Reason: &why, Department: t.DepartmentCode, OnBehalfOf: a.onBehalf(), EventTime: now, ActorRef: a.p.ActorRef,
			IdemKey: idemOr(idem, "review")}); err != nil {
			return err
		}
		t.Status, t.AdjustmentReason, t.AdjustmentAt, t.ConfirmedBy, t.ConfirmedAt = domain.TaskAdjustmentRequested, &why, &now, nil, nil
		if err := store.SaveSubmissionState(ctx, tx, *t, now); err != nil {
			return err
		}
		if err := store.InsertTaskEvents(ctx, tx, a.p.TenantID, []store.TaskEvent{{ID: ids.New(), TaskID: t.ID, Action: "adjustment_requested",
			StatusAfter: t.Status, SubmissionID: t.SubmittedSubmissionID, Reason: &why, OnBehalfOf: a.onBehalf(), EventTime: now, ActorRef: a.p.ActorRef,
			IdemKey: idemOr(idem, "task")}}); err != nil {
			return err
		}
		out.Task = viewOf(*t, *p, nil, now)
		return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: a.p.TenantID, Type: port.EventSubmissionAdjustmentRequested, SchemaVersion: 1,
			OccurredAt: now, Producer: port.EventProducerName, Payload: port.SubmissionAdjustmentRequestedV1{TaskID: t.ID.String(),
				SubmissionID: t.SubmittedSubmissionID.String(), FormCode: t.FormCode, CommuneCode: t.CommuneCode, PeriodLabel: p.Label, Reason: why,
				RecipientRefs: refStrings(t.Assignees), ActorRef: a.p.ActorRef.String(), EventTime: ts(now)}})
	})
	return out, err
}

// Confirm "Xác nhận" (tùy chọn) bản đã gửi.
func (s *Service) Confirm(ctx context.Context, taskID uuid.UUID, note, idem string) (ReviewResult, error) {
	a, err := s.writer(ctx, authz.KNTinh)
	if err != nil {
		return ReviewResult{}, err
	}
	var why *string
	if n := strings.TrimSpace(note); n != "" {
		if r, ok := domain.Reason(n, 2000); ok {
			why = &r
		} else {
			return ReviewResult{}, problem.Validation.WithField("note", "10–2000 ký tự nếu có")
		}
	}
	now := s.now()
	var out ReviewResult
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		t, p, err := s.reviewable(ctx, tx, a, taskID)
		if err != nil {
			return err
		}
		if t.Status != domain.TaskSubmitted && t.Status != domain.TaskConfirmed {
			return ErrNotSubmitted.WithDetail("chỉ xác nhận bản đã gửi chưa có yêu cầu điều chỉnh")
		}
		if err := store.InsertReview(ctx, tx, a.p.TenantID, store.Review{ID: ids.New(), TaskID: t.ID, SubmissionID: *t.SubmittedSubmissionID,
			Action: "confirmed", Reason: why, Department: t.DepartmentCode, OnBehalfOf: a.onBehalf(), EventTime: now, ActorRef: a.p.ActorRef,
			IdemKey: idemOr(idem, "review")}); err != nil {
			return err
		}
		t.Status, t.ConfirmedBy, t.ConfirmedAt = domain.TaskConfirmed, &a.p.ActorRef, &now
		if err := store.SaveSubmissionState(ctx, tx, *t, now); err != nil {
			return err
		}
		out.Task = viewOf(*t, *p, nil, now)
		return store.InsertTaskEvents(ctx, tx, a.p.TenantID, []store.TaskEvent{{ID: ids.New(), TaskID: t.ID, Action: "confirmed", StatusAfter: t.Status,
			SubmissionID: t.SubmittedSubmissionID, Reason: why, OnBehalfOf: a.onBehalf(), EventTime: now, ActorRef: a.p.ActorRef, IdemKey: idemOr(idem, "task")}})
	})
	return out, err
}

// PeriodView là kỳ kèm đợt và số đã nộp.
type PeriodView struct {
	Period    store.Period
	Batch     store.Batch
	Submitted int
	Total     int
	Reopened  int
	Runs      []store.Run
}

func (s *Service) periodInScope(ctx context.Context, tx pgx.Tx, a actor, id uuid.UUID, lock bool) (*store.Period, *store.Batch, error) {
	p, err := store.PeriodByID(ctx, tx, id, lock)
	if err != nil {
		return nil, nil, err
	}
	if p == nil || !a.allowsProvince(p.ProvinceCode, p.DepartmentCode) {
		return nil, nil, ErrPeriodNotFound
	}
	b, err := store.BatchByID(ctx, tx, p.BatchID, false)
	if err != nil {
		return nil, nil, err
	}
	if b.Status == "withdrawn" {
		return nil, nil, ErrBatchWithdrawn
	}
	return p, b, nil
}

// GetPeriod trả kỳ (khung Chốt kỳ: chốt tự động lúc, đã nộp n/N).
func (s *Service) GetPeriod(ctx context.Context, id uuid.UUID) (PeriodView, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return PeriodView{}, err
	}
	var v PeriodView
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		p, b, err := s.periodInScope(ctx, tx, a, id, false)
		if err != nil {
			return err
		}
		v.Period, v.Batch = *p, *b
		ts, err := store.TasksOfPeriod(ctx, tx, id, nil, false)
		if err != nil {
			return err
		}
		for _, t := range ts {
			if a.k.Role == authz.KNXa && !slices.Contains(a.k.Communes, t.CommuneCode) {
				continue
			}
			v.Total++
			if t.SubmittedAt != nil {
				v.Submitted++
			}
			if t.ReopenedUntil != nil {
				v.Reopened++
			}
		}
		v.Runs, err = store.Runs(ctx, tx, id, nil, 50)
		return err
	})
	return v, err
}

// ClosePeriod "Chốt kỳ ngay" (FN-TI-02.04 B4). Kỳ đã chốt nhưng còn xã/biểu đang mở lại → chốt lại (reclosed).
// Hệ thống tự cập nhật tổng hợp MỘT lần lúc chốt (FN-TI-02.05, nhật ký "Hệ thống").
func (s *Service) ClosePeriod(ctx context.Context, id uuid.UUID, idem string) (PeriodView, error) {
	a, err := s.writer(ctx, authz.KNTinh)
	if err != nil {
		return PeriodView{}, err
	}
	now := s.now()
	var cons consolidateOut
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		p, b, err := s.periodInScope(ctx, tx, a, id, true)
		if err != nil {
			return err
		}
		tasks, err := store.TasksOfPeriod(ctx, tx, id, nil, true)
		if err != nil {
			return err
		}
		tids := make([]uuid.UUID, 0, len(tasks))
		var reopened []uuid.UUID
		for _, t := range tasks {
			tids = append(tids, t.ID)
			if t.ReopenedUntil != nil {
				reopened = append(reopened, t.ID)
			}
		}
		if err := selfActAs(ctx, tx, a, tids); err != nil {
			return err
		}
		if p.Status == "closed" {
			if len(reopened) == 0 {
				return problem.Conflict.WithDetail("Kỳ đã chốt")
			}
			cons, err = s.reclose(ctx, tx, a.p.TenantID, *p, *b, tasks, reopened, a.p.ActorRef, a.onBehalf(), idemOr(idem, "reclose"), now)
			return err
		}
		cons, err = s.closeTx(ctx, tx, a.p.TenantID, *p, *b, a.p.ActorRef, a.onBehalf(), "manual", idemOr(idem, "close"), now)
		return err
	})
	if err != nil {
		return PeriodView{}, err
	}
	s.afterConsolidate(cons)
	return s.GetPeriod(ctx, id)
}

// closeTx chốt kỳ: sổ period_event + chiếu + tổng hợp tự động (Hệ thống) + sự kiện.
func (s *Service) closeTx(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p store.Period, b store.Batch, actor uuid.UUID, obo *uuid.UUID, mode, idem string,
	now time.Time) (consolidateOut, error) {
	action := "closed"
	if mode == "auto" {
		action = "auto_closed"
	}
	ev := store.PeriodEvent{ID: ids.New(), PeriodID: p.ID, BatchID: p.BatchID, Action: action, Department: p.DepartmentCode, OnBehalfOf: obo,
		EventTime: now, ActorRef: actor, IdemKey: idem}
	if err := store.InsertPeriodEvent(ctx, tx, tid, &ev); err != nil {
		return consolidateOut{}, err
	}
	if err := store.ClosePeriod(ctx, tx, p.ID, actor, mode, now); err != nil {
		return consolidateOut{}, err
	}
	p.Status = "closed"
	cons, err := s.consolidateTx(ctx, tx, tid, p, b, nil, domain.TriggerSystem, port.SystemActor, nil, idem+":cons", now)
	if err != nil {
		return cons, err
	}
	if err := s.mirrorForms(ctx, tx, p, formsport.ActionClosed, mode, nil, "", actor, idem, now, cons.runs); err != nil { // W6.2: cap/forms
		return cons, err
	}
	return cons, outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: port.EventPeriodClosed, SchemaVersion: 1, OccurredAt: now,
		Producer: port.EventProducerName, Payload: port.PeriodClosedV1{PeriodID: p.ID.String(), BatchID: p.BatchID.String(), Label: p.Label, Mode: mode,
			DepartmentCode: p.DepartmentCode, ProvinceCode: p.ProvinceCode, ActorRef: actor.String(), EventTime: ts(now)}})
}

// reclose chốt lại các xã/biểu đã mở lại, tổng hợp lại (lưu "phiên bản điều chỉnh" = lần tổng hợp mới).
func (s *Service) reclose(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p store.Period, b store.Batch, tasks []store.Task, ids_ []uuid.UUID,
	actor uuid.UUID, obo *uuid.UUID, idem string, now time.Time) (consolidateOut, error) {
	var communes []string
	var tpls []uuid.UUID
	for _, t := range tasks {
		if slices.Contains(ids_, t.ID) {
			if !slices.Contains(communes, t.CommuneCode) {
				communes = append(communes, t.CommuneCode)
			}
			if !slices.Contains(tpls, t.TemplateID) {
				tpls = append(tpls, t.TemplateID)
			}
		}
	}
	ev := store.PeriodEvent{ID: ids.New(), PeriodID: p.ID, BatchID: p.BatchID, Action: "reclosed", Communes: communes, TemplateIDs: tpls, TaskIDs: ids_,
		Department: p.DepartmentCode, OnBehalfOf: obo, EventTime: now, ActorRef: actor, IdemKey: idem}
	if err := store.InsertPeriodEvent(ctx, tx, tid, &ev); err != nil {
		return consolidateOut{}, err
	}
	if err := store.SetReopened(ctx, tx, ids_, nil, now); err != nil {
		return consolidateOut{}, err
	}
	cons, err := s.consolidateTx(ctx, tx, tid, p, b, tpls, domain.TriggerSystem, port.SystemActor, nil, idem+":cons", now)
	if err != nil {
		return cons, err
	}
	mode := formsport.ModeManual
	if actor == port.SystemActor {
		mode = formsport.ModeAuto
	}
	return cons, s.mirrorForms(ctx, tx, p, formsport.ActionClosed, mode, communes, "", actor, idem, now, cons.runs) // W6.2: cap/forms
}

// ReopenInput là yêu cầu mở lại kỳ đã chốt cho xã/biểu.
type ReopenInput struct {
	Communes    []string    // rỗng = mọi xã của kỳ
	TemplateIDs []uuid.UUID // rỗng = mọi biểu
	Reason      string
	Until       time.Time // thời hạn mở
}

// ReopenResult là kết quả mở lại.
type ReopenResult struct {
	Period          PeriodView
	TaskIDs         []uuid.UUID
	DispatchChanged bool // kỳ đã gửi Chi cục → cảnh báo "số liệu đã thay đổi"
}

// ReopenPeriod mở lại kỳ đã chốt cho một xã/biểu — sự kiện bù, lý do + thời hạn mở bắt buộc (FN-TI-02.04 B5);
// hết hạn mở → job tự chốt lại.
func (s *Service) ReopenPeriod(ctx context.Context, id uuid.UUID, in ReopenInput, idem string) (ReopenResult, error) {
	a, err := s.writer(ctx, authz.KNTinh)
	if err != nil {
		return ReopenResult{}, err
	}
	why, ok := domain.Reason(in.Reason, 500)
	if !ok {
		return ReopenResult{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	now := s.now()
	until := in.Until.UTC()
	if !until.After(now) || until.After(now.AddDate(0, 0, 30)) {
		return ReopenResult{}, problem.Validation.WithField("until", "thời hạn mở phải sau hiện tại và không quá 30 ngày")
	}
	var out ReopenResult
	var evt port.PeriodReopenedV1
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		p, _, err := s.periodInScope(ctx, tx, a, id, true)
		if err != nil {
			return err
		}
		if p.Status != "closed" {
			return ErrPeriodOpen
		}
		tasks, err := store.TasksOfPeriod(ctx, tx, id, nil, true)
		if err != nil {
			return err
		}
		var recips []uuid.UUID
		var communes, forms []string
		for _, t := range tasks {
			if len(in.Communes) > 0 && !slices.Contains(in.Communes, t.CommuneCode) {
				continue
			}
			if len(in.TemplateIDs) > 0 && !slices.Contains(in.TemplateIDs, t.TemplateID) {
				continue
			}
			out.TaskIDs = append(out.TaskIDs, t.ID)
			for _, r := range t.Assignees {
				if !slices.Contains(recips, r) {
					recips = append(recips, r)
				}
			}
			if !slices.Contains(communes, t.CommuneCode) {
				communes = append(communes, t.CommuneCode)
			}
			if !slices.Contains(forms, t.FormCode) {
				forms = append(forms, t.FormCode)
			}
		}
		if len(out.TaskIDs) == 0 {
			return problem.Validation.WithField("communes", "không có công việc nào của kỳ khớp xã/biểu đã chọn")
		}
		ev := store.PeriodEvent{ID: ids.New(), PeriodID: p.ID, BatchID: p.BatchID, Action: "reopened", Communes: in.Communes, TemplateIDs: in.TemplateIDs,
			TaskIDs: out.TaskIDs, Reason: &why, ReopenUntil: &until, Department: p.DepartmentCode, OnBehalfOf: a.onBehalf(), EventTime: now,
			ActorRef: a.p.ActorRef, IdemKey: idemOr(idem, "reopen")}
		if err := store.InsertPeriodEvent(ctx, tx, a.p.TenantID, &ev); err != nil {
			return err
		}
		if err := store.SetReopened(ctx, tx, out.TaskIDs, &until, now); err != nil {
			return err
		}
		if err := s.mirrorForms(ctx, tx, *p, formsport.ActionReopened, formsport.ModeManual, communes, why, a.p.ActorRef, idemOr(idem, "reopen"), now, nil); err != nil {
			return err // W6.2: cap/forms
		}
		if s.Dispatches != nil {
			if out.DispatchChanged, err = s.Dispatches.SentForPeriod(ctx, a.p.TenantID, p.ID); err != nil {
				return err
			}
		}
		sort.Strings(communes)
		sort.Strings(forms)
		evt = port.PeriodReopenedV1{PeriodID: p.ID.String(), BatchID: p.BatchID.String(), Label: p.Label, Communes: communes, FormCodes: forms, Reason: why,
			ReopenUntil: ts(until), RecipientRefs: refStrings(recips), DispatchChanged: out.DispatchChanged, ActorRef: a.p.ActorRef.String(), EventTime: ts(now)}
		return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: a.p.TenantID, Type: port.EventPeriodReopened, SchemaVersion: 1,
			OccurredAt: now, Producer: port.EventProducerName, Payload: evt})
	})
	if err != nil {
		return ReopenResult{}, err
	}
	out.Period, err = s.GetPeriod(ctx, id)
	return out, err
}

// PeriodLog trả nhật ký kỳ (chốt, tự chốt, mở lại, chốt lại).
func (s *Service) PeriodLog(ctx context.Context, id uuid.UUID) ([]LogEntry, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return nil, err
	}
	var evs []store.PeriodEvent
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if _, _, err := s.periodInScope(ctx, tx, a, id, false); err != nil {
			return err
		}
		var err error
		evs, err = store.PeriodEvents(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	refs := []uuid.UUID{}
	for _, e := range evs {
		refs = append(refs, e.ActorRef)
	}
	names := s.names(ctx, refs)
	out := make([]LogEntry, 0, len(evs))
	for _, e := range evs {
		le := LogEntry{Event: "period." + e.Action, ActorRef: e.ActorRef, ActorDisplay: names[e.ActorRef], At: e.EventTime, Reason: e.Reason, OnBehalfOf: e.OnBehalfOf}
		if e.ReopenUntil != nil {
			v := ts(*e.ReopenUntil)
			le.Changes = append(le.Changes, Change{Field: "reopen_until", New: &v})
		}
		if len(e.Communes) > 0 {
			v := strings.Join(e.Communes, ",")
			le.Changes = append(le.Changes, Change{Field: "communes", New: &v})
		}
		out = append(out, le)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Trạng thái nộp + nhắc nhở (FN-TI-02.03)
// ---------------------------------------------------------------------------

// StatusRow là một dòng bảng trạng thái nộp.
type StatusRow struct {
	Task      store.Task
	Display   string
	NoOfficer bool
}

// StatusBoard là bảng trạng thái nộp của đợt × kỳ × biểu.
type StatusBoard struct {
	Period store.Period
	Counts map[string]int
	Rows   []StatusRow
	Names  map[uuid.UUID]string
}

// Status trả trạng thái nộp theo xã thời gian thực (đọc thẳng bảng chiếu công việc).
func (s *Service) Status(ctx context.Context, batchID uuid.UUID, periodID, templateID *uuid.UUID, status string) (StatusBoard, error) {
	a, err := s.knActor(ctx, authz.KNTinh, authz.KNTW, authz.KNAdmin)
	if err != nil {
		return StatusBoard{}, err
	}
	now := s.now()
	board := StatusBoard{Counts: map[string]int{}}
	for _, st := range append(slices.Clone(domain.DisplayStatuses), "no_officer") {
		board.Counts[st] = 0
	}
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if _, err := s.batchInScope(ctx, tx, a, batchID, false); err != nil {
			return err
		}
		ps, err := store.PeriodsOfBatch(ctx, tx, batchID)
		if err != nil {
			return err
		}
		if len(ps) == 0 {
			return ErrPeriodNotFound
		}
		board.Period = currentPeriod(ps, now)
		if periodID != nil {
			found := false
			for _, p := range ps {
				if p.ID == *periodID {
					board.Period, found = p, true
				}
			}
			if !found {
				return ErrPeriodNotFound
			}
		}
		pid := board.Period.ID
		tasks, err := store.ListTasks(ctx, tx, store.TaskFilter{BatchID: &batchID, PeriodID: &pid, TemplateID: templateID})
		if err != nil {
			return err
		}
		for _, t := range tasks {
			v := viewOf(t, board.Period, nil, now)
			row := StatusRow{Task: t, Display: v.Display, NoOfficer: len(t.Assignees) == 0}
			board.Counts[v.Display]++
			if row.NoOfficer {
				board.Counts["no_officer"]++
			}
			if status != "" && status != v.Display && (status != "no_officer" || !row.NoOfficer) {
				continue
			}
			board.Rows = append(board.Rows, row)
		}
		return nil
	})
	if err != nil {
		return board, err
	}
	refs := []uuid.UUID{}
	for _, r := range board.Rows {
		refs = append(refs, r.Task.Assignees...)
	}
	board.Names = s.names(ctx, refs)
	return board, nil
}

// currentPeriod: kỳ đang diễn ra (hoặc gần nhất đã qua) để mặc định chọn.
func currentPeriod(ps []store.Period, now time.Time) store.Period {
	cur := ps[0]
	for _, p := range ps {
		if !p.From.After(now) {
			cur = p
		}
	}
	return cur
}

// RemindInput là yêu cầu "Gửi nhắc nhở".
type RemindInput struct {
	PeriodID   uuid.UUID
	TemplateID *uuid.UUID
	Communes   []string // xã đã chọn; rỗng + AllPending = mọi xã chưa nộp
	AllPending bool
	Message    string
}

// RemindResult là kết quả nhắc.
type RemindResult struct {
	Reminder         store.Reminder
	SkippedNoOfficer []string
	SkippedDone      []string
}

// Remind gửi nhắc tới cán bộ phụ trách xã chưa nộp/trễ hạn (thông báo trong app + đẩy chỉ dữ liệu — tới tay < 1 phút).
func (s *Service) Remind(ctx context.Context, batchID uuid.UUID, in RemindInput, idem string) (RemindResult, error) {
	a, err := s.writer(ctx, authz.KNTinh)
	if err != nil {
		return RemindResult{}, err
	}
	msg := strings.TrimSpace(in.Message)
	if n := len([]rune(msg)); n < 1 || n > 1000 {
		return RemindResult{}, problem.Validation.WithField("message", "bắt buộc, tối đa 1000 ký tự")
	}
	if len(in.Communes) == 0 && !in.AllPending {
		return RemindResult{}, problem.Validation.WithField("communes", "chọn xã hoặc all_pending = true")
	}
	now := s.now()
	var out RemindResult
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		b, err := s.batchInScope(ctx, tx, a, batchID, false)
		if err != nil {
			return err
		}
		if b.Status == "withdrawn" {
			return ErrBatchWithdrawn
		}
		p, err := store.PeriodByID(ctx, tx, in.PeriodID, false)
		if err != nil {
			return err
		}
		if p == nil || p.BatchID != batchID {
			return ErrPeriodNotFound
		}
		tasks, err := store.ListTasks(ctx, tx, store.TaskFilter{BatchID: &batchID, PeriodID: &p.ID, TemplateID: in.TemplateID})
		if err != nil {
			return err
		}
		rem, skippedNo, skippedDone := pickReminder(tasks, *p, in.Communes, now)
		out.SkippedNoOfficer, out.SkippedDone = skippedNo, skippedDone
		if len(rem.TaskIDs) == 0 {
			return problem.Validation.WithField("communes", "không có xã nào cần nhắc (đã nộp hoặc chưa có cán bộ)")
		}
		rem.ID, rem.BatchID, rem.PeriodID, rem.TemplateID, rem.Mode, rem.Message = ids.New(), batchID, p.ID, in.TemplateID, port.ReminderManual, msg
		rem.Department, rem.OnBehalfOf, rem.EventTime, rem.ActorRef, rem.IdemKey = b.DepartmentCode, a.onBehalf(), now, a.p.ActorRef, idemOr(idem, "remind")
		if err := s.recordReminder(ctx, tx, a.p.TenantID, &rem, *p); err != nil {
			return err
		}
		out.Reminder = rem
		return nil
	})
	return out, err
}

// pickReminder chọn công việc cần nhắc: còn phải gửi, kỳ mở, có cán bộ.
func pickReminder(tasks []store.Task, p store.Period, communes []string, now time.Time) (store.Reminder, []string, []string) {
	var rem store.Reminder
	var noOfficer, done []string
	for _, t := range tasks {
		if len(communes) > 0 && !slices.Contains(communes, t.CommuneCode) {
			continue
		}
		st := domain.TaskState{Status: t.Status, DueAt: t.DueAt, PeriodClosed: p.Status == "closed", ReopenedUntil: t.ReopenedUntil}
		if !st.Pending() || !st.Open(now) {
			if !slices.Contains(done, t.CommuneCode) {
				done = append(done, t.CommuneCode)
			}
			continue
		}
		if len(t.Assignees) == 0 {
			if !slices.Contains(noOfficer, t.CommuneCode) {
				noOfficer = append(noOfficer, t.CommuneCode)
			}
			continue
		}
		rem.TaskIDs = append(rem.TaskIDs, t.ID)
		if !slices.Contains(rem.Communes, t.CommuneCode) {
			rem.Communes = append(rem.Communes, t.CommuneCode)
		}
		for _, r := range t.Assignees {
			if !slices.Contains(rem.Recipients, r) {
				rem.Recipients = append(rem.Recipients, r)
			}
		}
	}
	return rem, noOfficer, done
}

func (s *Service) recordReminder(ctx context.Context, tx pgx.Tx, tid uuid.UUID, rem *store.Reminder, p store.Period) error {
	if err := store.InsertReminder(ctx, tx, tid, rem); err != nil {
		return err
	}
	if err := store.MarkReminded(ctx, tx, rem.TaskIDs, rem.EventTime); err != nil {
		return err
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: port.EventReportTaskReminded, SchemaVersion: 1, OccurredAt: rem.EventTime,
		Producer: port.EventProducerName, Payload: port.ReportTaskRemindedV1{ReminderID: rem.ID.String(), BatchID: rem.BatchID.String(),
			PeriodID: p.ID.String(), PeriodLabel: p.Label, Mode: rem.Mode, Message: rem.Message, DueAt: ts(p.DueAt), Communes: rem.Communes,
			TaskIDs: refStrings(rem.TaskIDs), RecipientRefs: refStrings(rem.Recipients), ActorRef: rem.ActorRef.String(), EventTime: ts(rem.EventTime)}})
}

// RemindersOf trả lịch sử nhắc của đợt.
func (s *Service) RemindersOf(ctx context.Context, batchID uuid.UUID, periodID *uuid.UUID) ([]store.Reminder, map[uuid.UUID]string, error) {
	a, err := s.knActor(ctx, authz.KNTinh, authz.KNTW, authz.KNAdmin)
	if err != nil {
		return nil, nil, err
	}
	var out []store.Reminder
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if _, err := s.batchInScope(ctx, tx, a, batchID, false); err != nil {
			return err
		}
		var err error
		out, err = store.Reminders(ctx, tx, batchID, periodID, 200)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	refs := []uuid.UUID{}
	for _, r := range out {
		refs = append(refs, r.ActorRef)
	}
	return out, s.names(ctx, refs), nil
}

func autoMessage(mode string, p store.Period) string {
	due := p.DueAt.In(zone()).Format("15:04 02/01/2006")
	if mode == port.ReminderAutoBefore {
		return fmt.Sprintf("Sắp đến hạn nộp báo cáo %s (hạn %s).", p.Label, due)
	}
	return fmt.Sprintf("Báo cáo %s đã trễ hạn (hạn %s) — vẫn nộp được tới khi chốt kỳ.", p.Label, due)
}
