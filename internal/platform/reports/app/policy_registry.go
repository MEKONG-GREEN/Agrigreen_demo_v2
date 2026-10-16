package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"slices"
)

// PolicyRegistry holds an immutable copy of application contributions.
type PolicyRegistry struct{ policy port.Policy }

func NewPolicyRegistry(policies ...port.Policy) *PolicyRegistry {
	r := &PolicyRegistry{policy: port.Policy{SourceRoles: map[string][]string{}}}
	merge := func(dst, src []string) []string {
		for _, role := range src {
			if role != "" && !slices.Contains(dst, role) {
				dst = append(dst, role)
			}
		}
		return dst
	}
	for _, p := range policies {
		r.policy.ReaderRoles = merge(r.policy.ReaderRoles, p.ReaderRoles)
		r.policy.SchedulerRoles = merge(r.policy.SchedulerRoles, p.SchedulerRoles)
		r.policy.FormReaderRoles = merge(r.policy.FormReaderRoles, p.FormReaderRoles)
		r.policy.OrganizationScopeRoles = merge(r.policy.OrganizationScopeRoles, p.OrganizationScopeRoles)
		for module, roles := range p.SourceRoles {
			r.policy.SourceRoles[module] = merge(r.policy.SourceRoles[module], roles)
		}
		if p.AttachmentLimitParam != "" {
			if r.policy.AttachmentLimitParam != "" && r.policy.AttachmentLimitParam != p.AttachmentLimitParam {
				panic("reports: conflicting attachment limit parameter registrations")
			}
			r.policy.AttachmentLimitParam = p.AttachmentLimitParam
		}
	}
	return r
}
func (r *PolicyRegistry) ReaderRoles() []string {
	if r == nil {
		return nil
	}
	return slices.Clone(r.policy.ReaderRoles)
}
func (r *PolicyRegistry) SchedulerRoles() []string {
	if r == nil {
		return nil
	}
	return slices.Clone(r.policy.SchedulerRoles)
}
func (r *PolicyRegistry) FormReaderRoles() []string {
	if r == nil {
		return nil
	}
	return slices.Clone(r.policy.FormReaderRoles)
}
func (r *PolicyRegistry) OrganizationScopeRoles() []string {
	if r == nil {
		return nil
	}
	return slices.Clone(r.policy.OrganizationScopeRoles)
}
func (r *PolicyRegistry) SourceRoles(module string) []string {
	if r == nil {
		return nil
	}
	return slices.Clone(r.policy.SourceRoles[module])
}
func (r *PolicyRegistry) AttachmentLimitParam() string {
	if r == nil {
		return ""
	}
	return r.policy.AttachmentLimitParam
}
