package store

// W11.26: một sổ, mỗi mẫu/phiếu/đợt/dòng đánh giá có stream riêng. SQL chỉ SELECT/INSERT;
// mọi lời gọi phải nằm trong TX r_hrm đã gắn tenant. Không gọi port hoặc vault ở đây.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
)

type AppraisalEvent struct {
	ID          uuid.UUID
	Kind        string
	Key         string
	PeriodRef   *uuid.UUID
	StaffRef    *uuid.UUID
	Action      string
	Payload     json.RawMessage
	Reason      string
	RequestHash string
	IdemKey     string
	Actor       uuid.UUID
	RecordedAt  time.Time
	Seq         int64
}

const appraisalColumns = `id, entity_kind, entity_key, period_ref, staff_ref, action, payload, reason, request_hash,
  idem_key, actor_ref, recorded_at, seq`

func scanAppraisalEvent(row pgx.CollectableRow) (AppraisalEvent, error) {
	var x AppraisalEvent
	err := row.Scan(&x.ID, &x.Kind, &x.Key, &x.PeriodRef, &x.StaffRef, &x.Action, &x.Payload, &x.Reason, &x.RequestHash,
		&x.IdemKey, &x.Actor, &x.RecordedAt, &x.Seq)
	return x, err
}

// LockAppraisal khóa chuỗi nghiệp vụ trước replay/kiểm phiên bản/ghi. scope do server dựng, không dùng PII.
func LockAppraisal(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, scope string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_hrm.appraisal:' || $1::text || ':' || $2, 0))`, tenant, scope)
	return err
}

// AppraisalCurrent trả bản mới nhất mỗi đối tượng; period nil không lọc (mẫu hoặc danh sách đợt).
func AppraisalCurrent(ctx context.Context, tx pgx.Tx, kind string, period *uuid.UUID) ([]AppraisalEvent, error) {
	rows, err := tx.Query(ctx, `SELECT `+appraisalColumns+` FROM (
    SELECT DISTINCT ON (entity_key) `+appraisalColumns+` FROM pf_hrm.appraisal_event
    WHERE entity_kind = $1 AND ($2::uuid IS NULL OR period_ref = $2) ORDER BY entity_key, seq DESC) v
    ORDER BY entity_key`, kind, period)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanAppraisalEvent)
}

// AppraisalEntrySummaries không chuyển toàn bộ câu trả lời tiêu chí ra màn danh sách/tiến độ.
// keepText chỉ dành cho bảng MS05/06 xuất, vẫn không lấy câu trả lời chi tiết.
func AppraisalEntrySummaries(ctx context.Context, tx pgx.Tx, period uuid.UUID, keepText bool) ([]AppraisalEvent, error) {
	payload := `(payload #- '{self,answers}' #- '{manager,answers}')`
	if !keepText {
		payload = `(` + payload + ` - 'reason' - 'note' - 'personnel_proposal' - 'correction_reason')`
	}
	cols := strings.Replace(appraisalColumns, "payload", payload+" AS payload", 1)
	rows, err := tx.Query(ctx, `SELECT `+cols+` FROM (
    SELECT DISTINCT ON (entity_key) * FROM pf_hrm.appraisal_event
    WHERE entity_kind = 'entry' AND period_ref = $1 ORDER BY entity_key, seq DESC) v ORDER BY entity_key`, period)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanAppraisalEvent)
}

func AppraisalHistory(ctx context.Context, tx pgx.Tx, kind, key string) ([]AppraisalEvent, error) {
	rows, err := tx.Query(ctx, `SELECT `+appraisalColumns+` FROM pf_hrm.appraisal_event
    WHERE entity_kind = $1 AND entity_key = $2 ORDER BY seq`, kind, key)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanAppraisalEvent)
}

