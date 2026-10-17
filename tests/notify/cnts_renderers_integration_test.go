package notify_test

import (
	"context"
	"encoding/json"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
	"time"
)

func TestCNTSRegisteredAlertAndReminder(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	admin := f.user(authz.Scope{Units: []string{prov91}}, partyport.RoleKNProvinceAdmin)
	outsider := f.user(authz.Scope{Units: []string{ward2}}, partyport.RoleKNOfficer)
	foreign := f.asActor(ids.New(), ids.New(), a.actor, partyport.RoleKNOfficer)
	alert := extport.CNTSAlertChangedV1{AlertRef: ids.New(), Action: extport.CNTSAlertAssigned, Level: "urgent",
		Title: "Cảnh báo 0901234567", ProvinceCode: stringRef(prov91), RecipientRefs: []uuid.UUID{a.actor, a.actor, uuid.Nil},
		DueAt: f.clk.Now().Format(time.RFC3339), EventTime: f.clk.Now().Format(time.RFC3339)}
	e := f.event(f.tenant, extport.EventCNTSAlertChanged, alert)
	if !f.process(extport.NotifyConsumerCNTS, e) || f.process(extport.NotifyConsumerCNTS, e) {
		t.Fatal("registered alert inbox replay")
	}
	got := a.inbox()
	if len(got) != 1 || got[0]["kind"] != port.KindCNTSAlertAssigned || strings.Contains(got[0]["body"].(string), "0901234567") || !strings.Contains(got[0]["body"].(string), "***") {
		t.Fatalf("registered alert/redaction: %v", got)
	}
	alert.AlertRef = ids.New()
	alert.RecipientRefs = nil
	f.process(extport.NotifyConsumerCNTS, f.event(f.tenant, extport.EventCNTSAlertChanged, alert))
	if len(admin.inbox()) != 1 || len(outsider.inbox()) != 0 || len(foreign.inbox()) != 0 {
		t.Fatal("registered topic escaped scope/tenant")
	}
	reminder := extport.CNTSFormRemindedV1{PeriodRef: ids.New(), Slot: extport.CNTSReminderRepeat, CommuneCode: ward1,
		Message: "Nhắc nộp báo cáo", Channels: []string{"in_app", "email"}, RecipientRefs: []uuid.UUID{a.actor}, EventTime: f.clk.Now().Format(time.RFC3339)}
	f.process(extport.NotifyConsumerCNTS, f.event(f.tenant, extport.EventCNTSFormReminded, reminder))
	if len(a.inbox()) != 2 || len(admin.inbox()) != 1 || len(outsider.inbox()) != 0 {
		t.Fatal("registered reminder delivery scope")
	}
	var emails int
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_notify.email_delivery WHERE recipient_ref=$1 AND kind IN ($2,$3)", a.actor, port.KindCNTSAlertAssigned, port.KindCNTSFormReminder).Scan(&emails)
	}); err != nil {
		t.Fatal(err)
	}
	if emails != 2 {
		t.Fatalf("direct urgent/reminder emails, no topic mail: %d", emails)
	}
}
func stringRef(s string) *string { return &s }

func TestRegisteredRendererKeepsDeliveryGuards(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	b := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	foreign := f.asActor(ids.New(), ids.New(), a.actor, partyport.RoleKNOfficer)
	calls := 0
	f.mod = notify.New(f.appDB, f.clk, notify.Deps{Scope: f.scope,
		Consumers: []port.ConsumerRegistration{{Definitions: []port.ConsumerDefinition{{Name: "fixture.notify", Order: 1, Events: []string{"fixture.notification.rendered"}}}}},
		Renderers: []port.EventRendererRegistration{{Events: []string{"fixture.notification.rendered"}, Render: func(string, json.RawMessage, time.Time) ([]port.NotificationDraft, error) {
			calls++
			return []port.NotificationDraft{
				{RecipientRef: &a.actor, Kind: port.KindCNTSDutyChanged, Title: "  Liên hệ 0901234567  ", Body: "  Nội dung  ", DeepLink: "/kn/cnts/lich-truc", CreatedAt: f.clk.Now()},
				{RecipientRef: &b.actor, Kind: port.KindCNTSDutyChanged, Title: "Invalid link", DeepLink: "https://external.invalid", CreatedAt: f.clk.Now()},
			}, nil
		}}},
	})
	e := f.event(f.tenant, "fixture.notification.rendered", map[string]any{})
	if !f.process("fixture.notify", e) || f.process("fixture.notify", e) || calls != 1 {
		t.Fatal("renderer bypassed inbox dedup")
	}
	got := a.inbox()
	if len(got) != 1 || got[0]["title"] != "Liên hệ ***" || got[0]["body"] != "Nội dung" || got[0]["severity"] != "normal" || got[0]["id"] == uuid.Nil.String() {
		t.Fatalf("normalization/identity: %v", got)
	}
	if len(b.inbox()) != 0 || len(foreign.inbox()) != 0 {
		t.Fatal("invalid link/foreign tenant received")
	}
	var event uuid.UUID
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT source_event_id FROM pf_notify.notification WHERE recipient_ref=$1", a.actor).Scan(&event)
	}); err != nil {
		t.Fatal(err)
	}
	if event != e.EventID {
		t.Fatal("source provenance lost")
	}
}
