package handler_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	contentport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// fakeAreaFarmers: nông dân theo xã (thay pf/geo + pf/accounts).
type fakeAreaFarmers struct {
	mu    sync.Mutex
	byTid map[uuid.UUID]map[string][]uuid.UUID
	calls [][]string
}

func (f *fakeAreaFarmers) add(tid uuid.UUID, commune string, farmer uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byTid[tid] == nil {
		f.byTid[tid] = map[string][]uuid.UUID{}
	}
	f.byTid[tid][commune] = append(f.byTid[tid][commune], farmer)
}

func (f *fakeAreaFarmers) FarmersInCommunes(_ context.Context, tid uuid.UUID, communes []string) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, communes)
	var out []uuid.UUID
	for c, refs := range f.byTid[tid] {
		if communes == nil || slices.Contains(communes, c) {
			out = append(out, refs...)
		}
	}
	return out, nil
}

// HTX US-LOG-05: nông dân ghi nhật ký → quản lý HTX của đúng HTX nhận "nhật ký chờ
// duyệt" (chủ đề HTX + vai coop_manager); ghi hộ không báo; duyệt / yêu cầu bổ sung →
// người ghi nhận thông báo cá nhân (AC-3) kèm đẩy; giao lặp không trùng.
func TestFarmLogNotificationsHTXUSLOG05(t *testing.T) {
	f := newFixture(t)
	coopA, coopB := ids.New(), ids.New()
	mgrA := f.userIn(f.tenant, coopA, authz.Scope{Units: []string{ward1}}, partyport.RoleCoopManager)
	mgrB := f.userIn(f.tenant, coopB, authz.Scope{Units: []string{ward1}}, partyport.RoleCoopManager)
	officer := f.userIn(f.tenant, coopA, authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	farmer := f.asActor(f.tenant, uuid.Nil, ids.New(), coopport.RoleFarmer)
	farmer.device(fcmToken)

	logID, logRef := ids.New(), ids.New()
	hh := ids.New()
	rec := func(onBehalf bool) {
		f.process(port.ConsumerCropCycle, f.event(f.tenant, coopport.EventFarmLogRecorded, coopport.FarmLogRecordedV1{
			LogID: logID, LogRef: logRef, Version: 1, SeasonRef: ids.New(), PlotRef: ids.New(), HouseholdRef: &hh, CooperativeRef: coopA,
			CommuneCode: ward1, ActivityCode: "bon_phan", LocationStatus: "mismatch", GPSSource: "device", BecameCurrent: true,
			OnBehalf: onBehalf, AuthorRef: farmer.actor, EventTime: f.clk.Now().Format(time.RFC3339)}))
	}
	rec(false)
	in := mgrA.inbox()
	if len(in) != 1 || in[0]["kind"] != port.KindFarmLogPending || in[0]["topic"] != true ||
		in[0]["deep_link"] != "/cropcycle/farm-logs/"+logID.String() || !strings.Contains(in[0]["body"].(string), "cảnh báo vị trí") {
		t.Fatalf("quản lý HTX A: %v", in)
	}
	if len(mgrB.inbox()) != 0 || len(officer.inbox()) != 0 || len(farmer.inbox()) != 0 {
		t.Fatal("chỉ quản lý của đúng HTX nhận nhật ký chờ duyệt")
	}
	// Ghi hộ (quản lý HTX ghi) → không báo.
	logID = ids.New()
	rec(true)
	if len(mgrA.inbox()) != 1 {
		t.Fatal("nhật ký ghi hộ không được báo")
	}

	reviewer := ids.New()
	note := "Bổ sung ảnh chụp bao phân"
	review := func(decision string) bool {
		return f.process(port.ConsumerCropCycle, f.event(f.tenant, coopport.EventFarmLogReviewed, coopport.FarmLogReviewedV1{
			ReviewID: ids.New(), LogID: logID, LogRef: logRef, SeasonRef: ids.New(), CooperativeRef: coopA, Decision: decision,
			Note: &note, AuthorRef: farmer.actor, ReviewerRef: reviewer, EventTime: f.clk.Now().Format(time.RFC3339)}))
	}
	review(coopport.LogNeedsSupplement)
	in = farmer.inbox()
	if len(in) != 1 || in[0]["kind"] != port.KindFarmLogReviewed || in[0]["topic"] != false || !strings.Contains(in[0]["title"].(string), "cần bổ sung") ||
		strings.Contains(in[0]["body"].(string), note) {
		t.Fatalf("nông dân nhận yêu cầu bổ sung (không lộ ghi chú): %v", in)
	}
	if err := f.flushPush(f.tenant); err != nil {
		t.Fatal(err)
	}
	if msgs := f.pusher.messages(); len(msgs) != 1 || msgs[0].Kind != port.KindFarmLogReviewed {
		t.Fatalf("đẩy tới máy nông dân: %v", msgs)
	}
	// Giao lặp cùng sự kiện → không trùng.
	env := f.event(f.tenant, coopport.EventFarmLogReviewed, coopport.FarmLogReviewedV1{ReviewID: ids.New(), LogID: logID, LogRef: logRef,
		CooperativeRef: coopA, Decision: coopport.LogApproved, AuthorRef: farmer.actor, ReviewerRef: reviewer, EventTime: f.clk.Now().Format(time.RFC3339)})
	if !f.process(port.ConsumerCropCycle, env) || f.process(port.ConsumerCropCycle, env) {
		t.Fatal("inbox phải bỏ qua lần giao lặp")
	}
	if in := farmer.inbox(); len(in) != 2 || !strings.Contains(in[0]["title"].(string), "đã được duyệt") {
		t.Fatalf("phê duyệt: %v", in)
	}
	// Đường nông dân của pf/sync (App HTX): chỉ thông báo cá nhân, kể cả khi pf/sync truyền xã của nông hộ.
	fs := f.mod.SyncSource().(syncport.FarmerSource)
	ch, _, err := fs.FarmerChanges(farmer.ctx(), syncport.FarmerScope{Households: []uuid.UUID{hh}, Communes: []string{ward1}}, port.EntityNotification, 0, 100)
	if err != nil || len(ch) != 2 {
		t.Fatalf("pull nông dân: %v %d", err, len(ch))
	}
	// Tenant khác không thấy.
	other := f.asActor(ids.New(), uuid.Nil, farmer.actor, coopport.RoleFarmer)
	if len(other.inbox()) != 0 {
		t.Fatal("cách ly tenant")
	}
}

// HTX US-SEASON-01: nông dân tự mở vụ → quản lý HTX; quản lý mở vụ hộ → không báo.
func TestSeasonOpenedByFarmerNotifiesManagerHTXUSSEASON01(t *testing.T) {
	f := newFixture(t)
	coop := ids.New()
	mgr := f.userIn(f.tenant, coop, authz.Scope{Units: []string{ward1}}, partyport.RoleCoopManager)
	open := func(onBehalf bool) {
		f.process(port.ConsumerCropCycle, f.event(f.tenant, coopport.EventSeasonOpened, coopport.SeasonOpenedV1{
			SeasonRef: ids.New(), EventID: ids.New(), PlotRef: ids.New(), PlotCode: "TH01", CooperativeRef: coop, CommuneCode: ward1,
			SeasonCode: "HT", SeasonYear: 2026, VarietyCode: "OM5451", SOPVersionID: ids.New(), SowingDate: "2026-09-20",
			AreaHa: "1.5", OnBehalf: onBehalf, ActorRef: ids.New(), EventTime: f.clk.Now().Format(time.RFC3339)}))
	}
	open(false)
	open(true)
	in := mgr.inbox()
	if len(in) != 1 || in[0]["kind"] != port.KindSeasonOpened || !strings.Contains(in[0]["body"].(string), "OM5451") {
		t.Fatalf("quản lý HTX nhận vụ mới: %v", in)
	}
}

// HTX US-NEWS-03: cảnh báo khẩn tới nông dân có thửa trong địa bàn (thông báo cá nhân +
// đẩy), không tới nông dân ngoài địa bàn; quản lý HTX của HTX trong địa bàn nhận qua
// chủ đề theo xã; cảnh báo cấp tỉnh mở rộng ra xã; toàn quốc → mọi nông dân của tenant.
func TestAlertReachesFarmersAndCoopManagersInAreaHTXUSNEWS03(t *testing.T) {
	f := newFixture(t)
	farmers := &fakeAreaFarmers{byTid: map[uuid.UUID]map[string][]uuid.UUID{}}
	f.mod.UseAreaFarmers(farmers)
	in1 := f.asActor(f.tenant, uuid.Nil, ids.New(), coopport.RoleFarmer)
	in1.device(fcmToken)
	out := f.asActor(f.tenant, uuid.Nil, ids.New(), coopport.RoleFarmer)
	farmers.add(f.tenant, ward1, in1.actor)
	farmers.add(f.tenant, ward1, in1.actor) // hai thửa, một tài khoản → một thông báo
	farmers.add(f.tenant, ward92, out.actor)
	mgrIn := f.userIn(f.tenant, ids.New(), authz.Scope{Units: []string{ward1}}, partyport.RoleCoopManager)
	mgrOut := f.userIn(f.tenant, ids.New(), authz.Scope{Units: []string{ward92}}, partyport.RoleCoopManager)

	alert := func(level string, units []string, title string) {
		f.process(port.ConsumerContent, f.event(f.tenant, contentport.EventAlertPublished, contentport.AlertPublishedV1{
			DocumentRef: ids.New().String(), VersionNo: 1, Title: title, Summary: "Thăm đồng, phun theo khuyến cáo",
			AudienceLevel: level, AudienceUnits: units, PublisherRef: ids.New().String(), PublishedAt: f.clk.Now().Format(time.RFC3339)}))
	}
	alert("province", []string{prov91}, "Rầy nâu bùng phát")
	if !slices.Contains(farmers.calls[0], ward1) {
		t.Fatalf("tỉnh phải mở rộng ra xã: %v", farmers.calls)
	}
	got := in1.inbox()
	if len(got) != 1 || got[0]["kind"] != port.KindAlert || got[0]["severity"] != "urgent" || got[0]["topic"] != false || got[0]["title"] != "Rầy nâu bùng phát" {
		t.Fatalf("nông dân trong địa bàn: %v", got)
	}
	if len(out.inbox()) != 0 || len(mgrOut.inbox()) != 0 {
		t.Fatal("ngoài địa bàn không nhận")
	}
	if m := mgrIn.inbox(); len(m) != 1 || m[0]["topic"] != true {
		t.Fatalf("quản lý HTX trong địa bàn: %v", m)
	}
	if err := f.flushPush(f.tenant); err != nil {
		t.Fatal(err)
	}
	if msgs := f.pusher.messages(); len(msgs) != 1 || msgs[0].Kind != port.KindAlert {
		t.Fatalf("đẩy tới máy nông dân: %v", msgs)
	}
	alert("national", nil, "Bão số 5")
	if farmers.calls[1] != nil || len(out.inbox()) != 1 || len(in1.inbox()) != 2 {
		t.Fatalf("toàn quốc → mọi nông dân: calls=%v", farmers.calls)
	}
}
