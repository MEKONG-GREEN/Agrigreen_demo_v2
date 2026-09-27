package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"time"
)

// vaultHTTPClientFromEnv dựng HTTP client tới identity-vault. Có chứng thư client
// thì bật mTLS (bề mặt Internal, mục 11); Local/Dev có thể chạy không TLS.
func vaultHTTPClientFromEnv() (*http.Client, error) {
	c := &http.Client{Timeout: 5 * time.Second}
	cert, key, ca := os.Getenv("AG_VAULT_CLIENT_CERT"), os.Getenv("AG_VAULT_CLIENT_KEY"), os.Getenv("AG_VAULT_CA")
	if cert == "" && key == "" && ca == "" {
		return c, nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cert != "" || key != "" {
		pair, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	if ca != "" {
		pem, err := os.ReadFile(ca) //nolint:gosec // đường dẫn do vận hành cấu hình
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("AG_VAULT_CA không chứa chứng thư hợp lệ")
		}
		cfg.RootCAs = pool
	}
	c.Transport = &http.Transport{TLSClientConfig: cfg}
	return c, nil
}
