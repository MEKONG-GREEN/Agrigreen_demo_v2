package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// CGH FN-09 (dự báo nhu cầu, phương pháp tạm cgh.forecast/v1 — cần DCRD xác nhận) và G-10 (Mã HTX trong kết quả cân đối).
func TestForecastFN09AndCoopCodeG10(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()
	o1 := adm.owner(f.c1, "0906100001")
	for _, sn := range []string{"F1", "F2", "F3"} {
		adm.machine(f.c1, o1, "MLD", sn, "2025-06-01")
	}
	adm.area(f.c1, "DX", 2023, "400", false)
	adm.area(f.c1, "DX", 2024, "440", false)
	adm.area(f.c1, "DX", 2025, "484", false)
	adm.area(f.c2, "DX", 2025, "500", false)

	r := adm.mustOK(adm.post("/forecasts", map[string]any{"season_code": "DX", "season_year": 2026}), http.StatusCreated, "chạy dự báo")
	run := r.body["run"].(map[string]any)
	if run["rule_version"] != "cgh.forecast/v1" || run["method_provisional"] != true || run["reference_date"] != "2026-11-01" {
		t.Fatalf("lần chạy: %v", run)
	}
	id := run["id"].(string)
	v := adm.mustOK(f.viewerUser().get("/forecasts/"+id), http.StatusOK, "đọc dự báo").body
	rows := map[string]map[string]any{}
	for _, it := range list(v, "results") {
		rows[it["cooperative_ref"].(string)+"|"+it["machine_type"].(string)] = it
	}
	c1 := rows[f.c1.String()+"|MLD"]
	// 484 × (1 + 10 %) = 532,4 ha ÷ 100 ha/máy = 5,324 → 6 máy; đang có 3 → thiếu 3.
	if str(c1, "forecast_area_ha") != "532.4" || str(c1, "growth_pct") != "10" || str(c1, "required") != "5.324" ||
		num(c1, "required_whole") != 6 || num(c1, "capacity_now") != 3 || num(c1, "gap") != -3 || c1["status"] != "ok" {
		t.Fatalf("HTX c1: %v", c1)
	}
	if in := c1["inputs"].(map[string]any); len(in["rates"].(map[string]any)) != 2 || in["capped"] != false {
		t.Fatalf("đầu vào lưu kèm: %v", in)
	}
	if c2 := rows[f.c2.String()+"|MLD"]; str(c2, "forecast_area_ha") != "500" || str(c2, "growth_pct") != "0" {
		t.Fatalf("HTX c2 (chỉ 1 năm): %v", c2)
	}
	if rg := rows[f.c1.String()+"|MGD"]; rg["status"] != "no_norm" {
		t.Fatalf("máy gặt chưa có định mức: %v", rg)
	}
	// Bất biến: định mức mới áp cho lần chạy mới; lần cũ giữ nguyên; chuỗi băm hợp lệ.
	f.params.approve("cgh.norm_ha_per_machine:MLD:lam_dat", "50", "2026-06-01")
	r2 := adm.mustOK(adm.post("/forecasts", map[string]any{"season_code": "DX", "season_year": 2026}), http.StatusCreated, "chạy lại")
	id2 := r2.body["run"].(map[string]any)["id"].(string)
	old := list(adm.mustOK(adm.get("/forecasts/"+id+"?cooperative_ref="+f.c1.String()+"&machine_type=MLD"), http.StatusOK, "lần cũ").body, "results")
	neu := list(adm.mustOK(adm.get("/forecasts/"+id2+"?cooperative_ref="+f.c1.String()+"&machine_type=MLD"), http.StatusOK, "lần mới").body, "results")
	if len(old) != 1 || num(old[0], "required_whole") != 6 || len(neu) != 1 || num(neu[0], "required_whole") != 11 {
		t.Fatalf("lần cũ %v / lần mới %v", old, neu)
	}
	if runs := list(adm.mustOK(adm.get("/forecasts?season_code=DX&season_year=2026"), http.StatusOK, "danh sách").body, "items"); len(runs) != 2 {
		t.Fatalf("hai lần chạy: %v", runs)
	}
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		res, err := ledger.Verify(context.Background(), tx, "pf_mechanization.forecast_result", f.tenant, "frun:"+id)
		if err != nil || !res.OK || res.RowsChecked == 0 {
			t.Fatalf("chuỗi forecast_result: %+v %v", res, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE pf_mechanization.forecast_result SET gap = 0 WHERE run_id = $1", id)
		return err
	})
	if err == nil {
		t.Fatal("sổ dự báo phải bất biến")
	}
	if r := f.viewerUser().post("/forecasts", map[string]any{"season_code": "DX", "season_year": 2026}); r.status != http.StatusForbidden {
		t.Fatalf("Cục KTHT chỉ xem: %d", r.status)
	}

	// G-10: Mã HTX và nhãn nguồn diện tích trong bảng "HTX cần ưu tiên".
	adm.compute("DX", 2025)
	dash := adm.mustOK(adm.get("/dashboard?season_code=DX&season_year=2025"), http.StatusOK, "dashboard").body
	pr := list(dash, "priority")
	if len(pr) != 2 || pr[0]["cooperative_code"] != "HTX000002" || pr[1]["cooperative_code"] != "HTX000001" || pr[0]["area_source_label"] != "Nhập tay" {
		t.Fatalf("G-10: %v", pr)
	}
}
