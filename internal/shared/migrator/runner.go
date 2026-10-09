package migrator

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// LedgerTable là tên bảng sổ trong mỗi schema.
const LedgerTable = "schema_migrations"

// LedgerComment đánh dấu bảng sổ: tiền tố "ag:no-rls" miễn cổng shared.v_missing_rls (bảng không có tenant);
// bộ chạy chỉ đọc bảng mang dấu này.
const LedgerComment = "ag:no-rls: sổ migration theo module (W7.1, R-DB-12, ADR-0038) — không có tenant"

// lockKey là khóa tư vấn của bộ chạy v2 (mỗi database). Thêm vào đó bộ chạy giữ khóa của golang-migrate cho
// hai bảng schema_migrations_shared/_<cụm> để v1 và v2 không chạy chồng trong kỳ chuyển.
const lockKey int64 = 727038

// Options tùy chọn bộ chạy.
type Options struct {
	FS      fs.FS    // mặc định migrations.FS
	Modules []Module // mặc định CatalogModules()
	Log     func(format string, args ...any)
}

func (o *Options) fill() {
	if o.FS == nil {
		o.FS = migrations.FS
	}
	if o.Modules == nil {
		o.Modules = CatalogModules()
	}
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
}

// Result là kết quả một lần Up.
type Result struct {
	Imported []string // ghi sổ từ trạng thái golang-migrate, không chạy
	Applied  []string // đã chạy
}

// State là trạng thái đọc được của một database.
type State struct {
	Plan  *Plan
	Rows  []Row
	V1    map[string]V1State
	Recon Reconciliation
}

// Inspect đọc trạng thái (không sửa gì) — dùng cho migrate verify-runner.
func Inspect(ctx context.Context, dsn string, c migrations.Cluster, o Options) (*State, error) {
	o.fill()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	return inspect(ctx, conn, c, o)
}

func inspect(ctx context.Context, conn *pgx.Conn, c migrations.Cluster, o Options) (*State, error) {
	plan, err := BuildPlan(o.FS, c, o.Modules)
	if err != nil {
		return nil, err
	}
	rows, err := readLedgers(ctx, conn)
	if err != nil {
		return nil, err
	}
	v1 := map[string]V1State{}
	for _, d := range []string{SharedDir, string(c)} {
		s, err := readV1(ctx, conn, d)
		if err != nil {
			return nil, err
		}
		v1[d] = s
	}
	return &State{Plan: plan, Rows: rows, V1: v1, Recon: Reconcile(plan.Files, rows, v1)}, nil
}

// Up áp migration cho database của cụm c qua dsn (vai ag_migrator, nối thẳng — không PgBouncer): đối chiếu sổ,
// ghi sổ các tệp golang-migrate đã áp (lần đầu chuyển, không chạy lại), rồi áp mọi tệp vắng trong sổ theo thứ
// tự kế hoạch, mỗi tệp một giao dịch cùng dòng sổ và đồng bộ phiên bản golang-migrate (để quay về v1 được).
func Up(ctx context.Context, dsn string, c migrations.Cluster, o Options) (Result, error) {
	o.fill()
	var res Result
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return res, err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	unlock, err := lockAll(ctx, conn, c)
	if err != nil {
		return res, err
	}
	defer unlock()

	st, err := inspect(ctx, conn, c, o)
	if err != nil {
		return res, err
	}
	if err := st.Recon.Err(); err != nil {
		return res, err
	}
	if len(st.Recon.Import) > 0 {
		err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			for _, f := range st.Recon.Import {
				if err := record(ctx, tx, f, "import"); err != nil {
					return err
				}
				res.Imported = append(res.Imported, f.Key())
			}
			return nil
		})
		if err != nil {
			return Result{}, fmt.Errorf("migrator: ghi sổ từ golang-migrate: %w", err)
		}
		o.Log("%s: ghi sổ %d tệp đã áp bởi golang-migrate (không chạy lại)", c, len(res.Imported))
	}
	for _, f := range st.Recon.Apply {
		start := time.Now()
		err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, f.SQL); err != nil {
				return sqlError(f, err)
			}
			if err := record(ctx, tx, f, "apply"); err != nil {
				return err
			}
			return syncV1(ctx, tx, f.Dir, f.Version)
		})
		if err != nil {
			return res, err
		}
		res.Applied = append(res.Applied, f.Key())
		o.Log("%s: áp %s (%s) %s", c, f.Key(), f.Owner, time.Since(start).Round(time.Millisecond))
	}
	return res, nil
}

