package store

// KN v7.7 FN-XA-03.03 (migration pf 000078, schema pf_coop từ W3.3): sổ sự kiện bù kn_profile_event khi sửa hồ
// sơ HTX/nông hộ và cập nhật bảng chiếu trong cùng giao dịch (R-21). Tên sổ (TG_ARGV) giữ pf_party.kn_profile_event.
// Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant (tenant.WithTenantTx).

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// UpdateHouseholdArea ghi diện tích canh tác hiện hành của nông hộ.
func UpdateHouseholdArea(ctx context.Context, tx pgx.Tx, ref uuid.UUID, area *decimal.Decimal) error {
	_, err := tx.Exec(ctx, `UPDATE pf_coop.household SET cultivated_area_ha = $2::numeric, updated_at = now() WHERE household_ref = $1`,
		ref, decPtr(area))
	return err
}

// LockHousehold khóa dòng nông hộ để ghi trong cùng giao dịch.
func LockHousehold(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT 1 FROM pf_coop.household WHERE household_ref = $1 FOR UPDATE`, ref)
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
	err := tx.QueryRow(ctx, `SELECT created_by, created_at, updated_at FROM pf_coop.household WHERE household_ref = $1`, ref).
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
	return tx.QueryRow(ctx, `INSERT INTO pf_coop.kn_profile_event
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
	return noRows(scanProfileEvent(tx.QueryRow(ctx, "SELECT "+profileEventCols+" FROM pf_coop.kn_profile_event WHERE idem_key = $1::text", key)))
}

// ProfileEvents trả sổ sửa hồ sơ của một chủ thể theo seq tăng dần.
func ProfileEvents(ctx context.Context, tx pgx.Tx, kind string, ref uuid.UUID) ([]ProfileEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+profileEventCols+` FROM pf_coop.kn_profile_event
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
