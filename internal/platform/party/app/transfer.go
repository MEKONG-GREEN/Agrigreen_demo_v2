package app

// Generic two-context protocol. No HTTP/CLI route or owner adapter is enabled here.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var transferModule = db.Module("pf/party")

type Transfers struct {
	database tenant.Beginner
	clock    clock.Clock
	registry *port.TransferRegistry
}

var _ port.TenantTransfers = (*Transfers)(nil)

func NewTransfers(database tenant.Beginner, clk clock.Clock, registry *port.TransferRegistry) *Transfers {
	return &Transfers{database: database, clock: clk, registry: registry}
}
func (t *Transfers) authenticate(p port.TransferParticipants) error {
	if t == nil || t.registry == nil || t.database == nil || t.clock == nil {
		return port.ErrTransferConfiguration
	}
	if err := t.registry.Authenticate(p); err != nil {
		return err
	}
	for _, ctx := range []context.Context{p.Source(), p.Target()} {
		principal, ok := authz.FromContext(ctx)
		tid, err := tenant.FromContext(ctx)
		if !ok || err != nil || tid != principal.TenantID {
			return port.ErrTransferAuthority
		}
	}
	return nil
}
func (t *Transfers) local(ctx context.Context, fn func(pgx.Tx) error) error {
	return db.WithContextTx(ctx, t.database, transferModule, fn)
}

// Unknown owner errors must not leak SQL/private artifact details or be mistaken
// for an absent receipt (which could otherwise trigger a new owner write).
func transferOwnerError(err error) error {
	if err == nil {
		return nil
	}
	for _, safe := range []error{port.ErrTransferConfiguration, port.ErrTransferAuthority, port.ErrTransferConflict, port.ErrTransferNotReady, port.ErrTransferDependency} {
		if errors.Is(err, safe) {
			return safe
		}
	}
	return port.ErrTransferDependency
}
func transferActor(ctx context.Context) uuid.UUID  { p, _ := authz.FromContext(ctx); return p.ActorRef }
func transferTenant(ctx context.Context) uuid.UUID { p, _ := authz.FromContext(ctx); return p.TenantID }
func (t *Transfers) now() time.Time                { return t.clock.Now().UTC() }
func transferClone[T any](v T) (T, error) {
	var out T
	raw, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}
