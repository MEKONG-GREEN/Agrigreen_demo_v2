package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// ---------------------------------------------------------------------------
// Sổ cảnh báo MRV (pf 000045)

// AlertRow là một dòng sổ cảnh báo.
type AlertRow struct {
	ID            uuid.UUID
	Kind          string
	Level         int
	Severity      string
	AdminUnitCode string
	SubjectType   string
	SubjectID     uuid.UUID
	SubjectRootID uuid.UUID
	PlotRef       *uuid.UUID
	SeasonRef     *uuid.UUID
	Detail        json.RawMessage
	Params        []domain.ParamUse
	EventTime     time.Time
	ActorRef      uuid.UUID
	IdemKey       string
	RuleVersion   string
	Seq           int64
	RecordedAt    time.Time
}

const alertCols = `a.id, a.kind, a.level, a.severity, a.admin_unit_code, a.subject_type, a.subject_id, a.subject_root_id,
  a.plot_ref, a.season_ref, a.detail::text, a.params::text, a.event_time, a.actor_ref, a.idem_key, a.rule_version, a.seq, a.recorded_at`

func scanAlert(row pgx.Row) (AlertRow, error) {
	var a AlertRow
	var detail, params string
	err := row.Scan(&a.ID, &a.Kind, &a.Level, &a.Severity, &a.AdminUnitCode, &a.SubjectType, &a.SubjectID, &a.SubjectRootID,
		&a.PlotRef, &a.SeasonRef, &detail, &params, &a.EventTime, &a.ActorRef, &a.IdemKey, &a.RuleVersion, &a.Seq, &a.RecordedAt)
	if err != nil {
		return a, err
	}
	a.Detail = json.RawMessage(detail)
	if err := json.Unmarshal([]byte(params), &a.Params); err != nil {
		return a, err
	}
	return a, nil
}

// InsertAlert ghi sổ cảnh báo. Gọi IdemExists("alert", key) trước: trong giao dịch của consumer,
// lỗi 23505 làm hỏng cả giao dịch.
func InsertAlert(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a *AlertRow) error {
	if a.ID == uuid.Nil {
		a.ID = ids.New()
	}
	params := a.Params
	if params == nil {
		params = []domain.ParamUse{}
	}
	pb, err := json.Marshal(params)
	if err != nil {
		return err
	}
	detail := a.Detail
	if len(detail) == 0 {
		detail = json.RawMessage("{}")
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_mrv.alert
	    (id, tenant_id, stream_key, kind, level, severity, admin_unit_code, subject_type, subject_id, subject_root_id,
	     plot_ref, season_ref, detail, params, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14::jsonb, $15, $16, $17, $18)
	  RETURNING seq, recorded_at`,
		a.ID, tid, domain.StreamKey(a.AdminUnitCode), a.Kind, a.Level, a.Severity, a.AdminUnitCode, a.SubjectType, a.SubjectID,
		a.SubjectRootID, a.PlotRef, a.SeasonRef, string(detail), string(pb), a.EventTime.UTC(), a.ActorRef, a.IdemKey,
		a.RuleVersion).Scan(&a.Seq, &a.RecordedAt)
}

// AlertByIdem đọc dòng cảnh báo theo idem_key (nil khi chưa có).
func AlertByIdem(ctx context.Context, tx pgx.Tx, key string) (*AlertRow, error) {
	a, err := scanAlert(tx.QueryRow(ctx, "SELECT "+alertCols+" FROM pf_mrv.alert a WHERE a.idem_key = $1::text"+ledger.NewestByIdem("a"), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// uuidStrings: mảng uuid gửi dạng chuỗi (chế độ thực thi PgBouncer không mã hóa []uuid.UUID).
func uuidStrings(us []uuid.UUID) []string {
	out := make([]string, 0, len(us))
	for _, u := range us {
		out = append(out, u.String())
	}
	return out
}

// AlertsOfRoots trả mọi cảnh báo của các chuỗi (gốc minh chứng / log_ref), cũ → mới.
func AlertsOfRoots(ctx context.Context, tx pgx.Tx, roots []uuid.UUID) ([]AlertRow, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, "SELECT "+alertCols+` FROM pf_mrv.alert a WHERE a.subject_root_id = ANY($1::uuid[])
	  ORDER BY a.recorded_at, a.seq`, uuidStrings(roots))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AlertRow, error) { return scanAlert(r) })
}

// UrgentHeads cho biết phiên bản minh chứng nào (trong heads) đang mang nhãn "Cảnh báo khẩn".
func UrgentHeads(ctx context.Context, tx pgx.Tx, heads []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(heads) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT subject_id FROM pf_mrv.alert
	  WHERE kind = 'pest_urgent' AND subject_id = ANY($1::uuid[])`, uuidStrings(heads))
	if err != nil {
		return nil, err
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	for _, id := range found {
		out[id] = true
	}
	return out, err
}

// AlertFilter lọc danh sách cảnh báo.
type AlertFilter struct {
	Kind  string
	Unit  string
	All   bool
	Units []string
	After uuid.UUID
	Limit int
}

// Alerts phân trang cảnh báo theo id (UUIDv7 ~ thời gian ghi), trong phạm vi.
func Alerts(ctx context.Context, tx pgx.Tx, f AlertFilter) ([]AlertRow, error) {
	rows, err := tx.Query(ctx, "SELECT "+alertCols+` FROM pf_mrv.alert a
	  WHERE a.id > $1 AND ($2::bool OR a.admin_unit_code = ANY($3::text[]))
	    AND ($4::text = '' OR a.kind = $4::text) AND ($5::text = '' OR a.admin_unit_code = $5::text)
	  ORDER BY a.id LIMIT $6`, f.After, f.All, units(f.Units), f.Kind, f.Unit, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AlertRow, error) { return scanAlert(r) })
}

