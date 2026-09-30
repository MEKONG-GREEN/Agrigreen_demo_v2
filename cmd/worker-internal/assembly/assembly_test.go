package assembly

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// G3 hub: ba consumer nội bộ, vai hub_app, handler dựng được (Q-MKG-04, Q-MKG-06/10 phụ thuộc).
func TestHubG3ConsumersRegistered(t *testing.T) {
	want := map[string]string{
		"mkg.hub.weigh_ticket":   "mkg.measurement.weigh_ticket.recorded",
		"mkg.hub.roll_count":     "mkg.fieldops.roll_count.recorded",
		"mkg.hub.reconciliation": "mkg.measurement.reconciliation.completed",
	}
	got := map[string]bool{}
	for _, c := range Consumers {
		ev, ok := want[c.Name]
		if !ok {
			continue
		}
		got[c.Name] = true
		if c.Role != "hub_app" || len(c.Events) != 1 || c.Events[0] != ev || c.Build(clock.System{}) == nil {
			t.Errorf("%s: vai %q sự kiện %v", c.Name, c.Role, c.Events)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("thiếu consumer hub G3: %v", got)
	}
}

// R-03: worker-internal (và mọi module nó nối) không phụ thuộc internal/platform/*, kể cả gián tiếp.
func TestNoPlatformDependency(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("không có go trong PATH")
	}
	out, err := exec.Command("go", "list", "-deps", "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/worker-internal").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range strings.Fields(string(out)) {
		if strings.Contains(p, "/internal/platform/") || strings.HasSuffix(p, "/internal/platform") {
			t.Errorf("R-03: worker-internal phụ thuộc %s", p)
		}
	}
}

// Mọi consumer nội bộ: tên mkg.<module>.<việc> duy nhất, có handler, chỉ nghe sự
// kiện nội bộ mkg.* có schema ở contracts/events-internal/ (ADR-0018).
func TestConsumerRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Consumers {
		if !ValidName(c.Name) || seen[c.Name] {
			t.Errorf("tên hàng đợi %q sai quy ước hoặc trùng", c.Name)
		}
		seen[c.Name] = true
		if (c.Build == nil && c.BuildWith == nil) || len(c.Events) == 0 {
			t.Errorf("%s: thiếu handler hoặc sự kiện", c.Name)
		}
		for _, e := range c.Events {
			if !strings.HasPrefix(e, "mkg.") {
				t.Errorf("%s: %s không phải sự kiện vùng mkg", c.Name, e)
			}
			matches, _ := filepath.Glob(filepath.Join("..", "..", "..", "contracts", "events-internal", e+".v*.json"))
			if len(matches) == 0 {
				t.Errorf("%s: %s không có schema ở contracts/events-internal/", c.Name, e)
			}
		}
	}
	if len(Queues()) != len(Consumers) {
		t.Fatal("Queues phải liệt kê đủ consumer")
	}
}

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{
		"mkg.hub.weigh_ticket": true, "mkg.costing.loss": true,
		"pf.hub.x": false, "mkg.hub": false, "mkg.Hub.x": false, "mkg.hub.x.y": false, "mkg..x": false,
	} {
		if ValidName(name) != want {
			t.Errorf("ValidName(%q) = %v", name, !want)
		}
	}
}

// Schema nội bộ tồn tại cho các sự kiện đã đóng băng ở G3-0.
func TestFrozenInternalSchemasExist(t *testing.T) {
	for _, e := range []string{"mkg.measurement.weigh_ticket.recorded", "mkg.measurement.reconciliation.completed", "mkg.fieldops.roll_count.recorded"} {
		p := filepath.Join("..", "..", "..", "contracts", "events-internal", fmt.Sprintf("%s.v1.json", e))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("thiếu %s: %v", p, err)
		}
	}
}

// G3-2: consumer tích hợp fieldops ↔ measurement, đúng vai và sự kiện.
func TestG32ConsumersRegistered(t *testing.T) {
	want := map[string][2]string{
		"mkg.fieldops.weigh_ticket":  {"fieldops_app", "mkg.measurement.weigh_ticket.recorded"},
		"mkg.measurement.roll_count": {"measurement_app", "mkg.fieldops.roll_count.recorded"},
	}
	n := 0
	for _, c := range Consumers {
		w, ok := want[c.Name]
		if !ok {
			continue
		}
		n++
		if c.Role != w[0] || len(c.Events) != 1 || c.Events[0] != w[1] {
			t.Errorf("%s: vai %q sự kiện %v", c.Name, c.Role, c.Events)
		}
	}
	if n != len(want) {
		t.Fatalf("thiếu consumer G3-2: %d/%d", n, len(want))
	}
}
