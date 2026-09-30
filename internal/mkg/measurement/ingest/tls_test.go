package ingest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type pkiCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newPKICA(t *testing.T, cn string) pkiCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return pkiCA{cert: c, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func (ca pkiCA) issue(t *testing.T, cn string, usage x509.ExtKeyUsage) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn}, DNSNames: []string{cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// CA thiết bị riêng (G3, runbook device-pki): ingest chỉ tin CA thiết bị cho client auth;
// CA dịch vụ nội bộ (đã ký chứng thư máy chủ) nằm trong AG_INGEST_CLIENT_CA → từ chối khởi động.
func TestRequireSeparateDeviceCA(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	internal := newPKICA(t, "agrigreen-dev-internal-ca")
	device := newPKICA(t, "agrigreen-dev-device-ca")
	srv := write("ingest.crt", internal.issue(t, "ingest", x509.ExtKeyUsageServerAuth))

	if err := RequireSeparateDeviceCA(srv, write("device-ca.crt", device.pem)); err != nil {
		t.Fatalf("CA thiết bị riêng phải được nhận: %v", err)
	}
	if err := RequireSeparateDeviceCA(srv, write("ca.crt", internal.pem)); !errors.Is(err, ErrSharedDeviceCA) {
		t.Fatalf("CA dịch vụ nội bộ làm CA client phải bị từ chối, nhận %v", err)
	}
	// Gộp PEM khi chuyển CA thiết bị (runbook): cả hai đều phải là CA thiết bị — lẫn CA nội bộ → từ chối.
	both := append(append([]byte{}, device.pem...), internal.pem...)
	if err := RequireSeparateDeviceCA(srv, write("bundle.crt", both)); !errors.Is(err, ErrSharedDeviceCA) {
		t.Fatalf("bó CA lẫn CA nội bộ phải bị từ chối, nhận %v", err)
	}
	device2 := newPKICA(t, "agrigreen-dev-device-ca-2")
	rotate := append(append([]byte{}, device.pem...), device2.pem...)
	if err := RequireSeparateDeviceCA(srv, write("rotate.crt", rotate)); err != nil {
		t.Fatalf("hai CA thiết bị (chuyển CA) phải được nhận: %v", err)
	}
	// Chứng thư lá (không phải CA) trong AG_INGEST_CLIENT_CA → từ chối.
	leafOnly := write("leaf.crt", device.issue(t, "edge-sim-01", x509.ExtKeyUsageClientAuth))
	if err := RequireSeparateDeviceCA(srv, leafOnly); err == nil {
		t.Fatal("chứng thư lá trong AG_INGEST_CLIENT_CA phải bị từ chối")
	}
	if err := RequireSeparateDeviceCA(srv, write("empty.crt", []byte("không phải PEM"))); err == nil {
		t.Fatal("tệp không có PEM phải bị từ chối")
	}
}
