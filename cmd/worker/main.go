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
//	idempotency  mỗi giờ: xóa Idempotency-Key hết hạn (R-51) và idem_key sổ quá 30 ngày (W2.1) ở mọi cụm
//	extension_escalation  mỗi 15 phút: leo thang nhiệm vụ khuyến nông quá SLA lên TTKN tỉnh (KN US-TASK-02)
//	reports_schedules     mỗi 15 phút: lịch gửi báo cáo định kỳ — mỗi mốc tạo đúng một việc xuất (KN US-DASH-03)
//	notify_email_digest   mỗi giờ: email tóm tắt chỉ đạo khẩn tới hộp thư công vụ (pf/notify, ADR-0015)
//	mechanization_area_feed_check  mỗi giờ: dữ liệu diện tích vụ App HTX quá ngưỡng độ tươi → báo Admin CGH (CGH FN-07)
//	metrics_snapshot      00:45 hằng ngày (sau anchor): ghi sổ chỉ số của hôm qua + ảnh chụp lớp GIS (pf/metrics, G3)
//	mrv_review_escalation mỗi 15 phút: hồ sơ MRV chờ duyệt quá hạn → báo người duyệt, rồi quản trị TTKN tỉnh (KN US-MRV-06)
//
// Khi có lịch, worker đăng ký hai gauge của ADR-0006 (gauges.go): ag.sync.conflict.overdue
// (xung đột đồng bộ quá SLA, theo tenant) và ag.db.standby_replication_lag (chỉ khi có
// AG_DB_STANDBY_URL).
//
// Mọi consumer ghi lần xử lý thất bại vào pf_sync.integration_failure (vai sync_app — màn
// giám sát đồng bộ, GIS US-SYNC-01); lần giao cuối trước DLQ phát pf.sync.integration.dead_lettered
// → pf/notify báo Admin nền tảng (GIS US-SYNC-02).
//
// Lệnh một lần (không chạy consumer/job): -metrics-backfill=YYYY-MM-DD:YYYY-MM-DD ghi sổ chỉ
// số + ảnh chụp lớp cho từng ngày đã kết thúc (-metrics-tenant chọn một tenant, mặc định mọi
// tenant có sự kiện; -metrics-keys chọn chỉ số). Dòng không đổi được giữ; khác → bản ghi bù.
//
// Consumer pf/notify (vai notify_app): pf.notify.directive, pf.notify.content,
// pf.notify.task, pf.notify.sync, pf.notify.party (dựng thông báo) và pf.notify.push
// (gửi thông điệp dữ liệu tới thiết bị).
//
// Consumer pf/reports (vai reports_app): pf.reports.export (dựng tệp Excel/PDF) và
// pf.reports.delivery (email lịch gửi báo cáo).
//
// Consumer liên module G2 (cmd/api-platform/assembly.Consumers, dựng toàn bộ module pf
// một lần qua assembly.Build, mỗi module một vai DB):
//
//	pf.mechanization.area_sync  pf.cropcycle.coop_season_area.changed → diện tích vụ CGH (CGH FN-07, QT-03)
//	pf.geo.hub_valve            mkg.hub.facility.published / mkg.hub.capacity.published → mô hình đọc hub (kiểm hợp đồng van)
//	pf.cropcycle.htx_collection_valve  mkg.fieldops.collection_schedule/htx_collection_statement.published → lịch gom, bảng kê HTX
//	pf.cropcycle.gps_log        vụ / nhật ký / sản lượng → log GPS pf/geo sau commit (HTX US-GPS-03)
//	pf.extension.gps_log        yêu cầu hỗ trợ → log GPS pf/geo sau commit (HTX US-GPS-03)
//	pf.simulation.snapshot      ảnh chụp đầu vào (SupplySource pf/cropcycle+party+geo, Router pf/waterway)
//
// Consumer pf.notify.cropcycle (vai notify_app): nhật ký chờ duyệt / kết quả duyệt / nông
// dân mở vụ (HTX US-LOG-05, US-SEASON-01).
// Consumer pf/waterway (vai waterway_app): pf.waterway.build (dựng phiên bản mạng
// đường thủy từ tập đoạn đã xác nhận).
//
// Biến môi trường:
//
//	AG_DB_URL, AG_AMQP_URL             như G0 (AG_AMQP_URL chỉ cần khi có consumer)
//	AG_DB_DIRECT_URL                   (tùy chọn) URL nối THẲNG PostgreSQL cho job + khóa leader;
//	                                   mặc định AG_DB_URL. Không trỏ vào PgBouncer.
//	AG_DB_PASSWORD_AG_OUTBOX_PUBLISHER vai của job vận hành (migration shared/000002)
//	AG_CLUSTERS                        (tùy chọn) cụm của job neo/dọn idempotency, mặc định mọi cụm;
//	                                   phải có pf (cmd/internal/clusterenv, G4 tách vùng)
//	AG_WORKER_JOBS                     mặc định cho -jobs: all | none | anchor,idempotency,extension_escalation,notify_email_digest
//	AG_DB_PASSWORD_EXTENSION_APP       (hoặc AG_DB_APP_PASSWORD) vai extension_app — khi có job extension_escalation
//	AG_DB_PASSWORD_SYNC_APP            vai sync_app — ghi lần xử lý thất bại của consumer (giám sát đồng bộ)
//	AG_DB_PASSWORD_MECHANIZATION_APP   vai mechanization_app — job mechanization_area_feed_check
//	AG_DB_PASSWORD_NOTIFY_APP          vai notify_app (cùng extension_app, masterdata_app, geo_app, accounts_app) — consumer/job pf/notify
//	AG_FCM_PROJECT_ID, AG_FCM_CREDENTIALS_FILE  FCM HTTP v1 (không đặt → kênh đẩy chỉ ghi log); AG_FCM_ENDPOINT, AG_FCM_TOKEN_URL tùy chọn
//	AG_SMTP_HOST, AG_SMTP_PORT, AG_SMTP_USERNAME, AG_SMTP_PASSWORD, AG_SMTP_FROM, AG_SMTP_DIGEST_TO  email tóm tắt (SMTP trong lãnh thổ)
//	AG_ANCHOR_SIGNING_KEY              Ed25519 (base64 seed 32 byte) — bắt buộc khi có job anchor
//	AG_ANCHOR_KEY_ID                   (tùy chọn) mã khóa ghi vào manifest
//	AG_S3_* hoặc AG_OBJSTORE_DIR       kho manifest, tệp báo cáo (shared/objstore)
//	AG_SMTP_HOST/PORT/USERNAME/PASSWORD/FROM/TLS  (tùy chọn) email lịch gửi báo cáo; thiếu → bỏ gửi kèm cảnh báo
//	AG_WEB_OPS_URL                     (tùy chọn) URL web-ops cho liên kết tải lại trong email
//	AG_REPORT_LINK_BASE                URL gốc công khai api-platform (vd https://api.<domain>) cho liên kết tải
//	                                   báo cáo Chi cục 7 ngày (A4); thiếu → email vượt giới hạn đính kèm ghi "Gửi lỗi"
//	AG_RABBITMQ_MGMT_URL               (tùy chọn) management API cho metric độ sâu DLQ
//	AG_KC_ADMIN_URL, AG_KC_REALM,      Keycloak Admin REST (tài khoản dịch vụ agrigreen-accounts) cho consumer
//	AG_KC_CLIENT_ID, AG_KC_CLIENT_SECRET  pf.accounts.org_status (khóa/mở tài khoản theo trạng thái tổ chức)
//	AG_DB_PASSWORD_ACCOUNTS_APP        (hoặc AG_DB_APP_PASSWORD) vai accounts_app của consumer đó
//	AG_OTLP_ENDPOINT, ...              OpenTelemetry (shared/telemetry)
//	AG_HEALTH_ADDR                     /healthz, /readyz (pool ping + kết nối RabbitMQ) — mặc định :8087, chỉ mạng nội bộ
//
// `worker -healthcheck <url>`: GET url, thoát 0 khi 200 — healthcheck Docker (distroless).
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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/internal/clusterenv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/internal/healthsrv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics"
	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	mrvport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	syncmod "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/leader"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/opsjobs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
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
	return append([]consumer{{
		name: assembly.AuditConsentConsumer, cluster: migrations.PF, role: "worker_platform",
		events: []string{port.EventConsentRecorded},
		handler: func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
			log.Info("đồng ý đã ghi", "event", env.EventType, "event_id", env.EventID, "tenant_id", env.TenantID, "trace_id", env.TraceID, "outcome", "ok")
			return nil
		},
	}, mrvExportConsumer(log), accountsOrgStatusConsumer(log), waterwayBuildConsumer()}, append(reportsConsumers(log), wiredConsumers(log)...)...)
}

