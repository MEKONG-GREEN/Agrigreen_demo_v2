package port

// FieldError attaches a validation error to a field without implementation imports.
type FieldError struct {
	Field string
	Err   error
}

func (e FieldError) Error() string { return e.Field + ": " + e.Err.Error() }
func (e FieldError) Unwrap() error { return e.Err }

// AttributeSpec normalizes a single attribute. Functions are pure and receive no authority.
type AttributeSpec struct {
	Required  bool
	Normalize func(string) (string, error) `json:"-"`
}

// CatalogDefinition declares an owned schema; identity and writes remain in the capability.
type CatalogDefinition struct {
	Catalog        string
	Name           string
	Attributes     map[string]AttributeSpec
	EffectiveDated bool
	Sync           bool
	SeedValidFrom  string
	ValidateAttrs  func(map[string]string) error `json:"-"`
	Constraints    *CatalogConstraints           `json:"-"`
}

type CatalogSeed struct {
	Catalog string            `json:"catalog"`
	Code    string            `json:"code"`
	Name    string            `json:"name"`
	Attrs   map[string]string `json:"attrs"`
}

type CatalogRegistration struct {
	Definitions                []CatalogDefinition
	Seeds                      []CatalogSeed
	InvalidEffectiveDateDetail string
}

// CatalogMetadata is the immutable, serializable description of a schema.
type CatalogMetadata struct {
	Catalog        string
	Name           string
	Attributes     []string
	EffectiveDated bool
	Sync           bool
	SeedValidFrom  string
}
