package kn77_test

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// TestFN_TW_01_01_LoadNote: ghi chú tải (docs/runbooks/kn-report-cycle-load.md) — phát hành 4 biểu × 102 xã × 17 tuần =
// 6.936 công việc trong một giao dịch (mục tiêu ≪ 2 s trên máy chủ, R-52), 102 xã gửi T04, "Cập nhật", rồi dashboard
// tỉnh đọc bảng chỉ số dựng sẵn với 20 yêu cầu đồng thời (mục tiêu < 5 s — FN-TW-01.01 AC; kiểm 500 người dùng thật ở
// staging bằng tools/loadgen). Ngưỡng trong kiểm thử rộng hơn (máy dev dùng chung) — số đo in ra log.
func TestFN_TW_01_01_LoadNote(t *testing.T) {
	if testing.Short() {
		t.Skip("kiểm thử tải: bỏ qua với -short")
	}
	f := newFixture(t, fixtureOpts{communes: 102})
	forms := f.publishForms("T03", "T04", "T05", "T06")
	officer := ids.New()
	f.asg.l = []accport.Assignment{{ID: ids.New(), UserRef: officer, Role: authz.KNXa, Departments: []string{dept}, Communes: f.communes89, Active: true,
		ValidFrom: t0.AddDate(0, -1, 0)}}
	tinh := f.kn(f.tenant, authz.KNTinh, dept, f.p89, nil)
	xa := f.knFor(f.tenant, officer, authz.KNXa, dept, f.p89, f.communes89, nil)

	start := time.Now()
	r := tinh.must(tinh.post("/v1/extension/report-batches", map[string]any{"report_type": "weekly",
		"template_ids": []string{forms["T03"], forms["T04"], forms["T05"], forms["T06"]}, "scope": map[string]any{"mode": "province"},
		"from_date": "2026-09-07", "to_date": "2027-01-03", "season_label": "Thu Đông 2026"}), http.StatusCreated, "phát hành 6.936 công việc")
	issue := time.Since(start)
	if n := num(obj(r.body, "batch"), "task_count"); n != 4*102*17 {
		t.Fatalf("số công việc: %v", n)
	}
	p1 := str(arr(r.body, "periods")[0], "id")
	t.Logf("phát hành %d công việc: %v", 4*102*17, issue)
	if issue > 20*time.Second {
		t.Fatalf("phát hành quá chậm: %v", issue)
	}
	l := xa.must(xa.get("/v1/extension/report-tasks?period_id="+p1+"&form_code=T04"), http.StatusOK, "công việc T04")
	for i, it := range arr(l.body, "items") {
		xa.must(submit(xa, str(it, "id"), "submitted", map[string]string{"K": fmt.Sprint(i + 1), "L": "1", "N": "2"}), http.StatusCreated, "gửi T04")
	}
	start = time.Now()
	tinh.must(tinh.post("/v1/extension/consolidations", map[string]any{"period_id": p1, "template_ids": []string{forms["T04"]}}), http.StatusCreated, "cập nhật 102 xã")
	t.Logf("tổng hợp 102 xã × T04 (226 cột): %v", time.Since(start))

	var mu sync.Mutex
	var lat []time.Duration
	var wg sync.WaitGroup
	for w := 0; w < 20; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 3; k++ {
				s := time.Now()
				d := tinh.do(f.app, http.MethodGet, "/v1/extension/kn-dashboard?period_id="+p1, "", nil)
				el := time.Since(s)
				if d.status != http.StatusOK {
					t.Errorf("dashboard %d", d.status)
				}
				mu.Lock()
				lat = append(lat, el)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	p95 := lat[len(lat)*95/100]
	t.Logf("dashboard 20 đồng thời × 3: p50 %v, p95 %v, max %v", lat[len(lat)/2], p95, lat[len(lat)-1])
	if p95 > 5*time.Second {
		t.Fatalf("FN-TW-01.01 AC: dashboard p95 %v > 5 s", p95)
	}
	d := tinh.must(tinh.get("/v1/extension/kn-dashboard?period_id="+p1), http.StatusOK, "dashboard")
	// Σ T04 cột K (DT xuống giống cây 1) = 1 + … + 102 = 5253 → veg_sown (E = Σ DT xuống giống các cây).
	if v := obj(obj(d.body, "cards"), "veg_sown")["value"]; v != "5253" {
		t.Fatalf("DT xuống giống rau màu toàn tỉnh: %v", v)
	}
}
