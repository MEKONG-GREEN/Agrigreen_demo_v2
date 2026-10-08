// Lệnh api-public: Public API cho bên thứ ba (partner.<domain>) — tiến trình RIÊNG (ADR-0022 mục 1).
// Nhị phân chỉ có route Public; không import vùng mkg (R-03) và không import kho định danh, kể cả
// client (RB-3) — archlint cưỡng chế.
//
// TẮT ở ĐTK 1: AG_PUBLIC_API_ENABLED ≠ "1" → thoát lỗi ngay khi khởi động (đóng khi lỗi, không lặng lẽ
// mở). Phase K bật bằng cấu hình + số bản sao (ADR-0025, R-01).
//
// Cấu hình (biến môi trường):
//
//	AG_PUBLIC_API_ENABLED      "1" mới nghe
//	AG_PUBLIC_HTTP_ADDR        mặc định :8085
//	AG_PUBLIC_OPERATOR_TENANT  tenant đơn vị vận hành (chủ sổ khách hàng API và sổ đo lường) — bắt buộc
//	AG_APIKEY_PEPPER_V<n>, AG_APIKEY_PEPPER_CURRENT  pepper HMAC (≥ 32 byte, trình quản lý bí mật) — bắt buộc
//	AG_PUBLIC_REPLICAS         số bản sao (chia giới hạn tần suất trong tiến trình khi không có Valkey), mặc định 1
//	AG_VALKEY_URL              (tùy chọn) giới hạn tần suất dùng chung — adapter chưa có ở G5-1, dùng bộ đếm trong tiến trình
//	AG_PF_DB_URL               URL gốc tới PgBouncer cụm pf — vai ghép theo module (public_api, apiclient_app,
//	                           metering_app, party_app, cropcycle_app, geo_app, mrv_app, masterdata_app)
//	AG_S3_ENDPOINT, AG_S3_BUCKET, AG_S3_USE_TLS, AG_S3_ACCESS_KEY, AG_S3_SECRET_KEY  kho tệp xuất (chỉ đọc —
//	                           phát luồng GET /v1/jobs/{id}/file); hoặc AG_OBJSTORE_DIR ở dev
//	AG_EGRESS_MODE             observe | enforce (pf/metering); Public API luôn đóng khi lỗi
//	AG_DB_PASSWORD_<VAI> hoặc AG_DB_APP_PASSWORD, AG_DB_VIA_PGBOUNCER, AG_ENV, AG_OTLP_ENDPOINT
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-public/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func main() {
	log := telemetry.NewLogger("api-public", os.Stdout)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, log, os.Getenv); err != nil {
		log.Error("dừng do lỗi", "event", "startup", "outcome", "error", "error", err.Error())
		os.Exit(1)
	}
}

// ErrDisabled: Public API tắt (ĐTK 1) — tiến trình thoát lỗi.
var ErrDisabled = errors.New("api-public: " + port.EnvEnabled + " ≠ 1 — Public API tắt ở ĐTK 1 (ADR-0022), thoát")

func run(ctx context.Context, log *slog.Logger, getenv func(string) string) error {
	if getenv(port.EnvEnabled) != "1" {
		return ErrDisabled
	}
	// RB-7: production chỉ nối đích trong lãnh thổ.
	if err := residency.CheckEnv(); err != nil {
		return err
	}
	operator, err := uuid.Parse(getenv(port.EnvOperatorTenant))
	if err != nil || operator == uuid.Nil {
		return fmt.Errorf("api-public: %s bắt buộc, UUID tenant đơn vị vận hành", port.EnvOperatorTenant)
	}
	peppers, err := apiclient.PeppersFromEnv()
	if err != nil {
		return err
	}
	if peppers == nil {
		return fmt.Errorf("api-public: thiếu %s<n> / %s", port.EnvPepperPrefix, port.EnvPepperCurrent)
	}
	base := getenv("AG_PF_DB_URL")
	if base == "" {
		return errors.New("api-public: thiếu AG_PF_DB_URL")
	}
	shutdownOTel, err := telemetry.Setup(ctx, "api-public")
	if err != nil {
		return err
	}
	defer func() { _ = shutdownOTel(context.Background()) }()

	opts := db.OptionsFromEnv("api-public")
	var pools []*pgxpool.Pool
	defer func() {
		for _, p := range pools {
			p.Close()
		}
	}()
	open := func(ctx context.Context, role string) (*pgxpool.Pool, error) {
		dsn, err := dbadmin.RoleURL(base, migrations.PF, "", role)
		if err != nil {
			return nil, err
		}
		p, err := db.Open(ctx, dsn, opts)
		if err == nil {
			pools = append(pools, p)
		}
		return p, err
	}
	var store objstore.Store
	if getenv("AG_S3_ENDPOINT") != "" || getenv("AG_OBJSTORE_DIR") != "" {
		if store, err = objstore.FromEnv(ctx); err != nil {
			return err
		}
	}
	replicas := 1
	if v := getenv("AG_PUBLIC_REPLICAS"); v != "" {
		if replicas, err = strconv.Atoi(v); err != nil || replicas < 1 {
			return errors.New("api-public: AG_PUBLIC_REPLICAS phải là số nguyên ≥ 1")
		}
	}
	if getenv(port.EnvValkeyURL) != "" {
		log.Warn("AG_VALKEY_URL đã đặt nhưng adapter Valkey chưa có ở G5-1 — dùng giới hạn trong tiến trình chia theo AG_PUBLIC_REPLICAS",
			"event", "startup", "outcome", "degraded")
	}
	mode, err := meter.ParseMode(getenv(meter.EnvEgressMode))
	if err != nil {
		return err
	}
	mod, d, err := assembly.Build(ctx, open, assembly.Config{Operator: operator, Peppers: peppers, Store: store, Replicas: replicas,
		Production: residency.Production(getenv), EgressMode: mode, Clock: clock.System{}, Log: log})
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: env(getenv, port.EnvHTTPAddr, port.DefaultHTTPAddr), Handler: mod.PublicRouter(d),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 5 * time.Minute}
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

func env(getenv func(string) string, k, def string) string {
	if v := getenv(k); v != "" {
		return v
	}
	return def
}
