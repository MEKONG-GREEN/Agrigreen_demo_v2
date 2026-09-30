package ingest

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
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
	caPEM, err := os.ReadFile(clientCAFile) //nolint:gosec // đường dẫn do vận hành cấu hình
	if err != nil {
		return nil, fmt.Errorf("ingest: đọc AG_INGEST_CLIENT_CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("ingest: AG_INGEST_CLIENT_CA không chứa chứng thư PEM hợp lệ")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}, nil
}

// ErrSharedDeviceCA: CA tin cậy cho client (AG_INGEST_CLIENT_CA) cũng là CA đã ký chứng thư máy
// chủ ingest — tức CA dịch vụ nội bộ đang được dùng để xác thực THIẾT BỊ. Production phải dùng CA
// thiết bị riêng (docs/runbooks/device-pki.md): chứng thư dịch vụ nội bộ (optimizer, api-platform…)
// không được là chứng thư client hợp lệ của bề mặt nhận lô.
var ErrSharedDeviceCA = errors.New("ingest: AG_INGEST_CLIENT_CA phải là CA thiết bị riêng, không được là CA đã ký chứng thư máy chủ")

// RequireSeparateDeviceCA kiểm AG_INGEST_CLIENT_CA chỉ gồm chứng thư CA (basicConstraints CA:TRUE)
// và không CA nào trong đó ký chứng thư máy chủ certFile. cmd/ingest gọi khi AG_ENV=production
// hoặc AG_INGEST_REQUIRE_DEVICE_CA=1 (Compose dev bật).
func RequireSeparateDeviceCA(certFile, clientCAFile string) error {
	srvPEM, err := os.ReadFile(certFile) //nolint:gosec // đường dẫn do vận hành cấu hình
	if err != nil {
		return fmt.Errorf("ingest: đọc AG_INGEST_TLS_CERT: %w", err)
	}
	srv, err := allCerts(srvPEM)
	if err != nil {
		return fmt.Errorf("ingest: AG_INGEST_TLS_CERT: %w", err)
	}
	caPEM, err := os.ReadFile(clientCAFile) //nolint:gosec // đường dẫn do vận hành cấu hình
	if err != nil {
		return fmt.Errorf("ingest: đọc AG_INGEST_CLIENT_CA: %w", err)
	}
	cas, err := allCerts(caPEM)
	if err != nil {
		return fmt.Errorf("ingest: AG_INGEST_CLIENT_CA: %w", err)
	}
	for _, ca := range cas {
		if !ca.IsCA {
			return fmt.Errorf("ingest: AG_INGEST_CLIENT_CA chứa chứng thư không phải CA (%s)", ca.Subject.CommonName)
		}
		if srv[0].CheckSignatureFrom(ca) == nil {
			return fmt.Errorf("%w (%s)", ErrSharedDeviceCA, ca.Subject.CommonName)
		}
	}
	return nil
}

func allCerts(pemBytes []byte) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	for rest := pemBytes; ; {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		if b.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(b.Bytes)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, errors.New("không có chứng thư PEM hợp lệ")
	}
	return out, nil
}
