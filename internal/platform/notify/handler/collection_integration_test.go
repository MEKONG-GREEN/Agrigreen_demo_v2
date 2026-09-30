package handler_test

import (
	"strings"
	"testing"

	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// RB-6 chiều mua (G3-2): lịch gom / bảng kê số cuộn MKG mới → quản lý HTX của đúng HTX nhận
// thông báo (mở /htx/thu-gom); HTX khác không nhận; giao lặp không trùng.
func TestCollectionNotifications_RB6_BuySide(t *testing.T) {
	f := newFixture(t)
	coopA, coopB := ids.New(), ids.New()
	mgrA := f.userIn(f.tenant, coopA, authz.Scope{Units: []string{ward1}}, partyport.RoleCoopManager)
	mgrB := f.userIn(f.tenant, coopB, authz.Scope{Units: []string{ward1}}, partyport.RoleCoopManager)
	sched := f.event(f.tenant, ccport.EventCollectionScheduleReceived, ccport.CollectionScheduleReceivedV1{
		ScheduleRef: ids.New(), CooperativeRef: coopA, CommuneCode: ward1, WindowFrom: "2026-10-01", WindowTo: "2026-10-05",
		Status: "planned", Revision: 1, First: true})
	f.process(port.ConsumerCropCycle, sched)
	f.process(port.ConsumerCropCycle, sched)
	f.process(port.ConsumerCropCycle, f.event(f.tenant, ccport.EventCollectionStatementReceived, ccport.CollectionStatementReceivedV1{
		StatementRef: ids.New(), CooperativeRef: coopA, PlotRef: ids.New(), CollectedOn: "2026-09-29", Rolls: 120, Revision: 2}))
	in := mgrA.inbox()
	if len(in) != 2 {
		t.Fatalf("quản lý HTX A: %v", in)
	}
	kinds := map[any]map[string]any{}
	for _, n := range in {
		kinds[n["kind"]] = n
	}
	s, st := kinds[port.KindCollectionSchedule], kinds[port.KindCollectionStatement]
	if s == nil || st == nil || s["deep_link"] != "/htx/thu-gom" || !strings.Contains(s["body"].(string), "01/10/2026") ||
		!strings.Contains(st["title"].(string), "cần xác nhận lại") || !strings.Contains(st["body"].(string), "120 cuộn") {
		t.Fatalf("nội dung thông báo: %v", in)
	}
	if len(mgrB.inbox()) != 0 {
		t.Fatal("HTX khác không nhận thông báo thu gom")
	}
}
