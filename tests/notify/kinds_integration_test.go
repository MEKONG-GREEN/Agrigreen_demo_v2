package notify_test

import (
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
	"time"
)

func kindFixtureConsumer() port.ConsumerRegistration {
	return port.ConsumerRegistration{Definitions: []port.ConsumerDefinition{{Name: "fixture.kind", Order: 1, Events: []string{"fixture.kind.created"}}}}
}
func TestRegisteredKindVocabularyPersistsAndUnknownRollsBack(t *testing.T) {
	f := newFixture(t)
	actor := f.user(authz.Scope{Units: []string{prov91}}, partyport.RoleKNOfficer)
	var old struct{ Kinds []string }
	raw, err := os.ReadFile("../golden/kn-notify-kinds.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if len(old.Kinds) != 43 {
		t.Fatal("missing kind vocabulary")
	}
	selected := ""
	f.mod = notify.New(f.appDB, f.clk, notify.Deps{Scope: f.scope, Kinds: []port.KindRegistration{extension.NotificationKinds(), price.NotificationKinds()}, Consumers: []port.ConsumerRegistration{kindFixtureConsumer()}, Renderers: []port.EventRendererRegistration{{Events: []string{"fixture.kind.created"}, Render: func(string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) {
		return []port.NotificationDraft{{RecipientRef: &actor.actor, Kind: selected, Title: "Fixture", DeepLink: "/fixture", CreatedAt: f.clk.Now()}}, nil
	}}}})
	for _, kind := range old.Kinds {
		selected = kind
		if !f.process("fixture.kind", f.event(f.tenant, "fixture.kind.created", map[string]string{"kind": kind})) {
			t.Fatal("registered kind not consumed")
		}
	}
	count := func() int {
		t.Helper()
		var n int
		if err := tenant.WithTenantTx(context.Background(), f.appDB, f.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_notify.notification WHERE source_event_type='fixture.kind.created'").Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count() != 43 {
		t.Fatal("registered legacy vocabulary changed")
	}
	selected = "fixture_kind"
	event := f.event(f.tenant, "fixture.kind.created", map[string]string{"kind": selected})
	if _, err := f.mod.EventHandler()(context.Background(), nil, event); err == nil {
		t.Fatal("unregistered kind reached DB")
	}
	if count() != 43 {
		t.Fatal("unknown kind wrote notification")
	}
	selected = old.Kinds[0]
	if !f.process("fixture.kind", event) || f.process("fixture.kind", event) {
		t.Fatal("kind rejection/retry replay")
	}
	if count() != 44 {
		t.Fatal("kind retry not persisted exactly once")
	}
}
func TestMissingOwnedKindDeclarationCannotWrite(t *testing.T) {
	recipient := ids.New()
	module := notify.New(nil, clock.System{}, notify.Deps{Consumers: []port.ConsumerRegistration{kindFixtureConsumer()}, Renderers: []port.EventRendererRegistration{{Events: []string{"fixture.kind.created"}, Render: func(string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) {
		return []port.NotificationDraft{{RecipientRef: &recipient, Kind: extport.KindDirective, Title: "Fixture", DeepLink: "/fixture"}}, nil
	}}}})
	if err := module.EventHandler()(context.Background(), nil, outbox.Envelope{TenantID: uuid.New(), EventType: "fixture.kind.created"}); err == nil {
		t.Fatal("unregistered owned kind wrote DB")
	}
}
