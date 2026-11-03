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

func consumed(events []store.Event, id uuid.UUID) bool {
	for _, e := range events {
		if e.Action == "verified" && e.ChallengeID == id {
			return true
		}
	}
	return false
}
func (s *Service) Verify(ctx context.Context, link port.Secret, id uuid.UUID, code port.Secret, rawIP string) (port.Session, error) {
	if !s.available() {
		return port.Session{}, port.ErrUnavailable
	}
	ip, valid := s.ipHash(rawIP)
	if !valid {
		return port.Session{}, s.unknown(ctx, ip)
	}
	g, err := s.loadLink(ctx, link, ip)
	if err != nil {
		return port.Session{}, err
	}
	state, err := s.current(ctx, g)
	if err != nil {
		return port.Session{}, s.knownDenied(ctx, g, ip, "owner")
	}
	var out port.Session
	err = s.guestTx(ctx, g, ip, state, func(tx pgx.Tx, events []store.Event) (error, error) {
		c := challengeFor(events, uuid.Nil)
		if c == nil || c.ID != id || c.Until == nil {
			return port.ErrDenied, s.deny(ctx, tx, g, ip, "challenge")
		}
		if challengeExpired(events, id) || !c.Until.After(s.now()) {
			return port.ErrDenied, s.event(ctx, tx, g, ip, "denied", "challenge_expired", id, uuid.Nil, "", "", nil)
		}
		if !delivered(events, id) || consumed(events, id) {
			return port.ErrDenied, s.deny(ctx, tx, g, ip, "challenge")
		}
		if count(events, "otp_wrong")+count(events, "verified") >= s.cfg.MaxAttempts {
			return port.ErrDenied, s.event(ctx, tx, g, ip, "quota", "attempts", id, uuid.Nil, "", "", nil)
		}
		// Always compare keyed fixed-length values; malformed/huge codes never enter hashing.
		candidate := "invalid"
		if domain.ValidCode(code) {
			candidate = code.Reveal()
		}
		want := domain.MAC(s.cfg.Key, "otp-check", id.String()+":"+s.otp(g, id).Reveal())
		got := domain.MAC(s.cfg.Key, "otp-check", id.String()+":"+candidate)
		if !domain.SameMAC(want, got) {
			return port.ErrDenied, s.event(ctx, tx, g, ip, "otp_wrong", "code", id, uuid.Nil, "", "", nil)
		}
		ref, e := uuid.NewV7()
		if e != nil {
			return nil, e
		}
		until := minimum(s.now().Add(s.cfg.SessionTTL), g.Until, state.Until).UTC().Truncate(time.Microsecond)
		token := s.sessionToken(g, ref, until)
		hash, _ := s.tokenHash(token, "session")
		e = s.event(ctx, tx, g, ip, "verified", "", id, ref, hash, "", &until)
		if e != nil {
			return nil, e
		}
		out = port.Session{Token: token, Binding: g.Binding, Until: until}
		return nil, nil
	})
	if err != nil {
		return port.Session{}, err
	}
	return out, nil
}
func (s *Service) Authorize(ctx context.Context, token port.Secret, rawIP string) (port.Binding, error) {
	if !s.available() {
		return port.Binding{}, port.ErrUnavailable
	}
	ip, valid := s.ipHash(rawIP)
	if !valid {
		return port.Binding{}, s.unknown(ctx, ip)
	}
	hash, valid := s.tokenHash(token, "session")
	if !valid {
		return port.Binding{}, s.unknown(ctx, ip)
	}
	var session *store.Event
	var g *store.Grant
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var e error
		session, e = store.BySession(ctx, tx, hash)
		if e != nil || session == nil {
			return e
		}
		g, e = store.ByID(ctx, tx, session.GrantID)
		return e
	})
	if err != nil {
		return port.Binding{}, port.ErrUnavailable
	}
	if session == nil || g == nil || !s.validGrant(g) || session.Until == nil || session.SessionRef == uuid.Nil {
		return port.Binding{}, s.unknown(ctx, ip)
	}
	expected := s.sessionToken(g, session.SessionRef, *session.Until)
	if !domain.SameMAC(expected.Reveal(), token.Reveal()) {
		return port.Binding{}, s.unknown(ctx, ip)
	}
	state, err := s.current(ctx, g)
	if err != nil {
		return port.Binding{}, s.knownDenied(ctx, g, ip, "owner")
	}
	err = s.guestTx(ctx, g, ip, state, func(tx pgx.Tx, events []store.Event) (error, error) {
		expired := !session.Until.After(s.now())
		for _, event := range events {
			if event.Action == "session_denied" && event.SessionRef == session.SessionRef && event.Reason == "expired" {
				expired = true
			}
		}
		if expired {
			return port.ErrDenied, s.event(ctx, tx, g, ip, "session_denied", "expired", session.ChallengeID, session.SessionRef, "", "", nil)
		}
		var latest *store.Event
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].Action == "verified" {
				latest = &events[i]
				break
			}
		}
		if latest == nil || latest.SessionRef != session.SessionRef {
			return port.ErrDenied, s.deny(ctx, tx, g, ip, "session_replaced")
		}
		return nil, s.event(ctx, tx, g, ip, "session_ok", "", session.ChallengeID, session.SessionRef, "", "", nil)
	})
	if err != nil {
		return port.Binding{}, err
	}
	return g.Binding, nil
}
