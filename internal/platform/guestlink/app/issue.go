package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Service) Issue(ctx context.Context, b port.Binding, until time.Time, key string) (port.Grant, error) {
	if !s.available() {
		return port.Grant{}, port.ErrUnavailable
	}
	if !requestKeyPattern.MatchString(key) {
		return port.Grant{}, s.issuerFailure(ctx, "issue_invalid")
	}
	p, state, err := s.issuer(ctx, b)
	if err != nil {
		return port.Grant{}, s.issuerFailure(ctx, "issuer")
	}
	now := s.now()
	if !state.Enabled || state.Locked || !state.Until.After(now) || !verifiedEmail(state) || state.ContactRevision == uuid.Nil || state.OwnerRevision == uuid.Nil || state.Contact.Reveal() == "" {
		return port.Grant{}, s.issuerFailure(ctx, "issue_state")
	}
	if until.IsZero() {
		until = now.Add(s.cfg.LinkTTL)
	}
	until = minimum(until.UTC().Truncate(time.Microsecond), state.Until.UTC().Truncate(time.Microsecond), now.Add(s.cfg.LinkTTL))
	if !until.After(now) {
		return port.Grant{}, s.issuerFailure(ctx, "issue_state")
	}
	binding := domain.BindingKey(b)
	idem := domain.MAC(s.cfg.Key, "issue-key", p.ActorRef.String()+":"+binding+":"+key)
	id, err := uuid.NewV7()
	if err != nil {
		return port.Grant{}, port.ErrUnavailable
	}
	g := store.Grant{ID: id, Binding: b, Key: binding, IssueKey: idem, OwnerRevision: state.OwnerRevision, ContactRevision: state.ContactRevision, Until: until, Issuer: p.ActorRef, At: now}
	hash, _ := s.tokenHash(s.linkToken(&g), "link")
	g.LinkHash = hash
	var semantic error
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, "binding:"+binding, "subject:"+b.Subject.String()); e != nil {
			return e
		}
		old, e := store.ByIssue(ctx, tx, idem)
		if e != nil {
			return e
		}
		latest, e := store.Latest(ctx, tx, binding)
		if e != nil {
			return e
		}
		if old != nil {
			events, e := store.Events(ctx, tx, old.ID)
			if e != nil {
				return e
			}
			if !s.validGrant(old) || old.Binding != b || old.Issuer != p.ActorRef || latest == nil || latest.ID != old.ID || revoked(events) || reason(old, state, now) != "" {
				semantic = port.ErrDenied
				return nil
			}
			g = *old
			return nil
		}
		if latest != nil {
			e = s.event(ctx, tx, latest, domain.MAC(s.cfg.Key, "ip", "issuer"), "rotated", "replaced", uuid.Nil, uuid.Nil, "", "", nil)
			if e != nil {
				return e
			}
		}
		return store.InsertGrant(ctx, tx, g)
	})
	if err != nil {
		return port.Grant{}, port.ErrUnavailable
	}
	if semantic != nil {
		return port.Grant{}, semantic
	}
	return port.Grant{ID: g.ID, Binding: g.Binding, Link: s.linkToken(&g), Until: g.Until}, nil
}
func (s *Service) Revoke(ctx context.Context, b port.Binding) error {
	if !s.available() {
		return port.ErrUnavailable
	}
	_, _, err := s.issuer(ctx, b)
	if err != nil {
		return s.issuerFailure(ctx, "revoke_issuer")
	}
	key := domain.BindingKey(b)
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, "binding:"+key, "subject:"+b.Subject.String()); e != nil {
			return e
		}
		g, e := store.Latest(ctx, tx, key)
		if e != nil || g == nil {
			return e
		}
		events, e := store.Events(ctx, tx, g.ID)
		if e != nil || revoked(events) {
			return e
		}
		return s.event(ctx, tx, g, domain.MAC(s.cfg.Key, "ip", "issuer"), "revoked", "issuer", uuid.Nil, uuid.Nil, "", "", nil)
	})
	if err != nil {
		return port.ErrUnavailable
	}
	return nil
}
