// Package store: SQL của CT/DA, điểm trình diễn, hộ mô hình, cấp quyền CNTS (pf_extension.cnts_project, cnts_demo_site,
// cnts_model_household, cnts_grant — sổ chỉ ghi thêm qua shared.apply_ledger) và tọa độ (cnts_model_point, bảng thường)
// — W11.47 — trên pool vai extension_app, luôn trong giao dịch có ngữ cảnh tenant (RLS).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
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

// Lock tuần tự hóa ghi theo khóa nghiệp vụ.
func Lock(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.cntsmodel:' || $1::text, 0))", key)
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

func parseRefs(ss []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(ss))
	for _, s := range ss {
		r, err := uuid.Parse(s)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func dateText(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// collect đọc mọi dòng bằng scan.
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
// CT/DA
// ---------------------------------------------------------------------------

// Project là một phiên bản CT/DA.
type Project struct {
	Meta
	Ref          uuid.UUID
	Event        string
	Province     string
	StatusReason string
	domain.Project
}

const projectCols = `id, seq, project_ref, event, province_code, code, name, kind, coalesce(basis, ''), coalesce(scale_note, ''),
  start_date, end_date, budget::text, manager_ref, program_ref, coalesce(training_note, ''), coalesce(supplies_note, ''),
  attachment_refs::text[], coalesce(status_reason, ''), ` + metaCols

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	var budget *string
	var refs []string
	dest := []any{&p.ID, &p.Seq, &p.Ref, &p.Event, &p.Province, &p.Code, &p.Name, &p.Kind, &p.Basis, &p.ScaleNote, &p.Start, &p.End,
		&budget, &p.ManagerRef, &p.ProgramRef, &p.TrainingNote, &p.SuppliesNote, &refs, &p.StatusReason}
	if err := row.Scan(append(dest, p.dest()...)...); err != nil {
		return p, err
	}
	var err error
	if p.Budget, err = parseDec(budget); err != nil {
		return p, err
	}
	p.Attachments, err = parseRefs(refs)
	return p, err
}

// InsertProject ghi một phiên bản CT/DA; Seq do DB tính.
func InsertProject(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p *Project) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_project (id, tenant_id, stream_key, project_ref, event, province_code,
	    code, name, kind, basis, scale_note, start_date, end_date, budget, manager_ref, program_ref, training_note, supplies_note,
	    attachment_refs, status_reason, event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::date, $13::date, $14::numeric, $15, $16, $17, $18,
	    $19::text[]::uuid[], $20, $21, $22, $23, $24, $25, $26)
	  RETURNING seq, recorded_at`,
		p.ID, tid, "cproj:"+p.Ref.String(), p.Ref, p.Event, p.Province, p.Code, p.Name, p.Kind, optText(p.Basis),
		optText(p.ScaleNote), p.Start.Format(time.DateOnly), dateText(p.End), decText(p.Budget), p.ManagerRef, p.ProgramRef,
		optText(p.TrainingNote), optText(p.SuppliesNote), refStrings(p.Attachments), optText(p.StatusReason), p.EventTime.UTC(),
		p.ActorRef, p.OnBehalfOf, p.SupportReason, p.IdemKey, domain.RuleVersion).Scan(&p.Seq, &p.RecordedAt)
}

// LatestProject trả bản hiện hành (nil = không có).
func LatestProject(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Project, error) {
	return one(scanProject(tx.QueryRow(ctx, "SELECT "+projectCols+` FROM pf_extension.cnts_project
	  WHERE project_ref = $1 ORDER BY seq DESC LIMIT 1`, ref)))
}

// ProjectByIdem trả phiên bản đã ghi với khóa idem.
func ProjectByIdem(ctx context.Context, tx pgx.Tx, key string) (*Project, error) {
	return one(scanProject(tx.QueryRow(ctx, "SELECT "+projectCols+" FROM pf_extension.cnts_project WHERE idem_key = $1 LIMIT 1", key)))
}

// ProjectVersions trả mọi phiên bản (cũ trước).
func ProjectVersions(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Project, error) {
	rows, err := tx.Query(ctx, "SELECT "+projectCols+" FROM pf_extension.cnts_project WHERE project_ref = $1 ORDER BY seq", ref)
	return collect(rows, err, scanProject)
}

// ListProjects trả bản hiện hành của mọi CT/DA trong tenant (lọc phạm vi ở ứng dụng).
func ListProjects(ctx context.Context, tx pgx.Tx) ([]Project, error) {
	rows, err := tx.Query(ctx, "SELECT "+projectCols+` FROM (
	    SELECT DISTINCT ON (project_ref) * FROM pf_extension.cnts_project ORDER BY project_ref, seq DESC) p
	  ORDER BY start_date DESC, code`)
	return collect(rows, err, scanProject)
}

// ProjectCodeTaken cho biết mã đã thuộc CT/DA khác (bản hiện hành).
func ProjectCodeTaken(ctx context.Context, tx pgx.Tx, code string, except uuid.UUID) (bool, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM (
	    SELECT DISTINCT ON (project_ref) project_ref, code FROM pf_extension.cnts_project ORDER BY project_ref, seq DESC) p
	  WHERE p.code = $1 AND p.project_ref <> $2`, code, except).Scan(&n)
	return n > 0, err
}

