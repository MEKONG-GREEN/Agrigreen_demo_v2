package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// VesselEvent là một dòng sổ vessel_event (một phiên bản danh mục phương tiện).
type VesselEvent struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	Seq                int64
	VesselID           uuid.UUID
	Kind               string // created | revised
	Version            int
	Code               string
	VehicleKind        string
	Name               *string
	OwnerRef           *uuid.UUID
	RegisteredTons     *decimal.Decimal
	StrawPayloadTons   *decimal.Decimal
	RegistrationExpiry *time.Time
	RateType           *string
	RateVND            *decimal.Decimal
	Status             string
	Reason             *string
	EventTime          time.Time
	RecordedAt         time.Time
	ActorRef           uuid.UUID
	IdemKey            string
	RuleVersion        string
}

// InsertVesselEvent ghi sổ danh mục phương tiện.
func InsertVesselEvent(ctx context.Context, tx pgx.Tx, e *VesselEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_logistics.vessel_event
	    (id, tenant_id, stream_key, vessel_id, kind, version, code, vehicle_kind, name, owner_ref, registered_tons, straw_payload_tons,
	     registration_expiry, rate_type, rate_vnd, status, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::numeric, $12::numeric, $13, $14, $15::numeric, $16, $17, $18, $19, $20, $21)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, "vessel:"+e.VesselID.String(), e.VesselID, e.Kind, e.Version, e.Code, e.VehicleKind, e.Name, e.OwnerRef,
		decStr(e.RegisteredTons), decStr(e.StrawPayloadTons), e.RegistrationExpiry, e.RateType, decStr(e.RateVND), e.Status, e.Reason,
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, e.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// VesselEventByIdem tìm sự kiện danh mục theo idem_key.
func VesselEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*VesselEvent, error) {
	var e VesselEvent
	var rt, rv, pt, rate *string
	err := tx.QueryRow(ctx, `SELECT id, tenant_id, seq, vessel_id, kind, version, code, vehicle_kind, name, owner_ref, registered_tons::text,
	    straw_payload_tons::text, registration_expiry, rate_type, rate_vnd::text, status, reason, event_time, recorded_at, actor_ref, idem_key,
	    rule_version FROM mkg_logistics.vessel_event WHERE idem_key = $1`, key).Scan(&e.ID, &e.TenantID, &e.Seq, &e.VesselID, &e.Kind,
		&e.Version, &e.Code, &e.VehicleKind, &e.Name, &e.OwnerRef, &rt, &pt, &e.RegistrationExpiry, &rv, &rate, &e.Status, &e.Reason,
		&e.EventTime, &e.RecordedAt, &e.ActorRef, &e.IdemKey, &e.RuleVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.RateType = rv
	if e.RegisteredTons, err = parseDec(rt); err != nil {
		return nil, err
	}
	if e.StrawPayloadTons, err = parseDec(pt); err != nil {
		return nil, err
	}
	if e.RateVND, err = parseDec(rate); err != nil {
		return nil, err
	}
	return &e, nil
}

// Vessel là một dòng vessel_current.
type Vessel struct {
	VesselID           uuid.UUID
	Version            int
	Code               string
	VehicleKind        string
	Name               *string
	OwnerRef           *uuid.UUID
	RegisteredTons     *decimal.Decimal
	StrawPayloadTons   *decimal.Decimal
	RegistrationExpiry *time.Time
	RateType           *string
	RateVND            *decimal.Decimal
	Status             string
	LastEventID        uuid.UUID
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

const vesselCols = `vessel_id, version, code, vehicle_kind, name, owner_ref, registered_tons::text, straw_payload_tons::text,
  registration_expiry, rate_type, rate_vnd::text, status, last_event_id, created_at, updated_at`

func scanVessel(row pgx.Row) (Vessel, error) {
	var v Vessel
	var rt, pt, rate *string
	err := row.Scan(&v.VesselID, &v.Version, &v.Code, &v.VehicleKind, &v.Name, &v.OwnerRef, &rt, &pt, &v.RegistrationExpiry, &v.RateType,
		&rate, &v.Status, &v.LastEventID, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return v, err
	}
	if v.RegisteredTons, err = parseDec(rt); err != nil {
		return v, err
	}
	if v.StrawPayloadTons, err = parseDec(pt); err != nil {
		return v, err
	}
	v.RateVND, err = parseDec(rate)
	return v, err
}

// GetVessel đọc phương tiện theo id; forUpdate khóa dòng.
func GetVessel(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Vessel, error) {
	q := "SELECT " + vesselCols + " FROM mkg_logistics.vessel_current WHERE vessel_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	v, err := scanVessel(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// VesselByCode đọc phương tiện theo số hiệu chuẩn hóa.
func VesselByCode(ctx context.Context, tx pgx.Tx, code string) (*Vessel, error) {
	v, err := scanVessel(tx.QueryRow(ctx, "SELECT "+vesselCols+" FROM mkg_logistics.vessel_current WHERE code = $1", code))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ListVessels liệt kê phương tiện (lọc trạng thái nếu có), theo số hiệu.
func ListVessels(ctx context.Context, tx pgx.Tx, status string) ([]Vessel, error) {
	rows, err := tx.Query(ctx, "SELECT "+vesselCols+` FROM mkg_logistics.vessel_current WHERE ($1 = '' OR status = $1)
	    ORDER BY code LIMIT 1000`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Vessel
	for rows.Next() {
		v, err := scanVessel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ApplyVesselEvent ghi bảng chiếu danh mục từ một phiên bản.
func ApplyVesselEvent(ctx context.Context, tx pgx.Tx, e VesselEvent) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_logistics.vessel_current
	    (tenant_id, vessel_id, version, code, vehicle_kind, name, owner_ref, registered_tons, straw_payload_tons, registration_expiry,
	     rate_type, rate_vnd, status, last_event_id, created_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, $9::numeric, $10, $11, $12::numeric, $13, $14, $15)
	  ON CONFLICT (tenant_id, vessel_id) DO UPDATE SET version = EXCLUDED.version, code = EXCLUDED.code,
	    vehicle_kind = EXCLUDED.vehicle_kind, name = EXCLUDED.name, owner_ref = EXCLUDED.owner_ref,
	    registered_tons = EXCLUDED.registered_tons, straw_payload_tons = EXCLUDED.straw_payload_tons,
	    registration_expiry = EXCLUDED.registration_expiry, rate_type = EXCLUDED.rate_type, rate_vnd = EXCLUDED.rate_vnd,
	    status = EXCLUDED.status, last_event_id = EXCLUDED.last_event_id, updated_at = now()`,
		e.TenantID, e.VesselID, e.Version, e.Code, e.VehicleKind, e.Name, e.OwnerRef, decStr(e.RegisteredTons), decStr(e.StrawPayloadTons),
		e.RegistrationExpiry, e.RateType, decStr(e.RateVND), e.Status, e.ID, e.EventTime.UTC())
	return err
}
