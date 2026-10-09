package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// DisputeEvent là một dòng sổ dispute_event.
type DisputeEvent struct {
	ID               uuid.UUID
	Seq              int64
	DisputeID        uuid.UUID
	LotRef           uuid.UUID
	Action           string
	StatusAfter      string
	ReconciliationID *uuid.UUID
	Severity         *string
	HubOut           *mass.Kg
	CustomerIn       *mass.Kg
	Diff             *mass.Kg
	DiffPct          *decimal.Decimal
	EvidenceRefs     []uuid.UUID
	Resolution       *string
	AgreedKg         *mass.Kg
	Note             *string
	EventTime        time.Time
	RecordedAt       time.Time
	ActorRef         uuid.UUID
	IdemKey          string
}

// InsertDisputeEvent ghi sổ tranh chấp.
func InsertDisputeEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *DisputeEvent) error {
	refs := e.EvidenceRefs
	if refs == nil {
		refs = []uuid.UUID{}
	}
	b, err := json.Marshal(refs)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO mkg_measurement.dispute_event
	    (id, tenant_id, stream_key, dispute_id, lot_ref, action, status_after, reconciliation_id, severity, hub_out_kg, customer_in_kg,
	     diff_kg, diff_pct, evidence_refs, resolution, agreed_kg, note, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::numeric, $11::numeric, $12::numeric, $13::numeric, $14::jsonb, $15, $16::numeric,
	          $17, $18, $19, $20, $21) RETURNING seq, recorded_at`,
		e.ID, tid, "dispute:"+e.DisputeID.String(), e.DisputeID, e.LotRef, e.Action, e.StatusAfter, e.ReconciliationID, e.Severity,
		kgArg(e.HubOut), kgArg(e.CustomerIn), kgArg(e.Diff), decArg(e.DiffPct), string(b), e.Resolution, kgArg(e.AgreedKg), e.Note,
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.RuleDispute).Scan(&e.Seq, &e.RecordedAt)
}

// DisputeEventByIdem trả dispute_id của sự kiện có idem_key.
func DisputeEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, "SELECT dispute_id FROM mkg_measurement.dispute_event WHERE idem_key = $1"+ledger.NewestByIdem(""), key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &id, err
}

// DisputeEvents liệt kê lịch sử một hồ sơ.
func DisputeEvents(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]DisputeEvent, error) {
	rows, err := tx.Query(ctx, `SELECT id, seq, dispute_id, lot_ref, action, status_after, evidence_refs::text, resolution, agreed_kg::text,
	    note, event_time, recorded_at, actor_ref FROM mkg_measurement.dispute_event WHERE stream_key = $1 ORDER BY seq`, "dispute:"+id.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DisputeEvent
	for rows.Next() {
		var e DisputeEvent
		var refs string
		var agreed *string
		if err := rows.Scan(&e.ID, &e.Seq, &e.DisputeID, &e.LotRef, &e.Action, &e.StatusAfter, &refs, &e.Resolution, &agreed, &e.Note,
			&e.EventTime, &e.RecordedAt, &e.ActorRef); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(refs), &e.EvidenceRefs); err != nil {
			return nil, err
		}
		if e.AgreedKg, err = kgScan(agreed); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Dispute là một dòng dispute_current.
type Dispute struct {
	ID               uuid.UUID
	LotRef           uuid.UUID
	Status           string
	Severity         string
	ReconciliationID uuid.UUID
	HubOut           *mass.Kg
	CustomerIn       *mass.Kg
	Diff             mass.Kg
	DiffPct          *decimal.Decimal
	EvidenceRefs     []uuid.UUID
	Resolution       *string
	AgreedKg         *mass.Kg
	OpenedAt         time.Time
	OpenedBy         uuid.UUID
	LastSeq          int64
}

const disputeCols = `dispute_id, lot_ref, status, severity, reconciliation_id, hub_out_kg::text, customer_in_kg::text, diff_kg::text,
  diff_pct::text, evidence_refs::text, resolution, agreed_kg::text, opened_at, opened_by, last_seq`

func scanDispute(row pgx.Row) (Dispute, error) {
	var d Dispute
	var hout, cin, pct, agreed *string
	var diff, refs string
	err := row.Scan(&d.ID, &d.LotRef, &d.Status, &d.Severity, &d.ReconciliationID, &hout, &cin, &diff, &pct, &refs, &d.Resolution,
		&agreed, &d.OpenedAt, &d.OpenedBy, &d.LastSeq)
	if err != nil {
		return d, err
	}
	if d.HubOut, err = kgScan(hout); err != nil {
		return d, err
	}
	if d.CustomerIn, err = kgScan(cin); err != nil {
		return d, err
	}
	if d.Diff, err = mass.ParseKg(diff); err != nil {
		return d, err
	}
	if d.DiffPct, err = decScan(pct); err != nil {
		return d, err
	}
	if d.AgreedKg, err = kgScan(agreed); err != nil {
		return d, err
	}
	err = json.Unmarshal([]byte(refs), &d.EvidenceRefs)
	return d, err
}

// GetDispute đọc hồ sơ (nil khi không có).
func GetDispute(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Dispute, error) {
	q := "SELECT " + disputeCols + " FROM mkg_measurement.dispute_current WHERE dispute_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	d, err := scanDispute(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ActiveDispute trả hồ sơ đang mở của lô (nil khi không có).
func ActiveDispute(ctx context.Context, tx pgx.Tx, lot uuid.UUID) (*Dispute, error) {
	d, err := scanDispute(tx.QueryRow(ctx, "SELECT "+disputeCols+` FROM mkg_measurement.dispute_current
	  WHERE lot_ref = $1 AND status IN ('open', 'under_review')`, lot))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ListDisputes liệt kê hồ sơ theo trạng thái (rỗng = mọi), sau con trỏ id.
func ListDisputes(ctx context.Context, tx pgx.Tx, status string, after uuid.UUID, limit int) ([]Dispute, error) {
	rows, err := tx.Query(ctx, "SELECT "+disputeCols+` FROM mkg_measurement.dispute_current
	  WHERE ($1 = '' OR status = $1) AND dispute_id > $2 ORDER BY dispute_id LIMIT $3`, status, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dispute
	for rows.Next() {
		d, err := scanDispute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ApplyDisputeEvent chiếu một sự kiện vào dispute_current.
func ApplyDisputeEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e DisputeEvent) error {
	refs := e.EvidenceRefs
	if refs == nil {
		refs = []uuid.UUID{}
	}
	b, _ := json.Marshal(refs)
	if e.Action == domain.ActOpen {
		_, err := tx.Exec(ctx, `INSERT INTO mkg_measurement.dispute_current
		    (tenant_id, dispute_id, lot_ref, status, severity, reconciliation_id, hub_out_kg, customer_in_kg, diff_kg, diff_pct, evidence_refs,
		     opened_at, opened_by, last_seq)
		  VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8::numeric, $9::numeric, $10::numeric, $11::jsonb, $12, $13, $14)`,
			tid, e.DisputeID, e.LotRef, e.StatusAfter, deref(e.Severity), e.ReconciliationID, kgArg(e.HubOut), kgArg(e.CustomerIn),
			kgArg(e.Diff), decArg(e.DiffPct), string(b), e.EventTime.UTC(), e.ActorRef, e.Seq)
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE mkg_measurement.dispute_current
	   SET status = $2, evidence_refs = evidence_refs || $3::jsonb, resolution = coalesce($4, resolution),
	       agreed_kg = coalesce($5::numeric, agreed_kg), last_seq = $6, updated_at = now()
	 WHERE dispute_id = $1`, e.DisputeID, e.StatusAfter, string(b), e.Resolution, kgArg(e.AgreedKg), e.Seq)
	return err
}

