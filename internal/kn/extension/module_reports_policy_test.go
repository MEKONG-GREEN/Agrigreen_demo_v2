package extension

import (
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"slices"
	"testing"
)

func TestReportPolicyRegistration(t *testing.T) {
	r := ReportPolicy()
	for _, role := range []string{extport.RoleKNOfficer, extport.RoleKNProvinceAdmin, extport.RoleKNNationalAdmin} {
		if !slices.Contains(r.ReaderRoles, role) || !slices.Contains(r.FormReaderRoles, role) || !slices.Contains(r.OrganizationScopeRoles, role) || !slices.Contains(r.SourceRoles["extension"], role) || !slices.Contains(r.SourceRoles["cropcycle"], role) {
			t.Fatalf("KN grant missing %s", role)
		}
		if slices.Contains(r.SchedulerRoles, role) != (role != extport.RoleKNOfficer) {
			t.Fatalf("scheduler grant changed %s", role)
		}
	}
	if len(r.SourceRoles["mechanization"]) != 0 || r.AttachmentLimitParam != extport.ParamAttachmentLimitMB {
		t.Fatal("KN policy crossed sources or changed approved parameter")
	}
}
