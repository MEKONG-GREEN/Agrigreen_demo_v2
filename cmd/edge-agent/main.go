// Lệnh edge-agent: nhị phân tĩnh chạy tại hub (vùng biên OT), đọc cân PLC/IoT,
// đệm đĩa ≥72 giờ (đầy → ngừng + báo động), ký Ed25519 tại nguồn, đẩy theo lô qua
// mTLS chỉ đi ra (mục 14; ADR-0019). Hợp đồng dây đã đóng băng ở G3-0:
// internal/mkg/measurement/port/edgewire + contracts/edge/batch.v1.json.
//
// Ranh giới (tools/archlint): chỉ import thư viện chuẩn, internal/shared/{clock,ids,mass}
// và edgewire — không pgx, không DB, không module nghiệp vụ; build CGO_ENABLED=0.
// Vì archlint chỉ cho phép đúng các package trên, toàn bộ mã nằm trong package main
// này (không có package con). Bộ điều hợp mẫu cân chính thức chờ C-6.
//
// Lệnh con:
//
//	edge-agent run     -config cfg.json          chạy agent
//	edge-agent enroll  -config cfg.json -epoch N [-rotate] [-abandon-buffer]
//	edge-agent info    -config cfg.json          in key_id, khóa công khai, SHA-256 chứng thư
//	edge-agent update  apply|guard|commit|rollback|status|sign|keygen …
//	edge-agent version
//
// Vận hành: docs/runbooks/edge-agent.md.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// version đặt lúc build: -ldflags "-X main.version=1.2.3".
var version = "0.0.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func usage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `dùng: edge-agent <run|enroll|info|update|version> [cờ]   (xem docs/runbooks/edge-agent.md)`)
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	log := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	var err error
	switch args[0] {
	case "run":
		err = cmdRun(args[1:], log)
	case "enroll":
		err = cmdEnroll(args[1:], stdout, log)
	case "info":
		err = cmdInfo(args[1:], stdout)
	case "update":
		err = cmdUpdate(args[1:], stdout, log)
	case "version":
		_, _ = fmt.Fprintf(stdout, "edge-agent %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
	default:
		usage(stderr)
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "edge-agent:", err)
		return 1
	}
	return 0
}

func loadConfigFlag(fs *flag.FlagSet, args []string) (*Config, error) {
	path := fs.String("config", "/etc/edge-agent/config.json", "tệp cấu hình JSON")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return LoadConfig(*path)
}

func cmdRun(args []string, log *slog.Logger) error {
	cfg, err := loadConfigFlag(flag.NewFlagSet("run", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	a, err := NewAgent(cfg, clock.System{}, nil, log)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return a.Run(ctx)
}

// retire đổi tên tệp/thư mục cũ thành <path>.retired-<epoch>-<thời điểm> (giữ để pháp chứng, không xóa).
func retire(path string, epoch int, now time.Time) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) { //nolint:gosec // đường dẫn do người vận hành cấu hình
		return nil
	}
	return os.Rename(path, fmt.Sprintf("%s.retired-e%d-%s", path, epoch, now.UTC().Format("20060102T150405Z"))) //nolint:gosec // đường dẫn do người vận hành cấu hình
}

// EnrollResult là thông tin in ra cho phê duyệt trong sổ thiết bị.
type EnrollResult struct {
	DeviceID        string `json:"device_id"`
	Epoch           int    `json:"epoch"`
	KeyID           string `json:"key_id"`
	PublicKeyHex    string `json:"public_key_hex"`
	PublicKeyBase64 string `json:"public_key_base64"`
	CSRPath         string `json:"csr_path"`
	CSRSHA256       string `json:"csr_sha256"`
	CSRSubject      string `json:"csr_subject"`
	CSR             string `json:"csr_pem"`
}

