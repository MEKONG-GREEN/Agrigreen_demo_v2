package store

// Mẫu báo cáo dạng lưới KN v7.7 (migration pf 000065–000066). Mọi hàm nhận pgx.Tx đã ở
// ngữ cảnh tenant (RLS); không lọc tenant trong SQL.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// FormRuleVersion là rule_version của sổ mẫu báo cáo.
const FormRuleVersion = "reports.form.v1"

// FormTemplate là một dòng form_template.
type FormTemplate struct {
	ID             uuid.UUID
	Code           string
	Name           string
	DepartmentCode string
	EntryLevel     string
	Status         string
	RetiredReason  *string
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

const formTplCols = `id, code, name, department_code, entry_level, status, retired_reason, created_by, created_at, updated_at`

func scanFormTpl(row pgx.Row) (FormTemplate, error) {
	var t FormTemplate
	err := row.Scan(&t.ID, &t.Code, &t.Name, &t.DepartmentCode, &t.EntryLevel, &t.Status, &t.RetiredReason, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

// InsertFormTemplate ghi mẫu mới.
func InsertFormTemplate(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t FormTemplate) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_reports.form_template (tenant_id, `+formTplCols+`)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		tid, t.ID, t.Code, t.Name, t.DepartmentCode, t.EntryLevel, t.Status, t.RetiredReason, t.CreatedBy, t.CreatedAt.UTC(), t.UpdatedAt.UTC())
	return err
}

// FormTemplateByID đọc (khóa nếu lock) một mẫu; không có → nil.
func FormTemplateByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*FormTemplate, error) {
	q := "SELECT " + formTplCols + " FROM pf_reports.form_template WHERE id = $1::uuid"
	if lock {
		q += " FOR UPDATE"
	}
	return oneTpl(tx.QueryRow(ctx, q, id))
}

// FormTemplateByCode đọc mẫu theo (phòng ban, mã).
func FormTemplateByCode(ctx context.Context, tx pgx.Tx, dept, code string) (*FormTemplate, error) {
	return oneTpl(tx.QueryRow(ctx, "SELECT "+formTplCols+" FROM pf_reports.form_template WHERE department_code = $1::text AND code = $2::text", dept, code))
}

func oneTpl(row pgx.Row) (*FormTemplate, error) {
	t, err := scanFormTpl(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// FormTemplateFilter lọc danh sách mẫu (rỗng = mọi giá trị).
type FormTemplateFilter struct {
	Department string
	Level      string
	Status     string
}

// ListFormTemplates trả mẫu theo bộ lọc, sắp theo phòng ban, mã.
func ListFormTemplates(ctx context.Context, tx pgx.Tx, f FormTemplateFilter) ([]FormTemplate, error) {
	rows, err := tx.Query(ctx, "SELECT "+formTplCols+` FROM pf_reports.form_template
	  WHERE ($1::text = '' OR department_code = $1::text) AND ($2::text = '' OR entry_level = $2::text) AND ($3::text = '' OR status = $3::text)
	  ORDER BY department_code, code`, f.Department, f.Level, f.Status)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (FormTemplate, error) { return scanFormTpl(r) })
}

// UpdateFormTemplate ghi tên, trạng thái của mẫu.
func UpdateFormTemplate(ctx context.Context, tx pgx.Tx, t FormTemplate) error {
	_, err := tx.Exec(ctx, `UPDATE pf_reports.form_template SET name = $2, status = $3, retired_reason = $4, updated_at = $5 WHERE id = $1::uuid`,
		t.ID, t.Name, t.Status, t.RetiredReason, t.UpdatedAt.UTC())
	return err
}

// FormVersion là một dòng form_template_version.
type FormVersion struct {
	ID              uuid.UUID
	TemplateID      uuid.UUID
	Version         int
	Status          string
	Def             port.FormDefinition
	SHA256          string
	EffectiveFrom   *time.Time
	EffectiveLabel  *string
	Origin          string
	OriginVersionID *uuid.UUID
	ImportID        *uuid.UUID
	CreatedBy       uuid.UUID
	CreatedAt       time.Time
	UpdatedBy       uuid.UUID
	UpdatedAt       time.Time
	ProposedBy      *uuid.UUID
	ProposedAt      *time.Time
	PublishedBy     *uuid.UUID
	PublishedAt     *time.Time
	RetiredBy       *uuid.UUID
	RetiredAt       *time.Time
	RetireReason    *string
}

const formVerCols = `id, template_id, version, status, definition::text, definition_sha256, effective_from, effective_label, origin,
  origin_version_id, import_id, created_by, created_at, updated_by, updated_at, proposed_by, proposed_at, published_by, published_at,
  retired_by, retired_at, retire_reason`

func scanFormVer(row pgx.Row) (FormVersion, error) {
	var v FormVersion
	var def string
	err := row.Scan(&v.ID, &v.TemplateID, &v.Version, &v.Status, &def, &v.SHA256, &v.EffectiveFrom, &v.EffectiveLabel, &v.Origin,
		&v.OriginVersionID, &v.ImportID, &v.CreatedBy, &v.CreatedAt, &v.UpdatedBy, &v.UpdatedAt, &v.ProposedBy, &v.ProposedAt,
		&v.PublishedBy, &v.PublishedAt, &v.RetiredBy, &v.RetiredAt, &v.RetireReason)
	if err != nil {
		return v, err
	}
	return v, json.Unmarshal([]byte(def), &v.Def)
}

func oneVer(row pgx.Row) (*FormVersion, error) {
	v, err := scanFormVer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// DefinitionJSON trả JSON chuẩn và sha256 của định nghĩa.
func DefinitionJSON(d port.FormDefinition) (string, string, error) {
	b, err := json.Marshal(d)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(b)
	return string(b), hex.EncodeToString(sum[:]), nil
}

// InsertFormVersion ghi phiên bản mới; số phiên bản = lớn nhất + 1 (khóa theo mẫu).
func InsertFormVersion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v *FormVersion) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_reports.form:' || $1::text || ':' || $2::text, 0))", tid, v.TemplateID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "SELECT coalesce(max(version), 0) + 1 FROM pf_reports.form_template_version WHERE template_id = $1::uuid", v.TemplateID).Scan(&v.Version); err != nil {
		return err
	}
	def, sum, err := DefinitionJSON(v.Def)
	if err != nil {
		return err
	}
	v.SHA256 = sum
	_, err = tx.Exec(ctx, `INSERT INTO pf_reports.form_template_version (tenant_id, id, template_id, version, status, definition, definition_sha256,
	    effective_from, effective_label, origin, origin_version_id, import_id, created_by, created_at, updated_by, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8::date, $9, $10, $11, $12, $13, $14, $15, $16)`,
		tid, v.ID, v.TemplateID, v.Version, v.Status, def, sum, datePtr(v.EffectiveFrom), v.EffectiveLabel, v.Origin, v.OriginVersionID, v.ImportID,
		v.CreatedBy, v.CreatedAt.UTC(), v.UpdatedBy, v.UpdatedAt.UTC())
	return err
}

func datePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

// FormVersionByID đọc phiên bản theo id.
func FormVersionByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*FormVersion, error) {
	q := "SELECT " + formVerCols + " FROM pf_reports.form_template_version WHERE id = $1::uuid"
	if lock {
		q += " FOR UPDATE"
	}
	return oneVer(tx.QueryRow(ctx, q, id))
}

// FormVersionByNumber đọc phiên bản theo (mẫu, số).
func FormVersionByNumber(ctx context.Context, tx pgx.Tx, templateID uuid.UUID, n int, lock bool) (*FormVersion, error) {
	q := "SELECT " + formVerCols + " FROM pf_reports.form_template_version WHERE template_id = $1::uuid AND version = $2::integer"
	if lock {
		q += " FOR UPDATE"
	}
	return oneVer(tx.QueryRow(ctx, q, templateID, n))
}

// FormVersions trả mọi phiên bản của mẫu (mới nhất trước).
func FormVersions(ctx context.Context, tx pgx.Tx, templateID uuid.UUID) ([]FormVersion, error) {
	rows, err := tx.Query(ctx, "SELECT "+formVerCols+" FROM pf_reports.form_template_version WHERE template_id = $1::uuid ORDER BY version DESC", templateID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (FormVersion, error) { return scanFormVer(r) })
}

// OpenFormVersion trả phiên bản đang soạn (nháp hoặc chờ phát hành) của mẫu; không có → nil.
func OpenFormVersion(ctx context.Context, tx pgx.Tx, templateID uuid.UUID) (*FormVersion, error) {
	return oneVer(tx.QueryRow(ctx, "SELECT "+formVerCols+` FROM pf_reports.form_template_version
	  WHERE template_id = $1::uuid AND status IN ('draft', 'proposed')`, templateID))
}

// ApplicableFormVersion trả phiên bản đã phát hành mới nhất có áp dụng từ ≤ ngày at.
func ApplicableFormVersion(ctx context.Context, tx pgx.Tx, templateID uuid.UUID, at time.Time) (*FormVersion, error) {
	return oneVer(tx.QueryRow(ctx, "SELECT "+formVerCols+` FROM pf_reports.form_template_version
	  WHERE template_id = $1::uuid AND status = 'published' AND effective_from <= $2::date
	  ORDER BY effective_from DESC, version DESC LIMIT 1`, templateID, at.Format("2006-01-02")))
}

