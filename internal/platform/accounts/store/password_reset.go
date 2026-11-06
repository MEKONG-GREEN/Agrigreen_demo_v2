package store

// pf_accounts.password_reset (#255, ADR-0056): yêu cầu tự đặt lại mật khẩu. Không có email, IP hay mã thô.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PasswordReset là một dòng pf_accounts.password_reset.
type PasswordReset struct {
	ID           uuid.UUID
	UserRef      uuid.UUID
	RequestedAt  time.Time
	TokenHash    []byte
	SentAt       *time.Time
	ExpiresAt    *time.Time
	ClaimedUntil *time.Time
	UsedAt       *time.Time
}

// LockPasswordResetUser khóa advisory theo tài khoản trong giao dịch: đếm và ghi yêu cầu không chạy song song.
func LockPasswordResetUser(ctx context.Context, tx pgx.Tx, user uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_accounts.password_reset:' || $1::text, 0))`, user)
	return err
}

// CountPasswordResets đếm yêu cầu đã xếp gửi của tài khoản từ since.
func CountPasswordResets(ctx context.Context, tx pgx.Tx, user uuid.UUID, since time.Time) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_accounts.password_reset WHERE user_ref = $1 AND requested_at > $2`,
		user, since.UTC()).Scan(&n)
	return n, err
}

// InsertPasswordReset ghi một yêu cầu (chưa có mã).
func InsertPasswordReset(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r PasswordReset) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.password_reset (tenant_id, id, user_ref, requested_at) VALUES ($1, $2, $3, $4)`,
		tid, r.ID, r.UserRef, r.RequestedAt.UTC())
	return err
}

// PasswordResetByID đọc một yêu cầu; forUpdate khóa dòng. Không có → nil.
func PasswordResetByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*PasswordReset, error) {
	q := `SELECT id, user_ref, requested_at, token_hash, sent_at, expires_at, claimed_until, used_at
	  FROM pf_accounts.password_reset WHERE id = $1`
	if forUpdate {
		q += ` FOR UPDATE`
	}
	var r PasswordReset
	err := tx.QueryRow(ctx, q, id).Scan(&r.ID, &r.UserRef, &r.RequestedAt, &r.TokenHash, &r.SentAt, &r.ExpiresAt, &r.ClaimedUntil, &r.UsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// SetPasswordResetToken ghi băm mã và hạn của yêu cầu chưa gửi, chưa dùng (lần gửi lại thay mã chưa gửi).
func SetPasswordResetToken(ctx context.Context, tx pgx.Tx, id uuid.UUID, hash []byte, expires time.Time) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE pf_accounts.password_reset SET token_hash = $2, expires_at = $3
	  WHERE id = $1 AND sent_at IS NULL AND used_at IS NULL`, id, hash, expires.UTC())
	return tag.RowsAffected() == 1, err
}

// MarkPasswordResetSent ghi thời điểm đã gửi email.
func MarkPasswordResetSent(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_accounts.password_reset SET sent_at = $2 WHERE id = $1 AND sent_at IS NULL`, id, at.UTC())
	return err
}

// ClaimPasswordReset đặt lease khi xác nhận (dòng đã khóa FOR UPDATE).
func ClaimPasswordReset(ctx context.Context, tx pgx.Tx, id uuid.UUID, until time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_accounts.password_reset SET claimed_until = $2 WHERE id = $1 AND used_at IS NULL`, id, until.UTC())
	return err
}

// ReleasePasswordReset bỏ lease (đổi mật khẩu không thành — liên kết dùng lại được tới khi hết hạn).
func ReleasePasswordReset(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE pf_accounts.password_reset SET claimed_until = NULL WHERE id = $1 AND used_at IS NULL`, id)
	return err
}

// UsePasswordResets đánh dấu đã dùng yêu cầu id và mọi yêu cầu còn mở khác của tài khoản (liên kết cũ hết dùng).
func UsePasswordResets(ctx context.Context, tx pgx.Tx, user uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_accounts.password_reset SET used_at = $2, claimed_until = NULL
	  WHERE user_ref = $1 AND used_at IS NULL AND token_hash IS NOT NULL`, user, at.UTC())
	return err
}
