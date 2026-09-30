// Lệnh outbox-publisher: quét shared.outbox của mọi cụm và phát lên RabbitMQ
// (mục 12). Chạy bằng vai ag_outbox_publisher (BYPASSRLS, chỉ sửa được cột trạng thái).
//
//	AG_DB_URL             URL gốc tới PostgreSQL (host, cổng, sslmode)
//	AG_AMQP_URL           URL RabbitMQ của vhost nền tảng (sự kiện kind = platform, mọi cụm)
//	AG_AMQP_INTERNAL_URL  URL RabbitMQ của vhost mkg.internal (ADR-0018): sự kiện kind = internal
//	                      của cụm mkg. Thiếu → sự kiện nội bộ tồn trên outbox (metric backlog báo),
//	                      KHÔNG BAO GIỜ phát chúng lên vhost nền tảng.
//
// Khi khởi động, TRƯỚC khi phát thông điệp đầu tiên, lệnh khai báo mọi hàng đợi consumer
// của cụm pf (assembly.Queues: consumer của worker + optimizer) cùng DLQ và binding — sự
// kiện phát trước lần khởi động đầu của một consumer không bị mất (docs/runbooks/rabbitmq-queues.md).
//
// Tách theo vùng (G4 Kubernetes, RB-5): AG_CLUSTERS chọn cụm (mặc định mọi cụm, như Compose),
// AG_<CỤM>_DB_URL đặt URL gốc riêng từng cụm (cmd/internal/clusterenv). Bản vùng nền tảng:
// AG_CLUSTERS=pf; bản vùng mkg: AG_CLUSTERS=mkg,ts (+ AG_AMQP_INTERNAL_URL). Hàng đợi pf chỉ
// khai báo khi có cụm pf; vhost mkg.internal chỉ nối khi có cụm mkg.
//
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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/internal/clusterenv"
	internalassembly "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/worker-internal/assembly"

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
	clusters, err := clusterenv.Selected()
	if err != nil {
		return err
	}
	broker, err := outbox.DialRabbit(amqpURL)
	if err != nil {
		return err
	}
	defer func() { _ = broker.Close() }()
	// Hàng đợi consumer của cụm pf chỉ do bản publisher phụ trách cụm pf khai báo: bản của
	// vùng mkg (AG_CLUSTERS=mkg,ts) chỉ có quyền phát lên ag.events của vhost nền tảng (van).
	if clusterenv.Has(clusters, migrations.PF) {
		if err := declareQueues(broker); err != nil {
			return err
		}
		log.Info("đã khai báo hàng đợi consumer", "event", "startup", "queues", len(assembly.Queues()), "outcome", "ok")
	}

	// Vhost mkg.internal: kết nối và tài khoản riêng (ADR-0018).
	var internalBroker *outbox.Rabbit
	if u := os.Getenv("AG_AMQP_INTERNAL_URL"); u != "" && clusterenv.Has(clusters, migrations.MKG) {
		internalBroker, err = outbox.DialRabbit(u)
		if err != nil {
			return err
		}
		defer func() { _ = internalBroker.Close() }()
		if err := declareInternalQueues(internalBroker); err != nil {
			return err
		}
		log.Info("đã khai báo hàng đợi vhost mkg.internal", "event", "startup", "queues", len(internalassembly.Queues()), "outcome", "ok")
	} else if clusterenv.Has(clusters, migrations.MKG) {
		log.Warn("thiếu AG_AMQP_INTERNAL_URL — sự kiện nội bộ MKG tồn trên outbox cụm mkg", "event", "startup", "outcome", "degraded")
	}

	var wg sync.WaitGroup
	pools := map[string]*pgxpool.Pool{}
	for _, c := range clusters {
		dsn, err := dbadmin.RoleURL(clusterenv.BaseURL(c, base), c, "", dbadmin.OutboxPublisher)
		if err != nil {
			return err
		}
		pool, err := db.Open(ctx, dsn, db.OptionsFromEnv("outbox-publisher"))
		if err != nil {
			return err
		}
		defer pool.Close()
		pools[string(c)] = pool
		publishers := []*outbox.Publisher{{Pool: pool, Broker: broker, Batch: 100, Kind: outbox.KindPlatform}}
		if c == migrations.MKG && internalBroker != nil {
			publishers = append(publishers, &outbox.Publisher{Pool: pool, Broker: internalBroker, Batch: 100, Kind: outbox.KindInternal})
		}
		cluster := string(c)
		for _, p := range publishers {
			kind := string(p.Kind)
			wg.Add(1)
			go func(p *outbox.Publisher) {
				defer wg.Done()
				p.Run(ctx, 500*time.Millisecond, func(err error) {
					log.Warn("phát outbox lỗi", "event", "outbox.publish", "cluster", cluster, "kind", kind, "outcome", "retry", "error", err.Error())
				})
			}(p)
		}
	}
	if _, err := outbox.RegisterBacklogMetrics(pools); err != nil {
		log.Warn("không đăng ký được metric outbox", "event", "startup", "outcome", "error", "error", err.Error())
	}
	log.Info("đang chạy", "event", "startup", "outcome", "ok")
	wg.Wait()
	return nil
}

// declareQueues khai báo hàng đợi consumer trên một kênh riêng rồi đóng kênh.
func declareQueues(b *outbox.Rabbit) error {
	ch, err := b.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()
	return assembly.DeclareQueues(ch)
}

// declareInternalQueues khai báo hàng đợi consumer của worker-internal trên vhost mkg.internal.
func declareInternalQueues(b *outbox.Rabbit) error {
	ch, err := b.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()
	return internalassembly.DeclareQueues(ch)
}
