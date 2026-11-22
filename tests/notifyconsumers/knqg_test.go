package notifyconsumers_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// W11.53b (#158): gốc lắp ráp nối consumer pf.notify.cnts_knqg ở thứ tự 18 (cuối danh mục, sau HRM), đúng một sự kiện;
// loại thông báo cnts_knqg_* không trùng loại cũ và không nằm trong từ vựng cũ (đăng ký riêng); hàng đợi khai đúng sự kiện;
// renderer phủ sự kiện của consumer (notify.New kiểm và panic nếu sai).
func TestAssemblyAddsKNQGConsumer_W1153b(t *testing.T) {
	deps := notify.Deps{Consumers: assembly.NotificationConsumers(), Renderers: assembly.NotificationRenderers(), Kinds: assembly.NotificationKinds(),
		Digests: assembly.NotificationDigests(), Builders: assembly.NotificationBuilders(nil, nil, nil, nil)}
	got := descriptors(t, notify.New(nil, clock.System{}, deps).Consumers())
	last := got[len(got)-1]
	if last.Name != extport.NotifyConsumerCNTSKNQG || last.Order != extport.NotifyConsumerCNTSKNQGOrder || last.Order != len(got) ||
		!slices.Equal(last.Events, []string{extport.EventCNTSKNQGChanged}) {
		t.Fatalf("consumer KNQG: %+v", last)
	}
	var kinds []string
	for _, r := range assembly.NotificationKinds() {
		kinds = append(kinds, r.Kinds...)
	}
	raw, err := os.ReadFile("../golden/kn-notify-kinds.json")
	if err != nil {
		t.Fatal(err)
	}
	var old struct{ Kinds []string }
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	for _, k := range extension.KNQGNotificationKinds().Kinds {
		if n := len(slices.DeleteFunc(slices.Clone(kinds), func(x string) bool { return x != k })); n != 1 || slices.Contains(old.Kinds, k) {
			t.Fatalf("loại %s xuất hiện %d lần hoặc trùng từ vựng cũ", k, n)
		}
	}
	declared := false
	for _, q := range assembly.Queues() {
		if q.Name == extport.NotifyConsumerCNTSKNQG {
			declared = slices.Equal(q.Events, last.Events)
		}
	}
	if !declared {
		t.Fatal("hàng đợi pf.notify.cnts_knqg chưa khai báo đúng sự kiện")
	}
	n := 0
	for _, r := range assembly.NotificationRenderers() {
		if slices.Contains(r.Events, extport.EventCNTSKNQGChanged) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("renderer KNQG: %d", n)
	}
}
