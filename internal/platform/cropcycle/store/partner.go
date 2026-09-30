package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// HarvestDates là ngày gieo / ngày thu hoạch của một vụ cho bên nhận của cấp quyền
// season_harvest_dates (ADR-0020; proto FM-01/02 — không cuộn rơm trước ngày gặt). KHÔNG có
// nông hộ, sản lượng, người mở vụ.
type HarvestDates struct {
	SeasonRef           uuid.UUID
	PlotRef             uuid.UUID
	CooperativeRef      uuid.UUID
	CommuneCode         string
	SeasonCode          string
	SeasonYear          int
	SowingDate          time.Time
	ExpectedHarvestDate time.Time
	HarvestOpenDate     time.Time
	Status              string
	CompletedAt         *time.Time
	SyncSeq             int64
}

// SeasonHarvestDates trả vụ của tenant hiện tại (bên cấp) theo sync_seq > after (con trỏ đổi khi
// vụ đổi); plot ≠ Nil lọc theo thửa.
func SeasonHarvestDates(ctx context.Context, tx pgx.Tx, plot uuid.UUID, after int64, limit int) ([]HarvestDates, error) {
	rows, err := tx.Query(ctx, `SELECT season_ref, plot_ref, cooperative_ref, commune_code, season_code, season_year, sowing_date,
	    expected_harvest_date, harvest_open_date, status, completed_at, sync_seq
	  FROM pf_cropcycle.season_current
	 WHERE sync_seq > $1::bigint AND ($2::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR plot_ref = $2::uuid)
	 ORDER BY sync_seq LIMIT $3::int`, after, plot, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HarvestDates{}
	for rows.Next() {
		var h HarvestDates
		if err := rows.Scan(&h.SeasonRef, &h.PlotRef, &h.CooperativeRef, &h.CommuneCode, &h.SeasonCode, &h.SeasonYear, &h.SowingDate,
			&h.ExpectedHarvestDate, &h.HarvestOpenDate, &h.Status, &h.CompletedAt, &h.SyncSeq); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
