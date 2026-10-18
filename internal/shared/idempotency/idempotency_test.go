package idempotency

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// IDEM-SCOPE-003: dấu khác khi tài nguyên/quyền thay đổi, giữ được retry sau refresh token/trật tự query/vai.
func TestRequestHashAuthorization(t *testing.T) {
	ctx := WithAuthorizationScope(context.Background(), []byte("resolved-scope-a"))
	p := authz.Principal{OrgRef: uuid.New(), Roles: []string{"editor", "reader"}, MFA: true}
	hash := func(c context.Context, who authz.Principal, url string) []byte {
		return requestHash(c, who, httptest.NewRequest("POST", url, nil), []byte(`{"a":1}`))
	}
	first := hash(ctx, p, "/v1/resources/a?x=1&y=2")
	refreshed := p
	refreshed.AuthTime = time.Now()
	refreshed.Roles = []string{"reader", "editor"}
	if !bytes.Equal(first, hash(ctx, refreshed, "/v1/resources/a?y=2&x=1")) {
		t.Fatal("refresh token/thứ tự vai hoặc query không làm đổi quyền")
	}
	for name, tc := range map[string]struct {
		ctx context.Context
		p   authz.Principal
		url string
	}{
		"resource": {ctx, p, "/v1/resources/b?x=1&y=2"},
		"query":    {ctx, p, "/v1/resources/a?x=2&y=2"},
		"scope":    {WithAuthorizationScope(ctx, []byte("resolved-scope-b")), p, "/v1/resources/a?x=1&y=2"},
		"role":     {ctx, authz.Principal{OrgRef: p.OrgRef, Roles: []string{"reader"}, MFA: true}, "/v1/resources/a?x=1&y=2"},
		"org":      {ctx, authz.Principal{OrgRef: uuid.New(), Roles: p.Roles, MFA: true}, "/v1/resources/a?x=1&y=2"},
		"mfa":      {ctx, authz.Principal{OrgRef: p.OrgRef, Roles: p.Roles, MFA: false}, "/v1/resources/a?x=1&y=2"},
	} {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(first, hash(tc.ctx, tc.p, tc.url)) {
				t.Fatal("cache không được vượt tài nguyên/quyền")
			}
		})
	}
	raw := []byte("stable")
	bound := WithAuthorizationScope(ctx, raw)
	before := hash(bound, p, "/a")
	raw[0] = 'X'
	if !bytes.Equal(before, hash(bound, p, "/a")) {
		t.Fatal("người gọi sửa buffer đã gắn vào context")
	}
}
