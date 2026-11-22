package app

import (
	"context"
	"sort"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/jackc/pgx/v5"
)

// LiveWindow is a typed read window with no reporting-period identity. It does
// not authorize Refresh or create any snapshot. All caller/session checks and
// native public source ports remain the same as the existing source reader.
type LiveWindow struct {
	From, To, Cutoff, ConfirmationAt time.Time
}

type LiveValues struct {
	Values map[string]domain.Value
	Rows   []domain.Row
}

func (s *Service) ReadLive202(ctx context.Context, w LiveWindow, sourceKeys []string) (LiveValues, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return LiveValues{}, err
	}
	if w.From.IsZero() || w.To.Before(w.From) || w.Cutoff.IsZero() || w.ConfirmationAt.IsZero() {
		return LiveValues{}, ErrDependency.WithDetail("Khoảng đọc nguồn không hợp lệ")
	}
	from, err := windowFrom(rc.KHTCRule, w.From, w.To)
	if err != nil {
		return LiveValues{}, err
	}
	// External source I/O is outside every module transaction. ConfirmationAt is
	// supplied as the earlier of selected-day end and the actual read instant.
	periods, err := s.Sources.Periods(ctx, c.p.TenantID, port.KHTCQuery{From: from, To: w.To, PriceAt: &w.Cutoff, ConfirmationAt: &w.ConfirmationAt,
		Target: port.CrosswalkTarget{CalendarID: rc.KHTCRule.CalendarID, RuleTag: rc.KHTCRule.Tag(), PeriodType: rc.PeriodWeek}})
	if err != nil {
		return LiveValues{}, ErrDependency.WithCause(err)
	}
	weeks := make([]string, 0, len(periods))
	for _, p := range periods {
		weeks = append(weeks, p.TargetKey)
	}
	var frozen map[string]map[string]domain.Frozen
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; frozen, e = store.FrozenWeeks(ctx, tx, weeks); return e }); err != nil {
		return LiveValues{}, err
	}
	keys := append([]string{}, sourceKeys...)
	keys = append(keys, domain.ReportKeys(periods)...)
	sort.Strings(keys)
	out := LiveValues{Values: map[string]domain.Value{}, Rows: domain.RowsAt(periods, w.From, w.To, w.Cutoff)}
	for _, raw := range keys {
		if _, exists := out.Values[raw]; exists {
			continue
		}
		key, err := domain.ParseSourceKey(raw)
		if err != nil {
			// Match the existing snapshot reader's Invalid/unknown semantics:
			// strict parsing remains unchanged and invalid keys produce no value.
			continue
		}
		prior := map[string]domain.Frozen{}
		for week, values := range frozen {
			if value, ok := values[raw]; ok {
				prior[week] = value
			}
		}
		out.Values[raw] = domain.EvaluateAt(key, periods, prior, w.From, w.To, w.Cutoff)
	}
	return out, nil
}
