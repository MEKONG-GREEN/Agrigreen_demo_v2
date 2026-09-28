package port

import (
	"time"

	"github.com/google/uuid"
)

// Entity chiều xuống của pf/sync do pf/masterdata cung cấp (docs/dev/sync-contract.md).
const (
	SyncEntityCatalogItem = "catalog_item"
	SyncEntityAdminUnit   = "admin_unit"
)

// syncNS là không gian tên UUIDv5 cho mã đồng bộ của dữ liệu chủ: mục danh mục
// và đơn vị hành chính có khóa tự nhiên (mã), không có UUID riêng.
var syncNS = uuid.NewSHA1(uuid.NameSpaceURL, []byte("urn:agrigreen:pf/masterdata:sync"))

// CatalogItemSyncID là id cố định của một mục danh mục trong pull (UUIDv5 của
// danh mục + mã). Cùng mục luôn cùng id, ở mọi tenant và mọi lượt pull.
func CatalogItemSyncID(catalog, code string) uuid.UUID {
	return uuid.NewSHA1(syncNS, []byte("catalog_item:"+catalog+":"+code))
}

// AdminUnitSyncID là id cố định của một PHIÊN BẢN hiệu lực của đơn vị hành
// chính (UUIDv5 của mã + ngày bắt đầu hiệu lực): mã có thể được dùng lại cho
// đơn vị khác sau sắp xếp (vd 91), nên mã thôi không đủ.
func AdminUnitSyncID(code string, validFrom time.Time) uuid.UUID {
	return uuid.NewSHA1(syncNS, []byte("admin_unit:"+code+":"+validFrom.Format(time.DateOnly)))
}
