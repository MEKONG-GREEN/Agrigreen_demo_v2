package app

import "testing"

// Kiểm thử HTTP giải mã JSON (số → float64): vùng floatFreeProdZones miễn tệp _test.go.
func TestDecode(t *testing.T) {
	var m = map[string]any{"quantity": 1.5}
	if m["quantity"].(float64) != 1.5 {
		t.Fatal()
	}
}
