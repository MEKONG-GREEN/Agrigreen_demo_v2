// Package gateway là thân tiến trình của cổng tích hợp (cmd/integration-gateway, ADR-0033 §1; W5.2): một
// cổng cho mọi đối tác — MKG như bên thứ ba. Mở rộng từ cmd/api-public (ADR-0022), giữ nguyên khóa API HMAC
// có pepper theo phiên bản, hạn mức + 429, sổ đo lường trước khi trả byte (ADR-0023), điểm kiểm soát xuất
// đóng khi lỗi (ADR-0024); thêm listener mTLS cho đối tác ghi khối lượng lớn (§41.2) — chứng chỉ gắn với
// khách hàng API qua port MachineIdentity, kiểm CẢ khóa lẫn chứng chỉ.
//
// cmd/integration-gateway và bí danh một kỳ cmd/api-public (ADR-0033 §7.5) cùng gọi Run; chỉ khác tên đơn vị
// (log, telemetry, application_name). Không import vùng mkg (R-03) và không import kho định danh, kể cả
// client (RB-3) — archlint cưỡng chế cho cả hai gốc.
//
// Cấu hình (biến môi trường; tên AG_GATEWAY_* thắng, tên AG_PUBLIC_* cũ giữ một kỳ):
//
//	AG_GATEWAY_ENABLED | AG_PUBLIC_API_ENABLED     "1" mới nghe; khác → thoát lỗi (đóng khi lỗi)
//	AG_GATEWAY_HTTP_ADDR | AG_PUBLIC_HTTP_ADDR     listener HTTP sau Ingress (chỉ khóa API), mặc định :8085
//	AG_GATEWAY_OPERATOR_TENANT | AG_PUBLIC_OPERATOR_TENANT  tenant đơn vị vận hành — bắt buộc
//	AG_GATEWAY_REPLICAS | AG_PUBLIC_REPLICAS       số bản sao (chia giới hạn tần suất trong tiến trình), mặc định 1
//	AG_APIKEY_PEPPER_V<n>, AG_APIKEY_PEPPER_CURRENT pepper HMAC (≥ 32 byte) — bắt buộc
//	AG_GATEWAY_MTLS_ADDR        (tùy chọn) listener mTLS cho đối tác ghi lớn, vd :8443 — rỗng = tắt
//	AG_GATEWAY_TLS_CERT, AG_GATEWAY_TLS_KEY  chứng thư máy chủ của listener mTLS
//	AG_GATEWAY_CLIENT_CA        CA đối tác (PEM) ký chứng chỉ client — KHÔNG phải CA dịch vụ nội bộ
//	AG_GATEWAY_MTLS_BINDINGS    tệp JSON gắn chứng chỉ ↔ khách hàng API (StaticIdentity); bắt buộc khi bật mTLS
//	AG_VALKEY_URL, AG_PF_DB_URL, AG_S3_*, AG_OBJSTORE_DIR, AG_EGRESS_MODE, AG_DB_PASSWORD_APP_PUBLIC
//	hoặc AG_DB_APP_PASSWORD, AG_DB_VIA_PGBOUNCER, AG_ENV, AG_OTLP_ENDPOINT — như api-public.
//
// Vai đăng nhập DB: app_public (ADR-0028, ADR-0033 §1) — cổng là CÙNG đơn vị triển khai với api-public cũ,
// không thêm vai/bí mật/userlist PgBouncer.
package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Tên đơn vị triển khai.
const (
	Unit      = "integration-gateway"
	AliasUnit = "api-public" // bí danh một kỳ (ADR-0033 §7.5) — gỡ ở bản phát hành sau W5.2
)

// Biến môi trường mới của cổng (ADR-0033 Hệ quả: AG_PUBLIC_API_ENABLED → AG_GATEWAY_ENABLED, tên cũ giữ một kỳ).
const (
	EnvEnabled        = "AG_GATEWAY_ENABLED"
	EnvHTTPAddr       = "AG_GATEWAY_HTTP_ADDR"
	EnvOperatorTenant = "AG_GATEWAY_OPERATOR_TENANT"
	EnvReplicas       = "AG_GATEWAY_REPLICAS"
	EnvMTLSAddr       = "AG_GATEWAY_MTLS_ADDR"
	EnvTLSCert        = "AG_GATEWAY_TLS_CERT"
	EnvTLSKey         = "AG_GATEWAY_TLS_KEY"
	EnvClientCA       = "AG_GATEWAY_CLIENT_CA"
	EnvMTLSBindings   = "AG_GATEWAY_MTLS_BINDINGS"

	legacyReplicas = "AG_PUBLIC_REPLICAS"
)

// ErrDisabled: cổng tắt (cờ bật ≠ "1") — tiến trình thoát lỗi, không mở cổng.
var ErrDisabled = errors.New("integration-gateway: " + EnvEnabled + " (hoặc " + port.EnvEnabled + ") ≠ 1 — cổng tắt, thoát")

