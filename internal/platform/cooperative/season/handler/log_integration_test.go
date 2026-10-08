package handler_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// HTX US-LOG-01 (GPS + thời gian tự đính, máy móc), HTX US-LOG-02 (ghi hộ; AC-2 chặn
// khi chưa mở vụ), HTX US-GPS-01/02 (nhãn vị trí không chặn lưu).
func TestFarmLogRecord_HTX_US_LOG_01_02(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	fa := f.farmer(f.h1)
	r := fa.log(f.p1, nil)
	expectCode(t, r, http.StatusConflict, "cropcycle.season_not_open", "chưa mở vụ")
	if str(r.body, "title") != "Lô ruộng này chưa được mở vụ. Vui lòng mở vụ trước khi ghi nhật ký." {
		t.Fatalf("thông báo: %s", r.raw)
	}
	season := fa.mustOpen(f.p1)
	f.managerA().mustOpen(f.p2)

	expectCode(t, fa.log(f.p1, map[string]any{"machine_codes": []string{"OLD"}}), http.StatusUnprocessableEntity, "cropcycle.catalog_invalid", "máy ngừng dùng")
	expectCode(t, fa.log(f.p1, map[string]any{"sop_step_seq": 9}), http.StatusUnprocessableEntity, "validation_failed", "bước không có")
	expectCode(t, fa.log(f.p1, map[string]any{"note": "Liên hệ 0912 345 678"}), http.StatusUnprocessableEntity, "validation_failed", "SĐT trong ghi chú")
	expectCode(t, fa.log(f.p1, map[string]any{"activity_code": "nhay_mua"}), http.StatusUnprocessableEntity, "validation_failed", "hoạt động lạ")
	expectCode(t, fa.log(f.p2, nil), http.StatusForbidden, "cropcycle.out_of_scope", "thửa của hộ khác")
	expectCode(t, f.managerB().log(f.p1, nil), http.StatusForbidden, "cropcycle.out_of_scope", "HTX khác")

	r = fa.log(f.p1, nil)
	expect(t, r, http.StatusCreated, "ghi nhật ký")
	l := get(r.body, "log")
	if str(l, "season_ref") != season || str(l, "location_status") != "match" || str(l, "gps", "source") != "device" ||
		str(l, "event_time") == "" || len(list(l, "machine_codes")) != 1 || str(list(l, "inputs")[0], "qty") != "50" ||
		get(l, "on_behalf") != false || str(r.body, "state", "status") != "pending" {
		t.Fatalf("nhật ký: %s", r.raw)
	}
	// Quản lý HTX ghi hộ (US-LOG-02 AC-1); GPS bị từ chối → "Thiếu GPS" (US-GPS-01 AC-4).
	r = f.managerA().log(f.p1, map[string]any{"gps": map[string]any{"source": "denied"}, "activity_code": "kiem_tra", "sop_step_seq": nil})
	expect(t, r, http.StatusCreated, "ghi hộ")
	if get(r.body, "log", "on_behalf") != true || str(r.body, "log", "location_status") != "missing" {
		t.Fatalf("ghi hộ: %s", r.raw)
	}
	// Thửa lệch 120 m → "Vị trí không khớp", vẫn lưu (US-GPS-01 AC-3).
	r = f.managerA().log(f.p2, nil)
	expect(t, r, http.StatusCreated, "ghi nhật ký lệch vị trí")
	if str(r.body, "log", "location_status") != "mismatch" || str(r.body, "log", "distance_m") != "120.0" {
		t.Fatalf("nhãn lệch: %s", r.raw)
	}
	// Cán bộ KN xã đọc được nhật ký, không ghi được.
	if n := len(list(f.officerA().get("/v1/cropcycle/farm-logs").body, "items")); n != 3 {
		t.Fatalf("KN xã thấy 3 nhật ký, có %d", n)
	}
	expectCode(t, f.officerA().log(f.p1, nil), http.StatusForbidden, "forbidden", "KN không ghi")
	if ev := f.outbox(port.EventFarmLogRecorded); len(ev) != 3 {
		t.Fatalf("farm_log.recorded: %d", len(ev))
	}
	// Gọi lặp cùng Idempotency-Key.
	k := key()
	a := fa.postKey("/v1/cropcycle/farm-logs", k, map[string]any{"plot_ref": f.p1.String(), "activity_code": "tuoi_nuoc"})
	b := fa.postKey("/v1/cropcycle/farm-logs", k, map[string]any{"plot_ref": f.p1.String(), "activity_code": "tuoi_nuoc"})
	if a.status != http.StatusCreated || b.status != http.StatusCreated || str(a.body, "log", "id") != str(b.body, "log", "id") {
		t.Fatalf("gọi lặp: %s / %s", a.raw, b.raw)
	}
}

