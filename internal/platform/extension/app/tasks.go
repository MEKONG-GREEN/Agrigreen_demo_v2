package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// CreateTaskInput là dữ liệu vào của CreateTask.
type CreateTaskInput struct {
	ID             *uuid.UUID // UUIDv7 sinh trên máy khi ngoại tuyến; nil → server sinh
	CommuneCode    string
	CooperativeRef uuid.UUID
	HouseholdRef   uuid.UUID // chỉ mã tham chiếu (R-41)
	PlotRef        uuid.UUID
	Category       string
	Description    string
	Priority       string
	AssigneeRef    uuid.UUID // tùy chọn: giao ngay khi tạo
	IdemKey        string
}

// TaskResult là kết quả một thao tác ghi nhiệm vụ.
type TaskResult struct {
	Task     domain.Task
	Event    domain.Event
	Replayed bool
}

// CreateTask tạo nhiệm vụ hỗ trợ: due_at = lúc tạo + SLA tra theo thời điểm tạo
// (tham số extension.task_sla_hours, mặc định 24 giờ — US-TASK-02 AC-2).
func (s *Service) CreateTask(ctx context.Context, in CreateTaskInput) (TaskResult, error) {
	p, err := authz.Require(ctx, taskCreators...)
	if err != nil {
		return TaskResult{}, err
	}
	if in.IdemKey == "" {
		return TaskResult{}, problem.IdempotencyRequired
	}
	nt := domain.NewTaskInput{CommuneCode: in.CommuneCode, Category: in.Category, Description: in.Description, Priority: in.Priority}
	if err := nt.Validate(); err != nil {
		return TaskResult{}, fieldProblem(err)
	}
	source := domain.SourceOfficer
	if p.Has(port.RoleCoopManager) && !p.Has(port.RoleKNOfficer) && !p.Has(port.RoleKNProvinceAdmin) {
		// Quản lý HTX chỉ tạo yêu cầu cho chính HTX mình.
		source = domain.SourceCoopManager
		if in.CooperativeRef == uuid.Nil {
			in.CooperativeRef = p.OrgRef
		}
		if in.CooperativeRef != p.OrgRef || p.OrgRef == uuid.Nil {
			return TaskResult{}, ErrOutOfScope.WithDetail("quản lý HTX chỉ tạo yêu cầu cho HTX của mình")
		}
		if in.AssigneeRef != uuid.Nil {
			return TaskResult{}, problem.Validation.WithField("assignee_ref", "quản lý HTX không phân công cán bộ")
		}
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return TaskResult{}, err
	}
	if !sc.Allows(nt.CommuneCode) {
		return TaskResult{}, ErrOutOfScope
	}
	province, level, err := s.provinceOf(ctx, nt.CommuneCode, "commune_code")
	if err != nil {
		return TaskResult{}, err
	}
	if level != "commune" {
		return TaskResult{}, ErrUnitInvalid.WithField("commune_code", "nhiệm vụ gắn với một xã")
	}
	if err := s.checkCooperative(ctx, in.CooperativeRef, nt.CommuneCode); err != nil {
		return TaskResult{}, err
	}
	now := s.now()
	sla, err := s.paramDecimal(ctx, port.ParamTaskSLAHours, now, domain.DefaultSLAHours)
	if err != nil {
		return TaskResult{}, err
	}
	if !domain.ValidSLA(sla) {
		sla = domain.DefaultSLAHours
	}
	id := ids.New()
	if in.ID != nil {
		id = *in.ID
	}
	t := domain.Task{
		ID: id, CommuneCode: nt.CommuneCode, ProvinceCode: province, CooperativeRef: in.CooperativeRef,
		HouseholdRef: in.HouseholdRef, PlotRef: in.PlotRef, Category: nt.Category, Description: nt.Description,
		Priority: nt.Priority, Source: source, Status: domain.StatusNew, CreatedBy: p.ActorRef, CreatedAt: now,
		SLAHours: sla, DueAt: domain.DueAt(now, sla), EscalatedUnit: province, EscalationOrgRef: s.provinceOrgOf(ctx, p.OrgRef),
	}
	if in.AssigneeRef != uuid.Nil {
		t.Status, t.AssigneeRef = domain.StatusAssigned, in.AssigneeRef
	}
	return s.writeTask(ctx, p.TenantID, in.IdemKey, func(tx pgx.Tx) (TaskResult, error) {
		if in.ID != nil {
			if prev, err := store.TaskByID(ctx, tx, id, false); err != nil {
				return TaskResult{}, err
			} else if prev != nil {
				return TaskResult{}, ErrIDReuse
			}
		}
		if t.AssigneeRef != uuid.Nil {
			if err := checkAssignee(ctx, tx, t, t.AssigneeRef); err != nil {
				return TaskResult{}, err
			}
		}
		ev := domain.Event{
			ID: ids.New(), TenantID: p.TenantID, TaskID: t.ID, Action: domain.ActionCreated, StatusAfter: t.Status,
			AssigneeRef: t.AssigneeRef, DueAt: t.DueAt, EscalatedUnit: province, EscalationOrgRef: t.EscalationOrgRef,
			Snapshot: snapshot(t), EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey,
		}
		if err := store.InsertEvent(ctx, tx, &ev); err != nil {
			return TaskResult{}, err
		}
		t.LastSeq = ev.Seq
		if err := store.InsertTask(ctx, tx, p.TenantID, t, ev); err != nil {
			return TaskResult{}, err
		}
		cur, err := store.TaskByID(ctx, tx, t.ID, false)
		if err != nil || cur == nil {
			return TaskResult{}, errors.Join(errors.New("extension: không đọc lại được nhiệm vụ vừa tạo"), err)
		}
		return TaskResult{Task: *cur, Event: ev}, nil
	})
}

