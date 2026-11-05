// Package khtcplan225 composes real KN services for assembly assertions without
// exposing their private implementation types from the composition root.
package khtcplan225

import (
	"context"
	"errors"
	khtcapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/domain"
	periodapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"sync/atomic"
)

type Entry225 struct {
	Ref                                          uuid.UUID
	ProgramRef                                   *uuid.UUID
	Tab, Kind, Name, Code, Tracking, ProgramName string
	SortOrder                                    int
	Communes                                     []string
}
type Input225 struct {
	Year             int
	DraftID          uuid.UUID
	ExpectedRevision int64
	Entries          []Entry225
	Note, IdemKey    string
}
type Event225 struct {
	ID, DraftID uuid.UUID
	Seq         int64
}
type Result225 struct {
	Event   Event225
	Entries []Entry225
}
type App225 struct{ service *khtcapp.Service }

func NewApp225(db, referenceDB tenant.Beginner, clk clock.Clock) *App225 {
	return &App225{service: &khtcapp.Service{DB: db, Clock: clk, YearReferences: &periodapp.Service{DB: referenceDB, Clock: clk}}}
}
func (c *App225) Save(ctx context.Context, in Input225) (Result225, bool, error) {
	entries := make([]domain.Entry, len(in.Entries))
	for i, e := range in.Entries {
		entries[i] = domain.Entry{Ref: e.Ref, ProgramRef: e.ProgramRef, Tab: e.Tab, Kind: e.Kind, Name: e.Name, Code: e.Code, Tracking: e.Tracking, SortOrder: e.SortOrder, Communes: e.Communes}
	}
	result, replay, err := c.service.SaveInitialDraft225(ctx, khtcapp.InitialDraftInput225{Year: in.Year, DraftID: in.DraftID, ExpectedRevision: in.ExpectedRevision, Entries: entries, Note: in.Note, IdemKey: in.IdemKey})
	out := Result225{Event: Event225{ID: result.Event.ID, DraftID: result.Event.DraftID, Seq: result.Event.Seq}, Entries: make([]Entry225, len(result.Entries))}
	for i, e := range result.Entries {
		out.Entries[i] = Entry225{Ref: e.Ref, Name: e.Name, Code: e.Code, ProgramName: e.ProgramName}
	}
	return out, replay, err
}
func (c *App225) Publish(ctx context.Context, year int, key string) error {
	_, _, err := c.service.SaveVersion(ctx, khtcapp.SaveInput{Year: year, IdemKey: key, Entries: []domain.Entry{}})
	return err
}
func (c *App225) History(ctx context.Context, year int, before int64, limit int) ([]Event225, error) {
	events, err := c.service.InitialDraftHistory225(ctx, year, before, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Event225, len(events))
	for i, e := range events {
		out[i] = Event225{ID: e.ID, DraftID: e.DraftID, Seq: e.Seq}
	}
	return out, nil
}
func YearCounts225(ctx context.Context, db tenant.Beginner, year int) (drafts, versions int, err error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return 0, 0, errors.New("fixture principal missing")
	}
	err = tenant.WithTenantTx(ctx, db, p.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM pf_extension.khtc_plan_draft225_event WHERE plan_year=$1),(SELECT count(*) FROM pf_extension.khtc_plan_version WHERE plan_year=$1)`, year).Scan(&drafts, &versions)
	})
	return
}

// LostAckDB225 commits the real owned write, then loses exactly its first ACK.
// Only the fixture knows the private header SQL; assembly asserts public results.
type LostAckDB225 struct {
	tenant.Beginner
	fired atomic.Bool
}

func NewLostAckDB225(db tenant.Beginner) *LostAckDB225 { return &LostAckDB225{Beginner: db} }
func (b *LostAckDB225) Fired() bool                    { return b.fired.Load() }

type lostAckTx225 struct {
	pgx.Tx
	owner  *LostAckDB225
	writes bool
}

func (b *LostAckDB225) BeginTx(ctx context.Context, opt pgx.TxOptions) (pgx.Tx, error) {
	tx, e := b.Beginner.BeginTx(ctx, opt)
	if e != nil {
		return nil, e
	}
	return &lostAckTx225{Tx: tx, owner: b}, nil
}
func (tx *lostAckTx225) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "INSERT INTO pf_extension.khtc_plan_draft225_event") {
		tx.writes = true
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}
func (tx *lostAckTx225) Commit(ctx context.Context) error {
	if e := tx.Tx.Commit(ctx); e != nil {
		return e
	}
	if tx.writes && tx.owner.fired.CompareAndSwap(false, true) {
		return context.DeadlineExceeded
	}
	return nil
}
