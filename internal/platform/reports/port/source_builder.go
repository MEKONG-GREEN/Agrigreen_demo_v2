package port

import (
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"time"
)

// SourceBuildQuery is created from an authorized frozen job, never registration input.
type SourceBuildQuery struct {
	Source      string
	JobID       uuid.UUID
	RequestedBy uuid.UUID
	ArgsVersion int
	SourceArgs  json.RawMessage
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
	Source    string
	Build     SourceBuilder
	JobPolicy *SourceJobPolicy
}

// SourceScopeMode controls whether a policy-enabled source also requires geography.
// Tenant scope is an owner-authorized tenant query, never a synthetic Scope.All.
type SourceScopeMode string

const (
	SourceScopeGeographic SourceScopeMode = "geographic"
	SourceScopeTenant     SourceScopeMode = "tenant"
)

type SourceJobOperation string

const (
	SourceJobRequest  SourceJobOperation = "request"
	SourceJobReplay   SourceJobOperation = "replay"
	SourceJobList     SourceJobOperation = "list"
	SourceJobDetail   SourceJobOperation = "detail"
	SourceJobDownload SourceJobOperation = "download"
	SourceJobBuild    SourceJobOperation = "build"
)

// SourceFreezeRequest contains server-bound identity and bounded client arguments.
// Freeze must strictly decode its own schema, canonicalize using RequestedAt,
// and must not echo sensitive arguments in errors.
type SourceFreezeRequest struct {
	Source, TemplateKey, Format  string
	TenantID, JobID, RequestedBy uuid.UUID
	RequestedAt                  time.Time
	SourceArgs                   json.RawMessage
}

// SourceJobAccess checks CURRENT authority. ActorRef is the authenticated HTTP
// reader; it is Nil during build. The worker owner must look up RequestedBy's
// current rights itself, without constructing a Principal from frozen roles.
type SourceJobAccess struct {
	Operation                    SourceJobOperation
	Now                          time.Time
	ActorRef                     uuid.UUID
	Source, TemplateKey, Format  string
	TenantID, JobID, RequestedBy uuid.UUID
	RequestedAt                  time.Time
	ArgsVersion                  int
	Scope                        authz.Scope
	SourceArgs                   json.RawMessage
}
type SourceJobPolicy struct {
	Version      int
	ScopeMode    SourceScopeMode
	Freeze       func(context.Context, SourceFreezeRequest) (json.RawMessage, error)
	AuthorizeJob func(context.Context, SourceJobAccess) error
}