// LatestPublishedEffective trả "áp dụng từ" lớn nhất của các phiên bản đã phát hành (còn hiệu lực).
func LatestPublishedEffective(ctx context.Context, tx pgx.Tx, templateID uuid.UUID) (*time.Time, error) {
	var t *time.Time
	err := tx.QueryRow(ctx, `SELECT max(effective_from)::timestamptz FROM pf_reports.form_template_version
	  WHERE template_id = $1::uuid AND status = 'published'`, templateID).Scan(&t)
	return t, err
}

// SaveFormDraft ghi định nghĩa và áp dụng từ kỳ của nháp.
func SaveFormDraft(ctx context.Context, tx pgx.Tx, v *FormVersion) error {
	def, sum, err := DefinitionJSON(v.Def)
	if err != nil {
		return err
	}
	v.SHA256 = sum
	_, err = tx.Exec(ctx, `UPDATE pf_reports.form_template_version SET definition = $2::jsonb, definition_sha256 = $3, effective_from = $4::date,
	    effective_label = $5, updated_by = $6, updated_at = $7 WHERE id = $1::uuid`,
		v.ID, def, sum, datePtr(v.EffectiveFrom), v.EffectiveLabel, v.UpdatedBy, v.UpdatedAt.UTC())
	return err
}

// SetFormVersionStatus ghi chuyển trạng thái (đề nghị, rút, phát hành, ngừng dùng).
func SetFormVersionStatus(ctx context.Context, tx pgx.Tx, v FormVersion) error {
	_, err := tx.Exec(ctx, `UPDATE pf_reports.form_template_version SET status = $2, proposed_by = $3, proposed_at = $4, published_by = $5,
	    published_at = $6, retired_by = $7, retired_at = $8, retire_reason = $9 WHERE id = $1::uuid`,
		v.ID, v.Status, v.ProposedBy, utcPtr(v.ProposedAt), v.PublishedBy, utcPtr(v.PublishedAt), v.RetiredBy, utcPtr(v.RetiredAt), v.RetireReason)
	return err
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// ---------------------------------------------------------------------------
// Gán mẫu vào loại báo cáo
// ---------------------------------------------------------------------------

// AssignmentEvent là một dòng sổ form_assignment_event.
type AssignmentEvent struct {
	ID             uuid.UUID
	AssignmentID   uuid.UUID
	Action         string
	TemplateID     uuid.UUID
	DepartmentCode string
	EntryLevel     string
	ReportType     string
	Reason         *string
	EventTime      time.Time
	ActorRef       uuid.UUID
	IdemKey        string
}

// InsertAssignmentEvent ghi sổ gán (DB tính seq và chuỗi băm).
func InsertAssignmentEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e AssignmentEvent) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_reports.form_assignment_event (id, tenant_id, stream_key, assignment_id, action, template_id,
	    department_code, entry_level, report_type, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		e.ID, tid, "assignment:"+e.AssignmentID.String(), e.AssignmentID, e.Action, e.TemplateID, e.DepartmentCode, e.EntryLevel, e.ReportType,
		e.Reason, e.EventTime.UTC(), e.ActorRef, e.IdemKey, FormRuleVersion)
	return err
}

// Assignment là bảng chiếu form_assignment.
type Assignment struct {
	ID             uuid.UUID
	TemplateID     uuid.UUID
	DepartmentCode string
	EntryLevel     string
	ReportType     string
	Status         string
	AssignedBy     uuid.UUID
	AssignedAt     time.Time
	RevokedBy      *uuid.UUID
	RevokedAt      *time.Time
	RevokeReason   *string
	LastEventID    uuid.UUID
}

const asgCols = `id, template_id, department_code, entry_level, report_type, status, assigned_by, assigned_at, revoked_by, revoked_at, revoke_reason, last_event_id`

func scanAsg(row pgx.Row) (Assignment, error) {
	var a Assignment
	err := row.Scan(&a.ID, &a.TemplateID, &a.DepartmentCode, &a.EntryLevel, &a.ReportType, &a.Status, &a.AssignedBy, &a.AssignedAt,
		&a.RevokedBy, &a.RevokedAt, &a.RevokeReason, &a.LastEventID)
	return a, err
}

