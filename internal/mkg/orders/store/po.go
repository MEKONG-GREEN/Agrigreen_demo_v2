package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// PO là ảnh chụp đơn mua.
type PO struct {
	Meta
	Kind            string
	POID            uuid.UUID
	Status          string
	Code            string
	Category        string
	SupplierRef     uuid.UUID
	ContractRef     *uuid.UUID
	ContractVersion *int
	FacilityRef     *uuid.UUID
	Unit            string
	OrderedQty      decimal.Decimal
	UnitPrice       decimal.Decimal
	PriceSource     string
	ExpectedDate    *time.Time
	Description     *string
	CreatorRef      uuid.UUID
	ApproverRef     *uuid.UUID
	Reason          *string
	LastEventID     uuid.UUID
}

const poCols = `po_id, status, code, category, supplier_ref, contract_ref, contract_version, facility_ref, unit, ordered_qty, unit_price,
  price_source, expected_date, description, creator_ref, approver_ref, reason, event_time`

const poSelect = `po_id, status, code, category, supplier_ref, contract_ref, contract_version, facility_ref, unit, ordered_qty::text,
  unit_price::text, price_source, expected_date, description, creator_ref, approver_ref, reason, event_time, last_event_id, last_seq`

func scanPO(row pgx.Row) (PO, error) {
	var p PO
	err := row.Scan(&p.POID, &p.Status, &p.Code, &p.Category, &p.SupplierRef, &p.ContractRef, &p.ContractVersion, &p.FacilityRef, &p.Unit,
		&p.OrderedQty, &p.UnitPrice, &p.PriceSource, &p.ExpectedDate, &p.Description, &p.CreatorRef, &p.ApproverRef, &p.Reason, &p.EventTime,
		&p.LastEventID, &p.Seq)
	return p, err
}

// InsertPOEvent ghi sổ đơn mua và dựng lại bảng chiếu của đơn.
func InsertPOEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p *PO) error {
	err := tx.QueryRow(ctx, `INSERT INTO mkg_orders.po_event
	    (id, tenant_id, stream_key, po_id, kind, status, code, category, supplier_ref, contract_ref, contract_version, facility_ref, unit,
	     ordered_qty, unit_price, price_source, expected_date, description, creator_ref, approver_ref, reason, event_time, supersedes_id,
	     correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::numeric, $15::numeric, $16, $17, $18, $19, $20, $21, $22, $23,
	          $24, $25, $26, $27)
	  RETURNING seq, recorded_at`,
		p.ID, tid, "po:"+p.POID.String(), p.POID, p.Kind, p.Status, p.Code, p.Category, p.SupplierRef, p.ContractRef, p.ContractVersion,
		p.FacilityRef, p.Unit, p.OrderedQty.String(), p.UnitPrice.String(), p.PriceSource, p.ExpectedDate, p.Description, p.CreatorRef,
		p.ApproverRef, p.Reason, p.EventTime.UTC(), p.SupersedesID, p.CorrectionReason, p.ActorRef, p.IdemKey, p.RuleVersion).Scan(&p.Seq, &p.RecordedAt)
	if err != nil {
		return err
	}
	_, err = rebuildProjection(ctx, tx, "po_current", "po_event", "po_id", poCols, "po_id = $1", p.POID)
	return err
}

// GetPO đọc đơn mua; forUpdate khóa dòng bảng chiếu.
func GetPO(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*PO, error) {
	q := "SELECT " + poSelect + " FROM mkg_orders.po_current WHERE po_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return noRows(scanPO(tx.QueryRow(ctx, q, id)))
}

// ListPOs liệt kê đơn mua (mới trước).
func ListPOs(ctx context.Context, tx pgx.Tx, supplier *uuid.UUID, status string, limit int) ([]PO, error) {
	rows, err := tx.Query(ctx, "SELECT "+poSelect+` FROM mkg_orders.po_current
	  WHERE ($1::uuid IS NULL OR supplier_ref = $1) AND ($2::text = '' OR status = $2::text) ORDER BY code DESC LIMIT $3`, supplier, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PO
	for rows.Next() {
		p, err := scanPO(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
