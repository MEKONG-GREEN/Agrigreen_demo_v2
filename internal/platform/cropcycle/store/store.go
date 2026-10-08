// Package store: SQL của pf/cropcycle (van thu gom collection_*, W3.3), chỉ chạy trong giao dịch dưới vai r_cropcycle
// (R-05) và luôn trong giao dịch có ngữ cảnh tenant (tenant.WithTenantTx) — RLS
// áp dụng. Tham số luôn ép kiểu tường minh vì pool dùng chế độ thực thi của
// PgBouncer (QueryExecModeExec). Số (diện tích, khối lượng) đi vào/ra dạng chuỗi
// decimal, không bao giờ float ở Go (R-10).
package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
)

func day(t time.Time) string { return domain.FormatDay(t) }

func strs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// lockSync tuần tự hóa việc cấp sync_seq của một bảng chiếu trong tenant tới khi
// giao dịch kết thúc: giá trị sequence lấy sau khóa nên thứ tự commit trùng thứ
// tự sync_seq, con trỏ chiều xuống không bỏ sót dòng commit muộn (mẫu pf/geo).
func lockSync(ctx context.Context, tx pgx.Tx, table string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_cropcycle.'||$1::text||'_sync:' || current_setting('app.tenant_id', true), 0))", table)
	return err
}
