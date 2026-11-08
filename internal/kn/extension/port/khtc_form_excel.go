package port

import "github.com/google/uuid"

// KHTCFormExcelSnapshot is an explicit, detached server-owned form projection.
// It is NOT a publication, authorization grant, file receipt or persistence API.
// The application owner must supply actual published/assigned template identities
// and recheck current tenant/session/scope/MFA/read-only/status before every use.
// No producer or live Excel endpoint is registered by the technical codec.
type KHTCFormExcelSnapshot struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	PeriodID   uuid.UUID
	Department string
	Sheets     []KHTCFormExcelSheet
	Periods    []KHTCFormExcelPeriod
}

type KHTCFormExcelSheet struct {
	Key              string
	Name             string
	Kind             string // grid | narrative; narrative row codes are stable section IDs.
	TemplateID       uuid.UUID
	TemplateVersion  int64
	TemplateHash     string
	Columns          []KHTCFormExcelColumn
	Rows             []KHTCFormExcelRow
	AllowAdjustments bool
}

type KHTCFormExcelColumn struct {
	Letter      string
	Title       string
	Kind        string // fixed | from_plan | prior_cumulative | input | formula
	DataType    string // text | number | integer
	Required    bool
	NonNegative bool
	MaxLength   int // runes; 0 uses the codec's bounded text default.
	Choices     []string
	// This technical slice supports explicit server_value formula projections
	// only. Excel expression translation/emission belongs to the published-schema
	// producer/integration remainder; it must never be silently literalized.
	FormulaMode string
}

type KHTCFormExcelRow struct {
	Code             string
	SystemValues     map[string]string // exact trusted values by letter, including formula projections.
	Locked           []string          // additionally locked input letters, e.g. module-owned values.
	AdjustmentLocked bool              // owner explicitly forbids adjustments for this row.
}

type KHTCFormExcelPeriod struct {
	ID   uuid.UUID // an allowed closed adjustment period supplied by the owner.
	Name string
}

// KHTCFormExcelContent contains editable values only. Rows are addressed by
// sheet key -> item code (or stable narrative section ID) -> configured letter.
// It is a detached in-memory editor shape, never an import/file/save receipt.
type KHTCFormExcelContent struct {
	Rows        map[string]map[string]map[string]string
	Adjustments []KHTCFormExcelAdjustment
}

type KHTCFormExcelAdjustment struct {
	Sheet    string
	ItemCode string
	PeriodID uuid.UUID
	Amount   string
	Reason   string
}

type KHTCFormExcelIssue struct {
	Sheet    string
	Row      int
	ItemCode string
	Column   string
	Severity string // error | ignored
	Code     string
	Message  string // fixed diagnostic; never includes an uploaded value or formula.
}

type KHTCFormExcelSheetSummary struct {
	Sheet        string
	Rows         int
	Valid        int
	Invalid      int
	IgnoredCells int
}

// KHTCFormExcelValidation is a detached report, not trusted Merge input. The
// byte hash/fingerprint binds technical data only, never authority or provenance.
type KHTCFormExcelValidation struct {
	SnapshotHash string
	FileSHA256   string
	Sheets       []KHTCFormExcelSheetSummary
	Issues       []KHTCFormExcelIssue
	ValidRows    int
}

// KHTCFormExcelPreview is an opaque memory-only preview. Result returns a copy;
// changing it cannot change the validated data later passed to Merge.
type KHTCFormExcelPreview interface {
	Result() KHTCFormExcelValidation
}

// KHTCFormExcelCodec has no DB, file store, HTTP, session or clock dependency.
// Merge returns unsaved data only; it neither saves nor consumes a send attempt.
// A preview must belong to the same pinned codec and is revalidated at merge.
// keep/replace apply per existing editable cell; adjustments apply per
// (sheet,itemCode,period) group while retaining valid rows within that group.
type KHTCFormExcelCodec interface {
	SnapshotHash() string
	Generate(KHTCFormExcelContent) ([]byte, error)
	Preview([]byte) (KHTCFormExcelPreview, error)
	Merge(KHTCFormExcelContent, KHTCFormExcelPreview, string) (KHTCFormExcelContent, error)
	ErrorWorkbook(KHTCFormExcelPreview) ([]byte, error)
}