// Enroll sinh khóa nguồn + khóa/CSR mTLS cho kỳ epoch và ghi state.json (counter từ 0).
func Enroll(cfg *Config, epoch int, rotate, abandon bool, now time.Time, log *slog.Logger) (EnrollResult, error) {
	var res EnrollResult
	if epoch < 1 {
		return res, errors.New("-epoch phải ≥ 1 (số kỳ do sổ thiết bị cấp)")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil { //nolint:gosec // đường dẫn do người vận hành cấu hình
		return res, err
	}
	st, err := ReadState(cfg.DataDir)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		if !abandon {
			return res, fmt.Errorf("%w — dùng -abandon-buffer để lưu trữ trạng thái cũ và đăng ký kỳ mới", err)
		}
		exists = false
	}
	oldEpoch := 0
	unacked := false
	if exists {
		if !rotate {
			return res, fmt.Errorf("thiết bị đã đăng ký kỳ %d — thay khóa/cài lại dùng -rotate -epoch %d", st.Epoch, st.Epoch+1)
		}
		if epoch <= st.Epoch {
			return res, fmt.Errorf("-epoch %d phải lớn hơn kỳ hiện tại %d (bộ đếm kỳ cũ không bao giờ dùng lại)", epoch, st.Epoch)
		}
		oldEpoch = st.Epoch
		w, werr := OpenWAL(walDir(cfg.DataDir), st.DeviceID, st.Epoch, cfg.Buffer.SegmentBytes, cfg.Buffer.MaxBytes, log)
		if werr != nil {
			unacked = true
		} else {
			unacked = w.Last() > st.AckedCounter
			_ = w.Close()
		}
	} else {
		has, herr := HasSegments(walDir(cfg.DataDir))
		if herr != nil {
			return res, herr
		}
		unacked = has
	}
	if _, perr := os.Stat(pendingPath(cfg.DataDir)); perr == nil {
		unacked = true
	}
	if unacked && !abandon {
		return res, errors.New("đệm còn số đo chưa được ingest xác nhận — chờ đẩy hết, hoặc -abandon-buffer (lưu trữ đệm cũ, số đo đó sẽ không được nhận)")
	}
	// Kỳ mới: lưu trữ (không xóa) khóa, chứng thư, đệm, trạng thái cũ.
	tlsKey := cfg.Ingest.CertKeyFile
	if tlsKey == "" {
		tlsKey = filepath.Join(cfg.DataDir, "client.key")
	}
	csrPath := filepath.Join(cfg.DataDir, "client.csr")
	for _, p := range []string{cfg.KeyFile, tlsKey, csrPath, walDir(cfg.DataDir), pendingPath(cfg.DataDir), statePath(cfg.DataDir)} {
		if err := retire(p, oldEpoch, now); err != nil {
			return res, err
		}
	}
	priv, err := GenerateSourceKey(cfg.KeyFile)
	if err != nil {
		return res, err
	}
	csr, err := GenerateTLSCSR(tlsKey, csrPath, cfg.DeviceID)
	if err != nil {
		return res, err
	}
	pub := priv.Public().(ed25519.PublicKey)
	if err := WriteState(cfg.DataDir, State{DeviceID: cfg.DeviceID, Epoch: epoch, KeyID: edgewire.KeyID(pub), EnrolledAt: now.UTC().Format(time.RFC3339)}); err != nil {
		return res, err
	}
	blk, _ := pem.Decode(csr)
	h := sha256.Sum256(blk.Bytes)
	parsed, _ := x509.ParseCertificateRequest(blk.Bytes)
	subj := ""
	if parsed != nil {
		subj = parsed.Subject.String() + " SAN=" + strings.Join(parsed.DNSNames, ",")
	}
	return EnrollResult{
		DeviceID: cfg.DeviceID, Epoch: epoch, KeyID: edgewire.KeyID(pub),
		PublicKeyHex: hex.EncodeToString(pub), PublicKeyBase64: base64.StdEncoding.EncodeToString(pub),
		CSRPath: csrPath, CSRSHA256: hex.EncodeToString(h[:]), CSRSubject: subj, CSR: string(csr),
	}, nil
}

func cmdEnroll(args []string, stdout io.Writer, log *slog.Logger) error {
	fs := flag.NewFlagSet("enroll", flag.ContinueOnError)
	epoch := fs.Int("epoch", 0, "kỳ đăng ký do sổ thiết bị cấp (lần đầu 1; thay khóa/mất đĩa: N+1)")
	rotate := fs.Bool("rotate", false, "thay khóa: đăng ký kỳ mới cho thiết bị đã đăng ký")
	abandon := fs.Bool("abandon-buffer", false, "lưu trữ đệm/trạng thái cũ không thể gửi (mất đĩa một phần, mất khóa)")
	cfg, err := loadConfigFlag(fs, args)
	if err != nil {
		return err
	}
	res, err := Enroll(cfg, *epoch, *rotate, *abandon, time.Now(), log)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	_, _ = fmt.Fprintf(stdout, "%s\n\nGửi device_id, epoch, public_key_hex, key_id và CSR cho scale_supervisor để đề xuất vào sổ thiết bị;\n"+
		"device_approver (khác người đề xuất, R-56) duyệt. Sau khi CA nội bộ ký CSR, đặt chứng thư vào ingest.cert_file,\n"+
		"chạy `edge-agent info` để lấy cert_sha256 bổ sung vào sổ, rồi khởi động dịch vụ.\n", b)
	return nil
}

