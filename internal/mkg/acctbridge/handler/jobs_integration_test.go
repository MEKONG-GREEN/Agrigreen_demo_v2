package handler_test

import (
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// G4 đóng khoảng trống (web thay "mã vừa xem"): GET /v1/acctbridge/jobs liệt kê job dựng / đẩy, mới nhất trước,
// lọc kind / status / kỳ, phân trang theo con trỏ job_id; giá trị lạ 422; tenant khác không thấy (RB-2).
func TestAcctbridgeJobsList_G4(t *testing.T) {
	f := newFixture(t, nil)
	f.approvedMapping(f.tenant, nil)
	periods := []string{}
	for i := 0; i < 3; i++ {
		period, closer := ids.New(), ids.New()
		f.closePeriod(f.tenant, period, closer)
		periods = append(periods, period.String())
	}
	op := f.operator()
	p1 := op.get("/v1/acctbridge/jobs?kind=build&limit=2")
	mustStatus(t, p1, http.StatusOK, "trang 1")
	items := arr(p1.body, "items")
	next := str(p1.body, "next_cursor")
	if len(items) != 2 || str(items[0], "period_id") != periods[2] || str(items[0], "kind") != "build" || str(items[0], "status") != "queued" ||
		next == "" {
		t.Fatalf("trang 1: %s", p1.raw)
	}
	p2 := op.get("/v1/acctbridge/jobs?kind=build&limit=2&cursor=" + next)
	if items := arr(p2.body, "items"); len(items) != 1 || str(items[0], "period_id") != periods[0] || p2.body["next_cursor"] != nil {
		t.Fatalf("trang 2: %s", p2.raw)
	}
	if l := op.get("/v1/acctbridge/jobs?period_id=" + periods[1]); len(arr(l.body, "items")) != 1 {
		t.Fatalf("lọc kỳ: %s", l.raw)
	}
	if l := op.get("/v1/acctbridge/jobs?kind=push"); len(arr(l.body, "items")) != 0 {
		t.Fatalf("chưa có job đẩy: %s", l.raw)
	}
	f.drain(f.tenant)
	if l := f.chief().get("/v1/acctbridge/jobs?status=done"); len(arr(l.body, "items")) != 3 {
		t.Fatalf("sau khi worker dựng: %s", l.raw)
	}
	mustStatus(t, op.get("/v1/acctbridge/jobs?kind=export"), http.StatusUnprocessableEntity, "kind lạ")
	mustStatus(t, op.get("/v1/acctbridge/jobs?status=running"), http.StatusUnprocessableEntity, "status lạ")
	mustStatus(t, op.get("/v1/acctbridge/jobs?cursor=x"), http.StatusUnprocessableEntity, "con trỏ sai")
	if l := f.as(ids.New(), port.RoleAccountingBridgeOperator).get("/v1/acctbridge/jobs"); l.status != http.StatusOK || len(arr(l.body, "items")) != 0 {
		t.Fatalf("RB-2: %s", l.raw)
	}
}