func snapshot(t domain.Task) map[string]any {
	m := map[string]any{
		"commune_code": t.CommuneCode, "province_code": t.ProvinceCode, "category": t.Category,
		"description": t.Description, "priority": t.Priority, "source": t.Source, "sla_hours": t.SLAHours.String(),
	}
	for k, v := range map[string]uuid.UUID{"cooperative_ref": t.CooperativeRef, "household_ref": t.HouseholdRef, "plot_ref": t.PlotRef} {
		if v != uuid.Nil {
			m[k] = v.String()
		}
	}
	return m
}

// checkAssignee: người được giao phải là cán bộ đang hoạt động trong danh bạ, phụ
// trách xã của nhiệm vụ hoặc thuộc TTKN tỉnh chứa xã đó.
func checkAssignee(ctx context.Context, tx pgx.Tx, t domain.Task, assignee uuid.UUID) error {
	st, err := store.StaffByActor(ctx, tx, assignee)
	if err != nil {
		return err
	}
	if st == nil || !st.Active {
		return ErrAssigneeInvalid.WithDetail("cán bộ chưa có trong danh bạ hoặc đã ngừng")
	}
	if st.UnitCode != t.CommuneCode && st.UnitCode != t.ProvinceCode {
		return ErrAssigneeInvalid
	}
	return nil
}

// replayTask đọc lại kết quả của lần ghi trước cùng idem_key.
func replayTask(ctx context.Context, tx pgx.Tx, key string) (*TaskResult, error) {
	ev, err := store.EventByIdemKey(ctx, tx, key)
	if err != nil || ev == nil {
		return nil, err
	}
	cur, err := store.TaskByID(ctx, tx, ev.TaskID, false)
	if err != nil || cur == nil {
		return nil, err
	}
	return &TaskResult{Task: *cur, Event: *ev, Replayed: true}, nil
}

