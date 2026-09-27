package telemetry

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestTraceparent(t *testing.T) {
	id := TraceIDFromHeader("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if id != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("got %q", id)
	}
	for _, bad := range []string{"", "garbage", "00-00000000000000000000000000000000-00f067aa0ba902b7-01"} {
		if TraceIDFromHeader(bad) != "" {
			t.Errorf("%q phải bị bỏ qua", bad)
		}
	}
	if len(NewTraceID()) != 32 {
		t.Fatal("trace_id phải 32 ký tự hex")
	}
}

func TestLoggerFields(t *testing.T) {
	var buf bytes.Buffer
	NewLogger("api-platform", &buf).Info("x", "event", "request")
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["service"] != "api-platform" || m["ts"] == nil {
		t.Fatalf("thiếu trường chuẩn: %v", m)
	}
}
