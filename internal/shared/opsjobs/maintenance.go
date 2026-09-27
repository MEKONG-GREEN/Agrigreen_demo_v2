package opsjobs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PurgeIdempotency xóa khóa Idempotency-Key đã hết hạn theo lô tới khi hết.
// pool là vai ag_outbox_publisher (BYPASSRLS — xem migration shared/000002).
func PurgeIdempotency(ctx context.Context, pool *pgxpool.Pool, batch int) (int64, error) {
	if batch <= 0 {
		batch = 5000
	}
	var total int64
	for {
		var n int64
		if err := pool.QueryRow(ctx, "SELECT shared.purge_expired_idempotency($1)", batch).Scan(&n); err != nil {
			return total, fmt.Errorf("opsjobs: purge_expired_idempotency: %w", err)
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

// EnsurePartitions tạo trước phân vùng tháng (UTC) từ tháng hiện tại tới
// monthsAhead tháng cho mọi bảng sổ. Chạy bằng ag_migrator (owner). Idempotent.
func EnsurePartitions(ctx context.Context, pool *pgxpool.Pool, monthsAhead int) ([]PartitionReport, error) {
	if monthsAhead <= 0 {
		monthsAhead = 24
	}
	tables, err := LedgerTables(ctx, pool)
	if err != nil {
		return nil, err
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
