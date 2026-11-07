package handler_test

// CNTS-V4-CHECKOUT-PHOTO (#249, BRD CNTS v4.0 FN-12 CV-07/CV-08, BR-06..08, AC-09, AC-11..13): check-in bắt buộc ảnh,
// check-out kèm ảnh, thời gian ở địa điểm, "Đồng bộ trễ"; check-in cũ không ảnh giữ nguyên (đếm trước/sau, quay lui).

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// jpegBytes là ảnh JPEG tối thiểu (chữ ký đầu tệp đủ để nhận kiểu); jpegB64 là thân gửi lên.
var (
	jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}, bytes.Repeat([]byte{0x42}, 256)...)
	jpegB64   = base64.StdEncoding.EncodeToString(jpegBytes)
)

// approvedSchedule lập, gửi và duyệt lịch tuần 11/10/2027 của officer; trả mã lịch và người duyệt.
func approvedSchedule(t *testing.T, f *fix, officer user, items []map[string]any) (uuid.UUID, user) {
	t.Helper()
	sid := ids.New()
	want(t, officer.post("/v1/extension/work-schedules", map[string]any{"id": sid, "week_start": "2027-10-11", "action": "submit", "items": items}), 201, "", "gửi duyệt")
	lead := tinh("lanh_dao_phong")
	lead.Manager = true
	lead.MgmtLevel = "truong_phong"
	reviewer := f.user(lead)
	want(t, reviewer.post("/v1/extension/work-schedules/"+sid.String()+"/approve", map[string]any{"version": 1}), 200, "", "duyệt")
	return sid, reviewer
}

func visit(lat float64, at string, photo any) map[string]any {
	return map[string]any{"lat": lat, "lon": 105.0, "accuracy_m": 10, "captured_at": at, "photo": photo}
}

