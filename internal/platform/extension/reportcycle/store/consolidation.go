package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// RegionRef là cấu trúc khu vực của một xã lúc chạy tổng hợp (gap §4.6).
type RegionRef struct {
	Commune     string `json:"commune"`
	CommuneName string `json:"commune_name"`
	Region      string `json:"region"`
	RegionName  string `json:"region_name"`
	SortOrder   int    `json:"sort_order"`
}

// Run là một dòng consolidation_run (bất biến).
type Run struct {
	ID               uuid.UUID
	RunNo            int
	PeriodID         uuid.UUID
	BatchID          uuid.UUID
	TemplateID       uuid.UUID
	VersionID        uuid.UUID
	FormCode         string
	DepartmentCode   string
	ProvinceCode     string
	Trigger          string
	CommunesTotal    int
	CommunesWithData int
	CommunesChanged  int
	CellsChanged     int
	Provisional      bool
	MissingCommunes  []string
	Regions          []RegionRef
	PreviousRunID    *uuid.UUID
	OnBehalfOf       *uuid.UUID
	EventTime        time.Time
	ActorRef         uuid.UUID
	IdemKey          string
	RecordedAt       time.Time
}

const runCols = `id, run_no, period_id, batch_id, template_id, version_id, form_code, department_code, province_code, trigger, communes_total,
  communes_with_data, communes_changed, cells_changed, provisional, missing_communes, region_snapshot::text, previous_run_id, on_behalf_of,
  event_time, actor_ref, idem_key, recorded_at`

func scanRun(row pgx.Row) (Run, error) {
	var r Run
	var regions string
	err := row.Scan(&r.ID, &r.RunNo, &r.PeriodID, &r.BatchID, &r.TemplateID, &r.VersionID, &r.FormCode, &r.DepartmentCode, &r.ProvinceCode, &r.Trigger,
		&r.CommunesTotal, &r.CommunesWithData, &r.CommunesChanged, &r.CellsChanged, &r.Provisional, &r.MissingCommunes, &regions, &r.PreviousRunID,
		&r.OnBehalfOf, &r.EventTime, &r.ActorRef, &r.IdemKey, &r.RecordedAt)
	if err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(regions), &r.Regions)
	return r, err
}

// InsertRun ghi sổ lần tổng hợp.
func InsertRun(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *Run) error {
	regions, err := json.Marshal(r.Regions)
	if err != nil {
		return err
	}
	if r.MissingCommunes == nil {
		r.MissingCommunes = []string{}
	}
	var seq int64
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.consolidation_run (id, tenant_id, stream_key, run_no, period_id, batch_id, template_id, version_id,
	    form_code, department_code, province_code, trigger, communes_total, communes_with_data, communes_changed, cells_changed, provisional,
	    missing_communes, region_snapshot, previous_run_id, on_behalf_of, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18::text[], $19::jsonb, $20, $21, $22, $23, $24, $25)
	  RETURNING seq, recorded_at`,
		r.ID, tid, "cons:"+r.PeriodID.String()+":"+r.TemplateID.String(), r.RunNo, r.PeriodID, r.BatchID, r.TemplateID, r.VersionID, r.FormCode,
		r.DepartmentCode, r.ProvinceCode, r.Trigger, r.CommunesTotal, r.CommunesWithData, r.CommunesChanged, r.CellsChanged, r.Provisional,
		r.MissingCommunes, string(regions), r.PreviousRunID, r.OnBehalfOf, r.EventTime.UTC(), r.ActorRef, r.IdemKey, RuleVersion).Scan(&seq, &r.RecordedAt)
}

// RunByID đọc một lần tổng hợp; không có → nil.
func RunByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Run, error) {
	r, err := scanRun(tx.QueryRow(ctx, "SELECT "+runCols+" FROM pf_extension.consolidation_run WHERE id = $1::uuid", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Runs trả các lần tổng hợp của kỳ × biểu (mới nhất trước) — "Nhật ký cập nhật".
func Runs(ctx context.Context, tx pgx.Tx, periodID uuid.UUID, templateID *uuid.UUID, limit int) ([]Run, error) {
	rows, err := tx.Query(ctx, "SELECT "+runCols+` FROM pf_extension.consolidation_run
	  WHERE period_id = $1::uuid AND ($2::uuid IS NULL OR template_id = $2::uuid) ORDER BY recorded_at DESC, run_no DESC LIMIT $3`,
		periodID, templateID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Run, error) { return scanRun(r) })
}

// Snapshot là giá trị tổng hợp của một lần chạy.
type Snapshot struct {
	RunID      uuid.UUID
	PeriodID   uuid.UUID
	TemplateID uuid.UUID
	Rows       json.RawMessage
	Totals     json.RawMessage
	Sources    json.RawMessage
	CreatedAt  time.Time
}

// InsertSnapshot ghi ảnh chụp (bất biến).
func InsertSnapshot(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s Snapshot) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.consolidation_snapshot (tenant_id, run_id, period_id, template_id, rows, totals, sources, created_at)
	  VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8)`, tid, s.RunID, s.PeriodID, s.TemplateID, string(s.Rows), string(s.Totals),
		string(s.Sources), s.CreatedAt.UTC())
	return err
}

