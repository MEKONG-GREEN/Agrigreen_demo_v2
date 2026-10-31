package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	fs "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/store"
	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	rs "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrAdhocRequestChanged  = problem.New(http.StatusConflict, "extension.khtc_adhoc_request_changed", "Yêu cầu bổ sung đã đổi — tải lại phiếu và đọc yêu cầu mới")
	ErrAdhocRequestPending  = problem.New(http.StatusConflict, "extension.khtc_adhoc_request_pending", "Phòng còn yêu cầu bổ sung chưa hoàn tất")
	ErrAdhocRequestConflict = problem.New(http.StatusConflict, "extension.khtc_adhoc_request_idempotency", "Khóa thao tác đã dùng cho nội dung yêu cầu khác")
)

type AdhocRequestInput struct {
	Indicators  []pd.AdhocIndicator `json:"indicators"`
	Instruction string              `json:"instruction"`
	DueAt       time.Time           `json:"due_at"`
}
type AdhocRequestView struct {
	Indicators    []pd.AdhocIndicator `json:"indicators"`
	ID            uuid.UUID           `json:"id"`
	PeriodID      uuid.UUID           `json:"period_id"`
	Department    string              `json:"department"`
	Instruction   string              `json:"instruction"`
	DueAt         time.Time           `json:"due_at"`
	State         string              `json:"state"`
	Overdue       bool                `json:"overdue"`
	ReturnApplied *bool               `json:"return_applied"`
	ActorRef      uuid.UUID           `json:"actor_ref"`
	At            time.Time           `json:"at"`
}

