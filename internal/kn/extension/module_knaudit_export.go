package extension

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knaudit"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// NewAuditExportFilterCodec exposes the pure owner codec for the existing audit
// registry. It does not register an export source or provide authorization.
func NewAuditExportFilterCodec(registry *accport.AuditRegistry) (*knaudit.ExportFilterCodec, error) {
	return knaudit.NewExportFilterCodec(registry)
}
