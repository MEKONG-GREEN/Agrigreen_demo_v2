package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// SalesOrder là ảnh chụp đơn bán.
type SalesOrder struct {
	Meta
	Kind               string
	OrderID            uuid.UUID
	Status             string
	Code               string
	CustomerRef        uuid.UUID
	Product            string
	OrderedKg          decimal.Decimal
	ListPrice          decimal.NullDecimal
	UnitPrice          decimal.Decimal
	DiscountPct        decimal.Decimal
	NetPrice           decimal.Decimal
	Amount             decimal.Decimal
	NeedsPriceApproval bool
	PriceRule          string
	PaymentTermDays    int
	DeliveryFrom       *time.Time
	DeliveryTo         *time.Time
	CreatorRef         uuid.UUID
	ApproverRef        *uuid.UUID
	Reason             *string
	LastEventID        uuid.UUID
}

const orderCols = `order_id, status, code, customer_ref, product, ordered_kg, list_price, unit_price, discount_pct, net_price, amount,
  needs_price_approval, price_rule, payment_term_days, delivery_from, delivery_to, creator_ref, approver_ref, reason, event_time`

const orderSelect = `order_id, status, code, customer_ref, product, ordered_kg::text, list_price::text, unit_price::text, discount_pct::text,
  net_price::text, amount::text, needs_price_approval, price_rule, payment_term_days, delivery_from, delivery_to, creator_ref, approver_ref,
  reason, event_time, last_event_id, last_seq`

func scanOrder(row pgx.Row) (SalesOrder, error) {
	var o SalesOrder
	err := row.Scan(&o.OrderID, &o.Status, &o.Code, &o.CustomerRef, &o.Product, &o.OrderedKg, &o.ListPrice, &o.UnitPrice, &o.DiscountPct,
		&o.NetPrice, &o.Amount, &o.NeedsPriceApproval, &o.PriceRule, &o.PaymentTermDays, &o.DeliveryFrom, &o.DeliveryTo, &o.CreatorRef,
		&o.ApproverRef, &o.Reason, &o.EventTime, &o.LastEventID, &o.Seq)
	return o, err
}

// InsertOrderEvent ghi sổ đơn bán và dựng lại bảng chiếu của đơn.
func InsertOrderEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, o *SalesOrder) error {
	err := tx.QueryRow(ctx, `INSERT INTO mkg_orders.order_event
	    (id, tenant_id, stream_key, order_id, kind, status, code, customer_ref, product, ordered_kg, list_price, unit_price, discount_pct,
	     net_price, amount, needs_price_approval, price_rule, payment_term_days, delivery_from, delivery_to, creator_ref, approver_ref, reason,
	     event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::numeric, $11::numeric, $12::numeric, $13::numeric, $14::numeric, $15::numeric, $16,
	          $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29)
	  RETURNING seq, recorded_at`,
		o.ID, tid, "order:"+o.OrderID.String(), o.OrderID, o.Kind, o.Status, o.Code, o.CustomerRef, o.Product, o.OrderedKg.String(), o.ListPrice,
		o.UnitPrice.String(), o.DiscountPct.String(), o.NetPrice.String(), o.Amount.StringFixed(2), o.NeedsPriceApproval, o.PriceRule,
		o.PaymentTermDays, o.DeliveryFrom, o.DeliveryTo, o.CreatorRef, o.ApproverRef, o.Reason, o.EventTime.UTC(), o.SupersedesID,
		o.CorrectionReason, o.ActorRef, o.IdemKey, o.RuleVersion).Scan(&o.Seq, &o.RecordedAt)
	if err != nil {
		return err
	}
	_, err = rebuildProjection(ctx, tx, "order_current", "order_event", "order_id", orderCols, "order_id = $1", o.OrderID)
	return err
}

// GetOrder đọc đơn bán; forUpdate khóa dòng.
func GetOrder(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*SalesOrder, error) {
	q := "SELECT " + orderSelect + " FROM mkg_orders.order_current WHERE order_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return noRows(scanOrder(tx.QueryRow(ctx, q, id)))
}

