//go:build allocationdiagnostics207

package domain

import (
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"testing"
)

type allocationSample207 struct {
	Operation string
	Iteration int
	Input     string
	InputLen  int
	Bytes     uint64
	Reason    string
}

func allocationMetadata207(t *testing.T) {
	t.Helper()
	race := "unreported"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "-race" {
				race = setting.Value
			}
		}
	}
	t.Logf("ALLOC207 metadata go=%s goos=%s goarch=%s procs=%d race_build_setting=%s memprofile_rate=%d", runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0), race, runtime.MemProfileRate)
}

func logAllocation207(t *testing.T, sample allocationSample207) {
	t.Helper()
	if sample.InputLen > 32 {
		sample.Input = fmt.Sprintf("<%d bytes>", sample.InputLen)
	}
	data, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ALLOC207 sample %s", data)
}

// This fixed observation campaign neither replaces nor relaxes the original
// byte guards. Every sample, including the first call, is reported.
func TestAllocationDiagnostics207(t *testing.T) {
	allocationMetadata207(t)
	samples := make([]allocationSample207, 0, 288)
	for i := 0; i < 32; i++ {
		used := allocs(func() {})
		samples = append(samples, allocationSample207{Operation: "empty", Iteration: i, Bytes: used})
	}
	inputs := []string{"1e-100000", "1e100000", "1E-999999999", "9e+2147483647", "+1e-2147483648", strings.Repeat("9", 100000), strings.Repeat("9", 23)}
	for _, kind := range []string{KindNumber, KindInt} {
		for _, input := range inputs {
			for i := 0; i < 16; i++ {
				var reason string
				used := allocs(func() { _, reason = cell(kind, nil, input) })
				samples = append(samples, allocationSample207{Operation: "cell/" + kind, Iteration: i, Input: input, InputLen: len(input), Bytes: used, Reason: reason})
				if reason == "" {
					t.Fatalf("hostile input accepted: kind=%s length=%d", kind, len(input))
				}
			}
		}
	}
	s, _ := Lookup(Form01)
	for i := 0; i < 16; i++ {
		content := Content{Tables: map[string][]Row{"vat_tu": {{"ten": "Vật tư", "ke_hoach": "1e-100000", "da_cap": "1e100000"}}}}
		var errs Errors
		used := allocs(func() { errs = s.Normalize(&content) })
		samples = append(samples, allocationSample207{Operation: "normalize", Iteration: i, Bytes: used})
		if len(errs) != 2 {
			t.Fatalf("Normalize returned %d errors, want2", len(errs))
		}
		raw := Content{Tables: map[string][]Row{"vat_tu": {{"ten": "X", "ke_hoach": "1e-100000", "da_cap": "1e100000"}}}}
		var warnings []string
		used = allocs(func() { warnings = s.Warnings(raw) })
		samples = append(samples, allocationSample207{Operation: "warnings", Iteration: i, Bytes: used})
		if len(warnings) != 0 {
			t.Fatalf("Warnings returned %d entries, want0", len(warnings))
		}
	}
	for _, sample := range samples {
		logAllocation207(t, sample)
	}
}

type allocationProfile207 struct {
	Bytes   int64
	Objects int64
	Stack   string
}

func profileSnapshot207() map[string]allocationProfile207 {
	n, _ := runtime.MemProfile(nil, true)
	records := make([]runtime.MemProfileRecord, n+64)
	for {
		var ok bool
		n, ok = runtime.MemProfile(records, true)
		if ok {
			break
		}
		records = make([]runtime.MemProfileRecord, n+64)
	}
	out := make(map[string]allocationProfile207)
	for _, record := range records[:n] {
		frames := runtime.CallersFrames(record.Stack())
		var stack []string
		for {
			frame, more := frames.Next()
			stack = append(stack, fmt.Sprintf("%s:%d", frame.Function, frame.Line))
			if !more {
				break
			}
		}
		key := strings.Join(stack, " <- ")
		before := out[key]
		out[key] = allocationProfile207{Bytes: before.Bytes + record.AllocBytes, Objects: before.Objects + record.AllocObjects, Stack: key}
	}
	return out
}

// Profile flushing and output occur outside the TotalAlloc interval. These
// profiles include flush/setup activity and are forensic observations, never
// subtractions from or replacements for a resource-budget measurement.
func TestAllocationAttribution207(t *testing.T) {
	allocationMetadata207(t)
	if runtime.MemProfileRate != 1 {
		t.Fatal("attribution campaign requires GODEBUG=memprofilerate=1 from process startup")
	}
	samples := make([]allocationSample207, 0, 64)
	before := profileSnapshot207()
	for i := 0; i < 32; i++ {
		used := allocs(func() {})
		samples = append(samples, allocationSample207{Operation: "profile/empty", Iteration: i, Bytes: used})
		var reason string
		used = allocs(func() { _, reason = cell(KindNumber, nil, "1E-999999999") })
		samples = append(samples, allocationSample207{Operation: "profile/cell", Iteration: i, Input: "1E-999999999", InputLen: len("1E-999999999"), Bytes: used, Reason: reason})
		if reason == "" {
			t.Fatal("hostile input accepted")
		}
	}
	runtime.GC()
	runtime.GC()
	after := profileSnapshot207()
	var delta []allocationProfile207
	for key, record := range after {
		record.Bytes -= before[key].Bytes
		record.Objects -= before[key].Objects
		if record.Bytes > 0 {
			delta = append(delta, record)
		}
	}
	sort.Slice(delta, func(i, j int) bool {
		if delta[i].Bytes != delta[j].Bytes {
			return delta[i].Bytes > delta[j].Bytes
		}
		return delta[i].Stack < delta[j].Stack
	})
	for _, record := range delta {
		t.Logf("ALLOC207 attribution bytes=%d objects=%d stack=%s", record.Bytes, record.Objects, record.Stack)
	}
	for _, sample := range samples {
		logAllocation207(t, sample)
	}
}
