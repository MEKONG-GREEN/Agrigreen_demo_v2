package handler_test

// CNTS v4.0 FN-18 (CR-CNTS-001, #254): lớp trang trại và số hộ, diện tích thủy sản bị ảnh hưởng của vùng dịch trên
// PostgreSQL/PostGIS thật. Hộ chăn nuôi (#251) và ổ dịch nhiều hộ (#252) được ghi thẳng vào sổ của pf_extension bằng vai
// quản trị kiểm thử — ADR-0056 mục 7–9.

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// farmVersion ghi một phiên bản hộ chăn nuôi (sổ lhh:<ref>, ADR-0056 mục 1).
func (f *fix) farmVersion(ref uuid.UUID, code, event, status, commune, kind string, checkin *uuid.UUID, reason string) {
	var why *string
	if reason != "" {
		why = &reason
	}
	f.exec(`INSERT INTO pf_extension.cnts_livestock_household (id, tenant_id, stream_key, household_ref, code, event, status,
	    commune_code, hamlet, person_ref, livestock_type, species_codes, head_counts, checkin_ref, reason, event_time, actor_ref,
	    idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'Ấp Long Hòa', $9, $10, '{HEO,GA}', '{350,1200}', $11, $12, now(), $13, $14,
	    'kn.cnts.livestock_household/v1')`,
		ids.New(), f.tenant, "lhh:"+ref.String(), ref, code, event, status, commune, ids.New(), kind, checkin, why, ids.New(),
		"seed:"+ids.New().String())
}

// farmCheckin ghi một lần check-in tại nhà hộ (HCN-07) và trả mã lần check-in.
func (f *fix) farmCheckin(ref uuid.UUID, lat, lng float64) *uuid.UUID {
	id := ids.New()
	f.exec(`INSERT INTO pf_extension.cnts_livestock_household_checkin (id, tenant_id, household_ref, location, gps_accuracy_m, mocked,
	    captured_at, actor_ref)
	  VALUES ($1, $2, $3, ST_SetSRID(ST_MakePoint($4, $5), 4326), 8.5, false, now(), $6)`, id, f.tenant, ref, lng, lat, ids.New())
	return &id
}

// farm ghi hộ Đang dùng (đã check-in) tại (lat, lng).
func (f *fix) farm(code, commune, kind string, lat, lng float64) uuid.UUID {
	ref := ids.New()
	f.farmVersion(ref, code, "created", "active", commune, kind, f.farmCheckin(ref, lat, lng), "")
	return ref
}

// outbreakV2 ghi một phiên bản ổ dịch nhiều hộ (rule kn.cnts.outbreak/v2) với DB-15, DB-16. ref = uuid.Nil → ổ dịch mới.
func (f *fix) outbreakV2(ref uuid.UUID, event, commune string, lat, lng float64, asOf string, households int, farmed, wild any) uuid.UUID {
	if ref == uuid.Nil {
		ref = ids.New()
	}
	f.exec(`INSERT INTO pf_extension.cnts_outbreak (id, tenant_id, stream_key, outbreak_ref, event, commune_code, disease_code,
	    species_code, received_at, source, locality, location, location_source, herd_size, dead, symptoms, as_of, event_time, actor_ref,
	    idem_key, rule_version, affected_households, aqua_farmed_ha, aqua_wild_ha)
	  VALUES ($1, $2, $3, $4, $5, $6, 'DTHCP', 'HEO', now(), 'can_bo', 'Ấp 1', ST_SetSRID(ST_MakePoint($7, $8), 4326), 'gps',
	    120, 15, 'Sốt, bỏ ăn', $9::date, now(), $10, $11, 'kn.cnts.outbreak/v2', $12, $13::numeric, $14::numeric)`,
		ids.New(), f.tenant, "outbreak:"+ref.String(), ref, event, commune, lng, lat, asOf, ids.New(), "seed-"+ids.New().String(),
		households, farmed, wild)
	return ref
}

func byKey(items []any, k string) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, it := range items {
		m := it.(map[string]any)
		out[str(m, k)] = m
	}
	return out
}