// envAny trả giá trị biến mới, rỗng thì biến cũ (kỳ chuyển tên).
func envAny(getenv func(string) string, name, legacy string) string {
	if v := getenv(name); v != "" {
		return v
	}
	return getenv(legacy)
}

// Enabled: cờ bật (tên mới hoặc cũ) bằng đúng "1".
func Enabled(getenv func(string) string) bool {
	return envAny(getenv, EnvEnabled, port.EnvEnabled) == "1"
}

// MTLSConfig là cấu hình listener mTLS đã đọc (rỗng Addr = tắt).
type MTLSConfig struct {
	Addr     string
	TLS      *tls.Config
	Identity MachineIdentity
}

// mtlsFromEnv đọc cấu hình mTLS. Tệp gắn chứng chỉ có thể đặt mà không bật listener: khi đó khách hàng
// require_mtls bị từ chối ở listener HTTP (đóng khi lỗi). Bật listener mà thiếu tệp gắn → lỗi khởi động.
func mtlsFromEnv(getenv func(string) string) (MTLSConfig, error) {
	cfg := MTLSConfig{Addr: getenv(EnvMTLSAddr)}
	if path := getenv(EnvMTLSBindings); path != "" {
		id, err := LoadStaticIdentity(path)
		if err != nil {
			return MTLSConfig{}, err
		}
		cfg.Identity = id
	}
	if cfg.Addr == "" {
		return cfg, nil
	}
	if cfg.Identity == nil {
		return MTLSConfig{}, fmt.Errorf("integration-gateway: %s đặt nhưng thiếu %s (gắn chứng chỉ ↔ khách hàng API)", EnvMTLSAddr, EnvMTLSBindings)
	}
	t, err := ServerTLS(getenv(EnvTLSCert), getenv(EnvTLSKey), getenv(EnvClientCA))
	if err != nil {
		return MTLSConfig{}, err
	}
	cfg.TLS = t
	return cfg, nil
}

// NewHandler dựng handler HTTP của cổng: router Public của pf/apiclient (mọi route /v1 — W5.3/W5.4 thêm feed
// và lô vào cùng router, kế thừa xác thực, hạn mức, đo lường, DLP), bọc lớp đọc chứng chỉ client đã kiểm.
// ids ≠ nil → xác thực kiểm thêm gắn chứng chỉ (MTLSAuthenticator). Dùng cho cả listener HTTP lẫn mTLS.
func NewHandler(mod *apiclient.Module, d apiclient.PublicDeps, ids MachineIdentity, log *slog.Logger) http.Handler {
	if ids != nil {
		inner := d.Auth
		if inner == nil {
			inner = mod.Authenticator()
		}
		d.Auth = MTLSAuthenticator{Inner: inner, Identity: ids, Log: log}
	}
	return WithPeerCert(mod.PublicRouter(d))
}

