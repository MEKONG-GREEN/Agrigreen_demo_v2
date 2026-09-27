package app

import (
	"context"
	"maps"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Các adapter port cho module khác. Tenant lấy từ context; không kiểm vai — module
// gọi đã kiểm vai của nghiệp vụ của nó. Lỗi "không có" bọc port.ErrUnitNotFound /
// port.ErrParamNotFound / port.ErrCatalogItemNotFound (dùng errors.Is).

// UnitsPort thực hiện port.AdminUnits.
type UnitsPort struct{ Svc *Service }

// ParamsPort thực hiện port.Params.
type ParamsPort struct{ Svc *Service }

// CatalogsPort thực hiện port.Catalogs.
type CatalogsPort struct{ Svc *Service }

var (
	_ port.AdminUnits = UnitsPort{}
	_ port.Params     = ParamsPort{}
	_ port.Catalogs   = CatalogsPort{}
)

// ToPortUnit chuyển phiên bản đơn vị sang DTO công khai (ngày → 00:00 giờ Việt Nam).
func ToPortUnit(u domain.Unit) port.AdminUnit {
	out := port.AdminUnit{
		Code: u.Code, Name: u.Name, Level: u.Level, ParentCode: u.ParentCode,
		ValidFrom: domain.DayStart(u.ValidFrom), SuccessorCodes: append([]string{}, u.Successors...),
	}
	if u.ValidTo != nil {
		t := domain.DayStart(*u.ValidTo)
		out.ValidTo = &t
	}
	return out
}

// Get thực hiện port.AdminUnits.
func (p UnitsPort) Get(ctx context.Context, code string, at time.Time) (port.AdminUnit, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return port.AdminUnit{}, err
	}
	u, err := p.Svc.getUnit(ctx, tid, code, at)
	if err != nil {
		return port.AdminUnit{}, err
	}
	return ToPortUnit(u), nil
}

// Descendants thực hiện port.AdminUnits.
func (p UnitsPort) Descendants(ctx context.Context, code string, at time.Time) ([]string, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	us, err := p.Svc.descendants(ctx, tid, code, at)
	if err != nil {
		return nil, err
	}
	codes := make([]string, len(us))
	for i, u := range us {
		codes[i] = u.Code
	}
	return codes, nil
}

// Current thực hiện port.AdminUnits.
func (p UnitsPort) Current(ctx context.Context, code string, at time.Time) ([]string, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	return p.Svc.current(ctx, tid, code, at)
}

// ToPortParam chuyển dòng sổ đã duyệt sang DTO công khai.
func ToPortParam(a domain.Approved) port.ParamValue {
	pv := port.ParamValue{Key: a.Key, Unit: a.Unit, Version: a.Version, EffectiveFrom: a.EffectiveFrom, SourceDoc: a.SourceDoc}
	if a.Value != nil {
		pv.Value = *a.Value
	}
	if a.Text != nil {
		pv.Text = *a.Text
	}
	return pv
}

// At thực hiện port.Params: phiên bản đã duyệt có hiệu lực tại thời điểm của sự việc.
func (p ParamsPort) At(ctx context.Context, key string, at time.Time) (port.ParamValue, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return port.ParamValue{}, err
	}
	a, err := p.Svc.paramAt(ctx, tid, key, at)
	if err != nil {
		return port.ParamValue{}, err
	}
	return ToPortParam(a), nil
}

// ToPortItem chuyển mục danh mục sang DTO công khai.
func ToPortItem(it domain.Item) port.CatalogItem {
	return port.CatalogItem{Catalog: it.Catalog, Code: it.Code, Name: it.Name, Attrs: maps.Clone(it.Attrs), Active: it.Active}
}

// Get thực hiện port.Catalogs (kể cả mục đã ngừng dùng, Active = false).
func (p CatalogsPort) Get(ctx context.Context, catalog, code string) (port.CatalogItem, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return port.CatalogItem{}, err
	}
	it, err := p.Svc.getItem(ctx, tid, catalog, code)
	if err != nil {
		return port.CatalogItem{}, err
	}
	return ToPortItem(it), nil
}

// List thực hiện port.Catalogs: mọi mục ĐANG DÙNG của danh mục, theo mã.
func (p CatalogsPort) List(ctx context.Context, catalog string) ([]port.CatalogItem, error) {
	if err := knownCatalog(catalog); err != nil {
		return nil, err
	}
	var out []port.CatalogItem
	err := tenant.FromCtxTx(ctx, p.Svc.DB, func(tx pgx.Tx) error {
		after := ""
		for {
			items, err := store.Items(ctx, tx, catalog, false, after, 500)
			if err != nil {
				return err
			}
			for _, it := range items {
				out = append(out, ToPortItem(it))
			}
			if len(items) < 500 {
				return nil
			}
			after = items[len(items)-1].Code
		}
	})
	return out, err
}
