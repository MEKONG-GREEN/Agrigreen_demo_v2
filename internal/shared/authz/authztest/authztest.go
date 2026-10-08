// Package authztest phát token giả cho kiểm thử — không bao giờ import từ mã chạy thật.
package authztest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// Issuer và Audience của token kiểm thử.
const (
	Issuer   = "https://auth.test.agrigreen.local/realms/agrigreen"
	Audience = "agrigreen-api"
)

// Issuer giả.
type FakeIssuer struct {
	key    *rsa.PrivateKey
	signer jose.Signer
}

// New tạo issuer với khóa RSA mới.
func New(t testing.TB) *FakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	return &FakeIssuer{key: key, signer: s}
}

// Verifier tin khóa của issuer giả.
func (f *FakeIssuer) Verifier() *authz.Verifier {
	return authz.NewVerifierWithKeys(Issuer, Audience, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{f.key.Public()}})
}

// Token mô tả token cần phát.
type Token struct {
	Subject  uuid.UUID
	TenantID uuid.UUID
	Roles    []string
	MFA      bool
	// AMR thay amr mặc định (["pwd"] + "otp" khi MFA) — kiểm chính sách yếu tố thứ hai.
	AMR      []string
	OrgRef   uuid.UUID
	Issuer   string // rỗng = Issuer
	Audience string // rỗng = Audience
	TTL      time.Duration
}

// Sign phát token đã ký.
func (f *FakeIssuer) Sign(t testing.TB, tk Token) string {
	t.Helper()
	iss, aud, ttl := tk.Issuer, tk.Audience, tk.TTL
	if iss == "" {
		iss = Issuer
	}
	if aud == "" {
		aud = Audience
	}
	if ttl == 0 {
		ttl = 5 * time.Minute
	}
	now := time.Now()
	std := jwt.Claims{
		Issuer: iss, Subject: tk.Subject.String(), Audience: jwt.Audience{aud},
		IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(ttl)),
	}
	amr := []string{"pwd"}
	if tk.MFA {
		amr = append(amr, "otp")
	}
	if tk.AMR != nil {
		amr = tk.AMR
	}
	extra := map[string]any{"tenant_id": tk.TenantID.String(), "realm_access": map[string]any{"roles": tk.Roles}, "amr": amr, "auth_time": now.Unix()}
	if tk.OrgRef != uuid.Nil {
		extra["org_ref"] = tk.OrgRef.String()
	}
	raw, err := jwt.Signed(f.signer).Claims(std).Claims(extra).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
