package app

import (
	"context"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// resolveTemplate trả phiên bản hiện hành của mẫu: phiên bản mới nhất của tenant,
// nếu chưa có thì mẫu hệ thống (phiên bản 0).
func resolveTemplate(ctx context.Context, tx pgx.Tx, key string) (domain.Template, error) {
	t, err := store.LatestTemplate(ctx, tx, key)
	if err != nil {
		return domain.Template{}, err
	}
	if t != nil {
		return *t, nil
	}
	if b, ok := domain.Builtin(key); ok {
		return b, nil
	}
	return domain.Template{}, ErrTemplateNotFound
}

// effectiveTemplates gộp mẫu hệ thống và phiên bản mới nhất của tenant.
func effectiveTemplates(ctx context.Context, tx pgx.Tx) ([]domain.Template, error) {
	latest, err := store.LatestTemplates(ctx, tx)
	if err != nil {
		return nil, err
	}
	byKey := map[string]domain.Template{}
	for _, b := range domain.Builtins() {
		byKey[b.Key] = b
	}
	for _, t := range latest {
		byKey[t.Key] = t
	}
	out := make([]domain.Template, 0, len(byKey))
	for _, t := range byKey {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// ListTemplates (Application API) trả mẫu đang dùng mà người gọi được xuất.
func (s *Service) ListTemplates(ctx context.Context) ([]domain.Template, error) {
	p, err := authz.Require(ctx, s.readerRoles()...)
	if err != nil {
		return nil, err
	}
	var all []domain.Template
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		all, e = effectiveTemplates(ctx, tx)
		return e
	}); err != nil {
		return nil, err
	}
	out := []domain.Template{}
	for _, t := range all {
		if t.Status == domain.TemplateActive && hasAny(p, s.sourceRoles(t.Def.Source)) {
			out = append(out, t)
		}
	}
	return out, nil
}

// AdminTemplates (Admin API) trả mọi mẫu (kể cả đã ngừng dùng).
func (s *Service) AdminTemplates(ctx context.Context) ([]domain.Template, error) {
	p, err := authz.Require(ctx, templateAdmin...)
	if err != nil {
		return nil, err
	}
	var out []domain.Template
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = effectiveTemplates(ctx, tx)
		return e
	})
	return out, err
}

// TemplateVersions trả lịch sử phiên bản của một mẫu (kèm mẫu hệ thống, phiên bản 0).
func (s *Service) TemplateVersions(ctx context.Context, key string) ([]domain.Template, error) {
	p, err := authz.Require(ctx, templateAdmin...)
	if err != nil {
		return nil, err
	}
	var out []domain.Template
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.TemplateVersions(ctx, tx, key)
		return e
	})
	if err != nil {
		return nil, err
	}
	if b, ok := domain.Builtin(key); ok {
		out = append(out, b)
	}
	if len(out) == 0 {
		return nil, ErrTemplateNotFound
	}
	return out, nil
}

// SaveTemplate tạo phiên bản mới của mẫu (Admin). Việc xuất đã tạo giữ ảnh chụp phiên bản cũ.
func (s *Service) SaveTemplate(ctx context.Context, key string, def domain.Definition) (domain.Template, error) {
	p, err := authz.Require(ctx, templateAdmin...)
	if err != nil {
		return domain.Template{}, err
	}
	key = strings.TrimSpace(key)
	if !domain.ValidKey(key) {
		return domain.Template{}, problem.Validation.WithField("key", "chữ thường, số, dấu chấm hoặc gạch dưới; 3–64 ký tự")
	}
	if err := def.Validate(); err != nil {
		return domain.Template{}, fieldProblem(err)
	}
	t := domain.Template{Key: key, Status: domain.TemplateActive, Def: def, CreatedBy: p.ActorRef, CreatedAt: s.now()}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error { return store.InsertTemplate(ctx, tx, p.TenantID, &t) })
	return t, err
}

// RetireTemplate ngừng dùng mẫu kèm lý do (phiên bản mới trạng thái retired).
func (s *Service) RetireTemplate(ctx context.Context, key, reason string) (domain.Template, error) {
	p, err := authz.Require(ctx, templateAdmin...)
	if err != nil {
		return domain.Template{}, err
	}
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return domain.Template{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	var t domain.Template
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := resolveTemplate(ctx, tx, key)
		if err != nil {
			return err
		}
		if cur.Status == domain.TemplateRetired {
			return ErrTemplateRetired
		}
		t = domain.Template{Key: key, Status: domain.TemplateRetired, Def: cur.Def, Reason: &reason, CreatedBy: p.ActorRef, CreatedAt: s.now()}
		return store.InsertTemplate(ctx, tx, p.TenantID, &t)
	})
	return t, err
}
