package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func knownCatalog(c string) error {
	if !domain.KnownCatalog(c) {
		return ErrCatalogNotFound
	}
	return nil
}

// ListItems liệt kê mục của một danh mục (phân trang theo mã).
func (s *Service) ListItems(ctx context.Context, catalog string, includeInactive bool, after string, limit int) (Page[domain.Item], error) {
	p, err := reader(ctx)
	if err != nil {
		return Page[domain.Item]{}, err
	}
	if err := knownCatalog(catalog); err != nil {
		return Page[domain.Item]{}, err
	}
	limit = clampLimit(limit)
	var items []domain.Item
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.Items(ctx, tx, catalog, includeInactive, after, limit+1)
		return e
	})
	if err != nil {
		return Page[domain.Item]{}, err
	}
	var pg Page[domain.Item]
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1].Code
		pg.Next = &last
	}
	pg.Items = items
	return pg, nil
}

// GetItem đọc một mục (kể cả đã ngừng dùng).
func (s *Service) GetItem(ctx context.Context, catalog, code string) (domain.Item, error) {
	p, err := reader(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	return s.getItem(ctx, p.TenantID, catalog, code)
}

func (s *Service) getItem(ctx context.Context, tid uuid.UUID, catalog, code string) (domain.Item, error) {
	if err := knownCatalog(catalog); err != nil {
		return domain.Item{}, err
	}
	var it *domain.Item
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		it, e = store.Item(ctx, tx, catalog, code, false)
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

// checkRefs kiểm tham chiếu trong danh mục: loại máy phải gắn một khâu đang dùng; cây trồng gắn nhóm
// (cây trồng cấp 1 đang dùng, khác chính nó); sâu bệnh gắn cây trồng bị ảnh hưởng đang dùng (KN v7.7); danh mục
// CNTS theo checkCNTSRefs (W11.41) với on là ngày áp dụng.
func checkRefs(ctx context.Context, tx pgx.Tx, it domain.Item, on time.Time) error {
	if domain.EffectiveDated(it.Catalog) {
		return checkCNTSRefs(ctx, tx, it, on)
	}
	switch it.Catalog {
	case port.CatalogCrop:
		parent := it.Attrs[port.AttrParentCode]
		if parent == "" {
			return nil
		}
		if parent == it.Code {
			return ErrCatalogRef.WithField("attrs.parent_code", "không được là chính nó")
		}
		g, err := store.Item(ctx, tx, port.CatalogCrop, parent, false)
		if err != nil {
			return err
		}
		if g == nil || !g.Active || g.Attrs[port.AttrParentCode] != "" {
			return ErrCatalogRef.WithField("attrs.parent_code", "phải là nhóm cây trồng cấp 1 đang dùng")
		}
		return nil
	case port.CatalogPest:
		for _, c := range domain.CodeList(it.Attrs[port.AttrAffectedCrops]) {
			cr, err := store.Item(ctx, tx, port.CatalogCrop, c, false)
			if err != nil {
				return err
			}
			if cr == nil || !cr.Active {
				return ErrCatalogRef.WithField("attrs.affected_crops", c)
			}
		}
		return nil
	case port.CatalogMachineType:
	default:
		return nil
	}
	st, err := store.Item(ctx, tx, port.CatalogFarmStage, it.Attrs["farm_stage"], false)
	if err != nil {
		return err
	}
	if st == nil || !st.Active {
		return ErrStageRef.WithField("attrs.farm_stage", it.Attrs["farm_stage"])
	}
	return nil
}

// writeChange ghi sổ lịch sử (và sổ hiệu lực của danh mục CNTS) rồi bảng hiện trạng trong cùng giao dịch (R-21).
func (s *Service) writeChange(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c *domain.Change, it domain.Item) error {
	if err := store.InsertChange(ctx, tx, tid, c); err != nil {
		return ledger.Classify(err)
	}
	if err := s.writeVersion(ctx, tx, tid, c, it); err != nil {
		return ledger.Classify(err)
	}
	return store.UpsertItem(ctx, tx, tid, it, c.ID)
}

// replayOnDuplicate: hai request cùng khóa chạy song song — bên thua đọc lại mục.
func (s *Service) replayOnDuplicate(ctx context.Context, tid uuid.UUID, catalog, code string, err error) (domain.Item, bool, error) {
	if !errors.Is(err, ledger.ErrDuplicate) {
		return domain.Item{}, false, err
	}
	it, rerr := s.getItem(ctx, tid, catalog, code)
	if rerr != nil {
		return domain.Item{}, false, err
	}
	return it, true, nil
}

// CreateItem thêm một mục danh mục. Gọi lặp cùng Idempotency-Key trả lại mục, replayed = true.
func (s *Service) CreateItem(ctx context.Context, in domain.Item, key string) (domain.Item, bool, error) {
	p, err := authz.Require(ctx, port.RoleMasterdataAdmin)
	if err != nil {
		return domain.Item{}, false, err
	}
	if key == "" {
		return domain.Item{}, false, problem.IdempotencyRequired
	}
	if err := knownCatalog(in.Catalog); err != nil {
		return domain.Item{}, false, err
	}
	it, err := prepareFacilityRef(in).Normalize()
	if err != nil {
		return domain.Item{}, false, asProblem(err)
	}
	if err := s.checkValidFrom(it); err != nil {
		return domain.Item{}, false, err
	}
	now := s.Clock.Now()
	it.Active, it.Revision, it.UpdatedBy, it.UpdatedAt, it.DeactivationReason = true, 1, p.ActorRef, now, ""
	it.ValidFrom = s.effectiveFrom(it)
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ChangeByIdemKey(ctx, tx, key); err != nil {
			return err
		} else if prev != nil {
			cur, err := store.Item(ctx, tx, prev.Catalog, prev.Code, false)
			if err != nil {
				return err
			}
			it, replayed = *cur, true
			return nil
		}
		if cur, err := store.Item(ctx, tx, it.Catalog, it.Code, true); err != nil {
			return err
		} else if cur != nil {
			return ErrItemExists
		}
		if err := checkRefs(ctx, tx, it, it.ValidFrom); err != nil {
			return err
		}
		if taken, err := store.ItemNameTaken(ctx, tx, it.Catalog, it.Name, it.Code); err != nil {
			return err
		} else if taken {
			return ErrItemNameTaken
		}
		c := &domain.Change{ID: ids.New(), Catalog: it.Catalog, Code: it.Code, Action: domain.ActionCreated,
			After: it.Snapshot(), EventTime: now, ActorRef: p.ActorRef, IdemKey: key}
		return s.writeChange(ctx, tx, p.TenantID, c, it)
	})
	if err != nil {
		return s.replayOnDuplicate(ctx, p.TenantID, it.Catalog, it.Code, err)
	}
	return it, replayed, nil
}

