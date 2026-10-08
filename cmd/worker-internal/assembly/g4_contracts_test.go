package assembly

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	abport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	costport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	ordport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
)

// G4-0: sự kiện nội bộ đóng băng có schema ở contracts/events-internal (x-kind internal, producer
// đúng module) và tên hàng đợi G4 đúng quy ước mkg.<module>.<việc>, không trùng hàng đợi đã có.
func TestG4FrozenInternalContracts(t *testing.T) {
	events := map[string]string{
		logport.EventTripCompleted:         "mkg/logistics",
		ordport.EventDeliveryAccepted:      "mkg/orders",
		costport.EventPeriodClosed:         "mkg/costing",
		costport.EventSettlementApproved:   "mkg/costing",
		costport.EventPeriodCloseRequested: "mkg/costing",
		abport.EventJournalPushed:          "mkg/acctbridge",
		abport.EventJournalBuildRequested:  "mkg/acctbridge",
		abport.EventJournalPushRequested:   "mkg/acctbridge",
	}
	for e, producer := range events {
		p := filepath.Join("..", "..", "..", "contracts", "events-internal", fmt.Sprintf("%s.v1.json", e))
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("thiếu %s: %v", p, err)
			continue
		}
		var s struct {
			EventType string `json:"x-event-type"`
			Kind      string `json:"x-kind"`
			Producer  string `json:"x-producer"`
		}
		if err := json.Unmarshal(raw, &s); err != nil || s.EventType != e || s.Kind != "internal" || s.Producer != producer {
			t.Errorf("%s: %+v %v", p, s, err)
		}
	}
	seen := map[string]bool{}
	for _, q := range []string{
		logport.RollCountConsumer, logport.WeighTicketConsumer, ordport.WeighTicketConsumer,
		costport.WeighTicketConsumer, costport.ReconciliationConsumer, costport.RollCountConsumer, costport.TripConsumer,
		costport.DeliveryConsumer, costport.JournalPushedConsumer, costport.PeriodCloseConsumer,
		abport.PeriodClosedConsumer, abport.JournalBuildConsumer, abport.JournalPushConsumer,
	} {
		if !ValidName(q) {
			t.Errorf("tên hàng đợi sai quy ước: %s", q)
		}
		if seen[q] {
			t.Errorf("tên hàng đợi trùng: %s", q)
		}
		seen[q] = true
	}
	// Không trùng hàng đợi G3 đã có (agent module thêm consumer G4 vào Consumers sau).
	for _, c := range Consumers {
		if seen[c.Name] && !slices.Contains([]string{"r_logistics", "r_orders", "r_costing", "r_acctbridge"}, c.Module.Role()) {
			t.Errorf("hàng đợi G4 %s trùng consumer của vai %s", c.Name, c.Module.Role())
		}
	}
}
