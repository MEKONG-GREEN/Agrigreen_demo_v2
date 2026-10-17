package port

// CatalogReference names a required active item. The capability resolves it in the caller's transaction.
type CatalogReference struct {
	Catalog string
	Code    string
	Field   string
}

// CatalogConstraintIssue preserves an owner's HTTP error contract without giving it write authority.
type CatalogConstraintIssue struct {
	Status int
	Code   string
	Title  string
	Field  string
}

type CatalogAdminUnitConstraint struct {
	Attribute string
	Level     string
	Issue     CatalogConstraintIssue
}

// CatalogUniqueTokensConstraint reserves tokens across these ordered attributes, including inactive items.
type CatalogUniqueTokensConstraint struct {
	Attributes []string
	Separator  string
	Issue      CatalogConstraintIssue
}

// CatalogConstraints is immutable configuration. References receives a copy of normalized attributes;
// it must be pure and cannot access the caller, tenant, transaction or database through this contract.
// A non-nil empty configuration explicitly declares that no additional constraints apply.
type CatalogConstraints struct {
	StableUUIDAttribute string
	References          func(map[string]string) ([]CatalogReference, error)
	AdminUnit           *CatalogAdminUnitConstraint
	UniqueTokens        *CatalogUniqueTokensConstraint
}
