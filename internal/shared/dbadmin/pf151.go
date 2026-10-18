package dbadmin

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Gộp dev → main: main có pf 000151_pf_kref_ctx_grant_agg (EXECUTE k_ref.ctx_* cho r_agg) và đã áp ở môi trường
// demo; dev bỏ tệp đó (nội dung tương đương nằm ở 000137_pf_kref_agg_ctx_grant, chỉ GRANT — chạy lại vô hại) và
// dùng 000137–000152 cho price/hrm/forms/… Ở DB đã áp bản main, golang-migrate đứng ở 151 nên bỏ qua im lặng
// 137–151 của dev. repairMainPF151 hạ phiên bản về 136 — khi và chỉ khi DB đúng là bản main: sạch, đứng ở 151 và
// chưa có đối tượng nào do dev 138–151 tạo — để lần áp kế tiếp chạy đủ 137 → mới nhất. Chạy lại không đổi gì.
const (
	mainPF151Version = 151
	mainPF151Reset   = 136
)

// devPF138to151Objects là đối tượng do dev pf 138–151 tạo (một tệp ít nhất một mục); có bất kỳ mục nào nghĩa là
// 151 trong sổ là của dev, không phải tệp của main.
var devPF138to151Objects = []string{
	"pf_price",                          // 138 (schema)
	"pf_forms.period_spec",              // 140
	"pf_accounts.assignment_attr_event", // 142
	"pf_masterdata.catalog_effective",   // 146
	"pf_hrm",                            // 147 (schema)
	"pf_forms.submission_meta",          // 149
}

// repairMainPF151 trả true nếu đã hạ schema_migrations_pf từ 151 về 136.
func repairMainPF151(ctx context.Context, dsn string) (bool, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	var repaired bool
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT to_regclass('schema_migrations_pf') IS NOT NULL").Scan(&exists); err != nil || !exists {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT version, dirty FROM schema_migrations_pf FOR UPDATE")
		if err != nil {
			return err
		}
		type row struct {
			version int64
			dirty   bool
		}
		got, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
			var x row
			return x, r.Scan(&x.version, &x.dirty)
		})
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].version != mainPF151Version || got[0].dirty {
			return nil
		}
		for _, obj := range devPF138to151Objects {
			q := "SELECT to_regclass($1) IS NOT NULL"
			if !strings.Contains(obj, ".") {
				q = "SELECT to_regnamespace($1) IS NOT NULL"
			}
			var present bool
			if err := tx.QueryRow(ctx, q, obj).Scan(&present); err != nil {
				return err
			}
			if present {
				return nil
			}
		}
		tag, err := tx.Exec(ctx, "UPDATE schema_migrations_pf SET version = $1 WHERE version = $2 AND NOT dirty",
			mainPF151Reset, mainPF151Version)
		if err != nil {
			return err
		}
		repaired = tag.RowsAffected() == 1
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("dbadmin: sửa pf 151 của main: %w", err)
	}
	return repaired, nil
}
