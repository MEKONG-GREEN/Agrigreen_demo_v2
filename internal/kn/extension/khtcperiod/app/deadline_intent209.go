package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var errDeadlineInterrupted = problem.Conflict.WithDetail("đổi hạn bị gián đoạn — cần đối soát ý định gốc")

type deadlineBase209 struct {
	NativeRef                                      uuid.UUID `json:"native_ref"`
	BeforeDue, BeforeExport, AfterDue, AfterExport time.Time
	Reason                                         string
}
type deadlineData209 struct {
	Base        deadlineBase209           `json:"base"`
	Expected    formsport.PeriodRevision  `json:"expected"`
	Applied     *formsport.PeriodRevision `json:"applied,omitempty"`
	UndoApplied *formsport.PeriodRevision `json:"undo_applied,omitempty"`
}

func deadlineAuthority209(c caller) (string, error) {
	// RequireSession supplied this trusted server projection; no client headers.
	return formsport.OperationHash(struct {
		Tenant, Actor uuid.UUID
		MFA           bool
		Roles         []string
		Owner         any
	}{c.p.TenantID, c.p.ActorRef, c.p.MFA, c.p.Roles, c.k})
}
func sameDeadline209(v *store.DeadlineIntent, c caller, id uuid.UUID, hash, authority string) bool {
	return v != nil && v.PeriodID == id && v.Actor == c.p.ActorRef && v.RequestHash == hash && v.AuthorityHash == authority
}
func revisionIdentity209(v formsport.PeriodRevision, c caller, ref uuid.UUID) bool {
	return v.TenantID == c.p.TenantID && v.Profile == domain.ProfileKHTC && v.PeriodID == ref && v.EventSeq >= 1 && v.DueSeq >= 0
}

func (s *Service) deadlineReplay209(ctx context.Context, c caller, id uuid.UUID, key, hash, authority string) (Detail, bool, error) {
	var v *store.DeadlineIntent
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; v, e = store.DeadlineByKey(ctx, tx, key); return e })
	if err != nil {
		return Detail{}, true, err
	}
	if v == nil {
		return Detail{}, false, nil
	}
	if !sameDeadline209(v, c, id, hash, authority) {
		return Detail{}, true, problem.Conflict.WithDetail("khóa thao tác không khớp yêu cầu/quyền hiện tại")
	}
	if v.Phase != "completed" {
		return Detail{}, true, errDeadlineInterrupted
	}
	var out Detail
	if err = json.Unmarshal(v.Outcome, &out); err != nil || out.Period.ID != id {
		return Detail{}, true, errDeadlineInterrupted
	}
	// The stored result is this operation's result, not a successor's current Get.
	return out, true, nil
}

func (s *Service) deadlinePhase209(ctx context.Context, c caller, v store.DeadlineIntent, data deadlineData209, from, to string, outcome json.RawMessage) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if e := store.LockDeadline(ctx, tx, c.p.TenantID, v.PeriodID, v.RequestKey); e != nil {
			return e
		}
		cur, e := store.DeadlineByOperation(ctx, tx, v.OperationID)
		if e != nil {
			return e
		}
		if cur == nil || cur.Phase != from || !sameDeadline209(cur, c, v.PeriodID, v.RequestHash, v.AuthorityHash) {
			return errDeadlineInterrupted
		}
		v.ID, v.Phase, v.Payload, v.Outcome, v.At = ids.New(), to, payload, outcome, s.now()
		return store.InsertDeadline(ctx, tx, c.p.TenantID, v)
	})
}
func (s *Service) deadlineUnresolved209(ctx context.Context, c caller, v store.DeadlineIntent, data deadlineData209, from string) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_ = s.deadlinePhase209(cleanup, c, v, data, from, "unresolved", nil)
}
func (s *Service) deadlineBoundAuthority209(ctx context.Context, id uuid.UUID, authority string) error {
	c, err := s.periodWriter(ctx, id)
	if err != nil {
		return err
	}
	hash, err := deadlineAuthority209(c)
	if err != nil {
		return err
	}
	if hash != authority {
		return problem.Forbidden.WithDetail("quyền của thao tác đổi hạn đã thay đổi")
	}
	return nil
}

