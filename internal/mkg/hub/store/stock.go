package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

func pctArg(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.StringFixed(2)
	return &s
}

func pctScan(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ---------------------------------------------------------------------------
// Lô
// ---------------------------------------------------------------------------

// InsertLot ghi sổ lô. sources được DB kiểm lại: Σ tỷ lệ = 1 chính xác.
func InsertLot(ctx context.Context, tx pgx.Tx, l *domain.Lot, deviceID *string) error {
	src, err := json.Marshal(l.Sources)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO mkg_hub.lot
	    (id, tenant_id, stream_key, facility_id, code, product, origin, batch_id, initial_kg, moisture_pct, sources,
	     event_time, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10::numeric, $11::jsonb, $12, $13, $14, $15, $16)
	  RETURNING seq, recorded_at`,
		l.ID, l.TenantID, FacilityStream(l.FacilityID), l.FacilityID, l.Code, string(l.Product), l.Origin, l.BatchID,
		l.Initial.String(), pctArg(l.MoisturePct), string(src), l.EventTime.UTC(), l.ActorRef, deviceID, l.IdemKey,
		domain.RuleLot).Scan(&l.Seq, &l.RecordedAt)
}

const lotCols = `id, tenant_id, facility_id, seq, code, product, origin, batch_id, initial_kg::text, moisture_pct::text,
  sources::text, event_time, recorded_at, actor_ref, idem_key`

func scanLot(row pgx.Row) (domain.Lot, error) {
	var l domain.Lot
	var product, initial, sources string
	var moisture *string
	err := row.Scan(&l.ID, &l.TenantID, &l.FacilityID, &l.Seq, &l.Code, &product, &l.Origin, &l.BatchID, &initial,
		&moisture, &sources, &l.EventTime, &l.RecordedAt, &l.ActorRef, &l.IdemKey)
	if err != nil {
		return l, err
	}
	l.Product = domain.Product(product)
	if l.Initial, err = mass.ParseKg(initial); err != nil {
		return l, err
	}
	if l.MoisturePct, err = pctScan(moisture); err != nil {
		return l, err
	}
	err = json.Unmarshal([]byte(sources), &l.Sources)
	return l, err
}

func oneLot(ctx context.Context, tx pgx.Tx, where string, args ...any) (*domain.Lot, error) {
	l, err := scanLot(tx.QueryRow(ctx, "SELECT "+lotCols+" FROM mkg_hub.lot WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// LotByID đọc một lô.
func LotByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.Lot, error) {
	return oneLot(ctx, tx, "id = $1", id)
}

// LotByIdem tìm lô đã ghi với idem_key.
func LotByIdem(ctx context.Context, tx pgx.Tx, key string) (*domain.Lot, error) {
	return oneLot(ctx, tx, "idem_key = $1", key)
}

// ---------------------------------------------------------------------------
// Tồn theo lô (bảng chiếu)
// ---------------------------------------------------------------------------

// InsertBalance tạo dòng tồn khi lô được tạo (cùng giao dịch, R-21).
func InsertBalance(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, l domain.Lot, remaining mass.Kg, lastSeq int64) error {
	status := "in_stock"
	if remaining.Decimal().IsZero() {
		status = "depleted"
	}
	_, err := tx.Exec(ctx, `INSERT INTO mkg_hub.lot_balance
	    (tenant_id, lot_id, facility_id, code, product, moisture_pct, received_at, initial_kg, remaining_kg, last_seq, status)
	  VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8::numeric, $9::numeric, $10, $11)`,
		tenantID, l.ID, l.FacilityID, l.Code, string(l.Product), pctArg(l.MoisturePct), l.EventTime.UTC(),
		l.Initial.String(), remaining.String(), lastSeq, status)
	return err
}

const balanceCols = `lot_id, facility_id, code, product, moisture_pct::text, received_at, initial_kg::text, remaining_kg::text, last_seq`

func scanBalance(row pgx.Row) (domain.Balance, error) {
	var b domain.Balance
	var product, initial, remaining string
	var moisture *string
	err := row.Scan(&b.LotID, &b.FacilityID, &b.Code, &product, &moisture, &b.ReceivedAt, &initial, &remaining, &b.LastSeq)
	if err != nil {
		return b, err
	}
	b.Product = domain.Product(product)
	if b.MoisturePct, err = pctScan(moisture); err != nil {
		return b, err
	}
	if b.Initial, err = mass.ParseKg(initial); err != nil {
		return b, err
	}
	b.Remaining, err = mass.ParseKg(remaining)
	return b, err
}

// BalanceForUpdate đọc và khóa tồn của một lô.
func BalanceForUpdate(ctx context.Context, tx pgx.Tx, lotID uuid.UUID) (*domain.Balance, error) {
	b, err := scanBalance(tx.QueryRow(ctx, "SELECT "+balanceCols+" FROM mkg_hub.lot_balance WHERE lot_id = $1 FOR UPDATE", lotID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// Balances liệt kê lô còn tồn của cơ sở; forUpdate khóa theo thứ tự lot_id (tránh deadlock).
func Balances(ctx context.Context, tx pgx.Tx, facility uuid.UUID, forUpdate bool) ([]domain.Balance, error) {
	q := "SELECT " + balanceCols + ` FROM mkg_hub.lot_balance WHERE facility_id = $1 AND status = 'in_stock' ORDER BY lot_id`
	if forUpdate {
		q += " FOR UPDATE"
	}
	rows, err := tx.Query(ctx, q, facility)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Balance
	for rows.Next() {
		b, err := scanBalance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SetBalance cập nhật tồn còn lại sau một dịch chuyển (cùng giao dịch).
func SetBalance(ctx context.Context, tx pgx.Tx, lotID uuid.UUID, remaining mass.Kg, lastSeq int64) error {
	status := "in_stock"
	if remaining.Decimal().IsZero() {
		status = "depleted"
	}
	_, err := tx.Exec(ctx, `UPDATE mkg_hub.lot_balance SET remaining_kg = $2::numeric, last_seq = $3, status = $4, updated_at = now()
	  WHERE lot_id = $1`, lotID, remaining.String(), lastSeq, status)
	return err
}

// UsedKg là tổng tồn còn lại theo lô của cơ sở — lô là nguồn sự thật duy nhất.
func UsedKg(ctx context.Context, tx pgx.Tx, facility uuid.UUID) (mass.Kg, error) {
	var s string
	if err := tx.QueryRow(ctx, `SELECT coalesce(sum(remaining_kg), 0)::numeric(14,3)::text FROM mkg_hub.lot_balance
	    WHERE facility_id = $1 AND status = 'in_stock'`, facility).Scan(&s); err != nil {
		return mass.Kg{}, err
	}
	return mass.ParseKg(s)
}

// RebuildBalances dựng lại bảng chiếu tồn của tenant hiện tại từ sổ lô + sổ dịch chuyển (R-22).
func RebuildBalances(ctx context.Context, tx pgx.Tx) (int64, error) {
	if _, err := tx.Exec(ctx, "DELETE FROM mkg_hub.lot_balance"); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO mkg_hub.lot_balance
	    (tenant_id, lot_id, facility_id, code, product, moisture_pct, received_at, initial_kg, remaining_kg, last_seq, status)
	  SELECT l.tenant_id, l.id, l.facility_id, l.code, l.product, l.moisture_pct, l.event_time, l.initial_kg,
	         coalesce(m.total, 0), coalesce(m.last_seq, 0),
	         CASE WHEN coalesce(m.total, 0) = 0 THEN 'depleted' ELSE 'in_stock' END
	    FROM mkg_hub.lot l
	    LEFT JOIN (SELECT lot_id, sum(qty_kg) AS total, max(seq) AS last_seq FROM mkg_hub.stock_movement GROUP BY lot_id) m
	      ON m.lot_id = l.id`)
	return tag.RowsAffected(), err
}

