package app

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// TaskView là công việc kèm kỳ, đợt và trạng thái hiển thị.
type TaskView struct {
	Task    store.Task
	Period  store.Period
	Batch   *store.Batch
	Display string
	Open    bool
}

func viewOf(t store.Task, p store.Period, b *store.Batch, now time.Time) TaskView {
	st := domain.TaskState{Status: t.Status, DueAt: t.DueAt, PeriodClosed: p.Status == "closed", ReopenedUntil: t.ReopenedUntil,
		Extended: domain.ExtendedLifecycle(t.DepartmentCode)}
	return TaskView{Task: t, Period: p, Batch: b, Display: st.Display(now), Open: st.Open(now)}
}

// TaskQuery lọc "Công việc cần làm" (FN-XA-03.01).
type TaskQuery struct {
	Commune    string
	PeriodType string
	Status     string // trạng thái hiển thị
	FormCode   string
	BatchID    *uuid.UUID
	PeriodID   *uuid.UUID
	Limit      int
}

// TaskList là danh sách kèm số đếm và nhắc mới nhất.
type TaskList struct {
	Items        []TaskView
	Todo         int // cần làm (còn phải gửi, kỳ mở)
	Overdue      int
	Adjustment   int
	LastReminder *store.Reminder
}

// ListTasks trả công việc của các xã phụ trách (Cấp Xã) hoặc của tỉnh (Cấp Tỉnh), mặc định sắp theo hạn gần nhất.
func (s *Service) ListTasks(ctx context.Context, q TaskQuery) (TaskList, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return TaskList{}, err
	}
	f := store.TaskFilter{Department: a.dept(), Province: a.province, BatchID: q.BatchID, PeriodID: q.PeriodID, FormCode: q.FormCode,
		PeriodType: q.PeriodType, OrderByDue: true, Limit: 5000}
	if a.k.Role == port.SessionRoleCommune {
		f.Communes = slices.Clone(a.k.Communes)
		if f.Communes == nil {
			f.Communes = []string{}
		}
		if q.Commune != "" {
			if !slices.Contains(a.k.Communes, q.Commune) {
				return TaskList{}, ErrOutOfScope
			}
			f.Communes = []string{q.Commune}
		}
	} else if q.Commune != "" {
		f.Communes = []string{q.Commune}
	}
	if q.Status != "" && !slices.Contains(domain.DisplayStatuses, q.Status) {
		return TaskList{}, problem.Validation.WithField("status", strings.Join(domain.DisplayStatuses, " | "))
	}
	now := s.now()
	var out TaskList
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		ts, err := store.ListTasks(ctx, tx, f)
		if err != nil {
			return err
		}
		pids := make([]uuid.UUID, 0)
		for _, t := range ts {
			if !slices.Contains(pids, t.PeriodID) {
				pids = append(pids, t.PeriodID)
			}
		}
		ps, err := store.PeriodsByIDs(ctx, tx, pids)
		if err != nil {
			return err
		}
		for _, t := range ts {
			v := viewOf(t, ps[t.PeriodID], nil, now)
			st := domain.TaskState{Status: t.Status}
			if v.Open && st.Pending() {
				out.Todo++
			}
			if v.Display == domain.TaskOverdue {
				out.Overdue++
			}
			if v.Display == domain.TaskAdjustmentRequested {
				out.Adjustment++
			}
			if q.Status != "" && v.Display != q.Status {
				continue
			}
			out.Items = append(out.Items, v)
		}
		if a.k.Role == port.SessionRoleCommune && len(a.k.Communes) > 0 {
			out.LastReminder, err = store.LatestReminderFor(ctx, tx, a.k.Communes, a.k.Department)
		}
		return err
	})
	if lim := clampLimit(q.Limit); q.Limit > 0 && len(out.Items) > lim {
		out.Items = out.Items[:lim]
	}
	return out, err
}

// TaskDetail là công việc để mở màn nhập (FN-XA-03.02).
type TaskDetail struct {
	TaskView
	FormName     string
	PeriodLabels []string // nhãn kỳ theo khe (tiêu đề cột theo kỳ của mẫu — FN-TI-02.02)
	OpenSlots    []int
	Provincial   map[string]string
	Carried      map[string]map[string]string // giá trị khe kỳ khác (chỉ xem)
	Latest       *store.Submission
	LatestSubmit *store.Submission
}

