package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DispatchLink là liên kết tải tệp Chi cục do api-platform phục vụ (A4, pf 000085). Chỉ giữ băm của mã thông báo.
type DispatchLink struct {
	ID           uuid.UUID
	DispatchID   uuid.UUID
	FileID       uuid.UUID
	RecipientRef uuid.UUID
	TokenHash    []byte
	TTLHours     int
	ExpiresAt    time.Time
	CreatedAt    time.Time
	RevokedAt    *time.Time
}

// InsertDispatchLink ghi một liên kết.
func InsertDispatchLink(ctx context.Context, tx pgx.Tx, tid uuid.UUID, l DispatchLink) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_reports.dispatch_link (tenant_id, id, dispatch_id, file_id, recipient_ref, token_hash, ttl_hours,
	    expires_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		tid, l.ID, l.DispatchID, l.FileID, l.RecipientRef, l.TokenHash, l.TTLHours, l.ExpiresAt.UTC(), l.CreatedAt.UTC())
	return err
}

// DispatchLinkByID đọc một liên kết (nil khi không có).
func DispatchLinkByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*DispatchLink, error) {
	var l DispatchLink
	err := tx.QueryRow(ctx, `SELECT id, dispatch_id, file_id, recipient_ref, token_hash, ttl_hours, expires_at, created_at, revoked_at
	  FROM pf_reports.dispatch_link WHERE id = $1::uuid`, id).Scan(&l.ID, &l.DispatchID, &l.FileID, &l.RecipientRef, &l.TokenHash, &l.TTLHours,
		&l.ExpiresAt, &l.CreatedAt, &l.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// RevokeDispatchLinks thu hồi mọi liên kết còn hiệu lực của một lần gửi; trả số liên kết bị thu hồi.
func RevokeDispatchLinks(ctx context.Context, tx pgx.Tx, dispatchID, actor uuid.UUID, reason string, at time.Time) (int64, error) {
	tag, err := tx.Exec(ctx, `UPDATE pf_reports.dispatch_link SET revoked_at = $2, revoked_by = $3, revoke_reason = $4
	  WHERE dispatch_id = $1::uuid AND revoked_at IS NULL`, dispatchID, at.UTC(), actor, reason)
	return tag.RowsAffected(), err
}

// LinkAccess là một dòng sổ truy cập liên kết tải (không dữ liệu cá nhân).
type LinkAccess struct {
	ID           uuid.UUID
	LinkID       uuid.UUID
	DispatchID   uuid.UUID
	FileID       uuid.UUID
	RecipientRef uuid.UUID
	Outcome      string
	IPHash       *string
	EventTime    time.Time
	RecordedAt   time.Time
	Seq          int64
}

// Kết quả truy cập liên kết.
const (
	LinkServed       = "served"
	LinkExpired      = "expired"
	LinkRevoked      = "revoked"
	LinkInvalid      = "invalid"
	LinkEgressDenied = "egress_denied"
)

// LinkAccessRuleVersion ghi vào sổ dispatch_link_access.
const LinkAccessRuleVersion = "reports.dispatch_link.v1"

// InsertLinkAccess ghi một dòng sổ truy cập (seq, băm chuỗi do DB tính).
func InsertLinkAccess(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a *LinkAccess) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_reports.dispatch_link_access (id, tenant_id, stream_key, link_id, dispatch_id, file_id, recipient_ref,
	    outcome, ip_hash, event_time, actor_ref, idem_key, rule_version) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $7, $11, $12)
	  RETURNING seq, recorded_at`, a.ID, tid, "dispatch:"+a.DispatchID.String(), a.LinkID, a.DispatchID, a.FileID, a.RecipientRef, a.Outcome, a.IPHash,
		a.EventTime.UTC(), "link-access:"+a.ID.String(), LinkAccessRuleVersion).Scan(&a.Seq, &a.RecordedAt)
}

// LinkAccesses trả sổ truy cập liên kết của một lần gửi (mới nhất trước).
func LinkAccesses(ctx context.Context, tx pgx.Tx, dispatchID uuid.UUID) ([]LinkAccess, error) {
	rows, err := tx.Query(ctx, `SELECT id, link_id, dispatch_id, file_id, recipient_ref, outcome, ip_hash, event_time, recorded_at, seq
	  FROM pf_reports.dispatch_link_access WHERE dispatch_id = $1::uuid ORDER BY seq DESC LIMIT 500`, dispatchID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (LinkAccess, error) {
		var a LinkAccess
		err := r.Scan(&a.ID, &a.LinkID, &a.DispatchID, &a.FileID, &a.RecipientRef, &a.Outcome, &a.IPHash, &a.EventTime, &a.RecordedAt, &a.Seq)
		return a, err
	})
}
