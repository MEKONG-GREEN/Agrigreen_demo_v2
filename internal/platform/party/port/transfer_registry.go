package port

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/google/uuid"
)

type TransferCutoverProof struct {
	IdentityReceipt, RehearsalReceipt      uuid.UUID
	BindingHash, ManifestHash, LogicalRoot TransferDigest
	ValidUntil                             time.Time
}
type TransferCutoverCheck func(TransferParticipants, TransferScope, TransferReport) (TransferCutoverProof, error)

type TransferRecoveryProof struct {
	PlanID, OperationID, ReasonRef              uuid.UUID
	IdentityRollbackReceipt, RehearsalReceipt   uuid.UUID
	BindingHash, ManifestHash, CompensationRoot TransferDigest
	ValidUntil                                  time.Time
}
type TransferRecoveryCheck func(TransferParticipants, TransferRecoveryRequest, TransferReport) (TransferRecoveryProof, error)

type TransferRegistry struct {
	verifier TransferTokenVerifier
	adapters []TransferAdapter
	cutover  TransferCutoverCheck
	recovery TransferRecoveryCheck
}

// NewTransferRegistry freezes owner registrations. Missing owner operations never
// become successful no-ops. Nil cutover keeps PrepareCutover unavailable until owners supply proofs.
func NewTransferRegistry(adapters []TransferAdapter, verifier TransferTokenVerifier, cutover TransferCutoverCheck, recovery ...TransferRecoveryCheck) (*TransferRegistry, error) {
	if len(recovery) > 1 || transferVerifierMissing(verifier) || len(adapters) == 0 || len(adapters) > 64 {
		return nil, ErrTransferConfiguration
	}
	r := &TransferRegistry{cutover: cutover, verifier: verifier}
	if len(recovery) == 1 {
		r.recovery = recovery[0]
	}
	seen := map[string]bool{}
	for _, a := range adapters {
		if _, ok := modreg.Entry(a.Module); !ok {
			return nil, ErrTransferConfiguration
		}
		if !validTransferName(a.Namespace) || a.Version == "" || len(a.Version) > 128 || seen[a.Namespace] || a.MaxBatch < 1 || a.MaxBatch > 500 {
			return nil, ErrTransferConfiguration
		}
		if a.EnterRecoveryGate == nil || a.ReadRecoveryGate == nil || a.ReadAppliedForRecovery == nil || a.RecoveryAuthority == nil || a.ReleaseSourceFence == nil || a.ReadSourceFenceRelease == nil || a.ReleaseTargetFence == nil || a.ReadTargetFenceRelease == nil || a.Authority == nil || a.FenceSource == nil || a.FenceTarget == nil || a.CheckSourceFence == nil || a.CheckTargetFence == nil || a.Scan == nil || a.Export == nil || a.ReadExport == nil || a.Apply == nil || a.ReadReceipt == nil || a.Verify == nil || a.Compensate == nil || a.ReadCompensation == nil || a.VerifyCompensation == nil {
			return nil, ErrTransferConfiguration
		}
		a.DependsOn = slices.Clone(a.DependsOn)
		slices.Sort(a.DependsOn)
		for i, dep := range a.DependsOn {
			if dep == a.Namespace || !validTransferName(dep) || (i > 0 && dep == a.DependsOn[i-1]) {
				return nil, ErrTransferConfiguration
			}
		}
		r.adapters = append(r.adapters, a)
		seen[a.Namespace] = true
	}
	slices.SortFunc(r.adapters, func(a, b TransferAdapter) int { return strings.Compare(a.Namespace, b.Namespace) })
	names := make([]string, 0, len(r.adapters))
	for _, a := range r.adapters {
		names = append(names, a.Namespace)
	}
	if _, err := r.Adapters(names); err != nil {
		return nil, err
	}
	return r, nil
}

