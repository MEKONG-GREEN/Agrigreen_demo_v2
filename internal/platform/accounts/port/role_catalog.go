package port

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// ErrRoleCatalog marks invalid trusted server configuration, not an HTTP error.
var ErrRoleCatalog = errors.New("accounts: invalid role catalog")

// RoleRegistration adds a managed realm role owned by an application. It cannot
// redefine a base role or create an account administrator. MFARole also requires
// login TOTP; LoginTOTP alone does not make a role an Admin API MFA role.
type RoleRegistration struct {
	Role      string
	MFARole   bool
	LoginTOTP bool
	OrgKinds  []string
	Grantors  []string
}

// RoleCatalog is an immutable, instance-local view of account role policy.
// Construct it with LegacyRoleCatalog and ExtendRoleCatalog; all returned
// collections are defensive copies. A zero value is invalid and denies queries.
type RoleCatalog struct {
	valid      bool
	managed    []string
	mfa        []string
	loginTOTP  []string
	orgKinds   map[string][]string
	grants     map[string][]string
	adminRoles []string
}

// LegacyRoleCatalog snapshots the existing policy as a temporary compatibility
// bridge. Moving application definitions out of that bridge is a separate step,
// after their owner adapter and assembly registration have been verified.
func LegacyRoleCatalog() *RoleCatalog {
	return &RoleCatalog{
		valid:   true,
		managed: slices.Clone(ManagedRoles), mfa: slices.Clone(MFARoles),
		loginTOTP: slices.Clone(LoginTOTPRoles), orgKinds: cloneRoleMap(RoleOrgKinds),
		grants: cloneRoleMap(Grants), adminRoles: slices.Clone(AdminRoles),
	}
}

// Valid reports whether this catalog was constructed from a valid base.
func (c *RoleCatalog) Valid() bool { return c != nil && c.valid }

// Clone returns an independent snapshot, or nil for an invalid catalog.
func (c *RoleCatalog) Clone() *RoleCatalog {
	if !c.Valid() {
		return nil
	}
	return &RoleCatalog{
		valid:   true,
		managed: slices.Clone(c.managed), mfa: slices.Clone(c.mfa),
		loginTOTP: slices.Clone(c.loginTOTP), orgKinds: cloneRoleMap(c.orgKinds),
		grants: cloneRoleMap(c.grants), adminRoles: slices.Clone(c.adminRoles),
	}
}

var registeredRoleName = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

// ExtendRoleCatalog adds trusted registrations without overriding or removing
// any base restriction. Grantors must already be account administrators, and
// each registration can grant only its own new managed role. The base and input
// slices remain unchanged, including on error. Nil base uses the legacy bridge.
func ExtendRoleCatalog(base *RoleCatalog, registrations ...RoleRegistration) (*RoleCatalog, error) {
	if base == nil {
		base = LegacyRoleCatalog()
	}
	if !base.Valid() {
		return nil, fmt.Errorf("%w: uninitialized base", ErrRoleCatalog)
	}
	out := base.Clone()
	for _, r := range registrations {
		if !registeredRoleName.MatchString(r.Role) || protectedRoleName(r.Role) {
			return nil, fmt.Errorf("%w: invalid or protected role %q", ErrRoleCatalog, r.Role)
		}
		if out.knownRole(r.Role) {
			return nil, fmt.Errorf("%w: duplicate role %q", ErrRoleCatalog, r.Role)
		}
		kinds, err := registrationValues(r.OrgKinds, "organization kind")
		if err != nil {
			return nil, err
		}
		grantors, err := registrationValues(r.Grantors, "grantor")
		if err != nil {
			return nil, err
		}
		for _, g := range grantors {
			if !slices.Contains(out.adminRoles, g) {
				return nil, fmt.Errorf("%w: grantor %q is not an account administrator", ErrRoleCatalog, g)
			}
		}
		out.managed = append(out.managed, r.Role)
		if r.MFARole {
			out.mfa = append(out.mfa, r.Role)
		}
		if r.LoginTOTP || r.MFARole {
			out.loginTOTP = append(out.loginTOTP, r.Role)
		}
		if len(kinds) > 0 {
			out.orgKinds[r.Role] = kinds
		}
		for _, g := range grantors {
			out.grants[g] = append(out.grants[g], r.Role)
		}
	}
	return out, nil
}

