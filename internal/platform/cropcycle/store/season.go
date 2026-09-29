package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
)

// SeasonEvent là một dòng sổ season_event.
type SeasonEvent struct {
	ID                  uuid.UUID
	TenantID            uuid.UUID
	Seq                 int64
	SeasonRef           uuid.UUID
	Action              string
	PlotRef             uuid.UUID
	PlotCode            string
	HouseholdRef        uuid.UUID
	CooperativeRef      uuid.UUID
	CommuneCode         string
	SeasonCode          string
	SeasonYear          int
	VarietyCode         string
	SOPTemplateRef      uuid.UUID
	SOPVersionID        uuid.UUID
	SOPVersionNo        int
	SowingDate          time.Time
	ExpectedHarvestDate time.Time
	HarvestOpenDate     time.Time
	AreaHa              decimal.Decimal
	ExpectedYield       *decimal.Decimal
	CopiedFrom          uuid.UUID
	HarvestID           uuid.UUID
	Reason              string
	GPS                 domain.GPS
	LocationStatus      string
	DistanceM           *decimal.Decimal
	OnBehalf            bool
	Channel             string
	EventTime           time.Time
	RecordedAt          time.Time
	SupersedesID        *uuid.UUID
	CorrectionReason    *string
	ActorRef            uuid.UUID
	DeviceID            *string
	IdemKey             string
}

// StreamSeason là khóa chuỗi của vụ.
func StreamSeason(ref uuid.UUID) string { return "season:" + ref.String() }