// RebuildDisputes dựng lại dispute_current từ sổ.
func RebuildDisputes(ctx context.Context, tx pgx.Tx, tid uuid.UUID) (int64, error) {
	if _, err := tx.Exec(ctx, "DELETE FROM mkg_measurement.dispute_current"); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT id, seq, dispute_id, lot_ref, action, status_after, reconciliation_id, severity, hub_out_kg::text,
	    customer_in_kg::text, diff_kg::text, diff_pct::text, evidence_refs::text, resolution, agreed_kg::text, event_time, actor_ref
	  FROM mkg_measurement.dispute_event ORDER BY stream_key, seq`)
	if err != nil {
		return 0, err
	}
	var es []DisputeEvent
	for rows.Next() {
		var e DisputeEvent
		var hout, cin, diff, pct, agreed *string
		var refs string
		if err := rows.Scan(&e.ID, &e.Seq, &e.DisputeID, &e.LotRef, &e.Action, &e.StatusAfter, &e.ReconciliationID, &e.Severity, &hout,
			&cin, &diff, &pct, &refs, &e.Resolution, &agreed, &e.EventTime, &e.ActorRef); err != nil {
			rows.Close()
			return 0, err
		}
		e.HubOut, _ = kgScan(hout)
		e.CustomerIn, _ = kgScan(cin)
		e.Diff, _ = kgScan(diff)
		e.DiffPct, _ = decScan(pct)
		e.AgreedKg, _ = kgScan(agreed)
		_ = json.Unmarshal([]byte(refs), &e.EvidenceRefs)
		es = append(es, e)
	}
	rows.Close()
	n := int64(0)
	for _, e := range es {
		if e.Action == domain.ActOpen {
			n++
		}
		if err := ApplyDisputeEvent(ctx, tx, tid, e); err != nil {
			return 0, err
		}
	}
	return n, rows.Err()
}

// ---------------------------------------------------------------------------
// Nhập số cân của khách (C-5 tạm)
// ---------------------------------------------------------------------------

// ImportRow là một dòng số cân của khách.
type ImportRow struct {
	LotRef           uuid.UUID `json:"lot_ref"`
	MassKg           mass.Kg   `json:"mass_kg"`
	MoisturePct      *string   `json:"moisture_pct"`
	WeighedAt        time.Time `json:"weighed_at"`
	CustomerTicketNo *string   `json:"customer_ticket_no"`
	Route            string    `json:"route"`
}

// Import là một lần nhập.
type Import struct {
	ID          uuid.UUID
	Seq         int64
	Channel     string
	CustomerRef *uuid.UUID
	EvidenceRef *uuid.UUID
	FileSHA256  *string
	Rows        []ImportRow
	TicketIDs   []uuid.UUID
	Provisional bool
	Reason      *string
	EventTime   time.Time
	RecordedAt  time.Time
	ActorRef    uuid.UUID
	IdemKey     string
}

// InsertImport ghi sổ lần nhập.
func InsertImport(ctx context.Context, tx pgx.Tx, tid uuid.UUID, im *Import) error {
	rows, err := json.Marshal(im.Rows)
	if err != nil {
		return err
	}
	tks, err := json.Marshal(im.TicketIDs)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO mkg_measurement.customer_weight_import
	    (id, tenant_id, stream_key, channel, customer_ref, evidence_ref, file_sha256, rows, ticket_ids, provisional, reason, event_time,
	     actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11, $12, $13, $14, $15) RETURNING seq, recorded_at`,
		im.ID, tid, "import:"+im.ID.String(), im.Channel, im.CustomerRef, im.EvidenceRef, im.FileSHA256, string(rows), string(tks),
		im.Provisional, im.Reason, im.EventTime.UTC(), im.ActorRef, im.IdemKey, domain.RuleImport).Scan(&im.Seq, &im.RecordedAt)
}

