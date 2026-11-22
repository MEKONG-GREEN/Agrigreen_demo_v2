package domain

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
)

// InheritedFieldCode preserves the established nearest-nonempty plan field
// semantics, including the existing16-parent traversal bound.
func InheritedFieldCode(entry port.KHTCPlanEntry, entries map[uuid.UUID]port.KHTCPlanEntry) string {
	field := entry.FieldCode
	for depth := 0; entry.ParentRef != nil && depth < 16; depth++ {
		parent, ok := entries[*entry.ParentRef]
		if !ok {
			break
		}
		entry = parent
		if field == "" {
			field = parent.FieldCode
		}
	}
	return field
}
