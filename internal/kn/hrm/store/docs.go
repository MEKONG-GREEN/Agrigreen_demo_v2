package store

// W11.24: SQL của kho tài liệu nhân sự — sổ doc_folder, doc, doc_access. Nội dung tệp ở pf/evidence; ở đây chỉ siêu dữ liệu.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
)

// FolderRow là một phiên bản đầy đủ của thư mục.
type FolderRow struct {
	ID         uuid.UUID
	FolderID   uuid.UUID
	ParentID   *uuid.UUID
	Name       string
	SystemKey  string
	DocType    string
	SortOrder  int
	ACL        []byte
	Status     string
	Change     string
	Note       string
	Seq        int64
	RecordedAt time.Time
	Actor      uuid.UUID
}

const folderCols = `id, folder_id, parent_id, name, coalesce(system_key, ''), coalesce(doc_type, ''), sort_order, acl, status, change,
  coalesce(note, ''), seq, recorded_at, actor_ref`

func scanFolder(r pgx.CollectableRow) (FolderRow, error) {
	var x FolderRow
	err := r.Scan(&x.ID, &x.FolderID, &x.ParentID, &x.Name, &x.SystemKey, &x.DocType, &x.SortOrder, &x.ACL, &x.Status, &x.Change,
		&x.Note, &x.Seq, &x.RecordedAt, &x.Actor)
	return x, err
}

// CurrentFolders trả phiên bản mới nhất (truy vấn con trả nguyên dòng: cột biểu thức như coalesce(...) không giữ tên cột —
// truy vấn ngoài phải đọc từ cột gốc) của mọi thư mục (kể cả đã ẩn), sắp theo sort_order, tên.
func CurrentFolders(ctx context.Context, tx pgx.Tx) ([]FolderRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+folderCols+` FROM (
	    SELECT DISTINCT ON (folder_id) * FROM pf_hrm.doc_folder ORDER BY folder_id, seq DESC) v
	  ORDER BY sort_order, name, folder_id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanFolder)
}

// FolderHistory trả mọi phiên bản của thư mục, cũ trước.
func FolderHistory(ctx context.Context, tx pgx.Tx, folderID uuid.UUID) ([]FolderRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+folderCols+` FROM pf_hrm.doc_folder WHERE folder_id = $1 ORDER BY seq`, folderID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanFolder)
}

// LockDocs khóa ghi cây thư mục của tenant trong giao dịch (trùng tên cùng cấp, thư mục rỗng khi ẩn).
func LockDocs(ctx context.Context, tx pgx.Tx, tid uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_hrm.docs:' || $1::text, 0))`, tid)
	return err
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// InsertFolder ghi một phiên bản thư mục.
func InsertFolder(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x FolderRow, at time.Time, idem string) (int64, error) {
	acl := x.ACL
	if len(acl) == 0 {
		acl = []byte("{}")
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_hrm.doc_folder (id, tenant_id, stream_key, folder_id, parent_id, name, system_key, doc_type,
	    sort_order, acl, status, change, note, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'folder:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12, $13, $14, $15, $16)
	  RETURNING seq`,
		x.ID, tid, x.FolderID, x.ParentID, x.Name, strOrNil(x.SystemKey), strOrNil(x.DocType), x.SortOrder, string(acl), x.Status,
		x.Change, strOrNil(x.Note), at.UTC(), x.Actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// DocRow là một phiên bản đầy đủ của tài liệu.
type DocRow struct {
	ID            uuid.UUID
	DocID         uuid.UUID
	FolderID      uuid.UUID
	Title         string
	DocType       string
	Year          *int
	UnitCode      string
	StaffIDs      []uuid.UUID
	DocNo         string
	IssuedOn      *time.Time
	Note          string
	FileRef       uuid.UUID
	FileName      string
	ContentType   string
	Size          int64
	Status        string
	Change        string
	ChangedFields []string
	Reason        string
	Seq           int64
	RecordedAt    time.Time
	Actor         uuid.UUID
}

const docCols = `id, doc_id, folder_id, title, doc_type, doc_year, coalesce(unit_code, ''), staff_ids, coalesce(doc_no, ''), issued_on,
  coalesce(note, ''), file_ref, file_name, content_type, size_bytes, status, change, changed_fields, coalesce(reason, ''), seq,
  recorded_at, actor_ref`

func scanDoc(r pgx.CollectableRow) (DocRow, error) {
	var x DocRow
	err := r.Scan(&x.ID, &x.DocID, &x.FolderID, &x.Title, &x.DocType, &x.Year, &x.UnitCode, &x.StaffIDs, &x.DocNo, &x.IssuedOn,
		&x.Note, &x.FileRef, &x.FileName, &x.ContentType, &x.Size, &x.Status, &x.Change, &x.ChangedFields, &x.Reason, &x.Seq,
		&x.RecordedAt, &x.Actor)
	return x, err
}

// CurrentDocs trả phiên bản mới nhất của mọi tài liệu (kể cả đã ẩn), mới tải trước.
func CurrentDocs(ctx context.Context, tx pgx.Tx) ([]DocRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+docCols+` FROM (
	    SELECT DISTINCT ON (doc_id) * FROM pf_hrm.doc ORDER BY doc_id, seq DESC) v ORDER BY doc_id DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanDoc)
}

// DocHistory trả mọi phiên bản của tài liệu, cũ trước; rỗng nếu không có.
func DocHistory(ctx context.Context, tx pgx.Tx, docID uuid.UUID) ([]DocRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+docCols+` FROM pf_hrm.doc WHERE doc_id = $1 ORDER BY seq`, docID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanDoc)
}

