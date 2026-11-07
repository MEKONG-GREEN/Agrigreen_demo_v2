package handler_test

// CNTS-V4-CHECKIN-MONITOR (#250, BRD CNTS v4.0 FN-12 BR-07/BR-08, AC-10, AC-13, Mục III.5 dòng 13): nhãn "Chưa check-out"
// lúc 23:59 giờ Việt Nam ở màn cán bộ xã và màn theo dõi; job extension_cnts_alert sinh cảnh báo cho người theo dõi lịch
// cán bộ xã, tự đóng khi check-out đồng bộ trễ. Giờ giả lập (clock.Fixed), không gắn ngày thật.

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// officerDir là danh bạ cán bộ KN giả (cổng KNDirectory của pf/accounts).
type officerDir struct{ list []accport.KNOfficer }

func (d *officerDir) Officers(context.Context, uuid.UUID, string, string) ([]accport.KNOfficer, error) {
	return d.list, nil
}

type alertRow struct {
	status     string
	level      string
	auto       bool
	recipients []uuid.UUID
	commune    string
	result     string
}

func noCheckoutAlerts(t *testing.T, f *fix) map[uuid.UUID]alertRow {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT source_ref, status, level, auto_closed, recipient_refs, commune_code,
	    coalesce(result, '') FROM pf_extension.cnts_alert_current WHERE tenant_id = $1 AND kind = 'no_checkout'`, f.tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[uuid.UUID]alertRow{}
	for rows.Next() {
		var src uuid.UUID
		var a alertRow
		if err := rows.Scan(&src, &a.status, &a.level, &a.auto, &a.recipients, &a.commune, &a.result); err != nil {
			t.Fatal(err)
		}
		out[src] = a
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// checkinOf trả check-in của mục trong danh sách check-in của lịch.
func checkinOf(t *testing.T, d map[string]any, item uuid.UUID) map[string]any {
	t.Helper()
	for _, v := range list(d, "checkins") {
		if c := v.(map[string]any); str(c, "item_id") == item.String() {
			return c
		}
	}
	t.Fatalf("không có check-in của mục %s: %v", item, d)
	return nil
}

// setClock dời đồng hồ giả lập tới at (chỉ tiến).
func setClock(f *fix, at time.Time) { f.clk.Advance(at.Sub(f.clk.Now())) }

func TestWorkScheduleNoCheckout(t *testing.T) {
	f := newFix(t)
	dir := &officerDir{}
	f.mod.UseKNContext(knResolver{}, dir)
	f.mod.UseCNTSAlerts(extension.CNTSAlertDeps{Directory: dir}) // nối người theo dõi lịch cho job cảnh báo
	officer := f.user(xa("30001"))
	place := map[string]any{"lat": 10.0, "lon": 105.0}
	itemA, itemB, itemOld := ids.New(), ids.New(), ids.New()
	sid, reviewer := approvedSchedule(t, f, officer, []map[string]any{
		{"id": itemA, "day": "2027-10-13", "session": "morning", "content": "Kiểm tra lò mổ", "commune_code": "30001", "place": place},
		{"id": itemB, "day": "2027-10-13", "session": "afternoon", "content": "Thăm hộ mô hình", "commune_code": "30001", "place": place},
		{"id": itemOld, "day": "2027-10-13", "session": "afternoon", "content": "Họp xã", "commune_code": "30001", "place": place},
	})
	dir.list = []accport.KNOfficer{
		{ActorRef: reviewer.ref, Level: accport.ScheduleLevelProvince, ProvinceCode: "89", MgmtLevel: "truong_phong"},
		{ActorRef: officer.ref, Level: accport.ScheduleLevelCommune, ProvinceCode: "89", Communes: []string{"30001"}},
	}
	ctx := context.Background()
	base := fmt.Sprintf("/v1/extension/work-schedules/%s/items/", sid)
	want(t, officer.post(base+itemA.String()+"/checkin", visit(10.001, "2027-10-13T08:00:00+07:00", jpegB64)), 201, "", "check-in A 08:00")
	want(t, officer.post(base+itemB.String()+"/checkin", visit(10.001, "2027-10-13T08:30:00+07:00", jpegB64)), 201, "", "check-in B 08:30")
	// Check-in cũ trước v4.0 (không ảnh, câu INSERT của bản trước #249): không gắn nhãn, không cảnh báo (không bắt nhập bù).
	if _, err := f.pool.Exec(ctx, `INSERT INTO pf_extension.work_schedule_checkin
	    (id, tenant_id, stream_key, schedule_id, item_id, item_version, owner_ref, department_code, day, captured_at,
	     far, weak_accuracy, mock_location, point_stored, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, 1, $6, $7, '2027-10-13', '2027-10-13T02:00:00Z', false, false, false, false,
	    '2027-10-13T02:00:05Z', $6, $8, 'test')`,
		ids.New(), f.tenant, "wcheckin:"+itemOld.String(), sid, itemOld, officer.ref, cnts, "checkin:legacy-"+ids.New().String()); err != nil {
		t.Fatal(err)
	}

	job := func(what string) int {
		t.Helper()
		r, err := f.mod.CNTSAlertJobRun(ctx, f.tenant, f.clk.Now())
		if err != nil {
			t.Fatalf("job %s: %v", what, err)
		}
		return r.NoCheckouts
	}
	label := func(u user, item uuid.UUID) bool {
		t.Helper()
		d := want(t, u.get("/v1/extension/work-schedules/"+sid.String()), 200, "", "đọc lịch")
		return checkinOf(t, d, item)["no_checkout"] == true
	}

	// 10:00 ngày công tác (t0): chưa tới 23:59 → không nhãn, không cảnh báo.
	if label(reviewer, itemA) || job("10:00") != 0 {
		t.Fatal("chưa tới 23:59 không gắn nhãn, không sinh cảnh báo")
	}
	// 23:58:59 vẫn chưa.
	setClock(f, time.Date(2027, 10, 13, 23, 58, 59, 0, time.FixedZone("ICT", 7*3600)))
	if label(reviewer, itemA) || job("23:58:59") != 0 || len(noCheckoutAlerts(t, f)) != 0 {
		t.Fatal("23:58:59 chưa gắn nhãn")
	}

	// BR-07 / AC-10: 23:59 cùng ngày chưa check-out → nhãn ở màn theo dõi và màn cán bộ xã; người theo dõi nhận cảnh báo.
	setClock(f, time.Date(2027, 10, 13, 23, 59, 0, 0, time.FixedZone("ICT", 7*3600)))
	if !label(reviewer, itemA) || !label(reviewer, itemB) || label(reviewer, itemOld) {
		t.Fatal("23:59: A, B mang nhãn Chưa check-out; check-in cũ không ảnh thì không")
	}
	mine := want(t, officer.get("/v1/extension/work-schedules/mine?week_start=2027-10-11"), 200, "", "lịch của tôi (cán bộ xã)")
	if checkinOf(t, obj(mine, "schedule"), itemA)["no_checkout"] != true {
		t.Fatalf("màn cán bộ xã phải có nhãn: %s", mine)
	}
	lst := want(t, reviewer.get("/v1/extension/work-schedules?week_start=2027-10-11&owner_level=commune"), 200, "", "danh sách theo dõi")
	row := list(lst, "items")[0].(map[string]any)
	if num(row, "no_checkout_count") != "2" || checkinOf(t, row, itemB)["no_checkout"] != true {
		t.Fatalf("danh sách theo dõi: %v", row)
	}
	d := want(t, reviewer.get("/v1/extension/work-schedules/"+sid.String()), 200, "", "chi tiết")
	if codes(list(d, "warnings"))["checkout_missing"] != 1 {
		t.Fatalf("cảnh báo checkout_missing: %v", list(d, "warnings"))
	}
	if n := job("23:59"); n != 2 {
		t.Fatalf("23:59 sinh %d cảnh báo, muốn 2", n)
	}
	al := noCheckoutAlerts(t, f)
	for _, it := range []uuid.UUID{itemA, itemB} {
		a, ok := al[it]
		if !ok || a.status != "new" || a.level != "attention" || a.commune != "30001" || !slices.Equal(a.recipients, []uuid.UUID{reviewer.ref}) {
			t.Fatalf("cảnh báo của mục %s: %+v (có %v)", it, a, ok)
		}
	}
	if len(al) != 2 {
		t.Fatalf("check-in cũ không sinh cảnh báo: %d", len(al))
	}
	if job("chạy lại") != 0 || len(noCheckoutAlerts(t, f)) != 2 {
		t.Fatal("job chạy lặp không sinh trùng (BR-04 FN-13)")
	}
	// Job tìm được tenant bằng vai ag_ops_jobs (GRANT tenant_id, day của sổ check-in).
	tids, err := extension.CNTSAlertTenants(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.OpsJobs))(ctx, f.clk.Now())
	if err != nil || !slices.Contains(tids, f.tenant) {
		t.Fatalf("job phải tìm thấy tenant có check-in: %v %v", tids, err)
	}

	// BR-08 / AC-13: check-out ngoại tuyến 16:30 ngày 13/10, đồng bộ 07:10 ngày 14/10 → gỡ nhãn, cảnh báo tự đóng, Đồng bộ trễ.
	setClock(f, time.Date(2027, 10, 14, 7, 10, 0, 0, time.FixedZone("ICT", 7*3600)))
	o := want(t, officer.post(base+itemA.String()+"/checkout", visit(10.001, "2027-10-13T16:30:00+07:00", jpegB64)), 201, "", "check-out đồng bộ trễ")
	if obj(o, "checkout")["synced_late"] != true {
		t.Fatalf("Đồng bộ trễ: %s", o)
	}
	if label(reviewer, itemA) || !label(reviewer, itemB) {
		t.Fatal("check-out đồng bộ trễ gỡ nhãn của A; B vẫn chưa check-out")
	}
	if job("sau đồng bộ") != 0 {
		t.Fatal("không sinh lại cảnh báo")
	}
	al = noCheckoutAlerts(t, f)
	if a := al[itemA]; a.status != "closed" || !a.auto || a.result == "" {
		t.Fatalf("cảnh báo A phải tự đóng: %+v", a)
	}
	if a := al[itemB]; a.status != "new" {
		t.Fatalf("cảnh báo B còn mở: %+v", a)
	}
}
