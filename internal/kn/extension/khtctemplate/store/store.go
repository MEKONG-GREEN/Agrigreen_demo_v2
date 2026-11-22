// Package store persists immutable KN template versions and publications under tenant RLS.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Version struct {
	ID           uuid.UUID         `json:"id"`
	Key          string            `json:"key"`
	Version      int64             `json:"version"`
	Action       string            `json:"action"`
	Definition   domain.Definition `json:"definition"`
	Hash         string            `json:"hash"`
	Rule         string            `json:"rule"`
	RequestHash  string            `json:"-"`
	RestoredFrom *uuid.UUID        `json:"restored_from,omitempty"`
	Actor        uuid.UUID         `json:"actor"`
	ActorDisplay string            `json:"actor_display"`
	At           time.Time         `json:"at"`
}
type Publication struct {
	ID           uuid.UUID `json:"id"`
	Key          string    `json:"key"`
	Sequence     int64     `json:"sequence"`
	VersionID    uuid.UUID `json:"version_id"`
	Version      int64     `json:"version"`
	Hash         string    `json:"hash"`
	AppliesFrom  time.Time `json:"applies_from"`
	RequestHash  string    `json:"-"`
	Actor        uuid.UUID `json:"actor"`
	ActorDisplay string    `json:"actor_display"`
	At           time.Time `json:"at"`
}
type Support struct {
	OnBehalfOf *uuid.UUID
	Reason     *string
}

func Lock(ctx context.Context, tx pgx.Tx, tid uuid.UUID, key string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('khtc_template:' || $1::uuid::text || ':' || $2,0))`, tid, key)
	return err
}

const versionCols = `id,template_key,seq,action,definition,definition_hash,request_hash,restored_from,actor_ref,event_time,rule_version`

func scanVersion(row pgx.Row) (*Version, error) {
	v := Version{}
	var raw string
	err := row.Scan(&v.ID, &v.Key, &v.Version, &v.Action, &raw, &v.Hash, &v.RequestHash, &v.RestoredFrom, &v.Actor, &v.At, &v.Rule)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(raw), &v.Definition); err != nil {
		return nil, err
	}
	return &v, nil
}
func Latest(ctx context.Context, tx pgx.Tx, key string) (*Version, error) {
	return scanVersion(tx.QueryRow(ctx, `SELECT `+versionCols+` FROM pf_extension.khtc_template_version WHERE template_key=$1 ORDER BY seq DESC LIMIT 1`, key))
}
func ByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Version, error) {
	return scanVersion(tx.QueryRow(ctx, `SELECT `+versionCols+` FROM pf_extension.khtc_template_version WHERE id=$1`, id))
}
func ByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Version, error) {
	return scanVersion(tx.QueryRow(ctx, `SELECT `+versionCols+` FROM pf_extension.khtc_template_version WHERE idem_key=$1`, idem))
}
func History(ctx context.Context, tx pgx.Tx, key string, limit int) ([]Version, error) {
	rows, err := tx.Query(ctx, `SELECT `+versionCols+` FROM pf_extension.khtc_template_version WHERE template_key=$1 ORDER BY seq DESC LIMIT $2`, key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		v, e := scanVersion(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
func Insert(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v *Version, idem string, support Support) error {
	raw, hash, err := v.Definition.BytesHash()
	if err != nil {
		return err
	}
	v.Hash = hash
	v.Rule = domain.RuleVersion
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_template_version
	(id,tenant_id,stream_key,template_key,action,definition,definition_hash,request_hash,restored_from,actor_ref,event_time,idem_key,rule_version,on_behalf_of,support_reason)
	VALUES($1,$2,'khtc_template:' || $3,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING seq`,
		v.ID, tid, v.Key, v.Action, string(raw), v.Hash, v.RequestHash, v.RestoredFrom, v.Actor, v.At, idem, domain.RuleVersion, support.OnBehalfOf, support.Reason).Scan(&v.Version)
}

const pubCols = `id,template_key,seq,version_id,version_no,definition_hash,applies_from,request_hash,actor_ref,event_time`

func scanPublication(row pgx.Row) (*Publication, error) {
	p := Publication{}
	err := row.Scan(&p.ID, &p.Key, &p.Sequence, &p.VersionID, &p.Version, &p.Hash, &p.AppliesFrom, &p.RequestHash, &p.Actor, &p.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}
func PublicationByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Publication, error) {
	return scanPublication(tx.QueryRow(ctx, `SELECT `+pubCols+` FROM pf_extension.khtc_template_publication WHERE idem_key=$1`, idem))
}
func Publications(ctx context.Context, tx pgx.Tx, key string, limit int) ([]Publication, error) {
	rows, err := tx.Query(ctx, `SELECT `+pubCols+` FROM pf_extension.khtc_template_publication WHERE template_key=$1 ORDER BY seq DESC LIMIT $2`, key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Publication{}
	for rows.Next() {
		p, e := scanPublication(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// A later publication supersedes prior eligible publications, including a revert.
// A future-dated publication does not displace the current one for earlier periods.
func Applicable(ctx context.Context, tx pgx.Tx, key string, start time.Time) (*Publication, error) {
	return scanPublication(tx.QueryRow(ctx, `SELECT `+pubCols+` FROM pf_extension.khtc_template_publication WHERE template_key=$1 AND applies_from <= $2::date ORDER BY seq DESC LIMIT 1`, key, start))
}
func FirstPublication(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Publication, error) {
	return scanPublication(tx.QueryRow(ctx, `SELECT `+pubCols+` FROM pf_extension.khtc_template_publication WHERE version_id=$1 ORDER BY seq LIMIT 1`, id))
}

// FirstEffective is per tenant (RLS) and fixed key. No row is an explicit legacy
// selection, never the result of swallowing an error from a publication lookup.
func FirstEffective(ctx context.Context, tx pgx.Tx, key string) (*time.Time, error) {
	var first *time.Time
	err := tx.QueryRow(ctx, `SELECT min(applies_from) FROM pf_extension.khtc_template_publication WHERE template_key=$1`, key).Scan(&first)
	return first, err
}
func Publish(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p *Publication, idem string, support Support) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.khtc_template_publication
	(id,tenant_id,stream_key,template_key,version_id,version_no,definition_hash,applies_from,request_hash,actor_ref,event_time,idem_key,rule_version,on_behalf_of,support_reason)
	VALUES($1,$2,'khtc_template_publish:' || $3,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING seq`, p.ID, tid, p.Key, p.VersionID, p.Version, p.Hash, p.AppliesFrom, p.RequestHash, p.Actor, p.At, idem, domain.RuleVersion, support.OnBehalfOf, support.Reason).Scan(&p.Sequence)
}

// HasNarrative checks existing drafts before removing a free-text slot. Historical
// payloads are immutable; acknowledgement only permits changing future definitions.
func HasNarrative(ctx context.Context, tx pgx.Tx, key string, ids []string) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	var found bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pf_extension.khtc_report_draft d,
	lateral jsonb_each_text(d.payload->'texts') t WHERE d.payload->'data'->>'template'=$1 AND t.key=ANY($2::text[]) AND btrim(t.value)<>'')`, key, ids).Scan(&found)
	return found, err
}
