package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// RebuildProjections dựng lại bảng chiếu của tenant thẳng từ sổ (R-22, G4): vai field_supervisor, bề mặt quản trị.
func (s *Service) RebuildProjections(ctx context.Context) (map[string]int64, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return nil, err
	}
	return s.RebuildTenant(ctx, p.TenantID)
}

// RebuildTenant dựng lại bảng chiếu của tenant tid từ sổ — tenant TƯỜNG MINH, không kiểm vai: chỉ cho công cụ
// vận hành chạy trên BẢN SAO database (diễn tập R-22, cmd/rebuilddrill). API dùng RebuildProjections (có vai).
func (s *Service) RebuildTenant(ctx context.Context, tid uuid.UUID) (map[string]int64, error) {
	var out map[string]int64
	_, err := s.write(ctx, tid, func(tx pgx.Tx) error {
		var e error
		out, e = store.RebuildProjections(ctx, tx)
		return e
	}, func(pgx.Tx) (bool, error) { return false, nil })
	return out, err
}
