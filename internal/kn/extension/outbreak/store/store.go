// Package store: SQL Báo cáo dịch bệnh BM-10 và báo nghi dịch (pf_extension.cnts_outbreak, cnts_suspect — W11.49) trên
// pool vai extension_app, luôn trong giao dịch có ngữ cảnh tenant (RLS). Hai sổ chỉ ghi thêm (shared.apply_ledger: DB
// tính seq, prev_hash, row_hash, chống trùng idem_key).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/domain"
)

// Meta là cột chung của một dòng sổ.
type Meta struct {
	ID            uuid.UUID
	Seq           int64
	Event         string
	StatusReason  *string
	EventTime     time.Time
	RecordedAt    time.Time
	ActorRef      uuid.UUID
	OnBehalfOf    *uuid.UUID
	SupportReason *string
	IdemKey       string
}

// Lock tuần tự hóa ghi theo khóa nghiệp vụ (ổ dịch, tin báo).
func Lock(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.cnts_outbreak:' || $1::text, 0))", key)
	return err
}

func optText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func latLon(p *domain.Point) (*string, *string) {
	if p == nil {
		return nil, nil
	}
	la, lo := p.Lat.String(), p.Lon.String()
	return &la, &lo
}

func point(lat, lon *string) (*domain.Point, error) {
	if lat == nil || lon == nil {
		return nil, nil
	}
	la, err := decimal.NewFromString(*lat)
	if err != nil {
		return nil, err
	}
	lo, err := decimal.NewFromString(*lon)
	if err != nil {
		return nil, err
	}
	return &domain.Point{Lat: la, Lon: lo}, nil
}

func decPtr(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func refStrings(rs []uuid.UUID) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.String())
	}
	return out
}

