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

func challengeFor(events []store.Event, id uuid.UUID) *store.Event {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Action == "challenge" && (id == uuid.Nil || events[i].ID == id) {
			e := events[i]
			return &e
		}
	}
	return nil
}
func delivered(events []store.Event, id uuid.UUID) bool {
	for _, e := range events {
		if e.Action == "sent" && e.ChallengeID == id {
			return true
		}
	}
	return false
}
func (s *Service) Open(ctx context.Context, token port.Secret, key, rawIP string) (port.Challenge, error) {
	if !s.available() {
		return port.Challenge{}, port.ErrUnavailable
	}
	ip, validIP := s.ipHash(rawIP)
	if !validIP || !requestKeyPattern.MatchString(key) {
		return port.Challenge{}, s.unknown(ctx, ip)
	}
	g, err := s.loadLink(ctx, token, ip)
	if err != nil {
		return port.Challenge{}, err
	}
	state, err := s.current(ctx, g)
	if err != nil {
		return port.Challenge{}, s.knownDenied(ctx, g, ip, "owner")
	}
	op := domain.MAC(s.cfg.Key, "open-key", g.ID.String()+":"+key)
	var selected store.Event
	var alreadySent bool
	err = s.guestTx(ctx, g, ip, state, func(tx pgx.Tx, events []store.Event) (error, error) {
		if count(events, "opened") >= s.cfg.MaxOpens {
			return port.ErrDenied, s.event(ctx, tx, g, ip, "quota", "opens", uuid.Nil, uuid.Nil, "", "", nil)
		}
		if e := s.event(ctx, tx, g, ip, "opened", "", uuid.Nil, uuid.Nil, "", "", nil); e != nil {
			return nil, e
		}
		for _, e := range events {
			if e.Action == "challenge" && e.OperationHash == op {
				selected = e
				alreadySent = delivered(events, e.ID)
				latest := challengeFor(events, uuid.Nil)
				if latest == nil || latest.ID != e.ID || consumed(events, e.ID) || e.Until == nil || !e.Until.After(s.now()) {
					return port.ErrDenied, nil
				}
				return nil, nil
			}
		}
		previous := challengeFor(events, uuid.Nil)
		if count(events, "challenge") >= s.cfg.MaxSends || previous != nil && s.now().Before(previous.At.Add(s.cfg.Cooldown)) {
			return port.ErrDenied, s.deny(ctx, tx, g, ip, "cooldown")
		}
		id, e := uuid.NewV7()
		if e != nil {
			return nil, e
		}
		until := minimum(s.now().Add(s.cfg.OTPTTL), g.Until, state.Until).UTC().Truncate(time.Microsecond)
		selected = store.Event{ID: id, GrantID: g.ID, Action: "challenge", OperationHash: op, IPHash: ip, Until: &until, Actor: g.Binding.Subject, At: s.now()}
		return nil, store.InsertEvent(ctx, tx, s.cfg.Tenant, selected)
	})
	if err != nil {
		return port.Challenge{}, err
	}
	if selected.Until == nil {
		return port.Challenge{}, port.ErrDenied
	}
	if alreadySent {
		return port.Challenge{ID: selected.ID, Until: *selected.Until, Delivery: port.Sent}, nil
	}
	result := port.Disabled
	if s.sender != nil && verifiedEmail(state) {
		result, err = s.sender.Send(ctx, port.Delivery{Key: selected.ID, Channel: "email", To: state.Contact, Code: s.otp(g, selected.ID), Until: *selected.Until}) // outside TX
		if err != nil || result != port.Sent && result != port.Disabled && result != port.Dropped {
			result = port.Dropped
		}
	}
	action := "disabled"
	if result == port.Sent {
		action = "sent"
	}
	// Record the actual delivery result even if the owner locked while transport
	// was in flight. Verification still rechecks the owner and immutable revisions.
	saveErr := s.tx(ctx, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, "binding:"+g.Key); e != nil {
			return e
		}
		events, e := store.Events(ctx, tx, g.ID)
		if e != nil {
			return e
		}
		if delivered(events, selected.ID) {
			return nil
		}
		return s.event(ctx, tx, g, ip, action, string(result), selected.ID, uuid.Nil, "", "", nil)
	})
	if saveErr != nil {
		return port.Challenge{}, port.ErrUnavailable
	}
	out := port.Challenge{ID: selected.ID, Until: *selected.Until, Delivery: result}
	if result != port.Sent {
		return out, port.ErrUnavailable
	}
	return out, nil
}
