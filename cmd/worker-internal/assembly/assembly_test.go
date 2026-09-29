package assembly

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Mọi consumer nội bộ: tên mkg.<module>.<việc> duy nhất, có handler, chỉ nghe sự
// kiện nội bộ mkg.* có schema ở contracts/events-internal/ (ADR-0018).
func TestConsumerRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Consumers {
		if !ValidName(c.Name) || seen[c.Name] {
			t.Errorf("tên hàng đợi %q sai quy ước hoặc trùng", c.Name)
		}
		seen[c.Name] = true
		if c.Build == nil || len(c.Events) == 0 {
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
