package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var errCloseInterrupted211 = problem.Conflict.WithDetail("đóng kỳ chưa xác định kết quả — đối soát thao tác gốc")

type CloseGuard211 interface {
	BeginClose211(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
	FinishClose211(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, func(pgx.Tx) error) error
	RejectClose211(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, func(pgx.Tx) error) error
}
type closeBase211 struct {
	Detail Detail
	Event  store.Event
}
type closeData211 struct {
	Base           closeBase211                  `json:"base"`
	Native         forms.PeriodOperationRequest  `json:"native"`
	Receipt        *forms.PeriodOperationReceipt `json:"receipt,omitempty"`
	Progress       *forms.PeriodProgress         `json:"progress,omitempty"`
	SubmissionRefs map[string]uuid.UUID          `json:"submission_refs,omitempty"`
	Rejection      string                        `json:"rejection,omitempty"`
}

func closeAuthority211(c caller) (string, error) {
	roles := slices.Clone(c.p.Roles)
	slices.Sort(roles)
	k := c.k
	k.Communes = slices.Clone(k.Communes)
	slices.Sort(k.Communes)
	k.Scope.Units = slices.Clone(k.Scope.Units)
	slices.Sort(k.Scope.Units)
	k.Duties = slices.Clone(k.Duties)
	slices.Sort(k.Duties)
	k.Powers = slices.Clone(k.Powers)
	slices.Sort(k.Powers)
	if k.Support != nil {
		support := *k.Support
		support.Reason = ""
		k.Support = &support
	}
	return forms.OperationHash(struct {
		Version       string
		Tenant, Actor uuid.UUID
		MFA           bool
		Roles         []string
		Owner         ext.SessionContext
	}{"close211/v1", c.p.TenantID, c.p.ActorRef, c.p.MFA, roles, k})
}
func decodeClose211(v store.CloseIntent211) (closeData211, error) {
	var d closeData211
	err := json.Unmarshal(v.Payload, &d)
	if err != nil || d.Native.OperationID != v.OperationID || d.Base.Event.ID != v.EventID || d.Base.Detail.Period.ID != v.PeriodID {
		return d, errCloseInterrupted211
	}
	if d.Progress != nil {
		seen := map[string]bool{}
		idsSeen := map[uuid.UUID]bool{}
		for i := range d.Progress.Units {
			unit := &d.Progress.Units[i]
			if unit.Unit == "" || seen[unit.Unit] || unit.Versions < 0 {
				return d, errCloseInterrupted211
			}
			seen[unit.Unit] = true
			ref, ok := d.SubmissionRefs[unit.Unit]
			if unit.Versions > 0 {
				if !ok || ref == uuid.Nil || idsSeen[ref] {
					return d, errCloseInterrupted211
				}
				idsSeen[ref] = true
				id := ref
				unit.LastSubmissionID = &id
			} else if ok {
				return d, errCloseInterrupted211
			}
		}
		for unit := range d.SubmissionRefs {
			if !seen[unit] {
				return d, errCloseInterrupted211
			}
		}
	}
	return d, nil
}
func (s *Service) closeCurrent211(ctx context.Context, v store.CloseIntent211) (caller, error) {
	c, err := s.periodWriter(ctx, v.PeriodID)
	if err != nil {
		return c, err
	}
	h, err := closeAuthority211(c)
	if err != nil {
		return c, err
	}
	if !c.p.MFA || c.p.ActorRef != v.Actor || h != v.AuthorityHash {
		return c, problem.Forbidden
	}
	return c, nil
}
func (s *Service) closeLoad211(ctx context.Context, tid uuid.UUID, key string) (*store.CloseIntent211, error) {
	var v *store.CloseIntent211
	err := s.tx(ctx, tid, func(tx pgx.Tx) error { var e error; v, e = store.CloseByKey211(ctx, tx, key); return e })
	return v, err
}
func (s *Service) closePhase211(ctx context.Context, c caller, v *store.CloseIntent211, data closeData211, to string) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	next := *v
	next.ID, next.Phase, next.Payload, next.At = ids.New(), to, payload, s.now()
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if e := store.LockClose211(ctx, tx, c.p.TenantID, v.PeriodID, v.RequestKey); e != nil {
			return e
		}
		cur, e := store.CloseByOperation211(ctx, tx, v.OperationID)
		if e != nil {
			return e
		}
		if cur == nil || cur.ID != v.ID {
			return errCloseInterrupted211
		}
		return store.InsertClose211(ctx, tx, c.p.TenantID, next)
	})
	if err == nil {
		*v = next
	}
	return err
}

