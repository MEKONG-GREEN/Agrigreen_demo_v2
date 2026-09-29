// Lệnh ingest: nhận lô số đo có chữ ký nguồn từ agent biên qua mTLS (mục 13.2,
// 14, 17; ADR-0019), ghi ts_weigh (cụm ts, vai ingest_ts) và tích phân phiếu cân
// (R-70; đọc cửa sổ bằng ts_reader, ghi phiếu qua port.TicketWriter trên vai
// ingest_mkg ở cụm mkg). Gốc lắp ráp vùng mkg — không import internal/platform/*.
// Hiện thực: internal/mkg/measurement/ingest.
//
// Cấu hình (biến môi trường):
//
//	AG_HTTP_ADDR                   bề mặt vận hành HTTP thuần (/healthz, /readyz), mặc định :8091 — chỉ trong cụm
//	AG_INGEST_HTTP_ADDR            listener nhận lô mTLS, mặc định :8443
//	AG_INGEST_TLS_CERT, AG_INGEST_TLS_KEY   chứng thư máy chủ
//	AG_INGEST_CLIENT_CA            CA cấp chứng thư client của thiết bị (bắt buộc)
//	AG_TS_DB_URL                   URL gốc tới cụm ts (host, cổng, sslmode) — vai ingest_ts, ts_reader
//	AG_MKG_DB_URL                  URL gốc tới cụm mkg — vai ingest_mkg
//	AG_DB_PASSWORD_<VAI> hoặc AG_DB_APP_PASSWORD; AG_DB_VIA_PGBOUNCER=1 qua PgBouncer
//	AG_INGEST_RATE_PER_MIN         lô/phút mỗi thiết bị, mặc định 120 (0 = không giới hạn)
//	AG_INGEST_RATE_BURST           dồn tối đa, mặc định 60
//	AG_INGEST_SKEW_FLAG_MS         ngưỡng gắn cờ lệch đồng hồ, mặc định 2000 (tạm)
//	AG_INGEST_INTEGRATE_INTERVAL   nhịp vòng tích phân, mặc định 15s
//	AG_INGEST_STUCK_AFTER          báo động phiên kẹt, mặc định 30m (tạm)
//	AG_OTLP_ENDPOINT               bật OpenTelemetry (ADR-0013)
//
// `ingest -healthcheck <url>`: GET url, thoát 0 khi 200 (ảnh distroless).
//
// Thiếu AG_TS_DB_URL: chạy chế độ G0 (chỉ /healthz trên AG_HTTP_ADDR).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// wireMeasurement nối hiện thực cổng mkg/measurement trên pool ingest_mkg. Người
// tích hợp gán khi module measurement (store sổ thiết bị, phiên cân, phiếu cân) có mặt.
var wireMeasurement func(ctx context.Context, pool *pgxpool.Pool, clk clock.Clock) (measurement.IngestPorts, error)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "-healthcheck" {
		os.Exit(healthcheck(os.Args[2]))
	}
	log := telemetry.NewLogger("ingest", os.Stdout)
	if err := run(log); err != nil {
		log.Error("dừng do lỗi", "event", "startup", "outcome", "error", "error", err.Error())
		os.Exit(1)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) (int, error) {
	v := os.Getenv(k)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s phải là số nguyên ≥ 0", k)
	}
	return n, nil
}

func envDur(k string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(k)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s phải là khoảng thời gian dương (vd 15s)", k)
	}
	return d, nil
}

type config struct {
	ratePerMin, burst, skewMs int
	interval, stuckAfter      time.Duration
}

