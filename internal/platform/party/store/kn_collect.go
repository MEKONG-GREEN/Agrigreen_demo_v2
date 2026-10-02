package store

// KN v7.7 FN-XA-03.03 (migration pf 000078): danh sách HTX theo xã phụ trách, sổ sự kiện bù kn_profile_event
// khi sửa hồ sơ HTX/nông hộ và cập nhật bảng chiếu trong cùng giao dịch (R-21).
// Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant (tenant.WithTenantTx).

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// KNCoop là HTX kèm số hộ thành viên đang hoạt động và thời điểm cập nhật cuối của hồ sơ.
type KNCoop struct {
	Cooperative
	HouseholdCount int
	UpdatedAt      time.Time
}

// KNCoopFilter là điều kiện liệt kê HTX của màn "Danh sách HTX" (FN-XA-03.03).
type KNCoopFilter struct {
	All     bool
	Units   []string
	Commune string // rỗng = mọi xã trong phạm vi
	Status  string // active | inactive | all
	Q       string
	AreaMin *decimal.Decimal
	AreaMax *decimal.Decimal
	After   uuid.UUID
	Limit   int
}

const knCoopCols = coopCols + `, greatest(o.updated_at, c.updated_at),
  (SELECT count(*) FROM pf_party.household_membership m
     JOIN pf_party.household h ON h.tenant_id = m.tenant_id AND h.household_ref = m.household_ref
    WHERE m.tenant_id = o.tenant_id AND m.org_ref = o.ref AND m.until_date IS NULL AND h.active)`

const knCoopFrom = ` FROM pf_party.org o JOIN pf_party.cooperative c ON c.tenant_id = o.tenant_id AND c.org_ref = o.ref`

func scanKNCoop(row pgx.Row) (KNCoop, error) {
	var k KNCoop
	var kind string
	var area, est, lat, lon *string
	err := row.Scan(&k.Ref, &kind, &k.Code, &k.Name, &k.AdminUnitCode, &k.ParentRef, &k.RepresentativeRef, &k.Active,
		&k.StatusReason, &k.LastSeq, &k.Profile.TaxCode, &area, &est, &lat, &lon, &k.Profile.Address, &k.UpdatedAt, &k.HouseholdCount)
	if err != nil {
		return k, err
	}
	k.Kind = OrgKindCooperative
	if k.Profile.AreaHa, err = parseDec(area); err != nil {
		return k, err
	}
	return k, nil
}

// OrgKindCooperative là kind của HTX (tránh import port trong hàm quét).
const OrgKindCooperative = "cooperative"

