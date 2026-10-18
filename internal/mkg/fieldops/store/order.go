// Package store: SQL của mkg/fieldops (schema mkg_fieldops, vai fieldops_app).
// Mọi hàm chạy trong giao dịch tenant do app/ mở (RLS là lớp chặn cuối, RB-2).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// OrderStream là khóa chuỗi băm của lệnh gom.
func OrderStream(id uuid.UUID) string { return "order:" + id.String() }

// LotStream là khóa chuỗi băm của sổ đếm cuộn: lô gom = lệnh gom.
func LotStream(orderID uuid.UUID) string { return "lot:" + orderID.String() }

// OrderEvent là một dòng sổ collection_order_event.
type OrderEvent struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	Seq         int64
	OrderID     uuid.UUID
	Kind        string
	Revision    int
	HTXRef      uuid.UUID
	CommuneCode string
	WindowFrom  time.Time
	WindowTo    time.Time
	PlotRefs    []uuid.UUID
	CrewRef     *uuid.UUID
	Reason      *string
	CreatorRef  uuid.UUID
	EventTime   time.Time
	RecordedAt  time.Time
	ActorRef    uuid.UUID
	DeviceID    *string
	IdemKey     string
}

// InsertOrderEvent ghi sổ lệnh gom.
func InsertOrderEvent(ctx context.Context, tx pgx.Tx, e *OrderEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_fieldops.collection_order_event
	    (id, tenant_id, stream_key, order_id, kind, revision, htx_ref, commune_code, window_from, window_to, plot_refs,
	     crew_ref, reason, creator_ref, event_time, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::uuid[], $12, $13, $14, $15, $16, $17, $18, $19)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, OrderStream(e.OrderID), e.OrderID, e.Kind, e.Revision, e.HTXRef, e.CommuneCode,
		e.WindowFrom, e.WindowTo, uuidStrings(e.PlotRefs), e.CrewRef, e.Reason, e.CreatorRef, e.EventTime.UTC(), e.ActorRef,
		e.DeviceID, e.IdemKey, domain.RuleOrder).Scan(&e.Seq, &e.RecordedAt)
}

const orderEventCols = `id, tenant_id, seq, order_id, kind, revision, htx_ref, commune_code, window_from, window_to, plot_refs::text[],
  crew_ref, reason, creator_ref, event_time, recorded_at, actor_ref, device_id, idem_key`

func scanOrderEvent(row pgx.Row) (OrderEvent, error) {
	var e OrderEvent
	var plots []string
	err := row.Scan(&e.ID, &e.TenantID, &e.Seq, &e.OrderID, &e.Kind, &e.Revision, &e.HTXRef, &e.CommuneCode, &e.WindowFrom,
		&e.WindowTo, &plots, &e.CrewRef, &e.Reason, &e.CreatorRef, &e.EventTime, &e.RecordedAt, &e.ActorRef, &e.DeviceID, &e.IdemKey)
	if err != nil {
		return e, err
	}
	e.PlotRefs, err = parseUUIDs(plots)
	return e, err
}

// uuidStrings / parseUUIDs: pgx không mã hóa []uuid.UUID của google/uuid — đi qua text[] ↔ uuid[].
func uuidStrings(xs []uuid.UUID) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = x.String()
	}
	return out
}

func parseUUIDs(xs []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, len(xs))
	for i, x := range xs {
		id, err := uuid.Parse(x)
		if err != nil {
			return nil, err
		}
		out[i] = id
	}
	return out, nil
}

