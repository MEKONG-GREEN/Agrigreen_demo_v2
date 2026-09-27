package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
)

// ---------------------------------------------------------------------------
// Diện tích canh tác & lịch mùa vụ (FN-06)
// ---------------------------------------------------------------------------

const areaCols = `id, cooperative_ref, season_code, season_year, coalesce(commune_code, ''), area_ha::text, source, locked,
  calendar::text, source_at, last_seq`

func scanArea(row pgx.Row) (domain.SeasonArea, error) {
	var a domain.SeasonArea
	var area, cal string
	err := row.Scan(&a.ID, &a.CooperativeRef, &a.Season.Code, &a.Season.Year, &a.CommuneCode, &area, &a.Source, &a.Locked, &cal,
		&a.SourceAt, &a.LastSeq)
	if err != nil {
		return a, err
	}
	d, err := parseDec(&area)
	if err != nil {
		return a, err
	}
	a.AreaHa = *d
	err = json.Unmarshal([]byte(cal), &a.Calendar)
	return a, err
}

// AreaByKey đọc diện tích vụ của HTX; forUpdate khóa dòng.
func AreaByKey(ctx context.Context, tx pgx.Tx, coop uuid.UUID, s domain.Season, forUpdate bool) (*domain.SeasonArea, error) {
	q := "SELECT " + areaCols + " FROM pf_mechanization.season_area WHERE cooperative_ref = $1 AND season_code = $2 AND season_year = $3"
	if forUpdate {
		q += " FOR UPDATE"
	}
	a, err := scanArea(tx.QueryRow(ctx, q, coop, s.Code, s.Year))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &a, err
}

// AreaByID đọc diện tích vụ theo id; forUpdate khóa dòng.
func AreaByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.SeasonArea, error) {
	q := "SELECT " + areaCols + " FROM pf_mechanization.season_area WHERE id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	a, err := scanArea(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &a, err
}

// AreasOfSeason trả mọi bản ghi diện tích của một vụ; coop khác Nil = chỉ HTX đó.
func AreasOfSeason(ctx context.Context, tx pgx.Tx, s domain.Season, coop uuid.UUID) ([]domain.SeasonArea, error) {
	rows, err := tx.Query(ctx, "SELECT "+areaCols+` FROM pf_mechanization.season_area
	  WHERE season_code = $1 AND season_year = $2 AND ($3 = `+nilUUID+` OR cooperative_ref = $3)
	  ORDER BY cooperative_ref`, s.Code, s.Year, coop)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.SeasonArea, error) { return scanArea(r) })
}

func calendarJSON(c []domain.CalendarEntry) (string, error) {
	if c == nil {
		c = []domain.CalendarEntry{}
	}
	b, err := json.Marshal(c)
	return string(b), err
}

const areaEventCols = `id, tenant_id, area_id, action, cooperative_ref, season_code, season_year, area_ha::text, source, locked,
  calendar::text, coalesce(reason, ''), event_time, actor_ref, idem_key, seq, recorded_at`

func scanAreaEvent(row pgx.Row) (domain.AreaEvent, error) {
	var e domain.AreaEvent
	var action, area, cal string
	a := &e.Area
	err := row.Scan(&e.ID, &e.TenantID, &a.ID, &action, &a.CooperativeRef, &a.Season.Code, &a.Season.Year, &area, &a.Source,
		&a.Locked, &cal, &e.Reason, &e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.Recorded)
	if err != nil {
		return e, err
	}
	e.Action = domain.AreaAction(action)
	d, err := parseDec(&area)
	if err != nil {
		return e, err
	}
	a.AreaHa = *d
	err = json.Unmarshal([]byte(cal), &a.Calendar)
	return e, err
}

// InsertAreaEvent ghi sổ diện tích vụ.
func InsertAreaEvent(ctx context.Context, tx pgx.Tx, e *domain.AreaEvent) error {
	a := e.Area
	cal, err := calendarJSON(a.Calendar)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_mechanization.season_area_event
	    (id, tenant_id, stream_key, area_id, action, cooperative_ref, season_code, season_year, area_ha, source, locked, calendar,
	     reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10, $11, $12::jsonb, $13, $14, $15, $16, $17)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, domain.AreaStreamKey(a.ID), a.ID, string(e.Action), a.CooperativeRef, a.Season.Code, a.Season.Year,
		a.AreaHa.String(), a.Source, a.Locked, cal, nilIfEmpty(e.Reason), e.EventTime.UTC(), e.ActorRef, e.IdemKey,
		domain.AreaRuleVersion).Scan(&e.Seq, &e.Recorded)
}

// AreaEventByIdem tìm dòng sổ diện tích đã ghi với idem_key.
func AreaEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*domain.AreaEvent, error) {
	e, err := scanAreaEvent(tx.QueryRow(ctx, "SELECT "+areaEventCols+" FROM pf_mechanization.season_area_event WHERE idem_key = $1", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

// AreaEvents trả Lịch sử thay đổi của một bản ghi diện tích vụ (FN-06 B7).
func AreaEvents(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]domain.AreaEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+areaEventCols+" FROM pf_mechanization.season_area_event WHERE stream_key = $1 ORDER BY seq",
		domain.AreaStreamKey(id))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.AreaEvent, error) { return scanAreaEvent(r) })
}

// UpsertArea ghi trạng thái hiện tại của diện tích vụ (R-21).
func UpsertArea(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, a domain.SeasonArea) error {
	cal, err := calendarJSON(a.Calendar)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_mechanization.season_area
	    (tenant_id, id, cooperative_ref, season_code, season_year, commune_code, area_ha, source, locked, calendar, source_at, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8, $9, $10::jsonb, $11, $12)
	  ON CONFLICT (tenant_id, id) DO UPDATE SET commune_code = EXCLUDED.commune_code, area_ha = EXCLUDED.area_ha,
	    source = EXCLUDED.source, locked = EXCLUDED.locked, calendar = EXCLUDED.calendar, source_at = EXCLUDED.source_at,
	    last_seq = EXCLUDED.last_seq, updated_at = now()`,
		tenantID, a.ID, a.CooperativeRef, a.Season.Code, a.Season.Year, nilIfEmpty(a.CommuneCode), a.AreaHa.String(), a.Source,
		a.Locked, cal, a.SourceAt.UTC(), a.LastSeq)
	return err
}
