// Package store: SQL của danh sách hộ chăn nuôi CNTS (CNTS-V4-HOUSEHOLD-LIST #251, ADR-0056): sổ chỉ ghi thêm
// pf_extension.cnts_livestock_household (shared.apply_ledger, chuỗi 'lhh:<household_ref>'), mã hộ duy nhất
// (cnts_livestock_household_key), bộ đếm mã (cnts_livestock_household_counter), check-in tại nhà hộ
// (cnts_livestock_household_checkin, bảng thường). Pool vai extension_app, luôn trong giao dịch có ngữ cảnh tenant (RLS).
//
// Bên đọc khác trong pf/extension (#236 chiến dịch theo hộ, #252 ổ dịch nhiều hộ, báo cáo định kỳ, lớp bản đồ) dùng
// LatestHousehold / ListHouseholds — khóa là household_ref, không phải mã hiển thị.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/domain"
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

// Household là một phiên bản hộ chăn nuôi.
type Household struct {
	Meta
	Ref uuid.UUID
	// Code là mã hộ HCN-01 (chép vào mọi phiên bản).
	Code      string
	Event     string
	Status    string
	PersonRef uuid.UUID
	domain.Fields
	CheckinRef        *uuid.UUID
	ChangedFields     []string
	PIIRevisionRef    *uuid.UUID
	Reason            string
	OriginOutbreakRef *uuid.UUID
}

const cols = `id, seq, household_ref, code, event, status, commune_code, coalesce(hamlet, ''), person_ref, livestock_type,
  species_codes, head_counts, checkin_ref, changed_fields, pii_revision_ref, coalesce(reason, ''), origin_outbreak_ref,
  event_time, recorded_at, actor_ref, on_behalf_of, support_reason, idem_key`

func scan(row pgx.Row) (Household, error) {
	var h Household
	var species []string
	var heads []int64
	err := row.Scan(&h.ID, &h.Seq, &h.Ref, &h.Code, &h.Event, &h.Status, &h.CommuneCode, &h.Hamlet, &h.PersonRef, &h.LivestockType,
		&species, &heads, &h.CheckinRef, &h.ChangedFields, &h.PIIRevisionRef, &h.Reason, &h.OriginOutbreakRef,
		&h.EventTime, &h.RecordedAt, &h.ActorRef, &h.OnBehalfOf, &h.SupportReason, &h.IdemKey)
	if err != nil {
		return h, err
	}
	if len(species) != len(heads) {
		return h, errors.New("store: species_codes và head_counts lệch độ dài")
	}
	h.Animals = make([]domain.Animal, len(species))
	for i := range species {
		h.Animals[i] = domain.Animal{Species: species[i], Heads: heads[i]}
	}
	return h, nil
}

