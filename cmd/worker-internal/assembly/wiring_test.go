package assembly

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// lazyPools là PoolFor giả: pool pgx lười (không nối DB tới khi dùng), ghi lại vai được hỏi.
type lazyPools struct {
	t     *testing.T
	mu    sync.Mutex
	asked map[string]int
	fail  string
}

func (l *lazyPools) pool(role string) (*pgxpool.Pool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if role == l.fail {
		return nil, errors.New("pool " + role + " lỗi")
	}
	l.asked[role]++
	p, err := pgxpool.New(context.Background(), "postgres://"+role+"@127.0.0.1:1/ag_mkg?sslmode=disable")
	if err != nil {
		l.t.Fatal(err)
	}
	l.t.Cleanup(p.Close)
	return p, nil
}

// G4 tích hợp (mục 1): mọi consumer của worker-internal dựng được handler; consumer costing nhận ĐỦ cổng
// đọc (Origins/Contracts/Trips/Deliveries) trên pool vai của module chủ dữ liệu — không còn costing.Deps{}.
func TestWorkerInternalConsumersBuildWithDeps_G4(t *testing.T) {
	lp := &lazyPools{t: t, asked: map[string]int{}}
	for _, c := range Consumers {
		h, err := c.Handler(clock.System{}, lp.pool)
		if err != nil || h == nil {
			t.Errorf("%s: handler nil / lỗi %v", c.Name, err)
		}
		if c.Module == costing.DBModule && c.BuildWith == nil {
			t.Errorf("%s: consumer costing phải dựng bằng BuildWith (cần cổng đọc module khác)", c.Name)
		}
	}
	// W2.6: mọi module dùng MỘT pool vai đăng nhập đơn vị (OpsRole = app_worker_internal), không còn pool <module>_app.
	if len(lp.asked) != 1 || lp.asked[OpsRole] == 0 {
		t.Errorf("pool được hỏi: %v (muốn chỉ %s)", lp.asked, OpsRole)
	}
	deps, err := CostingDeps(clock.System{}, lp.pool)
	if err != nil {
		t.Fatal(err)
	}
	if deps.Origins == nil || deps.Contracts == nil || deps.Trips == nil || deps.Deliveries == nil {
		t.Fatalf("CostingDeps thiếu cổng: %+v", deps)
	}
	// Pool của module chủ lỗi → consumer không dựng được (worker dừng khởi động, không chạy thiếu cổng).
	bad := &lazyPools{t: t, asked: map[string]int{}, fail: OpsRole}
	if _, err := CostingDeps(clock.System{}, bad.pool); err == nil {
		t.Errorf("pool %s lỗi phải trả lỗi", OpsRole)
	}
}
