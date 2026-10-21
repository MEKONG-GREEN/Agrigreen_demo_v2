package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *SourceBuilderRegistry) jobPolicy(source string) (port.SourceJobPolicy, bool) {
	if r == nil {
		return port.SourceJobPolicy{}, false
	}
	p, ok := r.policies[source]
	return p, ok
}

// Owner callbacks may receive sensitive filters. Never return callback details.
func sourcePolicyError(err error, freezing bool) error {
	if err == nil {
		return nil
	}
	var p *problem.Problem
	if errors.As(err, &p) && p.Status < 500 {
		if freezing {
			return problem.Validation.WithField("source_args", "bộ lọc không hợp lệ")
		}
		return problem.Forbidden
	}
	return ErrDependency
}
func (s *Service) freezeSourceArgs(ctx context.Context, j *domain.Job, raw json.RawMessage, policy port.SourceJobPolicy) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	input, err := domain.CanonicalSourceArgs(raw)
	if err != nil {
		return problem.Validation.WithField("source_args", "bộ lọc không hợp lệ")
	}
	frozen, err := policy.Freeze(ctx, port.SourceFreezeRequest{
		Source: j.Def.Source, TemplateKey: j.TemplateKey, Format: j.Format, TenantID: j.TenantID,
		JobID: j.ID, RequestedBy: j.RequestedBy, RequestedAt: j.RequestedAt, SourceArgs: slices.Clone(input),
	})
	if err != nil {
		return sourcePolicyError(err, true)
	}
	canonical, err := domain.CanonicalSourceArgs(frozen)
	if err != nil {
		return ErrDependency
	}
	j.SourceArgs = &domain.FrozenSourceArgs{Source: j.Def.Source, Version: policy.Version,
		JobID: j.ID, TenantID: j.TenantID, RequestedBy: j.RequestedBy, RequestedAt: j.RequestedAt, Args: slices.Clone(canonical)}
	return nil
}
func (s *Service) authorizeSourceJob(ctx context.Context, j domain.Job, actor uuid.UUID, op port.SourceJobOperation) (bool, error) {
	policy, enabled := s.Builders.jobPolicy(j.Def.Source)
	if !enabled {
		if j.SourceArgs != nil {
			return true, ErrDependency
		}
		return false, nil
	}
	if j.SourceArgs == nil || j.SourceArgs.Version != policy.Version || j.ValidateSourceArgs() != nil ||
		j.Trigger != domain.TriggerAPI || j.ScheduleID != nil || j.ScheduleSlot != nil {
		return true, ErrDependency
	}
	if policy.ScopeMode == port.SourceScopeTenant && (j.ScopeAll || len(j.ScopeUnits) > 0) {
		return true, ErrDependency
	}
	tid, err := tenant.FromContext(ctx)
	if err != nil || tid != j.TenantID {
		return true, problem.Forbidden
	}
	if op != port.SourceJobBuild {
		p, err := authz.Require(ctx, s.readerRoles()...)
		if err != nil || actor == uuid.Nil || p.ActorRef != actor || p.TenantID != j.TenantID {
			return true, problem.Forbidden
		}
	}
	err = policy.AuthorizeJob(ctx, port.SourceJobAccess{
		Operation: op, Now: s.now(), ActorRef: actor, Source: j.Def.Source, TemplateKey: j.TemplateKey, Format: j.Format,
		TenantID: j.TenantID, JobID: j.ID, RequestedBy: j.RequestedBy, RequestedAt: j.RequestedAt,
		ArgsVersion: j.SourceArgs.Version, Scope: authz.Scope{All: j.ScopeAll, Units: slices.Clone(j.ScopeUnits)}, SourceArgs: slices.Clone(j.SourceArgs.Args),
	})
	return true, sourcePolicyError(err, false)
}
func (s *Service) sourceRequestJob(ctx context.Context, p authz.Principal, t domain.Template, in ExportInput, original *domain.Job) (domain.Job, error) {
	if !hasAny(p, s.sourceRoles(t.Def.Source)) {
		return domain.Job{}, problem.Forbidden
	}
	policy, enabled := s.Builders.jobPolicy(t.Def.Source)
	if !enabled && len(in.SourceArgs) > 0 {
		return domain.Job{}, problem.Validation.WithField("source_args", "nguồn này không nhận bộ lọc mở rộng")
	}
	now := s.now()
	id := ids.New()
	if enabled {
		now = now.Truncate(time.Microsecond)
	}
	if original != nil {
		id, now = original.ID, original.RequestedAt
	}
	var scope authz.Scope
	var err error
	raw := in.Params
	if !enabled || policy.ScopeMode == port.SourceScopeGeographic {
		scope, err = s.reach(ctx, p)
		if err != nil {
			return domain.Job{}, err
		}
		scope, err = s.jobScope(p, scope, t.Def, &raw)
		if err != nil {
			return domain.Job{}, err
		}
	}
	params, err := s.normalizeParams(ctx, t.Def, raw, scope, now)
	if err != nil {
		return domain.Job{}, err
	}
	j := domain.Job{ID: id, TenantID: p.TenantID, TemplateKey: t.Key, TemplateVersion: t.Version, Def: t.Def, Format: in.Format,
		Params: params, ScopeAll: scope.All, ScopeUnits: slices.Clone(scope.Units), RequestedBy: p.ActorRef, RequestedAt: now,
		Trigger: domain.TriggerAPI, IdemKey: in.IdemKey, Status: domain.JobQueued}
	if enabled {
		if p.TenantID == uuid.Nil || p.ActorRef == uuid.Nil {
			return domain.Job{}, problem.Forbidden
		}
		if err = s.freezeSourceArgs(ctx, &j, in.SourceArgs, policy); err != nil {
			return domain.Job{}, err
		}
		op := port.SourceJobRequest
		if original != nil {
			op = port.SourceJobReplay
		}
		if original == nil {
			if _, err = s.authorizeSourceJob(ctx, j, p.ActorRef, op); err != nil {
				return domain.Job{}, err
			}
		}
	}
	return j, nil
}
func (s *Service) checkSourceReplay(ctx context.Context, p authz.Principal, in ExportInput, prev domain.Job) error {
	_, enabled := s.Builders.jobPolicy(prev.Def.Source)
	if !enabled && prev.SourceArgs == nil {
		if len(in.SourceArgs) > 0 {
			return problem.Validation.WithField("source_args", "nguồn này không nhận bộ lọc mở rộng")
		}
		return nil // preserve native replay behavior
	}
	if prev.TenantID != p.TenantID || prev.RequestedBy != p.ActorRef || prev.TemplateKey != strings.TrimSpace(in.TemplateKey) || prev.Format != in.Format {
		return problem.IdempotencyConflict
	}
	if _, err := s.authorizeSourceJob(ctx, prev, p.ActorRef, port.SourceJobReplay); err != nil {
		return err
	}
	t := domain.Template{Key: prev.TemplateKey, Version: prev.TemplateVersion, Def: prev.Def, Status: domain.TemplateActive}
	candidate, err := s.sourceRequestJob(ctx, p, t, in, &prev)
	if err != nil {
		return err
	}
	if candidate.Params != prev.Params || candidate.ScopeAll != prev.ScopeAll || !slices.Equal(candidate.ScopeUnits, prev.ScopeUnits) ||
		candidate.SourceArgs == nil || prev.SourceArgs == nil || !bytes.Equal(candidate.SourceArgs.Args, prev.SourceArgs.Args) {
		return problem.IdempotencyConflict
	}
	return nil
}
func (s *Service) readSourceJob(ctx context.Context, p authz.Principal, j domain.Job, op port.SourceJobOperation) error {
	if j.TenantID != p.TenantID {
		return ErrExportNotFound
	}
	enabled, err := s.authorizeSourceJob(ctx, j, p.ActorRef, op)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	policy, _ := s.Builders.jobPolicy(j.Def.Source)
	if policy.ScopeMode == port.SourceScopeGeographic {
		sc, err := s.reach(ctx, p)
		if err != nil {
			return err
		}
		if !hasAny(p, s.sourceRoles(j.Def.Source)) || !scopeContains(sc, j) {
			return problem.Forbidden
		}
	}
	return nil
}
func scopeContains(sc authz.Scope, j domain.Job) bool {
	if sc.All {
		return true
	}
	if j.ScopeAll || len(j.ScopeUnits) == 0 {
		return false
	}
	for _, unit := range j.ScopeUnits {
		if !sc.Allows(unit) {
			return false
		}
	}
	return true
}

