// Lệnh seed-synthetic sinh dữ liệu TỔNG HỢP cho Local/Dev/Staging (R-95: dữ liệu
// cá nhân thật không rời production). Không có người thật: họ tên ghép ngẫu
// nhiên, SĐT trong dải 09990xxxxx (nông hộ) và 09991xxxxx (đại diện HTX, chủ máy),
// CCCD dạng 0<tỉnh>99xxxxxxx, địa chỉ "Ấp Tổng Hợp N", mã tổ chức tiền tố SYN-.
//
// Bộ sinh gọi Application API và Admin API của một stack ĐANG CHẠY bằng bearer
// token — không đi tắt vào DB, nên dữ liệu qua đủ kiểm quyền, phạm vi, kho định
// danh và idempotency như dữ liệu thật. Cùng -seed (và cùng ngày chạy) → cùng dữ
// liệu; Idempotency-Key tất định nên chạy lại là an toàn.
//
// Dữ liệu sinh ra: tỉnh/xã ĐBSCL lấy từ seed masterdata · TTKN tỉnh, tổ KN xã ·
// HTX (người đại diện ở vault) · nông hộ (vault) · thửa 0,3–3 ha hình chữ nhật dài
// xoay ngẫu nhiên quanh tâm xã, không chồng lấn · danh bạ trực · nhiệm vụ (một phần
// đã đóng) · báo cáo gieo sạ và sâu bệnh · chủ máy, máy theo định mức × hệ số
// 0,4–1,4 · diện tích vụ Đông Xuân/Hè Thu · chạy cân đối.
//
// Token (tài khoản dịch vụ Keycloak ở Local/Dev) cần: platform_admin (phạm vi toàn
// quốc), masterdata_admin, registry_admin, vault_registrar, kn_province_admin
// (báo cáo hiện trường), cgh_admin, và xác thực MFA (amr) vì Admin API bắt buộc MFA.
//
// Cờ và biến môi trường:
//
//	-app-url   AG_SEED_APP_URL    Application API, mặc định http://localhost:8080
//	-admin-url AG_SEED_ADMIN_URL  Admin API, mặc định http://localhost:8082
//	-token     AG_SEED_TOKEN      bearer token (hoặc -token-file / AG_SEED_TOKEN_FILE)
//	-kc-token-url AG_SEED_KC_TOKEN_URL  (thay -token) endpoint token Keycloak Local/Dev, vd
//	                                   http://localhost:8180/realms/agrigreen/protocol/openid-connect/token
//	-kc-client AG_SEED_KC_CLIENT  client CHỈ-DEV agrigreen-seed (direct access grants), mặc định agrigreen-seed
//	-kc-user   AG_SEED_KC_USER    người dùng quản trị dev (scripts/dev-users.*, đã cấu hình TOTP)
//	           AG_SEED_KC_PASSWORD mật khẩu (chỉ qua biến môi trường, không qua cờ)
//	-kc-otp    AG_SEED_KC_OTP     mã TOTP 6 số hiện tại
//	-seed      AG_SEED_SEED       hạt giống, mặc định 1
//	-scale     AG_SEED_SCALE      small | medium, mặc định small
//	-plan      AG_SEED_PLAN       platform (mặc định) | mkg
//	AG_ENV=production → từ chối chạy (cả hai kế hoạch).
//
// Kế hoạch MKG (-plan mkg, R-95 G3-2) gọi API nội bộ MKG (cmd/api-internal) và đẩy lô số
// đo ký Ed25519 kiểu agent biên tới cmd/ingest qua mTLS; tất định theo -seed:
// lệnh gom (lập + duyệt bởi hai người), số cuộn bale → gather → boat_load, cách tính giá
// (theo tấn / theo cuộn), phiên cân HUB_IN/HUB_OUT (liên kết collection_order_ref), lô
// DIRECT gắn lệnh gom tay, số cân CUSTOMER_IN của khách với x % lô vượt dung sai, độ ẩm
// 12–22 %. Lô đầu là lô golden của seed prototype: 5 800/5 700/4 400 cuộn, HUB_IN 96,8 t,
// ước 88 t → +10 % gắn cờ. Cần cmd/ingest chạy vòng tích phân (phiếu HUB_IN/HUB_OUT được
// chờ tối đa -mkg-wait). Thửa/HTX là mã tổng hợp: nguồn thửa/HTX của fieldops phải trả
// được chúng (RB-2 — cấp quyền chia sẻ chưa chốt; ở Local dùng nguồn giả hoặc dữ liệu của
// kế hoạch platform).
//
//	-mkg-url           AG_SEED_MKG_URL           api-internal ứng dụng, mặc định http://localhost:8081
//	-mkg-admin-url     AG_SEED_MKG_ADMIN_URL     api-internal quản trị (MFA), mặc định http://localhost:8083
//	-mkg-ingest-url    AG_SEED_MKG_INGEST_URL    cmd/ingest mTLS, mặc định https://localhost:8443
//	-mkg-token         AG_SEED_MKG_TOKEN         token tenant MKG: field_supervisor, field_collector,
//	                                             scale_supervisor, reconciliation_officer, có MFA
//	-mkg-approver-token AG_SEED_MKG_APPROVER_TOKEN token NGƯỜI KHÁC: field_supervisor + device_approver (R-56)
//	-mkg-client-cert / -mkg-client-key  AG_SEED_MKG_CLIENT_CERT / _KEY  PEM chứng thư client mTLS
//	                                             (scripts/gen-dev-certs) có CN/SAN = hai mã thiết bị
//	-mkg-ingest-ca     AG_SEED_MKG_INGEST_CA     PEM CA của chứng thư máy chủ ingest
//	-mkg-device-in / -mkg-device-out             mã thiết bị (mặc định syn-scale-<seed>-in / -out)
//	-mkg-over-pct      AG_SEED_MKG_OVER_PCT      % lô CUSTOMER_IN vượt dung sai thương mại, mặc định 10
//	-mkg-wait          AG_SEED_MKG_WAIT          chờ phiếu cân sau khi đóng phiên, mặc định 2m
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Config là cấu hình một lần chạy.
type Config struct {
	AppURL, AdminURL string
	Token            string `json:"-"`
	KC               KCLogin
	Seed             uint64
	Scale            string
	Env              string // AG_ENV
	HTTP             *http.Client
	Log              *slog.Logger
	Today            time.Time // rỗng → hôm nay theo giờ Việt Nam
	Plan             string    // platform | mkg
	MKG              MKGConfig // -plan mkg
}

