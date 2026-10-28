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
//	price_calendar        mỗi phút: mốc khóa nhập giá 12:00 (±1 phút) + nhắc nhập giá lần 1/2 (pf/price, Báo giá FN-AD-04.04, FN-XA-03.04)
//
// Khi có lịch, worker đăng ký các gauge (gauges.go) — ADR-0006: ag.sync.conflict.overdue
// (xung đột đồng bộ quá SLA, theo tenant), ag.db.standby_replication_lag (chỉ khi có
// AG_DB_STANDBY_URL) và ag.db.ledger_default_rows (phân vùng DEFAULT có dòng, shared 000010).
//
// Mọi consumer ghi lần xử lý thất bại vào pf_sync.integration_failure (r_sync — màn
// giám sát đồng bộ, GIS US-SYNC-01); lần giao cuối trước DLQ phát pf.sync.integration.dead_lettered
// → pf/notify báo Admin nền tảng (GIS US-SYNC-02).
//
// Consumer pf.kref (W2.5, ADR-0029; vai đăng nhập app_worker → SET LOCAL ROLE r_kref): dựng bản chiếu k_ref từ
// pf.party.org.created/moved/deactivated/reactivated, pf.masterdata.admin_unit.changed, pf.party.share_grant.*;
// chỉ số ag.k_ref.lag. Lệnh một lần -kref-rebuild dựng lại k_ref mọi tenant từ nguồn (app_worker →
// r_party, r_masterdata, r_kref; ag_ops_jobs tìm tenant — AG_DB_PASSWORD_APP_WORKER hoặc AG_DB_APP_PASSWORD).
//
// Lệnh một lần (không chạy consumer/job): -metrics-backfill=YYYY-MM-DD:YYYY-MM-DD ghi sổ chỉ
// số + ảnh chụp lớp cho từng ngày đã kết thúc (-metrics-tenant chọn một tenant, mặc định mọi
// tenant có sự kiện; -metrics-keys chọn chỉ số). Dòng không đổi được giữ; khác → bản ghi bù.
//
// Consumer pf/notify (r_notify): pf.notify.directive, pf.notify.content,
// pf.notify.task, pf.notify.sync, pf.notify.party (dựng thông báo) và pf.notify.push
// (gửi thông điệp dữ liệu tới thiết bị).
//
// Consumer pf/reports (r_reports): pf.reports.export (dựng tệp Excel/PDF) và
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
// Consumer pf.notify.cropcycle (r_notify): nhật ký chờ duyệt / kết quả duyệt / nông
// dân mở vụ (HTX US-LOG-05, US-SEASON-01).
// Consumer pf/waterway (r_waterway): pf.waterway.build (dựng phiên bản mạng
// đường thủy từ tập đoạn đã xác nhận).
//
// Biến môi trường:
//
//	AG_DB_URL, AG_AMQP_URL             như G0 (AG_AMQP_URL chỉ cần khi có consumer)
//	AG_DB_DIRECT_URL                   (tùy chọn) URL nối THẲNG PostgreSQL cho job + khóa leader;
//	                                   mặc định AG_DB_URL. Không trỏ vào PgBouncer.
//	AG_DB_REPLICA_URL                  (tùy chọn) URL gốc bản sao chỉ đọc cụm pf: job metrics_snapshot và
//	                                   -metrics-backfill đọc view _pub trên đó khi đã phát lại tới cutoff (W6.3)
//	AG_DB_PASSWORD_AG_OPS_JOBS         vai của job vận hành ag_ops_jobs (migration shared/000002, 000009)
//	AG_CLUSTERS                        (tùy chọn) cụm của job neo/dọn idempotency, mặc định mọi cụm;
//	                                   phải có pf (cmd/internal/clusterenv, G4 tách vùng)
//	AG_WORKER_JOBS                     mặc định cho -jobs: all | none | anchor,idempotency,extension_escalation,notify_email_digest
//	AG_DB_PASSWORD_APP_WORKER          (hoặc AG_DB_APP_PASSWORD) vai đăng nhập app_worker (W2.6, ADR-0028) — MỘT pool cho
//	                                   mọi consumer và một pool nối thẳng cho job; mỗi module chạy SET LOCAL ROLE r_<module>
//	AG_DB_MAX_CONNS                    (tùy chọn) cỡ pool vai đơn vị, mặc định 20
//	AG_DB_PASSWORD_APP_AGG             (chỉ -agg-run) vai đăng nhập riêng app_agg của job tổng hợp (ADR-0031 §3, gói FC) —
//	                                   pool 2 kết nối, trên AG_DB_REPLICA_URL khi có; AG_AGG_GOVERNANCE_TENANT: tenant quản trị
//	AG_FCM_PROJECT_ID, AG_FCM_CREDENTIALS_FILE  FCM HTTP v1 (không đặt → kênh đẩy TẮT: bản gửi "dropped", không "sent" —
//	                                   P0-NOTIFY-FAILCLOSED); AG_FCM_ENDPOINT, AG_FCM_TOKEN_URL tùy chọn
//	AG_MAILER, AG_PUSHER               rỗng (tự chọn) | smtp/fcm | disabled | log (CHỈ AG_ENV=development|dev|local|test)
//	AG_SMTP_HOST, AG_SMTP_PORT, AG_SMTP_USERNAME, AG_SMTP_PASSWORD, AG_SMTP_FROM  email (SMTP trong lãnh thổ)
//	AG_KN_DIGEST_RECIPIENTS_JSON  hộp thư công vụ theo tenant KN; thiếu ánh xạ giữ nguyên hàng chờ, không dùng AG_SMTP_DIGEST_TO
//	AG_ANCHOR_SIGNING_KEY              Ed25519 (base64 seed 32 byte) — bắt buộc khi có job anchor
//	AG_ANCHOR_KEY_ID                   (tùy chọn) mã khóa ghi vào manifest
//	AG_S3_* hoặc AG_OBJSTORE_DIR       kho manifest, tệp báo cáo (shared/objstore)
//	AG_SMTP_HOST/PORT/USERNAME/PASSWORD/FROM/TLS  (tùy chọn) email lịch gửi báo cáo; thiếu → bỏ gửi kèm cảnh báo
//	AG_WEB_OPS_URL                     URL web-ops cho liên kết trong email; ngoài máy phát triển thiếu/localhost → email
//	                                   thông báo theo người nhận TẮT, email báo cáo bỏ dòng tải lại
//	AG_REPORT_LINK_BASE                URL gốc công khai api-platform (vd https://api.<domain>) cho liên kết tải
//	                                   báo cáo Chi cục 7 ngày (A4); thiếu → email vượt giới hạn đính kèm ghi "Gửi lỗi"
//	AG_REPORT_DISPATCH_LEASE           (tùy chọn) hạn nhận việc gửi báo cáo Chi cục, 3m–24h, mặc định 15m (P0-WORKER-REPORTS):
//	                                   quá hạn khi còn "Đang gửi" → "Gửi lại" tiếp tục cùng lượt, không gửi lại người đã nhận
//	AG_RABBITMQ_MGMT_URL               (tùy chọn) management API cho metric độ sâu DLQ
//	AG_INTEGRATION_CONTRIBUTORS        (W5.3) tenant đóng góp tập tổng hợp của feed cổng tích hợp (job apiclient_feed_build);
//	AG_INTEGRATION_BUILD_INTERVAL      chu kỳ dựng (mặc định 15m); job apiclient_feed_retention dọn feed quá 30 ngày
//	AG_KC_ADMIN_URL, AG_KC_REALM,      Keycloak Admin REST (tài khoản dịch vụ agrigreen-accounts) cho consumer
//	AG_KC_CLIENT_ID, AG_KC_CLIENT_SECRET  pf.accounts.org_status (khóa/mở tài khoản theo trạng thái tổ chức)
//	AG_OTLP_ENDPOINT, ...              OpenTelemetry (shared/telemetry)
//	AG_HEALTH_ADDR                     /healthz, /readyz (pool ping + kết nối RabbitMQ) — mặc định :8087, chỉ mạng nội bộ
//
// RabbitMQ mất kết nối → worker không thoát: kết nối lại có độ lùi (1s·2^n, trần 30s), khai báo lại hàng đợi và đăng ký
// lại mọi consumer (amqp_reconnect.go, P0-WORKER-REPORTS #128).
//
// `worker -healthcheck <url>`: GET url, thoát 0 khi 200 — healthcheck Docker (distroless).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	reportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/internal/clusterenv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/internal/healthsrv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics"
	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	mrvport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type consumer struct {
	name    string
	cluster migrations.Cluster
	// module (ADR-0028, W2.6): giao dịch inbox của consumer chạy SET LOCAL ROLE r_<module> (db.ForModule) trên
	// pool vai đăng nhập app_worker; db.Ops cho consumer không thuộc module (r_ops).
	module  db.ModuleID
	events  []string
	handler inbox.Handler
	// prepare (tùy chọn, P0-WORKER-REPORTS #128) dựng + kiểm phụ thuộc của handler lúc khởi động; lỗi → worker không chạy.
	prepare func(ctx context.Context) error
}

