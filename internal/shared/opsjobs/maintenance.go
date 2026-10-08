package opsjobs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// PurgeIdempotency xóa khóa Idempotency-Key đã hết hạn theo lô tới khi hết.
// pool là vai ag_ops_jobs (BYPASSRLS — migration shared/000002, chuyển vai ở shared/000009).
func PurgeIdempotency(ctx context.Context, pool *pgxpool.Pool, batch int) (int64, error) {
	return purgeLoop(ctx, pool, "purge_expired_idempotency", batch)
}

// PurgeLedgerIdem xóa dòng đăng ký idem_key của sổ (shared.ledger_idem) đã quá hạn
// 30 ngày theo lô tới khi hết (W2.1, DB-11, ADR-0012). Bản ghi sổ không bị đụng tới;
// sau hạn, ghi lại cùng idem_key được nhận như bản ghi mới. pool: ag_ops_jobs
// (chỉ có EXECUTE trên hàm SECURITY DEFINER — migration shared/000006, 000009).
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
	Moved       int64 // số dòng đã chuyển từ DEFAULT sang phân vùng tháng (shared.drain_default_partition)
	DefaultRows int64 // ước lượng số dòng còn trong phân vùng DEFAULT sau khi chạy (>0 = cần xem)
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
		// DEFAULT có dòng (thống kê): chuyển ra trước — kể cả tháng đã qua, mà create_monthly_partitions
		// (chỉ từ tháng hiện tại) không bao giờ tạo. Tháng hiện tại/tương lai bị chặn mà thống kê chưa kịp
		// thấy thì create_monthly_partitions tự chuyển (nhánh check_violation, shared 000010).
		est, err := defaultRows(ctx, pool, lt.Table)
		if err != nil {
			return out, err
		}
		if est > 0 {
			if err := pool.QueryRow(ctx, "SELECT shared.drain_default_partition($1::regclass)", lt.Table).Scan(&r.Moved); err != nil {
				return out, fmt.Errorf("opsjobs: chuyển dòng khỏi DEFAULT của %s: %w", lt.Table, err)
			}
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
		// drain chuyển MỌI dòng (mỗi dòng thuộc một tháng) nên DEFAULT rỗng chắc chắn; thống kê chỉ cập
		// nhật khi tiến trình đã xóa xả số đếm (tới ~10 giây) — không đọc lại để tránh cảnh báo giả.
		if r.Moved == 0 {
			if r.DefaultRows, err = defaultRows(ctx, pool, lt.Table); err != nil {
				return out, err
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// defaultRows ước lượng số dòng trong phân vùng DEFAULT của một bảng — thống kê n_live_tup như
// shared.ledger_default_rows(), không đọc dữ liệu (owner bị FORCE RLS nên count(*) trả 0). 0 nếu
// bảng không có DEFAULT.
func defaultRows(ctx context.Context, pool *pgxpool.Pool, table string) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT coalesce((SELECT s.n_live_tup FROM pg_partitioned_table pt
	    JOIN pg_stat_all_tables s ON s.relid = pt.partdefid
	    WHERE pt.partrelid = $1::regclass AND pt.partdefid <> 0), 0)`, table).Scan(&n)
	return n, err
}

// DefaultRows là số dòng ước lượng trong phân vùng DEFAULT của một bảng phân vùng.
type DefaultRows struct {
	Table string // schema.bảng (bảng cha)
	Rows  int64
}

// DefaultPartitionRows đọc shared.ledger_default_rows() (shared 000010): mọi bảng phân vùng có DEFAULT,
// kèm số dòng ước lượng. pool: ag_ops_jobs (EXECUTE) hoặc owner.
func DefaultPartitionRows(ctx context.Context, pool *pgxpool.Pool) ([]DefaultRows, error) {
	rows, err := pool.Query(ctx, "SELECT table_name, est_rows FROM shared.ledger_default_rows()")
	if err != nil {
		return nil, fmt.Errorf("opsjobs: ledger_default_rows: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DefaultRows, error) {
		var d DefaultRows
		err := r.Scan(&d.Table, &d.Rows)
		return d, err
	})
}

// RegisterDefaultPartitionRows đăng ký gauge telemetry.MetricLedgerDefaultRows (theo cluster, table) đọc
// từ từng pool (khóa = tên cụm). Luật cảnh báo AgLedgerDefaultPartitionRows
// (deploy/observability/prometheus/agrigreen-platform.rules.yml). Lỗi ở một cụm không chặn cụm khác.
func RegisterDefaultPartitionRows(pools map[string]*pgxpool.Pool) (metric.Registration, error) {
	m := telemetry.Meter()
	g, err := m.Int64ObservableGauge(telemetry.MetricLedgerDefaultRows,
		metric.WithDescription("Số dòng ước lượng trong phân vùng DEFAULT (> 0: từng thiếu phân vùng tháng — chạy migrate maintain)"))
	if err != nil {
		return nil, err
	}
	return m.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		var errs []error
		for c, p := range pools {
			rs, err := DefaultPartitionRows(ctx, p)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", c, err))
				continue
			}
			for _, r := range rs {
				o.ObserveInt64(g, r.Rows, metric.WithAttributes(attribute.String("cluster", c), attribute.String("table", r.Table)))
			}
		}
		return errors.Join(errs...)
	}, g)
}
