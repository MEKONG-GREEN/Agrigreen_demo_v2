package main

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
)

// ADR-0019 §3: bộ đếm đơn điệu bền vững — sập (không checkpoint) vẫn tiếp tục đúng sau số đo cuối đã fsync.
func TestCounterPersistsAcrossCrash(t *testing.T) {
	cfg, k := enrolled(t, 1<<20)
	s := openStore(t, cfg, k)
	appendN(t, s, 5)
	_ = s.WAL().Close() // "sập": không Checkpoint, state.json vẫn last_counter = 0

	s = openStore(t, cfg, k)
	if s.LastCounter() != 5 {
		t.Fatalf("last = %d, cần 5", s.LastCounter())
	}
	c, err := s.Append(reading(9))
	if err != nil || c != 6 {
		t.Fatalf("counter kế = %d (%v), cần 6", c, err)
	}
	// Xác nhận hết + gỡ WAL: bộ đếm vẫn tiếp tục từ state (không quay về 1).
	if err := s.Ack(6); err != nil {
		t.Fatal(err)
	}
	if s.WAL().Last() != 0 && s.WAL().First() != 0 {
		t.Fatalf("WAL chưa gỡ: first %d", s.WAL().First())
	}
	_ = s.Close()
	s = openStore(t, cfg, k)
	defer func() { _ = s.Close() }()
	if c, _ := s.Append(reading(1)); c != 7 {
		t.Fatalf("counter sau gỡ WAL = %d, cần 7", c)
	}
	if st := s.State(); st.AckedCounter != 6 {
		t.Fatalf("acked = %d", st.AckedCounter)
	}
}

// Đệm đầy không tiêu bộ đếm: không tạo khoảng trống.
func TestBufferFullDoesNotConsumeCounter(t *testing.T) {
	cfg, k := enrolled(t, 8192)
	s := openStore(t, cfg, k)
	defer func() { _ = s.Close() }()
	var last int64
	for {
		c, err := s.Append(reading(1))
		if errors.Is(err, ErrBufferFull) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		last = c
	}
	if _, err := s.Append(reading(1)); !errors.Is(err, ErrBufferFull) {
		t.Fatal("mong vẫn đầy")
	}
	if err := s.Ack(last / 2); err != nil {
		t.Fatal(err)
	}
	c, err := s.Append(reading(1))
	if err != nil || c != last+1 {
		t.Fatalf("sau khi gỡ: counter %d (%v), cần %d", c, err, last+1)
	}
}

// Mất đĩa: mọi dấu hiệu → từ chối khởi động, cần kỳ đăng ký mới (ADR-0019 §5).
func TestDiskLossRequiresNewEpoch(t *testing.T) {
	t.Run("not_enrolled", func(t *testing.T) {
		cfg := testConfig(t, t.TempDir(), 1<<20)
		if _, err := OpenStore(cfg, nil, quietLog()); !errors.Is(err, ErrNotEnrolled) {
			t.Fatalf("có %v", err)
		}
	})
	t.Run("state_lost_wal_present", func(t *testing.T) {
		cfg, k := enrolled(t, 1<<20)
		s := openStore(t, cfg, k)
		appendN(t, s, 3)
		_ = s.Close()
		_ = os.Remove(statePath(cfg.DataDir))
		if _, err := OpenStore(cfg, k.Public().(ed25519.PublicKey), quietLog()); !errors.Is(err, ErrStateLost) {
			t.Fatalf("có %v", err)
		}
	})
	t.Run("wal_lost_unacked", func(t *testing.T) {
		cfg, k := enrolled(t, 1<<20)
		s := openStore(t, cfg, k)
		appendN(t, s, 3)
		_ = s.Close() // checkpoint last_counter = 3
		if err := os.RemoveAll(walDir(cfg.DataDir)); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenStore(cfg, k.Public().(ed25519.PublicKey), quietLog()); !errors.Is(err, ErrCounterGap) {
			t.Fatalf("có %v", err)
		}
	})
	t.Run("wal_head_segment_lost", func(t *testing.T) {
		cfg, k := enrolled(t, 1<<20)
		s := openStore(t, cfg, k)
		appendN(t, s, 100)
		first := s.WAL().segs[0].path
		_ = s.Close()
		_ = os.Remove(first)
		if _, err := OpenStore(cfg, k.Public().(ed25519.PublicKey), quietLog()); !errors.Is(err, ErrCounterGap) {
			t.Fatalf("có %v", err)
		}
	})
	t.Run("key_lost", func(t *testing.T) {
		cfg, _ := enrolled(t, 1<<20)
		if _, err := OpenStore(cfg, nil, quietLog()); !errors.Is(err, ErrKeyLost) {
			t.Fatalf("có %v", err)
		}
		other, _, _ := ed25519.GenerateKey(nil)
		if _, err := OpenStore(cfg, other, quietLog()); !errors.Is(err, ErrKeyLost) {
			t.Fatalf("khóa khác: có %v", err)
		}
	})
}

