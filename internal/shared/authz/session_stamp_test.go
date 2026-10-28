package authz_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

func TestSessionAuthorizationStampBindsCompleteOwnerProjection(t *testing.T) {
	id, node, supported := uuid.New(), uuid.New(), uuid.New()
	baseScope := authz.SessionScope{NodeID: node, Department: "fixture_department", UnitCodes: []string{"one", "two"}, OnBehalfOfRef: supported, RequiresNode: true}
	payload := []byte(`{"targets":["one"],"read_only":false}`)
	makeSession := func(profile string, sid uuid.UUID, scope authz.SessionScope, data []byte) authz.Session {
		t.Helper()
		session, err := authz.NewSession(profile, sid, scope)
		if err != nil {
			t.Fatal(err)
		}
		return session.WithPayload(data)
	}
	base := makeSession("fixture", id, baseScope, payload)
	stamp, err := base.AuthorizationStamp()
	if err != nil || len(stamp) != 32 {
		t.Fatal("missing fixed-size authorization digest", err)
	}
	clone := makeSession("fixture", id, baseScope, payload)
	again, err := clone.AuthorizationStamp()
	if err != nil || !bytes.Equal(stamp, again) {
		t.Fatal("equal immutable projections do not have deterministic stamps", err)
	}
	scopeChange := func(change func(*authz.SessionScope)) authz.Session {
		scope := base.Scope()
		change(&scope)
		return makeSession("fixture", id, scope, payload)
	}
	cases := map[string]authz.Session{
		"profile":      makeSession("other", id, baseScope, payload),
		"id":           makeSession("fixture", uuid.New(), baseScope, payload),
		"node":         scopeChange(func(s *authz.SessionScope) { s.NodeID = uuid.New() }),
		"department":   scopeChange(func(s *authz.SessionScope) { s.Department = "other" }),
		"units":        scopeChange(func(s *authz.SessionScope) { s.UnitCodes = []string{"one"} }),
		"supported":    scopeChange(func(s *authz.SessionScope) { s.OnBehalfOfRef = uuid.New() }),
		"node guard":   scopeChange(func(s *authz.SessionScope) { s.RequiresNode = false }),
		"targets":      base.WithPayload([]byte(`{"targets":["two"],"read_only":false}`)),
		"read only":    base.WithPayload([]byte(`{"targets":["one"],"read_only":true}`)),
		"opaque bytes": base.WithPayload([]byte{0, 255, 10, 1}),
		"no payload":   base.WithPayload(nil),
	}
	for name, session := range cases {
		t.Run(name, func(t *testing.T) {
			changed, err := session.AuthorizationStamp()
			if err != nil || bytes.Equal(stamp, changed) {
				t.Fatal("changed owner projection reused authorization stamp", err)
			}
		})
	}
	// Reading or mutating a returned digest must not change the session or later stamps.
	beforeScope, beforePayload := base.Scope(), base.Payload()
	stamp[0] ^= 255
	after, err := base.AuthorizationStamp()
	if err != nil || !bytes.Equal(after, again) || !reflect.DeepEqual(beforeScope, base.Scope()) || !bytes.Equal(beforePayload, base.Payload()) {
		t.Fatal("stamp aliases session state or mutates its projection", err)
	}
	if invalid, err := (authz.Session{}).WithPayload([]byte("opaque")).AuthorizationStamp(); !errors.Is(err, authz.ErrInvalidSession) || invalid != nil {
		t.Fatal("zero session acquired a stamp")
	}
}
