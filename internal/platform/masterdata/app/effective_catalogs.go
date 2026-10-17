package app

import (
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// EffectiveCatalogsPort retains tenant transactions and schema checks in the capability.
type EffectiveCatalogsPort struct{ Svc *Service }

var _ port.EffectiveCatalogs = EffectiveCatalogsPort{}

func (p EffectiveCatalogsPort) ItemAt(ctx context.Context, catalog, code string, on time.Time) (port.EffectiveCatalogItem, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return port.EffectiveCatalogItem{}, err
	}
	if err := p.Svc.knownCatalog(catalog); err != nil {
		return port.EffectiveCatalogItem{}, err
	}
	var it *domain.Item
	err = p.Svc.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		it, e = p.Svc.itemAt(ctx, tx, catalog, code, domain.DayOf(on))
		return e
	})
	if err != nil {
		return port.EffectiveCatalogItem{}, err
	}
	if it == nil {
		return port.EffectiveCatalogItem{}, ErrItemNotFound.WithCause(port.ErrNotEffective)
	}
	return port.EffectiveCatalogItem{Item: ToPortItem(*it), ValidFrom: it.ValidFrom}, nil
}

func (p EffectiveCatalogsPort) ListAt(ctx context.Context, catalog string, on time.Time) ([]port.EffectiveCatalogItem, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := p.Svc.knownCatalog(catalog); err != nil {
		return nil, err
	}
	items, err := p.Svc.listAt(ctx, tid, catalog, domain.DayOf(on), false)
	if err != nil {
		return nil, err
	}
	out := make([]port.EffectiveCatalogItem, len(items))
	for i, it := range items {
		out[i] = port.EffectiveCatalogItem{Item: ToPortItem(it), ValidFrom: it.ValidFrom}
	}
	return out, nil
}

func (p EffectiveCatalogsPort) CodeByAttribute(ctx context.Context, catalog, attribute, value string) (string, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return "", err
	}
	if err := p.Svc.knownCatalog(catalog); err != nil {
		return "", err
	}
	declared := false
	for _, m := range p.Svc.CatalogMetadata() {
		if m.Catalog == catalog {
			declared = slices.Contains(m.Attributes, attribute)
			break
		}
	}
	if !declared {
		return "", problem.Validation.WithField("attribute", "thuộc tính không có trong định nghĩa danh mục")
	}
	var code string
	err = p.Svc.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		code, e = store.CodeByAttribute(ctx, tx, catalog, attribute, value)
		return e
	})
	return code, err
}
