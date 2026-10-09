package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// Leg là một dòng sổ leg_event (một chặng của một phiên bản kế hoạch).
type Leg struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	Seq            int64
	TripID         uuid.UUID
	LegID          uuid.UUID
	PlanRevision   int
	LegSeq         int
	Mode           port.Mode
	FromKind       string
	FromRef        *uuid.UUID
	From           port.GeoPoint
	ToKind         string
	ToRef          *uuid.UUID
	To             port.GeoPoint
	DistanceKm     decimal.Decimal
	RouteSource    port.RouteSource
	SnapshotID     *uuid.UUID
	NetworkVersion *int
	ManualReason   *string
	MassKg         *mass.Kg
	RateVNDPerTKm  decimal.Decimal
	PlannedCost    money.Amount
	ParamVersion   string
	Flags          []string
	EventTime      time.Time
	RecordedAt     time.Time
	ActorRef       uuid.UUID
	IdemKey        string
	RuleVersion    string
}

// InsertLeg ghi một chặng vào sổ.
func InsertLeg(ctx context.Context, tx pgx.Tx, l *Leg) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_logistics.leg_event
	    (id, tenant_id, stream_key, trip_id, leg_id, plan_revision, leg_seq, mode, from_kind, from_ref, from_lat, from_lng, to_kind, to_ref,
	     to_lat, to_lng, distance_km, route_source, route_snapshot_id, network_version, manual_reason, mass_kg, rate_vnd_per_t_km,
	     planned_cost, param_version, flags, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::numeric, $12::numeric, $13, $14, $15::numeric, $16::numeric, $17::numeric, $18,
	          $19, $20, $21, $22::numeric, $23::numeric, $24::numeric, $25, $26, $27, $28, $29, $30)
	  RETURNING seq, recorded_at`,
		l.ID, l.TenantID, "legs:"+l.TripID.String(), l.TripID, l.LegID, l.PlanRevision, l.LegSeq, string(l.Mode), l.FromKind, l.FromRef,
		l.From.Lat.String(), l.From.Lng.String(), l.ToKind, l.ToRef, l.To.Lat.String(), l.To.Lng.String(), l.DistanceKm.StringFixed(3),
		string(l.RouteSource), l.SnapshotID, l.NetworkVersion, l.ManualReason, kgStr(l.MassKg), l.RateVNDPerTKm.String(),
		l.PlannedCost.String(), l.ParamVersion, flags(l.Flags), l.EventTime.UTC(), l.ActorRef, l.IdemKey, l.RuleVersion).Scan(&l.Seq, &l.RecordedAt)
}

const legCols = `id, tenant_id, seq, trip_id, leg_id, plan_revision, leg_seq, mode, from_kind, from_ref, from_lat::text, from_lng::text,
  to_kind, to_ref, to_lat::text, to_lng::text, distance_km::text, route_source, route_snapshot_id, network_version, manual_reason,
  mass_kg::text, rate_vnd_per_t_km::text, planned_cost::text, param_version, flags, event_time, recorded_at, actor_ref, idem_key,
  rule_version`

func scanLeg(row pgx.Row) (Leg, error) {
	var l Leg
	var mode, src string
	var fl, fg, tl, tg, km, rate, cost string
	var mk *string
	err := row.Scan(&l.ID, &l.TenantID, &l.Seq, &l.TripID, &l.LegID, &l.PlanRevision, &l.LegSeq, &mode, &l.FromKind, &l.FromRef, &fl, &fg,
		&l.ToKind, &l.ToRef, &tl, &tg, &km, &src, &l.SnapshotID, &l.NetworkVersion, &l.ManualReason, &mk, &rate, &cost, &l.ParamVersion,
		&l.Flags, &l.EventTime, &l.RecordedAt, &l.ActorRef, &l.IdemKey, &l.RuleVersion)
	if err != nil {
		return l, err
	}
	l.Mode, l.RouteSource = port.Mode(mode), port.RouteSource(src)
	for _, p := range []struct {
		s string
		d *decimal.Decimal
	}{{fl, &l.From.Lat}, {fg, &l.From.Lng}, {tl, &l.To.Lat}, {tg, &l.To.Lng}, {km, &l.DistanceKm}, {rate, &l.RateVNDPerTKm}} {
		if *p.d, err = decimal.NewFromString(p.s); err != nil {
			return l, err
		}
	}
	if l.MassKg, err = parseKg(mk); err != nil {
		return l, err
	}
	a, err := parseAmt(&cost)
	if err != nil {
		return l, err
	}
	l.PlannedCost = *a
	return l, nil
}

func queryLegs(ctx context.Context, tx pgx.Tx, q string, args ...any) ([]Leg, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Leg
	for rows.Next() {
		l, err := scanLeg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// LatestPlan đọc phiên bản kế hoạch chặng mới nhất của chuyến (asOf ≠ nil: chỉ bản ghi recorded_at < asOf).
func LatestPlan(ctx context.Context, tx pgx.Tx, trip uuid.UUID, asOf *time.Time) ([]Leg, error) {
	return queryLegs(ctx, tx, "SELECT "+legCols+` FROM mkg_logistics.leg_event
	    WHERE trip_id = $1 AND ($2::timestamptz IS NULL OR recorded_at < $2)
	      AND plan_revision = (SELECT max(plan_revision) FROM mkg_logistics.leg_event
	                            WHERE trip_id = $1 AND ($2::timestamptz IS NULL OR recorded_at < $2))
	    ORDER BY leg_seq`, trip, utcPtr(asOf))
}

// LegsByIdemPrefix đọc các chặng của một lần lập (idem_key = prefix + ":" + seq).
// Các chặng của một lần lập ghi trong một giao dịch nên cùng recorded_at; phát lại
// sau hạn 30 ngày của shared.ledger_idem (ADR-0012) tạo bộ chặng mới cùng khóa —
// chỉ lấy bộ mới nhất.
func LegsByIdemPrefix(ctx context.Context, tx pgx.Tx, prefix string) ([]Leg, error) {
	return queryLegs(ctx, tx, "SELECT "+legCols+` FROM mkg_logistics.leg_event
	    WHERE left(idem_key, length($1)) = $1
	      AND recorded_at = (SELECT max(recorded_at) FROM mkg_logistics.leg_event WHERE left(idem_key, length($1)) = $1)
	    ORDER BY leg_seq`, prefix+":")
}

// ---------------------------------------------------------------------------
// Ảnh chụp kết quả định tuyến (nguyên văn + SHA-256)
// ---------------------------------------------------------------------------

// Snapshot là một dòng route_snapshot.
type Snapshot struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	TripID         uuid.UUID
	LegID          uuid.UUID
	Provider       string
	Request        []byte
	Raw            []byte
	RawSHA256      []byte
	GrantorTenant  *uuid.UUID
	NetworkVersion *int
	FetchedAt      time.Time
	EventTime      time.Time
	RecordedAt     time.Time
	ActorRef       uuid.UUID
	IdemKey        string
	RuleVersion    string
}

// Nhà cung cấp định tuyến.
const (
	ProviderWaterway = "pf_partner_waterway"
	ProviderRoad     = "road_haversine"
)

// InsertSnapshot ghi ảnh chụp (DB kiểm raw_sha256 = sha256(raw)).
func InsertSnapshot(ctx context.Context, tx pgx.Tx, s *Snapshot) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_logistics.route_snapshot
	    (id, tenant_id, stream_key, trip_id, leg_id, provider, request, raw, raw_sha256, grantor_tenant, network_version, fetched_at,
	     event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING recorded_at`,
		s.ID, s.TenantID, "route:"+s.TripID.String(), s.TripID, s.LegID, s.Provider, string(s.Request), s.Raw, s.RawSHA256, s.GrantorTenant,
		s.NetworkVersion, s.FetchedAt.UTC(), s.EventTime.UTC(), s.ActorRef, s.IdemKey, s.RuleVersion).Scan(&s.RecordedAt)
}

// GetSnapshot đọc một ảnh chụp.
func GetSnapshot(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Snapshot, error) {
	var s Snapshot
	err := tx.QueryRow(ctx, `SELECT id, tenant_id, trip_id, leg_id, provider, request, raw, raw_sha256, grantor_tenant, network_version,
	    fetched_at, event_time, recorded_at, actor_ref, idem_key, rule_version FROM mkg_logistics.route_snapshot WHERE id = $1`, id).Scan(
		&s.ID, &s.TenantID, &s.TripID, &s.LegID, &s.Provider, &s.Request, &s.Raw, &s.RawSHA256, &s.GrantorTenant, &s.NetworkVersion,
		&s.FetchedAt, &s.EventTime, &s.RecordedAt, &s.ActorRef, &s.IdemKey, &s.RuleVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}
