package store

// W11.25 (pf 20261021122821): xếp loại năm, trạng thái xử lý kỳ nâng bậc, lần xuất Mẫu số 1/2/4, sổ quyết định, kết quả
// job tính mốc. Mọi bảng là sổ chỉ ghi thêm — không UPDATE/DELETE.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
)

// --- Xếp loại năm. ---

// RatingRow là một dòng sổ xếp loại năm.
type RatingRow struct {
	ID         uuid.UUID
	StaffID    uuid.UUID
	Year       int
	Rating     string
	Source     string
	Void       bool
	Supersedes *uuid.UUID
	Reason     string
	Actor      uuid.UUID
	RecordedAt time.Time
	Seq        int64
}

const ratingCols = `id, staff_id, year, coalesce(rating, ''), source, action = 'void', supersedes_id, coalesce(correction_reason, ''),
  actor_ref, recorded_at, seq`

func scanRating(r pgx.CollectableRow) (RatingRow, error) {
	var x RatingRow
	var y int16
	err := r.Scan(&x.ID, &x.StaffID, &y, &x.Rating, &x.Source, &x.Void, &x.Supersedes, &x.Reason, &x.Actor, &x.RecordedAt, &x.Seq)
	x.Year = int(y)
	return x, err
}

// Ratings trả mọi dòng xếp loại (staffID Nil = mọi hồ sơ), theo hồ sơ rồi seq.
func Ratings(ctx context.Context, tx pgx.Tx, staffID uuid.UUID) ([]RatingRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+ratingCols+` FROM pf_hrm.annual_rating WHERE ($1::uuid IS NULL OR staff_id = $1)
	  ORDER BY staff_id, seq`, nilable(staffID))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanRating)
}

// LockRating dùng chung cho nhập/chỉnh kết quả năm và chốt đợt đánh giá, trước đọc dòng bị thay.
func LockRating(ctx context.Context, tx pgx.Tx, tenant, staff uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_hrm.rating:' || $1::text || ':' || $2::text, 0))`, tenant, staff)
	return err
}

