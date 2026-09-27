// Lệnh worker: consumer RabbitMQ, chọn module bằng cờ -modules (mục 6).
// Mỗi consumer có quorum queue riêng + DLQ; xử lý idempotent qua shared.inbox.
//
// G0 có consumer mẫu "pf.audit.consent" ghi nhận sự kiện đồng ý để kiểm chứng
// toàn tuyến outbox → hàng đợi → inbox. Consumer nghiệp vụ thật thêm từ G1.
//
//	AG_DB_URL, AG_AMQP_URL
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type consumer struct {
	name    string
	cluster migrations.Cluster
	role    string
	events  []string
	handler inbox.Handler
}

func consumers(log *slog.Logger) []consumer {
	return []consumer{{
		name: "pf.audit.consent", cluster: migrations.PF, role: "worker_platform",
		events: []string{port.EventConsentRecorded},
		handler: func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
			log.Info("đồng ý đã ghi", "event", env.EventType, "event_id", env.EventID, "tenant_id", env.TenantID, "trace_id", env.TraceID, "outcome", "ok")
			return nil
		},
	}}
}

func main() {
	log := telemetry.NewLogger("worker", os.Stdout)
	mods := flag.String("modules", "all", "danh sách consumer, phân tách bằng dấu phẩy, hoặc all")
	flag.Parse()
	if err := run(log, *mods); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("dừng do lỗi", "event", "startup", "outcome", "error", "error", err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger, mods string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	base, amqpURL := os.Getenv("AG_DB_URL"), os.Getenv("AG_AMQP_URL")
	if base == "" || amqpURL == "" {
		return errors.New("thiếu AG_DB_URL hoặc AG_AMQP_URL")
	}
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return err
	}
	defer conn.Close()

	errc := make(chan error, 8)
	started := 0
	for _, c := range consumers(log) {
		if mods != "all" && !strings.Contains(","+mods+",", ","+c.name+",") {
			continue
		}
		dsn, err := dbadmin.RoleURL(base, c.cluster, "", c.role)
		if err != nil {
			return err
		}
		pool, err := db.Open(ctx, dsn, db.OptionsFromEnv("worker"))
		if err != nil {
			return err
		}
		defer pool.Close()
		ch, err := conn.Channel()
		if err != nil {
			return err
		}
		if err := inbox.DeclareQueue(ch, c.name, c.events); err != nil {
			return err
		}
		started++
		go func(c consumer) { errc <- inbox.Consume(ctx, ch, pool, c.name, c.handler, log) }(c)
	}
	if started == 0 {
		return errors.New("không có consumer nào được chọn")
	}
	log.Info("đang chạy", "event", "startup", "consumers", started, "outcome", "ok")
	return <-errc
}
