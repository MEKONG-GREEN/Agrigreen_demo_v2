package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func TestTransferOwnerErrorIsOpaqueAndDoesNotMeanMissingReceipt(t *testing.T) {
	marker := "private-owner-payload"
	if got := transferOwnerError(errors.New(marker)); got != port.ErrTransferDependency || strings.Contains(got.Error(), marker) || errors.Is(got, port.ErrTransferNotReady) {
		t.Fatal("unsafe owner error", got)
	}
	for _, safe := range []error{port.ErrTransferAuthority, port.ErrTransferConfiguration, port.ErrTransferConflict, port.ErrTransferNotReady, port.ErrTransferDependency} {
		if got := transferOwnerError(fmt.Errorf("%s: %w", marker, safe)); got != safe {
			t.Fatal("safe code not normalized", got)
		}
	}
}
func TestTransferBatchDependencyMappingsMustMatchExactReferences(t *testing.T) {
	dependency := port.TransferKey{Namespace: "base", Ref: ids.New()}
	member := port.TransferKey{Namespace: "child", Ref: ids.New()}
	req := port.TransferBatchRequest{Namespace: "child", Headers: []port.TransferHeader{{Key: member, References: []port.TransferReference{{Key: dependency}}}}, Mappings: []port.TransferMapping{{Key: member, TargetRef: ids.New()}}, DependencyMappings: []port.TransferMapping{{Key: dependency, TargetRef: ids.New()}}}
	req.MapHash, _ = port.TransferBatchMapDigest(req.Mappings, req.DependencyMappings)
	if err := validateTransferBatchMaps(req); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"missing", "extra", "primary-overlap"} {
		t.Run(kind, func(t *testing.T) {
			bad, _ := transferClone(req)
			switch kind {
			case "missing":
				bad.DependencyMappings = nil
			case "extra":
				bad.DependencyMappings = append(bad.DependencyMappings, port.TransferMapping{Key: port.TransferKey{Namespace: "unused", Ref: ids.New()}, TargetRef: ids.New()})
			case "primary-overlap":
				bad.DependencyMappings = append(bad.DependencyMappings, bad.Mappings[0])
			}
			bad.MapHash, _ = port.TransferBatchMapDigest(bad.Mappings, bad.DependencyMappings)
			if err := validateTransferBatchMaps(bad); !errors.Is(err, port.ErrTransferConflict) {
				t.Fatal("unproven dependency mapping accepted", err)
			}
		})
	}
}

func TestTransferProofReplayComparesInstantsAcrossJSONAndZones(t *testing.T) {
	proof := port.TransferCutoverProof{IdentityReceipt: ids.New(), RehearsalReceipt: ids.New(), BindingHash: port.TransferDigest(strings.Repeat("a", 64)), ManifestHash: port.TransferDigest(strings.Repeat("b", 64)), LogicalRoot: port.TransferDigest(strings.Repeat("c", 64)), ValidUntil: time.Now().Add(time.Hour)}
	stored, err := transferClone(proof)
	if err != nil {
		t.Fatal(err)
	}
	same := proof
	same.ValidUntil = proof.ValidUntil.In(time.FixedZone("other", 7*60*60))
	if !sameTransferCutoverProof(stored, same) {
		t.Fatal("same proof instant was rejected after JSON/zone conversion")
	}
	same.ValidUntil = same.ValidUntil.Add(time.Nanosecond)
	if sameTransferCutoverProof(stored, same) {
		t.Fatal("different expiry accepted")
	}
}
