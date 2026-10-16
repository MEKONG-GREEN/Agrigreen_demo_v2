package main

import (
	"golang.org/x/tools/go/analysis/analysistest"
	"strings"
	"testing"
)

func TestApplicationNamespaceCannotBypassBoundary(t *testing.T) {
	cases := []struct{ from, to, rule string }{
		{"internal/shared/x", "internal/kn/price/port", "shared/"},
		{"internal/kn/extension/app", "internal/platform/forms/store", "R-02"},
		{"internal/platform/forms/app", "internal/kn/extension/store", "R-02"},
		{"internal/kn/extension/app", "internal/kn/price/store", "R-02"},
		{"internal/kn/extension/app", "internal/mkg/fieldops/port", "R-03"},
		{"internal/mkg/fieldops/app", "internal/kn/price/port", "R-03"},
		{"internal/vault/identity/app", "internal/kn/price/port", "RB-3"},
		{"cmd/api-internal", "internal/kn/price", "R-03"},
		{"cmd/api-platform", "internal/kn/price/store", "R-02"},
		{"internal/kn/price/port", "internal/kn/price/store", "mục 8"},
		{"internal/kn/extension/app", "internal/platform/forms/port", ""},
		{"cmd/api-platform", "internal/kn/price", ""},
	}
	for _, tc := range cases {
		from, to := locate(modulePath+"/"+tc.from), locate(modulePath+"/"+tc.to)
		got := violation(from, to, to.path)
		if (tc.rule == "" && got != "") || (tc.rule != "" && !strings.Contains(got, tc.rule)) {
			t.Errorf("%s -> %s: %q, want %q", tc.from, tc.to, got, tc.rule)
		}
	}
}

func TestApplicationNamespaceRetainsNumericAndLedgerGuards(t *testing.T) {
	for _, rel := range []string{"price/app", "hrm/domain", "extension/reportcycle/app", "extension/khtc/domain", "extension/khtcreport/app", "extension/khtcsource/app"} {
		for _, zone := range []string{"kn", "platform"} {
			pkg := modulePath + "/internal/" + zone + "/" + rel
			if !floatFree(pkg) || !floatFree(pkg+"_test") {
				t.Errorf("float guard missing: %s", pkg)
			}
			if !businessPkg(pkg) {
				t.Errorf("ledger guard missing: %s", pkg)
			}
		}
	}
}

func TestApplicationNamespaceAnalyzersRejectUnsafeSource(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), NoFloat, "example.com/ag/internal/kn/extension/khtcreport/app")
	analysistest.Run(t, analysistest.TestData(), NoHardDelete, "example.com/ag/internal/kn/contact/store")
}

// The generated driver can import its own tested package; other imports and handwritten drivers stay checked.
func TestGeneratedDriverSelfImportOnly(t *testing.T) {
	if err := Boundary.Flags.Set("module", "example.com/ag"); err != nil {
		t.Fatal(err)
	}
	analysistest.Run(t, analysistest.TestData(), Boundary,
		"example.com/ag/internal/kn/fixture.test",
		"example.com/ag/internal/kn/handwritten.test")
}
