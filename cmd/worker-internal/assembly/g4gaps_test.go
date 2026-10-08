package assembly

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
)

// proto FM-05 (T-1, G4 đóng khoảng trống): sự kiện nội bộ mới mkg.logistics.trip.milestone_recorded v1 có schema
// (x-kind internal, producer mkg/logistics, không trường cước / tài xế) và consumer mkg.hub.trip_milestone của vai
// hub_app nhận đúng sự kiện đó (ADR-0018).
func TestTripMilestoneContractAndConsumer_FM05(t *testing.T) {
	p := filepath.Join("..", "..", "..", "contracts", "events-internal", logport.EventTripMilestoneRecorded+".v1.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		EventType  string                     `json:"x-event-type"`
		Kind       string                     `json:"x-kind"`
		Producer   string                     `json:"x-producer"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil || s.EventType != logport.EventTripMilestoneRecorded || s.Kind != "internal" ||
		s.Producer != "mkg/logistics" {
		t.Fatalf("%s: %+v %v", p, s, err)
	}
	for _, k := range []string{"actual_cost", "planned_cost", "cost_source", "driver_ref", "rate_vnd"} {
		if _, ok := s.Properties[k]; ok {
			t.Errorf("schema mốc chuyến không được có %s", k)
		}
	}
	if _, ok := s.Properties["trip_code"]; !ok {
		t.Error("thông báo hàng đến phải mang mã chuyến (trip_code)")
	}
	n := 0
	for _, c := range Consumers {
		if c.Name != hubport.TripMilestoneConsumer {
			continue
		}
		n++
		if c.Module != hub.DBModule || len(c.Events) != 1 || c.Events[0] != logport.EventTripMilestoneRecorded || !ValidName(c.Name) {
			t.Fatalf("consumer %s: vai %s sự kiện %v", c.Name, c.Module.Role(), c.Events)
		}
	}
	if n != 1 {
		t.Fatalf("phải có đúng một consumer %s: %d", hubport.TripMilestoneConsumer, n)
	}
}
