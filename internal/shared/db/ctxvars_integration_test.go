package db_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ctxRow đọc mọi biến ngữ cảnh qua k_ref.ctx_* (pf 000091) — NULL thành "<nil>".
const ctxRowSQL = `SELECT current_user,
	coalesce(k_ref.ctx_uuid('app.tenant_id')::text, '<nil>'),
	coalesce(k_ref.ctx_uuid('app.node_id')::text, '<nil>'),
	coalesce(k_ref.ctx_text('app.dept'), '<nil>'),
	coalesce(array_to_string(k_ref.ctx_text('app.xa_ids')::text[], '|'), '<nil>'),
	coalesce(k_ref.ctx_text('app.purpose'), '<nil>'),
	coalesce(k_ref.ctx_uuid('app.actor_ref')::text, '<nil>'),
	coalesce(k_ref.ctx_uuid('app.on_behalf_of_ref')::text, '<nil>')`

func ctxRow(ctx context.Context, tx pgx.Tx) (string, error) {
	v := make([]string, 8)
	ptrs := make([]any, len(v))
	for i := range v {
		ptrs[i] = &v[i]
	}
	if err := tx.QueryRow(ctx, ctxRowSQL).Scan(ptrs...); err != nil {
		return "", err
	}
	return strings.Join(v, " "), nil
}

// W2.4 (ADR-0028 §2, DB-13): giao dịch của module đã chuyển (pf/content trên app_platform) thấy biến ngữ
// cảnh từ Principal/KN qua k_ref.ctx_*; sau COMMIT và sau ROLLBACK, giao dịch kế tiếp trên CÙNG kết nối
// (pool 1 kết nối) không thấy giá trị cũ — biến vắng → NULL; ngoài giao dịch current_setting rỗng.
func TestContextVarsVisibleAndReset_W24(t *testing.T) {
	for _, mode := range execModes {
		t.Run(mode.name, func(t *testing.T) {
			p := pool(t, dbadmin.AppPlatform, mode.bouncer, 1)
			tid, actor, node, helped := ids.New(), ids.New(), ids.New(), ids.New()
			base := tenant.WithID(context.Background(), tid)
			full := authz.WithPurpose(authz.WithPrincipal(base, authz.Principal{ActorRef: actor, TenantID: tid, OrgRef: ids.New(),
				KN: &authz.KNContext{Role: authz.KNXa, Department: "TT", OrgRef: node, Communes: []string{"30280", "30283"},
					Support: &authz.KNSupport{SessionID: ids.New(), OnBehalfOf: helped}}}), "khuyen-nong")
			want := fmt.Sprintf("r_content %s %s TT 30280|30283 khuyen-nong %s %s", tid, node, actor, helped)
			empty := fmt.Sprintf("r_content %s <nil> <nil> <nil> <nil> <nil> <nil>", tid)

			check := func(ctx context.Context, want string, fnErr error) {
				t.Helper()
				err := db.WithContextTx(ctx, p, modContent, func(tx pgx.Tx) error {
					got, err := ctxRow(ctx, tx)
					if err != nil {
						return err
					}
					if got != want {
						return fmt.Errorf("trong giao dịch:\n nhận %s\n muốn %s", got, want)
					}
					return fnErr
				})
				if !errors.Is(err, fnErr) {
					t.Fatal(err)
				}
			}
			check(full, want, nil)  // COMMIT
			check(base, empty, nil) // cùng kết nối: không còn giá trị cũ
			boom := errors.New("lỗi giả")
			check(full, want, boom) // ROLLBACK
			check(base, empty, nil)

			// Ngoài giao dịch: vai đăng nhập, biến rỗng ('' trên kết nối đã từng đặt — DB-13).
			var cur, rest string
			if err := p.QueryRow(context.Background(), `SELECT current_user, concat_ws('|',
				coalesce(current_setting('app.node_id', true), ''), coalesce(current_setting('app.actor_ref', true), ''),
				coalesce(current_setting('app.xa_ids', true), ''), coalesce(current_setting('app.on_behalf_of_ref', true), ''))`).Scan(&cur, &rest); err != nil {
				t.Fatal(err)
			}
			if cur != dbadmin.AppPlatform || rest != "|||" {
				t.Fatalf("sau giao dịch: current_user=%s biến=%q", cur, rest)
			}
		})
	}
}

