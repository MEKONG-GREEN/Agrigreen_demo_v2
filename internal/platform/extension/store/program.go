package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
)

const programCols = `id, tenant_id, version_key, commune_code, province_code, cooperative_ref, season_code, season_year,
  registered_area_ha::text, deploying_area_ha::text, completed_area_ha::text, coalesce(note, ''), version, previous_id,
  supersedes_id, correction_reason, event_time, recorded_at, actor_ref, device_id, idem_key, seq`

func scanProgram(row pgx.Row) (domain.ProgramReport, error) {
	var p domain.ProgramReport
	var reg, dep, comp string
	var prev *uuid.UUID
	err := row.Scan(&p.ID, &p.TenantID, &p.VersionKey, &p.CommuneCode, &p.ProvinceCode, &p.CooperativeRef, &p.SeasonCode,
		&p.SeasonYear, &reg, &dep, &comp, &p.Note, &p.Version, &prev, &p.SupersedesID, &p.CorrectionReason, &p.EventTime,
		&p.RecordedAt, &p.ActorRef, &p.DeviceID, &p.IdemKey, &p.Seq)
	if err != nil {
		return p, err
	}
	p.PreviousID = refOrNil(prev)
	for _, x := range []struct {
		s string
		d *decimal.Decimal
	}{{reg, &p.RegisteredHa}, {dep, &p.DeployingHa}, {comp, &p.CompletedHa}} {
		if *x.d, err = decimal.NewFromString(x.s); err != nil {
			return p, err
		}
	}
	return p, nil
}

func oneProgram(row pgx.Row) (*domain.ProgramReport, error) {
	p, err := scanProgram(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ProgramByIdemKey tìm báo cáo Đề án đã ghi với idem_key.
func ProgramByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.ProgramReport, error) {
	return oneProgram(tx.QueryRow(ctx, "SELECT "+programCols+" FROM pf_extension.program_report WHERE idem_key = $1", key))
}

// ProgramByID đọc một phiên bản báo cáo Đề án.
func ProgramByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.ProgramReport, error) {
	return oneProgram(tx.QueryRow(ctx, "SELECT "+programCols+" FROM pf_extension.program_report WHERE id = $1", id))
}

// ProgramCurrent là phiên bản hiện hành của một khóa kèm trạng thái duyệt.
type ProgramCurrent struct {
	ReportID         uuid.UUID
	Version          int
	SubmittedBy      uuid.UUID
	ApprovedReportID *uuid.UUID
}

// ProgramCurrentByKey đọc (và khóa) phiên bản hiện hành của khóa.
func ProgramCurrentByKey(ctx context.Context, tx pgx.Tx, key string) (*ProgramCurrent, error) {
	var c ProgramCurrent
	err := tx.QueryRow(ctx, `SELECT report_id, version, submitted_by, approved_report_id
	  FROM pf_extension.program_report_current WHERE version_key = $1 FOR UPDATE`, key).
		Scan(&c.ReportID, &c.Version, &c.SubmittedBy, &c.ApprovedReportID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// InsertProgram ghi sổ báo cáo Đề án.
func InsertProgram(ctx context.Context, tx pgx.Tx, p *domain.ProgramReport) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.program_report
	    (id, tenant_id, stream_key, version_key, commune_code, province_code, cooperative_ref, season_code, season_year,
	     registered_area_ha, deploying_area_ha, completed_area_ha, note, version, previous_id, supersedes_id, correction_reason,
	     event_time, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::numeric, $11::numeric, $12::numeric, $13, $14, $15, $16, $17,
	          $18, $19, $20, $21, $22)
	  RETURNING seq, recorded_at`,
		p.ID, p.TenantID, domain.ProgramStreamKey(p.VersionKey), p.VersionKey, p.CommuneCode, p.ProvinceCode, p.CooperativeRef,
		p.SeasonCode, p.SeasonYear, p.RegisteredHa.String(), p.DeployingHa.String(), p.CompletedHa.String(), nilIfEmpty(p.Note),
		p.Version, nilIfZero(p.PreviousID), p.SupersedesID, p.CorrectionReason, p.EventTime.UTC(), p.ActorRef, p.DeviceID,
		p.IdemKey, domain.RuleVersion).Scan(&p.Seq, &p.RecordedAt)
}

// UpsertProgramCurrent đặt phiên bản hiện hành (phiên bản mới → chưa duyệt).
func UpsertProgramCurrent(ctx context.Context, tx pgx.Tx, p domain.ProgramReport) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.program_report_current
	    (tenant_id, version_key, report_id, commune_code, province_code, cooperative_ref, season_code, season_year,
	     registered_area_ha, deploying_area_ha, completed_area_ha, version, submitted_by)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10::numeric, $11::numeric, $12, $13)
	  ON CONFLICT (tenant_id, version_key) DO UPDATE SET
	    report_id = EXCLUDED.report_id, registered_area_ha = EXCLUDED.registered_area_ha,
	    deploying_area_ha = EXCLUDED.deploying_area_ha, completed_area_ha = EXCLUDED.completed_area_ha,
	    version = EXCLUDED.version, submitted_by = EXCLUDED.submitted_by,
	    approved_report_id = NULL, approved_by = NULL, approved_at = NULL, updated_at = now()`,
		p.TenantID, p.VersionKey, p.ID, p.CommuneCode, p.ProvinceCode, p.CooperativeRef, p.SeasonCode, p.SeasonYear,
		p.RegisteredHa.String(), p.DeployingHa.String(), p.CompletedHa.String(), p.Version, p.ActorRef)
	return err
}

// ApproveProgram ghi quyết định duyệt và đánh dấu phiên bản hiện hành đã duyệt.
// Đã duyệt trước đó → false.
func ApproveProgram(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p domain.ProgramReport, approver uuid.UUID, at time.Time, note string) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO pf_extension.program_report_approval (tenant_id, report_id, approver_ref, approved_at, note)
	  VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, tid, p.ID, approver, at.UTC(), nilIfEmpty(note))
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE pf_extension.program_report_current
	  SET approved_report_id = $2, approved_by = $3, approved_at = $4, updated_at = now()
	  WHERE version_key = $1 AND report_id = $2`, p.VersionKey, p.ID, approver, at.UTC())
	return err == nil, err
}
