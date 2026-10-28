package app

import (
	"errors"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// AttributeCatalog selects legacy only for an absent dependency. A configured
// zero snapshot stays invalid and every consuming entry point fails closed.
func (s *Service) AttributeCatalog() *port.AttributeCatalog {
	if s.Attributes != nil {
		return s.Attributes
	}
	return port.LegacyAttributeCatalog()
}

func (s *Service) attributeCapabilities(a port.Assignment, attrs port.AssignmentAttrs) (port.AttributeCapabilities, error) {
	catalog := s.AttributeCatalog()
	if !catalog.Valid() {
		return port.AttributeCapabilities{}, ErrDependency.WithDetail("danh mục thuộc tính chưa sẵn sàng")
	}
	_, err := domain.NormalizeAttrsWithCatalog(a.Role, domain.AttrsInput{ProvinceGroup: attrs.ProvinceGroup, Duties: attrs.Duties,
		InternalUnit: attrs.InternalUnit, Facilities: attrs.Facilities, Households: attrs.Households}, catalog)
	if err != nil {
		return port.AttributeCapabilities{}, ErrContextRevoked.WithDetail("thuộc tính vai không còn trong danh mục")
	}
	caps, err := catalog.Capabilities(a.Role, a.MgmtLevel, attrs.ProvinceGroup)
	if errors.Is(err, port.ErrAttributeCatalog) {
		return port.AttributeCapabilities{}, ErrDependency
	}
	if err != nil {
		return port.AttributeCapabilities{}, ErrContextRevoked.WithDetail("thuộc tính vai không còn trong danh mục")
	}
	return caps, nil
}