// ---------------------------------------------------------------------------
// Điểm trình diễn
// ---------------------------------------------------------------------------

// Site là một phiên bản điểm.
type Site struct {
	Meta
	Ref          uuid.UUID
	Event        string
	ProjectRef   uuid.UUID
	StatusReason string
	domain.Site
}

const siteCols = `id, seq, site_ref, event, project_ref, commune_code, name, model_type_code, scale::text, coalesce(scale_unit, ''),
  officer_ref, coalesce(diary_form, ''), cooperative_ref, coalesce(note, ''), coalesce(status_reason, ''), ` + metaCols

func scanSite(row pgx.Row) (Site, error) {
	var s Site
	var scale *string
	dest := []any{&s.ID, &s.Seq, &s.Ref, &s.Event, &s.ProjectRef, &s.CommuneCode, &s.Name, &s.ModelType, &scale, &s.Scale.Unit,
		&s.OfficerRef, &s.DiaryForm, &s.CooperativeRef, &s.Note, &s.StatusReason}
	if err := row.Scan(append(dest, s.dest()...)...); err != nil {
		return s, err
	}
	var err error
	s.Scale.Value, err = parseDec(scale)
	return s, err
}

// InsertSite ghi một phiên bản điểm.
func InsertSite(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s *Site) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_demo_site (id, tenant_id, stream_key, site_ref, event, project_ref,
	    commune_code, name, model_type_code, scale, scale_unit, officer_ref, diary_form, cooperative_ref, note, status_reason,
	    event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::numeric, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
	  RETURNING seq, recorded_at`,
		s.ID, tid, "csite:"+s.Ref.String(), s.Ref, s.Event, s.ProjectRef, s.CommuneCode, s.Name, s.ModelType, decText(s.Scale.Value),
		optText(s.Scale.Unit), s.OfficerRef, optText(s.DiaryForm), s.CooperativeRef, optText(s.Note), optText(s.StatusReason),
		s.EventTime.UTC(), s.ActorRef, s.OnBehalfOf, s.SupportReason, s.IdemKey, domain.RuleVersion).Scan(&s.Seq, &s.RecordedAt)
}

// LatestSite trả bản hiện hành (nil = không có).
func LatestSite(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Site, error) {
	return one(scanSite(tx.QueryRow(ctx, "SELECT "+siteCols+` FROM pf_extension.cnts_demo_site
	  WHERE site_ref = $1 ORDER BY seq DESC LIMIT 1`, ref)))
}

// SiteByIdem trả phiên bản đã ghi với khóa idem.
func SiteByIdem(ctx context.Context, tx pgx.Tx, key string) (*Site, error) {
	return one(scanSite(tx.QueryRow(ctx, "SELECT "+siteCols+" FROM pf_extension.cnts_demo_site WHERE idem_key = $1 LIMIT 1", key)))
}

// SiteVersions trả mọi phiên bản (cũ trước).
func SiteVersions(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Site, error) {
	rows, err := tx.Query(ctx, "SELECT "+siteCols+" FROM pf_extension.cnts_demo_site WHERE site_ref = $1 ORDER BY seq", ref)
	return collect(rows, err, scanSite)
}

// SiteFilter lọc điểm (bản hiện hành). Project nil = mọi dự án; Communes nil = mọi xã.
type SiteFilter struct {
	Project  *uuid.UUID
	Communes []string
	Refs     []uuid.UUID // nil = mọi điểm
}

// ListSites trả bản hiện hành của các điểm theo bộ lọc.
func ListSites(ctx context.Context, tx pgx.Tx, f SiteFilter) ([]Site, error) {
	var refs []string
	if f.Refs != nil {
		refs = refStrings(f.Refs)
	}
	rows, err := tx.Query(ctx, "SELECT "+siteCols+` FROM (
	    SELECT DISTINCT ON (site_ref) * FROM pf_extension.cnts_demo_site
	    WHERE ($1::uuid IS NULL OR project_ref = $1) AND ($2::text[] IS NULL OR commune_code = ANY ($2::text[]))
	      AND ($3::text[] IS NULL OR site_ref = ANY ($3::text[]::uuid[]))
	    ORDER BY site_ref, seq DESC) s
	  ORDER BY commune_code, name, site_ref`, f.Project, f.Communes, refs)
	return collect(rows, err, scanSite)
}

// ---------------------------------------------------------------------------
// Hộ mô hình
// ---------------------------------------------------------------------------

// Household là một phiên bản hộ mô hình.
type Household struct {
	Meta
	Ref          uuid.UUID
	Event        string
	SiteRef      uuid.UUID
	ProjectRef   uuid.UUID
	CommuneCode  string
	PersonRef    uuid.UUID
	StatusReason string
	domain.Household
}

const householdCols = `id, seq, household_ref, event, site_ref, project_ref, commune_code, person_ref, scale::text,
  coalesce(scale_unit, ''), keeper_kind, keeper_ref, self_entry, joined_on, coalesce(note, ''), coalesce(status_reason, ''), ` + metaCols

func scanHousehold(row pgx.Row) (Household, error) {
	var h Household
	var scale *string
	dest := []any{&h.ID, &h.Seq, &h.Ref, &h.Event, &h.SiteRef, &h.ProjectRef, &h.CommuneCode, &h.PersonRef, &scale, &h.Scale.Unit,
		&h.KeeperKind, &h.KeeperRef, &h.SelfEntry, &h.JoinedOn, &h.Note, &h.StatusReason}
	if err := row.Scan(append(dest, h.dest()...)...); err != nil {
		return h, err
	}
	var err error
	h.Scale.Value, err = parseDec(scale)
	return h, err
}

// InsertHousehold ghi một phiên bản hộ.
func InsertHousehold(ctx context.Context, tx pgx.Tx, tid uuid.UUID, h *Household) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_model_household (id, tenant_id, stream_key, household_ref, event,
	    site_ref, project_ref, commune_code, person_ref, scale, scale_unit, keeper_kind, keeper_ref, self_entry, joined_on, note,
	    status_reason, event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::numeric, $11, $12, $13, $14, $15::date, $16, $17, $18, $19, $20, $21, $22, $23)
	  RETURNING seq, recorded_at`,
		h.ID, tid, "chh:"+h.Ref.String(), h.Ref, h.Event, h.SiteRef, h.ProjectRef, h.CommuneCode, h.PersonRef, decText(h.Scale.Value),
		optText(h.Scale.Unit), h.KeeperKind, h.KeeperRef, h.SelfEntry, dateText(h.JoinedOn), optText(h.Note), optText(h.StatusReason),
		h.EventTime.UTC(), h.ActorRef, h.OnBehalfOf, h.SupportReason, h.IdemKey, domain.RuleVersion).Scan(&h.Seq, &h.RecordedAt)
}