// CloseOperationPolicy211 authorizes only a durable original Close intent, on
// execute AND read. It performs KN-only reads, never calls Get/native ports.
func (s *Service) CloseOperationPolicy211(ctx context.Context, p authz.Principal, in forms.PeriodOperationRequest) (forms.PeriodOperationGrant, error) {
	deny := forms.PeriodOperationGrant{}
	if in.Action != forms.PeriodOperationClose || in.Profile != domain.ProfileKHTC || in.Expected.TenantID != p.TenantID {
		return deny, problem.Forbidden
	}
	var v *store.CloseIntent211
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		v, e = store.CloseByOperation211(ctx, tx, in.OperationID)
		return e
	})
	if err != nil {
		return deny, err
	}
	if v == nil || v.Phase == "prepared" || v.Phase == "rejected" {
		return deny, problem.Forbidden
	}
	c, err := s.closeCurrent211(ctx, *v)
	if err != nil {
		return deny, err
	}
	if c.p.ActorRef != p.ActorRef || c.p.TenantID != p.TenantID || c.p.MFA != p.MFA {
		return deny, problem.Forbidden
	}
	data, err := decodeClose211(*v)
	if err != nil {
		return deny, err
	}
	a, err := forms.PeriodOperationDigest(data.Native)
	if err != nil {
		return deny, err
	}
	b, err := forms.PeriodOperationDigest(in)
	if err != nil || a != b {
		return deny, problem.Forbidden
	}
	var period *store.Period
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error { var e error; period, e = store.PeriodByID(ctx, tx, v.PeriodID); return e })
	if err != nil {
		return deny, err
	}
	if period == nil || period.FormsPeriodRef != in.PeriodID {
		return deny, problem.Forbidden
	}
	return forms.PeriodOperationGrant{Allowed: true, Binding: "kn-khtc-close/v1:" + v.OperationID.String() + ":" + v.AuthorityHash}, nil
}

type closeProof211 struct {
	svc *Service
	op  uuid.UUID
}

