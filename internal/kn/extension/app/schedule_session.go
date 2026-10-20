package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// scheduleOwnerProvince uses only the selected owner's node, preserving the old
// maximum ancestor depth and empty province for an unrelated or missing node.
func (s *Service) scheduleOwnerProvince(ctx context.Context, k port.SessionContext) (string, error) {
	ref := k.UnitRef
	if ref == uuid.Nil {
		ref = k.OrgRef
	}
	if ref == uuid.Nil {
		return "", nil
	}
	if s.Orgs == nil {
		return "", ErrDependency.WithDetail("chưa nối cây tổ chức (pf/party)")
	}
	for range 4 {
		org, err := s.Orgs.Get(ctx, ref)
		if errors.Is(err, partyport.ErrOrgNotFound) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		switch org.Kind {
		case partyport.OrgTTKNProvince:
			return org.AdminUnitCode, nil
		case partyport.OrgTTKNNational:
			return "", nil
		}
		ref = org.ParentRef
		if ref == uuid.Nil {
			return "", nil
		}
	}
	return "", nil
}