func (s *Service) changeDeadlineCAS209(ctx context.Context, id uuid.UUID, in DeadlineInput, idem string) (Detail, error) {
	if s.ConditionalControl == nil {
		return Detail{}, ErrDependency.WithDetail("chưa nối cổng điều khiển kỳ có điều kiện")
	}
	c, err := s.periodWriter(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	authority, err := deadlineAuthority209(c)
	if err != nil {
		return Detail{}, err
	}
	hash, err := formsport.OperationHash(struct {
		Period      uuid.UUID
		Due, Export time.Time
		Reason      string
	}{id, in.DueAt.UTC(), in.ExportDueAt.UTC(), in.Reason})
	if err != nil {
		return Detail{}, err
	}
	if idem == "" {
		idem = ids.New().String()
	}
	key, err := formsport.OperationHash(idem)
	if err != nil {
		return Detail{}, err
	}
	if out, found, e := s.deadlineReplay209(ctx, c, id, key, hash, authority); found {
		return out, e
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
	if err = domain.ValidateDeadlines(d.Period.Cutoff, in.DueAt, in.ExportDueAt, zone()); err != nil {
		return Detail{}, inputErr(err)
	}
	reason := ""
	if why, ok := domain.ValidReason(in.Reason); ok {
		reason = why
	}
	snapshot, err := s.ConditionalControl.ControlSnapshot(ctx, domain.ProfileKHTC, d.Period.FormsPeriodRef)
	if err != nil {
		return Detail{}, err
	}
	if !revisionIdentity209(snapshot.Revision, c, d.Period.FormsPeriodRef) || !snapshot.Progress.DueAt.Equal(d.DueAt) {
		return Detail{}, errDeadlineInterrupted
	}
	data := deadlineData209{Base: deadlineBase209{NativeRef: d.Period.FormsPeriodRef, BeforeDue: d.DueAt, BeforeExport: d.ExportDueAt, AfterDue: in.DueAt.UTC(), AfterExport: in.ExportDueAt.UTC(), Reason: reason}, Expected: snapshot.Revision}
	payload, err := json.Marshal(data)
	if err != nil {
		return Detail{}, err
	}
	v := store.DeadlineIntent{ID: ids.New(), OperationID: ids.New(), PeriodID: id, Actor: c.p.ActorRef, RequestKey: key, RequestHash: hash, AuthorityHash: authority, Phase: "prepared", Payload: payload, At: s.now()}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if e := store.LockDeadline(ctx, tx, c.p.TenantID, id, key); e != nil {
			return e
		}
		exists, e := store.DeadlineByKey(ctx, tx, key)
		if e != nil {
			return e
		}
		if exists != nil {
			return errDeadlineInterrupted
		}
		pending, e := store.PendingDeadline(ctx, tx, id)
		if e != nil {
			return e
		}
		if pending {
			return errDeadlineInterrupted
		}
		p, e := store.PeriodByID(ctx, tx, id)
		if e != nil {
			return e
		}
		if p == nil {
			return ErrNotFound
		}
		evs, e := store.Events(ctx, tx, []uuid.UUID{id})
		if e != nil {
			return e
		}
		before, export := deadlinesOf(*p, evs[id])
		if terminalOf(evs[id]) != "" || !before.Equal(data.Base.BeforeDue) || !export.Equal(data.Base.BeforeExport) {
			return errDeadlineInterrupted
		}
		return store.InsertDeadline(ctx, tx, c.p.TenantID, v)
	})
	if err != nil {
		return Detail{}, classify(err)
	}
	if err = s.deadlineBoundAuthority209(ctx, id, authority); err != nil {
		s.deadlineUnresolved209(ctx, c, v, data, "prepared")
		return Detail{}, err
	}
	nativeKey := "khtc:deadline209:" + v.OperationID.String()
	applied, err := s.ConditionalControl.ChangeDueCAS(deadlineNative209(ctx, s, c, d.Period, data.Base.AfterDue, reason, nativeKey), domain.ProfileKHTC, data.Base.NativeRef, data.Expected, data.Base.AfterDue, reason, nativeKey)
	if err != nil {
		s.deadlineUnresolved209(ctx, c, v, data, "prepared")
		return Detail{}, err
	}
	if !revisionIdentity209(applied, c, data.Base.NativeRef) {
		s.deadlineUnresolved209(ctx, c, v, data, "prepared")
		return Detail{}, errDeadlineInterrupted
	}
	data.Applied = &applied
	if err = s.deadlinePhase209(ctx, c, v, data, "prepared", "applied", nil); err != nil {
		return Detail{}, errDeadlineInterrupted.WithCause(err)
	}
	event := store.Event{ID: ids.New(), PeriodID: id, Action: store.ActionDeadline, ActorRef: c.p.ActorRef, EventTime: s.now(), DueAt: ptrTime(data.Base.AfterDue), ExportDueAt: ptrTime(data.Base.AfterExport)}
	if reason != "" {
		event.Reason = &reason
	}
	outcome, err := s.deadlineOutcome209(ctx, c, d.Period, event)
	bodySucceeded := false
	if err == nil {
		err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			if e := store.LockDeadline(ctx, tx, c.p.TenantID, id, key); e != nil {
				return e
			}
			current, e := store.DeadlineByOperation(ctx, tx, v.OperationID)
			if e != nil {
				return e
			}
			if current == nil || current.Phase != "applied" || !sameDeadline209(current, c, id, hash, authority) {
				return errDeadlineInterrupted
			}
			if e = store.InsertEvent(ctx, tx, c.p.TenantID, &event, "khtc:deadline209:event:"+v.OperationID.String()); e != nil {
				return e
			}
			outcome.Events[len(outcome.Events)-1] = event // actual committed ledger sequence
			raw, e := json.Marshal(outcome)
			if e != nil {
				return e
			}
			v.ID, v.Phase, v.Outcome, v.At = ids.New(), "completed", raw, s.now()
			v.Payload, e = json.Marshal(data)
			if e != nil {
				return e
			}
			if e = store.InsertDeadline(ctx, tx, c.p.TenantID, v); e != nil {
				return e
			}
			bodySucceeded = true
			return nil
		})
	}
	if err == nil {
		return outcome, nil
	}
	if bodySucceeded { // Commit may have succeeded: never undo an unknown local commit.
		s.deadlineUnresolved209(ctx, c, v, data, "applied")
		return Detail{}, errDeadlineInterrupted.WithCause(err)
	}
	return Detail{}, s.undoDeadline209(ctx, c, v, data, err)
}