// ---------------------------------------------------------------------------
// Dịch chuyển tồn
// ---------------------------------------------------------------------------

// InsertMovement ghi sổ dịch chuyển; DB tính seq theo chuỗi 'lot:<id>'.
func InsertMovement(ctx context.Context, tx pgx.Tx, m *domain.Movement, deviceID *string) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_hub.stock_movement
	    (id, tenant_id, stream_key, facility_id, lot_id, kind, qty_kg, balance_after_kg, ref_type, ref_id, event_time,
	     supersedes_id, correction_reason, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8::numeric, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	  RETURNING seq, recorded_at`,
		m.ID, m.TenantID, domain.StreamKey(m.LotID), m.FacilityID, m.LotID, string(m.Kind), m.Qty.String(), m.BalanceAfter.String(),
		m.RefType, m.RefID, m.EventTime.UTC(), m.SupersedesID, m.CorrectionReason, m.ActorRef, deviceID, m.IdemKey,
		domain.RuleMovement).Scan(&m.Seq, &m.RecordedAt)
}

const movementCols = `id, tenant_id, facility_id, lot_id, seq, kind, qty_kg::text, balance_after_kg::text, ref_type, ref_id,
  event_time, recorded_at, supersedes_id, correction_reason, actor_ref, idem_key`

func scanMovement(row pgx.Row) (domain.Movement, error) {
	var m domain.Movement
	var kind, qty, after string
	err := row.Scan(&m.ID, &m.TenantID, &m.FacilityID, &m.LotID, &m.Seq, &kind, &qty, &after, &m.RefType, &m.RefID,
		&m.EventTime, &m.RecordedAt, &m.SupersedesID, &m.CorrectionReason, &m.ActorRef, &m.IdemKey)
	if err != nil {
		return m, err
	}
	m.Kind = domain.MovementKind(kind)
	if m.Qty, err = mass.ParseKg(qty); err != nil {
		return m, err
	}
	m.BalanceAfter, err = mass.ParseKg(after)
	return m, err
}

func queryMovements(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]domain.Movement, error) {
	rows, err := tx.Query(ctx, "SELECT "+movementCols+" FROM mkg_hub.stock_movement WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Movement
	for rows.Next() {
		m, err := scanMovement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func oneMovement(ctx context.Context, tx pgx.Tx, where string, args ...any) (*domain.Movement, error) {
	ms, err := queryMovements(ctx, tx, where+" LIMIT 1", args...)
	if err != nil || len(ms) == 0 {
		return nil, err
	}
	return &ms[0], nil
}

// MovementByID đọc một dịch chuyển.
func MovementByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.Movement, error) {
	return oneMovement(ctx, tx, "id = $1", id)
}

// MovementByIdem tìm dịch chuyển đã ghi với idem_key.
func MovementByIdem(ctx context.Context, tx pgx.Tx, key string) (*domain.Movement, error) {
	return oneMovement(ctx, tx, "idem_key = $1", key)
}

// CorrectionOf tìm dòng bù đã trỏ về dịch chuyển id (mỗi dòng chỉ bù một lần).
func CorrectionOf(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.Movement, error) {
	return oneMovement(ctx, tx, "supersedes_id = $1", id)
}

// MovementsOfLot liệt kê dịch chuyển của lô theo seq.
func MovementsOfLot(ctx context.Context, tx pgx.Tx, lotID uuid.UUID) ([]domain.Movement, error) {
	return queryMovements(ctx, tx, "lot_id = $1 ORDER BY seq", lotID)
}

// MovementsByRef liệt kê dịch chuyển của một mẻ / phiếu xuất.
func MovementsByRef(ctx context.Context, tx pgx.Tx, refType string, refID uuid.UUID) ([]domain.Movement, error) {
	return queryMovements(ctx, tx, "ref_type = $1 AND ref_id = $2 ORDER BY id", refType, refID)
}

// ---------------------------------------------------------------------------
// Mẻ biến đổi và phiếu xuất
// ---------------------------------------------------------------------------

// InsertBatch ghi sổ mẻ; DB kiểm loss = input − output.
func InsertBatch(ctx context.Context, tx pgx.Tx, b *domain.Batch, deviceID *string) error {
	in, err := json.Marshal(b.Inputs)
	if err != nil {
		return err
	}
	out, err := json.Marshal(b.Outputs)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO mkg_hub.batch
	    (id, tenant_id, stream_key, facility_id, kind, inputs, outputs, input_kg, output_kg, loss_kg, event_time,
	     actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8::numeric, $9::numeric, $10::numeric, $11, $12, $13, $14, $15)
	  RETURNING seq, recorded_at`,
		b.ID, b.TenantID, FacilityStream(b.FacilityID), b.FacilityID, string(b.Kind), string(in), string(out),
		b.Input.String(), b.Output.String(), b.Loss.String(), b.EventTime.UTC(), b.ActorRef, deviceID, b.IdemKey,
		domain.RuleBatch).Scan(&b.Seq, &b.RecordedAt)
}

