package notifyconsumers_test

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	hrmport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// NOTIFY-HCTH (#48): gốc lắp ráp nối đăng ký của pf/hrm — danh mục consumer của API/worker/khai báo hàng đợi giữ nguyên 16
// consumer cũ (tên, thứ tự, sự kiện) và thêm pf.notify.hrm ở thứ tự 17; loại thông báo hrm_* không trùng loại cũ; builder
// phủ đúng sự kiện của consumer HRM (notify.New kiểm và panic nếu sai).
func TestAssemblyAddsHRMConsumerKindsBuilders_NotifyHCTH(t *testing.T) {
	deps := notify.Deps{Consumers: assembly.NotificationConsumers(), Renderers: assembly.NotificationRenderers(), Kinds: assembly.NotificationKinds(),
		Digests: assembly.NotificationDigests(), Builders: assembly.NotificationBuilders(nil, nil, nil, nil)}
	got := descriptors(t, notify.New(nil, clock.System{}, deps).Consumers())
	raw, err := os.ReadFile("../golden/kn-notify-consumers.json")
	if err != nil {
		t.Fatal(err)
	}
	var legacy []port.ConsumerDefinition
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(legacy)+1 || !reflect.DeepEqual(got[:len(legacy)], legacy) {
		t.Fatalf("danh mục consumer cũ bị đổi:\n%v", got)
	}
	last := got[len(got)-1]
	if last.Name != hrmport.NotifyConsumerHRM || last.Order != hrmport.NotifyConsumerHRMOrder ||
		!slices.Equal(last.Events, []string{hrmport.EventPromotionDueDetected, hrmport.EventPlanReminded}) {
		t.Fatalf("consumer HRM: %+v", last)
	}
	var kinds []string
	for _, r := range assembly.NotificationKinds() {
		kinds = append(kinds, r.Kinds...)
	}
	for _, k := range hrm.NotificationKinds().Kinds {
		if n := len(slices.DeleteFunc(slices.Clone(kinds), func(x string) bool { return x != k })); n != 1 {
			t.Fatalf("loại %s xuất hiện %d lần", k, n)
		}
	}
	// Khai báo hàng đợi (API/worker) dùng cùng danh mục.
	declared := false
	for _, q := range assembly.Queues() {
		if q.Name == hrmport.NotifyConsumerHRM {
			declared = slices.Equal(q.Events, last.Events)
		}
	}
	if !declared {
		t.Fatal("hàng đợi pf.notify.hrm chưa khai báo đúng sự kiện")
	}
}
