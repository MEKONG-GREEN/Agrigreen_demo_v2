package port

// FormSeedRegistration is trusted startup configuration, never request input.
// A single default set supplies the existing seed endpoint.
type FormSeedRegistration struct {
	Key               string
	Default           bool
	DefaultDepartment string
	Version           string
	Templates         []FormSeedTemplate
}
type FormSeedTemplate struct {
	Code       string
	Name       string
	EntryLevel string
	ReportType string
	Def        FormDefinition
}
