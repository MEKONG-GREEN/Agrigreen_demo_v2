package app

import (
	"context"
	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/dispatch"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"time"
)

// Native rules are extended only by explicitly registered application policy.
func (s *Service) readerRoles() []string {
	base := append([]string{}, cghRoles...)
	base = append(base, ccport.RoleCoopManager, port.RoleReportsAdmin, port.RoleMasterdataAdmin)
	return append(base, s.Policies.ReaderRoles()...)
}
func (s *Service) schedulerRoles() []string {
	return append([]string{mechport.RoleCGHAdmin, ccport.RoleCoopManager, authz.RolePlatformAdmin}, s.Policies.SchedulerRoles()...)
}
func (s *Service) formReaderRoles() []string {
	return append([]string{port.RoleReportsAdmin, authz.RolePlatformAdmin}, s.Policies.FormReaderRoles()...)
}
func (s *Service) sourceRoles(source string) []string {
	module := domain.Sources[source].Module
	base := []string{authz.RolePlatformAdmin}
	switch module {
	case "mechanization":
		base = append([]string{}, cghRoles...)
	case "cropcycle":
		base = append(base, ccport.RoleCoopManager)
	}
	return append(base, s.Policies.SourceRoles(module)...)
}
func (s *Service) coopOnly(p authz.Principal) bool {
	return p.Has(ccport.RoleCoopManager) && !p.Has(authz.RolePlatformAdmin) && !hasAny(p, s.Policies.OrganizationScopeRoles())
}
func (s *Service) attachmentLimit(ctx context.Context, at time.Time) (int, error) {
	key := s.Policies.AttachmentLimitParam()
	if key == "" {
		return dispatch.DefaultAttachmentLimitMB, nil
	}
	return s.paramInt(ctx, key, at, decimalInt(dispatch.DefaultAttachmentLimitMB))
}
