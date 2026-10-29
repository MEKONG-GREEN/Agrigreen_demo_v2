package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	"github.com/jackc/pgx/v5"
	"strings"
	"unicode/utf8"
)

type ArchiveQuery struct {
	Year          int
	Kind, Search  string
	AllVersions   bool
	Offset, Limit int
}
type ArchiveResult struct {
	Items   []store.ArchiveRow `json:"items"`
	HasMore bool               `json:"has_more"`
	Offset  int                `json:"offset"`
}

func (q ArchiveQuery) Validate() error {
	if q.Year < 2000 || q.Year > 2100 || q.Offset < 0 || q.Offset > 100000 || q.Limit < 1 || q.Limit > 50 || !utf8.ValidString(q.Search) || len(q.Search) > 800 || utf8.RuneCountInString(q.Search) > 200 {
		return ErrInvalid
	}
	switch q.Kind {
	case "", "week", "month", "quarter", "year", "program", "adhoc":
		return nil
	}
	return ErrInvalid
}

// Archive keeps the existing KHTC session and tenant policy; no cross-centre grant.
func (s *Service) Archive(ctx context.Context, q ArchiveQuery) (ArchiveResult, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return ArchiveResult{}, err
	}
	if err = q.Validate(); err != nil {
		return ArchiveResult{}, err
	}
	out := ArchiveResult{Items: []store.ArchiveRow{}, Offset: q.Offset}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		var e error
		out.Items, e = store.Archive(ctx, tx, q.Year, q.Kind, strings.TrimSpace(q.Search), q.AllVersions, q.Offset, q.Limit+1)
		return e
	})
	if err != nil {
		return ArchiveResult{}, err
	}
	if len(out.Items) > q.Limit {
		out.HasMore = true
		out.Items = out.Items[:q.Limit]
	}
	return out, nil
}
