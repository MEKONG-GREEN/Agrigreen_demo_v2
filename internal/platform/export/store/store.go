// Package store ghi/đọc ba sổ chỉ ghi thêm của pf_export (vai r_export, RLS theo tenant).
package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RuleVersion ghi vào mọi dòng sổ.
const RuleVersion = "export/w10.17-v1"

// TemplateVersion là một phiên bản mẫu đã ghi sổ.
type TemplateVersion struct {
	ID       uuid.UUID
	Key      string
	Version  int
	Title    string
	Owner    string
	Raw      []byte // định nghĩa JSON
	Hash     string
	ActorRef uuid.UUID
	At       time.Time
}

// GetTemplate trả phiên bản mẫu đã ghi (nil nếu chưa).
func GetTemplate(ctx context.Context, tx pgx.Tx, key string, version int) (*TemplateVersion, error) {
	var t TemplateVersion
	err := tx.QueryRow(ctx, `SELECT id, template_key, version, title, owner, definition, definition_hash, actor_ref, event_time
  FROM pf_export.template_version WHERE template_key = $1 AND version = $2`, key, version).
		Scan(&t.ID, &t.Key, &t.Version, &t.Title, &t.Owner, &t.Raw, &t.Hash, &t.ActorRef, &t.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &t, err
}

// InsertTemplate ghi phiên bản mẫu (idem_key 'template:<khóa>:v<n>' — trùng → unique_violation ở shared.ledger_idem).
func InsertTemplate(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t TemplateVersion) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_export.template_version
  (id, tenant_id, stream_key, template_key, version, title, owner, definition, definition_hash, event_time, actor_ref, idem_key, rule_version)
  VALUES ($1, $2, 'template:' || $3, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, 'template:' || $3 || ':v' || $4::int::text, $11)`,
		t.ID, tid, t.Key, t.Version, t.Title, t.Owner, string(t.Raw), t.Hash, t.At, t.ActorRef, RuleVersion)
	return err
}

// Templates trả mọi phiên bản mẫu đã ghi trong tenant (không định nghĩa), theo khóa, phiên bản.
func Templates(ctx context.Context, tx pgx.Tx) ([]TemplateVersion, error) {
	rows, err := tx.Query(ctx, `SELECT id, template_key, version, title, owner, definition_hash, actor_ref, event_time
  FROM pf_export.template_version ORDER BY template_key, version`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (TemplateVersion, error) {
		var t TemplateVersion
		err := r.Scan(&t.ID, &t.Key, &t.Version, &t.Title, &t.Owner, &t.Hash, &t.ActorRef, &t.At)
		return t, err
	})
}

// DocNumber là số văn bản đã cấp.
type DocNumber struct {
	ID      uuid.UUID
	Series  string
	Year    int
	N       int64
	Subject string
}

// Text là "{n}/<ký hiệu>".
func (d DocNumber) Text() string { return strconv.FormatInt(d.N, 10) + "/" + d.Series }

// NumberFor trả số đã cấp cho subject theo ký hiệu (nil nếu chưa).
func NumberFor(ctx context.Context, tx pgx.Tx, series, subject string) (*DocNumber, error) {
	var d DocNumber
	err := tx.QueryRow(ctx, `SELECT id, series, year, seq, subject FROM pf_export.doc_number
  WHERE idem_key = 'docno:' || $1 || ':' || $2`, series, subject).Scan(&d.ID, &d.Series, &d.Year, &d.N, &d.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &d, err
}

// AllocateNumber cấp số kế tiếp của (ký hiệu, năm): n = seq của luồng do shared.fn_chain gán dưới khóa đầu luồng — tuần
// tự, không trùng; giao dịch hủy thì đầu luồng cũng lùi nên không hở số.
func AllocateNumber(ctx context.Context, tx pgx.Tx, tid, actor uuid.UUID, series string, year int, subject string, at time.Time, id uuid.UUID) (DocNumber, error) {
	d := DocNumber{ID: id, Series: series, Year: year, Subject: subject}
	err := tx.QueryRow(ctx, `INSERT INTO pf_export.doc_number
  (id, tenant_id, stream_key, series, year, subject, event_time, actor_ref, idem_key, rule_version)
  VALUES ($1, $2, 'docno:' || $3 || ':' || $4::int::text, $3, $4, $5, $6, $7, 'docno:' || $3 || ':' || $5, $8) RETURNING seq`,
		id, tid, series, year, subject, at, actor, RuleVersion).Scan(&d.N)
	return d, err
}

const runHead = `id, seq, subject, template_key, template_version, template_hash, input_hash, coalesce(doc_number, ''), doc_number_auto,
  file_name, content_type, body_hash, octet_length(body), actor_ref, event_time`

// Run là một lần xuất.
type Run struct {
	ID              uuid.UUID
	Seq             int64
	Subject         string
	Template        string
	TemplateVersion int
	TemplateHash    string
	InputHash       string
	DocNumber       string
	DocNumberAuto   bool
	FileName        string
	ContentType     string
	Body            []byte
	BodyHash        string
	Size            int64
	ActorRef        uuid.UUID
	At              time.Time
}

func scanHead(row pgx.Row, extra ...any) (Run, error) {
	var r Run
	dst := []any{&r.ID, &r.Seq, &r.Subject, &r.Template, &r.TemplateVersion, &r.TemplateHash, &r.InputHash, &r.DocNumber, &r.DocNumberAuto,
		&r.FileName, &r.ContentType, &r.BodyHash, &r.Size, &r.ActorRef, &r.At}
	err := row.Scan(append(dst, extra...)...)
	return r, err
}

// InsertRun ghi lần xuất, trả seq (lần thứ n của subject).
func InsertRun(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *Run, idem string) error {
	var number any
	if r.DocNumber != "" {
		number = r.DocNumber
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_export.export_run
  (id, tenant_id, stream_key, subject, template_key, template_version, template_hash, input_hash, doc_number, doc_number_auto,
   file_name, content_type, body, body_hash, event_time, actor_ref, idem_key, rule_version)
  VALUES ($1, $2, 'run:' || $3, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17) RETURNING seq`,
		r.ID, tid, r.Subject, r.Template, r.TemplateVersion, r.TemplateHash, r.InputHash, number, r.DocNumberAuto,
		r.FileName, r.ContentType, r.Body, r.BodyHash, r.At, r.ActorRef, idem, RuleVersion).Scan(&r.Seq)
}

