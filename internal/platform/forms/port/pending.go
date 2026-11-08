package port

import (
	"context"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// PendingQuery selects native submitted, unreviewed forms which still accept
// this unit and have not reached their submission limit. Empty selectors mean
// all within the owner's complete grant, never all tenants or all profiles.
// Bounds match the existing native 200-row read ceiling. Cursor is opaque.
type PendingQuery struct {
	Profile       string
	UnitKind      string
	PeriodIDs     []uuid.UUID
	Units         []string
	TemplateCodes []string
	Limit         int
	Cursor        string
}

// PendingScope is an explicit, complete owner read scope. Each All flag means
// the whole corresponding dimension; otherwise its slice is the exact set.
// Empty slices with All=false deliberately authorize nothing. A flag and a
// nonempty slice together are invalid. This scope can only narrow native policy.
type PendingScope struct {
	AllPeriods    bool
	PeriodIDs     []uuid.UUID
	AllUnits      bool
	Units         []string
	AllTemplates  bool
	TemplateCodes []string
	AllOwners     bool
	OwnerNodes    []uuid.UUID
}

type PendingGrant struct {
	Allowed  bool
	Complete bool
	// Binding versions current account, selected context, role, node, read-only
	// and revocation facts checked by the owner for this invocation.
	Binding string
	Scope   PendingScope
}

// PendingPolicy MUST read/revalidate the actual current authority on EVERY call,
// including continuation/retry. It must be read-only. It runs before any native data or cursor replay,
// outside a DB transaction. It must reject revoked/mismatched authority; a
// selected-session stamp is only an immutable projection, not OIDC sid/jti.
// Nil policy or incomplete scope makes the additive capability unavailable.
type PendingPolicy func(context.Context, authz.Principal, PendingQuery) (PendingGrant, error)

type PendingSubmission struct {
	PeriodID     uuid.UUID `json:"period_id"`
	PeriodLabel  string    `json:"period_label"`
	TemplateCode string    `json:"template_code"`
	Unit         string    `json:"unit"`
	SubmissionID uuid.UUID `json:"submission_id"`
	Attempt      int64     `json:"attempt"`
	SubmittedAt  time.Time `json:"submitted_at"`
	Decision     string    `json:"decision"`
	Status       string    `json:"status"`
}

// PendingPage's Count covers the ENTIRE permitted query, including rows beyond
// this page. CoverageComplete proves that count, not that Items exhaust it.
// NextCursor empty means this page exhausts the matching set after its cursor.
// One native SQL statement supplies count, page and Snapshot. Continuation
// revalidates authority and the native visibility epoch; changes require a new query.
// Snapshot hashes PostgreSQL's statement visibility snapshot, database and server
// start. Any database writer/visibility change may conservatively require a
// restart, including commit of an already-active transaction. It is not a
// retained historical read. The policy must be read-only to avoid self-invalidating
// pagination. A digest collision is theoretically possible (SHA-256).
type PendingPage struct {
	Items                []PendingSubmission
	Count                int64
	CoverageComplete     bool
	QueryFingerprint     string
	AuthorityFingerprint string
	Snapshot             string
	NextCursor           string
}

type PendingReader interface {
	PendingSubmissions(context.Context, PendingQuery) (PendingPage, error)
}

var (
	ErrPendingUnavailable = problem.New(http.StatusServiceUnavailable, "forms.pending_unavailable", "Không có đủ quyền hoặc phạm vi để đếm phiếu chờ rà soát")
	ErrPendingChanged     = problem.New(http.StatusConflict, "forms.pending_changed", "Tập phiếu hoặc quyền đã đổi — tải lại từ trang đầu")
	pendingProfile        = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	pendingUnit           = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)
	pendingCode           = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{0,63}$`)
)

// NormalizePendingQuery bounds every string/collection before parsing a cursor,
// calling an owner or opening a connection. It also owns all returned slices.
func NormalizePendingQuery(in PendingQuery) (PendingQuery, error) {
	if len(in.Profile) > 40 || !pendingProfile.MatchString(in.Profile) ||
		!slices.Contains([]string{UnitDepartment, UnitCommune, UnitFacility, UnitHousehold}, in.UnitKind) ||
		in.Limit < 1 || in.Limit > 200 || len(in.Cursor) > 2048 || !utf8.ValidString(in.Cursor) ||
		!validPendingIDs(in.PeriodIDs) || !validPendingStrings(in.Units, pendingUnit) || !validPendingStrings(in.TemplateCodes, pendingCode) {
		return PendingQuery{}, problem.Validation.WithField("pending_query", "bộ lọc native hoặc giới hạn không hợp lệ")
	}
	in.PeriodIDs = sortedPendingIDs(in.PeriodIDs)
	in.Units = append([]string{}, in.Units...)
	slices.Sort(in.Units)
	in.TemplateCodes = append([]string{}, in.TemplateCodes...)
	slices.Sort(in.TemplateCodes)
	return in, nil
}

// NormalizePendingGrant rejects ambiguous/missing coverage, not a proven empty
// authorized set. It takes ownership of every server-supplied scope slice.
func NormalizePendingGrant(in PendingGrant) (PendingGrant, error) {
	s := in.Scope
	if !in.Complete || !utf8.ValidString(in.Binding) || len(in.Binding) > 512 || strings.TrimSpace(in.Binding) == "" || strings.ContainsRune(in.Binding, 0) ||
		!validPendingIDs(s.PeriodIDs) || !validPendingIDs(s.OwnerNodes) || !validPendingStrings(s.Units, pendingUnit) || !validPendingStrings(s.TemplateCodes, pendingCode) ||
		(s.AllPeriods && len(s.PeriodIDs) > 0) || (s.AllUnits && len(s.Units) > 0) || (s.AllTemplates && len(s.TemplateCodes) > 0) || (s.AllOwners && len(s.OwnerNodes) > 0) {
		return PendingGrant{}, ErrPendingUnavailable
	}
	s.PeriodIDs = sortedPendingIDs(s.PeriodIDs)
	s.OwnerNodes = sortedPendingIDs(s.OwnerNodes)
	s.Units = append([]string{}, s.Units...)
	slices.Sort(s.Units)
	s.TemplateCodes = append([]string{}, s.TemplateCodes...)
	slices.Sort(s.TemplateCodes)
	in.Scope = s
	return in, nil
}

func validPendingIDs(values []uuid.UUID) bool {
	if len(values) > 200 {
		return false
	}
	seen := map[uuid.UUID]bool{}
	for _, v := range values {
		if v == uuid.Nil || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}
func validPendingStrings(values []string, pattern *regexp.Regexp) bool {
	if len(values) > 200 {
		return false
	}
	seen := map[string]bool{}
	for _, v := range values {
		if len(v) > 100 || !pattern.MatchString(v) || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}
func sortedPendingIDs(values []uuid.UUID) []uuid.UUID {
	out := append([]uuid.UUID{}, values...)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return out
}
