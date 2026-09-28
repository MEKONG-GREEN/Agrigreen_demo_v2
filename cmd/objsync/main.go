// Lệnh objsync: sao lưu gia tăng kho đối tượng sang kho sao lưu (vùng thứ hai
// trong lãnh thổ) kèm manifest SHA-256, phục hồi ngược lại, và kiểm checksum
// (mục 22). Xem docs/runbooks/backup.md.
//
//	objsync backup   [-prefix P] [-dst-prefix P] [-label L] [-out tệp]   kho chính → kho sao lưu, ghi manifest
//	objsync restore  [-manifest khóa|latest] [-out tệp]                  kho sao lưu → kho chính theo manifest
//	objsync verify   [-manifest khóa|latest] [-manifest-file tệp]         đọc lại kho chính, so checksum
//
// Kho chính: AG_S3_* hoặc AG_OBJSTORE_DIR (như ứng dụng). Kho sao lưu:
// AG_BACKUP_S3_ENDPOINT, AG_BACKUP_S3_BUCKET, AG_BACKUP_S3_ACCESS_KEY,
// AG_BACKUP_S3_SECRET_KEY, AG_BACKUP_S3_USE_TLS (hoặc AG_BACKUP_OBJSTORE_DIR).
// Credential TÁCH BIỆT (mục 22): "backup" dùng danh tính backup-writer của kho
// sao lưu; "restore"/"verify" dùng restore-reader (chỉ đọc) — không lệnh nào
// cần cả quyền ghi sao lưu lẫn quyền đọc phục hồi.
//
// Kết quả in ra stdout dạng JSON (không có bí mật). Mã thoát: 0 ổn, 1 lỗi/sai lệch, 2 sai cú pháp.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objsync"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
)

// BackupEnvPrefix là tiền tố biến của kho sao lưu.
const BackupEnvPrefix = "AG_BACKUP_"

type report struct {
	Command     string            `json:"command"`
	OK          bool              `json:"ok"`
	ManifestKey string            `json:"manifest_key,omitempty"`
	Objects     int               `json:"objects"`
	Copied      int               `json:"copied"`
	Skipped     int               `json:"skipped"`
	Bytes       int64             `json:"bytes"`
	Problems    []objsync.Problem `json:"problems,omitempty"`
	Error       string            `json:"error,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "dùng: objsync backup|restore|verify [cờ]")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	rep, err := run(ctx, os.Args[1], os.Args[2:])
	stop()
	if err != nil {
		rep.Error = err.Error()
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	fmt.Fprintln(os.Stdout, string(b))
	if errors.Is(err, errUsage) {
		os.Exit(2)
	}
	if err != nil || !rep.OK {
		os.Exit(1)
	}
}

var errUsage = errors.New("sai cú pháp")

func run(ctx context.Context, cmd string, args []string) (report, error) {
	rep := report{Command: cmd}
	fs := flag.NewFlagSet("objsync "+cmd, flag.ContinueOnError)
	prefix := fs.String("prefix", "", "backup: chỉ sao lưu khóa có tiền tố này")
	dstPrefix := fs.String("dst-prefix", "", "backup: tiền tố trong kho sao lưu (vd backups/ khi dùng chung bucket)")
	label := fs.String("label", "", "backup: nhãn ghi vào manifest")
	manifestKey := fs.String("manifest", "latest", "restore/verify: khóa manifest trong kho sao lưu hoặc latest")
	manifestFile := fs.String("manifest-file", "", "verify: đọc manifest từ tệp cục bộ thay vì kho sao lưu")
	out := fs.String("out", "", "ghi manifest đã dùng ra tệp cục bộ (lưu kèm báo cáo diễn tập)")
	if err := fs.Parse(args); err != nil {
		return rep, errUsage
	}
	if err := residency.CheckEnv(); err != nil {
		return rep, err
	}
	primary, err := objstore.FromEnv(ctx)
	if err != nil {
		return rep, fmt.Errorf("kho chính: %w", err)
	}
	var m objsync.Manifest
	switch cmd {
	case "backup":
		bk, err := objstore.FromEnvPrefix(ctx, BackupEnvPrefix)
		if err != nil {
			return rep, fmt.Errorf("kho sao lưu: %w", err)
		}
		r, err := objsync.Backup(ctx, primary, bk, objsync.Options{SrcPrefix: *prefix, DstPrefix: *dstPrefix, Label: *label})
		rep.ManifestKey, rep.Copied, rep.Skipped, rep.Bytes, rep.Objects = r.ManifestKey, r.Copied, r.Skipped, r.Bytes, r.Manifest.Count
		if err != nil {
			return rep, err
		}
		m = r.Manifest
	case "restore":
		bk, err := objstore.FromEnvPrefix(ctx, BackupEnvPrefix)
		if err != nil {
			return rep, fmt.Errorf("kho sao lưu: %w", err)
		}
		var key string
		if m, key, err = objsync.ReadManifest(ctx, bk, *manifestKey); err != nil {
			return rep, err
		}
		rep.ManifestKey, rep.Objects = key, m.Count
		r, err := objsync.Restore(ctx, bk, primary, m, "")
		rep.Copied, rep.Skipped, rep.Bytes = r.Copied, r.Skipped, r.Bytes
		if err != nil {
			return rep, err
		}
	case "verify":
		if *manifestFile != "" {
			b, err := os.ReadFile(*manifestFile) //nolint:gosec // đường dẫn do người vận hành truyền vào
			if err != nil {
				return rep, err
			}
			if m, err = objsync.ParseManifest(b); err != nil {
				return rep, err
			}
			rep.ManifestKey = *manifestFile
		} else {
			bk, err := objstore.FromEnvPrefix(ctx, BackupEnvPrefix)
			if err != nil {
				return rep, fmt.Errorf("kho sao lưu: %w", err)
			}
			if m, rep.ManifestKey, err = objsync.ReadManifest(ctx, bk, *manifestKey); err != nil {
				return rep, err
			}
		}
		rep.Objects = m.Count
		probs, err := objsync.Verify(ctx, primary, m)
		rep.Problems = probs
		if err != nil {
			return rep, err
		}
	default:
		return rep, errUsage
	}
	if *out != "" {
		b, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return rep, err
		}
		if err := os.WriteFile(*out, b, 0o600); err != nil {
			return rep, err
		}
	}
	rep.OK = len(rep.Problems) == 0
	return rep, nil
}
