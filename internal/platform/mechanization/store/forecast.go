package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// ---------------------------------------------------------------------------
// Dự báo nhu cầu (FN-09, cgh.forecast/v1) — sổ bất biến
// ---------------------------------------------------------------------------

const forecastRunCols = `id, tenant_id, season_code, season_year, reference_date, pilot_stages, method::text, result_count,
  missing_norms, event_time, actor_ref, idem_key, rule_version, seq, recorded_at`

func scanForecastRun(row pgx.Row) (domain.ForecastRun, error) {
	var r domain.ForecastRun
	var method string
	err := row.Scan(&r.ID, &r.TenantID, &r.Season.Code, &r.Season.Year, &r.ReferenceDate, &r.PilotStages, &method, &r.ResultCount,
		&r.MissingNorms, &r.EventTime, &r.ActorRef, &r.IdemKey, &r.RuleVersion, &r.Seq, &r.Recorded)
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal([]byte(method), &r.Method)
}

// InsertForecastRun ghi một lần chạy dự báo.
func InsertForecastRun(ctx context.Context, tx pgx.Tx, r *domain.ForecastRun) error {
	m, err := json.Marshal(r.Method)
	if err != nil {
		return err
	}
	if r.MissingNorms == nil {
		r.MissingNorms = []string{}
	}
	if r.PilotStages == nil {
		r.PilotStages = []string{}
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_mechanization.forecast_run
	    (id, tenant_id, stream_key, season_code, season_year, reference_date, pilot_stages, method, result_count, missing_norms,
	     event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12, $13, $14)
	  RETURNING seq, recorded_at`,
		r.ID, r.TenantID, domain.ForecastRunStreamKey(r.Season), r.Season.Code, r.Season.Year, date(r.ReferenceDate), r.PilotStages,
		string(m), r.ResultCount, r.MissingNorms, r.EventTime.UTC(), r.ActorRef, r.IdemKey, r.RuleVersion,
	).Scan(&r.Seq, &r.Recorded)
}

func oneForecastRun(ctx context.Context, tx pgx.Tx, where string, args ...any) (*domain.ForecastRun, error) {
	r, err := scanForecastRun(tx.QueryRow(ctx, "SELECT "+forecastRunCols+" FROM pf_mechanization.forecast_run WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// ForecastRunByIdem tìm lần chạy đã ghi với idem_key.
func ForecastRunByIdem(ctx context.Context, tx pgx.Tx, key string) (*domain.ForecastRun, error) {
	return oneForecastRun(ctx, tx, "idem_key = $1"+ledger.NewestByIdem(""), key)
}

// ForecastRunByID đọc một lần chạy.
func ForecastRunByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.ForecastRun, error) {
	return oneForecastRun(ctx, tx, "id = $1", id)
}

// ListForecastRuns liệt kê lần chạy dự báo (mới nhất trước); seasonCode rỗng = mọi vụ.
func ListForecastRuns(ctx context.Context, tx pgx.Tx, seasonCode string, year int) ([]domain.ForecastRun, error) {
	rows, err := tx.Query(ctx, "SELECT "+forecastRunCols+` FROM pf_mechanization.forecast_run
	  WHERE ($1 = '' OR season_code = $1) AND ($2 = 0 OR season_year = $2)
	  ORDER BY season_year DESC, season_code, seq DESC LIMIT 200`, seasonCode, year)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.ForecastRun, error) { return scanForecastRun(r) })
}

const forecastResultCols = `id, run_id, season_code, season_year, cooperative_ref, coalesce(commune_code, ''), coalesce(province_code, ''),
  farm_stage, machine_type, base_area_ha::text, growth_pct::text, forecast_area_ha::text, norm_key, norm_value::text, norm_version,
  required::text, required_whole, capacity_now, gap, status, inputs::text, seq`

func scanForecastResult(row pgx.Row) (domain.ForecastResult, error) {
	var r domain.ForecastResult
	var base, growth, fc, nv, req *string
	var normVersion *int
	var inputs string
	err := row.Scan(&r.ID, &r.RunID, &r.Season.Code, &r.Season.Year, &r.CooperativeRef, &r.CommuneCode, &r.ProvinceCode, &r.FarmStage,
		&r.MachineType, &base, &growth, &fc, &r.NormKey, &nv, &normVersion, &req, &r.RequiredWhole, &r.CapacityNow, &r.Gap, &r.Status,
		&inputs, &r.Seq)
	if err != nil {
		return r, err
	}
	if r.BaseAreaHa, err = parseDec(base); err != nil {
		return r, err
	}
	if r.GrowthPct, err = parseDec(growth); err != nil {
		return r, err
	}
	if r.ForecastAreaHa, err = parseDec(fc); err != nil {
		return r, err
	}
	if r.Required, err = parseDec(req); err != nil {
		return r, err
	}
	n, err := parseDec(nv)
	if err != nil {
		return r, err
	}
	if n != nil && normVersion != nil {
		r.Norm = &domain.ParamRef{Key: r.NormKey, Value: *n, Version: *normVersion}
	}
	return r, json.Unmarshal([]byte(inputs), &r.Inputs)
}

// InsertForecastResults ghi các dòng dự báo (bất biến) theo lô.
func InsertForecastResults(ctx context.Context, tx pgx.Tx, run domain.ForecastRun, results []*domain.ForecastResult) error {
	b := &pgx.Batch{}
	for _, r := range results {
		in, err := json.Marshal(r.Inputs)
		if err != nil {
			return err
		}
		var nv *string
		var nver *int
		if r.Norm != nil {
			s, v := r.Norm.Value.String(), r.Norm.Version
			nv, nver = &s, &v
		}
		row := r
		b.Queue(`INSERT INTO pf_mechanization.forecast_result
	    (id, tenant_id, stream_key, run_id, season_code, season_year, cooperative_ref, commune_code, province_code, farm_stage,
	     machine_type, base_area_ha, growth_pct, forecast_area_ha, norm_key, norm_value, norm_version, required, required_whole,
	     capacity_now, gap, status, inputs, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::numeric, $13::numeric, $14::numeric, $15, $16::numeric, $17,
	          $18::numeric, $19, $20, $21, $22, $23::jsonb, $24, $25, $26, $27)
	  RETURNING seq`,
			r.ID, run.TenantID, domain.ForecastResultStreamKey(run.ID), run.ID, run.Season.Code, run.Season.Year, r.CooperativeRef,
			nilIfEmpty(r.CommuneCode), nilIfEmpty(r.ProvinceCode), r.FarmStage, r.MachineType, decPtr(r.BaseAreaHa), decPtr(r.GrowthPct),
			decPtr(r.ForecastAreaHa), r.NormKey, nv, nver, decPtr(r.Required), r.RequiredWhole, r.CapacityNow, r.Gap, r.Status,
			string(in), run.EventTime.UTC(), run.ActorRef, run.IdemKey+":"+r.CooperativeRef.String()+":"+r.MachineType, run.RuleVersion,
		).QueryRow(func(qr pgx.Row) error { return qr.Scan(&row.Seq) })
	}
	return tx.SendBatch(ctx, b).Close()
}

// ForecastResults đọc dòng dự báo đã lưu của một lần chạy trong phạm vi.
func ForecastResults(ctx context.Context, tx pgx.Tx, f ResultFilter) ([]domain.ForecastResult, error) {
	rows, err := tx.Query(ctx, "SELECT "+forecastResultCols+` FROM pf_mechanization.forecast_result
	  WHERE run_id = $1
	    AND ($2::boolean OR commune_code = ANY($3::text[]) OR province_code = ANY($3::text[]))
	    AND ($4 = '' OR province_code = $4) AND ($5 = '' OR commune_code = $5)
	    AND ($6 = `+nilUUID+` OR cooperative_ref = $6) AND ($7 = '' OR farm_stage = $7) AND ($8 = '' OR machine_type = $8)
	  ORDER BY seq`, f.RunID, f.All, units(f.Units), f.ProvinceCode, f.CommuneCode, f.CooperativeRef, f.FarmStage, f.MachineType)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.ForecastResult, error) { return scanForecastResult(r) })
}
