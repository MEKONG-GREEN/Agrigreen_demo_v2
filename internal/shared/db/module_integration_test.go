package db_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Hai module mẫu của W2.3 (ADR-0028 §4 bước 1).
var (
	modContent = db.Module("pf/content")
	modNotify  = db.Module("pf/notify")
)

// ownTable / otherTable: bảng của chính module và của module kia.
var ownTable = map[string]string{"r_content": "pf_content.document", "r_notify": "pf_notify.notification"}

func otherOf(m db.ModuleID) db.ModuleID {
	if m == modContent {
		return modNotify
	}
	return modContent
}

// execModes: QueryExecModeExec (như qua PgBouncer, AG_TEST_PGBOUNCER_URL nếu có) và statement cache mặc định
// của pgx (nối thẳng) — quyền phải kiểm lúc thực thi theo vai hiện hành ở cả hai (ADR-0028 §3).
var execModes = []struct {
	name    string
	bouncer bool
}{{"exec", true}, {"statement-cache", false}}

// pool mở pool riêng maxConns kết nối của vai role (để chắc chắn tái dùng cùng kết nối máy chủ).
func pool(t *testing.T, role string, bouncer bool, maxConns int32) *pgxpool.Pool {
	t.Helper()
	env := pgtest.Get(t)
	base := env.DirectURL
	if bouncer {
		base = env.PoolURL
	}
	dsn, err := dbadmin.RoleURL(base, migrations.PF, env.Suffix, role)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.Open(context.Background(), dsn, db.Options{ViaPgBouncer: bouncer, MaxConns: maxConns, AppName: "w23-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// T4 (ADR-0028 §3): trong giao dịch current_user = r_<module>, session_user = vai đăng nhập, tenant đặt
// cục bộ; sau COMMIT/ROLLBACK kết nối về vai đăng nhập, không ngữ cảnh. Câu lệnh đã chuẩn bị trong giao dịch
// (statement cache) chạy lại ngoài giao dịch bị từ chối: quyền kiểm theo vai hiện hành lúc thực thi.
// Vai đăng nhập cũ content_app (kỳ chuyển) cũng SET LOCAL ROLE r_content được.
func TestWithContextTxSetsLocalRole_T4(t *testing.T) {
	for _, mode := range execModes {
		for _, login := range []string{dbadmin.AppPlatform, "content_app"} {
			t.Run(mode.name+"/"+login, func(t *testing.T) {
				p := pool(t, login, mode.bouncer, 1)
				tid := ids.New()
				ctx := tenant.WithID(context.Background(), tid)
				countSQL := "SELECT count(*) FROM " + ownTable["r_content"]
				err := db.WithContextTx(ctx, p, modContent, func(tx pgx.Tx) error {
					var cur, sess, ten string
					if err := tx.QueryRow(ctx, "SELECT current_user, session_user, current_setting('app.tenant_id', true)").Scan(&cur, &sess, &ten); err != nil {
						return err
					}
					if cur != "r_content" || sess != login || ten != tid.String() {
						return fmt.Errorf("trong giao dịch: current_user=%s session_user=%s tenant=%s", cur, sess, ten)
					}
					var n int
					return tx.QueryRow(ctx, countSQL).Scan(&n)
				})
				if err != nil {
					t.Fatal(err)
				}
				// Rollback cũng trả vai.
				boom := errors.New("lỗi giả")
				if err := db.WithContextTx(ctx, p, modContent, func(pgx.Tx) error { return boom }); !errors.Is(err, boom) {
					t.Fatalf("muốn lỗi fn, nhận %v", err)
				}
				var cur, ten string
				if err := p.QueryRow(context.Background(), "SELECT current_user, coalesce(current_setting('app.tenant_id', true), '')").Scan(&cur, &ten); err != nil {
					t.Fatal(err)
				}
				if cur != login || ten != "" {
					t.Fatalf("sau giao dịch: current_user=%s tenant=%q (muốn %s, rỗng)", cur, ten, login)
				}
				if login == dbadmin.AppPlatform {
					var n int
					if err := p.QueryRow(context.Background(), countSQL).Scan(&n); pgCode(err) != "42501" {
						t.Fatalf("câu lệnh đã chuẩn bị chạy lại ngoài WithContextTx phải 42501, nhận %v", err)
					}
				}
			})
		}
	}
	if err := db.WithContextTx(context.Background(), pool(t, dbadmin.AppPlatform, true, 1), modContent, func(pgx.Tx) error { return nil }); !errors.Is(err, tenant.ErrMissing) {
		t.Fatalf("thiếu tenant trong ctx phải ErrMissing, nhận %v", err)
	}
}

// T5 (ADR-0028 §3): nhiều module chạy song song trên ít kết nối tái dùng — mỗi giao dịch chỉ thấy bảng của
// module mình, không đọc được bảng của module kia (42501), vai không rò giữa giao dịch.
func TestModulesIsolatedOnSharedConnections_T5(t *testing.T) {
	for _, mode := range execModes {
		t.Run(mode.name, func(t *testing.T) {
			p := pool(t, dbadmin.AppPlatform, mode.bouncer, 2)
			ctx := tenant.WithID(context.Background(), ids.New())
			var wg sync.WaitGroup
			errc := make(chan error, 8)
			for g := 0; g < 8; g++ {
				wg.Add(1)
				go func(g int) {
					defer wg.Done()
					for i := 0; i < 20; i++ {
						m := modContent
						if (g+i)%2 == 1 {
							m = modNotify
						}
						if err := checkIsolated(ctx, p, m); err != nil {
							errc <- err
							return
						}
					}
				}(g)
			}
			wg.Wait()
			close(errc)
			for err := range errc {
				t.Error(err)
			}
		})
	}
}

func checkIsolated(ctx context.Context, p *pgxpool.Pool, m db.ModuleID) error {
	return db.WithContextTx(ctx, p, m, func(tx pgx.Tx) error {
		var cur string
		var n int
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&cur); err != nil {
			return err
		}
		if cur != m.Role() {
			return fmt.Errorf("%s: current_user=%s", m.Name(), cur)
		}
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+ownTable[m.Role()]).Scan(&n); err != nil {
			return fmt.Errorf("%s đọc bảng của mình: %w", m.Name(), err)
		}
		sp, err := tx.Begin(ctx)
		if err != nil {
			return err
		}
		err = sp.QueryRow(ctx, "SELECT count(*) FROM "+ownTable[otherOf(m).Role()]).Scan(&n)
		_ = sp.Rollback(ctx)
		if pgCode(err) != "42501" {
			return fmt.Errorf("%s đọc bảng của %s phải 42501, nhận %w", m.Name(), otherOf(m).Name(), err)
		}
		return nil
	})
}

// ADR-0028 §1–2: vai đăng nhập NOINHERIT tự nó không có quyền bảng nào (kể cả shared.*) — truy vấn ngoài
// WithContextTx bị từ chối; vai đăng nhập chỉ SET ROLE được sang r_* của module đơn vị mình ghép.
func TestLoginRoleAloneHasNoTablePrivileges(t *testing.T) {
	env := pgtest.Get(t)
	ctx := context.Background()
	p := pool(t, dbadmin.AppPlatform, true, 1)
	for _, q := range []string{"SELECT count(*) FROM pf_content.document", "SELECT count(*) FROM pf_notify.notification", "SELECT count(*) FROM shared.outbox"} {
		var n int
		if err := p.QueryRow(ctx, q).Scan(&n); pgCode(err) != "42501" {
			t.Errorf("%s ngoài WithContextTx phải 42501, nhận %v", q, err)
		}
	}
	admin := env.AdminPool(t, migrations.PF)
	for _, u := range dbadmin.LoginUnits {
		var n int
		err := admin.QueryRow(ctx, `
			SELECT count(*) FROM pg_class c JOIN pg_namespace ns ON ns.oid = c.relnamespace
			 WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')
			   AND (ns.nspname LIKE 'pf\_%' OR ns.nspname IN ('shared', 'k_ref'))
			   AND (CASE WHEN c.relkind = 'S' THEN has_sequence_privilege($1, c.oid, 'USAGE, SELECT, UPDATE')
			             ELSE has_table_privilege($1, c.oid, 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE')
			                  OR has_any_column_privilege($1, c.oid, 'SELECT, INSERT, UPDATE') END)`, u.Role).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s có quyền trên %d bảng (muốn 0)", u.Role, n)
		}
	}
	// app_public không ghép pf/notify → không SET ROLE r_notify được.
	pub := pool(t, dbadmin.AppPublic, true, 1)
	err := db.WithContextTx(tenant.WithID(ctx, ids.New()), pub, modNotify, func(pgx.Tx) error { return nil })
	if pgCode(err) != "42501" {
		t.Fatalf("app_public SET LOCAL ROLE r_notify phải 42501, nhận %v", err)
	}
}

// Registry: module lạ → panic lúc khởi tạo, không chạy được với vai đoán.
func TestModuleUnknownPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("db.Module với module lạ phải panic")
		}
	}()
	_ = db.Module("pf/khong_co")
}
