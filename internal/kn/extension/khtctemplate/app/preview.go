package app

import (
	"context"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PreviewInput struct {
	PeriodID   uuid.UUID          `json:"period_id"`
	VersionID  *uuid.UUID         `json:"version_id"`
	Definition *domain.Definition `json:"definition"`
	Scenario   string             `json:"scenario"`
}
type Preview struct {
	Context        port.KHTCTemplateContext `json:"context"`
	Document       domain.Rendered          `json:"document"`
	DefinitionHash string                   `json:"definition_hash"`
	Scenario       string                   `json:"scenario"`
}

// Preview captures the real period outside authoring transactions, then renders
// a clone of the selected definition. Explicit simulations do not alter DB state.
func (s *Service) Preview(ctx context.Context, key string, in PreviewInput) (Preview, error) {
	c, err := s.reader(ctx, true)
	if err != nil {
		return Preview{}, err
	}
	if !domain.Fixed(key) {
		return Preview{}, ErrNotFound
	}
	if in.Definition != nil && in.VersionID != nil {
		return Preview{}, ErrInvalid
	}
	definition := domain.Seed(key)
	if in.Definition != nil {
		definition = domain.Canonical(*in.Definition)
	} else {
		err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			var v *store.Version
			var e error
			if in.VersionID != nil {
				v, e = store.ByID(ctx, tx, *in.VersionID)
			} else {
				v, e = store.Latest(ctx, tx, key)
			}
			if e != nil {
				return e
			}
			if in.VersionID != nil && v == nil {
				return ErrNotFound
			}
			if v != nil {
				if v.Key != key {
					return ErrNotFound
				}
				definition = v.Definition
			}
			return nil
		})
		if err != nil {
			return Preview{}, err
		}
	}
	if definition.Key != key {
		return Preview{}, ErrInvalid
	}
	if err = domain.Validate(definition, nil); err != nil {
		return Preview{}, inputError(err)
	}
	if s.Contexts == nil {
		return Preview{}, ErrDependency.WithDetail("Chưa nối nguồn xem trước mẫu báo cáo")
	}
	data, err := s.Contexts.TemplateContext(ctx, key, in.PeriodID)
	if err != nil {
		return Preview{}, err
	}
	if data.PeriodID == uuid.Nil {
		return Preview{}, ErrNotFound
	}
	doc, err := domain.Render(definition, data, in.Scenario)
	if err != nil {
		return Preview{}, inputError(err)
	}
	_, definitionHash, err := definition.BytesHash()
	if err != nil {
		return Preview{}, inputError(err)
	}
	return Preview{Context: data, Document: doc, DefinitionHash: definitionHash, Scenario: in.Scenario}, nil
}
