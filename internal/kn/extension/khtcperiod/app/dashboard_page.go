package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

// DashboardPage reads at most 100 period records and one batch of events per TX.
// Native ports and drafts are read only after TX, for live rows in the selected
// quarter or the annual quarter/year report table. Terminal rows need no I/O.
func (s *Service) DashboardPage(ctx context.Context, year, quarter int, department string, before uuid.UUID) ([]Detail, uuid.UUID, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, uuid.Nil, err
	}
	if year < 2000 || year > 2100 || quarter < 1 || quarter > 4 || len(department) > 64 {
		return nil, uuid.Nil, problem.Validation
	}
	start := time.Date(year, time.Month((quarter-1)*3+1), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 3, 0).AddDate(0, 0, -1)
	var periods []store.Period
	var events map[uuid.UUID][]store.Event
	var requests map[uuid.UUID]map[string]*store.AdhocRequest
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		periods, e = store.DashboardPage(ctx, tx, year, department, before, adhocReader(c))
		if e != nil {
			return e
		}
		ids := make([]uuid.UUID, 0, len(periods))
		for _, p := range periods {
			ids = append(ids, p.ID)
		}
		events, e = store.Events(ctx, tx, ids)
		if e != nil {
			return e
		}
		var adhocIDs []uuid.UUID
		for _, p := range periods {
			if p.Type == domain.TypeAdhoc && terminalOf(events[p.ID]) == "" && !p.From.After(end) && !p.To.Before(start) {
				adhocIDs = append(adhocIDs, p.ID)
			}
		}
		requests, e = store.ActiveAdhocRequestsForPeriods(ctx, tx, adhocIDs)
		return e
	})
	if err != nil {
		return nil, uuid.Nil, err
	}
	next := uuid.Nil
	if len(periods) == 100 {
		next = periods[len(periods)-1].ID
	}
	names, _, _ := s.unitDepartments(ctx, c.k)
	out := make([]Detail, 0, len(periods))
	for _, p := range periods {
		ev := events[p.ID]
		term := terminalOf(ev)
		d := Detail{Period: p, Events: ev, State: term}
		inQuarter := !p.From.After(end) && !p.To.Before(start)
		if term == "" && (inQuarter || p.Type == domain.TypeQuarter || p.Type == domain.TypeYear) {
			d = s.project(ctx, c, p, ev, nil, names, requests[p.ID])
			d.Sources = nil
		}
		out = append(out, d)
	}
	return out, next, nil
}
