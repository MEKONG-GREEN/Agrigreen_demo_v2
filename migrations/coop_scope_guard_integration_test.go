package migrations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// savepointBeginner mở "giao dịch" của db.ForModule là một savepoint trong giao dịch quản trị của kiểm thử (dữ
// liệu gieo không COMMIT, cuối kiểm thử ROLLBACK cả).
type savepointBeginner struct{ tx pgx.Tx }

func (b savepointBeginner) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return b.tx.Begin(ctx)
}

// FD (rủi ro R-5 của W3.4, docs/design/cooperative.md §18.4 biện pháp 2): db.ForModule từ chối mở giao dịch cho
// người dùng HTX/xã không có Nút — nếu không, p_scope nhánh "app.node_id rỗng" cho thấy cả tenant. Đi qua đúng
// đường của module (db.WithModuleTenantTx → setLocalContext), vai r_cooperative, chính sách pf 000134 thật.
func TestCoopScopeNodeGuard_FD(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	tx, err := env.AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s := seedScope(ctx, t, tx, true)
	mod := db.Module("pf/cooperative")
	b := savepointBeginner{tx}

	plots := func(c context.Context) ([]uuid.UUID, error) {
		var got []uuid.UUID
		err := db.WithModuleTenantTx(c, b, mod, s.tid, func(t2 pgx.Tx) error {
			got = visible(c, t, t2, `SELECT ref FROM pf_coop.plot`)
			return nil
		})
		asAdmin(ctx, t, tx) // savepoint đã RELEASE: vai r_cooperative còn trong giao dịch ngoài
		return got, err
	}
	who := func(p authz.Principal) context.Context {
		p.TenantID, p.ActorRef = s.tid, ids.New()
		return authz.WithPrincipal(ctx, p)
	}
	all := []uuid.UUID{s.plot1, s.plot2, s.draft1, s.draft2}

	for _, c := range []struct {
		name string
		ctx  context.Context
		want []uuid.UUID // nil + deny: bị chặn
		deny bool
	}{
		{"quản lý HTX không org_ref → 403", who(authz.Principal{Roles: []string{"coop_manager"}}), nil, true},
		{"Cấp Xã (ngữ cảnh KN) không Nút → 403", who(authz.Principal{Roles: []string{"kn_user"},
			KN: &authz.KNContext{Role: authz.KNXa, Department: "TT", Communes: []string{xa1}}}), nil, true},
		{"cán bộ KN (realm) không org_ref → 403", who(authz.Principal{Roles: []string{"kn_officer"}}), nil, true},
		{"quản lý HTX C1 → phạm vi C1", who(authz.Principal{Roles: []string{"coop_manager"}, OrgRef: s.c1}),
			[]uuid.UUID{s.plot1, s.draft1, s.draft2}, false},
		{"Cấp Xã X1, xã 10001 → phạm vi xã", who(authz.Principal{Roles: []string{"kn_user"},
			KN: &authz.KNContext{Role: authz.KNXa, Department: "TT", OrgRef: s.x1, Communes: []string{xa1}}}), []uuid.UUID{s.plot1, s.draft1}, false},
		{"Admin KN không Nút → cả tenant", who(authz.Principal{Roles: []string{"kn_user"}, KN: &authz.KNContext{Role: authz.KNAdmin}}), all, false},
		{"platform_admin không org_ref → cả tenant", who(authz.Principal{Roles: []string{authz.RolePlatformAdmin, "coop_manager"}}), all, false},
		{"consumer/job (không Principal) → cả tenant", ctx, all, false},
	} {
		got, err := plots(c.ctx)
		if c.deny {
			pb := problem.From(err)
			if !errors.Is(err, db.ErrNodeScopeRequired) || pb.Status != 403 || pb.Code != "node_scope_required" || got != nil {
				t.Errorf("%s: lỗi %v (%d %s), thửa %v — muốn 403 node_scope_required", c.name, err, pb.Status, pb.Code, got)
			}
			continue
		}
		if err != nil || !sameSet(got, c.want...) {
			t.Errorf("%s: thửa %v, lỗi %v — muốn %v", c.name, got, err, c.want)
		}
	}

	// db.WithoutNodeScope vẫn dùng được trong giao dịch của người dùng có Nút (kiểm chồng lấn giữa HTX).
	c1 := who(authz.Principal{Roles: []string{"coop_manager"}, OrgRef: s.c1})
	if err := db.WithModuleTenantTx(c1, b, mod, s.tid, func(t2 pgx.Tx) error {
		return db.WithoutNodeScope(c1, t2, func() error {
			if got := visible(c1, t, t2, `SELECT ref FROM pf_coop.plot`); !sameSet(got, all...) {
				t.Errorf("WithoutNodeScope: %v, muốn cả tenant", got)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	asAdmin(ctx, t, tx)
}