func collect(rows pgx.Rows, err error) ([]Household, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Household
	for rows.Next() {
		h, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func one(h Household, err error) (*Household, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func refStrings(rs []uuid.UUID) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.String()
	}
	return out
}

func optText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Lock tuần tự hóa ghi theo hộ.
func Lock(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.cnts_livestock_household:' || $1::text, 0))", ref.String())
	return err
}

// Insert ghi một phiên bản (DB tính seq, prev_hash, row_hash).
func Insert(ctx context.Context, tx pgx.Tx, tid uuid.UUID, h *Household) error {
	species := make([]string, len(h.Animals))
	heads := make([]int64, len(h.Animals))
	for i, a := range h.Animals {
		species[i], heads[i] = a.Species, a.Heads
	}
	changed := h.ChangedFields
	if changed == nil {
		changed = []string{}
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_livestock_household (id, tenant_id, stream_key, household_ref, code, event,
	    status, commune_code, hamlet, person_ref, livestock_type, species_codes, head_counts, checkin_ref, changed_fields,
	    pii_revision_ref, reason, origin_outbreak_ref, event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
	  RETURNING seq, recorded_at`,
		h.ID, tid, "lhh:"+h.Ref.String(), h.Ref, h.Code, h.Event, h.Status, h.CommuneCode, optText(h.Hamlet), h.PersonRef,
		h.LivestockType, species, heads, h.CheckinRef, changed, h.PIIRevisionRef, optText(h.Reason), h.OriginOutbreakRef,
		h.EventTime.UTC(), h.ActorRef, h.OnBehalfOf, h.SupportReason, h.IdemKey, domain.RuleVersion).Scan(&h.Seq, &h.RecordedAt)
}

// Latest trả bản hiện hành của hộ (nil = không có).
func Latest(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Household, error) {
	return one(scan(tx.QueryRow(ctx, "SELECT "+cols+` FROM pf_extension.cnts_livestock_household
	  WHERE household_ref = $1 ORDER BY seq DESC LIMIT 1`, ref)))
}

// ByIdem trả phiên bản đã ghi với khóa idem (nil = chưa).
func ByIdem(ctx context.Context, tx pgx.Tx, key string) (*Household, error) {
	return one(scan(tx.QueryRow(ctx, "SELECT "+cols+" FROM pf_extension.cnts_livestock_household WHERE idem_key = $1 LIMIT 1", key)))
}

// Versions trả mọi phiên bản của hộ (cũ trước).
func Versions(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Household, error) {
	rows, err := tx.Query(ctx, "SELECT "+cols+" FROM pf_extension.cnts_livestock_household WHERE household_ref = $1 ORDER BY seq", ref)
	return collect(rows, err)
}

// Filter lọc danh sách (bản hiện hành). Communes nil = mọi xã; chuỗi rỗng = không lọc.
type Filter struct {
	Communes      []string
	Status        string
	LivestockType string
	Limit, Offset int
}

// List trả một trang bản hiện hành theo bộ lọc (sắp theo mã hộ) và tổng số hộ khớp.
func List(ctx context.Context, tx pgx.Tx, f Filter) ([]Household, int, error) {
	const cur = `SELECT DISTINCT ON (household_ref) * FROM pf_extension.cnts_livestock_household
	    WHERE ($1::text[] IS NULL OR commune_code = ANY ($1::text[])) ORDER BY household_ref, seq DESC`
	const where = ` WHERE ($2 = '' OR h.status = $2) AND ($3 = '' OR h.livestock_type = $3)`
	var total int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM ("+cur+") h"+where, f.Communes, f.Status, f.LivestockType).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, "SELECT "+cols+" FROM ("+cur+") h"+where+" ORDER BY h.code LIMIT $4 OFFSET $5",
		f.Communes, f.Status, f.LivestockType, f.Limit, f.Offset)
	out, err := collect(rows, err)
	return out, total, err
}

// ActiveInCommune trả tối đa limit hộ Đang dùng (bản hiện hành) của xã, ghi gần nhất trước — cảnh báo trùng.
func ActiveInCommune(ctx context.Context, tx pgx.Tx, commune string, limit int) ([]Household, error) {
	rows, err := tx.Query(ctx, "SELECT "+cols+` FROM (
	    SELECT DISTINCT ON (household_ref) * FROM pf_extension.cnts_livestock_household
	    WHERE household_ref IN (SELECT household_ref FROM pf_extension.cnts_livestock_household WHERE commune_code = $1)
	    ORDER BY household_ref, seq DESC) h
	  WHERE h.status = 'active' AND h.commune_code = $1 ORDER BY h.recorded_at DESC LIMIT $2`, commune, limit)
	return collect(rows, err)
}

// NextCode cấp mã hộ kế tiếp của tenant (bộ đếm không dùng lại — ADR-0056 mục 2), cùng giao dịch ghi phiên bản đầu.
func NextCode(ctx context.Context, tx pgx.Tx, tid uuid.UUID) (string, error) {
	var n int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_livestock_household_counter AS c (tenant_id, last) VALUES ($1, 1)
	  ON CONFLICT (tenant_id) DO UPDATE SET last = c.last + 1 RETURNING last`, tid).Scan(&n)
	if err != nil {
		return "", err
	}
	return domain.Code(n), nil
}

// InsertKey ghi mã hộ (UNIQUE (tenant_id, code)).
func InsertKey(ctx context.Context, tx pgx.Tx, tid, ref uuid.UUID, code string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_livestock_household_key (tenant_id, household_ref, code) VALUES ($1, $2, $3)`,
		tid, ref, code)
	return err
}

// Checkin là một dòng check-in tại nhà hộ.
type Checkin struct {
	ID           uuid.UUID
	HouseholdRef uuid.UUID
	domain.Checkin
	RecordedAt time.Time
	ActorRef   uuid.UUID
	Reason     string
}

// InsertCheckin ghi một lần check-in.
func InsertCheckin(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c Checkin) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_livestock_household_checkin (id, tenant_id, household_ref, location,
	    gps_accuracy_m, mocked, captured_at, actor_ref, reason)
	  VALUES ($1, $2, $3, ST_SetSRID(ST_MakePoint($4::float8, $5::float8), 4326), $6::numeric, $7, $8, $9, $10)`,
		c.ID, tid, c.HouseholdRef, c.Lon, c.Lat, c.AccuracyM.String(), c.Mocked, c.CapturedAt.UTC(), c.ActorRef, optText(c.Reason))
	return err
}

// Checkins đọc các lần check-in theo mã (mã đã xóa theo yêu cầu xóa dữ liệu → vắng).
func Checkins(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]Checkin, error) {
	out := map[uuid.UUID]Checkin{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT id, household_ref, ST_Y(location), ST_X(location), gps_accuracy_m::text, mocked, captured_at,
	    recorded_at, actor_ref, coalesce(reason, '')
	  FROM pf_extension.cnts_livestock_household_checkin WHERE id = ANY ($1::text[]::uuid[])`, refStrings(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Checkin
		var acc string
		if err := rows.Scan(&c.ID, &c.HouseholdRef, &c.Lat, &c.Lon, &acc, &c.Mocked, &c.CapturedAt, &c.RecordedAt, &c.ActorRef,
			&c.Reason); err != nil {
			return nil, err
		}
		if c.AccuracyM, err = decimal.NewFromString(acc); err != nil {
			return nil, err
		}
		out[c.ID] = c
	}
	return out, rows.Err()
}

// TypeCount là số hộ Đang dùng (bản hiện hành) của một xã theo loại chăn nuôi HCN-06.
type TypeCount struct {
	Commune, Type string
	Households    int
}

// HeadCount là tổng số con của hộ Đang dùng (bản hiện hành) theo vật nuôi × loại chăn nuôi (chuỗi thập phân).
type HeadCount struct {
	Species, Type, Heads string
}

// activeCurrent là bản hiện hành của mọi hộ, giữ hộ Đang dùng thuộc các xã $1 (NULL = mọi xã). Lọc xã SAU khi lấy bản
// hiện hành: hộ đã chuyển xã chỉ tính ở xã hiện tại.
const activeCurrent = `SELECT * FROM (SELECT DISTINCT ON (household_ref) status, commune_code, livestock_type, species_codes, head_counts
    FROM pf_extension.cnts_livestock_household ORDER BY household_ref, seq DESC) h
  WHERE h.status = 'active' AND ($1::text[] IS NULL OR h.commune_code = ANY ($1::text[]))`

// ActiveTally đếm hộ Đang dùng theo xã × loại chăn nuôi và cộng số con theo vật nuôi × loại (FN-16 BR-07, ADR-0056 mục 9):
// hộ Chờ bổ sung vị trí, Vô hiệu hóa không tính (FN-10 BR-08). communes nil = mọi xã.
func ActiveTally(ctx context.Context, tx pgx.Tx, communes []string) ([]TypeCount, []HeadCount, error) {
	rows, err := tx.Query(ctx, `SELECT commune_code, livestock_type, count(*)::int FROM (`+activeCurrent+`) a
	  GROUP BY 1, 2 ORDER BY 1, 2`, communes)
	if err != nil {
		return nil, nil, err
	}
	types, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TypeCount, error) {
		var t TypeCount
		return t, r.Scan(&t.Commune, &t.Type, &t.Households)
	})
	if err != nil {
		return nil, nil, err
	}
	rows, err = tx.Query(ctx, `SELECT s.code, a.livestock_type, sum(s.heads)::text FROM (`+activeCurrent+`) a,
	    unnest(a.species_codes, a.head_counts) AS s(code, heads)
	  GROUP BY 1, 2 ORDER BY 1, 2`, communes)
	if err != nil {
		return nil, nil, err
	}
	heads, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (HeadCount, error) {
		var h HeadCount
		return h, r.Scan(&h.Species, &h.Type, &h.Heads)
	})
	return types, heads, err
}