// BatchByIdem tìm mẻ đã ghi với idem_key.
func BatchByIdem(ctx context.Context, tx pgx.Tx, key string) (*domain.Batch, error) {
	var b domain.Batch
	var kind, in, out, inKg, outKg, loss string
	err := tx.QueryRow(ctx, `SELECT id, tenant_id, facility_id, seq, kind, inputs::text, outputs::text, input_kg::text,
	    output_kg::text, loss_kg::text, event_time, recorded_at, actor_ref, idem_key FROM mkg_hub.batch WHERE idem_key = $1`, key).
		Scan(&b.ID, &b.TenantID, &b.FacilityID, &b.Seq, &kind, &in, &out, &inKg, &outKg, &loss, &b.EventTime, &b.RecordedAt,
			&b.ActorRef, &b.IdemKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.Kind = domain.BatchKind(kind)
	if err := json.Unmarshal([]byte(in), &b.Inputs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(out), &b.Outputs); err != nil {
		return nil, err
	}
	if b.Input, err = mass.ParseKg(inKg); err != nil {
		return nil, err
	}
	if b.Output, err = mass.ParseKg(outKg); err != nil {
		return nil, err
	}
	if b.Loss, err = mass.ParseKg(loss); err != nil {
		return nil, err
	}
	return &b, nil
}

// Dispatch là một phiếu xuất (dòng sổ mkg_hub.dispatch).
type Dispatch struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	FacilityID     uuid.UUID
	Seq            int64
	Mass           mass.Kg
	DestinationRef *uuid.UUID
	Allocations    []domain.Allocation
	Strategy       string
	RuleVersion    string
	EventTime      time.Time
	ActorRef       uuid.UUID
	IdemKey        string
}