func (a closeProof211) VerifyKHTCOperation(ctx context.Context, p authz.Principal, k ext.SessionContext, proof ext.KHTCOperationProof, op forms.Operation) (bool, error) {
	if proof.IntentID != a.op || proof.SessionID != k.ID || proof.Expected != op || op.Action != forms.OperationClose || op.IdemKey != "forms.operation:"+a.op.String() {
		return false, nil
	}
	var v *store.CloseIntent211
	err := a.svc.tx(ctx, p.TenantID, func(tx pgx.Tx) error { var e error; v, e = store.CloseByOperation211(ctx, tx, a.op); return e })
	if err != nil || v == nil {
		return false, err
	}
	if v.PeriodID != proof.KNPeriodID {
		return false, nil
	}
	data, err := decodeClose211(*v)
	if err != nil {
		return false, err
	}
	grant, err := a.svc.CloseOperationPolicy211(ctx, p, data.Native)
	return grant.Allowed, err
}
func (s *Service) closeNativeContext211(ctx context.Context, c caller, v store.CloseIntent211, d closeData211) context.Context {
	return ext.WithKHTCOperation(ctx, closeProof211{svc: s, op: v.OperationID}, ext.KHTCOperationProof{IntentID: v.OperationID, KNPeriodID: v.PeriodID, SessionID: c.k.ID, Expected: forms.Operation{Action: forms.OperationClose, Profile: domain.ProfileKHTC, PeriodID: d.Native.PeriodID, IdemKey: "forms.operation:" + v.OperationID.String()}})
}
func validCloseReceipt211(c caller, v store.CloseIntent211, d closeData211, r forms.PeriodOperationReceipt) bool {
	digest, err := forms.PeriodOperationDigest(d.Native)
	if err != nil {
		return false
	}
	if r.OperationID != v.OperationID || r.Profile != d.Native.Profile || r.PeriodID != d.Native.PeriodID || r.Action != forms.PeriodOperationClose || r.ActorRef != v.Actor || r.RequestDigest != digest || r.Expected != d.Native.Expected || len(r.AuthorityHash) != 64 {
		return false
	}
	if r.Outcome == forms.PeriodOperationRejected {
		return r.RejectionCode != ""
	}
	return r.Outcome == forms.PeriodOperationApplied && revisionIdentity209(r.Applied, c, d.Native.PeriodID) && r.Result.Status == "closed" && r.Result.SnapshotID != uuid.Nil && r.Result.SnapshotNo > 0 && len(r.Result.SnapshotHash) == 64
}
func (s *Service) rejectClose211(ctx context.Context, c caller, v *store.CloseIntent211, d closeData211) error {
	payload, err := json.Marshal(d)
	if err != nil {
		return err
	}
	next := *v
	next.ID, next.Phase, next.Payload, next.At = ids.New(), "rejected", payload, s.now()
	err = s.CloseGuard.RejectClose211(ctx, v.PeriodID, v.OperationID, v.EventID, func(tx pgx.Tx) error {
		if e := store.LockClose211(ctx, tx, c.p.TenantID, v.PeriodID, v.RequestKey); e != nil {
			return e
		}
		cur, e := store.CloseByOperation211(ctx, tx, v.OperationID)
		if e != nil {
			return e
		}
		if cur == nil || cur.ID != v.ID {
			return errCloseInterrupted211
		}
		return store.InsertClose211(ctx, tx, c.p.TenantID, next)
	})
	if err == nil {
		*v = next
	}
	return err
}
func (s *Service) hydrateClose211(ctx context.Context, c caller, v store.CloseIntent211, d *closeData211) error {
	nctx := s.closeNativeContext211(ctx, c, v, *d)
	before, err := s.ConditionalControl.ControlSnapshot(nctx, d.Native.Profile, d.Native.PeriodID)
	if err != nil {
		return err
	}
	if before.Revision != d.Receipt.Applied || before.Progress.Status != "closed" {
		return errCloseInterrupted211
	}
	progress, err := s.Control.Progress(nctx, d.Native.Profile, d.Native.PeriodID)
	if err != nil {
		return err
	}
	after, err := s.ConditionalControl.ControlSnapshot(nctx, d.Native.Profile, d.Native.PeriodID)
	if err != nil {
		return err
	}
	if after.Revision != d.Receipt.Applied || !reflect.DeepEqual(before.Progress, progress) || !reflect.DeepEqual(after.Progress, progress) || progress.ID != d.Native.PeriodID || progress.Profile != d.Native.Profile {
		return errCloseInterrupted211
	}
	seen := map[string]bool{}
	for _, u := range progress.Units {
		if seen[u.Unit] {
			return errCloseInterrupted211
		}
		seen[u.Unit] = true
	}
	for _, dept := range d.Base.Detail.Period.Departments {
		if !seen[dept] {
			return errCloseInterrupted211
		}
	}
	d.SubmissionRefs = map[string]uuid.UUID{}
	for _, unit := range progress.Units {
		if unit.Versions > 0 {
			if unit.LastSubmissionID == nil || *unit.LastSubmissionID == uuid.Nil {
				return errCloseInterrupted211
			}
			d.SubmissionRefs[unit.Unit] = *unit.LastSubmissionID
		}
	}
	d.Progress = &progress
	return nil
}
func closeOutcome211(d closeData211, event store.Event, requests map[string]*store.AdhocRequest) Detail {
	out := d.Base.Detail
	out.Events = append(slices.Clone(out.Events), event)
	out.State = domain.PeriodClosed
	out.FormsStatus = "closed"
	out.CanClose, out.CanCancel, out.CloseForced = false, false, false
	out.Forms = slices.Clone(out.Forms)
	out.Counts.FormsSent = 0
	units := map[string]forms.UnitProgress{}
	for _, u := range d.Progress.Units {
		units[u.Unit] = u
	}
	for i := range out.Forms {
		row := &out.Forms[i]
		row.NativeLate = nil
		row.RequestDueAt = nil
		row.RequestState = ""
		u := units[row.Department]
		row.View = domain.FormStateOf(domain.FormFacts{Versions: u.Versions, Decision: u.Decision, Terminal: domain.PeriodClosed, Max: out.Period.MaxSubmissions})
		row.LastSubmittedAt, row.ReviewedAt, row.ReviewNote, row.Late = u.LastSubmittedAt, u.ReviewedAt, u.ReviewNote, u.Late
		projectAdhocRequest(row, requests[row.Department], u, domain.PeriodClosed)
		if row.View.CountsAsSent {
			out.Counts.FormsSent++
		}
	}
	return out
}
func (s *Service) completeClose211(ctx context.Context, c caller, v *store.CloseIntent211, d closeData211) (Detail, error) {
	var out Detail
	next := *v
	err := s.CloseGuard.FinishClose211(ctx, v.PeriodID, v.OperationID, v.EventID, func(tx pgx.Tx) error {
		if e := store.LockClose211(ctx, tx, c.p.TenantID, v.PeriodID, v.RequestKey); e != nil {
			return e
		}
		cur, e := store.CloseByOperation211(ctx, tx, v.OperationID)
		if e != nil {
			return e
		}
		if cur == nil || cur.ID != v.ID || cur.Phase != "hydrated" {
			return errCloseInterrupted211
		}
		events, e := store.Events(ctx, tx, []uuid.UUID{v.PeriodID})
		if e != nil {
			return e
		}
		event := d.Base.Event
		found := false
		for _, x := range events[v.PeriodID] {
			if x.ID == v.EventID {
				if x.Action != event.Action || x.ActorRef != event.ActorRef || x.Forced != event.Forced || !reflect.DeepEqual(x.Reason, event.Reason) {
					return errCloseInterrupted211
				}
				event = x
				found = true
			} else if x.Action == store.ActionClosed || x.Action == store.ActionCancelled {
				return errCloseInterrupted211
			}
		}
		if !found {
			if e = store.InsertEvent(ctx, tx, c.p.TenantID, &event, "khtc:close211:"+v.OperationID.String()); e != nil {
				return e
			}
		}
		requests := map[string]*store.AdhocRequest{}
		if d.Base.Detail.Period.Type == domain.TypeAdhoc {
			for _, dept := range d.Base.Detail.Period.Departments {
				request, e := store.ActiveAdhocRequest(ctx, tx, v.PeriodID, dept)
				if e != nil {
					return e
				}
				if request != nil {
					requests[dept] = request
				}
			}
		}
		out = closeOutcome211(d, event, requests)
		raw, e := json.Marshal(out)
		if e != nil {
			return e
		}
		next.ID, next.Phase, next.Outcome, next.At = ids.New(), "completed", raw, s.now()
		return store.InsertClose211(ctx, tx, c.p.TenantID, next)
	})
	if err != nil {
		return Detail{}, errCloseInterrupted211.WithCause(err)
	}
	*v = next
	return out, nil
}
func (s *Service) closeCAS211(ctx context.Context, id uuid.UUID, reason, key string) (Detail, error) {
	if s.Operations == nil || s.ConditionalControl == nil || s.CloseGuard == nil {
		return Detail{}, ErrDependency.WithDetail("chưa nối cổng receipt/giữ quyền đóng kỳ")
	}
	c, err := s.periodWriter(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	if !c.p.MFA {
		return Detail{}, problem.MFARequired
	}
	authority, err := closeAuthority211(c)
	if err != nil {
		return Detail{}, err
	}
	if key == "" {
		return Detail{}, problem.Validation.WithField("idempotency_key", "bắt buộc")
	}
	requestHash, err := forms.OperationHash(struct {
		Period uuid.UUID
		Reason string
	}{id, strings.TrimSpace(reason)})
	if err != nil {
		return Detail{}, err
	}
	requestKey, err := forms.OperationHash(key)
	if err != nil {
		return Detail{}, err
	}
	v, err := s.closeLoad211(ctx, c.p.TenantID, requestKey)
	if err != nil {
		return Detail{}, err
	}
	fresh := v == nil
	if fresh {
		base, e := s.Get(ctx, id)
		if e != nil {
			return Detail{}, e
		}
		decision, e := domain.CanClose(base.State, base.DueAt, s.now(), s.closeLateDays(ctx, s.now()), reason)
		if errors.Is(e, domain.ErrReasonRequired) {
			return Detail{}, problem.Validation.WithField("reason", "đóng kỳ: lý do bắt buộc 10–500 ký tự")
		}
		if e != nil {
			return Detail{}, ErrNotClosable.WithCause(e)
		}
		snapshot, e := s.ConditionalControl.ControlSnapshot(ctx, domain.ProfileKHTC, base.Period.FormsPeriodRef)
		if e != nil {
			return Detail{}, e
		}
		if !revisionIdentity209(snapshot.Revision, c, base.Period.FormsPeriodRef) {
			return Detail{}, errCloseInterrupted211
		}
		op, eventID := ids.New(), ids.New()
		event := store.Event{ID: eventID, PeriodID: id, Action: store.ActionClosed, Forced: decision.Forced, State: &base.State, ActorRef: c.p.ActorRef, EventTime: s.now()}
		if decision.Reason != "" {
			event.Reason = &decision.Reason
		}
		data := closeData211{Base: closeBase211{Detail: base, Event: event}, Native: forms.PeriodOperationRequest{OperationID: op, Profile: domain.ProfileKHTC, PeriodID: base.Period.FormsPeriodRef, Action: forms.PeriodOperationClose, Expected: snapshot.Revision}}
		payload, e := json.Marshal(data)
		if e != nil {
			return Detail{}, e
		}
		v = &store.CloseIntent211{ID: ids.New(), OperationID: op, PeriodID: id, EventID: eventID, Actor: c.p.ActorRef, RequestKey: requestKey, RequestHash: requestHash, AuthorityHash: authority, Phase: "prepared", Payload: payload, At: s.now()}
		e = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			if err := store.LockClose211(ctx, tx, c.p.TenantID, id, requestKey); err != nil {
				return err
			}
			old, err := store.CloseByKey211(ctx, tx, requestKey)
			if err != nil {
				return err
			}
			if old != nil {
				return errCloseInterrupted211
			}
			events, err := store.Events(ctx, tx, []uuid.UUID{id})
			if err != nil {
				return err
			}
			due, exp := deadlinesOf(base.Period, events[id])
			if terminalOf(events[id]) != "" || !due.Equal(base.DueAt) || !exp.Equal(base.ExportDueAt) {
				return errCloseInterrupted211
			}
			return store.InsertClose211(ctx, tx, c.p.TenantID, *v)
		})
		if e != nil {
			return Detail{}, errCloseInterrupted211.WithCause(e)
		}
	}
	if v.PeriodID != id || v.Actor != c.p.ActorRef || v.RequestHash != requestHash || v.AuthorityHash != authority {
		return Detail{}, problem.Conflict.WithDetail("khóa đóng kỳ không khớp yêu cầu/quyền")
	}
	data, err := decodeClose211(*v)
	if err != nil {
		return Detail{}, err
	}
	if v.Phase == "rejected" {
		return Detail{}, problem.Conflict.WithDetail("thao tác đóng kỳ đã bị từ chối")
	}
	if v.Phase == "prepared" {
		err = s.CloseGuard.BeginClose211(ctx, id, v.OperationID, v.EventID)
		if err != nil {
			if errors.Is(err, domain.ErrCancelling) {
				data.Rejection = "cancel-won-before-native"
				if e := s.rejectClose211(ctx, c, v, data); e != nil {
					return Detail{}, errCloseInterrupted211.WithCause(e)
				}
				return Detail{}, ErrCancelling
			}
			return Detail{}, errCloseInterrupted211.WithCause(err)
		}
		if err = s.closePhase211(ctx, c, v, data, "dispatched"); err != nil {
			return Detail{}, errCloseInterrupted211.WithCause(err)
		}
		receipt, e := s.Operations.ExecutePeriodOperation(s.closeNativeContext211(ctx, c, *v, data), data.Native)
		if e != nil || !validCloseReceipt211(c, *v, data, receipt) {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			_ = s.closePhase211(cleanup, c, v, data, "unresolved")
			return Detail{}, errCloseInterrupted211.WithCause(e)
		}
		data.Receipt = &receipt
	} else {
		receipt, e := s.Operations.ReadPeriodOperation(s.closeNativeContext211(ctx, c, *v, data), data.Native)
		if e != nil || !validCloseReceipt211(c, *v, data, receipt) {
			return Detail{}, errCloseInterrupted211.WithCause(e)
		}
		if data.Receipt != nil && !reflect.DeepEqual(*data.Receipt, receipt) {
			return Detail{}, errCloseInterrupted211
		}
		data.Receipt = &receipt
	}
	if v.Phase == "completed" {
		var out Detail
		if json.Unmarshal(v.Outcome, &out) != nil || out.Period.ID != id {
			return Detail{}, errCloseInterrupted211
		}
		return out, nil
	}
	if data.Receipt.Outcome == forms.PeriodOperationRejected {
		data.Rejection = data.Receipt.RejectionCode
		if err = s.rejectClose211(ctx, c, v, data); err != nil {
			return Detail{}, errCloseInterrupted211.WithCause(err)
		}
		return Detail{}, problem.Conflict.WithDetail(data.Rejection)
	}
	if v.Phase == "dispatched" || v.Phase == "unresolved" {
		if err = s.closePhase211(ctx, c, v, data, "applied"); err != nil {
			return Detail{}, errCloseInterrupted211.WithCause(err)
		}
	}
	if data.Progress == nil {
		if err = s.hydrateClose211(ctx, c, *v, &data); err != nil {
			if v.Phase != "unresolved" {
				_ = s.closePhase211(ctx, c, v, data, "unresolved")
			}
			return Detail{}, errCloseInterrupted211.WithCause(err)
		}
		if err = s.closePhase211(ctx, c, v, data, "hydrated"); err != nil {
			return Detail{}, errCloseInterrupted211.WithCause(err)
		}
	}
	if v.Phase == "applied" && data.Progress != nil {
		if err = s.closePhase211(ctx, c, v, data, "hydrated"); err != nil {
			return Detail{}, errCloseInterrupted211.WithCause(err)
		}
	}
	return s.completeClose211(ctx, c, v, data)
}
