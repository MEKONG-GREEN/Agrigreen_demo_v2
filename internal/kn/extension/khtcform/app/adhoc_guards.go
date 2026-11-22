package app

import (
	"bytes"
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/store"
	periods "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/domain"
	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func requestToken(r *ps.AdhocRequest, token string) error {
	if r == nil || r.State == "failed" || r.State == "cancelled" {
		if token != "" {
			return ErrAdhocRequestChanged
		}
		return nil
	}
	if r.State == "intent" || token != r.ID.String() {
		return ErrAdhocRequestChanged
	}
	return nil
}

// Reject raw oversized input before normalization, decimal parsing or hashing.
func boundsAdhocInput(sh domain.Sheet) error {
	if len(sh.Cells) > domain.MaxCells || len(sh.Adjustments) > domain.MaxAdjustments || len(sh.Texts) > 100 {
		return problem.Validation.WithDetail("Bảng quá lớn")
	}
	for _, cell := range sh.Cells {
		if len(cell.Done) > 64 || len(cell.People) > 64 || len(cell.Budget) > 64 || len(cell.Status) > 8000 || len(cell.Content) > 8000 {
			return problem.Validation.WithDetail("Ô phiếu quá dài")
		}
	}
	for _, value := range sh.Texts {
		if len(value) > 32000 {
			return problem.Validation.WithDetail("Mục lời văn quá dài")
		}
	}
	for _, a := range sh.Adjustments {
		if len(a.Amount) > 64 || len(a.Reason) > 2000 {
			return problem.Validation.WithDetail("Điều chỉnh quá dài")
		}
	}
	if len(sh.AdhocRequestID) > 36 || len(sh.AdhocValues) > 20 || len(sh.AdhocText) > 32000 || utf8.RuneCountInString(sh.AdhocText) > 8000 {
		return problem.Validation.WithDetail("Câu trả lời bổ sung quá lớn")
	}
	for key, raw := range sh.AdhocValues {
		if len(key) > 40 || len(raw) > 23 {
			return problem.Validation.WithDetail("Chỉ tiêu hoặc số bổ sung quá dài")
		}
	}
	return nil
}
func (s *Service) validateRequestToken(ctx context.Context, c caller, f Form, sh domain.Sheet, submit bool) error {
	var r *ps.AdhocRequest
	if f.Period.Type == pd.TypeAdhoc {
		if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			var e error
			r, e = ps.ActiveAdhocRequest(ctx, tx, f.Period.ID, f.Department)
			return e
		}); err != nil {
			return err
		}
	}
	if err := requestToken(r, sh.AdhocRequestID); err != nil {
		return err
	}
	if r == nil {
		if len(sh.AdhocValues) > 0 || strings.TrimSpace(sh.AdhocText) != "" {
			return ErrAdhocRequestChanged
		}
		return nil
	}
	result := domain.Result{Issues: domain.ValidateAdhocAnswer(sh, r.Indicators, submit)}
	if !result.Valid() {
		return invalid(result)
	}
	return nil
}

// A lost native Submit response must reconcile against the actual native history.
// An orphan sealed row never proves that a submission succeeded.
func (s *Service) replayAdhocSubmit(ctx context.Context, id uuid.UUID, dept string, in SaveInput, key string) (Form, bool, error) {
	c, err := s.caller(ctx, dept)
	if err != nil {
		return Form{}, false, err
	}
	var sealed *store.Sheet
	if err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; sealed, e = store.ByIdem(ctx, tx, "khtc:form:seal:"+key); return e }); err != nil {
		return Form{}, false, err
	}
	if sealed == nil {
		return Form{}, false, nil
	}
	p, _, err := s.periodCtx(ctx, c, id, dept)
	if err != nil {
		return Form{}, false, err
	}
	if p.Type != pd.TypeAdhoc {
		return Form{}, false, nil
	}
	_, hash, e := in.Sheet.Normalize().Hash()
	if e != nil {
		return Form{}, false, e
	}
	if sealed.ActorRef != c.p.ActorRef || sealed.PeriodID != id || sealed.Department != dept || sealed.BaseAttempt != int(in.BaseAttempt) || !bytes.Equal(hash, sealed.ContentHash) {
		return Form{}, false, ErrAdhocRequestConflict
	}
	// Canonical role/scope is still checked by load and the native port.
	f, err := s.load(ctx, c, id, dept)
	if err != nil {
		return Form{}, false, err
	}
	subs, err := s.Entry.Submissions(ctx, pd.ProfileKHTC, p.FormsPeriodRef, dept)
	if err != nil {
		return Form{}, false, err
	}
	for _, sb := range subs {
		if ref := sheetRef(sb.Values); ref != nil && *ref == sealed.ID {
			if err = s.completeAdhocRequest(ctx, c, f, sealed.ID, sealed.Content); err != nil {
				return Form{}, true, err
			}
			f, err = s.load(ctx, c, id, dept)
			return f, true, err
		}
	}
	return Form{}, false, nil
}
func (s *Service) attachAdhocRequest(ctx context.Context, c caller, f *Form) error {
	if f.Period.Type != pd.TypeAdhoc {
		return nil
	}
	var r *ps.AdhocRequest
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		r, e = ps.ActiveAdhocRequest(ctx, tx, f.Period.ID, f.Department)
		return e
	}); err != nil {
		return err
	}
	if r == nil {
		return nil
	}
	view := s.requestView(r)
	f.Supplement = &view
	f.RequestNote = r.Instruction
	f.DueAt = r.DueAt
	f.Overdue = s.now().After(r.DueAt)
	if r.State == "intent" {
		f.Editable = false
	}
	return nil
}
func adhocError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.ConstraintName {
		case "khtc_adhoc_request_changed":
			return ErrAdhocRequestChanged
		case "khtc_adhoc_request_pending":
			return ErrAdhocRequestPending
		case "khtc_adhoc_request_terminal":
			return ErrFormLocked
		}
	}
	return err
}