// writeTask chạy một thao tác ghi sổ nhiệm vụ có chống lặp theo idem_key.
func (s *Service) writeTask(ctx context.Context, tid uuid.UUID, key string, fn func(tx pgx.Tx) (TaskResult, error)) (TaskResult, error) {
	var res TaskResult
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		if prev, err := replayTask(ctx, tx, key); err != nil {
			return err
		} else if prev != nil {
			res = *prev
			return nil
		}
		var err error
		res, err = fn(tx)
		return err
	})
	err = ledger.Classify(err)
	if errors.Is(err, ledger.ErrDuplicate) {
		// Hai request cùng khóa chạy song song: bên thua đọc lại bản ghi của bên thắng.
		var prev *TaskResult
		rerr := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
			var e error
			prev, e = replayTask(ctx, tx, key)
			return e
		})
		if rerr == nil && prev != nil {
			return *prev, nil
		}
	}
	return res, err
}

// UpdateInput là một sự việc trên nhiệm vụ đã có (API hoặc bản ghi đồng bộ task_update).
type UpdateInput struct {
	EventID     *uuid.UUID // id bản ghi do máy sinh (UUIDv7); nil → server sinh
	TaskID      uuid.UUID
	Action      domain.Action
	Note        string
	AssigneeRef uuid.UUID  // action = assigned
	EventTime   *time.Time // thời điểm tại hiện trường (đồng bộ ngoại tuyến); nil = hiện tại
	DeviceID    *string
	IdemKey     string
}

// UpdateTask ghi một sự việc: phân công, tiếp nhận, tiến độ, hoàn thành, chuyển
// tiếp lên tỉnh, mở lại.
func (s *Service) UpdateTask(ctx context.Context, in UpdateInput) (TaskResult, error) {
	p, err := authz.Require(ctx, taskCreators...)
	if err != nil {
		return TaskResult{}, err
	}
	if in.IdemKey == "" {
		return TaskResult{}, problem.IdempotencyRequired
	}
	if in.Action == domain.ActionCreated || in.Action == "" {
		return TaskResult{}, problem.Validation.WithField("action", "không hỗ trợ")
	}
	in.Note = strings.TrimSpace(in.Note)
	if err := domain.ContainsPIINote(in.Note); err != nil {
		return TaskResult{}, fieldProblem(err)
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return TaskResult{}, err
	}
	now := s.now()
	eventTime := now
	if in.EventTime != nil && !in.EventTime.IsZero() && !in.EventTime.After(now) {
		eventTime = in.EventTime.UTC()
	}
	var reopenSLA = domain.DefaultSLAHours
	if in.Action == domain.ActionReopened {
		if reopenSLA, err = s.paramDecimal(ctx, port.ParamTaskSLAHours, now, domain.DefaultSLAHours); err != nil {
			return TaskResult{}, err
		}
		if !domain.ValidSLA(reopenSLA) {
			reopenSLA = domain.DefaultSLAHours
		}
	}
	return s.writeTask(ctx, p.TenantID, in.IdemKey, func(tx pgx.Tx) (TaskResult, error) {
		cur, err := store.TaskByID(ctx, tx, in.TaskID, true)
		if err != nil {
			return TaskResult{}, err
		}
		if cur == nil || !canSee(p, sc, *cur) {
			return TaskResult{}, ErrTaskNotFound
		}
		if err := authorizeAction(p, *cur, in.Action); err != nil {
			return TaskResult{}, err
		}
		if in.Action == domain.ActionEscalated && len([]rune(in.Note)) < domain.MinNoteLen {
			return TaskResult{}, problem.Validation.WithField("note", "ghi rõ lý do chuyển tiếp lên cấp tỉnh (≥ 10 ký tự)")
		}
		next, err := domain.Transition(cur.Status, in.Action, in.Note, cur.AssigneeRef != uuid.Nil)
		if errors.Is(err, domain.ErrTransition) {
			return TaskResult{}, ErrTaskState.WithDetail(fmt.Sprintf("không thể %s khi nhiệm vụ đang ở trạng thái %s", in.Action, cur.Status))
		}
		if err != nil {
			return TaskResult{}, fieldProblem(err)
		}
		t := *cur
		t.Status = next
		if in.Note != "" {
			t.LastNote = in.Note
		}
		switch in.Action {
		case domain.ActionAssigned:
			if in.AssigneeRef == uuid.Nil {
				return TaskResult{}, problem.Validation.WithField("assignee_ref", "bắt buộc")
			}
			if err := checkAssignee(ctx, tx, t, in.AssigneeRef); err != nil {
				return TaskResult{}, err
			}
			t.AssigneeRef = in.AssigneeRef
		case domain.ActionResolved:
			rt := eventTime
			t.ResolvedAt = &rt
		case domain.ActionEscalated:
			at := now
			t.EscalatedAt = &at
		case domain.ActionReopened:
			t.ResolvedAt, t.EscalatedAt, t.SLAHours = nil, nil, reopenSLA
			t.DueAt = domain.DueAt(now, reopenSLA)
		}
		evID := ids.New()
		if in.EventID != nil {
			if prev, err := store.EventByID(ctx, tx, *in.EventID); err != nil {
				return TaskResult{}, err
			} else if prev != nil {
				return TaskResult{}, ErrIDReuse
			}
			evID = *in.EventID
		}
		ev := domain.Event{
			ID: evID, TenantID: p.TenantID, TaskID: t.ID, Action: in.Action, StatusAfter: t.Status,
			AssigneeRef: t.AssigneeRef, DueAt: t.DueAt, Note: in.Note, EventTime: eventTime, ActorRef: p.ActorRef,
			DeviceID: in.DeviceID, IdemKey: in.IdemKey,
		}
		if in.Action == domain.ActionEscalated {
			ev.EscalatedUnit, ev.EscalationOrgRef = t.EscalatedUnit, t.EscalationOrgRef
		}
		if err := store.InsertEvent(ctx, tx, &ev); err != nil {
			return TaskResult{}, err
		}
		t.LastSeq = ev.Seq
		if err := store.UpdateTask(ctx, tx, t, ev); err != nil {
			return TaskResult{}, err
		}
		if in.Action == domain.ActionEscalated {
			if err := enqueueEscalated(ctx, tx, t, ev, port.EscalationReasonManual); err != nil {
				return TaskResult{}, err
			}
		}
		out, err := store.TaskByID(ctx, tx, t.ID, false)
		if err != nil || out == nil {
			return TaskResult{}, errors.Join(errors.New("extension: không đọc lại được nhiệm vụ"), err)
		}
		return TaskResult{Task: *out, Event: ev}, nil
	})
}

