package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// ---------------------------------------------------------------------------
// Xác nhận của HTX
// ---------------------------------------------------------------------------

// Confirmation là một dòng sổ htx_confirmation.
type Confirmation struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	Seq               int64
	OrderID           uuid.UUID
	PlotRef           uuid.UUID
	HTXRef            uuid.UUID
	StatementRef      uuid.UUID
	StatementRevision int
	RollCountID       uuid.UUID
	MKGRolls          int
	Outcome           string
	HTXRolls          int
	Reason            *string
	Channel           string
	EvidenceRef       *uuid.UUID
	EventTime         time.Time
	RecordedAt        time.Time
	ActorRef          uuid.UUID
	IdemKey           string
}

// InsertConfirmation ghi sổ xác nhận của HTX.
func InsertConfirmation(ctx context.Context, tx pgx.Tx, c *Confirmation) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_fieldops.htx_confirmation
	    (id, tenant_id, stream_key, order_id, plot_ref, htx_ref, statement_ref, statement_revision, roll_count_id, mkg_rolls,
	     outcome, htx_rolls, reason, channel, evidence_ref, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	  RETURNING seq, recorded_at`,
		c.ID, c.TenantID, OrderStream(c.OrderID), c.OrderID, c.PlotRef, c.HTXRef, c.StatementRef, c.StatementRevision,
		c.RollCountID, c.MKGRolls, c.Outcome, c.HTXRolls, c.Reason, c.Channel, c.EvidenceRef, c.EventTime.UTC(), c.ActorRef,
		c.IdemKey, domain.RuleConfirmation).Scan(&c.Seq, &c.RecordedAt)
}

const confirmCols = `id, tenant_id, seq, order_id, plot_ref, htx_ref, statement_ref, statement_revision, roll_count_id, mkg_rolls,
  outcome, htx_rolls, reason, channel, evidence_ref, event_time, recorded_at, actor_ref, idem_key`

func scanConfirm(row pgx.Row) (Confirmation, error) {
	var c Confirmation
	err := row.Scan(&c.ID, &c.TenantID, &c.Seq, &c.OrderID, &c.PlotRef, &c.HTXRef, &c.StatementRef, &c.StatementRevision,
		&c.RollCountID, &c.MKGRolls, &c.Outcome, &c.HTXRolls, &c.Reason, &c.Channel, &c.EvidenceRef, &c.EventTime,
		&c.RecordedAt, &c.ActorRef, &c.IdemKey)
	return c, err
}

// ConfirmationByIdem tìm xác nhận theo idem_key.
func ConfirmationByIdem(ctx context.Context, tx pgx.Tx, key string) (*Confirmation, error) {
	c, err := scanConfirm(tx.QueryRow(ctx, "SELECT "+confirmCols+" FROM mkg_fieldops.htx_confirmation WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Confirmations liệt kê xác nhận của một lệnh gom theo seq.
func Confirmations(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]Confirmation, error) {
	rows, err := tx.Query(ctx, "SELECT "+confirmCols+" FROM mkg_fieldops.htx_confirmation WHERE order_id = $1 ORDER BY seq", orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Confirmation
	for rows.Next() {
		c, err := scanConfirm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Đối soát chiều mua
// ---------------------------------------------------------------------------

// Recon là bảng chiếu buyside_reconciliation_current.
type Recon struct {
	OrderID           uuid.UUID
	PlotRef           uuid.UUID
	HTXRef            uuid.UUID
	StatementRef      uuid.UUID
	StatementRevision int
	MKGRolls          int
	MKGRollCountID    uuid.UUID
	HTXRolls          *int
	Outcome           *string
	ConfirmationID    *uuid.UUID
	ConfirmedRevision *int
	Status            string
	UpdatedAt         time.Time
}

const reconCols = `order_id, plot_ref, htx_ref, statement_ref, statement_revision, mkg_rolls, mkg_roll_count_id, htx_rolls,
  outcome, confirmation_id, confirmed_revision, status, updated_at`

func scanRecon(row pgx.Row) (Recon, error) {
	var r Recon
	err := row.Scan(&r.OrderID, &r.PlotRef, &r.HTXRef, &r.StatementRef, &r.StatementRevision, &r.MKGRolls, &r.MKGRollCountID,
		&r.HTXRolls, &r.Outcome, &r.ConfirmationID, &r.ConfirmedRevision, &r.Status, &r.UpdatedAt)
	return r, err
}

// GetRecon đọc đối soát của (lệnh gom, thửa); nil nếu chưa có bảng kê.
func GetRecon(ctx context.Context, tx pgx.Tx, orderID, plot uuid.UUID) (*Recon, error) {
	r, err := scanRecon(tx.QueryRow(ctx, "SELECT "+reconCols+` FROM mkg_fieldops.buyside_reconciliation_current
	    WHERE order_id = $1 AND plot_ref = $2`, orderID, plot))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// PutRecon ghi (thêm hoặc thay) dòng đối soát.
func PutRecon(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, r Recon) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.buyside_reconciliation_current
	    (tenant_id, order_id, plot_ref, htx_ref, statement_ref, statement_revision, mkg_rolls, mkg_roll_count_id, htx_rolls,
	     outcome, confirmation_id, confirmed_revision, status)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	  ON CONFLICT (tenant_id, order_id, plot_ref) DO UPDATE SET
	    statement_revision = EXCLUDED.statement_revision, mkg_rolls = EXCLUDED.mkg_rolls,
	    mkg_roll_count_id = EXCLUDED.mkg_roll_count_id, htx_rolls = EXCLUDED.htx_rolls, outcome = EXCLUDED.outcome,
	    confirmation_id = EXCLUDED.confirmation_id, confirmed_revision = EXCLUDED.confirmed_revision,
	    status = EXCLUDED.status, updated_at = now()`,
		tenantID, r.OrderID, r.PlotRef, r.HTXRef, r.StatementRef, r.StatementRevision, r.MKGRolls, r.MKGRollCountID, r.HTXRolls,
		r.Outcome, r.ConfirmationID, r.ConfirmedRevision, r.Status)
	return err
}

