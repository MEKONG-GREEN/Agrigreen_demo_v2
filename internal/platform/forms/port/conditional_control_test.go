package port

import (
	"encoding/json"
	"strings"
	"testing"
)

// Revisions are server-derived bigint heads, not arbitrary precision numeric
// input. Their serialized durable-intent form must reject exponent/overflow
// values without ever expanding a decimal exponent or calling BigInt.
func TestPeriodRevision159_BoundedIntegerEncoding(t *testing.T) {
	for _, field := range []string{"event_seq", "due_seq"} {
		for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100_000), "9223372036854775808", `"1e100000"`, `"1e-100000"`} {
			var revision PeriodRevision
			if err := json.Unmarshal([]byte(`{"`+field+`":`+raw+`}`), &revision); err == nil {
				t.Fatalf("%s accepted hostile/unbounded integer (%d bytes)", field, len(raw))
			}
		}
	}
	var revision PeriodRevision
	if err := json.Unmarshal([]byte(`{"event_seq":9223372036854775807,"due_seq":0}`), &revision); err != nil || revision.EventSeq != 1<<63-1 || revision.DueSeq != 0 {
		t.Fatalf("bigint encoding changed: %+v %v", revision, err)
	}
}
