package port

// Parameter kinds are metadata; numerical bounds remain advisory.
const (
	ParamKindDecimal = "decimal"
	ParamKindText    = "text"
	ParamKindColor   = "color"
)

// ParamSpec describes a base key and optional dimensions.
type ParamSpec struct {
	Key         string // khóa, hoặc tiền tố với khóa có chiều (Dimensional)
	Name        string
	Unit        string
	Kind        string
	Min, Max    string
	Dimensional bool // khóa có chiều: Key + ":<chiều>…"
}

// ParamSeed is an approved initial value. The capability supplies system actors,
// transactions, ledger writes and idempotency; a registration cannot supply them.
type ParamSeed struct {
	Key           string  `json:"key"`
	Value         *string `json:"value,omitempty"`
	Text          *string `json:"text,omitempty"`
	Unit          string  `json:"unit"`
	EffectiveFrom string  `json:"effective_from"`
	SourceDoc     string  `json:"source_doc"`
}

// ParamRegistration adds owned parameter metadata and initial values.
type ParamRegistration struct {
	Specs []ParamSpec
	Seeds []ParamSeed
}
