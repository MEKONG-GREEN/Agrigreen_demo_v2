package app

import (
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Hash canonical server state. Session/expiry/support-request reason are lease
// attributes; role, all scopes, assignments and effective powers remain pinned.
func openContextHash(c caller) string {
	k := c.k
	k.ID = uuid.Nil
	k.ExpiresAt = time.Time{}
	k.DefaultRoute = ""
	if k.Support != nil {
		v := *k.Support
		v.SessionID = uuid.Nil
		v.Reason = ""
		k.Support = &v
	}
	sortStrings := func(x []string) []string { v := append([]string{}, x...); slices.Sort(v); return slices.Compact(v) }
	k.Scope.Units = sortStrings(k.Scope.Units)
	k.Communes = sortStrings(k.Communes)
	k.Powers = sortStrings(k.Powers)
	k.Duties = sortStrings(k.Duties)
	sortIDs := func(x []uuid.UUID) []uuid.UUID {
		v := append([]uuid.UUID{}, x...)
		slices.SortFunc(v, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		return slices.Compact(v)
	}
	k.Facilities = sortIDs(k.Facilities)
	k.Households = sortIDs(k.Households)
	h, _ := forms.OperationHash(k)
	return h
}

type AdhocOpenRecovery struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	From             time.Time `json:"from"`
	To               time.Time `json:"to"`
	DueAt            time.Time `json:"due_at"`
	Departments      []string  `json:"departments"`
	Generation       int64     `json:"generation"`
	RequiresAdoption bool      `json:"requires_adoption"`
	CanAdopt         bool      `json:"can_adopt"`
	LeaseExpiresAt   time.Time `json:"lease_expires_at"`
	CreatedAt        time.Time `json:"created_at"`
}

func (s *Service) recoveryWriter(ctx context.Context) (caller, error) {
	c, e := s.openingWriter(ctx, OpenInput{Request: domain.Request{Type: domain.TypeAdhoc}})
	if e == nil && !c.k.ExpiresAt.IsZero() && !c.k.ExpiresAt.After(s.now()) {
		return c, problem.Forbidden.WithDetail("Ngữ cảnh phiên đã hết hạn")
	}
	return c, e
}
func (s *Service) AdhocOpenRecoveries(ctx context.Context) ([]AdhocOpenRecovery, error) {
	c, err := s.recoveryWriter(ctx)
	if err != nil {
		return nil, err
	}
	var xs []store.AdhocOpenIntent
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		xs, e = store.IncompleteAdhocOpens(ctx, tx, c.p.ActorRef, openContextHash(c))
		return e
	})
	if err != nil {
		return nil, err
	}
	out := []AdhocOpenRecovery{}
	for _, x := range xs {
		if !validOpenIntent(x) {
			return nil, ErrDependency
		}
		adopt := x.EffectiveSessionID != c.k.ID
		out = append(out, AdhocOpenRecovery{ID: x.ID, Name: x.Plan.Name, From: x.Plan.From, To: x.Plan.To, DueAt: x.Plan.DueAt, Departments: x.Departments, Generation: x.LeaseGeneration, RequiresAdoption: adopt, CanAdopt: adopt && !x.EffectiveExpiresAt.IsZero() && !x.EffectiveExpiresAt.After(s.now()) && !c.k.ExpiresAt.IsZero(), LeaseExpiresAt: x.EffectiveExpiresAt, CreatedAt: x.At})
	}
	return out, nil
}

type AdhocOpenReconcileInput struct {
	ExpectedGeneration int64  `json:"expected_generation"`
	Reason             string `json:"reason"`
}

func (s *Service) ReconcileAdhocOpen(ctx context.Context, id uuid.UUID, in AdhocOpenReconcileInput) (Detail, error) {
	c, err := s.recoveryWriter(ctx)
	if err != nil {
		return Detail{}, err
	}
	if in.ExpectedGeneration < 0 || len(in.Reason) > 2000 {
		return Detail{}, problem.Validation
	}
	reason := strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(reason) > 500 {
		return Detail{}, problem.Validation
	}
	var x *store.AdhocOpenIntent
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		x, e = store.AdhocOpenByID(ctx, tx, id)
		if e != nil {
			return e
		}
		if x == nil || x.ActorRef != c.p.ActorRef || x.ContextHash != openContextHash(c) {
			return problem.Forbidden
		}
		if e = store.LockAdhocOpen(ctx, tx, c.p.TenantID, x.NativeKey); e != nil {
			return e
		}
		x, e = store.AdhocOpenByID(ctx, tx, id)
		if e != nil {
			return e
		}
		if x.LeaseGeneration != in.ExpectedGeneration {
			return ErrAdhocOpenConflict
		}
		if x.EffectiveSessionID != c.k.ID {
			if c.k.ExpiresAt.IsZero() || x.EffectiveExpiresAt.IsZero() || x.EffectiveExpiresAt.After(s.now()) {
				return ErrAdhocOpenConflict.WithDetail("Chỉ tiếp nhận khi phiên cũ đã hết hạn")
			}
			if utf8.RuneCountInString(reason) < 10 {
				return problem.Validation.WithField("reason", "Lý do tiếp nhận từ 10 đến 500 ký tự")
			}
			if e = store.AppendAdhocOpenLease(ctx, tx, c.p.TenantID, *x, c.k.ID, c.k.ExpiresAt, s.now(), reason, ids.New()); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return Detail{}, classify(err)
	}
	var original OpenInput
	if !validOpenIntent(*x) || json.Unmarshal(x.Input, &original) != nil || !strings.HasPrefix(x.NativeKey, "khtc:") {
		return Detail{}, ErrDependency
	}
	return s.openAdhoc(ctx, c, original, strings.TrimPrefix(x.NativeKey, "khtc:"))
}