func (s *Service) taskInScope(ctx context.Context, tx pgx.Tx, a actor, id uuid.UUID, lock bool) (*store.Task, error) {
	t, err := store.TaskByID(ctx, tx, id, lock)
	if err != nil {
		return nil, err
	}
	if t == nil || t.Withdrawn || !a.allowsTask(*t) {
		return nil, ErrTaskNotFound
	}
	return t, nil
}

// GetTask trả chi tiết công việc.
func (s *Service) GetTask(ctx context.Context, id uuid.UUID) (TaskDetail, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return TaskDetail{}, err
	}
	now := s.now()
	var d TaskDetail
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		t, err := s.taskInScope(ctx, tx, a, id, false)
		if err != nil {
			return err
		}
		b, err := store.BatchByID(ctx, tx, t.BatchID, false)
		if err != nil {
			return err
		}
		ps, err := store.PeriodsOfBatch(ctx, tx, t.BatchID)
		if err != nil {
			return err
		}
		var p store.Period
		for _, x := range ps {
			d.PeriodLabels = append(d.PeriodLabels, x.Label)
			if x.ID == t.PeriodID {
				p = x
			}
		}
		d.TaskView = viewOf(*t, p, b, now)
		for _, f := range b.Forms {
			if f.TemplateID == t.TemplateID {
				d.FormName = f.Name
			}
		}
		if d.Open && t.PeriodBlock {
			d.OpenSlots = []int{t.Slot}
		}
		pv, err := store.ProvincialValues(ctx, tx, t.BatchID, &t.TemplateID)
		if err != nil {
			return err
		}
		d.Provincial = pv[t.TemplateID][t.CommuneCode]
		if t.LatestSubmissionID != nil {
			if d.Latest, err = store.SubmissionByID(ctx, tx, *t.LatestSubmissionID); err != nil {
				return err
			}
		}
		if t.SubmittedSubmissionID != nil {
			if d.LatestSubmit, err = store.SubmissionByID(ctx, tx, *t.SubmittedSubmissionID); err != nil {
				return err
			}
		}
		if t.PeriodBlock {
			fi, err := s.forms(a.p.TenantID).get(ctx, t.VersionID)
			if err != nil {
				return err
			}
			if d.Carried, err = carried(ctx, tx, fi, *t, 0); err != nil {
				return err
			}
		}
		return nil
	})
	return d, err
}

// SubmissionInput là một lần Lưu nháp / Gửi báo cáo.
type SubmissionInput struct {
	ID        *uuid.UUID // client ngoại tuyến tự sinh (UUIDv7) — gửi lại cùng id trả bản đã có
	Kind      string     // draft | submitted
	Rows      []store.SubRow
	EventTime *time.Time
	DeviceID  string
}

// SubmissionResult là bài nộp đã ghi (hoặc vấn đề tại ô).
type SubmissionResult struct {
	Submission store.Submission
	Task       TaskView
	Issues     []repport.CellIssue
	Replayed   bool
}

