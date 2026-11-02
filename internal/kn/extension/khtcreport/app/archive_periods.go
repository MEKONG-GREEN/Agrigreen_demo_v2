package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	"github.com/jackc/pgx/v5"
	"strings"
)

type ArchivePeriodsResult struct {
	Items   []store.ArchivePeriod `json:"items"`
	HasMore bool                  `json:"has_more"`
	Offset  int                   `json:"offset"`
}

func (s *Service) ArchivePeriods(ctx context.Context, q ArchiveQuery) (ArchivePeriodsResult, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return ArchivePeriodsResult{}, err
	}
	if err = q.Validate(); err != nil {
		return ArchivePeriodsResult{}, err
	}
	out := ArchivePeriodsResult{Items: []store.ArchivePeriod{}, Offset: q.Offset}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		var e error
		out.Items, e = store.ArchivePeriods(ctx, tx, q.Year, q.Kind, strings.TrimSpace(q.Search), q.Offset, q.Limit+1)
		return e
	})
	if err != nil {
		return ArchivePeriodsResult{}, err
	}
	if len(out.Items) > q.Limit {
		out.HasMore = true
		out.Items = out.Items[:q.Limit]
	}
	return out, nil
}
