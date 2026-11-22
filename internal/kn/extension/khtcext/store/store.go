// Package store: SQL của "Số liệu nguồn ngoài" KHTC (pf 20261028152846 — pf_extension.khtc_ext_indicator,
// khtc_ext_evidence, khtc_ext_figure, khtc_ext_import) trên pool vai của pf/extension, luôn trong giao dịch có ngữ cảnh
// tenant (RLS). Mọi bảng chỉ ghi thêm; sửa = bản ghi bù.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcext/domain"
)

// Indicator là một chỉ tiêu nguồn ngoài.
type Indicator struct {
	ID       uuid.UUID
	Code     string
	Name     string
	Unit     string
	Group    string
	ActorRef uuid.UUID
	At       time.Time
}

const indicatorCols = `id, code, name, unit, group_name, actor_ref, created_at`

func scanIndicator(r pgx.Row) (Indicator, error) {
	var i Indicator
	err := r.Scan(&i.ID, &i.Code, &i.Name, &i.Unit, &i.Group, &i.ActorRef, &i.At)
	return i, err
}

// Indicators liệt kê danh mục chỉ tiêu theo tên.
func Indicators(ctx context.Context, tx pgx.Tx) ([]Indicator, error) {
	rows, err := tx.Query(ctx, `SELECT `+indicatorCols+` FROM pf_extension.khtc_ext_indicator ORDER BY name_key, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Indicator, error) { return scanIndicator(r) })
}

// IndicatorByIdem đọc chỉ tiêu đã ghi với khóa idempotent (nil nếu chưa có).
func IndicatorByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Indicator, error) {
	i, err := scanIndicator(tx.QueryRow(ctx, `SELECT `+indicatorCols+` FROM pf_extension.khtc_ext_indicator WHERE idem_key = $1`, idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &i, err
}

// InsertIndicator ghi chỉ tiêu mới (trùng mã/tên → unique_violation khtc_ext_indicator_code/_name).
func InsertIndicator(ctx context.Context, tx pgx.Tx, tid uuid.UUID, i *Indicator, idem string) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_ext_indicator (tenant_id, id, code, name, name_key, unit, group_name, actor_ref, idem_key)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING created_at`,
		tid, i.ID, i.Code, i.Name, domain.NameKey(i.Name), i.Unit, i.Group, i.ActorRef, idem).Scan(&i.At)
}

// Evidence là siêu dữ liệu một tệp minh chứng (nội dung ở kho đối tượng).
type Evidence struct {
	ID          uuid.UUID
	FileName    string
	ContentType string
	Size        int64
	SHA256      string
	ObjectKey   string
	ActorRef    uuid.UUID
	At          time.Time
}

const evidenceCols = `id, file_name, content_type, size_bytes, sha256, object_key, actor_ref, created_at`

func scanEvidence(r pgx.Row) (Evidence, error) {
	var e Evidence
	err := r.Scan(&e.ID, &e.FileName, &e.ContentType, &e.Size, &e.SHA256, &e.ObjectKey, &e.ActorRef, &e.At)
	return e, err
}

// InsertEvidence ghi siêu dữ liệu tệp; gọi lặp cùng khóa → đọc lại bản cũ (replayed = true).
func InsertEvidence(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *Evidence, idem string) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_ext_evidence (tenant_id, id, file_name, content_type, size_bytes, sha256, object_key, actor_ref, idem_key)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT ON CONSTRAINT khtc_ext_evidence_idem DO NOTHING`,
		tid, e.ID, e.FileName, e.ContentType, e.Size, e.SHA256, e.ObjectKey, e.ActorRef, idem)
	if err != nil {
		return false, err
	}
	old, err := scanEvidence(tx.QueryRow(ctx, `SELECT `+evidenceCols+` FROM pf_extension.khtc_ext_evidence WHERE idem_key = $1`, idem))
	if err != nil {
		return false, err
	}
	*e = old
	return tag.RowsAffected() == 0, nil
}

// EvidenceByIDs đọc siêu dữ liệu các tệp theo mã.
func EvidenceByIDs(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]Evidence, error) {
	out := map[uuid.UUID]Evidence{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT `+evidenceCols+` FROM pf_extension.khtc_ext_evidence WHERE id = ANY($1::uuid[])`, uuidStrings(ids))
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Evidence, error) { return scanEvidence(r) })
	for _, e := range list {
		out[e.ID] = e
	}
	return out, err
}

