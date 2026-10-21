package app

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func transferNormalState(s *store.TransferState) bool {
	return s.Plan.Status == "accepted" || s.Plan.Status == "importing" || s.Plan.Status == "verified" || s.Plan.Status == "cutover_prepared"
}
func transferBatchView(b port.TransferBatchRequest) port.TransferBatch {
	return port.TransferBatch{ID: b.BatchID, PlanID: b.Scope.PlanID, Namespace: b.Namespace, ManifestHash: b.ManifestHash, MapHash: b.MapHash, Mappings: slices.Clone(b.Mappings)}
}
func (t *Transfers) batches(p port.TransferParticipants, plan uuid.UUID) ([]port.TransferBatchRequest, error) {
	var out []port.TransferBatchRequest
	err := t.local(p.Target(), func(tx pgx.Tx) error {
		var err error
		out, err = store.TransferBatches(p.Target(), tx, plan)
		return err
	})
	return out, err
}
func (t *Transfers) proof(p port.TransferParticipants, plan, batch uuid.UUID, operation, namespace string) (*store.TransferProof, error) {
	var out *store.TransferProof
	err := t.local(p.Target(), func(tx pgx.Tx) error {
		var err error
		out, err = store.TransferProofOf(p.Target(), tx, plan, batch, operation, namespace)
		return err
	})
	return out, err
}
func transferDecodeReceipt(proof *store.TransferProof) (port.TransferReceipt, error) {
	var r port.TransferReceipt
	if proof == nil {
		return r, port.ErrTransferNotReady
	}
	if err := json.Unmarshal(proof.Document, &r); err != nil {
		return r, port.ErrTransferConflict
	}
	hash, err := transferDigest(r)
	if err != nil || hash != proof.Hash || r.Ref != proof.Ref {
		return r, port.ErrTransferConflict
	}
	return r, nil
}
func compareTransferKeys(a, b port.TransferKey) int {
	if c := strings.Compare(a.Namespace, b.Namespace); c != 0 {
		return c
	}
	return strings.Compare(a.Ref.String(), b.Ref.String())
}
func transferSortMappings(items []port.TransferMapping) {
	slices.SortFunc(items, func(a, b port.TransferMapping) int { return compareTransferKeys(a.Key, b.Key) })
}

