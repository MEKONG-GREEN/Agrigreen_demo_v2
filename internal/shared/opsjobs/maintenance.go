package opsjobs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PurgeIdempotency xóa khóa Idempotency-Key đã hết hạn theo lô tới khi hết.
// pool là vai ag_outbox_publisher (BYPASSRLS — xem migration shared/000002).
func PurgeIdempotency(ctx context.Context, pool *pgxpool.Pool, batch int) (int64, error) {
	return purgeLoop(ctx, pool, "purge_expired_idempotency", batch)
}

// PurgeLedgerIdem xóa dòng đăng ký idem_key của sổ (shared.ledger_idem) đã quá hạn
// 30 ngày theo lô tới khi hết (W2.1, DB-11, ADR-0012). Bản ghi sổ không bị đụng tới;
// sau hạn, ghi lại cùng idem_key được nhận như bản ghi mới. pool: ag_outbox_publisher
// (chỉ có EXECUTE trên hàm SECURITY DEFINER — migration shared/000006).
func PurgeLedgerIdem(ctx context.Context, pool *pgxpool.Pool, batch int) (int64, error) {
	return purgeLoop(ctx, pool, "purge_expired_ledger_idem", batch)
}

// PurgeResult là số dòng đã xóa của một lần dọn idempotency trên một cụm.
type PurgeResult struct {
	HTTPKeys   int64 // shared.idempotency_key (48 giờ, R-51)
	LedgerIdem int64 // shared.ledger_idem (30 ngày, W2.1)
}

// PurgeAllIdempotency dọn cả hai bảng idempotency của một cụm. Lỗi ở bảng đầu
// không chặn bảng sau; trả số đã xóa từng phần kèm lỗi gộp.
func PurgeAllIdempotency(ctx context.Context, pool *pgxpool.Pool, batch int) (PurgeResult, error) {
	var r PurgeResult
	var err1, err2 error
	r.HTTPKeys, err1 = PurgeIdempotency(ctx, pool, batch)
	r.LedgerIdem, err2 = PurgeLedgerIdem(ctx, pool, batch)
	return r, errors.Join(err1, err2)
}

