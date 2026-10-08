package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// TenantLister trả các tenant cần chạy job cho ngày d.
type TenantLister func(ctx context.Context, d clock.Date) ([]uuid.UUID, error)

// JobTenants liệt kê tenant bằng pool ag_ops_jobs (BYPASSRLS, không có quyền
// trên pf_metrics): tenant có chuỗi sổ đã neo của ngày d (job anchor 00:30 chạy
// trước), không có neo → tenant có sự kiện outbox.
func JobTenants(pool *pgxpool.Pool) TenantLister {
	return func(ctx context.Context, d clock.Date) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, `SELECT DISTINCT tenant_id FROM shared.chain_anchors_of($1) ORDER BY 1`, domain.DateTime(d))
		if err != nil {
			return nil, err
		}
		var out []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil || len(out) > 0 {
			return out, err
		}
		rows, err = pool.Query(ctx, `SELECT DISTINCT tenant_id FROM shared.outbox ORDER BY 1`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, rows.Err()
	}
}

// SnapshotDateFor là ngày job chụp khi chạy lúc now: hôm qua theo giờ VN (job 00:45).
func SnapshotDateFor(now time.Time) clock.Date {
	return domain.AddDays(clock.BusinessDate(now.Add(-45*time.Minute)), -1)
}

// JobResult là kết quả một lần chạy job metrics_snapshot.
type JobResult struct {
	Tenants   int
	Values    RecordResult
	Snapshots int
}

// RunSnapshotJob ghi sổ giá trị chỉ số và ảnh chụp lớp GIS của ngày d cho mọi
// tenant. Idempotent: chạy lại không ghi thêm dòng trùng. Lỗi của một tenant không
// chặn tenant khác.
func (s *Service) RunSnapshotJob(ctx context.Context, tenants TenantLister, d clock.Date) (JobResult, error) {
	var res JobResult
	tids, err := tenants(ctx, d)
	if err != nil {
		return res, err
	}
	var errs []error
	for _, tid := range tids {
		res.Tenants++
		r, err := s.RecordDay(ctx, tid, d, nil)
		res.Values.add(r)
		if err != nil {
			errs = append(errs, fmt.Errorf("tenant %s chỉ số: %w", tid, err))
		}
		n, err := s.BuildSnapshots(ctx, tid, d)
		res.Snapshots += n
		if err != nil {
			errs = append(errs, fmt.Errorf("tenant %s ảnh chụp: %w", tid, err))
		}
	}
	return res, errors.Join(errs...)
}
