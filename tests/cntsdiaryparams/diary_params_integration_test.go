package cntsdiaryparams_test

// KN-PARAMS-CNTS-DIARY-OWNER (#85): hạn sửa lần ghi đã gửi và hạn lưu tọa độ GPS của nhật ký nuôi CNTS (W11.48) đọc tham số
// CÓ PHIÊN BẢN của pf/masterdata THẬT: đăng ký riêng nạp vào tenant (seed 7/365), đổi phiên bản qua luồng 4 mắt (người đặt ≠
// người duyệt), áp dụng theo NGÀY NGHIỆP VỤ giờ Việt Nam của thời điểm sự việc; tenant khác không bị ảnh hưởng; phát lại giữ
// hạn lưu đã chốt; job dọn theo hạn đã chốt; giá trị đã duyệt sai → 503, không rơi về mặc định.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func TestCNTSDiary_VersionedParams_85(t *testing.T) {
	f := newFix(t)
	ctx := context.Background()
	to := func(at time.Time) { f.clk.Advance(at.Sub(f.clk.Now())) }
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	at := func(tid uuid.UUID, key string, when time.Time) (params.ParamValue, error) {
		return f.md.Params().At(tenant.WithID(ctx, tid), key, clock.BusinessDay(when))
	}

	// ----- Nạp đăng ký mới vào tenant (dữ liệu nền), tenant khác, tenant chưa nạp -----
	other, bare := ids.New(), ids.New()
	for _, tid := range []uuid.UUID{f.tenant, other} {
		if res, err := f.md.Seed(ctx, tid); err != nil || res.Params == 0 {
			t.Fatalf("nạp dữ liệu nền: %+v %v", res, err)
		}
	}
	for key, v := range map[string]string{knport.ParamCNTSDiaryEditWindowDays: "7", knport.ParamCNTSDiaryGPSRetentionDays: "365"} {
		for _, tid := range []uuid.UUID{f.tenant, other} {
			pv, err := at(tid, key, t0)
			if err != nil || pv.Value.String() != v || pv.Version != 1 || pv.Unit != "ngày" || pv.SourceDoc == "" ||
				!pv.EffectiveFrom.Equal(clock.StartOfBusinessDate(clock.Date{Year: 2026, Month: 1, Day: 1})) {
				t.Fatalf("seed %s tenant %s: %+v %v", key, tid, pv, err)
			}
		}
		if _, err := at(bare, key, t0); !errors.Is(err, params.ErrParamNotFound) {
			t.Fatalf("tenant chưa nạp không có %s: %v", key, err)
		}
	}
	// Đăng ký KN cũ vẫn nạp đủ, không lẫn khóa.
	if pv, err := at(f.tenant, knport.ParamCNTSLocationDistanceM, t0); err != nil || pv.Value.String() != "200" {
		t.Fatalf("tham số KN cũ: %+v %v", pv, err)
	}

	// ----- Hộ mô hình, lứa, lần ghi đã gửi có tọa độ (10:00 13/10/2027 VN) -----
	chu := f.user(tinh("chu_nhiem"))
	officer := f.user(xa("30001"))
	f.xa.list = []accport.XaScope{{ActorRef: officer.ref, Communes: []string{"30001"}}}
	pref := str(want(t, chu.post(base+"projects", map[string]any{"code": "da-85", "name": "Mô hình gà 2027", "kind": "tinh",
		"start_date": "2027-03-01"}), 201, "", "dự án"), "ref")
	sref := str(want(t, chu.post(base+"demo-sites", map[string]any{"project_ref": pref, "commune_code": "30001", "name": "Điểm gà",
		"model_type_code": "GA_TP", "officer_ref": officer.ref.String(), "diary_form": "BM-08",
		"location": map[string]any{"lat": 10.38, "lng": 105.43, "source": "map"}}), 201, "", "điểm"), "ref")
	href := str(want(t, officer.post(base+"model-households", map[string]any{"site_ref": sref, "owner_name": "Lê Văn Gà", "owner_phone": "0912345670",
		"keeper_kind": "officer", "keeper_ref": officer.ref.String(), "location": map[string]any{"lat": 10.381, "lng": 105.431, "source": "gps"}}),
		201, "", "hộ"), "ref")
	cref := str(want(t, officer.post(base+"diary-cycles", map[string]any{"household_ref": href, "breed": "Gà lai Mía", "received_date": "2027-10-01",
		"initial_count": 500}), 201, "", "lứa"), "ref")
	entry := func(lat float64, extra map[string]any) map[string]any {
		return merge(map[string]any{"cycle_ref": cref, "entry_kind": "daily", "submit": true,
			"metrics":  map[string]any{"alive": "500", "dead": "0", "feed_kg": "10"},
			"location": map[string]any{"lat": lat, "lng": 105.4311, "accuracy_m": "8"}}, extra)
	}
	purgeAfter := func(ref string) string {
		t.Helper()
		var d time.Time
		if err := f.pool.QueryRow(ctx, `SELECT purge_after FROM pf_extension.cnts_diary_fix WHERE tenant_id = $1 AND entry_ref = $2`,
			f.tenant, ref).Scan(&d); err != nil {
			t.Fatalf("purge_after %s: %v", ref, err)
		}
		return d.Format(time.DateOnly)
	}
	k1 := key()
	e1 := want(t, officer.postK(base+"diary-entries", k1, entry(10.3811, nil)), 201, "", "lần ghi 13/10")
	r1 := str(e1, "ref")
	if str(e1, "entry_date") != "2027-10-13" || str(e1, "editable_until") != "2027-10-20" || e1["can_edit"] != true {
		t.Fatalf("seed 7 ngày: %v", e1)
	}
	if p := purgeAfter(r1); p != "2028-10-12" {
		t.Fatalf("seed 365 ngày từ ngày ghi 13/10/2027: %s", p)
	}

	// ----- Đổi phiên bản qua luồng 4 mắt: hạn sửa 3, hạn lưu 30, hiệu lực 15/10/2027 -----
	setter, approver := f.mdAdmin(mdport.RoleParamSetter), f.mdAdmin(mdport.RoleParamApprover)
	propose := func(key, value, eff string) string {
		t.Helper()
		b := want(t, setter.post("/v1/masterdata/params/versions", map[string]any{"key": key, "value": value, "unit": "ngày",
			"effective_from": eff, "source_doc": "QĐ thử nghiệm #85"}), 201, "", "đề xuất "+key+"="+value)
		id := str(b, "id")
		want(t, setter.post("/v1/masterdata/params/versions/"+id+"/submit", map[string]any{}), 200, "", "gửi duyệt")
		return id
	}
	approve := func(id string) {
		t.Helper()
		want(t, setter.post("/v1/masterdata/params/versions/"+id+"/approve", map[string]any{}), 403, "", "người đặt không tự duyệt")
		want(t, approver.post("/v1/masterdata/params/versions/"+id+"/approve", map[string]any{}), 200, "", "duyệt")
	}
	editID := propose(knport.ParamCNTSDiaryEditWindowDays, "3", "2027-10-15")
	retID := propose(knport.ParamCNTSDiaryGPSRetentionDays, "30", "2027-10-15")
	if pv, err := at(f.tenant, knport.ParamCNTSDiaryEditWindowDays, day("2027-10-16")); err != nil || pv.Value.String() != "7" {
		t.Fatalf("chưa duyệt thì chưa áp dụng: %+v %v", pv, err)
	}
	approve(editID)
	approve(retID)
	if pv, err := at(other, knport.ParamCNTSDiaryEditWindowDays, day("2027-10-16")); err != nil || pv.Value.String() != "7" || pv.Version != 1 {
		t.Fatalf("tenant khác không đổi: %+v %v", pv, err)
	}
	if pv, err := at(f.tenant, knport.ParamCNTSDiaryEditWindowDays, day("2027-10-16")); err != nil || pv.Value.String() != "3" || pv.Version != 2 {
		t.Fatalf("phiên bản 2: %+v %v", pv, err)
	}

	// Trước ngày hiệu lực: vẫn 7 ngày.
	if g := want(t, officer.get(base+"diary-entries/"+r1), 200, "", "xem 13/10"); str(g, "editable_until") != "2027-10-20" {
		t.Fatalf("13/10 vẫn bản 1: %v", g)
	}
	// 23:30 14/10 VN (16:30Z) — ngày nghiệp vụ 14/10: chưa tới hiệu lực.
	to(time.Date(2027, 10, 14, 16, 30, 0, 0, time.UTC))
	if g := want(t, officer.get(base+"diary-entries/"+r1), 200, "", "xem 23:30 14/10"); str(g, "editable_until") != "2027-10-20" {
		t.Fatalf("23:30 14/10 VN vẫn bản 1: %v", g)
	}
	// Ghi ngoại tuyến: thiết bị ghi 17:00 14/10 VN, gửi lên sau nửa đêm → hạn lưu theo NGÀY THIẾT BỊ GHI (bản 1, 365).
	to(time.Date(2027, 10, 14, 17, 30, 0, 0, time.UTC)) // 00:30 15/10 VN
	e2 := want(t, officer.post(base+"diary-entries", entry(10.3812, map[string]any{"captured_at": "2027-10-14T10:00:00Z"})), 201, "",
		"lần ghi ngoại tuyến 14/10")
	r2 := str(e2, "ref")
	if str(e2, "entry_date") != "2027-10-14" || purgeAfter(r2) != "2028-10-13" {
		t.Fatalf("hạn lưu theo ngày thiết bị ghi (365): %v %s", e2, purgeAfter(r2))
	}
	// 00:30 15/10 VN — ngày nghiệp vụ 15/10: hạn sửa 3 ngày → lần ghi 13/10 sửa tới 16/10.
	g := want(t, officer.get(base+"diary-entries/"+r1), 200, "", "xem 00:30 15/10")
	if str(g, "editable_until") != "2027-10-16" || g["can_edit"] != true {
		t.Fatalf("00:30 15/10 VN áp dụng bản 2 (3 ngày): %v", g)
	}
	if again := want(t, officer.postK(base+"diary-entries", k1, entry(10.3811, nil)), 201, "", "phát lại"); str(again, "ref") != r1 ||
		purgeAfter(r1) != "2028-10-12" {
		t.Fatalf("phát lại giữ lần ghi và hạn lưu đã chốt: %v %s", again, purgeAfter(r1))
	}
	// Lần ghi mới ngày 15/10 có tọa độ → hạn lưu 30 ngày, hạn sửa 3 ngày (bản 2).
	to(time.Date(2027, 10, 15, 2, 0, 0, 0, time.UTC)) // 09:00 15/10 VN
	e3 := want(t, officer.post(base+"diary-entries", entry(10.3813, nil)), 201, "", "lần ghi 15/10")
	r3 := str(e3, "ref")
	if str(e3, "entry_date") != "2027-10-15" || purgeAfter(r3) != "2027-11-14" || str(e3, "editable_until") != "2027-10-18" {
		t.Fatalf("lần ghi 15/10 — hạn lưu 30, hạn sửa 3: %v %s", e3, purgeAfter(r3))
	}
	// 23:59 16/10 VN còn sửa được; 00:30 17/10 VN quá hạn (3 ngày).
	to(time.Date(2027, 10, 16, 16, 59, 0, 0, time.UTC))
	if g := want(t, officer.get(base+"diary-entries/"+r1), 200, "", "23:59 16/10"); g["can_edit"] != true {
		t.Fatalf("23:59 16/10 VN còn trong hạn: %v", g)
	}
	to(time.Date(2027, 10, 16, 17, 30, 0, 0, time.UTC))
	g = want(t, officer.get(base+"diary-entries/"+r1), 200, "", "00:30 17/10")
	if g["can_edit"] != false {
		t.Fatalf("00:30 17/10 VN hết hạn sửa: %v", g)
	}
	lk := want(t, officer.post(base+"diary-entries/"+r1, map[string]any{"base_version": json.Number(num(g, "version")), "entry_kind": "daily",
		"metrics": map[string]any{"alive": "500", "dead": "0", "feed_kg": "11"}}), 409, "extension.cnts_diary_entry_locked", "quá 3 ngày")
	if !strings.Contains(str(lk, "detail"), "3 ngày") {
		t.Fatalf("thông báo hạn sửa theo tham số: %v", lk)
	}

	// ----- Dọn tọa độ theo hạn đã chốt: ngày 15/11/2027 chỉ xóa tọa độ lần ghi 15/10 (hạn 14/11) -----
	if _, err := f.pool.Exec(ctx, `SELECT pf_extension.purge_diary_fixes('2027-11-15', 100000)`); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_diary_fix WHERE tenant_id = $1 AND entry_ref = $2`, f.tenant, r3); n != 0 {
		t.Fatalf("tọa độ hết hạn lưu 30 ngày phải bị dọn: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_diary_fix WHERE tenant_id = $1 AND entry_ref IN ($2, $3)`, f.tenant, r1, r2); n != 2 {
		t.Fatalf("tọa độ còn hạn 365 ngày phải giữ: %d", n)
	}

	// ----- Giá trị đã duyệt sai (masterdata chỉ coi min/max là gợi ý) → 503, không rơi về mặc định -----
	approve(propose(knport.ParamCNTSDiaryEditWindowDays, "2.5", "2027-10-20"))
	approve(propose(knport.ParamCNTSDiaryGPSRetentionDays, "0", "2027-10-20"))
	to(time.Date(2027, 10, 20, 2, 0, 0, 0, time.UTC))
	want(t, officer.get(base+"diary-entries/"+r1), 503, "extension.dependency_unavailable", "hạn sửa không nguyên")
	want(t, officer.get(base+"diary-entries?cycle_ref="+cref), 503, "extension.dependency_unavailable", "danh sách")
	want(t, officer.post(base+"diary-entries", entry(10.3814, map[string]any{"submit": false})), 503, "extension.dependency_unavailable",
		"hạn lưu 0 ngày")
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_diary_entry WHERE tenant_id = $1 AND entry_date = '2027-10-20'`, f.tenant); n != 0 {
		t.Fatalf("không ghi khi không đọc được hạn lưu: %d", n)
	}
	// Sửa tham số bằng phiên bản mới hợp lệ → hoạt động lại.
	approve(propose(knport.ParamCNTSDiaryEditWindowDays, "5", "2027-10-21"))
	approve(propose(knport.ParamCNTSDiaryGPSRetentionDays, "60", "2027-10-21"))
	to(time.Date(2027, 10, 21, 2, 0, 0, 0, time.UTC))
	if g := want(t, officer.get(base+"diary-entries/"+r3), 200, "", "sau khi sửa tham số"); str(g, "editable_until") != "2027-10-20" {
		t.Fatalf("hạn sửa 5 ngày từ 15/10: %v", g)
	}
	e4 := want(t, officer.post(base+"diary-entries", entry(10.3815, nil)), 201, "", "lần ghi 21/10")
	if p := purgeAfter(str(e4, "ref")); p != "2027-12-20" {
		t.Fatalf("hạn lưu 60 ngày từ 21/10: %s", p)
	}
}
