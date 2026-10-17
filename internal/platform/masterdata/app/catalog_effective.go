package app

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// today là ngày nghiệp vụ hiện tại (giờ Việt Nam).
func (s *Service) today() time.Time { return domain.DayOf(s.Clock.Now()) }

// effectiveFrom: ngày áp dụng của lần ghi (zero → hôm nay).
func (s *Service) effectiveFrom(it domain.Item) time.Time {
	if it.ValidFrom.IsZero() {
		return s.today()
	}
	return it.ValidFrom
}

// writeVersion ghi dòng sổ hiệu lực cùng giao dịch với dòng catalog_change (danh mục theo ngày).
func (s *Service) writeVersion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c *domain.Change, it domain.Item) error {
	if !s.catalogEffective(it.Catalog) {
		return nil
	}
	v := &domain.Version{ID: ids.New(), Catalog: it.Catalog, Code: it.Code, ChangeID: c.ID, ValidFrom: s.effectiveFrom(it),
		After: domain.Snapshot{Name: it.Name, Attrs: it.Attrs, Active: it.Active}, EventTime: c.EventTime, ActorRef: c.ActorRef,
		IdemKey: c.IdemKey}
	return store.InsertEffective(ctx, tx, tid, v)
}

// itemAt đọc mục với giá trị có hiệu lực tại ngày d (danh mục theo ngày); mục chưa có sổ hiệu lực → bảng hiện trạng.
func (s *Service) itemAt(ctx context.Context, tx pgx.Tx, catalog, code string, d time.Time) (*domain.Item, error) {
	code = domain.CanonicalCode(catalog, code)
	if s.catalogEffective(catalog) {
		v, err := store.EffectiveAt(ctx, tx, catalog, code, d)
		if err != nil {
			return nil, err
		}
		if v != nil {
			it := v.Item()
			return &it, nil
		}
		if has, err := store.HasEffective(ctx, tx, catalog, code); err != nil || has {
			return nil, err // có sổ nhưng chưa hiệu lực tại d
		}
	}
	return store.Item(ctx, tx, catalog, code, false)
}

// ItemAt đọc một mục theo ngày (GET …/{catalog}/{code}?at=). Danh mục không theo hiệu lực → hiện trạng.
func (s *Service) ItemAt(ctx context.Context, catalog, code string, at time.Time) (domain.Item, error) {
	p, err := reader(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	if err := s.knownCatalog(catalog); err != nil {
		return domain.Item{}, err
	}
	var it *domain.Item
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		it, e = s.itemAt(ctx, tx, catalog, code, domain.DayOf(at))
		return e
	})
	if err != nil {
		return domain.Item{}, err
	}
	if it == nil {
		return domain.Item{}, ErrItemNotFound
	}
	return *it, nil
}

// ListItemsAt liệt kê mục của danh mục theo ngày với giá trị có hiệu lực tại ngày at (không phân trang — danh mục vài
// trăm mục; mục ngừng dùng tại ngày đó chỉ khi includeInactive).
func (s *Service) ListItemsAt(ctx context.Context, catalog string, at time.Time, includeInactive bool) ([]domain.Item, error) {
	p, err := reader(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.knownCatalog(catalog); err != nil {
		return nil, err
	}
	return s.listAt(ctx, p.TenantID, catalog, domain.DayOf(at), includeInactive)
}

func (s *Service) listAt(ctx context.Context, tid uuid.UUID, catalog string, d time.Time, includeInactive bool) ([]domain.Item, error) {
	var items []domain.Item
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		items, e = store.ItemsAt(ctx, tx, catalog, d)
		return e
	})
	if err != nil || includeInactive {
		return items, err
	}
	return slices.DeleteFunc(items, func(it domain.Item) bool { return !it.Active }), nil
}

// ItemVersions liệt kê các giá trị có hiệu lực từ ngày của một mục theo ngày (Admin, FN-06 "danh mục có lịch sử").
func (s *Service) ItemVersions(ctx context.Context, catalog, code string) ([]domain.Version, error) {
	p, err := authz.Require(ctx, port.RoleMasterdataAdmin)
	if err != nil {
		return nil, err
	}
	if err := s.knownCatalog(catalog); err != nil {
		return nil, err
	}
	var vs []domain.Version
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if it, e := store.Item(ctx, tx, catalog, code, false); e != nil {
			return e
		} else if it == nil {
			return ErrItemNotFound
		}
		var e error
		vs, e = store.EffectiveVersions(ctx, tx, catalog, domain.CanonicalCode(catalog, code))
		return e
	})
	return vs, err
}
