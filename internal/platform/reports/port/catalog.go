package port

// CatalogRegistration is trusted startup metadata supplied by an application.
// It contains no actor, tenant, database access or request-supplied policy.
type CatalogRegistration struct {
	Sources   map[string]CatalogSource
	Templates []CatalogTemplate
}
type CatalogSource struct {
	Key          string
	Module       string
	Levels       []string
	DefaultLevel string
	NeedsPeriod  bool
	UsesSeason   bool
	NeedsCompare bool
	Columns      []CatalogColumn
}
type CatalogColumn struct {
	Key      string
	Title    string
	Kind     string
	Scale    int32
	MinLevel string
	MaxLevel string
}
type CatalogTemplate struct {
	Key string
	Def CatalogDefinition
}
type CatalogDefinition struct {
	Title       string
	Source      string
	Grouping    string
	Columns     []CatalogTemplateColumn
	Formats     []string
	Provisional bool
	FormNote    string
}
type CatalogTemplateColumn struct {
	Key   string
	Title string
}
