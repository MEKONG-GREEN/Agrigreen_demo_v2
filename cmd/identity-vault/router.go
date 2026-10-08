package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// Pinger là pool DB để /readyz kiểm.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps là phụ thuộc của identity-vault.
type Deps struct {
	Log      *slog.Logger
	Verifier *authz.Verifier // xác minh token người dùng cuối được chuyển tiếp (R-54)
	Module   httpx.Module
	DB       Pinger
	// KMSLocal: kho chạy KEK phần mềm — phơi ở /readyz (kms_local=1) để vận hành thấy rủi ro (M10).
	KMSLocal    bool
	KMSProvider string
}

// maxBody giới hạn thân request.
const maxBody = 64 << 10

// NewRouter dựng API của kho định danh: /v1 bắt buộc Bearer token của người
// dùng cuối; tenant và người yêu cầu lấy từ token (R-16).
func NewRouter(d Deps) *gin.Engine {
	r := httpx.NewEngine(d.Log, maxBody)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "zone": "vault"}) })
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if d.DB == nil || d.DB.Ping(ctx) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db_unavailable"})
			return
		}
		kmsLocal := 0
		if d.KMSLocal {
			kmsLocal = 1
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready", "kms_local": kmsLocal, "kms_provider": d.KMSProvider})
	})
	v1 := r.Group("/v1", httpx.Authenticate(d.Verifier))
	d.Module.RegisterApp(v1)
	return r
}

// TLSConfigFromEnv đọc AG_TLS_CERT, AG_TLS_KEY (bật TLS) và AG_TLS_CLIENT_CA
// (bật mTLS: bắt buộc chứng thư client do CA này cấp, mục 11 bề mặt Internal).
// Không đặt AG_TLS_CERT → nil (HTTP thuần, chỉ cho dev).
func TLSConfigFromEnv() (*tls.Config, error) {
	certFile, keyFile, caFile := os.Getenv("AG_TLS_CERT"), os.Getenv("AG_TLS_KEY"), os.Getenv("AG_TLS_CLIENT_CA")
	if certFile == "" && keyFile == "" {
		if caFile != "" {
			return nil, errors.New("AG_TLS_CLIENT_CA cần kèm AG_TLS_CERT và AG_TLS_KEY")
		}
		return nil, nil
	}
	if certFile == "" || keyFile == "" {
		return nil, errors.New("cần đặt cả AG_TLS_CERT và AG_TLS_KEY")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("nạp chứng thư TLS: %w", err)
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	if caFile != "" {
		pem, err := os.ReadFile(caFile) //nolint:gosec // đường dẫn do người vận hành cấu hình
		if err != nil {
			return nil, fmt.Errorf("đọc AG_TLS_CLIENT_CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("AG_TLS_CLIENT_CA không chứa chứng thư PEM hợp lệ")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
}