// UpdateItem sửa tên/thuộc tính. Không đổi gì thì không ghi sổ.
func (s *Service) UpdateItem(ctx context.Context, in domain.Item) (domain.Item, error) {
	p, err := authz.Require(ctx, port.RoleMasterdataAdmin)
	if err != nil {
		return domain.Item{}, err
	}
	if err := knownCatalog(in.Catalog); err != nil {
		return domain.Item{}, err
	}
	upd, err := prepareFacilityRef(in).Normalize()
	if err != nil {
		return domain.Item{}, asProblem(err)
	}
	if err := s.checkValidFrom(upd); err != nil {
		return domain.Item{}, err
	}
	upd.ValidFrom = s.effectiveFrom(upd)
	now := s.Clock.Now()
	var out domain.Item
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.Item(ctx, tx, upd.Catalog, upd.Code, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrItemNotFound
		}
		upd = keepFacilityRef(upd, *cur)
		if cur.SameContent(upd) {
			out = *cur
			return nil
		}
		if err := checkRefs(ctx, tx, upd, upd.ValidFrom); err != nil {
			return err
		}
		if upd.Name != cur.Name {
			if taken, err := store.ItemNameTaken(ctx, tx, upd.Catalog, upd.Name, upd.Code); err != nil {
				return err
			} else if taken {
				return ErrItemNameTaken
			}
		}
		next := *cur
		next.Name, next.Attrs, next.Revision, next.UpdatedBy, next.UpdatedAt = upd.Name, upd.Attrs, cur.Revision+1, p.ActorRef, now
		next.ValidFrom = upd.ValidFrom
		before := cur.Snapshot()
		c := &domain.Change{ID: ids.New(), Catalog: cur.Catalog, Code: cur.Code, Action: domain.ActionUpdated,
			Before: &before, After: next.Snapshot(), EventTime: now, ActorRef: p.ActorRef, IdemKey: "put:" + ids.New().String()}
		out = next
		return s.writeChange(ctx, tx, p.TenantID, c, next)
	})
	return out, err
}

