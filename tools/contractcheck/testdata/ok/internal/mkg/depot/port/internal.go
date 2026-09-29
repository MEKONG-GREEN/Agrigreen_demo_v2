// Ca hợp lệ: sự kiện nội bộ MKG (ADR-0018) — vhost mkg.internal, không qua van.
package port

import (
	"example.com/ag/internal/shared/mass"
	"github.com/google/uuid"
)

// StockMovedV1 là sự kiện nội bộ; được mang khối lượng chính xác vì không rời vùng mkg.
//
//ag:internal-event mkg.depot.stock.moved v1
type StockMovedV1 struct {
	LotRef uuid.UUID `json:"lot_ref"`
	Kg     mass.Kg   `json:"kg"`
	Note   *string   `json:"note"`
}