// k_ref.ctx_*: rỗng → NULL, uuid sai dạng → NULL (không lỗi), tên ngoài app.* → 22023; hai hàm được inline.
func TestCtxFunctions_W24(t *testing.T) {
	p := pool(t, dbadmin.AppPlatform, true, 1)
	ctx := tenant.WithID(context.Background(), ids.New())
	err := db.WithContextTx(ctx, p, modContent, func(tx pgx.Tx) error {
		up := strings.ToUpper(uuid.NewString())
		for _, c := range []struct{ val, wantUUID, wantText string }{
			{"", "<nil>", "<nil>"},
			{"khong-phai-uuid", "<nil>", "khong-phai-uuid"},
			{"0190a5c3-7b1e-7cc0-8f00-00000000000", "<nil>", "0190a5c3-7b1e-7cc0-8f00-00000000000"},
			{"{0190a5c3-7b1e-7cc0-8f00-000000000000}", "<nil>", "{0190a5c3-7b1e-7cc0-8f00-000000000000}"},
			{up, strings.ToLower(up), up},
		} {
			var u, s string
			if err := tx.QueryRow(ctx, `SELECT set_config('app.w24_probe', $1, true)`, c.val).Scan(new(string)); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT coalesce(k_ref.ctx_uuid('app.w24_probe')::text, '<nil>'),
				coalesce(k_ref.ctx_text('app.w24_probe'), '<nil>')`).Scan(&u, &s); err != nil {
				return fmt.Errorf("%q: %w", c.val, err)
			}
			if u != c.wantUUID || s != c.wantText {
				return fmt.Errorf("%q: ctx_uuid=%s ctx_text=%s, muốn %s %s", c.val, u, s, c.wantUUID, c.wantText)
			}
		}
		// Biến chưa từng đặt trong phiên → NULL.
		var n string
		if err := tx.QueryRow(ctx, `SELECT coalesce(k_ref.ctx_text('app.chua_tung_dat'), '<nil>')`).Scan(&n); err != nil || n != "<nil>" {
			return fmt.Errorf("biến chưa đặt: %q %w", n, err)
		}
		// Tên ngoài app.* bị từ chối.
		for _, q := range []string{`SELECT k_ref.ctx_text('role')`, `SELECT k_ref.ctx_uuid('search_path')`,
			`SELECT k_ref.ctx_text('app.')`, `SELECT k_ref.ctx_text('APP.node_id')`, `SELECT k_ref.ctx_text(NULL)`} {
			sp, err := tx.Begin(ctx)
			if err != nil {
				return err
			}
			var s *string
			err = sp.QueryRow(ctx, q).Scan(&s)
			_ = sp.Rollback(ctx)
			if pgCode(err) != "22023" {
				return fmt.Errorf("%s phải 22023, nhận %w", q, err)
			}
		}
		// Inline (§17.2): kế hoạch chỉ còn current_setting, không gọi hàm k_ref.
		rows, err := tx.Query(ctx, `EXPLAIN (VERBOSE, COSTS OFF) SELECT k_ref.ctx_uuid('app.node_id'), k_ref.ctx_text('app.dept')`)
		if err != nil {
			return err
		}
		plan, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if s := strings.Join(plan, "\n"); strings.Contains(s, "ctx_") || !strings.Contains(s, "current_setting") {
			return fmt.Errorf("k_ref.ctx_* không được inline:\n%s", s)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Quyền (pf 000091): EXECUTE chỉ cho r_* (vai cũ <module>_app có qua membership), không cho PUBLIC hay vai
// đăng nhập NOINHERIT tự nó.
func TestCtxFunctionGrants_W24(t *testing.T) {
	env := pgtest.Get(t)
	admin := env.AdminPool(t, migrations.PF)
	ctx := context.Background()
	for role, want := range map[string]bool{"r_content": true, "r_notify": true, "content_app": true, dbadmin.AppPlatform: false, "public": false} {
		for _, fn := range []string{"k_ref.ctx_uuid(text)", "k_ref.ctx_text(text)"} {
			var got bool
			q := `SELECT has_function_privilege($1, $2::regprocedure, 'EXECUTE')`
			args := []any{role, fn}
			if role == "public" {
				q = `SELECT EXISTS (SELECT 1 FROM pg_proc p, aclexplode(p.proacl) a WHERE p.oid = $1::regprocedure AND a.grantee = 0)`
				args = []any{fn}
			}
			if err := admin.QueryRow(ctx, q, args...).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("%s EXECUTE %s = %v, muốn %v", role, fn, got, want)
			}
		}
	}
}
