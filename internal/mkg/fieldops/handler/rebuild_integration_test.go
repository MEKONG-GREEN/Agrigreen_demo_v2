package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// fieldopsProjections là mọi bảng chiếu fieldops (kể cả hai bảng đệm không dựng lại — băm phải giữ nguyên).
var fieldopsProjections = []string{store.ProjOrders, store.ProjTotals, store.ProjRecon, store.ProjCrews, store.ProjAssignments,
	store.ProjParams, "weighed_loading_current", "order_plot_harvest_current"}

// dumpProjections chụp nội dung bảng chiếu của tenant (bỏ updated_at — DEFAULT now()), sắp theo văn bản.
func (f *fixture) dumpProjections() map[string][]string {
	f.t.Helper()
	out := map[string][]string{}
	f.inTenant(func(tx pgx.Tx) error {
		for _, t := range fieldopsProjections {
			rows, err := tx.Query(context.Background(), "SELECT (to_jsonb(x) - 'updated_at')::text AS r FROM mkg_fieldops."+t+" x ORDER BY r")
			if err != nil {
				return err
			}
			xs, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
			out[t] = xs
		}
		return nil
	})
	return out
}

func sameDump(t *testing.T, want, got map[string][]string, what string) {
	t.Helper()
	for _, tb := range fieldopsProjections {
		a, b := want[tb], got[tb]
		if len(a) != len(b) {
			t.Fatalf("%s: %s %d dòng ≠ %d dòng\nmuốn %v\nđược %v", what, tb, len(a), len(b), a, b)
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("%s: %s dòng %d khác\nmuốn %s\nđược %s", what, tb, i, a[i], b[i])
			}
		}
	}
}

