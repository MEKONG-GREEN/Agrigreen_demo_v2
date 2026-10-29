package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	periodstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	reportstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	templateapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/app"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TemplateContext previews the composer's source projection without selecting a
// publication or persisting a draft. An Admin can therefore repair a template
// whose published definition cannot currently render. Saved draft sources remain
// frozen; an unsaved preview uses the same capture and initial narrative logic.
func (s *Service) TemplateContext(ctx context.Context, key string, id uuid.UUID) (port.KHTCTemplateContext, error) {
	if _, _, err := port.RequireSession(ctx, port.SessionRoleAdmin); err != nil {
		return port.KHTCTemplateContext{}, err
	}
	c, err := s.reader(ctx)
	if err != nil {
		return port.KHTCTemplateContext{}, err
	}
	var draft *reportstore.Draft
	var term string
	var content domain.Draft
	// Use the existing period loader and tenant RLS before any cross-module source read.
	var period periodstore.Period
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		p, terminal, e := loadPeriod(ctx, tx, id)
		if e != nil {
			return e
		}
		if domain.Template(p.Type) != key {
			return templateapp.ErrInvalid.WithField("period_id", "Kỳ không thuộc mẫu đã chọn")
		}
		period, term = p, terminal
		draft, e = reportstore.LatestDraft(ctx, tx, id)
		return e
	})
	if err != nil {
		return port.KHTCTemplateContext{}, err
	}
	if draft != nil {
		content = draft.Content
	} else {
		// Capture can call ports. The period read transaction above has already ended.
		var pErr error
		var captured domain.Data
		var defaults map[string]string
		captured, defaults, pErr = s.capture(ctx, c, period)
		if pErr != nil {
			return port.KHTCTemplateContext{}, pErr
		}
		content, pErr = s.initialDraft(ctx, c, captured, defaults)
		if pErr != nil {
			return port.KHTCTemplateContext{}, pErr
		}
	}
	if content.Data.Template != key {
		return port.KHTCTemplateContext{}, templateapp.ErrInvalid.WithField("period_id", "Bản soạn không thuộc mẫu đã chọn")
	}
	return templateapp.ContextFromDraft(content, term), nil
}

// fixedTemplateSections reads only the embedded definition. Historical nil means
// the original fixed engine, while explicit legacy carries its own source frame.
func fixedTemplateSections(pinned *port.KHTCTemplateSnapshot, content domain.Draft) ([]domain.Section, error) {
	if pinned == nil {
		return domain.Sections(content.Data.Template), nil
	}
	definition, err := templatedomain.PinnedDefinition(*pinned, content.Data.Template, content.Data.Period.From)
	if err != nil {
		return nil, ErrInvalid.WithCause(err)
	}
	if pinned.Mode == "legacy" {
		var source templatedomain.LegacyDefinition
		if err := json.Unmarshal(pinned.Definition, &source); err != nil {
			return nil, ErrInvalid.WithCause(err)
		}
		var sections []domain.Section
		if err := json.Unmarshal(source.Sections, &sections); err != nil {
			return nil, ErrInvalid.WithCause(err)
		}
		return sections, nil
	}
	// Render preserves hierarchical order and computes numbering without trusting
	// protected/seeded flags from the browser. Empty input is not used to render here.
	children := map[string][]templatedomain.Node{}
	for _, node := range definition.Nodes {
		children[node.Parent] = append(children[node.Parent], node)
	}
	out := []domain.Section{}
	var visit func(string)
	visit = func(parent string) {
		nodes := children[parent]
		slices.SortFunc(nodes, func(a, b templatedomain.Node) int { return a.Order - b.Order })
		for _, n := range nodes {
			out = append(out, domain.Section{Key: n.ID, Title: n.Title, Editable: n.TextSlot})
			visit(n.ID)
		}
	}
	visit("")
	return out, nil
}

// renderFixedTemplate is pure: it never resolves "latest" or reads live source
// status while rendering a stored revision. The caller supplies its captured
// terminal state and export filter together with the frozen source content.
func renderFixedTemplate(pinned *port.KHTCTemplateSnapshot, content domain.Draft, terminal string, filter domain.Filter) (*templatedomain.Rendered, error) {
	if pinned == nil {
		return nil, nil
	}
	definition, err := templatedomain.PinnedDefinition(*pinned, content.Data.Template, content.Data.Period.From)
	if err != nil {
		return nil, ErrInvalid.WithCause(err)
	}
	if pinned.Mode == "legacy" {
		return nil, nil
	}
	if err := filter.Validate(); err != nil {
		return nil, ErrInvalid.WithCause(err)
	}
	selected := content
	selected.Data.Rows = domain.SelectRows(content.Data.Rows, filter)
	context := templateapp.ContextFromDraft(selected, terminal)
	rendered, err := templatedomain.Render(definition, context, "actual")
	if err != nil {
		return nil, ErrInvalid.WithCause(err)
	}
	return &rendered, nil
}

func cloneFixedTemplate(pinned port.KHTCTemplateSnapshot) port.KHTCTemplateSnapshot {
	out := pinned
	out.Definition = append(json.RawMessage(nil), pinned.Definition...)
	if pinned.LegacyUntil != nil {
		until := *pinned.LegacyUntil
		out.LegacyUntil = &until
	}
	return out
}

func selectFixedTemplate(ctx context.Context, provider port.KHTCFixedTemplates, content domain.Draft) (*port.KHTCTemplateSnapshot, error) {
	if !templatedomain.Fixed(content.Data.Template) {
		return nil, nil
	}
	if provider == nil {
		return nil, ErrDependency.WithDetail("Chưa nối nguồn mẫu cho bản soạn mới")
	}
	pinned, err := provider.Select(ctx, content.Data.Template, content.Data.Period.From)
	if err != nil {
		return nil, err
	}
	if _, err = templatedomain.PinnedDefinition(pinned, content.Data.Template, content.Data.Period.From); err != nil {
		return nil, ErrInvalid.WithCause(fmt.Errorf("selected template: %w", err))
	}
	cloned := cloneFixedTemplate(pinned)
	return &cloned, nil
}