// Completion is based on a real native submission referencing the sealed token.
// A sealed-but-unsubmitted orphan never completes a request.
func (s *Service) completeAdhocRequest(ctx context.Context, c caller, f Form, sealedID uuid.UUID, sh domain.Sheet) error {
	if sh.AdhocRequestID == "" {
		return nil
	}
	requestID, err := uuid.Parse(sh.AdhocRequestID)
	if err != nil {
		return ErrAdhocRequestChanged
	}
	subs, err := s.Entry.Submissions(ctx, pd.ProfileKHTC, f.Period.FormsPeriodRef, f.Department)
	if err != nil {
		return err
	}
	var submitted *uuid.UUID
	for _, sb := range subs {
		if id := sheetRef(sb.Values); id != nil && *id == sealedID {
			copy := sb.ID
			submitted = &copy
			break
		}
	}
	if submitted == nil {
		return ErrDependency
	}
	var r *ps.AdhocRequest
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; r, e = ps.AdhocRequestByID(ctx, tx, requestID); return e })
	if err != nil {
		return err
	}
	if r == nil || r.PeriodID != f.Period.ID || r.Department != f.Department {
		return ErrAdhocRequestChanged
	}
	_, err = s.requestEvent(ctx, c, r, "fulfilled", submitted, r.ReturnApplied, "Bài nộp native đã tham chiếu bảng niêm phong đúng yêu cầu")
	return err
}

func (s *Service) adhocReviewContext(ctx context.Context, c caller, p ps.Period, submission uuid.UUID, decision, note, key string) context.Context {
	if p.Type != pd.TypeAdhoc {
		return ctx
	}
	hash, _ := forms.OperationHash(struct{ Decision, Note string }{decision, strings.TrimSpace(note)})
	return periods.BindAdhocOperation(ctx, s.DB, c.k, p.ID, forms.Operation{Action: forms.OperationReview, Profile: pd.ProfileKHTC, PeriodID: p.FormsPeriodRef, SubmissionID: submission, PayloadHash: hash, IdemKey: key})
}

// The immutable native late flag uses the period deadline. Supplemental responses
// are additionally evaluated against their own request deadline without rewriting it.
func (s *Service) attachAdhocSubmissionDeadlines(ctx context.Context, c caller, f *Form) error {
	if f.Period.Type != pd.TypeAdhoc || len(f.Submissions) == 0 {
		return nil
	}
	return s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		for i := range f.Submissions {
			submission := &f.Submissions[i]
			if submission.SheetID == nil {
				continue
			}
			sheet, err := store.ByID(ctx, tx, *submission.SheetID)
			if err != nil {
				return err
			}
			if sheet == nil || sheet.PeriodID != f.Period.ID || sheet.Department != f.Department || sheet.Content.AdhocRequestID == "" {
				continue
			}
			id, err := uuid.Parse(sheet.Content.AdhocRequestID)
			if err != nil {
				return ErrAdhocRequestChanged
			}
			request, err := ps.AdhocRequestByID(ctx, tx, id)
			if err != nil {
				return err
			}
			if request == nil || request.PeriodID != f.Period.ID || request.Department != f.Department {
				return ErrAdhocRequestChanged
			}
			native := submission.Late
			submission.NativeLate = &native
			submission.Late = submission.SubmittedAt.After(request.DueAt)
		}
		return nil
	})
}
