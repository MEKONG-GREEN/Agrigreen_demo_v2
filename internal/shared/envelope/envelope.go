// Package envelope là định dạng phong bì mã hóa phiên bản 1 (mục 10.2,
// ADR-0004, docs/dev/envelope-format.md). Chỉ có mật mã, không có logic nghiệp vụ.
//
//	DEK        = 32 byte ngẫu nhiên, mỗi tệp một DEK
//	ciphertext = AES-256-GCM(DEK, nonce 12 byte, không AAD) = bản mã ‖ tag 16 byte
//	wrappedDEK = crypto_box_seal(DEK, khóa công khai X25519 của chủ thể) — 80 byte
//
// Thiết bị hiện trường chỉ gọi được Seal (chỉ có khóa công khai). Open cần khóa
// bí mật — chỉ chạy trong kho định danh.
package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/nacl/box"
)

// Thông số của phiên bản 1.
const (
	Version       = 1
	Alg           = "A256GCM+X25519-SEALEDBOX"
	DEKSize       = 32
	NonceSize     = 12
	TagSize       = 16
	KeySize       = 32
	WrappedDEKLen = box.AnonymousOverhead + DEKSize // 32 + 16 + 32 = 80
)

// Lỗi của package. Không chứa nội dung giải mã.
var (
	ErrOpen      = errors.New("envelope: không mở được (sai khóa hoặc dữ liệu bị sửa)")
	ErrMalformed = errors.New("envelope: sai độ dài trường")
)

// Sealed là phong bì đã mã hóa.
type Sealed struct {
	Nonce      []byte // 12 byte
	Ciphertext []byte // bản mã ‖ tag
	WrappedDEK []byte // 80 byte
}

// Seal mã hóa plaintext cho chủ thể có khóa công khai recipientPub.
func Seal(plaintext []byte, recipientPub *[KeySize]byte) (Sealed, error) {
	return SealFrom(rand.Reader, plaintext, recipientPub)
}

// SealFrom như Seal nhưng lấy ngẫu nhiên từ r (DEK, nonce, khóa tạm của sealed
// box, theo thứ tự đó). Chỉ dùng r khác crypto/rand để sinh bộ vector kiểm thử.
func SealFrom(r io.Reader, plaintext []byte, recipientPub *[KeySize]byte) (Sealed, error) {
	dek := make([]byte, DEKSize)
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(r, dek); err != nil {
		return Sealed{}, fmt.Errorf("envelope: sinh DEK: %w", err)
	}
	if _, err := io.ReadFull(r, nonce); err != nil {
		return Sealed{}, fmt.Errorf("envelope: sinh nonce: %w", err)
	}
	ct, err := EncryptWithDEK(dek, nonce, plaintext)
	if err != nil {
		return Sealed{}, err
	}
	wrapped, err := WrapDEKFrom(r, dek, recipientPub)
	if err != nil {
		return Sealed{}, err
	}
	clear(dek)
	return Sealed{Nonce: nonce, Ciphertext: ct, WrappedDEK: wrapped}, nil
}

// Open mở phong bì bằng cặp khóa của chủ thể.
func Open(s Sealed, pub, priv *[KeySize]byte) ([]byte, error) {
	dek, err := UnwrapDEK(s.WrappedDEK, pub, priv)
	if err != nil {
		return nil, err
	}
	defer clear(dek)
	return DecryptWithDEK(dek, s.Nonce, s.Ciphertext)
}

// EncryptWithDEK = AES-256-GCM(dek, nonce, plaintext), không AAD.
func EncryptWithDEK(dek, nonce, plaintext []byte) ([]byte, error) {
	aead, err := gcm(dek, nonce)
	if err != nil {
		return nil, err
	}
	return aead.Seal(nil, nonce, plaintext, nil), nil
}

// DecryptWithDEK mở bản mã AES-256-GCM.
func DecryptWithDEK(dek, nonce, ciphertext []byte) ([]byte, error) {
	aead, err := gcm(dek, nonce)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < TagSize {
		return nil, ErrMalformed
	}
	pt, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrOpen
	}
	return pt, nil
}

// WrapDEK seal DEK bằng khóa công khai X25519 (crypto_box_seal).
func WrapDEK(dek []byte, recipientPub *[KeySize]byte) ([]byte, error) {
	return WrapDEKFrom(rand.Reader, dek, recipientPub)
}

// WrapDEKFrom như WrapDEK, khóa tạm lấy từ r.
func WrapDEKFrom(r io.Reader, dek []byte, recipientPub *[KeySize]byte) ([]byte, error) {
	if len(dek) != DEKSize || recipientPub == nil {
		return nil, ErrMalformed
	}
	out, err := box.SealAnonymous(nil, dek, recipientPub, r)
	if err != nil {
		return nil, fmt.Errorf("envelope: seal DEK: %w", err)
	}
	return out, nil
}

// UnwrapDEK mở DEK (crypto_box_seal_open) — chỉ gọi trong kho định danh.
func UnwrapDEK(wrapped []byte, pub, priv *[KeySize]byte) ([]byte, error) {
	if len(wrapped) != WrappedDEKLen || pub == nil || priv == nil {
		return nil, ErrMalformed
	}
	dek, ok := box.OpenAnonymous(nil, wrapped, pub, priv)
	if !ok || len(dek) != DEKSize {
		return nil, ErrOpen
	}
	return dek, nil
}

// GenerateKeyPair sinh cặp khóa X25519 của một chủ thể.
func GenerateKeyPair(r io.Reader) (pub, priv *[KeySize]byte, err error) {
	if r == nil {
		r = rand.Reader
	}
	return box.GenerateKey(r)
}

// Key32 chép b (phải đúng 32 byte) sang mảng khóa.
func Key32(b []byte) (*[KeySize]byte, error) {
	if len(b) != KeySize {
		return nil, ErrMalformed
	}
	var k [KeySize]byte
	copy(k[:], b)
	return &k, nil
}

func gcm(key, nonce []byte) (cipher.AEAD, error) {
	if len(key) != DEKSize || len(nonce) != NonceSize {
		return nil, ErrMalformed
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
