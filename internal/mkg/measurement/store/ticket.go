// Package store: SQL của mkg/measurement (schema mkg_measurement). Mọi hàm nhận
// pgx.Tx đã ở ngữ cảnh tenant (tenant.WithTenantTx) — RLS là lớp chặn cuối.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// LotStream là khóa chuỗi của lô.
func LotStream(lot uuid.UUID) string { return "lot:" + lot.String() }

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func kgArg(k *mass.Kg) *string {
	if k == nil {
		return nil
	}
	s := k.String()
	return &s
}

func kgScan(s *string) (*mass.Kg, error) {
	if s == nil {
		return nil, nil
	}
	k, err := mass.ParseKg(*s)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func decArg(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.StringFixed(domain.PctScale)
	return &s
}

func decScan(s *string) (*decimal.Decimal, error) {
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
// Phiếu cân
// ---------------------------------------------------------------------------

// InsertTicket ghi sổ phiếu cân (d đã qua domain.NormalizeDraft).
func InsertTicket(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d port.TicketDraft, importRef *uuid.UUID) (int64, time.Time, error) {
	var (
		deviceID, sha      *string
		epoch, used, total *int
		cFrom, cTo         *int64
		batches            *string
	)
	if in := d.Integration; in != nil {
		deviceID, sha = &in.Window.DeviceID, strPtr(in.ReadingsSHA256)
		e, u, tt := in.Window.Epoch, in.ReadingsUsed, in.ReadingsTotal
		epoch, used, total = &e, &u, &tt
		f, to := in.Window.CounterFrom, in.Window.CounterTo
		cFrom, cTo = &f, &to
		ids := in.BatchIDs
		if ids == nil {
			ids = []uuid.UUID{}
		}
		b, err := json.Marshal(ids)
		if err != nil {
			return 0, time.Time{}, err
		}
		s := string(b)
		batches = &s
	}
	var seq int64
	var rec time.Time
	err := tx.QueryRow(ctx, `INSERT INTO mkg_measurement.weigh_ticket
	    (id, tenant_id, stream_key, lot_ref, point, route, source, mass_kg, moisture_pct, moisture_state, session_ref, epoch,
	     counter_from, counter_to, readings_used, readings_total, batch_ids, readings_sha256, evidence_ref, customer_ref, import_ref,
	     reason, event_time, supersedes_id, correction_reason, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, $9::numeric, $10, $11, $12, $13, $14, $15, $16, $17::jsonb, $18, $19, $20, $21,
	          $22, $23, $24, $25, $26, $27, $28, $29)
	  RETURNING seq, recorded_at`,
		d.ID, tid, LotStream(d.LotRef), d.LotRef, string(d.Point), string(d.Route), string(d.Source), d.MassKg.String(),
		strPtr(d.MoisturePct), d.MoistureState, d.SessionRef, epoch, cFrom, cTo, used, total, batches, sha, d.EvidenceRef,
		d.CustomerRef, importRef, strPtr(d.Reason), d.EventTime.UTC(), d.Supersedes, strPtr(d.CorrectionReason), d.ActorRef,
		deviceID, d.IdemKey, domain.RuleVersionOf(d)).Scan(&seq, &rec)
	return seq, rec, err
}

const ticketCols = `id, lot_ref, point, route, source, mass_kg::text, moisture_pct::text, moisture_state, session_ref, epoch,
  counter_from, counter_to, readings_used, readings_total, batch_ids::text, readings_sha256, evidence_ref, customer_ref,
  reason, event_time, supersedes_id, correction_reason, actor_ref, device_id, idem_key, rule_version, seq, recorded_at`

func oneTicket(ctx context.Context, tx pgx.Tx, where string, args ...any) (*port.Ticket, error) {
	rows, err := tx.Query(ctx, "SELECT "+ticketCols+", rule_version FROM mkg_measurement.weigh_ticket WHERE "+where+" LIMIT 1", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	t, rv, err := scanTicketRV(rows)
	if err != nil {
		return nil, err
	}
	if t.Integration != nil {
		t.Integration.RuleVersion = rv
	}
	return &t, nil
}

func scanTicketRV(row pgx.Row) (port.Ticket, string, error) {
	var rv string
	var t port.Ticket
	var point, route, source, massS string
	var moisture, batches, sha, reason, corr, deviceID *string
	var epoch, used, total *int
	var cFrom, cTo *int64
	err := row.Scan(&t.ID, &t.LotRef, &point, &route, &source, &massS, &moisture, &t.MoistureState, &t.SessionRef, &epoch,
		&cFrom, &cTo, &used, &total, &batches, &sha, &t.EvidenceRef, &t.CustomerRef, &reason, &t.EventTime, &t.Supersedes,
		&corr, &t.ActorRef, &deviceID, &t.IdemKey, new(string), &t.Seq, &t.RecordedAt, &rv)
	if err != nil {
		return t, "", err
	}
	t.Point, t.Route, t.Source = port.WeighPoint(point), port.Route(route), port.TicketSource(source)
	if t.MassKg, err = mass.ParseKg(massS); err != nil {
		return t, "", err
	}
	t.MoisturePct, t.Reason, t.CorrectionReason = deref(moisture), deref(reason), deref(corr)
	if t.Source == port.SourceEdge && deviceID != nil && epoch != nil && cFrom != nil && cTo != nil {
		in := &port.Integration{Window: port.Window{DeviceID: *deviceID, Epoch: *epoch, CounterFrom: *cFrom, CounterTo: *cTo},
			NetKg: t.MassKg, ReadingsSHA256: deref(sha), RuleVersion: rv}
		if used != nil {
			in.ReadingsUsed = *used
		}
		if total != nil {
			in.ReadingsTotal = *total
		}
		if batches != nil {
			if err := json.Unmarshal([]byte(*batches), &in.BatchIDs); err != nil {
				return t, "", err
			}
		}
		t.Integration = in
	}
	return t, rv, nil
}

// TicketByIdem tìm phiếu theo idem_key.
func TicketByIdem(ctx context.Context, tx pgx.Tx, key string) (*port.Ticket, error) {
	return oneTicket(ctx, tx, "idem_key = $1", key)
}

// TicketByID tìm phiếu theo id.
func TicketByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*port.Ticket, error) {
	return oneTicket(ctx, tx, "id = $1", id)
}

// TicketsOfLot liệt kê mọi phiếu của lô theo seq (kể cả phiếu đã bị bù).
func TicketsOfLot(ctx context.Context, tx pgx.Tx, lot uuid.UUID) ([]port.Ticket, error) {
	rows, err := tx.Query(ctx, "SELECT "+ticketCols+", rule_version FROM mkg_measurement.weigh_ticket WHERE stream_key = $1 ORDER BY seq", LotStream(lot))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []port.Ticket
	for rows.Next() {
		t, _, err := scanTicketRV(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Bảng chiếu lot_mass_current
// ---------------------------------------------------------------------------

// PointMass là phiếu hiệu lực ở một điểm.
type PointMass struct {
	Kg       mass.Kg
	TicketID uuid.UUID
	At       time.Time
}

// LotMass là một dòng lot_mass_current.
type LotMass struct {
	LotRef        uuid.UUID
	Route         port.Route
	Points        map[port.WeighPoint]PointMass
	HubInMoisture *decimal.Decimal
	LastSeq       int64
	AsOf          time.Time
}

const lotMassCols = `lot_ref, route, hub_in_kg::text, hub_in_ticket, hub_in_at, hub_in_moisture_pct::text, hub_out_kg::text,
  hub_out_ticket, hub_out_at, customer_in_kg::text, customer_in_ticket, customer_in_at, last_seq, as_of`

func scanLotMass(row pgx.Row) (LotMass, error) {
	var m LotMass
	var route string
	var ik, ok, ck, moist *string
	var it, ot, ct *uuid.UUID
	var ia, oa, ca *time.Time
	if err := row.Scan(&m.LotRef, &route, &ik, &it, &ia, &moist, &ok, &ot, &oa, &ck, &ct, &ca, &m.LastSeq, &m.AsOf); err != nil {
		return m, err
	}
	m.Route = port.Route(route)
	m.Points = map[port.WeighPoint]PointMass{}
	for _, p := range []struct {
		pt port.WeighPoint
		k  *string
		t  *uuid.UUID
		a  *time.Time
	}{{port.PointHubIn, ik, it, ia}, {port.PointHubOut, ok, ot, oa}, {port.PointCustomerIn, ck, ct, ca}} {
		if p.k == nil || p.t == nil {
			continue
		}
		k, err := mass.ParseKg(*p.k)
		if err != nil {
			return m, err
		}
		pm := PointMass{Kg: k, TicketID: *p.t}
		if p.a != nil {
			pm.At = *p.a
		}
		m.Points[p.pt] = pm
	}
	var err error
	m.HubInMoisture, err = decScan(moist)
	return m, err
}

// GetLotMass đọc bảng chiếu (nil khi chưa có).
func GetLotMass(ctx context.Context, tx pgx.Tx, lot uuid.UUID, forUpdate bool) (*LotMass, error) {
	q := "SELECT " + lotMassCols + " FROM mkg_measurement.lot_mass_current WHERE lot_ref = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	m, err := scanLotMass(tx.QueryRow(ctx, q, lot))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// LockLotMass bảo đảm có dòng bảng chiếu của lô và khóa nó (tuần tự hóa mọi lần
// ghi phiếu của cùng lô). Dòng mới nhận tuyến route.
func LockLotMass(ctx context.Context, tx pgx.Tx, tid, lot uuid.UUID, route port.Route) (LotMass, bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO mkg_measurement.lot_mass_current (tenant_id, lot_ref, route, last_seq, as_of)
	  VALUES ($1, $2, $3, 0, now()) ON CONFLICT (tenant_id, lot_ref) DO NOTHING`, tid, lot, string(route))
	if err != nil {
		return LotMass{}, false, err
	}
	m, err := GetLotMass(ctx, tx, lot, true)
	if err != nil {
		return LotMass{}, false, err
	}
	if m == nil {
		return LotMass{}, false, errors.New("measurement: không khóa được bảng chiếu lô")
	}
	return *m, tag.RowsAffected() == 1, nil
}

// ApplyTicket đặt phiếu hiệu lực của điểm cân vào bảng chiếu.
func ApplyTicket(ctx context.Context, tx pgx.Tx, t port.Ticket) error {
	var col string
	switch t.Point {
	case port.PointHubIn:
		col = "hub_in"
	case port.PointHubOut:
		col = "hub_out"
	default:
		col = "customer_in"
	}
	extra := ""
	args := []any{t.LotRef, t.MassKg.String(), t.ID, t.EventTime.UTC(), t.Seq, t.RecordedAt}
	if t.Point == port.PointHubIn {
		extra = ", hub_in_moisture_pct = $7::numeric"
		args = append(args, strPtr(t.MoisturePct))
	}
	_, err := tx.Exec(ctx, `UPDATE mkg_measurement.lot_mass_current
	   SET `+col+`_kg = $2::numeric, `+col+`_ticket = $3, `+col+`_at = $4, last_seq = greatest(last_seq, $5), as_of = greatest(as_of, $6),
	       updated_at = now()`+extra+`
	 WHERE lot_ref = $1`, args...)
	return err
}

// ToView chuyển bảng chiếu sang port.LotMassView.
func (m LotMass) ToView() port.LotMassView {
	v := port.LotMassView{LotRef: m.LotRef, Route: m.Route, TicketIDs: map[port.WeighPoint]uuid.UUID{}, AsOf: m.AsOf}
	for pt, pm := range m.Points {
		k := pm.Kg
		v.TicketIDs[pt] = pm.TicketID
		switch pt {
		case port.PointHubIn:
			v.HubInKg = &k
		case port.PointHubOut:
			v.HubOutKg = &k
		case port.PointCustomerIn:
			v.CustomerInKg = &k
		}
	}
	return v
}

// RebuildLotMass dựng lại lot_mass_current của tenant từ sổ phiếu (R-22):
// phiếu hiệu lực = phiếu không bị phiếu nào bù.
func RebuildLotMass(ctx context.Context, tx pgx.Tx, tid uuid.UUID) (int64, error) {
	if _, err := tx.Exec(ctx, "DELETE FROM mkg_measurement.lot_mass_current"); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, "SELECT "+ticketCols+`, rule_version FROM mkg_measurement.weigh_ticket t
	  WHERE NOT EXISTS (SELECT 1 FROM mkg_measurement.weigh_ticket s WHERE s.supersedes_id = t.id) ORDER BY lot_ref, seq`)
	if err != nil {
		return 0, err
	}
	var ts []port.Ticket
	for rows.Next() {
		t, _, err := scanTicketRV(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		ts = append(ts, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	lots := map[uuid.UUID]bool{}
	for _, t := range ts {
		if _, _, err := LockLotMass(ctx, tx, tid, t.LotRef, t.Route); err != nil {
			return 0, err
		}
		if err := ApplyTicket(ctx, tx, t); err != nil {
			return 0, err
		}
		lots[t.LotRef] = true
	}
	// as_of / last_seq của bảng chiếu sống = lớn nhất trên MỌI phiếu đã áp (kể cả phiếu đã bị bù); dòng mới của
	// LockLotMass mang now() của lần dựng lại → đặt lại từ sổ để dựng lại = bảng chiếu hiện hành (R-22, rà E2E G4).
	if _, err := tx.Exec(ctx, `UPDATE mkg_measurement.lot_mass_current c SET as_of = m.as_of, last_seq = m.last_seq
	  FROM (SELECT lot_ref, max(recorded_at) AS as_of, max(seq) AS last_seq FROM mkg_measurement.weigh_ticket GROUP BY lot_ref) m
	 WHERE c.lot_ref = m.lot_ref`); err != nil {
		return 0, err
	}
	return int64(len(lots)), nil
}
