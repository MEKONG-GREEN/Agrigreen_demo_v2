package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
)

// Template là một mẫu SOP.
type Template struct {
	Ref               uuid.UUID
	Code              string
	Name              string
	NameKey           string
	Crop              string
	Scope             string
	CooperativeRef    uuid.UUID
	Standard          string
	Description       string
	Status            string
	StatusReason      string
	SourceTemplateRef uuid.UUID
	LatestVersion     int
	PublishedVersion  *int
	CreatedBy         uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

const templateCols = `ref, code, name, name_key, crop, scope, cooperative_ref, coalesce(standard, ''), coalesce(description, ''),
  status, coalesce(status_reason, ''), source_template_ref, latest_version, published_version, created_by, created_at, updated_at`

func scanTemplate(row pgx.Row) (Template, error) {
	var t Template
	var coop, src *uuid.UUID
	err := row.Scan(&t.Ref, &t.Code, &t.Name, &t.NameKey, &t.Crop, &t.Scope, &coop, &t.Standard, &t.Description,
		&t.Status, &t.StatusReason, &src, &t.LatestVersion, &t.PublishedVersion, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	t.CooperativeRef, t.SourceTemplateRef = deref(coop), deref(src)
	return t, err
}

// InsertTemplate ghi mẫu mới.
func InsertTemplate(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t Template) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_cropcycle.sop_template
	    (tenant_id, ref, code, name, name_key, crop, scope, cooperative_ref, standard, description, status,
	     source_template_ref, latest_version, created_by)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::text, $5::text, $6::text, $7::text, $8::uuid, $9::text, $10::text, $11::text,
	     $12::uuid, $13::int, $14::uuid)`,
		tid, t.Ref, t.Code, t.Name, t.NameKey, t.Crop, t.Scope, nilIfZero(t.CooperativeRef), nilIfEmpty(t.Standard),
		nilIfEmpty(t.Description), t.Status, nilIfZero(t.SourceTemplateRef), t.LatestVersion, t.CreatedBy)
	return err
}

// TemplateByRef đọc mẫu; forUpdate khóa dòng.
func TemplateByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID, forUpdate bool) (*Template, error) {
	q := "SELECT " + templateCols + " FROM pf_cropcycle.sop_template WHERE ref = $1::uuid"
	if forUpdate {
		q += " FOR UPDATE"
	}
	t, err := scanTemplate(tx.QueryRow(ctx, q, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TemplateTaken cho biết mã hoặc tên (chuẩn hóa) đã được dùng.
func TemplateTaken(ctx context.Context, tx pgx.Tx, code, nameKey string) (codeTaken, nameTaken bool, err error) {
	err = tx.QueryRow(ctx, `SELECT
	    EXISTS (SELECT 1 FROM pf_cropcycle.sop_template WHERE code = $1::text),
	    EXISTS (SELECT 1 FROM pf_cropcycle.sop_template WHERE name_key = $2::text)`, code, nameKey).Scan(&codeTaken, &nameTaken)
	return
}

// TemplateFilter lọc danh sách mẫu.
type TemplateFilter struct {
	Status         string
	CooperativeRef uuid.UUID // khác Nil: mẫu hệ thống + mẫu riêng của HTX này
	OnlyVisibleTo  bool      // true: áp CooperativeRef như trên; false: mọi mẫu
	After          uuid.UUID
	Limit          int
}

// ListTemplates liệt kê mẫu theo ref (cursor).
func ListTemplates(ctx context.Context, tx pgx.Tx, f TemplateFilter) ([]Template, error) {
	rows, err := tx.Query(ctx, "SELECT "+templateCols+` FROM pf_cropcycle.sop_template
	  WHERE ($1::text = '' OR status = $1::text)
	    AND (NOT $2::boolean OR scope = 'system' OR cooperative_ref = $3::uuid)
	    AND ref > $4::uuid
	  ORDER BY ref LIMIT $5::int`, f.Status, f.OnlyVisibleTo, f.CooperativeRef, f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetTemplateState đổi trạng thái / phiên bản mới nhất / đã phát hành của mẫu.
func SetTemplateState(ctx context.Context, tx pgx.Tx, t Template) error {
	_, err := tx.Exec(ctx, `UPDATE pf_cropcycle.sop_template
	  SET status = $2::text, status_reason = $3::text, latest_version = $4::int, published_version = $5::int, updated_at = now()
	  WHERE ref = $1::uuid`, t.Ref, t.Status, nilIfEmpty(t.StatusReason), t.LatestVersion, t.PublishedVersion)
	return err
}

// Version là một phiên bản SOP.
type Version struct {
	ID                uuid.UUID
	TemplateRef       uuid.UUID
	VersionNo         int
	Status            string
	Steps             []domain.Step
	HarvestDayOffset  *int
	HarvestWindowDays *int
	SourceVersionRef  uuid.UUID
	ChangeNote        string
	AuthoredBy        uuid.UUID
	AuthoredAt        time.Time
	UpdatedAt         time.Time
	PublishedBy       uuid.UUID
	PublishedAt       *time.Time
	SyncSeq           int64
}

const versionCols = `id, template_ref, version_no, status, steps, harvest_day_offset, harvest_window_days, source_version_ref,
  coalesce(change_note, ''), authored_by, authored_at, updated_at, published_by, published_at, sync_seq`

func scanVersion(row pgx.Row) (Version, error) {
	var v Version
	var raw []byte
	var src, pub *uuid.UUID
	if err := row.Scan(&v.ID, &v.TemplateRef, &v.VersionNo, &v.Status, &raw, &v.HarvestDayOffset, &v.HarvestWindowDays, &src,
		&v.ChangeNote, &v.AuthoredBy, &v.AuthoredAt, &v.UpdatedAt, &pub, &v.PublishedAt, &v.SyncSeq); err != nil {
		return v, err
	}
	v.SourceVersionRef, v.PublishedBy = deref(src), deref(pub)
	if err := json.Unmarshal(raw, &v.Steps); err != nil {
		return v, err
	}
	return v, nil
}

func harvestOf(steps []domain.Step) (*int, *int) {
	h, ok := domain.HarvestStep(steps)
	if !ok {
		return nil, nil
	}
	off, win := h.DayOffset, h.WindowDays
	return &off, &win
}

// InsertVersion ghi phiên bản (nháp) mới.
func InsertVersion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v Version) error {
	raw, err := json.Marshal(stepsOrEmpty(v.Steps))
	if err != nil {
		return err
	}
	off, win := harvestOf(v.Steps)
	if err := lockSync(ctx, tx, "sop_version"); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_cropcycle.sop_version
	    (tenant_id, id, template_ref, version_no, status, steps, step_count, harvest_day_offset, harvest_window_days,
	     source_version_ref, change_note, authored_by, authored_at)
	  VALUES ($1::uuid, $2::uuid, $3::uuid, $4::int, $5::text, $6::jsonb, $7::int, $8::int, $9::int, $10::uuid, $11::text,
	     $12::uuid, $13::timestamptz)`,
		tid, v.ID, v.TemplateRef, v.VersionNo, v.Status, string(raw), len(v.Steps), off, win, nilIfZero(v.SourceVersionRef),
		nilIfEmpty(v.ChangeNote), v.AuthoredBy, v.AuthoredAt.UTC())
	return err
}

