package ingest

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

// ServerTLS dựng cấu hình mTLS của listener ingest (cùng cách đặt tên với listener
// Internal của api-platform: AG_INGEST_TLS_CERT, AG_INGEST_TLS_KEY, AG_INGEST_CLIENT_CA).
// Chứng thư client do CA nội bộ cấp là BẮT BUỘC (RequireAndVerifyClientCert) —
// ingest không bao giờ chạy HTTP thuần trên bề mặt nhận lô (mục 17).
func ServerTLS(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" || clientCAFile == "" {
		return nil, errors.New("ingest: cần AG_INGEST_TLS_CERT, AG_INGEST_TLS_KEY và AG_INGEST_CLIENT_CA (mTLS bắt buộc)")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("ingest: nạp chứng thư máy chủ: %w", err)
	}
	pem, err := os.ReadFile(clientCAFile) //nolint:gosec // đường dẫn do vận hành cấu hình
	if err != nil {
		return nil, fmt.Errorf("ingest: đọc AG_INGEST_CLIENT_CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("ingest: AG_INGEST_CLIENT_CA không chứa chứng thư PEM hợp lệ")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}, nil
}