// CheckExportReplay runs before the common HTTP response cache. Otherwise a
// successful cached POST could outlive an owner's revocation without reaching
// RequestExport. It does not create a job or bypass the common idempotency lock.
func (s *Service) CheckExportReplay(ctx context.Context, in ExportInput) error {
	p, err := authz.Require(ctx, s.readerRoles()...)
	if err != nil {
		return err
	}
	if in.IdemKey == "" {
		return nil
	} // common middleware reports its standard key error
	return s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		prev, err := store.JobByIdemKey(ctx, tx, in.IdemKey)
		if err != nil || prev == nil {
			return err
		}
		if err = s.checkReplayTemplate(ctx, tx, in, *prev); err != nil {
			return err
		}
		return s.checkSourceReplay(ctx, p, in, *prev)
	})
}
func (s *Service) HasSourceJobPolicies() bool {
	return s.Builders != nil && len(s.Builders.policies) > 0
}
func (s *Service) checkReplayTemplate(ctx context.Context, tx pgx.Tx, in ExportInput, prev domain.Job) error {
	// Include policy inputs with an old native key even when source_args is omitted.
	t, err := s.resolveTemplate(ctx, tx, strings.TrimSpace(in.TemplateKey))
	if err != nil {
		if prev.SourceArgs != nil {
			return err
		}
		return nil
	}
	_, enabled := s.Builders.jobPolicy(t.Def.Source)
	if enabled && (prev.Def.Source != t.Def.Source || prev.RequestedBy == uuid.Nil) {
		return problem.IdempotencyConflict
	}
	if prev.SourceArgs != nil && t.Status != domain.TemplateActive {
		return ErrTemplateRetired
	}
	return nil
}
