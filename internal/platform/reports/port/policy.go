package port

// Policy is a trusted application registration, never request input. It adds
// roles to the capability's native rules; actor/tenant and data scope stay in
// reports. SourceRoles is keyed by the data-source module.
type Policy struct {
	DispatchProfile        *DispatchProfile
	ReaderRoles            []string
	SchedulerRoles         []string
	FormReaderRoles        []string
	OrganizationScopeRoles []string
	SourceRoles            map[string][]string
	AttachmentLimitParam   string
}

// DispatchProfile supplies application-owned message labels and egress namespaces.
// It cannot supply identity or data scope.
type DispatchProfile struct {
	SubjectDepartment string
	Salutation        string
	SenderName        string
	TemplatePrefix    string
	SheetTemplateKey  string
}