func TestWorkScheduleCheckoutPhoto(t *testing.T) {
	f := newFix(t)
	officer := f.user(xa("30001"))
	stranger := f.user(xa("30002"))
	place := map[string]any{"lat": 10.0, "lon": 105.0}
	itemA, itemB := ids.New(), ids.New()
	sid, reviewer := approvedSchedule(t, f, officer, []map[string]any{
		{"id": itemA, "day": "2027-10-13", "session": "morning", "content": "Kiểm tra lò mổ", "commune_code": "30001", "place": place},
		{"id": itemB, "day": "2027-10-13", "session": "afternoon", "content": "Thăm hộ mô hình", "commune_code": "30001", "place": place},
	})
	base := fmt.Sprintf("/v1/extension/work-schedules/%s/items/", sid)
	in, out := base+itemA.String()+"/checkin", base+itemA.String()+"/checkout"

	// FN-12 luồng ngoại lệ "Check-in, check-out không có ảnh → chặn".
	if r := officer.post(in, visit(10.001, "2027-10-13T08:00:00+07:00", nil)); r.status != 422 || !bytes.Contains(r.raw, []byte(`"photo"`)) {
		t.Fatalf("check-in thiếu ảnh phải 422 chỉ trường photo: %d %s", r.status, r.raw)
	}
	want(t, officer.post(in, visit(10.001, "2027-10-13T08:00:00+07:00", base64.StdEncoding.EncodeToString([]byte("không phải ảnh")))), 422, "validation_failed", "không phải ảnh")
	want(t, officer.post(in, visit(10.001, "2027-10-13T08:00:00+07:00", "%%%")), 422, "validation_failed", "base64 hỏng")
	big := base64.StdEncoding.EncodeToString(append(append([]byte{}, jpegBytes...), make([]byte, 500<<10)...))
	want(t, officer.post(in, visit(10.001, "2027-10-13T08:00:00+07:00", big)), 422, "validation_failed", "ảnh quá 500 KB")
	// B4b: check-out chỉ sau check-in.
	want(t, officer.post(out, visit(10.001, "2027-10-13T09:00:00+07:00", jpegB64)), 409, "extension.checkout_no_checkin", "check-out trước check-in")

	c := want(t, officer.post(in, visit(10.001, "2027-10-13T08:00:00+07:00", jpegB64)), 201, "", "check-in 08:00 có ảnh")
	ck := obj(c, "checkin")
	if ck["legacy_no_photo"] != false || str(obj(ck, "photo"), "content_type") != "image/jpeg" || ck["checkout"] != nil {
		t.Fatalf("check-in có ảnh: %s", c)
	}
	// Ảnh: người đăng ký và người duyệt xem được; cán bộ xã khác không.
	for who, u := range map[string]user{"người đăng ký": officer, "người duyệt": reviewer} {
		p := u.get(base + itemA.String() + "/checkin/photo")
		if p.status != 200 || !bytes.Equal(p.raw, jpegBytes) {
			t.Fatalf("%s xem ảnh check-in: %d", who, p.status)
		}
	}
	if p := stranger.get(base + itemA.String() + "/checkin/photo"); p.status != 404 {
		t.Fatalf("cán bộ xã khác xem ảnh: %d %s", p.status, p.raw)
	}
	want(t, officer.get(base+itemA.String()+"/checkout/photo"), 404, "extension.visit_photo_not_found", "chưa check-out chưa có ảnh")

	// AC-09: bấm check-out mà chưa chụp ảnh thì không lưu được.
	want(t, officer.post(out, visit(10.001, "2027-10-13T09:30:00+07:00", nil)), 422, "validation_failed", "check-out thiếu ảnh")
	want(t, officer.post(out, visit(10.001, "2027-10-13T07:30:00+07:00", jpegB64)), 422, "validation_failed", "check-out trước giờ check-in")
	want(t, officer.post(out, visit(10.001, "2027-10-14T07:30:00+07:00", jpegB64)), 422, "validation_failed", "check-out khác ngày công tác")

	// AC-12: check-in 08:00, check-out 11:30 → 3 giờ 30 phút (bấm 11:30, gửi lúc 12:00 — AC-11 giữ thời điểm lúc bấm).
	f.clk.Advance(2 * time.Hour)
	k := key()
	o := want(t, officer.postK(out, k, visit(10.001, "2027-10-13T11:30:00+07:00", jpegB64)), 201, "", "check-out 11:30")
	co := obj(o, "checkout")
	if num(co, "stay_minutes") != "210" || co["synced_late"] != false || co["far"] != false || str(co, "captured_at") != "2027-10-13T04:30:00Z" ||
		obj(co, "location") == nil || str(obj(co, "photo"), "content_type") != "image/jpeg" || len(list(o, "warnings")) != 0 {
		t.Fatalf("check-out: %s", o)
	}
	rep := want(t, officer.postK(out, k, visit(10.001, "2027-10-13T11:30:00+07:00", jpegB64)), 201, "", "gọi lặp check-out")
	if obj(obj(rep, "checkout"), "location") != nil || str(obj(rep, "checkout"), "id") != str(co, "id") {
		t.Fatalf("phát lại check-out (không giữ tọa độ trong cache): %s", rep)
	}
	// BR-06: không sửa được.
	want(t, officer.post(out, visit(10.002, "2027-10-13T11:40:00+07:00", jpegB64)), 409, "extension.checkout_exists", "check-out lần hai")
	if p := reviewer.get(base + itemA.String() + "/checkout/photo"); p.status != 200 || !bytes.Equal(p.raw, jpegBytes) {
		t.Fatalf("người duyệt xem ảnh check-out: %d", p.status)
	}

	// AC-13: check-out ngoại tuyến 16:30 ngày 13/10, đồng bộ 07:10 ngày 14/10 → ghi 16:30 kèm "Đồng bộ trễ"; cách > 500 m.
	want(t, officer.post(base+itemB.String()+"/checkin", visit(10.001, "2027-10-13T11:45:00+07:00", jpegB64)), 201, "", "check-in mục chiều")
	f.clk.Advance(19*time.Hour + 10*time.Minute) // 07:10 ngày 14/10 giờ Việt Nam
	o = want(t, officer.post(base+itemB.String()+"/checkout", visit(10.01, "2027-10-13T16:30:00+07:00", jpegB64)), 201, "", "check-out đồng bộ trễ")
	co = obj(o, "checkout")
	if co["synced_late"] != true || str(co, "captured_at") != "2027-10-13T09:30:00Z" || str(co, "synced_at") != "2027-10-14T00:10:00Z" ||
		co["far"] != true || codes(list(o, "warnings"))["checkout_far"] != 1 {
		t.Fatalf("đồng bộ trễ: %s", o)
	}

	// Lịch: check-in kèm check-out lồng; cảnh báo check-out xa; tọa độ cả hai chỉ người có quyền thấy.
	d := want(t, reviewer.get("/v1/extension/work-schedules/"+sid.String()), 200, "", "người duyệt đọc lịch")
	if codes(list(d, "warnings"))["checkout_far"] != 1 || len(list(d, "checkins")) != 2 {
		t.Fatalf("lịch: %s", d)
	}
	for _, v := range list(d, "checkins") {
		ci := v.(map[string]any)
		if obj(ci, "checkout") == nil || obj(obj(ci, "checkout"), "location") == nil || ci["legacy_no_photo"] != false {
			t.Fatalf("check-in trong lịch: %v", ci)
		}
	}
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM pf_extension.work_schedule_checkin_point WHERE tenant_id = $1`, f.tenant).Scan(&n); err != nil || n != 4 {
		t.Fatalf("tọa độ check-in + check-out ở bảng có hạn lưu: %d %v", n, err)
	}
}

// TestWorkScheduleCheckinLegacy: kịch bản đếm trước/sau và quay lui (PLAN mục 1.7). Check-in cũ không ảnh (ghi bằng
// đúng câu INSERT của bản trước #249 — cũng là bản chạy lại khi quay lui ứng dụng, lược đồ chỉ thêm cột NULL được) giữ
// nguyên: không đổi số dòng, không đổi băm, chuỗi băm xác minh được; API đánh dấu legacy_no_photo, không bắt nhập bù.
func TestWorkScheduleCheckinLegacy(t *testing.T) {
	f := newFix(t)
	officer := f.user(xa("30001"))
	place := map[string]any{"lat": 10.0, "lon": 105.0}
	itemOld, itemNew := ids.New(), ids.New()
	sid, reviewer := approvedSchedule(t, f, officer, []map[string]any{
		{"id": itemOld, "day": "2027-10-13", "session": "morning", "content": "Kiểm tra lò mổ", "commune_code": "30001", "place": place},
		{"id": itemNew, "day": "2027-10-13", "session": "afternoon", "content": "Thăm hộ", "commune_code": "30001", "place": place},
	})
	ctx := context.Background()
	oldID := ids.New()
	// Câu INSERT của bản trước #249 (store.InsertCheckin cũ, không có cột ảnh).
	if _, err := f.pool.Exec(ctx, `INSERT INTO pf_extension.work_schedule_checkin
	    (id, tenant_id, stream_key, schedule_id, item_id, item_version, owner_ref, department_code, day, captured_at,
	     accuracy_m, distance_m, far, weak_accuracy, mock_location, point_stored, device_id, event_time, actor_ref, idem_key,
	     rule_version)
	  VALUES ($1, $2, $3, $4, $5, 1, $6, $7, '2027-10-13', '2027-10-13T01:00:00Z', 9, 111.2, false, false, false, false, NULL,
	    '2027-10-13T01:00:05Z', $6, $8, 'test')`,
		oldID, f.tenant, "wcheckin:"+itemOld.String(), sid, itemOld, officer.ref, cnts, "checkin:legacy-"+oldID.String()); err != nil {
		t.Fatal(err)
	}
	type counts struct{ total, legacy int }
	count := func() (c counts, hashes string) {
		t.Helper()
		if err := f.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE photo_key IS NULL),
		    coalesce(string_agg(encode(row_hash, 'hex'), ',' ORDER BY id) FILTER (WHERE photo_key IS NULL), '')
		  FROM pf_extension.work_schedule_checkin WHERE tenant_id = $1`, f.tenant).Scan(&c.total, &c.legacy, &hashes); err != nil {
			t.Fatal(err)
		}
		return c, hashes
	}
	before, beforeHash := count()
	if before != (counts{1, 1}) {
		t.Fatalf("trước: %+v", before)
	}

	d := want(t, officer.get("/v1/extension/work-schedules/"+sid.String()), 200, "", "lịch có check-in cũ")
	old := list(d, "checkins")[0].(map[string]any)
	if old["legacy_no_photo"] != true || old["photo"] != nil {
		t.Fatalf("check-in cũ phải đánh dấu dữ liệu cũ: %v", old)
	}
	want(t, reviewer.get(fmt.Sprintf("/v1/extension/work-schedules/%s/items/%s/checkin/photo", sid, itemOld)), 404, "extension.visit_photo_not_found", "check-in cũ không ảnh")
	// Không bắt nhập bù: check-in cũ vẫn check-out được (kèm ảnh), mục mới check-in phải có ảnh.
	want(t, officer.post(fmt.Sprintf("/v1/extension/work-schedules/%s/items/%s/checkout", sid, itemOld), visit(10.001, "2027-10-13T09:00:00+07:00", jpegB64)), 201, "", "check-out sau check-in cũ")
	want(t, officer.post(fmt.Sprintf("/v1/extension/work-schedules/%s/items/%s/checkin", sid, itemNew), visit(10.001, "2027-10-13T09:30:00+07:00", jpegB64)), 201, "", "check-in mới có ảnh")

	after, afterHash := count()
	if after != (counts{2, 1}) || afterHash != beforeHash {
		t.Fatalf("sau: %+v (dòng cũ đổi băm: %v)", after, afterHash != beforeHash)
	}
	// ADR-0012: dòng cũ (thiếu 4 cột ảnh) và dòng mới (có ảnh) đều xác minh được chuỗi băm.
	for _, it := range []uuid.UUID{itemOld, itemNew} {
		var ok bool
		if err := f.pool.QueryRow(ctx, "SELECT ok FROM shared.verify_chain('pf_extension.work_schedule_checkin'::regclass, $1, $2)",
			f.tenant, "wcheckin:"+it.String()).Scan(&ok); err != nil || !ok {
			t.Fatalf("verify_chain %s: %v %v", it, ok, err)
		}
	}
	// Ràng buộc bốn cột ảnh cùng có hoặc cùng NULL.
	half := ids.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO pf_extension.work_schedule_checkin
	    (id, tenant_id, stream_key, schedule_id, item_id, item_version, owner_ref, department_code, day, captured_at,
	     far, weak_accuracy, mock_location, point_stored, event_time, actor_ref, idem_key, rule_version, photo_key)
	  VALUES ($1, $2, $3, $4, $5, 1, $6, $7, '2027-10-13', now(), false, false, false, false, now(), $6, $8, 'test', 'k')`,
		ids.New(), f.tenant, "wcheckin:"+half.String(), sid, half, officer.ref, cnts, "checkin:half-"+ids.New().String()); err == nil {
		t.Fatal("ảnh thiếu cột phải bị chặn")
	}
}
