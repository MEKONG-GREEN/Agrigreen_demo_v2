// Lệnh worker: consumer RabbitMQ (chọn bằng -modules, mục 6) và lịch job vận
// hành (chọn bằng -jobs). Mỗi consumer có quorum queue riêng + DLQ; xử lý
// idempotent qua shared.inbox.
//
// G0 có consumer mẫu "pf.audit.consent" ghi nhận sự kiện đồng ý để kiểm chứng
// toàn tuyến outbox → hàng đợi → inbox. Consumer nghiệp vụ thật thêm từ G1.
//
// Lịch vận hành (giờ Asia/Ho_Chi_Minh) chỉ chạy ở MỘT bản worker — leader giữ
// pg_try_advisory_lock trên kết nối riêng (shared/leader):
//
//	anchor       00:30 hằng ngày: neo chuỗi mọi cụm + manifest ký số (mục 8.2)
//	idempotency  mỗi giờ: xóa Idempotency-Key hết hạn ở mọi cụm (R-51)
//	extension_escalation  mỗi 15 phút: leo thang nhiệm vụ khuyến nông quá SLA lên TTKN tỉnh (KN US-TASK-02)
//
// Biến môi trường:
//
//	AG_DB_URL, AG_AMQP_URL             như G0 (AG_AMQP_URL chỉ cần khi có consumer)
//	AG_DB_DIRECT_URL                   (tùy chọn) URL nối THẲNG PostgreSQL cho job + khóa leader;
//	                                   mặc định AG_DB_URL. Không trỏ vào PgBouncer.
//	AG_DB_PASSWORD_AG_OUTBOX_PUBLISHER vai của job vận hành (migration shared/000002)
//	AG_WORKER_JOBS                     mặc định cho -jobs: all | none | anchor,idempotency,extension_escalation
//	AG_DB_PASSWORD_EXTENSION_APP       (hoặc AG_DB_APP_PASSWORD) vai extension_app — khi có job extension_escalation
//	AG_ANCHOR_SIGNING_KEY              Ed25519 (base64 seed 32 byte) — bắt buộc khi có job anchor
//	AG_ANCHOR_KEY_ID                   (tùy chọn) mã khóa ghi vào manifest
//	AG_S3_* hoặc AG_OBJSTORE_DIR       kho manifest (shared/objstore)
//	AG_RABBITMQ_MGMT_URL               (tùy chọn) management API cho metric độ sâu DLQ
//	AG_OTLP_ENDPOINT, ...              OpenTelemetry (shared/telemetry)
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
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/leader"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/opsjobs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
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
	}, mrvExportConsumer(log)}
}

func main() {
	log := telemetry.NewLogger("worker", os.Stdout)
	mods := flag.String("modules", "all", "danh sách consumer, phân tách bằng dấu phẩy, all hoặc none")
	defJobs := os.Getenv("AG_WORKER_JOBS")
	if defJobs == "" {
		defJobs = "all"
	}
	jobs := flag.String("jobs", defJobs, "job vận hành: all, none hoặc anchor,idempotency")
	flag.Parse()
	if err := run(log, *mods, *jobs); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("dừng do lỗi", "event", "startup", "outcome", "error", "error", err.Error())
		os.Exit(1)
	}
}

func selected(list, name string) bool {
	return list == "all" || strings.Contains(","+list+",", ","+name+",")
}

func run(log *slog.Logger, mods, jobs string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdown, err := telemetry.Setup(ctx, "worker")
	if err != nil {
		return err
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = shutdown(sctx)
	}()
	base := os.Getenv("AG_DB_URL")
	if base == "" {
		return errors.New("thiếu AG_DB_URL")
	}

	errc := make(chan error, 8)
	started := 0
	if mods != "none" {
		n, closeFn, err := startConsumers(ctx, log, base, mods, errc)
		if err != nil {
			return err
		}
		defer closeFn()
		started += n
	}
	if jobs != "none" {
		sched, closeFn, err := buildSchedule(ctx, log, jobs)
		if err != nil {
			return err
		}
		defer closeFn()
		lockPool := sched.pools[migrations.PF]
		started++
		go func() {
			errc <- leader.Run(ctx, lockPool, leader.LockSchedules, 15*time.Second, leader.Options{}, log, sched.s.Run)
		}()
	}
	if started == 0 {
		return errors.New("không có consumer hay job nào được chọn")
	}
	if _, err := inbox.RegisterDLQMetrics(nil, os.Getenv("AG_RABBITMQ_MGMT_URL")); err != nil {
		log.Warn("không đăng ký được metric DLQ", "event", "startup", "outcome", "error", "error", err.Error())
	}
	log.Info("đang chạy", "event", "startup", "consumers", mods, "jobs", jobs, "outcome", "ok")
	return <-errc
}

func startConsumers(ctx context.Context, log *slog.Logger, base, mods string, errc chan<- error) (int, func(), error) {
	amqpURL := os.Getenv("AG_AMQP_URL")
	if amqpURL == "" {
		return 0, func() {}, errors.New("thiếu AG_AMQP_URL (hoặc chạy -modules=none)")
	}
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return 0, func() {}, err
	}
	var pools []*pgxpool.Pool
	closeFn := func() {
		for _, p := range pools {
			p.Close()
		}
		_ = conn.Close()
	}
	started := 0
	for _, c := range consumers(log) {
		if !selected(mods, c.name) {
			continue
		}
		dsn, err := dbadmin.RoleURL(base, c.cluster, "", c.role)
		if err != nil {
			closeFn()
			return 0, func() {}, err
		}
		pool, err := db.Open(ctx, dsn, db.OptionsFromEnv("worker"))
		if err != nil {
			closeFn()
			return 0, func() {}, err
		}
		pools = append(pools, pool)
		ch, err := conn.Channel()
		if err != nil {
			closeFn()
			return 0, func() {}, err
		}
		if err := inbox.DeclareQueue(ch, c.name, c.events); err != nil {
			closeFn()
			return 0, func() {}, err
		}
		started++
		go func(c consumer) { errc <- inbox.Consume(ctx, ch, pool, c.name, c.handler, log) }(c)
	}
	return started, closeFn, nil
}

