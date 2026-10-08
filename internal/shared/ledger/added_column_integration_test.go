package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

var errRollback = errors.New("rollback")

// ADR-0012 "Cột thêm vào sổ" (shared 000011): ADD COLUMN trên sổ đã có dòng. Băm cả dòng (to_jsonb)
// làm dòng cũ hiện khóa mới = null → verify_chain báo đứt giả. Cột NULL được, không DEFAULT, đăng ký
// bằng shared.register_ledger_columns → dòng cũ xác minh sạch; mọi sửa đổi giá trị vẫn bị bắt.
func TestLedgerAddColumnKeepsChainVerifiable(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	owner := env.Pool(t, migrations.MKG, dbadmin.Migrator)
	app := appPool(t)
	schema := "fxt_" + ids.New().String()[24:]
	table := schema + ".l"
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := owner.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	mustExec("CREATE SCHEMA " + schema)
	t.Cleanup(func() { _, _ = owner.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	mustExec(`CREATE TABLE ` + table + ` (
	    id uuid NOT NULL, tenant_id uuid NOT NULL, stream_key text NOT NULL, seq bigint NOT NULL,
	    payload integer NOT NULL, recorded_at timestamptz NOT NULL DEFAULT now(), idem_key text NOT NULL,
	    prev_hash bytea NOT NULL, row_hash bytea NOT NULL, PRIMARY KEY (id, recorded_at)
	  ) PARTITION BY RANGE (recorded_at)`)
	mustExec(`SELECT shared.apply_ledger($1::regclass, $1::text)`, table)
	mustExec("GRANT USAGE ON SCHEMA " + schema + " TO fieldops_app")
	mustExec("GRANT SELECT ON " + table + " TO fieldops_app")

	tid := ids.New()
	stream := "s:" + ids.New().String()
	ins := func(payload int, cols string, vals ...any) {
		t.Helper()
		ph := ""
		for i := range vals {
			ph += fmt.Sprintf(", $%d", 6+i)
		}
		if err := tenant.WithTenantTx(ctx, owner, tid, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO "+table+" (id, tenant_id, stream_key, payload, idem_key"+cols+
				") VALUES ($1, $2, $3, $4, $5"+ph+")", append([]any{ids.New(), tid, stream, payload, ids.New().String()}, vals...)...)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	verifyIn := func(tx pgx.Tx) ledger.VerifyResult {
		t.Helper()
		r, err := ledger.Verify(ctx, tx, table, tid, stream)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	verify := func() ledger.VerifyResult {
		t.Helper()
		var r ledger.VerifyResult
		// Vai ứng dụng: verify_chain đọc sổ đăng ký cột qua hàm SECURITY DEFINER.
		if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error { r = verifyIn(tx); return nil }); err != nil {
			t.Fatal(err)
		}
		return r
	}
	wantOK := func(n int64) {
		t.Helper()
		if r := verify(); !r.OK || r.RowsChecked != n {
			t.Fatalf("chuỗi phải hợp lệ với %d dòng: %+v (reason %v)", n, r, deref(r.Reason))
		}
	}
	wantBroken := func(seq int64) {
		t.Helper()
		if r := verify(); r.OK || r.BrokenAtSeq == nil || *r.BrokenAtSeq != seq {
			t.Fatalf("phải báo đứt tại seq %d: %+v", seq, r)
		}
	}

	ins(1, "")
	ins(2, "")
	wantOK(2)

	// Đợt 1: cột a. Chưa đăng ký → đứt giả ở dòng đầu (lỗi gốc); đăng ký → sạch.
	mustExec("ALTER TABLE " + table + " ADD COLUMN a text NULL")
	wantBroken(1)
	mustExec(`SELECT shared.register_ledger_columns($1::regclass, ARRAY['a'])`, table)
	wantOK(2)
	ins(3, ", a", "x") // seq 3: a có giá trị
	ins(4, "")         // seq 4: a NULL, khóa a có trong băm

	// Đợt 2: cột b, c.
	mustExec("ALTER TABLE " + table + " ADD COLUMN b integer NULL, ADD COLUMN c uuid NULL")
	mustExec(`SELECT shared.register_ledger_columns($1::regclass, ARRAY['b', 'c'])`, table)
	ins(5, ", a, b", "y", 5)
	wantOK(5)

	// DEFAULT bị từ chối (dòng cũ sẽ hiện giá trị chưa từng ghi) — và quả thật làm đứt chuỗi.
	if err := tenant.WithTenantTx(ctx, owner, tid, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "ALTER TABLE "+table+" ADD COLUMN d integer NULL DEFAULT 0"); err != nil {
			return err
		}
		if r := verifyIn(tx); r.OK {
			t.Errorf("ADD COLUMN … DEFAULT phải làm đứt chuỗi khi không đăng ký được: %+v", r)
		}
		sp, err := tx.Begin(ctx)
		if err != nil {
			return err
		}
		_, err = sp.Exec(ctx, `SELECT shared.register_ledger_columns($1::regclass, ARRAY['d'])`, table)
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "23514" {
			t.Errorf("đăng ký cột có DEFAULT phải bị từ chối 23514, nhận %v", err)
		}
		_ = sp.Rollback(ctx)
		return errRollback
	}); !errors.Is(err, errRollback) {
		t.Fatal(err)
	}
	// Không phải bảng sổ → từ chối.
	if _, err := owner.Exec(ctx, `SELECT shared.register_ledger_columns('shared.outbox'::regclass, ARRAY['kind'])`); err == nil {
		t.Error("đăng ký cột cho bảng không phải sổ phải bị từ chối")
	}
	wantOK(5)

	// Kẻ có quyền cao sửa vượt trigger: mọi thay đổi giá trị vẫn bị bắt.
	admin := env.AdminPool(t, migrations.MKG)
	tamper := func(set string, payload int) {
		t.Helper()
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "UPDATE "+table+" SET "+set+" WHERE tenant_id = $1 AND payload = $2", tid, payload); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name, set, undo string
		payload         int
		seq             int64
	}{
		{"dòng cũ: cột thêm từ NULL thành có giá trị", "a = 'z'", "a = NULL", 1, 1},
		{"dòng cũ: cột đợt 2 có giá trị", "c = gen_random_uuid()", "c = NULL", 2, 2},
		{"dòng mới: cột thêm có giá trị về NULL", "a = NULL", "a = 'x'", 3, 3},
		{"dòng đợt 1: cột đợt 1 NULL thành có giá trị", "a = 'w'", "a = NULL", 4, 4},
		{"dòng mới nhất: b về NULL", "b = NULL", "b = 5", 5, 5},
	}
	for _, c := range cases {
		tamper(c.set, c.payload)
		if r := verify(); r.OK || r.BrokenAtSeq == nil || *r.BrokenAtSeq != c.seq {
			t.Errorf("%s: phải báo đứt tại seq %d: %+v", c.name, c.seq, r)
		}
		tamper(c.undo, c.payload)
		wantOK(5)
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
