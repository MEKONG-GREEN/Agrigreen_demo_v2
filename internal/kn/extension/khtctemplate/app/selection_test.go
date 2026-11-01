package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/jackc/pgx/v5"
)

type lookupFailure struct{ err error }

func (f lookupFailure) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) { return nil, f.err }

func TestSelectionDoesNotFallbackOnDatabaseError(t *testing.T) {
	want := errors.New("injected lookup outage")
	s := Service{DB: lookupFailure{want}, Clock: clock.NewFixed(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))}
	selected, err := port.NewSessionContext(port.SessionContext{ID: ids.New(), Role: port.SessionRoleAdmin, Scope: authz.Scope{All: true}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{TenantID: ids.New(), ActorRef: ids.New(), SelectedSession: &selected})
	got, err := s.Select(ctx, "mau_1", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, want) || got.Mode != "" {
		t.Fatalf("database error became fallback: %v %+v", err, got)
	}
}
func TestPublishedHashMismatchIsAnError(t *testing.T) {
	_, err := snapshot(store.Version{Key: "mau_1", Rule: domain.RuleVersion, Version: 1, Definition: domain.Seed("mau_1"), Hash: strings.Repeat("0", 64)}, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	var p *problem.Problem
	if !errors.As(err, &p) || p.Code != ErrInvalid.Code {
		t.Fatalf("hash mismatch did not fail closed: %v", err)
	}
}
