package port

import (
	"context"
	"errors"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
)

var (
	ErrTransferConfiguration = errors.New("party: invalid transfer registration")
	ErrTransferAuthority     = errors.New("party: transfer authority required")
	ErrTransferConflict      = errors.New("party: transfer evidence conflict")
	ErrTransferNotReady      = errors.New("party: transfer evidence is not ready")
	ErrTransferDependency    = errors.New("party: transfer owner unavailable")
)

// TransferTokenVerifier is the real API verifier, not an owner boolean.
// Raw tokens remain in authenticated contexts and are never persisted by transfers.
type TransferTokenVerifier interface {
	Verify(context.Context, string) (authz.Principal, error)
}

type TransferDigest string

type TransferSide string

const (
	TransferSource TransferSide = "source"
	TransferTarget TransferSide = "target"
)

// TransferParticipants holds two actual server-authenticated request contexts.
// It is never a JSON request or a synthesized cross-tenant Principal.
type TransferParticipants struct{ source, target context.Context }

func NewTransferParticipants(source, target context.Context) (TransferParticipants, error) {
	sp, sok := transferPrincipal(source)
	tp, tok := transferPrincipal(target)
	if !sok || !tok || sp.TenantID == tp.TenantID {
		return TransferParticipants{}, ErrTransferAuthority
	}
	return TransferParticipants{source: source, target: target}, nil
}
func transferPrincipal(ctx context.Context) (authz.Principal, bool) {
	if ctx == nil || ctx.Err() != nil {
		return authz.Principal{}, false
	}
	p, ok := authz.FromContext(ctx)
	_, raw := authz.RawToken(ctx)
	return p, raw && ok && p.ActorRef != uuid.Nil && p.TenantID != uuid.Nil && p.MFA && p.Has(authz.RolePlatformAdmin)
}
func (p TransferParticipants) Source() context.Context { return p.source }
func (p TransferParticipants) Target() context.Context { return p.target }
func (p TransferParticipants) Validate() error {
	sp, sok := transferPrincipal(p.source)
	tp, tok := transferPrincipal(p.target)
	if !sok || !tok || sp.TenantID == tp.TenantID {
		return ErrTransferAuthority
	}
	return nil
}

type TransferScope struct {
	SourceTenant   uuid.UUID      `json:"source_tenant"`
	SourceScopeRef uuid.UUID      `json:"source_scope_ref"`
	TargetTenant   uuid.UUID      `json:"target_tenant"`
	TargetScopeRef uuid.UUID      `json:"target_scope_ref"`
	IntentID       uuid.UUID      `json:"intent_id"`
	PlanID         uuid.UUID      `json:"plan_id"`
	BindingHash    TransferDigest `json:"binding_hash"`
}
type TransferKey struct {
	Namespace string    `json:"namespace"`
	Ref       uuid.UUID `json:"ref"`
}
type TransferReference struct {
	Key            TransferKey `json:"key"`
	External       bool        `json:"external"`
	ExternalTenant uuid.UUID   `json:"external_tenant,omitempty"`
	BasisRef       uuid.UUID   `json:"basis_ref,omitempty"`
}
type TransferHeader struct {
	Key            TransferKey         `json:"key"`
	SourceTenant   uuid.UUID           `json:"source_tenant"`
	SourceScopeRef uuid.UUID           `json:"source_scope_ref"`
	SourceRowHash  TransferDigest      `json:"source_row_hash"`
	LogicalHash    TransferDigest      `json:"logical_hash"`
	SourceSeq      int64               `json:"source_seq"`
	References     []TransferReference `json:"references"`
}
type TransferApproval struct {
	Ref        uuid.UUID `json:"ref"`
	ActorRef   uuid.UUID `json:"actor_ref"`
	Epoch      string    `json:"epoch"`
	ValidUntil time.Time `json:"valid_until"`
}
type TransferAuthorityRequest struct {
	Scope     TransferScope
	Namespace string
	Side      TransferSide
	Action    string
}
type TransferAuthority func(context.Context, TransferAuthorityRequest) (TransferApproval, error)

