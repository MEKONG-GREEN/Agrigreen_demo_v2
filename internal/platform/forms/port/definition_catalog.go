package port

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// TemplateVersion is an immutable catalog entry. Consumers pin ID when creating a
// period; selecting a newer catalog version never changes an existing period.
type TemplateVersion struct {
	ID      uuid.UUID
	Profile string
	Code    string
	Version int64
	Title   string
	Fields  json.RawMessage
	Fixture bool
}

// TemplateCatalog extends the read capability without changing Publisher's
// existing contract. All operations apply the owner's profile and code read scope.
// History uses beforeVersion=0 for newest, then the last returned version as cursor.
type TemplateCatalog interface {
	ListTemplates(context.Context, string) ([]TemplateVersion, error)
	TemplateVersion(context.Context, string, string, int64) (TemplateVersion, error)
	TemplateHistory(context.Context, string, string, int64, int) ([]TemplateVersion, error)
}
