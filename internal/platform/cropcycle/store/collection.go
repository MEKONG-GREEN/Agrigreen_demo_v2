package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// ---------------------------------------------------------------------------
// Sổ nhận van (collection_valve_receipt)
// ---------------------------------------------------------------------------

// ValveReceipt là một dòng sổ nhận van collection_schedule / htx_collection_statement.
type ValveReceipt struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	Seq            int64
	Contract       string
	SubjectRef     uuid.UUID
	Revision       int
	CooperativeRef uuid.UUID
	SourceEventID  uuid.UUID
	SourceTenantID uuid.UUID
	Payload        json.RawMessage
	BecameCurrent  bool
	EventTime      time.Time
	RecordedAt     time.Time
	ActorRef       uuid.UUID
	IdemKey        string
}

// ReceiptIdemKey là khóa chống trùng của sổ nhận: (hợp đồng, ref, revision).
func ReceiptIdemKey(contract string, ref uuid.UUID, revision int) string {
	return "valve:" + contract + ":" + ref.String() + ":" + strconv.Itoa(revision)
}

// ReceiptExists cho biết (hợp đồng, ref, revision) đã được nhận.
func ReceiptExists(ctx context.Context, tx pgx.Tx, key string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_cropcycle.collection_valve_receipt WHERE idem_key = $1::text)`, key).Scan(&ok)
	return ok, err
}

// InsertReceipt ghi sổ nhận; DB tính seq và băm.
func InsertReceipt(ctx context.Context, tx pgx.Tx, r *ValveReceipt) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_cropcycle.collection_valve_receipt
	    (id, tenant_id, stream_key, contract, subject_ref, revision, cooperative_ref, source_event_id, source_tenant_id, payload,
	     became_current, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::text, $5::uuid, $6::int, $7::uuid, $8::uuid, $9::uuid, $10::jsonb,
	     $11::boolean, $12::timestamptz, $13::uuid, $14::text, $15::text)
	  RETURNING seq, recorded_at`,
		r.ID, r.TenantID, "valve:"+r.Contract+":"+r.SubjectRef.String(), r.Contract, r.SubjectRef, r.Revision, r.CooperativeRef,
		r.SourceEventID, r.SourceTenantID, string(r.Payload), r.BecameCurrent, r.EventTime.UTC(), r.ActorRef, r.IdemKey,
		domain.RuleCollectionValve).Scan(&r.Seq, &r.RecordedAt)
}

// ---------------------------------------------------------------------------
// Bảng chiếu lịch gom
// ---------------------------------------------------------------------------

// ScheduleView là một dòng collection_schedule_view.
type ScheduleView struct {
	ScheduleRef    uuid.UUID
	CooperativeRef uuid.UUID
	CommuneCode    string
	WindowFrom     time.Time
	WindowTo       time.Time
	Status         string
	Revision       int
	ReceiptID      uuid.UUID
	SourceTenantID uuid.UUID
	PublishedAt    time.Time
	ReceivedAt     time.Time
	SyncSeq        int64
	UpdatedAt      time.Time
}

const scheduleCols = `schedule_ref, cooperative_ref, commune_code, window_from, window_to, status, revision, receipt_id,
  source_tenant_id, published_at, received_at, sync_seq, updated_at`

func scanSchedule(row pgx.Row) (ScheduleView, error) {
	var v ScheduleView
	err := row.Scan(&v.ScheduleRef, &v.CooperativeRef, &v.CommuneCode, &v.WindowFrom, &v.WindowTo, &v.Status, &v.Revision,
		&v.ReceiptID, &v.SourceTenantID, &v.PublishedAt, &v.ReceivedAt, &v.SyncSeq, &v.UpdatedAt)
	return v, err
}