// InsertSeasonEvent ghi một sự kiện vụ; DB tính seq và băm.
func InsertSeasonEvent(ctx context.Context, tx pgx.Tx, e *SeasonEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_cropcycle.season_event
	    (id, tenant_id, stream_key, season_ref, action, plot_ref, plot_code, household_ref, cooperative_ref, commune_code,
	     season_code, season_year, variety_code, sop_template_ref, sop_version_id, sop_version_no, sowing_date,
	     expected_harvest_date, harvest_open_date, area_ha, expected_yield_t_per_ha, copied_from_season_ref, harvest_id, reason,
	     gps, gps_accuracy_m, gps_source, location_status, distance_m, on_behalf, channel, event_time, supersedes_id,
	     correction_reason, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::uuid, $7::text, $8::uuid, $9::uuid, $10::text,
	     $11::text, $12::int, $13::text, $14::uuid, $15::uuid, $16::int, $17::date,
	     $18::date, $19::date, $20::numeric, $21::numeric, $22::uuid, $23::uuid, $24::text,
	     `+gpsInsertSQL("$25", "$26")+`, $27::numeric, $28::text, $29::text, $30::numeric, $31::boolean, $32::text, $33::timestamptz, $34::uuid,
	     $35::text, $36::uuid, $37::text, $38::text, $39::text)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, StreamSeason(e.SeasonRef), e.SeasonRef, e.Action, e.PlotRef, e.PlotCode, nilIfZero(e.HouseholdRef), e.CooperativeRef, e.CommuneCode,
		e.SeasonCode, e.SeasonYear, e.VarietyCode, e.SOPTemplateRef, e.SOPVersionID, e.SOPVersionNo, day(e.SowingDate),
		day(e.ExpectedHarvestDate), day(e.HarvestOpenDate), e.AreaHa.String(), decPtr(e.ExpectedYield), nilIfZero(e.CopiedFrom), nilIfZero(e.HarvestID), nilIfEmpty(e.Reason),
		decPtr(e.GPS.Lon), decPtr(e.GPS.Lat), decPtr(e.GPS.AccuracyM), e.GPS.Source, e.LocationStatus, decPtr(e.DistanceM), e.OnBehalf, e.Channel, e.EventTime.UTC(), e.SupersedesID,
		e.CorrectionReason, e.ActorRef, e.DeviceID, e.IdemKey, domain.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

const seasonEventCols = `id, tenant_id, seq, season_ref, action, plot_ref, plot_code, household_ref, cooperative_ref, commune_code,
  season_code, season_year, variety_code, sop_template_ref, sop_version_id, sop_version_no, sowing_date, expected_harvest_date,
  harvest_open_date, area_ha::text, expected_yield_t_per_ha::text, copied_from_season_ref, harvest_id, coalesce(reason, ''),
  ` + gpsSelect + `, gps_accuracy_m::text, gps_source, location_status, distance_m::text, on_behalf, channel, event_time, recorded_at,
  supersedes_id, correction_reason, actor_ref, device_id, idem_key`

func scanSeasonEvent(row pgx.Row) (SeasonEvent, error) {
	var e SeasonEvent
	var hh, copied, harvest *uuid.UUID
	var area string
	var expected, lon, lat, acc, dist *string
	if err := row.Scan(&e.ID, &e.TenantID, &e.Seq, &e.SeasonRef, &e.Action, &e.PlotRef, &e.PlotCode, &hh, &e.CooperativeRef, &e.CommuneCode,
		&e.SeasonCode, &e.SeasonYear, &e.VarietyCode, &e.SOPTemplateRef, &e.SOPVersionID, &e.SOPVersionNo, &e.SowingDate, &e.ExpectedHarvestDate,
		&e.HarvestOpenDate, &area, &expected, &copied, &harvest, &e.Reason,
		&lon, &lat, &acc, &e.GPS.Source, &e.LocationStatus, &dist, &e.OnBehalf, &e.Channel, &e.EventTime, &e.RecordedAt,
		&e.SupersedesID, &e.CorrectionReason, &e.ActorRef, &e.DeviceID, &e.IdemKey); err != nil {
		return e, err
	}
	e.HouseholdRef, e.CopiedFrom, e.HarvestID = deref(hh), deref(copied), deref(harvest)
	var err error
	if e.AreaHa, err = decimal.NewFromString(area); err != nil {
		return e, err
	}
	for _, p := range []struct {
		s *string
		d **decimal.Decimal
	}{{expected, &e.ExpectedYield}, {lon, &e.GPS.Lon}, {lat, &e.GPS.Lat}, {acc, &e.GPS.AccuracyM}, {dist, &e.DistanceM}} {
		if *p.d, err = parseDecPtr(p.s); err != nil {
			return e, err
		}
	}
	return e, nil
}

func oneSeasonEvent(ctx context.Context, tx pgx.Tx, where string, arg any) (*SeasonEvent, error) {
	e, err := scanSeasonEvent(tx.QueryRow(ctx, "SELECT "+seasonEventCols+" FROM pf_cropcycle.season_event WHERE "+where+" LIMIT 1", arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// SeasonEventByIdem đọc sự kiện theo idem_key (gọi lặp).
func SeasonEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*SeasonEvent, error) {
	return oneSeasonEvent(ctx, tx, "idem_key = $1::text", key)
}

// SeasonEventByID đọc sự kiện theo id.
func SeasonEventByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*SeasonEvent, error) {
	return oneSeasonEvent(ctx, tx, "id = $1::uuid", id)
}

// SeasonEvents liệt kê lịch sử sự kiện của vụ theo seq.
func SeasonEvents(ctx context.Context, tx pgx.Tx, season uuid.UUID) ([]SeasonEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+seasonEventCols+" FROM pf_cropcycle.season_event WHERE stream_key = $1::text ORDER BY seq", StreamSeason(season))
	if err != nil {
		return nil, err
	}
	return collectSeasonEvents(rows)
}

// AllSeasonEvents đọc toàn bộ sổ vụ của tenant theo thứ tự ghi (dựng lại bảng chiếu, R-22).
func AllSeasonEvents(ctx context.Context, tx pgx.Tx) ([]SeasonEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+seasonEventCols+" FROM pf_cropcycle.season_event ORDER BY recorded_at, stream_key, seq")
	if err != nil {
		return nil, err
	}
	return collectSeasonEvents(rows)
}

