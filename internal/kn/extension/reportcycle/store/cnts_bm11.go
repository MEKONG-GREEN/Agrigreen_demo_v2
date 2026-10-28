package store

// W11.45: SQL của Báo cáo phòng CNTS BM-11 (pf 20261028034853 — pf_extension.cnts_bm11_report chỉ INSERT,
// pf_extension.cnts_bm11_event sổ chỉ ghi thêm). Luôn trong giao dịch có ngữ cảnh tenant.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
)

// BM11Report là đầu báo cáo.
type BM11Report struct {
	ID          uuid.UUID
	Province    string
	Type        string
	PeriodKey   string
	From, To    time.Time
	Label       string
	Recipients  []string
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
	IdemKey     string
	Last        *BM11Event // bước cuối (danh sách)
	LastContent *BM11Event // bước cuối có nội dung (danh sách: không nạp nội dung)
}

// BM11Event là một bước của sổ.
type BM11Event struct {
	ID          uuid.UUID
	ReportID    uuid.UUID
	Seq         int64
	Event       string
	Status      string
	Content     json.RawMessage // nil với bước không mang nội dung
	ContentHash string
	Comment     string
	Recipient   string
	ActorRef    uuid.UUID
	At          time.Time
	IdemKey     string
}

const bm11ReportCols = `id, province_code, report_type, period_key, period_from, period_to, period_label, recipients, created_by, created_at, idem_key`

func scanBM11Report(row pgx.Row) (BM11Report, error) {
	var r BM11Report
	err := row.Scan(&r.ID, &r.Province, &r.Type, &r.PeriodKey, &r.From, &r.To, &r.Label, &r.Recipients, &r.CreatedBy, &r.CreatedAt, &r.IdemKey)
	return r, err
}

// LockBM11 giữ khóa tư vấn của báo cáo tới hết giao dịch (cùng khóa với trigger máy trạng thái).
func LockBM11(ctx context.Context, tx pgx.Tx, tid, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pf_extension.fn_cnts_bm11_lock($1, $2)`, tid, id)
	return err
}

// LockBM11Period giữ khóa tư vấn của (tỉnh, loại, kỳ) khi tạo — hai lần tạo đồng thời xếp hàng.
func LockBM11Period(ctx context.Context, tx pgx.Tx, tid uuid.UUID, province, typ, key string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('cnts_bm11_period:' || $1::text || ':' || $2::text || ':' || $3::text || ':' || $4::text, 0))`,
		tid.String(), province, typ, key)
	return err
}

// InsertBM11Report ghi đầu báo cáo (trùng tỉnh × loại × kỳ → 23505 cnts_bm11_report_period).
func InsertBM11Report(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *BM11Report) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_bm11_report (tenant_id, id, province_code, report_type, period_key, period_from,
	    period_to, period_label, recipients, created_by, created_at, idem_key)
	  VALUES ($1, $2, $3, $4, $5, $6::date, $7::date, $8, $9, $10, $11, $12) RETURNING created_at`,
		tid, r.ID, r.Province, r.Type, r.PeriodKey, r.From.Format(time.DateOnly), r.To.Format(time.DateOnly), r.Label, r.Recipients,
		r.CreatedBy, r.CreatedAt.UTC(), r.IdemKey).Scan(&r.CreatedAt)
}

// BM11ReportByID đọc đầu báo cáo (nil nếu không có).
func BM11ReportByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*BM11Report, error) {
	r, err := scanBM11Report(tx.QueryRow(ctx, `SELECT `+bm11ReportCols+` FROM pf_extension.cnts_bm11_report WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// BM11ReportByIdem đọc đầu báo cáo theo khóa idem của lần tạo (nil nếu chưa có).