// ListRecon liệt kê đối soát, cursor = (order_id, plot_ref) cuối.
func ListRecon(ctx context.Context, tx pgx.Tx, orderID *uuid.UUID, status string, afterOrder, afterPlot uuid.UUID, limit int) ([]Recon, error) {
	rows, err := tx.Query(ctx, "SELECT "+reconCols+` FROM mkg_fieldops.buyside_reconciliation_current
	    WHERE ($1::uuid IS NULL OR order_id = $1) AND ($2 = '' OR status = $2) AND (order_id, plot_ref) > ($3, $4)
	    ORDER BY order_id, plot_ref LIMIT $5`, orderID, status, afterOrder, afterPlot, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recon
	for rows.Next() {
		r, err := scanRecon(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Ca tổ gom
// ---------------------------------------------------------------------------

// CrewShift là một dòng sổ crew_shift.
type CrewShift struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Seq           int64
	CrewRef       uuid.UUID
	OrderID       uuid.UUID
	Kind          string
	Headcount     *int
	EquipmentRefs []string
	EventTime     time.Time
	RecordedAt    time.Time
	ActorRef      uuid.UUID
	DeviceID      *string
	IdemKey       string
}

// InsertCrewShift ghi sổ ca tổ gom.
func InsertCrewShift(ctx context.Context, tx pgx.Tx, c *CrewShift) error {
	if c.EquipmentRefs == nil {
		c.EquipmentRefs = []string{}
	}
	return tx.QueryRow(ctx, `INSERT INTO mkg_fieldops.crew_shift
	    (id, tenant_id, stream_key, crew_ref, order_id, kind, headcount, equipment_refs, event_time, actor_ref, device_id,
	     idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING seq, recorded_at`,
		c.ID, c.TenantID, "crew:"+c.CrewRef.String(), c.CrewRef, c.OrderID, c.Kind, c.Headcount, c.EquipmentRefs,
		c.EventTime.UTC(), c.ActorRef, c.DeviceID, c.IdemKey, domain.RuleCrewShift).Scan(&c.Seq, &c.RecordedAt)
}

const crewCols = `id, tenant_id, seq, crew_ref, order_id, kind, headcount, equipment_refs, event_time, recorded_at, actor_ref, device_id, idem_key`

func scanCrew(row pgx.Row) (CrewShift, error) {
	var c CrewShift
	err := row.Scan(&c.ID, &c.TenantID, &c.Seq, &c.CrewRef, &c.OrderID, &c.Kind, &c.Headcount, &c.EquipmentRefs, &c.EventTime,
		&c.RecordedAt, &c.ActorRef, &c.DeviceID, &c.IdemKey)
	return c, err
}

// CrewShiftByIdem tìm ca theo idem_key.
func CrewShiftByIdem(ctx context.Context, tx pgx.Tx, key string) (*CrewShift, error) {
	c, err := scanCrew(tx.QueryRow(ctx, "SELECT "+crewCols+" FROM mkg_fieldops.crew_shift WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CrewShifts liệt kê ca của một lệnh gom.
func CrewShifts(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]CrewShift, error) {
	rows, err := tx.Query(ctx, "SELECT "+crewCols+" FROM mkg_fieldops.crew_shift WHERE order_id = $1 ORDER BY event_time, seq", orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CrewShift
	for rows.Next() {
		c, err := scanCrew(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Học kg/cuộn
// ---------------------------------------------------------------------------

// Samples trả tối đa limit lô đã cân mới nhất trước; htx = nil → toàn tenant.
func Samples(ctx context.Context, tx pgx.Tx, htx *uuid.UUID, limit int) ([]domain.WeighedSample, error) {
	rows, err := tx.Query(ctx, `SELECT order_id, rolls, weighed_kg::text FROM mkg_fieldops.weighed_loading_current
	    WHERE ($1::uuid IS NULL OR htx_ref = $1) ORDER BY weighed_at DESC, order_id DESC LIMIT $2`, htx, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.WeighedSample
	for rows.Next() {
		var s domain.WeighedSample
		var k string
		if err := rows.Scan(&s.LotRef, &s.Rolls, &k); err != nil {
			return nil, err
		}
		if s.WeighedKg, err = mass.ParseKg(k); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SamplesBefore như Samples nhưng chỉ lô cân trước before và bỏ exclude (tất định cho đối soát).
func SamplesBefore(ctx context.Context, tx pgx.Tx, htx *uuid.UUID, before time.Time, exclude uuid.UUID, limit int) ([]domain.WeighedSample, error) {
	rows, err := tx.Query(ctx, `SELECT order_id, rolls, weighed_kg::text FROM mkg_fieldops.weighed_loading_current
	    WHERE ($1::uuid IS NULL OR htx_ref = $1) AND weighed_at < $2 AND order_id <> $3
	    ORDER BY weighed_at DESC, order_id DESC LIMIT $4`, htx, before.UTC(), exclude, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.WeighedSample
	for rows.Next() {
		var s domain.WeighedSample
		var k string
		if err := rows.Scan(&s.LotRef, &s.Rolls, &k); err != nil {
			return nil, err
		}
		if s.WeighedKg, err = mass.ParseKg(k); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// WeighedTicket là phiếu nguồn của dòng bảng đệm (nil nếu dòng do làm mới tay).
type WeighedTicket struct {
	TicketRef           *uuid.UUID
	SupersedesTicketRef *uuid.UUID
}

// WeighedOf đọc phiếu nguồn của dòng bảng đệm của lô gom (nil khi chưa có dòng).
func WeighedOf(ctx context.Context, tx pgx.Tx, order uuid.UUID) (*WeighedTicket, error) {
	var w WeighedTicket
	err := tx.QueryRow(ctx, `SELECT ticket_ref, supersedes_ticket_ref FROM mkg_fieldops.weighed_loading_current WHERE order_id = $1`,
		order).Scan(&w.TicketRef, &w.SupersedesTicketRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// PutWeighedTicket ghi khối lượng cân của lô gom theo phiếu (consumer nội bộ).
func PutWeighedTicket(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, u Unweighed, kg mass.Kg, at time.Time, ticket uuid.UUID,
	supersedes *uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.weighed_loading_current
	    (tenant_id, order_id, htx_ref, rolls, weighed_kg, weighed_at, ticket_ref, supersedes_ticket_ref)
	  VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, $8)
	  ON CONFLICT (tenant_id, order_id) DO UPDATE SET rolls = EXCLUDED.rolls, weighed_kg = EXCLUDED.weighed_kg,
	    weighed_at = EXCLUDED.weighed_at, ticket_ref = EXCLUDED.ticket_ref, supersedes_ticket_ref = EXCLUDED.supersedes_ticket_ref,
	    updated_at = now()`,
		tenantID, u.OrderID, u.HTXRef, u.Rolls, kg.String(), at.UTC(), ticket, supersedes)
	return err
}

// RefreshWeighedRolls cập nhật số cuộn xếp ghe/xe của dòng bảng đệm đã có (bản ghi bù ở
// công đoạn boat_load sau khi cân). Không có dòng → không làm gì.
func RefreshWeighedRolls(ctx context.Context, tx pgx.Tx, order uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE mkg_fieldops.weighed_loading_current w SET rolls = t.n, updated_at = now()
	    FROM (SELECT sum(rolls)::int AS n FROM mkg_fieldops.roll_total_current WHERE order_id = $1 AND stage = 'boat_load') t
	    WHERE w.order_id = $1 AND t.n > 0 AND w.rolls <> t.n`, order)
	return err
}

// AllMatched cho biết mọi thửa của lô gom có đối soát chiều mua và đều match.
func AllMatched(ctx context.Context, tx pgx.Tx, order uuid.UUID, plots []uuid.UUID) (bool, error) {
	if len(plots) == 0 {
		return false, nil
	}
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM mkg_fieldops.buyside_reconciliation_current
	    WHERE order_id = $1 AND plot_ref = ANY($2::uuid[]) AND status = 'match'`, order, uuidStrings(plots)).Scan(&n)
	return n == len(plots), err
}

// Unweighed là một lô gom đã xếp ghe/xe chưa có khối lượng cân trong bảng đệm.
type Unweighed struct {
	OrderID uuid.UUID
	HTXRef  uuid.UUID
	Rolls   int
}

// UnweighedLots liệt kê lô gom có Σ cuộn xếp ghe/xe > 0 mà chưa có trong bảng đệm cân.
func UnweighedLots(ctx context.Context, tx pgx.Tx, limit int) ([]Unweighed, error) {
	rows, err := tx.Query(ctx, `SELECT t.order_id, min(t.htx_ref::text)::uuid, sum(t.rolls)::int
	    FROM mkg_fieldops.roll_total_current t
	    WHERE t.stage = 'boat_load'
	      AND NOT EXISTS (SELECT 1 FROM mkg_fieldops.weighed_loading_current w WHERE w.order_id = t.order_id)
	    GROUP BY t.order_id HAVING sum(t.rolls) > 0 ORDER BY t.order_id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Unweighed
	for rows.Next() {
		var u Unweighed
		if err := rows.Scan(&u.OrderID, &u.HTXRef, &u.Rolls); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// PutWeighed ghi khối lượng cân của một lô gom vào bảng đệm.
func PutWeighed(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, u Unweighed, kg mass.Kg, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.weighed_loading_current (tenant_id, order_id, htx_ref, rolls, weighed_kg, weighed_at)
	  VALUES ($1, $2, $3, $4, $5::numeric, $6)
	  ON CONFLICT (tenant_id, order_id) DO UPDATE SET rolls = EXCLUDED.rolls, weighed_kg = EXCLUDED.weighed_kg,
	    weighed_at = EXCLUDED.weighed_at, updated_at = now()`,
		tenantID, u.OrderID, u.HTXRef, u.Rolls, kg.String(), at.UTC())
	return err
}

// ---------------------------------------------------------------------------
// Sổ van
// ---------------------------------------------------------------------------

// ValveExport là một dòng mkg_fieldops.valve_export.
type ValveExport struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	Seq             int64
	Contract        string
	ContractVersion int
	EventType       string
	SchemaVersion   int
	SubjectRef      uuid.UUID
	OrderID         uuid.UUID
	Payload         json.RawMessage
	EventTime       time.Time
	RecordedAt      time.Time
	ActorRef        uuid.UUID
	IdemKey         string
}

// InsertValveExport ghi sổ xuất kèm SHA-256 của payload.
func InsertValveExport(ctx context.Context, tx pgx.Tx, v *ValveExport) error {
	sum := sha256.Sum256(v.Payload)
	return tx.QueryRow(ctx, `INSERT INTO mkg_fieldops.valve_export
	    (id, tenant_id, stream_key, contract, contract_version, event_type, schema_version, subject_ref, order_id, payload,
	     payload_sha256, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12, $13, $14, $15) RETURNING seq, recorded_at`,
		v.ID, v.TenantID, "valve:"+v.Contract+":"+v.SubjectRef.String(), v.Contract, v.ContractVersion, v.EventType,
		v.SchemaVersion, v.SubjectRef, v.OrderID, string(v.Payload), sum[:], v.EventTime.UTC(), v.ActorRef, v.IdemKey,
		domain.RuleValve).Scan(&v.Seq, &v.RecordedAt)
}

const valveCols = `id, tenant_id, seq, contract, contract_version, event_type, schema_version, subject_ref, order_id, payload::text,
  event_time, recorded_at, actor_ref, idem_key`

// ValveExports liệt kê bản ghi xuất của một lệnh gom, mới nhất trước.
func ValveExports(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, limit int) ([]ValveExport, error) {
	rows, err := tx.Query(ctx, "SELECT "+valveCols+` FROM mkg_fieldops.valve_export WHERE order_id = $1
	    ORDER BY recorded_at DESC, id DESC LIMIT $2`, orderID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ValveExport
	for rows.Next() {
		var v ValveExport
		var p string
		if err := rows.Scan(&v.ID, &v.TenantID, &v.Seq, &v.Contract, &v.ContractVersion, &v.EventType, &v.SchemaVersion,
			&v.SubjectRef, &v.OrderID, &p, &v.EventTime, &v.RecordedAt, &v.ActorRef, &v.IdemKey); err != nil {
			return nil, err
		}
		v.Payload = json.RawMessage(p)
		out = append(out, v)
	}
	return out, rows.Err()
}
