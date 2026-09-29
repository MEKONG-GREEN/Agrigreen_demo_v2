package main

import (
	"bytes"
	"crypto/ecdsa"
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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// testCA là CA tự ký của kiểm thử; issue cấp chứng thư lá có CN = name.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCA(t *testing.T, cn string) *testCA {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return &testCA{cert: c, key: k, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func (ca *testCA) issue(t *testing.T, cn string, usage x509.ExtKeyUsage, dns ...string) (certPEM, keyPEM []byte) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: cn}, DNSNames: dns,
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:   time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca.cert, &k.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(k)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd})
}

func writeFile(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// internalFixture dựng listener Internal THẬT từ biến môi trường (internalConfigFromEnv)
// với Internal API của pf/simulation (xác thực mTLS mặc định: CN/SAN = optimizer).
type internalFixture struct {
	srv *httptest.Server
	ca  *testCA
}

func newInternalFixture(t *testing.T) *internalFixture {
	t.Helper()
	dir := t.TempDir()
	ca := newTestCA(t, "agrigreen-dev-ca")
	sc, sk := ca.issue(t, "api-platform", x509.ExtKeyUsageServerAuth, "localhost")
	t.Setenv("AG_INTERNAL_HTTP_ADDR", "127.0.0.1:0")
	t.Setenv("AG_INTERNAL_TLS_CERT", writeFile(t, dir, "server.crt", sc))
	t.Setenv("AG_INTERNAL_TLS_KEY", writeFile(t, dir, "server.key", sk))
	t.Setenv("AG_INTERNAL_CLIENT_CA", writeFile(t, dir, "ca.crt", ca.pem))
	t.Setenv("AG_INTERNAL_CLIENT_NAMES", "")
	cfg, err := internalConfigFromEnv()
	if err != nil || cfg == nil {
		t.Fatalf("cấu hình Internal: %v", err)
	}
	// Module không cần DB: mọi ca dưới đây dừng ở bước xác thực/kiểm đầu vào.
	sim := simulation.New(nil, clock.System{}, simulation.Deps{})
	srv := httptest.NewUnstartedServer(NewInternalRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), sim.RegisterInternal))
	srv.TLS = cfg.TLS
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return &internalFixture{srv: srv, ca: ca}
}

func (f *internalFixture) client(t *testing.T, ca *testCA, cn string) *http.Client {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(f.ca.cert)
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	if ca != nil {
		c, k := ca.issue(t, cn, x509.ExtKeyUsageClientAuth)
		pair, err := tls.X509KeyPair(c, k)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: cfg}}
}

const simPath = "/internal/v1/simulations/0190a000-0000-7000-8000-000000000001"

func do(t *testing.T, c *http.Client, method, url string, body []byte, tenant string) (*http.Response, error) {
	t.Helper()
	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	if tenant != "" {
		req.Header.Set("X-Tenant-ID", tenant)
	}
	resp, err := c.Do(req)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	return resp, err
}

// GIS US-SIM (Internal API): listener Internal bắt buộc chứng thư client — bắt tay
// TLS không có chứng thư bị máy chủ từ chối, không tới được route.
func TestInternalListenerRequiresClientCert(t *testing.T) {
	f := newInternalFixture(t)
	if _, err := do(t, f.client(t, nil, ""), http.MethodGet, f.srv.URL+simPath+"/snapshot", nil, ""); err == nil {
		t.Fatal("thiếu chứng thư client mà vẫn kết nối được")
	}
	// Chứng thư do CA khác cấp (dù CN đúng) cũng bị từ chối ở TLS.
	other := newTestCA(t, "other-ca")
	if _, err := do(t, f.client(t, other, "optimizer"), http.MethodGet, f.srv.URL+simPath+"/snapshot", nil, ""); err == nil {
		t.Fatal("chứng thư của CA lạ mà vẫn kết nối được")
	}
}

// GIS US-SIM (Internal API): chứng thư hợp lệ nhưng CN không thuộc
// AG_INTERNAL_CLIENT_NAMES → 403 simulation.internal_forbidden; CN optimizer qua
// được xác thực (dừng ở kiểm header tenant → 400).
func TestInternalListenerRejectsWrongCN(t *testing.T) {
	f := newInternalFixture(t)
	resp, err := do(t, f.client(t, f.ca, "web-ops"), http.MethodGet, f.srv.URL+simPath+"/snapshot", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("CN lạ: mong 403, nhận %d", resp.StatusCode)
	}
	resp, err = do(t, f.client(t, f.ca, "optimizer"), http.MethodGet, f.srv.URL+simPath+"/snapshot", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("CN optimizer, thiếu X-Tenant-ID: mong 400, nhận %d", resp.StatusCode)
	}
}

// Giới hạn thân request của listener Internal lớn hơn 1 MiB của Application API:
// thân vượt simulation.MaxInternalBodyBytes → 413 từ chính handler (engine giới
// hạn 1 MiB sẽ cắt ngang và handler trả 400 do lỗi đọc thân).
func TestInternalListenerBodyLimit(t *testing.T) {
	f := newInternalFixture(t)
	c := f.client(t, f.ca, "optimizer")
	resp, err := do(t, c, http.MethodPost, f.srv.URL+simPath+"/result", bytes.Repeat([]byte("x"), simulation.MaxInternalBodyBytes+2), "0190a000-0000-7000-8000-0000000000aa")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("thân quá lớn: mong 413, nhận %d", resp.StatusCode)
	}
	resp, err = do(t, c, http.MethodGet, f.srv.URL+"/healthz", nil, "")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz Internal: %v %v", resp, err)
	}
}

func TestInternalConfigFromEnv(t *testing.T) {
	t.Setenv("AG_INTERNAL_HTTP_ADDR", "")
	if cfg, err := internalConfigFromEnv(); cfg != nil || err != nil {
		t.Fatalf("không đặt địa chỉ → tắt listener: %v %v", cfg, err)
	}
	t.Setenv("AG_INTERNAL_HTTP_ADDR", ":8084")
	t.Setenv("AG_INTERNAL_TLS_CERT", "")
	t.Setenv("AG_INTERNAL_TLS_KEY", "")
	t.Setenv("AG_INTERNAL_CLIENT_CA", "")
	_, err := internalConfigFromEnv()
	if err == nil || !strings.Contains(err.Error(), "mTLS") {
		t.Fatalf("thiếu chứng thư mà vẫn bật listener Internal: %v", err)
	}
}