func deadlineNative209(ctx context.Context, s *Service, c caller, p store.Period, due time.Time, reason, key string) context.Context {
	if p.Type != domain.TypeAdhoc {
		return ctx
	}
	hash, _ := formsport.OperationHash(struct {
		Due    time.Time
		Reason string
	}{due.UTC(), reason})
	return BindAdhocOperation(ctx, s.DB, c.k, p.ID, formsport.Operation{Action: formsport.OperationChangeDue, Profile: domain.ProfileKHTC, PeriodID: p.FormsPeriodRef, PayloadHash: hash, IdemKey: key})
}

// All projection I/O occurs before the final own transaction. Only the sequence
// returned by InsertEvent is filled inside it before freezing this response.
func (s *Service) deadlineOutcome209(ctx context.Context, c caller, p store.Period, event store.Event) (Detail, error) {
	if err := s.deadlineBoundAuthority209(ctx, p.ID, mustAuthority209(c)); err != nil {
		return Detail{}, err
	}
	_, evs, srcs, err := s.load(ctx, c, p.ID)
	if err != nil {
		return Detail{}, err
	}
	if terminalOf(evs) != "" {
		return Detail{}, ErrTerminal
	}
	requests, err := s.activeRequests(ctx, c, p)
	if err != nil {
		return Detail{}, err
	}
	names, _, _ := s.unitDepartments(ctx, c.k)
	return s.project(ctx, c, p, append(evs, event), srcs, names, requests), nil
}
func mustAuthority209(c caller) string { hash, _ := deadlineAuthority209(c); return hash }

func (s *Service) undoDeadline209(ctx context.Context, c caller, v store.DeadlineIntent, data deadlineData209, cause error) error {
	if data.Applied == nil {
		return errDeadlineInterrupted.WithCause(cause)
	}
	if err := s.deadlineBoundAuthority209(ctx, v.PeriodID, v.AuthorityHash); err != nil {
		s.deadlineUnresolved209(ctx, c, v, data, "applied")
		return errors.Join(cause, errDeadlineInterrupted.WithCause(err))
	}
	if err := s.deadlinePhase209(ctx, c, v, data, "applied", "undo_prepared", nil); err != nil {
		return errors.Join(cause, errDeadlineInterrupted.WithCause(err))
	}
	p, _, _, err := s.load(ctx, c, v.PeriodID)
	if err != nil {
		s.deadlineUnresolved209(ctx, c, v, data, "undo_prepared")
		return errors.Join(cause, errDeadlineInterrupted.WithCause(err))
	}
	key := "khtc:deadline209:undo:" + v.OperationID.String()
	reason := "Hoàn tác: ghi sổ KHTC không thành công"
	undone, err := s.ConditionalControl.ChangeDueCAS(deadlineNative209(ctx, s, c, p, data.Base.BeforeDue, reason, key), domain.ProfileKHTC, data.Base.NativeRef, *data.Applied, data.Base.BeforeDue, reason, key)
	if err != nil {
		s.deadlineUnresolved209(ctx, c, v, data, "undo_prepared")
		return errors.Join(cause, errDeadlineInterrupted.WithCause(err))
	}
	if !revisionIdentity209(undone, c, data.Base.NativeRef) {
		s.deadlineUnresolved209(ctx, c, v, data, "undo_prepared")
		return errors.Join(cause, errDeadlineInterrupted)
	}
	data.UndoApplied = &undone
	if err = s.deadlinePhase209(ctx, c, v, data, "undo_prepared", "undone", nil); err != nil {
		return errors.Join(cause, errDeadlineInterrupted.WithCause(err))
	}
	return cause
}
