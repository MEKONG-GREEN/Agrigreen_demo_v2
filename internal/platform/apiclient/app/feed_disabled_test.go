package app

import (
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
)

type disabledFeedTrap struct{ dbCalls, sourceCalls int }

func (x *disabledFeedTrap) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	x.dbCalls++
	panic("disabled feed reached database")
}
func (x *disabledFeedTrap) PublishedRecords(context.Context, string, uuid.UUID, string) ([]json.RawMessage, error) {
	x.sourceCalls++
	panic("disabled feed fetched publication source")
}

func TestDisabledFeedStopsBeforeSourceDatabaseAndSnapshotSigner(t *testing.T) {
	trap := &disabledFeedTrap{}
	name := port.FeedPriceBoardDaily
	svc := &Service{DB: trap, DisabledFeeds: map[string]bool{name: true},
		PublishedSources: map[string]port.PublishedSource{name: trap}, Publishers: map[string][]port.Publisher{name: {{Tenant: ids.New(), Code: "89"}}}}
	ct, err := FeedContract(name, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, buildErr := svc.BuildPublished(context.Background(), name)
	_, readErr := svc.ReadFeed(context.Background(), ct, "", nil, 10)
	_, snapshotErr := svc.SnapshotURL(context.Background(), name, 1)
	_, checksumErr := svc.DatasetChecksums(context.Background(), name, 1, 10)
	for _, err := range []error{buildErr, readErr, snapshotErr, checksumErr} {
		if problem.From(err).Code != port.ErrFeedDisabled.Code || problem.From(err).Status != 403 {
			t.Fatalf("disabled feed: %v", err)
		}
	}
	if trap.dbCalls != 0 || trap.sourceCalls != 0 {
		t.Fatalf("disabled feed reached work: %+v", trap)
	}
}
