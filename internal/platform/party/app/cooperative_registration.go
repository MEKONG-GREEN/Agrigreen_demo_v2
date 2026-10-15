package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
)

// RegisterCooperative là đường nội bộ cho D2; POST tổ chức chung không tạo HTX.
// Khóa ổn định giữ cơ chế hoàn tất hồ sơ hai bước khi gửi lại (coop.go).
func (r *Registry) RegisterCooperative(ctx context.Context, in port.CooperativeRegistration) (uuid.UUID, error) {
	if _, err := authz.Require(ctx, orgNodeRoles...); err != nil {
		return uuid.Nil, err
	}
	profile := &domain.CooperativeProfile{TaxCode: in.TaxCode, AreaHa: in.AreaHa, EstablishedOn: in.EstablishedOn, Address: in.Address}
	if in.Lat != nil && in.Lon != nil {
		profile.Location = &domain.Point{Lat: *in.Lat, Lon: *in.Lon}
	}
	req := CreateOrgInput{Kind: port.OrgCooperative, Name: in.Name, AdminUnitCode: in.CommuneCode,
		ParentRef: in.ParentRef, IdemKey: in.IdemKey, Cooperative: profile, UniqueName: true, RepresentativeRef: in.RepresentativeRef}
	if in.Representative != nil {
		req.Representative = &Representative{FullName: in.Representative.FullName, Phone: in.Representative.Phone}
	}
	out, err := r.CreateOrg(ctx, req)
	return out.Org.Ref, err
}

var _ port.CooperativeRegistrar = (*Registry)(nil)
