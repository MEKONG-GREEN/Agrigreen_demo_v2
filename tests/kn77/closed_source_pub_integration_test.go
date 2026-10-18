package kn77_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// TestW11_10_ClosedSourcePubView: view nguồn module cho KHTC pf_extension_pub.v1_consolidation_closed (pf 000144) —
// một dòng mỗi kỳ × biểu có khóa kỳ đã lưu; số liệu = lần tổng hợp mới nhất ghi không muộn hơn lần chốt gần nhất chạm
// biểu (QĐ-6 mặc định: ảnh chụp đã chốt); lần "Cập nhật" tay sau khi mở lại không lọt vào cho tới khi chốt lại; cờ chốt
// biểu/kỳ/khóa kỳ (Q-048: mọi biểu nguồn của tuần đã chốt).
func TestW11_10_ClosedSourcePubView(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	c.issue(t, "T01", "T05")
	c1 := f.communes89[0]

	// asReader chạy fn như bên đọc thật: giao dịch tenant + SET LOCAL ROLE r_extension_pub (chỉ quyền trên view _pub;
	// RLS bảng gốc áp qua chủ view nên phải có app.tenant_id).
	asReader := func(fn func(tx pgx.Tx) error) {
		t.Helper()
		if err := tenant.WithTenantTx(context.Background(), f.env.AdminPool(t, migrations.PF), f.tenant, func(tx pgx.Tx) error {
			if _, err := tx.Exec(context.Background(), "SET LOCAL ROLE r_extension_pub"); err != nil {
				return err
			}
			return fn(tx)
		}); err != nil {
			t.Fatal(err)
		}
	}
	// row đọc một dòng của view.
	type row struct {
		key                         string
		formClosed, periodClosed    bool
		keyClosed, hasRun           bool
		provinceM, closedAt, runRef string
	}
	read := func(period, form string) row {
		t.Helper()
		var r row
		var m, at, run *string
		asReader(func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), `
		    SELECT period_key, form_closed, period_closed, key_closed, run_id IS NOT NULL, closed_at::text, run_id::text,
		           (SELECT x->'cells'->>'M' FROM jsonb_array_elements(totals) x WHERE x->>'kind' = 'province' AND coalesce(x->>'indicator', '') = '' LIMIT 1)
		      FROM pf_extension_pub.v1_consolidation_closed
		     WHERE tenant_id = $1 AND period_id = $2::uuid AND form_code = $3`, f.tenant, period, form).
				Scan(&r.key, &r.formClosed, &r.periodClosed, &r.keyClosed, &r.hasRun, &at, &run, &m)
		})
		for _, p := range []struct {
			dst *string
			src *string
		}{{&r.provinceM, m}, {&r.closedAt, at}, {&r.runRef, run}} {
			if p.src != nil {
				*p.dst = *p.src
			}
		}
		return r
	}
	var n int
	asReader(func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM pf_extension_pub.v1_consolidation_closed WHERE batch_id = $1::uuid
		    AND department_code = 'trong_trot' AND period_type = 'week'`, c.batch).Scan(&n)
	})
	if n != 4 {
		t.Fatalf("2 kỳ × 2 biểu: %d dòng", n)
	}
	// Không có app.tenant_id → RLS bảng gốc áp qua view: 0 dòng.
	if m := f.count(`SELECT count(*) FROM pf_extension_pub.v1_consolidation_closed WHERE batch_id = $1::uuid`, c.batch); m != 0 {
		t.Fatalf("view phải áp RLS bảng gốc: %d dòng", m)
	}

	// Chưa chốt: cờ false, không có số đã chốt — kể cả khi đã có lần "Cập nhật" tay.
	tid := str(c.task(t, c.xa1, "T01", c1, c.p1), "id")
	c.xa1.must(submit(c.xa1, tid, "submitted", map[string]string{"F": "20"}), http.StatusCreated, "gửi")
	c.tinh.must(c.tinh.post("/v1/extension/consolidations", map[string]any{"period_id": c.p1}), http.StatusCreated, "cập nhật tay trước chốt")
	if r := read(c.p1, "T01"); r.key != "2026-W37" || r.formClosed || r.periodClosed || r.keyClosed || r.hasRun || r.closedAt != "" {
		t.Fatalf("trước chốt: %+v", r)
	}

	// Chốt: không có thay đổi so với lần tay → Hệ thống không ghi lần mới cho T01; số đã chốt = lần tay (≤ lúc chốt).
	c.tinh.must(c.tinh.post("/v1/extension/report-periods/"+c.p1+"/close", map[string]any{}), http.StatusOK, "chốt kỳ")
	closed := read(c.p1, "T01")
	if !closed.formClosed || !closed.periodClosed || !closed.keyClosed || !closed.hasRun || closed.provinceM != "20" || closed.closedAt == "" {
		t.Fatalf("sau chốt: %+v", closed)
	}
	if r := read(c.p1, "T05"); !r.formClosed || !r.keyClosed {
		t.Fatalf("T05 sau chốt: %+v", r)
	}
	// Kỳ 2 (tuần khác) chưa chốt: không ảnh hưởng khóa W37.
	if r := read(c.p2, "T01"); r.key != "2026-W38" || r.formClosed || r.keyClosed || r.hasRun {
		t.Fatalf("kỳ 2: %+v", r)
	}

	// Mở lại một xã × T01 → nguồn đang sửa: cờ T01/kỳ/khóa false, T05 vẫn chốt; số vẫn là ảnh chụp lúc chốt dù xã gửi
	// lại và Cấp Tỉnh "Cập nhật" tay (số tạm).
	until := f.clk.Now().Add(48 * time.Hour).Format(time.RFC3339)
	c.tinh.must(c.tinh.post("/v1/extension/report-periods/"+c.p1+"/reopen", map[string]any{"communes": []string{c1}, "template_ids": []string{c.forms["T01"]},
		"reason": "Xã phát hiện nhập nhầm số tuần 1", "until": until}), http.StatusOK, "mở lại")
	c.xa1.must(submit(c.xa1, tid, "submitted", map[string]string{"F": "26"}), http.StatusCreated, "gửi lại khi mở")
	c.tinh.must(c.tinh.post("/v1/extension/consolidations", map[string]any{"period_id": c.p1}), http.StatusCreated, "cập nhật tay khi mở lại")
	if r := read(c.p1, "T01"); r.formClosed || r.periodClosed || r.keyClosed || r.provinceM != "20" || r.runRef != closed.runRef || r.closedAt != closed.closedAt {
		t.Fatalf("đang mở lại: %+v (lúc chốt %+v)", r, closed)
	}
	if r := read(c.p1, "T05"); !r.formClosed || r.periodClosed || r.keyClosed {
		t.Fatalf("T05 khi T01 mở lại: %+v", r)
	}

	// Hết hạn mở → job chốt lại: số mới nhất ≤ lần chốt lại (lần tay khi mở lại đã có số 26), cờ chốt trở lại.
	f.clk.Advance(49 * time.Hour)
	if jr, err := f.ext.ReportCycleJobRun(context.Background(), f.tenant, f.clk.Now()); err != nil || jr.Reclosed != 1 {
		t.Fatalf("job chốt lại: %+v %v", jr, err)
	}
	if r := read(c.p1, "T01"); !r.formClosed || !r.periodClosed || !r.keyClosed || r.provinceM != "26" || r.runRef == closed.runRef || r.closedAt == closed.closedAt {
		t.Fatalf("sau chốt lại: %+v", r)
	}
}