func protectedRoleName(role string) bool {
	return slices.Contains([]string{"platform_admin", RoleFarmer, RoleLoginTOTP,
		"offline_access", "uma_authorization", "cgh_sync"}, role) ||
		strings.HasPrefix(role, "default-roles-") || strings.HasPrefix(role, "service-account-")
}

func (c *RoleCatalog) knownRole(role string) bool {
	if c.IsManaged(role) || slices.Contains(c.mfa, role) || slices.Contains(c.loginTOTP, role) || slices.Contains(c.adminRoles, role) {
		return true
	}
	if _, ok := c.orgKinds[role]; ok {
		return true
	}
	if _, ok := c.grants[role]; ok {
		return true
	}
	for _, roles := range c.grants {
		if slices.Contains(roles, role) {
			return true
		}
	}
	return false
}

func registrationValues(values []string, field string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if strings.TrimSpace(v) != v || v == "" || slices.Contains(out, v) {
			return nil, fmt.Errorf("%w: blank, untrimmed or duplicate %s", ErrRoleCatalog, field)
		}
		out = append(out, v)
	}
	return out, nil
}

func cloneRoleMap(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for k, values := range in {
		out[k] = slices.Clone(values)
	}
	return out
}

// IsManaged reports whether a role is managed by the account API.
func (c *RoleCatalog) IsManaged(role string) bool {
	return c.Valid() && slices.Contains(c.managed, role)
}

// ManagedRoles returns the managed realm role names in catalog order.
func (c *RoleCatalog) ManagedRoles() []string {
	if !c.Valid() {
		return nil
	}
	return slices.Clone(c.managed)
}

// MFARoles returns roles marked MFA for the Admin API policy.
func (c *RoleCatalog) MFARoles() []string {
	if !c.Valid() {
		return nil
	}
	return slices.Clone(c.mfa)
}

// LoginTOTPRoles returns roles requiring TOTP enrollment at login.
func (c *RoleCatalog) LoginTOTPRoles() []string {
	if !c.Valid() {
		return nil
	}
	return slices.Clone(c.loginTOTP)
}

// NeedsMFA reports whether any supplied realm role is marked MFA.
func (c *RoleCatalog) NeedsMFA(roles []string) bool {
	return c.Valid() && anyCatalogRole(c.mfa, roles)
}

// NeedsLoginTOTP reports whether any supplied realm role requires login TOTP.
func (c *RoleCatalog) NeedsLoginTOTP(roles []string) bool {
	return c.Valid() && anyCatalogRole(c.loginTOTP, roles)
}

func anyCatalogRole(catalog, roles []string) bool {
	for _, role := range roles {
		if slices.Contains(catalog, role) {
			return true
		}
	}
	return false
}

// Grantable returns the union of grantable roles for the authenticated realm
// roles. It does not consider selected application sessions or add admin roles.
func (c *RoleCatalog) Grantable(callerRoles []string) []string {
	if !c.Valid() {
		return nil
	}
	var out []string
	for _, caller := range callerRoles {
		for _, role := range c.grants[caller] {
			if !slices.Contains(out, role) {
				out = append(out, role)
			}
		}
	}
	return out
}

// RoleOrgKinds returns a deep copy of the role to organization-kind restrictions.
func (c *RoleCatalog) RoleOrgKinds() map[string][]string {
	if !c.Valid() {
		return nil
	}
	return cloneRoleMap(c.orgKinds)
}

// OrgKinds returns the organization kinds allowed for a role; empty means there
// is no additional kind restriction. Role recognition is checked separately.
func (c *RoleCatalog) OrgKinds(role string) []string {
	if !c.Valid() {
		return nil
	}
	return slices.Clone(c.orgKinds[role])
}
