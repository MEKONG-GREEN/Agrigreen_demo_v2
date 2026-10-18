package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// Sync thực hiện sync/port.Source cho entity "catalog_item" và "admin_unit"
// (docs/dev/sync-contract.md).
//
// Cursor là sync_seq (sequence pf_masterdata.sync_seq, migration 000019) — lấy
// số mới mỗi khi dòng được ghi. version của thay đổi cũng là sync_seq, nên luôn
// tăng kể cả khi mục được dùng lại sau khi ngừng dùng.
//
//   - catalog_item: mọi mục của các danh mục dùng chung (giống lúa, khâu, loại
//     máy, vụ, dịch hại) — không lọc theo địa bàn. Mục ngừng dùng → tombstone.
//   - admin_unit: phiên bản đơn vị có mã thuộc phạm vi người gọi (All: mọi đơn
//     vị). Phiên bản đã hết hiệu lực (valid_to ≤ hôm nay) → tombstone, kể cả
//     đơn vị cũ đã sáp nhập vào một đơn vị trong phạm vi.
type Sync struct{ Svc *Service }

var _ syncport.FarmerSource = Sync{}

// FarmerEntities thực hiện syncport.FarmerSource: danh mục là dữ liệu công khai; đơn vị
// hành chính chỉ các xã của nông hộ liên kết.
func (Sync) FarmerEntities() []string {
	return []string{port.SyncEntityCatalogItem, port.SyncEntityAdminUnit}
}

// FarmerChanges thực hiện syncport.FarmerSource.
func (s Sync) FarmerChanges(ctx context.Context, fs syncport.FarmerScope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	return s.Changes(ctx, authz.Scope{Units: fs.Communes}, entity, after, limit)
}

// Entities thực hiện syncport.Source.
func (Sync) Entities() []string {
	return []string{port.SyncEntityCatalogItem, port.SyncEntityAdminUnit}
}

// SyncCatalogItem là payload của entity catalog_item (cùng trường với
// GET /v1/masterdata/catalogs/{catalog}, trừ trạng thái — mục ngừng dùng là tombstone).
type SyncCatalogItem struct {
	Catalog   string            `json:"catalog"`
	Code      string            `json:"code"`
	Name      string            `json:"name"`
	Attrs     map[string]string `json:"attrs"`
	Revision  int               `json:"revision"`
	UpdatedAt string            `json:"updated_at"`
}

// SyncAdminUnit là payload của entity admin_unit (một phiên bản hiệu lực).
type SyncAdminUnit struct {
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	Level          string   `json:"level"`
	ParentCode     *string  `json:"parent_code"`
	ValidFrom      string   `json:"valid_from"`
	ValidTo        *string  `json:"valid_to"` // khác null: đã định ngày hết hiệu lực (trong tương lai)
	SuccessorCodes []string `json:"successor_codes"`
}

// Changes thực hiện syncport.Source.
func (s Sync) Changes(ctx context.Context, scope authz.Scope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	p, err := reader(ctx)
	if err != nil {
		return nil, after, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var out []syncport.Change
	last := after
	err = s.Svc.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		switch entity {
		case port.SyncEntityCatalogItem:
			rows, err := store.SyncItems(ctx, tx, s.Svc.catalogSyncKeys(), after, limit)
			if err != nil {
				return err
			}
			for _, r := range rows {
				ch, err := itemChange(r)
				if err != nil {
					return err
				}
				out, last = append(out, ch), r.Seq
			}
		case port.SyncEntityAdminUnit:
			if scope.Empty() {
				return nil
			}
			rows, err := store.SyncUnits(ctx, tx, scope.All, scope.Units, after, limit)
			if err != nil {
				return err
			}
			today := domain.DayOf(s.Svc.Clock.Now())
			for _, r := range rows {
				ch, err := unitChange(r, today)
				if err != nil {
					return err
				}
				out, last = append(out, ch), r.Seq
			}
		default:
			return fmt.Errorf("masterdata: entity %q không thuộc module", entity)
		}
		return nil
	})
	if err != nil {
		return nil, after, err
	}
	return out, last, nil
}

func itemChange(r store.SyncItem) (syncport.Change, error) {
	it := r.Item
	ch := syncport.Change{Entity: port.SyncEntityCatalogItem, ID: port.CatalogItemSyncID(it.Catalog, it.Code), Version: r.Seq, Op: syncport.OpTombstone}
	if !it.Active {
		return ch, nil
	}
	attrs := it.Attrs
	if attrs == nil {
		attrs = map[string]string{}
	}
	data, err := json.Marshal(SyncCatalogItem{
		Catalog: it.Catalog, Code: it.Code, Name: it.Name, Attrs: attrs, Revision: it.Revision,
		UpdatedAt: it.UpdatedAt.UTC().Format(time.RFC3339Nano),
	})
	ch.Op, ch.Data = syncport.OpUpsert, data
	return ch, err
}

func unitChange(r store.SyncUnit, today time.Time) (syncport.Change, error) {
	u := r.Unit
	ch := syncport.Change{Entity: port.SyncEntityAdminUnit, ID: port.AdminUnitSyncID(u.Code, u.ValidFrom), Version: r.Seq, Op: syncport.OpTombstone}
	if u.ValidTo != nil && !u.ValidTo.After(today) {
		return ch, nil
	}
	d := SyncAdminUnit{
		Code: u.Code, Name: u.Name, Level: string(u.Level), ValidFrom: u.ValidFrom.Format(time.DateOnly),
		SuccessorCodes: u.Successors,
	}
	if d.SuccessorCodes == nil {
		d.SuccessorCodes = []string{}
	}
	if u.ParentCode != "" {
		pc := u.ParentCode
		d.ParentCode = &pc
	}
	if u.ValidTo != nil {
		vt := u.ValidTo.Format(time.DateOnly)
		d.ValidTo = &vt
	}
	data, err := json.Marshal(d)
	ch.Op, ch.Data = syncport.OpUpsert, data
	return ch, err
}
