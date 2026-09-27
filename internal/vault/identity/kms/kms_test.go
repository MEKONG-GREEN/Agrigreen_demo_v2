package kms

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
)

func TestLocalWrapUnwrap(t *testing.T) {
	ctx := context.Background()
	k, err := NewLocal("k1", bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	w, err := k.Wrap(ctx, []byte("bí mật"), []byte("aad-1"))
	if err != nil {
		t.Fatal(err)
	}
	pt, err := k.Unwrap(ctx, "k1", w, []byte("aad-1"))
	if err != nil || string(pt) != "bí mật" {
		t.Fatalf("mở: %v", err)
	}
	if _, err := k.Unwrap(ctx, "k1", w, []byte("aad-2")); !errors.Is(err, ErrUnwrap) {
		t.Fatalf("aad khác phải lỗi, nhận %v", err)
	}
	if _, err := k.Unwrap(ctx, "k2", w, []byte("aad-1")); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("key_id khác phải lỗi, nhận %v", err)
	}
	other, _ := NewLocal("k1", bytes.Repeat([]byte{2}, 32))
	if _, err := other.Unwrap(ctx, "k1", w, []byte("aad-1")); !errors.Is(err, ErrUnwrap) {
		t.Fatalf("KEK khác phải lỗi, nhận %v", err)
	}
}

func TestLocalFromEnv(t *testing.T) {
	t.Setenv(EnvKEK, "")
	if _, err := LocalFromEnv(); err == nil {
		t.Fatal("thiếu biến phải lỗi")
	}
	t.Setenv(EnvKEK, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 16)))
	if _, err := LocalFromEnv(); err == nil {
		t.Fatal("KEK 16 byte phải lỗi")
	}
	t.Setenv(EnvKEK, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)))
	t.Setenv(EnvKEKID, "dev-2026")
	k, err := LocalFromEnv()
	if err != nil || k.KeyID() != "dev-2026" {
		t.Fatalf("%v %v", k, err)
	}
}