func BM11ReportByIdem(ctx context.Context, tx pgx.Tx, idem string) (*BM11Report, error) {
	r, err := scanBM11Report(tx.QueryRow(ctx, `SELECT `+bm11ReportCols+` FROM pf_extension.cnts_bm11_report WHERE idem_key = $1`, idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// BM11ReportByPeriod đọc báo cáo của tỉnh × loại × kỳ (nil nếu chưa có).
func BM11ReportByPeriod(ctx context.Context, tx pgx.Tx, province, typ, key string) (*BM11Report, error) {
	r, err := scanBM11Report(tx.QueryRow(ctx, `SELECT `+bm11ReportCols+` FROM pf_extension.cnts_bm11_report
	  WHERE province_code = $1 AND report_type = $2 AND period_key = $3`, province, typ, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// BM11Filter lọc danh sách. Province rỗng = mọi tỉnh (Admin/Cấp Quốc gia).
type BM11Filter struct {
	Province  string
	Type      string
	Recipient string // khác rỗng: chỉ báo cáo có nơi nhận này
	Limit     int
}

// ListBM11 liệt kê báo cáo (mới nhất trước) kèm bước cuối (không nội dung).
func ListBM11(ctx context.Context, tx pgx.Tx, f BM11Filter) ([]BM11Report, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 500
	}
	rows, err := tx.Query(ctx, `SELECT r.id, r.province_code, r.report_type, r.period_key, r.period_from, r.period_to, r.period_label, r.recipients,
	    r.created_by, r.created_at, r.idem_key, e.id, e.seq, e.event, e.status, coalesce(e.comment, ''), e.actor_ref, e.event_time
	  FROM pf_extension.cnts_bm11_report r
	  JOIN LATERAL (SELECT * FROM pf_extension.cnts_bm11_event x WHERE x.tenant_id = r.tenant_id AND x.report_id = r.id
	                ORDER BY x.seq DESC LIMIT 1) e ON true
	 WHERE ($1::text = '' OR r.province_code = $1::text) AND ($2::text = '' OR r.report_type = $2::text)
	   AND ($3::text = '' OR $3::text = ANY (r.recipients))
	 ORDER BY r.period_to DESC, r.report_type, r.id DESC LIMIT $4`, f.Province, f.Type, f.Recipient, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (BM11Report, error) {
		var r BM11Report
		var e BM11Event
		err := row.Scan(&r.ID, &r.Province, &r.Type, &r.PeriodKey, &r.From, &r.To, &r.Label, &r.Recipients, &r.CreatedBy, &r.CreatedAt,
			&r.IdemKey, &e.ID, &e.Seq, &e.Event, &e.Status, &e.Comment, &e.ActorRef, &e.At)
		e.ReportID = r.ID
		r.Last = &e
		return r, err
	})
}

// LastSentBM11 trả báo cáo đã gửi gần nhất của tỉnh × loại có kỳ kết thúc trước before (nil nếu không có).
func LastSentBM11(ctx context.Context, tx pgx.Tx, province, typ string, before time.Time) (*BM11Report, error) {
	r, err := scanBM11Report(tx.QueryRow(ctx, `SELECT `+bm11ReportCols+` FROM pf_extension.cnts_bm11_report r
	  WHERE province_code = $1 AND report_type = $2 AND period_to < $3::date
	    AND EXISTS (SELECT 1 FROM pf_extension.cnts_bm11_event e WHERE e.tenant_id = r.tenant_id AND e.report_id = r.id AND e.event = 'approved')
	  ORDER BY period_to DESC LIMIT 1`, province, typ, before.Format(time.DateOnly)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

const bm11EventCols = `id, report_id, seq, event, status, content, coalesce(content_hash, ''), coalesce(comment, ''), coalesce(recipient, ''),
  actor_ref, event_time, idem_key`

func scanBM11Event(row pgx.Row) (BM11Event, error) {
	var e BM11Event
	var content []byte
	err := row.Scan(&e.ID, &e.ReportID, &e.Seq, &e.Event, &e.Status, &content, &e.ContentHash, &e.Comment, &e.Recipient, &e.ActorRef, &e.At,
		&e.IdemKey)
	if content != nil {
		e.Content = json.RawMessage(content)
	}
	return e, err
}

// BM11Events trả mọi bước của báo cáo (cũ trước).
func BM11Events(ctx context.Context, tx pgx.Tx, report uuid.UUID) ([]BM11Event, error) {
	rows, err := tx.Query(ctx, `SELECT `+bm11EventCols+` FROM pf_extension.cnts_bm11_event WHERE report_id = $1 ORDER BY seq`, report)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (BM11Event, error) { return scanBM11Event(r) })
}

// BM11EventByIdem đọc bước đã ghi với khóa idem (nil nếu chưa có).
func BM11EventByIdem(ctx context.Context, tx pgx.Tx, idem string) (*BM11Event, error) {
	e, err := scanBM11Event(tx.QueryRow(ctx, `SELECT `+bm11EventCols+` FROM pf_extension.cnts_bm11_event WHERE idem_key = $1`, idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

// InsertBM11Event ghi một bước (trigger kiểm máy trạng thái — check_violation cnts_bm11_state / cnts_bm11_received).
func InsertBM11Event(ctx context.Context, tx pgx.Tx, tid uuid.UUID, province string, e *BM11Event) error {
	var content, hash, comment, recipient *string
	if e.Content != nil {
		s := string(e.Content)
		content, hash = &s, &e.ContentHash
	}
	if e.Comment != "" {
		comment = &e.Comment
	}
	if e.Recipient != "" {
		recipient = &e.Recipient
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_bm11_event (id, tenant_id, stream_key, report_id, province_code, event, status,
	    content, content_hash, comment, recipient, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12, $13, $14, $15) RETURNING seq`,
		e.ID, tid, "cnts_bm11:"+e.ReportID.String(), e.ReportID, province, e.Event, e.Status, content, hash, comment, recipient, e.At.UTC(), e.ActorRef, e.IdemKey,
		domain.BM11RuleVersion).Scan(&e.Seq)
}
