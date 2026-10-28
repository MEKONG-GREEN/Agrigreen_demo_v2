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
	if !strings.Contains(cell("A1"), "LỊCH TIÊM PHÒNG VACCIN GÀ THƯƠNG PHẨM NĂM 2027") || cell("A4") != "STT" || cell("C4") != "Họ tên nông dân" ||
		cell("D4") != "CB theo dõi MH" || cell("F5") != "Lần 1" || cell("F6") != "Ngày tiêm dự kiến" || cell("G6") != "Ngày tiêm thực tế" ||
		cell("A7") != "1" || cell("B7") != "Xã 30001" || cell("E7") != "01/10/2027" || cell("H7") != "07/10/2027" || cell("I7") != "Quá hạn" {
		t.Fatalf("bố cục BM-07: A1=%q A4=%q F5=%q F6=%q A7=%q B7=%q E7=%q H7=%q I7=%q", cell("A1"), cell("A4"), cell("F5"), cell("F6"),
			cell("A7"), cell("B7"), cell("E7"), cell("H7"), cell("I7"))
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