type TransferFence struct {
	Ref           uuid.UUID      `json:"ref"`
	Epoch         string         `json:"epoch"`
	HighWatermark string         `json:"high_watermark"`
	HeaderRoot    TransferDigest `json:"header_root"`
	HeaderCount   int64          `json:"header_count"`
}
type TransferFenceRequest struct {
	Scope     TransferScope
	Namespace string
	Side      TransferSide
}
type TransferFenceWriter func(context.Context, TransferFenceRequest) (TransferFence, error)
type TransferFenceChecker func(context.Context, TransferFenceRequest, TransferFence) error

type TransferScanRequest struct {
	Scope     TransferScope
	Namespace string
	Fence     TransferFence
	Cursor    string
	Limit     int
	DryRun    bool
}
type TransferPage struct {
	Items    []TransferHeader
	Next     string
	Complete bool
}
type TransferScanner func(context.Context, TransferScanRequest) (TransferPage, error)

type TransferMapping struct {
	Key       TransferKey `json:"key"`
	TargetRef uuid.UUID   `json:"target_ref"`
}
type TransferArtifact struct {
	Ref              uuid.UUID      `json:"ref"`
	ExportReceiptRef uuid.UUID      `json:"export_receipt_ref"`
	Hash             TransferDigest `json:"hash"`
	HeaderRoot       TransferDigest `json:"header_root"`
	ManifestHash     TransferDigest `json:"manifest_hash"`
	MapHash          TransferDigest `json:"map_hash"`
	BindingHash      TransferDigest `json:"binding_hash"`
	OwnerVersion     string         `json:"owner_version"`
}
type TransferBatchRequest struct {
	Scope        TransferScope
	Namespace    string
	BatchID      uuid.UUID
	ManifestHash TransferDigest
	MapHash      TransferDigest
	Headers      []TransferHeader
	Mappings     []TransferMapping
	// DependencyMappings contain only referenced predecessors with verified receipts.
	// They are not batch members and may never inflate receipt item counts.
	DependencyMappings []TransferMapping
	SourceFence        TransferFence
	TargetFence        TransferFence
}
type TransferExporter func(context.Context, TransferBatchRequest) (TransferArtifact, error)
type TransferExportReader func(context.Context, TransferBatchRequest) (TransferArtifact, error)
type TransferApplyRequest struct {
	TransferBatchRequest
	Artifact TransferArtifact
}
type TransferAppliedRef struct {
	Key           TransferKey    `json:"key"`
	TargetRef     uuid.UUID      `json:"target_ref"`
	TargetRowHash TransferDigest `json:"target_row_hash"`
	LogicalHash   TransferDigest `json:"logical_hash"`
}
type TransferReceipt struct {
	// Recovery fields are nil on apply; compensation binds its reserved operation,
	// reason and original durable receipt. SourceLogicalRoot retains the source
	// commitment while LogicalRoot describes the actual compensated result.
	OperationID       uuid.UUID            `json:"operation_id,omitempty"`
	ReasonRef         uuid.UUID            `json:"reason_ref,omitempty"`
	CompensatesRef    uuid.UUID            `json:"compensates_ref,omitempty"`
	SourceLogicalRoot TransferDigest       `json:"source_logical_root,omitempty"`
	Ref               uuid.UUID            `json:"ref"`
	PlanID            uuid.UUID            `json:"plan_id"`
	BatchID           uuid.UUID            `json:"batch_id"`
	TargetTenant      uuid.UUID            `json:"target_tenant"`
	TargetScopeRef    uuid.UUID            `json:"target_scope_ref"`
	BindingHash       TransferDigest       `json:"binding_hash"`
	ManifestHash      TransferDigest       `json:"manifest_hash"`
	MapHash           TransferDigest       `json:"map_hash"`
	ArtifactHash      TransferDigest       `json:"artifact_hash"`
	LogicalRoot       TransferDigest       `json:"logical_root"`
	OwnerVersion      string               `json:"owner_version"`
	Items             []TransferAppliedRef `json:"items"`
}

// TransferImporter may be called concurrently for the same (plan_id, batch_id),
// including through independent core instances after a lost checkpoint. The
// owner MUST enforce that unique pair in its database and commit the receipt,
// ledger rows and provenance in one transaction. A retry returns the same durable
// receipt; a different request hash for that pair must fail. An owner lock may
// serialize the check/write, but an in-memory mutex is not sufficient.
type TransferImporter func(context.Context, TransferApplyRequest) (TransferReceipt, error)
type TransferReceiptReader func(context.Context, TransferApplyRequest) (TransferReceipt, error)
type TransferVerifier func(context.Context, TransferApplyRequest, TransferReceipt) error

