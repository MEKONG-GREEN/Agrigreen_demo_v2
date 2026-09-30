package assembly

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders"
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
		if c.Role == costing.DBRole && c.BuildWith == nil {
			t.Errorf("%s: consumer costing phải dựng bằng BuildWith (cần cổng đọc module khác)", c.Name)
		}
	}
	for _, role := range []string{hub.DBRole, orders.DBRole, logistics.DBRole, costing.DBRole} {
		if lp.asked[role] == 0 {
			t.Errorf("không consumer nào hỏi pool vai %s", role)
		}
	}
	deps, err := CostingDeps(clock.System{}, lp.pool)
	if err != nil {
		t.Fatal(err)
	}
	if deps.Origins == nil || deps.Contracts == nil || deps.Trips == nil || deps.Deliveries == nil {
		t.Fatalf("CostingDeps thiếu cổng: %+v", deps)
	}
	// Pool của module chủ lỗi → consumer không dựng được (worker dừng khởi động, không chạy thiếu cổng).
	for _, role := range []string{hub.DBRole, orders.DBRole, logistics.DBRole} {
		bad := &lazyPools{t: t, asked: map[string]int{}, fail: role}
		if _, err := CostingDeps(clock.System{}, bad.pool); err == nil {
			t.Errorf("pool %s lỗi phải trả lỗi", role)
		}
	}
}