func collectSeasonEvents(rows pgx.Rows) ([]SeasonEvent, error) {
	defer rows.Close()
	var out []SeasonEvent
	for rows.Next() {
		e, err := scanSeasonEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Bảng chiếu season_current
// ---------------------------------------------------------------------------

// Season là trạng thái hiện tại của một vụ.
type Season struct {
	SeasonRef           uuid.UUID
	PlotRef             uuid.UUID
	PlotCode            string
	HouseholdRef        uuid.UUID
	CooperativeRef      uuid.UUID
	CommuneCode         string
	SeasonCode          string
	SeasonYear          int
	VarietyCode         string
	SOPTemplateRef      uuid.UUID
	SOPVersionID        uuid.UUID
	SOPVersionNo        int
	SowingDate          time.Time
	ExpectedHarvestDate time.Time
	HarvestOpenDate     time.Time
	AreaHa              decimal.Decimal
	ExpectedYield       *decimal.Decimal
	Status              string
	CurrentEventID      uuid.UUID
	Version             int64
	LocationStatus      string
	GPSSource           string
	OnBehalf            bool
	OpenedBy            uuid.UUID
	OpenedAt            time.Time
	CopiedFrom          uuid.UUID
	HarvestID           uuid.UUID
	CompletedAt         *time.Time
	SyncSeq             int64
	UpdatedAt           time.Time
}

// ApplySeasonEvent áp một sự kiện sổ lên trạng thái hiện tại (dùng chung cho
// ghi trực tiếp và dựng lại — R-21/R-22). cur nil với sự kiện opened.
func ApplySeasonEvent(cur *Season, e SeasonEvent) Season {
	var s Season
	if cur != nil {
		s = *cur
	}
	switch e.Action {
	case "opened":
		s = Season{SeasonRef: e.SeasonRef, OpenedBy: e.ActorRef, OpenedAt: e.EventTime, OnBehalf: e.OnBehalf,
			LocationStatus: e.LocationStatus, GPSSource: e.GPS.Source, CopiedFrom: e.CopiedFrom, Status: "open"}
	case "completed":
		s.Status, s.HarvestID = "completed", e.HarvestID
		t := e.EventTime
		s.CompletedAt = &t
	case "cancelled":
		s.Status = "cancelled"
	}
	s.PlotRef, s.PlotCode, s.HouseholdRef, s.CooperativeRef, s.CommuneCode = e.PlotRef, e.PlotCode, e.HouseholdRef, e.CooperativeRef, e.CommuneCode
	s.SeasonCode, s.SeasonYear, s.VarietyCode = e.SeasonCode, e.SeasonYear, e.VarietyCode
	s.SOPTemplateRef, s.SOPVersionID, s.SOPVersionNo = e.SOPTemplateRef, e.SOPVersionID, e.SOPVersionNo
	s.SowingDate, s.ExpectedHarvestDate, s.HarvestOpenDate = e.SowingDate, e.ExpectedHarvestDate, e.HarvestOpenDate
	s.AreaHa, s.ExpectedYield = e.AreaHa, e.ExpectedYield
	s.CurrentEventID, s.Version = e.ID, e.Seq
	return s
}

// SaveSeason ghi (chèn hoặc cập nhật) trạng thái hiện tại; cấp sync_seq mới.
func SaveSeason(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s Season) error {
	if err := lockSync(ctx, tx, "season"); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_cropcycle.season_current
	    (tenant_id, season_ref, plot_ref, plot_code, household_ref, cooperative_ref, commune_code, season_code, season_year,
	     variety_code, sop_template_ref, sop_version_id, sop_version_no, sowing_date, expected_harvest_date, harvest_open_date,
	     area_ha, expected_yield_t_per_ha, status, current_event_id, version, location_status, gps_source, on_behalf, opened_by,
	     opened_at, copied_from_season_ref, harvest_id, completed_at)
	  VALUES ($1::uuid, $2::uuid, $3::uuid, $4::text, $5::uuid, $6::uuid, $7::text, $8::text, $9::int,
	     $10::text, $11::uuid, $12::uuid, $13::int, $14::date, $15::date, $16::date,
	     $17::numeric, $18::numeric, $19::text, $20::uuid, $21::bigint, $22::text, $23::text, $24::boolean, $25::uuid,
	     $26::timestamptz, $27::uuid, $28::uuid, $29::timestamptz)
	  ON CONFLICT (tenant_id, season_ref) DO UPDATE SET
	     plot_ref = EXCLUDED.plot_ref, plot_code = EXCLUDED.plot_code, household_ref = EXCLUDED.household_ref,
	     cooperative_ref = EXCLUDED.cooperative_ref, commune_code = EXCLUDED.commune_code, season_code = EXCLUDED.season_code,
	     season_year = EXCLUDED.season_year, variety_code = EXCLUDED.variety_code, sop_template_ref = EXCLUDED.sop_template_ref,
	     sop_version_id = EXCLUDED.sop_version_id, sop_version_no = EXCLUDED.sop_version_no, sowing_date = EXCLUDED.sowing_date,
	     expected_harvest_date = EXCLUDED.expected_harvest_date, harvest_open_date = EXCLUDED.harvest_open_date,
	     area_ha = EXCLUDED.area_ha, expected_yield_t_per_ha = EXCLUDED.expected_yield_t_per_ha, status = EXCLUDED.status,
	     current_event_id = EXCLUDED.current_event_id, version = EXCLUDED.version, location_status = EXCLUDED.location_status,
	     gps_source = EXCLUDED.gps_source, on_behalf = EXCLUDED.on_behalf, opened_by = EXCLUDED.opened_by,
	     opened_at = EXCLUDED.opened_at, copied_from_season_ref = EXCLUDED.copied_from_season_ref,
	     harvest_id = EXCLUDED.harvest_id, completed_at = EXCLUDED.completed_at,
	     sync_seq = nextval('pf_cropcycle.season_sync_seq'), updated_at = now()`,
		tid, s.SeasonRef, s.PlotRef, s.PlotCode, nilIfZero(s.HouseholdRef), s.CooperativeRef, s.CommuneCode, s.SeasonCode, s.SeasonYear,
		s.VarietyCode, s.SOPTemplateRef, s.SOPVersionID, s.SOPVersionNo, day(s.SowingDate), day(s.ExpectedHarvestDate), day(s.HarvestOpenDate),
		s.AreaHa.String(), decPtr(s.ExpectedYield), s.Status, s.CurrentEventID, s.Version, s.LocationStatus, s.GPSSource, s.OnBehalf, s.OpenedBy,
		s.OpenedAt.UTC(), nilIfZero(s.CopiedFrom), nilIfZero(s.HarvestID), s.CompletedAt)
	return err
}

const seasonCols = `s.season_ref, s.plot_ref, s.plot_code, s.household_ref, s.cooperative_ref, s.commune_code, s.season_code,
  s.season_year, s.variety_code, s.sop_template_ref, s.sop_version_id, s.sop_version_no, s.sowing_date, s.expected_harvest_date,
  s.harvest_open_date, s.area_ha::text, s.expected_yield_t_per_ha::text, s.status, s.current_event_id, s.version, s.location_status,
  s.gps_source, s.on_behalf, s.opened_by, s.opened_at, s.copied_from_season_ref, s.harvest_id, s.completed_at, s.sync_seq, s.updated_at`

func scanSeason(row pgx.Row) (Season, error) {
	var s Season
	var hh, copied, harvest *uuid.UUID
	var area string
	var expected *string
	if err := row.Scan(&s.SeasonRef, &s.PlotRef, &s.PlotCode, &hh, &s.CooperativeRef, &s.CommuneCode, &s.SeasonCode,
		&s.SeasonYear, &s.VarietyCode, &s.SOPTemplateRef, &s.SOPVersionID, &s.SOPVersionNo, &s.SowingDate, &s.ExpectedHarvestDate,
		&s.HarvestOpenDate, &area, &expected, &s.Status, &s.CurrentEventID, &s.Version, &s.LocationStatus,
		&s.GPSSource, &s.OnBehalf, &s.OpenedBy, &s.OpenedAt, &copied, &harvest, &s.CompletedAt, &s.SyncSeq, &s.UpdatedAt); err != nil {
		return s, err
	}
	s.HouseholdRef, s.CopiedFrom, s.HarvestID = deref(hh), deref(copied), deref(harvest)
	var err error
	if s.AreaHa, err = decimal.NewFromString(area); err != nil {
		return s, err
	}
	s.ExpectedYield, err = parseDecPtr(expected)
	return s, err
}

func oneSeason(ctx context.Context, tx pgx.Tx, q string, args ...any) (*Season, error) {
	s, err := scanSeason(tx.QueryRow(ctx, q, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SeasonByRef đọc vụ; forUpdate khóa dòng.
func SeasonByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID, forUpdate bool) (*Season, error) {
	q := "SELECT " + seasonCols + " FROM pf_cropcycle.season_current s WHERE s.season_ref = $1::uuid"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneSeason(ctx, tx, q, ref)
}

// OpenSeasonOfPlot trả vụ đang mở của thửa (nếu có).
func OpenSeasonOfPlot(ctx context.Context, tx pgx.Tx, plot uuid.UUID, forUpdate bool) (*Season, error) {
	q := "SELECT " + seasonCols + " FROM pf_cropcycle.season_current s WHERE s.plot_ref = $1::uuid AND s.status = 'open'"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneSeason(ctx, tx, q, plot)
}

// LatestSeasonOfPlot trả vụ gần nhất (không kể vụ đã hủy) của thửa.
func LatestSeasonOfPlot(ctx context.Context, tx pgx.Tx, plot uuid.UUID) (*Season, error) {
	return oneSeason(ctx, tx, "SELECT "+seasonCols+` FROM pf_cropcycle.season_current s
	  WHERE s.plot_ref = $1::uuid AND s.status <> 'cancelled' ORDER BY s.opened_at DESC, s.season_ref DESC LIMIT 1`, plot)
}

// OpenSeasonByPlotCode trả vụ đang mở của thửa có mã hiển thị code trong HTX coop (nhập sản lượng Excel).
func OpenSeasonByPlotCode(ctx context.Context, tx pgx.Tx, coop uuid.UUID, code string) (*Season, error) {
	return oneSeason(ctx, tx, "SELECT "+seasonCols+` FROM pf_cropcycle.season_current s
	  WHERE s.cooperative_ref = $1::uuid AND s.plot_code = $2::text AND s.status = 'open' LIMIT 1`, coop, code)
}

// SeasonFilter lọc danh sách vụ.
type SeasonFilter struct {
	Scope          Filter
	CooperativeRef uuid.UUID
	PlotRef        uuid.UUID
	Status         string
	SeasonCode     string
	SeasonYear     int
	VarietyCode    string
	After          uuid.UUID
	Limit          int
}

// ListSeasons liệt kê vụ trong phạm vi theo season_ref (cursor).
func ListSeasons(ctx context.Context, tx pgx.Tx, f SeasonFilter) ([]Season, error) {
	args := append(f.Scope.args(), nilIfZero(f.CooperativeRef), nilIfZero(f.PlotRef), f.Status, f.SeasonCode, f.SeasonYear, f.VarietyCode, f.After, f.Limit)
	rows, err := tx.Query(ctx, "SELECT "+seasonCols+" FROM pf_cropcycle.season_current s WHERE "+scopeSQL("s", 1)+`
	    AND ($5::uuid IS NULL OR s.cooperative_ref = $5::uuid) AND ($6::uuid IS NULL OR s.plot_ref = $6::uuid)
	    AND ($7::text = '' OR s.status = $7::text) AND ($8::text = '' OR s.season_code = $8::text)
	    AND ($9::int = 0 OR s.season_year = $9::int) AND ($10::text = '' OR s.variety_code = $10::text)
	    AND s.season_ref > $11::uuid
	  ORDER BY s.season_ref LIMIT $12::int`, args...)
	if err != nil {
		return nil, err
	}
	return collectSeasons(rows)
}

// StageSeasons trả vụ cho lớp giai đoạn GIS (GIS US-SEASON-01): mọi vụ đang mở và vụ hoàn thành từ doneSince,
// trong phạm vi, mỗi thửa ưu tiên vụ đang mở rồi vụ mở gần nhất; tối đa limit dòng (theo plot_ref).
func StageSeasons(ctx context.Context, tx pgx.Tx, scope Filter, coop uuid.UUID, commune string, doneSince time.Time, limit int) ([]Season, error) {
	args := append(scope.args(), nilIfZero(coop), commune, doneSince.UTC(), limit)
	rows, err := tx.Query(ctx, "SELECT "+seasonCols+` FROM (
	    SELECT DISTINCT ON (x.plot_ref) x.* FROM pf_cropcycle.season_current x
	    WHERE (x.status = 'open' OR (x.status = 'completed' AND x.completed_at >= $7::timestamptz))
	    ORDER BY x.plot_ref, (x.status = 'open') DESC, x.opened_at DESC, x.season_ref DESC) s
	  WHERE `+scopeSQL("s", 1)+`
	    AND ($5::uuid IS NULL OR s.cooperative_ref = $5::uuid) AND ($6::text = '' OR s.commune_code = $6::text)
	  ORDER BY s.plot_ref LIMIT $8::int`, args...)
	if err != nil {
		return nil, err
	}
	return collectSeasons(rows)
}

// SeasonsChangedAfter trả vụ trong phạm vi có sync_seq > after (chiều xuống).
func SeasonsChangedAfter(ctx context.Context, tx pgx.Tx, scope Filter, after int64, limit int) ([]Season, error) {
	args := append(scope.args(), after, limit)
	rows, err := tx.Query(ctx, "SELECT "+seasonCols+" FROM pf_cropcycle.season_current s WHERE "+scopeSQL("s", 1)+`
	    AND s.sync_seq > $5::bigint ORDER BY s.sync_seq LIMIT $6::int`, args...)
	if err != nil {
		return nil, err
	}
	return collectSeasons(rows)
}

func collectSeasons(rows pgx.Rows) ([]Season, error) {
	defer rows.Close()
	var out []Season
	for rows.Next() {
		s, err := scanSeason(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