// Submit ghi một phiên bản bài nộp BẤT BIẾN (RB-1): "Lưu nháp" luôn ghi (kèm vấn đề), "Gửi báo cáo" chỉ ghi khi
// đạt yêu cầu của mẫu; sau chốt kỳ → từ chối nếu chưa mở lại. Admin nhập thay mang on_behalf_of (FN-AD-04.07).
func (s *Service) Submit(ctx context.Context, taskID uuid.UUID, in SubmissionInput, idem string) (SubmissionResult, error) {
	a, err := s.writer(ctx, port.SessionRoleCommune)
	if err != nil {
		return SubmissionResult{}, err
	}
	if in.Kind != domain.KindDraft && in.Kind != domain.KindSubmitted {
		return SubmissionResult{}, problem.Validation.WithField("kind", "draft | submitted")
	}
	if len(in.Rows) == 0 || len(in.Rows) > 500 {
		return SubmissionResult{}, problem.Validation.WithField("rows", "1–500 dòng")
	}
	sessionID, supportReason, err := a.supportInfo()
	if err != nil {
		return SubmissionResult{}, err
	}
	now := s.now()
	evTime := now
	if in.EventTime != nil {
		if in.EventTime.After(now.Add(5 * time.Minute)) {
			return SubmissionResult{}, problem.Validation.WithField("event_time", "không được ở tương lai")
		}
		evTime = in.EventTime.UTC()
	}
	subID := ids.New()
	if in.ID != nil {
		if _, err := ids.Parse(in.ID.String()); err != nil {
			return SubmissionResult{}, problem.Validation.WithField("id", "phải là UUIDv7")
		}
		subID = *in.ID
	}
	var out SubmissionResult
	fc := s.forms(a.p.TenantID)
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if in.ID != nil {
			old, err := store.SubmissionByID(ctx, tx, subID)
			if err != nil {
				return err
			}
			if old != nil {
				if old.TaskID != taskID {
					return problem.Conflict.WithField("id", "id đã dùng cho công việc khác")
				}
				t, err := s.taskInScope(ctx, tx, a, taskID, false)
				if err != nil {
					return err
				}
				p, err := store.PeriodByID(ctx, tx, t.PeriodID, false)
				if err != nil {
					return err
				}
				out = SubmissionResult{Submission: *old, Task: viewOf(*t, *p, nil, now), Replayed: true}
				return nil
			}
		}
		t, err := s.taskInScope(ctx, tx, a, taskID, true)
		if err != nil {
			return err
		}
		p, err := store.PeriodByID(ctx, tx, t.PeriodID, false)
		if err != nil {
			return err
		}
		st := domain.TaskState{Status: t.Status, DueAt: t.DueAt, PeriodClosed: p.Status == "closed", ReopenedUntil: t.ReopenedUntil}
		if !st.Open(now) {
			return ErrPeriodClosed
		}
		fi, err := fc.get(ctx, t.VersionID)
		if err != nil {
			return err
		}
		for i, r := range in.Rows {
			if fi.hasInd && r.Indicator == "" {
				return problem.Validation.WithField("rows", "mẫu có dòng chỉ tiêu: mỗi dòng cần indicator")
			}
			in.Rows[i].Key = rowKey(t.CommuneCode, r.Indicator)
		}
		pv, err := store.ProvincialValues(ctx, tx, t.BatchID, &t.TemplateID)
		if err != nil {
			return err
		}
		names, err := s.regionNames(ctx, t.ProvinceCode)
		if err != nil {
			return err
		}
		system := systemCells(fi, names.commune(ctx, s, t.CommuneCode), names.region(t.RegionCode), pv[t.TemplateID][t.CommuneCode])
		xr, err := xrefs(ctx, tx, fi, t.PeriodID, t.CommuneCode)
		if err != nil {
			return err
		}
		carry, err := carried(ctx, tx, fi, *t, 0)
		if err != nil {
			return err
		}
		mode := repport.EvalDraft
		if in.Kind == domain.KindSubmitted {
			mode = repport.EvalSubmit
		}
		rows, issues, valid := s.evalRows(fi, mode, *t, in.Rows, carry, system, xr, t.RegionCode)
		if in.Kind == domain.KindSubmitted && !valid {
			out.Issues = issues
			return errStop
		}
		issuesJSON, err := json.Marshal(issues)
		if err != nil {
			return err
		}
		sub := store.Submission{ID: subID, TaskID: t.ID, VersionNo: t.LatestVersion + 1, FormVersionID: t.VersionID, Kind: in.Kind, Rows: rows,
			Issues: issuesJSON, PreviousID: t.LatestSubmissionID, Commune: t.CommuneCode, Department: t.DepartmentCode, OnBehalfOf: a.onBehalf(),
			SupportSessionID: sessionID, SupportReason: supportReason, EventTime: evTime, ActorRef: a.p.ActorRef, IdemKey: idemOr(idem, "sub")}
		sub.AdjustmentResponse = in.Kind == domain.KindSubmitted && t.Status == domain.TaskAdjustmentRequested
		if in.DeviceID != "" {
			d := in.DeviceID
			sub.DeviceID = &d
		}
		if err := store.InsertSubmission(ctx, tx, a.p.TenantID, &sub); err != nil {
			return err
		}
		t.LatestVersion, t.LatestSubmissionID = sub.VersionNo, &sub.ID
		action := "draft_saved"
		if in.Kind == domain.KindSubmitted {
			action = "submitted"
			t.Status, t.SubmittedSubmissionID, t.SubmittedAt, t.SubmittedBy, t.OnBehalfOf = domain.TaskSubmitted, &sub.ID, &now, &a.p.ActorRef, sub.OnBehalfOf
			if t.FirstSubmittedAt == nil {
				t.FirstSubmittedAt = &now
			}
			t.ConfirmedBy, t.ConfirmedAt = nil, nil
		} else if t.Status == domain.TaskNotStarted {
			t.Status = domain.TaskDraft
		}
		if err := store.SaveSubmissionState(ctx, tx, *t, now); err != nil {
			return err
		}
		if err := store.InsertTaskEvents(ctx, tx, a.p.TenantID, []store.TaskEvent{{ID: ids.New(), TaskID: t.ID, Action: action, StatusAfter: t.Status,
			SubmissionID: &sub.ID, Reason: supportReason, OnBehalfOf: sub.OnBehalfOf, EventTime: now, ActorRef: a.p.ActorRef, IdemKey: idemOr(idem, "task")}}); err != nil {
			return err
		}
		if in.Kind == domain.KindSubmitted {
			if err := outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: a.p.TenantID, Type: port.EventSubmissionSubmitted, SchemaVersion: 1,
				OccurredAt: now, Producer: port.EventProducerName, Payload: port.SubmissionSubmittedV1{SubmissionID: sub.ID.String(), TaskID: t.ID.String(),
					PeriodID: t.PeriodID.String(), TemplateID: t.TemplateID.String(), FormCode: t.FormCode, CommuneCode: t.CommuneCode,
					DepartmentCode: t.DepartmentCode, ProvinceCode: t.ProvinceCode, VersionNo: sub.VersionNo, AdjustmentResponse: sub.AdjustmentResponse,
					OnBehalfOf: ptrString(sub.OnBehalfOf), ActorRef: a.p.ActorRef.String(), EventTime: ts(now)}}); err != nil {
				return err
			}
		}
		out = SubmissionResult{Submission: sub, Task: viewOf(*t, *p, nil, now), Issues: issues}
		return nil
	})
	if errors.Is(err, errStop) {
		return out, ErrSubmissionInvalid.With("issues", out.Issues)
	}
	return out, err
}

