package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/jackc/pgx/v5"
)

// OpenPeriodCount is the global open-period metadata count shared by the menu
// and hot card (BRD P768). Data filters do not narrow it. It keeps the existing
// reader/adhoc policy and is not a receipt or a claim that native Forms is open.
func (s *Service) OpenPeriodCount(ctx context.Context) (int, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		count, e = store.OpenPeriodCount(ctx, tx, adhocReader(c))
		return e
	})
	return count, err
}