// OrderEventByIdem tìm sự kiện lệnh gom đã ghi với idem_key.
func OrderEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*OrderEvent, error) {
	e, err := scanOrderEvent(tx.QueryRow(ctx, "SELECT "+orderEventCols+" FROM mkg_fieldops.collection_order_event WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// OrderEvents liệt kê lịch sử một lệnh gom theo seq.
func OrderEvents(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]OrderEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+orderEventCols+" FROM mkg_fieldops.collection_order_event WHERE order_id = $1 ORDER BY seq", orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrderEvent
	for rows.Next() {
		e, err := scanOrderEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Order là bảng chiếu collection_order_current.
type Order struct {
	TenantID    uuid.UUID
	OrderID     uuid.UUID
	Status      string
	Revision    int
	HTXRef      uuid.UUID
	CommuneCode string
	WindowFrom  time.Time
	WindowTo    time.Time
	PlotRefs    []uuid.UUID
	CrewRef     *uuid.UUID
	Reason      *string
	CreatorRef  uuid.UUID
	ApproverRef *uuid.UUID
	ApprovedAt  *time.Time
	Published   bool
	LastSeq     int64
	SyncSeq     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// HasPlot cho biết thửa thuộc lệnh gom.
func (o Order) HasPlot(p uuid.UUID) bool {
	for _, x := range o.PlotRefs {
		if x == p {
			return true
		}
	}
	return false
}

const orderCols = `tenant_id, order_id, status, revision, htx_ref, commune_code, window_from, window_to, plot_refs::text[], crew_ref,
  reason, creator_ref, approver_ref, approved_at, published, last_seq, sync_seq, created_at, updated_at`

func scanOrder(row pgx.Row) (Order, error) {
	var o Order
	var plots []string
	err := row.Scan(&o.TenantID, &o.OrderID, &o.Status, &o.Revision, &o.HTXRef, &o.CommuneCode, &o.WindowFrom, &o.WindowTo,
		&plots, &o.CrewRef, &o.Reason, &o.CreatorRef, &o.ApproverRef, &o.ApprovedAt, &o.Published, &o.LastSeq,
		&o.SyncSeq, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return o, err
	}
	o.PlotRefs, err = parseUUIDs(plots)
	return o, err
}

// GetOrder đọc lệnh gom hiện hành; forUpdate khóa dòng (tuần tự hóa mọi ghi của một lệnh gom).
func GetOrder(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Order, error) {
	q := "SELECT " + orderCols + " FROM mkg_fieldops.collection_order_current WHERE order_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	o, err := scanOrder(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// ApplyOrderEvent cập nhật bảng chiếu theo sự kiện (R-21, cùng giao dịch).
func ApplyOrderEvent(ctx context.Context, tx pgx.Tx, e OrderEvent, published bool) error {
	var approver *uuid.UUID
	var approvedAt *time.Time
	if e.Kind == domain.OrderApproved {
		a, t := e.ActorRef, e.EventTime.UTC()
		approver, approvedAt = &a, &t
	}
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.collection_order_current AS c
	    (tenant_id, order_id, status, revision, htx_ref, commune_code, window_from, window_to, plot_refs, crew_ref, reason,
	     creator_ref, approver_ref, approved_at, published, last_seq, created_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::uuid[], $10, $11, $12, $13, $14, $15, $16, $17)
	  ON CONFLICT (tenant_id, order_id) DO UPDATE SET
	    status = EXCLUDED.status, revision = EXCLUDED.revision, htx_ref = EXCLUDED.htx_ref,
	    commune_code = EXCLUDED.commune_code, window_from = EXCLUDED.window_from, window_to = EXCLUDED.window_to,
	    plot_refs = EXCLUDED.plot_refs, crew_ref = EXCLUDED.crew_ref, reason = EXCLUDED.reason,
	    creator_ref = EXCLUDED.creator_ref,
	    approver_ref = CASE WHEN EXCLUDED.status = 'planned' THEN NULL ELSE coalesce(EXCLUDED.approver_ref, c.approver_ref) END,
	    approved_at  = CASE WHEN EXCLUDED.status = 'planned' THEN NULL ELSE coalesce(EXCLUDED.approved_at, c.approved_at) END,
	    published = c.published OR EXCLUDED.published, last_seq = EXCLUDED.last_seq,
	    sync_seq = nextval('mkg_fieldops.sync_seq'), updated_at = now()`,
		e.TenantID, e.OrderID, e.Kind, e.Revision, e.HTXRef, e.CommuneCode, e.WindowFrom, e.WindowTo, uuidStrings(e.PlotRefs), e.CrewRef,
		e.Reason, e.CreatorRef, approver, approvedAt, published, e.Seq, e.EventTime.UTC())
	return err
}

// ListOrders liệt kê lệnh gom theo order_id (UUIDv7 ~ thời gian), cursor = order_id cuối.
func ListOrders(ctx context.Context, tx pgx.Tx, status string, htx *uuid.UUID, after uuid.UUID, limit int) ([]Order, error) {
	rows, err := tx.Query(ctx, "SELECT "+orderCols+` FROM mkg_fieldops.collection_order_current
	    WHERE ($1 = '' OR status = $1) AND ($2::uuid IS NULL OR htx_ref = $2) AND order_id > $3
	    ORDER BY order_id LIMIT $4`, status, htx, after, limit)
	if err != nil {
		return nil, err
	}
	return collectOrders(rows)
}

// OrdersSince trả lệnh gom có sync_seq > seq (pull của app hiện trường MKG).
func OrdersSince(ctx context.Context, tx pgx.Tx, seq int64, limit int) ([]Order, error) {
	rows, err := tx.Query(ctx, "SELECT "+orderCols+` FROM mkg_fieldops.collection_order_current
	    WHERE sync_seq > $1 ORDER BY sync_seq LIMIT $2`, seq, limit)
	if err != nil {
		return nil, err
	}
	return collectOrders(rows)
}

func collectOrders(rows pgx.Rows) ([]Order, error) {
	defer rows.Close()
	var out []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
