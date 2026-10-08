// Package testpki sinh PKI THỬ cho kiểm thử mTLS của cổng tích hợp: CA đối tác, chứng thư máy chủ, chứng chỉ
// client — tạo lúc chạy trong thư mục tạm, không bao giờ có khóa nào trong repo.
package testpki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// CA là CA thử.
type CA struct {
	Cert *x509.Certificate
	Key  *ecdsa.PrivateKey
	PEM  []byte
}

var serial atomic.Int64

func next() *big.Int { return big.NewInt(serial.Add(1) + 1) }

// NewCA sinh CA tự ký.
func NewCA(t testing.TB, cn string) *CA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: next(), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return &CA{Cert: c, Key: key, PEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// Issue ký chứng thư lá (server: DNS localhost + IP 127.0.0.1, EKU serverAuth; còn lại EKU clientAuth).
func (ca *CA) Issue(t testing.TB, cn string, server bool) (tls.Certificate, *x509.Certificate, []byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: next(), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	if server {
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tpl.DNSNames = []string{"localhost"}
		tpl.IPAddresses = []net.IP{net.IPv4(127, 0, 0, 1)}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return pair, leaf, certPEM, keyPEM
}

// Write ghi tệp vào dir và trả đường dẫn.
func Write(t testing.TB, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// ServerFiles sinh chứng thư máy chủ (do ca ký) và ghi cert/key/CA client vào dir: trả đường dẫn
// (cert, key, clientCA) cho gateway.ServerTLS.
func ServerFiles(t testing.TB, dir string, serverCA, clientCA *CA) (string, string, string) {
	t.Helper()
	_, _, certPEM, keyPEM := serverCA.Issue(t, "integration-gateway", true)
	return Write(t, dir, "tls.crt", certPEM), Write(t, dir, "tls.key", keyPEM), Write(t, dir, "client-ca.crt", clientCA.PEM)
}

// Client dựng http.Client tin serverCA, mang chứng chỉ client nếu có.
func Client(serverCA *CA, cert *tls.Certificate) *http.Client {
	pool := x509.NewCertPool()
	pool.AddCert(serverCA.Cert)
	cfg := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}, Timeout: 10 * time.Second}
}
