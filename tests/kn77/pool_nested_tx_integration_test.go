package kn77_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// sharedUnitPool mở pool vai đăng nhập đơn vị app_platform (gốc lắp ráp thật: MỘT pool cho mọi module, mỗi module
// SET LOCAL ROLE r_<module>) với MaxConns nhỏ — P0-POOL-KN.
func sharedUnitPool(t *testing.T, env *pgtest.Env, maxConns int32) *pgxpool.Pool {
	t.Helper()
	dsn, err := dbadmin.RoleURL(env.PoolURL, migrations.PF, env.Suffix, dbadmin.AppPlatform)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.Open(context.Background(), dsn, db.Options{ViaPgBouncer: true, MaxConns: maxConns, AppName: "pgtest-p0poolkn"})
	if err != nil {
		t.Fatalf("mở pool chung app_platform: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// tryDo như caller.do nhưng có hạn chờ và trả lỗi thay vì Fatal (gọi được từ goroutine).
func tryDo(cl *http.Client, c caller, method, path, idem string, body any) (int, string, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.f.app.URL+path, rd)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(authz.ContextHeader, c.ctxID.String())
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	r, err := cl.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	return r.StatusCode, string(raw), nil
}

// TestP0PoolKN_NoNestedTxPoolStarvation (#125): pf/extension + pf/reports trên CHUNG một pool nhỏ (2 kết nối) như gốc
// lắp ráp thật. Gọi SONG SONG (số lời gọi ≫ cỡ pool) mở phiếu biểu nhiều kỳ (GetTask → phiên bản mẫu pf/reports), mở
// lại kỳ (→ "kỳ đã gửi Chi cục" pf/reports), lưu nháp (→ phiên bản mẫu) và "Cập nhật" tổng hợp (→ phiên bản mẫu).
// Trước sửa: mỗi thao tác giữ một kết nối trong giao dịch rồi chờ kết nối thứ hai → cạn pool, mọi lời gọi treo tới hết
// hạn. Sau sửa: không đường nào giữ kết nối trong khi chờ kết nối khác → tất cả xong trong hạn.
func TestP0PoolKN_NoNestedTxPoolStarvation(t *testing.T) {
	const poolSize = 2
	const perKind = 6 // 4 loại × 6 = 24 lời gọi song song trên pool 2 kết nối
	c := setupCycleWith(t, fixtureOpts{sharedPool: poolSize})
	f := c.f
	c.issue(t, "T01")
	c1 := f.communes89[0]
	t1 := str(c.task(t, c.xa1, "T01", c1, c.p1), "id")
	t2 := str(c.task(t, c.xa1, "T01", c1, c.p2), "id")
	// Tuần tự (pool đủ 2 kết nối cho một thao tác lồng): mở phiếu, nộp tuần 1, chốt kỳ 1.
	c.xa1.must(c.xa1.get("/v1/extension/report-tasks/"+t1), http.StatusOK, "mở phiếu")
	c.xa1.must(submit(c.xa1, t1, "submitted", map[string]string{"E": "90", "F": "25"}), http.StatusCreated, "gửi tuần 1")
	c.tinh.must(c.tinh.post("/v1/extension/report-periods/"+c.p1+"/close", map[string]any{}), http.StatusOK, "chốt kỳ 1")

	cl := &http.Client{Timeout: 10 * time.Second}
	until := f.clk.Now().Add(48 * time.Hour).Format(time.RFC3339)
	type result struct {
		what   string
		status int
		body   string
		err    error
	}
	var wg sync.WaitGroup
	res := make(chan result, 4*perKind)
	start := make(chan struct{})
	run := func(what string, want []int, fn func() (int, string, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			st, body, err := fn()
			ok := err == nil
			if ok {
				ok = false
				for _, w := range want {
					ok = ok || st == w
				}
			}
			if !ok {
				res <- result{what: what, status: st, body: body, err: err}
			}
		}()
	}
	for i := 0; i < perKind; i++ {
		run(fmt.Sprintf("GetTask#%d", i), []int{http.StatusOK}, func() (int, string, error) {
			return tryDo(cl, c.xa1, http.MethodGet, "/v1/extension/report-tasks/"+t1, "", nil)
		})
		run(fmt.Sprintf("Reopen#%d", i), []int{http.StatusOK}, func() (int, string, error) {
			return tryDo(cl, c.tinh, http.MethodPost, "/v1/extension/report-periods/"+c.p1+"/reopen", key(), map[string]any{"communes": []string{c1},
				"reason": "Xã phát hiện nhập nhầm số tuần 1", "until": until})
		})
		run(fmt.Sprintf("SubmitDraft#%d", i), []int{http.StatusCreated}, func() (int, string, error) {
			return tryDo(cl, c.xa1, http.MethodPost, "/v1/extension/report-tasks/"+t2+"/submissions", key(),
				map[string]any{"kind": "draft", "rows": []any{map[string]any{"input": map[string]string{"E": "90", "G": fmt.Sprint(10 + i)}}}})
		})
		run(fmt.Sprintf("Consolidate#%d", i), []int{http.StatusOK, http.StatusCreated}, func() (int, string, error) {
			return tryDo(cl, c.tinh, http.MethodPost, "/v1/extension/consolidations", key(), map[string]any{"period_id": c.p1})
		})
	}
	began := time.Now()
	close(start)
	wg.Wait()
	close(res)
	var fails []result
	for r := range res {
		fails = append(fails, r)
	}
	if len(fails) > 0 {
		for _, r := range fails {
			t.Errorf("%s: status=%d err=%v body=%.200s", r.what, r.status, r.err, r.body)
		}
		t.Fatalf("P0-POOL-KN: %d/%d lời gọi song song hỏng trên pool %d kết nối sau %s — giao dịch lồng nhau làm cạn pool",
			len(fails), 4*perKind, poolSize, time.Since(began).Round(time.Millisecond))
	}
	t.Logf("P0-POOL-KN: %d lời gọi song song trên pool %d kết nối xong sau %s", 4*perKind, poolSize, time.Since(began).Round(time.Millisecond))
	// Ngữ nghĩa mở lại giữ nguyên: mỗi lần mở lại ghi một sự kiện, cờ dispatch_changed = false (kỳ chưa gửi Chi cục).
	if n := f.count(`SELECT count(*) FROM pf_extension.period_event WHERE tenant_id = $1 AND period_id = $2::uuid AND action = 'reopened'`,
		f.tenant, c.p1); n != perKind {
		t.Fatalf("số sự kiện mở lại: %d, muốn %d", n, perKind)
	}
}
