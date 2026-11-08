package envelope

import (
	"bytes"
	"testing"
)

func TestSupportEnvelopeAAD274(t *testing.T) {
	pub, priv, err := GenerateKeyPair(nil)
	if err != nil {
		t.Fatal(err)
	}
	aad := []byte("kn.account_support/v1:tenant-a:request-a")
	sealed, err := SealWithAAD([]byte("synthetic support content"), aad, pub)
	if err != nil {
		t.Fatal(err)
	}
	dek, err := UnwrapDEK(sealed.WrappedDEK, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(dek)
	if got, err := DecryptWithDEKAndAAD(dek, sealed.Nonce, sealed.Ciphertext, aad); err != nil || !bytes.Equal(got, []byte("synthetic support content")) {
		t.Fatalf("roundtrip: %q %v", got, err)
	}
	for _, wrong := range [][]byte{nil, []byte("kn.account_support/v1:tenant-b:request-a"), []byte("kn.account_support/v1:tenant-a:request-b"), []byte("other-purpose:tenant-a:request-a")} {
		if _, err := DecryptWithDEKAndAAD(dek, sealed.Nonce, sealed.Ciphertext, wrong); err == nil {
			t.Fatal("ciphertext accepted with changed binding")
		}
	}
}