func (s *Service) requestView(r *ps.AdhocRequest) AdhocRequestView {
	return AdhocRequestView{r.Indicators, r.ID, r.PeriodID, r.Department, r.Instruction, r.DueAt, r.State,
		(r.State == "intent" || r.State == "issued") && s.now().After(r.DueAt), r.ReturnApplied, r.ActorRef, r.At}
}
func requestWriter(c caller, p ps.Period) bool {
	return !c.k.ReadOnly() && (c.k.Role == ext.SessionRoleAdmin || c.review && c.p.ActorRef == p.ActorRef)
}
func (s *Service) RequestSupplement(ctx context.Context, id uuid.UUID, dept string, in AdhocRequestInput, idem string) (AdhocRequestView, error) {
	if idem == "" {
		return AdhocRequestView{}, problem.IdempotencyRequired
	}
	in.Instruction = strings.TrimSpace(in.Instruction)
	in.DueAt = in.DueAt.UTC()
	definitions, e := pd.NormalizeAdhocIndicators(in.Indicators)
	if e != nil {
		return AdhocRequestView{}, problem.Validation.WithField("indicators", e.Error())
	}
	in.Indicators = definitions
	if in.Instruction == "" || utf8.RuneCountInString(in.Instruction) > 4000 || strings.ContainsRune(in.Instruction, 0) || in.DueAt.IsZero() {
		return AdhocRequestView{}, problem.Validation.WithField("instruction", "Nhập nội dung (tối đa 4000 ký tự) và hạn gửi")
	}
	c, err := s.caller(ctx, dept)
	if err != nil {
		return AdhocRequestView{}, err
	}
	if c.k.Role == ext.SessionRoleAdmin && !c.p.MFA {
		return AdhocRequestView{}, problem.MFARequired
	}
	p, events, err := s.periodCtx(ctx, c, id, dept)
	if err != nil {
		return AdhocRequestView{}, err
	}
	if p.Type != pd.TypeAdhoc || !requestWriter(c, p) {
		return AdhocRequestView{}, problem.Forbidden
	}
	raw, _ := json.Marshal(struct {
		Period                  uuid.UUID
		Department, Instruction string
		Due                     time.Time
		Indicators              []pd.AdhocIndicator
	}{id, dept, in.Instruction, in.DueAt, in.Indicators})
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	key := "khtc:adhoc:request:" + id.String() + ":" + idem
	var req *ps.AdhocRequest
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; req, e = ps.AdhocRequestByKey(ctx, tx, id, key); return e })
	if err != nil {
		return AdhocRequestView{}, err
	}
	if req != nil {
		if req.PayloadHash != hash {
			return AdhocRequestView{}, ErrAdhocRequestConflict
		}
		return s.resumeAdhocRequest(ctx, c, p, req)
	}
	if terminalOf(events) != "" {
		return AdhocRequestView{}, ErrFormLocked
	}
	if !in.DueAt.After(s.now()) || in.DueAt.After(s.now().AddDate(1, 0, 0)) {
		return AdhocRequestView{}, problem.Validation.WithField("due_at", "Hạn phải sau hiện tại và trong 1 năm")
	}
	subs, err := s.Entry.Submissions(ctx, pd.ProfileKHTC, p.FormsPeriodRef, dept)
	if err != nil {
		return AdhocRequestView{}, err
	}
	attempt := int64(0)
	if len(subs) > 0 {
		attempt = subs[0].Version
	}
	if attempt >= int64(p.MaxSubmissions) || attempt >= 3 {
		return AdhocRequestView{}, ErrNotReviewable
	}
	now := s.now()
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if e := ps.LockAdhocRequest(ctx, tx, c.p.TenantID, id); e != nil {
			return e
		}
		var e error
		req, e = ps.AdhocRequestByKey(ctx, tx, id, key)
		if e != nil {
			return e
		}
		if req != nil {
			if req.PayloadHash != hash {
				return ErrAdhocRequestConflict
			}
			return nil
		}
		current, e := ps.LatestAdhocRequest(ctx, tx, id, dept)
		if e != nil {
			return e
		}
		if current != nil && (current.State == "intent" || current.State == "issued") {
			return ErrAdhocRequestPending
		}
		final, e := rs.GetFinal(ctx, tx, id)
		if e != nil {
			return e
		}
		if final != nil {
			return ErrFormLocked
		}
		evs, e := ps.Events(ctx, tx, []uuid.UUID{id})
		if e != nil {
			return e
		}
		if terminalOf(evs[id]) != "" {
			return ErrFormLocked
		}
		req = &ps.AdhocRequest{Indicators: in.Indicators, ID: ids.New(), PeriodID: id, Department: dept, Instruction: in.Instruction, DueAt: in.DueAt,
			PayloadHash: hash, IdemKey: key, BaseAttempt: attempt, ActorRef: c.p.ActorRef, At: now, State: "intent"}
		return ps.InsertAdhocRequest(ctx, tx, c.p.TenantID, req)
	})
	if err != nil {
		return AdhocRequestView{}, err
	}
	return s.resumeAdhocRequest(ctx, c, p, req)
}
func (s *Service) ResumeAdhocRequest(ctx context.Context, period, request uuid.UUID) (AdhocRequestView, error) {
	c, err := s.caller(ctx, "")
	if err != nil {
		return AdhocRequestView{}, err
	}
	if c.k.Role == ext.SessionRoleAdmin && !c.p.MFA {
		return AdhocRequestView{}, problem.MFARequired
	}
	var r *ps.AdhocRequest
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; r, e = ps.AdhocRequestByID(ctx, tx, request); return e })
	if err != nil {
		return AdhocRequestView{}, err
	}
	if r == nil || r.PeriodID != period {
		return AdhocRequestView{}, ErrFormNotFound
	}
	p, _, err := s.periodCtx(ctx, c, period, r.Department)
	if err != nil {
		return AdhocRequestView{}, err
	}
	if !requestWriter(c, p) {
		return AdhocRequestView{}, problem.Forbidden
	}
	return s.resumeAdhocRequest(ctx, c, p, r)
}
func (s *Service) requestEvent(ctx context.Context, c caller, r *ps.AdhocRequest, state string, native *uuid.UUID, applied *bool, reason string) (AdhocRequestView, error) {
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if e := ps.LockAdhocRequest(ctx, tx, c.p.TenantID, r.PeriodID); e != nil {
			return e
		}
		current, e := ps.AdhocRequestByID(ctx, tx, r.ID)
		if e != nil {
			return e
		}
		if current == nil {
			return ErrFormNotFound
		}
		// Another same-key retry may already have completed delivery.
		if current.State == "fulfilled" && state != "fulfilled" || current.State == state && sameRequestObservation(current.NativeSubmissionRef, native, current.ReturnApplied, applied) {
			r = current
			return nil
		}
		evs, e := ps.Events(ctx, tx, []uuid.UUID{r.PeriodID})
		if e != nil {
			return e
		}
		if terminalOf(evs[r.PeriodID]) != "" && state == "issued" {
			state = "cancelled"
			reason = "Kỳ đã kết thúc; không mở lại"
		}
		ref := "none"
		if native != nil {
			ref = native.String()
		}
		flag := "unknown"
		if applied != nil {
			if *applied {
				flag = "returned"
			} else {
				flag = "not_returned"
			}
		}
		event := ps.AdhocRequestEvent{ID: ids.New(), RequestID: r.ID, PeriodID: r.PeriodID, ActorRef: c.p.ActorRef,
			Action: state, NativeSubmissionRef: native, ReturnApplied: applied, Reason: reason, At: s.now(), IdemKey: "khtc:adhoc:event:" + r.ID.String() + ":" + state + ":" + ref + ":" + flag}
		if e := ps.InsertAdhocRequestEvent(ctx, tx, c.p.TenantID, event); e != nil {
			return e
		}
		r.State = state
		r.NativeSubmissionRef = native
		r.ReturnApplied = applied
		return nil
	})
	if err != nil {
		return AdhocRequestView{}, err
	}
	return s.requestView(r), nil
}
func (s *Service) resumeAdhocRequest(ctx context.Context, c caller, p ps.Period, r *ps.AdhocRequest) (AdhocRequestView, error) {
	if r.State == "fulfilled" {
		return s.requestView(r), nil
	}
	// Ports are deliberately outside every DB transaction.
	subs, err := s.Entry.Submissions(ctx, pd.ProfileKHTC, p.FormsPeriodRef, r.Department)
	if err != nil {
		return AdhocRequestView{}, err
	}

	if submitted, err := s.observedAdhocSubmission(ctx, c, p, r, subs); err != nil {
		return AdhocRequestView{}, err
	} else if submitted != nil {
		return s.requestEvent(ctx, c, r, "fulfilled", submitted, r.ReturnApplied, "Đã đối chiếu bài nộp native đúng token")
	}
	var native *uuid.UUID
	returned := false
	for _, sb := range subs {
		if sb.Version == r.BaseAttempt {
			copy := sb.ID
			native = &copy
			returned = sb.Decision == DecisionReturned
		}
	}
	_, events, err := s.periodCtx(ctx, c, p.ID, r.Department)
	if err != nil {
		return AdhocRequestView{}, err
	}
	if terminalOf(events) != "" {
		return s.requestEvent(ctx, c, r, "cancelled", native, &returned, "Kỳ đã kết thúc; đã đối chiếu quyết định native")
	}

	if r.State == "failed" || r.State == "cancelled" {
		return s.requestEvent(ctx, c, r, r.State, native, &returned, "Đã đối chiếu tác động Return native")
	}
	if r.State == "issued" {
		return s.requestView(r), nil
	}
	if native != nil && !returned {
		_, callErr := s.Entry.Review(s.adhocReviewContext(ctx, c, p, *native, DecisionReturned, "Yêu cầu bổ sung "+r.ID.String(), "khtc:adhoc:return:"+r.ID.String()), pd.ProfileKHTC, *native, DecisionReturned, "Yêu cầu bổ sung "+r.ID.String(), "khtc:adhoc:return:"+r.ID.String())
		// Reconcile a lost response before recording any failure.
		observed, readErr := s.Entry.Submissions(ctx, pd.ProfileKHTC, p.FormsPeriodRef, r.Department)
		if readErr != nil {
			return AdhocRequestView{}, readErr
		}
		for _, sb := range observed {
			if sb.ID == *native && sb.Decision == DecisionReturned {
				returned = true
			}
		}
		if callErr != nil && !returned {
			var pb *problem.Problem
			if errors.Is(callErr, context.DeadlineExceeded) || errors.Is(callErr, context.Canceled) || !errors.As(callErr, &pb) || pb.Status >= 500 {
				return AdhocRequestView{}, callErr
			}
			_, eventErr := s.requestEvent(ctx, c, r, "failed", native, &returned, "Native từ chối trả phiếu; đã đối chiếu chưa trả")
			if eventErr != nil {
				return AdhocRequestView{}, eventErr
			}
			return AdhocRequestView{}, callErr
		}
	}
	return s.requestEvent(ctx, c, r, "issued", native, &returned, "Yêu cầu bổ sung đã phát hành trên phiếu được giao")
}
func (s *Service) AdhocRequests(ctx context.Context, id uuid.UUID, dept string) ([]AdhocRequestView, error) {
	c, err := s.caller(ctx, dept)
	if err != nil {
		return nil, err
	}
	if c.k.ReadOnly() || c.k.Role == ext.SessionRoleNational {
		return nil, problem.Forbidden
	}
	var period *ps.Period
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; period, e = ps.PeriodByID(ctx, tx, id); return e }); err != nil {
		return nil, err
	}
	if period == nil || period.Type != pd.TypeAdhoc {
		return nil, ErrFormNotFound
	}
	if dept != "" {
		if _, _, err := s.periodCtx(ctx, c, id, dept); err != nil {
			return nil, err
		}
	}
	if dept == "" && c.k.Role != ext.SessionRoleAdmin && !c.review {
		return nil, problem.Forbidden
	}
	if !c.khtc && (!c.member || dept != c.k.Department) {
		return nil, problem.Forbidden
	}
	var rows []ps.AdhocRequest
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; rows, e = ps.AdhocRequests(ctx, tx, id, dept); return e })
	if err != nil {
		return nil, err
	}
	out := []AdhocRequestView{}
	for i := range rows {
		out = append(out, s.requestView(&rows[i]))
	}
	return out, nil
}