// canSee: nhiệm vụ trong phạm vi xã/tỉnh của người gọi, hoặc được giao cho người
// gọi; quản lý HTX chỉ thấy nhiệm vụ của HTX mình.
func canSee(p authz.Principal, sc authz.Scope, t domain.Task) bool {
	if onlyCoopManager(p) {
		return t.CooperativeRef != uuid.Nil && t.CooperativeRef == p.OrgRef && visible(sc, t.CommuneCode, t.ProvinceCode)
	}
	return visible(sc, t.CommuneCode, t.ProvinceCode) || (t.AssigneeRef != uuid.Nil && t.AssigneeRef == p.ActorRef)
}

func onlyCoopManager(p authz.Principal) bool {
	return p.Has(port.RoleCoopManager) && !p.Has(port.RoleKNOfficer) && !p.Has(port.RoleKNProvinceAdmin) &&
		!p.Has(port.RoleKNNationalAdmin) && !p.Has(authz.RolePlatformAdmin)
}

func isSupervisor(p authz.Principal) bool {
	return p.Has(port.RoleKNProvinceAdmin) || p.Has(port.RoleKNNationalAdmin) || p.Has(authz.RolePlatformAdmin)
}

// authorizeAction kiểm ai được ghi sự việc nào.
func authorizeAction(p authz.Principal, t domain.Task, a domain.Action) error {
	isAssignee := t.AssigneeRef != uuid.Nil && t.AssigneeRef == p.ActorRef
	switch a {
	case domain.ActionAssigned:
		if isSupervisor(p) || p.Has(port.RoleKNOfficer) {
			return nil
		}
	case domain.ActionAccepted, domain.ActionProgress:
		if isAssignee {
			return nil
		}
		return ErrNotAssignee
	case domain.ActionResolved, domain.ActionEscalated:
		if isAssignee || isSupervisor(p) {
			return nil
		}
		return ErrNotAssignee
	case domain.ActionReopened:
		// Người yêu cầu (vd quản lý HTX) hoặc cấp giám sát mở lại khi chưa đạt.
		if isSupervisor(p) || t.CreatedBy == p.ActorRef {
			return nil
		}
	}
	return problem.Forbidden
}

