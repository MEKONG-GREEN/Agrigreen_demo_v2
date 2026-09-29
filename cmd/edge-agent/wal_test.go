package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func openTestWAL(t *testing.T, dir string, epoch int, maxBytes int64) *WAL {
	t.Helper()
	w, err := OpenWAL(dir, "scale-hub01-in", epoch, 4096, maxBytes, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func walAppend(t *testing.T, w *WAL, from, n int) {
	t.Helper()
	for i := range n {
		r := reading(from + i)
		r.Counter = int64(from + i)
		if err := w.Append(r); err != nil {
			t.Fatalf("append %d: %v", from+i, err)
		}
	}
}

// ADR-0019 §6: WAL phân đoạn — đọc lại liên tiếp qua nhiều đoạn, sau khi mở lại.
func TestWALSegmentsReadAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	w := openTestWAL(t, dir, 1, 1<<20)
	walAppend(t, w, 1, 200)
	if len(w.segs) < 3 {
		t.Fatalf("mong nhiều đoạn, có %d", len(w.segs))
	}
	_ = w.Close()
	w = openTestWAL(t, dir, 1, 1<<20)
	defer func() { _ = w.Close() }()
	if w.First() != 1 || w.Last() != 200 {
		t.Fatalf("first/last = %d/%d", w.First(), w.Last())
	}
	for _, from := range []int64{1, 37, 38, 150, 199} {
		rs, err := w.ReadFrom(from, 60)
		if err != nil {
			t.Fatal(err)
		}
		want := min(60, 200-from+1)
		if int64(len(rs)) != want {
			t.Fatalf("from %d: %d số đo, cần %d", from, len(rs), want)
		}
		for i, r := range rs {
			if r.Counter != from+int64(i) || r.ValueKg != reading(int(from)+i).ValueKg {
				t.Fatalf("from %d: số đo %d sai: %+v", from, i, r)
			}
		}
	}
	// counter không liên tiếp bị từ chối
	r := reading(0)
	r.Counter = 205
	if err := w.Append(r); err == nil {
		t.Fatal("mong lỗi counter không liên tiếp")
	}
}

// Mục 14: đệm đầy → ngừng nhận, không ghi đè; sau khi xác nhận + gỡ đoạn thì nhận lại.
func TestWALFullRefusesNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	w := openTestWAL(t, dir, 1, 8192)
	defer func() { _ = w.Close() }()
	var n int
	for n = 1; ; n++ {
		r := reading(n)
		r.Counter = int64(n)
		err := w.Append(r)
		if errors.Is(err, ErrBufferFull) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	full := n - 1
	if full < 50 || w.Bytes() > 8192 {
		t.Fatalf("đầy sau %d số đo, %d byte", full, w.Bytes())
	}
	// Vẫn đầy ở lần thử sau; dữ liệu cũ còn nguyên.
	r := reading(n)
	r.Counter = int64(n)
	if err := w.Append(r); !errors.Is(err, ErrBufferFull) {
		t.Fatalf("mong ErrBufferFull, có %v", err)
	}
	rs, err := w.ReadFrom(1, full)
	if err != nil || len(rs) != full || rs[0].Counter != 1 {
		t.Fatalf("dữ liệu cũ bị mất: %d %v", len(rs), err)
	}
	// Xác nhận đoạn đầu → gỡ → nhận lại.
	first := w.segs[0].last
	if err := w.TrimThrough(first); err != nil {
		t.Fatal(err)
	}
	if w.First() != first+1 {
		t.Fatalf("first sau gỡ = %d", w.First())
	}
	if err := w.Append(r); err != nil {
		t.Fatalf("sau gỡ vẫn không nhận: %v", err)
	}
	// Gỡ một phần đoạn (chưa xác nhận hết) không gỡ gì.
	before := w.Bytes()
	if err := w.TrimThrough(first + 1); err != nil || w.Bytes() != before {
		t.Fatal("không được gỡ đoạn còn số đo chưa xác nhận")
	}
}

// Phục hồi sau sập: đuôi dở của đoạn cuối bị cắt; số đo đã fsync còn nguyên.
func TestWALCrashTruncatedTail(t *testing.T) {
	for name, junk := range map[string][]byte{
		"partial_header": {0x40, 0x00},
		"partial_body":   encodeWALFrame([]byte(`{"counter":11,"device_time":"x"}`))[:20],
		"bad_crc":        func() []byte { f := encodeWALFrame([]byte(`{"counter":11}`)); f[5] ^= 0xff; return f }(),
		"bad_json":       encodeWALFrame([]byte(`not json`)),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			w := openTestWAL(t, dir, 1, 1<<20)
			walAppend(t, w, 1, 10)
			path := w.segs[len(w.segs)-1].path
			size := w.segs[len(w.segs)-1].size
			_ = w.Close()
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.Write(junk)
			_ = f.Close()

			w = openTestWAL(t, dir, 1, 1<<20)
			defer func() { _ = w.Close() }()
			if w.Last() != 10 {
				t.Fatalf("last = %d, cần 10", w.Last())
			}
			if fi, _ := os.Stat(path); fi.Size() != size {
				t.Fatalf("đuôi chưa bị cắt: %d ≠ %d", fi.Size(), size)
			}
			walAppend(t, w, 11, 1)
			rs, _ := w.ReadFrom(1, 100)
			if len(rs) != 11 {
				t.Fatalf("đọc lại %d số đo", len(rs))
			}
		})
	}
}