// ListKNCooperatives liệt kê HTX theo bộ lọc, phân trang cursor theo ref (R-53).
func ListKNCooperatives(ctx context.Context, tx pgx.Tx, f KNCoopFilter) ([]KNCoop, error) {
	rows, err := tx.Query(ctx, "SELECT "+knCoopCols+knCoopFrom+`
	    WHERE ($1::boolean OR o.admin_unit_code = ANY($2::text[]))
	      AND ($3 = '' OR o.admin_unit_code = $3)
	      AND ($4 = 'all' OR ($4 = 'active' AND o.active) OR ($4 = 'inactive' AND NOT o.active))
	      AND ($5 = '' OR o.name ILIKE '%' || $5 || '%' OR o.code ILIKE $5 || '%' OR c.tax_code LIKE $5 || '%')
	      AND ($6::numeric IS NULL OR c.area_ha >= $6::numeric)
	      AND ($7::numeric IS NULL OR c.area_ha <= $7::numeric)
	      AND o.ref > $8
	    ORDER BY o.ref LIMIT $9`,
		f.All, units(f.Units), f.Commune, f.Status, escapeLike(strings.TrimSpace(f.Q)), decPtr(f.AreaMin), decPtr(f.AreaMax), f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KNCoop{}
	for rows.Next() {
		k, err := scanKNCoop(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// KNCooperativeByRef đọc một HTX kèm số hộ; nil nếu không có (hoặc không phải HTX).
func KNCooperativeByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*KNCoop, error) {
	k, err := scanKNCoop(tx.QueryRow(ctx, "SELECT "+knCoopCols+knCoopFrom+" WHERE o.ref = $1", ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// UpdateOrgProfile ghi tên và người đại diện hiện hành của tổ chức (bảng chiếu, cùng giao dịch với sổ).
func UpdateOrgProfile(ctx context.Context, tx pgx.Tx, ref uuid.UUID, name string, representative uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE pf_party.org SET name = $2, representative_ref = $3, updated_at = now() WHERE ref = $1`,
		ref, name, nilIfZero(representative))
	return err
}

// UpdateCoopProfile ghi diện tích, mã số thuế, trụ sở hiện hành của HTX.
func UpdateCoopProfile(ctx context.Context, tx pgx.Tx, ref uuid.UUID, area *decimal.Decimal, taxCode, headOffice string) error {
	_, err := tx.Exec(ctx, `UPDATE pf_party.cooperative SET area_ha = $2::numeric, tax_code = $3, head_office = $4, updated_at = now()
	  WHERE org_ref = $1`, ref, decPtr(area), nilIfEmpty(taxCode), nilIfEmpty(strings.TrimSpace(headOffice)))
	return err
}

// UpdateHouseholdArea ghi diện tích canh tác hiện hành của nông hộ.
func UpdateHouseholdArea(ctx context.Context, tx pgx.Tx, ref uuid.UUID, area *decimal.Decimal) error {
	_, err := tx.Exec(ctx, `UPDATE pf_party.household SET cultivated_area_ha = $2::numeric, updated_at = now() WHERE household_ref = $1`,
		ref, decPtr(area))
	return err
}

// LockHousehold khóa dòng nông hộ để ghi trong cùng giao dịch.
func LockHousehold(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT 1 FROM pf_party.household WHERE household_ref = $1 FOR UPDATE`, ref)
	return err
}

// HouseholdMeta là người tạo, thời điểm tạo của nông hộ (dòng "created" của khung Log).
type HouseholdMeta struct {
	CreatedBy uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// HouseholdMetaByRef đọc người tạo, thời điểm tạo/cập nhật của nông hộ; nil nếu không có.
func HouseholdMetaByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*HouseholdMeta, error) {
	var m HouseholdMeta
	err := tx.QueryRow(ctx, `SELECT created_by, created_at, updated_at FROM pf_party.household WHERE household_ref = $1`, ref).
		Scan(&m.CreatedBy, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ---------------------------------------------------------------------------
// Sổ kn_profile_event
// ---------------------------------------------------------------------------

// Loại chủ thể của kn_profile_event.
const (
	ProfileCooperative = "cooperative"
	ProfileHousehold   = "household"
)

// ProfileRuleVersion là phiên bản quy tắc của sổ sửa hồ sơ KN.
const ProfileRuleVersion = "kn-profile-2026.10"

// ProfileEvent là một dòng sổ sửa hồ sơ HTX/nông hộ. Before/After là JSON phẳng KHÔNG có dữ liệu cá nhân.
type ProfileEvent struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	SubjectKind      string
	SubjectRef       uuid.UUID
	CommuneCode      string
	Action           string
	Before, After    []byte
	Reason           *string
	OnBehalfOf       *uuid.UUID
	SupportSessionID *uuid.UUID
	SupportReason    *string
	EventTime        time.Time
	ActorRef         uuid.UUID
	IdemKey          string
	Seq              int64
	RecordedAt       time.Time
}

// ProfileStream là stream_key của một chủ thể.
func ProfileStream(kind string, ref uuid.UUID) string {
	return "kn_profile:" + kind + ":" + ref.String()
}

// InsertProfileEvent ghi sổ; DB tính seq, chuỗi băm (shared.fn_chain).
func InsertProfileEvent(ctx context.Context, tx pgx.Tx, e *ProfileEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_party.kn_profile_event
	    (id, tenant_id, stream_key, subject_kind, subject_ref, commune_code, action, before, after, reason, on_behalf_of,
	     support_session_id, support_reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11, $12, $13, $14, $15, $16, $17) RETURNING seq, recorded_at`,
		e.ID, e.TenantID, ProfileStream(e.SubjectKind, e.SubjectRef), e.SubjectKind, e.SubjectRef, e.CommuneCode, e.Action,
		string(e.Before), string(e.After), e.Reason, e.OnBehalfOf, e.SupportSessionID, e.SupportReason, e.EventTime.UTC(), e.ActorRef,
		e.IdemKey, ProfileRuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

const profileEventCols = `id, tenant_id, subject_kind, subject_ref, commune_code, action, before, after, reason, on_behalf_of,
  support_session_id, support_reason, event_time, actor_ref, idem_key, seq, recorded_at`

func scanProfileEvent(row pgx.Row) (ProfileEvent, error) {
	var e ProfileEvent
	err := row.Scan(&e.ID, &e.TenantID, &e.SubjectKind, &e.SubjectRef, &e.CommuneCode, &e.Action, &e.Before, &e.After, &e.Reason,
		&e.OnBehalfOf, &e.SupportSessionID, &e.SupportReason, &e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.RecordedAt)
	return e, err
}

// ProfileEventByIdem đọc dòng sổ đã ghi với idem_key (gọi lặp PUT cùng Idempotency-Key); nil nếu chưa có.
func ProfileEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*ProfileEvent, error) {
	return noRows(scanProfileEvent(tx.QueryRow(ctx, "SELECT "+profileEventCols+" FROM pf_party.kn_profile_event WHERE idem_key = $1::text", key)))
}

// ProfileEvents trả sổ sửa hồ sơ của một chủ thể theo seq tăng dần.
func ProfileEvents(ctx context.Context, tx pgx.Tx, kind string, ref uuid.UUID) ([]ProfileEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+profileEventCols+` FROM pf_party.kn_profile_event
	    WHERE stream_key = $1 ORDER BY seq`, ProfileStream(kind, ref))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProfileEvent{}
	for rows.Next() {
		e, err := scanProfileEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CommuneParent trả Nút cha mặc định cho HTX mới của xã: Nút xã (extension_team) đang hoạt động của xã, nếu
// không có thì TTKN tỉnh đang hoạt động của tỉnh; Nil nếu không có cả hai (HTX đứng gốc — domain cho phép).
func CommuneParent(ctx context.Context, tx pgx.Tx, commune, province string) (uuid.UUID, error) {
	var ref uuid.UUID
	err := tx.QueryRow(ctx, `SELECT ref FROM pf_party.org
	    WHERE active AND ((kind = 'extension_team' AND admin_unit_code = $1) OR (kind = 'ttkn_province' AND admin_unit_code = $2 AND $2 <> ''))
	    ORDER BY (kind = 'extension_team') DESC, ref LIMIT 1`, commune, province).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return ref, err
}
