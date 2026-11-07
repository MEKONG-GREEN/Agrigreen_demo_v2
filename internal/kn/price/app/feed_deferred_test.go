package app

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

type feedDBTrap struct{ calls int }

func (b *feedDBTrap) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	b.calls++
	return nil, errors.New("feed must stop before database access")
}

func TestDeferredPublicFeedStopsBeforeTransaction(t *testing.T) {
	trap := &feedDBTrap{}
	profiles := []port.Profile{
		{Name: "isolated_consumer", WebsiteDeferred: true, Roles: map[string]string{"viewer": "TW"}},
		{Name: "other_consumer", Roles: map[string]string{"viewer": "TW"}},
	}
	svc, err := NewService(trap, clock.NewFixed(time.Date(2026, 11, 6, 6, 0, 0, 0, time.UTC)), profiles)
	if err != nil {
		t.Fatal(err)
	}
	tickets := NewTicketService(svc)
	for _, name := range []string{"", "isolated_consumer"} {
		raw, err := (PublicFeed{T: tickets, Profile: name}).PublishedRecords(context.Background(), port.FeedPriceBoardDaily, ids.New(), "89")
		if problem.From(err).Status != 403 || problem.From(err).Code != port.ErrWebsiteNotConnected.Code || len(raw) != 0 {
			t.Fatalf("deferred profile must deny, not report an empty successful snapshot: %v %v", raw, err)
		}
	}
	if trap.calls != 0 {
		t.Fatalf("deferred source opened %d transactions", trap.calls)
	}
	_, err = (PublicFeed{T: tickets, Profile: "other_consumer"}).PublishedRecords(context.Background(), port.FeedPriceBoardDaily, ids.New(), "89")
	if err == nil || problem.From(err).Code == port.ErrWebsiteNotConnected.Code || trap.calls != 1 {
		t.Fatalf("other profile must retain its existing read path: calls=%d err=%v", trap.calls, err)
	}
}
