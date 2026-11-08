package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CNTSDefinitionIntent fences one KN request key across definition targets. Native
// Forms remains the sole owner of the canonical definition and its durable receipt.
type CNTSDefinitionIntent struct {
	ID            uuid.UUID
	Key           string
	ActorRef      uuid.UUID
	SessionRef    uuid.UUID
	AuthorityHash string
	RequestHash   string
	Request       json.RawMessage
	Command       json.RawMessage
	EventTime     time.Time
}

func LockCNTSDefinition(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('cnts-definition275:'||$1::uuid::text||':'||$2,0))", tenant, key)
	return err
}

func CNTSDefinitionByKey(ctx context.Context, tx pgx.Tx, key string) (*CNTSDefinitionIntent, error) {
	var p CNTSDefinitionIntent
	err := tx.QueryRow(ctx, `SELECT id,operation_key,actor_ref,session_ref,authority_hash,request_hash,request_body,command_body,event_time
      FROM pf_extension.cnts_definition_intent WHERE operation_key=$1 LIMIT 1`, key).Scan(&p.ID, &p.Key, &p.ActorRef, &p.SessionRef, &p.AuthorityHash, &p.RequestHash, &p.Request, &p.Command, &p.EventTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

func InsertCNTSDefinition(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, p CNTSDefinitionIntent) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_definition_intent
      (id,tenant_id,stream_key,operation_key,actor_ref,session_ref,authority_hash,request_hash,request_body,command_body,event_time,idem_key,rule_version)
      VALUES ($1,$2,'cnts-definition:'||$3,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10,'intent:'||$3,'kn.cnts_definition/v1')`, p.ID, tenant, p.Key, p.ActorRef, p.SessionRef, p.AuthorityHash, p.RequestHash, p.Request, p.Command, p.EventTime)
	return err
}
