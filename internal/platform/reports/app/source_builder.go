package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"slices"
)

// SourceBuilderRegistry is assembled once; a registered error never falls back.
type SourceBuilderRegistry struct {
	builders map[string]port.SourceBuilder
	policies map[string]port.SourceJobPolicy
}

func NewSourceBuilderRegistry(registrations ...port.SourceBuilderRegistration) *SourceBuilderRegistry {
	r := &SourceBuilderRegistry{builders: map[string]port.SourceBuilder{}, policies: map[string]port.SourceJobPolicy{}}
	for _, registration := range registrations {
		if registration.Source == "" || registration.Build == nil {
			panic("reports: invalid source builder registration")
		}
		if _, exists := r.builders[registration.Source]; exists {
			panic("reports: duplicate source builder registration: " + registration.Source)
		}
		if policy := registration.JobPolicy; policy != nil {
			if policy.Version < 1 || policy.Freeze == nil || policy.AuthorizeJob == nil || (policy.ScopeMode != port.SourceScopeGeographic && policy.ScopeMode != port.SourceScopeTenant) {
				panic("reports: incomplete source job policy")
			}
			r.policies[registration.Source] = *policy
		}
		r.builders[registration.Source] = registration.Build
	}
	return r
}
func (r *SourceBuilderRegistry) lookup(source string) (port.SourceBuilder, bool) {
	if r == nil {
		return nil, false
	}
	b, ok := r.builders[source]
	return b, ok
}
func sourceBuildQuery(j domain.Job, scope authz.Scope) (port.SourceBuildQuery, error) {
	q := port.SourceBuildQuery{Source: j.Def.Source, JobID: j.ID, RequestedBy: j.RequestedBy, TenantID: j.TenantID, Scope: authz.Scope{All: scope.All, Units: slices.Clone(scope.Units)}, At: j.RequestedAt, Unit: j.Params.UnitCode, Level: j.Params.Level, SeasonCode: j.Params.SeasonCode, SeasonYear: j.Params.SeasonYear}
	if err := j.ValidateSourceArgs(); err != nil {
		return q, ErrDependency
	}
	if j.SourceArgs != nil {
		q.ArgsVersion = j.SourceArgs.Version
		q.SourceArgs = slices.Clone(j.SourceArgs.Args)
	}
	for _, column := range j.Def.Columns {
		q.Columns = append(q.Columns, port.SourceBuildColumn{Key: column.Key})
	}
	if j.Params.Period != "" {
		period, err := domain.ParsePeriod(j.Params.Period)
		if err != nil {
			return q, err
		}
		q.Periods = slices.Clone(period.Months)
		q.PeriodLabel = period.Label
	}
	return q, nil
}
func sourceBuilt(data port.SourceData) built {
	row := func(in port.SourceRow) domain.Row {
		if in == nil {
			return nil
		}
		out := make(domain.Row, len(in))
		for key, cell := range in {
			out[key] = domain.Value{Text: cell.Text}
			if cell.Dec != nil {
				value := out[key]
				n := *cell.Dec
				value.Dec = &n
				out[key] = value
			}
		}
		return out
	}
	b := built{total: row(data.Total), provisional: data.Provisional, notes: slices.Clone(data.Notes)}
	for _, r := range data.Rows {
		b.rows = append(b.rows, row(r))
	}
	for _, m := range data.Meta {
		b.meta = append(b.meta, domain.Meta{Label: m.Label, Value: m.Value})
	}
	return b
}
func (s *Service) buildRegisteredSource(ctx context.Context, j domain.Job, sc authz.Scope, builder port.SourceBuilder) (built, error) {
	query, err := sourceBuildQuery(j, sc)
	if err != nil {
		return built{}, err
	}
	data, err := builder(ctx, query)
	if err != nil {
		return built{}, err
	}
	return sourceBuilt(data), nil
}
