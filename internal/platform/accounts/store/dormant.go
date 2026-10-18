package store

// D24 (W10.05): job accounts_dormant — tài khoản ứng viên và mốc kích hoạt lại ở pf.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// DormantTenants: tenant có tài khoản đang hoạt động. pool là ag_ops_jobs (worker, nối thẳng; quyền cột
// tenant_id, enabled — migration pf 20261013134254).
func DormantTenants(ctx context.Context, pool *pgxpool.Pool) ([]uuid.UUID, error) {
	rows, err := pool.Query(ctx, "SELECT DISTINCT tenant_id FROM pf_accounts.account WHERE enabled")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// DormantCandidates liệt kê tài khoản đang hoạt động của tenant trong tx, trừ tài khoản nông dân (nông hộ tự
// kích hoạt lại bằng OTP — chờ kênh SMS W10.06, chưa vào job).
func DormantCandidates(ctx context.Context, tx pgx.Tx) ([]domain.Account, error) {
	rows, err := tx.Query(ctx, "SELECT "+accountCols+` FROM pf_accounts.account
	  WHERE enabled AND NOT ($1 = ANY(roles)) ORDER BY user_ref`, port.RoleFarmer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LastReactivations trả, theo tài khoản, thời điểm tạo hoặc mở khóa gần nhất ghi ở sổ account_event — tính là
// "hoạt động" để tài khoản vừa được Admin mở khóa không bị khóa lại ngay.
func LastReactivations(ctx context.Context, tx pgx.Tx) (map[uuid.UUID]time.Time, error) {
	rows, err := tx.Query(ctx, `SELECT user_ref, max(event_time) FROM pf_accounts.account_event
	  WHERE action IN ('created', 'unlocked') GROUP BY user_ref`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]time.Time{}
	for rows.Next() {
		var u uuid.UUID
		var t time.Time
		if err := rows.Scan(&u, &t); err != nil {
			return nil, err
		}
		out[u] = t.UTC()
	}
	return out, rows.Err()
}