// SnapshotOf đọc ảnh chụp của lần chạy; không có → nil.
func SnapshotOf(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (*Snapshot, error) {
	var s Snapshot
	var rows, totals, sources string
	err := tx.QueryRow(ctx, `SELECT run_id, period_id, template_id, rows::text, totals::text, sources::text, created_at
	  FROM pf_extension.consolidation_snapshot WHERE run_id = $1::uuid`, runID).Scan(&s.RunID, &s.PeriodID, &s.TemplateID, &rows, &totals, &sources, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.Rows, s.Totals, s.Sources = json.RawMessage(rows), json.RawMessage(totals), json.RawMessage(sources)
	return &s, nil
}

// CellChange là một dòng consolidation_cell_change.
type CellChange struct {
	ID          uuid.UUID
	RunID       uuid.UUID
	PeriodID    uuid.UUID
	TemplateID  uuid.UUID
	FormCode    string
	Commune     string
	RowKey      string
	Letter      string
	Old, New    *string
	Kind        string
	Submission  *uuid.UUID
	ChangedBy   *uuid.UUID
	OnBehalfOf  *uuid.UUID
	SubmittedAt *time.Time
	CreatedAt   time.Time
}

// InsertCellChanges ghi hàng loạt ô thay đổi (unnest).
func InsertCellChanges(ctx context.Context, tx pgx.Tx, tid uuid.UUID, cs []CellChange) error {
	const chunk = 5000
	for start := 0; start < len(cs); start += chunk {
		part := cs[start:min(start+chunk, len(cs))]
		n := len(part)
		ids, runs, periods, tpls := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
		codes, communes, keys, letters, kinds := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
		olds, news := make([]*string, n), make([]*string, n)
		subs, bys, obos := make([]*uuid.UUID, n), make([]*uuid.UUID, n), make([]*uuid.UUID, n)
		ats, created := make([]*string, n), make([]string, n)
		for i, c := range part {
			ids[i], runs[i], periods[i], tpls[i], codes[i], communes[i], keys[i], letters[i], kinds[i] = c.ID.String(), c.RunID.String(), c.PeriodID.String(), c.TemplateID.String(), c.FormCode,
				c.Commune, c.RowKey, c.Letter, c.Kind
			olds[i], news[i], subs[i], bys[i], obos[i], created[i] = c.Old, c.New, c.Submission, c.ChangedBy, c.OnBehalfOf, c.CreatedAt.UTC().Format(time.RFC3339Nano)
			if c.SubmittedAt != nil {
				s := c.SubmittedAt.UTC().Format(time.RFC3339Nano)
				ats[i] = &s
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pf_extension.consolidation_cell_change (tenant_id, id, run_id, period_id, template_id, form_code,
		    commune_code, row_key, letter, old_value, new_value, change_kind, submission_id, changed_by, on_behalf_of, submitted_at, created_at)
		  SELECT $1, u.* FROM unnest($2::uuid[], $3::uuid[], $4::uuid[], $5::uuid[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[],
		    $11::text[], $12::text[], $13::uuid[], $14::uuid[], $15::uuid[], $16::timestamptz[], $17::timestamptz[]) AS u`,
			tid, ids, runs, periods, tpls, codes, communes, keys, letters, olds, news, kinds, pstrs(subs), pstrs(bys), pstrs(obos), ats, created); err != nil {
			return err
		}
	}
	return nil
}

// CellChanges trả ô thay đổi của lần chạy theo (xã, cột).
func CellChanges(ctx context.Context, tx pgx.Tx, runID uuid.UUID, after *uuid.UUID, limit int) ([]CellChange, error) {
	rows, err := tx.Query(ctx, `SELECT id, run_id, period_id, template_id, form_code, commune_code, row_key, letter, old_value, new_value, change_kind,
	    submission_id, changed_by, on_behalf_of, submitted_at, created_at FROM pf_extension.consolidation_cell_change
	  WHERE run_id = $1::uuid AND ($2::uuid IS NULL OR id > $2::uuid) ORDER BY id LIMIT $3`, runID, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (CellChange, error) {
		var c CellChange
		err := r.Scan(&c.ID, &c.RunID, &c.PeriodID, &c.TemplateID, &c.FormCode, &c.Commune, &c.RowKey, &c.Letter, &c.Old, &c.New, &c.Kind, &c.Submission,
			&c.ChangedBy, &c.OnBehalfOf, &c.SubmittedAt, &c.CreatedAt)
		return c, err
	})
}

// Latest là con trỏ lần tổng hợp mới nhất.
type Latest struct {
	PeriodID   uuid.UUID
	TemplateID uuid.UUID
	RunID      uuid.UUID
	RunNo      int
	BatchID    uuid.UUID
	FormCode   string
	RunAt      time.Time
}

// UpsertLatest đặt con trỏ lần mới nhất của kỳ × biểu.
func UpsertLatest(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r Run) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.consolidation_latest (tenant_id, period_id, template_id, run_id, run_no, batch_id, form_code,
	    department_code, province_code, run_at, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
	  ON CONFLICT (tenant_id, period_id, template_id) DO UPDATE SET run_id = EXCLUDED.run_id, run_no = EXCLUDED.run_no, run_at = EXCLUDED.run_at,
	    updated_at = EXCLUDED.updated_at`,
		tid, r.PeriodID, r.TemplateID, r.ID, r.RunNo, r.BatchID, r.FormCode, r.DepartmentCode, r.ProvinceCode, r.EventTime.UTC())
	return err
}

// LatestOfPeriod trả con trỏ lần mới nhất của mọi biểu trong kỳ, khóa theo template_id.
func LatestOfPeriod(ctx context.Context, tx pgx.Tx, periodID uuid.UUID) (map[uuid.UUID]Latest, error) {
	rows, err := tx.Query(ctx, `SELECT period_id, template_id, run_id, run_no, batch_id, form_code, run_at FROM pf_extension.consolidation_latest
	  WHERE period_id = $1::uuid`, periodID)
	if err != nil {
		return nil, err
	}
	ls, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Latest, error) {
		var l Latest
		err := r.Scan(&l.PeriodID, &l.TemplateID, &l.RunID, &l.RunNo, &l.BatchID, &l.FormCode, &l.RunAt)
		return l, err
	})
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]Latest{}
	for _, l := range ls {
		out[l.TemplateID] = l
	}
	return out, nil
}

// LatestForForm trả con trỏ mới nhất của mỗi kỳ cho mã mẫu ở phạm vi (dashboard: tiến độ theo kỳ, sâu bệnh so kỳ trước).
func LatestForForm(ctx context.Context, tx pgx.Tx, province, dept, formCode string, periodIDs []uuid.UUID) ([]Latest, error) {
	rows, err := tx.Query(ctx, `SELECT period_id, template_id, run_id, run_no, batch_id, form_code, run_at FROM pf_extension.consolidation_latest
	  WHERE province_code = $1 AND department_code = $2 AND form_code = $3 AND period_id = ANY($4::uuid[])`, province, dept, formCode, strs(periodIDs))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Latest, error) {
		var l Latest
		err := r.Scan(&l.PeriodID, &l.TemplateID, &l.RunID, &l.RunNo, &l.BatchID, &l.FormCode, &l.RunAt)
		return l, err
	})
}