// Run chạy cổng tới khi ctx hủy. unit là tên đơn vị (Unit hoặc AliasUnit).
func Run(ctx context.Context, log *slog.Logger, getenv func(string) string, unit string) error {
	if !Enabled(getenv) {
		return ErrDisabled
	}
	// Môi trường thử của đối tác (W5.8, R-INT-08): production + AG_PARTNER_SANDBOX → không khởi động (trước mọi kết nối).
	sandbox, err := port.SandboxFromEnv(getenv, residency.Production(getenv))
	if err != nil {
		return err
	}
	if sandbox {
		log.Warn("CHẾ ĐỘ THỬ CỦA ĐỐI TÁC — chỉ nhận khóa agks_, DB phải là DB thử dữ liệu tổng hợp", "event", "startup", "sandbox", true, "outcome", "ok")
	}
	// RB-7: production chỉ nối đích trong lãnh thổ.
	if err := residency.CheckEnv(); err != nil {
		return err
	}
	operator, err := uuid.Parse(envAny(getenv, EnvOperatorTenant, port.EnvOperatorTenant))
	if err != nil || operator == uuid.Nil {
		return fmt.Errorf("%s: %s (hoặc %s) bắt buộc, UUID tenant đơn vị vận hành", unit, EnvOperatorTenant, port.EnvOperatorTenant)
	}
	peppers, err := apiclient.PeppersFromEnv()
	if err != nil {
		return err
	}
	if peppers == nil {
		return fmt.Errorf("%s: thiếu %s<n> / %s", unit, port.EnvPepperPrefix, port.EnvPepperCurrent)
	}
	mt, err := mtlsFromEnv(getenv)
	if err != nil {
		return err
	}
	replicas := 1
	if v := envAny(getenv, EnvReplicas, legacyReplicas); v != "" {
		if replicas, err = strconv.Atoi(v); err != nil || replicas < 1 {
			return fmt.Errorf("%s: %s phải là số nguyên ≥ 1", unit, EnvReplicas)
		}
	}
	mode, err := meter.ParseMode(getenv(meter.EnvEgressMode))
	if err != nil {
		return err
	}
	base := getenv("AG_PF_DB_URL")
	if base == "" {
		return fmt.Errorf("%s: thiếu AG_PF_DB_URL", unit)
	}
	shutdownOTel, err := telemetry.Setup(ctx, unit)
	if err != nil {
		return err
	}
	defer func() { _ = shutdownOTel(context.Background()) }()

	opts := db.OptionsFromEnv(unit)
	var pools []*pgxpool.Pool
	defer func() {
		for _, p := range pools {
			p.Close()
		}
	}()
	// W2.6 (ADR-0028): một pool cho vai đăng nhập đơn vị app_public — mở lại cùng vai trả cùng pool.
	opened := map[string]*pgxpool.Pool{}
	open := func(ctx context.Context, role string) (*pgxpool.Pool, error) {
		if p, ok := opened[role]; ok {
			return p, nil
		}
		dsn, err := dbadmin.RoleURL(base, migrations.PF, "", role)
		if err != nil {
			return nil, err
		}
		p, err := db.Open(ctx, dsn, opts)
		if err == nil {
			pools = append(pools, p)
			opened[role] = p
		}
		return p, err
	}
	var store objstore.Store
	if getenv("AG_S3_ENDPOINT") != "" || getenv("AG_OBJSTORE_DIR") != "" {
		if store, err = objstore.FromEnv(ctx); err != nil {
			return err
		}
	}
	if getenv(port.EnvValkeyURL) != "" {
		log.Warn("AG_VALKEY_URL đã đặt nhưng adapter Valkey chưa có — dùng giới hạn trong tiến trình chia theo số bản sao",
			"event", "startup", "outcome", "degraded")
	}
	production := residency.Production(getenv)
	allowUnapproved := getenv(port.EnvAllowUnapproved) == "1"
	if allowUnapproved && production {
		return fmt.Errorf("%s: %s=1 bị cấm ở production (hợp đồng tích hợp chưa approved không phục vụ)", unit, port.EnvAllowUnapproved)
	}
	if allowUnapproved {
		log.Warn("phục vụ feed có hợp đồng tích hợp chưa approved (chỉ ngoài production)", "event", "startup", "outcome", "degraded")
	}
	ing, err := IngestFromEnv(getenv, production, log)
	if err != nil {
		return err
	}
	if c, ok := ing.Queue.(interface{ Close() }); ok {
		defer c.Close()
	}
	mod, d, err := assembly.Build(ctx, open, assembly.Config{Operator: operator, Peppers: peppers, Store: store, Replicas: replicas,
		Production: production, AllowUnapproved: allowUnapproved, EgressMode: mode, Clock: clock.System{}, Log: log,
		IngestQueue: ing.Queue, IngestAllowProposed: ing.AllowProposed, IngestMaxQueued: ing.MaxQueued, IngestRetryAfter: ing.RetryAfter,
		IngestMaxFileBytes: ing.MaxFileBytes, IngestMaxFileRecords: ing.MaxFileRecords, Sandbox: sandbox})
	if err != nil {
		return err
	}
	if d.Ingest != nil {
		// Bộ xử lý lô chạy trong tiến trình cổng: consumer hàng đợi riêng của từng đối tác (vách ngăn R-INT-04).
		go func() {
			if err := d.Ingest.Run(ctx); err != nil && ctx.Err() == nil {
				log.Error("bộ xử lý lô nạp dừng", "event", "ingest.worker_stopped", "outcome", "error", "error", err.Error())
			}
		}()
	}
	if mt.Identity != nil && mt.Addr == "" {
		log.Warn("đã nạp gắn chứng chỉ nhưng listener mTLS tắt — khách hàng require_mtls bị từ chối (401)",
			"event", "startup", "outcome", "degraded")
	}
	handler := NewHandler(mod, d, mt.Identity, log)
	servers := []*http.Server{{Addr: envAny(getenv, EnvHTTPAddr, port.EnvHTTPAddr), Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 5 * time.Minute}}
	if servers[0].Addr == "" {
		servers[0].Addr = port.DefaultHTTPAddr
	}
	if mt.Addr != "" {
		servers = append(servers, &http.Server{Addr: mt.Addr, Handler: handler, TLSConfig: mt.TLS,
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 5 * time.Minute})
	}
	errc := make(chan error, len(servers))
	for _, s := range servers {
		go func(s *http.Server) {
			if s.TLSConfig != nil {
				errc <- s.ListenAndServeTLS("", "")
				return
			}
			errc <- s.ListenAndServe()
		}(s)
		log.Info("lắng nghe", "event", "startup", "unit", unit, "addr", s.Addr, "mtls", s.TLSConfig != nil, "outcome", "ok")
	}
	var runErr error
	select {
	case <-ctx.Done():
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, s := range servers {
		if err := s.Shutdown(shutdown); err != nil && runErr == nil {
			runErr = err
		}
	}
	return runErr
}