// Entry là một dòng sổ số liệu (một phiên bản của chỉ tiêu × kỳ).
type Entry struct {
	ID          uuid.UUID
	Seq         int64
	PeriodID    uuid.UUID
	IndicatorID uuid.UUID
	Action      string
	Value       *decimal.Decimal
	Unit        string
	Agency      string
	Document    string
	SourceDate  *time.Time
	Evidence    []uuid.UUID
	ImportID    *uuid.UUID
	Supersedes  *uuid.UUID
	Reason      string
	ActorRef    uuid.UUID
	At          time.Time
}

const entryCols = `id, seq, period_id, indicator_id, action, value::text, unit, coalesce(agency, ''), coalesce(document, ''), source_date,
  evidence_ids::text[], import_id, supersedes_id, coalesce(correction_reason, ''), actor_ref, event_time`

func scanEntry(r pgx.Row) (Entry, error) {
	var e Entry
	var v *string
	var ev []string
	err := r.Scan(&e.ID, &e.Seq, &e.PeriodID, &e.IndicatorID, &e.Action, &v, &e.Unit, &e.Agency, &e.Document, &e.SourceDate, &ev,
		&e.ImportID, &e.Supersedes, &e.Reason, &e.ActorRef, &e.At)
	if err != nil {
		return e, err
	}
	if v != nil {
		d, err := decimal.NewFromString(*v)
		if err != nil {
			return e, err
		}
		e.Value = &d
	}
	e.Evidence, err = parseUUIDs(ev)
	return e, err
}

// InsertEntry ghi một phiên bản (trigger kiểm kỳ còn mở, chỉ tiêu, minh chứng, bản hiệu lực trước).
func InsertEntry(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *Entry, idem string) error {
	var value *string
	if e.Value != nil {
		s := e.Value.String()
		value = &s
	}
	var date *string
	if e.SourceDate != nil {
		s := e.SourceDate.Format(time.DateOnly)
		date = &s
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_ext_figure (id, tenant_id, stream_key, period_id, indicator_id, action, value,
	    unit, agency, document, source_date, evidence_ids, import_id, event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'khtc_ext_figure:' || $3::uuid::text || ':' || $4::uuid::text, $3, $4, $5, $6::numeric, $7, $8, $9, $10::date,
	    $11::uuid[], $12, $13, $14, $15, $16, $17, $18) RETURNING seq`,
		e.ID, tid, e.PeriodID, e.IndicatorID, e.Action, value, e.Unit, nilIfEmpty(e.Agency), nilIfEmpty(e.Document), date,
		uuidStrings(e.Evidence), e.ImportID, e.At.UTC(), e.Supersedes, nilIfEmpty(e.Reason), e.ActorRef, idem, domain.RuleVersion).Scan(&e.Seq)
}

// EntryByIdem đọc phiên bản đã ghi với khóa idempotent (nil nếu chưa có).
func EntryByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Entry, error) {
	e, err := scanEntry(tx.QueryRow(ctx, `SELECT `+entryCols+` FROM pf_extension.khtc_ext_figure WHERE idem_key = $1`, idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

// Latest trả bản mới nhất của mỗi chỉ tiêu × kỳ (gồm cả bản 'withdraw') của các kỳ cho trước; nil periods = mọi kỳ.
func Latest(ctx context.Context, tx pgx.Tx, periods []uuid.UUID) ([]Entry, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (period_id, indicator_id) `+entryCols+` FROM pf_extension.khtc_ext_figure
	  WHERE ($1::uuid[] IS NULL OR period_id = ANY($1::uuid[])) ORDER BY period_id, indicator_id, seq DESC`, nullableUUIDs(periods))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entry, error) { return scanEntry(r) })
}

