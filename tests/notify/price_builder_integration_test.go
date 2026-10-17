package notify_test

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
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

var errFixturePriceLookup = errors.New("fixture price directory unavailable")

type retryPriceDirectory struct {
	fail      bool
	recipient uuid.UUID
	tenants   []uuid.UUID
}

func (d *retryPriceDirectory) ProvinceOfficers(_ context.Context, tid uuid.UUID, _ string) ([]uuid.UUID, error) {
	d.tenants = append(d.tenants, tid)
	if d.fail {
		return nil, errFixturePriceLookup
	}
	return []uuid.UUID{d.recipient, d.recipient, uuid.Nil}, nil
}
func (d *retryPriceDirectory) Stewards(_ context.Context, tid uuid.UUID, _ string) ([]uuid.UUID, error) {
	d.tenants = append(d.tenants, tid)
	return []uuid.UUID{d.recipient}, nil
}

func TestPriceOwnedBuilderLookupRetryTenantAndRedaction(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{prov91}}, partyport.RoleKNProvinceAdmin)
	foreign := f.asActor(ids.New(), ids.New(), a.actor, partyport.RoleKNProvinceAdmin)
	directory := &retryPriceDirectory{fail: true, recipient: a.actor}
	f.mod = notify.New(f.appDB, f.clk, notify.Deps{Scope: f.scope,
		Consumers: []notifyport.ConsumerRegistration{price.NotificationConsumers()}, Builders: price.NotificationBuilders(directory)})
	e := f.event(f.tenant, priceport.EventAlertRaised, priceport.AlertRaisedV1{Profile: "kn_fixture", Day: "2027-10-13", PointName: "Xã thử 0901234567", TicketID: ids.New(), ItemCodes: []string{"A"}})
	if _, err := inbox.Process(context.Background(), f.appDB, priceport.NotifyConsumerPrice, e, f.mod.EventHandler()); !errors.Is(err, errFixturePriceLookup) {
		t.Fatalf("lookup error identity: %v", err)
	}
	if len(a.inbox()) != 0 {
		t.Fatal("lookup failure wrote notification")
	}
	directory.fail = false
	if !f.process(priceport.NotifyConsumerPrice, e) || f.process(priceport.NotifyConsumerPrice, e) {
		t.Fatal("lookup retry/replay not idempotent")
	}
	got := a.inbox()
	if len(got) != 1 || got[0]["kind"] != priceport.KindPriceAlert || got[0]["deep_link"] != "/kn/bao-gia/kiem-duyet" || strings.Contains(got[0]["title"].(string), "0901234567") || !strings.Contains(got[0]["title"].(string), "***") {
		t.Fatalf("builder delivery/redaction: %v", got)
	}
	if len(foreign.inbox()) != 0 {
		t.Fatal("registered builder escaped tenant")
	}
	if len(directory.tenants) != 3 {
		t.Fatalf("unexpected lookup calls: %v", directory.tenants)
	}
	for _, tid := range directory.tenants {
		if tid != f.tenant {
			t.Fatal("lookup used wrong tenant")
		}
	}
}