func (t *Transfers) ReserveBatch(p port.TransferParticipants, ref port.TransferRef, limit int) (port.TransferBatch, error) {
	source, target, adapters, err := t.pair(p, ref, "reserve", false)
	if err != nil {
		return port.TransferBatch{}, err
	}
	if !transferNormalState(target) {
		return port.TransferBatch{}, port.ErrTransferNotReady
	}
	if limit < 1 || limit > 500 {
		return port.TransferBatch{}, port.ErrTransferConflict
	}
	if err := t.fences(p, source, target, adapters); err != nil {
		return port.TransferBatch{}, err
	}
	batches, err := t.batches(p, ref.ID)
	if err != nil {
		return port.TransferBatch{}, err
	}
	assigned := map[port.TransferKey]bool{}
	completed := map[port.TransferKey]port.TransferMapping{}
	for _, b := range batches {
		for _, m := range b.Mappings {
			if assigned[m.Key] {
				return port.TransferBatch{}, port.ErrTransferConflict
			}
			assigned[m.Key] = true
		}
		proof, err := t.proof(p, ref.ID, b.BatchID, "apply", b.Namespace)
		if err != nil {
			return port.TransferBatch{}, err
		}
		if proof == nil {
			// No request idempotency key in v1: return the oldest unfinished reservation.
			if _, _, _, _, err := t.batch(p, port.TransferBatchRef{PlanID: ref.ID, BatchID: b.BatchID}, "reserve_replay", false); err != nil {
				return port.TransferBatch{}, err
			}
			return transferBatchView(b), nil
		}
		receipt, err := transferDecodeReceipt(proof)
		if err != nil {
			return port.TransferBatch{}, err
		}
		if err := validateTransferReceipt(b, receipt, false); err != nil {
			return port.TransferBatch{}, err
		}
		for _, m := range b.Mappings {
			completed[m.Key] = m
		}
	}
	if target.Plan.Status == "verified" || target.Plan.Status == "cutover_prepared" {
		return port.TransferBatch{}, port.ErrTransferNotReady
	}
	headers, err := t.headers(p.Target(), ref.ID)
	if err != nil {
		return port.TransferBatch{}, err
	}
	var selected []port.TransferHeader
	var adapter port.TransferAdapter
	for _, a := range adapters {
		predecessorsComplete := true
		for _, dep := range a.DependsOn {
			for _, h := range headers {
				if h.Key.Namespace == dep {
					if _, ok := completed[h.Key]; !ok {
						predecessorsComplete = false
					}
				}
			}
		}
		if !predecessorsComplete {
			continue
		}
		for _, h := range headers {
			if h.Key.Namespace != a.Namespace || assigned[h.Key] {
				continue
			}
			ready := true
			for _, ref := range h.References {
				if !ref.External {
					if _, ok := completed[ref.Key]; !ok {
						ready = false
					}
				}
			}
			if ready {
				selected = append(selected, h)
				if len(selected) >= min(limit, a.MaxBatch) {
					break
				}
			}
		}
		if len(selected) > 0 {
			adapter = a
			break
		}
	}
	if len(selected) == 0 {
		return port.TransferBatch{}, port.ErrTransferNotReady
	}
	dependencySet := map[port.TransferKey]bool{}
	for _, h := range selected {
		for _, ref := range h.References {
			if !ref.External {
				dependencySet[ref.Key] = true
			}
		}
	}
	deps := make([]port.TransferMapping, 0, len(dependencySet))
	for key := range dependencySet {
		deps = append(deps, completed[key])
	}
	transferSortMappings(deps)
	request := port.TransferBatchRequest{Scope: source.Plan.Scope, Namespace: adapter.Namespace, BatchID: ids.New(), ManifestHash: source.Plan.ManifestHash, Headers: selected, DependencyMappings: deps, SourceFence: source.SourceFences[adapter.Namespace], TargetFence: target.Intent.TargetFences[adapter.Namespace]}
	err = t.local(p.Target(), func(tx pgx.Tx) error {
		if err := store.LockTransfer(p.Target(), tx, request.Scope.TargetTenant, target.Intent.ID); err != nil {
			return err
		}
		current, err := store.TransferLatest(p.Target(), tx, target.Intent.ID)
		if err != nil {
			return err
		}
		if current == nil || current.Sequence != target.Sequence {
			return port.ErrTransferConflict
		}
		for _, h := range selected {
			m, err := store.ReserveTransferMapping(p.Target(), tx, request.Scope, h.Key)
			if err != nil {
				return err
			}
			request.Mappings = append(request.Mappings, m)
		}
		transferSortMappings(request.Mappings)
		for _, dep := range deps {
			actual, err := store.TransferMappingOf(p.Target(), tx, request.Scope, dep.Key)
			if err != nil {
				return err
			}
			if actual == nil || *actual != dep {
				return port.ErrTransferConflict
			}
		}
		request.MapHash, err = port.TransferBatchMapDigest(request.Mappings, request.DependencyMappings)
		if err != nil {
			return err
		}
		if err := store.InsertTransferBatch(p.Target(), tx, transferActor(p.Target()), request, t.now()); err != nil {
			return err
		}
		target.Plan.Status = "importing"
		return store.AppendTransferState(p.Target(), tx, request.Scope.TargetTenant, request.Scope.TargetScopeRef, transferActor(p.Target()), target.Intent.ID, target.Sequence, "batch_reserved", target, t.now())
	})
	return transferBatchView(request), err
}

