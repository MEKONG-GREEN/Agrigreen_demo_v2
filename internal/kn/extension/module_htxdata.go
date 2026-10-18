package extension

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/htxread"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// NewPublishedHTXData exposes the KN-owned published reader to composition roots.
// The adapter always runs with r_extension, including when passed an application pool.
func NewPublishedHTXData(pool tenant.Beginner) port.HTXData {
	return htxread.New(pool)
}
