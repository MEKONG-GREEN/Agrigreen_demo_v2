package port

import (
	"context"

	"github.com/google/uuid"
)

// SystemPeriodProgress is an unfiltered server-side period snapshot for trusted jobs.
// OwnerRef is the stored publisher node, never the user who published the period.
// These fields do not change the HTTP PeriodProgress contract.
type SystemPeriodProgress struct {
	Progress       PeriodProgress
	OwnerRef       *uuid.UUID
	PublisherLevel string
}

// SystemProgressReader reads one known period for a trusted tenant job without
// constructing a user Principal. It is not a user-facing authorization bypass:
// an existing authenticated identity or tenant context must match tenantID.
type SystemProgressReader interface {
	SystemProgress(ctx context.Context, tenantID uuid.UUID, profile string, periodID uuid.UUID) (SystemPeriodProgress, error)
}
