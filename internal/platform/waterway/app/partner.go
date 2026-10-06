package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// MaxPartnerRoutePoints là số điểm tối đa của một lời gọi định tuyến đối tác (chuyến nhiều điểm dừng).
const MaxPartnerRoutePoints = 25

// PartnerRoute là kết quả định tuyến cho bên nhận kèm tenant bên cấp đã dùng.
type PartnerRoute struct {
	Grantor uuid.UUID
	Route   port.Route
}

// PartnerRoute định tuyến đường thủy trên mạng đã phát hành (hoặc ghim version) của tenant BÊN CẤP
// cho bên nhận (tài khoản dịch vụ mkg_sync của tenant MKG — mkg/logistics, proto FM-05), sau khi kiểm
// cấp quyền loại waterway_routes (ADR-0020, G4-0). Tenant của phép định tuyến và của tham số
// (pf/masterdata) là bên cấp, đặt tường minh SAU kiểm cấp quyền; RLS vẫn áp (vai waterway_app, không
// BYPASSRLS). Ghi sổ đọc partner_read_log trước khi trả (lỗi → 503, không trả dữ liệu).
func (s *Service) PartnerRoute(ctx context.Context, grantor uuid.UUID, points []port.LatLng, minLoad decimal.Decimal, version int, endpoint, query string) (PartnerRoute, error) {
	if s.Sharing == nil {
		if _, err := authz.Require(ctx, partyport.RolePartnerSync); err != nil {
			return PartnerRoute{}, err
		}
		return PartnerRoute{}, partyport.ErrShareGrantMissing
	}
	g, err := s.Sharing.Authorize(ctx, partyport.KindWaterwayRoutes, grantor)
	if err != nil {
		return PartnerRoute{}, err
	}
	if len(points) < 2 || len(points) > MaxPartnerRoutePoints {
		return PartnerRoute{}, problem.Validation.WithField("points", "từ 2 tới 25 điểm")
	}
	if version < 0 {
		return PartnerRoute{}, problem.Validation.WithField("version", "phải là số nguyên dương")
	}
	gctx, err := tenant.WithGrant(ctx, g.Access) // H4: chỉ bằng chứng do pf/party đúc mới đổi được sang bên cấp
	if err != nil {
		return PartnerRoute{}, err
	}
	r, err := s.RouteBetween(gctx, points, minLoad, version)
	if err != nil {
		return PartnerRoute{}, err
	}
	if len(query) > 500 {
		query = query[:500]
	}
	if err := s.Sharing.RecordRead(ctx, g, partyport.PartnerRead{Kind: partyport.KindWaterwayRoutes,
		Endpoint: endpoint, Query: query, Items: len(r.Legs)}); err != nil {
		return PartnerRoute{}, err
	}
	return PartnerRoute{Grantor: g.Grantor, Route: r}, nil
}
