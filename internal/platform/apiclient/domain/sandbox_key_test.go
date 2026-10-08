package domain

import (
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

// W5.8 (R-INT-08): khóa của môi trường thử có tiền tố agks_, cùng dạng prefix; khóa thật không bao giờ mang agks_.
func TestGenerateSandboxKey_W58(t *testing.T) {
	g, err := GenerateKeyFor(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := port.ParseKey(g.Full)
	if !ok || p != g.Prefix || !strings.HasPrefix(g.Full, port.SandboxKeyTokenPrefix+g.Prefix+"_") || !port.IsSandboxKey(g.Full) {
		t.Fatalf("khóa thử sai dạng: %q", g.Prefix)
	}
	real, err := GenerateKeyFor(nil, false)
	if err != nil || port.IsSandboxKey(real.Full) {
		t.Fatalf("khóa thật không phải khóa thử: %v", err)
	}
}
