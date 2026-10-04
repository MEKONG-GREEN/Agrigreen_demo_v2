package main

import "testing"

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
