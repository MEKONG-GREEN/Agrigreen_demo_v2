// Package store: SQL của mẫu biểu dự án KNQG (pf_extension.cnts_knqg_doc — sổ chỉ ghi thêm qua shared.apply_ledger) và
// số liệu thực tế từ nhật ký nuôi (pf_extension.cnts_diary_cycle, cnts_diary_entry — W11.48, cùng module, chỉ đọc) — W11.53
// — trên pool vai extension_app, luôn trong giao dịch có ngữ cảnh tenant (RLS).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/domain"
)

// Meta là cột chung của một dòng sổ.
type Meta struct {
	ID            uuid.UUID
	Seq           int64
	EventTime     time.Time
	RecordedAt    time.Time
	ActorRef      uuid.UUID
	OnBehalfOf    *uuid.UUID
	SupportReason *string
	IdemKey       string
}

// Scan là tệp scan đã ký (kho đối tượng).
type Scan struct {
	Key, Name, Type, SHA256 string
	Size                    int64
}

// Doc là một phiên bản biên bản/báo cáo.
type Doc struct {
	Meta
	Ref          uuid.UUID
	Form         string
	ProjectRef   uuid.UUID
	SiteRef      *uuid.UUID
	Period       string
	Revision     int
	Event        string
	Status       string
	Content      domain.Content
	Scan         *Scan
	StatusReason string
}

// Lock tuần tự hóa ghi theo khóa nghiệp vụ.
func Lock(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.cntsknqg:' || $1::text, 0))", key)
	return err
}

func optText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

const docCols = `id, seq, doc_ref, form, project_ref, site_ref, coalesce(period, ''), revision, event, status, content,
  signed_key, signed_name, signed_type, signed_size, signed_sha256, coalesce(status_reason, ''),
  event_time, recorded_at, actor_ref, on_behalf_of, support_reason, idem_key`

