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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func main() {
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
	return serve(ctx, log, []*http.Server{
		{Addr: env("AG_HTTP_ADDR", ":8081"), Handler: assembly.NewRouter(d)},
		{Addr: env("AG_ADMIN_HTTP_ADDR", ":8083"), Handler: assembly.NewAdminRouter(d)},
	})
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
