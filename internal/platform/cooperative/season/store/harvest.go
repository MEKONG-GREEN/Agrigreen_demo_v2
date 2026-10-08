package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Harvest là một dòng sổ harvest_declaration.
type Harvest struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	Seq              int64
	SeasonRef        uuid.UUID
	PlotRef          uuid.UUID
	HouseholdRef     uuid.UUID
	CooperativeRef   uuid.UUID
	CommuneCode      string
	SeasonCode       string
	SeasonYear       int
	VarietyCode      string
	HarvestDate      time.Time
	MassKg           mass.Kg
	AreaHa           decimal.Decimal
	YieldTPerHa      decimal.Decimal
	MoisturePct      *decimal.Decimal
	AnomalyKind      string
	AnomalyThreshold *decimal.Decimal
	AnomalyConfirmed bool
	Source           string
	GPS              domain.GPS
	LocationStatus   string
	DistanceM        *decimal.Decimal
	OnBehalf         bool
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	DeviceID         *string
	IdemKey          string
}

// InsertHarvest ghi một khai báo sản lượng; DB tính seq và băm.
func InsertHarvest(ctx context.Context, tx pgx.Tx, h *Harvest) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_coop.harvest_declaration
	    (id, tenant_id, stream_key, season_ref, plot_ref, household_ref, cooperative_ref, commune_code, season_code, season_year,
	     variety_code, harvest_date, mass_kg, area_ha, yield_t_per_ha, moisture_pct, anomaly_kind, anomaly_threshold,
	     anomaly_confirmed, source, gps, gps_accuracy_m, gps_source, location_status, distance_m, on_behalf, event_time,
	     supersedes_id, correction_reason, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::uuid, $6::uuid, $7::uuid, $8::text, $9::text, $10::int,
	     $11::text, $12::date, $13::numeric, $14::numeric, $15::numeric, $16::numeric, $17::text, $18::numeric,
	     $19::boolean, $20::text, `+gpsInsertSQL("$21", "$22")+`, $23::numeric, $24::text, $25::text, $26::numeric, $27::boolean, $28::timestamptz,
	     $29::uuid, $30::text, $31::uuid, $32::text, $33::text, $34::text)
	  RETURNING seq, recorded_at`,
		h.ID, h.TenantID, "harvest:"+h.SeasonRef.String(), h.SeasonRef, h.PlotRef, nilIfZero(h.HouseholdRef), h.CooperativeRef, h.CommuneCode,
		h.SeasonCode, h.SeasonYear, h.VarietyCode, day(h.HarvestDate), h.MassKg.String(), h.AreaHa.String(), h.YieldTPerHa.String(),
		decPtr(h.MoisturePct), nilIfEmpty(h.AnomalyKind), decPtr(h.AnomalyThreshold), h.AnomalyConfirmed, h.Source,
		decPtr(h.GPS.Lon), decPtr(h.GPS.Lat), decPtr(h.GPS.AccuracyM), h.GPS.Source, h.LocationStatus, decPtr(h.DistanceM), h.OnBehalf, h.EventTime.UTC(),
		h.SupersedesID, h.CorrectionReason, h.ActorRef, h.DeviceID, h.IdemKey, domain.RuleVersion).Scan(&h.Seq, &h.RecordedAt)
}

const harvestCols = `id, tenant_id, seq, season_ref, plot_ref, household_ref, cooperative_ref, commune_code, season_code, season_year,
  variety_code, harvest_date, mass_kg::text, area_ha::text, yield_t_per_ha::text, moisture_pct::text, coalesce(anomaly_kind, ''),
  anomaly_threshold::text, anomaly_confirmed, source, ` + gpsSelect + `, gps_accuracy_m::text, gps_source, location_status,
  distance_m::text, on_behalf, event_time, recorded_at, supersedes_id, correction_reason, actor_ref, device_id, idem_key`

func scanHarvest(row pgx.Row) (Harvest, error) {
	var h Harvest
	var hh *uuid.UUID
	var massS, area, yield string
	var moist, thr, lon, lat, acc, dist *string
	if err := row.Scan(&h.ID, &h.TenantID, &h.Seq, &h.SeasonRef, &h.PlotRef, &hh, &h.CooperativeRef, &h.CommuneCode, &h.SeasonCode, &h.SeasonYear,
		&h.VarietyCode, &h.HarvestDate, &massS, &area, &yield, &moist, &h.AnomalyKind,
		&thr, &h.AnomalyConfirmed, &h.Source, &lon, &lat, &acc, &h.GPS.Source, &h.LocationStatus,
		&dist, &h.OnBehalf, &h.EventTime, &h.RecordedAt, &h.SupersedesID, &h.CorrectionReason, &h.ActorRef, &h.DeviceID, &h.IdemKey); err != nil {
		return h, err
	}
	h.HouseholdRef = deref(hh)
	var err error
	if h.MassKg, err = mass.ParseKg(massS); err != nil {
		return h, err
	}
	if h.AreaHa, err = decimal.NewFromString(area); err != nil {
		return h, err
	}
	if h.YieldTPerHa, err = decimal.NewFromString(yield); err != nil {
		return h, err
	}
	for _, p := range []struct {
		s *string
		d **decimal.Decimal
	}{{moist, &h.MoisturePct}, {thr, &h.AnomalyThreshold}, {lon, &h.GPS.Lon}, {lat, &h.GPS.Lat}, {acc, &h.GPS.AccuracyM}, {dist, &h.DistanceM}} {
		if *p.d, err = parseDecPtr(p.s); err != nil {
			return h, err
		}
	}
	return h, nil
}

func oneHarvest(ctx context.Context, tx pgx.Tx, where string, arg any) (*Harvest, error) {
	h, err := scanHarvest(tx.QueryRow(ctx, "SELECT "+harvestCols+" FROM pf_coop.harvest_declaration WHERE "+where+" LIMIT 1", arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// HarvestByIdem đọc khai báo theo idem_key.
func HarvestByIdem(ctx context.Context, tx pgx.Tx, key string) (*Harvest, error) {
	return oneHarvest(ctx, tx, "idem_key = $1::text", key)
}

// HarvestByID đọc khai báo theo id.
func HarvestByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Harvest, error) {
	return oneHarvest(ctx, tx, "id = $1::uuid", id)
}

// AllHarvests đọc toàn bộ sổ sản lượng theo thứ tự ghi.
func AllHarvests(ctx context.Context, tx pgx.Tx) ([]Harvest, error) {
	rows, err := tx.Query(ctx, "SELECT "+harvestCols+" FROM pf_coop.harvest_declaration ORDER BY recorded_at, stream_key, seq")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Harvest
	for rows.Next() {
		h, err := scanHarvest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// SetSeasonHarvest trỏ vụ đã hoàn thành về khai báo sản lượng hiện hành (bản ghi bù).
func SetSeasonHarvest(ctx context.Context, tx pgx.Tx, season, harvest uuid.UUID) error {
	if err := lockSync(ctx, tx, "season"); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE pf_coop.season_current SET harvest_id = $2::uuid, updated_at = now(),
	    sync_seq = nextval('pf_coop.season_sync_seq') WHERE season_ref = $1::uuid`, season, harvest)
	return err
}
