// Lệnh outbox-publisher: quét shared.outbox của mọi cụm và phát lên RabbitMQ
// (mục 12). Chạy bằng vai ag_outbox_publisher (BYPASSRLS, chỉ sửa được cột trạng thái).
//
//	AG_DB_URL      URL gốc tới PostgreSQL (host, cổng, sslmode)
//	AG_AMQP_URL    URL RabbitMQ
//	AG_OTLP_ENDPOINT, ...  OpenTelemetry: span publish nối trace của request gốc; metric
//	               ag.outbox.backlog, ag.outbox.oldest_age theo cụm (mục 19)
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func main() {
	log := telemetry.NewLogger("outbox-publisher", os.Stdout)
	if err := run(); err != nil {
		log.Error("dừng do lỗi", "event", "startup", "outcome", "error", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	log := telemetry.NewLogger("outbox-publisher", os.Stdout)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// RB-7: production chỉ được nối tới đích trong lãnh thổ (internal/shared/residency).
	if err := residency.CheckEnv(); err != nil {
		return err
	}
	shutdown, err := telemetry.Setup(ctx, "outbox-publisher")
	if err != nil {
		return err
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = shutdown(sctx)
	}()
	base, amqpURL := os.Getenv("AG_DB_URL"), os.Getenv("AG_AMQP_URL")
	if base == "" || amqpURL == "" {
		return errors.New("thiếu AG_DB_URL hoặc AG_AMQP_URL")
	}
	broker, err := outbox.DialRabbit(amqpURL)
	if err != nil {
		return err
	}
	defer func() { _ = broker.Close() }()

	var wg sync.WaitGroup
	pools := map[string]*pgxpool.Pool{}
	for _, c := range migrations.Clusters {
		dsn, err := dbadmin.RoleURL(base, c, "", dbadmin.OutboxPublisher)
		if err != nil {
			return err
		}
		pool, err := db.Open(ctx, dsn, db.OptionsFromEnv("outbox-publisher"))
		if err != nil {
			return err
		}
		defer pool.Close()
		pools[string(c)] = pool
		p := &outbox.Publisher{Pool: pool, Broker: broker, Batch: 100}
		cluster := string(c)
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Run(ctx, 500*time.Millisecond, func(err error) {
				log.Warn("phát outbox lỗi", "event", "outbox.publish", "cluster", cluster, "outcome", "retry", "error", err.Error())
			})
		}()
	}
	if _, err := outbox.RegisterBacklogMetrics(pools); err != nil {
		log.Warn("không đăng ký được metric outbox", "event", "startup", "outcome", "error", "error", err.Error())
	}
	log.Info("đang chạy", "event", "startup", "outcome", "ok")
	wg.Wait()
	return nil
}