// HasInfrastructure (KN FR-08) cho biết thửa có minh chứng hạ tầng tưới tiêu ĐẠT còn hiệu lực: bản
// hiện hành của một chuỗi irrigation_infrastructure có infra_ready = true, không bị "Yêu cầu kiểm
// tra lại", gắn đúng thửa HOẶC không gắn thửa mà thuộc cùng xã (xác nhận cho cả vùng).
func HasInfrastructure(ctx context.Context, tx pgx.Tx, evidenceType string, plotRef uuid.UUID, commune string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1
	  FROM pf_mrv.evidence_current c JOIN pf_mrv.evidence e ON e.id = c.head_id
	  WHERE c.evidence_type = $1::text AND c.review_status <> 'rejected'
	    AND (e.field_values->>'infra_ready') = 'true'
	    AND (e.plot_ref = $2::uuid OR (e.plot_ref IS NULL AND c.admin_unit_code = $3::text)))`,
		evidenceType, plotRef, commune).Scan(&ok)
	return ok, err
}

// PendingHead là bản hiện hành đang chờ duyệt của một hồ sơ MRV.
type PendingHead struct {
	RootID        uuid.UUID
	HeadID        uuid.UUID
	AdminUnitCode string
	EvidenceType  string
	PlotRef       *uuid.UUID
	Since         time.Time // lúc phiên bản vào hệ thống (recorded_at) — mốc tính hạn xử lý
	Raised        int       // mức leo thang cao nhất đã ghi cho phiên bản này
}

// OverduePending trả hồ sơ chờ duyệt cần leo thang THÊM ít nhất một mức: vào hệ thống trước
// slaBefore mà chưa có mức 1, hoặc trước escBefore mà chưa có mức 2 — cũ nhất trước. Mỗi dòng trả
// về đều sinh ít nhất một cảnh báo, nên vòng lặp theo lô luôn tiến. Giao dịch tenant (vai mrv_app).
func OverduePending(ctx context.Context, tx pgx.Tx, slaBefore, escBefore time.Time, limit int) ([]PendingHead, error) {
	rows, err := tx.Query(ctx, `SELECT q.root_id, q.head_id, q.admin_unit_code, q.evidence_type, q.plot_ref, q.recorded_at, q.raised
	  FROM (SELECT c.root_id, c.head_id, c.admin_unit_code, c.evidence_type, e.plot_ref, e.recorded_at,
	          coalesce((SELECT max(a.level) FROM pf_mrv.alert a WHERE a.kind = 'review_overdue' AND a.subject_id = c.head_id), 0) AS raised
	        FROM pf_mrv.evidence_current c JOIN pf_mrv.evidence e ON e.id = c.head_id
	        WHERE c.review_status = 'pending' AND e.recorded_at < $1::timestamptz) q
	  WHERE (q.raised < 1) OR (q.raised < 2 AND q.recorded_at < $2::timestamptz)
	  ORDER BY q.recorded_at, q.head_id LIMIT $3`, slaBefore.UTC(), escBefore.UTC(), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PendingHead, error) {
		var p PendingHead
		err := r.Scan(&p.RootID, &p.HeadID, &p.AdminUnitCode, &p.EvidenceType, &p.PlotRef, &p.Since, &p.Raised)
		return p, err
	})
}