func sqlError(f File, err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Position > 0 {
		line := 1 + strings.Count(f.SQL[:min(int(pe.Position), len(f.SQL))], "\n")
		return fmt.Errorf("migrator: %s dòng %d: %w", f.Key(), line, err)
	}
	return fmt.Errorf("migrator: %s: %w", f.Key(), err)
}

func lockAll(ctx context.Context, conn *pgx.Conn, c migrations.Cluster) (func(), error) {
	var dbName, schema string
	if err := conn.QueryRow(ctx, "SELECT current_database(), coalesce(current_schema(), 'public')").Scan(&dbName, &schema); err != nil {
		return nil, err
	}
	keys := []int64{lockKey}
	for _, d := range []string{SharedDir, string(c)} {
		id, err := database.GenerateAdvisoryLockId(dbName, schema, v1Table(d))
		if err != nil {
			return nil, err
		}
		var k int64
		if _, err := fmt.Sscan(id, &k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	for _, k := range keys {
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", k); err != nil {
			return nil, err
		}
	}
	return func() {
		for _, k := range keys {
			_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", k)
		}
	}, nil
}

func v1Table(dir string) string { return "schema_migrations_" + dir }

func readV1(ctx context.Context, conn *pgx.Conn, dir string) (V1State, error) {
	var s V1State
	var reg *string
	if err := conn.QueryRow(ctx, "SELECT to_regclass(quote_ident(current_schema()) || '.' || quote_ident($1))::text", v1Table(dir)).Scan(&reg); err != nil {
		return s, err
	}
	if reg == nil {
		return s, nil
	}
	s.Exists = true
	err := conn.QueryRow(ctx, "SELECT version, dirty FROM "+*reg+" LIMIT 1").Scan(&s.Version, &s.Dirty)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	return s, err
}

// readLedgers đọc mọi bảng sổ (mang LedgerComment) của database.
func readLedgers(ctx context.Context, conn *pgx.Conn) ([]Row, error) {
	rows, err := conn.Query(ctx, `
		SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE c.relname = $1 AND c.relkind = 'r' AND obj_description(c.oid, 'pg_class') = $2
		 ORDER BY 1`, LedgerTable, LedgerComment)
	if err != nil {
		return nil, err
	}
	schemas, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	var out []Row
	for _, s := range schemas {
		q := fmt.Sprintf("SELECT dir, file, version, module, checksum, method FROM %s.%s ORDER BY dir, version",
			pgx.Identifier{s}.Sanitize(), LedgerTable)
		rs, err := conn.Query(ctx, q)
		if err != nil {
			return nil, err
		}
		got, err := pgx.CollectRows(rs, func(r pgx.CollectableRow) (Row, error) {
			x := Row{Schema: s}
			return x, r.Scan(&x.Dir, &x.File, &x.Version, &x.Module, &x.Checksum, &x.Method)
		})
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

// ledgerSchema chọn schema chứa sổ của tệp: schema đầu tiên của module chủ (owns_schema) đã có trong database;
// module giả, hoặc module chưa có schema ở database này → shared.
func ledgerSchema(ctx context.Context, tx pgx.Tx, f File) (string, error) {
	for _, s := range f.LedgerSchemas {
		var ok bool
		if err := tx.QueryRow(ctx, "SELECT to_regnamespace(quote_ident($1)) IS NOT NULL", s).Scan(&ok); err != nil {
			return "", err
		}
		if ok {
			return s, nil
		}
	}
	return SharedDir, nil
}

// ensureLedger tạo bảng sổ trong schema nếu chưa có: chỉ ghi thêm (trigger shared.fn_immutable chặn
// UPDATE/DELETE/TRUNCATE — cũng thỏa cổng v_missing_immutability của schema ts_*), không quyền cho PUBLIC hay
// vai nào khác ngoài chủ (ag_migrator), RLS bật + ép (xem dưới), chú thích LedgerComment.
func ensureLedger(ctx context.Context, tx pgx.Tx, schema string) error {
	tbl := pgx.Identifier{schema, LedgerTable}.Sanitize()
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", tbl).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	stmts := []string{
		`CREATE TABLE ` + tbl + ` (
		  dir        text        NOT NULL,
		  file       text        NOT NULL,
		  version    bigint      NOT NULL CHECK (version > 0),
		  module     text        NOT NULL,
		  checksum   text        NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
		  method     text        NOT NULL CHECK (method IN ('apply', 'import')),
		  applied_at timestamptz NOT NULL DEFAULT now(),
		  applied_by text        NOT NULL DEFAULT current_user,
		  PRIMARY KEY (dir, file))`,
		`COMMENT ON TABLE ` + tbl + ` IS '` + LedgerComment + `'`,
		`REVOKE ALL ON ` + tbl + ` FROM PUBLIC`,
		// RLS bật + ép với một policy toàn quyền: chỉ chủ (ag_migrator) có quyền trên bảng, nên policy không mở
		// gì thêm — nhưng thỏa các migration kiểm "mọi bảng của schema module có RLS" (vd pf 000133).
		`ALTER TABLE ` + tbl + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + tbl + ` FORCE ROW LEVEL SECURITY`,
		`CREATE POLICY p_migration_ledger ON ` + tbl + ` USING (true) WITH CHECK (true)`,
		`CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON ` + tbl + ` FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable()`,
		`CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON ` + tbl + ` FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable()`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(ctx, s); err != nil {
			return fmt.Errorf("migrator: tạo sổ %s: %w", tbl, err)
		}
	}
	return nil
}

func record(ctx context.Context, tx pgx.Tx, f File, method string) error {
	schema, err := ledgerSchema(ctx, tx, f)
	if err != nil {
		return err
	}
	if err := ensureLedger(ctx, tx, schema); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO "+pgx.Identifier{schema, LedgerTable}.Sanitize()+
		" (dir, file, version, module, checksum, method) VALUES ($1, $2, $3, $4, $5, $6)",
		f.Dir, f.Name, f.Version, f.Owner, f.Checksum, method)
	if err != nil {
		return fmt.Errorf("migrator: ghi sổ %s: %w", f.Key(), err)
	}
	return nil
}

// syncV1 giữ bảng golang-migrate schema_migrations_<thư mục> bằng số lớn nhất đã áp (tạo nếu chưa có, cùng DDL
// với golang-migrate) — trong kỳ chuyển, quay về bộ chạy cũ không chạy lại tệp v2 đã áp. Tệp số nhỏ hơn
// (ngoài thứ tự) không đổi phiên bản.
func syncV1(ctx context.Context, tx pgx.Tx, dir string, version int64) error {
	var schema string
	if err := tx.QueryRow(ctx, "SELECT coalesce(current_schema(), 'public')").Scan(&schema); err != nil {
		return err
	}
	tbl := pgx.Identifier{schema, v1Table(dir)}.Sanitize()
	if _, err := tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+tbl+" (version bigint not null primary key, dirty boolean not null)"); err != nil {
		return err
	}
	var cur int64
	err := tx.QueryRow(ctx, "SELECT version FROM "+tbl+" LIMIT 1").Scan(&cur)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && cur >= version {
		return nil
	}
	if _, err := tx.Exec(ctx, "TRUNCATE "+tbl); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO "+tbl+" (version, dirty) VALUES ($1, false)", version)
	return err
}