// Metric là một dòng consolidation_metric.
type Metric struct {
	RunID    uuid.UUID
	PeriodID uuid.UUID
	FormCode string
	Metric   string
	Scope    string
	Unit     string
	Value    decimal.Decimal
}

// InsertMetrics ghi chỉ số dựng sẵn của lần chạy.
func InsertMetrics(ctx context.Context, tx pgx.Tx, tid, runID, periodID, batchID uuid.UUID, form, dept, province string, ms []Metric) error {
	if len(ms) == 0 {
		return nil
	}
	names, scopes, units, values := make([]string, len(ms)), make([]string, len(ms)), make([]string, len(ms)), make([]string, len(ms))
	for i, m := range ms {
		names[i], scopes[i], units[i], values[i] = m.Metric, m.Scope, m.Unit, m.Value.String()
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.consolidation_metric (tenant_id, run_id, period_id, batch_id, form_code, department_code,
	    province_code, metric, scope, unit_code, value)
	  SELECT $1, $2, $3, $4, $5, $6, $7, u.metric, u.scope, u.unit, u.value::numeric
	  FROM unnest($8::text[], $9::text[], $10::text[], $11::text[]) AS u(metric, scope, unit, value)`,
		tid, runID, periodID, batchID, form, dept, province, names, scopes, units, values)
	return err
}

// MetricsOfRuns đọc chỉ số của các lần chạy (chỉ số theo tiền tố metric nếu có).
func MetricsOfRuns(ctx context.Context, tx pgx.Tx, runIDs []uuid.UUID, scopes []string) ([]Metric, error) {
	rows, err := tx.Query(ctx, `SELECT run_id, period_id, form_code, metric, scope, unit_code, value::text FROM pf_extension.consolidation_metric
	  WHERE run_id = ANY($1::uuid[]) AND scope = ANY($2::text[])`, strs(runIDs), scopes)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Metric, error) {
		var m Metric
		var v string
		if err := r.Scan(&m.RunID, &m.PeriodID, &m.FormCode, &m.Metric, &m.Scope, &m.Unit, &v); err != nil {
			return m, err
		}
		d, err := decimal.NewFromString(v)
		m.Value = d
		return m, err
	})
}
