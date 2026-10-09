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
// Kết quả cân đối (FN-09) — sổ bất biến
// ---------------------------------------------------------------------------

const runCols = `id, tenant_id, season_code, season_year, reference_date, pilot_stages, thresholds::text, result_count,
  status_counts::text, missing_norms, event_time, actor_ref, idem_key, rule_version, seq, recorded_at`

func scanRun(row pgx.Row) (domain.BalanceRun, error) {
	var r domain.BalanceRun
	var th, counts string
	err := row.Scan(&r.ID, &r.TenantID, &r.Season.Code, &r.Season.Year, &r.ReferenceDate, &r.PilotStages, &th, &r.ResultCount,
		&counts, &r.MissingNorms, &r.EventTime, &r.ActorRef, &r.IdemKey, &r.RuleVersion, &r.Seq, &r.Recorded)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(th), &r.Thresholds); err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(counts), &r.StatusCounts)
	return r, err
}

// InsertRun ghi một lần cân đối.
func InsertRun(ctx context.Context, tx pgx.Tx, r *domain.BalanceRun) error {
	th, err := json.Marshal(r.Thresholds)
	if err != nil {
		return err
	}
	counts, err := json.Marshal(r.StatusCounts)
	if err != nil {
		return err
	}
	if r.MissingNorms == nil {
		r.MissingNorms = []string{}
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_mechanization.balance_run
	    (id, tenant_id, stream_key, season_code, season_year, reference_date, pilot_stages, thresholds, result_count, status_counts,
	     missing_norms, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10::jsonb, $11, $12, $13, $14, $15)
	  RETURNING seq, recorded_at`,
		r.ID, r.TenantID, domain.RunStreamKey(r.Season), r.Season.Code, r.Season.Year, date(r.ReferenceDate), r.PilotStages,
		string(th), r.ResultCount, string(counts), r.MissingNorms, r.EventTime.UTC(), r.ActorRef, r.IdemKey, r.RuleVersion,
	).Scan(&r.Seq, &r.Recorded)
}

func oneRun(ctx context.Context, tx pgx.Tx, where string, args ...any) (*domain.BalanceRun, error) {
	r, err := scanRun(tx.QueryRow(ctx, "SELECT "+runCols+" FROM pf_mechanization.balance_run WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// RunByIdem tìm lần cân đối đã ghi với idem_key (gọi lặp).
func RunByIdem(ctx context.Context, tx pgx.Tx, key string) (*domain.BalanceRun, error) {
	return oneRun(ctx, tx, "idem_key = $1"+ledger.NewestByIdem(""), key)
}

// RunByID đọc một lần cân đối.
func RunByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.BalanceRun, error) {
	return oneRun(ctx, tx, "id = $1", id)
}

// LatestRun trả lần cân đối mới nhất của vụ; Code rỗng = mới nhất của mọi vụ.
func LatestRun(ctx context.Context, tx pgx.Tx, s domain.Season) (*domain.BalanceRun, error) {
	if s.Code == "" {
		return oneRun(ctx, tx, "true ORDER BY event_time DESC, id DESC LIMIT 1")
	}
	return oneRun(ctx, tx, "stream_key = $1 ORDER BY seq DESC LIMIT 1", domain.RunStreamKey(s))
}

// ListRuns liệt kê các lần cân đối; seasonCode rỗng = mọi vụ; latestOnly = chỉ lần mới nhất của mỗi vụ.
func ListRuns(ctx context.Context, tx pgx.Tx, seasonCode string, year int, latestOnly bool) ([]domain.BalanceRun, error) {
	q := "SELECT " + runCols + ` FROM pf_mechanization.balance_run
	  WHERE ($1 = '' OR season_code = $1) AND ($2 = 0 OR season_year = $2)`
	if latestOnly {
		q = "SELECT DISTINCT ON (season_code, season_year) " + runCols + ` FROM pf_mechanization.balance_run
	  WHERE ($1 = '' OR season_code = $1) AND ($2 = 0 OR season_year = $2)
	  ORDER BY season_code, season_year, seq DESC`
		q = "SELECT * FROM (" + q + ") x ORDER BY season_year, season_code"
	} else {
		q += " ORDER BY season_year DESC, season_code, seq DESC LIMIT 200"
	}
	rows, err := tx.Query(ctx, q, seasonCode, year)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.BalanceRun, error) { return scanRun(r) })
}

const resultCols = `id, run_id, season_code, season_year, reference_date, cooperative_ref, coalesce(commune_code, ''),
  coalesce(province_code, ''), farm_stage, machine_type, area_ha::text, coalesce(area_source, ''), area_locked, norm_key,
  norm_value::text, norm_version, coalesce(norm_source_doc, ''), required::text, required_whole, machines_total, capacity,
  coverage_pct::text, gap, status, seq`

func scanResult(row pgx.Row) (domain.BalanceResult, error) {
	var r domain.BalanceResult
	var area, normValue, required, pct *string
	var normVersion *int
	var normDoc, status string
	err := row.Scan(&r.ID, &r.RunID, &r.Season.Code, &r.Season.Year, &r.ReferenceDate, &r.CooperativeRef, &r.CommuneCode,
		&r.ProvinceCode, &r.FarmStage, &r.MachineType, &area, &r.AreaSource, &r.AreaLocked, &r.NormKey, &normValue, &normVersion,
		&normDoc, &required, &r.Figures.RequiredWhole, &r.MachinesTotal, &r.Capacity, &pct, &r.Figures.Gap, &status, &r.Seq)
	if err != nil {
		return r, err
	}
	r.Figures.Status = domain.Coverage(status)
	if r.AreaHa, err = parseDec(area); err != nil {
		return r, err
	}
	nv, err := parseDec(normValue)
	if err != nil {
		return r, err
	}
	if nv != nil && normVersion != nil {
		r.Norm = &domain.ParamRef{Key: r.NormKey, Value: *nv, Version: *normVersion, SourceDoc: normDoc}
	}
	if r.Figures.Required, err = parseDec(required); err != nil {
		return r, err
	}
	r.Figures.CoveragePct, err = parseDec(pct)
	return r, err
}

// InsertResults ghi các dòng kết quả (bất biến) của một lần cân đối theo lô (pgx.Batch);
// idem_key của mỗi dòng = <idem_key lần tính>:<HTX>:<chủng loại>.
func InsertResults(ctx context.Context, tx pgx.Tx, run domain.BalanceRun, results []*domain.BalanceResult) error {
	b := &pgx.Batch{}
	for _, r := range results {
		var normValue *string
		var normVersion *int
		var normDoc *string
		if r.Norm != nil {
			s := r.Norm.Value.String()
			v := r.Norm.Version
			normValue, normVersion, normDoc = &s, &v, nilIfEmpty(r.Norm.SourceDoc)
		}
		f := r.Figures
		key := run.IdemKey + ":" + r.CooperativeRef.String() + ":" + r.MachineType
		row := r
		b.Queue(`INSERT INTO pf_mechanization.balance_result
	    (id, tenant_id, stream_key, run_id, season_code, season_year, reference_date, cooperative_ref, commune_code, province_code,
	     farm_stage, machine_type, area_ha, area_source, area_locked, norm_key, norm_value, norm_version, norm_source_doc,
	     required, required_whole, machines_total, capacity, coverage_pct, gap, status, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::numeric, $14, $15, $16, $17::numeric, $18, $19,
	          $20::numeric, $21, $22, $23, $24::numeric, $25, $26, $27, $28, $29, $30)
	  RETURNING seq`,
			r.ID, run.TenantID, domain.ResultStreamKey(run.ID), run.ID, run.Season.Code, run.Season.Year, date(run.ReferenceDate),
			r.CooperativeRef, nilIfEmpty(r.CommuneCode), nilIfEmpty(r.ProvinceCode), r.FarmStage, r.MachineType, decPtr(r.AreaHa),
			nilIfEmpty(r.AreaSource), r.AreaLocked, r.NormKey, normValue, normVersion, normDoc, decPtr(f.Required), f.RequiredWhole,
			r.MachinesTotal, r.Capacity, decPtr(f.CoveragePct), f.Gap, string(f.Status), run.EventTime.UTC(), run.ActorRef, key,
			run.RuleVersion,
		).QueryRow(func(qr pgx.Row) error { return qr.Scan(&row.Seq) })
	}
	return tx.SendBatch(ctx, b).Close()
}

// ResultFilter lọc kết quả đã lưu của một lần cân đối theo phạm vi và địa bàn.
type ResultFilter struct {
	RunID          uuid.UUID
	All            bool     // phạm vi toàn quốc
	Units          []string // phạm vi: mã tỉnh/xã được thấy
	ProvinceCode   string
	CommuneCode    string
	CooperativeRef uuid.UUID
	FarmStage      string
	MachineType    string
}

// Results đọc kết quả đã lưu (FN-10: không tính lại).
func Results(ctx context.Context, tx pgx.Tx, f ResultFilter) ([]domain.BalanceResult, error) {
	rows, err := tx.Query(ctx, "SELECT "+resultCols+` FROM pf_mechanization.balance_result
	  WHERE run_id = $1
	    AND ($2::boolean OR commune_code = ANY($3::text[]) OR province_code = ANY($3::text[]))
	    AND ($4 = '' OR province_code = $4) AND ($5 = '' OR commune_code = $5)
	    AND ($6 = `+nilUUID+` OR cooperative_ref = $6) AND ($7 = '' OR farm_stage = $7) AND ($8 = '' OR machine_type = $8)
	  ORDER BY seq`, f.RunID, f.All, units(f.Units), f.ProvinceCode, f.CommuneCode, f.CooperativeRef, f.FarmStage, f.MachineType)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.BalanceResult, error) { return scanResult(r) })
}
