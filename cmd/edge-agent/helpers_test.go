package main

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
)

var testT0 = time.Date(2026, 12, 3, 0, 14, 22, 100_000_000, time.UTC)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// testConfig: cấu hình nhỏ cho kiểm thử (đệm vài chục KB).
func testConfig(t *testing.T, dir string, maxBytes int64) *Config {
	t.Helper()
	c := &Config{
		DeviceID: "scale-hub01-in",
		DataDir:  dir,
		Adapter:  AdapterConfig{Type: AdapterSimulator, Simulator: &SimConfig{Seed: 7}},
		Buffer:   BufferConfig{MaxRateHz: 0.0001, SegmentBytes: 4096, MaxBytes: maxBytes},
		Health:   HealthConfig{Listen: "127.0.0.1:0"},
		Ingest:   IngestConfig{BackoffBase: Duration(time.Millisecond), BackoffMax: Duration(10 * time.Millisecond)},
	}
	if err := c.normalize(); err != nil {
		t.Fatal(err)
	}
	return c
}

// enrolled đăng ký thiết bị trong dir và trả cấu hình + khóa.
func enrolled(t *testing.T, maxBytes int64) (*Config, ed25519.PrivateKey) {
	t.Helper()
	cfg := testConfig(t, t.TempDir(), maxBytes)
	if _, err := Enroll(cfg, 1, false, false, testT0, quietLog()); err != nil {
		t.Fatal(err)
	}
	k, err := LoadSourceKey(cfg.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, k
}

func openStore(t *testing.T, cfg *Config, k ed25519.PrivateKey) *Store {
	t.Helper()
	s, err := OpenStore(cfg, k.Public().(ed25519.PublicKey), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func reading(i int) edgewire.Reading {
	return edgewire.Reading{DeviceTime: edgewire.FormatDeviceTime(testT0.Add(time.Duration(i) * 500 * time.Millisecond)), ValueKg: gramsToKg(int64(i) * 1000), Stable: i%2 == 0, Unit: edgewire.UnitKg}
}

func appendN(t *testing.T, s *Store, n int) {
	t.Helper()
	for i := range n {
		if _, err := s.Append(reading(i)); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
}

// testPKI sinh CA, chứng thư máy chủ (127.0.0.1) và chứng thư client (CN = device) vào dir.
type testPKI struct {
	CAFile, CertFile, KeyFile string
	Server                    tls.Certificate
	Pool                      *x509.CertPool
}

func newTestPKI(t *testing.T, dir, device string) testPKI {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	leaf := func(cn string, ext x509.ExtKeyUsage, ip bool) ([]byte, *ecdsa.PrivateKey) {
		k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{ext}, DNSNames: []string{cn}}
		if ip {
			tpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		}
		der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &k.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		return der, k
	}
	write := func(name, typ string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: b}), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	sDER, sKey := leaf("ingest", x509.ExtKeyUsageServerAuth, true)
	cDER, cKey := leaf(device, x509.ExtKeyUsageClientAuth, false)
	ckDER, _ := x509.MarshalPKCS8PrivateKey(cKey)
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return testPKI{
		CAFile:   write("ca.crt", "CERTIFICATE", caDER),
		CertFile: write("client.crt", "CERTIFICATE", cDER),
		KeyFile:  write("client-tls.key", "PRIVATE KEY", ckDER),
		Server:   tls.Certificate{Certificate: [][]byte{sDER}, PrivateKey: sKey},
		Pool:     pool,
	}
}
