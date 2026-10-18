package extension

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/htxread"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/cropcyclesrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// NewPublishedHTXData exposes the KN-owned published reader to composition roots.
// The adapter always runs with r_extension, including when passed an application pool.
func NewPublishedHTXData(pool tenant.Beginner) port.HTXData {
	return htxread.New(pool)
}

// NewPublishedHTXProduction exposes the KN-owned production publication reader.
// The caller supplies an authorized tenant context, including when running a frozen job.
func NewPublishedHTXProduction(pool tenant.Beginner) cropcyclesrc.ReportData {
	return htxread.New(pool)
}
