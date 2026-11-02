package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type definitionAuthorization struct {
	actor          actor
	currentVersion int64
	currentID      uuid.UUID
}

// AuthorizeTemplateDefinition is also used before the HTTP idempotency cache.
// It deliberately does not enforce CAS: a successful retry still rechecks current
// permission, but may replay its original version after later versions exist.
func (s *Service) AuthorizeTemplateDefinition(ctx context.Context, in TemplateInput, base *int64) error {
	_, err := s.authorizeTemplateDefinition(ctx, in, base)
	return err
}

func (s *Service) authorizeTemplateDefinition(ctx context.Context, in TemplateInput, base *int64) (definitionAuthorization, error) {
	principal, ok := authz.FromContext(ctx)
	if !ok {
		return definitionAuthorization{}, problem.Unauthenticated
	}
	if base != nil && *base < 0 {
		return definitionAuthorization{}, problem.Validation.WithField("base_version", "phiên bản gốc không được âm")
	}
	prof, ok := s.profiles[in.Profile]
	if !ok {
		return definitionAuthorization{}, port.ErrProfileNotFound
	}
	policy, registered := s.DefinitionPolicies[in.Profile]
	if !registered && !s.RequiredDefinitionPolicies[in.Profile] {
		_, a, err := s.resolve(ctx, in.Profile)
		if err != nil {
			return definitionAuthorization{}, err
		}
		if !a.define || !a.allowsTemplate(in.Code) {
			return definitionAuthorization{}, problem.Forbidden
		}
		return definitionAuthorization{actor: a}, nil
	}
	if policy == nil {
		return definitionAuthorization{}, problem.Forbidden.WithDetail("chưa đăng ký chính sách định nghĩa biểu")
	}
	subject := port.ResolvedActor{}
	if prof.ResolveActor != nil {
		resolved, handled, err := prof.ResolveActor(ctx, principal, prof)
		if err != nil {
			return definitionAuthorization{}, err
		}
		if handled {
			subject = resolved
		}
	}
	a := actor{ref: principal.ActorRef, tenant: principal.TenantID, role: subject.Role, dept: subject.Department, scope: subject.Scope, node: subject.Node}
	var current *store.Template
	err := s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		var e error
		current, e = store.LatestTemplateByCode(ctx, tx, in.Profile, in.Code)
		return e
	})
	if err != nil {
		return definitionAuthorization{}, err
	}
	fields, err := json.Marshal(in.Fields)
	if err != nil {
		return definitionAuthorization{}, err
	}
	req := port.TemplateDefinitionRequest{Principal: principal, Actor: subject,
		Proposed: port.TemplateDefinition{Profile: in.Profile, Code: in.Code, Title: strings.TrimSpace(in.Title), Fields: fields, Fixture: in.Fixture}}
	// A policy cannot mutate the caller's optimistic version pointer.
	if base != nil {
		value := *base
		req.BaseVersion = &value
	}
	approved := definitionAuthorization{actor: a}
	if current != nil {
		previous, e := json.Marshal(current.Fields)
		if e != nil {
			return definitionAuthorization{}, e
		}
		req.Current = &port.TemplateDefinition{Profile: current.Profile, Code: current.Code, Title: current.Title, Fields: previous, Fixture: current.Fixture}
		req.CurrentID, req.CurrentVersion = current.ID, current.Version
		approved.currentID, approved.currentVersion = current.ID, current.Version
	}
	decision, err := policy(ctx, req)
	if err != nil {
		return definitionAuthorization{}, err
	}
	if !decision.Allowed {
		return definitionAuthorization{}, problem.Forbidden
	}
	if base == nil || *base < 0 {
		return definitionAuthorization{}, problem.Validation.WithField("base_version", "bắt buộc phiên bản gốc từ 0 trở lên")
	}
	return approved, nil
}

// defineTemplateVersion is the single write implementation for REST and Publisher.
// Owner I/O finishes before the write transaction; only immutable version checks,
// durable replay lookup and the append run while locks are held.
func (s *Service) defineTemplateVersion(ctx context.Context, in TemplateInput, base *int64, idem string) (store.Template, error) {
	if _, ok := authz.FromContext(ctx); !ok {
		return store.Template{}, problem.Unauthenticated
	}
	if base != nil {
		version := *base
		base = &version
	}
	in.Title = strings.TrimSpace(in.Title)
	if err := domain.ValidateTemplate(in.Code, in.Title, in.Fields); err != nil {
		var te domain.ErrTemplate
		if errors.As(err, &te) {
			return store.Template{}, port.ErrInvalidTemplate.WithField(te.Field, te.Msg)
		}
		return store.Template{}, port.ErrInvalidTemplate.WithDetail(err.Error())
	}
	if base != nil && *base < 0 {
		return store.Template{}, problem.Validation.WithField("base_version", "phiên bản gốc không được âm")
	}
	approved, err := s.authorizeTemplateDefinition(ctx, in, base)
	if err != nil {
		return store.Template{}, err
	}
	a := approved.actor
	key := idemOr(idem, "template:"+in.Code)
	result := store.Template{ID: ids.New(), Profile: in.Profile, Code: in.Code, Title: in.Title, Fields: in.Fields, Fixture: in.Fixture, ActorRef: a.ref, EventTime: s.Clock.Now().UTC()}
	_, registered := s.DefinitionPolicies[in.Profile]
	required := registered || s.RequiredDefinitionPolicies[in.Profile]
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		if err := store.LockTemplateDefinition(ctx, tx, a.tenant, in.Profile, in.Code, key); err != nil {
			return err
		}
		replay, err := store.TemplateByIdem(ctx, tx, key)
		if err != nil {
			return err
		}
		if replay != nil {
			if !sameTemplateDefinition(*replay, result) || (base != nil && *base != replay.Version-1) {
				return problem.IdempotencyConflict
			}
			result = *replay
			return nil
		}
		current, err := store.LatestTemplateByCode(ctx, tx, in.Profile, in.Code)
		if err != nil {
			return err
		}
		var version int64
		var currentID uuid.UUID
		if current != nil {
			version, currentID = current.Version, current.ID
		}
		if (base != nil && *base != version) || (required && (approved.currentVersion != version || approved.currentID != currentID)) {
			return problem.Conflict.WithField("base_version", fmt.Sprintf("phiên bản hiện hành là %d; đọc lại trước khi lưu", version))
		}
		return store.InsertTemplate(ctx, tx, a.tenant, &result, key)
	})
	return result, classify(err)
}

func sameTemplateDefinition(a, b store.Template) bool {
	if a.Profile != b.Profile || a.Code != b.Code || a.Title != b.Title || a.Fixture != b.Fixture || a.ActorRef != b.ActorRef {
		return false
	}
	af, ae := json.Marshal(a.Fields)
	bf, be := json.Marshal(b.Fields)
	return ae == nil && be == nil && bytes.Equal(af, bf)
}