// LatestHousehold trả bản hiện hành (nil = không có).
func LatestHousehold(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Household, error) {
	return one(scanHousehold(tx.QueryRow(ctx, "SELECT "+householdCols+` FROM pf_extension.cnts_model_household
	  WHERE household_ref = $1 ORDER BY seq DESC LIMIT 1`, ref)))
}

// HouseholdByIdem trả phiên bản đã ghi với khóa idem.
func HouseholdByIdem(ctx context.Context, tx pgx.Tx, key string) (*Household, error) {
	return one(scanHousehold(tx.QueryRow(ctx, "SELECT "+householdCols+" FROM pf_extension.cnts_model_household WHERE idem_key = $1 LIMIT 1", key)))
}

// HouseholdVersions trả mọi phiên bản (cũ trước).
func HouseholdVersions(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Household, error) {
	rows, err := tx.Query(ctx, "SELECT "+householdCols+" FROM pf_extension.cnts_model_household WHERE household_ref = $1 ORDER BY seq", ref)
	return collect(rows, err, scanHousehold)
}

// HouseholdFilter lọc hộ (bản hiện hành). Trường nil = không lọc.
type HouseholdFilter struct {
	Site     *uuid.UUID
	Project  *uuid.UUID
	Communes []string
}

// ListHouseholds trả bản hiện hành của các hộ theo bộ lọc.
func ListHouseholds(ctx context.Context, tx pgx.Tx, f HouseholdFilter) ([]Household, error) {
	rows, err := tx.Query(ctx, "SELECT "+householdCols+` FROM (
	    SELECT DISTINCT ON (household_ref) * FROM pf_extension.cnts_model_household
	    WHERE ($1::uuid IS NULL OR site_ref = $1) AND ($2::uuid IS NULL OR project_ref = $2)
	      AND ($3::text[] IS NULL OR commune_code = ANY ($3::text[]))
	    ORDER BY household_ref, seq DESC) h
	  ORDER BY commune_code, site_ref, joined_on NULLS LAST, household_ref`, f.Site, f.Project, f.Communes)
	return collect(rows, err, scanHousehold)
}