// HTX US-LOG-01 AC-3: sửa nhật ký = bản ghi bù, GIỮ thời gian và GPS; sửa trên phiên
// bản cũ → concurrent_edit; quản lý HTX sửa hộ là bản ghi bù có lý do (X1). Sổ chỉ ghi thêm.
func TestFarmLogCorrection_HTX_US_LOG_01_AC3(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	fa := f.farmer(f.h1)
	fa.mustOpen(f.p1)
	r := fa.log(f.p1, nil)
	expect(t, r, http.StatusCreated, "ghi")
	v1 := str(r.body, "log", "id")
	at := str(r.body, "log", "event_time")
	path := "/v1/cropcycle/farm-logs/" + v1 + "/corrections"
	expectCode(t, fa.post(path, map[string]any{"activity_code": "bon_phan", "correction_reason": "Sửa lượng phân bón",
		"event_time": "2026-09-28T01:00:00Z"}), http.StatusUnprocessableEntity, "cropcycle.locked_fields", "đổi thời gian")
	expectCode(t, fa.post(path, map[string]any{"activity_code": "bon_phan", "correction_reason": "Sửa lượng phân bón",
		"gps": map[string]any{"lon": "105.7", "lat": "10.05"}}), http.StatusUnprocessableEntity, "cropcycle.locked_fields", "đổi GPS")
	r = fa.post(path, map[string]any{"activity_code": "bon_phan", "sop_step_seq": 3, "correction_reason": "Sửa lượng phân bón",
		"inputs": []map[string]any{{"name": "NPK 20-20-15", "qty": "45.5", "unit": "kg"}}})
	expect(t, r, http.StatusCreated, "sửa nhật ký")
	v2 := str(r.body, "log", "id")
	if str(r.body, "log", "event_time") != at || str(r.body, "log", "gps", "lon") != "105.6000000" || num(r.body, "log", "version") != 2 ||
		str(r.body, "log", "supersedes_id") != v1 || str(r.body, "log", "log_ref") != v1 {
		t.Fatalf("bản ghi bù phải giữ thời gian + GPS: %s", r.raw)
	}
	old := fa.get("/v1/cropcycle/farm-logs/" + v1)
	if str(old.body, "state", "status") != "superseded" || str(old.body, "state", "superseded_by") != v2 || len(list(old.body, "versions")) != 2 {
		t.Fatalf("phiên bản cũ: %s", old.raw)
	}
	expectCode(t, fa.post(path, map[string]any{"activity_code": "bon_phan", "correction_reason": "Sửa trên bản đã bị thay"}),
		http.StatusConflict, "cropcycle.concurrent_edit", "sửa bản cũ")
	r = f.managerA().post("/v1/cropcycle/farm-logs/"+v2+"/corrections", map[string]any{"activity_code": "bon_phan",
		"correction_reason": "Quản lý HTX sửa hộ theo biên bản"})
	expect(t, r, http.StatusCreated, "sửa hộ")
	if get(r.body, "log", "on_behalf") != true || str(r.body, "log", "author_ref") != f.managerA().actor.String() && str(r.body, "log", "author_ref") == fa.actor.String() {
		t.Fatalf("sửa hộ: %s", r.raw)
	}
	err := tenant.WithTenantTx(context.Background(), f.db, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE pf_coop.farm_log SET note = 'sửa lén' WHERE id = $1::uuid", v1)
		return err
	})
	if !errors.Is(ledger.Classify(err), ledger.ErrImmutable) {
		t.Fatalf("sổ farm_log phải chặn UPDATE: %v", err)
	}
	var ok bool
	if err := tenant.WithTenantTx(context.Background(), f.db, f.tenant, func(tx pgx.Tx) error {
		res, err := ledger.Verify(context.Background(), tx, "pf_coop.farm_log", f.tenant, "farm_log:"+v1)
		ok = res.OK && res.RowsChecked == 3
		return err
	}); err != nil || !ok {
		t.Fatalf("chuỗi băm nhật ký: %v %v", ok, err)
	}
}

// HTX US-LOG-03: ghi hàng loạt = một đầu lô + một dòng mỗi thửa; lô chưa mở vụ hoặc
// ngoài phạm vi bị loại kèm lý do.
func TestFarmLogBatch_HTX_US_LOG_03(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	mgr := f.managerA()
	mgr.mustOpen(f.p1)
	mgr.mustOpen(f.p2)
	f.managerB().mustOpen(f.p4)
	r := mgr.post("/v1/cropcycle/farm-logs/batch", map[string]any{"plot_refs": []string{f.p1.String(), f.p2.String(), f.p3.String(), f.p4.String()},
		"activity_code": "bon_phan", "sop_step_seq": 3, "machine_codes": []string{"MPT"}, "note": "Bón thúc cả tổ"})
	expect(t, r, http.StatusCreated, "ghi hàng loạt")
	if num(r.body, "requested") != 4 || num(r.body, "accepted") != 2 || len(list(r.body, "lines")) != 2 {
		t.Fatalf("lô: %s", r.raw)
	}
	reasons := map[string]string{}
	for _, s := range list(r.body, "skipped") {
		reasons[str(s, "plot_ref")] = str(s, "reason")
	}
	if reasons[f.p3.String()] != "Lô chưa mở vụ" || reasons[f.p4.String()] != "Lô không thuộc quyền quản lý" {
		t.Fatalf("lý do bỏ qua: %v", reasons)
	}
	batch := str(r.body, "batch_ref")
	for _, l := range list(r.body, "lines") {
		if str(l, "batch_ref") != batch || get(l, "on_behalf") != true || str(l, "gps", "source") != "manual_pick" {
			t.Fatalf("dòng lô: %v", l)
		}
	}
	expectCode(t, mgr.post("/v1/cropcycle/farm-logs/batch", map[string]any{"plot_refs": []string{f.p3.String()}, "activity_code": "bon_phan"}),
		http.StatusUnprocessableEntity, "cropcycle.batch_empty", "không có lô hợp lệ")
}

