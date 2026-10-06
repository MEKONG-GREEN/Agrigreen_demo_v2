package tenant_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// H4: thiếu bằng chứng cấp quyền → Within từ chối trước khi chạm giao dịch (tx nil không bị dùng).
func TestWithinRejectsZeroGrantWithoutTouchingTx(t *testing.T) {
	ran := false
	for _, g := range []tenant.Grant{{}, tenant.MintGrant(ids.New(), uuid.Nil), tenant.MintGrant(uuid.Nil, ids.New())} {
		err := tenant.Within(context.Background(), nil, g, func() error { ran = true; return nil })
		if !errors.Is(err, tenant.ErrGrantMissing) {
			t.Fatalf("muốn ErrGrantMissing, nhận %v", err)
		}
	}
	if ran {
		t.Fatal("fn không được chạy khi thiếu bằng chứng")
	}
}

// H4: WithGrant chỉ đổi tenant của context khi bằng chứng hợp lệ VÀ tenant hiện có = tenant nguồn.
func TestWithGrant(t *testing.T) {
	a, b := ids.New(), ids.New()
	base := tenant.WithID(context.Background(), a)

	ctx, err := tenant.WithGrant(base, tenant.MintGrant(a, b))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := tenant.FromContext(ctx); got != b {
		t.Fatalf("muốn tenant đích %s, nhận %s", b, got)
	}

	cases := []struct {
		name string
		ctx  context.Context
		g    tenant.Grant
		want error
	}{
		{"zero", base, tenant.Grant{}, tenant.ErrGrantMissing},
		{"nguồn khác", base, tenant.MintGrant(ids.New(), b), tenant.ErrGrantMismatch},
		{"context không tenant", context.Background(), tenant.MintGrant(a, b), tenant.ErrMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tenant.WithGrant(tc.ctx, tc.g)
			if !errors.Is(err, tc.want) {
				t.Fatalf("muốn %v, nhận %v", tc.want, err)
			}
			if got != tc.ctx {
				t.Fatal("lỗi thì phải trả nguyên context cũ")
			}
		})
	}
}

func TestGrantAccessors(t *testing.T) {
	a, b := ids.New(), ids.New()
	g := tenant.MintGrant(a, b)
	if g.From() != a || g.Tenant() != b || !g.Valid() {
		t.Fatalf("bằng chứng sai: %v %v %v", g.From(), g.Tenant(), g.Valid())
	}
	if (tenant.Grant{}).Valid() {
		t.Fatal("Grant zero phải không hợp lệ")
	}
}
