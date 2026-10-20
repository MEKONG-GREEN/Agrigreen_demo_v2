// Package store: SQL Trang chủ phòng + kho văn bản phòng (W11.54) trên pool vai extension_app, luôn trong giao dịch có
// ngữ cảnh tenant (RLS). Ba sổ chỉ ghi thêm (shared.apply_ledger: DB tính seq, prev_hash, row_hash, chống trùng
// idem_key): pf_extension.dept_profile, dept_doc_folder, dept_doc. Số liệu thẻ của trang chủ đọc bảng của chính
// pf/extension và view công bố (pf_price_pub — hợp đồng W11.10/W11.76).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/domain"
)

// Meta là cột chung của một dòng sổ.
type Meta struct {
	ID            uuid.UUID
	Seq           int64
	Event         string
	EventTime     time.Time
	RecordedAt    time.Time
	ActorRef      uuid.UUID
	OnBehalfOf    *uuid.UUID
	SupportReason *string
	IdemKey       string
}

// Lock tuần tự hóa ghi theo khóa nghiệp vụ.
func Lock(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.dept_home:' || $1::text, 0))", key)
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

// ---------------------------------------------------------------------------
// Giới thiệu phòng
// ---------------------------------------------------------------------------

// ProfileRow là một phiên bản giới thiệu phòng.
type ProfileRow struct {
	Department string
	domain.Profile
	Meta
}

const profileCols = `id, seq, department_code, event, intro, duties, event_time, recorded_at, actor_ref, on_behalf_of, support_reason, idem_key`

func scanProfile(row pgx.Row) (ProfileRow, error) {
	var r ProfileRow
	err := row.Scan(&r.ID, &r.Seq, &r.Department, &r.Event, &r.Intro, &r.Duties, &r.EventTime, &r.RecordedAt, &r.ActorRef, &r.OnBehalfOf,
		&r.SupportReason, &r.IdemKey)
	return r, err
}

// InsertProfile ghi một phiên bản giới thiệu phòng.
func InsertProfile(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *ProfileRow) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.dept_profile (id, tenant_id, stream_key, department_code, event, intro, duties,
	    event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING seq, recorded_at`,
		r.ID, tid, "dept_profile:"+r.Department, r.Department, r.Event, r.Intro, r.Duties, r.EventTime.UTC(), r.ActorRef, r.OnBehalfOf,
		r.SupportReason, r.IdemKey, domain.RuleVersion).Scan(&r.Seq, &r.RecordedAt)
}

// ProfileHistory trả mọi phiên bản giới thiệu của phòng, mới nhất trước.
func ProfileHistory(ctx context.Context, tx pgx.Tx, dept string, limit int) ([]ProfileRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+profileCols+` FROM pf_extension.dept_profile WHERE department_code = $1
	  ORDER BY seq DESC LIMIT $2`, dept, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProfileRow{}
	for rows.Next() {
		r, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ProfileByIdem đọc phiên bản theo idem_key; không có → nil.
func ProfileByIdem(ctx context.Context, tx pgx.Tx, key string) (*ProfileRow, error) {
	r, err := scanProfile(tx.QueryRow(ctx, `SELECT `+profileCols+` FROM pf_extension.dept_profile WHERE idem_key = $1
	  ORDER BY recorded_at DESC LIMIT 1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ---------------------------------------------------------------------------
// Thư mục
// ---------------------------------------------------------------------------

// FolderRow là một phiên bản thư mục.
type FolderRow struct {
	Ref          uuid.UUID
	Department   string
	ParentRef    *uuid.UUID
	Name         string
	Hidden       bool
	StatusReason *string
	Meta
}

const folderCols = `id, seq, folder_ref, department_code, parent_ref, event, name, hidden, status_reason, event_time, recorded_at,
  actor_ref, on_behalf_of, support_reason, idem_key`

func scanFolder(row pgx.Row) (FolderRow, error) {
	var r FolderRow
	err := row.Scan(&r.ID, &r.Seq, &r.Ref, &r.Department, &r.ParentRef, &r.Event, &r.Name, &r.Hidden, &r.StatusReason, &r.EventTime,
		&r.RecordedAt, &r.ActorRef, &r.OnBehalfOf, &r.SupportReason, &r.IdemKey)
	return r, err
}

func collectFolders(rows pgx.Rows, err error) ([]FolderRow, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FolderRow{}
	for rows.Next() {
		r, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertFolder ghi một phiên bản thư mục.
func InsertFolder(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *FolderRow) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.dept_doc_folder (id, tenant_id, stream_key, folder_ref, department_code, parent_ref,
	    event, name, hidden, status_reason, event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING seq, recorded_at`,
		r.ID, tid, "dept_folder:"+r.Ref.String(), r.Ref, r.Department, r.ParentRef, r.Event, r.Name, r.Hidden, r.StatusReason,
		r.EventTime.UTC(), r.ActorRef, r.OnBehalfOf, r.SupportReason, r.IdemKey, domain.RuleVersion).Scan(&r.Seq, &r.RecordedAt)
}

// FoldersCurrent trả bản hiện hành của mọi thư mục của phòng (dept rỗng = mọi phòng).
func FoldersCurrent(ctx context.Context, tx pgx.Tx, dept string) ([]FolderRow, error) {
	return collectFolders(tx.Query(ctx, `SELECT `+folderCols+` FROM (
	    SELECT DISTINCT ON (folder_ref) * FROM pf_extension.dept_doc_folder WHERE ($1 = '' OR department_code = $1)
	    ORDER BY folder_ref, seq DESC) f ORDER BY name, folder_ref`, dept))
}

// FolderHistory trả mọi phiên bản của thư mục, mới nhất trước.
func FolderHistory(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]FolderRow, error) {
	return collectFolders(tx.Query(ctx, `SELECT `+folderCols+` FROM pf_extension.dept_doc_folder WHERE folder_ref = $1 ORDER BY seq DESC`, ref))
}

// FolderByIdem đọc phiên bản thư mục theo idem_key; không có → nil.
func FolderByIdem(ctx context.Context, tx pgx.Tx, key string) (*FolderRow, error) {
	r, err := scanFolder(tx.QueryRow(ctx, `SELECT `+folderCols+` FROM pf_extension.dept_doc_folder WHERE idem_key = $1
	  ORDER BY recorded_at DESC LIMIT 1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ---------------------------------------------------------------------------
// Văn bản
// ---------------------------------------------------------------------------

// File là tệp hiện hành của một phiên bản văn bản (ở kho đối tượng).
type File struct {
	Version     int
	ObjectKey   string
	Name        string
	ContentType string
	Size        int64
	SHA256      string
}

// DocRow là một phiên bản văn bản.
type DocRow struct {
	Ref        uuid.UUID
	Department string
	FolderRef  *uuid.UUID
	domain.DocMeta
	File
	Hidden       bool
	StatusReason *string
	Meta
}

const docCols = `id, seq, doc_ref, department_code, folder_ref, event, title, kind, field, doc_number, issuer, summary, issued_on,
  effective_from, effective_to, audience, file_version, object_key, file_name, content_type, size_bytes, sha256, hidden, status_reason,
  event_time, recorded_at, actor_ref, on_behalf_of, support_reason, idem_key`

func scanDoc(row pgx.Row) (DocRow, error) {
	var r DocRow
	var field, number, issuer, summary *string
	var fv int32
	err := row.Scan(&r.ID, &r.Seq, &r.Ref, &r.Department, &r.FolderRef, &r.Event, &r.Title, &r.Kind, &field, &number, &issuer, &summary,
		&r.IssuedOn, &r.EffectiveFrom, &r.EffectiveTo, &r.Audience, &fv, &r.ObjectKey, &r.Name, &r.ContentType, &r.Size, &r.SHA256,
		&r.Hidden, &r.StatusReason, &r.EventTime, &r.RecordedAt, &r.ActorRef, &r.OnBehalfOf, &r.SupportReason, &r.IdemKey)
	r.Field, r.Number, r.Issuer, r.Summary, r.Version = str(field), str(number), str(issuer), str(summary), int(fv)
	if r.Audience == nil {
		r.Audience = []string{}
	}
	return r, err
}

func collectDocs(rows pgx.Rows, err error) ([]DocRow, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocRow{}
	for rows.Next() {
		r, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertDoc ghi một phiên bản văn bản.
func InsertDoc(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *DocRow) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.dept_doc (id, tenant_id, stream_key, doc_ref, department_code, folder_ref, event,
	    title, kind, field, doc_number, issuer, summary, issued_on, effective_from, effective_to, audience, file_version, object_key,
	    file_name, content_type, size_bytes, sha256, hidden, status_reason, event_time, actor_ref, on_behalf_of, support_reason,
	    idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25,
	    $26, $27, $28, $29, $30, $31) RETURNING seq, recorded_at`,
		r.ID, tid, "dept_doc:"+r.Ref.String(), r.Ref, r.Department, r.FolderRef, r.Event, r.Title, r.Kind, optText(r.Field),
		optText(r.Number), optText(r.Issuer), optText(r.Summary), r.IssuedOn, r.EffectiveFrom, r.EffectiveTo, r.Audience, r.Version,
		r.ObjectKey, r.Name, r.ContentType, r.Size, r.SHA256, r.Hidden, r.StatusReason, r.EventTime.UTC(), r.ActorRef, r.OnBehalfOf,
		r.SupportReason, r.IdemKey, domain.RuleVersion).Scan(&r.Seq, &r.RecordedAt)
}

// DocsCurrent trả bản hiện hành của mọi văn bản của phòng (dept rỗng = mọi phòng), mới cập nhật trước. Lọc quyền,
// từ khóa, loại, hiệu lực làm ở ứng dụng (kho văn bản của một phòng cỡ vài nghìn dòng).
func DocsCurrent(ctx context.Context, tx pgx.Tx, dept string) ([]DocRow, error) {
	return collectDocs(tx.Query(ctx, `SELECT `+docCols+` FROM (
	    SELECT DISTINCT ON (doc_ref) * FROM pf_extension.dept_doc WHERE ($1 = '' OR department_code = $1)
	    ORDER BY doc_ref, seq DESC) d ORDER BY recorded_at DESC, doc_ref`, dept))
}

// DocHistory trả mọi phiên bản của văn bản, mới nhất trước (rỗng = không có).
func DocHistory(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]DocRow, error) {
	return collectDocs(tx.Query(ctx, `SELECT `+docCols+` FROM pf_extension.dept_doc WHERE doc_ref = $1 ORDER BY seq DESC`, ref))
}

// DocByIdem đọc phiên bản văn bản theo idem_key; không có → nil.
func DocByIdem(ctx context.Context, tx pgx.Tx, key string) (*DocRow, error) {
	r, err := scanDoc(tx.QueryRow(ctx, `SELECT `+docCols+` FROM pf_extension.dept_doc WHERE idem_key = $1
	  ORDER BY recorded_at DESC LIMIT 1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ---------------------------------------------------------------------------
// Số liệu thẻ trang chủ (chỉ đọc)
// ---------------------------------------------------------------------------

// Counts là các số đếm nghiệp vụ của phòng tại ngày today (giờ Việt Nam).
type Counts struct {
	SchedulesWeek    int // lịch công tác tuần này đã gửi/duyệt
	SchedulesPending int // lịch công tác chờ duyệt (thẻ "chờ duyệt" của Lãnh đạo phòng)
	AlertsOpen       int // CNTS: cảnh báo chưa đóng
	AlertsOverdue    int // CNTS: cảnh báo quá hạn xử lý
	OutbreaksActive  int // CNTS: ổ dịch chưa kết thúc
	TasksOpen        int // Trồng trọt: nhiệm vụ hỗ trợ đang mở
	TasksOverdue     int // Trồng trọt: nhiệm vụ quá hạn SLA
	FieldReportsWeek int // Trồng trọt: báo cáo hiện trường 7 ngày qua
	KHTCPeriodsYear  int // KHTC: kỳ báo cáo đã mở trong năm
	KHTCExports      int // KHTC: lần xuất báo cáo Mẫu 1–5
}

// CountsFor đếm số liệu thẻ của phòng dept (chỉ những bảng của phòng đó). week = thứ Hai của tuần hiện tại.
func CountsFor(ctx context.Context, tx pgx.Tx, dept string, week, now time.Time) (Counts, error) {
	var c Counts
	if err := tx.QueryRow(ctx, `SELECT
	    count(*) FILTER (WHERE week_start = $2 AND status IN ('pending', 'approved'))::int,
	    count(*) FILTER (WHERE status = 'pending')::int
	  FROM pf_extension.work_schedule_current WHERE department_code = $1`, dept, week).Scan(&c.SchedulesWeek, &c.SchedulesPending); err != nil {
		return c, err
	}
	switch dept {
	case "chan_nuoi_thuy_san":
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status <> 'closed')::int,
		    count(*) FILTER (WHERE status <> 'closed' AND overdue_at IS NOT NULL)::int
		  FROM pf_extension.cnts_alert_current WHERE department_code = $1`, dept).Scan(&c.AlertsOpen, &c.AlertsOverdue); err != nil {
			return c, err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM (
		    SELECT DISTINCT ON (outbreak_ref) event FROM pf_extension.cnts_outbreak ORDER BY outbreak_ref, seq DESC) o
		  WHERE event <> 'closed'`).Scan(&c.OutbreaksActive); err != nil {
			return c, err
		}
	case "trong_trot":
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status <> 'resolved')::int,
		    count(*) FILTER (WHERE status <> 'resolved' AND due_at < $1)::int
		  FROM pf_extension.task_current`, now).Scan(&c.TasksOpen, &c.TasksOverdue); err != nil {
			return c, err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM pf_extension.field_report_current WHERE updated_at >= $1`,
			now.AddDate(0, 0, -7)).Scan(&c.FieldReportsWeek); err != nil {
			return c, err
		}
	case "ke_hoach_tai_chinh":
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM pf_extension.khtc_period WHERE plan_year = $1`,
			week.Year()).Scan(&c.KHTCPeriodsYear); err != nil {
			return c, err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM pf_extension.khtc_report_export`).Scan(&c.KHTCExports); err != nil {
			return c, err
		}
	}
	return c, nil
}

// PriceInfo là tóm tắt bảng giá đã duyệt (Thông tin – Truyền thông, view công bố pf_price_pub — W11.76).
type PriceInfo struct {
	BoardDate *time.Time
	Items     int
	WeekKey   string
	Tickets   int
	WeekDays  int // số ngày đã duyệt của tuần
}

// PriceSummary đọc ngày bảng giá đã duyệt gần nhất (không đang mở duyệt) và số phiếu của tuần báo giá gần nhất. View
// chưa có (cụm pf_price chưa cài) → ok = false.
func PriceSummary(ctx context.Context, tx pgx.Tx, profile string) (PriceInfo, bool, error) {
	var p PriceInfo
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM (VALUES (to_regclass('pf_price_pub.v1_board_daily')),
	    (to_regclass('pf_price_pub.v1_ticket_week'))) v(c) WHERE c IS NOT NULL`).Scan(&n); err != nil {
		return p, false, err
	}
	if n != 2 {
		return p, false, nil
	}
	err := tx.QueryRow(ctx, `SELECT board_date, count(*)::int FROM pf_price_pub.v1_board_daily
	  WHERE profile = $1 AND NOT board_reopened GROUP BY board_date ORDER BY board_date DESC LIMIT 1`, profile).Scan(&p.BoardDate, &p.Items)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return p, false, err
	}
	err = tx.QueryRow(ctx, `SELECT week_key, sum(tickets_collected)::int, max(days_approved)::int FROM pf_price_pub.v1_ticket_week
	  WHERE profile = $1 GROUP BY week_key, week_to ORDER BY week_to DESC LIMIT 1`, profile).Scan(&p.WeekKey, &p.Tickets, &p.WeekDays)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return p, false, err
	}
	return p, true, nil
}
