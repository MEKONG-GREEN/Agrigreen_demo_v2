package port

import (
	"fmt"
	"slices"
)

// WithRoleCatalogOrder returns an independent catalog with the presentation
// order of reference. Only exact permutations of existing lists are accepted:
// no role, grant, MFA requirement or organization restriction can be changed.
// Both arguments are trusted server configuration, never request/session data.
func WithRoleCatalogOrder(catalog, reference *RoleCatalog) (*RoleCatalog, error) {
	if !catalog.Valid() || !reference.Valid() {
		return nil, fmt.Errorf("%w: invalid ordering catalog", ErrRoleCatalog)
	}
	out := catalog.Clone()
	for _, pair := range []struct{ have, order []string }{
		{out.managed, reference.managed}, {out.mfa, reference.mfa},
		{out.loginTOTP, reference.loginTOTP}, {out.adminRoles, reference.adminRoles},
	} {
		if !rolePermutation(pair.have, pair.order) {
			return nil, fmt.Errorf("%w: ordering must preserve every role", ErrRoleCatalog)
		}
	}
	if len(out.grants) != len(reference.grants) {
		return nil, fmt.Errorf("%w: ordering must preserve grantors", ErrRoleCatalog)
	}
	for caller, roles := range out.grants {
		order, ok := reference.grants[caller]
		if !ok || !rolePermutation(roles, order) {
			return nil, fmt.Errorf("%w: ordering must preserve grants for %q", ErrRoleCatalog, caller)
		}
		out.grants[caller] = slices.Clone(order)
	}
	out.managed = slices.Clone(reference.managed)
	out.mfa = slices.Clone(reference.mfa)
	out.loginTOTP = slices.Clone(reference.loginTOTP)
	out.adminRoles = slices.Clone(reference.adminRoles)
	return out, nil
}

func rolePermutation(have, order []string) bool {
	if len(have) != len(order) {
		return false
	}
	seen := make(map[string]bool, len(order))
	for _, role := range order {
		if seen[role] || !slices.Contains(have, role) {
			return false
		}
		seen[role] = true
	}
	return true
}