// R-22 (G4, A-4): POST /v1/fieldops/projections/rebuild dựng lại sáu bảng chiếu fieldops thẳng từ sổ = bảng
// chiếu sống (lệnh gom đã duyệt/giao/đóng + lệnh bị hủy, đếm cuộn ba công đoạn + bản bù, đối soát chiều mua có xác
// nhận rồi MKG sửa → lệch, tổ gom sửa + ngừng, phân công, tham số duyệt + từ chối); sync_seq giữ nguyên (con trỏ
// pull của app không lùi); bảng chiếu bị sửa tay → dựng lại về đúng sổ; chỉ field_supervisor; bảng đệm không đổi.
func TestFieldopsProjectionRebuildEqualsLive_R22(t *testing.T) {
	f := newFixture(t, nil)
	col, sup := f.collector(), f.supervisor()
	id := f.approvedOrder()
	crew := f.crew("TO-RB1", "500")
	mustStatus(t, f.assign(sup, id, crew, 200, nil), http.StatusCreated, "giao tổ")
	p0, p1 := f.plots[0], f.plots[1]
	mustStatus(t, f.count(col, id, p0, port.StageBale, 120, nil), http.StatusCreated, "bale p0")
	mustStatus(t, f.count(col, id, p0, port.StageGather, 120, nil), http.StatusCreated, "gather p0")
	bl := f.count(col, id, p0, port.StageBoatLoad, 120, nil)
	mustStatus(t, bl, http.StatusCreated, "boat_load p0")
	mustStatus(t, f.count(col, id, p1, port.StageBale, 80, nil), http.StatusCreated, "bale p1")
	mustStatus(t, sup.post("/v1/fieldops/orders/"+id+"/htx-confirmations", map[string]any{"plot_ref": p0.String(), "outcome": "confirmed",
		"event_time": f.ts(0)}), http.StatusCreated, "HTX xác nhận")
	mustStatus(t, col.post("/v1/fieldops/roll-counts/"+str(bl.body, "roll_count", "id")+"/corrections", map[string]any{
		"rolls": 118, "discrepancy_reason": "hai cuộn rơi xuống kênh", "correction_reason": "đếm lại khi cập bến", "event_time": f.ts(0)}),
		http.StatusCreated, "sửa boat_load")
	mustStatus(t, sup.post("/v1/fieldops/crews/"+crew+"/revisions", crewBody("TO-RB1", "650")), http.StatusCreated, "sửa tổ")
	other := f.crew("TO-RB2", "300")
	mustStatus(t, sup.post("/v1/fieldops/crews/"+other+"/deactivate", map[string]any{"reason": "tổ giải thể sau vụ"}), http.StatusCreated, "ngừng tổ")
	cancelled := f.approvedOrder()
	mustStatus(t, sup.post("/v1/fieldops/orders/"+cancelled+"/cancel", map[string]any{"reason": "HTX hoãn gom vì mưa lớn"}), http.StatusCreated, "hủy")
	a, b := f.supervisor(), f.supervisor()
	for i, dec := range []string{"approved", "rejected"} {
		pr := a.post("/v1/fieldops/params/baling_deadline_days/versions", map[string]any{"value": []string{"7", "9"}[i],
			"reason": "khách chốt hạn cuộn mùa khô", "effective_from": f.clk.Now().AddDate(0, 0, i).Format("2006-01-02")})
		mustStatus(t, pr, http.StatusCreated, "đề xuất tham số")
		body := map[string]any{"decision": dec}
		if dec == "rejected" {
			body["reason"] = "chưa có biên bản của khách"
		}
		mustStatus(t, b.post("/v1/fieldops/params/baling_deadline_days/versions/"+str(pr.body, "param", "version_id")+"/decision", body),
			http.StatusCreated, "quyết định tham số")
	}
	// Bảng đệm nguồn ngoài có dữ liệu (không có sổ riêng) — dựng lại phải giữ nguyên.
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO mkg_fieldops.weighed_loading_current (tenant_id, order_id, htx_ref, rolls, weighed_kg, weighed_at)
		  VALUES ($1, $2, $3, 118, 2596.000, $4)`, f.tenant, parseID(t, id), f.htx, f.clk.Now().Add(-time.Minute))
		return err
	})

	live := f.dumpProjections()
	if len(live[store.ProjOrders]) != 2 || len(live[store.ProjTotals]) != 4 || len(live[store.ProjRecon]) != 1 || len(live[store.ProjCrews]) != 2 ||
		len(live[store.ProjAssignments]) != 1 || len(live[store.ProjParams]) != 2 {
		t.Fatalf("dữ liệu sống chưa đủ: %v", live)
	}
	var syncBefore int64
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT sum(sync_seq) FROM mkg_fieldops.collection_order_current").Scan(&syncBefore)
	})

	mustCode(t, col.post("/v1/fieldops/projections/rebuild", nil), http.StatusForbidden, "forbidden", "tổ gom không dựng lại")
	r := sup.post("/v1/fieldops/projections/rebuild", nil)
	mustStatus(t, r, http.StatusOK, "dựng lại")
	if str(r.body, store.ProjOrders) != "2" || str(r.body, store.ProjTotals) != "4" || str(r.body, store.ProjRecon) != "1" {
		t.Fatalf("số dòng dựng lại: %s", r.raw)
	}
	sameDump(t, live, f.dumpProjections(), "dựng lại = sống")

	// Bảng chiếu lệch sổ (sửa thẳng bằng quản trị cụm) → dựng lại về đúng sổ, sync_seq không lùi.
	admin := f.env.AdminPool(t, migrations.MKG)
	for _, q := range []string{
		"UPDATE mkg_fieldops.roll_total_current SET rolls = 1 WHERE tenant_id = $1",
		"UPDATE mkg_fieldops.buyside_reconciliation_current SET status = 'match', htx_rolls = 118 WHERE tenant_id = $1",
		"UPDATE mkg_fieldops.crew_current SET name = 'sai' WHERE tenant_id = $1",
		"DELETE FROM mkg_fieldops.param_current WHERE tenant_id = $1",
		"UPDATE mkg_fieldops.collection_order_current SET status = 'closed' WHERE tenant_id = $1 AND status = 'cancelled'",
	} {
		if _, err := admin.Exec(context.Background(), q, f.tenant); err != nil {
			t.Fatal(err)
		}
	}
	n, err := f.svc.RebuildTenant(context.Background(), f.tenant)
	if err != nil || n[store.ProjParams] != 2 {
		t.Fatalf("RebuildTenant: %v %v", n, err)
	}
	sameDump(t, live, f.dumpProjections(), "sau khi sửa tay")
	var syncAfter int64
	f.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT sum(sync_seq) FROM mkg_fieldops.collection_order_current").Scan(&syncAfter)
	})
	if syncAfter != syncBefore {
		t.Fatalf("sync_seq đổi khi dựng lại: %d → %d", syncBefore, syncAfter)
	}
	// Tenant khác không bị đụng (RB-2): dựng lại tenant rỗng không lỗi, không dòng.
	other2 := f.as(parseID(t, crew), port.RoleFieldSupervisor)
	if r := other2.post("/v1/fieldops/projections/rebuild", nil); r.status != http.StatusOK || str(r.body, store.ProjOrders) != "0" {
		t.Fatalf("tenant khác: %s", r.raw)
	}
	sameDump(t, live, f.dumpProjections(), "sau khi tenant khác dựng lại")
}
