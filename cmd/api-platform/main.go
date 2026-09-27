// Lệnh api-platform: Application API và Admin API của nền tảng AgriGreen.
//
// Cấu hình (biến môi trường):
//
//	AG_HTTP_ADDR         Application API, mặc định :8080
//	AG_ADMIN_HTTP_ADDR   Admin API (MFA bắt buộc), mặc định :8082 — chỉ mở trong mạng quản trị
//	AG_PF_DB_URL         URL gốc tới database cụm pf (host, cổng, sslmode) — vai và mật khẩu ghép theo module
//	AG_DB_PASSWORD_<VAI> hoặc AG_DB_APP_PASSWORD
//	AG_DB_VIA_PGBOUNCER  =1 khi đi qua PgBouncer transaction mode
//	AG_OIDC_ISSUER       issuer của realm Keycloak
//	AG_OIDC_AUDIENCE     audience bắt buộc trong token
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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func main() {
	log := telemetry.NewLogger("api-platform", os.Stdout)
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

	base := os.Getenv("AG_PF_DB_URL")
	issuer, audience := os.Getenv("AG_OIDC_ISSUER"), os.Getenv("AG_OIDC_AUDIENCE")
	if base == "" || issuer == "" || audience == "" {
		return errors.New("thiếu AG_PF_DB_URL, AG_OIDC_ISSUER hoặc AG_OIDC_AUDIENCE")
	}
	opts := db.OptionsFromEnv("api-platform")
	open := func(ctx context.Context, role string) (*pgxpool.Pool, error) {
		dsn, err := dbadmin.RoleURL(base, migrations.PF, "", role)
		if err != nil {
			return nil, err
		}
		return db.Open(ctx, dsn, opts)
	}
	ops, err := open(ctx, "platform_api")
	if err != nil {
		return err
	}
	defer ops.Close()
	mods, pools, err := buildModules(ctx, open, clock.System{})
	for _, p := range pools {
		defer p.Close()
	}
	if err != nil {
		return err
	}

	verifier, err := authz.NewVerifier(ctx, issuer, audience)
	if err != nil {
		return err
	}
	d := Deps{Log: log, Verifier: verifier, Ops: ops, Modules: mods, Pools: pools}
	servers := []*http.Server{
		{Addr: env("AG_HTTP_ADDR", ":8080"), Handler: NewRouter(d)},
		{Addr: env("AG_ADMIN_HTTP_ADDR", ":8082"), Handler: NewAdminRouter(d)},
	}
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