func allConsumers(log *slog.Logger) []consumer {
	return append(consumers(log), notifyConsumers(log)...)
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "-healthcheck" {
		os.Exit(healthsrv.Healthcheck(os.Args[2]))
	}
	log := telemetry.NewLogger("worker", os.Stdout)
	mods := flag.String("modules", "all", "danh sách consumer, phân tách bằng dấu phẩy, all hoặc none")
	defJobs := os.Getenv("AG_WORKER_JOBS")
	if defJobs == "" {
		defJobs = "all"
	}
	jobs := flag.String("jobs", defJobs, "job vận hành: all, none hoặc anchor,idempotency")
	backfill := flag.String("metrics-backfill", "", "ghi sổ chỉ số + ảnh chụp lớp cho khoảng YYYY-MM-DD:YYYY-MM-DD rồi thoát")
	bfTenant := flag.String("metrics-tenant", "", "(với -metrics-backfill) chỉ một tenant")
	bfKeys := flag.String("metrics-keys", "", "(với -metrics-backfill) danh sách chỉ số, phân tách dấu phẩy")
	flag.Parse()
	if *backfill != "" {
		if err := runMetricsBackfill(log, *backfill, *bfTenant, *bfKeys); err != nil {
			log.Error("backfill chỉ số lỗi", "event", "metrics.backfill", "outcome", "error", "error", err.Error())
			os.Exit(1)
		}
		return
	}
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
	// RB-7: production chỉ được nối tới đích trong lãnh thổ (internal/shared/residency).
	if err := residency.CheckEnv(); err != nil {
		return err
	}
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
	hs := healthsrv.New("platform") // G4 (k8s probe): /healthz sống, /readyz pool + RabbitMQ
	if mods != "none" {
		n, closeFn, err := startConsumers(ctx, log, base, mods, errc, hs)
		if err != nil {
			return err
		}
		defer closeFn()
		started += n
		hs.Info("consumers", n)
	}
	if jobs != "none" {
		sched, closeFn, err := buildSchedule(ctx, log, jobs)
		if err != nil {
			return err
		}
		defer closeFn()
		lockPool := sched.pools[migrations.PF]
		direct := os.Getenv("AG_DB_DIRECT_URL")
		if direct == "" {
			direct = base
		}
		defer registerGauges(ctx, log, direct, lockPool)() // ADR-0006: xung đột quá SLA, độ trễ standby
		hs.Add("jobs_db", healthsrv.Pools(func() []*pgxpool.Pool {
			out := make([]*pgxpool.Pool, 0, len(sched.pools))
			for _, p := range sched.pools {
				out = append(out, p)
			}
			return out
		}))
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
	defer hs.Start("", ":8087", log, errc)()
	log.Info("đang chạy", "event", "startup", "consumers", mods, "jobs", jobs, "outcome", "ok")
	return <-errc
}

func startConsumers(ctx context.Context, log *slog.Logger, base, mods string, errc chan<- error, hs *healthsrv.Server) (int, func(), error) {
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
	// GIS US-SYNC-01/02: mọi lần xử lý thất bại ghi vào pf_sync.integration_failure (vai
	// sync_app, giao dịch riêng) cho màn giám sát đồng bộ; lần giao cuối (vào DLQ) phát
	// pf.sync.integration.dead_lettered → pf/notify báo Admin.
	recDSN, err := dbadmin.RoleURL(base, migrations.PF, "", syncmod.DBRole)
	if err != nil {
		closeFn()
		return 0, func() {}, err
	}
	recPool, err := db.Open(ctx, recDSN, db.Options{AppName: "worker-sync-monitor", MaxConns: 2})
	if err != nil {
		closeFn()
		return 0, func() {}, err
	}
	pools = append(pools, recPool)
	hs.Add("amqp", healthsrv.Closed(conn.IsClosed))
	hs.Add("db", healthsrv.Pools(func() []*pgxpool.Pool { return pools }))
	rec := syncmod.FailureRecorder(recPool, clock.System{})
	started := 0
	for _, c := range allConsumers(log) {
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
		go func(c consumer) { errc <- inbox.ConsumeWith(ctx, ch, pool, c.name, c.handler, log, rec) }(c)
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
	// Tách theo vùng (G4, RB-5): AG_CLUSTERS giới hạn cụm của job vận hành (neo, dọn
	// idempotency). Bản worker vùng nền tảng ở Kubernetes: AG_CLUSTERS=pf; cụm mkg/ts/vault
	// neo và dọn bằng CronJob `migrate anchor|purge-idempotency` của vùng đó. Cụm pf bắt buộc
	// (khóa leader và các job pf).
	clusters, err := clusterenv.Selected()
	if err != nil {
		return nil, func() {}, err
	}
	if !clusterenv.Has(clusters, migrations.PF) {
		return nil, func() {}, errors.New("worker: AG_CLUSTERS phải có cụm pf (khóa leader, job pf)")
	}
	out := &opsSchedule{pools: map[migrations.Cluster]*pgxpool.Pool{}}
	closeFn := func() {
		for _, p := range out.pools {
			p.Close()
		}
	}
	for _, c := range clusters {
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
				for _, c := range clusters {
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
				for _, c := range clusters {
					// Cả Idempotency-Key HTTP (48 giờ) và idem_key sổ quá 30 ngày (W2.1).
					r, err := opsjobs.PurgeAllIdempotency(ctx, out.pools[c], 5000)
					if err != nil {
						errs = append(errs, err)
						continue
					}
					log.Info("dọn idempotency", "event", "job.idempotency", "cluster", string(c),
						"deleted", r.HTTPKeys, "ledger_idem_deleted", r.LedgerIdem, "outcome", "ok")
				}
				return errors.Join(errs...)
			},
		})
	}
	if selected(jobs, extension.JobEscalation) || selected(jobs, extension.JobWorkSchedule) {
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
		if selected(jobs, extension.JobEscalation) {
			js = append(js, kn.Jobs(extension.OverdueTenants(out.pools[migrations.PF]), log)...)
		}
		if selected(jobs, extension.JobWorkSchedule) {
			// KN v7.7 P1 → P5: người nhận nhắc / người duyệt lấy từ sổ gán vai trò (phòng ban × tỉnh × cấp quản
			// lý) bằng vai accounts_app; thiếu → danh bạ trực pf_extension.staff như trước.
			accPool, err := openRole(accounts.DBRole)
			if err != nil {
				closeFn()
				return nil, func() {}, err
			}
			acc := accounts.New(accPool, clk, accounts.Deps{Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Departments: pty.Departments(), Units: md.AdminUnits()})
			kn.UseKNContext(acc.ExtensionKNContext(), acc.ExtensionKNDirectory())
			// KN v7.7 P5: khóa tuần đã kết thúc, nhắc hạn đăng ký / hạn duyệt lịch công tác.
			js = append(js, kn.ScheduleJobs(extension.ScheduleTenants(out.pools[migrations.PF]), log)...)
		}
	}
	if selected(jobs, notifyport.JobEmailDigest) && os.Getenv("AG_SMTP_HOST") != "" {
		// Email tóm tắt chỉ chạy khi đã cấu hình SMTP; tìm tenant bằng ag_outbox_publisher (2 cột).
		var kept []*pgxpool.Pool
		nt, err := buildNotify(ctx, log, direct, func(p *pgxpool.Pool) { kept = append(kept, p) })
		prevClose := closeFn
		closeFn = func() {
			for _, p := range kept {
				p.Close()
			}
			prevClose()
		}
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		js = append(js, nt.Jobs(notify.DigestTenants(out.pools[migrations.PF]), log)...)
	}
	if selected(jobs, reports.JobSchedules) {
		// Lịch gửi báo cáo: tìm tenant bằng pool ag_outbox_publisher (hai cột), chạy bằng vai reports_app;
		// tham số hạn mức DLP đọc qua pf/masterdata bằng vai của nó (R-05).
		repPool, err := openPF(ctx, direct, reports.DBRole, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		mdPool, err := openPF(ctx, direct, masterdata.DBRole, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		md := masterdata.New(mdPool, clk)
		rep := reports.New(repPool, clk, reports.Deps{Params: md.Params(), Log: log})
		// G5 (ADR-0024): lối "lịch" của xuất báo cáo qua điểm kiểm soát chung (kênh report_export) —
		// Checkpoint của pf/metering (vai metering_app); workerEgress ghi đè cho kiểm thử.
		// AG_EGRESS_MODE sai → lỗi khởi động.
		mode, err := assembly.EgressModeFromEnv(os.Getenv)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		egress := workerEgress
		if egress == nil {
			meterPool, err := openPF(ctx, direct, metering.DBRole, &closeFn)
			if err != nil {
				closeFn()
				return nil, func() {}, err
			}
			egress = metering.New(meterPool, clk, metering.Deps{Params: md.Params(), Mode: mode}).Checkpoint()
		}
		rep.UseEgress(egress, mode)
		js = append(js, rep.Jobs(reports.ScheduleTenants(out.pools[migrations.PF]), log)...)
	}
	if selected(jobs, mechport.JobAreaFeedCheck) {
		// Độ tươi dữ liệu App HTX (CGH FN-07 AC-02/AC-03): tìm tenant bằng ag_outbox_publisher
		// (hai cột), ghi cảnh báo bằng vai mechanization_app; ngưỡng đọc qua pf/masterdata (R-05).
		mechPool, err := openPF(ctx, direct, mechanization.DBRole, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		mdPool, err := openPF(ctx, direct, masterdata.DBRole, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		mech := mechanization.New(mechPool, clk, mechanization.Deps{Params: masterdata.New(mdPool, clk).Params()})
		js = append(js, mech.Jobs(mechanization.FeedTenants(out.pools[migrations.PF]), log)...)
	}
	if selected(jobs, metricsport.JobMetricsSnapshot) {
		// Chỉ số có lịch sử + ảnh chụp lớp GIS: dựng module pf qua assembly.Build (mỗi nguồn một vai DB,
		// R-05); tìm tenant bằng pool ag_outbox_publisher (neo chuỗi của ngày, dự phòng outbox).
		mods, mpools, err := buildPlatform(ctx, direct)
		prevClose := closeFn
		closeFn = func() {
			for _, p := range mpools {
				p.Close()
			}
			prevClose()
		}
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		js = append(js, mods.Metrics.Jobs(metrics.JobTenants(out.pools[migrations.PF]), log)...)
	}
	if selected(jobs, mrvport.JobReviewEscalator) {
		// Leo thang hồ sơ MRV quá hạn (KN US-MRV-06): tìm tenant bằng ag_outbox_publisher (hai cột),
		// ghi cảnh báo + sự kiện bằng vai mrv_app; SLA đọc qua pf/masterdata bằng vai của nó (R-05).
		mrvPool, err := openPF(ctx, direct, mrv.DBRole, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		mdPool, err := openPF(ctx, direct, masterdata.DBRole, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		mrvMod := mrv.New(mrvPool, clk, mrv.Deps{Params: masterdata.New(mdPool, clk).Params()})
		js = append(js, mrvMod.Jobs(mrv.PendingTenants(out.pools[migrations.PF]), log)...)
	}
	if selected(jobs, extension.JobReportCycle) {
		// KN v7.7 P4 — vòng đời báo cáo (tự chốt kỳ + tổng hợp "Hệ thống", chốt lại sau hạn mở, tự nhắc): cần engine mẫu
		// (pf/reports), khu vực (pf/party), gán vai trò (pf/accounts) → dựng module pf qua assembly.Build (mỗi module một
		// vai DB, R-05); tìm tenant bằng ag_outbox_publisher (hai cột của report_period).
		mods, mpools, err := buildPlatform(ctx, direct)
		prevClose := closeFn
		closeFn = func() {
			for _, p := range mpools {
				p.Close()
			}
			prevClose()
		}
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		js = append(js, mods.Extension.ReportCycleJobs(extension.ReportCycleTenants(out.pools[migrations.PF]), log)...)
	}
	if selected(jobs, apiclientport.JobKeyExpiry) && os.Getenv(apiclientport.EnvOperatorTenant) != "" {
		// G5 (ADR-0022 mục 4, RB-1): khóa API quá hạn → sự kiện expired ở sổ api_key_event của tenant đơn vị
		// vận hành (AG_PUBLIC_OPERATOR_TENANT; không đặt — ĐTK 1 — thì không có job), vai apiclient_app.
		mod, err := apiclientKeyJobs(ctx, direct, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		js = append(js, mod.Jobs(log)...)
	}
	if len(js) == 0 {
		closeFn()
		return nil, func() {}, errors.New("-jobs không chọn job hợp lệ nào (anchor, idempotency, " + extension.JobEscalation + ", " + extension.JobWorkSchedule + ", " +
			notifyport.JobEmailDigest + ", " + reports.JobSchedules + ", " + mechport.JobAreaFeedCheck + ", " + metricsport.JobMetricsSnapshot +
			", " + mrvport.JobReviewEscalator + ", " + extension.JobReportCycle +
			", " + apiclientport.JobKeyExpiry + ")")
	}
	out.s = &schedule.Scheduler{Clock: clk, Log: log, Jobs: js}
	return out, closeFn, nil
}

// openPF mở pool nối thẳng cụm pf cho một vai của job và đăng ký đóng pool.
func openPF(ctx context.Context, direct, role string, closeFn *func()) (*pgxpool.Pool, error) {
	d, err := dbadmin.RoleURL(direct, migrations.PF, "", role)
	if err != nil {
		return nil, err
	}
	p, err := db.Open(ctx, d, db.Options{AppName: "worker-" + role, MaxConns: 2})
	if err != nil {
		return nil, err
	}
	prev := *closeFn
	*closeFn = func() { p.Close(); prev() }
	return p, nil
}
