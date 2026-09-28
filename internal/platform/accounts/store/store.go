// Package store: SQL của pf/accounts, chỉ chạy trong giao dịch tenant của vai
// accounts_app (R-05). Không có cột dữ liệu cá nhân.
package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

func causePtr(c port.LockCause) *string {
	if c == "" {
		return nil
	}
	s := string(c)
	return &s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// InsertEvent ghi một dòng sổ; seq, recorded_at do DB tính (shared.fn_chain).
func InsertEvent(ctx context.Context, tx pgx.Tx, e *domain.Event) error {
	var details *string
	if len(e.Details) > 0 {
		b, err := json.Marshal(e.Details)
		if err != nil {
			return err
		}
		s := string(b)
		details = &s
	}
	roles := e.Account.Roles
	if roles == nil {
		roles = []string{}
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_accounts.account_event
	    (id, tenant_id, stream_key, user_ref, action, org_ref, roles, enabled, lock_cause, reason, details,
	     event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14, $15)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, domain.StreamKey(e.Account.UserRef), e.Account.UserRef, string(e.Action), e.Account.OrgRef,
		roles, e.Account.Enabled, causePtr(e.Account.LockCause), strPtr(e.Reason), details,
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

const eventCols = `id, tenant_id, user_ref, action, org_ref, roles, enabled, coalesce(lock_cause, ''), coalesce(reason, ''),
  details, event_time, recorded_at, actor_ref, idem_key, seq`

func scanEvent(row pgx.Row) (domain.Event, error) {
	var e domain.Event
	var action, cause string
	var details []byte
	err := row.Scan(&e.ID, &e.TenantID, &e.Account.UserRef, &action, &e.Account.OrgRef, &e.Account.Roles, &e.Account.Enabled,
		&cause, &e.Reason, &details, &e.EventTime, &e.RecordedAt, &e.ActorRef, &e.IdemKey, &e.Seq)
	if err != nil {
		return e, err
	}
	e.Action, e.Account.LockCause = domain.Action(action), port.LockCause(cause)
	if len(details) > 0 {
		if err := json.Unmarshal(details, &e.Details); err != nil {
			return e, err
		}
	}
	return e, nil
}

// EventByIdemKey tìm dòng sổ đã ghi với idem_key (gọi lặp).
func EventByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Event, error) {
	e, err := scanEvent(tx.QueryRow(ctx, "SELECT "+eventCols+" FROM pf_accounts.account_event WHERE idem_key = $1 ORDER BY seq LIMIT 1", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// Events liệt kê nhật ký thao tác của một tài khoản theo seq (chỉ đọc).
func Events(ctx context.Context, tx pgx.Tx, user uuid.UUID) ([]domain.Event, error) {
	rows, err := tx.Query(ctx, "SELECT "+eventCols+" FROM pf_accounts.account_event WHERE user_ref = $1 ORDER BY seq", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

const accountCols = "user_ref, org_ref, roles, enabled, coalesce(lock_cause, '')"

func scanAccount(row pgx.Row) (domain.Account, error) {
	var a domain.Account
	var cause string
	err := row.Scan(&a.UserRef, &a.OrgRef, &a.Roles, &a.Enabled, &cause)
	a.LockCause = port.LockCause(cause)
	return a, err
}

// Account đọc trạng thái hiện tại; nil nếu chưa có. forUpdate khóa dòng.
func Account(ctx context.Context, tx pgx.Tx, user uuid.UUID, forUpdate bool) (*domain.Account, error) {
	q := "SELECT " + accountCols + " FROM pf_accounts.account WHERE user_ref = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	a, err := scanAccount(tx.QueryRow(ctx, q, user))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Accounts đọc trạng thái của nhiều tài khoản (cho danh sách).
func Accounts(ctx context.Context, tx pgx.Tx, users []uuid.UUID) (map[uuid.UUID]domain.Account, error) {
	out := map[uuid.UUID]domain.Account{}
	if len(users) == 0 {
		return out, nil
	}
	refs := make([]string, 0, len(users))
	for _, u := range users {
		refs = append(refs, u.String())
	}
	rows, err := tx.Query(ctx, "SELECT "+accountCols+" FROM pf_accounts.account WHERE user_ref = ANY($1::uuid[])", refs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out[a.UserRef] = a
	}
	return out, rows.Err()
}

// LockedByOrg liệt kê tài khoản của tổ chức đang bị khóa với nguyên nhân cause.
func LockedByOrg(ctx context.Context, tx pgx.Tx, org uuid.UUID, cause port.LockCause) ([]domain.Account, error) {
	rows, err := tx.Query(ctx, "SELECT "+accountCols+` FROM pf_accounts.account
	  WHERE org_ref = $1 AND NOT enabled AND lock_cause = $2 ORDER BY user_ref FOR UPDATE`, org, string(cause))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SaveAccount ghi bảng hiện tại theo dòng sổ vừa ghi (R-21).
func SaveAccount(ctx context.Context, tx pgx.Tx, e domain.Event) error {
	a := e.Account
	roles := a.Roles
	if roles == nil {
		roles = []string{}
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.account
	    (tenant_id, user_ref, org_ref, roles, enabled, lock_cause, last_event_id, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	  ON CONFLICT (tenant_id, user_ref) DO UPDATE SET org_ref = excluded.org_ref, roles = excluded.roles,
	    enabled = excluded.enabled, lock_cause = excluded.lock_cause, last_event_id = excluded.last_event_id,
	    last_seq = excluded.last_seq, updated_at = now()`,
		e.TenantID, a.UserRef, a.OrgRef, roles, a.Enabled, causePtr(a.LockCause), e.ID, e.Seq)
	return err
}
