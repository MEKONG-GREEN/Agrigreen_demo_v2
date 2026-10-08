package db

// Vai tổng hợp r_agg (W6.4, ADR-0031 §3–§5, R-DB-11, R-SEC-03): NOLOGIN, BYPASSRLS, chỉ SELECT trên view công bố
// _pub (membership r_<module>_pub, bootstrap) và cột tenant_id của k_ref.org (pf 000129). Vai đăng nhập của đơn vị
// chạy job (app_worker) là thành viên NOINHERIT — quyền chỉ có sau SET LOCAL ROLE, và chuỗi đổi vai chỉ được viết
// trong gói này (archlint agnosessionset: SET LOCAL ROLE, set_config('role', …) và tên vai r_agg ngoài
// internal/shared/db bị chặn; WithAggTx chỉ được gọi từ bộ chạy job của cap/analytics).
//
// WithAggTx là đường DUY NHẤT vào r_agg: giao dịch CHỈ ĐỌC, chạy trước dưới vai module chủ danh mục job (owner,
// hiện là r_metrics) với tenant quản trị để authorize kiểm job đã khai + đã duyệt bốn mắt + đúng phiên bản; chỉ
// khi authorize trả nil mới đổi sang r_agg (SET LOCAL — hết khi COMMIT/ROLLBACK, kết nối về pool ở vai đăng nhập).
// Nhật ký chạy ghi ở giao dịch khác của module chủ (giao dịch này chỉ đọc).

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// AggRole là vai tổng hợp vượt đơn vị (ADR-0031 §3). Chỉ dùng qua WithAggTx.
const AggRole = "r_agg"

// AggStatementTimeout là trần thời gian mỗi câu lệnh trong giao dịch r_agg (ADR-0031 §3: statement_timeout cho job).
const AggStatementTimeout = "10min"

// ErrAggSetup: gọi WithAggTx thiếu tham số (lỗi lắp ráp).
var ErrAggSetup = errors.New("db: WithAggTx cần module chủ, tenant quản trị, authorize và fn")

// AggTx là giao dịch đang chạy dưới r_agg. InTenant đặt app.tenant_id cục bộ để đọc lát của một tenant qua view
// _pub (view chạy bằng quyền chủ ag_migrator, FORCE RLS — BYPASSRLS của bên đọc không xuyên qua view, xem
// ADR-0031 Hiện thực); truy vấn thẳng bảng (k_ref.org) được BYPASSRLS.
type AggTx struct{ tx pgx.Tx }

// Tx trả giao dịch r_agg (chỉ đọc).
func (a AggTx) Tx() pgx.Tx { return a.tx }

// InTenant chạy fn với app.tenant_id = tid trong cùng giao dịch r_agg, rồi xóa lại.
func (a AggTx) InTenant(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	if tid == uuid.Nil {
		return tenant.ErrMissing
	}
	if _, err := a.tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tid.String()); err != nil {
		return fmt.Errorf("db: r_agg đặt tenant: %w", err)
	}
	if err := fn(a.tx); err != nil {
		return err
	}
	if _, err := a.tx.Exec(ctx, "SELECT set_config('app.tenant_id', '', true)"); err != nil {
		return fmt.Errorf("db: r_agg xóa tenant: %w", err)
	}
	return nil
}

// aggSwitchSQL đổi vai sang r_agg ($1), xóa mọi biến ngữ cảnh app.* (job không thuộc người dùng nào) và đặt
// statement_timeout, chế độ chỉ đọc — mọi giá trị là SET LOCAL.
const aggSwitchSQL = "SELECT set_config('role', $1, true), set_config('app.tenant_id', '', true), " +
	"set_config('" + VarNodeID + "', '', true), set_config('" + VarDept + "', '', true), " +
	"set_config('" + VarXaIDs + "', '', true), set_config('" + VarPurpose + "', '', true), " +
	"set_config('" + VarActorRef + "', '', true), set_config('" + VarOnBehalfOfRef + "', '', true), " +
	"set_config('statement_timeout', $2, true), set_config('transaction_read_only', 'on', true)"

// WithAggTx mở giao dịch CHỈ ĐỌC: BEGIN READ ONLY → SET LOCAL ROLE <owner> + app.tenant_id = governance →
// authorize(tx) (lỗi → ROLLBACK, không bao giờ đổi vai) → SET LOCAL ROLE r_agg → fn → COMMIT.
// owner là module giữ danh mục job (pf/metrics); governance là tenant quản trị nơi danh mục + quyết định duyệt nằm.
func WithAggTx(ctx context.Context, b tenant.Beginner, owner ModuleID, governance uuid.UUID,
	authorize func(pgx.Tx) error, fn func(AggTx) error) error {
	if b == nil || !owner.Valid() || governance == uuid.Nil || authorize == nil || fn == nil {
		return ErrAggSetup
	}
	return tenant.WithTenantTxOptions(ctx, ForModule(b, owner), governance, pgx.TxOptions{AccessMode: pgx.ReadOnly},
		func(tx pgx.Tx) error {
			if err := authorize(tx); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, aggSwitchSQL, AggRole, AggStatementTimeout); err != nil {
				return fmt.Errorf("db: SET LOCAL ROLE %s: %w", AggRole, err)
			}
			return fn(AggTx{tx: tx})
		})
}
