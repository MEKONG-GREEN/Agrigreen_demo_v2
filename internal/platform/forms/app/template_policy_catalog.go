package app

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// TemplateVersion selects an exact immutable version within the current read scope.
func (s *Service) TemplateVersion(ctx context.Context, profile, code string, version int64) (store.Template, error) {
	_, a, err := s.resolve(ctx, profile)
	if err != nil {
		return store.Template{}, err
	}
	if !domain.ValidTemplateCode(code) {
		return store.Template{}, problem.Validation.WithField("code", "mã biểu không hợp lệ")
	}
	if !a.allowsTemplate(code) {
		return store.Template{}, port.ErrTemplateNotFound
	}
	if version < 1 {
		return store.Template{}, problem.Validation.WithField("version", "phiên bản phải lớn hơn 0")
	}
	var found *store.Template
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		var e error
		found, e = store.TemplateByCodeVersion(ctx, tx, profile, code, version)
		return e
	})
	if err != nil {
		return store.Template{}, err
	}
	if found == nil {
		return store.Template{}, port.ErrTemplateNotFound
	}
	return *found, nil
}

// TemplateHistory reads bounded history without exposing codes denied by the
// owner's read filter. Locked structure does not remove a readable catalog entry.
func (s *Service) TemplateHistory(ctx context.Context, profile, code string, before int64, limit int) ([]store.Template, error) {
	_, a, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	if !domain.ValidTemplateCode(code) {
		return nil, problem.Validation.WithField("code", "mã biểu không hợp lệ")
	}
	if !a.allowsTemplate(code) {
		return nil, port.ErrTemplateNotFound
	}
	if before < 0 {
		return nil, problem.Validation.WithField("before_version", "mốc phiên bản không được âm")
	}
	if limit < 1 || limit > 200 {
		return nil, problem.Validation.WithField("limit", "giới hạn từ 1 đến 200")
	}
	var out []store.Template
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		var e error
		out, e = store.TemplateVersions(ctx, tx, profile, code, before, limit)
		return e
	})
	return out, err
}

var _ port.TemplateCatalog = Publisher{}

// ListTemplates returns the current scoped catalog, including custom codes.
func (pb Publisher) ListTemplates(ctx context.Context, profile string) ([]port.TemplateVersion, error) {
	ts, err := pb.Svc.Templates(ctx, profile)
	if err != nil {
		return nil, err
	}
	return catalogVersions(ts)
}

// TemplateVersion implements the optional immutable catalog capability.
func (pb Publisher) TemplateVersion(ctx context.Context, profile, code string, version int64) (port.TemplateVersion, error) {
	t, err := pb.Svc.TemplateVersion(ctx, profile, code, version)
	if err != nil {
		return port.TemplateVersion{}, err
	}
	return catalogVersion(t)
}

// TemplateHistory implements the optional bounded history capability.
func (pb Publisher) TemplateHistory(ctx context.Context, profile, code string, before int64, limit int) ([]port.TemplateVersion, error) {
	ts, err := pb.Svc.TemplateHistory(ctx, profile, code, before, limit)
	if err != nil {
		return nil, err
	}
	return catalogVersions(ts)
}

func catalogVersion(t store.Template) (port.TemplateVersion, error) {
	fields, err := json.Marshal(t.Fields)
	if err != nil {
		return port.TemplateVersion{}, err
	}
	return port.TemplateVersion{ID: t.ID, Profile: t.Profile, Code: t.Code, Version: t.Version, Title: t.Title, Fields: fields, Fixture: t.Fixture}, nil
}

func catalogVersions(ts []store.Template) ([]port.TemplateVersion, error) {
	out := make([]port.TemplateVersion, 0, len(ts))
	for _, t := range ts {
		v, err := catalogVersion(t)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
