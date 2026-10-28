package port

import "slices"

// NativeRoleCatalog snapshots account policy without application-owned roles.
// Applications may explicitly extend it with their trusted registrations. It is
// an opt-in base; omitted module configuration still uses LegacyRoleCatalog.
func NativeRoleCatalog() *RoleCatalog {
	managed := []string{
		RoleAccountAdmin, RoleAccountAdminProvince,
		"param_setter", "param_approver", "masterdata_admin", "registry_admin", "reports_admin",
		"consent_writer", "consent_reader", "coop_manager",
		"cgh_viewer", "cgh_admin",
		"plot_editor", "plot_viewer", "geo_admin",
		"content_editor_national", "content_editor_province", "content_reviewer",
		"mrv_collector", "mrv_reviewer", "mrv_viewer", "mrv_exporter",
		"device_admin", "sync_conflict_resolver",
		"vault_registrar", "vault_display", "vault_directory", "vault_evidence_viewer", "vault_eraser", "vault_pubkey_sync",
	}
	// Select native keys, retaining their existing organization-kind policy.
	// Copy values so extending a catalog cannot mutate the compatibility bridge.
	kinds := make(map[string][]string)
	for _, role := range []string{"coop_manager", RoleAccountAdminProvince, "cgh_viewer", "content_editor_province"} {
		kinds[role] = slices.Clone(RoleOrgKinds[role])
	}
	return &RoleCatalog{
		valid: true, managed: managed,
		mfa: slices.Clone(MFARoles), loginTOTP: slices.Clone(MFARoles),
		orgKinds: kinds,
		grants: map[string][]string{
			"platform_admin":         slices.Clone(managed),
			RoleAccountAdmin:         without(managed, RoleAccountAdmin, "param_approver", "vault_eraser"),
			RoleAccountAdminProvince: {"coop_manager"},
		},
		adminRoles: slices.Clone(AdminRoles),
	}
}
