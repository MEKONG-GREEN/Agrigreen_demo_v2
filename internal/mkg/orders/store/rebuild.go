package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Rebuild dựng lại mọi bảng chiếu của tenant hiện tại thẳng từ sổ (R-22). Trả số dòng theo bảng.
func Rebuild(ctx context.Context, tx pgx.Tx, tid uuid.UUID) (map[string]int64, error) {
	out := map[string]int64{}
	steps := []struct {
		name, current, event, distinct, cols string
	}{
		{"contract_current", "contract_current", "contract_event", "contract_id, version", contractCols},
		{"po_current", "po_current", "po_event", "po_id", poCols},
		{"receipt_current", "receipt_current", "receipt_event", "receipt_id", receiptCols},
		{"order_current", "order_current", "order_event", "order_id", orderCols},
		{"delivery_current", "delivery_current", "delivery_event", "delivery_id", deliveryCols},
	}
	for _, s := range steps {
		n, err := rebuildProjection(ctx, tx, s.current, s.event, s.distinct, s.cols, "")
		if err != nil {
			return nil, err
		}
		out[s.name] = n
	}
	n, err := rebuildParams(ctx, tx, "")
	if err != nil {
		return nil, err
	}
	out["param_current"] = n
	if out["lot_ticket_current"], err = RebuildTickets(ctx, tx, tid); err != nil {
		return nil, err
	}
	return out, nil
}

// Projections là các bảng chiếu của module (để kiểm thử so sánh trước/sau dựng lại).
var Projections = []string{"contract_current", "po_current", "receipt_current", "order_current", "delivery_current", "param_current",
	"lot_ticket_current"}
