package port

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"time"
)

// SourceBuildQuery is created from an authorized frozen job, never registration input.
type SourceBuildQuery struct {
	Source      string
	TenantID    uuid.UUID
	Scope       authz.Scope
	At          time.Time
	Unit        string
	Level       string
	SeasonCode  string
	SeasonYear  int
	Periods     []string
	PeriodLabel string
	Columns     []SourceBuildColumn
}
type SourceBuildColumn struct{ Key string }
type SourceCell struct {
	Text string
	Dec  *decimal.Decimal
}
type SourceRow map[string]SourceCell
type SourceMeta struct {
	Label string
	Value string
}
type SourceData struct {
	Rows        []SourceRow
	Total       SourceRow
	Meta        []SourceMeta
	Provisional bool
	Notes       []string
}
type SourceBuilder func(context.Context, SourceBuildQuery) (SourceData, error)
type SourceBuilderRegistration struct {
	Source string
	Build  SourceBuilder
}
