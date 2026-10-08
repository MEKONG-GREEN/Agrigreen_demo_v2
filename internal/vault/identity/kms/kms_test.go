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

// fakeHSM giả lập hiện thực KMS/HSM thật: không cài Softwarer.
type fakeHSM struct{ KMS }

func TestFromEnvProvider(t *testing.T) {
	kek := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	for _, p := range []string{"", "local", " LOCAL "} {
		k, name, err := FromEnv(env(map[string]string{EnvProvider: p, EnvKEK: kek}))
		if err != nil || name != ProviderLocal || !IsSoftware(k) {
			t.Fatalf("AG_VAULT_KMS=%q: %v %q %v", p, k, name, err)
		}
	}
	if _, _, err := FromEnv(env(map[string]string{EnvProvider: "local"})); err == nil {
		t.Fatal("local thiếu KEK phải lỗi")
	}
	_, name, err := FromEnv(env(map[string]string{EnvProvider: "vendor-x", EnvKEK: kek}))
	if !errors.Is(err, ErrUnknownProvider) || name != "vendor-x" {
		t.Fatalf("nhà cung cấp chưa hỗ trợ phải lỗi ErrUnknownProvider, nhận %q %v", name, err)
	}
	if IsSoftware(fakeHSM{}) {
		t.Fatal("hiện thực không cài Softwarer phải được coi là KMS thật")
	}
}
