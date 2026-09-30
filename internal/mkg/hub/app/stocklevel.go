package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// StockByProduct cài đặt port.Stock (G4, tenant tường minh, không kiểm vai hub — mkg/orders O-05).
func (s *Service) StockByProduct(ctx context.Context, tid uuid.UUID, product string, facility *uuid.UUID) ([]port.StockLevel, error) {
	var out []port.StockLevel
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		ls, err := store.StockByProduct(ctx, tx, product, facility)
		if err != nil {
			return err
		}
		for _, l := range ls {
			out = append(out, port.StockLevel{FacilityID: l.FacilityID, Product: l.Product, AvailableKg: l.AvailableKg, Lots: l.Lots})
		}
		return nil
	})
	return out, err
}

var _ port.Stock = (*Service)(nil)
