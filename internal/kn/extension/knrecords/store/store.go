// Package store: SQL hồ sơ KN riêng pf_extension.kn_producer (migration 20261028080436_pf_extension_kn_producer) trên
// vai r_extension, luôn trong giao dịch có ngữ cảnh tenant (RLS). Sổ chỉ ghi thêm (shared.apply_ledger); bản hiện hành
// = dòng có seq lớn nhất của chuỗi 'kn_producer:<producer_ref>'. Không nhắc bảng HTX nào.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knrecords/domain"
)

// Producer là một dòng sổ kn_producer.
type Producer struct {
	ID            uuid.UUID
	Seq           int64
	Ref           uuid.UUID
	Kind          string
	Event         string
	CommuneCode   string
	Name          *string
	PersonRef     uuid.UUID
	ParentRef     *uuid.UUID
	HTXRef        *uuid.UUID
	AreaHa        *decimal.Decimal
	TaxCode       *string
	Address       *string
	StatusReason  *string
	EventTime     time.Time
	RecordedAt    time.Time
	ActorRef      uuid.UUID
	OnBehalfOf    *uuid.UUID
	SupportReason *string
	IdemKey       string
}

func stream(ref uuid.UUID) string { return "kn_producer:" + ref.String() }

const cols = `id, seq, producer_ref, kind, event, commune_code, name, person_ref, parent_ref, htx_ref, area_ha, tax_code, address,
  status_reason, event_time, recorded_at, actor_ref, on_behalf_of, support_reason, idem_key`

func scan(row pgx.Row) (Producer, error) {
	var p Producer
	err := row.Scan(&p.ID, &p.Seq, &p.Ref, &p.Kind, &p.Event, &p.CommuneCode, &p.Name, &p.PersonRef, &p.ParentRef, &p.HTXRef, &p.AreaHa,
		&p.TaxCode, &p.Address, &p.StatusReason, &p.EventTime, &p.RecordedAt, &p.ActorRef, &p.OnBehalfOf, &p.SupportReason, &p.IdemKey)
	return p, err
}

func collect(rows pgx.Rows, err error) ([]Producer, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Producer{}
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Lock tuần tự hóa ghi một hồ sơ.
func Lock(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.'||$1::text, 0))", stream(ref))
	return err
}

// LockName tuần tự hóa tạo/đổi tên trong (loại, xã) để chặn trùng tên đồng thời.
func LockName(ctx context.Context, tx pgx.Tx, kind, commune string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.kn_producer_name:'||$1::text||':'||$2::text, 0))",
		kind, commune)
	return err
}

