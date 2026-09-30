package handler_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// G4 đóng khoảng trống (web thay "mã vừa xem"): danh sách phân trang theo con trỏ — lô có giá thành (tổng hợp như
// /lots/{id}/cost, mới nhất trước), doanh thu carbon đã ghi (lọc kỳ), job đóng kỳ (lọc kỳ / trạng thái); con trỏ
// sai 422; tenant khác không thấy (RB-2).
func TestCostingListsCursor_G4(t *testing.T) {
	f := newFixture(t)
	acct := f.accountant()
	now := f.clk.Now()
	lots := []uuid.UUID{ids.New(), ids.New(), ids.New()}
	for i, lot := range lots {
		for _, cat := range []string{"storage", "handling"} {
			mustStatus(t, acct.app(http.MethodPost, "/v1/costing/entries", map[string]any{"lot_ref": lot.String(), "category": cat,
				"amount": []string{"100.00", "250.50", "1000.00"}[i], "event_time": now.Add(-time.Hour).Format(time.RFC3339),
				"note": "Chi phí kho bãi ghi tay (kiểm thử danh sách)"}), http.StatusCreated, "bút toán tay")
		}
	}
	p1 := acct.app(http.MethodGet, "/v1/costing/lots?limit=2", nil)
	mustStatus(t, p1, http.StatusOK, "trang 1 lô")
	items := listOf(p1.body, "items")
	next := str(p1.body, "next_cursor")
	if len(items) != 2 || str(items[0], "lot_ref") != lots[2].String() || str(items[0], "total_cost") != "2000.00" ||
		len(listOf(items[0], "categories")) != 2 || items[0]["cost_per_tonne_undetermined"] != true || next != lots[1].String() {
		t.Fatalf("trang 1 lô: %s", p1.raw)
	}
	p2 := acct.app(http.MethodGet, "/v1/costing/lots?limit=2&cursor="+next, nil)
	if items := listOf(p2.body, "items"); len(items) != 1 || str(items[0], "lot_ref") != lots[0].String() || str(items[0], "total_cost") != "200.00" ||
		p2.body["next_cursor"] != nil {
		t.Fatalf("trang 2 lô: %s", p2.raw)
	}
	mustStatus(t, acct.app(http.MethodGet, "/v1/costing/lots?cursor=abc", nil), http.StatusUnprocessableEntity, "con trỏ sai")

	var carbon []string
	for _, amt := range []string{"1000000.00", "2000000.00", "3000000.00"} {
		r := acct.app(http.MethodPost, "/v1/costing/carbon-revenue", map[string]any{"amount": amt, "reference": "VCS đợt thử"})
		mustStatus(t, r, http.StatusCreated, "carbon")
		carbon = append(carbon, str(r.body, "id"))
	}
	c1 := acct.app(http.MethodGet, "/v1/costing/carbon-revenue?limit=2", nil)
	if items := listOf(c1.body, "items"); len(items) != 2 || str(items[0], "id") != carbon[2] || str(items[0], "retained_amount") != "1350000.00" ||
		str(items[0], "reference") != "VCS đợt thử" || str(c1.body, "next_cursor") != carbon[1] {
		t.Fatalf("carbon trang 1: %s", c1.raw)
	}
	period := str(listOf(c1.body, "items")[0], "period_ref")
	if c := acct.app(http.MethodGet, "/v1/costing/carbon-revenue?period_ref="+period, nil); len(listOf(c.body, "items")) != 3 {
		t.Fatalf("carbon theo kỳ: %s", c.raw)
	}
	if c := acct.app(http.MethodGet, "/v1/costing/carbon-revenue?period_ref="+ids.New().String(), nil); len(listOf(c.body, "items")) != 0 {
		t.Fatalf("carbon kỳ khác: %s", c.raw)
	}

	// Job đóng kỳ: kỳ tháng trước (đã hết) có bút toán → yêu cầu đóng → job queued.
	code, from, _ := f.prevMonth()
	mustStatus(t, acct.app(http.MethodPost, "/v1/costing/entries", map[string]any{"lot_ref": lots[0].String(), "category": "storage",
		"amount": "10.00", "event_time": from.Add(48 * time.Hour).Format(time.RFC3339), "note": "Chi phí kho kỳ trước (kiểm thử)"}),
		http.StatusCreated, "bút toán kỳ trước")
	pid := f.periodID(f.tenant, code)
	closer := f.closer()
	r := closer.app(http.MethodPost, "/v1/costing/periods/"+pid.String()+"/close", nil)
	mustStatus(t, r, http.StatusAccepted, "đóng kỳ")
	job := str(r.body, "job_id")
	j := closer.app(http.MethodGet, "/v1/costing/period-close-jobs?period_id="+pid.String(), nil)
	if items := listOf(j.body, "items"); len(items) != 1 || str(items[0], "job_id") != job || str(items[0], "status") != "queued" ||
		str(items[0], "period_code") != code {
		t.Fatalf("job đóng kỳ: %s", j.raw)
	}
	if j := closer.app(http.MethodGet, "/v1/costing/period-close-jobs?status=completed", nil); len(listOf(j.body, "items")) != 0 {
		t.Fatalf("lọc completed: %s", j.raw)
	}
	mustStatus(t, closer.app(http.MethodGet, "/v1/costing/period-close-jobs?status=done", nil), http.StatusUnprocessableEntity, "trạng thái lạ")

	// RB-2: tenant khác không thấy gì.
	o := f.as(ids.New(), port.RoleCostAccountant)
	for _, p := range []string{"/v1/costing/lots", "/v1/costing/carbon-revenue", "/v1/costing/period-close-jobs"} {
		if r := o.app(http.MethodGet, p, nil); r.status != http.StatusOK || len(listOf(r.body, "items")) != 0 {
			t.Fatalf("RB-2 %s: %s", p, r.raw)
		}
	}
}

func listOf(m map[string]any, k string) []map[string]any {
	xs, _ := m[k].([]any)
	out := make([]map[string]any, 0, len(xs))
	for _, x := range xs {
		o, _ := x.(map[string]any)
		out = append(out, o)
	}
	return out
}
