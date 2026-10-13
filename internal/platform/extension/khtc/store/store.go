// Package store: SQL Danh mục KHTC, danh sách CT/DA, Kế hoạch năm (pf_extension.khtc_*, migration pf 20261013070305) trên pool
// vai extension_app, luôn trong giao dịch có ngữ cảnh tenant (RLS). Ba sổ chỉ ghi thêm (shared.apply_ledger: DB tính seq,
// prev_hash, row_hash, chống trùng idem_key); khtc_plan_entry chỉ ghi thêm (shared.apply_append_only).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtc/domain"
)

// Meta là cột chung của một dòng sổ.
type Meta struct {
	ID            uuid.UUID
	Seq           int64
	EventTime     time.Time
	RecordedAt    time.Time
	ActorRef      uuid.UUID
	OnBehalfOf    *uuid.UUID
	SupportReason *string
	IdemKey       string
}

// Lock tuần tự hóa ghi theo khóa nghiệp vụ (vd mã danh mục, năm kế hoạch).
func Lock(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.khtc:' || $1::text, 0))", key)
	return err
}

// ---------------------------------------------------------------------------
// Danh mục KHTC
// ---------------------------------------------------------------------------

// CatalogRow là một phiên bản mục danh mục.
type CatalogRow struct {
	domain.CatalogItem
	Meta
}

const catCols = `id, seq, kind, code, name, sort_order, status, default_for, attrs, event_time, recorded_at, actor_ref, on_behalf_of,
  support_reason, idem_key`

func scanCat(row pgx.Row) (CatalogRow, error) {
	var r CatalogRow
	var attrs []byte
	err := row.Scan(&r.ID, &r.Seq, &r.Kind, &r.Code, &r.Name, &r.SortOrder, &r.Status, &r.DefaultFor, &attrs, &r.EventTime,
		&r.RecordedAt, &r.ActorRef, &r.OnBehalfOf, &r.SupportReason, &r.IdemKey)
	if err != nil {
		return r, err
	}
	r.Attrs = map[string]string{}
	err = json.Unmarshal(attrs, &r.Attrs)
	return r, err
}

