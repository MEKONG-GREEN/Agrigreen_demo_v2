package app

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
)

// editFixedTemplate keeps the original administrative validation/audit copying.
// Only declared narrative slots get an additional write representation. Source,
// calculated, plan and module tokens still have no API write field.
func editFixedTemplate(pinned *port.KHTCTemplateSnapshot, content domain.Draft, edits domain.Edits, actor uuid.UUID, at time.Time) (domain.Draft, error) {
	if pinned == nil {
		return domain.Edit(content, edits, actor, at)
	}
	definition, err := templatedomain.PinnedDefinition(*pinned, content.Data.Template, content.Data.Period.From)
	if err != nil {
		return domain.Draft{}, ErrInvalid.WithCause(err)
	}
	if pinned.Mode == "legacy" {
		if _, err = fixedTemplateSections(pinned, content); err != nil {
			return domain.Draft{}, err
		}
		return domain.Edit(content, edits, actor, at)
	}
	slots, locks := map[string]bool{}, map[string]bool{}
	for _, node := range definition.Nodes {
		slots[node.ID] = node.TextSlot
		for _, field := range node.LockedAdmin {
			locks[field] = true
		}
	}
	for field := range edits.Admin {
		if locks[field] {
			return domain.Draft{}, ErrInvalid.WithField("admin."+field, "Ô hành chính đã được Admin khóa trong mẫu này")
		}
	}
	for field, value := range edits.Texts {
		if !slots[field] || utf8.RuneCountInString(value) > templatedomain.MaxBodyRunes || strings.ContainsRune(value, 0) {
			return domain.Draft{}, ErrInvalid.WithField("texts."+field, "Vùng lời văn không hợp lệ hoặc vượt giới hạn")
		}
	}
	out, err := domain.Edit(content, domain.Edits{Admin: edits.Admin, Adhoc: edits.Adhoc}, actor, at)
	if err != nil {
		return domain.Draft{}, err
	}
	for field, value := range edits.Texts {
		out.Texts[field] = strings.TrimSpace(value)
	}
	return out, nil
}