func scanDoc(row pgx.Row) (Doc, error) {
	var d Doc
	var raw []byte
	var key, name, typ, sum *string
	var size *int64
	if err := row.Scan(&d.ID, &d.Seq, &d.Ref, &d.Form, &d.ProjectRef, &d.SiteRef, &d.Period, &d.Revision, &d.Event, &d.Status, &raw,
		&key, &name, &typ, &size, &sum, &d.StatusReason,
		&d.EventTime, &d.RecordedAt, &d.ActorRef, &d.OnBehalfOf, &d.SupportReason, &d.IdemKey); err != nil {
		return d, err
	}
	if err := json.Unmarshal(raw, &d.Content); err != nil {
		return d, err
	}
	if key != nil {
		d.Scan = &Scan{Key: *key, SHA256: deref(sum)}
		d.Scan.Name, d.Scan.Type = deref(name), deref(typ)
		if size != nil {
			d.Scan.Size = *size
		}
	}
	return d, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func collect(rows pgx.Rows, err error) ([]Doc, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Doc
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func one(d Doc, err error) (*Doc, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Insert ghi một phiên bản; Seq do DB tính.
func Insert(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d *Doc) error {
	raw, err := json.Marshal(d.Content)
	if err != nil {
		return err
	}
	var key, name, typ, sum *string
	var size *int64
	if d.Scan != nil {
		key, name, typ, sum, size = &d.Scan.Key, optText(d.Scan.Name), optText(d.Scan.Type), &d.Scan.SHA256, &d.Scan.Size
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_knqg_doc (id, tenant_id, stream_key, doc_ref, form, project_ref, site_ref,
	    period, revision, event, status, content, signed_key, signed_name, signed_type, signed_size, signed_sha256, status_reason,
	    event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
	  RETURNING seq, recorded_at`,
		d.ID, tid, "knqg:"+d.Ref.String(), d.Ref, d.Form, d.ProjectRef, d.SiteRef, optText(d.Period), d.Revision, d.Event, d.Status,
		string(raw), key, name, typ, size, sum, optText(d.StatusReason), d.EventTime.UTC(), d.ActorRef, d.OnBehalfOf, d.SupportReason,
		d.IdemKey, domain.RuleVersion).Scan(&d.Seq, &d.RecordedAt)
}

// Latest trả bản hiện hành (nil = không có).
func Latest(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Doc, error) {
	return one(scanDoc(tx.QueryRow(ctx, "SELECT "+docCols+` FROM pf_extension.cnts_knqg_doc WHERE doc_ref = $1 ORDER BY seq DESC LIMIT 1`, ref)))
}

// ByIdem trả phiên bản đã ghi với khóa idem.
func ByIdem(ctx context.Context, tx pgx.Tx, key string) (*Doc, error) {
	return one(scanDoc(tx.QueryRow(ctx, "SELECT "+docCols+" FROM pf_extension.cnts_knqg_doc WHERE idem_key = $1 LIMIT 1", key)))
}

// Versions trả mọi phiên bản (cũ trước).
func Versions(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Doc, error) {
	rows, err := tx.Query(ctx, "SELECT "+docCols+" FROM pf_extension.cnts_knqg_doc WHERE doc_ref = $1 ORDER BY seq", ref)
	return collect(rows, err)
}

// Filter lọc danh sách (bản hiện hành).
type Filter struct {
	Project *uuid.UUID
	Form    string
	Content bool // false → nội dung rỗng (màn danh sách)
}

// List trả bản hiện hành của các biểu (lọc phạm vi ở ứng dụng).
func List(ctx context.Context, tx pgx.Tx, f Filter) ([]Doc, error) {
	cols := docCols
	if !f.Content {
		cols = strings.Replace(docCols, "content,", "'{}'::jsonb,", 1)
	}
	rows, err := tx.Query(ctx, "SELECT "+cols+` FROM (
	    SELECT DISTINCT ON (doc_ref) * FROM pf_extension.cnts_knqg_doc
	    WHERE ($1::uuid IS NULL OR project_ref = $1) AND ($2 = '' OR form = $2)
	    ORDER BY doc_ref, seq DESC) d
	  ORDER BY form, coalesce(period, '') DESC, event_time DESC`, f.Project, f.Form)
	return collect(rows, err)
}

// ---------------------------------------------------------------------------
// Thực tế từ nhật ký (W11.48 — chỉ đọc)
// ---------------------------------------------------------------------------

// Diary đọc thực tế của điểm site từ nhật ký: chỉ lứa có ít nhất một lần ghi Đã xác thực.
func Diary(ctx context.Context, tx pgx.Tx, site uuid.UUID) (domain.DiaryFacts, error) {
	var f domain.DiaryFacts
	var out, profit *string
	var from, to *time.Time
	err := tx.QueryRow(ctx, `
	  WITH e AS (
	    SELECT DISTINCT ON (entry_ref) cycle_ref, status, entry_date FROM pf_extension.cnts_diary_entry
	    WHERE site_ref = $1 ORDER BY entry_ref, seq DESC),
	  v AS (SELECT cycle_ref, count(*) AS n, max(entry_date) AS last FROM e WHERE status = 'verified' GROUP BY cycle_ref),
	  c AS (
	    SELECT DISTINCT ON (cycle_ref) * FROM pf_extension.cnts_diary_cycle WHERE site_ref = $1 ORDER BY cycle_ref, seq DESC)
	  SELECT count(DISTINCT c.household_ref), coalesce(sum(c.initial_count), 0), coalesce(sum(v.n), 0),
	    (sum(c.revenue_output_kg) FILTER (WHERE c.event = 'closed'))::text,
	    (sum(c.revenue_amount - coalesce(c.cost_seed, 0) - coalesce(c.cost_feed, 0) - coalesce(c.cost_depreciation, 0)
	      - coalesce(c.cost_transport, 0) - coalesce(c.cost_vet, 0) - coalesce(c.cost_probiotic, 0)) FILTER (WHERE c.event = 'closed'))::text,
	    min(c.received_date), max(v.last)
	  FROM c JOIN v ON v.cycle_ref = c.cycle_ref`, site).Scan(&f.Households, &f.Heads, &f.Verified, &out, &profit, &from, &to)
	if err != nil {
		return f, err
	}
	if out != nil {
		d, err := decimal.NewFromString(*out)
		if err != nil {
			return f, err
		}
		f.OutputKg = &d
	}
	if profit != nil {
		d, err := decimal.NewFromString(*profit)
		if err != nil {
			return f, err
		}
		f.Profit = &d
	}
	f.From, f.To = from, to
	return f, nil
}