type opsSchedule struct {
	s     *schedule.Scheduler
	pools map[migrations.Cluster]*pgxpool.Pool
}

// buildSchedule mở pool ag_outbox_publisher nối thẳng tới mọi cụm và dựng lịch.
// Thiếu cấu hình của job đã chọn → lỗi khởi động (không âm thầm bỏ neo chuỗi).
func buildSchedule(ctx context.Context, log *slog.Logger, jobs string) (*opsSchedule, func(), error) {
	direct := os.Getenv("AG_DB_DIRECT_URL")
	if direct == "" {
		direct = os.Getenv("AG_DB_URL")
	}
	out := &opsSchedule{pools: map[migrations.Cluster]*pgxpool.Pool{}}
	closeFn := func() {
		for _, p := range out.pools {
			p.Close()
		}
	}
	for _, c := range migrations.Clusters {
		dsn, err := dbadmin.RoleURL(direct, c, "", dbadmin.OutboxPublisher)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		// Không dùng AG_DB_VIA_PGBOUNCER: khóa leader là khóa mức phiên.
		pool, err := db.Open(ctx, dsn, db.Options{AppName: "worker-jobs", MaxConns: 4})
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		out.pools[c] = pool
	}
	clk := clock.System{}
	var js []schedule.Job
	if selected(jobs, "anchor") {
		signer, err := opsjobs.SignerFromEnv()
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		store, err := objstore.FromEnv(ctx)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		log.Info("khóa ký neo", "event", "startup", "key_id", signer.KeyID, "outcome", "ok")
		js = append(js, schedule.Job{
			Name: "anchor", Spec: schedule.Daily{Hour: 0, Minute: 30}, RunOnStart: true, Timeout: time.Hour,
			Run: func(ctx context.Context) error {
				day := opsjobs.AnchorDateFor(clk.Now())
				var errs []error
				for _, c := range migrations.Clusters {
					r, err := opsjobs.AnchorDaily(ctx, out.pools[c], store, signer, string(c), day)
					if err != nil {
						errs = append(errs, err)
						continue
					}
					log.Info("neo chuỗi", "event", "job.anchor", "cluster", string(c), "business_date", day.String(),
						"inserted", r.Inserted, "entries", r.Entries, "skipped", r.Skipped, "object_key", r.Key, "outcome", "ok")
				}
				return errors.Join(errs...)
			},
		})
	}
	if selected(jobs, "idempotency") {
		js = append(js, schedule.Job{
			Name: "idempotency", Spec: schedule.Every{Interval: time.Hour}, RunOnStart: true, Timeout: 15 * time.Minute,
			Run: func(ctx context.Context) error {
				var errs []error
				for _, c := range migrations.Clusters {
					n, err := opsjobs.PurgeIdempotency(ctx, out.pools[c], 5000)
					if err != nil {
						errs = append(errs, err)
						continue
					}
					log.Info("dọn idempotency", "event", "job.idempotency", "cluster", string(c), "deleted", n, "outcome", "ok")
				}
				return errors.Join(errs...)
			},
		})
	}
	if selected(jobs, extension.JobEscalation) {
		// Leo thang SLA nhiệm vụ khuyến nông: tìm tenant bằng pool ag_outbox_publisher
		// (chỉ đọc 4 cột), ghi bằng vai extension_app của module (R-05).
		openRole := func(role string) (*pgxpool.Pool, error) {
			d, err := dbadmin.RoleURL(direct, migrations.PF, "", role)
			if err != nil {
				return nil, err
			}
			p, err := db.Open(ctx, d, db.Options{AppName: "worker-extension", MaxConns: 2})
			if err != nil {
				return nil, err
			}
			prevClose := closeFn
			closeFn = func() { p.Close(); prevClose() }
			return p, nil
		}
		extPool, err := openRole(extension.DBRole)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		// Leo thang cần cây tổ chức (tìm TTKN tỉnh) và đơn vị hành chính (tỉnh của xã):
		// dựng port của party/masterdata bằng vai riêng của từng module (R-05).
		mdPool, err := openRole(masterdata.DBRole)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		partyPool, err := openRole(party.DBRole)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		md := masterdata.New(mdPool, clk)
		pty := party.New(partyPool, clk, party.Deps{AdminUnits: md.AdminUnits()})
		kn := extension.New(extPool, clk, extension.Deps{
			Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Units: md.AdminUnits(), Params: md.Params(), Catalogs: md.Catalogs(),
		})
		js = append(js, kn.Jobs(extension.OverdueTenants(out.pools[migrations.PF]), log)...)
	}
	if len(js) == 0 {
		closeFn()
		return nil, func() {}, errors.New("-jobs không chọn job hợp lệ nào (anchor, idempotency, " + extension.JobEscalation + ")")
	}
	out.s = &schedule.Scheduler{Clock: clk, Log: log, Jobs: js}
	return out, closeFn, nil
}