// Adapters returns a stable dependency order and fresh slices. Callers must name
// dependencies explicitly; selecting one collection cannot silently add another dataset.
func (r *TransferRegistry) Adapters(names []string) ([]TransferAdapter, error) {
	if r == nil || len(names) == 0 || len(names) > 64 {
		return nil, ErrTransferConfiguration
	}
	byName := map[string]TransferAdapter{}
	for _, a := range r.adapters {
		byName[a.Namespace] = a
	}
	selected := map[string]bool{}
	for _, name := range names {
		if selected[name] {
			return nil, ErrTransferConfiguration
		}
		if _, ok := byName[name]; !ok {
			return nil, ErrTransferConfiguration
		}
		selected[name] = true
	}
	state := map[string]int{}
	out := []TransferAdapter{}
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return ErrTransferConfiguration
		}
		if state[name] == 2 {
			return nil
		}
		a, exists := byName[name]
		if !exists || !selected[name] {
			return ErrTransferConfiguration
		}
		state[name] = 1
		for _, dep := range a.DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[name] = 2
		a.DependsOn = slices.Clone(a.DependsOn)
		out = append(out, a)
		return nil
	}
	ordered := slices.Clone(names)
	slices.Sort(ordered)
	for _, name := range ordered {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (r *TransferRegistry) Cutover() TransferCutoverCheck {
	if r == nil {
		return nil
	}
	return r.cutover
}
func validTransferName(s string) bool {
	if len(s) == 0 || len(s) > 96 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '.' && c != '/' && c != ':' && c != '-' {
			return false
		}
	}
	return true
}
func ValidTransferDigest(d TransferDigest) bool {
	if len(d) != 64 {
		return false
	}
	for _, c := range d {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
func transferHash(v any) (TransferDigest, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(raw)
	return TransferDigest(hex.EncodeToString(h[:])), nil
}

// TransferBindingDigest binds only the pair and business scopes. Later intent/plan
// identifiers and hashes cannot recursively change this stable binding.
func TransferBindingDigest(scope TransferScope) (TransferDigest, error) {
	if scope.SourceTenant == uuid.Nil || scope.TargetTenant == uuid.Nil || scope.SourceTenant == scope.TargetTenant || scope.SourceScopeRef == uuid.Nil || scope.TargetScopeRef == uuid.Nil {
		return "", ErrTransferConflict
	}
	return transferHash(struct {
		Version                                                    string
		SourceTenant, SourceScopeRef, TargetTenant, TargetScopeRef uuid.UUID
	}{"party.transfer.binding/v1", scope.SourceTenant, scope.SourceScopeRef, scope.TargetTenant, scope.TargetScopeRef})
}

type TransferFenceBinding struct {
	Namespace string        `json:"namespace"`
	Side      TransferSide  `json:"side"`
	Fence     TransferFence `json:"fence"`
}

func cloneTransferHeader(h TransferHeader) TransferHeader {
	h.References = append([]TransferReference{}, h.References...)
	slices.SortFunc(h.References, func(a, b TransferReference) int {
		if c := compareTransferKey(a.Key, b.Key); c != 0 {
			return c
		}
		if a.External != b.External {
			if !a.External {
				return -1
			}
			return 1
		}
		if c := strings.Compare(a.ExternalTenant.String(), b.ExternalTenant.String()); c != 0 {
			return c
		}
		return strings.Compare(a.BasisRef.String(), b.BasisRef.String())
	})
	return h
}
func compareTransferKey(a, b TransferKey) int {
	if c := strings.Compare(a.Namespace, b.Namespace); c != 0 {
		return c
	}
	return strings.Compare(a.Ref.String(), b.Ref.String())
}

// TransferHeaderRoot commits the complete frozen inventory of one source scope.
// It is independent of the manifest and normalizes empty collections. Owner fences
// must compute this same versioned root only after their durable write fence holds.
func TransferHeaderRoot(tenantID, scopeRef uuid.UUID, namespace string, headers []TransferHeader) (TransferDigest, error) {
	if tenantID == uuid.Nil || scopeRef == uuid.Nil || !validTransferName(namespace) || len(headers) > 100000 {
		return "", ErrTransferConflict
	}
	items := make([]TransferHeader, 0, len(headers))
	seen := map[TransferKey]bool{}
	for _, h := range headers {
		if h.Key.Namespace != namespace || h.Key.Ref == uuid.Nil || h.SourceTenant != tenantID || h.SourceScopeRef != scopeRef || !ValidTransferDigest(h.SourceRowHash) || !ValidTransferDigest(h.LogicalHash) || h.SourceSeq < 0 || seen[h.Key] || len(h.References) > 128 {
			return "", ErrTransferConflict
		}
		seen[h.Key] = true
		h = cloneTransferHeader(h)
		for i, ref := range h.References {
			if !validTransferName(ref.Key.Namespace) || ref.Key.Ref == uuid.Nil || (i > 0 && ref.Key == h.References[i-1].Key) {
				return "", ErrTransferConflict
			}
			if ref.External {
				if ref.ExternalTenant == uuid.Nil || ref.BasisRef == uuid.Nil {
					return "", ErrTransferConflict
				}
			} else if ref.ExternalTenant != uuid.Nil || ref.BasisRef != uuid.Nil {
				return "", ErrTransferConflict
			}
		}
		items = append(items, h)
	}
	slices.SortFunc(items, func(a, b TransferHeader) int { return compareTransferKey(a.Key, b.Key) })
	return transferHash(struct {
		Version            string
		TenantID, ScopeRef uuid.UUID
		Namespace          string
		Items              []TransferHeader
	}{"party.transfer.headers/v1", tenantID, scopeRef, namespace, items})
}

// TransferManifestDigest validates provenance, declared dependency edges and the
// complete frozen source inventory before hashing a canonical typed representation.
// Cyclic aggregates are unavailable in v1 until an owner supplies an atomic-group contract.
func TransferManifestDigest(scope TransferScope, adapters []TransferAdapterVersion, fences []TransferFenceBinding, headers []TransferHeader) (TransferDigest, error) {
	binding, err := TransferBindingDigest(scope)
	if err != nil || binding != scope.BindingHash || scope.IntentID == uuid.Nil || scope.PlanID == uuid.Nil || len(adapters) == 0 || len(adapters) > 64 || len(headers) > 100000 {
		return "", ErrTransferConflict
	}
	versions := slices.Clone(adapters)
	slices.SortFunc(versions, func(a, b TransferAdapterVersion) int { return strings.Compare(a.Namespace, b.Namespace) })
	namespaces := map[string]bool{}
	dependencies := map[string]map[string]bool{}
	for i, a := range versions {
		if _, ok := modreg.Entry(a.Module); !ok || !validTransferName(a.Namespace) || a.Version == "" || len(a.Version) > 128 || namespaces[a.Namespace] {
			return "", ErrTransferConflict
		}
		a.DependsOn = append([]string{}, a.DependsOn...)
		slices.Sort(a.DependsOn)
		dependencies[a.Namespace] = map[string]bool{}
		for _, dep := range a.DependsOn {
			if !validTransferName(dep) || dep == a.Namespace || dependencies[a.Namespace][dep] {
				return "", ErrTransferConflict
			}
			dependencies[a.Namespace][dep] = true
		}
		versions[i] = a
		namespaces[a.Namespace] = true
	}
	nsState := map[string]int{}
	var visitNamespace func(string) error
	visitNamespace = func(name string) error {
		if !namespaces[name] || nsState[name] == 1 {
			return ErrTransferConflict
		}
		if nsState[name] == 2 {
			return nil
		}
		nsState[name] = 1
		for dep := range dependencies[name] {
			if err := visitNamespace(dep); err != nil {
				return err
			}
		}
		nsState[name] = 2
		return nil
	}
	for name := range namespaces {
		if err := visitNamespace(name); err != nil {
			return "", err
		}
	}
	fs := slices.Clone(fences)
	slices.SortFunc(fs, func(a, b TransferFenceBinding) int {
		if c := strings.Compare(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		return strings.Compare(string(a.Side), string(b.Side))
	})
	seenFences := map[string]bool{}
	for _, f := range fs {
		key := f.Namespace + ":" + string(f.Side)
		if !namespaces[f.Namespace] || (f.Side != TransferSource && f.Side != TransferTarget) || seenFences[key] || f.Fence.Ref == uuid.Nil || f.Fence.Epoch == "" || len(f.Fence.Epoch) > 128 || f.Fence.HighWatermark == "" || len(f.Fence.HighWatermark) > 512 || !ValidTransferDigest(f.Fence.HeaderRoot) || f.Fence.HeaderCount < 0 || f.Fence.HeaderCount > 100000 {
			return "", ErrTransferConflict
		}
		seenFences[key] = true
	}
	if len(seenFences) != 2*len(versions) {
		return "", ErrTransferNotReady
	}
	items := make([]TransferHeader, 0, len(headers))
	groups := map[string][]TransferHeader{}
	seen := map[TransferKey]TransferHeader{}
	for _, h := range headers {
		if !namespaces[h.Key.Namespace] {
			return "", ErrTransferConflict
		}
		if _, duplicate := seen[h.Key]; duplicate {
			return "", ErrTransferConflict
		}
		h = cloneTransferHeader(h)
		seen[h.Key] = h
		groups[h.Key.Namespace] = append(groups[h.Key.Namespace], h)
		items = append(items, h)
	}
	// Validate headers before graph traversal, then prove every internal key exists.
	for namespace := range namespaces {
		if _, err := TransferHeaderRoot(scope.SourceTenant, scope.SourceScopeRef, namespace, groups[namespace]); err != nil {
			return "", err
		}
	}
	for _, h := range items {
		for _, ref := range h.References {
			if ref.External {
				continue
			}
			if _, ok := seen[ref.Key]; !ok {
				return "", ErrTransferNotReady
			}
			if ref.Key.Namespace != h.Key.Namespace && !dependencies[h.Key.Namespace][ref.Key.Namespace] {
				return "", ErrTransferConflict
			}
		}
	}
	keyState := map[TransferKey]int{}
	var visitKey func(TransferKey) error
	visitKey = func(key TransferKey) error {
		if keyState[key] == 1 {
			return ErrTransferConflict
		}
		if keyState[key] == 2 {
			return nil
		}
		keyState[key] = 1
		for _, ref := range seen[key].References {
			if !ref.External {
				if err := visitKey(ref.Key); err != nil {
					return err
				}
			}
		}
		keyState[key] = 2
		return nil
	}
	for key := range seen {
		if err := visitKey(key); err != nil {
			return "", err
		}
	}
	for _, f := range fs {
		if f.Side != TransferSource {
			continue
		}
		root, err := TransferHeaderRoot(scope.SourceTenant, scope.SourceScopeRef, f.Namespace, groups[f.Namespace])
		if err != nil {
			return "", err
		}
		if root != f.Fence.HeaderRoot || int64(len(groups[f.Namespace])) != f.Fence.HeaderCount {
			return "", ErrTransferConflict
		}
	}
	slices.SortFunc(items, func(a, b TransferHeader) int { return compareTransferKey(a.Key, b.Key) })
	return transferHash(struct {
		Version  string
		Scope    TransferScope
		Adapters []TransferAdapterVersion
		Fences   []TransferFenceBinding
		Items    []TransferHeader
	}{"party.transfer.manifest/v1", scope, versions, fs, items})
}

func TransferMapDigest(mappings []TransferMapping) (TransferDigest, error) {
	out := append([]TransferMapping{}, mappings...)
	slices.SortFunc(out, func(a, b TransferMapping) int { return compareTransferKey(a.Key, b.Key) })
	seenTargets := map[uuid.UUID]bool{}
	for i, m := range out {
		if !validTransferName(m.Key.Namespace) || m.Key.Ref == uuid.Nil || m.TargetRef == uuid.Nil || m.TargetRef.Version() != 7 || seenTargets[m.TargetRef] || (i > 0 && m.Key == out[i-1].Key) {
			return "", ErrTransferConflict
		}
		seenTargets[m.TargetRef] = true
	}
	return transferHash(struct {
		Version string
		Items   []TransferMapping
	}{"party.transfer.idmap/v1", out})
}

// TransferBatchMapDigest binds primary and dependency maps separately. Dependencies
// are verified predecessor keys; they cannot also be primary items of this batch.
func TransferBatchMapDigest(mappings, dependencies []TransferMapping) (TransferDigest, error) {
	primary, err := TransferMapDigest(mappings)
	if err != nil {
		return "", err
	}
	deps, err := TransferMapDigest(dependencies)
	if err != nil {
		return "", err
	}
	all := append(slices.Clone(mappings), dependencies...)
	if _, err := TransferMapDigest(all); err != nil {
		return "", err
	}
	return transferHash(struct {
		Version               string
		Primary, Dependencies TransferDigest
	}{"party.transfer.batch-map/v1", primary, deps})
}

// Authenticate rechecks signatures/expiry and native claims for both actual
// participants on EVERY operation, before owner authority callbacks or replay.
func (r *TransferRegistry) Authenticate(p TransferParticipants) error {
	if r == nil || transferVerifierMissing(r.verifier) || p.Validate() != nil {
		return ErrTransferAuthority
	}
	for _, ctx := range []context.Context{p.Source(), p.Target()} {
		old, ok := authz.FromContext(ctx)
		raw, hasRaw := authz.RawToken(ctx)
		if !ok || !hasRaw {
			return ErrTransferAuthority
		}
		fresh, err := r.verifier.Verify(ctx, raw)
		if err != nil || fresh.ActorRef != old.ActorRef || fresh.TenantID != old.TenantID || fresh.OrgRef != old.OrgRef || fresh.MFA != old.MFA || !fresh.MFA || !fresh.Has(authz.RolePlatformAdmin) || !sameTransferRoles(old.Roles, fresh.Roles) {
			return ErrTransferAuthority
		}
	}
	return nil
}
func sameTransferRoles(a, b []string) bool {
	ac, bc := slices.Clone(a), slices.Clone(b)
	slices.Sort(ac)
	slices.Sort(bc)
	return slices.Equal(slices.Compact(ac), slices.Compact(bc))
}

func transferVerifierMissing(v TransferTokenVerifier) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// TransferLogicalRoot compares business commitments independently of target IDs,
// tenant envelopes and ledger metadata. Owners canonicalize each LogicalHash.
func TransferLogicalRoot(headers []TransferHeader) (TransferDigest, error) {
	if len(headers) > 100000 {
		return "", ErrTransferConflict
	}
	type item struct {
		Key         TransferKey    `json:"key"`
		LogicalHash TransferDigest `json:"logical_hash"`
	}
	out := make([]item, 0, len(headers))
	for _, h := range headers {
		if !validTransferName(h.Key.Namespace) || h.Key.Ref == uuid.Nil || !ValidTransferDigest(h.LogicalHash) {
			return "", ErrTransferConflict
		}
		out = append(out, item{h.Key, h.LogicalHash})
	}
	slices.SortFunc(out, func(a, b item) int { return compareTransferKey(a.Key, b.Key) })
	for i := 1; i < len(out); i++ {
		if out[i].Key == out[i-1].Key {
			return "", ErrTransferConflict
		}
	}
	return transferHash(struct {
		Version string
		Items   []item
	}{"party.transfer.logical/v1", out})
}

func (r *TransferRegistry) Recovery() TransferRecoveryCheck {
	if r == nil {
		return nil
	}
	return r.recovery
}