// InsertDispatch ghi sổ phiếu xuất.
func InsertDispatch(ctx context.Context, tx pgx.Tx, d *Dispatch, deviceID *string) error {
	al, err := json.Marshal(d.Allocations)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO mkg_hub.dispatch
	    (id, tenant_id, stream_key, facility_id, mass_kg, destination_ref, allocations, strategy, event_time,
	     actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5::numeric, $6, $7::jsonb, $8, $9, $10, $11, $12, $13) RETURNING seq`,
		d.ID, d.TenantID, FacilityStream(d.FacilityID), d.FacilityID, d.Mass.String(), d.DestinationRef, string(al), d.Strategy,
		d.EventTime.UTC(), d.ActorRef, deviceID, d.IdemKey, d.RuleVersion).Scan(&d.Seq)
}

// DispatchByIdem tìm phiếu xuất đã ghi với idem_key.
func DispatchByIdem(ctx context.Context, tx pgx.Tx, key string) (*Dispatch, error) {
	var d Dispatch
	var m, al string
	err := tx.QueryRow(ctx, `SELECT id, tenant_id, facility_id, seq, mass_kg::text, destination_ref, allocations::text,
	    strategy, rule_version, event_time, actor_ref, idem_key FROM mkg_hub.dispatch WHERE idem_key = $1`, key).
		Scan(&d.ID, &d.TenantID, &d.FacilityID, &d.Seq, &m, &d.DestinationRef, &al, &d.Strategy, &d.RuleVersion, &d.EventTime,
			&d.ActorRef, &d.IdemKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if d.Mass, err = mass.ParseKg(m); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(al), &d.Allocations); err != nil {
		return nil, err
	}
	return &d, nil
}