func AppraisalLatest(ctx context.Context, tx pgx.Tx, kind, key string) (AppraisalEvent, bool, error) {
	rows, err := tx.Query(ctx, `SELECT `+appraisalColumns+` FROM pf_hrm.appraisal_event
    WHERE entity_kind = $1 AND entity_key = $2 ORDER BY seq DESC LIMIT 1`, kind, key)
	if err != nil {
		return AppraisalEvent{}, false, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanAppraisalEvent)
	if errors.Is(err, pgx.ErrNoRows) {
		return AppraisalEvent{}, false, nil
	}
	return x, err == nil, err
}

// AppraisalEntryHistory lấy tối đa 51 dòng chỉ chứa tóm tắt; dòng thứ51 xác định còn trang tiếp.
func AppraisalEntryHistory(ctx context.Context, tx pgx.Tx, key string, before int64) ([]AppraisalEvent, error) {
	cols := strings.Replace(appraisalColumns, "payload", `(payload #- '{self,answers}' #- '{manager,answers}') AS payload`, 1)
	rows, err := tx.Query(ctx, `SELECT `+cols+` FROM pf_hrm.appraisal_event
    WHERE entity_kind = 'entry' AND entity_key = $1 AND ($2::bigint = 0 OR seq < $2)
    ORDER BY seq DESC LIMIT 51`, key, before)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanAppraisalEvent)
}

// AppraisalVersion dùng id bản ghi bất biến mà đợt đã chọn, không lấy latest thay phiên bản.
func AppraisalVersion(ctx context.Context, tx pgx.Tx, id uuid.UUID) (AppraisalEvent, bool, error) {
	rows, err := tx.Query(ctx, `SELECT `+appraisalColumns+` FROM pf_hrm.appraisal_event WHERE id = $1`, id)
	if err != nil {
		return AppraisalEvent{}, false, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanAppraisalEvent)
	if errors.Is(err, pgx.ErrNoRows) {
		return AppraisalEvent{}, false, nil
	}
	return x, err == nil, err
}

func AppraisalPeriodByRef(ctx context.Context, tx pgx.Tx, id uuid.UUID) (AppraisalEvent, bool, error) {
	rows, err := tx.Query(ctx, `SELECT `+appraisalColumns+` FROM pf_hrm.appraisal_event
    WHERE entity_kind = 'period' AND period_ref = $1 ORDER BY seq DESC LIMIT 1`, id)
	if err != nil {
		return AppraisalEvent{}, false, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanAppraisalEvent)
	if errors.Is(err, pgx.ErrNoRows) {
		return AppraisalEvent{}, false, nil
	}
	return x, err == nil, err
}

// AppraisalReplay bền hơn TTL middleware: cùng khóa nhưng khác route/thân phải bị từ chối ở ứng dụng.
func AppraisalReplay(ctx context.Context, tx pgx.Tx, idem string) (AppraisalEvent, bool, error) {
	rows, err := tx.Query(ctx, `SELECT `+appraisalColumns+` FROM pf_hrm.appraisal_event WHERE idem_key = $1 ORDER BY recorded_at DESC LIMIT 1`, idem)
	if err != nil {
		return AppraisalEvent{}, false, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanAppraisalEvent)
	if errors.Is(err, pgx.ErrNoRows) {
		return AppraisalEvent{}, false, nil
	}
	return x, err == nil, err
}

func InsertAppraisal(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, x AppraisalEvent, at time.Time) (AppraisalEvent, error) {
	err := tx.QueryRow(ctx, `INSERT INTO pf_hrm.appraisal_event
    (id, tenant_id, stream_key, entity_kind, entity_key, period_ref, staff_ref, action, payload, reason, request_hash,
     event_time, actor_ref, idem_key, rule_version)
    VALUES ($1,$2,$3 || ':' || $4,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING seq, recorded_at`,
		x.ID, tenant, x.Kind, x.Key, x.PeriodRef, x.StaffRef, x.Action, x.Payload, x.Reason, x.RequestHash,
		at.UTC(), x.Actor, x.IdemKey, domain.RuleVersion).Scan(&x.Seq, &x.RecordedAt)
	return x, err
}