// HTX US-LOG-05: duyệt / yêu cầu bổ sung; người ghi không tự duyệt; chỉ nhật ký ĐÃ DUYỆT
// tính vào tiến độ SOP trên bảng điều hành (AC-2).
func TestFarmLogReview_HTX_US_LOG_05(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP OM5451", "OM5451")
	fa := f.farmer(f.h1)
	mgr := f.managerA()
	fa.mustOpen(f.p1)
	r := fa.log(f.p1, map[string]any{"gps": map[string]any{"lon": "105.6", "lat": "10.05"}})
	expect(t, r, http.StatusCreated, "ghi")
	v1 := str(r.body, "log", "id")
	pending := mgr.get("/v1/cropcycle/farm-logs?status=pending")
	if len(list(pending.body, "items")) != 1 || str(list(pending.body, "items")[0], "location_status") != "match" {
		t.Fatalf("danh sách chờ duyệt kèm nhãn vị trí (AC-1): %s", pending.raw)
	}
	expectCode(t, mgr.post("/v1/cropcycle/farm-logs/"+v1+"/reviews", map[string]any{"decision": "needs_supplement"}),
		http.StatusUnprocessableEntity, "validation_failed", "yêu cầu bổ sung thiếu ghi chú")
	expectCode(t, f.managerB().post("/v1/cropcycle/farm-logs/"+v1+"/reviews", map[string]any{"decision": "approved"}),
		http.StatusNotFound, "cropcycle.log_not_found", "HTX khác duyệt")
	expectCode(t, fa.post("/v1/cropcycle/farm-logs/"+v1+"/reviews", map[string]any{"decision": "approved"}),
		http.StatusForbidden, "forbidden", "nông dân không duyệt")
	r = mgr.post("/v1/cropcycle/farm-logs/"+v1+"/reviews", map[string]any{"decision": "needs_supplement", "note": "Bổ sung ảnh bao phân"})
	expect(t, r, http.StatusCreated, "yêu cầu bổ sung")
	if str(r.body, "state", "status") != "needs_supplement" {
		t.Fatalf("trạng thái: %s", r.raw)
	}
	rv := f.outbox(port.EventFarmLogReviewed)
	if len(rv) != 1 || str(rv[0], "author_ref") != fa.actor.String() || str(rv[0], "note") != "Bổ sung ảnh bao phân" {
		t.Fatalf("farm_log.reviewed báo người ghi (AC-3): %v", rv)
	}
	expectCode(t, mgr.post("/v1/cropcycle/farm-logs/"+v1+"/reviews", map[string]any{"decision": "approved"}),
		http.StatusConflict, "cropcycle.log_state", "duyệt lại khi đang chờ bổ sung")
	dash := func() float64 {
		d := mgr.get("/v1/cropcycle/dashboard")
		expect(t, d, http.StatusOK, "bảng điều hành")
		return num(d.body, "totals", "approved_logs")
	}
	if dash() != 0 {
		t.Fatal("nhật ký chưa duyệt không được tính")
	}
	r = fa.post("/v1/cropcycle/farm-logs/"+v1+"/corrections", map[string]any{"activity_code": "bon_phan", "sop_step_seq": 3,
		"correction_reason": "Bổ sung theo yêu cầu", "file_refs": []string{"0192f000-0000-7000-8000-000000000001"}})
	expect(t, r, http.StatusCreated, "bổ sung")
	v2 := str(r.body, "log", "id")
	expect(t, mgr.post("/v1/cropcycle/farm-logs/"+v2+"/reviews", map[string]any{"decision": "approved"}), http.StatusCreated, "duyệt")
	if dash() != 1 {
		t.Fatal("nhật ký đã duyệt phải tính vào bảng điều hành")
	}
	d := mgr.get("/v1/cropcycle/dashboard")
	if str(d.body, "totals", "sop_progress_pct") != "20.0" {
		t.Fatalf("tiến độ SOP 1/5 bước: %s", d.raw)
	}
	// Người ghi không tự duyệt: quản lý ghi hộ rồi tự duyệt → chặn.
	r = mgr.log(f.p1, map[string]any{"activity_code": "kiem_tra", "sop_step_seq": nil})
	expect(t, r, http.StatusCreated, "ghi hộ")
	expectCode(t, mgr.post("/v1/cropcycle/farm-logs/"+str(r.body, "log", "id")+"/reviews", map[string]any{"decision": "approved"}),
		http.StatusForbidden, "cropcycle.self_review", "tự duyệt")
}