func sameRequestObservation(a, b *uuid.UUID, x, y *bool) bool {
	sameID := a == nil && b == nil || a != nil && b != nil && *a == *b
	sameFlag := x == nil && y == nil || x != nil && y != nil && *x == *y
	return sameID && sameFlag
}
func (s *Service) observedAdhocSubmission(ctx context.Context, c caller, p ps.Period, r *ps.AdhocRequest, subs []formsport.SubmissionRef) (*uuid.UUID, error) {
	if len(subs) > 3 {
		return nil, ErrDependency
	}
	for _, sb := range subs {
		ref := sheetRef(sb.Values)
		if ref == nil {
			continue
		}
		var sheet *fs.Sheet
		if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; sheet, e = fs.ByID(ctx, tx, *ref); return e }); err != nil {
			return nil, err
		}
		if sheet != nil && sheet.Kind == fs.KindSealed && sheet.PeriodID == p.ID && sheet.Department == r.Department && sheet.Content.AdhocRequestID == r.ID.String() {
			copy := sb.ID
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *Service) AdhocInbox(ctx context.Context) ([]AdhocRequestView, error) {
	_, k, err := ext.RequireSession(ctx)
	if err != nil {
		return nil, err
	}
	if k.Role != ext.SessionRoleProvince || k.ReadOnly() || k.Department == "" {
		return nil, problem.Forbidden
	}
	c, err := s.caller(ctx, k.Department)
	if err != nil {
		return nil, err
	}
	var rows []ps.AdhocRequest
	if err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; rows, e = ps.AdhocRequestInbox(ctx, tx, k.Department); return e }); err != nil {
		return nil, err
	}
	out := []AdhocRequestView{}
	for i := range rows {
		out = append(out, s.requestView(&rows[i]))
	}
	return out, nil
}