func consumers(log *slog.Logger) []consumer {
	return append([]consumer{{
		name: assembly.AuditConsentConsumer, cluster: migrations.PF, module: db.Ops,
		events: []string{port.EventConsentRecorded},
		handler: func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
			log.Info("đồng ý đã ghi", "event", env.EventType, "event_id", env.EventID, "tenant_id", env.TenantID, "trace_id", env.TraceID, "outcome", "ok")
			return nil
		},
	}, mrvExportConsumer(log), accountsOrgStatusConsumer(log), waterwayBuildConsumer(), krefConsumer()}, append(reportsConsumers(log), wiredConsumers(log)...)...)
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
	krefRebuild := flag.Bool("kref-rebuild", false, "dựng lại k_ref của mọi tenant từ nguồn rồi thoát (W2.5)")
	aggRun := flag.String("agg-run", "", "chạy một job tổng hợp đã khai và đã duyệt (vai đăng nhập app_agg, ADR-0031 §3) rồi thoát")
	aggActor := flag.String("agg-actor", "", "(với -agg-run) người chạy (uuid) ghi vào nhật ký pf_metrics.agg_run")
	aggK := flag.Int("agg-k", 0, "(với -agg-run) ngưỡng k; 0 = max(k job, k sàn publicdata.k_min)")
	flag.Parse()
	if *aggRun != "" {
		if err := runAggJob(log, *aggRun, *aggActor, *aggK); err != nil {
			os.Exit(1)
		}
		return
	}
	if *krefRebuild {
		if err := runKRefRebuild(log); err != nil {
			log.Error("dựng lại k_ref lỗi", "event", "kref.rebuild", "outcome", "error", "error", err.Error())
			os.Exit(1)
		}
		return
	}
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
		defer registerGauges(ctx, log, direct, sched.pools)() // ADR-0006: xung đột quá SLA, độ trễ standby; phân vùng DEFAULT
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
	var sel []consumer
	for _, c := range allConsumers(log) {
		if !selected(mods, c.name) {
			continue
		}
		if c.cluster != migrations.PF || !c.module.Valid() {
			return 0, func() {}, errors.New("worker: consumer " + c.name + " thiếu module (ADR-0028) hoặc không ở cụm pf")
		}
		sel = append(sel, c)
	}
	var pools []*pgxpool.Pool
	cur := &currentConn{}
	closeFn := func() {
		for _, p := range pools {
			p.Close()
		}
		cur.close()
	}
	// W2.6 (ADR-0028): MỘT pool vai đăng nhập app_worker cho mọi consumer; mỗi consumer chạy giao dịch inbox
	// dưới vai nhóm của module nhận (db.ForModule). GIS US-SYNC-01/02: mọi lần xử lý thất bại ghi vào
	// pf_sync.integration_failure (r_sync, giao dịch riêng) cho màn giám sát đồng bộ; lần giao cuối (vào DLQ)
	// phát pf.sync.integration.dead_lettered → pf/notify báo Admin.
	dsn, err := dbadmin.RoleURL(base, migrations.PF, "", dbadmin.AppWorker)
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
	hs.Add("amqp", healthsrv.Closed(cur.isClosed))
	hs.Add("db", healthsrv.Pools(func() []*pgxpool.Pool { return pools }))
	// Ghi lần thất bại trên pool riêng (cùng vai app_worker): không tranh kết nối với giao dịch inbox đang mở.
	recPool, err := db.Open(ctx, dsn, db.Options{AppName: "worker-sync-monitor", MaxConns: 2, ViaPgBouncer: db.OptionsFromEnv("").ViaPgBouncer})
	if err != nil {
		closeFn()
		return 0, func() {}, err
	}
	pools = append(pools, recPool)
	rec := syncmod.FailureRecorder(recPool, clock.System{})
	// P0-WORKER-REPORTS (#128): dựng + kiểm phụ thuộc của handler (vd pf/reports cần pool DB thật) TRƯỚC khi nhận
	// thông điệp — thiếu → không khởi động, thay vì panic giữa lúc xử lý.
	for _, c := range sel {
		if c.prepare == nil {
			continue
		}
		if err := c.prepare(ctx); err != nil {
			closeFn()
			return 0, func() {}, fmt.Errorf("worker: consumer %s chưa sẵn sàng: %w", c.name, err)
		}
	}
	dbFor := func(c consumer) tenant.Beginner { return db.ForModule(pool, c.module) }
	// Lần quay số đầu: lỗi → không khởi động (cấu hình sai lộ ngay). Sau đó mất kết nối → kết nối lại, không thoát.
	first, err := dialSession(amqpURL, sel, dbFor)
	if err != nil {
		closeFn()
		return 0, func() {}, err
	}
	cur.set(first.conn)
	loop := amqpLoop[*amqpSession]{
		connect: func(context.Context) (*amqpSession, error) {
			s, err := dialSession(amqpURL, sel, dbFor)
			if err == nil {
				cur.set(s.conn)
			}
			return s, err
		},
		serve:   func(ctx context.Context, s *amqpSession) error { return serveSession(ctx, s, log, rec) },
		close:   func(s *amqpSession) { _ = s.conn.Close() },
		backoff: amqpBackoff,
		sleep:   sleepCtx,
		log:     log,
	}
	go func() { errc <- loop.run(ctx, first) }()
	return len(sel), closeFn, nil
}