func oneImport(ctx context.Context, tx pgx.Tx, where string, arg any) (*Import, error) {
	var im Import
	var rows, tks string
	err := tx.QueryRow(ctx, `SELECT id, seq, channel, customer_ref, evidence_ref, file_sha256, rows::text, ticket_ids::text, provisional,
	    reason, event_time, recorded_at, actor_ref, idem_key FROM mkg_measurement.customer_weight_import WHERE `+where, arg).
		Scan(&im.ID, &im.Seq, &im.Channel, &im.CustomerRef, &im.EvidenceRef, &im.FileSHA256, &rows, &tks, &im.Provisional, &im.Reason,
			&im.EventTime, &im.RecordedAt, &im.ActorRef, &im.IdemKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(rows), &im.Rows); err != nil {
		return nil, err
	}
	return &im, json.Unmarshal([]byte(tks), &im.TicketIDs)
}

// ImportByIdem tìm lần nhập theo idem_key.
func ImportByIdem(ctx context.Context, tx pgx.Tx, key string) (*Import, error) {
	return oneImport(ctx, tx, "idem_key = $1"+ledger.NewestByIdem(""), key)
}

// ImportByID tìm lần nhập theo id.
func ImportByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Import, error) {
	return oneImport(ctx, tx, "id = $1", id)
}