func (t *Transfers) batch(p port.TransferParticipants, ref port.TransferBatchRef, action string, recovery bool) (*store.TransferState, *store.TransferState, port.TransferAdapter, port.TransferBatchRequest, error) {
	source, target, adapters, err := t.pair(p, port.TransferRef{ID: ref.PlanID}, action, recovery)
	if err != nil {
		return nil, nil, port.TransferAdapter{}, port.TransferBatchRequest{}, err
	}
	if (!recovery && !transferNormalState(target)) || target.Plan.ID != ref.PlanID {
		return nil, nil, port.TransferAdapter{}, port.TransferBatchRequest{}, port.ErrTransferNotReady
	}
	if err := t.fences(p, source, target, adapters); err != nil {
		return nil, nil, port.TransferAdapter{}, port.TransferBatchRequest{}, err
	}
	var req *port.TransferBatchRequest
	err = t.local(p.Target(), func(tx pgx.Tx) error {
		var err error
		req, err = store.TransferBatchByID(p.Target(), tx, ref.PlanID, ref.BatchID)
		return err
	})
	if err != nil {
		return nil, nil, port.TransferAdapter{}, port.TransferBatchRequest{}, err
	}
	if req == nil {
		return nil, nil, port.TransferAdapter{}, port.TransferBatchRequest{}, port.ErrTransferNotReady
	}
	if req.Scope != source.Plan.Scope || req.BatchID != ref.BatchID || req.ManifestHash != source.Plan.ManifestHash || target.Plan.ManifestHash != req.ManifestHash {
		return nil, nil, port.TransferAdapter{}, port.TransferBatchRequest{}, port.ErrTransferConflict
	}
	var adapter port.TransferAdapter
	for _, a := range adapters {
		if a.Namespace == req.Namespace {
			adapter = a
		}
	}
	if adapter.Namespace == "" || len(req.Headers) == 0 || len(req.Headers) > adapter.MaxBatch || req.SourceFence != source.SourceFences[req.Namespace] || req.TargetFence != target.Intent.TargetFences[req.Namespace] {
		return nil, nil, port.TransferAdapter{}, port.TransferBatchRequest{}, port.ErrTransferConflict
	}
	if err := validateTransferBatchMaps(*req); err != nil {
		return nil, nil, port.TransferAdapter{}, port.TransferBatchRequest{}, err
	}
	err = t.local(p.Target(), func(tx pgx.Tx) error {
		for _, m := range append(slices.Clone(req.Mappings), req.DependencyMappings...) {
			actual, err := store.TransferMappingOf(p.Target(), tx, req.Scope, m.Key)
			if err != nil {
				return err
			}
			if actual == nil || *actual != m {
				return port.ErrTransferConflict
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, port.TransferAdapter{}, port.TransferBatchRequest{}, err
	}
	// The stored batch must be an exact subset of the sealed local source headers.
	sealed, err := t.headers(p.Source(), ref.PlanID)
	if err != nil {
		return nil, nil, port.TransferAdapter{}, port.TransferBatchRequest{}, err
	}
	byKey := map[port.TransferKey]port.TransferHeader{}
	for _, h := range sealed {
		byKey[h.Key] = h
	}
	expected := []port.TransferHeader{}
	for _, h := range req.Headers {
		original, ok := byKey[h.Key]
		if !ok {
			return nil, nil, port.TransferAdapter{}, port.TransferBatchRequest{}, port.ErrTransferConflict
		}
		expected = append(expected, original)
	}
	actualRoot, err := port.TransferHeaderRoot(req.Scope.SourceTenant, req.Scope.SourceScopeRef, req.Namespace, req.Headers)
	if err != nil {
		return nil, nil, port.TransferAdapter{}, port.TransferBatchRequest{}, err
	}
	expectedRoot, err := port.TransferHeaderRoot(req.Scope.SourceTenant, req.Scope.SourceScopeRef, req.Namespace, expected)
	if err != nil || actualRoot != expectedRoot {
		return nil, nil, port.TransferAdapter{}, port.TransferBatchRequest{}, port.ErrTransferConflict
	}
	return source, target, adapter, *req, nil
}
func validateTransferBatchMaps(req port.TransferBatchRequest) error {
	hash, err := port.TransferBatchMapDigest(req.Mappings, req.DependencyMappings)
	if err != nil || hash != req.MapHash || len(req.Headers) != len(req.Mappings) {
		return port.ErrTransferConflict
	}
	members := map[port.TransferKey]bool{}
	dependencies := map[port.TransferKey]bool{}
	for _, h := range req.Headers {
		if h.Key.Namespace != req.Namespace || members[h.Key] {
			return port.ErrTransferConflict
		}
		members[h.Key] = true
		for _, ref := range h.References {
			if !ref.External {
				dependencies[ref.Key] = true
			}
		}
	}
	for _, m := range req.Mappings {
		if !members[m.Key] {
			return port.ErrTransferConflict
		}
	}
	if len(dependencies) != len(req.DependencyMappings) {
		return port.ErrTransferConflict
	}
	for _, m := range req.DependencyMappings {
		if !dependencies[m.Key] || members[m.Key] {
			return port.ErrTransferConflict
		}
	}
	return nil
}
func validateTransferArtifact(req port.TransferBatchRequest, a port.TransferArtifact, version string) error {
	root, err := port.TransferHeaderRoot(req.Scope.SourceTenant, req.Scope.SourceScopeRef, req.Namespace, req.Headers)
	if err != nil || a.Ref == uuid.Nil || a.ExportReceiptRef == uuid.Nil || !port.ValidTransferDigest(a.Hash) || a.HeaderRoot != root || a.ManifestHash != req.ManifestHash || a.MapHash != req.MapHash || a.BindingHash != req.Scope.BindingHash || a.OwnerVersion != version {
		return port.ErrTransferConflict
	}
	return nil
}
func transferDecodeArtifact(proof *store.TransferProof, req port.TransferBatchRequest, version string) (port.TransferArtifact, error) {
	var artifact port.TransferArtifact
	if proof == nil {
		return artifact, port.ErrTransferNotReady
	}
	if err := json.Unmarshal(proof.Document, &artifact); err != nil {
		return artifact, port.ErrTransferConflict
	}
	hash, err := transferDigest(artifact)
	if err != nil || hash != proof.Hash || artifact.ExportReceiptRef != proof.Ref {
		return artifact, port.ErrTransferConflict
	}
	if err := validateTransferArtifact(req, artifact, version); err != nil {
		return artifact, err
	}
	return artifact, nil
}

func validateTransferReceipt(req port.TransferBatchRequest, r port.TransferReceipt, compensation bool) error {
	if r.Ref == uuid.Nil || r.PlanID != req.Scope.PlanID || r.BatchID != req.BatchID || r.TargetTenant != req.Scope.TargetTenant || r.TargetScopeRef != req.Scope.TargetScopeRef || r.BindingHash != req.Scope.BindingHash || r.ManifestHash != req.ManifestHash || r.MapHash != req.MapHash || !port.ValidTransferDigest(r.ArtifactHash) || !port.ValidTransferDigest(r.LogicalRoot) || len(r.Items) != len(req.Headers) {
		return port.ErrTransferConflict
	}
	if !compensation && (r.OperationID != uuid.Nil || r.ReasonRef != uuid.Nil || r.CompensatesRef != uuid.Nil || r.SourceLogicalRoot != "") {
		return port.ErrTransferConflict
	}
	expected := map[port.TransferKey]port.TransferMapping{}
	hashes := map[port.TransferKey]port.TransferDigest{}
	for _, m := range req.Mappings {
		expected[m.Key] = m
	}
	for _, h := range req.Headers {
		hashes[h.Key] = h.LogicalHash
	}
	seen := map[port.TransferKey]bool{}
	for _, item := range r.Items {
		m, ok := expected[item.Key]
		if !ok || seen[item.Key] || item.TargetRef != m.TargetRef || !port.ValidTransferDigest(item.TargetRowHash) || !port.ValidTransferDigest(item.LogicalHash) || (!compensation && item.LogicalHash != hashes[item.Key]) {
			return port.ErrTransferConflict
		}
		seen[item.Key] = true
	}
	if !compensation {
		root, err := port.TransferLogicalRoot(req.Headers)
		if err != nil || root != r.LogicalRoot {
			return port.ErrTransferConflict
		}
	}
	return nil
}
func transferCanonicalReceipt(r port.TransferReceipt) (port.TransferReceipt, error) {
	out, err := transferClone(r)
	if err != nil {
		return out, err
	}
	slices.SortFunc(out.Items, func(a, b port.TransferAppliedRef) int { return compareTransferKeys(a.Key, b.Key) })
	return out, nil
}
func (t *Transfers) ownerReceipt(p port.TransferParticipants, a port.TransferAdapter, req port.TransferApplyRequest) (port.TransferReceipt, bool, error) {
	copyReq, err := transferClone(req)
	if err != nil {
		return port.TransferReceipt{}, false, err
	}
	receipt, err := a.ReadReceipt(p.Target(), copyReq)
	if err != nil {
		return port.TransferReceipt{}, false, transferOwnerError(err)
	}
	receipt, err = transferCanonicalReceipt(receipt)
	if err != nil {
		return port.TransferReceipt{}, true, err
	}
	if receipt.OwnerVersion != a.Version || receipt.ArtifactHash != req.Artifact.Hash {
		return port.TransferReceipt{}, true, port.ErrTransferConflict
	}
	if err := validateTransferReceipt(req.TransferBatchRequest, receipt, false); err != nil {
		return port.TransferReceipt{}, true, err
	}
	copyReq, err = transferClone(req)
	if err != nil {
		return port.TransferReceipt{}, true, err
	}
	copyReceipt, err := transferClone(receipt)
	if err != nil {
		return port.TransferReceipt{}, true, err
	}
	if err := a.Verify(p.Target(), copyReq, copyReceipt); err != nil {
		return port.TransferReceipt{}, true, transferOwnerError(err)
	}
	return receipt, true, nil
}
func (t *Transfers) dependencies(p port.TransferParticipants, req port.TransferBatchRequest) error {
	if len(req.DependencyMappings) == 0 {
		return nil
	}
	batches, err := t.batches(p, req.Scope.PlanID)
	if err != nil {
		return err
	}
	wanted := map[port.TransferKey]port.TransferMapping{}
	for _, m := range req.DependencyMappings {
		wanted[m.Key] = m
	}
	done := map[port.TransferKey]bool{}
	for _, b := range batches {
		needed := false
		for _, m := range b.Mappings {
			if want, ok := wanted[m.Key]; ok {
				if want != m {
					return port.ErrTransferConflict
				}
				needed = true
			}
		}
		if !needed {
			continue
		}
		_, _, adapter, previous, err := t.batch(p, port.TransferBatchRef{PlanID: req.Scope.PlanID, BatchID: b.BatchID}, "dependency_read", false)
		if err != nil {
			return err
		}
		proof, err := t.proof(p, req.Scope.PlanID, b.BatchID, "apply", b.Namespace)
		if err != nil {
			return err
		}
		stored, err := transferDecodeReceipt(proof)
		if err != nil {
			return err
		}
		artifactProof, err := t.proof(p, req.Scope.PlanID, b.BatchID, "export", b.Namespace)
		if err != nil {
			return err
		}
		if artifactProof == nil {
			return port.ErrTransferNotReady
		}
		artifact, err := transferDecodeArtifact(artifactProof, previous, adapter.Version)
		if err != nil {
			return err
		}
		actual, _, err := t.ownerReceipt(p, adapter, port.TransferApplyRequest{TransferBatchRequest: previous, Artifact: artifact})
		if err != nil {
			return err
		}
		hash, err := transferDigest(actual)
		if err != nil || hash != proof.Hash || actual.Ref != stored.Ref {
			return port.ErrTransferConflict
		}
		for _, m := range b.Mappings {
			if _, ok := wanted[m.Key]; ok {
				done[m.Key] = true
			}
		}
	}
	if len(done) != len(wanted) {
		return port.ErrTransferNotReady
	}
	return nil
}
func (t *Transfers) checkpoint(p port.TransferParticipants, target *store.TransferState, req port.TransferBatchRequest, operation string, ref uuid.UUID, document any) error {
	hash, err := transferDigest(document)
	if err != nil {
		return err
	}
	old, err := t.proof(p, req.Scope.PlanID, req.BatchID, operation, req.Namespace)
	if err != nil {
		return err
	}
	if old != nil {
		if old.Ref != ref || old.Hash != hash {
			return port.ErrTransferConflict
		}
		return nil
	}
	return t.local(p.Target(), func(tx pgx.Tx) error {
		if err := store.LockTransfer(p.Target(), tx, req.Scope.TargetTenant, target.Intent.ID); err != nil {
			return err
		}
		if err := store.InsertTransferProof(p.Target(), tx, req.Scope, transferActor(p.Target()), req.BatchID, operation, req.Namespace, ref, hash, document, t.now()); err != nil {
			return err
		}
		return store.AppendTransferState(p.Target(), tx, req.Scope.TargetTenant, req.Scope.TargetScopeRef, transferActor(p.Target()), target.Intent.ID, target.Sequence, "batch_"+operation, target, t.now())
	})
}
func (t *Transfers) ExportBatch(p port.TransferParticipants, ref port.TransferBatchRef) (port.TransferArtifact, error) {
	_, target, a, req, err := t.batch(p, ref, "export", false)
	if err != nil {
		return port.TransferArtifact{}, err
	}
	if err := t.dependencies(p, req); err != nil {
		return port.TransferArtifact{}, err
	}
	copyReq, err := transferClone(req)
	if err != nil {
		return port.TransferArtifact{}, err
	}
	artifact, err := a.ReadExport(p.Source(), copyReq)
	err = transferOwnerError(err)
	if errors.Is(err, port.ErrTransferNotReady) {
		copyReq, e := transferClone(req)
		if e != nil {
			return port.TransferArtifact{}, e
		}
		artifact, err = a.Export(p.Source(), copyReq)
		err = transferOwnerError(err)
	}
	if err != nil {
		return port.TransferArtifact{}, err
	}
	if err := validateTransferArtifact(req, artifact, a.Version); err != nil {
		return port.TransferArtifact{}, err
	}
	copyReq, err = transferClone(req)
	if err != nil {
		return port.TransferArtifact{}, err
	}
	durable, err := a.ReadExport(p.Source(), copyReq)
	if err != nil {
		return port.TransferArtifact{}, transferOwnerError(err)
	}
	if durable != artifact {
		return port.TransferArtifact{}, port.ErrTransferConflict
	}
	if err := t.authenticate(p); err != nil {
		return port.TransferArtifact{}, err
	}
	return artifact, t.checkpoint(p, target, req, "export", artifact.ExportReceiptRef, artifact)
}
func (t *Transfers) ApplyBatch(p port.TransferParticipants, ref port.TransferBatchRef) (port.TransferReceipt, error) {
	_, target, a, req, err := t.batch(p, ref, "apply", false)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	if err := t.dependencies(p, req); err != nil {
		return port.TransferReceipt{}, err
	}
	proof, err := t.proof(p, ref.PlanID, ref.BatchID, "export", req.Namespace)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	if proof == nil {
		return port.TransferReceipt{}, port.ErrTransferNotReady
	}
	artifact, err := transferDecodeArtifact(proof, req, a.Version)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	copyReq, err := transferClone(req)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	durableArtifact, err := a.ReadExport(p.Source(), copyReq)
	if err != nil {
		return port.TransferReceipt{}, transferOwnerError(err)
	}
	if artifact != durableArtifact {
		return port.TransferReceipt{}, port.ErrTransferConflict
	}
	apply := port.TransferApplyRequest{TransferBatchRequest: req, Artifact: artifact}
	receipt, found, err := t.ownerReceipt(p, a, apply)
	if !found && errors.Is(err, port.ErrTransferNotReady) {
		if target.Plan.Status != "importing" {
			return port.TransferReceipt{}, port.ErrTransferNotReady
		}
		copyApply, e := transferClone(apply)
		if e != nil {
			return port.TransferReceipt{}, e
		}
		returned, e := a.Apply(p.Target(), copyApply)
		if e != nil {
			return port.TransferReceipt{}, transferOwnerError(e)
		}
		returned, e = transferCanonicalReceipt(returned)
		if e != nil {
			return port.TransferReceipt{}, e
		}
		receipt, _, err = t.ownerReceipt(p, a, apply)
		if err != nil {
			return port.TransferReceipt{}, err
		}
		expected, e := transferDigest(returned)
		if e != nil {
			return port.TransferReceipt{}, e
		}
		actual, e := transferDigest(receipt)
		if e != nil || expected != actual {
			return port.TransferReceipt{}, port.ErrTransferConflict
		}
	}
	if err != nil {
		return port.TransferReceipt{}, err
	}
	if err := t.authenticate(p); err != nil {
		return port.TransferReceipt{}, err
	}
	return receipt, t.checkpoint(p, target, req, "apply", receipt.Ref, receipt)
}
func (t *Transfers) Verify(p port.TransferParticipants, ref port.TransferRef) (port.TransferReport, error) {
	source, target, adapters, err := t.pair(p, ref, "verify", false)
	if err != nil {
		return port.TransferReport{}, err
	}
	if !transferNormalState(target) {
		return port.TransferReport{}, port.ErrTransferNotReady
	}
	manifest, headers, err := t.manifest(p, source, target, adapters)
	if err != nil {
		return port.TransferReport{}, err
	}
	if manifest != source.Plan.ManifestHash || manifest != target.Plan.ManifestHash {
		return port.TransferReport{}, port.ErrTransferConflict
	}
	targetHeaders, err := t.headers(p.Target(), ref.ID)
	if err != nil {
		return port.TransferReport{}, err
	}
	if len(headers) != len(targetHeaders) {
		return port.TransferReport{}, port.ErrTransferConflict
	}
	batches, err := t.batches(p, ref.ID)
	if err != nil {
		return port.TransferReport{}, err
	}
	seen := map[port.TransferKey]bool{}
	result := []port.TransferHeader{}
	for _, b := range batches {
		_, _, a, req, err := t.batch(p, port.TransferBatchRef{PlanID: ref.ID, BatchID: b.BatchID}, "verify_batch", false)
		if err != nil {
			return port.TransferReport{}, err
		}
		proof, err := t.proof(p, ref.ID, b.BatchID, "apply", b.Namespace)
		if err != nil {
			return port.TransferReport{}, err
		}
		if proof == nil {
			return port.TransferReport{}, port.ErrTransferNotReady
		}
		artifactProof, err := t.proof(p, ref.ID, b.BatchID, "export", b.Namespace)
		if err != nil {
			return port.TransferReport{}, err
		}
		if artifactProof == nil {
			return port.TransferReport{}, port.ErrTransferNotReady
		}
		artifact, err := transferDecodeArtifact(artifactProof, req, a.Version)
		if err != nil {
			return port.TransferReport{}, err
		}
		receipt, _, err := t.ownerReceipt(p, a, port.TransferApplyRequest{TransferBatchRequest: req, Artifact: artifact})
		if err != nil {
			return port.TransferReport{}, err
		}
		hash, err := transferDigest(receipt)
		if err != nil || hash != proof.Hash {
			return port.TransferReport{}, port.ErrTransferConflict
		}
		for _, item := range receipt.Items {
			if seen[item.Key] {
				return port.TransferReport{}, port.ErrTransferConflict
			}
			seen[item.Key] = true
			result = append(result, port.TransferHeader{Key: item.Key, LogicalHash: item.LogicalHash})
		}
	}
	if len(seen) != len(headers) {
		return port.TransferReport{}, port.ErrTransferNotReady
	}
	for _, h := range headers {
		if !seen[h.Key] {
			return port.TransferReport{}, port.ErrTransferNotReady
		}
	}
	root, err := port.TransferLogicalRoot(headers)
	if err != nil {
		return port.TransferReport{}, err
	}
	actual, err := port.TransferLogicalRoot(result)
	if err != nil || actual != root {
		return port.TransferReport{}, port.ErrTransferConflict
	}
	report := port.TransferReport{PlanID: ref.ID, Count: len(headers), Root: root}
	if target.Plan.Status == "verified" || target.Plan.Status == "cutover_prepared" {
		if target.VerifiedRoot != root {
			return port.TransferReport{}, port.ErrTransferConflict
		}
		return report, nil
	}
	target.VerifiedRoot = root
	target.Plan.Status = "verified"
	return report, t.write(p.Target(), target.Intent.ID, target.Plan.Scope.TargetScopeRef, "verified", target)
}
func (t *Transfers) PrepareCutover(p port.TransferParticipants, ref port.TransferRef) (port.TransferPlan, error) {
	report, err := t.Verify(p, ref)
	if err != nil {
		return port.TransferPlan{}, err
	}
	source, target, adapters, err := t.pair(p, ref, "prepare_cutover", false)
	if err != nil {
		return port.TransferPlan{}, err
	}
	if t.registry.Cutover() == nil {
		return port.TransferPlan{}, port.ErrTransferNotReady
	}
	proof, err := t.registry.Cutover()(p, source.Plan.Scope, report)
	if err != nil {
		return port.TransferPlan{}, transferOwnerError(err)
	}
	if proof.IdentityReceipt == uuid.Nil || proof.RehearsalReceipt == uuid.Nil || proof.BindingHash != source.Plan.Scope.BindingHash || proof.ManifestHash != source.Plan.ManifestHash || proof.LogicalRoot != report.Root || !proof.ValidUntil.After(t.now()) {
		return port.TransferPlan{}, port.ErrTransferNotReady
	}
	if err := t.fences(p, source, target, adapters); err != nil {
		return port.TransferPlan{}, err
	}
	if target.Plan.Status == "cutover_prepared" {
		if target.CutoverProof == nil || *target.CutoverProof != proof {
			return port.TransferPlan{}, port.ErrTransferConflict
		}
		return target.Plan, nil
	}
	target.CutoverProof = &proof
	target.Plan.Status = "cutover_prepared"
	return target.Plan, t.write(p.Target(), target.Intent.ID, target.Plan.Scope.TargetScopeRef, "cutover_prepared", target)
}
