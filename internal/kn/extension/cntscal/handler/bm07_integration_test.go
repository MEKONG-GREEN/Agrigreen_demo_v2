package handler_test

// BF-CN-VACC (#168, CN-09): lịch tiêm gắn hộ mô hình (FN-10), bảng và tệp Excel BM-07, NC-11 → ngày tiêm thực tế (quy tắc
// liên biểu số 7), cảnh báo "Mũi tiêm quá hạn" (Mục III.5 dòng 11) qua job extension_cnts_alert — PostgreSQL thật.

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	caldomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/domain"
	calstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/store"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

const cb = "/v1/extension/cnts/"

func (f *fix) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func TestVaccinationBM07(t *testing.T) {
	f := newFix(t)
	ctx := context.Background()
	chu := f.user(tinh("chu_nhiem"))
	officer := f.user(xa("30001"))
	stranger := f.user(xa("30002"))
	f.xa.list = []accport.XaScope{{ActorRef: officer.ref, Communes: []string{"30001"}}, {ActorRef: stranger.ref, Communes: []string{"30002"}}}

	// ----- Hộ mô hình của KN (W11.47) -----
	p := want(t, chu.post(cb+"projects", map[string]any{"code": "da-ga-2027", "name": "Mô hình gà 2027", "kind": "tinh", "start_date": "2027-03-01"}),
		201, "", "dự án")
	s := want(t, chu.post(cb+"demo-sites", map[string]any{"project_ref": str(p, "ref"), "commune_code": "30001", "name": "Điểm gà Bình Hòa",
		"model_type_code": "GA_TP", "officer_ref": officer.ref.String(), "diary_form": "BM-08",
		"location": map[string]any{"lat": 10.38, "lng": 105.43, "source": "map"}}), 201, "", "điểm")
	sref := str(s, "ref")
	h := want(t, chu.post(cb+"model-households", map[string]any{"site_ref": sref, "owner_name": "Nguyễn Văn Mẫu", "owner_phone": "0900000001",
		"keeper_kind": "officer", "keeper_ref": officer.ref.String(), "location": map[string]any{"lat": 10.381, "lng": 105.431, "source": "gps"}}),
		201, "", "hộ")
	href := str(h, "ref")

	// ----- Lập lịch theo hộ mô hình: xã, loại mô hình suy từ hộ/điểm -----
	want(t, chu.post(cb+"vaccination-batches", map[string]any{"household_ref": ids.New().String(), "received_date": "2027-10-01",
		"age_at_receipt": 1}), 422, "extension.cnts_vaccination_invalid", "hộ không có trong sổ hộ mô hình")
	want(t, chu.post(cb+"vaccination-batches", map[string]any{"household_ref": href, "commune_code": "30002", "received_date": "2027-10-01",
		"age_at_receipt": 1}), 422, "extension.cnts_vaccination_invalid", "xã khác xã của hộ")
	want(t, chu.post(cb+"vaccination-batches", map[string]any{"household_ref": href, "model_type_code": "GA_KHAC", "received_date": "2027-10-01",
		"age_at_receipt": 1}), 422, "extension.cnts_vaccination_invalid", "loại mô hình khác của điểm")
	b := want(t, chu.post(cb+"vaccination-batches", map[string]any{"household_ref": href, "received_date": "2027-10-01", "age_at_receipt": 1,
		"head_count": 200}), 201, "", "lập lịch cho hộ mô hình")
	bref := str(b, "ref")
	m := obj(b, "model")
	if str(b, "commune_code") != "30001" || str(b, "model_type_code") != "GA_TP" || str(m, "site_ref") != sref || str(m, "project_code") != "DA-GA-2027" ||
		str(m, "officer_ref") != officer.ref.String() || str(m, "owner_label") == "" || strings.Contains(str(m, "owner_label"), "Nguyễn Văn Mẫu") {
		t.Fatalf("lứa gắn hộ mô hình: %s", b)
	}
	// Một lứa theo địa bàn cùng loại mô hình (không gắn hộ) vẫn lên bảng.
	want(t, chu.post(cb+"vaccination-batches", map[string]any{"commune_code": "30001", "model_type_code": "GA_TP", "locality": "Ấp 3",
		"received_date": "2027-10-05", "age_at_receipt": 1}), 201, "", "lứa theo địa bàn")
	if l := list(want(t, chu.get(cb+"vaccination-batches?site_ref="+sref), 200, "", "lọc theo điểm"), "items"); len(l) != 1 {
		t.Fatalf("lọc theo điểm: %v", l)
	}

	// ----- Bảng BM-07 -----
	want(t, chu.get(cb+"vaccination-matrix"), 422, "extension.cnts_vaccination_invalid", "thiếu loại mô hình")
	mx := want(t, chu.get(cb+"vaccination-matrix?model_type_code=GA_TP"), 200, "", "bảng BM-07")
	cols, rows := list(mx, "columns"), list(mx, "rows")
	if len(cols) != 3 || len(rows) != 2 || str(cols[0].(map[string]any), "vaccine_code") != "VX_ND_F" {
		t.Fatalf("bảng: %s", mx)
	}
	r0 := rows[0].(map[string]any)
	cells := list(r0, "cells")
	if str(r0, "ref") != bref || len(cells) != 3 || str(r0, "household_text") != str(m, "owner_label") || str(r0, "commune_name") != "Xã 30001" {
		t.Fatalf("hàng 1: %v", r0)
	}
	if c := cells[1].(map[string]any); str(c, "expected_date") != "2027-10-07" || c["overdue"] != true {
		t.Fatalf("ô Gumboro L1 (dự kiến 01/10 + 7 − 1, quá hạn): %v", c)
	}
	if l := list(want(t, chu.get(cb+"vaccination-matrix?model_type_code=GA_TP&site_ref="+sref), 200, "", "bảng một điểm"), "rows"); len(l) != 1 {
		t.Fatalf("bảng theo điểm: %v", l)
	}
	if l := list(want(t, stranger.get(cb+"vaccination-matrix?model_type_code=GA_TP"), 200, "", "xã khác"), "rows"); len(l) != 0 {
		t.Fatalf("xã khác thấy hàng: %v", l)
	}
	if l := list(want(t, officer.get(cb+"vaccination-matrix?model_type_code=GA_TP"), 200, "", "Cấp Xã"), "rows"); len(l) != 2 ||
		l[0].(map[string]any)["can_record"] != true || l[0].(map[string]any)["can_plan"] != false {
		t.Fatalf("Cấp Xã ghi ngày thực tế trên bảng: %v", l)
	}

	// ----- Xuất Excel đúng bố cục sheet mẫu -----
	x := chu.get(cb + "vaccination-matrix/xlsx?model_type_code=GA_TP&received_from=2027-01-01")
	if x.status != http.StatusOK || !bytes.HasPrefix(x.raw, []byte("PK")) {
		t.Fatalf("xuất Excel: %d %.200s", x.status, x.raw)
	}
	xf, err := excelize.OpenReader(bytes.NewReader(x.raw))
	if err != nil {
		t.Fatal(err)
	}
	sh := xf.GetSheetName(0)
	cell := func(ref string) string {
		v, err := xf.GetCellValue(sh, ref)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// Như sheet mẫu: tiêu đề hàng 1, tiêu đề cột hàng 2–3 (vaccine một lần không ghi lần), dữ liệu từ hàng 4.
	if cell("A1") != "LỊCH TIÊM PHÒNG VACCIN MÔ HÌNH NUÔI GÀ THƯƠNG PHẨM NĂM 2027" || cell("A2") != "Stt" || cell("C2") != "Họ tên nông dân" ||
		cell("D2") != "CB theo dõi MH" || cell("F2") != "Vaccin VX_ND_F" || cell("F3") != "Ngày tiêm dự kiến" || cell("H2") != "Vaccin Gumboro" ||
		cell("H3") != "Ngày tiêm dự kiến (Lần 1)" || cell("I3") != "Ngày tiêm thực tế (Lần 1)" || cell("A4") != "1" || cell("B4") != "Xã 30001" ||
		cell("E4") != "01/10/2027" || cell("H4") != "07/10/2027" || cell("I4") != "Quá hạn" {
		t.Fatalf("bố cục BM-07: A1=%q A2=%q F2=%q F3=%q H2=%q H3=%q A4=%q B4=%q E4=%q H4=%q I4=%q", cell("A1"), cell("A2"), cell("F2"), cell("F3"),
			cell("H2"), cell("H3"), cell("A4"), cell("B4"), cell("E4"), cell("H4"), cell("I4"))
	}
	if mc, err := xf.GetMergeCells(sh); err != nil || len(mc) < 7 {
		t.Fatalf("ô gộp tiêu đề: %d %v", len(mc), err)
	}
	if strings.Contains(string(x.raw), "0900000001") {
		t.Fatal("tệp Excel lộ SĐT")
	}
	_ = xf.Close()

	// ----- Cảnh báo dòng 11: job sinh cho cán bộ theo dõi điểm, mỗi mũi một lần -----
	r, err := f.mod.CNTSAlertJobRun(ctx, f.tenant, t0)
	if err != nil || r.Vaccines != 4 { // lứa hộ: ND_F 03/10, Gumboro L1 07/10; lứa địa bàn: ND_F 07/10, Gumboro L1 11/10
		t.Fatalf("job mũi quá hạn: %+v %v", r, err)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_alert_current WHERE tenant_id = $1 AND kind = 'vaccine_overdue' AND level = 'attention'
	    AND source_ref = $2 AND $3 = ANY (recipient_refs)`, f.tenant, bref, officer.ref); n != 2 {
		t.Fatalf("cảnh báo gửi cán bộ theo dõi điểm: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = 'pf.extension.cnts_alert.changed'`, f.tenant); n < 4 {
		t.Fatalf("sự kiện cho pf/notify: %d", n)
	}
	if r, err = f.mod.CNTSAlertJobRun(ctx, f.tenant, t0.Add(5*time.Minute)); err != nil || r.Vaccines != 0 {
		t.Fatalf("chạy lại không sinh trùng: %+v %v", r, err)
	}

	// ----- NC-11 ở nhật ký của lứa có vaccination_batch_ref → ngày tiêm thực tế -----
	cy := want(t, officer.post(cb+"diary-cycles", map[string]any{"household_ref": href, "breed": "Gà ta", "received_date": "2027-10-01",
		"initial_count": 200, "age_days": 1, "vaccination_batch_ref": bref}), 201, "", "mở lứa nhật ký gắn lịch tiêm")
	want(t, officer.post(cb+"diary-entries", map[string]any{"cycle_ref": str(cy, "ref"), "entry_kind": "daily",
		"captured_at": t0.Add(-time.Hour).Format(time.RFC3339),
		"metrics":     map[string]any{"alive": "200", "dead": "0", "feed_kg": "10"},
		"tables": map[string]any{"vaccinations": []map[string]any{{"vaccine": "Gumboro", "shot": 0, "date": "2027-10-08"},
			{"vaccine": "vx_nd_f", "shot": 1, "date": "2027-10-04"}}},
		"location": map[string]any{"lat": 10.3811, "lng": 105.4311, "accuracy_m": "8"}, "submit": true}), 201, "", "gửi lần ghi có NC-11")
	g := want(t, chu.get(cb+"vaccination-batches/"+bref), 200, "", "lịch sau NC-11")
	vs := list(g, "versions")
	if num(g, "version") != "2" || str(g, "event") != "dose_recorded" || !strings.Contains(str(vs[0].(map[string]any), "status_reason"), "NC-11") ||
		str(g, "updated_by") != officer.ref.String() {
		t.Fatalf("lưu vết NC-11: %s", g)
	}
	for _, d := range list(g, "doses") {
		dd := d.(map[string]any)
		switch str(dd, "vaccine_code") + "/" + num(dd, "shot") {
		case "VX_ND_F/1":
			if str(dd, "actual_date") != "2027-10-04" {
				t.Fatalf("ND_F L1: %v", dd)
			}
		case "VX_GUMBORO/1":
			if str(dd, "actual_date") != "2027-10-08" {
				t.Fatalf("Gumboro L1: %v", dd)
			}
		case "VX_GUMBORO/2":
			if dd["actual_date"] != nil {
				t.Fatalf("Gumboro L2 không được điền: %v", dd)
			}
		}
	}
	// Mũi đã tiêm → job tự đóng hai cảnh báo của lứa hộ; lứa địa bàn vẫn mở.
	if _, err := f.mod.CNTSAlertJobRun(ctx, f.tenant, t0.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_alert_current WHERE tenant_id = $1 AND kind = 'vaccine_overdue' AND status = 'closed'
	    AND auto_closed AND source_ref = $2`, f.tenant, bref); n != 2 {
		t.Fatalf("tự đóng sau khi tiêm: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_alert_current WHERE tenant_id = $1 AND kind = 'vaccine_overdue' AND status <> 'closed'`,
		f.tenant); n != 2 {
		t.Fatalf("cảnh báo lứa địa bàn còn mở: %d", n)
	}
}

// bm07Household dựng dự án → điểm (xã 30001, GA_TP) → hộ mô hình do officer theo dõi; trả mã hộ và mã điểm.
func bm07Household(t *testing.T, f *fix, chu, officer user) (string, string) {
	t.Helper()
	f.xa.list = []accport.XaScope{{ActorRef: officer.ref, Communes: []string{"30001"}}}
	p := want(t, chu.post(cb+"projects", map[string]any{"code": "da-168", "name": "Mô hình gà 168", "kind": "tinh", "start_date": "2027-03-01"}),
		201, "", "dự án")
	s := want(t, chu.post(cb+"demo-sites", map[string]any{"project_ref": str(p, "ref"), "commune_code": "30001", "name": "Điểm gà 168",
		"model_type_code": "GA_TP", "officer_ref": officer.ref.String(), "diary_form": "BM-08",
		"location": map[string]any{"lat": 10.38, "lng": 105.43, "source": "map"}}), 201, "", "điểm")
	h := want(t, chu.post(cb+"model-households", map[string]any{"site_ref": str(s, "ref"), "owner_name": "Nguyễn Văn Mẫu", "owner_phone": "0900000001",
		"keeper_kind": "officer", "keeper_ref": officer.ref.String(), "location": map[string]any{"lat": 10.381, "lng": 105.431, "source": "gps"}}),
		201, "", "hộ")
	return str(h, "ref"), str(s, "ref")
}

func nc11Entry(cycle string, shot int, date string) map[string]any {
	return map[string]any{"cycle_ref": cycle, "entry_kind": "daily", "captured_at": t0.Add(-time.Hour).Format(time.RFC3339),
		"metrics":  map[string]any{"alive": "200", "dead": "0", "feed_kg": "10"},
		"tables":   map[string]any{"vaccinations": []map[string]any{{"vaccine": "Gumboro", "shot": shot, "date": date}}},
		"location": map[string]any{"lat": 10.3811, "lng": 105.4311, "accuracy_m": "8"}, "submit": true}
}

// Codex #168 [P1]: cán bộ xã 30001 gắn mã lịch theo địa bàn của xã 30002 vào lứa nhật ký của hộ mình rồi gửi NC-11 — lịch
// xã 30002 không được ghi (trước đây: ngày thực tế 08/10, phiên bản 2, người ghi là cán bộ xã 30001).
func TestBM07_NC11ScopeCrossCommune(t *testing.T) {
	f := newFix(t)
	chu, officer := f.user(tinh("chu_nhiem")), f.user(xa("30001"))
	href, _ := bm07Household(t, f, chu, officer)
	b := want(t, chu.post(cb+"vaccination-batches", map[string]any{"commune_code": "30002", "locality": "Ấp xã khác", "model_type_code": "GA_TP",
		"received_date": "2027-10-01", "age_at_receipt": 1}), 201, "", "lịch địa bàn xã 30002")
	bref := str(b, "ref")
	want(t, officer.get(cb+"vaccination-batches/"+bref), 404, "", "cán bộ xã 30001 không thấy lịch xã 30002")
	cy := want(t, officer.post(cb+"diary-cycles", map[string]any{"household_ref": href, "breed": "Gà ta", "received_date": "2027-10-01",
		"initial_count": 200, "age_days": 1, "vaccination_batch_ref": bref}), 201, "", "lứa gắn mã lịch xã khác")
	want(t, officer.post(cb+"diary-entries", nc11Entry(str(cy, "ref"), 1, "2027-10-08")), 201, "", "gửi NC-11")
	g := want(t, chu.get(cb+"vaccination-batches/"+bref), 200, "", "lịch xã 30002 sau NC-11")
	if num(g, "version") != "1" {
		t.Fatalf("lịch xã khác bị ghi: phiên bản %s, người ghi %s", num(g, "version"), str(g, "updated_by"))
	}
	for _, d := range list(g, "doses") {
		if x := d.(map[string]any); x["actual_date"] != nil {
			t.Fatalf("ghi chéo xã: %v", x)
		}
	}
}

// Codex #168 [P2]: điều chỉnh lần ghi (chỉ đổi lượng thức ăn, giữ nguyên dòng NC-11 lần 0) không điền thêm Gumboro L2.
func TestBM07_NC11RevisionStable(t *testing.T) {
	f := newFix(t)
	chu, officer := f.user(tinh("chu_nhiem")), f.user(xa("30001"))
	href, _ := bm07Household(t, f, chu, officer)
	b := want(t, chu.post(cb+"vaccination-batches", map[string]any{"household_ref": href, "received_date": "2027-10-01", "age_at_receipt": 1}),
		201, "", "lịch của hộ")
	bref := str(b, "ref")
	cy := want(t, officer.post(cb+"diary-cycles", map[string]any{"household_ref": href, "breed": "Gà ta", "received_date": "2027-10-01",
		"initial_count": 200, "age_days": 1, "vaccination_batch_ref": bref}), 201, "", "lứa gắn lịch")
	payload := nc11Entry(str(cy, "ref"), 0, "2027-10-08")
	e := want(t, officer.post(cb+"diary-entries", payload), 201, "", "gửi NC-11")
	want(t, chu.post(cb+"diary-entries/"+str(e, "ref")+"/verify", map[string]any{"base_version": 1}), 200, "", "xác thực")
	g := want(t, chu.get(cb+"vaccination-batches/"+bref), 200, "", "lịch sau NC-11")
	version := num(g, "version")
	delete(payload, "cycle_ref")
	delete(payload, "captured_at")
	delete(payload, "location")
	payload["base_version"], payload["reason"] = 2, "Sửa lượng thức ăn theo sổ giấy"
	payload["metrics"].(map[string]any)["feed_kg"] = "11"
	want(t, officer.post(cb+"diary-entries/"+str(e, "ref")+"/correct", payload), 200, "", "điều chỉnh chỉ lượng thức ăn")
	g = want(t, chu.get(cb+"vaccination-batches/"+bref), 200, "", "lịch sau điều chỉnh")
	for _, d := range list(g, "doses") {
		x := d.(map[string]any)
		switch str(x, "vaccine_code") + "/" + num(x, "shot") {
		case "VX_GUMBORO/1":
			if str(x, "actual_date") != "2027-10-08" {
				t.Fatalf("Gumboro L1: %v", x)
			}
		case "VX_GUMBORO/2":
			if x["actual_date"] != nil {
				t.Fatalf("cùng dòng NC-11 điền thêm Gumboro L2: %v (phiên bản %s)", x["actual_date"], num(g, "version"))
			}
		}
	}
	if num(g, "version") != version {
		t.Fatalf("điều chỉnh không có mũi mới vẫn tạo phiên bản lịch: %s → %s", version, num(g, "version"))
	}
}

// Codex #168 [P2]: 2.000 lứa địa bàn mới hơn cùng GA_TP ở xã khác — lọc đúng điểm/trạng thái/phạm vi xã vẫn trả lứa cũ của hộ
// (lọc trước giới hạn); job cảnh báo đọc HẾT các trang: giữ mở cảnh báo của lứa cũ và sinh cảnh báo cho lứa ở trang sau.
func TestBM07_FiltersAndAlertsBeyondCap(t *testing.T) {
	f := newFix(t)
	ctx := context.Background()
	chu, officer := f.user(tinh("chu_nhiem")), f.user(xa("30001"))
	href, sref := bm07Household(t, f, chu, officer)
	b := want(t, chu.post(cb+"vaccination-batches", map[string]any{"household_ref": href, "received_date": "2027-09-01", "age_at_receipt": 1}),
		201, "", "lứa cũ của hộ")
	bref := str(b, "ref")
	if r, err := f.mod.CNTSAlertJobRun(ctx, f.tenant, t0); err != nil || r.Vaccines != 3 {
		t.Fatalf("job đầu: %+v %v", r, err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	received := time.Date(2027, 10, 12, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2000; i++ {
		id := ids.New()
		x := calstore.Batch{Meta: calstore.Meta{ID: ids.New(), EventTime: t0, ActorRef: chu.ref, IdemKey: "cap-" + id.String()}, Ref: id,
			Event: caldomain.BatchCreated, CommuneCode: "30002", ModelType: "GA_TP", Schedule: "QT_GA",
			ScheduleValidFrom: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Locality: "Lứa mới chưa quá hạn", Received: received, AgeAtReceipt: 1,
			Doses: caldomain.GenerateDoses([]caldomain.PlanDose{{Vaccine: "VX_ND_F", Shot: 1, AgeDays: 3}}, received, 1)}
		if err := calstore.InsertBatch(ctx, tx, f.tenant, &x); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	mx := want(t, chu.get(cb+"vaccination-matrix?model_type_code=GA_TP&site_ref="+sref), 200, "", "bảng đúng điểm")
	if rows := list(mx, "rows"); len(rows) != 1 || str(rows[0].(map[string]any), "ref") != bref {
		t.Fatalf("lọc điểm sau 2.000 lứa mới hơn: %d hàng, truncated=%v", len(rows), mx["truncated"])
	}
	if l := list(want(t, chu.get(cb+"vaccination-batches?status=overdue&model_type_code=GA_TP"), 200, "", "lọc quá hạn"), "items"); len(l) != 1 ||
		str(l[0].(map[string]any), "ref") != bref {
		t.Fatalf("lọc trạng thái quá hạn: %d", len(l))
	}
	if l := list(want(t, officer.get(cb+"vaccination-batches?model_type_code=GA_TP"), 200, "", "Cấp Xã 30001"), "items"); len(l) != 1 {
		t.Fatalf("phạm vi xã trước giới hạn: %d", len(l))
	}
	if r, err := f.mod.CNTSAlertJobRun(ctx, f.tenant, t0.Add(5*time.Minute)); err != nil || r.Vaccines != 0 {
		t.Fatalf("job lại: %+v %v", r, err)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_alert_current WHERE tenant_id = $1 AND source_ref = $2 AND kind = 'vaccine_overdue'
	    AND status <> 'closed'`, f.tenant, bref); n != 3 {
		t.Fatalf("mũi cũ chưa tiêm bị tự đóng: còn mở %d, muốn 3", n)
	}
	// Lứa cũ ở trang sau vẫn sinh cảnh báo.
	want(t, chu.post(cb+"vaccination-batches", map[string]any{"commune_code": "30001", "model_type_code": "GA_TP", "locality": "Ấp cũ",
		"received_date": "2027-08-01", "age_at_receipt": 1}), 201, "", "lứa địa bàn cũ")
	if r, err := f.mod.CNTSAlertJobRun(ctx, f.tenant, t0.Add(10*time.Minute)); err != nil || r.Vaccines != 3 {
		t.Fatalf("lứa ở trang sau phải sinh cảnh báo: %+v %v", r, err)
	}
}