// purgeLoop gọi shared.<fn>(batch) lặp tới khi một lô trả ít hơn batch.
func purgeLoop(ctx context.Context, pool *pgxpool.Pool, fn string, batch int) (int64, error) {
	if batch <= 0 {
		batch = 5000
	}
	var total int64
	for {
		var n int64
		if err := pool.QueryRow(ctx, "SELECT shared."+fn+"($1)", batch).Scan(&n); err != nil {
			return total, fmt.Errorf("opsjobs: %s: %w", fn, err)
		}
		total += n
		if n < int64(batch) || ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

// LedgerTable là một bảng sổ (bảng cha phân vùng có cột row_hash).
type LedgerTable struct {
	Table  string // schema.bảng
	Ledger string // tên logic truyền cho fn_chain (tham số trigger trg_chain)
}

// LedgerTables liệt kê bảng sổ theo cùng tiêu chí với shared.v_missing_immutability
// (có cột row_hash), chỉ bảng cha (không phải phân vùng).
func LedgerTables(ctx context.Context, pool *pgxpool.Pool) ([]LedgerTable, error) {
	rows, err := pool.Query(ctx, `SELECT format('%I.%I', n.nspname, c.relname),
	    coalesce((SELECT split_part(encode(t.tgargs, 'escape'), '\000', 1)
	              FROM pg_trigger t WHERE t.tgrelid = c.oid AND t.tgname = 'trg_chain'), '')
	  FROM pg_class c
	  JOIN pg_namespace n ON n.oid = c.relnamespace
	  WHERE c.relkind IN ('r', 'p') AND NOT c.relispartition
	    AND n.nspname NOT IN ('pg_catalog', 'information_schema')
	    AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'row_hash' AND NOT a.attisdropped)
	  ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LedgerTable
	for rows.Next() {
		var l LedgerTable
		if err := rows.Scan(&l.Table, &l.Ledger); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// PartitionReport là kết quả tạo phân vùng cho một bảng sổ.
type PartitionReport struct {
	Table       string
	Created     int   // số phân vùng tháng mới tạo
	DefaultRows int64 // ước lượng số dòng trong phân vùng DEFAULT (>0 = từng thiếu phân vùng)
	Partitioned bool  // false: bảng sổ không phân vùng — bỏ qua
}

// AppendOnlyPartitionedTables liệt kê bảng CHỈ GHI THÊM phân vùng không có chuỗi
// băm (shared.apply_append_only — chuỗi thời gian ts_*, ADR-0017): bảng cha phân
// vùng có trigger trg_immutable của shared.fn_immutable và không có cột row_hash.
func AppendOnlyPartitionedTables(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT format('%I.%I', n.nspname, c.relname)
	  FROM pg_class c
	  JOIN pg_namespace n ON n.oid = c.relnamespace
	  WHERE c.relkind = 'p' AND NOT c.relispartition
	    AND n.nspname NOT IN ('pg_catalog', 'information_schema')
	    AND EXISTS (SELECT 1 FROM pg_trigger t WHERE t.tgrelid = c.oid AND t.tgname = 'trg_immutable'
	                AND t.tgfoid = 'shared.fn_immutable'::regproc)
	    AND NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'row_hash' AND NOT a.attisdropped)
	  ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// EnsurePartitions tạo trước phân vùng tháng (UTC) từ tháng hiện tại tới
// monthsAhead tháng cho mọi bảng sổ và mọi bảng chỉ ghi thêm phân vùng (chuỗi
// thời gian ts_*). Chạy bằng ag_migrator (owner). Idempotent.
func EnsurePartitions(ctx context.Context, pool *pgxpool.Pool, monthsAhead int) ([]PartitionReport, error) {
	if monthsAhead <= 0 {
		monthsAhead = 24
	}
	tables, err := LedgerTables(ctx, pool)
	if err != nil {
		return nil, err
	}
	appendOnly, err := AppendOnlyPartitionedTables(ctx, pool)
	if err != nil {
		return nil, err
	}
	for _, t := range appendOnly {
		tables = append(tables, LedgerTable{Table: t})
	}
	count := func(t string) (int, error) {
		var n int
		err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_inherits WHERE inhparent = $1::regclass", t).Scan(&n)
		return n, err
	}
	var out []PartitionReport
	for _, lt := range tables {
		r := PartitionReport{Table: lt.Table}
		if err := pool.QueryRow(ctx, "SELECT relkind = 'p' FROM pg_class WHERE oid = $1::regclass", lt.Table).Scan(&r.Partitioned); err != nil {
			return out, err
		}
		if !r.Partitioned {
			out = append(out, r)
			continue
		}
		before, err := count(lt.Table)
		if err != nil {
			return out, err
		}
		if _, err := pool.Exec(ctx, "SELECT shared.create_monthly_partitions($1::regclass, (now() AT TIME ZONE 'UTC')::date, $2)", lt.Table, monthsAhead); err != nil {
			return out, fmt.Errorf("opsjobs: tạo phân vùng %s: %w", lt.Table, err)
		}
		after, err := count(lt.Table)
		if err != nil {
			return out, err
		}
		r.Created = after - before
		// Thống kê, không đọc dữ liệu: owner bị FORCE RLS nên count(*) trả 0.
		_ = pool.QueryRow(ctx, `SELECT coalesce(s.n_live_tup, 0) FROM pg_inherits i
		    JOIN pg_class p ON p.oid = i.inhrelid
		    LEFT JOIN pg_stat_all_tables s ON s.relid = p.oid
		    WHERE i.inhparent = $1::regclass AND pg_get_expr(p.relpartbound, p.oid) = 'DEFAULT'`, lt.Table).Scan(&r.DefaultRows)
		out = append(out, r)
	}
	return out, nil
}