// History trả mọi phiên bản của các kỳ cho trước, mới trước (tối đa limit).
func History(ctx context.Context, tx pgx.Tx, periods []uuid.UUID, limit int) ([]Entry, error) {
	if limit < 1 || limit > 1000 {
		limit = 200
	}
	rows, err := tx.Query(ctx, `SELECT `+entryCols+` FROM pf_extension.khtc_ext_figure
	  WHERE ($1::uuid[] IS NULL OR period_id = ANY($1::uuid[])) ORDER BY recorded_at DESC, seq DESC LIMIT $2`, nullableUUIDs(periods), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entry, error) { return scanEntry(r) })
}

// EntriesByIDs đọc các phiên bản theo mã (để hiện "cũ → mới").
func EntriesByIDs(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]Entry, error) {
	out := map[uuid.UUID]Entry{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT `+entryCols+` FROM pf_extension.khtc_ext_figure WHERE id = ANY($1::uuid[])`, uuidStrings(ids))
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entry, error) { return scanEntry(r) })
	for _, e := range list {
		out[e.ID] = e
	}
	return out, err
}

// Import là một bước của "Nạp Excel tổng hợp".
type Import struct {
	ID       uuid.UUID // mã dòng sổ
	ImportID uuid.UUID
	Seq      int64
	Year     int
	Action   string
	FileName string
	FileSHA  string
	Payload  ImportPayload
	ActorRef uuid.UUID
	At       time.Time
}

// ImportPayload là nội dung đã kiểm (không có byte tệp).
type ImportPayload struct {
	Raw       []domain.RawRow    `json:"raw"`
	Result    domain.CheckResult `json:"result"`
	Overwrite bool               `json:"overwrite,omitempty"`
	Reason    string             `json:"reason,omitempty"`
	Recorded  int                `json:"recorded,omitempty"`
	Skipped   int                `json:"skipped,omitempty"`
}

const importCols = `id, import_id, seq, plan_year, action, file_name, file_sha256, payload, actor_ref, event_time`

func scanImport(r pgx.Row) (*Import, error) {
	var i Import
	var raw []byte
	err := r.Scan(&i.ID, &i.ImportID, &i.Seq, &i.Year, &i.Action, &i.FileName, &i.FileSHA, &raw, &i.ActorRef, &i.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &i, json.Unmarshal(raw, &i.Payload)
}

// InsertImport ghi một bước nạp (trigger: một 'staged', tối đa một 'committed' sau 'staged').
func InsertImport(ctx context.Context, tx pgx.Tx, tid uuid.UUID, i *Import, idem string) error {
	raw, err := json.Marshal(i.Payload)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_ext_import (id, tenant_id, stream_key, import_id, plan_year, action, file_name,
	    file_sha256, payload, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'khtc_ext_import:' || $3::uuid::text, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12) RETURNING seq`,
		i.ID, tid, i.ImportID, i.Year, i.Action, i.FileName, i.FileSHA, string(raw), i.At.UTC(), i.ActorRef, idem, domain.RuleVersion).Scan(&i.Seq)
}

// ImportStep đọc bước action của lần nạp (nil nếu chưa có).
func ImportStep(ctx context.Context, tx pgx.Tx, importID uuid.UUID, action string) (*Import, error) {
	return scanImport(tx.QueryRow(ctx, `SELECT `+importCols+` FROM pf_extension.khtc_ext_import WHERE import_id = $1 AND action = $2`, importID, action))
}

// ImportByIdem đọc bước đã ghi với khóa idempotent (nil nếu chưa có).
func ImportByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Import, error) {
	return scanImport(tx.QueryRow(ctx, `SELECT `+importCols+` FROM pf_extension.khtc_ext_import WHERE idem_key = $1`, idem))
}

// LockImport tuần tự hóa các lần "Hoàn tất" của cùng lần nạp (cùng khóa với trigger).
func LockImport(ctx context.Context, tx pgx.Tx, tid, importID uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('khtc_ext_import:' || $1::uuid::text || ':' || $2::uuid::text, 0))`, tid, importID)
	return err
}

func uuidStrings(xs []uuid.UUID) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = x.String()
	}
	return out
}

func nullableUUIDs(xs []uuid.UUID) []string {
	if xs == nil {
		return nil
	}
	return uuidStrings(xs)
}

func parseUUIDs(xs []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(xs))
	for _, x := range xs {
		u, err := uuid.Parse(x)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