// ErrProduction: bộ sinh không bao giờ chạy trên production (R-95).
var ErrProduction = errors.New("seed-synthetic: AG_ENV=production — từ chối sinh dữ liệu tổng hợp trên production (R-95)")

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	log := telemetry.NewLogger("seed-synthetic", os.Stdout)
	cfg, err := parseFlags(os.Args[1:], log)
	if err == nil {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if cfg.Plan == "mkg" {
			_, err = RunMKG(ctx, cfg.MKG)
		} else {
			_, err = Run(ctx, cfg)
		}
	}
	if err != nil {
		log.Error("dừng do lỗi", "event", "seed.run", "outcome", "error", "error", err.Error())
		os.Exit(1)
	}
}

func parseFlags(args []string, log *slog.Logger) (Config, error) {
	fs := flag.NewFlagSet("seed-synthetic", flag.ContinueOnError)
	cfg := Config{Log: log, Env: os.Getenv("AG_ENV")}
	fs.StringVar(&cfg.AppURL, "app-url", envOr("AG_SEED_APP_URL", "http://localhost:8080"), "Application API")
	fs.StringVar(&cfg.AdminURL, "admin-url", envOr("AG_SEED_ADMIN_URL", "http://localhost:8082"), "Admin API")
	token := fs.String("token", os.Getenv("AG_SEED_TOKEN"), "bearer token của tài khoản dịch vụ")
	tokenFile := fs.String("token-file", os.Getenv("AG_SEED_TOKEN_FILE"), "tệp chứa bearer token")
	seed := fs.String("seed", envOr("AG_SEED_SEED", "1"), "hạt giống (số nguyên không âm)")
	fs.StringVar(&cfg.Scale, "scale", envOr("AG_SEED_SCALE", "small"), "small | medium")
	fs.StringVar(&cfg.KC.TokenURL, "kc-token-url", os.Getenv("AG_SEED_KC_TOKEN_URL"), "endpoint token Keycloak Local/Dev (thay cho -token)")
	fs.StringVar(&cfg.KC.ClientID, "kc-client", envOr("AG_SEED_KC_CLIENT", "agrigreen-seed"), "client chỉ-dev có direct access grants")
	fs.StringVar(&cfg.KC.Username, "kc-user", os.Getenv("AG_SEED_KC_USER"), "người dùng quản trị dev")
	fs.StringVar(&cfg.KC.OTP, "kc-otp", os.Getenv("AG_SEED_KC_OTP"), "mã TOTP hiện tại")
	cfg.KC.Password = os.Getenv("AG_SEED_KC_PASSWORD")
	fs.StringVar(&cfg.Plan, "plan", envOr("AG_SEED_PLAN", "platform"), "platform | mkg")
	m := &cfg.MKG
	fs.StringVar(&m.AppURL, "mkg-url", envOr("AG_SEED_MKG_URL", "http://localhost:8081"), "API nội bộ MKG (ứng dụng)")
	fs.StringVar(&m.AdminURL, "mkg-admin-url", envOr("AG_SEED_MKG_ADMIN_URL", "http://localhost:8083"), "API nội bộ MKG (quản trị)")
	fs.StringVar(&m.IngestURL, "mkg-ingest-url", envOr("AG_SEED_MKG_INGEST_URL", "https://localhost:8443"), "cmd/ingest mTLS")
	fs.StringVar(&m.Operator, "mkg-token", os.Getenv("AG_SEED_MKG_TOKEN"), "token vận hành tenant MKG")
	fs.StringVar(&m.Approver, "mkg-approver-token", os.Getenv("AG_SEED_MKG_APPROVER_TOKEN"), "token người duyệt (khác người đề xuất)")
	certFile := fs.String("mkg-client-cert", os.Getenv("AG_SEED_MKG_CLIENT_CERT"), "PEM chứng thư client mTLS")
	keyFile := fs.String("mkg-client-key", os.Getenv("AG_SEED_MKG_CLIENT_KEY"), "PEM khóa client mTLS")
	caFile := fs.String("mkg-ingest-ca", os.Getenv("AG_SEED_MKG_INGEST_CA"), "PEM CA máy chủ ingest")
	fs.StringVar(&m.DeviceIn, "mkg-device-in", "", "mã thiết bị HUB_IN")
	fs.StringVar(&m.DeviceOut, "mkg-device-out", "", "mã thiết bị HUB_OUT")
	over := fs.String("mkg-over-pct", envOr("AG_SEED_MKG_OVER_PCT", "10"), "% lô vượt dung sai")
	fs.DurationVar(&m.Wait, "mkg-wait", 2*time.Minute, "chờ phiếu cân")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if cfg.Plan != "platform" && cfg.Plan != "mkg" {
		return cfg, fmt.Errorf("-plan phải là platform hoặc mkg")
	}
	if n, err := strconv.Atoi(*over); err == nil {
		m.OverPct = n
	} else {
		return cfg, fmt.Errorf("-mkg-over-pct phải là số nguyên")
	}
	if cfg.Plan == "mkg" && *certFile != "" {
		hc, der, err := mkgIngestClient(*certFile, *keyFile, *caFile)
		if err != nil {
			return cfg, err
		}
		m.IngestHTTP, m.ClientCert = hc, der
	}
	n, err := strconv.ParseUint(*seed, 10, 64)
	if err != nil {
		return cfg, fmt.Errorf("-seed phải là số nguyên không âm")
	}
	cfg.Seed = n
	m.Seed, m.Scale, m.Env, m.Log = n, cfg.Scale, cfg.Env, log
	cfg.Token = strings.TrimSpace(*token)
	if cfg.Token == "" && *tokenFile != "" {
		b, err := os.ReadFile(*tokenFile) //nolint:gosec // đường dẫn do người vận hành truyền vào
		if err != nil {
			return cfg, fmt.Errorf("đọc -token-file: %w", err)
		}
		cfg.Token = strings.TrimSpace(string(b))
	}
	return cfg, nil
}

