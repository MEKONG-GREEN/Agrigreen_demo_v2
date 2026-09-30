package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// Receipt là ảnh chụp phiếu nhận hàng.
type Receipt struct {
	Meta
	Kind                string
	ReceiptID           uuid.UUID
	POID                uuid.UUID
	Status              string
	Code                string
	Category            string
	SupplierRef         uuid.UUID
	LotRef              *uuid.UUID
	Unit                string
	OrderedQty          decimal.Decimal
	UnitPrice           decimal.Decimal
	TicketRef           *uuid.UUID
	TicketKg            decimal.NullDecimal
	ReceivedQty         decimal.NullDecimal
	QtySource           *string
	VariancePct         decimal.NullDecimal
	ThresholdPct        decimal.NullDecimal
	ParamVersion        *string
	NeedsVarianceReport bool
	AuditNote           *string
	ReportNote          *string
	ReportFileRef       *uuid.UUID
	ReportBy            *uuid.UUID
	CreatorRef          uuid.UUID
	ApproverRef         *uuid.UUID
	Reason              *string
	LastEventID         uuid.UUID
}

const receiptCols = `receipt_id, po_id, status, code, category, supplier_ref, lot_ref, unit, ordered_qty, unit_price, ticket_ref, ticket_kg,
  received_qty, qty_source, variance_pct, threshold_pct, param_version, needs_variance_report, audit_note, report_note, report_file_ref,
  report_by, creator_ref, approver_ref, reason, event_time`

const receiptSelect = `receipt_id, po_id, status, code, category, supplier_ref, lot_ref, unit, ordered_qty::text, unit_price::text, ticket_ref,
  ticket_kg::text, received_qty::text, qty_source, variance_pct::text, threshold_pct::text, param_version, needs_variance_report, audit_note,
  report_note, report_file_ref, report_by, creator_ref, approver_ref, reason, event_time, last_event_id, last_seq`

func scanReceipt(row pgx.Row) (Receipt, error) {
	var r Receipt
	err := row.Scan(&r.ReceiptID, &r.POID, &r.Status, &r.Code, &r.Category, &r.SupplierRef, &r.LotRef, &r.Unit, &r.OrderedQty, &r.UnitPrice,
		&r.TicketRef, &r.TicketKg, &r.ReceivedQty, &r.QtySource, &r.VariancePct, &r.ThresholdPct, &r.ParamVersion, &r.NeedsVarianceReport,
		&r.AuditNote, &r.ReportNote, &r.ReportFileRef, &r.ReportBy, &r.CreatorRef, &r.ApproverRef, &r.Reason, &r.EventTime, &r.LastEventID, &r.Seq)
	return r, err
}

// InsertReceiptEvent ghi sổ phiếu nhận hàng và dựng lại bảng chiếu của phiếu.
func InsertReceiptEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *Receipt) error {
	err := tx.QueryRow(ctx, `INSERT INTO mkg_orders.receipt_event
	    (id, tenant_id, stream_key, receipt_id, po_id, kind, status, code, category, supplier_ref, lot_ref, unit, ordered_qty, unit_price,
	     ticket_ref, ticket_kg, received_qty, qty_source, variance_pct, threshold_pct, param_version, needs_variance_report, audit_note,
	     report_note, report_file_ref, report_by, creator_ref, approver_ref, reason, event_time, supersedes_id, correction_reason, actor_ref,
	     idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::numeric, $14::numeric, $15, $16::numeric, $17::numeric, $18,
	          $19::numeric, $20::numeric, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35)
	  RETURNING seq, recorded_at`,
		r.ID, tid, "receipt:"+r.ReceiptID.String(), r.ReceiptID, r.POID, r.Kind, r.Status, r.Code, r.Category, r.SupplierRef, r.LotRef, r.Unit,
		r.OrderedQty.String(), r.UnitPrice.String(), r.TicketRef, r.TicketKg, r.ReceivedQty, r.QtySource, r.VariancePct, r.ThresholdPct,
		r.ParamVersion, r.NeedsVarianceReport, r.AuditNote, r.ReportNote, r.ReportFileRef, r.ReportBy, r.CreatorRef, r.ApproverRef, r.Reason,
		r.EventTime.UTC(), r.SupersedesID, r.CorrectionReason, r.ActorRef, r.IdemKey, r.RuleVersion).Scan(&r.Seq, &r.RecordedAt)
	if err != nil {
		return err
	}
	_, err = rebuildProjection(ctx, tx, "receipt_current", "receipt_event", "receipt_id", receiptCols, "receipt_id = $1", r.ReceiptID)
	return err
}

// GetReceipt đọc phiếu nhận hàng; forUpdate khóa dòng.
func GetReceipt(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Receipt, error) {
	q := "SELECT " + receiptSelect + " FROM mkg_orders.receipt_current WHERE receipt_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return noRows(scanReceipt(tx.QueryRow(ctx, q, id)))
}

// ReceiptsOfPO liệt kê phiếu nhận của đơn mua.
func ReceiptsOfPO(ctx context.Context, tx pgx.Tx, po uuid.UUID) ([]Receipt, error) {
	return queryReceipts(ctx, tx, "SELECT "+receiptSelect+" FROM mkg_orders.receipt_current WHERE po_id = $1 ORDER BY code", po)
}

