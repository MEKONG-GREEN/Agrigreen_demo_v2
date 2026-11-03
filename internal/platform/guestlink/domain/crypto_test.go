package domain_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/port"
	"github.com/google/uuid"
	"log/slog"
	"strings"
	"testing"
)

func TestSecretsAreNeverImplicitlySerialized(t *testing.T) {
	const raw = "synthetic-bearer-never-in-logs"
	secret := port.NewSecret(raw)
	encoded, err := json.Marshal(struct{ Token port.Secret }{secret})
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("attempt", "token", secret)
	for _, out := range []string{string(encoded), log.String(), fmt.Sprintf("%v %+v %#v", secret, secret, secret)} {
		if strings.Contains(out, raw) || !strings.Contains(out, "redacted") {
			t.Fatalf("secret escaped redaction: %s", out)
		}
	}
	if secret.Reveal() != raw {
		t.Fatal("explicit delivery reveal lost value")
	}
}
func TestOpaqueTokenBindsRoutingHashAndPurpose(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	tenant := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	id := uuid.MustParse("01900000-0000-7000-8000-000000000002")
	a := domain.DerivedToken(key, tenant, id, "link:binding-a")
	b := domain.DerivedToken(key, tenant, id, "session:binding-a")
	got, hash, err := domain.ParseToken(a)
	if err != nil || got != tenant || len(hash) != 64 || len(a.Reveal()) != 64 {
		t.Fatal("invalid routing/token")
	}
	if a.Reveal() == b.Reveal() {
		t.Fatal("token purpose not separated")
	}
	raw, err := base64.RawURLEncoding.DecodeString(a.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 1
	altered := port.NewSecret(base64.RawURLEncoding.EncodeToString(raw))
	other, otherHash, err := domain.ParseToken(altered)
	if err != nil || other == tenant || otherHash == hash {
		t.Fatal("changed tenant not included in token hash")
	}
	for _, value := range []string{"", "1e100000", "1e-100000", strings.Repeat("9", 100000), strings.Repeat("=", 64)} {
		if _, _, err := domain.ParseToken(port.NewSecret(value)); err == nil {
			t.Fatal("malformed token accepted")
		}
	}
}
func TestOTPIsBoundedPlainSixDigits(t *testing.T) {
	code := domain.Code([]byte(strings.Repeat("k", 32)), uuid.New())
	if !domain.ValidCode(code) {
		t.Fatal("generated code invalid")
	}
	for _, value := range []string{"12345", "1234567", "1e100000", "1e-100000", strings.Repeat("9", 100000), "１２３４５６", "123 56"} {
		if domain.ValidCode(port.NewSecret(value)) {
			t.Fatal("invalid code accepted")
		}
	}
}
