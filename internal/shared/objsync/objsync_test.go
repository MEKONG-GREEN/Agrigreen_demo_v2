package objsync_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objsync"
)

func local(t *testing.T) (*objstore.Local, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := objstore.NewLocal(dir, objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func put(t *testing.T, s objstore.Store, key, body string) {
	t.Helper()
	b := []byte(body)
	if _, err := s.Put(context.Background(), key, bytes.NewReader(b), int64(len(b)), objstore.SHA256Hex(b), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
}

// Sao lưu gia tăng → manifest → phục hồi vào kho sạch → kiểm checksum; rồi làm
// hỏng một đối tượng để chứng minh kiểm phát hiện.
func TestBackupRestoreVerify(t *testing.T) {
	ctx := context.Background()
	primary, _ := local(t)
	backup, _ := local(t)
	put(t, primary, "evidence/hh1/a.bin", "bản mã A")
	put(t, primary, "evidence/hh1/b.bin", "bản mã B")
	put(t, primary, "anchors/pf/2026-09-27.json", `{"payload":{}}`)

	t0 := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	r1, err := objsync.Backup(ctx, primary, backup, objsync.Options{Label: "pf", Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Copied != 3 || r1.Skipped != 0 || r1.Manifest.Count != 3 {
		t.Fatalf("lần đầu phải chép 3: %+v", r1)
	}

	// Lần hai: chỉ chép đối tượng mới (gia tăng).
	put(t, primary, "evidence/hh2/c.bin", "bản mã C")
	r2, err := objsync.Backup(ctx, primary, backup, objsync.Options{Label: "pf", Now: t0.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Copied != 1 || r2.Skipped != 3 || r2.Manifest.Count != 4 {
		t.Fatalf("lần hai phải chỉ chép 1: %+v", r2)
	}

	m, key, err := objsync.ReadManifest(ctx, backup, "latest")
	if err != nil || key != r2.ManifestKey || m.Count != 4 {
		t.Fatalf("manifest mới nhất: %s %v %+v", key, err, m)
	}

	fresh, freshDir := local(t)
	rr, err := objsync.Restore(ctx, backup, fresh, m, "")
	if err != nil || rr.Copied != 4 {
		t.Fatalf("phục hồi: %+v %v", rr, err)
	}
	probs, err := objsync.Verify(ctx, fresh, m)
	if err != nil || len(probs) != 0 {
		t.Fatalf("sau phục hồi checksum phải khớp: %v %v", probs, err)
	}

	// Làm hỏng nội dung (vượt kho — giả lập hỏng đĩa) và xóa một đối tượng.
	if err := os.WriteFile(filepath.Join(freshDir, "objects", "evidence", "hh1", "a.bin"), []byte("bản mã X"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(freshDir, "objects", "evidence", "hh2", "c.bin")); err != nil {
		t.Fatal(err)
	}
	probs, err = objsync.Verify(ctx, fresh, m)
	if err != nil || len(probs) != 2 {
		t.Fatalf("phải phát hiện 2 sai lệch: %v %v", probs, err)
	}
}

// Dùng chung bucket với tiền tố backups/: không sao lưu chính bản sao lưu.
func TestSameBucketPrefix(t *testing.T) {
	ctx := context.Background()
	s, _ := local(t)
	put(t, s, "evidence/a.bin", "A")
	r, err := objsync.Backup(ctx, s, s, objsync.Options{DstPrefix: "backups/"})
	if err != nil || r.Copied != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = objsync.Backup(ctx, s, s, objsync.Options{DstPrefix: "backups/", Now: time.Now().Add(time.Second)})
	if err != nil || r.Copied != 0 || r.Manifest.Count != 1 {
		t.Fatalf("lần hai: %+v %v", r, err)
	}
	if _, err := s.Head(ctx, "backups/evidence/a.bin"); err != nil {
		t.Fatal(err)
	}
}

// Nguồn hỏng (siêu dữ liệu băm khác nội dung) → dừng, không chép bản hỏng.
func TestSourceCorruptionStopsBackup(t *testing.T) {
	ctx := context.Background()
	primary, dir := local(t)
	backup, _ := local(t)
	put(t, primary, "evidence/a.bin", "bản mã A")
	if err := os.WriteFile(filepath.Join(dir, "objects", "evidence", "a.bin"), []byte("hỏng"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := objsync.Backup(ctx, primary, backup, objsync.Options{}); !errors.Is(err, objsync.ErrSourceCorrupt) {
		t.Fatalf("muốn ErrSourceCorrupt, nhận %v", err)
	}
	if _, err := backup.Head(ctx, "evidence/a.bin"); !errors.Is(err, objstore.ErrNotFound) {
		t.Fatal("không được chép bản hỏng")
	}
}

func TestParseManifestRejectsBad(t *testing.T) {
	for _, b := range []string{`{`, `{"format":"x","count":0,"entries":[]}`,
		`{"format":"ag.objects-manifest.v1","count":1,"entries":[]}`,
		`{"format":"ag.objects-manifest.v1","count":1,"entries":[{"key":"../x","size":1,"sha256":"00"}]}`} {
		if _, err := objsync.ParseManifest([]byte(b)); err == nil {
			t.Errorf("manifest %s phải bị từ chối", b)
		}
	}
}
