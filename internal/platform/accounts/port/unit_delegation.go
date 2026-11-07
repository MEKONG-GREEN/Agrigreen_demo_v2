package port

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// KNSPLIT-2-ACCOUNTS-UNIT-DELEGATION (#195): owner contract for internal units,
// time-limited delegations and the people scope. The shared rollout (#196)
// injects a provider in API/worker; an absent provider retains compatibility.
// A configured provider is final; its errors never select the legacy
// path, and its decisions never change Principal, tenant, realm roles or MFA.
//
// Definitions are trusted startup data. Decisions are computed only from the
// verified selected owner session already bound by server middleware, never from
// headers, request payloads or realm roles. Providers perform no I/O, so accounts
// may obtain every value before opening its transaction. Native checks (tenant
// RLS, grantor ownership, grantee assignment, idempotency actor and content,
// revocation and expiry) remain in accounts and are not delegated.

// InternalUnitSeedItem is one default internal unit declared by the owner.
type InternalUnitSeedItem struct {
	Code           string
	Kind           string
	Name           string
	DepartmentCode string // only for a kind in DepartmentKinds; dropped by the consumer when the unit lacks it
	SortOrder      int
}

// InternalUnitDefinition lists the owner's internal-unit catalog and seed.
type InternalUnitDefinition struct {
	Kinds           []CatalogItem // display order
	DepartmentKinds []string      // kinds which may reference a department of the same unit
	OrgKinds        []string      // organisation kinds which may own internal units
	SeedStatus      string
	Seed            []InternalUnitSeedItem
}

// DelegationDefinition lists the owner's delegable powers and limits.
type DelegationDefinition struct {
	Powers       []CatalogItem
	MaxDuration  time.Duration
	GranteeRoles []string // assignment roles which may receive a delegation
}

// DelegationGrantDecision is the owner's verdict for a grant in the selected
// session. Allowed=false maps to the consumer's own forbidden error; UnitRef and
// Department are set only when allowed. Grantee eligibility stays native.
type DelegationGrantDecision struct {
	Allowed    bool
	UnitRef    uuid.UUID
	Department string
}

// PeopleScopeMode selects the people visible to the selected session.
type PeopleScopeMode string

const (
	PeopleScopeSelf         PeopleScopeMode = "self"
	PeopleScopeAll          PeopleScopeMode = "all"
	PeopleScopeInternalUnit PeopleScopeMode = "internal_unit"
)

// PeopleScopeDecision tells accounts which query to run with its own current
// data. Only PeopleScopeInternalUnit carries UnitRef and the two roles.
type PeopleScopeDecision struct {
	Mode         PeopleScopeMode
	UnitRef      uuid.UUID
	InternalUnit string
	MemberRole   string
	OfficerRole  string
}

var (
	// ErrUnitDelegationDefinition reports invalid owner data; startup must stop.
	ErrUnitDelegationDefinition = errors.New("accounts: invalid unit/delegation definition")
	// ErrOwnerAttributesRevoked reports that the selected session's attributes
	// are no longer in the owner catalog (consumer: context revoked).
	ErrOwnerAttributesRevoked = errors.New("accounts: selected session attributes are no longer in the owner catalog")
)

// UnitDelegationPolicy is instance-scoped owner configuration.
//
// handled=false is reserved for a genuinely native request (no selected session
// and no compatibility binding); the consumer then keeps its native outcome.
// Every selected or partially bound session is handled, and an error is final.
// Definition accessors return independent copies.
type UnitDelegationPolicy interface {
	InternalUnitDefinition() InternalUnitDefinition
	DelegationDefinition() DelegationDefinition
	// AuthorizeDelegationGrant receives the requested department exactly as sent
	// ("" selects the session department).
	AuthorizeDelegationGrant(ctx context.Context, department string) (DelegationGrantDecision, bool, error)
	// AuthorizeDelegationRevoke checks only the session; grantor ownership, state
	// and tenant stay native. Admin API revocation does not use it.
	AuthorizeDelegationRevoke(ctx context.Context) (handled bool, err error)
	PeopleScope(ctx context.Context) (PeopleScopeDecision, bool, error)
}

var definitionCodeRE = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

func definitionError(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrUnitDelegationDefinition}, args...)...)
}

func checkCatalog(name string, items []CatalogItem) (map[string]bool, error) {
	if len(items) == 0 {
		return nil, definitionError("%s empty", name)
	}
	seen := map[string]bool{}
	for _, it := range items {
		if !definitionCodeRE.MatchString(it.Code) || strings.TrimSpace(it.Label) == "" || it.Label != strings.TrimSpace(it.Label) || seen[it.Code] {
			return nil, definitionError("%s item %q", name, it.Code)
		}
		seen[it.Code] = true
	}
	return seen, nil
}

func checkCodes(name string, codes []string, allowEmpty bool, within map[string]bool) error {
	if len(codes) == 0 && !allowEmpty {
		return definitionError("%s empty", name)
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if !definitionCodeRE.MatchString(c) || seen[c] || (within != nil && !within[c]) {
			return definitionError("%s code %q", name, c)
		}
		seen[c] = true
	}
	return nil
}

// ValidateInternalUnitDefinition checks generic structure only; it accepts no
// legacy defaults. A zero definition is invalid.
func ValidateInternalUnitDefinition(d InternalUnitDefinition) error {
	kinds, err := checkCatalog("kinds", d.Kinds)
	if err != nil {
		return err
	}
	if err := checkCodes("department kinds", d.DepartmentKinds, true, kinds); err != nil {
		return err
	}
	if err := checkCodes("org kinds", d.OrgKinds, false, nil); err != nil {
		return err
	}
	if strings.TrimSpace(d.SeedStatus) == "" {
		return definitionError("seed status empty")
	}
	seen := map[string]bool{}
	for _, s := range d.Seed {
		name := strings.TrimSpace(s.Name)
		switch {
		case !definitionCodeRE.MatchString(s.Code) || seen[s.Code]:
			return definitionError("seed code %q", s.Code)
		case !kinds[s.Kind]:
			return definitionError("seed %q kind", s.Code)
		case name == "" || name != s.Name || utf8.RuneCountInString(name) > 200:
			return definitionError("seed %q name", s.Code)
		case s.SortOrder < 0 || s.SortOrder > 100000:
			return definitionError("seed %q sort order", s.Code)
		case s.DepartmentCode != "" && (!slices.Contains(d.DepartmentKinds, s.Kind) || !definitionCodeRE.MatchString(s.DepartmentCode)):
			return definitionError("seed %q department", s.Code)
		}
		seen[s.Code] = true
	}
	return nil
}

// ValidateDelegationDefinition checks generic structure only. A zero definition
// is invalid; delegations must always be time-limited.
func ValidateDelegationDefinition(d DelegationDefinition) error {
	if _, err := checkCatalog("powers", d.Powers); err != nil {
		return err
	}
	if d.MaxDuration <= 0 {
		return definitionError("max duration")
	}
	return checkCodes("grantee roles", d.GranteeRoles, false, nil)
}
