package kn77_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// legacyLedger dựng lại trạng thái sổ ghi TRƯỚC bản nâng cấp review #128 cho lần gửi did (đang "sending" lượt 1): ghi các
// dòng sổ (append-only, trigger hash + RLS giữ nguyên) với dấu người nhận dạng cũ dispatch-mailed:<id>:<lượt>:<người nhận>,
// rồi đặt trạng thái chiếu status và nhả việc. Không đi qua mã nguồn sản phẩm (archlint R-02).
func legacyLedger(t *testing.T, f *fixture, did string, attempt int, mailed []string, final, status string) {
	t.Helper()
	ctx := context.Background()
	ins := func(tx pgx.Tx, action, payload, idem string) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_reports.dispatch_event (id, tenant_id, stream_key, dispatch_id, action, payload, department_code,
		    province_code, event_time, actor_ref, idem_key, rule_version)
		  SELECT gen_random_uuid(), d.tenant_id, 'dispatch:' || d.id::text, d.id, $3, $4::jsonb, d.department_code, d.province_code, $5,
		    d.created_by, $6, 'reports.dispatch.v1'
		  FROM pf_reports.dispatch d WHERE d.tenant_id = $1 AND d.id::text = $2`, f.tenant, did, action, payload, f.clk.Now(), idem)
		return err
	}
	if err := tenant.WithTenantTx(ctx, f.env.AdminPool(t, migrations.PF), f.tenant, func(tx pgx.Tx) error {
		for _, r := range mailed {
			if err := ins(tx, "recipient_mailed", fmt.Sprintf(`{"attempt":%d,"recipient_id":%q}`, attempt, r),
				fmt.Sprintf("dispatch-mailed:%s:%d:%s", did, attempt, r)); err != nil {
				return err
			}
		}
		if err := ins(tx, final, fmt.Sprintf(`{"attempt":%d}`, attempt), "legacy-"+final+":"+did); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE pf_reports.dispatch SET status = $3, claim_token = NULL, claim_until = NULL
		  WHERE tenant_id = $1 AND id::text = $2 AND attempts = $4`, f.tenant, did, status, attempt)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// TestP0WorkerReports_LegacyPartialFailureRetryKeepsMailed — review Codex #128 (59a4ec8f): lần gửi lỗi một phần TRƯỚC nâng
// cấp (lượt 1 đã gửi người 1, dấu dạng cũ theo lượt; lỗi ở người 2). "Gửi lại" sau nâng cấp tạo lượt 2 cùng vòng: dấu cũ của
// lượt 1 (vòng chưa hoàn tất) vẫn được tôn trọng — người 1 không nhận lần hai, người 2 nhận một lần.
func TestP0WorkerReports_LegacyPartialFailureRetryKeepsMailed(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	did, rids := dispatchWithTwoRecipients(t, c)
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi (trước nâng cấp)")
	legacyLedger(t, f, did, 1, rids[:1], "failed", "failed")
	m := &countMailer{n: map[string]int{}}
	f.rep.UseMailer(m)
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi lại sau nâng cấp")
	evs := f.events(repport.EventDispatchRequested)
	if a := attemptOf(t, evs[len(evs)-1]); a != 2 {
		t.Fatalf("gửi lại phải là lượt 2, có %d", a)
	}
	if err := deliver(f, evs[len(evs)-1]); err != nil {
		t.Fatal(err)
	}
	if m.count(claimAddr1) != 0 || m.count(claimAddr2) != 1 {
		t.Fatalf("gửi lại lần lỗi một phần trước nâng cấp gửi trùng người đã nhận: %v", m.n)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch WHERE tenant_id = $1 AND id::text = $2 AND status = 'sent' AND attempts = 2`,
		f.tenant, did); n != 1 {
		t.Fatal("trạng thái sent ở lượt 2")
	}
}

// TestP0WorkerReports_LegacyMarksIgnoredAfterSentRound — chiều ngược lại: vòng trước nâng cấp đã "sent" (dấu dạng cũ của
// lượt 1 cho cả hai người). "Gửi lại" chủ động sau đó là vòng mới → mọi người nhận lại; dấu cũ KHÔNG được tôn trọng vô điều kiện.
func TestP0WorkerReports_LegacyMarksIgnoredAfterSentRound(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	did, rids := dispatchWithTwoRecipients(t, c)
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi (trước nâng cấp)")
	legacyLedger(t, f, did, 1, rids, "sent", "sent")
	m := &countMailer{n: map[string]int{}}
	f.rep.UseMailer(m)
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi vòng mới")
	evs := f.events(repport.EventDispatchRequested)
	if err := deliver(f, evs[len(evs)-1]); err != nil {
		t.Fatal(err)
	}
	if m.count(claimAddr1) != 1 || m.count(claimAddr2) != 1 {
		t.Fatalf("vòng mới sau sent phải gửi mọi người dù còn dấu dạng cũ: %v", m.n)
	}
}