// Recovery-only read of the immutable original apply receipt/provenance; it must
// not require the old normal approval or assert that the target row is still active.
type TransferRecoveryReceiptReader func(context.Context, TransferRecoveryRequest, TransferApplyRequest) (TransferReceipt, error)

type TransferRecoveryRequest struct {
	Scope                  TransferScope
	OperationID, ReasonRef uuid.UUID
}
type TransferFenceRelease struct {
	Ref, IntentID, PlanID  uuid.UUID
	OperationID, ReasonRef uuid.UUID
	BindingHash            TransferDigest
	Fence                  TransferFence
	Side                   TransferSide
	Namespace              string
}

// TransferRecoveryGate is a durable owner phase transition. Every namespace and
// side must enter, including a scope whose exact holder never acquired a fence.
// Fence{} acknowledges durable absence under the SAME stable scope lock used by
// fence acquisition, Export and Apply. It is an immutable tombstone preventing
// late acquisition by that holder even after its recovery barrier is released.
// A fence appearing between ReadFenceForRecovery and zero-fence Enter must cause
// Conflict, so the core rediscovers the actual fence before retrying. The original
// fence epoch stays unchanged; gate receipts use their own operation/reason.
type TransferRecoveryGate struct {
	Ref, OperationID, ReasonRef, IntentID, PlanID uuid.UUID
	BindingHash                                   TransferDigest
	Fence                                         TransferFence
	Side                                          TransferSide
	Namespace                                     string
}

// TransferRecoveryFenceReader is read-only under fresh recovery authority.
// NotReady means this exact holder was never created, not unknown retention or
// database failure; a known core fence missing at its owner must fail closed.
type TransferRecoveryFenceReader func(context.Context, TransferRecoveryRequest, TransferFenceRequest) (TransferFence, error)

type TransferRecoveryGateWriter func(context.Context, TransferRecoveryRequest, TransferFenceRequest, TransferFence) (TransferRecoveryGate, error)
type TransferRecoveryGateReader func(context.Context, TransferRecoveryRequest, TransferFenceRequest, TransferFence) (TransferRecoveryGate, error)

// TransferFenceReleaser checks the recovery gate and exact active holder under
// the same owner transaction as its durable release receipt. A reader must read
// the immutable historical receipt before checking any newer active holder; an
// exact old replay returns its old outcome without opening the newer holder.
type TransferFenceReleaser func(context.Context, TransferRecoveryRequest, TransferFenceRequest, TransferFence) (TransferFenceRelease, error)
type TransferFenceReleaseReader func(context.Context, TransferRecoveryRequest, TransferFenceRequest, TransferFence) (TransferFenceRelease, error)

type TransferCompensationRequest struct {
	TransferApplyRequest
	OperationID uuid.UUID
	ReasonRef   uuid.UUID
	Applied     TransferReceipt
}
type TransferCompensator func(context.Context, TransferCompensationRequest) (TransferReceipt, error)
type TransferCompensationReader func(context.Context, TransferCompensationRequest) (TransferReceipt, error)

type TransferCompensationVerifier func(context.Context, TransferCompensationRequest, TransferReceipt) error

