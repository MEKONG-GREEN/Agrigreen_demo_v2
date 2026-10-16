package handler_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// KN US-HTX-05: nông hộ mới đăng ký từ App HTX / đổi HTX → cán bộ khuyến nông cấp xã
// của xã (danh bạ); xã chưa có ai → chủ đề vai kn_officer của xã. Nhập tệp không báo.
func TestHouseholdEventsNotifyCommuneOfficerKNUSHTX05(t *testing.T) {
	f := newFixture(t)
	off := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	other := f.user(authz.Scope{Units: []string{ward2}}, partyport.RoleKNOfficer)
	prov := f.user(authz.Scope{Units: []string{prov91, ward1, ward2}}, partyport.RoleKNProvinceAdmin)
	f.staff.add(f.tenant, port.StaffRef{ActorRef: off.actor, UnitCode: ward1, ProvinceCode: prov91})
	f.staff.add(f.tenant, port.StaffRef{ActorRef: prov.actor, UnitCode: prov91, ProvinceCode: prov91})
	hh := ids.New()
	coop := ids.New()
	reg := func(commune, src string) {
		f.process(port.ConsumerParty, f.event(f.tenant, coopport.EventHouseholdRegistered, coopport.HouseholdRegisteredV1{
			HouseholdRef: hh, CommuneCode: commune, CooperativeRef: &coop, Source: src, ActorRef: ids.New(), EventTime: f.clk.Now().Format(time.RFC3339)}))
	}
	reg(ward1, coopport.HouseholdSourceHTXApp)
	in := off.inbox()
	if len(in) != 1 || in[0]["kind"] != port.KindHouseholdRegistered || in[0]["deep_link"] != "/households/"+hh.String() || in[0]["topic"] != false {
		t.Fatalf("cán bộ xã: %v", in)
	}
	if len(prov.inbox()) != 0 || len(other.inbox()) != 0 {
		t.Fatal("chỉ cán bộ cấp xã của xã nhận")
	}
	reg(ward1, coopport.HouseholdSourceImport)
	if len(off.inbox()) != 1 {
		t.Fatal("nhập tệp không được báo")
	}
	// Xã 30301 chưa có cán bộ trong danh bạ → chủ đề kn_officer của xã.
	f.process(port.ConsumerParty, f.event(f.tenant, coopport.EventHouseholdMembershipChanged, coopport.HouseholdMembershipChangedV1{
		HouseholdRef: hh, CommuneCode: ward2, PrevCooperativeRef: &coop, Change: coopport.MembershipLeft, EffectiveDate: "2026-09-29",
		ActorRef: ids.New(), EventTime: f.clk.Now().Format(time.RFC3339)}))
	if in := other.inbox(); len(in) != 1 || in[0]["kind"] != port.KindHouseholdMembershipChanged || !strings.Contains(in[0]["title"].(string), "rời HTX") {
		t.Fatalf("chủ đề xã: %v", in)
	}
}

// KN US-TASK-01: nhiệm vụ tự sinh → cán bộ được giao nhận thông báo đẩy; không ai
// được giao → quản trị tỉnh (chủ đề) gán lại.
func TestTaskCreatedNotifiesAssigneeKNUSTASK01(t *testing.T) {
	f := newFixture(t)
	off := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	off.device(fcmToken)
	prov := f.user(authz.Scope{Units: []string{prov91, ward1, ward2}}, partyport.RoleKNProvinceAdmin)
	created := func(assignee *uuid.UUID, rule string) {
		f.process(port.ConsumerTask, f.event(f.tenant, extport.EventTaskCreated, extport.TaskCreatedV1{TaskID: ids.New(), Source: "htx_app",
			CommuneCode: ward1, ProvinceCode: prov91, Category: "sau_benh", Priority: "urgent", Status: "assigned", AssigneeRef: assignee,
			AssignmentRule: rule, DueAt: f.clk.Now().Add(24 * time.Hour).Format(time.RFC3339), CreatedAt: f.clk.Now().Format(time.RFC3339), Seq: 1}))
	}
	created(&off.actor, extport.AssignCommuneOnDuty)
	if in := off.inbox(); len(in) != 1 || in[0]["kind"] != port.KindTaskAssigned || in[0]["severity"] != "urgent" {
		t.Fatalf("người được giao: %v", in)
	}
	if err := f.flushPush(f.tenant); err != nil {
		t.Fatal(err)
	}
	if msgs := f.pusher.messages(); len(msgs) != 1 || msgs[0].Kind != port.KindTaskAssigned {
		t.Fatalf("đẩy: %v", msgs)
	}
	created(nil, extport.AssignAdminQueue)
	if in := prov.inbox(); len(in) != 1 || in[0]["topic"] != true || !strings.Contains(in[0]["title"].(string), "cần phân công") {
		t.Fatalf("quản trị tỉnh: %v", in)
	}
}

// KN US-SYNC-02 AC: đồng bộ ngay theo nhóm dữ liệu — entities[] lưu ở nudge và đi
// trong thông điệp sync_now (khóa "entities"); không có → như cũ (chỉ kind).
func TestNudgeEntitiesFilterKNUSSYNC02(t *testing.T) {
	f := newFixture(t)
	f.mod.UseSyncEntities([]string{"catalog_item", "plot", "task"})
	off := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	dev := off.device(fcmToken)
	prov := f.user(authz.Scope{Units: []string{prov91, ward1, ward2}}, partyport.RoleKNProvinceAdmin)
	if r := prov.post("/v1/sync/nudge", map[string]any{"device_id": dev.String(), "entities": []string{"khong_co"}}); r.status != http.StatusUnprocessableEntity || code(r) != "notify.nudge_entity_unknown" {
		t.Fatalf("entity lạ: %d %v", r.status, r.body)
	}
	r := prov.post("/v1/sync/nudge", map[string]any{"device_id": dev.String(), "entities": []string{"task", "plot", "task"}})
	if r.status != http.StatusAccepted || len(r.body["entities"].([]any)) != 2 {
		t.Fatalf("nudge có lọc: %d %v", r.status, r.body)
	}
	if err := f.flushPush(f.tenant); err != nil {
		t.Fatal(err)
	}
	msgs := f.pusher.messages()
	if len(msgs) != 1 || msgs[0].Data()["entities"] != "plot,task" || msgs[0].Data()["kind"] != port.KindSyncNow {
		t.Fatalf("thông điệp: %v", msgs)
	}
	f.clk.Advance(6 * time.Minute)
	prov.post("/v1/sync/nudge", map[string]any{"device_id": dev.String()})
	_ = f.flushPush(f.tenant)
	if msgs := f.pusher.messages(); len(msgs) != 2 || len(msgs[1].Data()) != 1 {
		t.Fatalf("không lọc → chỉ kind: %v", msgs[1].Data())
	}
}
