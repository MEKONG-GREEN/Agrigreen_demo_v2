// Lệnh api-internal: API nội bộ MKG (vùng mkg, mục 5 và 17). Nối các module
// internal/mkg/* qua cmd/api-internal/assembly; mỗi module một vai DB ở cụm mkg.
//
// Cấu hình (biến môi trường):
//
//	AG_HTTP_ADDR         bề mặt ứng dụng nội bộ MKG, mặc định :8081
//	AG_ADMIN_HTTP_ADDR   bề mặt quản trị (MFA bắt buộc), mặc định :8083
//	AG_MKG_DB_URL        URL gốc tới database cụm mkg (host, cổng, sslmode) — vai và mật khẩu ghép theo module
//	AG_DB_PASSWORD_<VAI> hoặc AG_DB_APP_PASSWORD
//	AG_DB_VIA_PGBOUNCER  =1 khi đi qua PgBouncer transaction mode
//	AG_OIDC_ISSUER, AG_OIDC_AUDIENCE, AG_OIDC_JWKS_URL (tùy chọn) — như api-platform
//	AG_OTLP_ENDPOINT     bật OpenTelemetry (ADR-0013)
//	AG_MKG_JOBS          job định kỳ: all | none | hub_capacity_daily — chỉ leader chạy. Mặc định all
//	                     khi có AG_MKG_DB_DIRECT_URL, none khi không
//	AG_MKG_DB_DIRECT_URL URL nối THẲNG cụm mkg (không qua PgBouncer) cho khóa leader + tìm tenant
//	                     (vai ag_outbox_publisher, AG_DB_PASSWORD_AG_OUTBOX_PUBLISHER)
//
// `api-internal -healthcheck <url>`: GET url, thoát 0 khi 200 — healthcheck của Docker
// (ảnh distroless không có curl/wget).
//
// Thiếu AG_MKG_DB_URL: chạy chế độ G0 (chỉ /healthz) để đơn vị triển khai cũ
// không hỏng khi chưa cấu hình cụm mkg.
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

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-internal/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/leader"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "-healthcheck" {
		os.Exit(healthcheck(os.Args[2]))
	}
	log := telemetry.NewLogger("api-internal", os.Stdout)
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

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := residency.CheckEnv(); err != nil { // RB-7
		return err
	}
	base := os.Getenv("AG_MKG_DB_URL")
	if base == "" {
		return serve(ctx, log, []*http.Server{{Addr: env("AG_HTTP_ADDR", ":8081"), Handler: healthOnly(log)}})
	}
	issuer, audience := os.Getenv("AG_OIDC_ISSUER"), os.Getenv("AG_OIDC_AUDIENCE")
	if issuer == "" || audience == "" {
		return errors.New("thiếu AG_OIDC_ISSUER hoặc AG_OIDC_AUDIENCE")
	}
	shutdownOTel, err := telemetry.Setup(ctx, "api-internal")
	if err != nil {
		return err
	}
	defer func() { _ = shutdownOTel(context.Background()) }()
	opts := db.OptionsFromEnv("api-internal")
	open := func(ctx context.Context, role string) (*pgxpool.Pool, error) {
		dsn, err := dbadmin.RoleURL(base, migrations.MKG, "", role)
		if err != nil {
			return nil, err
		}
		return db.Open(ctx, dsn, opts)
	}
	mods, ops, pools, err := assembly.Build(ctx, open, clock.System{})
	for _, p := range pools {
		defer p.Close()
	}
	if err != nil {
		return err
	}
	var verifier *authz.Verifier
	if jwks := os.Getenv("AG_OIDC_JWKS_URL"); jwks != "" {
		verifier = authz.NewVerifierWithKeys(issuer, audience, oidc.NewRemoteKeySet(context.WithoutCancel(ctx), jwks))
	} else if verifier, err = authz.NewVerifier(ctx, issuer, audience); err != nil {
		return err
	}
	d := assembly.Deps{Log: log, Verifier: verifier, Ops: ops, Modules: mods.List(), Pools: pools}
	// Lịch job của vùng mkg (GIS US-WH-01 AC-2: công bố sức chứa theo ngày qua van). Chỉ MỘT bản
	// api-internal chạy (khóa advisory trên kết nối THẲNG tới cụm mkg, không qua PgBouncer): mặc
	// định bật khi có AG_MKG_DB_DIRECT_URL; tìm tenant bằng vai ag_outbox_publisher, ghi bằng vai
	// của module (R-05).
	direct := os.Getenv("AG_MKG_DB_DIRECT_URL")
	defJobs := "none"
	if direct != "" {
		defJobs = "all"
	}
	if jobs := env("AG_MKG_JOBS", defJobs); jobs == "none" {
		log.Info("không chạy job định kỳ mkg (đặt AG_MKG_DB_DIRECT_URL hoặc AG_MKG_JOBS)", "event", "startup", "outcome", "ok")
	} else {
		if direct == "" {
			direct = base
		}
		dsn, err := dbadmin.RoleURL(direct, migrations.MKG, "", dbadmin.OutboxPublisher)
		if err != nil {
			return err
		}
		jobPool, err := db.Open(ctx, dsn, db.Options{AppName: "api-internal-jobs", MaxConns: 2})
		if err != nil {
			return err
		}
		defer jobPool.Close()
		sched := &schedule.Scheduler{Clock: clock.System{}, Log: log, Jobs: assembly.Jobs(mods, jobPool, jobs, log)}
		if len(sched.Jobs) == 0 {
			return errors.New("AG_MKG_JOBS không chọn job hợp lệ nào (all, none, " + assembly.JobNames() + ")")
		}
		go func() {
			if err := leader.Run(ctx, jobPool, leader.LockSchedules, 15*time.Second, leader.Options{}, log, sched.Run); err != nil &&
				!errors.Is(err, context.Canceled) {
				log.Error("lịch job mkg dừng", "event", "jobs", "outcome", "error", "error", err.Error())
			}
		}()
	}
	return serve(ctx, log, []*http.Server{
		{Addr: env("AG_HTTP_ADDR", ":8081"), Handler: assembly.NewRouter(d)},
		{Addr: env("AG_ADMIN_HTTP_ADDR", ":8083"), Handler: assembly.NewAdminRouter(d)},
	})
}

// healthcheck gọi url (chỉ địa chỉ loopback do người vận hành cấu hình) và trả mã thoát.
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

func healthOnly(log *slog.Logger) http.Handler {
	r := httpx.NewEngine(log, 1<<20)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "zone": "mkg"}) })
	return r
}

func serve(ctx context.Context, log *slog.Logger, servers []*http.Server) error {
	errc := make(chan error, len(servers))
	for _, s := range servers {
		s.ReadHeaderTimeout, s.ReadTimeout, s.WriteTimeout = 5*time.Second, 30*time.Second, 30*time.Second
		go func(s *http.Server) { errc <- s.ListenAndServe() }(s)
		log.Info("lắng nghe", "event", "startup", "addr", s.Addr, "outcome", "ok")
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
	for _, s := range servers {
		_ = s.Shutdown(shutdown)
	}
	return nil
}
