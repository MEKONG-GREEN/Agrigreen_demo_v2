package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCfgFile ghi tệp cấu hình JSON tối thiểu (đệm nhỏ) cho kiểm thử lệnh CLI.
func writeCfgFile(t *testing.T, dataDir string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"device_id": "edge-sim-01",
		"data_dir":  dataDir,
		"adapter":   map[string]any{"type": "simulator"},
		"buffer":    map[string]any{"max_rate_hz": 0.0001, "segment_bytes": 4096, "max_bytes": 1 << 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "cfg.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Bước dựng Compose dev (edge-sim-enroll) chạy lại mỗi lần `up`: lần đầu đăng ký,
// các lần sau giữ nguyên khóa/kỳ và thoát 0 (ADR-0019: không sinh khóa mới ngầm).
func TestEnrollSkipIfEnrolledIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	cfgPath := writeCfgFile(t, dir)
	args := []string{"enroll", "-config", cfgPath, "-epoch", "1", "-skip-if-enrolled"}
	var out, errb bytes.Buffer
	if code := run(args, &out, &errb); code != 0 {
		t.Fatalf("enroll lần đầu: mã %d, %s", code, errb.String())
	}
	key1, err := os.ReadFile(filepath.Join(dir, "source.key"))
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := run(args, &out, &errb); code != 0 {
		t.Fatalf("enroll lần hai với -skip-if-enrolled: mã %d, %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "giữ nguyên") || !strings.Contains(out.String(), `"public_key_base64"`) {
		t.Fatalf("thiếu thông tin kỳ hiện tại: %s", out.String())
	}
	key2, _ := os.ReadFile(filepath.Join(dir, "source.key"))
	if !bytes.Equal(key1, key2) {
		t.Fatal("khóa nguồn bị thay khi đã đăng ký")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.retired-*")); len(m) != 0 {
		t.Fatalf("không được lưu trữ tệp nào: %v", m)
	}
	// Không có cờ: vẫn từ chối đăng ký lại như cũ.
	if code := run([]string{"enroll", "-config", cfgPath, "-epoch", "1"}, &out, &errb); code == 0 {
		t.Fatal("mong từ chối đăng ký lại khi không có -skip-if-enrolled")
	}
	// Khóa nguồn mất: -skip-if-enrolled không che mất đĩa.
	if err := os.Rename(filepath.Join(dir, "source.key"), filepath.Join(dir, "source.key.moved")); err != nil {
		t.Fatal(err)
	}
	if code := run(args, &out, &errb); code == 0 {
		t.Fatal("mong lỗi khi khóa nguồn mất")
	}
	// info in public_key_base64 (dạng Admin API sổ thiết bị nhận).
	if err := os.Rename(filepath.Join(dir, "source.key.moved"), filepath.Join(dir, "source.key")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := run([]string{"info", "-config", cfgPath}, &out, &errb); code != 0 {
		t.Fatalf("info: %s", errb.String())
	}
	var info map[string]any
	if err := json.Unmarshal(out.Bytes(), &info); err != nil || info["public_key_base64"] == nil || info["epoch"] != float64(1) {
		t.Fatalf("info: %v %s", err, out.String())
	}
}