// ---------------------------------------------------------------------------
// Van công bố
// ---------------------------------------------------------------------------

// ValveExport là một dòng sổ xuất van.
type ValveExport struct {
	ID              uuid.UUID
	Seq             int64
	Contract        string
	ContractVersion int
	EventType       string
	SchemaVersion   int
	SubjectRef      uuid.UUID
	SourceRef       uuid.UUID
	PlotRef         uuid.UUID
	Revision        int
	Payload         json.RawMessage
	PayloadSHA256   []byte
	EventTime       time.Time
	RecordedAt      time.Time
	ActorRef        uuid.UUID
	IdemKey         string
}

// InsertValveExport ghi sổ xuất kèm SHA-256 của đúng byte đã kiểm.
func InsertValveExport(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v *ValveExport) error {
	sum := sha256.Sum256(v.Payload)
	v.PayloadSHA256 = sum[:]
	return tx.QueryRow(ctx, `INSERT INTO mkg_measurement.valve_export
	    (id, tenant_id, stream_key, contract, contract_version, event_type, schema_version, subject_ref, source_ref, plot_ref, revision,
	     payload, payload_sha256, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14, $15, $16, $17) RETURNING seq, recorded_at`,
		v.ID, tid, "valve:"+v.Contract+":"+v.SubjectRef.String(), v.Contract, v.ContractVersion, v.EventType, v.SchemaVersion, v.SubjectRef,
		v.SourceRef, v.PlotRef, v.Revision, string(v.Payload), v.PayloadSHA256, v.EventTime.UTC(), v.ActorRef, v.IdemKey, domain.RuleValve).
		Scan(&v.Seq, &v.RecordedAt)
}

const valveCols = `id, seq, contract, contract_version, event_type, schema_version, subject_ref, source_ref, plot_ref, revision,
  payload::text, payload_sha256, event_time, recorded_at, actor_ref, idem_key`

func scanValve(row pgx.Row) (ValveExport, error) {
	var v ValveExport
	var p string
	err := row.Scan(&v.ID, &v.Seq, &v.Contract, &v.ContractVersion, &v.EventType, &v.SchemaVersion, &v.SubjectRef, &v.SourceRef,
		&v.PlotRef, &v.Revision, &p, &v.PayloadSHA256, &v.EventTime, &v.RecordedAt, &v.ActorRef, &v.IdemKey)
	v.Payload = json.RawMessage(p)
	return v, err
}

// LastValveFor trả lần xuất gần nhất của (lô, thửa) — để giữ evidence_ref và tăng revision.
func LastValveFor(ctx context.Context, tx pgx.Tx, lot, plot uuid.UUID) (*ValveExport, error) {
	v, err := scanValve(tx.QueryRow(ctx, "SELECT "+valveCols+` FROM mkg_measurement.valve_export
	  WHERE source_ref = $1 AND plot_ref = $2 ORDER BY revision DESC LIMIT 1`, lot, plot))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ValveByIdem tìm lần xuất theo idem_key.
func ValveByIdem(ctx context.Context, tx pgx.Tx, key string) (*ValveExport, error) {
	v, err := scanValve(tx.QueryRow(ctx, "SELECT "+valveCols+" FROM mkg_measurement.valve_export WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ValvesOfLot liệt kê lần xuất của một lô.
func ValvesOfLot(ctx context.Context, tx pgx.Tx, lot uuid.UUID) ([]ValveExport, error) {
	return listValves(ctx, tx, "SELECT "+valveCols+" FROM mkg_measurement.valve_export WHERE source_ref = $1 ORDER BY recorded_at DESC, revision DESC", lot)
}

func listValves(ctx context.Context, tx pgx.Tx, q string, arg any) ([]ValveExport, error) {
	rows, err := tx.Query(ctx, q, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ValveExport
	for rows.Next() {
		v, err := scanValve(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