func stepsOrEmpty(s []domain.Step) []domain.Step {
	if s == nil {
		return []domain.Step{}
	}
	return s
}

// UpdateDraft thay bước và ghi chú của bản nháp (trigger chặn bản đã phát hành).
func UpdateDraft(ctx context.Context, tx pgx.Tx, v Version) error {
	raw, err := json.Marshal(stepsOrEmpty(v.Steps))
	if err != nil {
		return err
	}
	off, win := harvestOf(v.Steps)
	_, err = tx.Exec(ctx, `UPDATE pf_cropcycle.sop_version
	  SET steps = $2::jsonb, step_count = $3::int, harvest_day_offset = $4::int, harvest_window_days = $5::int,
	      change_note = $6::text, updated_at = now()
	  WHERE id = $1::uuid`, v.ID, string(raw), len(v.Steps), off, win, nilIfEmpty(v.ChangeNote))
	return err
}

// PublishVersion chuyển nháp sang đã phát hành (cấp sync_seq mới: đi xuống thiết bị).
func PublishVersion(ctx context.Context, tx pgx.Tx, id, by uuid.UUID, at time.Time) error {
	if err := lockSync(ctx, tx, "sop_version"); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE pf_cropcycle.sop_version
	  SET status = 'published', published_by = $2::uuid, published_at = $3::timestamptz, updated_at = now(),
	      sync_seq = nextval('pf_cropcycle.sop_sync_seq')
	  WHERE id = $1::uuid AND status = 'draft'`, id, by, at.UTC())
	return err
}

// VersionByID đọc phiên bản; forUpdate khóa dòng.
func VersionByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Version, error) {
	q := "SELECT " + versionCols + " FROM pf_cropcycle.sop_version WHERE id = $1::uuid"
	if forUpdate {
		q += " FOR UPDATE"
	}
	v, err := scanVersion(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// VersionsOf liệt kê mọi phiên bản của mẫu theo số phiên bản.
func VersionsOf(ctx context.Context, tx pgx.Tx, template uuid.UUID) ([]Version, error) {
	rows, err := tx.Query(ctx, "SELECT "+versionCols+" FROM pf_cropcycle.sop_version WHERE template_ref = $1::uuid ORDER BY version_no", template)
	if err != nil {
		return nil, err
	}
	return collectVersions(rows)
}

func collectVersions(rows pgx.Rows) ([]Version, error) {
	defer rows.Close()
	var out []Version
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// LatestPublished trả phiên bản đã phát hành mới nhất của mẫu.
func LatestPublished(ctx context.Context, tx pgx.Tx, template uuid.UUID) (*Version, error) {
	v, err := scanVersion(tx.QueryRow(ctx, "SELECT "+versionCols+` FROM pf_cropcycle.sop_version
	  WHERE template_ref = $1::uuid AND status = 'published' ORDER BY version_no DESC LIMIT 1`, template))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// PublishedVersionChange là một phiên bản đã phát hành kèm thông tin mẫu cho đồng bộ.
type PublishedVersionChange struct {
	Version
	Template Template
}

// PublishedVersionsAfter trả phiên bản đã phát hành có sync_seq > after (mẫu hệ
// thống, hoặc mẫu riêng của HTX trong coops; allCoops = mọi mẫu).
func PublishedVersionsAfter(ctx context.Context, tx pgx.Tx, allCoops bool, coops []uuid.UUID, after int64, limit int) ([]PublishedVersionChange, error) {
	rows, err := tx.Query(ctx, `SELECT v.id, v.template_ref, v.version_no, v.status, v.steps, v.harvest_day_offset, v.harvest_window_days,
	    v.source_version_ref, coalesce(v.change_note, ''), v.authored_by, v.authored_at, v.updated_at, v.published_by, v.published_at, v.sync_seq,
	    t.ref, t.code, t.name, t.name_key, t.crop, t.scope, t.cooperative_ref, coalesce(t.standard, ''), coalesce(t.description, ''),
	    t.status, coalesce(t.status_reason, ''), t.source_template_ref, t.latest_version, t.published_version, t.created_by, t.created_at, t.updated_at
	  FROM pf_cropcycle.sop_version v JOIN pf_cropcycle.sop_template t ON t.tenant_id = v.tenant_id AND t.ref = v.template_ref
	  WHERE v.status = 'published' AND v.sync_seq > $1::bigint
	    AND ($2::boolean OR t.scope = 'system' OR t.cooperative_ref = ANY($3::uuid[]))
	  ORDER BY v.sync_seq LIMIT $4::int`, after, allCoops, uuids(coops), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublishedVersionChange
	for rows.Next() {
		var c PublishedVersionChange
		var raw []byte
		var src, pub, coop, tsrc *uuid.UUID
		if err := rows.Scan(&c.ID, &c.TemplateRef, &c.VersionNo, &c.Status, &raw, &c.HarvestDayOffset, &c.HarvestWindowDays, &src,
			&c.ChangeNote, &c.AuthoredBy, &c.AuthoredAt, &c.UpdatedAt, &pub, &c.PublishedAt, &c.SyncSeq,
			&c.Template.Ref, &c.Template.Code, &c.Template.Name, &c.Template.NameKey, &c.Template.Crop, &c.Template.Scope, &coop,
			&c.Template.Standard, &c.Template.Description, &c.Template.Status, &c.Template.StatusReason, &tsrc,
			&c.Template.LatestVersion, &c.Template.PublishedVersion, &c.Template.CreatedBy, &c.Template.CreatedAt, &c.Template.UpdatedAt); err != nil {
			return nil, err
		}
		c.SourceVersionRef, c.PublishedBy = deref(src), deref(pub)
		c.Template.CooperativeRef, c.Template.SourceTemplateRef = deref(coop), deref(tsrc)
		if err := json.Unmarshal(raw, &c.Steps); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Gắn giống ↔ SOP
// ---------------------------------------------------------------------------

// Binding là gắn giống lúa ↔ mẫu SOP mặc định.
type Binding struct {
	ID          uuid.UUID
	VarietyCode string
	TemplateRef uuid.UUID
	Note        string
	BoundBy     uuid.UUID
	BoundAt     time.Time
}

// CurrentBinding trả gắn hiện hành của giống; forUpdate khóa dòng.
func CurrentBinding(ctx context.Context, tx pgx.Tx, variety string, forUpdate bool) (*Binding, error) {
	q := `SELECT id, variety_code, template_ref, coalesce(note, ''), bound_by, bound_at FROM pf_cropcycle.variety_sop_binding
	  WHERE variety_code = $1::text AND superseded_at IS NULL`
	if forUpdate {
		q += " FOR UPDATE"
	}
	var b Binding
	err := tx.QueryRow(ctx, q, variety).Scan(&b.ID, &b.VarietyCode, &b.TemplateRef, &b.Note, &b.BoundBy, &b.BoundAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ReplaceBinding đóng gắn cũ (nếu có) và ghi gắn mới.
func ReplaceBinding(ctx context.Context, tx pgx.Tx, tid uuid.UUID, old *Binding, b Binding) error {
	if old != nil {
		if _, err := tx.Exec(ctx, `UPDATE pf_cropcycle.variety_sop_binding SET superseded_at = now(), superseded_by = $2::uuid
		  WHERE id = $1::uuid`, old.ID, b.ID); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_cropcycle.variety_sop_binding (tenant_id, id, variety_code, template_ref, note, bound_by)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::uuid)`,
		tid, b.ID, b.VarietyCode, b.TemplateRef, nilIfEmpty(b.Note), b.BoundBy)
	return err
}

// ListBindings liệt kê gắn hiện hành theo mã giống.
func ListBindings(ctx context.Context, tx pgx.Tx) ([]Binding, error) {
	rows, err := tx.Query(ctx, `SELECT id, variety_code, template_ref, coalesce(note, ''), bound_by, bound_at
	  FROM pf_cropcycle.variety_sop_binding WHERE superseded_at IS NULL ORDER BY variety_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Binding
	for rows.Next() {
		var b Binding
		if err := rows.Scan(&b.ID, &b.VarietyCode, &b.TemplateRef, &b.Note, &b.BoundBy, &b.BoundAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
