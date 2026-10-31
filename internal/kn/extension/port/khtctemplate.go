package port

// W11.65 — KN owns report authoring; pf/export remains a generic immutable renderer.
import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// KHTCTemplateSnapshot pins a published fixed report definition, including its hash.
// Historical drafts without this field retain their original fixed layout.
type KHTCTemplateSnapshot struct {
	Mode           string          `json:"mode"` // legacy | published; never inferred from a failed lookup.
	Rule           string          `json:"rule"`
	ID             uuid.UUID       `json:"id"`
	Key            string          `json:"key"`
	Version        int64           `json:"version"`
	Hash           string          `json:"hash"`
	Definition     json.RawMessage `json:"definition"`
	ApplicableFrom time.Time       `json:"applicable_from"`
	LegacyUntil    *time.Time      `json:"legacy_until,omitempty"`
	WordKey        string          `json:"word_key,omitempty"`
	WordVersion    int             `json:"word_version,omitempty"`
	WordHash       string          `json:"word_hash,omitempty"`
	ExcelRule      string          `json:"excel_rule,omitempty"`
}

// KHTCFixedTemplates resolves only mau_1..mau_5. Tenant and actor come from ctx.
// Resolve is called outside a report write transaction. ByID never selects a newer version.
type KHTCFixedTemplates interface {
	// Select explicitly identifies builtin legacy before this key's first effective
	// publication, and strictly resolves published definitions from that date onward.
	Select(ctx context.Context, key string, periodStart time.Time) (KHTCTemplateSnapshot, error)
	Resolve(ctx context.Context, key string, periodStart time.Time) (KHTCTemplateSnapshot, error)
	ByID(ctx context.Context, id uuid.UUID) (KHTCTemplateSnapshot, error)
}

// KHTCTemplateRow contains approved reporting values, not raw records or household PII.
type KHTCTemplateRow struct {
	Ref        string            `json:"ref"`
	ParentRef  string            `json:"parent_ref,omitempty"`
	ProgramRef string            `json:"program_ref,omitempty"`
	Department string            `json:"department,omitempty"`
	Status     string            `json:"status,omitempty"` // normal | no_send | unclosed | no_activity | provisional
	Values     map[string]string `json:"values"`
}

// KHTCTemplateContext is a read-only snapshot captured before authoring transactions.
// Both preview and the fixed-report composer consume this projection.
type KHTCTemplateContext struct {
	PeriodID      uuid.UUID                    `json:"period_id"`
	PeriodName    string                       `json:"period_name"`
	PeriodType    string                       `json:"period_type"`
	PeriodStart   time.Time                    `json:"period_start"`
	PlanVersionID uuid.UUID                    `json:"plan_version_id"`
	Values        map[string]string            `json:"values"`
	Lists         map[string][]KHTCTemplateRow `json:"lists"`
	Texts         map[string]string            `json:"texts"`
	Provisional   []string                     `json:"provisional"`
	InputHash     string                       `json:"input_hash"`
}

type KHTCTemplateContextReader interface {
	TemplateContext(ctx context.Context, key string, periodID uuid.UUID) (KHTCTemplateContext, error)
}
