package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func TestEffectiveCatalogsRequireRegisteredSchema(t *testing.T) {
	ctx := tenant.WithID(context.Background(), uuid.New())
	for _, reg := range []*CatalogRegistry{nil, NewCatalogRegistry()} {
		p := EffectiveCatalogsPort{Svc: &Service{CatalogRegistry: reg}}
		if _, err := p.ItemAt(ctx, "unregistered_catalog", "A", time.Time{}); !errors.Is(err, ErrCatalogNotFound) {
			t.Fatalf("item: %v", err)
		}
		if _, err := p.ListAt(ctx, "unregistered_catalog", time.Time{}); !errors.Is(err, ErrCatalogNotFound) {
			t.Fatalf("list: %v", err)
		}
		if _, err := p.CodeByAttribute(ctx, "unregistered_catalog", "value", "x"); !errors.Is(err, ErrCatalogNotFound) {
			t.Fatalf("lookup: %v", err)
		}
	}
	p := EffectiveCatalogsPort{Svc: &Service{CatalogRegistry: NewCatalogRegistry(sampleCatalogRegistration())}}
	if _, err := p.CodeByAttribute(ctx, "fixture_catalog", "undeclared", "x"); err == nil {
		t.Fatal("undeclared attribute queried storage")
	}
}
