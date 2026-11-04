package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrDraftConflict225 = problem.New(http.StatusConflict, "extension.khtc_draft_revision_conflict", "Nháp máy chủ đã thay đổi — tải lại trước khi thao tác")
	ErrDraftActive225   = problem.New(http.StatusConflict, "extension.khtc_initial_draft_active", "Đã có nháp máy chủ của năm này; chưa thể ghi phiên bản áp dụng")
	ErrDraftYear225     = problem.New(http.StatusConflict, "extension.khtc_initial_draft_year_in_use", "Năm đã có phiên bản hoặc tham chiếu kỳ; không thể tạo nháp ban đầu")
	ErrDraftReplay225   = problem.New(http.StatusConflict, "extension.khtc_draft_operation_conflict", "Khóa thao tác đã gắn với nội dung hoặc quyền khác")
)

type InitialDraft225 struct {
	Event   store.DraftEvent225
	Entries []EntryView
}
type InitialDraftState225 struct {
	Year              int
	Revision          int64
	Draft             *InitialDraft225
	Available         bool
	UnavailableReason string
}
type InitialDraftInput225 struct {
	Year             int
	DraftID          uuid.UUID
	ExpectedRevision int64
	Entries          []domain.Entry
	Note, IdemKey    string
}

// ReadSessionContext strictly validates the full v1 owner payload/profile/scope.
// The returned context is freshly decoded: canonicalizing it cannot mutate the
// live principal/SelectedSession. Only ephemeral ID/expiry are normalized.
func (s *Service) draftCapability225(ctx context.Context, write bool) (caller, []byte, error) {
	p, k, err := port.RequireSession(ctx)
	c := caller{p: p, k: k}
	if err != nil {
		return c, nil, err
	}
	if p.ActorRef == uuid.Nil || p.TenantID == uuid.Nil {
		return c, nil, problem.Forbidden
	}
	// Match the existing selected-context policy: ordinary province staff do not
	// gain an MFA enrollment requirement from this new draft family.
	if (k.Role == port.SessionRoleAdmin || k.Support != nil) && !p.MFA {
		return c, nil, problem.MFARequired
	}
	if s.Clock == nil {
		return c, nil, ErrDependency
	}
	if k.ExpiresAt.IsZero() || !k.ExpiresAt.After(s.now()) {
		return c, nil, problem.Forbidden.WithDetail("Ngữ cảnh phiên đã hết hạn")
	}
	if k.Role != port.SessionRoleAdmin && (k.Role != port.SessionRoleProvince || k.Department != DeptKHTC) {
		return c, nil, problem.Forbidden
	}
	if write {
		if err := c.checkWrite(); err != nil {
			return c, nil, err
		}
	}
	canonical := k
	canonical.ID = uuid.Nil
	canonical.ExpiresAt = time.Time{}
	// Clone before canonical sorting, retaining every grant/support/assignment field.
	canonical.Communes = slices.Clone(k.Communes)
	slices.Sort(canonical.Communes)
	canonical.Duties = slices.Clone(k.Duties)
	slices.Sort(canonical.Duties)
	canonical.Powers = slices.Clone(k.Powers)
	slices.Sort(canonical.Powers)
	canonical.Scope.Units = slices.Clone(k.Scope.Units)
	slices.Sort(canonical.Scope.Units)
	roles := slices.Clone(p.Roles)
	slices.Sort(roles)
	scope := p.SelectedSession.Scope()
	scope.UnitCodes = slices.Clone(scope.UnitCodes)
	slices.Sort(scope.UnitCodes)
	raw, err := json.Marshal(struct {
		Actor, Tenant, Org uuid.UUID
		Roles              []string
		MFA                bool
		Profile            string
		Scope              authz.SessionScope
		OwnerVersion       int
		Owner              port.SessionContext
	}{p.ActorRef, p.TenantID, p.OrgRef, roles, p.MFA, p.SelectedSession.Profile(), scope, 1, canonical})
	if err != nil {
		return c, nil, err
	}
	h := sha256.Sum256(append([]byte("kn.khtc.initial-draft.capability/1\n"), raw...))
	return c, h[:], nil
}
func (s *Service) AuthorizeInitialDraft225(ctx context.Context, write bool) ([]byte, error) {
	_, stamp, err := s.draftCapability225(ctx, write)
	return stamp, err
}
func draftFailure225(err error) error {
	var p *pgconn.PgError
	if errors.As(err, &p) && p.Code == "23514" {
		switch {
		case strings.HasPrefix(p.Message, "khtc_draft225_year_referenced"):
			return ErrDraftYear225
		case strings.HasPrefix(p.Message, "khtc_draft225_"):
			return ErrDraftConflict225
		}
	}
	return err
}
func (s *Service) yearReferences225(ctx context.Context, year int) (port.KHTCPlanYearReferences, error) {
	if s.YearReferences == nil {
		return port.KHTCPlanYearReferences{}, ErrDependency.WithDetail("Chưa nối tham chiếu năm kế hoạch")
	}
	facts, err := s.YearReferences.PlanYearReferences(ctx, year)
	if err != nil {
		return facts, ErrDependency.WithDetail("Không xác minh được tham chiếu năm kế hoạch").WithCause(err)
	}
	return facts, nil
}
func draftActive225(e *store.DraftEvent225) bool { return e != nil && e.Action != "discard" }
func (s *Service) draftView225(ctx context.Context, tx pgx.Tx, e store.DraftEvent225) (InitialDraft225, error) {
	es, err := store.DraftEntries225(ctx, tx, e)
	if err != nil {
		return InitialDraft225{}, err
	}
	if len(es) != e.EntriesCount || !bytes.Equal(domain.EntriesHash(es), e.EntriesHash) {
		return InitialDraft225{}, ErrDependency.WithDetail("Nội dung nháp không khớp mốc đã ghi")
	}
	// A replay is this immutable event, never a live catalog join. Display Name
	// and optional row Code are already pinned in the normalized stored tree.
	return InitialDraft225{Event: e, Entries: entryViews(es, nil, nil)}, nil
}
func (s *Service) GetInitialDraft225(ctx context.Context, year int) (InitialDraftState225, error) {
	var out InitialDraftState225
	out.Year = year
	c, _, err := s.draftCapability225(ctx, false)
	if err != nil {
		return out, err
	}
	if err = validYear(year); err != nil {
		return out, err
	}
	facts, err := s.yearReferences225(ctx, year)
	if err != nil {
		return out, err
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		tail, e := store.CurrentDraft225(ctx, tx, year)
		if e != nil {
			return e
		}
		if tail != nil {
			out.Revision = tail.Seq
			if draftActive225(tail) {
				view, e := s.draftView225(ctx, tx, *tail)
				if e != nil {
					return e
				}
				out.Draft = &view
			}
		}
		published, e := store.HasPublished225(ctx, tx, year)
		if e != nil {
			return e
		}
		out.Available = !published && !facts.HasPeriodMetadata && !facts.HasOpenIntent
		switch {
		case published:
			out.UnavailableReason = "published_plan_exists"
		case facts.HasPeriodMetadata || facts.HasOpenIntent:
			out.UnavailableReason = "year_has_references"
		}
		return nil
	})
	return out, err
}
func (s *Service) InitialDraftHistory225(ctx context.Context, year int, after int64, limit int) ([]store.DraftEvent225, error) {
	c, _, err := s.draftCapability225(ctx, false)
	if err != nil {
		return nil, err
	}
	if err = validYear(year); err != nil {
		return nil, err
	}
	if after < 0 || limit < 1 || limit > 100 {
		return nil, problem.Validation
	}
	var out []store.DraftEvent225
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.DraftHistory225(ctx, tx, year, after, limit+1)
		return e
	})
	return out, err
}
func (s *Service) normalizeInitial225(ctx context.Context, tx pgx.Tx, year int, es []domain.Entry) error {
	cat, err := store.CatalogCurrent(ctx, tx, "")
	if err != nil {
		return err
	}
	progs, err := store.ProgramsCurrent(ctx, tx)
	if err != nil {
		return err
	}
	usable := map[uuid.UUID]bool{}
	pm := programMap(progs)
	for _, p := range progs {
		if p.Status == domain.StatusActive {
			usable[p.Ref] = true
		}
	}
	for i := range es {
		if es[i].Kind == domain.EntryProgram && strings.TrimSpace(es[i].Name) == "" && es[i].ProgramRef != nil {
			if p, ok := pm[*es[i].ProgramRef]; ok {
				es[i].Name = p.Name
			}
		}
	}
	codes := activeCodes(cat)
	if bad := domain.NormalizePlan(year, es, domain.Refs{Units: codes[domain.KindUnit], Funding: codes[domain.KindFundingSource], Fields: codes[domain.KindField], Programs: usable}); len(bad) > 0 {
		return fieldsProblem(ErrPlanInvalid, bad)
	}
	return nil
}
func (s *Service) SaveInitialDraft225(ctx context.Context, in InitialDraftInput225) (InitialDraft225, bool, error) {
	return s.writeInitial225(ctx, in, false)
}
func (s *Service) DiscardInitialDraft225(ctx context.Context, in InitialDraftInput225) (InitialDraft225, bool, error) {
	return s.writeInitial225(ctx, in, true)
}
func (s *Service) writeInitial225(ctx context.Context, in InitialDraftInput225, discard bool) (InitialDraft225, bool, error) {
	var out InitialDraft225
	c, capability, err := s.draftCapability225(ctx, true)
	if err != nil {
		return out, false, err
	}
	if err = validYear(in.Year); err != nil {
		return out, false, err
	}
	if in.ExpectedRevision < 0 {
		return out, false, problem.Validation.WithField("expected_revision", "không âm")
	}
	if in.IdemKey == "" {
		return out, false, problem.IdempotencyRequired
	}
	if len(in.IdemKey) > 200 || len(in.Note) > 4000 || utf8.RuneCountInString(in.Note) > 1000 {
		return out, false, problem.Validation
	}
	if discard && (in.DraftID == uuid.Nil || len(in.Entries) != 0) {
		return out, false, problem.Validation
	}
	if err = domain.CheckDraftResource225(in.Entries); err != nil {
		if fields, ok := err.(domain.Errors); ok {
			return out, false, fieldsProblem(ErrPlanInvalid, fields)
		}
		return out, false, ErrPlanInvalid.WithField("entries", "dữ liệu vượt giới hạn")
	}
	action := "save"
	if in.DraftID == uuid.Nil {
		action = "create"
	}
	if discard {
		action = "discard"
	}
	hash, err := domain.DraftRequestHash225(in.Year, in.DraftID, in.ExpectedRevision, action, in.Note, in.Entries)
	if err != nil {
		return out, false, ErrPlanInvalid
	}
	replay := func(tx pgx.Tx) (bool, error) {
		own, e := store.DraftByIdem225(ctx, tx, in.IdemKey)
		if e != nil || own == nil {
			return false, e
		}
		if own.ActorRef != c.p.ActorRef || !bytes.Equal(own.RequestHash, hash) || !bytes.Equal(own.AuthorityHash, capability) {
			return false, ErrDraftReplay225
		}
		out, e = s.draftView225(ctx, tx, *own)
		return true, e
	}
	var wasReplay bool
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; wasReplay, e = replay(tx); return e })
	if err != nil || wasReplay {
		return out, wasReplay, err
	}
	facts, err := s.yearReferences225(ctx, in.Year)
	if err != nil {
		return out, false, err
	}
	if facts.HasPeriodMetadata || facts.HasOpenIntent {
		return out, false, ErrDraftYear225
	}
	es := append([]domain.Entry{}, in.Entries...)
	for i := range es {
		es[i].Communes = slices.Clone(es[i].Communes)
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, "plan:"+strconv.Itoa(in.Year)); e != nil {
			return e
		}
		hit, e := replay(tx)
		if e != nil {
			return e
		}
		if hit {
			wasReplay = true
			return nil
		}
		published, e := store.HasPublished225(ctx, tx, in.Year)
		if e != nil {
			return e
		}
		if published {
			return ErrDraftYear225
		}
		tail, e := store.CurrentDraft225(ctx, tx, in.Year)
		if e != nil {
			return e
		}
		var seq int64
		if tail != nil {
			seq = tail.Seq
		}
		if seq != in.ExpectedRevision {
			return ErrDraftConflict225.With("latest_revision", seq)
		}
		if action == "create" {
			if draftActive225(tail) {
				return ErrDraftConflict225
			}
		} else if !draftActive225(tail) || tail.DraftID != in.DraftID {
			return ErrDraftConflict225
		}
		if !discard {
			if e := s.normalizeInitial225(ctx, tx, in.Year, es); e != nil {
				return e
			}
		}
		generation := in.DraftID
		if action == "create" {
			generation = newID()
		}
		event := store.DraftEvent225{Meta: c.meta(newID(), s.now(), in.IdemKey), Year: in.Year, DraftID: generation, Action: action, ExpectedRevision: in.ExpectedRevision, EntriesCount: len(es), EntriesHash: domain.EntriesHash(es), RequestHash: hash, AuthorityHash: capability, Note: strings.TrimSpace(in.Note)}
		if e := store.InsertDraft225(ctx, tx, c.p.TenantID, &event, es); e != nil {
			return e
		}
		out, e = s.draftView225(ctx, tx, event)
		return e
	})
	if dup(err) {
		err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			var e error
			wasReplay, e = replay(tx)
			if e == nil && !wasReplay {
				return ErrDraftReplay225
			}
			return e
		})
	}
	return out, wasReplay, draftFailure225(err)
}
