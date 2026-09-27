package app

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"io"
)

// errField: không mở được một trường định danh (sai khóa hoặc bản mã bị sửa).
var errField = errors.New("vault: không giải mã được trường định danh")

// sealField = nonce 12 byte ‖ AES-256-GCM(khóa dữ liệu của chủ thể, giá trị, aad).
// Giá trị rỗng → nil (cột NULL).
func sealField(r io.Reader, key, aad []byte, value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(r, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, []byte(value), aad), nil
}

// openField mở trường do sealField tạo. nil → "".
func openField(key, aad, ct []byte) (string, error) {
	if ct == nil {
		return "", nil
	}
	aead, err := newGCM(key)
	if err != nil {
		return "", err
	}
	n := aead.NonceSize()
	if len(ct) < n+aead.Overhead() {
		return "", errField
	}
	pt, err := aead.Open(nil, ct[:n], ct[n:], aad)
	if err != nil {
		return "", errField
	}
	return string(pt), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("vault: khóa dữ liệu phải 32 byte")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
