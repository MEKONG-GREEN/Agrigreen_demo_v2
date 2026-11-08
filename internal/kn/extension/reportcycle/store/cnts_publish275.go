package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CNTSPublishIntent is immutable KN-owned dispatch metadata. It never grants a
// native right: both the KN request and Forms publisher recheck current authority.
type CNTSPublishIntent struct {
	ID            uuid.UUID
	Key           string
	ActorRef      uuid.UUID
	SessionRef    uuid.UUID
	AuthorityHash string
	RequestHash   string
	BatchHash     string
	Request       json.RawMessage
	Batch         json.RawMessage
	Series        json.RawMessage
	EventTime     time.Time
}

func LockCNTSPublish(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('cnts-publish275:'||$1::uuid::text||':'||$2,0))", tenant, key)
	return err
}

func CNTSPublishIntentByKey(ctx context.Context, tx pgx.Tx, key string) (*CNTSPublishIntent, error) {
	var p CNTSPublishIntent
	err := tx.QueryRow(ctx, `SELECT id,operation_key,actor_ref,session_ref,authority_hash,request_hash,batch_hash,request_body,batch_body,series_body,event_time
      FROM pf_extension.cnts_publish_intent WHERE operation_key=$1 LIMIT 1`, key).Scan(&p.ID, &p.Key, &p.ActorRef, &p.SessionRef, &p.AuthorityHash, &p.RequestHash, &p.BatchHash, &p.Request, &p.Batch, &p.Series, &p.EventTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

func InsertCNTSPublishIntent(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, p CNTSPublishIntent) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_publish_intent
      (id,tenant_id,stream_key,operation_key,actor_ref,session_ref,authority_hash,request_hash,batch_hash,request_body,batch_body,series_body,event_time,idem_key,rule_version)
      VALUES ($1,$2,'cnts-publish:'||$3,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11::jsonb,$12,'intent:'||$3,'kn.cnts_publish/v1')`, p.ID, tenant, p.Key, p.ActorRef, p.SessionRef, p.AuthorityHash, p.RequestHash, p.BatchHash, p.Request, p.Batch, p.Series, p.EventTime)
	return err
}

func CNTSPublishReceipt(ctx context.Context, tx pgx.Tx, intent uuid.UUID) (json.RawMessage, error) {
	var raw json.RawMessage
	err := tx.QueryRow(ctx, `SELECT response_body FROM pf_extension.cnts_publish_receipt WHERE intent_ref=$1 LIMIT 1`, intent).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return raw, err
}

func InsertCNTSPublishReceipt(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, p CNTSPublishIntent, raw json.RawMessage) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_publish_receipt
      (id,tenant_id,stream_key,intent_ref,response_body,event_time,actor_ref,idem_key,rule_version)
      VALUES ($1,$2,'cnts-publish:'||$1::uuid::text,$1,$3::jsonb,$4,$5,'receipt:'||$1::uuid::text,'kn.cnts_publish/v1')`, p.ID, tenant, raw, p.EventTime, p.ActorRef)
	return err
}
