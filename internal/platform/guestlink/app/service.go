// Package app implements the dormant owner-neutral guest capability. All owner
// and delivery I/O happens between short, sequential module transactions.
package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/mail"
	"net/netip"
	"reflect"
	"regexp"
	"strconv"
	"time"
)

var DBModule = db.Module("pf/guestlink")
var requestKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,120}$`)

type Service struct {
	db     tenant.Beginner
	clk    clock.Clock
	cfg    Config
	owners map[string]port.Owner
	sender port.Sender
}

var _ port.GuestLinks = (*Service)(nil)

func New(b tenant.Beginner, clk clock.Clock, cfg Config, owners map[string]port.Owner, sender port.Sender) (*Service, error) {
	cfg = defaults(cfg)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.Key = append([]byte{}, cfg.Key...)
	if nilDependency(sender) {
		sender = nil
	}
	s := &Service{db: b, clk: clk, cfg: cfg, owners: map[string]port.Owner{}, sender: sender}
	for key, owner := range owners {
		if !domain.ValidOwner(key) || nilDependency(owner) {
			return nil, port.ErrInvalid
		}
		s.owners[key] = owner
	}
	if cfg.Enabled && (nilDependency(b) || nilDependency(clk) || len(s.owners) == 0) {
		return nil, port.ErrUnavailable
	}
	return s, nil
}
func (s *Service) available() bool { return s != nil && s.cfg.Enabled && s.db != nil && s.clk != nil }
func (s *Service) now() time.Time  { return s.clk.Now().UTC().Truncate(time.Microsecond) }
func (s *Service) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	return db.WithModuleTenantTx(ctx, s.db, DBModule, s.cfg.Tenant, fn)
}
func (s *Service) tokenHash(token port.Secret, purpose string) (string, bool) {
	t, h, err := domain.ParseToken(token)
	if err != nil || t != s.cfg.Tenant {
		return "", false
	}
	return domain.MAC(s.cfg.Key, purpose, h), true
}
func (s *Service) ipHash(raw string) (string, bool) {
	if len(raw) > 64 {
		return domain.MAC(s.cfg.Key, "ip", "invalid"), false
	}
	ip, err := netip.ParseAddr(raw)
	if err != nil {
		return domain.MAC(s.cfg.Key, "ip", "invalid"), false
	}
	return domain.MAC(s.cfg.Key, "ip", ip.Unmap().WithZone("").String()), true
}
func (s *Service) event(ctx context.Context, tx pgx.Tx, g *store.Grant, ip, action, reason string, challenge, session uuid.UUID, hash, op string, until *time.Time) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	e := store.Event{ID: id, IPHash: ip, Action: action, Reason: reason, ChallengeID: challenge, SessionRef: session, SessionHash: hash, OperationHash: op, Until: until, At: s.now()}
	if g != nil {
		e.GrantID = g.ID
		e.Actor = g.Binding.Subject
	}
	if action == "rotated" || action == "revoked" && reason == "issuer" {
		if p, ok := authz.FromContext(ctx); ok && p.TenantID == s.cfg.Tenant {
			e.Actor = p.ActorRef
		}
	}
	return store.InsertEvent(ctx, tx, s.cfg.Tenant, e)
}
func (s *Service) deny(ctx context.Context, tx pgx.Tx, g *store.Grant, ip, reason string) error {
	return s.event(ctx, tx, g, ip, "denied", reason, uuid.Nil, uuid.Nil, "", "", nil)
}
func (s *Service) unknown(ctx context.Context, ip string) error {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, "ip:"+ip); e != nil {
			return e
		}
		n, e := store.IPCount(ctx, tx, ip, s.now().Add(-s.cfg.RateWindow))
		if e != nil {
			return e
		}
		if n >= s.cfg.MaxIP {
			return s.event(ctx, tx, nil, ip, "quota", "rate", uuid.Nil, uuid.Nil, "", "", nil)
		}
		return s.deny(ctx, tx, nil, ip, "invalid")
	})
	if err != nil {
		return port.ErrUnavailable
	}
	return port.ErrDenied
}
func (s *Service) loadLink(ctx context.Context, token port.Secret, ip string) (*store.Grant, error) {
	hash, ok := s.tokenHash(token, "link")
	if !ok {
		return nil, s.unknown(ctx, ip)
	}
	var g *store.Grant
	err := s.tx(ctx, func(tx pgx.Tx) error { var e error; g, e = store.ByLink(ctx, tx, hash); return e })
	if err != nil {
		return nil, port.ErrUnavailable
	}
	if g == nil || !s.validGrant(g) {
		return nil, s.unknown(ctx, ip)
	}
	return g, nil
}
func (s *Service) current(ctx context.Context, g *store.Grant) (port.State, error) {
	owner := s.owners[g.Binding.Owner]
	if owner == nil {
		return port.State{}, port.ErrUnavailable
	}
	state, err := owner.Current(ctx, g.Binding) // deliberately outside TX
	if err != nil {
		return port.State{}, port.ErrDenied
	}
	return state, nil
}
func reason(g *store.Grant, state port.State, now time.Time) string {
	if !state.Enabled {
		return "disabled"
	}
	if state.Locked {
		return "locked"
	}
	if !state.Until.After(now) || !g.Until.After(now) {
		return "expired"
	}
	if state.OwnerRevision == uuid.Nil || state.OwnerRevision != g.OwnerRevision {
		return "owner_changed"
	}
	if state.Channel != "email" || !state.ContactVerified || state.ContactRevision == uuid.Nil || state.ContactRevision != g.ContactRevision {
		return "contact_changed"
	}
	return ""
}
func count(events []store.Event, action string) int {
	n := 0
	for _, e := range events {
		if e.Action == action {
			n++
		}
	}
	return n
}
func revoked(events []store.Event) bool {
	return count(events, "revoked") > 0 || count(events, "rotated") > 0
}
func minimum(ts ...time.Time) time.Time {
	v := ts[0]
	for _, t := range ts[1:] {
		if t.Before(v) {
			v = t
		}
	}
	return v
}

// The first error is semantic (audit commits), the second a DB error (rollback).
type guestAction func(pgx.Tx, []store.Event) (error, error)

func (s *Service) guestTx(ctx context.Context, g *store.Grant, ip string, state port.State, fn guestAction) error {
	var semantic error
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, "binding:"+g.Key, "subject:"+g.Binding.Subject.String(), "ip:"+ip); e != nil {
			return e
		}
		latest, e := store.Latest(ctx, tx, g.Key)
		if e != nil {
			return e
		}
		events, e := store.Events(ctx, tx, g.ID)
		if e != nil {
			return e
		}
		r := reason(g, state, s.now())
		if latest == nil || latest.ID != g.ID {
			r = "rotated"
		}
		if revoked(events) {
			r = "revoked"
		}
		if len(events) > 4096 {
			r = "history"
		}
		if r != "" {
			semantic = port.ErrDenied
			if !revoked(events) && (r == "locked" || r == "expired" || r == "owner_changed" || r == "contact_changed" || r == "disabled") {
				if e := s.event(ctx, tx, g, ip, "revoked", r, uuid.Nil, uuid.Nil, "", "", nil); e != nil {
					return e
				}
			}
			return s.deny(ctx, tx, g, ip, r)
		}
		n, e := store.IPCount(ctx, tx, ip, s.now().Add(-s.cfg.RateWindow))
		if e != nil {
			return e
		}
		m, e := store.SubjectCount(ctx, tx, g.Binding.Subject, s.now().Add(-s.cfg.RateWindow))
		if e != nil {
			return e
		}
		if n >= s.cfg.MaxIP || m >= s.cfg.MaxSubject {
			semantic = port.ErrDenied
			return s.event(ctx, tx, g, ip, "quota", "rate", uuid.Nil, uuid.Nil, "", "", nil)
		}
		var dbErr error
		semantic, dbErr = fn(tx, events)
		return dbErr
	})
	if err != nil {
		return port.ErrUnavailable
	}
	return semantic
}
func (s *Service) issuer(ctx context.Context, b port.Binding) (authz.Principal, port.State, error) {
	p, ok := authz.FromContext(ctx)
	if !ok || p.ActorRef == uuid.Nil || p.TenantID != s.cfg.Tenant || b.Tenant != s.cfg.Tenant || !domain.ValidBinding(b) {
		return authz.Principal{}, port.State{}, port.ErrDenied
	}
	owner := s.owners[b.Owner]
	if owner == nil {
		return authz.Principal{}, port.State{}, port.ErrUnavailable
	}
	state, err := owner.MayIssue(ctx, b) // deliberately outside TX
	if err != nil {
		return authz.Principal{}, port.State{}, port.ErrDenied
	}
	return p, state, nil
}

func (s *Service) seal(g *store.Grant) string {
	// Fixed-size identifiers and a numeric instant avoid fallible time JSON encoding.
	value := domain.BindingKey(g.Binding) + ":" + g.OwnerRevision.String() + ":" +
		g.ContactRevision.String() + ":" + strconv.FormatInt(g.Until.UnixMicro(), 10) + ":" + g.Issuer.String()
	return domain.MAC(s.cfg.Key, "grant-seal", value)
}
func (s *Service) linkToken(g *store.Grant) port.Secret {
	return domain.DerivedToken(s.cfg.Key, s.cfg.Tenant, g.ID, "link:"+s.seal(g))
}
func (s *Service) validGrant(g *store.Grant) bool {
	if g.ID == uuid.Nil || g.Binding.Tenant != s.cfg.Tenant || !domain.ValidBinding(g.Binding) || g.Key != domain.BindingKey(g.Binding) || g.OwnerRevision == uuid.Nil || g.ContactRevision == uuid.Nil {
		return false
	}
	hash, ok := s.tokenHash(s.linkToken(g), "link")
	return ok && domain.SameMAC(hash, g.LinkHash)
}
func (s *Service) sessionToken(g *store.Grant, ref uuid.UUID, until time.Time) port.Secret {
	return domain.DerivedToken(s.cfg.Key, s.cfg.Tenant, ref, "session:"+g.ID.String()+":"+s.seal(g)+":"+until.UTC().Format(time.RFC3339Nano))
}
func (s *Service) knownDenied(ctx context.Context, g *store.Grant, ip, why string) error {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, "binding:"+g.Key, "ip:"+ip); e != nil {
			return e
		}
		return s.deny(ctx, tx, g, ip, why)
	})
	if err != nil {
		return port.ErrUnavailable
	}
	return port.ErrDenied
}

func nilDependency(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func (s *Service) String() string   { return "guestlink.Service{redacted}" }
func (s *Service) GoString() string { return s.String() }

func verifiedEmail(state port.State) bool {
	raw := state.Contact.Reveal()
	if !state.ContactVerified || state.Channel != "email" || len(raw) > 254 || raw == "" {
		return false
	}
	address, err := mail.ParseAddress(raw)
	return err == nil && address.Name == "" && address.Address == raw
}

func (s *Service) issuerFailure(ctx context.Context, why string) error {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		id, e := uuid.NewV7()
		if e != nil {
			return e
		}
		actor := uuid.Nil
		if p, ok := authz.FromContext(ctx); ok && p.TenantID == s.cfg.Tenant {
			actor = p.ActorRef
		}
		return store.InsertEvent(ctx, tx, s.cfg.Tenant, store.Event{ID: id, Action: "denied", Reason: why, Actor: actor, IPHash: domain.MAC(s.cfg.Key, "ip", "issuer"), At: s.now()})
	})
	if err != nil {
		return port.ErrUnavailable
	}
	return port.ErrDenied
}

// Derivation binds a challenge to immutable grant metadata, even if a database
// writer attempts to reuse a challenge identifier from another resource.
func (s *Service) otp(g *store.Grant, id uuid.UUID) port.Secret {
	key := domain.MAC(s.cfg.Key, "otp-scope", g.ID.String()+":"+s.seal(g))
	return domain.Code([]byte(key), id)
}