// ScheduleByRef đọc lịch gom; lock = FOR UPDATE. nil nếu chưa có.
func ScheduleByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID, lock bool) (*ScheduleView, error) {
	q := "SELECT " + scheduleCols + " FROM pf_cropcycle.collection_schedule_view WHERE schedule_ref = $1::uuid"
	if lock {
		q += " FOR UPDATE"
	}
	v, err := scanSchedule(tx.QueryRow(ctx, q, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// PutSchedule thêm hoặc thay lịch gom (sync_seq mới sau khóa tư vấn — thứ tự commit).
func PutSchedule(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v ScheduleView) error {
	if err := lockSync(ctx, tx, "collection"); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_cropcycle.collection_schedule_view
	    (tenant_id, schedule_ref, cooperative_ref, commune_code, window_from, window_to, status, revision, receipt_id,
	     source_tenant_id, published_at, received_at)
	  VALUES ($1::uuid, $2::uuid, $3::uuid, $4::text, $5::date, $6::date, $7::text, $8::int, $9::uuid, $10::uuid, $11::timestamptz, $12::timestamptz)
	  ON CONFLICT (tenant_id, schedule_ref) DO UPDATE SET
	    cooperative_ref = EXCLUDED.cooperative_ref, commune_code = EXCLUDED.commune_code, window_from = EXCLUDED.window_from,
	    window_to = EXCLUDED.window_to, status = EXCLUDED.status, revision = EXCLUDED.revision, receipt_id = EXCLUDED.receipt_id,
	    source_tenant_id = EXCLUDED.source_tenant_id, published_at = EXCLUDED.published_at, received_at = EXCLUDED.received_at,
	    sync_seq = nextval('pf_cropcycle.collection_sync_seq'), updated_at = now()`,
		tid, v.ScheduleRef, v.CooperativeRef, v.CommuneCode, day(v.WindowFrom), day(v.WindowTo), v.Status, v.Revision, v.ReceiptID,
		v.SourceTenantID, v.PublishedAt.UTC(), v.ReceivedAt.UTC())
	return err
}

// CollectionFilter là phạm vi đọc lịch gom / bảng kê: Coop ≠ Nil = một HTX; Units ≠ nil
// = các xã (cán bộ KN, chỉ lịch gom); cả hai rỗng + All = toàn tenant.
type CollectionFilter struct {
	All   bool
	Coop  uuid.UUID
	Units []string
}

func (f CollectionFilter) args() (bool, uuid.UUID, bool, []string) {
	return f.All, f.Coop, f.Units != nil, strs(f.Units)
}

// ListSchedules liệt kê lịch gom mới nhất trước (schedule_ref UUIDv7 giảm dần).
func ListSchedules(ctx context.Context, tx pgx.Tx, f CollectionFilter, status string, after uuid.UUID, limit int) ([]ScheduleView, error) {
	all, coop, byUnits, units := f.args()
	rows, err := tx.Query(ctx, "SELECT "+scheduleCols+` FROM pf_cropcycle.collection_schedule_view
	    WHERE ($1::boolean OR cooperative_ref = $2::uuid OR ($3::boolean AND commune_code = ANY($4::text[])))
	      AND ($5::text = '' OR status = $5::text)
	      AND ($6::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR schedule_ref < $6::uuid)
	    ORDER BY schedule_ref DESC LIMIT $7::int`, all, coop, byUnits, units, status, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduleView{}
	for rows.Next() {
		v, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SchedulesChangedAfter trả lịch gom của một HTX đổi sau con trỏ đồng bộ.
func SchedulesChangedAfter(ctx context.Context, tx pgx.Tx, coop uuid.UUID, all bool, after int64, limit int) ([]ScheduleView, error) {
	rows, err := tx.Query(ctx, "SELECT "+scheduleCols+` FROM pf_cropcycle.collection_schedule_view
	    WHERE ($1::boolean OR cooperative_ref = $2::uuid) AND sync_seq > $3::bigint ORDER BY sync_seq LIMIT $4::int`, all, coop, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduleView{}
	for rows.Next() {
		v, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Bảng chiếu bảng kê
// ---------------------------------------------------------------------------

// StatementView là một dòng collection_statement_view.
type StatementView struct {
	StatementRef     uuid.UUID
	CooperativeRef   uuid.UUID
	PlotRef          uuid.UUID
	SeasonRef        *uuid.UUID
	CollectedOn      time.Time
	Rolls            int
	Revision         int
	ReceiptID        uuid.UUID
	SourceTenantID   uuid.UUID
	PublishedAt      time.Time
	ReceivedAt       time.Time
	ResponseID       *uuid.UUID
	ResponseRevision *int
	ResponseOutcome  *string
	ResponseHTXRolls *int
	ResponseReason   *string
	RespondedAt      *time.Time
	SyncSeq          int64
	UpdatedAt        time.Time
}

// Pending cho biết HTX chưa phản hồi revision hiện hành.
func (v StatementView) Pending() bool {
	return v.ResponseRevision == nil || *v.ResponseRevision != v.Revision
}

const statementCols = `statement_ref, cooperative_ref, plot_ref, season_ref, collected_on, rolls, revision, receipt_id,
  source_tenant_id, published_at, received_at, response_id, response_revision, response_outcome, response_htx_rolls,
  response_reason, responded_at, sync_seq, updated_at`

func scanStatement(row pgx.Row) (StatementView, error) {
	var v StatementView
	err := row.Scan(&v.StatementRef, &v.CooperativeRef, &v.PlotRef, &v.SeasonRef, &v.CollectedOn, &v.Rolls, &v.Revision,
		&v.ReceiptID, &v.SourceTenantID, &v.PublishedAt, &v.ReceivedAt, &v.ResponseID, &v.ResponseRevision, &v.ResponseOutcome,
		&v.ResponseHTXRolls, &v.ResponseReason, &v.RespondedAt, &v.SyncSeq, &v.UpdatedAt)
	return v, err
}

// StatementByRef đọc bảng kê; lock = FOR UPDATE. nil nếu chưa có.
func StatementByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID, lock bool) (*StatementView, error) {
	q := "SELECT " + statementCols + " FROM pf_cropcycle.collection_statement_view WHERE statement_ref = $1::uuid"
	if lock {
		q += " FOR UPDATE"
	}
	v, err := scanStatement(tx.QueryRow(ctx, q, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// PutStatement thêm hoặc thay bảng kê (giữ nguyên phản hồi hiện hành — phản hồi cho
// revision cũ vẫn hiển thị, bảng kê trở lại "chờ phản hồi" theo revision mới).
func PutStatement(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v StatementView) error {
	if err := lockSync(ctx, tx, "collection"); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_cropcycle.collection_statement_view
	    (tenant_id, statement_ref, cooperative_ref, plot_ref, season_ref, collected_on, rolls, revision, receipt_id,
	     source_tenant_id, published_at, received_at)
	  VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::date, $7::int, $8::int, $9::uuid, $10::uuid, $11::timestamptz, $12::timestamptz)
	  ON CONFLICT (tenant_id, statement_ref) DO UPDATE SET
	    cooperative_ref = EXCLUDED.cooperative_ref, plot_ref = EXCLUDED.plot_ref, season_ref = EXCLUDED.season_ref,
	    collected_on = EXCLUDED.collected_on, rolls = EXCLUDED.rolls, revision = EXCLUDED.revision, receipt_id = EXCLUDED.receipt_id,
	    source_tenant_id = EXCLUDED.source_tenant_id, published_at = EXCLUDED.published_at, received_at = EXCLUDED.received_at,
	    sync_seq = nextval('pf_cropcycle.collection_sync_seq'), updated_at = now()`,
		tid, v.StatementRef, v.CooperativeRef, v.PlotRef, v.SeasonRef, day(v.CollectedOn), v.Rolls, v.Revision, v.ReceiptID,
		v.SourceTenantID, v.PublishedAt.UTC(), v.ReceivedAt.UTC())
	return err
}

// SetStatementResponse gắn phản hồi hiện hành vào bảng kê.
func SetStatementResponse(ctx context.Context, tx pgx.Tx, r Response) error {
	if err := lockSync(ctx, tx, "collection"); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE pf_cropcycle.collection_statement_view SET
	    response_id = $2::uuid, response_revision = $3::int, response_outcome = $4::text, response_htx_rolls = $5::int,
	    response_reason = $6::text, responded_at = $7::timestamptz,
	    sync_seq = nextval('pf_cropcycle.collection_sync_seq'), updated_at = now()
	  WHERE statement_ref = $1::uuid`,
		r.StatementRef, r.ID, r.StatementRevision, r.Outcome, r.HTXRolls, r.Reason, r.EventTime.UTC())
	return err
}

// Trạng thái lọc bảng kê.
const (
	StatementPending   = "pending"
	StatementConfirmed = "confirmed"
	StatementDisputed  = "disputed"
)

// ListStatements liệt kê bảng kê mới nhất trước. status: pending (chưa phản hồi revision
// hiện hành) | confirmed | disputed (phản hồi cho revision hiện hành) | "" (mọi).
func ListStatements(ctx context.Context, tx pgx.Tx, f CollectionFilter, status string, after uuid.UUID, limit int) ([]StatementView, error) {
	rows, err := tx.Query(ctx, "SELECT "+statementCols+` FROM pf_cropcycle.collection_statement_view
	    WHERE ($1::boolean OR cooperative_ref = $2::uuid)
	      AND ($3::text = ''
	        OR ($3::text = 'pending' AND (response_revision IS NULL OR response_revision <> revision))
	        OR (response_revision = revision AND response_outcome = $3::text))
	      AND ($4::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR statement_ref < $4::uuid)
	    ORDER BY statement_ref DESC LIMIT $5::int`, f.All, f.Coop, status, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StatementView{}
	for rows.Next() {
		v, err := scanStatement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// StatementsChangedAfter trả bảng kê của một HTX đổi sau con trỏ đồng bộ.
func StatementsChangedAfter(ctx context.Context, tx pgx.Tx, coop uuid.UUID, all bool, after int64, limit int) ([]StatementView, error) {
	rows, err := tx.Query(ctx, "SELECT "+statementCols+` FROM pf_cropcycle.collection_statement_view
	    WHERE ($1::boolean OR cooperative_ref = $2::uuid) AND sync_seq > $3::bigint ORDER BY sync_seq LIMIT $4::int`, all, coop, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StatementView{}
	for rows.Next() {
		v, err := scanStatement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Sổ phản hồi của HTX
// ---------------------------------------------------------------------------

// Response là một dòng sổ collection_response.
type Response struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	Seq               int64
	StatementRef      uuid.UUID
	StatementRevision int
	CooperativeRef    uuid.UUID
	PlotRef           uuid.UUID
	MKGRolls          int
	Outcome           string
	HTXRolls          int
	Reason            *string
	SourceTenantID    uuid.UUID
	FeedSeq           int64
	EventTime         time.Time
	RecordedAt        time.Time
	SupersedesID      *uuid.UUID
	CorrectionReason  *string
	ActorRef          uuid.UUID
	DeviceID          *string
	IdemKey           string
}

// InsertResponse ghi sổ phản hồi. feed_seq cấp SAU khóa tư vấn theo tenant tới hết giao
// dịch: thứ tự commit = thứ tự feed_seq, con trỏ kéo của MKG không bỏ sót dòng.
func InsertResponse(ctx context.Context, tx pgx.Tx, r *Response) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_cropcycle.collection_response_feed:' || current_setting('app.tenant_id', true), 0))"); err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_cropcycle.collection_response
	    (id, tenant_id, stream_key, statement_ref, statement_revision, cooperative_ref, plot_ref, mkg_rolls, outcome, htx_rolls,
	     reason, source_tenant_id, feed_seq, event_time, supersedes_id, correction_reason, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::int, $6::uuid, $7::uuid, $8::int, $9::text, $10::int,
	     $11::text, $12::uuid, nextval('pf_cropcycle.collection_response_feed_seq'), $13::timestamptz, $14::uuid, $15::text, $16::uuid,
	     $17::text, $18::text, $19::text)
	  RETURNING seq, feed_seq, recorded_at`,
		r.ID, r.TenantID, "statement:"+r.StatementRef.String(), r.StatementRef, r.StatementRevision, r.CooperativeRef, r.PlotRef,
		r.MKGRolls, r.Outcome, r.HTXRolls, r.Reason, r.SourceTenantID, r.EventTime.UTC(), r.SupersedesID, r.CorrectionReason,
		r.ActorRef, r.DeviceID, r.IdemKey, domain.RuleCollectionResponse).Scan(&r.Seq, &r.FeedSeq, &r.RecordedAt)
}

const responseCols = `id, tenant_id, seq, statement_ref, statement_revision, cooperative_ref, plot_ref, mkg_rolls, outcome, htx_rolls,
  reason, source_tenant_id, feed_seq, event_time, recorded_at, supersedes_id, correction_reason, actor_ref, device_id, idem_key`

func scanResponse(row pgx.Row) (Response, error) {
	var r Response
	err := row.Scan(&r.ID, &r.TenantID, &r.Seq, &r.StatementRef, &r.StatementRevision, &r.CooperativeRef, &r.PlotRef, &r.MKGRolls,
		&r.Outcome, &r.HTXRolls, &r.Reason, &r.SourceTenantID, &r.FeedSeq, &r.EventTime, &r.RecordedAt, &r.SupersedesID,
		&r.CorrectionReason, &r.ActorRef, &r.DeviceID, &r.IdemKey)
	return r, err
}

func responses(ctx context.Context, tx pgx.Tx, q string, args ...any) ([]Response, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Response{}
	for rows.Next() {
		r, err := scanResponse(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ResponseByIdem tìm phản hồi theo khóa chống trùng.
func ResponseByIdem(ctx context.Context, tx pgx.Tx, key string) (*Response, error) {
	r, err := scanResponse(tx.QueryRow(ctx, "SELECT "+responseCols+" FROM pf_cropcycle.collection_response WHERE idem_key = $1::text"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ResponseByID tìm phản hồi theo id.
func ResponseByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Response, error) {
	r, err := scanResponse(tx.QueryRow(ctx, "SELECT "+responseCols+" FROM pf_cropcycle.collection_response WHERE id = $1::uuid", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ResponsesOfStatement liệt kê phản hồi của một bảng kê theo thứ tự ghi.
func ResponsesOfStatement(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Response, error) {
	return responses(ctx, tx, "SELECT "+responseCols+" FROM pf_cropcycle.collection_response WHERE statement_ref = $1::uuid ORDER BY seq", ref)
}

// ResponseFeed trả phản hồi thuộc bảng kê do tenant source gửi, feed_seq > after, tăng dần.
func ResponseFeed(ctx context.Context, tx pgx.Tx, source uuid.UUID, after int64, limit int) ([]Response, error) {
	return responses(ctx, tx, "SELECT "+responseCols+` FROM pf_cropcycle.collection_response
	    WHERE source_tenant_id = $1::uuid AND feed_seq > $2::bigint ORDER BY feed_seq LIMIT $3::int`, source, after, limit)
}