// ListOrders liệt kê đơn bán (mới trước).
func ListOrders(ctx context.Context, tx pgx.Tx, customer *uuid.UUID, status string, limit int) ([]SalesOrder, error) {
	rows, err := tx.Query(ctx, "SELECT "+orderSelect+` FROM mkg_orders.order_current
	  WHERE ($1::uuid IS NULL OR customer_ref = $1) AND ($2::text = '' OR status = $2::text) ORDER BY code DESC LIMIT $3`, customer, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SalesOrder
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Delivery là ảnh chụp giao nhận.
type Delivery struct {
	Meta
	Kind            string
	DeliveryID      uuid.UUID
	OrderID         uuid.UUID
	Status          string
	Code            string
	CustomerRef     uuid.UUID
	Route           string
	LotRef          uuid.UUID
	TripRef         *uuid.UUID
	PlannedDate     time.Time
	PlannedKg       decimal.Decimal
	UnitPrice       decimal.Decimal
	TicketRef       *uuid.UUID
	TicketKg        decimal.NullDecimal
	AcceptedKg      decimal.NullDecimal
	Amount          decimal.NullDecimal
	AcceptedAt      *time.Time
	AcceptanceID    *uuid.UUID
	DisputeRef      *uuid.UUID
	EvidenceFileRef *uuid.UUID
	EvidenceKind    *string
	Reason          *string
	LastEventID     uuid.UUID
}

const deliveryCols = `delivery_id, order_id, status, code, customer_ref, route, lot_ref, trip_ref, planned_date, planned_kg, unit_price,
  ticket_ref, ticket_kg, accepted_kg, amount, accepted_at, acceptance_id, dispute_ref, evidence_file_ref, evidence_kind, reason, rule_version,
  event_time, recorded_at`

const deliverySelect = `delivery_id, order_id, status, code, customer_ref, route, lot_ref, trip_ref, planned_date, planned_kg::text,
  unit_price::text, ticket_ref, ticket_kg::text, accepted_kg::text, amount::text, accepted_at, acceptance_id, dispute_ref, evidence_file_ref,
  evidence_kind, reason, rule_version, event_time, recorded_at`

func scanDelivery(row pgx.Row, extra ...any) (Delivery, error) {
	var d Delivery
	dst := []any{&d.DeliveryID, &d.OrderID, &d.Status, &d.Code, &d.CustomerRef, &d.Route, &d.LotRef, &d.TripRef, &d.PlannedDate, &d.PlannedKg,
		&d.UnitPrice, &d.TicketRef, &d.TicketKg, &d.AcceptedKg, &d.Amount, &d.AcceptedAt, &d.AcceptanceID, &d.DisputeRef, &d.EvidenceFileRef,
		&d.EvidenceKind, &d.Reason, &d.RuleVersion, &d.EventTime, &d.RecordedAt}
	if len(extra) == 0 {
		dst = append(dst, &d.LastEventID, &d.Seq)
	} else {
		dst = append(dst, extra...)
	}
	err := row.Scan(dst...)
	return d, err
}

// InsertDeliveryEvent ghi sổ giao nhận và dựng lại bảng chiếu.
func InsertDeliveryEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d *Delivery) error {
	err := tx.QueryRow(ctx, `INSERT INTO mkg_orders.delivery_event
	    (id, tenant_id, stream_key, delivery_id, order_id, kind, status, code, customer_ref, route, lot_ref, trip_ref, planned_date, planned_kg,
	     unit_price, ticket_ref, ticket_kg, accepted_kg, amount, accepted_at, acceptance_id, dispute_ref, evidence_file_ref, evidence_kind,
	     reason, event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::numeric, $15::numeric, $16, $17::numeric, $18::numeric,
	          $19::numeric, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31)
	  RETURNING seq, recorded_at`,
		d.ID, tid, "delivery:"+d.DeliveryID.String(), d.DeliveryID, d.OrderID, d.Kind, d.Status, d.Code, d.CustomerRef, d.Route, d.LotRef,
		d.TripRef, d.PlannedDate, d.PlannedKg.String(), d.UnitPrice.String(), d.TicketRef, d.TicketKg, d.AcceptedKg, d.Amount, d.AcceptedAt,
		d.AcceptanceID, d.DisputeRef, d.EvidenceFileRef, d.EvidenceKind, d.Reason, d.EventTime.UTC(), d.SupersedesID, d.CorrectionReason,
		d.ActorRef, d.IdemKey, d.RuleVersion).Scan(&d.Seq, &d.RecordedAt)
	if err != nil {
		return err
	}
	_, err = rebuildProjection(ctx, tx, "delivery_current", "delivery_event", "delivery_id", deliveryCols, "delivery_id = $1", d.DeliveryID)
	return err
}

// GetDelivery đọc giao nhận; forUpdate khóa dòng.
func GetDelivery(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Delivery, error) {
	q := "SELECT " + deliverySelect + ", last_event_id, last_seq FROM mkg_orders.delivery_current WHERE delivery_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return noRows(scanDelivery(tx.QueryRow(ctx, q, id)))
}

// DeliveriesOfOrder liệt kê lịch giao của đơn bán (theo ngày dự kiến).
func DeliveriesOfOrder(ctx context.Context, tx pgx.Tx, order uuid.UUID) ([]Delivery, error) {
	return queryDeliveries(ctx, tx, "SELECT "+deliverySelect+
		", last_event_id, last_seq FROM mkg_orders.delivery_current WHERE order_id = $1 ORDER BY planned_date, code", order)
}

// OpenDeliveriesOfLot liệt kê giao nhận của lô chưa kết thúc hoặc đã chấp nhận (để gắn / bù phiếu CUSTOMER_IN).
func OpenDeliveriesOfLot(ctx context.Context, tx pgx.Tx, lot uuid.UUID) ([]Delivery, error) {
	return queryDeliveries(ctx, tx, "SELECT "+deliverySelect+`, last_event_id, last_seq FROM mkg_orders.delivery_current
	    WHERE lot_ref = $1 AND status IN ('planned', 'dispatched', 'accepted') ORDER BY code FOR UPDATE`, lot)
}

// AcceptedAsOf trả dòng 'accepted' hiệu lực của mỗi giao nhận tính tới asOf (recorded_at < asOf), với
// accepted_at ∈ [from, to), sắp theo (accepted_at, delivery_id), sau con trỏ (afterAt, afterID).
func AcceptedAsOf(ctx context.Context, tx pgx.Tx, from, to, asOf time.Time, afterAt *time.Time, afterID *uuid.UUID, limit int) ([]Delivery, error) {
	return queryDeliveries(ctx, tx, "SELECT "+deliverySelect+`, id, seq FROM (
	    SELECT DISTINCT ON (delivery_id) * FROM mkg_orders.delivery_event
	    WHERE recorded_at < $3 ORDER BY delivery_id, seq DESC) d
	  WHERE status = 'accepted' AND accepted_at >= $1 AND accepted_at < $2
	    AND ($4::timestamptz IS NULL OR (accepted_at, delivery_id) > ($4::timestamptz, $5::uuid))
	  ORDER BY accepted_at, delivery_id LIMIT $6`, from, to, asOf, afterAt, afterID, limit)
}

func queryDeliveries(ctx context.Context, tx pgx.Tx, q string, args ...any) ([]Delivery, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