// Latest trả bản hiện hành; chưa có → nil.
func Latest(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Producer, error) {
	p, err := scan(tx.QueryRow(ctx, `SELECT `+cols+` FROM pf_extension.kn_producer WHERE producer_ref = $1::uuid
	  ORDER BY seq DESC LIMIT 1`, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Versions trả mọi phiên bản của hồ sơ theo seq tăng dần (nhật ký), tối đa limit dòng mới nhất.
func Versions(ctx context.Context, tx pgx.Tx, ref uuid.UUID, limit int) ([]Producer, error) {
	return collect(tx.Query(ctx, `SELECT `+cols+` FROM (SELECT * FROM pf_extension.kn_producer WHERE producer_ref = $1::uuid
	  ORDER BY seq DESC LIMIT $2::int) v ORDER BY seq`, ref, limit))
}

// ByIdem trả dòng đã ghi với khóa idempotency; chưa có → nil.
func ByIdem(ctx context.Context, tx pgx.Tx, key string) (*Producer, error) {
	p, err := scan(tx.QueryRow(ctx, `SELECT `+cols+` FROM pf_extension.kn_producer WHERE idem_key = $1::text
	  ORDER BY recorded_at DESC LIMIT 1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Insert ghi một dòng sổ (DB tính seq, băm).
func Insert(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p *Producer) error {
	var area any
	if p.AreaHa != nil {
		area = p.AreaHa.String()
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.kn_producer (id, tenant_id, stream_key, producer_ref, kind, event, commune_code,
	    name, person_ref, parent_ref, htx_ref, area_ha, tax_code, address, status_reason, event_time, actor_ref, on_behalf_of,
	    support_reason, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::text, $7::text, $8::text, $9::uuid, $10::uuid, $11::uuid,
	    $12::numeric, $13::text, $14::text, $15::text, $16::timestamptz, $17::uuid, $18::uuid, $19::text, $20::text, $21::text)
	  RETURNING seq, recorded_at`,
		p.ID, tid, stream(p.Ref), p.Ref, p.Kind, p.Event, p.CommuneCode, p.Name, p.PersonRef, p.ParentRef, p.HTXRef, area, p.TaxCode,
		p.Address, p.StatusReason, p.EventTime.UTC(), p.ActorRef, p.OnBehalfOf, p.SupportReason, p.IdemKey, domain.RuleVersion).
		Scan(&p.Seq, &p.RecordedAt)
}

// Filter là điều kiện đọc danh sách hiện hành. Phạm vi (All/Units) lọc TRONG SQL trước khi trả dòng.
type Filter struct {
	Kind    string
	All     bool
	Units   []string
	Commune string
	Status  string // active | inactive | all
	Q       string // tìm theo tên (HTX, THT), không phân biệt hoa thường
	Parent  *uuid.UUID
	HTX     *uuid.UUID
	After   uuid.UUID
	Limit   int
}

// List trả bản hiện hành theo producer_ref tăng dần (UUIDv7 ~ thứ tự tạo).
func List(ctx context.Context, tx pgx.Tx, f Filter) ([]Producer, error) {
	units := f.Units
	if units == nil {
		units = []string{}
	}
	return collect(tx.Query(ctx, `SELECT `+cols+` FROM (
	    SELECT DISTINCT ON (producer_ref) * FROM pf_extension.kn_producer
	    WHERE kind = $1::text AND producer_ref > $2::uuid
	    ORDER BY producer_ref, seq DESC) p
	  WHERE ($3::boolean OR p.commune_code = ANY($4::text[]))
	    AND ($5::text = '' OR p.commune_code = $5::text)
	    AND ($6::text = 'all' OR ($6::text = 'inactive') = (p.event = 'deactivated'))
	    AND ($7::text = '' OR strpos(lower(coalesce(p.name, '')), lower($7::text)) > 0)
	    AND ($8::uuid IS NULL OR p.parent_ref = $8::uuid)
	    AND ($9::uuid IS NULL OR p.htx_ref = $9::uuid)
	  ORDER BY p.producer_ref LIMIT $10::int`,
		f.Kind, f.After, f.All, units, f.Commune, f.Status, f.Q, f.Parent, f.HTX, f.Limit))
}

// ActiveNames trả tên của các hồ sơ khác cùng loại, cùng xã, đang hoạt động (app so trùng bằng domain.NameKey — không
// phụ thuộc collation của DB).
func ActiveNames(ctx context.Context, tx pgx.Tx, kind, commune string, except uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT p.name FROM (
	    SELECT DISTINCT ON (producer_ref) producer_ref, commune_code, name, event FROM pf_extension.kn_producer
	    WHERE kind = $1::text ORDER BY producer_ref, seq DESC) p
	  WHERE p.commune_code = $2::text AND p.event <> 'deactivated' AND p.producer_ref <> $3::uuid AND p.name IS NOT NULL`,
		kind, commune, except)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// PersonTaken: đã có Nông hộ khác đang hoạt động cùng chủ hộ (chủ thể kho định danh).
func PersonTaken(ctx context.Context, tx pgx.Tx, kind string, person, except uuid.UUID) (bool, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM (
	    SELECT DISTINCT ON (producer_ref) producer_ref, person_ref, event FROM pf_extension.kn_producer
	    WHERE kind = $1::text ORDER BY producer_ref, seq DESC) p
	  WHERE p.person_ref = $2::uuid AND p.event <> 'deactivated' AND p.producer_ref <> $3::uuid`, kind, person, except).Scan(&n)
	return n > 0, err
}

// MemberCounts đếm Nông hộ đang hoạt động theo HTX/THT cha (bản hiện hành).
func MemberCounts(ctx context.Context, tx pgx.Tx, parents []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(parents) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT p.parent_ref, count(*) FROM (
	    SELECT DISTINCT ON (producer_ref) producer_ref, parent_ref, event FROM pf_extension.kn_producer
	    WHERE kind = 'household' ORDER BY producer_ref, seq DESC) p
	  WHERE p.parent_ref = ANY($1::uuid[]) AND p.event <> 'deactivated'
	  GROUP BY p.parent_ref`, strs(parents))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref uuid.UUID
		var n int
		if err := rows.Scan(&ref, &n); err != nil {
			return nil, err
		}
		out[ref] = n
	}
	return out, rows.Err()
}

// Current trả bản hiện hành của các hồ sơ theo mã (tên HTX/THT cha của Nông hộ).
func Current(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) (map[uuid.UUID]Producer, error) {
	out := map[uuid.UUID]Producer{}
	if len(refs) == 0 {
		return out, nil
	}
	ps, err := collect(tx.Query(ctx, `SELECT `+cols+` FROM (SELECT DISTINCT ON (producer_ref) * FROM pf_extension.kn_producer
	  WHERE producer_ref = ANY($1::uuid[]) ORDER BY producer_ref, seq DESC) p`, strs(refs)))
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		out[p.Ref] = p
	}
	return out, nil
}

// strs đổi mã sang chuỗi để mã hóa uuid[] (pgx không có kế hoạch mã hóa []uuid.UUID).
func strs(refs []uuid.UUID) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.String()
	}
	return out
}
