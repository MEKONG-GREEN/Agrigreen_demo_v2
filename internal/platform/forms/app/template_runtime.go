package app

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func validTemplateBinding(binding string) bool {
	return utf8.ValidString(binding) && strings.TrimSpace(binding) != "" && len(binding) <= 512 && !strings.ContainsRune(binding, 0)
}
func templateAuthority(p authz.Principal, a actor, binding string) (string, error) {
	roles := slices.Clone(p.Roles)
	slices.Sort(roles)
	units := slices.Clone(a.scope.Units)
	slices.Sort(units)
	var sessionProfile string
	var sessionID uuid.UUID
	var sessionScope authz.SessionScope
	var sessionPayload []byte
	if p.SelectedSession != nil {
		sessionProfile = p.SelectedSession.Profile()
		sessionID = p.SelectedSession.ID()
		sessionScope = p.SelectedSession.Scope()
		slices.Sort(sessionScope.UnitCodes)
		sessionPayload = p.SelectedSession.Payload()
	}
	return port.OperationHash(struct {
		Actor, Tenant, Org, Node, Session         uuid.UUID
		Roles, Units                              []string
		Role, Department, SessionProfile, Binding string
		All, MFA                                  bool
		AuthTime                                  string
		SessionScope                              authz.SessionScope
		SessionPayload                            []byte
	}{a.ref, a.tenant, p.OrgRef, a.node, sessionID, roles, units, a.role, a.dept, sessionProfile, binding, a.scope.All, p.MFA, p.AuthTime.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), sessionScope, sessionPayload})
}

var _ port.TemplateCopies = Publisher{}
var _ port.TemplateUsages = Publisher{}

func (s *Service) copyInput(ctx context.Context, in port.TemplateCopy) (TemplateInput, error) {
	source, err := s.Template(ctx, in.Source.Profile, in.Source.ID)
	if err != nil {
		return TemplateInput{}, err
	}
	if source.Identity() != in.Source {
		return TemplateInput{}, problem.Conflict.WithField("source", "nguồn sao chép không khớp phiên bản và digest thật")
	}
	version, err := catalogVersion(source)
	if err != nil {
		return TemplateInput{}, err
	}
	return TemplateInput{Profile: source.Profile, Code: in.Code, Title: in.Title, Fields: source.Fields, Runtime: source.Runtime, Fixture: source.Fixture, BaseVersion: &in.BaseVersion, copySource: &version}, nil
}
func (s *Service) AuthorizeTemplateCopy(ctx context.Context, in port.TemplateCopy) error {
	proposed, err := s.copyInput(ctx, in)
	if err != nil {
		return err
	}
	return s.AuthorizeTemplateDefinition(ctx, proposed, proposed.BaseVersion)
}
func (s *Service) CopyTemplate(ctx context.Context, in port.TemplateCopy, idem string) (store.Template, error) {
	proposed, err := s.copyInput(ctx, in)
	if err != nil {
		return store.Template{}, err
	}
	return s.DefineTemplate(ctx, proposed, idem)
}
func (pb Publisher) CopyTemplate(ctx context.Context, in port.TemplateCopy, idem string) (port.TemplateVersion, error) {
	saved, err := pb.Svc.CopyTemplate(ctx, in, idem)
	if err != nil {
		return port.TemplateVersion{}, err
	}
	return catalogVersion(saved)
}
func (pb Publisher) TemplateUsage(ctx context.Context, profile string, id uuid.UUID) (port.TemplateUsage, error) {
	return pb.Svc.TemplateUsage(ctx, profile, id)
}
func (s *Service) TemplateUsage(ctx context.Context, profile string, id uuid.UUID) (port.TemplateUsage, error) {
	t, err := s.Template(ctx, profile, id)
	if err != nil {
		return port.TemplateUsage{}, err
	}
	var out port.TemplateUsage
	err = s.tx(ctx, t.TenantID, func(tx pgx.Tx) error { var e error; out, e = store.TemplateUsage(ctx, tx, t); return e })
	return out, err
}

var templateUseKey = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,200}$`)
var templateRuntimeContract = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}/v[1-9][0-9]{0,3}$`)

