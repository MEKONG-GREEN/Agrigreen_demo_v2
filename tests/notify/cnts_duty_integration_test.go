package notify_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// CNTS-DUTY-NOTIFY-001: inbox thật, đúng người bị đổi ca; không broadcast, không gửi trùng; push chỉ dữ liệu.
func TestCNTSDutyChanged_W1146(t *testing.T) {
	f := newFixture(t)
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	b := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	unrelated := f.user(authz.Scope{All: true}, partyport.RoleKNProvinceAdmin)
	foreign := f.asActor(ids.New(), ids.New(), a.actor, partyport.RoleKNOfficer)
	a.device(fcmToken)
	b.device("fcm-token-duty-officer-b-000000000000000")
	roster := ids.New()
	e := f.event(f.tenant, extport.EventCNTSDutyChanged, extport.CNTSDutyChangedV1{Month: "2027-10", RosterID: roster, Version: 2,
		OfficerRefs: []uuid.UUID{a.actor, b.actor, a.actor, uuid.Nil}, ActorRef: unrelated.actor, Department: partyport.DeptChanNuoiThuySan, EventTime: f.clk.Now().Format(time.RFC3339)})
	subscribed := false
	for _, c := range f.mod.Consumers() {
		if c.Name == extport.NotifyConsumerCNTS {
			for _, ev := range c.Events {
				if ev == e.EventType {
					subscribed = true
				}
			}
		}
	}
	if !subscribed {
		t.Fatal("worker chưa đăng ký consumer đổi ca")
	}
	if !f.process(extport.NotifyConsumerCNTS, e) || f.process(extport.NotifyConsumerCNTS, e) {
		t.Fatal("giao lại phải bị inbox bỏ qua")
	}
	for _, u := range []caller{a, b} {
		ns := u.inbox()
		if len(ns) != 1 || ns[0]["kind"] != port.KindCNTSDutyChanged || ns[0]["deep_link"] != "/kn/cnts/lich-truc" {
			t.Fatalf("hộp thư sai: %v", ns)
		}
	}
	if len(unrelated.inbox()) != 0 || len(foreign.inbox()) != 0 {
		t.Fatal("đã phát thông báo ra ngoài người nhận")
	}
	if err := f.flushPush(f.tenant); err != nil {
		t.Fatal(err)
	}
	if len(f.pusher.messages()) != 2 {
		t.Fatal("mỗi thiết bị phải nhận đúng một lần")
	}
	for _, msg := range f.pusher.messages() {
		if len(msg.Data()) != 2 || msg.Data()["kind"] != port.KindCNTSDutyChanged || msg.Data()["notification_id"] == "" {
			t.Fatalf("push chứa dữ liệu ngoài hợp đồng: %+v", msg)
		}
	}
}
