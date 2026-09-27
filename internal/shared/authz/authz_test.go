package authz_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func TestVerifyBuildsPrincipal(t *testing.T) {
	iss := authztest.New(t)
	sub, tid := ids.New(), ids.New()
	p, err := iss.Verifier().Verify(context.Background(), iss.Sign(t, authztest.Token{Subject: sub, TenantID: tid, Roles: []string{"kn_officer"}, MFA: true}))
	if err != nil {
		t.Fatal(err)
	}
	if p.ActorRef != sub || p.TenantID != tid || !p.Has("kn_officer") || !p.MFA {
		t.Fatalf("principal sai: %+v", p)
	}
}

// R-54: sai issuer, sai audience, hết hạn, sai khóa ký → đều bị từ chối.
func TestVerifyRejects(t *testing.T) {
	iss := authztest.New(t)
	other := authztest.New(t)
	base := authztest.Token{Subject: ids.New(), TenantID: ids.New()}
	cases := map[string]string{
		"issuer":   iss.Sign(t, withIss(base, "https://evil.example/realms/x")),
		"audience": iss.Sign(t, withAud(base, "khac")),
		"expired":  iss.Sign(t, withTTL(base, -time.Minute)),
		"key":      other.Sign(t, base),
		"tenant":   iss.Sign(t, authztest.Token{Subject: ids.New()}),
	}
	for name, tok := range cases {
		if _, err := iss.Verifier().Verify(context.Background(), tok); err == nil {
			t.Errorf("%s: token phải bị từ chối", name)
		}
	}
}

func withIss(t authztest.Token, s string) authztest.Token        { t.Issuer = s; return t }
func withAud(t authztest.Token, s string) authztest.Token        { t.Audience = s; return t }
func withTTL(t authztest.Token, d time.Duration) authztest.Token { t.TTL = d; return t }

// R-56: không tự phê duyệt; người phê duyệt phải có vai param_approver.
func TestSeparationOfDuties(t *testing.T) {
	setter := ids.New()
	self := authz.Principal{ActorRef: setter, Roles: []string{authz.RoleParamSetter, authz.RoleParamApprover}}
	if err := authz.EnsureDistinctApprover(setter, self); !errors.Is(err, authz.ErrSelfApproval) {
		t.Fatalf("tự phê duyệt phải bị chặn, nhận %v", err)
	}
	noRole := authz.Principal{ActorRef: ids.New(), Roles: []string{authz.RoleParamSetter}}
	if err := authz.EnsureDistinctApprover(setter, noRole); problem.From(err).Code != "forbidden" {
		t.Fatalf("thiếu vai phải bị chặn, nhận %v", err)
	}
	ok := authz.Principal{ActorRef: ids.New(), Roles: []string{authz.RoleParamApprover}}
	if err := authz.EnsureDistinctApprover(setter, ok); err != nil {
		t.Fatal(err)
	}
}
