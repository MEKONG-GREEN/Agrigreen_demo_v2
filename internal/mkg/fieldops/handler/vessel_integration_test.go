package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// G4 tích hợp mục 2 (proto FM-05): số hiệu ghe/sà lan TÙY CHỌN khi xếp ghe (boat_load) — qua POST roll-counts và
// sync push của app hiện trường; chuẩn hóa như danh mục ghe (GH-01), ghi sổ roll_count_vessel, mang trong
// mkg.fieldops.roll_count.recorded v1 (trường tùy chọn vessel_code, tương thích ngược); công đoạn khác → 422;
// bản bù không gửi số hiệu giữ số hiệu của bản bị bù; vắng số hiệu → payload không có vessel_code (app cũ).
func TestRollCountVesselCodeOptional_FM05(t *testing.T) {
	f := newFixture(t, nil)
	order := f.approvedOrder()
	col := f.collector()
	for _, st := range []string{"bale", "gather"} {
		mustStatus(t, f.count(col, order, f.plots[0], st, 100, nil), http.StatusCreated, st)
		mustStatus(t, f.count(col, order, f.plots[1], st, 50, nil), http.StatusCreated, st)
	}
	mustCode(t, f.count(col, order, f.plots[0], "gather", 100, map[string]any{"vessel_code": "CT-1"}), http.StatusUnprocessableEntity,
		"validation_failed", "số hiệu ở công đoạn gather")
	mustCode(t, f.count(col, order, f.plots[0], "boat_load", 100, map[string]any{"vessel_code": "CT_1"}), http.StatusUnprocessableEntity,
		"validation_failed", "ký tự lạ")
	r := f.count(col, order, f.plots[0], "boat_load", 100, map[string]any{"vessel_code": " ct 12 345 "})
	mustStatus(t, r, http.StatusCreated, "xếp ghe có số hiệu")
	if str(r.body, "roll_count", "vessel_code") != "CT12345" {
		t.Fatalf("chuẩn hóa: %s", r.raw)
	}
	rc := str(r.body, "roll_count", "id")
	// App hiện trường (sync push) gửi số hiệu trong payload.
	col.device = ids.New().String()
	push := col.post("/v1/fieldops/sync/push", map[string]any{"device_id": col.device, "records": []any{
		map[string]any{"id": ids.New().String(), "idem_key": "dev:" + col.device + ":1", "entity": "roll_count", "op": "create",
			"event_time": f.ts(-time.Minute), "payload": map[string]any{"order_id": order, "plot_ref": f.plots[1].String(), "stage": "boat_load",
				"rolls": 50, "vessel_code": "đt-0099"}}}})
	mustStatus(t, push, http.StatusOK, "push")
	if res := arr(push.body, "results"); len(res) != 1 || str(res[0], "status") != "accepted" {
		t.Fatalf("push: %s", push.raw)
	}
	// Bản bù không gửi số hiệu → giữ số hiệu cũ.
	mustStatus(t, col.post("/v1/fieldops/roll-counts/"+rc+"/corrections", map[string]any{"rolls": 90, "discrepancy_reason": "10 cuộn ướt để lại bờ",
		"correction_reason": "Đếm lại khi ghe rời bến", "event_time": f.ts(-time.Second)}), http.StatusCreated, "bù")

	c := f.supervisor().get("/v1/fieldops/orders/" + order + "/roll-counts")
	codes := map[string]string{}
	for _, x := range arr(c.body, "counts") {
		if str(x, "stage") == "boat_load" {
			codes[str(x, "rolls")] = str(x, "vessel_code")
		}
	}
	if codes["100"] != "CT12345" || codes["90"] != "CT12345" || codes["50"] != "ĐT-0099" {
		t.Fatalf("đọc lại số hiệu: %v — %s", codes, c.raw)
	}
	// Sự kiện nội bộ mang vessel_code (boat_load) và KHÔNG có trường này ở công đoạn khác.
	var withCode, without int
	f.inTenant(func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT payload::text FROM shared.outbox WHERE event_type = $1 AND kind = 'internal'`,
			port.EventRollCountRecorded)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var p port.RollCountRecordedV1
			if err := json.Unmarshal([]byte(raw), &p); err != nil {
				return err
			}
			switch {
			case p.Stage == port.StageBoatLoad && p.VesselCode != nil:
				withCode++
			case p.Stage != port.StageBoatLoad && p.VesselCode == nil:
				without++
			}
		}
		return rows.Err()
	})
	if withCode != 3 || without != 4 {
		t.Fatalf("payload roll_count.recorded: có số hiệu %d (muốn 3), công đoạn khác không có %d (muốn 4)", withCode, without)
	}
	// RB-1: sổ roll_count_vessel chỉ ghi thêm.
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE mkg_fieldops.roll_count_vessel SET vessel_code = 'X'")
		return err
	}); err == nil {
		t.Fatal("roll_count_vessel phải chặn UPDATE")
	}
}