func parseRefs(ss []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(ss))
	for _, s := range ss {
		u, err := uuid.Parse(s)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func decText(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

// ---------------------------------------------------------------------------
// Ổ dịch
// ---------------------------------------------------------------------------

// OutbreakRow là một phiên bản ổ dịch.
type OutbreakRow struct {
	Ref uuid.UUID
	domain.Outbreak
	Meta
}

const obCols = `id, seq, outbreak_ref, event, commune_code, disease_code, species_code, received_at, source, suspect_ref,
  household_ref, locality, ST_Y(location)::numeric(12,7)::text, ST_X(location)::numeric(12,7)::text, location_source,
  gps_accuracy_m::text, herd_size, herd_age_days, dead, symptoms, sample_date, lab_name, lab_result, culled_head,
  culled_kg::text, chemical_liters::text, buffer_note, prevention_note, attachment_refs::text[], as_of, progress_note, status_reason,
  captured_at, event_time, recorded_at, actor_ref, on_behalf_of, support_reason, idem_key`

func scanOutbreak(row pgx.Row) (OutbreakRow, error) {
	var r OutbreakRow
	var locality, labName, labResult, buffer, prevention, progress, lat, lon, acc, kg, liters *string
	var refs []string
	var herdAge, culled *int32
	var herd, dead int32
	err := row.Scan(&r.ID, &r.Seq, &r.Ref, &r.Event, &r.CommuneCode, &r.DiseaseCode, &r.SpeciesCode, &r.ReceivedAt, &r.Source,
		&r.SuspectRef, &r.HouseholdRef, &locality, &lat, &lon, &r.LocationSource, &acc, &herd, &herdAge, &dead, &r.Symptoms,
		&r.SampleDate, &labName, &labResult, &culled, &kg, &liters, &buffer, &prevention, &refs, &r.AsOf, &progress,
		&r.StatusReason, &r.CapturedAt, &r.EventTime, &r.RecordedAt, &r.ActorRef, &r.OnBehalfOf, &r.SupportReason, &r.IdemKey)
	if err != nil {
		return r, err
	}
	r.Locality, r.LabName, r.LabResult = str(locality), str(labName), str(labResult)
	r.BufferNote, r.PreventionNote, r.ProgressNote = str(buffer), str(prevention), str(progress)
	r.HerdSize, r.Dead = int64(herd), int64(dead)
	if herdAge != nil {
		v := int64(*herdAge)
		r.HerdAgeDays = &v
	}
	if culled != nil {
		v := int64(*culled)
		r.CulledHead = &v
	}
	if r.Location, err = point(lat, lon); err != nil {
		return r, err
	}
	if r.GPSAccuracyM, err = decPtr(acc); err != nil {
		return r, err
	}
	if r.CulledKg, err = decPtr(kg); err != nil {
		return r, err
	}
	if r.ChemicalLiters, err = decPtr(liters); err != nil {
		return r, err
	}
	if r.AttachmentRefs, err = parseRefs(refs); err != nil {
		return r, err
	}
	return r, nil
}

func collectOutbreaks(rows pgx.Rows, err error) ([]OutbreakRow, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OutbreakRow{}
	for rows.Next() {
		r, err := scanOutbreak(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertOutbreak ghi một phiên bản ổ dịch (stream 'outbreak:<ref>').
func InsertOutbreak(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *OutbreakRow) error {
	lat, lon := latLon(r.Location)
	refs := refStrings(r.AttachmentRefs)
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_outbreak (id, tenant_id, stream_key, outbreak_ref, event, commune_code,
	    disease_code, species_code, received_at, source, suspect_ref, household_ref, locality, location, location_source,
	    gps_accuracy_m, herd_size, herd_age_days, dead, symptoms, sample_date, lab_name, lab_result, culled_head, culled_kg,
	    chemical_liters, buffer_note, prevention_note, attachment_refs, as_of, progress_note, status_reason, captured_at,
	    event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::text, $7::text, $8::text, $9::timestamptz, $10::text,
	    $11::uuid, $12::uuid, $13::text, ST_SetSRID(ST_MakePoint($15::text::float8, $14::text::float8), 4326), $16::text,
	    $17::numeric, $18::int, $19::int, $20::int, $21::text, $22::date, $23::text, $24::text, $25::int, $26::numeric,
	    $27::numeric, $28::text, $29::text, $30::text[]::uuid[], $31::date, $32::text, $33::text, $34::timestamptz, $35::timestamptz,
	    $36::uuid, $37::uuid, $38::text, $39::text, $40::text)
	  RETURNING seq, recorded_at`,
		r.ID, tid, "outbreak:"+r.Ref.String(), r.Ref, r.Event, r.CommuneCode, r.DiseaseCode, r.SpeciesCode, r.ReceivedAt.UTC(),
		r.Source, r.SuspectRef, r.HouseholdRef, optText(r.Locality), lat, lon, r.LocationSource, decText(r.GPSAccuracyM),
		r.HerdSize, r.HerdAgeDays, r.Dead, r.Symptoms, r.SampleDate, optText(r.LabName), optText(r.LabResult), r.CulledHead,
		decText(r.CulledKg), decText(r.ChemicalLiters), optText(r.BufferNote), optText(r.PreventionNote), refs, r.AsOf,
		optText(r.ProgressNote), r.StatusReason, r.CapturedAt, r.EventTime.UTC(), r.ActorRef, r.OnBehalfOf, r.SupportReason,
		r.IdemKey, domain.RuleVersion).Scan(&r.Seq, &r.RecordedAt)
}

// OutbreakFilter lọc danh sách ổ dịch (bản hiện hành).
type OutbreakFilter struct {
	Communes    []string // nil = mọi xã (đã lọc phạm vi ở ứng dụng khi khác nil)
	DiseaseCode string
	From, To    *time.Time // ngày tiếp nhận (DB-02, giờ Việt Nam) trong [From, To]
}

// OutbreaksCurrent trả bản hiện hành của mọi ổ dịch thỏa bộ lọc, mới tiếp nhận trước.
func OutbreaksCurrent(ctx context.Context, tx pgx.Tx, f OutbreakFilter) ([]OutbreakRow, error) {
	return collectOutbreaks(tx.Query(ctx, `SELECT `+obCols+` FROM (
	    SELECT DISTINCT ON (outbreak_ref) * FROM pf_extension.cnts_outbreak ORDER BY outbreak_ref, seq DESC) o
	  WHERE ($1::text[] IS NULL OR commune_code = ANY($1::text[]))
	    AND ($2::text = '' OR disease_code = $2::text)
	    AND ($3::date IS NULL OR (received_at AT TIME ZONE 'Asia/Ho_Chi_Minh')::date >= $3::date)
	    AND ($4::date IS NULL OR (received_at AT TIME ZONE 'Asia/Ho_Chi_Minh')::date <= $4::date)
	  ORDER BY received_at DESC, outbreak_ref`, f.Communes, f.DiseaseCode, f.From, f.To))
}

// OutbreakHistory trả mọi phiên bản của một ổ dịch, mới nhất trước (rỗng = không có).
func OutbreakHistory(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]OutbreakRow, error) {
	return collectOutbreaks(tx.Query(ctx, `SELECT `+obCols+` FROM pf_extension.cnts_outbreak WHERE outbreak_ref = $1::uuid
	  ORDER BY seq DESC`, ref))
}

// OutbreakByIdem đọc phiên bản theo idem_key; không có → nil.
func OutbreakByIdem(ctx context.Context, tx pgx.Tx, key string) (*OutbreakRow, error) {
	r, err := scanOutbreak(tx.QueryRow(ctx, `SELECT `+obCols+` FROM pf_extension.cnts_outbreak WHERE idem_key = $1::text
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
// Báo nghi dịch
// ---------------------------------------------------------------------------

// SuspectRow là một phiên bản tin báo nghi dịch.
type SuspectRow struct {
	Ref uuid.UUID
	domain.Suspect
	OutbreakRef *uuid.UUID
	Meta
}

const susCols = `id, seq, suspect_ref, event, commune_code, reporter_kind, observed_at, species_code, disease_code,
  household_ref, locality, ST_Y(location)::numeric(12,7)::text, ST_X(location)::numeric(12,7)::text, gps_accuracy_m::text,
  affected, dead, description, attachment_refs::text[], outbreak_ref, status_reason, captured_at, event_time, recorded_at,
  actor_ref, on_behalf_of, support_reason, idem_key`

func scanSuspect(row pgx.Row) (SuspectRow, error) {
	var r SuspectRow
	var species, disease, locality, lat, lon, acc *string
	var refs []string
	var affected, dead *int32
	err := row.Scan(&r.ID, &r.Seq, &r.Ref, &r.Event, &r.CommuneCode, &r.ReporterKind, &r.ObservedAt, &species, &disease,
		&r.HouseholdRef, &locality, &lat, &lon, &acc, &affected, &dead, &r.Description, &refs, &r.OutbreakRef,
		&r.StatusReason, &r.CapturedAt, &r.EventTime, &r.RecordedAt, &r.ActorRef, &r.OnBehalfOf, &r.SupportReason, &r.IdemKey)
	if err != nil {
		return r, err
	}
	r.SpeciesCode, r.DiseaseCode, r.Locality = str(species), str(disease), str(locality)
	if affected != nil {
		v := int64(*affected)
		r.Affected = &v
	}
	if dead != nil {
		v := int64(*dead)
		r.Dead = &v
	}
	if r.Location, err = point(lat, lon); err != nil {
		return r, err
	}
	if r.GPSAccuracyM, err = decPtr(acc); err != nil {
		return r, err
	}
	if r.AttachmentRefs, err = parseRefs(refs); err != nil {
		return r, err
	}
	return r, nil
}

func collectSuspects(rows pgx.Rows, err error) ([]SuspectRow, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SuspectRow{}
	for rows.Next() {
		r, err := scanSuspect(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertSuspect ghi một phiên bản tin báo (stream 'suspect:<ref>').
func InsertSuspect(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *SuspectRow) error {
	lat, lon := latLon(r.Location)
	refs := refStrings(r.AttachmentRefs)
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_suspect (id, tenant_id, stream_key, suspect_ref, event, commune_code,
	    reporter_kind, observed_at, species_code, disease_code, household_ref, locality, location, gps_accuracy_m, affected, dead,
	    description, attachment_refs, outbreak_ref, status_reason, captured_at, event_time, actor_ref, on_behalf_of,
	    support_reason, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::text, $7::text, $8::timestamptz, $9::text, $10::text,
	    $11::uuid, $12::text,
	    CASE WHEN $13::text IS NULL THEN NULL ELSE ST_SetSRID(ST_MakePoint($14::text::float8, $13::text::float8), 4326) END,
	    $15::numeric, $16::int, $17::int, $18::text, $19::text[]::uuid[], $20::uuid, $21::text, $22::timestamptz, $23::timestamptz,
	    $24::uuid, $25::uuid, $26::text, $27::text, $28::text)
	  RETURNING seq, recorded_at`,
		r.ID, tid, "suspect:"+r.Ref.String(), r.Ref, r.Event, r.CommuneCode, r.ReporterKind, r.ObservedAt.UTC(),
		optText(r.SpeciesCode), optText(r.DiseaseCode), r.HouseholdRef, optText(r.Locality), lat, lon, decText(r.GPSAccuracyM),
		r.Affected, r.Dead, r.Description, refs, r.OutbreakRef, r.StatusReason, r.CapturedAt, r.EventTime.UTC(), r.ActorRef,
		r.OnBehalfOf, r.SupportReason, r.IdemKey, domain.RuleVersion).Scan(&r.Seq, &r.RecordedAt)
}

// SuspectFilter lọc danh sách tin báo (bản hiện hành).
type SuspectFilter struct {
	Communes []string
	Status   string
}

// SuspectsCurrent trả bản hiện hành của mọi tin báo thỏa bộ lọc, mới trước.
func SuspectsCurrent(ctx context.Context, tx pgx.Tx, f SuspectFilter) ([]SuspectRow, error) {
	return collectSuspects(tx.Query(ctx, `SELECT `+susCols+` FROM (
	    SELECT DISTINCT ON (suspect_ref) * FROM pf_extension.cnts_suspect ORDER BY suspect_ref, seq DESC) s
	  WHERE ($1::text[] IS NULL OR commune_code = ANY($1::text[])) AND ($2::text = '' OR event = $2::text)
	  ORDER BY observed_at DESC, suspect_ref`, f.Communes, f.Status))
}

// SuspectHistory trả mọi phiên bản của một tin báo, mới nhất trước.
func SuspectHistory(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]SuspectRow, error) {
	return collectSuspects(tx.Query(ctx, `SELECT `+susCols+` FROM pf_extension.cnts_suspect WHERE suspect_ref = $1::uuid
	  ORDER BY seq DESC`, ref))
}

// SuspectByIdem đọc phiên bản theo idem_key; không có → nil.
func SuspectByIdem(ctx context.Context, tx pgx.Tx, key string) (*SuspectRow, error) {
	r, err := scanSuspect(tx.QueryRow(ctx, `SELECT `+susCols+` FROM pf_extension.cnts_suspect WHERE idem_key = $1::text
	  ORDER BY recorded_at DESC, id DESC LIMIT 1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
