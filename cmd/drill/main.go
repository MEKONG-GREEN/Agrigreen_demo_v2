// Lệnh drill: diễn tập phục hồi (mục 22 tài liệu kiến trúc). Hai bước:
//
//	drill capture -out db-manifest.json
//	    NGAY TRƯỚC khi sao lưu (đã dừng tiến trình ghi): chụp số dòng mọi bảng,
//	    băm bộ tham số đã duyệt, ngày neo mới nhất của mọi cụm.
//	drill check -manifest db-manifest.json [-objects-manifest latest|tệp] [-report báo-cáo.json] [-strict]
//	    Trên môi trường ĐÃ PHỤC HỒI (sạch): số dòng, băm bộ tham số, verify_chain
//	    mọi sổ, tính lại mẫu cân đối CGH (FN-09) so với kết quả đã lưu, checksum kho
//	    đối tượng theo manifest kiểm kê, neo mới nhất khớp manifest ký Ed25519.
//	drill pubkey
//	    In khóa công khai (base64) suy từ AG_ANCHOR_SIGNING_KEY — chỉ cho Local/CI;
//	    production lấy khóa công khai qua kênh riêng, không đọc khóa ký.
//
// Biến môi trường:
//
//	AG_PG_ADMIN_URL        tài khoản đọc toàn cụm (superuser hoặc vai BYPASSRLS chỉ đọc) — chỉ đọc, giao dịch READ ONLY
//	AG_DB_SUFFIX           (tùy chọn) hậu tố database, như pgtest
//	AG_S3_* | AG_OBJSTORE_DIR           kho chính đã phục hồi (manifest neo, đối tượng)
//	AG_BACKUP_S3_* | AG_BACKUP_OBJSTORE_DIR  kho sao lưu, credential restore-reader (đọc manifest kiểm kê)
//	AG_ANCHOR_PUBLIC_KEY   khóa công khai Ed25519 TIN CẬY (base64, 32 byte)
//
// Báo cáo JSON in ra stdout (và -report). Mã thoát: 0 mọi kiểm đạt (với -strict:
// không kiểm nào bị bỏ qua), 1 có kiểm hỏng, 2 sai cấu hình. Runbook:
// docs/runbooks/restore-drill.md.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objsync"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/opsjobs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
)

var errConfig = errors.New("cấu hình")

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "dùng: drill capture|check|pubkey [cờ]")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code, err := run(ctx, os.Args[1], os.Args[2:])
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "drill:", err)
		if errors.Is(err, errConfig) {
			code = 2
		} else if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func run(ctx context.Context, cmd string, args []string) (int, error) {
	fs := flag.NewFlagSet("drill "+cmd, flag.ContinueOnError)
	out := fs.String("out", "", "capture: tệp manifest DB")
	manifest := fs.String("manifest", "", "check: tệp manifest DB do capture sinh")
	objects := fs.String("objects-manifest", "latest", "check: manifest kiểm kê kho đối tượng — khóa trong kho sao lưu, latest, tệp cục bộ (file:đường-dẫn) hoặc none")
	reportPath := fs.String("report", "", "check: ghi báo cáo JSON ra tệp")
	strict := fs.Bool("strict", false, "check: kiểm bị bỏ qua cũng tính là hỏng")
	ignore := fs.String("ignore-tables", "", "check: bảng được phép lệch số dòng, phân tách bằng dấu phẩy (schema.bảng)")
	sample := fs.Int("sample-runs", 20, "check: số lần cân đối mới nhất được tính lại")
	suffix := fs.String("suffix", os.Getenv("AG_DB_SUFFIX"), "hậu tố database (pgtest)")
	if err := fs.Parse(args); err != nil {
		return 2, fmt.Errorf("%w: %w", errConfig, err)
	}
	if err := residency.CheckEnv(); err != nil {
		return 2, fmt.Errorf("%w: %w", errConfig, err)
	}
	if cmd == "pubkey" {
		s, err := opsjobs.SignerFromEnv()
		if err != nil {
			return 2, fmt.Errorf("%w: %w", errConfig, err)
		}
		_, _ = fmt.Fprintln(os.Stdout, base64.StdEncoding.EncodeToString(s.Public()))
		return 0, nil
	}
	cfg := Config{AdminURL: os.Getenv("AG_PG_ADMIN_URL"), Suffix: *suffix, SampleRuns: *sample, IgnoreTables: map[string]bool{}}
	if cfg.AdminURL == "" {
		return 2, fmt.Errorf("%w: thiếu AG_PG_ADMIN_URL", errConfig)
	}
	for _, t := range strings.Split(*ignore, ",") {
		if t = strings.TrimSpace(t); t != "" {
			cfg.IgnoreTables[t] = true
		}
	}
	switch cmd {
	case "capture":
		if *out == "" {
			return 2, fmt.Errorf("%w: capture cần -out", errConfig)
		}
		m, err := Capture(ctx, cfg)
		if err != nil {
			return 1, err
		}
		b, _ := json.MarshalIndent(m, "", "  ")
		if err := os.WriteFile(*out, b, 0o600); err != nil { //nolint:gosec // đường dẫn do người vận hành truyền vào
			return 1, err
		}
		for c, cm := range m.Clusters {
			_, _ = fmt.Fprintf(os.Stdout, "%s: %d bảng, %d chuỗi, neo mới nhất %q, băm tham số %q\n", c, len(cm.Tables), cm.Chains, cm.LatestAnchorDate, cm.ParamSetHash)
		}
		return 0, nil
	case "check":
		return check(ctx, cfg, *manifest, *objects, *reportPath, *strict)
	}
	return 2, fmt.Errorf("%w: lệnh không hỗ trợ %q", errConfig, cmd)
}

