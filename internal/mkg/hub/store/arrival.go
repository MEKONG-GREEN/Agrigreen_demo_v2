package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// ArrivalStream là khóa chuỗi băm của thông báo hàng đến một chuyến.
func ArrivalStream(trip uuid.UUID) string { return "arrival:" + trip.String() }

// ArrivalEvent là một dòng sổ mkg_hub.arrival_event (mkg 000021).
type ArrivalEvent struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	TripID             uuid.UUID
	TripCode           string
	FacilityRef        uuid.UUID
	Kind               string // expected | arrived | cancelled
	Milestone          string
	Mode               string
	VesselCode         *string
	CollectionOrderRef *uuid.UUID
	LotRefs            []uuid.UUID
	PlannedKg          *mass.Kg
	SourceEventRef     uuid.UUID
	EventTime          time.Time
	ActorRef           uuid.UUID
	IdemKey            string
	RuleVersion        string
	Seq                int64
	RecordedAt         time.Time
}

// InsertArrivalEvent ghi sổ thông báo hàng đến.
func InsertArrivalEvent(ctx context.Context, tx pgx.Tx, e *ArrivalEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_hub.arrival_event
	    (id, tenant_id, stream_key, trip_id, trip_code, facility_ref, kind, milestone, mode, vessel_code, collection_order_ref,
	     lot_refs, planned_kg, source_event_ref, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::uuid[], $13::numeric, $14, $15, $16, $17, $18)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, ArrivalStream(e.TripID), e.TripID, e.TripCode, e.FacilityRef, e.Kind, e.Milestone, e.Mode, e.VesselCode,
		e.CollectionOrderRef, uuidTexts(e.LotRefs), kgArg(e.PlannedKg), e.SourceEventRef, e.EventTime.UTC(), e.ActorRef, e.IdemKey,
		e.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

func uuidTexts(xs []uuid.UUID) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = x.String()
	}
	return out
}

// ArrivalEventExists cho biết idem_key đã ghi (giao lặp sự kiện qua consumer khác / phát lại).
func ArrivalEventExists(ctx context.Context, tx pgx.Tx, idem string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM mkg_hub.arrival_event WHERE idem_key = $1)", idem).Scan(&ok)
	return ok, err
}

// Arrival là một dòng bảng chiếu mkg_hub.arrival_current.
type Arrival struct {
	TripID             uuid.UUID
	TripCode           string
	FacilityRef        uuid.UUID
	Status             string
	Mode               string
	VesselCode         *string
	CollectionOrderRef *uuid.UUID
	LotRefs            []uuid.UUID
	PlannedKg          *mass.Kg
	ExpectedSince      *time.Time
	ArrivedAt          *time.Time
	CancelledAt        *time.Time
	LastEventID        uuid.UUID
	LastSeq            int64
	UpdatedAt          time.Time
}

const arrivalCols = `trip_id, trip_code, facility_ref, status, mode, vessel_code, collection_order_ref, lot_refs::text[], planned_kg::text,
  expected_since, arrived_at, cancelled_at, last_event_id, last_seq, updated_at`

func scanArrival(r pgx.Row) (Arrival, error) {
	var a Arrival
	var lots []string
	var kg *string
	if err := r.Scan(&a.TripID, &a.TripCode, &a.FacilityRef, &a.Status, &a.Mode, &a.VesselCode, &a.CollectionOrderRef, &lots, &kg,
		&a.ExpectedSince, &a.ArrivedAt, &a.CancelledAt, &a.LastEventID, &a.LastSeq, &a.UpdatedAt); err != nil {
		return a, err
	}
	a.LotRefs = make([]uuid.UUID, 0, len(lots))
	for _, s := range lots {
		id, err := uuid.Parse(s)
		if err != nil {
			return a, err
		}
		a.LotRefs = append(a.LotRefs, id)
	}
	var err error
	a.PlannedKg, err = kgScan(kg)
	return a, err
}

