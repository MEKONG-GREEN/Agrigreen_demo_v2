package authz_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

func TestSessionProjectionIsImmutableAndNotSerialized(t *testing.T) {
	codes := []string{"one", "two"}
	s, err := authz.NewSession("example", uuid.New(), authz.SessionScope{UnitCodes: codes, RequiresNode: true})
	if err != nil {
		t.Fatal(err)
	}
	codes[0] = "changed"
	view := s.Scope()
	view.UnitCodes[1] = "changed"
	if !reflect.DeepEqual(s.Scope().UnitCodes, []string{"one", "two"}) {
		t.Fatal("session aliases mutable scope data")
	}
	p := authz.Principal{SelectedSession: &s}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["SelectedSession"]; ok {
		t.Fatal("server projection leaked into principal JSON")
	}
	if !((authz.Session{}).Scope().RequiresNode) {
		t.Fatal("zero session must fail closed")
	}
	for _, profile := range []string{"", "Example", "a/b", "a b"} {
		if _, err := authz.NewSession(profile, uuid.New(), authz.SessionScope{}); !errors.Is(err, authz.ErrInvalidSession) {
			t.Fatalf("invalid profile %q: %v", profile, err)
		}
	}
	if _, err := authz.NewSession("example", uuid.Nil, authz.SessionScope{}); !errors.Is(err, authz.ErrInvalidSession) {
		t.Fatal(err)
	}
}

func TestSessionRegistryFailsClosed(t *testing.T) {
	makeSession := func(profile string) authz.Session {
		s, err := authz.NewSession(profile, uuid.New(), authz.SessionScope{})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	registration := func(profile string, project authz.SessionProjector) authz.SessionRegistration {
		return authz.SessionRegistration{Profile: profile, Project: project}
	}
	yes := func(profile string) authz.SessionProjector {
		return func(context.Context, authz.Principal) (authz.Session, bool, error) {
			return makeSession(profile), true, nil
		}
	}
	no := func(context.Context, authz.Principal) (authz.Session, bool, error) {
		return authz.Session{}, false, nil
	}
	boom := errors.New("synthetic provider failure")
	cases := []struct {
		name    string
		entries []authz.SessionRegistration
		want    error
	}{
		{"missing", nil, authz.ErrSessionNotResolved},
		{"not handled", []authz.SessionRegistration{registration("example", no)}, authz.ErrSessionNotResolved},
		{"provider error", []authz.SessionRegistration{registration("example", func(context.Context, authz.Principal) (authz.Session, bool, error) {
			return authz.Session{}, false, boom
		})}, boom},
		{"error after match", []authz.SessionRegistration{registration("example", yes("example")), registration("other", func(context.Context, authz.Principal) (authz.Session, bool, error) {
			return authz.Session{}, false, boom
		})}, boom},
		{"zero result", []authz.SessionRegistration{registration("example", func(context.Context, authz.Principal) (authz.Session, bool, error) { return authz.Session{}, true, nil })}, authz.ErrInvalidSession},
		{"profile mismatch", []authz.SessionRegistration{registration("example", yes("other"))}, authz.ErrInvalidSession},
		{"ambiguous", []authz.SessionRegistration{registration("example", yes("example")), registration("other", yes("other"))}, authz.ErrSessionAmbiguous},
		{"ignored nonzero", []authz.SessionRegistration{registration("example", func(context.Context, authz.Principal) (authz.Session, bool, error) {
			return makeSession("example"), false, nil
		})}, authz.ErrInvalidSession},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := authz.NewSessionRegistry(c.entries...)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.ResolveSelected(context.Background(), authz.Principal{Roles: []string{"platform_admin"}}); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
	for _, entries := range [][]authz.SessionRegistration{{registration("bad/name", no)}, {registration("example", nil)}, {registration("example", no), registration("example", no)}} {
		if _, err := authz.NewSessionRegistry(entries...); !errors.Is(err, authz.ErrInvalidSession) {
			t.Fatal("invalid registration accepted")
		}
	}
	var nilRegistry *authz.SessionRegistry
	if _, err := nilRegistry.ResolveSelected(context.Background(), authz.Principal{}); !errors.Is(err, authz.ErrSessionNotResolved) {
		t.Fatal(err)
	}
	entries := []authz.SessionRegistration{registration("example", yes("example"))}
	r, err := authz.NewSessionRegistry(entries...)
	if err != nil {
		t.Fatal(err)
	}
	entries[0].Project = no
	s, err := r.ResolveSelected(context.Background(), authz.Principal{})
	if err != nil || s.Profile() != "example" {
		t.Fatalf("registry aliases caller registration: %v", err)
	}
	if _, err := r.ResolveSelected(context.Background(), authz.Principal{SelectedSession: &s}); !errors.Is(err, authz.ErrSessionAlreadyBound) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.ResolveSelected(ctx, authz.Principal{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSessionRegistryDoesNotMutateRealmRoles(t *testing.T) {
	p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), OrgRef: uuid.New(), Roles: []string{"reader"}, MFA: true}
	r, err := authz.NewSessionRegistry(authz.SessionRegistration{Profile: "example", Project: func(_ context.Context, input authz.Principal) (authz.Session, bool, error) {
		input.Roles[0] = "platform_admin"
		s, err := authz.NewSession("example", uuid.New(), authz.SessionScope{})
		return s, true, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ResolveSelected(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if p.Has("platform_admin") || !p.Has("reader") {
		t.Fatal("provider mutated token permissions")
	}
}

func TestSessionOpaquePayloadIsIsolated(t *testing.T) {
	s, err := authz.NewSession("example", uuid.New(), authz.SessionScope{Department: "original"})
	if err != nil {
		t.Fatal(err)
	}
	original := []byte{0, 1, 2, 255}
	bound := s.WithPayload(original)
	original[0] = 99
	read := bound.Payload()
	read[1] = 99
	if !reflect.DeepEqual(bound.Payload(), []byte{0, 1, 2, 255}) || s.Payload() != nil || bound.Scope().Department != "original" {
		t.Fatal("payload aliases input/output or changes original session")
	}
	p := authz.Principal{SelectedSession: &bound}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["SelectedSession"]; ok {
		t.Fatal("payload leaked to principal JSON")
	}
}
