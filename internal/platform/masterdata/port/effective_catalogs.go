package port

import (
	"context"
	"errors"
	"time"
)

// ErrNotEffective: the item has no value effective on the requested date.
var ErrNotEffective = errors.New("masterdata: mục danh mục không có hiệu lực tại ngày tra")

// EffectiveCatalogItem carries the value and its business effective date.
type EffectiveCatalogItem struct {
	Item      CatalogItem
	ValidFrom time.Time // business date represented as midnight UTC, as stored in DATE
}

// EffectiveCatalogs reads registered catalogs within the tenant from context.
// It interprets no application attributes; business adapters belong to their owners.
type EffectiveCatalogs interface {
	ItemAt(context.Context, string, string, time.Time) (EffectiveCatalogItem, error)
	ListAt(context.Context, string, time.Time) ([]EffectiveCatalogItem, error)
	CodeByAttribute(context.Context, string, string, string) (string, error)
}