// TransferAdapter is trusted owner code. No raw data or SQL crosses this port.
// All source functions receive the actual source context; target functions the target context.
type TransferAdapter struct {
	ReadFenceForRecovery       TransferRecoveryFenceReader `json:"-"`
	EnterRecoveryGate          TransferRecoveryGateWriter  `json:"-"`
	ReadRecoveryGate           TransferRecoveryGateReader  `json:"-"`
	Module, Namespace, Version string
	DependsOn                  []string
	MaxBatch                   int
	Authority                  TransferAuthority             `json:"-"`
	ReadAppliedForRecovery     TransferRecoveryReceiptReader `json:"-"`
	RecoveryAuthority          TransferAuthority             `json:"-"`
	ReleaseSourceFence         TransferFenceReleaser         `json:"-"`
	ReadSourceFenceRelease     TransferFenceReleaseReader    `json:"-"`
	ReleaseTargetFence         TransferFenceReleaser         `json:"-"`
	ReadTargetFenceRelease     TransferFenceReleaseReader    `json:"-"`
	FenceSource                TransferFenceWriter           `json:"-"`
	FenceTarget                TransferFenceWriter           `json:"-"`
	CheckSourceFence           TransferFenceChecker          `json:"-"`
	CheckTargetFence           TransferFenceChecker          `json:"-"`
	Scan                       TransferScanner               `json:"-"`
	Export                     TransferExporter              `json:"-"`
	ReadExport                 TransferExportReader          `json:"-"`
	Apply                      TransferImporter              `json:"-"`
	ReadReceipt                TransferReceiptReader         `json:"-"`
	Verify                     TransferVerifier              `json:"-"`
	Compensate                 TransferCompensator           `json:"-"`
	ReadCompensation           TransferCompensationReader    `json:"-"`
	VerifyCompensation         TransferCompensationVerifier  `json:"-"`
}

type TransferIntentInput struct {
	OperationID                    uuid.UUID
	SourceScopeRef, TargetScopeRef uuid.UUID
	Namespaces                     []string
}
type TransferRef struct{ ID uuid.UUID }
type TransferBatchRef struct{ PlanID, BatchID uuid.UUID }

type TransferIntent struct {
	ID           uuid.UUID                `json:"id"`
	Scope        TransferScope            `json:"scope"`
	Adapters     []TransferAdapterVersion `json:"adapters"`
	TargetFences map[string]TransferFence `json:"target_fences"`
}
type TransferAdapterVersion struct {
	Module    string   `json:"module"`
	Namespace string   `json:"namespace"`
	Version   string   `json:"version"`
	DependsOn []string `json:"depends_on"`
}
type TransferPlan struct {
	ID           uuid.UUID                `json:"id"`
	Scope        TransferScope            `json:"scope"`
	Status       string                   `json:"status"`
	ManifestHash TransferDigest           `json:"manifest_hash"`
	Count        int                      `json:"count"`
	Adapters     []TransferAdapterVersion `json:"adapters"`
}
type TransferBatch struct {
	ID           uuid.UUID         `json:"id"`
	PlanID       uuid.UUID         `json:"plan_id"`
	Namespace    string            `json:"namespace"`
	ManifestHash TransferDigest    `json:"manifest_hash"`
	MapHash      TransferDigest    `json:"map_hash"`
	Mappings     []TransferMapping `json:"mappings"`
}
type TransferReport struct {
	PlanID      uuid.UUID      `json:"plan_id"`
	Count       int            `json:"count"`
	Root        TransferDigest `json:"root"`
	Provisional bool           `json:"provisional"`
}

type TenantTransfers interface {
	Abort(TransferParticipants, TransferRef, uuid.UUID) (TransferPlan, error)
	ReleaseFences(TransferParticipants, TransferRef, uuid.UUID) (TransferPlan, error)
	TargetIntent(TransferParticipants, TransferIntentInput) (TransferIntent, error)
	Propose(TransferParticipants, TransferRef) (TransferPlan, error)
	DryRun(TransferParticipants, TransferRef) (TransferReport, error)
	FreezeTarget(TransferParticipants, TransferRef) (TransferIntent, error)
	FreezeSource(TransferParticipants, TransferRef) (TransferPlan, error)
	ScanNext(TransferParticipants, TransferRef) (TransferPlan, error)
	Seal(TransferParticipants, TransferRef) (TransferPlan, error)
	Accept(TransferParticipants, TransferRef) (TransferPlan, error)
	ReserveBatch(TransferParticipants, TransferRef, int) (TransferBatch, error)
	ExportBatch(TransferParticipants, TransferBatchRef) (TransferArtifact, error)
	ApplyBatch(TransferParticipants, TransferBatchRef) (TransferReceipt, error)
	Verify(TransferParticipants, TransferRef) (TransferReport, error)
	PrepareCutover(TransferParticipants, TransferRef) (TransferPlan, error)
	Compensate(TransferParticipants, TransferBatchRef, uuid.UUID) (TransferReceipt, error)
	Status(TransferParticipants, TransferRef) (TransferPlan, error)
}