func (pb Publisher) RecordTemplateUse(ctx context.Context, in port.TemplateUseRequest, idem string) (port.TemplateUseReceipt, error) {
	s := pb.Svc
	if _, joined := db.JoinedTx(ctx); joined {
		return port.TemplateUseReceipt{}, problem.Conflict.WithDetail("ghi nhận runtime cần giao dịch Forms riêng")
	}
	principal, ok := authz.FromContext(ctx)
	if !ok {
		return port.TemplateUseReceipt{}, problem.Unauthenticated
	}
	if !templateUseKey.MatchString(idem) {
		return port.TemplateUseReceipt{}, problem.IdempotencyRequired
	}
	if in.Reference == uuid.Nil || !templateRuntimeContract.MatchString(in.Runtime) || (in.Kind != port.TemplateUseRuntime && in.Kind != port.TemplateUseData) {
		return port.TemplateUseReceipt{}, problem.Validation.WithField("runtime", "hợp đồng và tham chiếu runtime không hợp lệ")
	}
	t, err := s.Template(ctx, in.Identity.Profile, in.Identity.ID)
	if err != nil {
		return port.TemplateUseReceipt{}, err
	}
	if in.Identity != t.Identity() {
		return port.TemplateUseReceipt{}, problem.Conflict.WithField("identity", "identity runtime không khớp phiên bản thật")
	}
	prof, a, err := s.resolve(ctx, t.Profile)
	if err != nil {
		return port.TemplateUseReceipt{}, err
	}
	policy := s.TemplateUsePolicies[t.Profile]
	if policy == nil {
		return port.TemplateUseReceipt{}, problem.Forbidden.WithDetail("owner chưa đăng ký quyền ghi nhận runtime")
	}
	var subject port.ResolvedActor
	if prof.ResolveActor != nil {
		resolved, handled, e := prof.ResolveActor(ctx, principal, prof)
		if e != nil {
			return port.TemplateUseReceipt{}, e
		}
		if handled {
			subject = resolved
		}
	}
	version, err := catalogVersion(t)
	if err != nil {
		return port.TemplateUseReceipt{}, err
	}
	// The policy receives independently allocated raw JSON, never the data that the TX writes.
	fields, err := json.Marshal(t.Fields)
	if err != nil {
		return port.TemplateUseReceipt{}, err
	}
	version.Fields = fields
	version.Runtime, err = port.NormalizeRuntime(t.Runtime)
	if err != nil {
		return port.TemplateUseReceipt{}, err
	}
	grant, err := policy(ctx, port.TemplateUsePolicyRequest{Principal: principal, Actor: subject, Template: version, Request: in})
	if err != nil {
		return port.TemplateUseReceipt{}, err
	}
	if !grant.Allowed || !validTemplateBinding(grant.Binding) || (in.Kind == port.TemplateUseData && !grant.HasData) {
		return port.TemplateUseReceipt{}, problem.Forbidden
	}
	authority, err := templateAuthority(principal, a, grant.Binding)
	if err != nil {
		return port.TemplateUseReceipt{}, err
	}
	hash, err := port.OperationHash(in)
	if err != nil {
		return port.TemplateUseReceipt{}, err
	}
	key := idem + ":template-use"
	receipt := port.TemplateUseReceipt{ID: ids.New(), Request: in, ActorRef: a.ref, HasData: grant.HasData}
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		if err := store.LockTemplateDefinition(ctx, tx, a.tenant, t.Profile, t.Code, key); err != nil {
			return err
		}
		prior, err := store.TemplateUseByIdem(ctx, tx, key)
		if err != nil {
			return err
		}
		if prior != nil {
			if prior.RequestHash != hash || prior.AuthorityHash != authority || prior.Receipt.ActorRef != a.ref {
				return problem.IdempotencyConflict
			}
			receipt = prior.Receipt
			return nil
		}
		return store.InsertTemplateUse(ctx, tx, t, &receipt, hash, authority, key, s.Clock.Now().UTC())
	})
	if err != nil {
		return port.TemplateUseReceipt{}, classify(err)
	}
	return receipt, nil
}