// History trả mọi phiên bản bài nộp của công việc ("Lịch sử sửa": người, thời điểm, giá trị cũ – mới).
func (s *Service) History(ctx context.Context, taskID uuid.UUID) ([]store.Submission, map[uuid.UUID]string, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return nil, nil, err
	}
	var subs []store.Submission
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if _, err := s.taskInScope(ctx, tx, a, taskID, false); err != nil {
			return err
		}
		var err error
		subs, err = store.SubmissionHistory(ctx, tx, taskID)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	refs := []uuid.UUID{}
	for _, s := range subs {
		refs = append(refs, s.ActorRef)
		if s.OnBehalfOf != nil {
			refs = append(refs, *s.OnBehalfOf)
		}
	}
	return subs, s.names(ctx, refs), nil
}

// TaskLog trả khung Log của công việc: gửi, lưu nháp, nhắc, yêu cầu điều chỉnh, xác nhận, chốt/mở lại.
func (s *Service) TaskLog(ctx context.Context, taskID uuid.UUID) ([]LogEntry, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return nil, err
	}
	var evs []store.TaskEvent
	var rems []store.Reminder
	var batch *store.Batch
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		t, err := s.taskInScope(ctx, tx, a, taskID, false)
		if err != nil {
			return err
		}
		if evs, err = store.TaskEvents(ctx, tx, taskID); err != nil {
			return err
		}
		all, err := store.Reminders(ctx, tx, t.BatchID, &t.PeriodID, 200)
		if err != nil {
			return err
		}
		for _, r := range all {
			if slices.Contains(r.TaskIDs, taskID) {
				rems = append(rems, r)
			}
		}
		batch, err = store.BatchByID(ctx, tx, t.BatchID, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	refs := []uuid.UUID{batch.IssuedBy}
	for _, e := range evs {
		refs = append(refs, e.ActorRef)
	}
	for _, r := range rems {
		refs = append(refs, r.ActorRef)
	}
	names := s.names(ctx, refs)
	var out []LogEntry
	for _, e := range evs {
		le := LogEntry{Event: "task." + e.Action, ActorRef: e.ActorRef, ActorDisplay: names[e.ActorRef], At: e.EventTime, Reason: e.Reason,
			OnBehalfOf: e.OnBehalfOf, Detail: e.Payload}
		if e.SubmissionID != nil {
			v := e.SubmissionID.String()
			le.Changes = []Change{{Field: "submission_id", New: &v}}
		}
		out = append(out, le)
	}
	for _, r := range rems {
		msg := r.Message
		out = append(out, LogEntry{Event: "task.reminded." + r.Mode, ActorRef: r.ActorRef, ActorDisplay: names[r.ActorRef], At: r.EventTime, Reason: &msg})
	}
	out = append(out, LogEntry{Event: "batch.issued", ActorRef: batch.IssuedBy, ActorDisplay: names[batch.IssuedBy], At: batch.IssuedAt})
	slices.SortStableFunc(out, func(x, y LogEntry) int { return y.At.Compare(x.At) })
	return out, nil
}
