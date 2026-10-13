// Package store: SQL của neo HTX (pf 20261013091454 — D2, ADR-0052): sổ pf_coop.htx_anchor_event, bảng chiếu htx_anchor,
// bí danh htx_anchor_alias, khóa nhận quyền htx_anchor_claim. Mọi hàm nhận pgx.Tx đã ở vai r_cooperative + tenant.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// RuleVersion ghim vào rule_version của sổ neo.
const RuleVersion = "cooperative.htx_anchor.v1"

// ErrUnique: vi phạm ràng buộc duy nhất (khóa neo, MST, Nút đã thuộc neo khác, neo đã nhận quyền).
var ErrUnique = errors.New("anchor: trùng khóa duy nhất")

func classify(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		if c := ledger.Classify(err); errors.Is(c, ledger.ErrDuplicate) {
			return c
		}
		return ErrUnique
	}
	return ledger.Classify(err)
}

// Anchor là dòng bảng chiếu.
type Anchor struct {
	TenantID          uuid.UUID
	AnchorID          uuid.UUID
	AnchorKey         string
	MST               *string
	Status            string
	CreatedByKind     string
	CreatedBy         uuid.UUID
	ProvisionalOrgRef uuid.UUID
	OrgRef            uuid.UUID
	HTXAccountRef     *uuid.UUID
	AgreementRef      *string
	ClaimedAt         *time.Time
	ClaimedBy         *uuid.UUID
	LastEventID       uuid.UUID
	LastSeq           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

const anchorCols = `tenant_id, anchor_id, anchor_key, mst, status, created_by_kind, created_by, provisional_org_ref, org_ref,
  htx_account_ref, agreement_ref, claimed_at, claimed_by, last_event_id, last_seq, created_at, updated_at`

func scanAnchor(row pgx.Row) (Anchor, error) {
	var a Anchor
	err := row.Scan(&a.TenantID, &a.AnchorID, &a.AnchorKey, &a.MST, &a.Status, &a.CreatedByKind, &a.CreatedBy, &a.ProvisionalOrgRef,
		&a.OrgRef, &a.HTXAccountRef, &a.AgreementRef, &a.ClaimedAt, &a.ClaimedBy, &a.LastEventID, &a.LastSeq, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func one(row pgx.Row) (*Anchor, error) {
	a, err := scanAnchor(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ByID đọc neo (forUpdate khóa dòng tới hết giao dịch); nil nếu không có.
func ByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Anchor, error) {
	q := "SELECT " + anchorCols + " FROM pf_coop.htx_anchor WHERE anchor_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return one(tx.QueryRow(ctx, q, id))
}

// ByKey đọc neo theo khóa (MST chính tắc hoặc mã tạm); nil nếu không có.
func ByKey(ctx context.Context, tx pgx.Tx, key string) (*Anchor, error) {
	return one(tx.QueryRow(ctx, "SELECT "+anchorCols+" FROM pf_coop.htx_anchor WHERE anchor_key = $1 OR mst = $1", key))
}

// ByOrg đọc neo mà Nút là bí danh; nil nếu Nút chưa thuộc neo nào.
func ByOrg(ctx context.Context, tx pgx.Tx, org uuid.UUID) (*Anchor, error) {
	return one(tx.QueryRow(ctx, "SELECT "+prefixed("a")+` FROM pf_coop.htx_anchor a
	  JOIN pf_coop.htx_anchor_alias al ON al.tenant_id = a.tenant_id AND al.anchor_id = a.anchor_id
	  WHERE al.org_ref = $1`, org))
}

func prefixed(alias string) string {
	return alias + ".tenant_id, " + alias + ".anchor_id, " + alias + ".anchor_key, " + alias + ".mst, " + alias + ".status, " +
		alias + ".created_by_kind, " + alias + ".created_by, " + alias + ".provisional_org_ref, " + alias + ".org_ref, " +
		alias + ".htx_account_ref, " + alias + ".agreement_ref, " + alias + ".claimed_at, " + alias + ".claimed_by, " +
		alias + ".last_event_id, " + alias + ".last_seq, " + alias + ".created_at, " + alias + ".updated_at"
}

// Filter lọc danh sách neo.
type Filter struct {
	Status string // provisional | claimed | rỗng
	Q      string // tiền tố khóa neo / MST
	After  uuid.UUID
	Limit  int
}

// List liệt kê neo theo anchor_id tăng dần.
func List(ctx context.Context, tx pgx.Tx, f Filter) ([]Anchor, error) {
	rows, err := tx.Query(ctx, "SELECT "+anchorCols+` FROM pf_coop.htx_anchor
	  WHERE ($1 = '' OR status = $1) AND ($2 = '' OR anchor_key LIKE $2 || '%') AND anchor_id > $3
	  ORDER BY anchor_id LIMIT $4`, f.Status, f.Q, f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Anchor
	for rows.Next() {
		a, err := scanAnchor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Event là một dòng sổ neo.
type Event struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	AnchorID          uuid.UUID
	Seq               int64
	Action            string
	AnchorKey         string
	MST               *string
	CreatedByKind     string
	ProvisionalOrgRef uuid.UUID
	OrgRef            uuid.UUID
	HTXAccountRef     *uuid.UUID
	VerificationRef   *string
	AgreementRef      *string
	Details           json.RawMessage
	EventTime         time.Time
	RecordedAt        time.Time
	ActorRef          uuid.UUID
	IdemKey           string
}

// InsertEvent ghi một dòng sổ (ledger.ErrDuplicate khi idem_key đã ghi). Trả seq do trg_chain gán.
func InsertEvent(ctx context.Context, tx pgx.Tx, e Event) (int64, error) {
	var details any
	if len(e.Details) > 0 {
		details = string(e.Details)
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_coop.htx_anchor_event (id, tenant_id, stream_key, anchor_id, action, anchor_key, mst,
	    created_by_kind, provisional_org_ref, org_ref, htx_account_ref, verification_ref, agreement_ref, details, event_time, actor_ref,
	    idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, $15, $16, $17, $18)
	  RETURNING seq`,
		e.ID, e.TenantID, "anchor:"+e.AnchorID.String(), e.AnchorID, e.Action, e.AnchorKey, e.MST, e.CreatedByKind, e.ProvisionalOrgRef,
		e.OrgRef, e.HTXAccountRef, e.VerificationRef, e.AgreementRef, details, e.EventTime.UTC(), e.ActorRef, e.IdemKey, RuleVersion).Scan(&seq)
	return seq, classify(err)
}

// EventByIdem đọc dòng sổ đã ghi với idem_key; nil nếu chưa có.
func EventByIdem(ctx context.Context, tx pgx.Tx, key string) (*Event, error) {
	evs, err := events(ctx, tx, "idem_key = $1", key)
	if err != nil || len(evs) == 0 {
		return nil, err
	}
	return &evs[0], nil
}

// Events trả sổ của một neo theo seq tăng dần (vết kiểm toán).
func Events(ctx context.Context, tx pgx.Tx, anchorID uuid.UUID) ([]Event, error) {
	return events(ctx, tx, "anchor_id = $1", anchorID)
}

func events(ctx context.Context, tx pgx.Tx, where string, arg any) ([]Event, error) {
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, anchor_id, seq, action, anchor_key, mst, created_by_kind, provisional_org_ref, org_ref,
	    htx_account_ref, verification_ref, agreement_ref, coalesce(details::text, ''), event_time, recorded_at, actor_ref, idem_key
	  FROM pf_coop.htx_anchor_event WHERE `+where+` ORDER BY seq`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var details string
		if err := rows.Scan(&e.ID, &e.TenantID, &e.AnchorID, &e.Seq, &e.Action, &e.AnchorKey, &e.MST, &e.CreatedByKind,
			&e.ProvisionalOrgRef, &e.OrgRef, &e.HTXAccountRef, &e.VerificationRef, &e.AgreementRef, &details, &e.EventTime,
			&e.RecordedAt, &e.ActorRef, &e.IdemKey); err != nil {
			return nil, err
		}
		if details != "" {
			e.Details = json.RawMessage(details)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// InsertAnchor ghi dòng chiếu của neo mới (ErrUnique khi khóa/MST/Nút đã có).
func InsertAnchor(ctx context.Context, tx pgx.Tx, a Anchor) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_coop.htx_anchor (tenant_id, anchor_id, anchor_key, mst, status, created_by_kind, created_by,
	    provisional_org_ref, org_ref, agreement_ref, last_event_id, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		a.TenantID, a.AnchorID, a.AnchorKey, a.MST, a.Status, a.CreatedByKind, a.CreatedBy, a.ProvisionalOrgRef, a.OrgRef, a.AgreementRef,
		a.LastEventID, a.LastSeq)
	return classify(err)
}

// SetMST cập nhật bảng chiếu sau mst_attached.
func SetMST(ctx context.Context, tx pgx.Tx, id uuid.UUID, mst string, ev uuid.UUID, seq int64) error {
	_, err := tx.Exec(ctx, `UPDATE pf_coop.htx_anchor SET mst = $2, anchor_key = $2, last_event_id = $3, last_seq = $4, updated_at = now()
	  WHERE anchor_id = $1`, id, mst, ev, seq)
	return classify(err)
}

// SetClaimed cập nhật bảng chiếu sau claimed.
func SetClaimed(ctx context.Context, tx pgx.Tx, id, org, account, by uuid.UUID, at time.Time, ev uuid.UUID, seq int64) error {
	_, err := tx.Exec(ctx, `UPDATE pf_coop.htx_anchor SET status = 'claimed', org_ref = $2, htx_account_ref = $3, claimed_by = $4,
	    claimed_at = $5, last_event_id = $6, last_seq = $7, updated_at = now()
	  WHERE anchor_id = $1`, id, org, account, by, at.UTC(), ev, seq)
	return classify(err)
}

// InsertAlias gắn Nút vào neo (ErrUnique khi Nút đã thuộc neo).
func InsertAlias(ctx context.Context, tx pgx.Tx, tid, org, anchorID uuid.UUID, role string, ev uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_coop.htx_anchor_alias (tenant_id, org_ref, anchor_id, role, event_id) VALUES ($1, $2, $3, $4, $5)`,
		tid, org, anchorID, role, ev)
	return classify(err)
}

// Aliases trả các Nút của neo.
func Aliases(ctx context.Context, tx pgx.Tx, anchorID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, "SELECT org_ref FROM pf_coop.htx_anchor_alias WHERE anchor_id = $1 ORDER BY created_at, org_ref", anchorID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// InsertClaim ghi khóa nhận quyền (ErrUnique khi neo đã nhận quyền).
func InsertClaim(ctx context.Context, tx pgx.Tx, tid, anchorID, ev, org uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_coop.htx_anchor_claim (tenant_id, anchor_id, event_id, org_ref, claimed_at) VALUES ($1, $2, $3, $4, $5)`,
		tid, anchorID, ev, org, at.UTC())
	return classify(err)
}

// CurrentOrg phân giải Nút qua bí danh: Nút thuộc neo → Nút hiện hành của neo; không thuộc neo → chính nó.
func CurrentOrg(ctx context.Context, tx pgx.Tx, org uuid.UUID) (uuid.UUID, error) {
	var cur uuid.UUID
	err := tx.QueryRow(ctx, `SELECT a.org_ref FROM pf_coop.htx_anchor_alias al
	  JOIN pf_coop.htx_anchor a ON a.tenant_id = al.tenant_id AND a.anchor_id = al.anchor_id WHERE al.org_ref = $1`, org).Scan(&cur)
	if errors.Is(err, pgx.ErrNoRows) {
		return org, nil
	}
	return cur, err
}

// ConfirmedSQL là biểu thức "Nút <col> đã xác nhận" (neo claimed qua bí danh) cho truy vấn của module.
func ConfirmedSQL(tenantCol, orgCol string) string {
	return `EXISTS (SELECT 1 FROM pf_coop.htx_anchor_alias al JOIN pf_coop.htx_anchor a
	  ON a.tenant_id = al.tenant_id AND a.anchor_id = al.anchor_id
	  WHERE al.tenant_id = ` + tenantCol + ` AND al.org_ref = ` + orgCol + ` AND a.status = 'claimed')`
}

// Confirmed trả trạng thái đã xác nhận của từng Nút (Nút không có neo claimed → false).
func Confirmed(ctx context.Context, tx pgx.Tx, orgs []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := make(map[uuid.UUID]bool, len(orgs))
	for _, o := range orgs {
		out[o] = false
	}
	if len(orgs) == 0 {
		return out, nil
	}
	strs := make([]string, len(orgs))
	for i, o := range orgs {
		strs[i] = o.String()
	}
	rows, err := tx.Query(ctx, `SELECT al.org_ref FROM pf_coop.htx_anchor_alias al
	  JOIN pf_coop.htx_anchor a ON a.tenant_id = al.tenant_id AND a.anchor_id = al.anchor_id
	  WHERE al.org_ref = ANY($1::text[]::uuid[]) AND a.status = 'claimed'`, strs)
	if err != nil {
		return nil, err
	}
	refs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	for _, r := range refs {
		out[r] = true
	}
	return out, nil
}

// CoopProfile là hồ sơ HTX tối thiểu để kiểm neo.
type CoopProfile struct {
	OrgRef  uuid.UUID
	TaxCode string
}

// ProfileByOrg đọc hồ sơ HTX của Nút; nil nếu Nút không có hồ sơ (không phải HTX).
func ProfileByOrg(ctx context.Context, tx pgx.Tx, org uuid.UUID) (*CoopProfile, error) {
	var p CoopProfile
	err := tx.QueryRow(ctx, "SELECT org_ref, coalesce(tax_code, '') FROM pf_coop.cooperative WHERE org_ref = $1", org).Scan(&p.OrgRef, &p.TaxCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// ProfileByTaxCode đọc Nút có hồ sơ mang mã số thuế; nil nếu không có.
func ProfileByTaxCode(ctx context.Context, tx pgx.Tx, tax string) (*CoopProfile, error) {
	var p CoopProfile
	err := tx.QueryRow(ctx, "SELECT org_ref, coalesce(tax_code, '') FROM pf_coop.cooperative WHERE tax_code = $1", tax).Scan(&p.OrgRef, &p.TaxCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// UnanchoredProfiles trả hồ sơ HTX chưa có neo (chuyển dữ liệu có trước D2), sắp theo org_ref.
func UnanchoredProfiles(ctx context.Context, tx pgx.Tx, limit int) ([]CoopProfile, error) {
	rows, err := tx.Query(ctx, `SELECT c.org_ref, coalesce(c.tax_code, '') FROM pf_coop.cooperative c
	  WHERE NOT EXISTS (SELECT 1 FROM pf_coop.htx_anchor_alias al WHERE al.tenant_id = c.tenant_id AND al.org_ref = c.org_ref)
	  ORDER BY c.org_ref LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CoopProfile
	for rows.Next() {
		var p CoopProfile
		if err := rows.Scan(&p.OrgRef, &p.TaxCode); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DataCounts là số dòng dữ liệu nghiệp vụ gắn một Nút HTX (vết "đã chuyển chủ" và báo cáo xung đột).
type DataCounts struct {
	Plots       int64 `json:"plots"`
	Memberships int64 `json:"memberships"`
	Seasons     int64 `json:"seasons"`
	FarmLogs    int64 `json:"farm_logs"`
	SeasonAreas int64 `json:"season_areas"`
}

// Total là tổng số dòng.
func (d DataCounts) Total() int64 {
	return d.Plots + d.Memberships + d.Seasons + d.FarmLogs + d.SeasonAreas
}

// CountData đếm dòng gắn Nút (bảng hiện tại/chiếu — sổ không bị đụng).
func CountData(ctx context.Context, tx pgx.Tx, org uuid.UUID) (DataCounts, error) {
	var d DataCounts
	err := tx.QueryRow(ctx, `SELECT
	    (SELECT count(*) FROM pf_coop.plot WHERE cooperative_ref = $1),
	    (SELECT count(*) FROM pf_coop.household_membership WHERE org_ref = $1),
	    (SELECT count(*) FROM pf_coop.season_current WHERE cooperative_ref = $1),
	    (SELECT count(*) FROM pf_coop.farm_log_current WHERE cooperative_ref = $1),
	    (SELECT count(*) FROM pf_coop.coop_season_area WHERE cooperative_ref = $1)`, org).
		Scan(&d.Plots, &d.Memberships, &d.Seasons, &d.FarmLogs, &d.SeasonAreas)
	return d, err
}
