// Lệnh identity-vault: kho định danh (mục 10, ADR-0004), cụm ag_vault riêng,
// bề mặt Internal — chỉ mTLS trong cụm (mục 11, 17).
//
// Cấu hình (biến môi trường):
//
//	AG_HTTP_ADDR         mặc định :8090
//	AG_VAULT_DB_URL      URL gốc tới database cụm vault (host, cổng, sslmode) — vai vault_app ghép vào
//	AG_DB_PASSWORD_VAULT_APP hoặc AG_DB_APP_PASSWORD
//	AG_DB_VIA_PGBOUNCER  =1 khi đi qua PgBouncer transaction mode
//	AG_OIDC_ISSUER       issuer của realm Keycloak — kho tự xác minh token người dùng cuối (R-54)
//	AG_OIDC_AUDIENCE     audience bắt buộc trong token
//	AG_OIDC_JWKS_URL     (tùy chọn) lấy khóa ký từ URL này thay vì discovery (xem cmd/api-platform/oidc.go)
//	AG_VAULT_KEK         KEK dev: base64 chuẩn của 32 byte (KMS phần mềm; production thay bằng KMS trong lãnh thổ)
//	AG_VAULT_KEK_ID      định danh KEK, mặc định local-1
//	AG_VAULT_BLIND_KEY   khóa HMAC của chỉ mục mù: base64 chuẩn, ≥ 32 byte, KHÁC KEK
//	AG_TLS_CERT, AG_TLS_KEY  bật TLS
//	AG_TLS_CLIENT_CA     bật mTLS: bắt buộc chứng thư client do CA này cấp
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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func main() {
	log := telemetry.NewLogger("identity-vault", os.Stdout)
	if err := run(log); err != nil {
		log.Error("dừng do lỗi", "event", "startup", "outcome", "error", "error", err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// RB-7: production chỉ được nối tới đích trong lãnh thổ (internal/shared/residency).
	if err := residency.CheckEnv(); err != nil {
		return err
	}

	base := os.Getenv("AG_VAULT_DB_URL")
	issuer, audience := os.Getenv("AG_OIDC_ISSUER"), os.Getenv("AG_OIDC_AUDIENCE")
	if base == "" || issuer == "" || audience == "" {
		return errors.New("thiếu AG_VAULT_DB_URL, AG_OIDC_ISSUER hoặc AG_OIDC_AUDIENCE")
	}
	shutdownOTel, err := telemetry.Setup(ctx, "identity-vault")
	if err != nil {
		return err
	}
	defer func() { _ = shutdownOTel(context.Background()) }()
	keys, err := identity.LocalKMSFromEnv()
	if err != nil {
		return err
	}
	blind, err := identity.BlindKeyFromEnv()
	if err != nil {
		return err
	}
	tlsCfg, err := TLSConfigFromEnv()
	if err != nil {
		return err
	}
	dsn, err := dbadmin.RoleURL(base, migrations.Vault, "", identity.DBRole)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, dsn, db.OptionsFromEnv("identity-vault"))
	if err != nil {
		return err
	}
	defer pool.Close()
	mod, err := identity.New(identity.Config{DB: pool, Clock: clock.System{}, KMS: keys, BlindKey: blind})
	if err != nil {
		return err
	}
	verifier, err := verifierFromEnv(ctx, issuer, audience)
	if err != nil {
		return err
	}

	addr := os.Getenv("AG_HTTP_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	srv := &http.Server{
		Addr: addr, Handler: NewRouter(Deps{Log: log, Verifier: verifier, Module: mod, DB: pool}), TLSConfig: tlsCfg,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		if tlsCfg != nil {
			errc <- srv.ListenAndServeTLS("", "")
			return
		}
		errc <- srv.ListenAndServe()
	}()
	log.Info("lắng nghe", "event", "startup", "addr", addr, "tls", tlsCfg != nil, "mtls", tlsCfg != nil && tlsCfg.ClientCAs != nil, "outcome", "ok")
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
