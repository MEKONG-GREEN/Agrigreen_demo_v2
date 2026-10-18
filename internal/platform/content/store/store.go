// Package store: SQL của pf/content, chỉ chạy trên giao dịch đã đặt tenant
// (tenant.WithTenantTx) bằng pool vai content_app (R-05).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// ---------------------------------------------------------------------------
// Sổ phiên bản
// ---------------------------------------------------------------------------

const versionCols = `id, document_ref, version_no, title, summary, body, body_format, category, tags, attachments,
	based_on_version, event_time, recorded_at, actor_ref, idem_key, seq`

func scanVersion(row pgx.Row) (*domain.Version, error) {
	var (
		v        domain.Version
		format   string
		category *string
		atts     []byte
	)
	err := row.Scan(&v.ID, &v.DocumentRef, &v.VersionNo, &v.Content.Title, &v.Content.Summary, &v.Content.Body, &format,
		&category, &v.Content.Tags, &atts, &v.BasedOnVersion, &v.EventTime, &v.RecordedAt, &v.AuthorRef, &v.IdemKey, &v.Seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v.Content.BodyFormat = domain.BodyFormat(format)
	if category != nil {
		v.Content.Category = *category
	}
	if err := json.Unmarshal(atts, &v.Content.Attachments); err != nil {
		return nil, err
	}
	if v.Content.Tags == nil {
		v.Content.Tags = []string{}
	}
	return &v, nil
}

// InsertVersion ghi một phiên bản vào sổ. seq và recorded_at do DB tính.
func InsertVersion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v *domain.Version) error {
	atts, err := json.Marshal(nonNil(v.Content.Attachments))
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_content.document_version
	    (id, tenant_id, stream_key, document_ref, version_no, title, summary, body, body_format, category, tags,
	     attachments, based_on_version, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14, $15, $16, $17)
	  RETURNING seq, recorded_at`,
		v.ID, tid, domain.StreamKey(v.DocumentRef), v.DocumentRef, v.VersionNo, v.Content.Title, v.Content.Summary,
		v.Content.Body, string(v.Content.BodyFormat), nilIfEmpty(v.Content.Category), nonNilS(v.Content.Tags), string(atts),
		v.BasedOnVersion, v.EventTime.UTC(), v.AuthorRef, v.IdemKey, domain.RuleVersion).Scan(&v.Seq, &v.RecordedAt)
}

// VersionByIdemKey tìm phiên bản đã ghi với idem_key (gọi lặp).
func VersionByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Version, error) {
	return scanVersion(tx.QueryRow(ctx, `SELECT `+versionCols+` FROM pf_content.document_version WHERE idem_key = $1`+ledger.NewestByIdem(""), key))
}

// Version đọc một phiên bản của tài liệu.
func Version(ctx context.Context, tx pgx.Tx, ref uuid.UUID, no int) (*domain.Version, error) {
	return scanVersion(tx.QueryRow(ctx, `SELECT `+versionCols+` FROM pf_content.document_version
	    WHERE document_ref = $1 AND version_no = $2`, ref, no))
}

// Versions đọc mọi phiên bản của tài liệu theo thứ tự.
func Versions(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]domain.Version, error) {
	rows, err := tx.Query(ctx, `SELECT `+versionCols+` FROM pf_content.document_version
	    WHERE document_ref = $1 ORDER BY version_no`, ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Version
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Sổ chuyển trạng thái
// ---------------------------------------------------------------------------

const eventCols = `id, document_ref, version_no, action, coalesce(reason, ''), event_time, recorded_at, actor_ref, idem_key, seq`

func scanEvent(row pgx.Row) (*domain.Event, error) {
	var (
		e      domain.Event
		action string
	)
	err := row.Scan(&e.ID, &e.DocumentRef, &e.VersionNo, &action, &e.Reason, &e.EventTime, &e.RecordedAt, &e.ActorRef, &e.IdemKey, &e.Seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Action = domain.Action(action)
	return &e, nil
}

// InsertEvent ghi một chuyển trạng thái vào sổ.
func InsertEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *domain.Event) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_content.document_event
	    (id, tenant_id, stream_key, document_ref, version_no, action, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	  RETURNING seq, recorded_at`,
		e.ID, tid, domain.StreamKey(e.DocumentRef), e.DocumentRef, e.VersionNo, string(e.Action), nilIfEmpty(e.Reason),
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// EventByIdemKey tìm chuyển trạng thái đã ghi với idem_key (gọi lặp).
func EventByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Event, error) {
	return scanEvent(tx.QueryRow(ctx, `SELECT `+eventCols+` FROM pf_content.document_event WHERE idem_key = $1`+ledger.NewestByIdem(""), key))
}

