package excel

import (
	"strings"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

// Merge only returns detached unsaved editor values. The report returned by
// Result is deliberately not accepted: the opaque preview must belong to this
// exact codec, then its original bytes are parsed and field-validated again.
// Current application authority/status and original-file storage remain the
// future owner's responsibility, never a claim of this memory-only capability.
func (c *Codec) Merge(existing port.KHTCFormExcelContent, preview port.KHTCFormExcelPreview, mode string) (port.KHTCFormExcelContent, error) {
	p, ok := preview.(*previewData)
	if !ok || p == nil || p.owner != c || (mode != "keep" && mode != "replace") {
		return port.KHTCFormExcelContent{}, ErrPreview
	}
	if err := c.validateContentShape(existing); err != nil {
		return port.KHTCFormExcelContent{}, err
	}
	checked, err := c.Preview(p.bytes)
	if err != nil {
		return port.KHTCFormExcelContent{}, err
	}
	incoming := checked.(*previewData).valid
	out := cloneContent(existing)
	for sheet, rows := range incoming.Rows {
		if out.Rows[sheet] == nil {
			out.Rows[sheet] = map[string]map[string]string{}
		}
		for code, values := range rows {
			if out.Rows[sheet][code] == nil {
				out.Rows[sheet][code] = map[string]string{}
			}
			for letter, value := range values {
				if mode == "replace" || strings.TrimSpace(out.Rows[sheet][code][letter]) == "" {
					out.Rows[sheet][code][letter] = value
				}
			}
		}
	}
	type group struct {
		sheet, code string
		period      uuid.UUID
	}
	key := func(a port.KHTCFormExcelAdjustment) group { return group{a.Sheet, a.ItemCode, a.PeriodID} }
	existingGroups, importedGroups := map[group]bool{}, map[group]bool{}
	for _, a := range out.Adjustments {
		existingGroups[key(a)] = true
	}
	for _, a := range incoming.Adjustments {
		importedGroups[key(a)] = true
	}
	if mode == "replace" {
		kept := out.Adjustments[:0]
		for _, a := range out.Adjustments {
			if !importedGroups[key(a)] {
				kept = append(kept, a)
			}
		}
		out.Adjustments = kept
	}
	for _, a := range incoming.Adjustments {
		if mode == "replace" || !existingGroups[key(a)] {
			out.Adjustments = append(out.Adjustments, a)
		}
	}
	return out, nil
}

func cloneContent(content port.KHTCFormExcelContent) port.KHTCFormExcelContent {
	out := port.KHTCFormExcelContent{Rows: map[string]map[string]map[string]string{}, Adjustments: append([]port.KHTCFormExcelAdjustment{}, content.Adjustments...)}
	for sheet, rows := range content.Rows {
		out.Rows[sheet] = map[string]map[string]string{}
		for code, values := range rows {
			out.Rows[sheet][code] = map[string]string{}
			for letter, value := range values {
				out.Rows[sheet][code][letter] = value
			}
		}
	}
	return out
}
