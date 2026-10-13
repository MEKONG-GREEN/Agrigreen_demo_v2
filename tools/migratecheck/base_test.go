package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitRun(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.email=t@example.invalid", "-c", "user.name=t", "-c", "core.autocrlf=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func wantOne(t *testing.T, p []string, sub string) {
	t.Helper()
	if len(p) != 1 || !strings.Contains(p[0], sub) {
		t.Fatalf("muốn đúng một lỗi chứa %q, nhận %v", sub, p)
	}
}

// WH-B: so với nhánh tích hợp — tệp mới phải dạng mốc UTC và xếp sau mọi tệp đã có trên nhánh; tệp đã có trên
// nhánh (kể cả số tuần tự của gói song song) luôn được chấp nhận.
func TestBaseRules_WHB(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("không có git")
	}
	root := t.TempDir()
	sql := "CREATE TABLE pf_x.t (id int);\n"
	writeFile(t, filepath.Join(root, "pf", "000001_pf_a.up.sql"), sql)
	writeFile(t, filepath.Join(root, "pf", "000002_pf_b.up.sql"), sql)
	writeFile(t, filepath.Join(root, "shared", "000001_s.up.sql"), sql)
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "base")

	p, err := checkBase(root, "HEAD")
	if err != nil || len(p) != 0 {
		t.Fatalf("không đổi gì phải sạch: %v %v", p, err)
	}
	if _, err := checkBase(root, "khong-co-ref"); err == nil {
		t.Fatal("ref không tồn tại phải lỗi")
	}

	// Tệp mới số tuần tự → từ chối (cấp số tập trung là nguồn xung đột).
	legacy := filepath.Join(root, "pf", "000003_pf_c.up.sql")
	writeFile(t, legacy, sql)
	p, _ = checkBase(root, "HEAD")
	wantOne(t, p, "phải dùng tên mốc UTC")

	// Cùng tệp mà đã có trên nhánh gốc (gói song song gộp trước) → chấp nhận.
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "goi song song")
	if p, _ = checkBase(root, "HEAD"); len(p) != 0 {
		t.Fatalf("tệp số cũ đã có trên nhánh gốc phải được chấp nhận: %v", p)
	}

	// Tệp mốc mới (DB mới: sau mọi số cũ; DB có sẵn: lớn hơn phiên bản đã áp) → chấp nhận, cả shared/.
	writeFile(t, filepath.Join(root, "pf", "20261013120000_pf_c.up.sql"), sql)
	writeFile(t, filepath.Join(root, "shared", "20261013120000_s2.up.sql"), sql)
	if p, _ = checkBase(root, "HEAD"); len(p) != 0 {
		t.Fatalf("tệp mốc mới phải sạch: %v", p)
	}
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "goi A")

	// Gói B tạo tệp lúc 09:00 nhưng gộp sau gói A (12:00): golang-migrate bỏ qua im lặng → từ chối.
	late := filepath.Join(root, "pf", "20261013090000_pf_d.up.sql")
	writeFile(t, late, sql)
	p, _ = checkBase(root, "HEAD")
	wantOne(t, p, "không lớn hơn phiên bản lớn nhất")
	if !strings.Contains(p[0], "20261013120000_pf_c.up.sql") || !strings.Contains(p[0], "-new pf_d") {
		t.Errorf("thông báo phải nêu tệp lớn nhất và cách đổi tên: %s", p[0])
	}
	// Đổi tên sang mốc mới → sạch.
	if err := os.Rename(late, filepath.Join(root, "pf", "20261013130000_pf_d.up.sql")); err != nil {
		t.Fatal(err)
	}
	if p, _ = checkBase(root, "HEAD"); len(p) != 0 {
		t.Fatalf("sau đổi tên phải sạch: %v", p)
	}
	// Tệp số cũ sau khi đã có tệp mốc → từ chối (luật tệp mới), dù số chưa dùng.
	writeFile(t, filepath.Join(root, "pf", "000004_pf_e.up.sql"), sql)
	p, _ = checkBase(root, "HEAD")
	wantOne(t, p, "phải dùng tên mốc UTC")
}

// Kiểm tên không cần git: hai dạng, trùng phiên bản, mốc sai/tương lai, tiền tố cụm.
func TestTimestampNames_WHB(t *testing.T) {
	defer func(f func() time.Time) { now = f }(now)
	now = func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) }
	cases := map[string]string{
		"20261013110000_pf_ok.up.sql":     "",
		"000123_legacy_ok.up.sql":         "",
		"20261313110000_pf_bad.up.sql":    "không phải ngày giờ UTC hợp lệ",
		"20251231235959_pf_old.up.sql":    "trước 2026",
		"20261015120000_pf_future.up.sql": "ở tương lai",
		"20261013110000_price_x.up.sql":   "phải đặt tên <mốc>_pf_<module>",
		"2026101311000_pf_x.up.sql":       "tên tệp phải là YYYYMMDDHHMMSS",
	}
	for name, want := range cases {
		_, p := checkName("pf", filepath.Join("pf", name))
		if want == "" && len(p) != 0 || want != "" && (len(p) != 1 || !strings.Contains(p[0], want)) {
			t.Errorf("%s: muốn %q, nhận %v", name, want, p)
		}
	}
	if _, p := checkName("shared", "shared/20261013110000_rls_x.up.sql"); len(p) != 0 {
		t.Errorf("shared/ không cần tiền tố cụm: %v", p)
	}

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pf", "000001_pf_a.up.sql"), "CREATE TABLE a (id int);")
	writeFile(t, filepath.Join(root, "pf", "20261013110000_pf_b.up.sql"), "CREATE TABLE b (id int);")
	writeFile(t, filepath.Join(root, "pf", "20261013110000_pf_c.up.sql"), "CREATE TABLE c (id int);")
	p, err := checkTree(root)
	if err != nil {
		t.Fatal(err)
	}
	wantOne(t, p, "trùng số")

	got, err := newFilePath(root, "pf_price_x", time.Date(2026, 10, 13, 16, 30, 0, 0, time.FixedZone("ICT", 7*3600)))
	if err != nil || got != filepath.ToSlash(filepath.Join(root, "pf", "20261013093000_pf_price_x.up.sql")) {
		t.Errorf("newFilePath: %s %v", got, err)
	}
	if _, err := newFilePath(root, "zz_x", now()); err == nil {
		t.Error("cụm không có phải lỗi")
	}
	if _, err := newFilePath(root, "pf_Bad", now()); err == nil {
		t.Error("chữ hoa phải lỗi")
	}
}
