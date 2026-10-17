package notify_test

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/google/uuid"
	"strings"
	"testing"
)

var errFixtureTaskLookup = errors.New("fixture task directory unavailable")

type retryTaskDirectory struct {
	fail      bool
	recipient uuid.UUID
	tenants   []uuid.UUID
}

func (d *retryTaskDirectory) ActiveStaff(_ context.Context, tid uuid.UUID, _ []string) ([]notifyport.StaffRef, error) {
	d.tenants = append(d.tenants, tid)
	if d.fail {
		return nil, errFixtureTaskLookup
	}
	record := notifyport.StaffRef{ActorRef: d.recipient, UnitCode: prov91, ProvinceCode: prov91}
	return []notifyport.StaffRef{record, record}, nil
}
func TestTaskOwnedBuilderLookupRetryTenantAndRedaction(t *testing.T) {
	f := newFixture(t)
	actor := f.user(authz.Scope{Units: []string{prov91}}, partyport.RoleKNProvinceAdmin)
	foreign := f.asActor(ids.New(), ids.New(), actor.actor, partyport.RoleKNProvinceAdmin)
	directory := &retryTaskDirectory{fail: true, recipient: actor.actor}
	f.mod = notify.New(f.appDB, f.clk, notify.Deps{Scope: f.scope, Consumers: []notifyport.ConsumerRegistration{extension.NotificationConsumers()}, Builders: extension.TaskNotificationBuilders(directory)})
	taskID := ids.New()
	event := f.event(f.tenant, extport.EventTaskEscalated, extport.TaskEscalatedV1{TaskID: taskID, Priority: "high", ProvinceCode: prov91, CommuneCode: "0901234567"})
	if _, err := inbox.Process(context.Background(), f.appDB, extport.NotifyConsumerTask, event, f.mod.EventHandler()); !errors.Is(err, errFixtureTaskLookup) {
		t.Fatalf("lookup identity: %v", err)
	}
	if len(actor.inbox()) != 0 {
		t.Fatal("lookup failure wrote notification")
	}
	directory.fail = false
	if !f.process(extport.NotifyConsumerTask, event) || f.process(extport.NotifyConsumerTask, event) {
		t.Fatal("lookup retry/replay not idempotent")
	}
	rows := actor.inbox()
	if len(rows) != 1 || rows[0]["kind"] != notifyport.KindTaskEscalated || rows[0]["deep_link"] != "/extension/tasks/"+taskID.String() || strings.Contains(rows[0]["body"].(string), "0901234567") || !strings.Contains(rows[0]["body"].(string), "***") {
		t.Fatalf("owner delivery/redaction: %v", rows)
	}
	if len(foreign.inbox()) != 0 {
		t.Fatal("task builder escaped tenant")
	}
	if len(directory.tenants) != 2 {
		t.Fatalf("lookup count: %v", directory.tenants)
	}
	for _, tid := range directory.tenants {
		if tid != f.tenant {
			t.Fatal("lookup used wrong tenant")
		}
	}
}
