// Lệnh worker-internal: consumer sự kiện NỘI BỘ MKG (vùng mkg, mục 5) trên vhost
// RabbitMQ mkg.internal (ADR-0018). Sổ consumer ở cmd/worker-internal/assembly
// (G3-0: rỗng — khung, health, khai báo hàng đợi). Không bao giờ nối vhost nền tảng.
//
// Cấu hình (biến môi trường):
//
//	AG_MKG_DB_URL          URL gốc tới database cụm mkg (host, cổng, sslmode) — vai ghép theo consumer
//	AG_DB_PASSWORD_<VAI>   hoặc AG_DB_APP_PASSWORD
//	AG_DB_VIA_PGBOUNCER    =1 khi đi qua PgBouncer transaction mode
//	AG_AMQP_INTERNAL_URL   URL RabbitMQ của vhost mkg.internal (tài khoản riêng của vùng mkg)
//	AG_HEALTH_ADDR         /healthz, /readyz — mặc định :8086 (chỉ trong mạng nội bộ)
//	AG_OTLP_ENDPOINT, ...  OpenTelemetry (shared/telemetry)
//
// `worker-internal -healthcheck <url>`: GET url, thoát 0 khi 200 — healthcheck Docker (distroless).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/worker-internal/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "-healthcheck" {
		os.Exit(healthcheck(os.Args[2]))
	}
	log := telemetry.NewLogger("worker-internal", os.Stdout)
	if err := run(log); err != nil {
		log.Error("dừng do lỗi", "event", "startup", "outcome", "error", "error", err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := residency.CheckEnv(); err != nil { // RB-7
		return err
	}
	base, amqpURL := os.Getenv("AG_MKG_DB_URL"), os.Getenv("AG_AMQP_INTERNAL_URL")
	if base == "" || amqpURL == "" {
		return errors.New("thiếu AG_MKG_DB_URL hoặc AG_AMQP_INTERNAL_URL")
	}
	shutdownOTel, err := telemetry.Setup(ctx, "worker-internal")
	if err != nil {
		return err
	}
	defer func() { _ = shutdownOTel(context.Background()) }()

	opts := db.OptionsFromEnv("worker-internal")
	pools := map[string]*pgxpool.Pool{}
	defer func() {
		for _, p := range pools {
			p.Close()
		}
	}()
	pool := func(role string) (*pgxpool.Pool, error) {
		if role == "" {
			role = assembly.OpsRole
		}
		if p, ok := pools[role]; ok {
			return p, nil
		}
		dsn, err := dbadmin.RoleURL(base, migrations.MKG, "", role)
		if err != nil {
			return nil, err
		}
		p, err := db.Open(ctx, dsn, opts)
		if err != nil {
			return nil, err
		}
		pools[role] = p
		return p, nil
	}
	if _, err := pool(assembly.OpsRole); err != nil {
		return err
	}

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	setup, err := conn.Channel()
	if err != nil {
		return err
	}
	if err := outbox.DeclareTopology(setup); err != nil {
		return err
	}
	if err := assembly.DeclareQueues(setup); err != nil {
		return err
	}
	_ = setup.Close()

	errc := make(chan error, len(assembly.Consumers)+1)
	clk := clock.System{}
	for _, c := range assembly.Consumers {
		p, err := pool(c.Role)
		if err != nil {
			return err
		}
		ch, err := conn.Channel()
		if err != nil {
			return err
		}
		h, err := c.Handler(clk, pool)
		if err != nil {
			return err
		}
		go func(name string) { errc <- inbox.Consume(ctx, ch, p, name, h, log) }(c.Name)
	}
	log.Info("đang chạy", "event", "startup", "consumers", len(assembly.Consumers), "outcome", "ok")

	srv := &http.Server{Addr: env("AG_HEALTH_ADDR", ":8086"), Handler: healthRouter(log, pools, conn),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	select {
	case <-ctx.Done():
	case err := <-errc:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	return nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func healthRouter(log *slog.Logger, pools map[string]*pgxpool.Pool, conn *amqp.Connection) http.Handler {
	r := httpx.NewEngine(log, 1<<10)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "zone": "mkg"}) })
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		for _, p := range pools {
			if err := p.Ping(ctx); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db_unavailable"})
				return
			}
		}
		if conn.IsClosed() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "amqp_unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready", "zone": "mkg", "consumers": len(assembly.Consumers)})
	})
	return r
}

// healthcheck gọi url (địa chỉ loopback trong cấu hình healthcheck) và trả mã thoát.
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
