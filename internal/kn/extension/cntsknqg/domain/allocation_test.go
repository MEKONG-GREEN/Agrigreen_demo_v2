package domain

import (
	"bufio"
	"bytes"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// Record every allocated block before any test callback runs. Sampling would
// miss a small regression; changing this rate inside a measurement would mix
// differently sampled records. The byte helper also checks the rate on use.
func TestMain(m *testing.M) {
	runtime.MemProfileRate = 1
	os.Exit(m.Run())
}

//go:noinline
func invokeAllocationCallback(f func()) {
	f()
}

type allocationRecord struct {
	bytes      uint64
	inCallback bool
	complete   bool
}

type allocationSnapshot map[string]allocationRecord

func snapshotAllocationBytes(t *testing.T) allocationSnapshot {
	t.Helper()
	if runtime.MemProfileRate != 1 {
		t.Fatal("allocation byte guard requires MemProfileRate=1")
	}
	// Allocation profiles can lag by two GC cycles. Flush outside the callback.
	// The public legacy profile exposes cumulative allocated bytes, including
	// freed blocks, and full stacks. MemProfileRecord truncates stacks at32.
	runtime.GC()
	runtime.GC()
	var profile bytes.Buffer
	if err := pprof.Lookup("allocs").WriteTo(&profile, 1); err != nil {
		t.Fatal(err)
	}
	out := make(allocationSnapshot)
	lines := bufio.NewScanner(&profile)
	lines.Buffer(make([]byte, 4096), 1<<20)
	var current string
	for lines.Scan() {
		line := lines.Text()
		if strings.HasPrefix(line, "#") && strings.Contains(line, ".invokeAllocationCallback+") {
			record := out[current]
			record.inCallback = true
			out[current] = record
		}
		if line == "" || line[0] < '0' || line[0] > '9' {
			continue // Profile header, symbol lines, and trailing MemStats.
		}
		counts, stack, ok := strings.Cut(line, " @")
		if !ok {
			t.Fatal("malformed allocation profile record")
		}
		_, cumulative, ok := strings.Cut(counts, "[")
		if !ok || !strings.HasSuffix(cumulative, "]") {
			t.Fatal("malformed cumulative allocation byte count")
		}
		_, byteText, ok := strings.Cut(strings.TrimSuffix(cumulative, "]"), ": ")
		if !ok {
			t.Fatal("allocation profile is missing allocated bytes")
		}
		allocated, err := strconv.ParseUint(byteText, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		key := strings.TrimSpace(stack)
		lastPC := key[strings.LastIndexByte(key, ' ')+1:]
		pc, err := strconv.ParseUint(lastPC, 0, 64)
		if err != nil || pc == 0 {
			t.Fatal("allocation profile is missing its final stack PC")
		}
		fn := runtime.FuncForPC(uintptr(pc - 1))
		if fn == nil {
			t.Fatal("allocation profile final frame cannot be resolved")
		}
		record := out[key]
		record.bytes += allocated
		record.complete = fn.Name() == "runtime.goexit" || fn.Name() == "runtime.main" || fn.Name() == "runtime.systemstack"
		out[key] = record
		current = key
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// allocatedBytes counts cumulative heap bytes allocated by the synchronous
// callback and its descendants. TotalAlloc also counts unrelated goroutines.
// A noinline sentinel attributes every callback stack, without selecting
// implementation functions or subtracting a noise estimate or a minimum.
func allocatedBytes(t *testing.T, f func()) uint64 {
	t.Helper()
	before := snapshotAllocationBytes(t)
	invokeAllocationCallback(f)
	after := snapshotAllocationBytes(t)
	var used uint64
	for stack, record := range after {
		prior := before[stack].bytes
		if record.bytes < prior {
			t.Fatal("allocation profile counter regressed")
		}
		delta := record.bytes - prior
		if delta == 0 {
			continue
		}
		if record.inCallback {
			used += delta
		} else if !record.complete {
			// A truncated stack could hide the sentinel. Fail closed instead
			// of silently excluding a deeply nested callback allocation.
			t.Fatal("allocation attribution ambiguous: new stack is incomplete")
		}
	}
	return used
}

//go:noinline
func allocateControlBlock() {
	block := make([]byte, 1<<20)
	block[0] = 1
	runtime.KeepAlive(block)
}

//go:noinline
func allocateNestedControl(depth int) {
	if depth == 0 {
		allocateControlBlock()
		return
	}
	allocateNestedControl(depth - 1)
}

func TestAllocationByteMeasurement(t *testing.T) {
	for i := 0; i < 2; i++ {
		if used := allocatedBytes(t, func() {}); used != 0 {
			t.Fatalf("empty callback iteration=%d: got %d bytes, want0", i, used)
		}
		// The block is dead by the final snapshot. AllocBytes must still count
		// it in both the first call and the repeated call.
		if used := allocatedBytes(t, allocateControlBlock); used != 1<<20 {
			t.Fatalf("freed block iteration=%d: got %d bytes, want%d", i, used, 1<<20)
		}
	}
	if used := allocatedBytes(t, func() { allocateNestedControl(40) }); used != 1<<20 {
		t.Fatalf("40-frame block: got %d bytes, want%d", used, 1<<20)
	}
	var request, completed atomic.Int32
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for round := int32(1); round <= 2; round++ {
			for request.Load() < round {
				runtime.Gosched()
			}
			allocateControlBlock()
			completed.Store(round)
		}
	}()
	defer func() { request.Store(3); <-finished }()
	callback := func() {
		round := request.Add(1)
		for completed.Load() < round {
			runtime.Gosched()
		}
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	callback()
	runtime.ReadMemStats(&after)
	raw := after.TotalAlloc - before.TotalAlloc
	if raw < 1<<20 {
		t.Fatalf("process-wide control got %d bytes, want at least%d", raw, 1<<20)
	}
	used := allocatedBytes(t, callback)
	if used != 0 {
		t.Fatalf("sibling allocation attributed to callback: got %d bytes, want0", used)
	}
	t.Logf("BYTE207 controls: process-wide sibling=%d, attributed sibling=%d, synchronous/freed=%d, deep40=%d, empty=0, first+repeat PASS", raw, used, 1<<20, 1<<20)
}
