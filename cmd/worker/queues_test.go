package main

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	simport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
)

// Mất sự kiện lần khởi động đầu (docs/runbooks/rabbitmq-queues.md): outbox-publisher khai
// báo trước assembly.Queues(), nên danh sách phải trùng KHỚP với consumer của worker (cùng
// tên, cùng sự kiện) cộng hàng đợi optimizer. Thêm consumer vào worker mà quên Queues →
// kiểm thử này hỏng.
func TestWorkerConsumersDeclaredByPublisher(t *testing.T) {
	queues := map[string][]string{}
	for _, q := range assembly.Queues() {
		if _, dup := queues[q.Name]; dup {
			t.Fatalf("hàng đợi %s khai hai lần", q.Name)
		}
		queues[q.Name] = q.Events
	}
	seen := map[string]bool{}
	for _, c := range allConsumers(slog.New(slog.DiscardHandler)) {
		ev, ok := queues[c.name]
		if !ok {
			t.Fatalf("consumer %s của worker không có trong assembly.Queues()", c.name)
		}
		a, b := slices.Clone(ev), slices.Clone(c.events)
		slices.Sort(a)
		slices.Sort(b)
		if !slices.Equal(a, b) {
			t.Fatalf("consumer %s: Queues %v ≠ worker %v", c.name, a, b)
		}
		seen[c.name] = true
	}
	for name := range queues {
		if !seen[name] && name != simport.OptimizerConsumer {
			t.Fatalf("hàng đợi %s không có consumer ở worker (hoặc optimizer)", name)
		}
	}
	if ev := queues[simport.OptimizerConsumer]; len(ev) != 1 || ev[0] != simport.EventJobRequested {
		t.Fatalf("hàng đợi optimizer: %v", ev)
	}
}

// GIS US-EVL-01: hàng đợi pf.metrics.layer_log bind cả ngừng dùng/khôi phục thửa của pf/geo.
func TestLayerLogQueueBindsPlotRetireRestore(t *testing.T) {
	for _, q := range assembly.Queues() {
		if q.Name != metricsport.LayerLogConsumer {
			continue
		}
		for _, ev := range []string{coopport.EventPlotCreated, coopport.EventPlotRetired, coopport.EventPlotRestored} {
			if !slices.Contains(q.Events, ev) {
				t.Fatalf("hàng đợi %s thiếu %s: %v", q.Name, ev, q.Events)
			}
		}
		return
	}
	t.Fatalf("không có hàng đợi %s", metricsport.LayerLogConsumer)
}
