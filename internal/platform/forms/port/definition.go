package port

import (
	"context"
	"encoding/json"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
)

// TemplateDefinition is a detached definition snapshot supplied to the owner policy.
// Fields is canonical JSON, not a mutable reference to a stored schema. The owner
// derives program ownership and structural locks from its own trusted registry.
type TemplateDefinition struct {
	Profile string
	Code    string
	Title   string
	Fields  json.RawMessage
	Fixture bool
}

// TemplateDefinitionRequest is evaluated outside the forms write transaction.
// CurrentID/CurrentVersion identify the snapshot the policy evaluated; forms must
// recheck that version under the definition lock before appending a new version.
// BaseVersion is zero for creation, otherwise the exact version being extended.
type TemplateDefinitionRequest struct {
	Principal      authz.Principal
	Actor          ResolvedActor
	Proposed       TemplateDefinition
	Current        *TemplateDefinition
	CurrentID      uuid.UUID
	CurrentVersion int64
	BaseVersion    *int64
}

// TemplateDefinitionDecision defaults to denial. A registered owner policy must
// explicitly allow the invocation; errors never fall back to Profile.DefineBy.
type TemplateDefinitionDecision struct{ Allowed bool }

// TemplateDefinitionPolicy authorizes writes only. AllowsTemplate remains a read
// filter, including for templates whose structure the owner has locked.
// Registration is server-side before serving requests; never deserialize policy
// functions or owner identity from a client request.
type TemplateDefinitionPolicy func(context.Context, TemplateDefinitionRequest) (TemplateDefinitionDecision, error)
