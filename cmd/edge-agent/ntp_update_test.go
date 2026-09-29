package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Máy chủ SNTP giả: đồng hồ máy chủ nhanh hơn đồng hồ thiết bị `skew`.
func fakeNTP(t *testing.T, clk clock.Clock, skew time.Duration, stratum byte) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 48)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 48 {
				continue
			}
			resp := make([]byte, 48)
			resp[0] = 4<<3 | 4
			resp[1] = stratum
			copy(resp[24:32], buf[40:48]) // originate = transmit của client
			ts := toNTP(clk.Now().Add(skew))
			binary.BigEndian.PutUint64(resp[32:], ts)
			binary.BigEndian.PutUint64(resp[40:], ts)
			_, _ = pc.WriteTo(resp, addr)
		}
	}()
	return pc.LocalAddr().String()
}

func TestSNTPOffset(t *testing.T) {
	clk := clock.NewFixed(testT0)
	addr := fakeNTP(t, clk, 1500*time.Millisecond, 2)
	off, _, err := QuerySNTP(context.Background(), addr, time.Second, clk)
	if err != nil {
		t.Fatal(err)
	}
	if d := off - 1500*time.Millisecond; d < -time.Millisecond || d > time.Millisecond {
		t.Fatalf("offset %v", off)
	}
	tr := NewNTPTracker(NTPConfig{MaxAge: Duration(time.Minute)}, clk, quietLog())
	if tr.OffsetMs() != nil {
		t.Fatal("chưa đồng bộ phải là null")
	}
	tr.Set(off)
	if v := tr.OffsetMs(); v == nil || *v < 1499 || *v > 1500 {
		t.Fatalf("offset ms %v", v)
	}
	clk.Advance(2 * time.Minute)
	if tr.OffsetMs() != nil {
		t.Fatal("độ lệch cũ phải thành null")
	}
	// stratum 0 (kiss-o'-death) bị từ chối.
	bad := fakeNTP(t, clk, 0, 0)
	if _, _, err := QuerySNTP(context.Background(), bad, time.Second, clk); err == nil {
		t.Fatal("mong lỗi stratum 0")
	}
	if got := fromNTP(toNTP(testT0)); got.Sub(testT0).Abs() > time.Microsecond {
		t.Fatalf("toNTP/fromNTP %v", got)
	}
}

func signedRelease(t *testing.T, priv ed25519.PrivateKey, ver string, bin []byte) Manifest {
	t.Helper()
	h := sha256.Sum256(bin)
	m := Manifest{Version: ver, OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: hex.EncodeToString(h[:]), Size: int64(len(bin))}
	if err := SignManifest(priv, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Tự cập nhật (mục 14): xác minh chữ ký/sha256/phiên bản, khe A/B, lùi lại khi không khỏe.
func TestUpdateVerifyApplyRollbackCommit(t *testing.T) {
	root := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(nil)
	_, other, _ := ed25519.GenerateKey(nil)
	bin := []byte("binary v1.1.0")
	m := signedRelease(t, priv, "1.1.0", bin)

	// Chữ ký của khóa khác, nhị phân bị sửa, sai kiến trúc → từ chối.
	if _, err := ApplyUpdate(root, pub, signedRelease(t, other, "1.1.0", bin), bytes.NewReader(bin), false, testT0); err == nil {
		t.Fatal("mong từ chối chữ ký lạ")
	}
	if _, err := ApplyUpdate(root, pub, m, bytes.NewReader([]byte("binary v1.1.X")), false, testT0); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("mong từ chối sha256: %v", err)
	}
	tampered := m
	tampered.Version = "9.9.9"
	if _, err := ApplyUpdate(root, pub, tampered, bytes.NewReader(bin), false, testT0); err == nil {
		t.Fatal("mong từ chối manifest bị sửa")
	}
	wrong := m
	wrong.Arch = "sparc"
	if err := SignManifest(priv, &wrong); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyUpdate(root, pub, wrong, bytes.NewReader(bin), false, testT0); err == nil {
		t.Fatal("mong từ chối sai kiến trúc")
	}

	// Cài vào khe b, chế độ thử.
	s, err := ApplyUpdate(root, pub, m, bytes.NewReader(bin), false, testT0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Active != "b" || s.Stable != "a" || !s.Trial {
		t.Fatalf("sau apply: %+v", s)
	}
	if got, _ := os.ReadFile(slotBinary(root, "b")); !bytes.Equal(got, bin) {
		t.Fatal("nhị phân trong khe b sai")
	}
	if _, err := ApplyUpdate(root, pub, m, bytes.NewReader(bin), false, testT0); err == nil {
		t.Fatal("không được cài chồng khi đang thử")
	}
	// Bản mới không khỏe: guard đếm 3 lần thử rồi lùi về khe a.
	for i := 1; i <= 3; i++ {
		rolled, s, err := GuardStart(root, 3, testT0)
		if err != nil || rolled || s.Attempts != i {
			t.Fatalf("guard %d: %v %+v %v", i, rolled, s, err)
		}
	}
	rolled, s, err := GuardStart(root, 3, testT0)
	if err != nil || !rolled || s.Active != "a" || s.Trial {
		t.Fatalf("mong lùi lại: %v %+v %v", rolled, s, err)
	}

	// Cài lại bản 1.1.0 (khe a vẫn chưa có phiên bản) rồi xác nhận khi khỏe.
	if s, err = ApplyUpdate(root, pub, m, bytes.NewReader(bin), false, testT0); err != nil {
		t.Fatal(err)
	}
	if s, err = CommitUpdate(root, testT0); err != nil || s.Stable != "b" || s.Trial {
		t.Fatalf("commit: %+v %v", s, err)
	}
	if rolled, _, _ := GuardStart(root, 3, testT0); rolled {
		t.Fatal("guard không được lùi bản đã xác nhận")
	}
	// Chống hạ cấp.
	old := signedRelease(t, priv, "1.0.9", []byte("old"))
	if _, err := ApplyUpdate(root, pub, old, bytes.NewReader([]byte("old")), false, testT0); err == nil {
		t.Fatal("mong chống hạ cấp")
	}
	// Bản 1.2.0 vào khe a; lùi tay → về b.
	v2 := []byte("binary v1.2.0")
	if s, err = ApplyUpdate(root, pub, signedRelease(t, priv, "1.2.0", v2), bytes.NewReader(v2), false, testT0); err != nil || s.Active != "a" {
		t.Fatalf("apply 1.2.0: %+v %v", s, err)
	}
	if s, err = RollbackUpdate(root, testT0); err != nil || s.Active != "b" || s.Trial {
		t.Fatalf("rollback tay: %+v %v", s, err)
	}
	if len(s.History) < 6 {
		t.Fatalf("lịch sử: %v", s.History)
	}
	if CompareVersions("1.10.0", "1.9.9") != 1 || CompareVersions("1.0.0", "1.0.0") != 0 {
		t.Fatal("CompareVersions")
	}
}