func collectCat(rows pgx.Rows, err error) ([]CatalogRow, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CatalogRow{}
	for rows.Next() {
		r, err := scanCat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertCatalog ghi một phiên bản (stream '<kind>:<code>').
func InsertCatalog(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *CatalogRow) error {
	attrs, err := json.Marshal(r.Attrs)
	if err != nil {
		return err
	}
	if r.DefaultFor == nil {
		r.DefaultFor = []string{}
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_catalog_item (id, tenant_id, stream_key, kind, code, name, sort_order,
	    status, default_for, attrs, event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::text, $5::text, $6::text, $7::int, $8::text, $9::text[], $10::jsonb,
	    $11::timestamptz, $12::uuid, $13::uuid, $14::text, $15::text, $16::text)
	  RETURNING seq, recorded_at`,
		r.ID, tid, r.Kind+":"+r.Code, r.Kind, r.Code, r.Name, r.SortOrder, r.Status, r.DefaultFor, string(attrs), r.EventTime.UTC(),
		r.ActorRef, r.OnBehalfOf, r.SupportReason, r.IdemKey, domain.RuleVersion).Scan(&r.Seq, &r.RecordedAt)
}

// CatalogCurrent trả bản hiện hành của mọi mục một loại ("" = mọi loại), theo loại, thứ tự, mã.
func CatalogCurrent(ctx context.Context, tx pgx.Tx, kind string) ([]CatalogRow, error) {
	return collectCat(tx.Query(ctx, `SELECT `+catCols+` FROM (
	    SELECT DISTINCT ON (kind, code) * FROM pf_extension.khtc_catalog_item
	    WHERE ($1::text = '' OR kind = $1::text) ORDER BY kind, code, seq DESC) c
	  ORDER BY kind, sort_order, code`, kind))
}

// CatalogHistory trả mọi phiên bản của một mục, mới nhất trước.
func CatalogHistory(ctx context.Context, tx pgx.Tx, kind, code string) ([]CatalogRow, error) {
	return collectCat(tx.Query(ctx, `SELECT `+catCols+` FROM pf_extension.khtc_catalog_item
	  WHERE kind = $1::text AND code = $2::text ORDER BY seq DESC`, kind, code))
}

// CatalogByIdem đọc dòng theo idem_key (mới nhất); không có → nil.
func CatalogByIdem(ctx context.Context, tx pgx.Tx, key string) (*CatalogRow, error) {
	r, err := scanCat(tx.QueryRow(ctx, `SELECT `+catCols+` FROM pf_extension.khtc_catalog_item WHERE idem_key = $1::text
	  ORDER BY recorded_at DESC, id DESC LIMIT 1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ---------------------------------------------------------------------------
// Danh sách CT/DA
// ---------------------------------------------------------------------------

// ProgramRow là một phiên bản CT/DA.
type ProgramRow struct {
	Ref uuid.UUID
	domain.Program
	Meta
}

const progCols = `id, seq, program_ref, code, name, kind, coalesce(field_code, ''), coalesce(owner_department, ''),
  coalesce(start_year, 0), coalesce(end_year, 0), status, coalesce(note, ''), event_time, recorded_at, actor_ref, on_behalf_of,
  support_reason, idem_key`

func scanProg(row pgx.Row) (ProgramRow, error) {
	var r ProgramRow
	err := row.Scan(&r.ID, &r.Seq, &r.Ref, &r.Code, &r.Name, &r.Kind, &r.FieldCode, &r.OwnerDepartment, &r.StartYear, &r.EndYear,
		&r.Status, &r.Note, &r.EventTime, &r.RecordedAt, &r.ActorRef, &r.OnBehalfOf, &r.SupportReason, &r.IdemKey)
	return r, err
}

func collectProg(rows pgx.Rows, err error) ([]ProgramRow, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProgramRow{}
	for rows.Next() {
		r, err := scanProg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func nz(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nzInt(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// InsertProgram ghi một phiên bản CT/DA (stream 'program:<ref>').
func InsertProgram(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *ProgramRow) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_program (id, tenant_id, stream_key, program_ref, code, name, kind,
	    field_code, owner_department, start_year, end_year, status, note, event_time, actor_ref, on_behalf_of, support_reason,
	    idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::text, $7::text, $8::text, $9::text, $10::smallint,
	    $11::smallint, $12::text, $13::text, $14::timestamptz, $15::uuid, $16::uuid, $17::text, $18::text, $19::text)
	  RETURNING seq, recorded_at`,
		r.ID, tid, "program:"+r.Ref.String(), r.Ref, r.Code, r.Name, r.Kind, nz(r.FieldCode), nz(r.OwnerDepartment),
		nzInt(r.StartYear), nzInt(r.EndYear), r.Status, nz(r.Note), r.EventTime.UTC(), r.ActorRef, r.OnBehalfOf, r.SupportReason,
		r.IdemKey, domain.RuleVersion).Scan(&r.Seq, &r.RecordedAt)
}

// ProgramsCurrent trả bản hiện hành của mọi CT/DA, theo mã.
func ProgramsCurrent(ctx context.Context, tx pgx.Tx) ([]ProgramRow, error) {
	return collectProg(tx.Query(ctx, `SELECT `+progCols+` FROM (
	    SELECT DISTINCT ON (program_ref) * FROM pf_extension.khtc_program ORDER BY program_ref, seq DESC) p
	  ORDER BY code, program_ref`))
}

// ProgramHistory trả mọi phiên bản của một CT/DA, mới nhất trước (rỗng = không có).
func ProgramHistory(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]ProgramRow, error) {
	return collectProg(tx.Query(ctx, `SELECT `+progCols+` FROM pf_extension.khtc_program WHERE program_ref = $1::uuid
	  ORDER BY seq DESC`, ref))
}

// ProgramCodeTaken cho biết mã đã thuộc CT/DA khác (bản hiện hành, kể cả đã ngừng).
func ProgramCodeTaken(ctx context.Context, tx pgx.Tx, code string, except uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM (
	    SELECT DISTINCT ON (program_ref) program_ref, code FROM pf_extension.khtc_program ORDER BY program_ref, seq DESC) p
	  WHERE lower(p.code) = lower($1::text) AND p.program_ref <> $2::uuid)`, code, except).Scan(&ok)
	return ok, err
}

// ProgramByIdem đọc dòng theo idem_key; không có → nil.
func ProgramByIdem(ctx context.Context, tx pgx.Tx, key string) (*ProgramRow, error) {
	r, err := scanProg(tx.QueryRow(ctx, `SELECT `+progCols+` FROM pf_extension.khtc_program WHERE idem_key = $1::text
	  ORDER BY recorded_at DESC, id DESC LIMIT 1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ---------------------------------------------------------------------------
// Kế hoạch năm
// ---------------------------------------------------------------------------

// VersionRow là phần đầu một phiên bản kế hoạch.
type VersionRow struct {
	Meta
	Year           int
	Basis          string
	DecisionNo     string
	DecisionDate   *time.Time
	EffectiveFrom  time.Time
	CopiedFromYear int
	Note           string
	EntriesCount   int
	EntriesHash    []byte
}

const verCols = `id, seq, plan_year, basis, coalesce(decision_no, ''), decision_date, effective_from, coalesce(copied_from_year, 0),
  coalesce(note, ''), entries_count, entries_hash, event_time, recorded_at, actor_ref, on_behalf_of, support_reason, idem_key`

func scanVer(row pgx.Row) (VersionRow, error) {
	var r VersionRow
	err := row.Scan(&r.ID, &r.Seq, &r.Year, &r.Basis, &r.DecisionNo, &r.DecisionDate, &r.EffectiveFrom, &r.CopiedFromYear, &r.Note,
		&r.EntriesCount, &r.EntriesHash, &r.EventTime, &r.RecordedAt, &r.ActorRef, &r.OnBehalfOf, &r.SupportReason, &r.IdemKey)
	return r, err
}

// InsertVersion ghi phần đầu phiên bản (stream 'plan:<năm>'; seq = số phiên bản).
func InsertVersion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *VersionRow) error {
	var dd any
	if r.DecisionDate != nil {
		dd = r.DecisionDate.Format(time.DateOnly)
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_plan_version (id, tenant_id, stream_key, plan_year, basis, decision_no,
	    decision_date, effective_from, copied_from_year, note, entries_count, entries_hash, event_time, actor_ref, on_behalf_of,
	    support_reason, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::smallint, $5::text, $6::text, $7::date, $8::date, $9::smallint, $10::text,
	    $11::int, $12::bytea, $13::timestamptz, $14::uuid, $15::uuid, $16::text, $17::text, $18::text)
	  RETURNING seq, recorded_at`,
		r.ID, tid, "plan:"+strconv.Itoa(r.Year), r.Year, r.Basis, nz(r.DecisionNo), dd, r.EffectiveFrom.Format(time.DateOnly),
		nzInt(r.CopiedFromYear), nz(r.Note), r.EntriesCount, r.EntriesHash, r.EventTime.UTC(), r.ActorRef, r.OnBehalfOf,
		r.SupportReason, r.IdemKey, domain.RuleVersion).Scan(&r.Seq, &r.RecordedAt)
}

// Versions trả mọi phiên bản của năm, theo số phiên bản.
func Versions(ctx context.Context, tx pgx.Tx, year int) ([]VersionRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+verCols+` FROM pf_extension.khtc_plan_version WHERE plan_year = $1::smallint ORDER BY seq`, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VersionRow{}
	for rows.Next() {
		r, err := scanVer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// VersionByIdem đọc phiên bản theo idem_key; không có → nil.
func VersionByIdem(ctx context.Context, tx pgx.Tx, key string) (*VersionRow, error) {
	r, err := scanVer(tx.QueryRow(ctx, `SELECT `+verCols+` FROM pf_extension.khtc_plan_version WHERE idem_key = $1::text
	  ORDER BY recorded_at DESC, id DESC LIMIT 1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func decArg(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.String()
}

// InsertEntries ghi cả cây của một phiên bản (một lô lệnh trong cùng giao dịch).
func InsertEntries(ctx context.Context, tx pgx.Tx, tid, versionID uuid.UUID, year int, es []domain.Entry) error {
	b := &pgx.Batch{}
	for i, e := range es {
		var stopped any
		if e.StoppedFrom != nil {
			stopped = e.StoppedFrom.Format(time.DateOnly)
		}
		communes := e.Communes
		if communes == nil {
			communes = []string{}
		}
		b.Queue(`INSERT INTO pf_extension.khtc_plan_entry (tenant_id, version_id, plan_year, entry_ref, position, tab, kind,
		    parent_ref, program_ref, sort_order, code, name, unit_code, planned_qty, scale_note, budget, funding_code, field_code,
		    department_code, communes, points_count, data_source, source_key, tracking, stopped_from, stop_decision, note)
		  VALUES ($1::uuid, $2::uuid, $3::smallint, $4::uuid, $5::int, $6::text, $7::text, $8::uuid, $9::uuid, $10::int, $11::text,
		    $12::text, $13::text, $14::numeric, $15::text, $16::numeric, $17::text, $18::text, $19::text, $20::text[], $21::int,
		    $22::text, $23::text, $24::text, $25::date, $26::text, $27::text)`,
			tid, versionID, year, e.Ref, i, e.Tab, e.Kind, e.ParentRef, e.ProgramRef, e.SortOrder, nz(e.Code), e.Name, nz(e.UnitCode),
			decArg(e.PlannedQty), nz(e.ScaleNote), decArg(e.Budget), nz(e.FundingCode), nz(e.FieldCode), nz(e.Department), communes,
			e.PointsCount, nz(e.DataSource), nz(e.SourceKey), e.Tracking, stopped, nz(e.StopDecision), nz(e.Note))
	}
	return tx.SendBatch(ctx, b).Close()
}

// Entries đọc cây của một phiên bản theo thứ tự lưu.
func Entries(ctx context.Context, tx pgx.Tx, versionID uuid.UUID) ([]domain.Entry, error) {
	rows, err := tx.Query(ctx, `SELECT entry_ref, tab, kind, parent_ref, program_ref, sort_order, coalesce(code, ''), name,
	    coalesce(unit_code, ''), planned_qty::text, coalesce(scale_note, ''), budget::text, coalesce(funding_code, ''),
	    coalesce(field_code, ''), coalesce(department_code, ''), communes, points_count, coalesce(data_source, ''),
	    coalesce(source_key, ''), tracking, stopped_from, coalesce(stop_decision, ''), coalesce(note, '')
	  FROM pf_extension.khtc_plan_entry WHERE version_id = $1::uuid ORDER BY position`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Entry{}
	for rows.Next() {
		var e domain.Entry
		var qty, budget *string
		if err := rows.Scan(&e.Ref, &e.Tab, &e.Kind, &e.ParentRef, &e.ProgramRef, &e.SortOrder, &e.Code, &e.Name, &e.UnitCode, &qty,
			&e.ScaleNote, &budget, &e.FundingCode, &e.FieldCode, &e.Department, &e.Communes, &e.PointsCount, &e.DataSource,
			&e.SourceKey, &e.Tracking, &e.StoppedFrom, &e.StopDecision, &e.Note); err != nil {
			return nil, err
		}
		for _, p := range []struct {
			s   *string
			dst **decimal.Decimal
		}{{qty, &e.PlannedQty}, {budget, &e.Budget}} {
			if p.s == nil {
				continue
			}
			d, err := decimal.NewFromString(*p.s)
			if err != nil {
				return nil, err
			}
			*p.dst = &d
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
