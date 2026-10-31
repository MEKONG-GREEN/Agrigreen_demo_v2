package app

import (
	"context"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SaveInput struct {
	BaseVersion         int64             `json:"base_version"`
	Definition          domain.Definition `json:"definition"`
	AcknowledgeTextLoss bool              `json:"acknowledge_text_loss"`
}
type RestoreInput struct {
	BaseVersion         int64     `json:"base_version"`
	VersionID           uuid.UUID `json:"version_id"`
	AcknowledgeTextLoss bool      `json:"acknowledge_text_loss"`
}
type PublishInput struct {
	BaseVersion int64     `json:"base_version"`
	VersionID   uuid.UUID `json:"version_id"`
	AppliesFrom string    `json:"applies_from"`
}

func (s *Service) Save(ctx context.Context, key string, in SaveInput, idem string) (store.Version, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return store.Version{}, err
	}
	if !domain.Fixed(key) || in.Definition.Key != key || in.BaseVersion < 0 || in.BaseVersion > 1000000000 {
		return store.Version{}, ErrInvalid
	}
	in.Definition = domain.Canonical(in.Definition)
	if err = domain.Validate(in.Definition, nil); err != nil {
		return store.Version{}, inputError(err)
	}
	requestHash := hash(in)
	idem, err = idemKey(c, "save", key, idem)
	if err != nil {
		return store.Version{}, err
	}
	var out store.Version
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, c.p.TenantID, key); e != nil {
			return e
		}
		replay, e := store.ByIdem(ctx, tx, idem)
		if e != nil {
			return e
		}
		if replay != nil {
			if replay.RequestHash != requestHash {
				return problem.IdempotencyConflict
			}
			out = *replay
			return nil
		}
		out, e = s.newVersion(ctx, tx, c, key, in.BaseVersion, in.Definition, nil, in.AcknowledgeTextLoss, requestHash, idem)
		return e
	})
	return out, err
}

func (s *Service) Restore(ctx context.Context, key string, in RestoreInput, idem string) (store.Version, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return store.Version{}, err
	}
	if !domain.Fixed(key) || in.VersionID == uuid.Nil || in.BaseVersion < 1 || in.BaseVersion > 1000000000 {
		return store.Version{}, ErrInvalid
	}
	requestHash := hash(in)
	idem, err = idemKey(c, "restore", key, idem)
	if err != nil {
		return store.Version{}, err
	}
	var out store.Version
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, c.p.TenantID, key); e != nil {
			return e
		}
		replay, e := store.ByIdem(ctx, tx, idem)
		if e != nil {
			return e
		}
		if replay != nil {
			if replay.RequestHash != requestHash {
				return problem.IdempotencyConflict
			}
			out = *replay
			return nil
		}
		source, e := store.ByID(ctx, tx, in.VersionID)
		if e != nil {
			return e
		}
		if source == nil || source.Key != key {
			return ErrNotFound
		}
		out, e = s.newVersion(ctx, tx, c, key, in.BaseVersion, source.Definition, &source.ID, in.AcknowledgeTextLoss, requestHash, idem)
		return e
	})
	return out, err
}

// newVersion only performs bounded pure validation and same-module DB reads inside
// the transaction. It never calls a source reader, capability, renderer I/O or network.
func (s *Service) newVersion(ctx context.Context, tx pgx.Tx, c caller, key string, base int64, definition domain.Definition, restored *uuid.UUID, ack bool, requestHash, idem string) (store.Version, error) {
	latest, err := store.Latest(ctx, tx, key)
	if err != nil {
		return store.Version{}, err
	}
	previous := domain.Seed(key)
	if latest != nil {
		if latest.Version != base {
			return store.Version{}, ErrVersion
		}
		previous = latest.Definition
	} else if base != 0 {
		return store.Version{}, ErrVersion
	}
	if err = domain.Validate(definition, &previous); err != nil {
		return store.Version{}, inputError(err)
	}
	removed := domain.RemovedTextSlots(previous, definition)
	if !ack && len(removed) > 0 {
		inUse, e := store.HasNarrative(ctx, tx, key, removed)
		if e != nil {
			return store.Version{}, e
		}
		if inUse {
			return store.Version{}, ErrNarrative
		}
	}
	out := store.Version{ID: ids.New(), Key: key, Action: "save", Definition: domain.Canonical(definition), RequestHash: requestHash, RestoredFrom: restored, Actor: c.p.ActorRef, At: s.Clock.Now().UTC()}
	if restored != nil {
		out.Action = "restore"
	}
	err = store.Insert(ctx, tx, c.p.TenantID, &out, idem, store.Support{})
	return out, err
}

func (s *Service) Publish(ctx context.Context, key string, in PublishInput, idem string) (store.Publication, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return store.Publication{}, err
	}
	if !domain.Fixed(key) || in.VersionID == uuid.Nil || in.BaseVersion < 1 || in.BaseVersion > 1000000000 {
		return store.Publication{}, ErrInvalid
	}
	if len(in.AppliesFrom) != 10 {
		return store.Publication{}, ErrInvalid.WithField("applies_from", "Chọn ngày bắt đầu kỳ áp dụng")
	}
	from, err := time.Parse(time.DateOnly, in.AppliesFrom)
	if err != nil || from.Year() < 2000 || from.Year() > 2100 {
		return store.Publication{}, ErrInvalid.WithField("applies_from", "Ngày áp dụng từ năm 2000–2100")
	}
	requestHash := hash(in)
	idem, err = idemKey(c, "publish", key, idem)
	if err != nil {
		return store.Publication{}, err
	}
	var out store.Publication
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if e := store.Lock(ctx, tx, c.p.TenantID, key); e != nil {
			return e
		}
		replay, e := store.PublicationByIdem(ctx, tx, idem)
		if e != nil {
			return e
		}
		if replay != nil {
			if replay.RequestHash != requestHash {
				return problem.IdempotencyConflict
			}
			out = *replay
			return nil
		}
		latest, e := store.Latest(ctx, tx, key)
		if e != nil {
			return e
		}
		if latest == nil {
			return ErrNotFound
		}
		if latest.Version != in.BaseVersion || latest.ID != in.VersionID {
			return ErrVersion
		}
		if e = domain.Validate(latest.Definition, nil); e != nil {
			return inputError(e)
		}
		out = store.Publication{ID: ids.New(), Key: key, VersionID: latest.ID, Version: latest.Version, Hash: latest.Hash, AppliesFrom: from, RequestHash: requestHash, Actor: c.p.ActorRef, At: s.Clock.Now().UTC()}
		return store.Publish(ctx, tx, c.p.TenantID, &out, idem, store.Support{})
	})
	return out, err
}