func TestEnrollRules(t *testing.T) {
	cfg, k := enrolled(t, 1<<20)
	st, _ := ReadState(cfg.DataDir)
	if st.Epoch != 1 || st.KeyID != edgewire.KeyID(k.Public().(ed25519.PublicKey)) || st.LastCounter != 0 {
		t.Fatalf("state sau enroll: %+v", st)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(cfg.KeyFile); fi.Mode().Perm() != 0o600 {
			t.Fatalf("quyền khóa %o", fi.Mode().Perm())
		}
	}
	csr, err := os.ReadFile(filepath.Join(cfg.DataDir, "client.csr"))
	if err != nil || !strings.Contains(string(csr), "CERTIFICATE REQUEST") {
		t.Fatal("thiếu CSR")
	}
	// Đăng ký lại không -rotate: từ chối.
	if _, err := Enroll(cfg, 2, false, false, testT0, quietLog()); err == nil {
		t.Fatal("mong từ chối đăng ký lại")
	}
	// Còn số đo chưa xác nhận: -rotate bị từ chối.
	s := openStore(t, cfg, k)
	appendN(t, s, 3)
	_ = s.Close()
	if _, err := Enroll(cfg, 2, true, false, testT0, quietLog()); err == nil {
		t.Fatal("mong từ chối thay khóa khi đệm chưa đẩy hết")
	}
	// Kỳ không tăng: từ chối.
	if _, err := Enroll(cfg, 1, true, true, testT0, quietLog()); err == nil {
		t.Fatal("mong từ chối epoch không tăng")
	}
	// Đã đẩy hết → thay khóa kỳ 2, bộ đếm từ 1, khóa mới.
	s = openStore(t, cfg, k)
	if err := s.Ack(3); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	res, err := Enroll(cfg, 2, true, false, testT0, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	if res.Epoch != 2 || res.KeyID == st.KeyID {
		t.Fatalf("enroll rotate: %+v", res)
	}
	k2, err := LoadSourceKey(cfg.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	s = openStore(t, cfg, k2)
	defer func() { _ = s.Close() }()
	if c, _ := s.Append(reading(0)); c != 1 {
		t.Fatalf("kỳ mới bắt đầu counter %d", c)
	}
	// Khóa cũ được lưu trữ, không xóa.
	m, _ := filepath.Glob(cfg.KeyFile + ".retired-e1-*")
	if len(m) != 1 {
		t.Fatalf("khóa cũ chưa được lưu trữ: %v", m)
	}
}

func TestConfigBufferSizing72h(t *testing.T) {
	c := &Config{DeviceID: "scale-hub01-in", DataDir: t.TempDir(), Adapter: AdapterConfig{Type: AdapterSimulator}, Buffer: BufferConfig{MaxRateHz: 10}}
	if err := c.normalize(); err != nil {
		t.Fatal(err)
	}
	need := int64(10*72*3600) * c.RecordEstimateBytes()
	if c.RequiredBufferBytes() != need || c.Buffer.MaxBytes < need {
		t.Fatalf("định cỡ: need %d max %d", need, c.Buffer.MaxBytes)
	}
	c2 := &Config{DeviceID: "scale-hub01-in", DataDir: t.TempDir(), Adapter: AdapterConfig{Type: AdapterSimulator}, Buffer: BufferConfig{MaxRateHz: 10, MaxBytes: need - 1}}
	if err := c2.normalize(); err == nil {
		t.Fatal("mong từ chối đệm < 72 giờ")
	}
	c3 := &Config{DeviceID: "scale-hub01-in", DataDir: t.TempDir(), Adapter: AdapterConfig{Type: AdapterSimulator}, Buffer: BufferConfig{Hours: 24}}
	if err := c3.normalize(); err == nil {
		t.Fatal("mong từ chối hours < 72")
	}
	c4 := &Config{DeviceID: "scale-hub01-in", DataDir: t.TempDir(), Adapter: AdapterConfig{Type: AdapterSimulator}, Ingest: IngestConfig{URL: "http://x/"}}
	if err := c4.normalize(); err == nil {
		t.Fatal("mong từ chối ingest không https")
	}
}
