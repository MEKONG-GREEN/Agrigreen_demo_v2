package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Danh sách phân trang theo con trỏ (G4 đóng khoảng trống — web nội bộ thay "mã vừa xem"). Con trỏ = khóa
// UUIDv7 cuối trang; nil = hết.

func clampLimit(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}

// LotCosts liệt kê lô có giá thành (mới nhất trước) kèm tổng hợp như GET /v1/costing/lots/{id}/cost.
func (s *Service) LotCosts(ctx context.Context, before *uuid.UUID, limit int) ([]LotCostView, *uuid.UUID, error) {
	limit = clampLimit(limit)
	var out []LotCostView
	err := s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		rows, err := store.LotsWithCosts(ctx, tx, before, limit+1)
		if err != nil {
			return err
		}
		for i := 0; i < len(rows); {
			j := i
			var cs []store.LotCost
			for j < len(rows) && rows[j].LotRef == rows[i].LotRef {
				cs = append(cs, rows[j].LotCost)
				j++
			}
			out = append(out, lotCostView(rows[i].LotRef, cs))
			i = j
		}
		return nil
	})
	var next *uuid.UUID
	if len(out) > limit {
		out = out[:limit]
		id := out[limit-1].LotRef
		next = &id
	}
	return out, next, err
}

// CarbonRevenues liệt kê khoản doanh thu carbon đã ghi (kỳ tùy chọn), mới nhất trước.
func (s *Service) CarbonRevenues(ctx context.Context, period, before *uuid.UUID, limit int) ([]store.Carbon, *uuid.UUID, error) {
	limit = clampLimit(limit)
	var out []store.Carbon
	err := s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		var e error
		out, e = store.ListCarbon(ctx, tx, period, before, limit+1)
		return e
	})
	var next *uuid.UUID
	if len(out) > limit {
		out = out[:limit]
		id := out[limit-1].ID
		next = &id
	}
	return out, next, err
}

// CloseJobs liệt kê job đóng kỳ (kỳ, trạng thái tùy chọn), mới nhất trước.
func (s *Service) CloseJobs(ctx context.Context, period *uuid.UUID, status string, before *uuid.UUID, limit int) ([]store.CloseJob, *uuid.UUID, error) {
	switch status {
	case "", "queued", "completed", "failed":
	default:
		return nil, nil, problem.Validation.WithField("status", "queued | completed | failed")
	}
	limit = clampLimit(limit)
	var out []store.CloseJob
	err := s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		var e error
		out, e = store.ListJobs(ctx, tx, period, status, before, limit+1)
		return e
	})
	var next *uuid.UUID
	if len(out) > limit {
		out = out[:limit]
		id := out[limit-1].ID
		next = &id
	}
	return out, next, err
}