// CoveredHouseholds trả bản hiện hành của các hộ có mã trong refs HOẶC thuộc điểm trong sites (phạm vi quyền theo dõi
// mô hình của phiên — #71). Hai danh sách rỗng → không truy vấn.
func CoveredHouseholds(ctx context.Context, tx pgx.Tx, refs, sites []uuid.UUID) ([]Household, error) {
	if len(refs) == 0 && len(sites) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, "SELECT "+householdCols+` FROM (
	    SELECT DISTINCT ON (household_ref) * FROM pf_extension.cnts_model_household
	    WHERE household_ref IN (SELECT household_ref FROM pf_extension.cnts_model_household
	      WHERE household_ref = ANY ($1::text[]::uuid[]) OR site_ref = ANY ($2::text[]::uuid[]))
	    ORDER BY household_ref, seq DESC) h
	  WHERE h.household_ref = ANY ($1::text[]::uuid[]) OR h.site_ref = ANY ($2::text[]::uuid[])
	  ORDER BY household_ref`, refStrings(refs), refStrings(sites))
	return collect(rows, err, scanHousehold)
}

// ---------------------------------------------------------------------------
// Cấp quyền
// ---------------------------------------------------------------------------

// Grant là một phiên bản cấp quyền.
type Grant struct {
	Meta
	Ref               uuid.UUID
	Event             string
	GranteeRef        uuid.UUID
	Province          string
	SuccessorRef      *uuid.UUID
	SuccessorGrantRef *uuid.UUID
	Reason            string
	domain.Grant
}

const grantCols = `id, seq, grant_ref, event, grantee_ref, role, province_code, communes, facility_refs::text[], site_refs::text[],
  household_refs::text[], forms, valid_from, valid_to, successor_ref, successor_grant_ref, coalesce(reason, ''), ` + metaCols

func scanGrant(row pgx.Row) (Grant, error) {
	var g Grant
	var fac, site, hh []string
	dest := []any{&g.ID, &g.Seq, &g.Ref, &g.Event, &g.GranteeRef, &g.Role, &g.Province, &g.Communes, &fac, &site, &hh, &g.Forms,
		&g.ValidFrom, &g.ValidTo, &g.SuccessorRef, &g.SuccessorGrantRef, &g.Reason}
	if err := row.Scan(append(dest, g.dest()...)...); err != nil {
		return g, err
	}
	var err error
	if g.Facilities, err = parseRefs(fac); err != nil {
		return g, err
	}
	if g.Sites, err = parseRefs(site); err != nil {
		return g, err
	}
	g.Households, err = parseRefs(hh)
	return g, err
}

// InsertGrant ghi một phiên bản cấp quyền.
func InsertGrant(ctx context.Context, tx pgx.Tx, tid uuid.UUID, g *Grant) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_grant (id, tenant_id, stream_key, grant_ref, event, grantee_ref, role,
	    province_code, communes, facility_refs, site_refs, household_refs, forms, valid_from, valid_to, successor_ref,
	    successor_grant_ref, reason, event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::text[], $10::text[]::uuid[], $11::text[]::uuid[], $12::text[]::uuid[], $13::text[],
	    $14::date, $15::date, $16, $17, $18, $19, $20, $21, $22, $23, $24)
	  RETURNING seq, recorded_at`,
		g.ID, tid, "cgrant:"+g.Ref.String(), g.Ref, g.Event, g.GranteeRef, g.Role, g.Province, nonNil(g.Communes),
		refStrings(g.Facilities), refStrings(g.Sites), refStrings(g.Households), nonNil(g.Forms), g.ValidFrom.Format(time.DateOnly),
		dateText(g.ValidTo), g.SuccessorRef, g.SuccessorGrantRef, optText(g.Reason), g.EventTime.UTC(), g.ActorRef, g.OnBehalfOf,
		g.SupportReason, g.IdemKey, domain.RuleVersion).Scan(&g.Seq, &g.RecordedAt)
}

// LatestGrant trả bản hiện hành (nil = không có).
func LatestGrant(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Grant, error) {
	return one(scanGrant(tx.QueryRow(ctx, "SELECT "+grantCols+` FROM pf_extension.cnts_grant
	  WHERE grant_ref = $1 ORDER BY seq DESC LIMIT 1`, ref)))
}

// GrantByIdem trả phiên bản đã ghi với khóa idem.
func GrantByIdem(ctx context.Context, tx pgx.Tx, key string) (*Grant, error) {
	return one(scanGrant(tx.QueryRow(ctx, "SELECT "+grantCols+" FROM pf_extension.cnts_grant WHERE idem_key = $1 LIMIT 1", key)))
}

// GrantVersions trả mọi phiên bản (cũ trước) — nhật ký thay đổi quyền.
func GrantVersions(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Grant, error) {
	rows, err := tx.Query(ctx, "SELECT "+grantCols+" FROM pf_extension.cnts_grant WHERE grant_ref = $1 ORDER BY seq", ref)
	return collect(rows, err, scanGrant)
}

// GranteeEverGranted cho biết grantee đã TỪNG có quyền CNTS (vai khác nông hộ) ở BẤT KỲ phiên bản nào trong tenant — kể
// cả quyền đã sửa thu hẹp, thu hồi, hết hạn (#71: quyết định phạm vi tường minh của phiên, chỉ đọc).
func GranteeEverGranted(ctx context.Context, tx pgx.Tx, grantee uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_extension.cnts_grant WHERE grantee_ref = $1 AND role <> $2)`,
		grantee, domain.RoleFarmer).Scan(&ok)
	return ok, err
}

