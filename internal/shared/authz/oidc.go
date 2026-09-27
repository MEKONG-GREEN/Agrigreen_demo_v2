package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
)

// Verifier xác minh access token OIDC: issuer, audience, chữ ký, hạn (R-54).
// Backend tự xác minh cả token do biên cấp — không tin header do gateway chèn.
type Verifier struct {
	v *oidc.IDTokenVerifier
}

// NewVerifier dùng discovery của issuer (Keycloak realm).
func NewVerifier(ctx context.Context, issuer, audience string) (*Verifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("authz: discovery %s: %w", issuer, err)
	}
	return &Verifier{v: provider.Verifier(&oidc.Config{ClientID: audience, SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256}})}, nil
}

// NewVerifierWithKeys dùng tập khóa có sẵn — cho kiểm thử và môi trường không có discovery.
func NewVerifierWithKeys(issuer, audience string, keys oidc.KeySet) *Verifier {
	return &Verifier{v: oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: audience, SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256}})}
}

type claims struct {
	TenantID    string   `json:"tenant_id"`
	Roles       []string `json:"roles"`
	AMR         []string `json:"amr"`
	AuthTime    int64    `json:"auth_time"`
	RealmAccess struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

// Yếu tố thứ hai được chấp nhận trong amr (RFC 8176).
var secondFactors = []string{"otp", "mfa", "hwk", "swk", "sms"}

// Verify xác minh token Bearer và dựng Principal.
func (v *Verifier) Verify(ctx context.Context, raw string) (Principal, error) {
	tok, err := v.v.Verify(ctx, raw)
	if err != nil {
		return Principal{}, fmt.Errorf("authz: token không hợp lệ: %w", err)
	}
	var c claims
	if err := tok.Claims(&c); err != nil {
		return Principal{}, err
	}
	actor, err := uuid.Parse(tok.Subject)
	if err != nil {
		return Principal{}, errors.New("authz: sub phải là UUID")
	}
	tid, err := uuid.Parse(c.TenantID)
	if err != nil || tid == uuid.Nil {
		return Principal{}, errors.New("authz: token thiếu claim tenant_id")
	}
	roles := append(slices.Clone(c.Roles), c.RealmAccess.Roles...)
	mfa := false
	for _, m := range c.AMR {
		if slices.Contains(secondFactors, strings.ToLower(m)) {
			mfa = true
		}
	}
	p := Principal{ActorRef: actor, TenantID: tid, Roles: roles, MFA: mfa}
	if c.AuthTime > 0 {
		p.AuthTime = time.Unix(c.AuthTime, 0).UTC()
	}
	return p, nil
}