// InsertRating ghi một dòng xếp loại (thêm, thay — supersedes + lý do, hoặc hủy).
func InsertRating(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x RatingRow, at time.Time, idem string) (int64, error) {
	action := "add"
	var rating, reason *string
	if x.Void {
		action = "void"
	} else {
		rating = &x.Rating
	}
	if x.Supersedes != nil {
		reason = &x.Reason
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_hrm.annual_rating (id, tenant_id, stream_key, staff_id, year, rating, source, action,
	    supersedes_id, correction_reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'rating:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING seq`,
		x.ID, tid, x.StaffID, x.Year, rating, x.Source, action, x.Supersedes, reason, at.UTC(), x.Actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// EffectiveRatings chiếu sổ xếp loại: bỏ dòng hủy và dòng đã bị thay; mỗi (hồ sơ, năm) lấy dòng hiệu lực mới nhất.
func EffectiveRatings(rows []RatingRow) map[uuid.UUID]map[int]RatingRow {
	dead := map[uuid.UUID]bool{}
	for _, r := range rows {
		if r.Supersedes != nil {
			dead[*r.Supersedes] = true
		}
	}
	out := map[uuid.UUID]map[int]RatingRow{}
	for _, r := range rows {
		if r.Void || dead[r.ID] {
			continue
		}
		if out[r.StaffID] == nil {
			out[r.StaffID] = map[int]RatingRow{}
		}
		if cur, ok := out[r.StaffID][r.Year]; !ok || r.Seq > cur.Seq {
			out[r.StaffID][r.Year] = r
		}
	}
	return out
}

// --- Trạng thái xử lý. ---

// PromoEvent là một dòng sổ promotion_event.
type PromoEvent struct {
	ID         uuid.UUID
	StaffID    uuid.UUID
	CaseKey    string
	Kind       string
	Action     string
	DueOn      *time.Time
	AwardEvent *uuid.UUID
	Months     *int
	ExportID   *uuid.UUID
	DecisionID *uuid.UUID
	Reason     string
	Actor      uuid.UUID
	RecordedAt time.Time
	Seq        int64
}

const promoEventCols = `id, staff_id, case_key, kind, action, due_on, award_event_id, months, export_id, decision_id, coalesce(reason, ''),
  actor_ref, recorded_at, seq`

func scanPromoEvent(r pgx.CollectableRow) (PromoEvent, error) {
	var x PromoEvent
	var m *int16
	err := r.Scan(&x.ID, &x.StaffID, &x.CaseKey, &x.Kind, &x.Action, &x.DueOn, &x.AwardEvent, &m, &x.ExportID, &x.DecisionID, &x.Reason,
		&x.Actor, &x.RecordedAt, &x.Seq)
	if m != nil {
		v := int(*m)
		x.Months = &v
	}
	return x, err
}

// PromoEvents trả mọi dòng trạng thái (staffID Nil = mọi hồ sơ), theo hồ sơ rồi seq.
func PromoEvents(ctx context.Context, tx pgx.Tx, staffID uuid.UUID) ([]PromoEvent, error) {
	rows, err := tx.Query(ctx, `SELECT `+promoEventCols+` FROM pf_hrm.promotion_event WHERE ($1::uuid IS NULL OR staff_id = $1)
	  ORDER BY staff_id, seq`, nilable(staffID))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanPromoEvent)
}

// InsertPromoEvent ghi một dòng trạng thái (chuỗi 'promo:<staff_id>').
func InsertPromoEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x PromoEvent, at time.Time, idem string) (int64, error) {
	var due *string
	if x.DueOn != nil {
		s := x.DueOn.Format(time.DateOnly)
		due = &s
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_hrm.promotion_event (id, tenant_id, stream_key, staff_id, case_key, kind, action, due_on,
	    award_event_id, months, export_id, decision_id, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'promo:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7::date, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING seq`,
		x.ID, tid, x.StaffID, x.CaseKey, x.Kind, x.Action, due, x.AwardEvent, x.Months, x.ExportID, x.DecisionID, strPtr(x.Reason), at.UTC(),
		x.Actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// --- Lần xuất Mẫu số 1/2/4. ---

// PromoExport là một dòng sổ promotion_export.
type PromoExport struct {
	ID          uuid.UUID
	Action      string
	Form        string
	ReviewMonth time.Time
	AsOf        time.Time
	ExportID    *uuid.UUID
	KnownAt     *time.Time
	Params      map[string]string
	Items       []domain.ExportItem
	StaffIDs    []uuid.UUID
	FileName    string
	SHA256      string
	ByteSize    int64
	Drift       *bool
	Actor       uuid.UUID
	RecordedAt  time.Time
	Seq         int64
}

const promoExportCols = `id, action, form, review_month, as_of, export_id, known_at, params, items, staff_ids::text[], file_name, file_sha256,
  byte_size, drift, actor_ref, recorded_at, seq`

func scanPromoExport(r pgx.CollectableRow) (PromoExport, error) {
	var x PromoExport
	var params, items []byte
	var sids []string
	if err := r.Scan(&x.ID, &x.Action, &x.Form, &x.ReviewMonth, &x.AsOf, &x.ExportID, &x.KnownAt, &params, &items, &sids, &x.FileName,
		&x.SHA256, &x.ByteSize, &x.Drift, &x.Actor, &x.RecordedAt, &x.Seq); err != nil {
		return x, err
	}
	for _, s := range sids {
		u, err := uuid.Parse(s)
		if err != nil {
			return x, err
		}
		x.StaffIDs = append(x.StaffIDs, u)
	}
	x.Params = map[string]string{}
	if err := json.Unmarshal(params, &x.Params); err != nil {
		return x, err
	}
	return x, json.Unmarshal(items, &x.Items)
}

// InsertPromoExport ghi một lần xuất/tải lại (chuỗi 'promo-export:<form>').
func InsertPromoExport(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x PromoExport, at time.Time, idem string) (int64, error) {
	params, err := json.Marshal(x.Params)
	if err != nil {
		return 0, err
	}
	if x.Params == nil {
		params = []byte("{}")
	}
	items, err := json.Marshal(x.Items)
	if err != nil {
		return 0, err
	}
	if x.Items == nil {
		items = []byte("[]")
	}
	var seq int64
	err = tx.QueryRow(ctx, `INSERT INTO pf_hrm.promotion_export (id, tenant_id, stream_key, action, form, review_month, as_of, export_id,
	    known_at, params, items, staff_ids, file_name, file_sha256, byte_size, drift, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'promo-export:' || $4::text, $3, $4, $5::date, $6::date, $7, $8, $9::jsonb, $10::jsonb, $11::text[]::uuid[], $12, $13,
	    $14, $15, $16, $17, $18, $19) RETURNING seq`,
		x.ID, tid, x.Action, x.Form, x.ReviewMonth.Format(time.DateOnly), x.AsOf.Format(time.DateOnly), x.ExportID, x.KnownAt, string(params),
		string(items), uuidStrs(x.StaffIDs), x.FileName, x.SHA256, x.ByteSize, x.Drift, at.UTC(), x.Actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// PromoExports trả các lần xuất (action 'export'), mới trước, tối đa limit.
func PromoExports(ctx context.Context, tx pgx.Tx, limit int) ([]PromoExport, error) {
	rows, err := tx.Query(ctx, `SELECT `+promoExportCols+` FROM pf_hrm.promotion_export WHERE action = 'export'
	  ORDER BY recorded_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanPromoExport)
}

// PromoExportByID trả một dòng sổ xuất; ok = false nếu không có.
func PromoExportByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (PromoExport, bool, error) {
	rows, err := tx.Query(ctx, `SELECT `+promoExportCols+` FROM pf_hrm.promotion_export WHERE id = $1`, id)
	if err != nil {
		return PromoExport{}, false, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanPromoExport)
	if errors.Is(err, pgx.ErrNoRows) {
		return PromoExport{}, false, nil
	}
	return x, err == nil, err
}

// --- Sổ quyết định. ---

// DecisionPerson là một người có tên trong quyết định (JSON trong promotion_decision.people — không PII).
type DecisionPerson struct {
	StaffID      uuid.UUID  `json:"staff_id"`
	CaseKey      string     `json:"case_key,omitempty"`
	EffectiveOn  string     `json:"effective_on"`
	Grade        string     `json:"grade,omitempty"`
	FromStep     int        `json:"from_step,omitempty"`
	ToStep       int        `json:"to_step,omitempty"`
	FromCoeff    string     `json:"from_coeff,omitempty"`
	ToCoeff      string     `json:"to_coeff,omitempty"`
	PctFrom      string     `json:"pct_from,omitempty"`
	PctTo        string     `json:"pct_to,omitempty"`
	StepEventID  *uuid.UUID `json:"step_event_id,omitempty"` // dòng 'step' đã ghi vào staff_event (hủy → dòng 'void')
	AwardEventID *uuid.UUID `json:"award_event_id,omitempty"`
}

// DecisionRow là một dòng sổ quyết định.
type DecisionRow struct {
	ID         uuid.UUID
	DecisionID uuid.UUID
	Action     string
	Kind       string
	No         string
	DecidedOn  time.Time
	Issuer     string
	ExportID   *uuid.UUID
	DocID      *uuid.UUID
	People     []DecisionPerson
	StaffIDs   []uuid.UUID
	Supersedes *uuid.UUID
	Reason     string
	Actor      uuid.UUID
	RecordedAt time.Time
	Seq        int64
}

const decisionCols = `id, decision_id, action, kind, decision_no, decided_on, issuer, export_id, doc_id, people, staff_ids::text[],
  supersedes_id, coalesce(correction_reason, ''), actor_ref, recorded_at, seq`

func scanDecision(r pgx.CollectableRow) (DecisionRow, error) {
	var x DecisionRow
	var people []byte
	var sids []string
	if err := r.Scan(&x.ID, &x.DecisionID, &x.Action, &x.Kind, &x.No, &x.DecidedOn, &x.Issuer, &x.ExportID, &x.DocID, &people, &sids,
		&x.Supersedes, &x.Reason, &x.Actor, &x.RecordedAt, &x.Seq); err != nil {
		return x, err
	}
	for _, s := range sids {
		u, err := uuid.Parse(s)
		if err != nil {
			return x, err
		}
		x.StaffIDs = append(x.StaffIDs, u)
	}
	return x, json.Unmarshal(people, &x.People)
}

// Decisions trả mọi dòng sổ quyết định (decisionID Nil = mọi quyết định), theo quyết định rồi seq.
func Decisions(ctx context.Context, tx pgx.Tx, decisionID uuid.UUID) ([]DecisionRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+decisionCols+` FROM pf_hrm.promotion_decision WHERE ($1::uuid IS NULL OR decision_id = $1)
	  ORDER BY recorded_at, seq`, nilable(decisionID))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanDecision)
}

// LockDecisions khóa ghi sổ quyết định của tenant trong giao dịch (kiểm số trùng, hủy theo thứ tự).
func LockDecisions(ctx context.Context, tx pgx.Tx, tid uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_hrm.promotion_decision:' || $1::text, 0))`, tid)
	return err
}

// InsertDecision ghi một dòng sổ quyết định (chuỗi 'decision:<decision_id>').
func InsertDecision(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x DecisionRow, at time.Time, idem string) (int64, error) {
	people, err := json.Marshal(x.People)
	if err != nil {
		return 0, err
	}
	if x.People == nil {
		people = []byte("[]")
	}
	var reason *string
	if x.Supersedes != nil {
		reason = &x.Reason
	}
	var seq int64
	err = tx.QueryRow(ctx, `INSERT INTO pf_hrm.promotion_decision (id, tenant_id, stream_key, decision_id, action, kind, decision_no,
	    decided_on, issuer, export_id, doc_id, people, staff_ids, supersedes_id, correction_reason, event_time, actor_ref, idem_key,
	    rule_version)
	  VALUES ($1, $2, 'decision:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7::date, $8, $9, $10, $11::jsonb, $12::text[]::uuid[], $13, $14,
	    $15, $16, $17, $18) RETURNING seq`,
		x.ID, tid, x.DecisionID, x.Action, x.Kind, x.No, x.DecidedOn.Format(time.DateOnly), x.Issuer, x.ExportID, x.DocID, string(people),
		uuidStrs(x.StaffIDs), x.Supersedes, reason, at.UTC(), x.Actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// --- Kết quả job. ---

// ScanRow là một lần chạy job tính mốc.
type ScanRow struct {
	Day        time.Time
	Counts     map[string]int
	RecordedAt time.Time
}

// InsertScan ghi kết quả job (chuỗi 'scan'); idem theo ngày → chạy lại cùng ngày đụng khóa trùng.
func InsertScan(ctx context.Context, tx pgx.Tx, id, tid, actor uuid.UUID, day time.Time, counts map[string]int, at time.Time, idem string) error {
	b, err := json.Marshal(counts)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_hrm.promotion_scan (id, tenant_id, stream_key, scan_day, counts, event_time, actor_ref, idem_key,
	    rule_version) VALUES ($1, $2, 'scan', $3::date, $4::jsonb, $5, $6, $7, $8)`, id, tid, day.Format(time.DateOnly), string(b), at.UTC(),
		actor, idem, domain.RuleVersion)
	return err
}

// LastScan trả lần chạy job gần nhất; ok = false nếu chưa chạy.
func LastScan(ctx context.Context, tx pgx.Tx) (ScanRow, bool, error) {
	var x ScanRow
	var b []byte
	err := tx.QueryRow(ctx, `SELECT scan_day, counts, recorded_at FROM pf_hrm.promotion_scan ORDER BY recorded_at DESC LIMIT 1`).
		Scan(&x.Day, &b, &x.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, false, nil
	}
	if err != nil {
		return x, false, err
	}
	return x, true, json.Unmarshal(b, &x.Counts)
}

// ScanTenants trả tenant có hồ sơ nhân sự (job — pool vai ag_ops_jobs, BYPASSRLS, chỉ đọc cột tenant_id).
func ScanTenants(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT tenant_id FROM pf_hrm.staff`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