func cmdInfo(args []string, stdout io.Writer) error {
	cfg, err := loadConfigFlag(flag.NewFlagSet("info", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	out := map[string]any{"device_id": cfg.DeviceID, "version": version, "required_buffer_bytes": cfg.RequiredBufferBytes(), "buffer_max_bytes": cfg.Buffer.MaxBytes}
	if st, err := ReadState(cfg.DataDir); err == nil {
		out["epoch"], out["key_id_state"], out["acked_counter"], out["last_counter"] = st.Epoch, st.KeyID, st.AckedCounter, st.LastCounter
	}
	if k, err := LoadSourceKey(cfg.KeyFile); err == nil {
		pub := k.Public().(ed25519.PublicKey)
		out["key_id"], out["public_key_hex"] = edgewire.KeyID(pub), hex.EncodeToString(pub)
	} else {
		out["key_error"] = err.Error()
	}
	if cfg.Ingest.CertFile != "" {
		if h, err := CertSHA256(cfg.Ingest.CertFile); err == nil {
			out["cert_sha256"] = h
		} else {
			out["cert_error"] = err.Error()
		}
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	_, _ = fmt.Fprintln(stdout, string(b))
	return nil
}

// openSource mở tệp cục bộ hoặc tải https:// qua client mTLS của agent (chỉ đi ra).
func openSource(cfg *Config, src string) (io.ReadCloser, error) {
	if !strings.HasPrefix(src, "https://") {
		return os.Open(filepath.Clean(src)) //nolint:gosec // đường dẫn do người vận hành truyền vào
	}
	c, err := NewMTLSClient(cfg.Ingest)
	if err != nil {
		return nil, err
	}
	resp, err := c.Get(src) //nolint:noctx,gosec // URL do người vận hành truyền vào; lệnh một lần, client có timeout
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("tải %s: HTTP %d", src, resp.StatusCode)
	}
	return resp.Body, nil
}

func cmdUpdate(args []string, stdout io.Writer, log *slog.Logger) error {
	if len(args) == 0 {
		return errors.New("dùng: edge-agent update <apply|guard|commit|rollback|status|sign|keygen>")
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("update "+sub, flag.ContinueOnError)
	now := time.Now()
	switch sub {
	case "keygen":
		out := fs.String("out", "release.key", "tệp khóa riêng phát hành (giữ NGOÀI thiết bị)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		der, _ := x509.MarshalPKCS8PrivateKey(priv)
		if err := writeNewFile(*out, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "release_public_key=%s\n", hex.EncodeToString(priv.Public().(ed25519.PublicKey)))
		return nil
	case "sign":
		keyPath := fs.String("key", "release.key", "khóa riêng phát hành")
		bin := fs.String("binary", "", "nhị phân cần phát hành")
		ver := fs.String("version", "", "phiên bản MAJOR.MINOR.PATCH")
		goos := fs.String("os", "linux", "GOOS")
		arch := fs.String("arch", runtime.GOARCH, "GOARCH")
		if err := fs.Parse(args); err != nil {
			return err
		}
		priv, err := LoadSourceKey(*keyPath)
		if err != nil {
			return err
		}
		b, err := readFile(*bin)
		if err != nil {
			return err
		}
		h := sha256.Sum256(b)
		m := Manifest{Version: *ver, OS: *goos, Arch: *arch, SHA256: hex.EncodeToString(h[:]), Size: int64(len(b))}
		if err := SignManifest(priv, &m); err != nil {
			return err
		}
		out, _ := json.MarshalIndent(m, "", "  ")
		_, _ = fmt.Fprintln(stdout, string(out))
		return nil
	}
	cfgPath := fs.String("config", "/etc/edge-agent/config.json", "tệp cấu hình JSON")
	manifest := fs.String("manifest", "", "manifest đã ký (tệp hoặc https://)")
	binary := fs.String("binary", "", "nhị phân (tệp hoặc https://)")
	allowDown := fs.Bool("allow-downgrade", false, "cho phép cài phiên bản cũ hơn (chủ ý)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	root := cfg.Update.Root
	if root == "" {
		return errors.New("update.root chưa cấu hình")
	}
	var s Slots
	switch sub {
	case "apply":
		pub, err := ParseReleaseKey(cfg.Update.ReleasePublicKey)
		if err != nil {
			return err
		}
		mr, err := openSource(cfg, *manifest)
		if err != nil {
			return err
		}
		var m Manifest
		err = json.NewDecoder(io.LimitReader(mr, 64<<10)).Decode(&m)
		_ = mr.Close()
		if err != nil {
			return fmt.Errorf("manifest: %w", err)
		}
		br, err := openSource(cfg, *binary)
		if err != nil {
			return err
		}
		defer func() { _ = br.Close() }()
		if s, err = ApplyUpdate(root, pub, m, br, *allowDown, now); err != nil {
			return err
		}
		log.Info("đã cài bản cập nhật vào khe thử — khởi động lại dịch vụ: systemctl restart edge-agent", "version", m.Version, "slot", s.Active)
	case "guard":
		var rolled bool
		if rolled, s, err = GuardStart(root, cfg.Update.MaxAttempts, now); err != nil {
			return err
		}
		if rolled {
			log.Error("bản cập nhật không khỏe — đã lùi về khe ổn định", "slot", s.Active, "version", s.Versions[s.Active])
		}
	case "commit":
		if s, err = CommitUpdate(root, now); err != nil {
			return err
		}
	case "rollback":
		if s, err = RollbackUpdate(root, now); err != nil {
			return err
		}
	case "status":
		if s, err = ReadSlots(root); err != nil {
			return err
		}
	default:
		return fmt.Errorf("update %s không hỗ trợ", sub)
	}
	out, _ := json.MarshalIndent(s, "", "  ")
	_, _ = fmt.Fprintln(stdout, string(out))
	return nil
}
