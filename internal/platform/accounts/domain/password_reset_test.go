package domain

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestResetTokenRoundTripAndTamper(t *testing.T) {
	tid, id := uuid.New(), uuid.New()
	tok, hash, err := NewResetToken(tid, id)
	if err != nil || len(tok) != 86 || len(hash) != sha256.Size {
		t.Fatalf("mã %q băm %d: %v", tok, len(hash), err)
	}
	gt, gid, got, ok := ParseResetToken(tok)
	if !ok || gt != tid || gid != id || !ResetTokenMatches(hash, got) {
		t.Fatal("tách mã không khớp")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(tok)
	if strings.Contains(string(hash), string(raw[32:])) {
		t.Fatal("băm không được chứa bí mật")
	}
	other, _, _ := NewResetToken(tid, id)
	_, _, got2, _ := ParseResetToken(other)
	if ResetTokenMatches(hash, got2) || ResetTokenMatches(nil, got) {
		t.Fatal("hai mã khác nhau không được khớp")
	}
	for _, bad := range []string{"", "abc", tok[:85], tok + "A", strings.Repeat("A", 86), tok[:85] + "B"} {
		if _, _, _, ok := ParseResetToken(bad); ok {
			t.Fatalf("mã sai định dạng được nhận: %q", bad)
		}
	}
}

func TestNormalizeResetEmail(t *testing.T) {
	for in, want := range map[string]string{" A.B@Kn.Example.VN ": "a.b@kn.example.vn", "x@y.vn": "x@y.vn"} {
		if got, ok := NormalizeResetEmail(in); !ok || got != want {
			t.Fatalf("%q → %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "khong-email", "a@b", "a@@b.vn", "a b@c.vn", "@c.vn", "a@.vn", "a@vn.", strings.Repeat("a", 250) + "@x.vn", "a\x00@b.vn"} {
		if _, ok := NormalizeResetEmail(bad); ok {
			t.Fatalf("email sai được nhận: %q", bad)
		}
	}
}
