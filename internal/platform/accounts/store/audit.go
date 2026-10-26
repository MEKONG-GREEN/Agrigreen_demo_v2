package store

import (
	"context"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const NativeAuditSourceKey = "accounts.account_event"

// AuditAccountEvents reads only safe columns from the existing ledger. No raw
// reason/details or current assignment joins are permitted here. A department
// was not captured by this ledger, so it cannot match a department filter.
func AuditAccountEvents(ctx context.Context, tx pgx.Tx, q port.AuditReadQuery) ([]port.AuditRecord, error) {
	if q.Department != "" {
		return []port.AuditRecord{}, nil
	}
	var before any
	var beforeID any
	includeEqual, sameSource := false, false
	if q.Before != nil {
		before = q.Before.RecordedAt
		beforeID = q.Before.EventRef
		includeEqual = strings.Compare(NativeAuditSourceKey, q.Before.SourceKey) < 0
		sameSource = NativeAuditSourceKey == q.Before.SourceKey
	}
	rows, err := tx.Query(ctx, `SELECT id,user_ref,actor_ref,event_time,recorded_at,action,enabled,roles,org_ref,coalesce(lock_cause,'')
 FROM pf_accounts.account_event
 WHERE recorded_at >= $1 AND recorded_at < $2 AND recorded_at <= $3
   AND ($4::uuid IS NULL OR actor_ref=$4)
   AND ($5='' OR 'account.'||action=$5)
   AND ($6::timestamptz IS NULL OR recorded_at<$6 OR (recorded_at=$6 AND ($7 OR ($8 AND id<$9))))
 ORDER BY recorded_at DESC,id DESC LIMIT $10`, q.From, q.To, q.AsOf, nullableAuditRef(q.ActorRef), q.Action, before, includeEqual, sameSource, beforeID, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []port.AuditRecord{}
	for rows.Next() {
		var r port.AuditRecord
		var action, cause string
		var enabled bool
		var roles []string
		var org uuid.UUID
		if err := rows.Scan(&r.EventRef, &r.EntityRef, &r.ActorRef, &r.OccurredAt, &r.RecordedAt, &action, &enabled, &roles, &org, &cause); err != nil {
			return nil, err
		}
		r.Action = "account." + action
		r.Outcome = "unknown"
		r.Changes = []port.AuditChange{
			{Field: "enabled", After: &port.AuditValue{Kind: port.AuditBoolean, Boolean: &enabled}},
			{Field: "roles", After: &port.AuditValue{Kind: port.AuditCodes, Codes: roles}},
		}
		if org != uuid.Nil {
			r.Changes = append(r.Changes, port.AuditChange{Field: "org_ref", After: &port.AuditValue{Kind: port.AuditRef, Ref: &org}})
		}
		causes := []string{}
		if cause != "" {
			causes = append(causes, cause)
		}
		r.Changes = append(r.Changes, port.AuditChange{Field: "lock_cause", After: &port.AuditValue{Kind: port.AuditCodes, Codes: causes}})
		out = append(out, r)
	}
	return out, rows.Err()
}
func nullableAuditRef(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
