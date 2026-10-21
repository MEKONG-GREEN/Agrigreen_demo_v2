package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

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
