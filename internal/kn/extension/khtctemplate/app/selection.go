package app

import (
	"context"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/jackc/pgx/v5"
)

// Select rolls out independently for each fixed key and period start. A lookup,
// permission, hash or version failure must never switch a report to builtin mode.
func (s *Service) Select(ctx context.Context, key string, start time.Time) (port.KHTCTemplateSnapshot, error) {
	c, err := s.reader(ctx, false)
	if err != nil {
		return port.KHTCTemplateSnapshot{}, err
	}
	if !domain.Fixed(key) {
		return port.KHTCTemplateSnapshot{}, ErrNotFound
	}
	if start.IsZero() || start.Year() < 2000 || start.Year() > 2100 {
		return port.KHTCTemplateSnapshot{}, ErrInvalid.WithField("period_start", "Ngày bắt đầu kỳ không hợp lệ")
	}
	var out port.KHTCTemplateSnapshot
	var legacy bool
	var boundary *time.Time
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		boundary, e = store.FirstEffective(ctx, tx, key)
		if e != nil {
			return e
		}
		if boundary == nil || start.Before(*boundary) {
			legacy = true
			return nil
		}
		publication, e := store.Applicable(ctx, tx, key, start)
		if e != nil {
			return e
		}
		if publication == nil {
			return ErrUnpublished
		}
		v, e := store.ByID(ctx, tx, publication.VersionID)
		if e != nil {
			return e
		}
		if v == nil {
			return ErrNotFound
		}
		if v.Key != key || v.Version != publication.Version || v.Hash != publication.Hash {
			return ErrInvalid.WithDetail("Tham chiếu phiên bản phát hành không khớp")
		}
		out, e = snapshot(*v, publication.AppliesFrom)
		return e
	})
	if err != nil {
		return port.KHTCTemplateSnapshot{}, err
	}
	if legacy {
		pinned, ok := s.LegacyTemplates[key]
		if !ok || pinned.Key != key {
			return port.KHTCTemplateSnapshot{}, ErrDependency.WithDetail("Chưa đăng ký định nghĩa mẫu nền")
		}
		if err = domain.ValidateLegacy(pinned); err != nil {
			return port.KHTCTemplateSnapshot{}, inputError(err)
		}
		out = pinned
		out.Definition = append([]byte(nil), pinned.Definition...)
		if boundary != nil {
			until := *boundary
			out.LegacyUntil = &until
		}
	}
	return out, nil
}
