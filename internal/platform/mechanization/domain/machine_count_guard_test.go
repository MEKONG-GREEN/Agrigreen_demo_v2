package domain

import "testing"

// TT-10 rejects zero declarations as well as negative/fractional values. Removing
// a type is expressed by omitting its line, with a zero tombstone on the ledger.
func TestMachineQuantityPositiveDeclarationsAndRemoval(t *testing.T) {
	for _, raw := range []string{"0", "-0", "0.0", "-1", "1.5", "1000001"} {
		if _, err := ParseQuantity(raw); err == nil {
			t.Errorf("quantity %q accepted", raw)
		}
	}
	for _, raw := range []string{"1", "2.0", "1000000"} {
		if _, err := ParseQuantity(raw); err != nil {
			t.Errorf("quantity %q: %v", raw, err)
		}
	}
	changes := PlanCounts(map[string]CountLine{"tractor": {MachineType: "tractor", Quantity: 2}}, nil)
	if len(changes) != 1 || changes[0].Action != CountRemoved || changes[0].Quantity != 0 || changes[0].PrevQuantity == nil || *changes[0].PrevQuantity != 2 {
		t.Fatalf("removal ledger changed: %+v", changes)
	}
}
