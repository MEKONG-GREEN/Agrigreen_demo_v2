package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type definitionAuthorization struct {
	actor          actor
	currentVersion int64
	currentID      uuid.UUID
	authority      string
}

// AuthorizeTemplateDefinition also runs before HTTP replay. Permission is fresh;
// CAS remains in the write TX so a legitimate retry can return its original row.
func (s *Service) AuthorizeTemplateDefinition(ctx context.Context, in TemplateInput, base *int64) error {
	detached, err := normalizeTemplateInput(in)
	if err != nil {
		return err
	}
	_, err = s.authorizeTemplateDefinition(ctx, detached, base)
	return err
}
func normalizeTemplateInput(in TemplateInput) (TemplateInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	raw, err := json.Marshal(in.Fields)
	if err != nil {
		return in, err
	}
	if err = json.Unmarshal(raw, &in.Fields); err != nil {
		return in, err
	}
	in.Runtime, err = port.NormalizeRuntime(in.Runtime)
	return in, err
}
func (s *Service) authorizeTemplateDefinition(ctx context.Context, in TemplateInput, base *int64) (definitionAuthorization, error) {
	principal, ok := authz.FromContext(ctx)
	if !ok {
		return definitionAuthorization{}, problem.Unauthenticated
	}
	if _, joined := db.JoinedTx(ctx); joined {
		return definitionAuthorization{}, problem.Conflict.WithDetail("định nghĩa biểu cần giao dịch Forms riêng")
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
		if in.Runtime != nil {
			return definitionAuthorization{}, port.ErrInvalidTemplate.WithField("runtime", "owner chưa đăng ký chính sách xác thực cấu hình runtime")
		}
		_, a, err := s.resolve(ctx, in.Profile)
		if err != nil {
			return definitionAuthorization{}, err
		}
		if !a.define || !a.allowsTemplate(in.Code) {
			return definitionAuthorization{}, problem.Forbidden
		}
		authority, err := templateAuthority(principal, a, "")
		return definitionAuthorization{actor: a, authority: authority}, err
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
	runtime, err := port.NormalizeRuntime(in.Runtime)
	if err != nil {
		return definitionAuthorization{}, err
	}
	req := port.TemplateDefinitionRequest{Principal: principal, Actor: subject, Proposed: port.TemplateDefinition{Profile: in.Profile, Code: in.Code, Title: in.Title, Fields: fields, Fixture: in.Fixture, Runtime: runtime}}
	if in.copySource != nil {
		source := *in.copySource
		source.Fields = bytes.Clone(source.Fields)
		source.Runtime, err = port.NormalizeRuntime(source.Runtime)
		if err != nil {
			return definitionAuthorization{}, err
		}
		req.CopySource = &source
	}
	if base != nil {
		v := *base
		req.BaseVersion = &v
	}
	approved := definitionAuthorization{actor: a}
	if current != nil {
		previous, e := json.Marshal(current.Fields)
		if e != nil {
			return definitionAuthorization{}, e
		}
		oldRuntime, e := port.NormalizeRuntime(current.Runtime)
		if e != nil {
			return definitionAuthorization{}, e
		}
		req.Current = &port.TemplateDefinition{Profile: current.Profile, Code: current.Code, Title: current.Title, Fields: previous, Fixture: current.Fixture, Runtime: oldRuntime}
		req.CurrentID, req.CurrentVersion = current.ID, current.Version
		approved.currentID, approved.currentVersion = current.ID, current.Version
		if current.Runtime != nil && in.Runtime == nil {
			return definitionAuthorization{}, port.ErrInvalidTemplate.WithField("runtime", "không được bỏ cấu hình runtime đã lưu")
		}
	}
	decision, err := policy(ctx, req)
	if err != nil {
		return definitionAuthorization{}, err
	}
	if !decision.Allowed {
		return definitionAuthorization{}, problem.Forbidden
	}
	if in.Runtime != nil && (!decision.RuntimeValidated || !validTemplateBinding(decision.Binding)) {
		return definitionAuthorization{}, port.ErrInvalidTemplate.WithField("runtime", "owner chưa xác thực hợp đồng hoặc cấu hình runtime")
	}
	if base == nil {
		return definitionAuthorization{}, problem.Validation.WithField("base_version", "bắt buộc phiên bản gốc từ 0 trở lên")
	}
	approved.authority, err = templateAuthority(principal, a, decision.Binding)
	return approved, err
}

type definitionIntent struct {
	Profile     string                  `json:"profile"`
	Code        string                  `json:"code"`
	Title       string                  `json:"title"`
	Fields      []domain.Field          `json:"fields"`
	Fixture     bool                    `json:"fixture"`
	Runtime     *port.RuntimeDefinition `json:"runtime"`
	BaseVersion *int64                  `json:"base_version"`
	CopySource  *port.TemplateIdentity  `json:"copy_source"`
}

// Both REST and Publisher use the canonical native row/key/stream lock. Only
// metadata missing from the old ledger lives in its immutable one-row extension.
func (s *Service) defineTemplateVersion(ctx context.Context, in TemplateInput, base *int64, idem string) (store.Template, error) {
	if _, ok := authz.FromContext(ctx); !ok {
		return store.Template{}, problem.Unauthenticated
	}
	if base != nil {
		v := *base
		base = &v
	}
	var err error
	in, err = normalizeTemplateInput(in)
	if err != nil {
		return store.Template{}, err
	}
	if err = domain.ValidateTemplate(in.Code, in.Title, in.Fields); err != nil {
		var te domain.ErrTemplate
		if errors.As(err, &te) {
			return store.Template{}, port.ErrInvalidTemplate.WithField(te.Field, te.Msg)
		}
		return store.Template{}, port.ErrInvalidTemplate.WithDetail(err.Error())
	}
	approved, err := s.authorizeTemplateDefinition(ctx, in, base)
	if err != nil {
		return store.Template{}, err
	}
	intent := definitionIntent{Profile: in.Profile, Code: in.Code, Title: in.Title, Fields: in.Fields, Fixture: in.Fixture, Runtime: in.Runtime, BaseVersion: base}
	if in.copySource != nil {
		identity := in.copySource.Identity
		intent.CopySource = &identity
	}
	raw, err := json.Marshal(intent)
	if err != nil {
		return store.Template{}, err
	}
	hash, err := port.OperationHash(intent)
	if err != nil {
		return store.Template{}, err
	}
	a := approved.actor
	var intentKey *string
	if in.Runtime != nil || in.copySource != nil {
		if !templateUseKey.MatchString(idem) {
			return store.Template{}, problem.IdempotencyRequired
		}
		intentKey = &idem
	}
	key := idemOr(idem, "template:"+in.Code)
	result := store.Template{ID: ids.New(), TenantID: a.tenant, Profile: in.Profile, Code: in.Code, Title: in.Title, Fields: in.Fields, Fixture: in.Fixture, Runtime: in.Runtime, ActorRef: a.ref, EventTime: s.Clock.Now().UTC()}
	_, registered := s.DefinitionPolicies[in.Profile]
	required := registered || s.RequiredDefinitionPolicies[in.Profile]
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		if intentKey != nil {
			if err := store.LockDefinitionIntent(ctx, tx, a.tenant, *intentKey); err != nil {
				return err
			}
		}
		if err := store.LockTemplateDefinition(ctx, tx, a.tenant, in.Profile, in.Code, key); err != nil {
			return err
		}
		replay, err := store.TemplateByIdem(ctx, tx, key)
		if err != nil {
			return err
		}
		if intentKey != nil {
			byIntent, e := store.DefinitionByIntent(ctx, tx, *intentKey)
			if e != nil {
				return e
			}
			if byIntent != nil {
				if replay != nil && replay.ID != byIntent.ID {
					return problem.IdempotencyConflict
				}
				replay = byIntent
			} else {
				historical, e := store.HasLegacyDefinitionIntent(ctx, tx, *intentKey)
				if e != nil {
					return e
				}
				if historical {
					return problem.IdempotencyConflict
				}
			}
		}
		if replay != nil {
			receipt, err := store.SavedDefinition(ctx, tx, replay.ID)
			if err != nil {
				return err
			}
			if receipt != nil {
				if receipt.IntentHash != hash || receipt.AuthorityHash != approved.authority || replay.ActorRef != a.ref {
					return problem.IdempotencyConflict
				}
			} else if in.copySource != nil || in.Runtime != nil || !sameTemplateDefinition(*replay, result) || (base != nil && *base != replay.Version-1) {
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
		if err = store.InsertTemplate(ctx, tx, a.tenant, &result, key); err != nil {
			return err
		}
		if err = store.InsertDefinition(ctx, tx, result, version, raw, hash, approved.authority, intentKey); err != nil {
			return err
		}
		saved, err := store.TemplateByID(ctx, tx, result.ID)
		if err != nil {
			return err
		}
		if saved == nil {
			return port.ErrTemplateNotFound
		}
		result = *saved
		return nil
	})
	if err != nil {
		return store.Template{}, classify(err)
	}
	return result, nil
}
func sameTemplateDefinition(a, b store.Template) bool {
	if a.Profile != b.Profile || a.Code != b.Code || a.Title != b.Title || a.Fixture != b.Fixture || a.ActorRef != b.ActorRef {
		return false
	}
	af, ae := json.Marshal(a.Fields)
	bf, be := json.Marshal(b.Fields)
	ar, re := json.Marshal(a.Runtime)
	br, se := json.Marshal(b.Runtime)
	return ae == nil && be == nil && re == nil && se == nil && bytes.Equal(af, bf) && bytes.Equal(ar, br)
}
