package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// internalConfig là cấu hình listener Internal (mục 11): chỉ trong cụm, mTLS bắt buộc.
type internalConfig struct {
	Addr string
	TLS  *tls.Config
}

// internalConfigFromEnv đọc AG_INTERNAL_HTTP_ADDR cùng AG_INTERNAL_TLS_CERT,
// AG_INTERNAL_TLS_KEY (chứng thư máy chủ) và AG_INTERNAL_CLIENT_CA (CA cấp chứng
// thư client). Không đặt AG_INTERNAL_HTTP_ADDR → nil (listener tắt). Đã đặt thì
// cả ba tệp là bắt buộc: listener Internal không bao giờ chạy HTTP thuần.
func internalConfigFromEnv() (*internalConfig, error) {
	addr := os.Getenv("AG_INTERNAL_HTTP_ADDR")
	if addr == "" {
		return nil, nil
	}
	certFile, keyFile, caFile := os.Getenv("AG_INTERNAL_TLS_CERT"), os.Getenv("AG_INTERNAL_TLS_KEY"), os.Getenv("AG_INTERNAL_CLIENT_CA")
	if certFile == "" || keyFile == "" || caFile == "" {
		return nil, errors.New("AG_INTERNAL_HTTP_ADDR cần kèm AG_INTERNAL_TLS_CERT, AG_INTERNAL_TLS_KEY và AG_INTERNAL_CLIENT_CA (mTLS bắt buộc)")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("nạp chứng thư Internal: %w", err)
	}
	pem, err := os.ReadFile(caFile) //nolint:gosec // đường dẫn do vận hành cấu hình
	if err != nil {
		return nil, fmt.Errorf("đọc AG_INTERNAL_CLIENT_CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("AG_INTERNAL_CLIENT_CA không chứa chứng thư PEM hợp lệ")
	}
	return &internalConfig{Addr: addr, TLS: &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}}, nil
}

// NewInternalRouter dựng Internal API (/internal/v1) cho optimizer. Giới hạn thân
// request lớn hơn Application API: phong bì kết quả tới simulation.MaxInternalBodyBytes
// (+1 byte để handler tự trả 413 thay vì lỗi đọc). Danh tính client (CN/SAN) do
// module kiểm (AG_INTERNAL_CLIENT_NAMES); chứng thư do TLS kiểm.
func NewInternalRouter(log *slog.Logger, register ...func(*gin.RouterGroup)) *gin.Engine {
	r := httpx.NewEngine(log, int64(simulation.MaxInternalBodyBytes)+1)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "surface": "internal"}) })
	g := r.Group("/internal/v1")
	for _, reg := range register {
		reg(g)
	}
	return r
}

// newInternalServer dựng http.Server của listener Internal. Ghi 8 MiB qua mạng
// cụm có thể chậm hơn Application API nên thời gian đọc/ghi dài hơn.
func newInternalServer(cfg *internalConfig, h http.Handler) *http.Server {
	return &http.Server{
		Addr: cfg.Addr, Handler: h, TLSConfig: cfg.TLS,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 120 * time.Second, WriteTimeout: 120 * time.Second,
	}
}
