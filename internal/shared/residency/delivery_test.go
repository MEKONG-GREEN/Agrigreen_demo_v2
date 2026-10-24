package residency

import (
	"errors"
	"testing"
)

func getenvMap(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLocalDev(t *testing.T) {
	for env, want := range map[string]bool{
		"development": true, "dev": true, "local": true, "test": true, " Local ": true,
		"": false, "staging": false, "production": false, "prod": false, "demo": false,
	} {
		if got := LocalDev(getenvMap(map[string]string{EnvName: env})); got != want {
			t.Fatalf("AG_ENV=%q: LocalDev=%v, muốn %v", env, got, want)
		}
	}
}

func TestPublicBaseURL(t *testing.T) {
	const key = "AG_WEB_OPS_URL"
	cases := []struct {
		env, val, want string
		fail           bool
	}{
		{env: "local", val: "", want: ""},
		{env: "local", val: "http://localhost:3000", want: "http://localhost:3000"},
		{env: "staging", val: "https://app.demo.agrigreen.vn", want: "https://app.demo.agrigreen.vn"},
		{env: "staging", val: "", fail: true},
		{env: "", val: "", fail: true},
		{env: "staging", val: "http://localhost:8080", fail: true},
		{env: "production", val: "http://127.0.0.1:8080", fail: true},
		{env: "production", val: "https://[::1]/", fail: true},
		{env: "production", val: "/reports", fail: true},
		{env: "production", val: "ftp://ops.agrigreen.vn", fail: true},
		{env: "production", val: "https://ops.agrigreen.vn", want: "https://ops.agrigreen.vn"},
	}
	for _, c := range cases {
		got, err := PublicBaseURL(getenvMap(map[string]string{EnvName: c.env, key: c.val}), key)
		if c.fail {
			if !errors.Is(err, ErrLinkBase) || got != "" {
				t.Fatalf("AG_ENV=%q %s=%q: muốn ErrLinkBase, được %q %v", c.env, key, c.val, got, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("AG_ENV=%q %s=%q: được %q %v, muốn %q", c.env, key, c.val, got, err, c.want)
		}
	}
}
