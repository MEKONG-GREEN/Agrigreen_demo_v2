// Package kms là lớp trừu tượng khóa bọc khóa (KEK) của kho định danh.
//
// Khóa bí mật X25519 và khóa dữ liệu của từng chủ thể chỉ lưu dưới dạng đã
// bọc bằng KEK. Production cắm hiện thực KMS/HSM trong lãnh thổ (RB-7) qua
// interface KMS; dev/test dùng Local với KEK đọc từ biến môi trường.
package kms

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

// KMS bọc và mở khóa bằng KEK. aad gắn bản bọc với ngữ cảnh (tenant, chủ thể,
// mục dùng) — mở với aad khác phải thất bại.
type KMS interface {
	// KeyID là định danh KEK hiện hành, lưu cạnh bản bọc để xoay khóa.
	KeyID() string
	Wrap(ctx context.Context, plaintext, aad []byte) ([]byte, error)
	Unwrap(ctx context.Context, keyID string, wrapped, aad []byte) ([]byte, error)
}

// Lỗi của package — không chứa dữ liệu khóa.
var (
	ErrUnknownKey = errors.New("kms: không có KEK với key_id này")
	ErrUnwrap     = errors.New("kms: không mở được bản bọc")
)

// Biến môi trường của Local.
const (
	EnvKEK   = "AG_VAULT_KEK"    // base64 chuẩn của đúng 32 byte
	EnvKEKID = "AG_VAULT_KEK_ID" // tùy chọn, mặc định "local-1"
)

// Local là KMS phần mềm: AES-256-GCM với KEK trong bộ nhớ. CHỈ cho dev/test;
// production dùng KMS/HSM trong lãnh thổ.
type Local struct {
	id   string
	aead cipher.AEAD
}

var _ KMS = (*Local)(nil)

// NewLocal tạo KMS phần mềm từ KEK 32 byte.
func NewLocal(id string, kek []byte) (*Local, error) {
	if len(kek) != 32 {
		return nil, errors.New("kms: KEK phải đúng 32 byte")
	}
	if id == "" {
		id = "local-1"
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Local{id: id, aead: aead}, nil
}

// LocalFromEnv đọc AG_VAULT_KEK (base64) và AG_VAULT_KEK_ID.
func LocalFromEnv() (*Local, error) {
	raw := strings.TrimSpace(os.Getenv(EnvKEK))
	if raw == "" {
		return nil, fmt.Errorf("kms: chưa đặt %s", EnvKEK)
	}
	kek, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("kms: %s phải là base64 chuẩn", EnvKEK)
	}
	return NewLocal(os.Getenv(EnvKEKID), kek)
}

// KeyID trả định danh KEK.
func (l *Local) KeyID() string { return l.id }

// Wrap = nonce 12 byte ‖ AES-256-GCM(KEK, plaintext, aad).
func (l *Local) Wrap(_ context.Context, plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, l.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return l.aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Unwrap mở bản bọc do Wrap tạo.
func (l *Local) Unwrap(_ context.Context, keyID string, wrapped, aad []byte) ([]byte, error) {
	if keyID != l.id {
		return nil, ErrUnknownKey
	}
	n := l.aead.NonceSize()
	if len(wrapped) < n+l.aead.Overhead() {
		return nil, ErrUnwrap
	}
	pt, err := l.aead.Open(nil, wrapped[:n], wrapped[n:], aad)
	if err != nil {
		return nil, ErrUnwrap
	}
	return pt, nil
}
