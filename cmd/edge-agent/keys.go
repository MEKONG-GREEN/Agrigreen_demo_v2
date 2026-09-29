package main

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Khóa của thiết bị (ADR-0019 §1):
//   - khóa ký nguồn Ed25519 — sinh và giữ trên thiết bị, PEM PKCS#8, quyền 0600;
//   - khóa + CSR mTLS client ECDSA P-256 (CN = SAN DNS = device_id) — CA nội bộ ký.

func isWindows() bool { return runtime.GOOS == "windows" }

// checkKeyPerm từ chối tệp khóa đọc được bởi nhóm/người khác (Unix).
func checkKeyPerm(path string) error {
	if isWindows() {
		return nil
	}
	fi, err := os.Stat(path) //nolint:gosec // đường dẫn do người vận hành cấu hình
	if err != nil {
		return err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("tệp khóa %s có quyền %o — cần 0600", path, fi.Mode().Perm())
	}
	return nil
}

// writeNewFile tạo tệp mới (không ghi đè) với quyền perm và fsync.
func writeNewFile(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { //nolint:gosec // đường dẫn do người vận hành cấu hình
		return err
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // đường dẫn do người vận hành cấu hình
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// GenerateSourceKey sinh khóa ký nguồn và ghi vào path (0600, không ghi đè).
func GenerateSourceKey(path string) (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	if err := writeNewFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, fmt.Errorf("ghi khóa nguồn: %w", err)
	}
	return priv, nil
}

// LoadSourceKey đọc khóa ký nguồn Ed25519 (PEM PKCS#8). os.ErrNotExist nếu không có.
func LoadSourceKey(path string) (ed25519.PrivateKey, error) {
	b, err := readFile(path)
	if err != nil {
		return nil, err
	}
	if err := checkKeyPerm(path); err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != "PRIVATE KEY" {
		return nil, errors.New("khóa nguồn phải là PEM \"PRIVATE KEY\" (PKCS#8)")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("khóa nguồn: %w", err)
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("khóa nguồn phải là Ed25519")
	}
	return priv, nil
}

// GenerateTLSCSR sinh khóa client mTLS (ECDSA P-256, 0600) và CSR PEM với CN = SAN = deviceID.
func GenerateTLSCSR(keyPath, csrPath, deviceID string) ([]byte, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return nil, err
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: deviceID, Organization: []string{"Mekong Green edge"}},
		DNSNames: []string{deviceID},
	}, k)
	if err != nil {
		return nil, err
	}
	csr := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	if err := writeNewFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, fmt.Errorf("ghi khóa mTLS: %w", err)
	}
	if err := writeNewFile(csrPath, csr, 0o644); err != nil {
		return nil, fmt.Errorf("ghi CSR: %w", err)
	}
	return csr, nil
}

// CertSHA256 là SHA-256 (hex) của chứng thư DER đầu tiên trong tệp PEM — giá trị ghi vào sổ thiết bị.
func CertSHA256(path string) (string, error) {
	b, err := readFile(path)
	if err != nil {
		return "", err
	}
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != "CERTIFICATE" {
		return "", errors.New("tệp chứng thư không có PEM CERTIFICATE")
	}
	h := sha256.Sum256(blk.Bytes)
	return hex.EncodeToString(h[:]), nil
}