// BulkAssignInput: phân công nhiều nhiệm vụ cho một hoặc nhiều cán bộ trong một lần xác nhận (US-TASK-04).
type BulkAssignInput struct {
	TaskIDs      []uuid.UUID
	AssigneeRefs []uuid.UUID // chia vòng tròn theo thứ tự nhiệm vụ
	IdemKey      string
}

// BulkAssign phân công hàng loạt trong MỘT giao dịch: một nhiệm vụ lỗi → không nhiệm vụ nào đổi.
func (s *Service) BulkAssign(ctx context.Context, in BulkAssignInput) ([]TaskResult, error) {
	p, err := authz.Require(ctx, supervisors...)
	if err != nil {
		return nil, err
	}
	if in.IdemKey == "" {
		return nil, problem.IdempotencyRequired
	}
	if len(in.TaskIDs) == 0 || len(in.TaskIDs) > 200 {
		return nil, problem.Validation.WithField("task_ids", "chọn 1–200 nhiệm vụ")
	}
	if len(in.AssigneeRefs) == 0 || len(in.AssigneeRefs) > 50 {
		return nil, problem.Validation.WithField("assignee_refs", "chọn 1–50 cán bộ")
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range in.TaskIDs {
		if seen[id] {
			return nil, problem.Validation.WithField("task_ids", "nhiệm vụ bị lặp")
		}
		seen[id] = true
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var out []TaskResult
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		out = out[:0]
		for i, id := range in.TaskIDs {
			key := fmt.Sprintf("%s:%d", in.IdemKey, i)
			if prev, err := replayTask(ctx, tx, key); err != nil {
				return err
			} else if prev != nil {
				out = append(out, *prev)
				continue
			}
			cur, err := store.TaskByID(ctx, tx, id, true)
			if err != nil {
				return err
			}
			if cur == nil || !visible(sc, cur.CommuneCode, cur.ProvinceCode) {
				return ErrTaskNotFound.With("task_id", id.String())
			}
			next, err := domain.Transition(cur.Status, domain.ActionAssigned, "", true)
			if err != nil {
				return ErrTaskState.With("task_id", id.String())
			}
			assignee := in.AssigneeRefs[i%len(in.AssigneeRefs)]
			if err := checkAssignee(ctx, tx, *cur, assignee); err != nil {
				return problem.From(err).With("task_id", id.String())
			}
			t := *cur
			t.Status, t.AssigneeRef = next, assignee
			ev := domain.Event{
				ID: ids.New(), TenantID: p.TenantID, TaskID: t.ID, Action: domain.ActionAssigned, StatusAfter: next,
				AssigneeRef: assignee, DueAt: t.DueAt, EventTime: now, ActorRef: p.ActorRef, IdemKey: key,
			}
			if err := store.InsertEvent(ctx, tx, &ev); err != nil {
				return err
			}
			t.LastSeq = ev.Seq
			if err := store.UpdateTask(ctx, tx, t, ev); err != nil {
				return err
			}
			res, err := store.TaskByID(ctx, tx, t.ID, false)
			if err != nil {
				return err
			}
			out = append(out, TaskResult{Task: *res, Event: ev})
		}
		return nil
	})
	return out, ledger.Classify(err)
}

// TaskDetail là nhiệm vụ kèm lịch sử.
type TaskDetail struct {
	Task   domain.Task
	Events []domain.Event
}

