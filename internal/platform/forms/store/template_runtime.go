package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type DefinitionReceipt struct {
	ActualBase                int64
	IntentHash, AuthorityHash string
}

func SavedDefinition(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*DefinitionReceipt, error) {
	var r DefinitionReceipt
	err := tx.QueryRow(ctx, `SELECT actual_base,intent_hash,authority_hash FROM pf_forms.template_definition WHERE template_id=$1`, id).Scan(&r.ActualBase, &r.IntentHash, &r.AuthorityHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// InsertDefinition extends exactly the native immutable row and its durable key.
// Its receipt/audit/config all roll back if any native/extension/commit fails.
func InsertDefinition(ctx context.Context, tx pgx.Tx, t Template, base int64, intent []byte, intentHash, authority string, intentKey *string) error {
	runtime, err := json.Marshal(t.Runtime)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_forms.template_definition(template_id,template_recorded_at,actual_base,runtime,intent,intent_hash,authority_hash,id,tenant_id,stream_key,event_time,actor_ref,idem_key,rule_version,intent_key)
 SELECT id,recorded_at,$2,$3::jsonb,$4::jsonb,$5,$6,$7,tenant_id,'definition:'||id::text,event_time,actor_ref,idem_key,'forms.definition/v1',$8
 FROM pf_forms.template WHERE id=$1`, t.ID, base, string(runtime), string(intent), intentHash, authority, ids.New(), intentKey)
	return err
}

func TemplateUsage(ctx context.Context, tx pgx.Tx, t Template) (port.TemplateUsage, error) {
	out := port.TemplateUsage{Identity: t.Identity(), State: port.TemplateUsageUnknown}
	var at, dataAt *time.Time
	err := tx.QueryRow(ctx, `WITH uses AS (
 SELECT recorded_at,'native' AS source,false AS has_data FROM pf_forms.period WHERE template_id=$1
 UNION ALL SELECT recorded_at,'native',true FROM pf_forms.submission WHERE template_id=$1
 UNION ALL SELECT recorded_at,'native',true FROM pf_forms.held_submission WHERE template_id=$1
 UNION ALL SELECT recorded_at,'runtime',has_data FROM pf_forms.template_use WHERE template_id=$1)
 SELECT min(recorded_at),min(recorded_at) FILTER(WHERE has_data),coalesce(bool_or(source='native'),false),coalesce(bool_or(source='runtime'),false) FROM uses`, t.ID).Scan(&at, &dataAt, &out.Native, &out.Runtime)
	if err != nil {
		return out, err
	}
	if at != nil {
		out.State = port.TemplateUsed
		out.FirstUsedAt = at
	} else if t.Tracked {
		out.State = port.TemplateUnused
	}
	if dataAt != nil {
		yes := true
		out.HasData = &yes
		out.FirstDataAt = dataAt
	} else if t.Tracked {
		no := false
		out.HasData = &no
	}

	return out, nil
}

type UseReceipt struct {
	Receipt                    port.TemplateUseReceipt
	RequestHash, AuthorityHash string
}

func TemplateUseByIdem(ctx context.Context, tx pgx.Tx, idem string) (*UseReceipt, error) {
	var r UseReceipt
	var raw string
	err := tx.QueryRow(ctx, `SELECT id,request::text,actor_ref,recorded_at,request_hash,authority_hash,has_data FROM pf_forms.template_use WHERE idem_key=$1`, idem).Scan(&r.Receipt.ID, &raw, &r.Receipt.ActorRef, &r.Receipt.RecordedAt, &r.RequestHash, &r.AuthorityHash, &r.Receipt.HasData)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, json.Unmarshal([]byte(raw), &r.Receipt.Request)
}
func InsertTemplateUse(ctx context.Context, tx pgx.Tx, t Template, r *port.TemplateUseReceipt, requestHash, authority, idem string, at time.Time) error {
	raw, err := json.Marshal(r.Request)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_forms.template_use(template_id,template_recorded_at,template_version,definition_digest,runtime_contract,runtime_ref,request,request_hash,authority_hash,id,tenant_id,stream_key,event_time,actor_ref,idem_key,rule_version,has_data)
 VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,'template_use:'||$1::uuid::text,$12,$13,$14,'forms.template_use/v1',$15) RETURNING recorded_at`, t.ID, t.RecordedAt, t.Version, t.Digest, r.Request.Runtime, r.Request.Reference, string(raw), requestHash, authority, r.ID, t.TenantID, at, r.ActorRef, idem, r.HasData).Scan(&r.RecordedAt)
}

// LockDefinitionIntent adds a whole-operation fence only for the new typed
// runtime/copy contract. Legacy seed calls keep their same-key multi-code API.
func LockDefinitionIntent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, key string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_forms.template.intent:'||$1::uuid::text||':'||$2::text,0))`, tid, key)
	return err
}
func DefinitionByIntent(ctx context.Context, tx pgx.Tx, key string) (*Template, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT template_id FROM pf_forms.template_definition WHERE intent_key=$1`, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return TemplateByID(ctx, tx, id)
}

// Native #191 keys include the real code suffix. A full exact EXISTS check can
// detect historical reuse without guessing from one page or a current head.
func HasLegacyDefinitionIntent(ctx context.Context, tx pgx.Tx, key string) (bool, error) {
	var found bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pf_forms.template t WHERE t.idem_key=$1::text||':template:'||t.code AND NOT EXISTS(SELECT 1 FROM pf_forms.template_definition d WHERE d.tenant_id=t.tenant_id AND d.template_id=t.id AND d.intent_key=$1))`, key).Scan(&found)
	return found, err
}
