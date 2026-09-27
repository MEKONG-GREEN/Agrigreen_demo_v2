package main

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestBoundary(t *testing.T) {
	if err := Boundary.Flags.Set("module", "example.com/ag"); err != nil {
		t.Fatal(err)
	}
	analysistest.Run(t, analysistest.TestData(), Boundary,
		"example.com/ag/internal/platform/alpha/app",
		"example.com/ag/internal/shared/x",
		"example.com/ag/cmd/app",
	)
}

func TestNoFloat(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), NoFloat,
		"example.com/ag/internal/mkg/costing",
		"example.com/ag/internal/platform/beta/free",
	)
}

func TestNoTimeNow(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), NoTimeNow, "example.com/ag/internal/platform/beta/domain")
}

func TestPIILog(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), PIILog, "example.com/ag/internal/platform/beta/pii")
}
