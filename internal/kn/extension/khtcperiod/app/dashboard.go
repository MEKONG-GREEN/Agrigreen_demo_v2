package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SourceStates reads the same source projection as Get, batching database and
// crosswalk reads for the selected dashboard periods. Crosswalk I/O is outside tx.
func (s *Service) SourceStates(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]SourceRow, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if len(ids) > 100 {
		return nil, problem.Validation.WithField("periods", "tối đa 100 kỳ")
	}
	out := make(map[uuid.UUID][]SourceRow)
	if len(ids) == 0 {
		return out, nil
	}
	var srcs []store.Source
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		srcs, e = store.SourcesForReader(ctx, tx, ids, adhocReader(c))
		return e
	})
	if err != nil {
		return nil, err
	}
	rows := s.sourceRows(ctx, srcs)
	for i, row := range rows {
		id := srcs[i].PeriodID
		out[id] = append(out[id], row)
	}
	return out, nil
}