// RunByIdem trả lần xuất theo idem_key (kèm byte), nil nếu chưa có.
func RunByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Run, error) {
	return one(tx.QueryRow(ctx, `SELECT `+runHead+`, body FROM pf_export.export_run WHERE idem_key = $1`, idem))
}

// RunByID trả lần xuất (kèm byte); subject rỗng = mọi subject (màn quản trị).
func RunByID(ctx context.Context, tx pgx.Tx, subject string, id uuid.UUID) (*Run, error) {
	return one(tx.QueryRow(ctx, `SELECT `+runHead+`, body FROM pf_export.export_run WHERE id = $1 AND ($2 = '' OR subject = $2)`, id, subject))
}

func one(row pgx.Row) (*Run, error) {
	var body []byte
	r, err := scanHead(row, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Body = body
	return &r, nil
}

// RunFilter lọc lịch sử lần xuất (rỗng = không lọc).
type RunFilter struct {
	Subject  string
	Template string
	Limit    int
}

// Runs trả lịch sử lần xuất (không byte), mới nhất trước.
func Runs(ctx context.Context, tx pgx.Tx, f RunFilter) ([]Run, error) {
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 200
	}
	rows, err := tx.Query(ctx, `SELECT `+runHead+` FROM pf_export.export_run
  WHERE ($1 = '' OR subject = $1) AND ($2 = '' OR template_key = $2)
  ORDER BY event_time DESC, id DESC LIMIT $3`, f.Subject, f.Template, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Run, error) { return scanHead(r) })
}
