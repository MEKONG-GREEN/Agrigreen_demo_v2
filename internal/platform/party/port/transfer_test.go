package port

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
)

func transferTestAdapter(name string) TransferAdapter {
	a := TransferAdapter{Module: "pf/party", Namespace: name, Version: "v1", MaxBatch: 2}
	a.Authority = func(context.Context, TransferAuthorityRequest) (TransferApproval, error) {
		return TransferApproval{}, ErrTransferNotReady
	}
	a.RecoveryAuthority = a.Authority
	a.EnterRecoveryGate = func(context.Context, TransferRecoveryRequest, TransferFenceRequest, TransferFence) (TransferRecoveryGate, error) {
		return TransferRecoveryGate{}, ErrTransferNotReady
	}
	a.ReadRecoveryGate = TransferRecoveryGateReader(a.EnterRecoveryGate)
	a.ReadAppliedForRecovery = func(context.Context, TransferRecoveryRequest, TransferApplyRequest) (TransferReceipt, error) {
		return TransferReceipt{}, ErrTransferNotReady
	}
	a.FenceSource = func(context.Context, TransferFenceRequest) (TransferFence, error) {
		return TransferFence{}, ErrTransferNotReady
	}
	a.FenceTarget = a.FenceSource
	a.CheckSourceFence = func(context.Context, TransferFenceRequest, TransferFence) error { return ErrTransferNotReady }
	a.CheckTargetFence = a.CheckSourceFence
	a.Scan = func(context.Context, TransferScanRequest) (TransferPage, error) {
		return TransferPage{}, ErrTransferNotReady
	}
	a.Export = func(context.Context, TransferBatchRequest) (TransferArtifact, error) {
		return TransferArtifact{}, ErrTransferNotReady
	}
	a.ReadExport = TransferExportReader(a.Export)
	a.Apply = func(context.Context, TransferApplyRequest) (TransferReceipt, error) {
		return TransferReceipt{}, ErrTransferNotReady
	}
	a.ReadReceipt = TransferReceiptReader(a.Apply)
	a.Verify = func(context.Context, TransferApplyRequest, TransferReceipt) error { return ErrTransferNotReady }
	a.Compensate = func(context.Context, TransferCompensationRequest) (TransferReceipt, error) {
		return TransferReceipt{}, ErrTransferNotReady
	}
	a.ReadCompensation = TransferCompensationReader(a.Compensate)
	a.VerifyCompensation = func(context.Context, TransferCompensationRequest, TransferReceipt) error { return ErrTransferNotReady }
	a.ReleaseSourceFence = func(context.Context, TransferRecoveryRequest, TransferFenceRequest, TransferFence) (TransferFenceRelease, error) {
		return TransferFenceRelease{}, ErrTransferNotReady
	}
	a.ReleaseTargetFence = a.ReleaseSourceFence
	a.ReadSourceFenceRelease = TransferFenceReleaseReader(a.ReleaseSourceFence)
	a.ReadTargetFenceRelease = TransferFenceReleaseReader(a.ReleaseSourceFence)
	return a
}
func transferTestContext(t *testing.T, iss *authztest.FakeIssuer, token authztest.Token) context.Context {
	t.Helper()
	raw := iss.Sign(t, token)
	p, err := iss.Verifier().Verify(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return authz.WithRawToken(authz.WithPrincipal(tenant.WithID(context.Background(), p.TenantID), p), raw)
}
func transferTestToken() authztest.Token {
	return authztest.Token{Subject: uuid.New(), TenantID: uuid.New(), OrgRef: uuid.New(), Roles: []string{authz.RolePlatformAdmin}, MFA: true}
}
func TestTransferRegistryRequiresRealCredentialsAndFreshNativeClaims(t *testing.T) {
	iss := authztest.New(t)
	a := transferTestAdapter("objects")
	r, err := NewTransferRegistry([]TransferAdapter{a}, iss.Verifier(), nil)
	if err != nil {
		t.Fatal(err)
	}
	source, target := transferTestToken(), transferTestToken()
	sc, tc := transferTestContext(t, iss, source), transferTestContext(t, iss, target)
	p, err := NewTransferParticipants(sc, tc)
	if err != nil || r.Authenticate(p) != nil {
		t.Fatal("real contexts denied", err)
	}
	if _, err := NewTransferParticipants(sc, sc); !errors.Is(err, ErrTransferAuthority) {
		t.Fatal("same tenant accepted", err)
	}
	original, _ := authz.FromContext(sc)
	noRaw := authz.WithPrincipal(tenant.WithID(context.Background(), original.TenantID), original)
	if _, err := NewTransferParticipants(noRaw, tc); !errors.Is(err, ErrTransferAuthority) {
		t.Fatal("context-only principal accepted", err)
	}
	for _, field := range []string{"org", "roles", "mfa", "actor", "tenant"} {
		t.Run(field, func(t *testing.T) {
			forged := original
			switch field {
			case "org":
				forged.OrgRef = uuid.New()
			case "roles":
				forged.Roles = append(slices.Clone(forged.Roles), "injected_admin")
			case "mfa":
				forged.MFA = false
			case "actor":
				forged.ActorRef = uuid.New()
			case "tenant":
				forged.TenantID = uuid.New()
			}
			pp := TransferParticipants{source: authz.WithPrincipal(sc, forged), target: tc}
			if !errors.Is(r.Authenticate(pp), ErrTransferAuthority) {
				t.Fatal("native claim mismatch accepted")
			}
		})
	}
	expired := source
	expired.TTL = -time.Minute
	expiredRaw := iss.Sign(t, expired)
	p.source = authz.WithRawToken(sc, expiredRaw)
	if err := r.Authenticate(p); !errors.Is(err, ErrTransferAuthority) {
		t.Fatal("expired token replay accepted", err)
	}
	p.source = sc
	p.target = authz.WithRawToken(tc, "not-a-token")
	if err := r.Authenticate(p); err != ErrTransferAuthority || strings.Contains(err.Error(), "not-a-token") {
		t.Fatal("verifier detail leaked", err)
	}
	cancelCtx, cancel := context.WithCancel(sc)
	cancel()
	if _, err := NewTransferParticipants(cancelCtx, tc); !errors.Is(err, ErrTransferAuthority) {
		t.Fatal("cancelled actor accepted", err)
	}
	var nilVerifier *authz.Verifier
	if _, err := NewTransferRegistry([]TransferAdapter{a}, nilVerifier, nil); !errors.Is(err, ErrTransferConfiguration) {
		t.Fatal("typed nil verifier accepted", err)
	}
}
func TestTransferRegistryCopiesDependenciesAndRejectsIncompleteOwners(t *testing.T) {
	iss := authztest.New(t)
	base, child := transferTestAdapter("base"), transferTestAdapter("child")
	child.DependsOn = []string{"base"}
	registrations := []TransferAdapter{child, base}
	r, err := NewTransferRegistry(registrations, iss.Verifier(), nil)
	if err != nil {
		t.Fatal(err)
	}
	registrations[0].Namespace = "mutated"
	child.DependsOn[0] = "mutated"
	got, err := r.Adapters([]string{"child", "base"})
	if err != nil || len(got) != 2 || got[0].Namespace != "base" || got[1].DependsOn[0] != "base" {
		t.Fatal("registration aliased or not ordered", got, err)
	}
	got[1].DependsOn[0] = "mutated"
	again, err := r.Adapters([]string{"child", "base"})
	if err != nil || again[1].DependsOn[0] != "base" {
		t.Fatal("returned slice aliased", err)
	}
	for _, names := range [][]string{nil, {"unknown"}, {"child"}, {"base", "base"}} {
		if _, err := r.Adapters(names); !errors.Is(err, ErrTransferConfiguration) {
			t.Fatal("invalid selection accepted", names, err)
		}
	}
	for _, change := range []func(*TransferAdapter){
		func(a *TransferAdapter) { a.Authority = nil }, func(a *TransferAdapter) { a.RecoveryAuthority = nil }, func(a *TransferAdapter) { a.ReadReceipt = nil }, func(a *TransferAdapter) { a.CheckSourceFence = nil }, func(a *TransferAdapter) { a.ReadExport = nil }, func(a *TransferAdapter) { a.ReleaseTargetFence = nil }, func(a *TransferAdapter) { a.Module = "pf/unknown" }, func(a *TransferAdapter) { a.MaxBatch = 501 },
	} {
		a := transferTestAdapter("one")
		change(&a)
		if _, err := NewTransferRegistry([]TransferAdapter{a}, iss.Verifier(), nil); !errors.Is(err, ErrTransferConfiguration) {
			t.Fatal("incomplete owner accepted", err)
		}
	}
	x, y := transferTestAdapter("x"), transferTestAdapter("y")
	x.DependsOn, y.DependsOn = []string{"y"}, []string{"x"}
	if _, err := NewTransferRegistry([]TransferAdapter{x, y}, iss.Verifier(), nil); !errors.Is(err, ErrTransferConfiguration) {
		t.Fatal("cycle accepted", err)
	}
	if r.Cutover() != nil {
		t.Fatal("missing identity/rehearsal invented readiness")
	}
}
func TestTransferManifestBindsScopeReferencesFencesAndCanonicalOrder(t *testing.T) {
	digest := TransferDigest(strings.Repeat("a", 64))
	scope := TransferScope{SourceTenant: uuid.New(), SourceScopeRef: uuid.New(), TargetTenant: uuid.New(), TargetScopeRef: uuid.New(), IntentID: ids.New(), PlanID: ids.New()}
	scope.BindingHash, _ = TransferBindingDigest(scope)
	versions := []TransferAdapterVersion{{Module: "pf/party", Namespace: "objects", Version: "v1"}}
	fences := []TransferFenceBinding{
		{Namespace: "objects", Side: TransferSource, Fence: TransferFence{Ref: ids.New(), Epoch: "source-v1", HighWatermark: "42", HeaderRoot: digest}},
		{Namespace: "objects", Side: TransferTarget, Fence: TransferFence{Ref: ids.New(), Epoch: "target-v1", HighWatermark: "0", HeaderRoot: digest}},
	}
	first, second := TransferKey{Namespace: "objects", Ref: uuid.New()}, TransferKey{Namespace: "objects", Ref: uuid.New()}
	headers := []TransferHeader{
		{Key: first, SourceTenant: scope.SourceTenant, SourceScopeRef: scope.SourceScopeRef, SourceRowHash: digest, LogicalHash: digest},
		{Key: second, SourceTenant: scope.SourceTenant, SourceScopeRef: scope.SourceScopeRef, SourceRowHash: digest, LogicalHash: digest, References: []TransferReference{{Key: first}}},
	}
	fences[0].Fence.HeaderRoot, _ = TransferHeaderRoot(scope.SourceTenant, scope.SourceScopeRef, "objects", headers)
	fences[0].Fence.HeaderCount = int64(len(headers))
	root, err := TransferManifestDigest(scope, versions, fences, headers)
	if err != nil {
		t.Fatal("legacy v4 source IDs denied", err)
	}
	slices.Reverse(headers)
	slices.Reverse(fences)
	same, err := TransferManifestDigest(scope, versions, fences, headers)
	if err != nil || same != root {
		t.Fatal("order changed hash", err)
	}
	foreign := slices.Clone(headers)
	foreign[0].SourceScopeRef = uuid.New()
	if _, err := TransferManifestDigest(scope, versions, fences, foreign); !errors.Is(err, ErrTransferConflict) {
		t.Fatal("foreign centre admitted", err)
	}
	if _, err := TransferManifestDigest(scope, versions, fences[:1], headers); !errors.Is(err, ErrTransferNotReady) {
		t.Fatal("one fence accepted", err)
	}
	lost := []TransferHeader{headers[0]}
	if _, err := TransferManifestDigest(scope, versions, fences, lost); !errors.Is(err, ErrTransferNotReady) {
		t.Fatal("dangling internal ref accepted", err)
	}
	changed := slices.Clone(fences)
	changed[0].Fence.Epoch = "new-epoch"
	other, err := TransferManifestDigest(scope, versions, changed, headers)
	if err != nil || other == root {
		t.Fatal("fence epoch not bound", err)
	}
	maps := []TransferMapping{{Key: first, TargetRef: ids.New()}, {Key: second, TargetRef: ids.New()}}
	mapRoot, err := TransferMapDigest(maps)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(maps)
	mapSame, err := TransferMapDigest(maps)
	if err != nil || mapRoot != mapSame {
		t.Fatal("map ordering changed hash", err)
	}
	maps[0].TargetRef = maps[1].TargetRef
	if _, err := TransferMapDigest(maps); !errors.Is(err, ErrTransferConflict) {
		t.Fatal("target collision admitted", err)
	}
}

func TestTransferCanonicalReferencesAndCompleteFrozenInventory(t *testing.T) {
	digest := TransferDigest(strings.Repeat("b", 64))
	scope := TransferScope{SourceTenant: uuid.New(), SourceScopeRef: uuid.New(), TargetTenant: uuid.New(), TargetScopeRef: uuid.New(), IntentID: ids.New(), PlanID: ids.New()}
	scope.BindingHash, _ = TransferBindingDigest(scope)
	key := TransferKey{Namespace: "base", Ref: uuid.New()}
	h := TransferHeader{Key: key, SourceTenant: scope.SourceTenant, SourceScopeRef: scope.SourceScopeRef, SourceRowHash: digest, LogicalHash: digest}
	nilRoot, err := TransferHeaderRoot(scope.SourceTenant, scope.SourceScopeRef, "base", []TransferHeader{h})
	if err != nil {
		t.Fatal(err)
	}
	h.References = []TransferReference{}
	emptyRoot, err := TransferHeaderRoot(scope.SourceTenant, scope.SourceScopeRef, "base", []TransferHeader{h})
	if err != nil || nilRoot != emptyRoot {
		t.Fatal("empty references are not canonical", err)
	}
	emptyMap, _ := TransferMapDigest(nil)
	emptyMap2, _ := TransferMapDigest([]TransferMapping{})
	if emptyMap != emptyMap2 {
		t.Fatal("empty mapping not canonical")
	}
	h.References = []TransferReference{{Key: key}, {Key: key, External: true, ExternalTenant: uuid.New(), BasisRef: uuid.New()}}
	if _, err := TransferHeaderRoot(scope.SourceTenant, scope.SourceScopeRef, "base", []TransferHeader{h}); !errors.Is(err, ErrTransferConflict) {
		t.Fatal("ambiguous key accepted", err)
	}
	h.References = nil
	child := h
	child.Key = TransferKey{Namespace: "child", Ref: uuid.New()}
	child.References = []TransferReference{{Key: key}}
	headers := []TransferHeader{h, child}
	versions := []TransferAdapterVersion{{Module: "pf/party", Namespace: "base", Version: "v1"}, {Module: "pf/party", Namespace: "child", Version: "v1", DependsOn: []string{"base"}}}
	fences := []TransferFenceBinding{}
	for _, v := range versions {
		var hs []TransferHeader
		for _, item := range headers {
			if item.Key.Namespace == v.Namespace {
				hs = append(hs, item)
			}
		}
		root, err := TransferHeaderRoot(scope.SourceTenant, scope.SourceScopeRef, v.Namespace, hs)
		if err != nil {
			t.Fatal(err)
		}
		fences = append(fences, TransferFenceBinding{Namespace: v.Namespace, Side: TransferSource, Fence: TransferFence{Ref: ids.New(), Epoch: "one", HighWatermark: "1", HeaderRoot: root, HeaderCount: int64(len(hs))}}, TransferFenceBinding{Namespace: v.Namespace, Side: TransferTarget, Fence: TransferFence{Ref: ids.New(), Epoch: "one", HighWatermark: "0", HeaderRoot: digest}})
	}
	good, err := TransferManifestDigest(scope, versions, fences, headers)
	if err != nil {
		t.Fatal(err)
	}
	versions[1].DependsOn = nil
	if _, err := TransferManifestDigest(scope, versions, fences, headers); !errors.Is(err, ErrTransferConflict) {
		t.Fatal("undeclared dependency accepted", err)
	}
	versions[1].DependsOn = []string{"base"}
	wrong := slices.Clone(fences)
	wrong[0].Fence.HeaderRoot = digest
	if _, err := TransferManifestDigest(scope, versions, wrong, headers); !errors.Is(err, ErrTransferConflict) {
		t.Fatal("incomplete inventory root accepted", err)
	}
	wrong = slices.Clone(fences)
	wrong[0].Fence.HeaderCount++
	if _, err := TransferManifestDigest(scope, versions, wrong, headers); !errors.Is(err, ErrTransferConflict) {
		t.Fatal("incomplete inventory count accepted", err)
	}
	// DAG itself is bound, even when the manifest currently contains no cross-namespace edge.
	child.References = nil
	headers[1] = child
	fences[2].Fence.HeaderRoot, _ = TransferHeaderRoot(scope.SourceTenant, scope.SourceScopeRef, "child", []TransferHeader{child})
	withDep, err := TransferManifestDigest(scope, versions, fences, headers)
	if err != nil {
		t.Fatal(err)
	}
	versions[1].DependsOn = nil
	noDep, err := TransferManifestDigest(scope, versions, fences, headers)
	if err != nil || noDep == withDep || good == noDep {
		t.Fatal("DAG not bound", err)
	}
	pmap := []TransferMapping{{Key: child.Key, TargetRef: ids.New()}}
	dmap := []TransferMapping{{Key: key, TargetRef: ids.New()}}
	full, err := TransferBatchMapDigest(pmap, dmap)
	partial, _ := TransferBatchMapDigest(pmap, nil)
	if err != nil || full == partial {
		t.Fatal("dependency map not bound", err)
	}
	if _, err := TransferBatchMapDigest(pmap, pmap); !errors.Is(err, ErrTransferConflict) {
		t.Fatal("primary/dependency overlap accepted", err)
	}
}
