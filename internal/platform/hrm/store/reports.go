package store

// W11.28 (pf 20261014073846): sổ báo cáo Sở (report_log) và truy vấn theo THỜI ĐIỂM BIẾT (known) để dựng lại đúng một lần
// xuất — sự kiện hồ sơ, danh mục, tham số ghi sổ sau known bị bỏ qua. known nil = hiện tại.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/domain"
)

// CurrentStaffKnown trả phiên bản đầu hồ sơ mới nhất đã ghi tới known (nil = hiện tại), sắp theo mã.
func CurrentStaffKnown(ctx context.Context, tx pgx.Tx, known *time.Time) ([]StaffRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+staffCols+` FROM (
	    SELECT DISTINCT ON (staff_id) `+staffCols+` FROM pf_hrm.staff
	     WHERE ($1::timestamptz IS NULL OR recorded_at <= $1) ORDER BY staff_id, seq DESC) v ORDER BY code`, known)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanStaff)
}

// CatalogKnown trả phiên bản mới nhất của mọi mục danh mục đã ghi tới known (nil = hiện tại), kể cả mục đã ngừng (biểu
// cũ vẫn in được tên).
func CatalogKnown(ctx context.Context, tx pgx.Tx, known *time.Time) ([]ItemRow, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (kind, code) `+itemCols+` FROM pf_hrm.catalog_item
	  WHERE ($1::timestamptz IS NULL OR recorded_at <= $1) ORDER BY kind, code, seq DESC`, known)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanItem)
}

// RuleAtKnown là RuleAt chỉ xét phiên bản đã ghi tới known (nil = hiện tại).
func RuleAtKnown(ctx context.Context, tx pgx.Tx, key string, at time.Time, known *time.Time) (RuleRow, bool, error) {
	rows, err := tx.Query(ctx, `SELECT `+ruleCols+` FROM pf_hrm.rule_param WHERE key = $1 AND valid_from <= $2::date
	    AND ($3::timestamptz IS NULL OR recorded_at <= $3)
	  ORDER BY valid_from DESC, seq DESC LIMIT 1`, key, at.Format(time.DateOnly), known)
	if err != nil {
		return RuleRow{}, false, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanRule)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuleRow{}, false, nil
	}
	return x, err == nil, err
}

// CheckSummary là kết quả một quy tắc kiểm tra lưu ở sổ — không PII (chỉ số người).
type CheckSummary struct {
	Rule  string `json:"rule"`
	Level string `json:"level"`
	OK    bool   `json:"ok"`
	Count int    `json:"count"`
}

// ReportLog là một dòng sổ báo cáo Sở.
type ReportLog struct {
	ID          uuid.UUID
	Action      string
	Report      string
	AsOf        time.Time
	Period      string
	ExportID    *uuid.UUID
	KnownAt     *time.Time
	Params      map[string]string
	StaffIDs    []uuid.UUID
	Sensitive   []string
	ItemCount   int
	FileName    string
	SHA256      string
	ByteSize    int64
	CheckStatus string
	Checks      []CheckSummary
	Drift       *bool
	Actor       uuid.UUID
	RecordedAt  time.Time
	Seq         int64
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// InsertReportLog ghi một dòng sổ báo cáo (chuỗi 'report:<NSxx>').
func InsertReportLog(ctx context.Context, tx pgx.Tx, tid uuid.UUID, l ReportLog, at time.Time, idem string) (int64, error) {
	params, err := json.Marshal(l.Params)
	if err != nil {
		return 0, err
	}
	if l.Params == nil {
		params = []byte("{}")
	}
	checks, err := json.Marshal(l.Checks)
	if err != nil {
		return 0, err
	}
	if l.Checks == nil {
		checks = []byte("[]")
	}
	sens := l.Sensitive
	if sens == nil {
		sens = []string{}
	}
	ids := uuidStrs(l.StaffIDs)
	if ids == nil {
		ids = []string{}
	}
	var size *int64
	if l.ByteSize > 0 {
		size = &l.ByteSize
	}
	var seq int64
	err = tx.QueryRow(ctx, `INSERT INTO pf_hrm.report_log (id, tenant_id, stream_key, action, report_code, as_of, period_label, export_id,
	    known_at, params, staff_ids, sensitive_fields, item_count, file_name, file_sha256, byte_size, check_status, checks, drift,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'report:' || $4::text, $3, $4, $5::date, $6, $7, $8, $9::jsonb, $10::text[]::uuid[], $11, $12, $13, $14, $15, $16,
	    $17::jsonb, $18, $19, $20, $21, $22) RETURNING seq`,
		l.ID, tid, l.Action, l.Report, l.AsOf.Format(time.DateOnly), l.Period, l.ExportID, l.KnownAt, string(params), ids, sens,
		l.ItemCount, strPtr(l.FileName), strPtr(l.SHA256), size, strPtr(l.CheckStatus), string(checks), l.Drift, at.UTC(), l.Actor, idem,
		domain.RuleVersion).Scan(&seq)
	return seq, err
}

const reportLogCols = `id, action, report_code, as_of, period_label, export_id, known_at, params, staff_ids::text[], sensitive_fields,
  item_count, coalesce(file_name, ''), coalesce(file_sha256, ''), coalesce(byte_size, 0), coalesce(check_status, ''), checks, drift,
  actor_ref, recorded_at, seq`

func scanReportLog(r pgx.CollectableRow) (ReportLog, error) {
	var l ReportLog
	var params, checks []byte
	var sids []string
	if err := r.Scan(&l.ID, &l.Action, &l.Report, &l.AsOf, &l.Period, &l.ExportID, &l.KnownAt, &params, &sids, &l.Sensitive, &l.ItemCount,
		&l.FileName, &l.SHA256, &l.ByteSize, &l.CheckStatus, &checks, &l.Drift, &l.Actor, &l.RecordedAt, &l.Seq); err != nil {
		return l, err
	}
	for _, x := range sids {
		u, err := uuid.Parse(x)
		if err != nil {
			return l, err
		}
		l.StaffIDs = append(l.StaffIDs, u)
	}
	l.Params = map[string]string{}
	_ = json.Unmarshal(params, &l.Params)
	_ = json.Unmarshal(checks, &l.Checks)
	return l, nil
}

// ReportLogs trả dòng sổ của biểu report (rỗng = mọi biểu) có action thuộc actions (rỗng = mọi), mới trước, tối đa limit.
func ReportLogs(ctx context.Context, tx pgx.Tx, report string, actions []string, limit int) ([]ReportLog, error) {
	if actions == nil {
		actions = []string{}
	}
	rows, err := tx.Query(ctx, `SELECT `+reportLogCols+` FROM pf_hrm.report_log
	  WHERE ($1 = '' OR report_code = $1) AND (cardinality($2::text[]) = 0 OR action = ANY($2))
	  ORDER BY recorded_at DESC, id DESC LIMIT $3`, report, actions, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanReportLog)
}

// ReportLogByID trả một dòng sổ; ok = false nếu không có.
func ReportLogByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (ReportLog, bool, error) {
	rows, err := tx.Query(ctx, `SELECT `+reportLogCols+` FROM pf_hrm.report_log WHERE id = $1`, id)
	if err != nil {
		return ReportLog{}, false, err
	}
	l, err := pgx.CollectExactlyOneRow(rows, scanReportLog)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReportLog{}, false, nil
	}
	return l, err == nil, err
}
