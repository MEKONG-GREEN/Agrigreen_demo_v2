package app

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	sa "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/app"
	sd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) liveDashboard202(ctx context.Context, c caller, window *domain.LiveM5Window, q DashboardQuery, now time.Time) (domain.DashboardProgress, error) {
	empty := domain.DashboardProgress{Programs: []domain.DashboardProgram{}, Rows: []domain.ProgramRow{}}
	if window == nil {
		return empty, nil
	}
	// The existing national reader may read dashboard metadata, but the native
	// kn_khtc profile does not grant that role submission access. Keep the actual
	// caller and report this bounded live slice as unavailable before new I/O.
	if c.kn.Role == port.SessionRoleNational {
		window.UnavailableReason = domain.LiveM5NativeFormsAccessUnavailable
		return empty, nil
	}
	if s.Plans == nil || s.Forms == nil {
		return empty, ErrDependency
	}
	plan, err := s.Plans.PlanAt(ctx, window.Year, window.Cutoff)
	if errors.Is(err, port.ErrKHTCPlanNotFound) {
		plan = port.KHTCPlan{Year: window.Year, Entries: []port.KHTCPlanEntry{}}
	} else if err != nil {
		return empty, err
	}
	if err := boundedPlan(plan); err != nil {
		return empty, err
	}
	if plan.VersionID != uuid.Nil {
		id := plan.VersionID.String()
		window.PlanVersionID = &id
	}
	var periods []ps.Period
	if err := s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		var e error
		// No current-period sentinel/entity: nil UUID disables the optional real
		// current-ID branch of the existing bounded weekly-input query.
		periods, e = ps.ReportInputPeriods(ctx, tx, window.Year, uuid.Nil, window.To, false)
		return e
	}); err != nil {
		return empty, err
	}
	sheets, err := s.submittedPeriods(ctx, c, periods)
	if err != nil {
		return empty, err
	}
	metrics := []domain.SourceMetric{}
	keys := []string{}
	for _, entry := range plan.Entries {
		if entry.DataSource == "module" && entry.SourceKey != "" {
			// Older accepted plans can retain invalid source keys. As with the
			// snapshot reader, they stay missing instead of blocking unrelated
			// status/manual items or inventing a numeric value.
			if _, err := sd.ParseSourceKey(entry.SourceKey); err == nil {
				keys = append(keys, entry.SourceKey)
			}
		}
	}
	if len(keys) > 0 {
		if s.Sources == nil {
			return empty, ErrDependency
		}
		read, err := s.Sources.ReadLive202(ctx, sa.LiveWindow{From: window.From, To: window.To, Cutoff: window.Cutoff, ConfirmationAt: window.ConfirmationAt}, keys)
		if err != nil {
			return empty, err
		}
		names := map[string]port.KHTCMetricName{}
		if reader, ok := s.Sources.Sources.(port.KHTCMetricNames); ok {
			names, err = reader.MetricNames(ctx, c.principal.TenantID, window.Cutoff)
			if err != nil {
				return empty, ErrDependency.WithCause(err)
			}
		}
		for raw, value := range read.Values {
			key, err := sd.ParseSourceKey(raw)
			if err != nil {
				return empty, ErrDependency.WithCause(err)
			}
			label, unit := metricLabel(key)
			if key.Mode == sd.ModePrice {
				if name, ok := names[key.Cell]; ok {
					label, unit = name.Label, name.Unit
				}
			}
			trace := []domain.SourceTrace{}
			for _, row := range read.Rows {
				if row.HasSource() && row.Department == key.Department && strings.EqualFold(row.FormCode, key.Form) {
					trace = append(trace, domain.SourceTrace{TargetKey: row.TargetKey, Module: row.SourceModule, Calendar: row.CalendarID, NativeKey: row.NativeKey, From: row.From, To: row.To, Closed: row.Closed, FiguresHash: row.FiguresHash})
				}
			}
			metrics = append(metrics, domain.SourceMetric{Trace: trace, Key: raw, Department: key.Department, Form: key.Form, Label: label, Unit: unit,
				Stock: strings.HasPrefix(key.Cell, "tong_dan_") || strings.Contains(key.Cell, "nha_yen"), Value: value})
		}
	}
	if err := boundedMetrics(metrics); err != nil {
		return empty, err
	}
	metrics = domain.WithYields(metrics)
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].Key < metrics[j].Key })
	data, err := domain.ComposeLiveM5(*window, plan, sheets, metrics, now)
	if err != nil {
		return empty, err
	}
	return domain.SummarizeLiveM5(*window, data, q.Department, q.Field, now)
}