// PutAssignment ghi bảng chiếu gán.
func PutAssignment(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a Assignment) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_reports.form_assignment (tenant_id, `+asgCols+`)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	  ON CONFLICT (tenant_id, id) DO UPDATE SET status = EXCLUDED.status, revoked_by = EXCLUDED.revoked_by, revoked_at = EXCLUDED.revoked_at,
	    revoke_reason = EXCLUDED.revoke_reason, last_event_id = EXCLUDED.last_event_id, updated_at = now()`,
		tid, a.ID, a.TemplateID, a.DepartmentCode, a.EntryLevel, a.ReportType, a.Status, a.AssignedBy, a.AssignedAt.UTC(),
		a.RevokedBy, utcPtr(a.RevokedAt), a.RevokeReason, a.LastEventID)
	return err
}

// AssignmentByID đọc (khóa nếu lock) một gán.
func AssignmentByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*Assignment, error) {
	q := "SELECT " + asgCols + " FROM pf_reports.form_assignment WHERE id = $1::uuid"
	if lock {
		q += " FOR UPDATE"
	}
	a, err := scanAsg(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AssignmentFilter lọc gán (rỗng = mọi giá trị).
type AssignmentFilter struct {
	TemplateID *uuid.UUID
	Department string
	Level      string
	ReportType string
	ActiveOnly bool
}

// ListAssignments trả gán theo bộ lọc.
func ListAssignments(ctx context.Context, tx pgx.Tx, f AssignmentFilter) ([]Assignment, error) {
	rows, err := tx.Query(ctx, "SELECT "+asgCols+` FROM pf_reports.form_assignment
	  WHERE ($1::uuid IS NULL OR template_id = $1::uuid) AND ($2::text = '' OR department_code = $2::text)
	    AND ($3::text = '' OR entry_level = $3::text) AND ($4::text = '' OR report_type = $4::text) AND (NOT $5::boolean OR status = 'active')
	  ORDER BY department_code, entry_level, report_type, assigned_at`, f.TemplateID, f.Department, f.Level, f.ReportType, f.ActiveOnly)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Assignment, error) { return scanAsg(r) })
}

// ---------------------------------------------------------------------------
// Nhật ký thiết kế
// ---------------------------------------------------------------------------

// FormEvent là một dòng sổ form_template_event.
type FormEvent struct {
	ID         uuid.UUID
	TemplateID uuid.UUID
	VersionID  *uuid.UUID
	Version    *int
	Action     string
	Changes    json.RawMessage
	Reason     *string
	EventTime  time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	Seq        int64
	RecordedAt time.Time
}

// InsertFormEvent ghi sổ nhật ký.
func InsertFormEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *FormEvent) error {
	ch := string(e.Changes)
	if ch == "" {
		ch = "[]"
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_reports.form_template_event (id, tenant_id, stream_key, template_id, version_id, version, action, changes,
	    reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12, $13) RETURNING seq, recorded_at`,
		e.ID, tid, "form:"+e.TemplateID.String(), e.TemplateID, e.VersionID, e.Version, e.Action, ch, e.Reason, e.EventTime.UTC(), e.ActorRef,
		e.IdemKey, FormRuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// FormEvents trả nhật ký của mẫu, mới nhất trước, sau con trỏ (id) nếu có.
func FormEvents(ctx context.Context, tx pgx.Tx, templateID uuid.UUID, before *uuid.UUID, limit int) ([]FormEvent, error) {
	rows, err := tx.Query(ctx, `SELECT id, template_id, version_id, version, action, changes::text, reason, event_time, actor_ref, idem_key, seq, recorded_at
	  FROM pf_reports.form_template_event WHERE template_id = $1::uuid AND ($2::uuid IS NULL OR id < $2::uuid)
	  ORDER BY id DESC LIMIT $3`, templateID, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (FormEvent, error) {
		var e FormEvent
		var ch string
		err := r.Scan(&e.ID, &e.TemplateID, &e.VersionID, &e.Version, &e.Action, &ch, &e.Reason, &e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.RecordedAt)
		e.Changes = json.RawMessage(ch)
		return e, err
	})
}

// ---------------------------------------------------------------------------
// Lần nạp Excel
// ---------------------------------------------------------------------------

// FormImport là một dòng form_import.
type FormImport struct {
	ID         uuid.UUID
	FileName   string
	FileSHA256 string
	SizeBytes  int64
	ObjectKey  *string
	SheetName  string
	Status     string
	ErrorCode  *string
	Report     json.RawMessage
	TemplateID *uuid.UUID
	VersionID  *uuid.UUID
	CreatedBy  uuid.UUID
	CreatedAt  time.Time
}

// InsertFormImport ghi lần nạp.
func InsertFormImport(ctx context.Context, tx pgx.Tx, tid uuid.UUID, f FormImport) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_reports.form_import (tenant_id, id, file_name, file_sha256, size_bytes, object_key, sheet_name, status,
	    error_code, report, template_id, version_id, created_by, created_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12, $13, $14)`,
		tid, f.ID, f.FileName, f.FileSHA256, f.SizeBytes, f.ObjectKey, f.SheetName, f.Status, f.ErrorCode, string(f.Report), f.TemplateID,
		f.VersionID, f.CreatedBy, f.CreatedAt.UTC())
	return err
}