func TestWALCorruptMiddleSegmentRefused(t *testing.T) {
	dir := t.TempDir()
	w := openTestWAL(t, dir, 1, 1<<20)
	walAppend(t, w, 1, 120)
	mid := w.segs[1].path
	_ = w.Close()
	b, _ := os.ReadFile(mid)
	b[len(b)/2] ^= 0xff
	if err := os.WriteFile(mid, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenWAL(dir, "scale-hub01-in", 1, 4096, 1<<20, quietLog()); !errors.Is(err, ErrWALCorrupt) {
		t.Fatalf("mong ErrWALCorrupt, có %v", err)
	}
}

func TestWALMissingMiddleSegmentRefused(t *testing.T) {
	dir := t.TempDir()
	w := openTestWAL(t, dir, 1, 1<<20)
	walAppend(t, w, 1, 120)
	mid := w.segs[1].path
	_ = w.Close()
	if err := os.Remove(mid); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenWAL(dir, "scale-hub01-in", 1, 4096, 1<<20, quietLog()); !errors.Is(err, ErrWALCorrupt) {
		t.Fatalf("mong ErrWALCorrupt, có %v", err)
	}
}

func TestWALForeignEpochRefused(t *testing.T) {
	dir := t.TempDir()
	w := openTestWAL(t, dir, 1, 1<<20)
	walAppend(t, w, 1, 3)
	_ = w.Close()
	if _, err := OpenWAL(dir, "scale-hub01-in", 2, 4096, 1<<20, quietLog()); !errors.Is(err, ErrWALForeign) {
		t.Fatalf("mong ErrWALForeign, có %v", err)
	}
}

// Sập khi vừa tạo đoạn (tiêu đề dở, chưa có số đo): đoạn bị gỡ, WAL tiếp tục.
func TestWALTornSegmentHeader(t *testing.T) {
	dir := t.TempDir()
	w := openTestWAL(t, dir, 1, 1<<20)
	walAppend(t, w, 1, 5)
	_ = w.Close()
	torn := filepath.Join(dir, "00000000000000000006.wal")
	if err := os.WriteFile(torn, []byte{0x10, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	w = openTestWAL(t, dir, 1, 1<<20)
	defer func() { _ = w.Close() }()
	if _, err := os.Stat(torn); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("đoạn tiêu đề dở chưa bị gỡ")
	}
	walAppend(t, w, 6, 1)
	if w.Last() != 6 {
		t.Fatalf("last = %d", w.Last())
	}
}