// SetActive ngừng dùng (active = false, lý do bắt buộc) hoặc dùng lại một mục.
// Không bao giờ xóa: dữ liệu cũ tham chiếu mục vẫn giữ nguyên (CGH FN-02).
func (s *Service) SetActive(ctx context.Context, catalog, code string, active bool, reason, key string) (domain.Item, bool, error) {
	p, err := authz.Require(ctx, port.RoleMasterdataAdmin)
	if err != nil {
		return domain.Item{}, false, err
	}
	if key == "" {
		return domain.Item{}, false, problem.IdempotencyRequired
	}
	if err := knownCatalog(catalog); err != nil {
		return domain.Item{}, false, err
	}
	reason = strings.TrimSpace(reason)
	if !active || reason != "" {
		if err := domain.ValidateReason(reason); err != nil {
			return domain.Item{}, false, asProblem(err)
		}
	}
	now := s.Clock.Now()
	var out domain.Item
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ChangeByIdemKey(ctx, tx, key); err != nil {
			return err
		} else if prev != nil {
			cur, err := store.Item(ctx, tx, prev.Catalog, prev.Code, false)
			if err != nil {
				return err
			}
			out, replayed = *cur, true
			return nil
		}
		cur, err := store.Item(ctx, tx, catalog, code, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrItemNotFound
		}
		if cur.Active == active {
			return ErrItemState
		}
		if active {
			if err := checkRefs(ctx, tx, *cur, s.today()); err != nil {
				return err
			}
		}
		next := *cur
		next.Active, next.Revision, next.UpdatedBy, next.UpdatedAt = active, cur.Revision+1, p.ActorRef, now
		next.DeactivationReason = ""
		action := domain.ActionReactivated
		if !active {
			next.DeactivationReason, action = reason, domain.ActionDeactivated
		}
		before := cur.Snapshot()
		c := &domain.Change{ID: ids.New(), Catalog: catalog, Code: code, Action: action, Before: &before, After: next.Snapshot(),
			Reason: reason, EventTime: now, ActorRef: p.ActorRef, IdemKey: key}
		out = next
		return s.writeChange(ctx, tx, p.TenantID, c, next)
	})
	if err != nil {
		return s.replayOnDuplicate(ctx, p.TenantID, catalog, code, err)
	}
	return out, replayed, nil
}

// History liệt kê lịch sử thay đổi của một mục (sổ catalog_change), phân trang theo seq.
func (s *Service) History(ctx context.Context, catalog, code string, afterSeq int64, limit int) (Page[domain.Change], error) {
	p, err := authz.Require(ctx, port.RoleMasterdataAdmin)
	if err != nil {
		return Page[domain.Change]{}, err
	}
	if err := knownCatalog(catalog); err != nil {
		return Page[domain.Change]{}, err
	}
	limit = clampLimit(limit)
	var cs []domain.Change
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if it, e := store.Item(ctx, tx, catalog, code, false); e != nil {
			return e
		} else if it == nil {
			return ErrItemNotFound
		}
		var e error
		cs, e = store.Changes(ctx, tx, catalog, code, afterSeq, limit+1)
		return e
	})
	if err != nil {
		return Page[domain.Change]{}, err
	}
	var pg Page[domain.Change]
	if len(cs) > limit {
		cs = cs[:limit]
		next := strconv.FormatInt(cs[limit-1].Seq, 10)
		pg.Next = &next
	}
	pg.Items = cs
	return pg, nil
}

// FormsInUse trả số mẫu báo cáo đang dùng mỗi mã (cột "biểu đang dùng"); nil khi chưa nối P3.
func (s *Service) FormsInUse(ctx context.Context, catalog string, codes []string) map[string]int {
	if s.Forms == nil || len(codes) == 0 {
		return nil
	}
	m, err := s.Forms.FormsUsing(ctx, catalog, codes)
	if err != nil {
		return nil // cột phụ: lỗi nguồn không chặn danh sách
	}
	if m == nil {
		m = map[string]int{}
	}
	return m
}

// checkValidFrom: ngày áp dụng chỉ nhận ở danh mục CNTS (có hiệu lực theo ngày) và phải hợp lệ.
func (s *Service) checkValidFrom(it domain.Item) error {
	if it.ValidFrom.IsZero() {
		return nil
	}
	if !domain.EffectiveDated(it.Catalog) {
		return problem.Validation.WithField("valid_from", "chỉ danh mục Chăn nuôi – Thủy sản có hiệu lực theo ngày")
	}
	return asProblem(domain.ValidateValidFrom(it.ValidFrom))
}
