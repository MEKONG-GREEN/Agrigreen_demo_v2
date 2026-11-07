package handler_test

// CNTS-V4-OUTBREAK-MULTI (#252, ADR-0056 mục 7–8, BRD CNTS v4.0 FN-11 BR-03, AC-04, AC-05; UR-XA-10 DB-04, DB-14…DB-16;
// FN-10 BR-06, BR-07, BR-08, AC-07, AC-09): ổ dịch nhiều hộ trên PostgreSQL thật.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func types(m map[string]any) []string {
	var out []string
	for _, h := range list(m, "households") {
		out = append(out, str(h.(map[string]any), "livestock_type"))
	}
	return out
}

func TestFN11_MultiHousehold_252(t *testing.T) {
	f := newFix(t)
	xa1 := f.user(xa("30001"))
	xa2 := f.user(xa("30002"))
	th := f.user(authz.KNContext{Role: authz.KNTinh, Department: cnts, ProvinceGroup: "tong_hop", Scope: authz.Scope{Units: []string{"89", "30001", "30002"}}})
	const p = "/v1/extension/cnts/outbreaks"
	const lh = "/v1/extension/cnts/livestock-households"

	a := hh(t, xa1, "Trần Văn A", "30001", "nong_ho")
	b := hh(t, xa1, "Trần Văn B", "30001", "trang_trai_nho")
	c := hh(t, xa1, "Trần Văn C", "30001", "trang_trai_cong_nghiep")
	other := hh(t, xa2, "Phạm Văn D", "30002", "nong_ho")
	gone := hh(t, xa1, "Trần Văn E", "30001", "nong_ho")
	want(t, th.post(lh+"/"+gone+"/deactivate", map[string]any{"base_version": 1, "reason": "Hộ đã giải thể"}), http.StatusOK, "", "vô hiệu hóa hộ")

	// DB-04: hộ ngoài danh sách, xã khác, vô hiệu hóa (BR-07, AC-07), lặp, household_ref cũ → chặn.
	want(t, xa1.post(p, with(report(), "household_refs", refs(ids.New().String()))), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "hộ không có")
	want(t, xa1.post(p, with(report(), "household_refs", refs(other))), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "hộ xã khác")
	want(t, xa1.post(p, with(report(), "household_refs", refs(a, gone))), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "hộ vô hiệu hóa")
	want(t, xa1.post(p, with(report(), "household_refs", refs(a, a))), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "hộ lặp")
	want(t, xa1.post(p, with(report(), "household_refs", refs(a), "household_ref", a)), http.StatusUnprocessableEntity,
		"extension.cnts_outbreak_invalid", "household_ref v1")

	// AC-04: chọn 3 hộ → DB-15 = 3, không sửa tay; DB-14 lấy theo từng hộ.
	ob := want(t, xa1.post(p, with(report(), "household_refs", refs(a, b, c), "affected_households", 9)), http.StatusCreated, "", "3 hộ")
	ref := str(ob, "ref")
	if num(ob, "affected_households") != "3" || ob["legacy"] != false || len(list(ob, "households")) != 3 ||
		types(ob)[0] != "nong_ho" || types(ob)[1] != "trang_trai_nho" || types(ob)[2] != "trang_trai_cong_nghiep" {
		t.Fatalf("AC-04, DB-14: %v", ob)
	}

	// BR-08, AC-09: nhập hộ chưa có → hộ "Chờ bổ sung vị trí" trong danh sách, gắn ổ dịch tạo ra nó; cán bộ chọn DB-14.
	nh := map[string]any{"owner_name": "Võ Thị Mới", "owner_phone": "0913000456", "hamlet": "Ấp Mới", "livestock_type": "trang_trai_vua",
		"animals": []any{map[string]any{"species_code": "heo", "head_count": 300}}}
	want(t, xa1.post(p, with(report(), "new_households", []any{with(nh, "owner_name", "")})), http.StatusUnprocessableEntity,
		"extension.cnts_outbreak_invalid", "hộ mới thiếu họ tên")
	want(t, xa1.post(p, with(report(), "new_households", []any{with(nh, "hamlet", "Gọi 0912345678")})), http.StatusUnprocessableEntity,
		"extension.cnts_outbreak_invalid", "ấp có SĐT")
	want(t, xa1.post(p, with(report(), "new_households", []any{with(nh, "animals", []any{map[string]any{"species_code": "khong_co", "head_count": 1}})})),
		http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "vật nuôi ngoài danh mục")
	ob2 := want(t, xa1.post(p, with(report(), "household_refs", refs(a), "new_households", []any{nh})), http.StatusCreated, "", "hộ mới tại DB-04")
	if num(ob2, "affected_households") != "2" || types(ob2)[1] != "trang_trai_vua" {
		t.Fatalf("hộ mới: %v", ob2)
	}
	newHH := str(list(ob2, "households")[1].(map[string]any), "household_ref")
	nd := want(t, xa1.get(lh+"/"+newHH), http.StatusOK, "", "hộ mới trong danh sách")
	if str(nd, "status") != "pending_location" || str(nd, "event") != "created_pending" || str(nd, "livestock_type") != "trang_trai_vua" {
		t.Fatalf("BR-08: %v", nd)
	}
	var origin uuid.UUID
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT origin_outbreak_ref FROM pf_extension.cnts_livestock_household
		  WHERE household_ref = $1`, newHH).Scan(&origin)
	}); err != nil || origin.String() != str(ob2, "ref") {
		t.Fatalf("origin_outbreak_ref = %v (%v), muốn %s", origin, err, str(ob2, "ref"))
	}
	// Hộ Chờ bổ sung vị trí dùng được ở báo cáo dịch bệnh khác (BR-08).
	want(t, xa1.post(p, with(report(), "household_refs", refs(newHH))), http.StatusCreated, "", "chọn hộ chờ vị trí")

	// BR-06: hộ đổi loại sau đó không đổi phiên bản ổ dịch đã ghi.
	want(t, xa1.post(lh+"/"+a, map[string]any{"base_version": 1, "commune_code": "30001", "hamlet": "Ấp Trần Văn A",
		"livestock_type": "trang_trai_vua", "animals": []any{map[string]any{"species_code": "heo", "head_count": 50}}}), http.StatusOK, "", "đổi loại hộ")
	d := want(t, xa1.get(p+"/"+ref), http.StatusOK, "", "chi tiết")
	cur := obj(d, "current")
	if types(cur)[0] != "nong_ho" || str(list(cur, "households")[0].(map[string]any), "owner_label") == "" {
		t.Fatalf("BR-06 chụp DB-14, nhãn chủ hộ: %v", cur)
	}

	// Diễn biến: đổi tập hộ (bỏ c, thêm hộ mới chụp loại hiện hành); phiên bản cũ giữ tập cũ.
	v2 := want(t, xa1.post(p+"/"+ref+"/progress", with(report(), "base_version", 1, "household_refs", refs(a, b), "as_of", "2027-10-13")),
		http.StatusCreated, "", "diễn biến 2 hộ")
	if num(v2, "affected_households") != "2" || types(v2)[0] != "trang_trai_vua" {
		t.Fatalf("phiên bản 2: %v", v2)
	}
	cl := want(t, th.post(p+"/"+ref+"/close", map[string]any{"base_version": 2, "reason": "Hết 21 ngày"}), http.StatusCreated, "", "kết thúc")
	if num(cl, "affected_households") != "2" || len(list(cl, "households")) != 2 || cl["legacy"] != false {
		t.Fatalf("bản kết thúc chụp hộ: %v", cl)
	}
	vs := list(want(t, th.get(p+"/"+ref), http.StatusOK, "", "lịch sử"), "versions")
	if len(vs) != 3 || num(vs[2].(map[string]any), "affected_households") != "3" {
		t.Fatalf("BR-02 phiên bản giữ tập hộ của nó: %v", vs)
	}
	// FN-10 BR-07, AC-07: hộ vô hiệu hóa sau đó vẫn hiện ở báo cáo cũ (kèm trạng thái hiện hành), không chọn được ở báo cáo mới.
	want(t, th.post(lh+"/"+c+"/deactivate", map[string]any{"base_version": 1, "reason": "Trang trại ngừng nuôi"}), http.StatusOK, "", "vô hiệu hóa c")
	v1 := list(want(t, th.get(p+"/"+ref), http.StatusOK, "", "báo cáo cũ"), "versions")[2].(map[string]any)
	if hs := list(v1, "households"); len(hs) != 3 || str(hs[2].(map[string]any), "status") != "deactivated" ||
		str(hs[2].(map[string]any), "livestock_type") != "trang_trai_cong_nghiep" {
		t.Fatalf("AC-07 báo cáo cũ giữ hộ đã vô hiệu hóa: %v", v1)
	}
	want(t, xa1.post(p, with(report(), "household_refs", refs(c))), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "AC-07 chọn hộ vô hiệu hóa")

	// AC-05, UR-XA-10: bệnh nhóm thủy sản bắt buộc DB-16 (nuôi trồng, khai thác); ≥ 0, ≤ 2 số lẻ; bệnh khác không ghi.
	aq := with(report(), "household_refs", refs(b), "disease_code", "ahpnd", "species_code", "tom")
	want(t, xa1.post(p, aq), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "AC-05 thiếu DB-16")
	want(t, xa1.post(p, with(aq, "aqua_farmed_ha", "2.5")), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "thiếu khai thác")
	want(t, xa1.post(p, with(aq, "aqua_farmed_ha", "1e100000", "aqua_wild_ha", "0")), http.StatusUnprocessableEntity, "validation_failed", "DB-16 1e100000")
	want(t, xa1.post(p, with(aq, "aqua_farmed_ha", "1.234", "aqua_wild_ha", "0")), http.StatusUnprocessableEntity, "validation_failed", "DB-16 3 số lẻ")
	want(t, xa1.post(p, with(aq, "aqua_farmed_ha", "-1", "aqua_wild_ha", "0")), http.StatusUnprocessableEntity, "validation_failed", "DB-16 âm")
	ok := want(t, xa1.post(p, with(aq, "aqua_farmed_ha", "2.50", "aqua_wild_ha", "0")), http.StatusCreated, "", "DB-16 đủ")
	if str(ok, "aqua_farmed_ha") != "2.5" || str(ok, "aqua_wild_ha") != "0" {
		t.Fatalf("DB-16: %v", ok)
	}
	// Bệnh chưa khai loài: xét nhóm của vật nuôi ổ dịch.
	want(t, xa1.post(p, with(aq, "disease_code", "dom_trang")), http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "nhóm theo vật nuôi")
	want(t, xa1.post(p, with(report(), "household_refs", refs(b), "aqua_farmed_ha", "1", "aqua_wild_ha", "1")),
		http.StatusUnprocessableEntity, "extension.cnts_outbreak_invalid", "DB-16 với bệnh không thủy sản")
}

// ADR-0056 mục 8: ổ dịch cũ một hộ (v1) giữ nguyên, API trả legacy = true; cập nhật diễn biến tạo phiên bản v2.
func TestFN11_LegacyOutbreak_252(t *testing.T) {
	f := newFix(t)
	xa1 := f.user(xa("30001"))
	tinh := f.user(authz.KNContext{Role: authz.KNTinh, Department: cnts, Scope: authz.Scope{Units: []string{"89", "30001"}}})
	const p = "/v1/extension/cnts/outbreaks"
	ref, oldHH := ids.New(), ids.New()
	ctx := context.Background()
	if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_outbreak (id, tenant_id, stream_key, outbreak_ref, event, commune_code,
		    disease_code, species_code, received_at, source, household_ref, locality, location, location_source, herd_size, dead,
		    symptoms, as_of, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, $3, $4, 'reported', '30001', 'dtlcp', 'heo', $5, 'can_bo', $6, 'Ấp cũ',
		    ST_SetSRID(ST_MakePoint(105.97, 10.25), 4326), 'gps', 80, 4, 'Sốt', '2027-10-10', $5, $7, $8, 'kn.cnts.outbreak/v1')`,
			ids.New(), f.tenant, "outbreak:"+ref.String(), ref, t0.Add(-72*time.Hour), oldHH, ids.New(), "legacy-"+ref.String())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	d := want(t, xa1.get(p+"/"+ref.String()), http.StatusOK, "", "ổ dịch cũ")
	cur := obj(d, "current")
	if cur["legacy"] != true || num(cur, "affected_households") != "1" || len(list(cur, "households")) != 0 ||
		str(cur, "household_ref") != oldHH.String() || str(cur, "locality") != "Ấp cũ" || cur["aqua_farmed_ha"] != nil {
		t.Fatalf("dữ liệu cũ: %v", cur)
	}
	// Kết thúc/mở lại chụp lại nội dung cũ — vẫn là dữ liệu cũ.
	cl := want(t, tinh.post(p+"/"+ref.String()+"/close", map[string]any{"base_version": 1, "reason": "Hết dịch"}), http.StatusCreated, "", "kết thúc v1")
	if cl["legacy"] != true || str(cl, "household_ref") != oldHH.String() {
		t.Fatalf("kết thúc ổ dịch cũ: %v", cl)
	}
	want(t, tinh.post(p+"/"+ref.String()+"/reopen", map[string]any{"base_version": 2, "reason": "Tái phát"}), http.StatusCreated, "", "mở lại v1")
	// Cập nhật diễn biến: bắt buộc chọn hộ theo quy tắc v2.
	want(t, xa1.post(p+"/"+ref.String()+"/progress", with(report(), "base_version", 3)), http.StatusUnprocessableEntity,
		"extension.cnts_outbreak_invalid", "v1 → cập nhật phải chọn hộ")
	h := hh(t, xa1, "Đỗ Văn Cũ", "30001", "nong_ho")
	v4 := want(t, xa1.post(p+"/"+ref.String()+"/progress", with(report(), "base_version", 3, "household_refs", refs(h))), http.StatusCreated, "", "v1 → v2")
	if v4["legacy"] != false || num(v4, "affected_households") != "1" || v4["household_ref"] != nil {
		t.Fatalf("phiên bản v2: %v", v4)
	}
	vs := list(want(t, xa1.get(p+"/"+ref.String()), http.StatusOK, "", "lịch sử"), "versions")
	if len(vs) != 4 || vs[3].(map[string]any)["legacy"] != true || vs[0].(map[string]any)["legacy"] != false {
		t.Fatalf("lịch sử giữ phiên bản cũ: %v", vs)
	}
}
