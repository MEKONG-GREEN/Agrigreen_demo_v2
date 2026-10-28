// Package store: SQL của nhật ký nuôi CNTS (W11.48) — lứa (pf_extension.cnts_diary_cycle), lần ghi (cnts_diary_entry — sổ
// chỉ ghi thêm qua shared.apply_ledger), tọa độ lúc ghi (cnts_diary_fix — bảng thường, không UPDATE) và ảnh đã tải
// (cnts_diary_photo) — trên pool vai extension_app, luôn trong giao dịch có ngữ cảnh tenant (RLS).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/domain"
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

const metaCols = `event_time, recorded_at, actor_ref, on_behalf_of, support_reason, idem_key`

func (m *Meta) dest() []any {
	return []any{&m.EventTime, &m.RecordedAt, &m.ActorRef, &m.OnBehalfOf, &m.SupportReason, &m.IdemKey}
}

// Lock tuần tự hóa ghi theo khóa nghiệp vụ (lứa, hộ).
func Lock(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.cntsdiary:' || $1::text, 0))", key)
	return err
}

func optText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func decText(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func parseDec(s *string) (*decimal.Decimal, error) {
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

func collect[T any](rows pgx.Rows, err error, scan func(pgx.Row) (T, error)) ([]T, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func one[T any](v T, err error) (*T, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ---------------------------------------------------------------------------
// Lứa
// ---------------------------------------------------------------------------

// Cycle là một phiên bản lứa.
type Cycle struct {
	Meta
	Ref          uuid.UUID
	Event        string
	HouseholdRef uuid.UUID
	SiteRef      uuid.UUID
	ProjectRef   uuid.UUID
	CommuneCode  string
	StatusReason string
	domain.Cycle
	domain.Accounting
}

const cycleCols = `id, seq, cycle_ref, event, household_ref, site_ref, project_ref, commune_code, form, breed, received_date,
  initial_count, initial_weight_g::text, age_days, supplies, vaccination_batch_ref, coalesce(note, ''), cost_seed::text,
  cost_feed::text, cost_depreciation::text, cost_transport::text, cost_vet::text, cost_probiotic::text, revenue_heads,
  revenue_output_kg::text, revenue_price::text, revenue_amount::text, coalesce(status_reason, ''), ` + metaCols

func scanCycle(row pgx.Row) (Cycle, error) {
	var c Cycle
	var iw, cs, cf, cd, ct, cv, cp, ok, pr, rv *string
	var supplies []byte
	dest := []any{&c.ID, &c.Seq, &c.Ref, &c.Event, &c.HouseholdRef, &c.SiteRef, &c.ProjectRef, &c.CommuneCode, &c.Form, &c.Breed,
		&c.ReceivedDate, &c.InitialCount, &iw, &c.AgeDays, &supplies, &c.VaccinationBatch, &c.Note, &cs, &cf, &cd, &ct, &cv, &cp,
		&c.RevenueHeads, &ok, &pr, &rv, &c.StatusReason}
	if err := row.Scan(append(dest, c.dest()...)...); err != nil {
		return c, err
	}
	if err := json.Unmarshal(supplies, &c.Supplies); err != nil {
		return c, err
	}
	var err error
	for _, p := range []struct {
		s *string
		d **decimal.Decimal
	}{{iw, &c.InitialWeightG}, {cs, &c.CostSeed}, {cf, &c.CostFeed}, {cd, &c.CostDepreciation}, {ct, &c.CostTransport},
		{cv, &c.CostVet}, {cp, &c.CostProbiotic}, {ok, &c.OutputKg}, {pr, &c.Price}, {rv, &c.Revenue}} {
		if *p.d, err = parseDec(p.s); err != nil {
			return c, err
		}
	}
	return c, nil
}

// InsertCycle ghi một phiên bản lứa.
func InsertCycle(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c *Cycle) error {
	supplies := c.Supplies
	if supplies == nil {
		supplies = []domain.Supply{}
	}
	sj, err := json.Marshal(supplies)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_diary_cycle (id, tenant_id, stream_key, cycle_ref, event, household_ref,
	    site_ref, project_ref, commune_code, form, breed, received_date, initial_count, initial_weight_g, age_days, supplies,
	    vaccination_batch_ref, note, cost_seed, cost_feed, cost_depreciation, cost_transport, cost_vet, cost_probiotic, revenue_heads,
	    revenue_output_kg, revenue_price, revenue_amount, status_reason, event_time, actor_ref, on_behalf_of, support_reason, idem_key,
	    rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::date, $13, $14::numeric, $15, $16::jsonb, $17, $18, $19::numeric,
	    $20::numeric, $21::numeric, $22::numeric, $23::numeric, $24::numeric, $25, $26::numeric, $27::numeric, $28::numeric, $29, $30,
	    $31, $32, $33, $34, $35)
	  RETURNING seq, recorded_at`,
		c.ID, tid, "dcycle:"+c.Ref.String(), c.Ref, c.Event, c.HouseholdRef, c.SiteRef, c.ProjectRef, c.CommuneCode, c.Form, c.Breed,
		c.ReceivedDate.Format(time.DateOnly), c.InitialCount, decText(c.InitialWeightG), c.AgeDays, string(sj), c.VaccinationBatch,
		optText(c.Note), decText(c.CostSeed), decText(c.CostFeed), decText(c.CostDepreciation), decText(c.CostTransport),
		decText(c.CostVet), decText(c.CostProbiotic), c.RevenueHeads, decText(c.OutputKg), decText(c.Price), decText(c.Revenue),
		optText(c.StatusReason), c.EventTime.UTC(), c.ActorRef, c.OnBehalfOf, c.SupportReason, c.IdemKey, domain.RuleVersion).
		Scan(&c.Seq, &c.RecordedAt)
}

// LatestCycle trả bản hiện hành của lứa (nil = không có).
func LatestCycle(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Cycle, error) {
	return one(scanCycle(tx.QueryRow(ctx, "SELECT "+cycleCols+` FROM pf_extension.cnts_diary_cycle
	  WHERE cycle_ref = $1 ORDER BY seq DESC LIMIT 1`, ref)))
}

// CycleByIdem trả phiên bản đã ghi với khóa idem.
func CycleByIdem(ctx context.Context, tx pgx.Tx, key string) (*Cycle, error) {
	return one(scanCycle(tx.QueryRow(ctx, "SELECT "+cycleCols+" FROM pf_extension.cnts_diary_cycle WHERE idem_key = $1 LIMIT 1", key)))
}

// CycleVersions trả mọi phiên bản (cũ trước).
func CycleVersions(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Cycle, error) {
	rows, err := tx.Query(ctx, "SELECT "+cycleCols+" FROM pf_extension.cnts_diary_cycle WHERE cycle_ref = $1 ORDER BY seq", ref)
	return collect(rows, err, scanCycle)
}

// CycleFilter lọc lứa (bản hiện hành). Trường nil/rỗng = không lọc.
type CycleFilter struct {
	Household *uuid.UUID
	Site      *uuid.UUID
	Project   *uuid.UUID
}

// ListCycles trả bản hiện hành của các lứa.
func ListCycles(ctx context.Context, tx pgx.Tx, f CycleFilter) ([]Cycle, error) {
	rows, err := tx.Query(ctx, "SELECT "+cycleCols+` FROM (
	    SELECT DISTINCT ON (cycle_ref) * FROM pf_extension.cnts_diary_cycle
	    WHERE ($1::uuid IS NULL OR household_ref = $1) AND ($2::uuid IS NULL OR site_ref = $2) AND ($3::uuid IS NULL OR project_ref = $3)
	    ORDER BY cycle_ref, seq DESC) c ORDER BY received_date DESC, cycle_ref`, f.Household, f.Site, f.Project)
	return collect(rows, err, scanCycle)
}

// ---------------------------------------------------------------------------
// Lần ghi
// ---------------------------------------------------------------------------

// Entry là một phiên bản lần ghi.
type Entry struct {
	Meta
	Ref          uuid.UUID
	Event        string
	Status       string
	CycleRef     uuid.UUID
	HouseholdRef uuid.UUID
	SiteRef      uuid.UUID
	ProjectRef   uuid.UUID
	CommuneCode  string
	Form         string
	CapturedAt   time.Time
	Photos       []uuid.UUID
	Checks       []domain.Check
	Location     domain.Location
	FixStored    bool
	AuthorRef    uuid.UUID
	ReviewerRef  *uuid.UUID
	StatusReason string
	domain.Entry
}

const entryCols = `id, seq, entry_ref, event, status, cycle_ref, household_ref, site_ref, project_ref, commune_code, form, entry_kind,
  entry_date, captured_at, metrics, tables, photo_refs::text[], checks, fix_accuracy_m::text, distance_m::text, has_fix, fix_stored,
  no_target_point, far, weak_accuracy, mock_location, mock_suspected, author_ref, reviewer_ref, coalesce(status_reason, ''), ` + metaCols

func scanEntry(row pgx.Row) (Entry, error) {
	var e Entry
	var metrics, tables, checks []byte
	var photos []string
	var acc, dist *string
	dest := []any{&e.ID, &e.Seq, &e.Ref, &e.Event, &e.Status, &e.CycleRef, &e.HouseholdRef, &e.SiteRef, &e.ProjectRef, &e.CommuneCode,
		&e.Form, &e.Kind, &e.EntryDate, &e.CapturedAt, &metrics, &tables, &photos, &checks, &acc, &dist, &e.Location.HasFix, &e.FixStored,
		&e.Location.NoTargetPoint, &e.Location.Far, &e.Location.WeakAccuracy, &e.Location.MockDevice, &e.Location.MockSuspected,
		&e.AuthorRef, &e.ReviewerRef, &e.StatusReason}
	if err := row.Scan(append(dest, e.dest()...)...); err != nil {
		return e, err
	}
	if err := json.Unmarshal(metrics, &e.Metrics); err != nil {
		return e, err
	}
	if err := json.Unmarshal(tables, &e.Tables); err != nil {
		return e, err
	}
	if err := json.Unmarshal(checks, &e.Checks); err != nil {
		return e, err
	}
	for _, s := range photos {
		r, err := uuid.Parse(s)
		if err != nil {
			return e, err
		}
		e.Photos = append(e.Photos, r)
	}
	var err error
	if e.Location.AccuracyM, err = parseDec(acc); err != nil {
		return e, err
	}
	e.Location.DistanceM, err = parseDec(dist)
	return e, err
}

// InsertEntry ghi một phiên bản lần ghi.
func InsertEntry(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *Entry) error {
	metrics := e.Metrics
	if metrics == nil {
		metrics = map[string]string{}
	}
	checks := e.Checks
	if checks == nil {
		checks = []domain.Check{}
	}
	mj, err := json.Marshal(metrics)
	if err != nil {
		return err
	}
	tj, err := json.Marshal(e.Tables)
	if err != nil {
		return err
	}
	cj, err := json.Marshal(checks)
	if err != nil {
		return err
	}
	l := e.Location
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_diary_entry (id, tenant_id, stream_key, entry_ref, event, status, cycle_ref,
	    household_ref, site_ref, project_ref, commune_code, form, entry_kind, entry_date, captured_at, metrics, tables, photo_refs, checks,
	    fix_accuracy_m, distance_m, has_fix, fix_stored, no_target_point, far, weak_accuracy, mock_location, mock_suspected, author_ref,
	    reviewer_ref, status_reason, event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::date, $15, $16::jsonb, $17::jsonb, $18::text[]::uuid[],
	    $19::jsonb, $20::numeric, $21::numeric, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37)
	  RETURNING seq, recorded_at`,
		e.ID, tid, "dentry:"+e.Ref.String(), e.Ref, e.Event, e.Status, e.CycleRef, e.HouseholdRef, e.SiteRef, e.ProjectRef, e.CommuneCode,
		e.Form, e.Kind, e.EntryDate.Format(time.DateOnly), e.CapturedAt.UTC(), string(mj), string(tj), refStrings(e.Photos), string(cj),
		decText(l.AccuracyM), decText(l.DistanceM), l.HasFix, e.FixStored, l.NoTargetPoint, l.Far, l.WeakAccuracy, l.MockDevice,
		l.MockSuspected, e.AuthorRef, e.ReviewerRef, optText(e.StatusReason), e.EventTime.UTC(), e.ActorRef, e.OnBehalfOf,
		e.SupportReason, e.IdemKey, domain.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// LatestEntry trả bản hiện hành của lần ghi (nil = không có).
func LatestEntry(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Entry, error) {
	return one(scanEntry(tx.QueryRow(ctx, "SELECT "+entryCols+` FROM pf_extension.cnts_diary_entry
	  WHERE entry_ref = $1 ORDER BY seq DESC LIMIT 1`, ref)))
}

// EntryByIdem trả phiên bản đã ghi với khóa idem.
func EntryByIdem(ctx context.Context, tx pgx.Tx, key string) (*Entry, error) {
	return one(scanEntry(tx.QueryRow(ctx, "SELECT "+entryCols+" FROM pf_extension.cnts_diary_entry WHERE idem_key = $1 LIMIT 1", key)))
}

// EntryVersions trả mọi phiên bản (cũ trước).
func EntryVersions(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Entry, error) {
	rows, err := tx.Query(ctx, "SELECT "+entryCols+" FROM pf_extension.cnts_diary_entry WHERE entry_ref = $1 ORDER BY seq", ref)
	return collect(rows, err, scanEntry)
}

// EntryFilter lọc lần ghi (bản hiện hành). Trường nil/rỗng = không lọc.
type EntryFilter struct {
	Cycle     *uuid.UUID
	Household *uuid.UUID
	Project   *uuid.UUID
	Statuses  []string
	Limit     int
}

// ListEntries trả bản hiện hành của các lần ghi (mới trước).
func ListEntries(ctx context.Context, tx pgx.Tx, f EntryFilter) ([]Entry, error) {
	var st []string
	if len(f.Statuses) > 0 {
		st = f.Statuses
	}
	limit := f.Limit
	if limit <= 0 || limit > 2000 {
		limit = 2000
	}
	rows, err := tx.Query(ctx, "SELECT "+entryCols+` FROM (
	    SELECT DISTINCT ON (entry_ref) * FROM pf_extension.cnts_diary_entry
	    WHERE ($1::uuid IS NULL OR cycle_ref = $1) AND ($2::uuid IS NULL OR household_ref = $2) AND ($3::uuid IS NULL OR project_ref = $3)
	    ORDER BY entry_ref, seq DESC) e
	  WHERE ($4::text[] IS NULL OR status = ANY ($4::text[]))
	  ORDER BY entry_date DESC, captured_at DESC LIMIT $5`, f.Cycle, f.Household, f.Project, st, limit)
	return collect(rows, err, scanEntry)
}

// ---------------------------------------------------------------------------
// Tọa độ lúc ghi (bảng thường)
// ---------------------------------------------------------------------------

// FixRow là tọa độ lúc ghi của một lần ghi.
type FixRow struct {
	EntryRef   uuid.UUID
	Lat, Lng   float64
	AccuracyM  *decimal.Decimal
	CapturedAt time.Time
}

// InsertFix ghi tọa độ lúc ghi (một lần; trùng = bỏ qua — không sửa được).
func InsertFix(ctx context.Context, tx pgx.Tx, tid, entry, household uuid.UUID, f domain.Fix, at time.Time, purgeAfter time.Time, actor uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_diary_fix (tenant_id, entry_ref, household_ref, point, accuracy_m, captured_at,
	    purge_after, actor_ref)
	  VALUES ($1, $2, $3, ST_SetSRID(ST_MakePoint($4, $5), 4326), $6::numeric, $7, $8::date, $9) ON CONFLICT DO NOTHING`,
		tid, entry, household, f.Lng, f.Lat, decText(f.AccuracyM), at.UTC(), purgeAfter.Format(time.DateOnly), actor)
	return err
}

// FixOf trả tọa độ lúc ghi của lần ghi (nil = không có / đã xóa theo hạn lưu).
func FixOf(ctx context.Context, tx pgx.Tx, entry uuid.UUID) (*FixRow, error) {
	var f FixRow
	var acc *string
	err := tx.QueryRow(ctx, `SELECT entry_ref, ST_Y(point), ST_X(point), accuracy_m::text, captured_at FROM pf_extension.cnts_diary_fix
	  WHERE entry_ref = $1`, entry).Scan(&f.EntryRef, &f.Lat, &f.Lng, &acc, &f.CapturedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.AccuracyM, err = parseDec(acc)
	return &f, err
}

// HouseholdFixes trả tọa độ các lần ghi của hộ (trừ lần ghi except) — dò tọa độ trùng y hệt (nghi giả lập).
func HouseholdFixes(ctx context.Context, tx pgx.Tx, household, except uuid.UUID) ([]domain.Point, error) {
	rows, err := tx.Query(ctx, `SELECT ST_Y(point), ST_X(point) FROM pf_extension.cnts_diary_fix
	  WHERE household_ref = $1 AND entry_ref <> $2 ORDER BY captured_at DESC LIMIT 500`, household, except)
	return collect(rows, err, func(r pgx.Row) (domain.Point, error) {
		var p domain.Point
		return p, r.Scan(&p.Lat, &p.Lng)
	})
}

// PurgeFixes gọi pf_extension.purge_diary_fixes: xóa một lô tọa độ quá hạn lưu (purge_after < today) của tenant hiện hành.
func PurgeFixes(ctx context.Context, tx pgx.Tx, today time.Time, limit int) (int, error) {
	var n int
	err := tx.QueryRow(ctx, "SELECT pf_extension.purge_diary_fixes($1::date, $2::int)", today.Format(time.DateOnly), limit).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------------------
// Ảnh
// ---------------------------------------------------------------------------

// Photo là ảnh nhật ký đã tải (bản mã ở pf/evidence).
type Photo struct {
	FileRef      uuid.UUID
	HouseholdRef uuid.UUID
	Kind         string
	ContentType  string
	Size         int
	CapturedAt   *time.Time
	UploadedBy   uuid.UUID
	UploadedAt   time.Time
}

const photoCols = `file_ref, household_ref, kind, content_type, size_bytes, captured_at, uploaded_by, uploaded_at`

func scanPhoto(row pgx.Row) (Photo, error) {
	var p Photo
	return p, row.Scan(&p.FileRef, &p.HouseholdRef, &p.Kind, &p.ContentType, &p.Size, &p.CapturedAt, &p.UploadedBy, &p.UploadedAt)
}

// InsertPhoto ghi ảnh đã tải (trùng mã tệp = bỏ qua).
func InsertPhoto(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p Photo) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_diary_photo (tenant_id, file_ref, household_ref, kind, content_type, size_bytes,
	    captured_at, uploaded_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT DO NOTHING`,
		tid, p.FileRef, p.HouseholdRef, p.Kind, p.ContentType, p.Size, p.CapturedAt, p.UploadedBy)
	return err
}

// PhotoByRef trả ảnh (nil = không có trong tenant).
func PhotoByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Photo, error) {
	return one(scanPhoto(tx.QueryRow(ctx, "SELECT "+photoCols+" FROM pf_extension.cnts_diary_photo WHERE file_ref = $1", ref)))
}

// Photos trả ảnh theo mã tệp.
func Photos(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) (map[uuid.UUID]Photo, error) {
	out := map[uuid.UUID]Photo{}
	if len(refs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, "SELECT "+photoCols+" FROM pf_extension.cnts_diary_photo WHERE file_ref = ANY ($1::text[]::uuid[])",
		refStrings(refs))
	ps, err := collect(rows, err, scanPhoto)
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		out[p.FileRef] = p
	}
	return out, nil
}