// Events đọc mọi chuyển trạng thái của tài liệu theo thứ tự.
func Events(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]domain.Event, error) {
	rows, err := tx.Query(ctx, `SELECT `+eventCols+` FROM pf_content.document_event WHERE document_ref = $1 ORDER BY seq`, ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Bảng chiếu document
// ---------------------------------------------------------------------------

const docCols = `ref, kind, audience_level, audience_units, coalesce(province_code, ''), parent_ref, status, created_by,
	created_at, updated_at, last_version_no, working_version_no, working_status, published_version_no, published_version_id,
	published_at, publisher_ref, coalesce(title, ''), coalesce(summary, ''), coalesce(category, ''), tags, retired_at,
	coalesce(retire_reason, ''), sync_seq`

func scanDoc(row pgx.Row) (*domain.Document, error) {
	var (
		d                   domain.Document
		kind, level, status string
		working             *string
	)
	err := row.Scan(&d.Ref, &kind, &level, &d.Audience.Units, &d.Audience.ProvinceCode, &d.ParentRef, &status, &d.CreatedBy,
		&d.CreatedAt, &d.UpdatedAt, &d.LastVersionNo, &d.WorkingVersionNo, &working, &d.PublishedVersionNo, &d.PublishedVersionID,
		&d.PublishedAt, &d.PublisherRef, &d.Title, &d.Summary, &d.Category, &d.Tags, &d.RetiredAt, &d.RetireReason, &d.SyncSeq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Kind, d.Audience.Level, d.Status = port.Kind(kind), port.AudienceLevel(level), domain.Status(status)
	if working != nil {
		w := domain.VersionStatus(*working)
		d.WorkingStatus = &w
	}
	if d.Audience.Units == nil {
		d.Audience.Units = []string{}
	}
	if d.Tags == nil {
		d.Tags = []string{}
	}
	return &d, nil
}

// InsertDocument tạo bảng chiếu cho tài liệu mới (bản nháp đầu).
func InsertDocument(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d *domain.Document) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_content.document
	    (tenant_id, ref, kind, audience_level, audience_units, province_code, parent_ref, status, created_by,
	     last_version_no, working_version_no, working_status)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	  RETURNING created_at, updated_at`,
		tid, d.Ref, string(d.Kind), string(d.Audience.Level), nonNilS(d.Audience.Units), nilIfEmpty(d.Audience.ProvinceCode),
		d.ParentRef, string(d.Status), d.CreatedBy, d.LastVersionNo, d.WorkingVersionNo, workingStr(d.WorkingStatus)).
		Scan(&d.CreatedAt, &d.UpdatedAt)
}

// DocumentByRef đọc bảng chiếu; forUpdate khóa dòng để tuần tự hóa chuyển trạng thái.
func DocumentByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID, forUpdate bool) (*domain.Document, error) {
	q := `SELECT ` + docCols + ` FROM pf_content.document WHERE ref = $1`
	if forUpdate {
		q += ` FOR UPDATE`
	}
	return scanDoc(tx.QueryRow(ctx, q, ref))
}

// UpdateDocument ghi trạng thái mới của bảng chiếu. search != nil → cập nhật
// chỉ mục tìm kiếm; bumpSync → lấy sync_seq mới (bản hiện hành đổi).
func UpdateDocument(ctx context.Context, tx pgx.Tx, d *domain.Document, search *string, bumpSync bool) error {
	return tx.QueryRow(ctx, `UPDATE pf_content.document SET
	    status = $2, last_version_no = $3, working_version_no = $4, working_status = $5,
	    published_version_no = $6, published_version_id = $7, published_at = $8, publisher_ref = $9,
	    title = $10, summary = $11, category = $12, tags = $13, retired_at = $14, retire_reason = $15,
	    search_text = CASE WHEN $16::boolean THEN $17 ELSE search_text END,
	    sync_seq = CASE WHEN $18::boolean THEN nextval('pf_content.document_sync_seq') ELSE sync_seq END,
	    updated_at = now()
	  WHERE ref = $1
	  RETURNING updated_at, sync_seq`,
		d.Ref, string(d.Status), d.LastVersionNo, d.WorkingVersionNo, workingStr(d.WorkingStatus),
		d.PublishedVersionNo, d.PublishedVersionID, d.PublishedAt, d.PublisherRef,
		nilIfEmpty(d.Title), nilIfEmpty(d.Summary), nilIfEmpty(d.Category), nonNilS(d.Tags), d.RetiredAt, nilIfEmpty(d.RetireReason),
		search != nil, search, bumpSync).Scan(&d.UpdatedAt, &d.SyncSeq)
}

// Visibility là phạm vi đọc: All hoặc tập mã đơn vị (đã gồm tỉnh cha của xã).
// Tài liệu toàn quốc luôn thấy được trong tenant.
type Visibility struct {
	All   bool
	Units []string
}

// Filter lọc danh sách.
type Filter struct {
	Vis      Visibility
	Statuses []domain.Status // rỗng = mọi trạng thái
	Kind     string
	Tag      string
	Category string
	TSQuery  string // biểu thức to_tsquery('simple') đã dựng bởi domain.SearchQuery
	// Cursor của danh sách người đọc: (published_at, ref) của dòng cuối trang trước.
	AfterTime *time.Time
	AfterRef  uuid.UUID
	Limit     int
	// ByRef: sắp theo ref giảm dần (danh sách quản trị) thay vì published_at.
	ByRef bool
}

// ListDocuments liệt kê theo bộ lọc.
func ListDocuments(ctx context.Context, tx pgx.Tx, f Filter) ([]domain.Document, error) {
	statuses := make([]string, 0, len(f.Statuses))
	for _, s := range f.Statuses {
		statuses = append(statuses, string(s))
	}
	q := `SELECT ` + docCols + ` FROM pf_content.document
	  WHERE ($1::boolean OR audience_level = 'national' OR audience_units && $2::text[])
	    AND (cardinality($3::text[]) = 0 OR status = ANY($3))
	    AND ($4::text = '' OR kind = $4::text)
	    AND ($5::text = '' OR tags @> ARRAY[$5::text])
	    AND ($6::text = '' OR category = $6::text)
	    AND ($7::text = '' OR search_tsv @@ to_tsquery('simple', $7::text))`
	args := []any{f.Vis.All, nonNilS(f.Vis.Units), statuses, f.Kind, f.Tag, f.Category, f.TSQuery}
	if f.ByRef {
		if f.AfterRef != uuid.Nil {
			q += ` AND ref < $8::uuid`
			args = append(args, f.AfterRef)
		}
		q += ` ORDER BY ref DESC`
	} else {
		if f.AfterTime != nil {
			q += ` AND (published_at, ref) < ($8::timestamptz, $9::uuid)`
			args = append(args, *f.AfterTime, f.AfterRef)
		}
		q += ` ORDER BY published_at DESC NULLS LAST, ref DESC`
	}
	args = append(args, f.Limit)
	q += ` LIMIT $` + strconv.Itoa(len(args))
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Document
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// Supplements liệt kê hướng dẫn đặc thù địa phương đã xuất bản của một quy trình gốc trong phạm vi.
func Supplements(ctx context.Context, tx pgx.Tx, parent uuid.UUID, vis Visibility) ([]domain.Document, error) {
	rows, err := tx.Query(ctx, `SELECT `+docCols+` FROM pf_content.document
	  WHERE parent_ref = $1 AND kind = 'supplement' AND status = 'published'
	    AND ($2::boolean OR audience_units && $3::text[])
	  ORDER BY published_at DESC, ref DESC LIMIT 100`, parent, vis.All, nonNilS(vis.Units))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Document
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// SyncRow là một thay đổi chiều xuống: bảng chiếu + phiên bản hiện hành (nil khi đã gỡ).
type SyncRow struct {
	Doc     domain.Document
	Version *domain.Version
}

// SyncChanges đọc tài liệu có sync_seq > after trong phạm vi, theo thứ tự tăng.
// Chỉ tài liệu đã từng xuất bản (published/retired) mới có sync_seq.
func SyncChanges(ctx context.Context, tx pgx.Tx, vis Visibility, after int64, limit int) ([]SyncRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+docCols+` FROM pf_content.document
	  WHERE sync_seq > $1 AND status IN ('published', 'retired')
	    AND ($2::boolean OR audience_level = 'national' OR audience_units && $3::text[])
	  ORDER BY sync_seq LIMIT $4`, after, vis.All, nonNilS(vis.Units), limit)
	if err != nil {
		return nil, err
	}
	var docs []domain.Document
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		docs = append(docs, *d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]SyncRow, 0, len(docs))
	for _, d := range docs {
		r := SyncRow{Doc: d}
		if d.Status == domain.StatusPublished && d.PublishedVersionNo != nil {
			v, err := Version(ctx, tx, d.Ref, *d.PublishedVersionNo)
			if err != nil {
				return nil, err
			}
			r.Version = v
		}
		out = append(out, r)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Tệp đính kèm
// ---------------------------------------------------------------------------

// AttachmentRow là một tệp đã tải lên.
type AttachmentRow struct {
	ID          uuid.UUID
	DocumentRef uuid.UUID
	Key         string
	SHA256      string
	Size        int64
	ContentType string
	FileName    string
	UploadedBy  uuid.UUID
	CreatedAt   time.Time
}

const attCols = `id, document_ref, object_key, sha256, size_bytes, content_type, file_name, uploaded_by, created_at`

func scanAtt(row pgx.Row) (*AttachmentRow, error) {
	var a AttachmentRow
	err := row.Scan(&a.ID, &a.DocumentRef, &a.Key, &a.SHA256, &a.Size, &a.ContentType, &a.FileName, &a.UploadedBy, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &a, err
}

// InsertAttachment ghi siêu dữ liệu tệp. Trùng idem_key → trả dòng cũ, replayed = true.
func InsertAttachment(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a *AttachmentRow, idemKey string) (replayed bool, err error) {
	prev, err := scanAtt(tx.QueryRow(ctx, `SELECT `+attCols+` FROM pf_content.attachment WHERE idem_key = $1`, idemKey))
	if err != nil {
		return false, err
	}
	if prev != nil {
		*a = *prev
		return true, nil
	}
	return false, tx.QueryRow(ctx, `INSERT INTO pf_content.attachment
	    (tenant_id, id, document_ref, object_key, sha256, size_bytes, content_type, file_name, uploaded_by, idem_key)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING created_at`,
		tid, a.ID, a.DocumentRef, a.Key, a.SHA256, a.Size, a.ContentType, a.FileName, a.UploadedBy, idemKey).Scan(&a.CreatedAt)
}

// AttachmentsByIDs đọc các tệp của một tài liệu theo id (bỏ id không thuộc tài liệu).
func AttachmentsByIDs(ctx context.Context, tx pgx.Tx, doc uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]AttachmentRow, error) {
	strs := make([]string, 0, len(ids))
	for _, id := range ids {
		strs = append(strs, id.String())
	}
	rows, err := tx.Query(ctx, `SELECT `+attCols+` FROM pf_content.attachment WHERE document_ref = $1 AND id = ANY($2::uuid[])`, doc, strs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]AttachmentRow{}
	for rows.Next() {
		a, err := scanAtt(rows)
		if err != nil {
			return nil, err
		}
		out[a.ID] = *a
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nonNilS(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func nonNil(xs []domain.Attachment) []domain.Attachment {
	if xs == nil {
		return []domain.Attachment{}
	}
	return xs
}

func workingStr(s *domain.VersionStatus) *string {
	if s == nil {
		return nil
	}
	v := string(*s)
	return &v
}
