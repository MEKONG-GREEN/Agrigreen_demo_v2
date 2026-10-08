package main

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

func TestProductionGuard(t *testing.T) {
	mtls := &tls.Config{ClientCAs: x509.NewCertPool(), ClientAuth: tls.RequireAndVerifyClientCert}
	tlsOnly := &tls.Config{}
	cases := []struct {
		name     string
		env      map[string]string
		tls      *tls.Config
		localKMS bool
		wantErr  []string // rỗng → không lỗi
	}{
		{name: "ngoài production không kiểm", env: map[string]string{"AG_ENV": "staging"}, localKMS: true},
		{name: "thiếu AG_ENV → lỗi, không mở cửa", env: map[string]string{}, localKMS: true, wantErr: []string{"AG_ENV"}},
		{name: "AG_ENV gõ sai → lỗi", env: map[string]string{"AG_ENV": "prd"}, localKMS: true, wantErr: []string{"AG_ENV"}},
		{name: "production đủ mTLS, KMS thật", env: map[string]string{"AG_ENV": "production"}, tls: mtls},
		{name: "production không TLS", env: map[string]string{"AG_ENV": "production"}, wantErr: []string{"mTLS"}},
		{name: "production TLS không xác thực client", env: map[string]string{"AG_ENV": "production"}, tls: tlsOnly, wantErr: []string{"mTLS"}},
		{name: "production KMS phần mềm", env: map[string]string{"AG_ENV": "production"}, tls: mtls, localKMS: true, wantErr: []string{"KMS phần mềm"}},
		{name: "production cả hai lỗi", env: map[string]string{"AG_ENV": "prod"}, localKMS: true, wantErr: []string{"mTLS", "KMS phần mềm"}},
		{name: "production ngoại lệ tường minh", env: map[string]string{"AG_ENV": "production", AllowPlaintextEnv: "1", AllowLocalKMSEnv: "1"},
			localKMS: true},
		{name: "ngoại lệ phải đúng giá trị 1", env: map[string]string{"AG_ENV": "production", AllowLocalKMSEnv: "true"}, tls: mtls, localKMS: true,
			wantErr: []string{"KMS phần mềm"}},
	}
	log := telemetry.NewLogger("test", io.Discard)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := productionGuard(func(k string) string { return c.env[k] }, c.tls, c.localKMS, log)
			if len(c.wantErr) == 0 {
				if err != nil {
					t.Fatalf("không mong lỗi: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("mong lỗi chứa %v", c.wantErr)
			}
			for _, w := range c.wantErr {
				if !strings.Contains(err.Error(), w) {
					t.Fatalf("lỗi %q thiếu %q", err, w)
				}
			}
		})
	}
}
