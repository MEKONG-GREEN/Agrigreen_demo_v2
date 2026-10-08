package app

// Vị trí hồ sơ HTX cho feed cooperative_directory (W5.5 — ADR-0033 §2): job dựng feed của pf/apiclient đọc danh bạ HTX
// của MỘT bên cấp (party/port/grantlookup) và ghép vị trí hồ sơ ở pf_coop (W3.3) qua đây — đọc bằng vai r_cooperative ở
// tenant bên cấp tường minh (RLS áp), chỉ tọa độ, không mã số thuế.

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// CoopLocations cài đặt grantlookup.CoopLocations: Nút HTX → [lon, lat] (độ, 7 chữ số thập phân) của tenant grantor;
// Nút không có hồ sơ hoặc chưa có tọa độ vắng mặt.
func (s *Service) CoopLocations(ctx context.Context, grantor uuid.UUID, refs []uuid.UUID) (map[uuid.UUID][2]string, error) {
	out := map[uuid.UUID][2]string{}
	if len(refs) == 0 {
		return out, nil
	}
	var rows map[uuid.UUID]store.Profile
	if err := tenant.WithTenantTx(ctx, s.DB, grantor, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ProfilesByRefs(ctx, tx, refs)
		return e
	}); err != nil {
		return nil, err
	}
	for k, p := range rows {
		if p.Lat != nil && p.Lon != nil {
			out[k] = [2]string{p.Lon.StringFixed(7), p.Lat.StringFixed(7)}
		}
	}
	return out, nil
}