func configFromEnv() (config, error) {
	var c config
	var err error
	if c.ratePerMin, err = envInt("AG_INGEST_RATE_PER_MIN", 120); err != nil {
		return c, err
	}
	if c.burst, err = envInt("AG_INGEST_RATE_BURST", 60); err != nil {
		return c, err
	}
	if c.skewMs, err = envInt("AG_INGEST_SKEW_FLAG_MS", measurement.IngestDefaultSkewFlagMs); err != nil {
		return c, err
	}
	if c.interval, err = envDur("AG_INGEST_INTEGRATE_INTERVAL", 15*time.Second); err != nil {
		return c, err
	}
	if c.stuckAfter, err = envDur("AG_INGEST_STUCK_AFTER", measurement.IngestDefaultStuckAfter); err != nil {
		return c, err
	}
	return c, nil
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := residency.CheckEnv(); err != nil { // RB-7
		return err
	}
	opsAddr := env("AG_HTTP_ADDR", ":8091")
	tsBase := os.Getenv("AG_TS_DB_URL")
	if tsBase == "" {
		return serve(ctx, log, []*http.Server{{Addr: opsAddr, Handler: opsRouter(log, nil)}}, nil)
	}
	mkgBase := os.Getenv("AG_MKG_DB_URL")
	if mkgBase == "" {
		return errors.New("thiếu AG_MKG_DB_URL (sổ thiết bị và phiếu cân ở cụm mkg)")
	}
	cfg, err := configFromEnv()
	if err != nil {
		return err
	}
	tlsCfg, err := measurement.IngestServerTLS(os.Getenv("AG_INGEST_TLS_CERT"), os.Getenv("AG_INGEST_TLS_KEY"), os.Getenv("AG_INGEST_CLIENT_CA"))
	if err != nil {
		return err
	}
	shutdownOTel, err := telemetry.Setup(ctx, "ingest")
	if err != nil {
		return err
	}
	defer func() { _ = shutdownOTel(context.Background()) }()
	opts := db.OptionsFromEnv("ingest")
	var pools []*pgxpool.Pool
	defer func() {
		for _, p := range pools {
			p.Close()
		}
	}()
	open := func(base string, c migrations.Cluster, role string) (*pgxpool.Pool, error) {
		dsn, err := dbadmin.RoleURL(base, c, "", role)
		if err != nil {
			return nil, err
		}
		p, err := db.Open(ctx, dsn, opts)
		if err != nil {
			return nil, fmt.Errorf("mở pool %s/%s: %w", c, role, err)
		}
		pools = append(pools, p)
		return p, nil
	}
	tsWriter, err := open(tsBase, migrations.TS, port.IngestTSRole)
	if err != nil {
		return err
	}
	tsReader, err := open(tsBase, migrations.TS, port.TSReaderRole)
	if err != nil {
		return err
	}
	mkgPool, err := open(mkgBase, migrations.MKG, port.IngestRole)
	if err != nil {
		return err
	}
	clk := clock.System{}
	if wireMeasurement == nil {
		return measurement.ErrIngestPortsNotWired
	}
	ports, err := wireMeasurement(ctx, mkgPool, clk)
	if err != nil {
		return err
	}
	if err := ports.Validate(); err != nil {
		return err
	}
	metrics := measurement.NewIngestMetrics(telemetry.Meter())
	store := &measurement.IngestTSStore{Writer: tsWriter, Reader: tsReader}
	rv := &measurement.IngestReceiver{
		Devices: ports.Devices, Store: store, Clock: clk, Log: log, Metrics: metrics,
		Limiter:    measurement.NewIngestRateLimiter(clk, cfg.ratePerMin, cfg.burst),
		SkewFlagMs: int64(cfg.skewMs),
	}
	loop := &measurement.IngestLoop{
		Sessions: ports.Sessions, Integrator: &measurement.IngestIntegrator{Windows: store, Tare: ports.Tare},
		Tickets: ports.Tickets, Clock: clk, Log: log, Metrics: metrics, StuckAfter: cfg.stuckAfter,
	}
	go loop.Run(ctx, cfg.interval)
	ingestSrv := &http.Server{Addr: env("AG_INGEST_HTTP_ADDR", ":8443"), Handler: rv.NewEngine(), TLSConfig: tlsCfg, ReadHeaderTimeout: 5 * time.Second}
	return serve(ctx, log, []*http.Server{{Addr: opsAddr, Handler: opsRouter(log, pools)}}, ingestSrv)
}

// opsRouter: /healthz (sống) và /readyz (mọi pool ping được).
func opsRouter(log *slog.Logger, pools []*pgxpool.Pool) http.Handler {
	r := httpx.NewEngine(log, 1<<10)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "zone": "mkg", "service": "ingest"}) })
	r.GET("/readyz", func(c *gin.Context) {
		if pools == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_configured"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		for _, p := range pools {
			if err := p.Ping(ctx); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db_unavailable"})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready", "zone": "mkg", "service": "ingest"})
	})
	return r
}

// healthcheck gọi url (loopback do vận hành cấu hình) và trả mã thoát.
func healthcheck(url string) int {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(url) //nolint:gosec,noctx // URL cố định trong cấu hình healthcheck
	if err != nil {
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func serve(ctx context.Context, log *slog.Logger, plain []*http.Server, tlsSrv *http.Server) error {
	errc := make(chan error, len(plain)+1)
	for _, s := range plain {
		s.ReadHeaderTimeout, s.ReadTimeout, s.WriteTimeout = 5*time.Second, 10*time.Second, 10*time.Second
		go func(s *http.Server) { errc <- s.ListenAndServe() }(s)
		log.Info("lắng nghe", "event", "startup", "addr", s.Addr, "surface", "ops", "outcome", "ok")
	}
	all := plain
	if tlsSrv != nil {
		tlsSrv.ReadHeaderTimeout, tlsSrv.ReadTimeout, tlsSrv.WriteTimeout = 5*time.Second, 30*time.Second, 30*time.Second
		go func() { errc <- tlsSrv.ListenAndServeTLS("", "") }()
		log.Info("lắng nghe", "event", "startup", "addr", tlsSrv.Addr, "surface", "ingest_mtls", "outcome", "ok")
		all = append(all, tlsSrv)
	}
	select {
	case <-ctx.Done():
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, s := range all {
		_ = s.Shutdown(shutdown)
	}
	return nil
}