// ArrivalByTrip đọc thông báo hàng đến của chuyến; nil nếu chưa có.
func ArrivalByTrip(ctx context.Context, tx pgx.Tx, trip uuid.UUID) (*Arrival, error) {
	a, err := scanArrival(tx.QueryRow(ctx, "SELECT "+arrivalCols+" FROM mkg_hub.arrival_current WHERE trip_id = $1", trip))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// arrivalFold chiếu sổ thông báo hàng đến thành dòng hiện hành: nội dung của sự kiện mới nhất (đích, phương tiện,
// lô), mốc chờ = sự kiện expected đầu tiên, mốc đến / hủy = sự kiện đầu tiên của loại đó. Cùng một câu cho ghi
// trực tiếp (một chuyến) và dựng lại (mọi chuyến) → dựng lại = sống (R-22).
const arrivalFold = `SELECT DISTINCT ON (e.trip_id) e.tenant_id, e.trip_id, e.trip_code, e.facility_ref, e.kind, e.mode, e.vessel_code,
         e.collection_order_ref, e.lot_refs, e.planned_kg,
         (SELECT min(x.event_time) FROM mkg_hub.arrival_event x WHERE x.trip_id = e.trip_id AND x.kind = 'expected'),
         (SELECT min(x.event_time) FROM mkg_hub.arrival_event x WHERE x.trip_id = e.trip_id AND x.kind = 'arrived'),
         (SELECT min(x.event_time) FROM mkg_hub.arrival_event x WHERE x.trip_id = e.trip_id AND x.kind = 'cancelled'),
         e.id, e.seq
    FROM mkg_hub.arrival_event e`

const arrivalInsert = `INSERT INTO mkg_hub.arrival_current (tenant_id, trip_id, trip_code, facility_ref, status, mode, vessel_code,
    collection_order_ref, lot_refs, planned_kg, expected_since, arrived_at, cancelled_at, last_event_id, last_seq) `

// ProjectArrival áp sổ của một chuyến vào bảng chiếu (cùng giao dịch với ghi sổ, R-21).
func ProjectArrival(ctx context.Context, tx pgx.Tx, trip uuid.UUID) error {
	_, err := tx.Exec(ctx, arrivalInsert+arrivalFold+` WHERE e.trip_id = $1 ORDER BY e.trip_id, e.seq DESC
	  ON CONFLICT (tenant_id, trip_id) DO UPDATE SET trip_code = EXCLUDED.trip_code, facility_ref = EXCLUDED.facility_ref,
	    status = EXCLUDED.status, mode = EXCLUDED.mode, vessel_code = EXCLUDED.vessel_code,
	    collection_order_ref = EXCLUDED.collection_order_ref, lot_refs = EXCLUDED.lot_refs, planned_kg = EXCLUDED.planned_kg,
	    expected_since = EXCLUDED.expected_since, arrived_at = EXCLUDED.arrived_at, cancelled_at = EXCLUDED.cancelled_at,
	    last_event_id = EXCLUDED.last_event_id, last_seq = EXCLUDED.last_seq, updated_at = now()`, trip)
	return err
}

// RebuildArrivals xóa và dựng lại arrival_current của tenant thẳng từ sổ (R-22).
func RebuildArrivals(ctx context.Context, tx pgx.Tx) (int64, error) {
	if _, err := tx.Exec(ctx, "DELETE FROM mkg_hub.arrival_current"); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, arrivalInsert+arrivalFold+" ORDER BY e.trip_id, e.seq DESC")
	return tag.RowsAffected(), err
}

// ListArrivals liệt kê thông báo hàng đến, mới nhất trước (trip_id UUIDv7), con trỏ = trip_id cuối trang.
func ListArrivals(ctx context.Context, tx pgx.Tx, facility *uuid.UUID, status string, before *uuid.UUID, limit int) ([]Arrival, error) {
	rows, err := tx.Query(ctx, "SELECT "+arrivalCols+` FROM mkg_hub.arrival_current
	    WHERE ($1::uuid IS NULL OR facility_ref = $1) AND ($2 = '' OR status = $2) AND ($3::uuid IS NULL OR trip_id < $3)
	    ORDER BY trip_id DESC LIMIT $4`, facility, status, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Arrival, error) { return scanArrival(r) })
}
