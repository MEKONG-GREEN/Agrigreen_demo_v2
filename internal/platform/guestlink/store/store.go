// Package store persists guest capability ledgers through r_guestlink + tenant RLS.
package store

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/port"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sort"
	"time"
)

type Grant struct {
	ID                             uuid.UUID
	Binding                        port.Binding
	Key, LinkHash, IssueKey        string
	OwnerRevision, ContactRevision uuid.UUID
	Until                          time.Time
	Issuer                         uuid.UUID
	At                             time.Time
}

const grantCols = `id,tenant_id,binding_key,owner,subject_ref,resource_ref,step,owner_revision,contact_revision,link_hash,issue_key,expires_at,actor_ref,event_time`

func scanGrant(row pgx.Row) (*Grant, error) {
	var g Grant
	err := row.Scan(&g.ID, &g.Binding.Tenant, &g.Key, &g.Binding.Owner, &g.Binding.Subject, &g.Binding.Resource, &g.Binding.Step, &g.OwnerRevision, &g.ContactRevision, &g.LinkHash, &g.IssueKey, &g.Until, &g.Issuer, &g.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &g, err
}
func ByLink(ctx context.Context, tx pgx.Tx, hash string) (*Grant, error) {
	return scanGrant(tx.QueryRow(ctx, `SELECT `+grantCols+` FROM pf_guestlink.grant_record WHERE link_hash=$1 ORDER BY recorded_at DESC LIMIT 1`, hash))
}
func ByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Grant, error) {
	return scanGrant(tx.QueryRow(ctx, `SELECT `+grantCols+` FROM pf_guestlink.grant_record WHERE id=$1 ORDER BY recorded_at DESC LIMIT 1`, id))
}
func ByIssue(ctx context.Context, tx pgx.Tx, key string) (*Grant, error) {
	return scanGrant(tx.QueryRow(ctx, `SELECT `+grantCols+` FROM pf_guestlink.grant_record WHERE issue_key=$1 ORDER BY recorded_at DESC LIMIT 1`, key))
}
func Latest(ctx context.Context, tx pgx.Tx, key string) (*Grant, error) {
	return scanGrant(tx.QueryRow(ctx, `SELECT `+grantCols+` FROM pf_guestlink.grant_record WHERE binding_key=$1 ORDER BY seq DESC LIMIT 1`, key))
}
func InsertGrant(ctx context.Context, tx pgx.Tx, g Grant) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_guestlink.grant_record(id,tenant_id,stream_key,binding_key,owner,subject_ref,resource_ref,step,owner_revision,contact_revision,link_hash,issue_key,expires_at,actor_ref,event_time,idem_key,rule_version)
 VALUES($1,$2,'guestlink:'||$3,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$11,'guestlink/v1')`, g.ID, g.Binding.Tenant, g.Key, g.Binding.Owner, g.Binding.Subject, g.Binding.Resource, g.Binding.Step, g.OwnerRevision, g.ContactRevision, g.LinkHash, g.IssueKey, g.Until, g.Issuer, g.At)
	return err
}

type Event struct {
	ID, GrantID, ChallengeID, SessionRef               uuid.UUID
	Action, OperationHash, SessionHash, IPHash, Reason string
	Until                                              *time.Time
	Actor                                              uuid.UUID
	At                                                 time.Time
}

const eventCols = `id,grant_id,action,challenge_id,session_ref,operation_hash,session_hash,ip_hash,expires_at,reason,actor_ref,event_time`

func scanEvent(row pgx.Row) (Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.GrantID, &e.Action, &e.ChallengeID, &e.SessionRef, &e.OperationHash, &e.SessionHash, &e.IPHash, &e.Until, &e.Reason, &e.Actor, &e.At)
	return e, err
}
func Events(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]Event, error) {
	rows, err := tx.Query(ctx, `SELECT `+eventCols+` FROM pf_guestlink.access_event WHERE grant_id=$1 ORDER BY seq LIMIT 4097`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) { return scanEvent(row) })
}
func BySession(ctx context.Context, tx pgx.Tx, hash string) (*Event, error) {
	e, err := scanEvent(tx.QueryRow(ctx, `SELECT `+eventCols+` FROM pf_guestlink.access_event WHERE session_hash=$1 AND action='verified' ORDER BY recorded_at DESC LIMIT 1`, hash))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}
func InsertEvent(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, e Event) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_guestlink.access_event(id,tenant_id,stream_key,grant_id,action,challenge_id,session_ref,operation_hash,session_hash,ip_hash,expires_at,reason,actor_ref,event_time,idem_key,rule_version)
 VALUES($1,$2,'guestlink:'||$3::uuid::text,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$1::uuid::text,'guestlink/v1')`, e.ID, tenant, e.GrantID, e.Action, e.ChallengeID, e.SessionRef, e.OperationHash, e.SessionHash, e.IPHash, e.Until, e.Reason, e.Actor, e.At)
	return err
}

// Fixed lock ordering prevents binding/subject/IP cycles under concurrent traffic.
func Lock(ctx context.Context, tx pgx.Tx, keys ...string) error {
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "guestlink:"+key); err != nil {
			return err
		}
	}
	return nil
}
func IPCount(ctx context.Context, tx pgx.Tx, hash string, since time.Time) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_guestlink.access_event WHERE ip_hash=$1 AND event_time>=$2 AND action<>'sent' AND action<>'disabled' AND action<>'challenge' AND action<>'quota'`, hash, since).Scan(&n)
	return n, err
}
func SubjectCount(ctx context.Context, tx pgx.Tx, subject uuid.UUID, since time.Time) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_guestlink.access_event e JOIN pf_guestlink.grant_record g ON g.id=e.grant_id AND g.tenant_id=e.tenant_id WHERE g.subject_ref=$1 AND e.event_time>=$2 AND e.action IN ('opened','otp_wrong','verified','session_ok')`, subject, since).Scan(&n)
	return n, err
}