func transferDigest(v any) (port.TransferDigest, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(raw)
	return port.TransferDigest(hex.EncodeToString(h[:])), nil
}
func transferVersions(adapters []port.TransferAdapter) []port.TransferAdapterVersion {
	out := make([]port.TransferAdapterVersion, 0, len(adapters))
	for _, a := range adapters {
		out = append(out, port.TransferAdapterVersion{Module: a.Module, Namespace: a.Namespace, Version: a.Version, DependsOn: append([]string{}, a.DependsOn...)})
	}
	return out
}
func (t *Transfers) adapters(versions []port.TransferAdapterVersion) ([]port.TransferAdapter, error) {
	names := make([]string, 0, len(versions))
	for _, v := range versions {
		names = append(names, v.Namespace)
	}
	adapters, err := t.registry.Adapters(names)
	if err != nil {
		return nil, err
	}
	// Compare canonical snapshots, not registration order; registry returns dependency order.
	want, err := transferDigest(transferSortedVersions(versions))
	if err != nil {
		return nil, err
	}
	actual, err := transferDigest(transferSortedVersions(transferVersions(adapters)))
	if err != nil {
		return nil, err
	}
	if want != actual {
		return nil, port.ErrTransferConflict
	}
	return adapters, nil
}
func transferSortedVersions(versions []port.TransferAdapterVersion) []port.TransferAdapterVersion {
	out := slices.Clone(versions)
	for i := range out {
		out[i].DependsOn = append([]string{}, out[i].DependsOn...)
		slices.Sort(out[i].DependsOn)
	}
	slices.SortFunc(out, func(a, b port.TransferAdapterVersion) int {
		if a.Namespace < b.Namespace {
			return -1
		}
		if a.Namespace > b.Namespace {
			return 1
		}
		return 0
	})
	return out
}
func transferScopeContext(p port.TransferParticipants, s port.TransferScope) error {
	if transferTenant(p.Source()) != s.SourceTenant || transferTenant(p.Target()) != s.TargetTenant {
		return port.ErrTransferAuthority
	}
	binding, err := port.TransferBindingDigest(s)
	if err != nil || binding != s.BindingHash {
		return port.ErrTransferConflict
	}
	for _, side := range []port.TransferSide{port.TransferSource, port.TransferTarget} {
		ctx, scopeRef := p.Source(), s.SourceScopeRef
		if side == port.TransferTarget {
			ctx, scopeRef = p.Target(), s.TargetScopeRef
		}
		node := db.ContextVarsFrom(ctx).NodeID
		if node != uuid.Nil && node != scopeRef {
			return port.ErrTransferAuthority
		}
	}
	return nil
}
func (t *Transfers) authority(p port.TransferParticipants, s port.TransferScope, adapters []port.TransferAdapter, oldSource, oldTarget map[string]port.TransferApproval, action string, recovery bool) (map[string]port.TransferApproval, map[string]port.TransferApproval, error) {
	if err := transferScopeContext(p, s); err != nil {
		return nil, nil, err
	}
	source, target := map[string]port.TransferApproval{}, map[string]port.TransferApproval{}
	for _, a := range adapters {
		authority := a.Authority
		if recovery {
			authority = a.RecoveryAuthority
		}
		for _, side := range []port.TransferSide{port.TransferSource, port.TransferTarget} {
			ctx, old, out := p.Source(), oldSource, source
			if side == port.TransferTarget {
				ctx, old, out = p.Target(), oldTarget, target
			}
			approval, err := authority(ctx, port.TransferAuthorityRequest{Scope: s, Namespace: a.Namespace, Side: side, Action: action})
			if err != nil || approval.Ref == uuid.Nil || approval.ActorRef != transferActor(ctx) || approval.Epoch == "" || len(approval.Epoch) > 128 || !approval.ValidUntil.After(t.now()) {
				return nil, nil, port.ErrTransferAuthority
			}
			if !recovery && old != nil {
				before, ok := old[a.Namespace]
				if !ok || before.Ref != approval.Ref || before.ActorRef != approval.ActorRef || before.Epoch != approval.Epoch {
					return nil, nil, port.ErrTransferAuthority
				}
			}
			out[a.Namespace] = approval
		}
	}
	return source, target, nil
}
func transferNewState(intent port.TransferIntent) *store.TransferState {
	return &store.TransferState{Intent: intent, SourceApprovals: map[string]port.TransferApproval{}, TargetApprovals: map[string]port.TransferApproval{}, SourceFences: map[string]port.TransferFence{}, Scans: map[string]store.TransferScanState{}}
}
func (t *Transfers) read(ctx context.Context, entity uuid.UUID) (*store.TransferState, error) {
	var s *store.TransferState
	err := t.local(ctx, func(tx pgx.Tx) error { var err error; s, err = store.TransferLatest(ctx, tx, entity); return err })
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, port.ErrTransferNotReady
	}
	return s, nil
}
func (t *Transfers) write(ctx context.Context, entity, scope uuid.UUID, kind string, s *store.TransferState) error {
	return t.local(ctx, func(tx pgx.Tx) error {
		return store.AppendTransferState(ctx, tx, transferTenant(ctx), scope, transferActor(ctx), entity, s.Sequence, kind, s, t.now())
	})
}
func (t *Transfers) pair(p port.TransferParticipants, ref port.TransferRef, action string, recovery bool) (*store.TransferState, *store.TransferState, []port.TransferAdapter, error) {
	if err := t.authenticate(p); err != nil {
		return nil, nil, nil, err
	}
	if ref.ID == uuid.Nil {
		return nil, nil, nil, port.ErrTransferConflict
	}
	source, err := t.read(p.Source(), ref.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	if source.Plan.ID != ref.ID || source.Plan.Scope.SourceTenant != transferTenant(p.Source()) {
		return nil, nil, nil, port.ErrTransferConflict
	}
	target, err := t.read(p.Target(), source.Plan.Scope.IntentID)
	if err != nil {
		return nil, nil, nil, err
	}
	ss, ts := source.Plan.Scope, target.Intent.Scope
	if ss.SourceTenant != ts.SourceTenant || ss.TargetTenant != ts.TargetTenant || ss.SourceScopeRef != ts.SourceScopeRef || ss.TargetScopeRef != ts.TargetScopeRef || ss.BindingHash != ts.BindingHash || ss.IntentID != target.Intent.ID {
		return nil, nil, nil, port.ErrTransferConflict
	}
	if target.Plan.ID != uuid.Nil && target.Plan.ID != source.Plan.ID {
		return nil, nil, nil, port.ErrTransferConflict
	}
	if !recovery && action != "status" && target.Plan.ID != uuid.Nil && !transferNormalState(target) {
		return nil, nil, nil, port.ErrTransferConflict
	}
	adapters, err := t.adapters(source.Plan.Adapters)
	if err != nil {
		return nil, nil, nil, err
	}
	targetVersions, err := transferDigest(transferSortedVersions(target.Intent.Adapters))
	if err != nil {
		return nil, nil, nil, err
	}
	sourceVersions, err := transferDigest(transferSortedVersions(source.Plan.Adapters))
	if err != nil || targetVersions != sourceVersions {
		return nil, nil, nil, port.ErrTransferConflict
	}
	if _, _, err = t.authority(p, ss, adapters, source.SourceApprovals, source.TargetApprovals, action, recovery); err != nil {
		return nil, nil, nil, err
	}
	return source, target, adapters, nil
}
func (t *Transfers) fences(p port.TransferParticipants, source, target *store.TransferState, adapters []port.TransferAdapter) error {
	for _, a := range adapters {
		sf, sok := source.SourceFences[a.Namespace]
		tf, tok := target.Intent.TargetFences[a.Namespace]
		if !sok || !tok {
			return port.ErrTransferNotReady
		}
		if err := a.CheckSourceFence(p.Source(), port.TransferFenceRequest{Scope: source.Plan.Scope, Namespace: a.Namespace, Side: port.TransferSource}, sf); err != nil {
			return port.ErrTransferConflict
		}
		if err := a.CheckTargetFence(p.Target(), port.TransferFenceRequest{Scope: target.Intent.Scope, Namespace: a.Namespace, Side: port.TransferTarget}, tf); err != nil {
			return port.ErrTransferConflict
		}
	}
	return nil
}
func validTransferFence(f port.TransferFence) bool {
	return f.Ref != uuid.Nil && f.Epoch != "" && len(f.Epoch) <= 128 && f.HighWatermark != "" && len(f.HighWatermark) <= 512 && port.ValidTransferDigest(f.HeaderRoot) && f.HeaderCount >= 0 && f.HeaderCount <= 100000
}

func (t *Transfers) TargetIntent(p port.TransferParticipants, input port.TransferIntentInput) (port.TransferIntent, error) {
	if err := t.authenticate(p); err != nil {
		return port.TransferIntent{}, err
	}
	if input.OperationID == uuid.Nil || input.SourceScopeRef == uuid.Nil || input.TargetScopeRef == uuid.Nil {
		return port.TransferIntent{}, port.ErrTransferConflict
	}
	adapters, err := t.registry.Adapters(input.Namespaces)
	if err != nil {
		return port.TransferIntent{}, err
	}
	scope := port.TransferScope{SourceTenant: transferTenant(p.Source()), TargetTenant: transferTenant(p.Target()), SourceScopeRef: input.SourceScopeRef, TargetScopeRef: input.TargetScopeRef}
	scope.BindingHash, err = port.TransferBindingDigest(scope)
	if err != nil {
		return port.TransferIntent{}, err
	}
	versions := transferVersions(adapters)
	hash, err := transferDigest(struct {
		Scope    port.TransferScope
		Adapters []port.TransferAdapterVersion
	}{scope, transferSortedVersions(versions)})
	if err != nil {
		return port.TransferIntent{}, err
	}
	sourceApproval, targetApproval, err := t.authority(p, scope, adapters, nil, nil, "intent", false)
	if err != nil {
		return port.TransferIntent{}, err
	}
	// Read a possible replay in its own local transaction, then recheck authority
	// outside that transaction. Owner callbacks may open their own module transaction.
	var replay *store.TransferState
	err = t.local(p.Target(), func(tx pgx.Tx) error {
		old, oldHash, err := store.TransferIntentByOperation(p.Target(), tx, input.OperationID)
		if err != nil {
			return err
		}
		if old == uuid.Nil {
			return nil
		}
		if oldHash != hash {
			return port.ErrTransferConflict
		}
		replay, err = store.TransferLatest(p.Target(), tx, old)
		if err == nil && replay == nil {
			return port.ErrTransferConflict
		}
		return err
	})
	if err != nil {
		return port.TransferIntent{}, err
	}
	if replay != nil {
		if _, _, err := t.authority(p, replay.Intent.Scope, adapters, replay.SourceApprovals, replay.TargetApprovals, "intent_replay", false); err != nil {
			return port.TransferIntent{}, err
		}
		return replay.Intent, nil
	}
	var out port.TransferIntent
	err = t.local(p.Target(), func(tx pgx.Tx) error {
		if err := store.LockTransfer(p.Target(), tx, scope.TargetTenant, input.OperationID); err != nil {
			return err
		}
		old, _, err := store.TransferIntentByOperation(p.Target(), tx, input.OperationID)
		if err != nil {
			return err
		}
		// A concurrent creator won after our read; retry rechecks its actual approvals.
		if old != uuid.Nil {
			return port.ErrTransferConflict
		}
		scope.IntentID = ids.New()
		out = port.TransferIntent{ID: scope.IntentID, Scope: scope, Adapters: versions, TargetFences: map[string]port.TransferFence{}}
		state := transferNewState(out)
		state.SourceApprovals, state.TargetApprovals = sourceApproval, targetApproval
		if err := store.InsertTransferIntent(p.Target(), tx, scope.TargetTenant, transferActor(p.Target()), input.OperationID, hash, out, t.now()); err != nil {
			return err
		}
		return store.AppendTransferState(p.Target(), tx, scope.TargetTenant, scope.TargetScopeRef, transferActor(p.Target()), out.ID, 0, "intent_created", state, t.now())
	})
	return out, err
}
func (t *Transfers) Propose(p port.TransferParticipants, ref port.TransferRef) (port.TransferPlan, error) {
	if err := t.authenticate(p); err != nil {
		return port.TransferPlan{}, err
	}
	target, err := t.read(p.Target(), ref.ID)
	if err != nil {
		return port.TransferPlan{}, err
	}
	if target.Intent.ID != ref.ID {
		return port.TransferPlan{}, port.ErrTransferConflict
	}
	if target.Plan.ID != uuid.Nil && !transferNormalState(target) {
		return port.TransferPlan{}, port.ErrTransferConflict
	}
	adapters, err := t.adapters(target.Intent.Adapters)
	if err != nil {
		return port.TransferPlan{}, err
	}
	if _, _, err = t.authority(p, target.Intent.Scope, adapters, target.SourceApprovals, target.TargetApprovals, "propose", false); err != nil {
		return port.TransferPlan{}, err
	}
	var out port.TransferPlan
	err = t.local(p.Source(), func(tx pgx.Tx) error {
		if err := store.LockTransfer(p.Source(), tx, transferTenant(p.Source()), ref.ID); err != nil {
			return err
		}
		old, err := store.TransferPlanByIntent(p.Source(), tx, ref.ID)
		if err != nil {
			return err
		}
		if old != uuid.Nil {
			state, err := store.TransferLatest(p.Source(), tx, old)
			if err != nil {
				return err
			}
			if state == nil {
				return port.ErrTransferConflict
			}
			out = state.Plan
			return nil
		}
		state, err := transferClone(*target)
		if err != nil {
			return err
		}
		scope := target.Intent.Scope
		scope.PlanID = ids.New()
		out = port.TransferPlan{ID: scope.PlanID, Scope: scope, Status: "proposed", Adapters: target.Intent.Adapters}
		state.Plan = out
		state.Sequence = 0
		if err := store.InsertTransferPlan(p.Source(), tx, scope.SourceTenant, transferActor(p.Source()), port.TransferSource, out, t.now()); err != nil {
			return err
		}
		return store.AppendTransferState(p.Source(), tx, scope.SourceTenant, scope.SourceScopeRef, transferActor(p.Source()), out.ID, 0, "proposed", &state, t.now())
	})
	return out, err
}
func (t *Transfers) FreezeTarget(p port.TransferParticipants, ref port.TransferRef) (port.TransferIntent, error) {
	if err := t.authenticate(p); err != nil {
		return port.TransferIntent{}, err
	}
	target, err := t.read(p.Target(), ref.ID)
	if err != nil {
		return port.TransferIntent{}, err
	}
	if target.Intent.ID != ref.ID {
		return port.TransferIntent{}, port.ErrTransferConflict
	}
	if target.Plan.ID != uuid.Nil && !transferNormalState(target) {
		return port.TransferIntent{}, port.ErrTransferConflict
	}
	adapters, err := t.adapters(target.Intent.Adapters)
	if err != nil {
		return port.TransferIntent{}, err
	}
	if _, _, err = t.authority(p, target.Intent.Scope, adapters, target.SourceApprovals, target.TargetApprovals, "freeze_target", false); err != nil {
		return port.TransferIntent{}, err
	}
	for _, a := range adapters {
		req := port.TransferFenceRequest{Scope: target.Intent.Scope, Namespace: a.Namespace, Side: port.TransferTarget}
		if f, ok := target.Intent.TargetFences[a.Namespace]; ok {
			if err := a.CheckTargetFence(p.Target(), req, f); err != nil {
				return port.TransferIntent{}, port.ErrTransferConflict
			}
			continue
		}
		f, err := a.FenceTarget(p.Target(), req)
		if err != nil {
			err = transferOwnerError(err)
			return port.TransferIntent{}, err
		}
		if !validTransferFence(f) {
			return port.TransferIntent{}, port.ErrTransferConflict
		}
		target.Intent.TargetFences[a.Namespace] = f
		if err := t.write(p.Target(), ref.ID, target.Intent.Scope.TargetScopeRef, "target_fenced", target); err != nil {
			return port.TransferIntent{}, err
		}
	}
	return target.Intent, nil
}
func (t *Transfers) FreezeSource(p port.TransferParticipants, ref port.TransferRef) (port.TransferPlan, error) {
	source, target, adapters, err := t.pair(p, ref, "freeze_source", false)
	if err != nil {
		return port.TransferPlan{}, err
	}
	if source.Plan.Status != "proposed" && source.Plan.Status != "frozen" {
		return port.TransferPlan{}, port.ErrTransferConflict
	}
	// Target fences may precede source proposal. Their exact immutable intent holder is retained.
	if len(target.Intent.TargetFences) != len(adapters) {
		return port.TransferPlan{}, port.ErrTransferNotReady
	}
	for _, a := range adapters {
		tf := target.Intent.TargetFences[a.Namespace]
		if err := a.CheckTargetFence(p.Target(), port.TransferFenceRequest{Scope: target.Intent.Scope, Namespace: a.Namespace, Side: port.TransferTarget}, tf); err != nil {
			return port.TransferPlan{}, port.ErrTransferConflict
		}
		req := port.TransferFenceRequest{Scope: source.Plan.Scope, Namespace: a.Namespace, Side: port.TransferSource}
		if f, ok := source.SourceFences[a.Namespace]; ok {
			if err := a.CheckSourceFence(p.Source(), req, f); err != nil {
				return port.TransferPlan{}, port.ErrTransferConflict
			}
			continue
		}
		f, err := a.FenceSource(p.Source(), req)
		if err != nil {
			err = transferOwnerError(err)
			return port.TransferPlan{}, err
		}
		if !validTransferFence(f) {
			return port.TransferPlan{}, port.ErrTransferConflict
		}
		source.SourceFences[a.Namespace] = f
		source.Plan.Status = "frozen"
		if err := t.write(p.Source(), ref.ID, source.Plan.Scope.SourceScopeRef, "source_fenced", source); err != nil {
			return port.TransferPlan{}, err
		}
	}
	return source.Plan, nil
}
func (t *Transfers) ScanNext(p port.TransferParticipants, ref port.TransferRef) (port.TransferPlan, error) {
	source, target, adapters, err := t.pair(p, ref, "scan", false)
	if err != nil {
		return port.TransferPlan{}, err
	}
	if source.Plan.Status != "frozen" && source.Plan.Status != "scanning" {
		return port.TransferPlan{}, port.ErrTransferConflict
	}
	if err := t.fences(p, source, target, adapters); err != nil {
		return port.TransferPlan{}, err
	}
	for _, a := range adapters {
		scan := source.Scans[a.Namespace]
		if scan.Complete {
			continue
		}
		page, err := a.Scan(p.Source(), port.TransferScanRequest{Scope: source.Plan.Scope, Namespace: a.Namespace, Fence: source.SourceFences[a.Namespace], Cursor: scan.Cursor, Limit: a.MaxBatch})
		if err != nil {
			return port.TransferPlan{}, transferOwnerError(err)
		}
		page, err = transferClone(page)
		if err != nil {
			return port.TransferPlan{}, err
		}
		if len(page.Items) > a.MaxBatch || len(page.Next) > 512 || (!page.Complete && (page.Next == "" || page.Next == scan.Cursor)) || (page.Complete && page.Next != "") {
			return port.TransferPlan{}, port.ErrTransferConflict
		}
		if _, err := port.TransferHeaderRoot(source.Plan.Scope.SourceTenant, source.Plan.Scope.SourceScopeRef, a.Namespace, page.Items); err != nil {
			return port.TransferPlan{}, err
		}
		if source.Plan.Count+len(page.Items) > 100000 {
			return port.TransferPlan{}, port.ErrTransferConflict
		}
		if err := t.fences(p, source, target, adapters); err != nil {
			return port.TransferPlan{}, err
		}
		source.Scans[a.Namespace] = store.TransferScanState{Cursor: page.Next, Complete: page.Complete}
		source.Plan.Count += len(page.Items)
		source.Plan.Status = "scanning"
		err = t.local(p.Source(), func(tx pgx.Tx) error {
			if err := store.LockTransfer(p.Source(), tx, source.Plan.Scope.SourceTenant, ref.ID); err != nil {
				return err
			}
			if err := store.InsertTransferHeaders(p.Source(), tx, source.Plan.Scope.SourceTenant, source.Plan.Scope.SourceScopeRef, ref.ID, page.Items); err != nil {
				return err
			}
			return store.AppendTransferState(p.Source(), tx, source.Plan.Scope.SourceTenant, source.Plan.Scope.SourceScopeRef, transferActor(p.Source()), ref.ID, source.Sequence, "page_recorded", source, t.now())
		})
		return source.Plan, err
	}
	return source.Plan, nil
}
func (t *Transfers) headers(ctx context.Context, plan uuid.UUID) ([]port.TransferHeader, error) {
	var out []port.TransferHeader
	err := t.local(ctx, func(tx pgx.Tx) error { var err error; out, err = store.TransferHeaders(ctx, tx, plan); return err })
	return out, err
}
func (t *Transfers) manifest(p port.TransferParticipants, source, target *store.TransferState, adapters []port.TransferAdapter) (port.TransferDigest, []port.TransferHeader, error) {
	if err := t.fences(p, source, target, adapters); err != nil {
		return "", nil, err
	}
	for _, a := range adapters {
		if !source.Scans[a.Namespace].Complete {
			return "", nil, port.ErrTransferNotReady
		}
	}
	headers, err := t.headers(p.Source(), source.Plan.ID)
	if err != nil {
		return "", nil, err
	}
	fences := []port.TransferFenceBinding{}
	for _, a := range adapters {
		fences = append(fences, port.TransferFenceBinding{Namespace: a.Namespace, Side: port.TransferSource, Fence: source.SourceFences[a.Namespace]}, port.TransferFenceBinding{Namespace: a.Namespace, Side: port.TransferTarget, Fence: target.Intent.TargetFences[a.Namespace]})
	}
	if len(headers) != source.Plan.Count {
		return "", nil, port.ErrTransferConflict
	}
	root, err := port.TransferManifestDigest(source.Plan.Scope, source.Plan.Adapters, fences, headers)
	return root, headers, err
}
func (t *Transfers) Seal(p port.TransferParticipants, ref port.TransferRef) (port.TransferPlan, error) {
	source, target, adapters, err := t.pair(p, ref, "seal", false)
	if err != nil {
		return port.TransferPlan{}, err
	}
	if source.Plan.Status != "scanning" && source.Plan.Status != "sealed" {
		return port.TransferPlan{}, port.ErrTransferConflict
	}
	root, _, err := t.manifest(p, source, target, adapters)
	if err != nil {
		return port.TransferPlan{}, err
	}
	if source.Plan.Status == "sealed" {
		if root != source.Plan.ManifestHash {
			return port.TransferPlan{}, port.ErrTransferConflict
		}
		return source.Plan, nil
	}
	source.Plan.ManifestHash, source.Plan.Status = root, "sealed"
	return source.Plan, t.write(p.Source(), ref.ID, source.Plan.Scope.SourceScopeRef, "sealed", source)
}
func (t *Transfers) Accept(p port.TransferParticipants, ref port.TransferRef) (port.TransferPlan, error) {
	source, target, adapters, err := t.pair(p, ref, "accept", false)
	if err != nil {
		return port.TransferPlan{}, err
	}
	if source.Plan.Status != "sealed" {
		return port.TransferPlan{}, port.ErrTransferNotReady
	}
	root, headers, err := t.manifest(p, source, target, adapters)
	if err != nil {
		return port.TransferPlan{}, err
	}
	if root != source.Plan.ManifestHash {
		return port.TransferPlan{}, port.ErrTransferConflict
	}
	if target.Plan.ID != uuid.Nil {
		if target.Plan.ManifestHash != root {
			return port.TransferPlan{}, port.ErrTransferConflict
		}
		return target.Plan, nil
	}
	target.Plan = source.Plan
	target.Plan.Status = "accepted"
	err = t.local(p.Target(), func(tx pgx.Tx) error {
		if err := store.LockTransfer(p.Target(), tx, transferTenant(p.Target()), target.Intent.ID); err != nil {
			return err
		}
		if err := store.InsertTransferPlan(p.Target(), tx, transferTenant(p.Target()), transferActor(p.Target()), port.TransferTarget, target.Plan, t.now()); err != nil {
			return err
		}
		if err := store.InsertTransferBinding(p.Target(), tx, transferActor(p.Target()), target.Plan, t.now()); err != nil {
			return err
		}
		if err := store.InsertTransferHeaders(p.Target(), tx, transferTenant(p.Target()), target.Plan.Scope.TargetScopeRef, target.Plan.ID, headers); err != nil {
			return err
		}
		return store.AppendTransferState(p.Target(), tx, transferTenant(p.Target()), target.Plan.Scope.TargetScopeRef, transferActor(p.Target()), target.Intent.ID, target.Sequence, "accepted", target, t.now())
	})
	return target.Plan, err
}
func (t *Transfers) Status(p port.TransferParticipants, ref port.TransferRef) (port.TransferPlan, error) {
	source, target, _, err := t.pair(p, ref, "status", false)
	if err != nil {
		return port.TransferPlan{}, err
	}
	if target.Plan.ID != uuid.Nil {
		return target.Plan, nil
	}
	return source.Plan, nil
}
func (t *Transfers) DryRun(p port.TransferParticipants, ref port.TransferRef) (port.TransferReport, error) {
	source, _, adapters, err := t.pair(p, ref, "dry_run", false)
	if err != nil {
		return port.TransferReport{}, err
	}
	if source.Plan.Status != "proposed" {
		return port.TransferReport{}, port.ErrTransferConflict
	}
	all := []port.TransferHeader{}
	for _, a := range adapters {
		cursor := ""
		seen := map[string]bool{}
		for pageNumber := 0; ; pageNumber++ {
			if pageNumber >= 100001 || p.Source().Err() != nil || p.Target().Err() != nil {
				return port.TransferReport{}, port.ErrTransferNotReady
			}
			page, err := a.Scan(p.Source(), port.TransferScanRequest{Scope: source.Plan.Scope, Namespace: a.Namespace, Cursor: cursor, Limit: a.MaxBatch, DryRun: true})
			if err != nil {
				return port.TransferReport{}, transferOwnerError(err)
			}
			page, err = transferClone(page)
			if err != nil {
				return port.TransferReport{}, err
			}
			if (!page.Complete && len(page.Items) == 0) || len(page.Items) > a.MaxBatch || len(all)+len(page.Items) > 100000 || len(page.Next) > 512 {
				return port.TransferReport{}, port.ErrTransferConflict
			}
			if _, err := port.TransferHeaderRoot(source.Plan.Scope.SourceTenant, source.Plan.Scope.SourceScopeRef, a.Namespace, page.Items); err != nil {
				return port.TransferReport{}, err
			}
			all = append(all, page.Items...)
			if page.Complete {
				if page.Next != "" {
					return port.TransferReport{}, port.ErrTransferConflict
				}
				break
			}
			if page.Next == "" || seen[page.Next] || page.Next == cursor {
				return port.TransferReport{}, port.ErrTransferConflict
			}
			seen[page.Next] = true
			cursor = page.Next
		}
	}
	root, err := transferDigest(all)
	return port.TransferReport{PlanID: ref.ID, Count: len(all), Root: root, Provisional: true}, err
}

// Release/abort stay unavailable until all reserved batches are probed under recovery rights.
func (t *Transfers) Abort(port.TransferParticipants, port.TransferRef, uuid.UUID) (port.TransferPlan, error) {
	return port.TransferPlan{}, port.ErrTransferNotReady
}
func (t *Transfers) ReleaseFences(port.TransferParticipants, port.TransferRef, uuid.UUID) (port.TransferPlan, error) {
	return port.TransferPlan{}, port.ErrTransferNotReady
}