func check(ctx context.Context, cfg Config, manifestPath, objectsRef, reportPath string, strict bool) (int, error) {
	if manifestPath == "" {
		return 2, fmt.Errorf("%w: check cần -manifest", errConfig)
	}
	b, err := os.ReadFile(manifestPath) //nolint:gosec // đường dẫn do người vận hành truyền vào
	if err != nil {
		return 2, fmt.Errorf("%w: %w", errConfig, err)
	}
	var want DBManifest
	if err := json.Unmarshal(b, &want); err != nil {
		return 2, fmt.Errorf("%w: manifest DB hỏng: %w", errConfig, err)
	}
	if v := strings.TrimSpace(os.Getenv("AG_ANCHOR_PUBLIC_KEY")); v != "" {
		raw, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return 2, fmt.Errorf("%w: AG_ANCHOR_PUBLIC_KEY phải là base64 của 32 byte", errConfig)
		}
		cfg.AnchorPub = raw
	}
	if st, err := objstore.FromEnv(ctx); err == nil {
		cfg.Store = st
	} else {
		fmt.Fprintln(os.Stderr, "drill: không có kho chính — bỏ qua kiểm neo/đối tượng:", err)
	}
	switch {
	case objectsRef == "none":
	case strings.HasPrefix(objectsRef, "file:"):
		ob, err := os.ReadFile(strings.TrimPrefix(objectsRef, "file:")) //nolint:gosec // đường dẫn do người vận hành truyền vào
		if err != nil {
			return 2, fmt.Errorf("%w: %w", errConfig, err)
		}
		m, err := objsync.ParseManifest(ob)
		if err != nil {
			return 2, fmt.Errorf("%w: %w", errConfig, err)
		}
		cfg.Objects = &m
	default:
		bk, err := objstore.FromEnvPrefix(ctx, "AG_BACKUP_")
		if err != nil {
			return 2, fmt.Errorf("%w: kho sao lưu: %w", errConfig, err)
		}
		m, _, err := objsync.ReadManifest(ctx, bk, objectsRef)
		if err != nil {
			return 1, err
		}
		cfg.Objects = &m
	}
	rep, err := Check(ctx, cfg, want, strict)
	if err != nil {
		return 1, err
	}
	out, _ := json.MarshalIndent(rep, "", "  ")
	_, _ = fmt.Fprintln(os.Stdout, string(out))
	if reportPath != "" {
		if err := os.WriteFile(reportPath, out, 0o600); err != nil { //nolint:gosec // đường dẫn do người vận hành truyền vào
			return 1, err
		}
	}
	if !rep.OK {
		return 1, nil
	}
	return 0, nil
}