func TestCNTSMap_FarmLayerAndImpact_FN18(t *testing.T) {
	f := newFix(t)
	officer := f.user(tinh("vien_chuc"))
	xa1 := f.user(xa("30001"))
	xa2 := f.user(xa("30002"))

	// ----- Hộ chăn nuôi (ADR-0056 mục 9: lớp trang trại = hộ Đang dùng loại trang trại, vị trí check-in hiện hành) -----
	f1 := f.farm("HCN0000001", "30001", "trang_trai_vua", 10.40, 105.105) // ~550 m tâm ổ dịch
	f.farm("HCN0000002", "30001", "nong_ho", 10.40, 105.101)              // nông hộ: không thuộc lớp trang trại
	pending := ids.New()                                                  // Chờ bổ sung vị trí (BR-08): không có lớp
	f.farmVersion(pending, "HCN0000003", "created_pending", "pending_location", "30001", "trang_trai_nho", nil, "")
	off := f.farm("HCN0000004", "30001", "trang_trai_cong_nghiep", 10.40, 105.102) // vô hiệu hóa (BR-07): không có lớp
	f.farmVersion(off, "HCN0000004", "deactivated", "deactivated", "30001", "trang_trai_cong_nghiep", nil, "Ngừng chăn nuôi")
	f5 := f.farm("HCN0000005", "30002", "trang_trai_nho", 10.58, 105.10) // xã 30002, ~20 km
	f6 := f.farm("HCN0000006", "30001", "trang_trai_nho", 10.70, 105.10) // đổi vị trí (H4): lớp lấy check-in hiện hành
	f.farmVersion(f6, "HCN0000006", "relocated", "active", "30001", "trang_trai_nho", f.farmCheckin(f6, 10.401, 105.10),
		"Chuyển chuồng về ấp mới")

	// ----- Ổ dịch: OB1 nhiều hộ có 2 lần diễn biến; OB2 dữ liệu cũ một hộ; OB3 xã 30002 -----
	ob1 := f.outbreakV2(uuid.Nil, "reported", "30001", 10.40, 105.10, "2027-10-10", 2, "1.5", "0.25")
	f.outbreakV2(ob1, "updated", "30001", 10.40, 105.10, "2027-10-12", 3, "2", "0.5")
	ob2 := f.seedOutbreak("30001", 10.41, 105.10, "2027-10-10")                                     // ~1,1 km
	ob3 := f.outbreakV2(uuid.Nil, "reported", "30002", 10.42, 105.10, "2027-10-10", 4, "0.75", nil) // ~2,2 km

	fo := want(t, officer.post(base+"zones/from-outbreak", map[string]any{"outbreak_ref": ob1.String(), "effective_on": "2027-10-10"}),
		201, "", "khoanh vùng từ OB1")
	zones := byKey(list(fo, "items"), "kind")
	dich, uyHiep := str(zones["vung_dich"], "ref"), str(zones["vung_uy_hiep"], "ref")
	livestock := want(t, officer.post(base+"zones", map[string]any{"kind": "vung_chan_nuoi", "name": "Vùng chăn nuôi tập trung",
		"communes": []string{"30001"}, "draw_mode": "polygon", "geometry": square(105.09, 10.39, 0.03), "effective_on": "2027-10-10"}),
		201, "", "vùng chăn nuôi")

	// ----- Lớp trang trại theo phạm vi (FN-18 AC-02) -----
	lt := want(t, officer.get(base+"layers"), 200, "", "lớp Cấp Tỉnh")
	farms := byKey(list(lt, "farms"), "household_ref")
	if len(farms) != 3 || farms[f1.String()] == nil || farms[f5.String()] == nil || farms[f6.String()] == nil {
		t.Fatalf("lớp trang trại Cấp Tỉnh phải đúng 3 hộ Đang dùng loại trang trại: %s", lt["farms"])
	}
	fm := farms[f1.String()]
	animals := list(fm, "animals")
	if str(fm, "code") != "HCN0000001" || str(fm, "livestock_type") != "trang_trai_vua" || str(fm, "livestock_type_label") != "Trang trại vừa" ||
		str(fm, "hamlet") != "Ấp Long Hòa" || len(animals) != 2 || num(animals[0].(map[string]any), "head_count") != "350" ||
		str(animals[0].(map[string]any), "species_name") != "Heo" {
		t.Fatalf("trang trại: %s", fm)
	}
	for _, k := range []string{"person_ref", "owner_name", "owner_phone", "name", "phone"} {
		if _, ok := fm[k]; ok {
			t.Fatalf("lớp trang trại không được có dữ liệu cá nhân %q: %s", k, fm)
		}
	}
	if lat := num(farms[f6.String()], "lat"); lat != "10.401" {
		t.Fatalf("hộ đổi vị trí phải theo check-in hiện hành: lat %s", lat)
	}
	if l := want(t, xa1.get(base+"layers"), 200, "", "lớp xã 30001"); len(byKey(list(l, "farms"), "household_ref")) != 2 {
		t.Fatalf("xã 30001 chỉ thấy trang trại xã mình: %s", l["farms"])
	}
	if l := want(t, xa2.get(base+"layers"), 200, "", "lớp xã 30002"); len(list(l, "farms")) != 1 ||
		str(list(l, "farms")[0].(map[string]any), "household_ref") != f5.String() {
		t.Fatalf("xã 30002: %s", l["farms"])
	}

	// ----- Ổ dịch hiện DB-15, DB-16, cờ dữ liệu cũ -----
	obs := byKey(list(lt, "outbreaks"), "ref")
	if o := obs[ob1.String()]; num(o, "affected_households") != "3" || str(o, "aqua_farmed_ha") != "2.00" || str(o, "aqua_wild_ha") != "0.50" ||
		o["legacy"] != false || num(o, "version") != "2" {
		t.Fatalf("OB1 lấy phiên bản mới nhất: %s", o)
	}
	if o := obs[ob2.String()]; num(o, "affected_households") != "1" || o["aqua_farmed_ha"] != nil || o["legacy"] != true {
		t.Fatalf("OB2 dữ liệu cũ một hộ: %s", o)
	}
	if o := obs[ob3.String()]; num(o, "affected_households") != "4" || str(o, "aqua_farmed_ha") != "0.75" || o["aqua_wild_ha"] != nil {
		t.Fatalf("OB3: %s", o)
	}

	// ----- Số hộ, diện tích bị ảnh hưởng của vùng (FN-18 BR-02; FN-11 BR-03: phiên bản mới nhất, không cộng dồn) -----
	imp := byKey(list(lt, "zone_impacts"), "zone_ref")
	if len(imp) != 3 || imp[str(livestock, "ref")] != nil {
		t.Fatalf("chỉ vùng dịch, vùng bị uy hiếp, vùng đệm có số ảnh hưởng: %s", lt["zone_impacts"])
	}
	if z := imp[dich]; num(z, "outbreak_count") != "1" || num(z, "affected_households") != "3" || str(z, "aqua_farmed_ha") != "2.00" ||
		str(z, "aqua_wild_ha") != "0.50" {
		t.Fatalf("vùng dịch 500 m: %s", z)
	}
	if z := imp[uyHiep]; num(z, "outbreak_count") != "3" || num(z, "affected_households") != "8" || str(z, "aqua_farmed_ha") != "2.75" ||
		str(z, "aqua_wild_ha") != "0.50" {
		t.Fatalf("vùng bị uy hiếp 3 km (OB1 3 + OB2 1 + OB3 4): %s", z)
	}
	if l := want(t, xa1.get(base+"layers"), 200, "", "lớp xã 30001"); num(byKey(list(l, "zone_impacts"), "zone_ref")[uyHiep],
		"affected_households") != "4" {
		t.Fatalf("Cấp Xã chỉ cộng ổ dịch xã mình (OB3 thuộc 30002): %s", l["zone_impacts"])
	}
	// Ngày cũ (AC-03): diễn biến lúc đó của OB1 là lần đầu.
	old := byKey(list(want(t, officer.get(base+"layers?date=2027-10-11"), 200, "", "ngày 11/10"), "zone_impacts"), "zone_ref")
	if z := old[dich]; num(z, "affected_households") != "2" || str(z, "aqua_farmed_ha") != "1.50" || str(z, "aqua_wild_ha") != "0.25" {
		t.Fatalf("vùng dịch ngày 11/10: %s", z)
	}

	// ----- Đối tượng trong vùng -----
	in := want(t, officer.get(base+"zones/"+uyHiep+"/contents"), 200, "", "trong vùng 3 km")
	inFarms := byKey(list(in, "farms"), "household_ref")
	if num(in, "farm_count") != "2" || inFarms[f1.String()] == nil || inFarms[f6.String()] == nil || inFarms[f1.String()]["distance_m"] == nil {
		t.Fatalf("trang trại trong vùng 3 km: %s", in["farms"])
	}
	if im := in["impact"].(map[string]any); num(im, "affected_households") != "8" || num(im, "outbreak_count") != "3" {
		t.Fatalf("số ảnh hưởng trong vùng 3 km: %s", im)
	}
	in = want(t, officer.get(base+"zones/"+dich+"/contents"), 200, "", "trong vùng dịch 500 m")
	if num(in, "farm_count") != "1" || str(list(in, "farms")[0].(map[string]any), "household_ref") != f6.String() {
		t.Fatalf("trang trại trong vùng 500 m: %s", in["farms"])
	}
	if c := want(t, officer.get(base+"zones/"+str(livestock, "ref")+"/contents"), 200, "", "trong vùng chăn nuôi"); c["impact"] != nil ||
		num(c, "farm_count") != "2" {
		t.Fatalf("vùng chăn nuôi không có số ảnh hưởng: %s", c)
	}

	// ----- Cách ly tenant (RLS) -----
	other := f.userIn(ids.New(), tinh("vien_chuc"))
	if l := want(t, other.get(base+"layers"), 200, "", "tenant khác"); len(list(l, "farms")) != 0 || len(list(l, "zone_impacts")) != 0 {
		t.Fatalf("tenant khác thấy trang trại: %s", l)
	}
	want(t, other.do(http.MethodGet, base+"zones/"+dich+"/contents", "", nil), 404, "extension.cnts_zone_not_found", "tenant khác")
}