// GetTask đọc một nhiệm vụ; ngoài phạm vi → 404 (không lộ sự tồn tại).
func (s *Service) GetTask(ctx context.Context, id uuid.UUID) (TaskDetail, error) {
	p, err := authz.Require(ctx, taskReaders...)
	if err != nil {
		return TaskDetail{}, err
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return TaskDetail{}, err
	}
	var d TaskDetail
	var found bool
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		t, err := store.TaskByID(ctx, tx, id, false)
		if err != nil || t == nil || !canSee(p, sc, *t) {
			return err
		}
		found, d.Task = true, *t
		d.Events, err = store.TaskEvents(ctx, tx, id)
		return err
	})
	if err != nil {
		return TaskDetail{}, err
	}
	if !found {
		return TaskDetail{}, ErrTaskNotFound
	}
	return d, nil
}

// TaskQuery là tham số liệt kê.
type TaskQuery struct {
	Status   string
	Priority string
	Assignee uuid.UUID
	Mine     bool
	Overdue  bool
	After    uuid.UUID
	Limit    int
}

// TaskPage là một trang nhiệm vụ.
type TaskPage struct {
	Items []domain.Task
	Next  *uuid.UUID
}

// ListTasks liệt kê nhiệm vụ trong phạm vi (US-TASK-03: lọc trạng thái, ưu tiên, cán bộ).
func (s *Service) ListTasks(ctx context.Context, q TaskQuery) (TaskPage, error) {
	p, err := authz.Require(ctx, taskReaders...)
	if err != nil {
		return TaskPage{}, err
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	if q.Status != "" && !validStatus(q.Status) {
		return TaskPage{}, problem.Validation.WithField("status", "không hợp lệ")
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return TaskPage{}, err
	}
	f := store.TaskFilter{
		All: sc.All, Units: sc.Units, Status: q.Status, Priority: q.Priority, AssigneeRef: q.Assignee,
		Overdue: q.Overdue, Now: s.now(), After: q.After, Limit: q.Limit + 1,
	}
	if q.Mine {
		f.AssigneeRef = p.ActorRef
	}
	if onlyCoopManager(p) {
		if p.OrgRef == uuid.Nil {
			return TaskPage{Items: []domain.Task{}}, nil
		}
		f.Cooperative = p.OrgRef
	}
	if sc.Empty() && !q.Mine {
		return TaskPage{Items: []domain.Task{}}, nil
	}
	if q.Mine {
		// Nhiệm vụ được giao cho chính mình: hiển thị cả khi phạm vi đổi.
		f.All = true
	}
	var items []domain.Task
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListTasks(ctx, tx, f)
		return e
	})
	if err != nil {
		return TaskPage{}, err
	}
	pg := TaskPage{Items: items}
	if len(items) > q.Limit {
		pg.Items = items[:q.Limit]
		last := pg.Items[q.Limit-1].ID
		pg.Next = &last
	}
	if pg.Items == nil {
		pg.Items = []domain.Task{}
	}
	return pg, nil
}

func validStatus(s string) bool {
	switch domain.Status(s) {
	case domain.StatusNew, domain.StatusAssigned, domain.StatusAccepted, domain.StatusInProgress, domain.StatusResolved:
		return true
	}
	return false
}

func enqueueEscalated(ctx context.Context, tx pgx.Tx, t domain.Task, ev domain.Event, reason string) error {
	payload := port.TaskEscalatedV1{
		TaskID: t.ID, CommuneCode: t.CommuneCode, ProvinceCode: t.ProvinceCode, Reason: reason, Status: string(t.Status),
		Priority: t.Priority, Category: t.Category, DueAt: t.DueAt.UTC().Format(time.RFC3339),
		EventTime: ev.EventTime.UTC().Format(time.RFC3339), Seq: ev.Seq,
	}
	if t.EscalationOrgRef != uuid.Nil {
		r := t.EscalationOrgRef
		payload.EscalationOrgRef = &r
	}
	if t.AssigneeRef != uuid.Nil {
		a := t.AssigneeRef
		payload.AssigneeRef = &a
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: ev.TenantID, Type: port.EventTaskEscalated, SchemaVersion: 1,
		OccurredAt: ev.EventTime, TraceID: telemetry.TraceID(ctx), Producer: port.EventProducerName, Payload: payload,
	})
}
