package port

import (
	"errors"
	"strings"
	"testing"
)

// Môi trường thử (W5.8, R-INT-08): production từ chối cờ; khóa thử agks_ phân biệt được với khóa thật agk_.
func TestSandboxFromEnv(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == EnvSandbox {
				return v
			}
			return ""
		}
	}
	if on, err := SandboxFromEnv(env("1"), false); !on || err != nil {
		t.Fatalf("ngoài production → bật: %v %v", on, err)
	}
	if _, err := SandboxFromEnv(env("1"), true); !errors.Is(err, ErrSandboxInProduction) {
		t.Fatalf("production + cờ thử → không khởi động: %v", err)
	}
	if on, err := SandboxFromEnv(env(""), true); on || err != nil {
		t.Fatalf("không cờ → tắt: %v %v", on, err)
	}
	if _, err := SandboxFromEnv(env("yes"), false); err == nil {
		t.Fatal("giá trị lạ → lỗi")
	}
	secret := strings.Repeat("A", 43)
	for key, sandbox := range map[string]bool{"agk_abcdefghijkl_" + secret: false, "agks_abcdefghijkl_" + secret: true} {
		if p, ok := ParseKey(key); !ok || p != "abcdefghijkl" || IsSandboxKey(key) != sandbox {
			t.Errorf("%s: %q %v sandbox=%v", key[:5], p, ok, IsSandboxKey(key))
		}
	}
	if _, ok := ParseKey("agkx_abcdefghijkl_" + secret); ok {
		t.Fatal("tiền tố lạ → sai dạng")
	}
}
