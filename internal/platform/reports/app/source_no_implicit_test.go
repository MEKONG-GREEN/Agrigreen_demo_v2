package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/google/uuid"
	"testing"
)

func TestSourceBuilderRequiresRegistration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		registry *SourceBuilderRegistry
	}{{name: "missing"}, {name: "empty", registry: NewSourceBuilderRegistry()}} {
		t.Run(tc.name, func(t *testing.T) {
			svc := Service{Catalog: NewCatalogRegistry(contractCatalogContribution(t)), Builders: tc.registry}
			_, err := svc.buildDataset(context.Background(), domain.Job{TenantID: uuid.New(), ScopeAll: true, Def: domain.Definition{Source: "extension.sowing"}, Params: domain.Params{Level: "province"}})
			if err == nil {
				t.Errorf("missing builder did not fail closed: %v", err)
			}
		})
	}
}
