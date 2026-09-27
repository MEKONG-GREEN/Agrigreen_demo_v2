// Lệnh api-platform: Application API của nền tảng AgriGreen.
//
// Cấu hình (biến môi trường):
//
//	AG_HTTP_ADDR        địa chỉ lắng nghe, mặc định :8080
//	AG_PF_DB_URL        URL gốc tới database cụm pf (host, cổng, sslmode) — tên vai và mật khẩu ghép theo module
//	AG_DB_PASSWORD_<VAI> hoặc AG_DB_APP_PASSWORD
//	AG_DB_VIA_PGBOUNCER =1 khi đi qua PgBouncer transaction mode
//	AG_OIDC_ISSUER      issuer của realm Keycloak
//	AG_OIDC_AUDIENCE    audience bắt buộc trong token
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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
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
	open := func(role string) (*db.Pool, error) {
		dsn, err := dbadmin.RoleURL(base, migrations.PF, "", role)
		if err != nil {
			return nil, err
		}
		return db.Open(ctx, dsn, opts)
	}
	ops, err := open("platform_api")
	if err != nil {
		return err
	}
	defer ops.Close()
	partyPool, err := open(party.DBRole)
	if err != nil {
		return err
	}
	defer partyPool.Close()

	verifier, err := authz.NewVerifier(ctx, issuer, audience)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              env("AG_HTTP_ADDR", ":8080"),
		Handler:           NewRouter(Deps{Log: log, Verifier: verifier, Clock: clock.System{}, Ops: ops, Party: partyPool}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("lắng nghe", "event", "startup", "addr", srv.Addr, "outcome", "ok")
	select {
	case <-ctx.Done():
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}