// ListGrants trả bản hiện hành của mọi quyền; grantee khác nil → chỉ của người đó.
func ListGrants(ctx context.Context, tx pgx.Tx, grantee *uuid.UUID) ([]Grant, error) {
	rows, err := tx.Query(ctx, "SELECT "+grantCols+` FROM (
	    SELECT DISTINCT ON (grant_ref) * FROM pf_extension.cnts_grant WHERE ($1::uuid IS NULL OR grantee_ref = $1)
	    ORDER BY grant_ref, seq DESC) g
	  ORDER BY valid_from DESC, grant_ref`, grantee)
	return collect(rows, err, scanGrant)
}

// ---------------------------------------------------------------------------
// Tọa độ (bảng thường)
// ---------------------------------------------------------------------------

// Point kinds.
const (
	PointSite      = "site"
	PointHousehold = "household"
)

// SetPoint ghi (hoặc thay) tọa độ của đối tượng.
func SetPoint(ctx context.Context, tx pgx.Tx, tid uuid.UUID, kind string, ref uuid.UUID, p domain.Point, actor uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_model_point (tenant_id, kind, ref, point, source, actor_ref)
	  VALUES ($1, $2, $3, ST_SetSRID(ST_MakePoint($4::float8, $5::float8), 4326), $6, $7)
	  ON CONFLICT (tenant_id, kind, ref) DO UPDATE SET point = EXCLUDED.point, source = EXCLUDED.source, actor_ref = EXCLUDED.actor_ref,
	    updated_at = now()`, tid, kind, ref, p.Lng, p.Lat, p.Source, actor)
	return err
}

// ClearPoint xóa tọa độ của đối tượng — bảng thường cnts_model_point (QĐ-8: vị trí hộ không nằm trong sổ không xóa
// được); dữ liệu nghiệp vụ ở các sổ không bị xóa.
func ClearPoint(ctx context.Context, tx pgx.Tx, tid uuid.UUID, kind string, ref uuid.UUID) error {
	_, err := tx.Exec(ctx, `DELETE FROM pf_extension.cnts_model_point WHERE tenant_id = $1 AND kind = $2 AND ref = $3`, tid, kind, ref)
	return err
}

// Points trả tọa độ của các đối tượng cùng loại.
func Points(ctx context.Context, tx pgx.Tx, kind string, refs []uuid.UUID) (map[uuid.UUID]domain.Point, error) {
	out := map[uuid.UUID]domain.Point{}
	if len(refs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT ref, ST_Y(point), ST_X(point), source FROM pf_extension.cnts_model_point
	  WHERE kind = $1 AND ref = ANY ($2::text[]::uuid[])`, kind, refStrings(refs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r uuid.UUID
		var p domain.Point
		if err := rows.Scan(&r, &p.Lat, &p.Lng, &p.Source); err != nil {
			return nil, err
		}
		out[r] = p
	}
	return out, rows.Err()
}

// CooperativeExists cho biết HTX org_ref có trong công bố pf_cooperative_pub.v1_cooperatives (chỉ đọc, ADR-0055).
func CooperativeExists(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_cooperative_pub.v1_cooperatives WHERE org_ref = $1)`, ref).Scan(&ok)
	return ok, err
}
