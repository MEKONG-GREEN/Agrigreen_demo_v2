package main

import (
	"slices"
	"testing"
)

// TestRepoUpToDate là cổng W1.1: module.yaml trong repo khớp mã nguồn và migration.
func TestRepoUpToDate(t *testing.T) {
	stale, err := run("../..", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) > 0 {
		t.Fatalf("module.yaml lỗi thời, chạy go run ./tools/modgen: %v", stale)
	}
}

// TestWiredModules: ghép trong tiến trình tính từ import của cmd/<đơn vị> (không tính port/,
// client/; theo cả cmd/internal/*).
func TestWiredModules(t *testing.T) {
	wired, err := wiredModules("../..", []string{"api-platform", "api-public", "identity-vault", "ingest", "migrate"})
	if err != nil {
		t.Fatal(err)
	}
	has := func(u, m string) bool { return slices.Contains(wired[u], m) }
	if !has("api-platform", "pf/party") || !has("api-platform", "pf/sync") || has("api-platform", "vault/identity") {
		t.Errorf("api-platform: %v (vault/identity chỉ qua client/, không ghép)", wired["api-platform"])
	}
	if !has("api-public", "pf/apiclient") || has("api-public", "pf/party") {
		t.Errorf("api-public: %v (pf/party chỉ qua port/)", wired["api-public"])
	}
	if !has("identity-vault", "vault/identity") || !has("ingest", "mkg/measurement") || len(wired["migrate"]) != 0 {
		t.Errorf("wired = %v", wired)
	}
}

func TestInProcess(t *testing.T) {
	cases := map[string]bool{
		"internal/platform/party":                 true,
		"internal/mkg/measurement/ingest":         true,
		"internal/platform/geo/port/farmerid":     false,
		"internal/vault/identity/client":          false,
		"internal/vault/identity/client/clientmt": false,
	}
	for in, want := range cases {
		if got := inProcess(in); got != want {
			t.Errorf("inProcess(%q) = %v", in, got)
		}
	}
}

func TestModuleOf(t *testing.T) {
	cases := map[string]string{
		modulePath + "/internal/platform/party/port":        "pf/party",
		modulePath + "/internal/vault/identity/client":      "vault/identity",
		modulePath + "/internal/mkg/measurement/port/edgew": "mkg/measurement",
		modulePath + "/internal/shared/db":                  "",
		modulePath + "/contracts/events":                    "",
		"example.com/x/internal/platform/party/port":        "",
	}
	for in, want := range cases {
		if got := moduleOf(in); got != want {
			t.Errorf("moduleOf(%q) = %q, cần %q", in, got, want)
		}
	}
}

func TestMentionsSchema(t *testing.T) {
	cases := []struct {
		lit, schema string
		want        bool
	}{
		{"SELECT 1 FROM ts_weigh.ticket", "ts_weigh", true},
		{"SELECT 1 FROM xts_weigh.ticket", "ts_weigh", false},
		{"ts_weigh", "ts_weigh", false},
		{"a pf_files.object b", "pf_files", true},
	}
	for _, tc := range cases {
		if got := mentionsSchema(tc.lit, tc.schema); got != tc.want {
			t.Errorf("mentionsSchema(%q, %q) = %v", tc.lit, tc.schema, got)
		}
	}
}
