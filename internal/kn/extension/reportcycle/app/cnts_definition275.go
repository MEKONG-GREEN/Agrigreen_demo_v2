package app

import (
	"context"
	"encoding/json"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) definition275Candidate(ctx context.Context, kind, code string, input any, key string) (store.CNTSDefinitionIntent, error) {
	empty := store.CNTSDefinitionIntent{}
	if err := s.AuthorizeCNTSDesignWrite(ctx, code); err != nil {
		return empty, err
	}
	if len(key) < 8 || len(key) > 200 {
		return empty, problem.IdempotencyRequired
	}
	p, k, err := port.RequireSession(ctx)
	if err != nil {
		return empty, err
	}
	request := struct {
		Kind, Code string
		Input      any
	}{kind, code, input}
	hash, err := formsport.OperationHash(request)
	if err != nil {
		return empty, err
	}
	authority, err := formsport.OperationHash(struct {
		Session port.SessionContext
		MFA     bool
		Org     uuid.UUID
	}{k, p.MFA, p.OrgRef})
	if err != nil {
		return empty, err
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return empty, err
	}
	return store.CNTSDefinitionIntent{Key: key, ActorRef: p.ActorRef, SessionRef: k.ID, AuthorityHash: authority, RequestHash: hash, Request: raw, EventTime: s.now()}, nil
}

func sameCNTSDefinition(a, b store.CNTSDefinitionIntent) bool {
	return a.Key == b.Key && a.ActorRef == b.ActorRef && a.SessionRef == b.SessionRef && a.AuthorityHash == b.AuthorityHash && a.RequestHash == b.RequestHash
}

// CheckCNTSDefinition rechecks the current selected session before HTTP replay.
// It is read-only: invalid or denied requests cannot create a request fence.
func (s *Service) CheckCNTSDefinition(ctx context.Context, kind, code string, input any, key string) error {
	candidate, err := s.definition275Candidate(ctx, kind, code, input, key)
	if err != nil {
		return err
	}
	p, _, err := port.RequireSession(ctx)
	if err != nil {
		return err
	}
	return s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		existing, err := store.CNTSDefinitionByKey(ctx, tx, key)
		if err != nil {
			return err
		}
		if existing != nil && !sameCNTSDefinition(*existing, candidate) {
			return problem.IdempotencyConflict
		}
		return nil
	})
}

// fenceCNTSDefinition runs after schema/base validation, before external native
// I/O. The immutable command is reused after lost ACK; Native DefineTemplate
// provides its original canonical receipt and atomic definition audit.
func (s *Service) fenceCNTSDefinition(ctx context.Context, kind, code string, input any, def formsport.TemplateDef, key string) (formsport.TemplateDef, error) {
	candidate, err := s.definition275Candidate(ctx, kind, code, input, key)
	if err != nil {
		return def, err
	}
	if candidate.Command, err = json.Marshal(def); err != nil {
		return def, err
	}
	candidate.ID = ids.New()
	p, _, err := port.RequireSession(ctx)
	if err != nil {
		return def, err
	}
	var stored *store.CNTSDefinitionIntent
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockCNTSDefinition(ctx, tx, p.TenantID, key); err != nil {
			return err
		}
		var err error
		stored, err = store.CNTSDefinitionByKey(ctx, tx, key)
		if err != nil {
			return err
		}
		if stored != nil {
			if !sameCNTSDefinition(*stored, candidate) {
				return problem.IdempotencyConflict
			}
			return nil
		}
		stored = &candidate
		return store.InsertCNTSDefinition(ctx, tx, p.TenantID, candidate)
	})
	if err != nil {
		return def, err
	}
	var frozen formsport.TemplateDef
	if err := json.Unmarshal(stored.Command, &frozen); err != nil {
		return formsport.TemplateDef{}, ErrDependency.WithCause(err)
	}
	return frozen, nil
}
