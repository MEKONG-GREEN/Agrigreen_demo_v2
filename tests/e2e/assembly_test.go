package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAssemblyMatchesAPIPlatform canh lệch giữa harness.Build và gốc lắp ráp
// thật cmd/api-platform/modules.go (package main, không import được): danh sách
// module, Sources và Sinks của pf/sync phải giống hệt. Lệch → sửa harness/stack.go.
func TestAssemblyMatchesAPIPlatform(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "cmd", "api-platform", "modules.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	want := map[string]string{
		`\[\]syncport\.Source\{([^}]*)\}`: "g.SyncSource(), cnt.SyncSource(), kn.SyncSource()",
		`\[\]syncport\.Sink\{([^}]*)\}`:   "g.SyncSink(), kn.SyncSink(), mrvMod.SyncSink()",
		`\[\]httpx\.Module\{([^}]*)\}`:    "md, pty, g, cnt, kn, mech, mrvMod, snc",
	}
	for re, exp := range want {
		m := regexp.MustCompile(re).FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("không tìm thấy %s trong cmd/api-platform/modules.go — gốc lắp ráp đổi cấu trúc, cập nhật harness", re)
		}
		got := strings.Join(strings.Fields(m[1]), " ")
		if got != exp {
			t.Errorf("gốc lắp ráp đã đổi: %s\n  modules.go: %s\n  harness:    %s\n→ cập nhật tests/e2e/harness/stack.go (Build)", re, got, exp)
		}
	}
}
