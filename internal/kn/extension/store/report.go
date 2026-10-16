package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

const reportCols = `id, tenant_id, version_key, report_kind, commune_code, province_code, cooperative_ref, period,
  coalesce(season_code, ''), coalesce(variety_code, ''), sown_area_ha::text, sown_on, coalesce(growth_stage, ''),
  coalesce(pest_code, ''), coalesce(severity, ''), affected_area_ha::text, urgent, coalesce(note, ''), version,
  previous_id, supersedes_id, correction_reason, event_time, recorded_at, actor_ref, device_id, idem_key, seq`

func scanReport(row pgx.Row) (domain.Report, error) {
	var r domain.Report
	var coop, prev *uuid.UUID
	var sown, affected *string
	var sownOn *time.Time
	err := row.Scan(&r.ID, &r.TenantID, &r.VersionKey, &r.Kind, &r.CommuneCode, &r.ProvinceCode, &coop, &r.Period,
		&r.SeasonCode, &r.VarietyCode, &sown, &sownOn, &r.GrowthStage, &r.PestCode, &r.Severity, &affected, &r.Urgent,
		&r.Note, &r.Version, &prev, &r.SupersedesID, &r.CorrectionReason, &r.EventTime, &r.RecordedAt, &r.ActorRef,
		&r.DeviceID, &r.IdemKey, &r.Seq)
	if err != nil {
		return r, err
	}
	r.CooperativeRef, r.PreviousID, r.SownOn = refOrNil(coop), refOrNil(prev), sownOn
	if r.SownAreaHa, err = parseDec(sown); err != nil {
		return r, err
	}
	r.AffectedAreaHa, err = parseDec(affected)
	return r, err
}

func oneReport(row pgx.Row) (*domain.Report, error) {
	r, err := scanReport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ReportByIdemKey tìm báo cáo đã ghi với idem_key.
func ReportByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Report, error) {
	return oneReport(tx.QueryRow(ctx, "SELECT "+reportCols+" FROM pf_extension.field_report WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
}

// ReportByID đọc một phiên bản báo cáo.
func ReportByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.Report, error) {
	return oneReport(tx.QueryRow(ctx, "SELECT "+reportCols+" FROM pf_extension.field_report WHERE id = $1", id))
}

// Current là phiên bản hiện hành của một khóa phiên bản.
type Current struct {
	ReportID uuid.UUID
	Version  int
}

// CurrentByKey đọc (và khóa) phiên bản hiện hành của khóa.
func CurrentByKey(ctx context.Context, tx pgx.Tx, key string) (*Current, error) {
	var c Current
	err := tx.QueryRow(ctx, "SELECT report_id, version FROM pf_extension.field_report_current WHERE version_key = $1 FOR UPDATE", key).
		Scan(&c.ReportID, &c.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// InsertReport ghi sổ báo cáo.
func InsertReport(ctx context.Context, tx pgx.Tx, r *domain.Report) error {
	var sownOn *string
	if r.SownOn != nil {
		s := r.SownOn.Format(time.DateOnly)
		sownOn = &s
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.field_report
	    (id, tenant_id, stream_key, version_key, report_kind, commune_code, province_code, cooperative_ref, period,
	     season_code, variety_code, sown_area_ha, sown_on, growth_stage, pest_code, severity, affected_area_ha, urgent,
	     note, version, previous_id, supersedes_id, correction_reason, event_time, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::numeric, $13::date, $14, $15, $16, $17::numeric, $18,
	          $19, $20, $21, $22, $23, $24, $25, $26, $27, $28)
	  RETURNING seq, recorded_at`,
		r.ID, r.TenantID, domain.ReportStreamKey(r.VersionKey), r.VersionKey, r.Kind, r.CommuneCode, r.ProvinceCode,
		nilIfZero(r.CooperativeRef), r.Period, nilIfEmpty(r.SeasonCode), nilIfEmpty(r.VarietyCode), decPtr(r.SownAreaHa),
		sownOn, nilIfEmpty(r.GrowthStage), nilIfEmpty(r.PestCode), nilIfEmpty(r.Severity), decPtr(r.AffectedAreaHa),
		r.Urgent, nilIfEmpty(r.Note), r.Version, nilIfZero(r.PreviousID), r.SupersedesID, r.CorrectionReason,
		r.EventTime.UTC(), r.ActorRef, r.DeviceID, r.IdemKey, domain.RuleVersion).Scan(&r.Seq, &r.RecordedAt)
}

// UpsertCurrent đặt phiên bản hiện hành của khóa (R-21).
func UpsertCurrent(ctx context.Context, tx pgx.Tx, r domain.Report) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.field_report_current
	    (tenant_id, version_key, report_id, report_kind, commune_code, province_code, cooperative_ref, period,
	     sown_area_ha, affected_area_ha, version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10::numeric, $11)
	  ON CONFLICT (tenant_id, version_key) DO UPDATE SET
	    report_id = EXCLUDED.report_id, cooperative_ref = EXCLUDED.cooperative_ref,
	    sown_area_ha = EXCLUDED.sown_area_ha, affected_area_ha = EXCLUDED.affected_area_ha,
	    version = EXCLUDED.version, updated_at = now()`,
		r.TenantID, r.VersionKey, r.ID, r.Kind, r.CommuneCode, r.ProvinceCode, nilIfZero(r.CooperativeRef), r.Period,
		decPtr(r.SownAreaHa), decPtr(r.AffectedAreaHa), r.Version)
	return err
}

// ReportFilter lọc báo cáo hiện hành.
type ReportFilter struct {
	All    bool
	Units  []string
	Period string
	Kind   string
	After  uuid.UUID
	Limit  int
}

// ListCurrentReports liệt kê phiên bản hiện hành trong phạm vi, phân trang theo report_id.
func ListCurrentReports(ctx context.Context, tx pgx.Tx, f ReportFilter) ([]domain.Report, error) {
	rows, err := tx.Query(ctx, "SELECT "+reportCols+` FROM pf_extension.field_report
	  WHERE id IN (SELECT c.report_id FROM pf_extension.field_report_current c
	      WHERE ($1::boolean OR c.commune_code = ANY($2::text[]) OR c.province_code = ANY($2::text[]))
	        AND ($3 = '' OR c.period = $3) AND ($4 = '' OR c.report_kind = $4))
	    AND id > $5
	  ORDER BY id LIMIT $6`, f.All, units(f.Units), f.Period, f.Kind, f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Report
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReportVersions trả mọi phiên bản của một khóa theo thứ tự.
func ReportVersions(ctx context.Context, tx pgx.Tx, key string) ([]domain.Report, error) {
	rows, err := tx.Query(ctx, "SELECT "+reportCols+" FROM pf_extension.field_report WHERE version_key = $1 ORDER BY seq", key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Report
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
