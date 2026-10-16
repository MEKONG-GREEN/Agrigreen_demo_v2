package extension

import (
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	reportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// ReportPolicy owns the existing KN realm-role and attachment parameter policy.
// Session-context policy is registered separately by ReportSessionResolver.
func ReportPolicy() reportsport.Policy {
	readers := []string{extport.RoleKNOfficer, extport.RoleKNProvinceAdmin, extport.RoleKNNationalAdmin}
	return reportsport.Policy{
		DispatchProfile:        &reportsport.DispatchProfile{SubjectDepartment: "Phòng Trồng trọt", Salutation: "Kính gửi Chi cục,", SenderName: "Trung tâm Khuyến nông", TemplatePrefix: "kn.", SheetTemplateKey: "kn.gsheet"},
		ReaderRoles:            readers,
		FormReaderRoles:        readers,
		SchedulerRoles:         []string{extport.RoleKNProvinceAdmin, extport.RoleKNNationalAdmin},
		OrganizationScopeRoles: readers,
		SourceRoles:            map[string][]string{"extension": readers, "cropcycle": readers},
		AttachmentLimitParam:   extport.ParamAttachmentLimitMB,
	}
}