type opsSchedule struct {
	s     *schedule.Scheduler
	pools map[migrations.Cluster]*pgxpool.Pool
}

// buildSchedule mở pool ag_ops_jobs nối thẳng tới mọi cụm và dựng lịch.
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
		dsn, err := dbadmin.RoleURL(direct, c, "", dbadmin.OpsJobs)
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
		// Leo thang SLA nhiệm vụ khuyến nông: tìm tenant bằng pool ag_ops_jobs
		// (chỉ đọc 4 cột), ghi bằng r_extension của module (R-05).
		extPool, err := openPF(ctx, direct, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		// Leo thang cần cây tổ chức (tìm TTKN tỉnh) và đơn vị hành chính (tỉnh của xã):
		// dựng port của party/masterdata bằng vai riêng của từng module (R-05).
		mdPool, err := openPF(ctx, direct, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		partyPool, err := openPF(ctx, direct, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		md := masterdata.New(mdPool, clk, masterdata.Deps{Params: []mdport.ParamRegistration{extension.MasterdataParams()}, Catalogs: []mdport.CatalogRegistration{extension.MasterdataCatalogs()}})
		pty := party.New(partyPool, clk, party.Deps{AdminUnits: md.AdminUnits()})
		kn := extension.New(extPool, clk, extension.Deps{
			Scope: pty.ScopeResolver(), Orgs: extension.NewPublishedOrganizations(extPool), Units: extension.NewPublishedAdminUnits(extPool), Params: md.Params(), Catalogs: md.Catalogs(),
		})
		if selected(jobs, extension.JobEscalation) {
			js = append(js, kn.Jobs(extension.OverdueTenants(out.pools[migrations.PF]), log)...)
		}
		if selected(jobs, extension.JobWorkSchedule) {
			// KN v7.7 P1 → P5: người nhận nhắc / người duyệt lấy từ sổ gán vai trò (phòng ban × tỉnh × cấp quản
			// lý) bằng r_accounts; thiếu → danh bạ trực pf_extension.staff như trước.
			accPool, err := openPF(ctx, direct, &closeFn)
			if err != nil {
				closeFn()
				return nil, func() {}, err
			}
			acc := accounts.New(accPool, clk, accounts.Deps{Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Departments: pty.Departments(), Units: md.AdminUnits()})
			kn.UseKNContext(acc.ExtensionKNContext(), acc.ExtensionKNDirectory())
			kn.UseRoleHolders(acc.RoleHolders()) // W11.46
			// KN v7.7 P5: khóa tuần đã kết thúc, nhắc hạn đăng ký / hạn duyệt lịch công tác.
			js = append(js, kn.ScheduleJobs(extension.ScheduleTenants(out.pools[migrations.PF]), log)...)
		}
	}
	if selected(jobs, extport.JobEmailDigest) && os.Getenv("AG_SMTP_HOST") != "" {
		// Email tóm tắt chỉ chạy khi đã cấu hình SMTP; tìm tenant bằng ag_ops_jobs (2 cột).
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
		// Lịch gửi báo cáo: tìm tenant bằng pool ag_ops_jobs (hai cột), chạy bằng r_reports;
		// tham số hạn mức DLP đọc qua pf/masterdata bằng vai của nó (R-05).
		repPool, err := openPF(ctx, direct, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		mdPool, err := openPF(ctx, direct, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		md := masterdata.New(mdPool, clk, masterdata.Deps{Params: []mdport.ParamRegistration{extension.MasterdataParams()}, Catalogs: []mdport.CatalogRegistration{extension.MasterdataCatalogs()}})
		rep := reports.New(repPool, clk, reports.Deps{FormSeeds: []reportsport.FormSeedRegistration{extension.ReportFormSeeds()}, Builders: extension.ReportBuilders(extension.ReportBuilderDeps{Clock: clk}), Catalog: []reportsport.CatalogRegistration{extension.ReportCatalog()}, Policies: []reportsport.Policy{extension.ReportPolicy()}, Params: md.Params(), Log: log})
		// G5 (ADR-0024): lối "lịch" của xuất báo cáo qua điểm kiểm soát chung (kênh report_export) —
		// Checkpoint của pf/metering (r_metering); workerEgress ghi đè cho kiểm thử.
		// AG_EGRESS_MODE sai → lỗi khởi động.
		mode, err := assembly.EgressModeFromEnv(os.Getenv)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		egress := workerEgress
		if egress == nil {
			meterPool, err := openPF(ctx, direct, &closeFn)
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
		// Độ tươi dữ liệu App HTX (CGH FN-07 AC-02/AC-03): tìm tenant bằng ag_ops_jobs
		// (hai cột), ghi cảnh báo bằng r_mechanization; ngưỡng đọc qua pf/masterdata (R-05).
		mechPool, err := openPF(ctx, direct, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		mdPool, err := openPF(ctx, direct, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		mech := mechanization.New(mechPool, clk, mechanization.Deps{Params: masterdata.New(mdPool, clk, masterdata.Deps{Params: []mdport.ParamRegistration{extension.MasterdataParams()}, Catalogs: []mdport.CatalogRegistration{extension.MasterdataCatalogs()}}).Params()})
		js = append(js, mech.Jobs(mechanization.FeedTenants(out.pools[migrations.PF]), log)...)
	}
	if selected(jobs, metricsport.JobMetricsSnapshot) {
		// Chỉ số có lịch sử + ảnh chụp lớp GIS: dựng module pf qua assembly.Build (pf/metrics đọc view _pub bằng
		// r_metrics, trên bản sao chỉ đọc khi có AG_DB_REPLICA_URL — W6.3); tìm tenant bằng pool ag_ops_jobs.
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
		// Leo thang hồ sơ MRV quá hạn (KN US-MRV-06): tìm tenant bằng ag_ops_jobs (hai cột),
		// ghi cảnh báo + sự kiện bằng r_mrv; SLA đọc qua pf/masterdata bằng vai của nó (R-05).
		mrvPool, err := openPF(ctx, direct, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		mdPool, err := openPF(ctx, direct, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		mrvMod := mrv.New(mrvPool, clk, mrv.Deps{Params: masterdata.New(mdPool, clk, masterdata.Deps{Params: []mdport.ParamRegistration{extension.MasterdataParams()}, Catalogs: []mdport.CatalogRegistration{extension.MasterdataCatalogs()}}).Params()})
		js = append(js, mrvMod.Jobs(mrv.PendingTenants(out.pools[migrations.PF]), log)...)
	}
	if selected(jobs, extension.JobCNTSAlert) {
		// W11.43 (BRD CNTS FN-13, UR-TINH-08, UR-TINH-13): cảnh báo quá hạn xử lý → Lãnh đạo phòng, nhắc nộp biểu kỳ CNTS, trễ
		// liên tiếp — cần cap/forms (SubmissionFeed) và gán vai trò (pf/accounts) → dựng module pf qua assembly.Build; tìm
		// tenant bằng ag_ops_jobs (cột của cnts_alert_current, pf_forms.period).
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
		js = append(js, mods.Extension.CNTSAlertJobs(extension.CNTSAlertTenants(out.pools[migrations.PF]), log)...)
	}
	if selected(jobs, extension.JobReportCycle) {
		// KN v7.7 P4 — vòng đời báo cáo (tự chốt kỳ + tổng hợp "Hệ thống", chốt lại sau hạn mở, tự nhắc): cần engine mẫu
		// (pf/reports), khu vực (pf/party), gán vai trò (pf/accounts) → dựng module pf qua assembly.Build (mỗi module một
		// vai DB, R-05); tìm tenant bằng ag_ops_jobs (hai cột của report_period).
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
	if selected(jobs, hrm.JobPromotionScan) {
		// W11.25 (BRD HCTH v3.0 FN-09 bước 1): tính mốc NLTX/TNVK hằng ngày → sổ pf_hrm.promotion_scan (+ thông báo khi nối);
		// dựng pf/hrm qua assembly.Build (r_hrm); tìm tenant bằng ag_ops_jobs (cột tenant_id của pf_hrm.staff).
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
		js = append(js, mods.HRM.PromotionJobs(hrm.PromotionTenants(out.pools[migrations.PF]), log)...)
	}
	if selected(jobs, apiclientport.JobKeyExpiry) && os.Getenv(apiclientport.EnvOperatorTenant) != "" {
		// G5 (ADR-0022 mục 4, RB-1): khóa API quá hạn → sự kiện expired ở sổ api_key_event của tenant đơn vị
		// vận hành (AG_PUBLIC_OPERATOR_TENANT; không đặt — ĐTK 1 — thì không có job), r_apiclient.
		mod, err := apiclientKeyJobs(ctx, direct, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		js = append(js, mod.Jobs(log)...)
	}
	if (selected(jobs, apiclientport.JobFeedBuild) || selected(jobs, apiclientport.JobFeedRetention)) && os.Getenv(apiclientport.EnvOperatorTenant) != "" {
		// W5.3 (ADR-0033 §2, ADR-0035): dựng tập dữ liệu của cổng tích hợp + dọn kho feed quá 30 ngày, r_apiclient.
		fjs, err := apiclientFeedJobs(ctx, direct, &closeFn, log)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		for _, j := range fjs {
			if selected(jobs, j.Name) {
				js = append(js, j)
			}
		}
	}
	if selected(jobs, apiclientport.JobWebhookDispatch) && os.Getenv(apiclientport.EnvOperatorTenant) != "" {
		// W5.7 (ADR-0033 §4): giao chỉ báo cursor tới webhook + ngắt mạch, r_apiclient; cần AG_WEBHOOK_KEY_V<n>.
		wjs, err := apiclientWebhookJobs(ctx, direct, &closeFn, log)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		js = append(js, wjs...)
	}
	if selected(jobs, priceport.JobCalendar) {
		// W11.74 (BRD Báo giá FN-AD-04.04 AC-01, FN-XA-03.04): mỗi phút ghi mốc khóa 12:00 (±1 phút) và nhắc nhập giá;
		// tìm tenant bằng ag_ops_jobs (cột tenant_id của pf_price, pf 000139), ghi bằng r_price; nhắc → outbox → pf/notify.
		pricePool, err := openPF(ctx, direct, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		pm, err := price.New(pricePool, clk, assembly.PriceProfiles())
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		js = append(js, pm.CalendarJobs(price.CalendarTenants(out.pools[migrations.PF]), log)...)
	}
	if selected(jobs, accport.JobDormant) && os.Getenv("AG_KC_ADMIN_URL") != "" {
		// D24 (W10.05): báo trước ngày 38, khóa ngày 45 tài khoản không hoạt động. Tìm tenant bằng ag_ops_jobs (cột
		// tenant_id, enabled của pf_accounts.account), ghi bằng r_accounts; lần đăng nhập đọc ở Keycloak (AG_KC_*,
		// view-events); ngưỡng là tham số có phiên bản của pf/masterdata (R-05). Chưa cấu hình Keycloak → không có job.
		dir, err := accounts.DirectoryFromEnv()
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		pfPool, err := openPF(ctx, direct, &closeFn)
		if err != nil {
			closeFn()
			return nil, func() {}, err
		}
		// AG_ACCOUNTS_DORMANT_DRY_RUN=true: chỉ đếm và ghi log (lần chạy đầu ở môi trường thật — runbook keycloak-mfa-d24).
		dry := os.Getenv("AG_ACCOUNTS_DORMANT_DRY_RUN") == "true"
		acc := accounts.New(pfPool, clk, accounts.Deps{Directory: dir, Params: masterdata.New(pfPool, clk, masterdata.Deps{Params: []mdport.ParamRegistration{extension.MasterdataParams()}, Catalogs: []mdport.CatalogRegistration{extension.MasterdataCatalogs()}}).Params(), DormantDryRun: dry})
		js = append(js, acc.Jobs(accounts.DormantTenants(out.pools[migrations.PF]), log)...)
	}
	if len(js) == 0 {
		closeFn()
		return nil, func() {}, errors.New("-jobs không chọn job hợp lệ nào (anchor, idempotency, " + accport.JobDormant + ", " + extension.JobEscalation + ", " + extension.JobWorkSchedule + ", " +
			extport.JobEmailDigest + ", " + reports.JobSchedules + ", " + mechport.JobAreaFeedCheck + ", " + metricsport.JobMetricsSnapshot +
			", " + mrvport.JobReviewEscalator + ", " + extension.JobReportCycle + ", " + extension.JobCNTSAlert +
			", " + apiclientport.JobKeyExpiry + ", " + apiclientport.JobFeedBuild + ", " + apiclientport.JobFeedRetention + ", " + apiclientport.JobWebhookDispatch +
			", " + priceport.JobCalendar + ", " + hrm.JobPromotionScan + ")")
	}
	out.s = &schedule.Scheduler{Clock: clk, Log: log, Jobs: js}
	return out, closeFn, nil
}

// openPF mở (một lần cho mỗi URL) pool nối thẳng cụm pf bằng vai đăng nhập app_worker cho job và đăng ký đóng
// pool. W2.6 (ADR-0028): mọi module của job dùng chung pool này, mỗi module tự bọc db.ForModule (r_<module>).
func openPF(ctx context.Context, direct string, closeFn *func()) (*pgxpool.Pool, error) {
	jobPoolsMu.Lock()
	defer jobPoolsMu.Unlock()
	if p, ok := jobPools[direct]; ok {
		return p, nil
	}
	d, err := dbadmin.RoleURL(direct, migrations.PF, "", dbadmin.AppWorker)
	if err != nil {
		return nil, err
	}
	o := db.OptionsFromEnv("worker-jobs-" + dbadmin.AppWorker)
	o.ViaPgBouncer = false // nối thẳng (AG_DB_DIRECT_URL)
	p, err := db.Open(ctx, d, o)
	if err != nil {
		return nil, err
	}
	jobPools[direct] = p
	prev := *closeFn
	*closeFn = func() {
		jobPoolsMu.Lock()
		delete(jobPools, direct)
		jobPoolsMu.Unlock()
		p.Close()
		prev()
	}
	return p, nil
}

var (
	jobPoolsMu sync.Mutex
	jobPools   = map[string]*pgxpool.Pool{}
)