// InsertDoc ghi một phiên bản tài liệu.
func InsertDoc(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x DocRow, at time.Time, idem string) (int64, error) {
	if x.StaffIDs == nil {
		x.StaffIDs = []uuid.UUID{}
	}
	if x.ChangedFields == nil {
		x.ChangedFields = []string{}
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_hrm.doc (id, tenant_id, stream_key, doc_id, folder_id, title, doc_type, doc_year, unit_code,
	    staff_ids, doc_no, issued_on, note, file_ref, file_name, content_type, size_bytes, status, change, changed_fields, reason,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'doc:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9::text[]::uuid[], $10, $11, $12, $13, $14, $15, $16,
	    $17, $18, $19, $20, $21, $22, $23, $24) RETURNING seq`,
		x.ID, tid, x.DocID, x.FolderID, x.Title, x.DocType, x.Year, strOrNil(x.UnitCode), uuidStrs(x.StaffIDs), strOrNil(x.DocNo),
		x.IssuedOn, strOrNil(x.Note), x.FileRef, x.FileName, x.ContentType, x.Size, x.Status, x.Change, x.ChangedFields,
		strOrNil(x.Reason), at.UTC(), x.Actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// DocAccess là một dòng nhật ký xem trước/tải xuống.
type DocAccess struct {
	ID         uuid.UUID
	Action     string
	DocID      uuid.UUID
	FileRef    uuid.UUID
	Scope      string
	Actor      uuid.UUID
	RecordedAt time.Time
}

// InsertDocAccess ghi một dòng nhật ký xem trước/tải xuống (chuỗi theo người xem).
func InsertDocAccess(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a DocAccess, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_hrm.doc_access (id, tenant_id, stream_key, action, doc_id, file_ref, scope, event_time,
	    actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'actor:' || $3::uuid::text, $4, $5, $6, $7, $8, $3::uuid, 'access:' || $1::uuid::text, $9)`,
		a.ID, tid, a.Actor, a.Action, a.DocID, a.FileRef, a.Scope, at.UTC(), domain.RuleVersion)
	return err
}

// DocAccessOf trả nhật ký xem trước/tải xuống của tài liệu, mới trước, tối đa limit.
func DocAccessOf(ctx context.Context, tx pgx.Tx, docID uuid.UUID, limit int) ([]DocAccess, error) {
	rows, err := tx.Query(ctx, `SELECT id, action, doc_id, file_ref, scope, actor_ref, recorded_at FROM pf_hrm.doc_access
	  WHERE doc_id = $1 ORDER BY recorded_at DESC, id DESC LIMIT $2`, docID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DocAccess, error) {
		var a DocAccess
		err := r.Scan(&a.ID, &a.Action, &a.DocID, &a.FileRef, &a.Scope, &a.Actor, &a.RecordedAt)
		return a, err
	})
}

// DocIDForFile tìm tài liệu đã nhận tệp; người gọi phải đọc lại phiên bản
// hiện hành và kiểm quyền, không dùng bản ghi cũ để cấp tệp đã thay thế.
func DocIDForFile(ctx context.Context, tx pgx.Tx, fileRef uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT doc_id FROM pf_hrm.doc WHERE file_ref = $1 ORDER BY seq DESC LIMIT 1`, fileRef).Scan(&id)
	return id, err
}
