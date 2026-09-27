// Lệnh outbox-publisher: quét shared.outbox của mọi cụm và phát lên RabbitMQ
// (mục 12). Chạy bằng vai ag_outbox_publisher (BYPASSRLS, chỉ sửa được cột trạng thái).
//
//	AG_DB_URL      URL gốc tới PostgreSQL (host, cổng, sslmode)
//	AG_AMQP_URL    URL RabbitMQ
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
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
	base, amqpURL := os.Getenv("AG_DB_URL"), os.Getenv("AG_AMQP_URL")
	if base == "" || amqpURL == "" {
		return errors.New("thiếu AG_DB_URL hoặc AG_AMQP_URL")
	}
	broker, err := outbox.DialRabbit(amqpURL)
	if err != nil {
		return err
	}
	defer broker.Close()

	var wg sync.WaitGroup
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
	log.Info("đang chạy", "event", "startup", "outcome", "ok")
	wg.Wait()
	return nil
}