// OpenReceiptsOfLot liệt kê phiếu nhận CHƯA quyết định gắn với lô (để gắn phiếu cân HUB_IN mới).
func OpenReceiptsOfLot(ctx context.Context, tx pgx.Tx, lot uuid.UUID) ([]Receipt, error) {
	return queryReceipts(ctx, tx, "SELECT "+receiptSelect+` FROM mkg_orders.receipt_current
	    WHERE lot_ref = $1 AND status IN ('awaiting_weigh', 'pending_approval') ORDER BY code FOR UPDATE`, lot)
}

// ApprovedReceiptsWithTicket liệt kê phiếu nhận ĐÃ DUYỆT đang gắn phiếu cân ticket (phiếu HUB_IN bị bù sau
// khi duyệt → bản bù phiếu nhận + bù khoản phải trả, RB-1). Khóa dòng.
func ApprovedReceiptsWithTicket(ctx context.Context, tx pgx.Tx, ticket uuid.UUID) ([]Receipt, error) {
	return queryReceipts(ctx, tx, "SELECT "+receiptSelect+` FROM mkg_orders.receipt_current
	    WHERE ticket_ref = $1 AND status = 'approved' ORDER BY code FOR UPDATE`, ticket)
}

func queryReceipts(ctx context.Context, tx pgx.Tx, q string, args ...any) ([]Receipt, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Receipt
	for rows.Next() {
		r, err := scanReceipt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Payable là một khoản phải trả đơn mua khác (sổ payable_event).
type Payable struct {
	Meta
	ReceiptID       uuid.UUID
	ReceiptEventID  uuid.UUID
	POID            uuid.UUID
	Category        string
	SupplierRef     uuid.UUID
	Unit            string
	Qty             decimal.Decimal
	UnitPrice       decimal.Decimal
	Amount          decimal.Decimal
	DueDate         time.Time
	PaymentTermDays int
	ParamVersion    string
}

// InsertPayable ghi khoản phải trả (idem_key 'payable:<receipt_id>' → đúng một khoản mỗi phiếu nhận). Bản bù
// (phiếu HUB_IN bù sau khi duyệt, RB-1) mang supersedes_id = dòng trước + lý do, cùng stream 'payable:<receipt_id>'.
func InsertPayable(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p *Payable) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_orders.payable_event
	    (id, tenant_id, stream_key, receipt_id, receipt_event_id, po_id, category, supplier_ref, unit, qty, unit_price, amount, due_date,
	     payment_term_days, param_version, event_time, actor_ref, idem_key, rule_version, supersedes_id, correction_reason)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::numeric, $11::numeric, $12::numeric, $13, $14, $15, $16, $17, $18, $19, $20, $21)
	  RETURNING seq, recorded_at`,
		p.ID, tid, "payable:"+p.ReceiptID.String(), p.ReceiptID, p.ReceiptEventID, p.POID, p.Category, p.SupplierRef, p.Unit, p.Qty.String(),
		p.UnitPrice.String(), p.Amount.StringFixed(2), p.DueDate, p.PaymentTermDays, p.ParamVersion, p.EventTime.UTC(), p.ActorRef, p.IdemKey,
		p.RuleVersion, p.SupersedesID, p.CorrectionReason).Scan(&p.Seq, &p.RecordedAt)
}

const payableSelect = `id, seq, recorded_at, event_time, actor_ref, idem_key, rule_version, receipt_id, receipt_event_id, po_id, category,
  supplier_ref, unit, qty::text, unit_price::text, amount::text, due_date, payment_term_days, param_version, supersedes_id, correction_reason`

func scanPayable(row pgx.Row) (Payable, error) {
	var p Payable
	err := row.Scan(&p.ID, &p.Seq, &p.RecordedAt, &p.EventTime, &p.ActorRef, &p.IdemKey, &p.RuleVersion, &p.ReceiptID, &p.ReceiptEventID,
		&p.POID, &p.Category, &p.SupplierRef, &p.Unit, &p.Qty, &p.UnitPrice, &p.Amount, &p.DueDate, &p.PaymentTermDays, &p.ParamVersion,
		&p.SupersedesID, &p.CorrectionReason)
	return p, err
}

// Payables liệt kê khoản phải trả theo (recorded_at, id) sau con trỏ.
func Payables(ctx context.Context, tx pgx.Tx, po *uuid.UUID, after *uuid.UUID, limit int) ([]Payable, error) {
	rows, err := tx.Query(ctx, "SELECT "+payableSelect+` FROM mkg_orders.payable_event
	  WHERE ($1::uuid IS NULL OR po_id = $1) AND ($2::uuid IS NULL OR id > $2) ORDER BY id LIMIT $3`, po, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payable
	for rows.Next() {
		p, err := scanPayable(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PayableOfReceipt trả khoản phải trả HIỆU LỰC của phiếu nhận — dòng mới nhất của stream (bản bù thay bản
// trước); nil = không có.
func PayableOfReceipt(ctx context.Context, tx pgx.Tx, receipt uuid.UUID) (*Payable, error) {
	return noRows(scanPayable(tx.QueryRow(ctx, "SELECT "+payableSelect+` FROM mkg_orders.payable_event WHERE receipt_id = $1
	    ORDER BY seq DESC LIMIT 1`, receipt)))
}