// Run kiểm môi trường rồi sinh dữ liệu; trả thống kê theo loại bản ghi.
func Run(ctx context.Context, cfg Config) (*Stats, error) {
	if e := strings.ToLower(strings.TrimSpace(cfg.Env)); e == "production" || e == "prod" {
		return nil, ErrProduction
	}
	sc, ok := scales[cfg.Scale]
	if !ok {
		return nil, fmt.Errorf("-scale phải là small hoặc medium, nhận %q", cfg.Scale)
	}
	if cfg.Log == nil {
		cfg.Log = telemetry.NewLogger("seed-synthetic", io.Discard)
	}
	hc := cfg.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	if cfg.Token == "" && cfg.KC.TokenURL != "" {
		tok, err := fetchToken(ctx, hc, cfg.KC)
		if err != nil {
			return nil, err
		}
		cfg.Token = tok
	}
	if cfg.Token == "" {
		return nil, errors.New("thiếu bearer token (-token, -token-file, AG_SEED_TOKEN hoặc -kc-token-url + -kc-user)")
	}
	today := cfg.Today
	if today.IsZero() {
		today = time.Now().In(clock.Zone())
	}
	s := &seeder{api: &api{app: cfg.AppURL, admin: cfg.AdminURL, token: cfg.Token, hc: hc}, log: cfg.Log, seed: cfg.Seed, scale: sc.Name, st: newStats()}
	start := time.Now()
	plan, err := s.Run(ctx, sc, today)
	if err != nil {
		return s.st, err
	}
	cfg.Log.Info("đã sinh dữ liệu tổng hợp", "event", "seed.done", "seed", cfg.Seed, "scale", sc.Name,
		"provinces", len(plan.Provinces), "created", s.st.Created, "existing", s.st.Existing,
		"duration_ms", time.Since(start).Milliseconds(), "outcome", "ok")
	return s.st, nil
}
