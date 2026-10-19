package authz

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/google/uuid"
)

// SessionScope is a server-owned data projection. It cannot change the actor,
// tenant, realm roles, MFA or processing purpose of the authenticated request.
type SessionScope struct {
	NodeID        uuid.UUID
	Department    string
	UnitCodes     []string
	OnBehalfOfRef uuid.UUID
	RequiresNode  bool
}

// Session is an immutable projection of a selected application session.
// Its scope comes from validated server state, never client input.
type Session struct {
	profile string
	id      uuid.UUID
	scope   SessionScope
	payload []byte
}

var (
	sessionProfilePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	ErrInvalidSession      = errors.New("authz: invalid selected session projection")
	ErrSessionNotResolved  = errors.New("authz: selected session has no owner")
	ErrSessionAmbiguous    = errors.New("authz: selected session has multiple owners")
	ErrSessionAlreadyBound = errors.New("authz: selected session is already bound")
)

func NewSession(profile string, id uuid.UUID, scope SessionScope) (Session, error) {
	if !sessionProfilePattern.MatchString(profile) || id == uuid.Nil {
		return Session{}, ErrInvalidSession
	}
	scope.UnitCodes = slices.Clone(scope.UnitCodes)
	return Session{profile: profile, id: id, scope: scope}, nil
}

func (s Session) Profile() string { return s.profile }
func (s Session) ID() uuid.UUID   { return s.id }
func (s Session) Valid() bool     { return s.profile != "" && s.id != uuid.Nil }

// WithPayload attaches opaque, server-owned application data. The shared kernel
// does not interpret it or use it to change authentication or database scope.
func (s Session) WithPayload(payload []byte) Session {
	s.payload = slices.Clone(payload)
	return s
}

// Payload returns an independent copy; application owners validate their own
// format and version. It is not a request or public HTTP serialization format.
func (s Session) Payload() []byte { return slices.Clone(s.payload) }

// Scope returns a copy. An accidentally bound zero Session denies database
// access through the node guard, even when token permissions would be broader.
func (s Session) Scope() SessionScope {
	if !s.Valid() {
		return SessionScope{RequiresNode: true}
	}
	out := s.scope
	out.UnitCodes = slices.Clone(out.UnitCodes)
	return out
}

// SessionProjector receives authenticated, server-resolved state. handled=false
// means this owner does not apply; errors must never fall back to realm roles.
type SessionProjector func(context.Context, Principal) (Session, bool, error)
type SessionRegistration struct {
	Profile string
	Project SessionProjector
}

type SessionRegistry struct{ entries []SessionRegistration }

// NewSessionRegistry takes trusted startup configuration, with no global state.
func NewSessionRegistry(entries ...SessionRegistration) (*SessionRegistry, error) {
	seen := map[string]bool{}
	for _, entry := range entries {
		if !sessionProfilePattern.MatchString(entry.Profile) || entry.Project == nil || seen[entry.Profile] {
			return nil, fmt.Errorf("authz: session registration: %w", ErrInvalidSession)
		}
		seen[entry.Profile] = true
	}
	return &SessionRegistry{entries: slices.Clone(entries)}, nil
}

// ResolveSelected is only for a request which has selected a session. Missing,
// ambiguous or failed providers return an error; callers cannot silently choose
// token permissions instead. The result has no identity or tenant overrides.
func (r *SessionRegistry) ResolveSelected(ctx context.Context, p Principal) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if p.SelectedSession != nil {
		return Session{}, ErrSessionAlreadyBound
	}
	if r == nil {
		return Session{}, ErrSessionNotResolved
	}
	var result Session
	found := false
	for _, entry := range r.entries {
		input := p
		input.Roles = slices.Clone(p.Roles)
		projected, handled, err := entry.Project(ctx, input)
		if err != nil {
			return Session{}, fmt.Errorf("authz: project session %s: %w", entry.Profile, err)
		}
		if !handled {
			if projected.Valid() {
				return Session{}, ErrInvalidSession
			}
			continue
		}
		if !projected.Valid() || projected.Profile() != entry.Profile {
			return Session{}, ErrInvalidSession
		}
		if found {
			return Session{}, ErrSessionAmbiguous
		}
		result, found = projected, true
	}
	if !found {
		return Session{}, ErrSessionNotResolved
	}
	return result, nil
}
