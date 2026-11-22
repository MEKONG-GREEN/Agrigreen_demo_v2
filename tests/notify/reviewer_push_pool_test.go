package notify_test

import (
	"context"
	"fmt"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestReviewerNotifyPushPoolBoundary(t *testing.T) {
	for _, size := range []int32{1, 2} {
		t.Run(fmt.Sprintf("pool_%d", size), func(t *testing.T) {
			f := newFixture(t)
			a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
			a.device(fcmToken)
			source, _ := f.directive(f.tenant, port.SeverityUrgent, []string{ward1}, a.actor)
			f.process(extport.NotifyConsumerDirective, source)
			var batch uuid.UUID
			if e := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
				return tx.QueryRow(context.Background(), `SELECT d.batch_id FROM pf_notify.push_delivery d JOIN pf_notify.notification n ON n.tenant_id=d.tenant_id AND n.id=d.notification_id WHERE n.source_event_id=$1 LIMIT 1`, source.EventID).Scan(&batch)
			}); e != nil {
				t.Fatal(e)
			}
			pool := pgtest.Get(t).SizedPool(t, migrations.PF, dbadmin.AppPlatform, size)
			mod := notify.New(pool, f.clk, notify.Deps{Pusher: f.pusher, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
			event := f.event(f.tenant, port.EventPushRequested, port.PushRequestedV1{BatchID: batch, Kind: "reviewer", Deliveries: 1})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := inbox.Process(ctx, db.ForModule(pool, notify.DBModule), port.ConsumerPush, event, mod.PushHandler())
			if err != nil {
				t.Fatalf("push must finish without needing a second pool connection (max=%d wait=%v): %v", size, pool.Stat().EmptyAcquireWaitTime(), err)
			}
			if len(f.pusher.messages()) != 1 {
				t.Fatalf("expected exactly one push, got %d", len(f.pusher.messages()))
			}
		})
	}
}
