package handler_test

// Test-only owner protocol evidence. These tables live only in pgtest's private
// database. This is not a production KN importer, producer fence, or cutover rehearsal.
// Stage-two contract dependencies: TransferLogicalRoot, VerifyCompensation and
// receipt OperationID/ReasonRef/CompensatesRef/SourceLogicalRoot.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const durableOwnerVersion = "pg-owner-fixture/v1"
const durableParentNS = "durable.parent"
const durableChildNS = "durable.child"

var durableOwnerDDLOnce sync.Once
var durableOwnerDDLErr error
var errDurableOwnerInjected = errors.New("test owner interrupted")

// No test calls t.Parallel: one private pgtest database, with tenant/node RLS on
// every fixture table and shared.fn_chain on the actual owner event ledger.
const durableOwnerDDL = `
CREATE TABLE pf_forms.transfer55_fixture_event (
 id uuid NOT NULL, tenant_id uuid NOT NULL, scope_ref uuid NOT NULL,
 stream_key text NOT NULL, seq bigint NOT NULL, entity_ref uuid NOT NULL,
 namespace text NOT NULL, action text NOT NULL, value text NOT NULL,
 parent_namespace text NOT NULL, parent_ref uuid NULL,
 source_tenant uuid NOT NULL, source_ref uuid NOT NULL,
 source_row_hash text NOT NULL, source_seq bigint NOT NULL,
 operation_id uuid NOT NULL, event_time timestamptz NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT now(), actor_ref uuid NOT NULL,
 idem_key text NOT NULL, rule_version text NOT NULL,
 prev_hash bytea NOT NULL, row_hash bytea NOT NULL,
 PRIMARY KEY(id,recorded_at)
) PARTITION BY RANGE(recorded_at);
SELECT shared.apply_ledger('pf_forms.transfer55_fixture_event','pf_forms.transfer55_fixture_event');
CREATE POLICY p_scope ON pf_forms.transfer55_fixture_event AS RESTRICTIVE
 USING(scope_ref=k_ref.ctx_uuid('app.node_id')) WITH CHECK(scope_ref=k_ref.ctx_uuid('app.node_id'));
CREATE TABLE pf_forms.transfer55_fixture_provenance (
 tenant_id uuid NOT NULL, scope_ref uuid NOT NULL, event_ref uuid NOT NULL,
 plan_id uuid NOT NULL, batch_id uuid NOT NULL, operation_id uuid NOT NULL,
 namespace text NOT NULL, source_tenant uuid NOT NULL, source_ref uuid NOT NULL,
 source_row_hash text NOT NULL, source_seq bigint NOT NULL, target_ref uuid NOT NULL,
 PRIMARY KEY(tenant_id,event_ref)
);
CREATE TABLE pf_forms.transfer55_fixture_receipt (
 tenant_id uuid NOT NULL, scope_ref uuid NOT NULL, plan_id uuid NOT NULL,
 batch_id uuid NOT NULL, operation_id uuid NOT NULL, phase text NOT NULL,
 request_hash text NOT NULL, document jsonb NOT NULL,
 PRIMARY KEY(tenant_id,plan_id,batch_id,phase,operation_id)
);
CREATE TABLE pf_forms.transfer55_fixture_artifact (
 tenant_id uuid NOT NULL, scope_ref uuid NOT NULL, plan_id uuid NOT NULL,
 batch_id uuid NOT NULL, request_hash text NOT NULL,
 artifact jsonb NOT NULL, payload jsonb NOT NULL,
 PRIMARY KEY(tenant_id,plan_id,batch_id)
);
CREATE TABLE pf_forms.transfer55_fixture_authority (
 tenant_id uuid NOT NULL, scope_ref uuid NOT NULL, namespace text NOT NULL,
 actor_ref uuid NOT NULL, approval_ref uuid NOT NULL, epoch text NOT NULL,
 valid_until timestamptz NOT NULL, enabled boolean NOT NULL,
 PRIMARY KEY(tenant_id,scope_ref,namespace)
);
CREATE TABLE pf_forms.transfer55_fixture_fence (
 tenant_id uuid NOT NULL, scope_ref uuid NOT NULL, namespace text NOT NULL,
 side text NOT NULL, holder uuid NOT NULL, document jsonb NOT NULL,
 active boolean NOT NULL,
 PRIMARY KEY(tenant_id,scope_ref,namespace,side)
);
DO $fixture$
DECLARE n text;
BEGIN
 FOREACH n IN ARRAY ARRAY['provenance','receipt','artifact','authority','fence'] LOOP
  EXECUTE format('SELECT shared.apply_tenant_rls(%L::regclass)','pf_forms.transfer55_fixture_'||n);
  EXECUTE format('CREATE POLICY p_scope ON pf_forms.transfer55_fixture_%I AS RESTRICTIVE USING(scope_ref=k_ref.ctx_uuid(''app.node_id'')) WITH CHECK(scope_ref=k_ref.ctx_uuid(''app.node_id''))',n);
 END LOOP;
END $fixture$;
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON pf_forms.transfer55_fixture_provenance FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON pf_forms.transfer55_fixture_receipt FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON pf_forms.transfer55_fixture_artifact FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
GRANT SELECT,INSERT ON pf_forms.transfer55_fixture_event,pf_forms.transfer55_fixture_provenance,
 pf_forms.transfer55_fixture_receipt,pf_forms.transfer55_fixture_artifact TO r_forms;
GRANT SELECT,INSERT,UPDATE ON pf_forms.transfer55_fixture_authority,pf_forms.transfer55_fixture_fence TO r_forms;
`

type durableOwnerValue struct {
	Value  string
	Parent *port.TransferKey
	Active bool
}
type durableOwnerExportRow struct {
	Header port.TransferHeader
	Value  durableOwnerValue
}
type durableOwner struct {
	pool *pgxpool.Pool
	pair port.TransferParticipants
	// These are fault controls only. All authority, fences, source values, exports,
	// target values, provenance and receipts are durable PostgreSQL rows.
	beforeApply       func() error
	failBeforeReceipt bool
	failAfterCommit   bool
	malformedRead     bool
	crashCompensation bool
}

func durableHash(value any) (port.TransferDigest, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(raw)
	return port.TransferDigest(hex.EncodeToString(h[:])), nil
}
func durableJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	return string(raw), err
}
func durableRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func (o *durableOwner) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	p, ok := authz.FromContext(ctx)
	tid, err := tenant.FromContext(ctx)
	if !ok || err != nil || tid != p.TenantID || p.OrgRef == uuid.Nil {
		return port.ErrTransferAuthority
	}
	return db.WithContextTx(ctx, o.pool, db.Module("pf/forms"), func(tx pgx.Tx) error {
		var role, tenantText, nodeText, actorText string
		if err := tx.QueryRow(ctx, `SELECT current_user,current_setting('app.tenant_id',true),current_setting('app.node_id',true),current_setting('app.actor_ref',true)`).Scan(&role, &tenantText, &nodeText, &actorText); err != nil {
			return err
		}
		if role != "r_forms" || tenantText != tid.String() || nodeText != p.OrgRef.String() || actorText != p.ActorRef.String() {
			return port.ErrTransferAuthority
		}
		return fn(tx)
	})
}
func durableSide(ctx context.Context, scope port.TransferScope, side port.TransferSide) (uuid.UUID, uuid.UUID, uuid.UUID, error) {
	tid, node, holder := scope.SourceTenant, scope.SourceScopeRef, scope.PlanID
	if side == port.TransferTarget {
		tid, node, holder = scope.TargetTenant, scope.TargetScopeRef, scope.IntentID
	}
	p, ok := authz.FromContext(ctx)
	if !ok || p.TenantID != tid || p.OrgRef != node {
		return uuid.Nil, uuid.Nil, uuid.Nil, port.ErrTransferAuthority
	}
	return tid, node, holder, nil
}
func (o *durableOwner) authorityTx(ctx context.Context, tx pgx.Tx, namespace string) (port.TransferApproval, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return port.TransferApproval{}, port.ErrTransferAuthority
	}
	var a port.TransferApproval
	var enabled bool
	err := tx.QueryRow(ctx, `SELECT approval_ref,actor_ref,epoch,valid_until,enabled FROM pf_forms.transfer55_fixture_authority WHERE scope_ref=$1 AND namespace=$2 FOR SHARE`, p.OrgRef, namespace).Scan(&a.Ref, &a.ActorRef, &a.Epoch, &a.ValidUntil, &enabled)
	if err != nil {
		return a, err
	}
	if !enabled || a.ActorRef != p.ActorRef || !a.ValidUntil.After(time.Now()) {
		return a, port.ErrTransferAuthority
	}
	return a, nil
}
func (o *durableOwner) authority(ctx context.Context, req port.TransferAuthorityRequest) (port.TransferApproval, error) {
	if _, _, _, err := durableSide(ctx, req.Scope, req.Side); err != nil {
		return port.TransferApproval{}, err
	}
	var a port.TransferApproval
	err := o.tx(ctx, func(tx pgx.Tx) error { var err error; a, err = o.authorityTx(ctx, tx, req.Namespace); return err })
	return a, err
}
func (o *durableOwner) headersTx(ctx context.Context, tx pgx.Tx, namespace string) ([]port.TransferHeader, error) {
	p, _ := authz.FromContext(ctx)
	rows, err := tx.Query(ctx, `SELECT entity_ref,encode(row_hash,'hex'),seq,value,parent_namespace,parent_ref FROM pf_forms.transfer55_fixture_event WHERE namespace=$1 AND action='source' ORDER BY entity_ref`, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []port.TransferHeader{}
	for rows.Next() {
		h := port.TransferHeader{SourceTenant: p.TenantID, SourceScopeRef: p.OrgRef, Key: port.TransferKey{Namespace: namespace}}
		var value, parentNS string
		var parent *uuid.UUID
		if err := rows.Scan(&h.Key.Ref, &h.SourceRowHash, &h.SourceSeq, &value, &parentNS, &parent); err != nil {
			return nil, err
		}
		v := durableOwnerValue{Value: value, Active: true}
		if parent != nil {
			key := port.TransferKey{Namespace: parentNS, Ref: *parent}
			v.Parent = &key
			h.References = []port.TransferReference{{Key: key}}
		}
		h.LogicalHash, err = durableHash(v)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
func (o *durableOwner) fenceTx(ctx context.Context, tx pgx.Tx, req port.TransferFenceRequest, expected *port.TransferFence) (port.TransferFence, error) {
	tid, node, holder, err := durableSide(ctx, req.Scope, req.Side)
	if err != nil {
		return port.TransferFence{}, err
	}
	var raw []byte
	var currentHolder uuid.UUID
	var active bool
	err = tx.QueryRow(ctx, `SELECT holder,document,active FROM pf_forms.transfer55_fixture_fence WHERE scope_ref=$1 AND namespace=$2 AND side=$3 FOR SHARE`, node, req.Namespace, req.Side).Scan(&currentHolder, &raw, &active)
	if err == nil {
		var f port.TransferFence
		if err = json.Unmarshal(raw, &f); err != nil {
			return f, err
		}
		if !active || currentHolder != holder || (expected != nil && *expected != f) {
			return f, port.ErrTransferConflict
		}
		return f, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) || expected != nil {
		return port.TransferFence{}, port.ErrTransferConflict
	}
	if _, err = o.authorityTx(ctx, tx, req.Namespace); err != nil {
		return port.TransferFence{}, err
	}
	headers := []port.TransferHeader{}
	if req.Side == port.TransferSource {
		headers, err = o.headersTx(ctx, tx, req.Namespace)
		if err != nil {
			return port.TransferFence{}, err
		}
	}
	root, err := port.TransferHeaderRoot(tid, node, req.Namespace, headers)
	if err != nil {
		return port.TransferFence{}, err
	}
	f := port.TransferFence{Ref: ids.New(), Epoch: holder.String(), HighWatermark: "durable-frozen/v1", HeaderRoot: root, HeaderCount: int64(len(headers))}
	doc, err := durableJSON(f)
	if err != nil {
		return f, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_forms.transfer55_fixture_fence(tenant_id,scope_ref,namespace,side,holder,document,active)VALUES($1,$2,$3,$4,$5,$6,true)`, tid, node, req.Namespace, req.Side, holder, doc)
	return f, err
}
func (o *durableOwner) fence(ctx context.Context, req port.TransferFenceRequest) (port.TransferFence, error) {
	var f port.TransferFence
	err := o.tx(ctx, func(tx pgx.Tx) error { var err error; f, err = o.fenceTx(ctx, tx, req, nil); return err })
	return f, err
}
func (o *durableOwner) checkFence(ctx context.Context, req port.TransferFenceRequest, f port.TransferFence) error {
	return o.tx(ctx, func(tx pgx.Tx) error { _, err := o.fenceTx(ctx, tx, req, &f); return err })
}
func (o *durableOwner) scan(ctx context.Context, req port.TransferScanRequest) (port.TransferPage, error) {
	var out port.TransferPage
	err := o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.authorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if !req.DryRun {
			if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferSource}, &req.Fence); err != nil {
				return err
			}
		}
		h, err := o.headersTx(ctx, tx, req.Namespace)
		if err != nil {
			return err
		}
		start := 0
		if req.Cursor != "" {
			start, err = strconv.Atoi(req.Cursor)
			if err != nil {
				return err
			}
		}
		if start < 0 || start > len(h) || req.Limit < 1 {
			return port.ErrTransferConflict
		}
		end := min(start+req.Limit, len(h))
		out.Items = append([]port.TransferHeader{}, h[start:end]...)
		out.Complete = end == len(h)
		if !out.Complete {
			out.Next = strconv.Itoa(end)
		}
		return nil
	})
	return out, err
}
func durableCheckMap(req port.TransferBatchRequest) error {
	hash, err := port.TransferBatchMapDigest(req.Mappings, req.DependencyMappings)
	if err != nil || hash != req.MapHash || len(req.Headers) != len(req.Mappings) {
		return port.ErrTransferConflict
	}
	return nil
}
func (o *durableOwner) readExport(ctx context.Context, req port.TransferBatchRequest) (port.TransferArtifact, error) {
	var out port.TransferArtifact
	want, err := durableHash(req)
	if err != nil {
		return out, err
	}
	err = o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.authorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferSource}, &req.SourceFence); err != nil {
			return err
		}
		var hash string
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT request_hash,artifact FROM pf_forms.transfer55_fixture_artifact WHERE plan_id=$1 AND batch_id=$2`, req.Scope.PlanID, req.BatchID).Scan(&hash, &raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return port.ErrTransferNotReady
		}
		if err != nil {
			return err
		}
		if hash != string(want) {
			return port.ErrTransferConflict
		}
		return json.Unmarshal(raw, &out)
	})
	return out, err
}
func (o *durableOwner) export(ctx context.Context, req port.TransferBatchRequest) (port.TransferArtifact, error) {
	if old, err := o.readExport(ctx, req); !errors.Is(err, port.ErrTransferNotReady) {
		return old, err
	}
	var out port.TransferArtifact
	err := o.tx(ctx, func(tx pgx.Tx) error {
		if err := durableCheckMap(req); err != nil {
			return err
		}
		if _, err := o.authorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferSource}, &req.SourceFence); err != nil {
			return err
		}
		current, err := o.headersTx(ctx, tx, req.Namespace)
		if err != nil {
			return err
		}
		byKey := map[port.TransferKey]port.TransferHeader{}
		for _, h := range current {
			byKey[h.Key] = h
		}
		payload := []durableOwnerExportRow{}
		for _, h := range req.Headers {
			original, ok := byKey[h.Key]
			if !ok {
				return port.ErrTransferConflict
			}
			originalRoot, err := port.TransferHeaderRoot(req.Scope.SourceTenant, req.Scope.SourceScopeRef, req.Namespace, []port.TransferHeader{original})
			if err != nil {
				return err
			}
			requestedRoot, err := port.TransferHeaderRoot(req.Scope.SourceTenant, req.Scope.SourceScopeRef, req.Namespace, []port.TransferHeader{h})
			if err != nil || originalRoot != requestedRoot {
				return port.ErrTransferConflict
			}
			var value string
			var parentNS string
			var parent *uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT value,parent_namespace,parent_ref FROM pf_forms.transfer55_fixture_event WHERE entity_ref=$1 AND namespace=$2 AND action='source'`, h.Key.Ref, h.Key.Namespace).Scan(&value, &parentNS, &parent); err != nil {
				return err
			}
			v := durableOwnerValue{Value: value, Active: true}
			if parent != nil {
				key := port.TransferKey{Namespace: parentNS, Ref: *parent}
				v.Parent = &key
			}
			payload = append(payload, durableOwnerExportRow{Header: h, Value: v})
		}
		hash, err := durableHash(payload)
		if err != nil {
			return err
		}
		headerRoot, err := port.TransferHeaderRoot(req.Scope.SourceTenant, req.Scope.SourceScopeRef, req.Namespace, req.Headers)
		if err != nil {
			return err
		}
		out = port.TransferArtifact{Ref: ids.New(), ExportReceiptRef: ids.New(), Hash: hash, HeaderRoot: headerRoot, ManifestHash: req.ManifestHash, MapHash: req.MapHash, BindingHash: req.Scope.BindingHash, OwnerVersion: durableOwnerVersion}
		art, err := durableJSON(out)
		if err != nil {
			return err
		}
		body, err := durableJSON(payload)
		if err != nil {
			return err
		}
		requestHash, err := durableHash(req)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO pf_forms.transfer55_fixture_artifact(tenant_id,scope_ref,plan_id,batch_id,request_hash,artifact,payload)VALUES($1,$2,$3,$4,$5,$6,$7)`, req.Scope.SourceTenant, req.Scope.SourceScopeRef, req.Scope.PlanID, req.BatchID, string(requestHash), art, body)
		return err
	})
	return out, err
}

// The fixture transport opens the source artifact with the separately verified
// source context; it never rewrites the target Principal or uses MintGrant.
func (o *durableOwner) payload(req port.TransferApplyRequest) ([]durableOwnerExportRow, error) {
	a, err := o.readExport(o.pair.Source(), req.TransferBatchRequest)
	if err != nil {
		return nil, err
	}
	if a != req.Artifact {
		return nil, port.ErrTransferConflict
	}
	var out []durableOwnerExportRow
	err = o.tx(o.pair.Source(), func(tx pgx.Tx) error {
		if _, err := o.authorityTx(o.pair.Source(), tx, req.Namespace); err != nil {
			return err
		}
		if _, err := o.fenceTx(o.pair.Source(), tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferSource}, &req.SourceFence); err != nil {
			return err
		}
		var raw []byte
		if err := tx.QueryRow(o.pair.Source(), `SELECT payload FROM pf_forms.transfer55_fixture_artifact WHERE plan_id=$1 AND batch_id=$2`, req.Scope.PlanID, req.BatchID).Scan(&raw); err != nil {
			return err
		}
		return json.Unmarshal(raw, &out)
	})
	if err != nil {
		return nil, err
	}
	hash, err := durableHash(out)
	if err != nil || hash != a.Hash {
		return nil, port.ErrTransferConflict
	}
	return out, nil
}
func (o *durableOwner) receiptTx(ctx context.Context, tx pgx.Tx, req port.TransferApplyRequest, phase string, operation uuid.UUID, want port.TransferDigest) (port.TransferReceipt, error) {
	var hash string
	var raw []byte
	var out port.TransferReceipt
	err := tx.QueryRow(ctx, `SELECT request_hash,document FROM pf_forms.transfer55_fixture_receipt WHERE plan_id=$1 AND batch_id=$2 AND phase=$3 AND operation_id=$4`, req.Scope.PlanID, req.BatchID, phase, operation).Scan(&hash, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, port.ErrTransferNotReady
	}
	if err != nil {
		return out, err
	}
	if hash != string(want) {
		return out, port.ErrTransferConflict
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}
func (o *durableOwner) readReceipt(ctx context.Context, req port.TransferApplyRequest) (port.TransferReceipt, error) {
	want, err := durableHash(req)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	var out port.TransferReceipt
	err = o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.authorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, &req.TargetFence); err != nil {
			return err
		}
		var err error
		out, err = o.receiptTx(ctx, tx, req, "apply", uuid.Nil, want)
		return err
	})
	if err == nil && o.malformedRead {
		out.MapHash = ""
	}
	return out, err
}
func (o *durableOwner) insertReceipt(ctx context.Context, tx pgx.Tx, req port.TransferApplyRequest, phase string, operation uuid.UUID, requestHash port.TransferDigest, out port.TransferReceipt) error {
	raw, err := durableJSON(out)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_forms.transfer55_fixture_receipt(tenant_id,scope_ref,plan_id,batch_id,operation_id,phase,request_hash,document)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, req.Scope.TargetTenant, req.Scope.TargetScopeRef, req.Scope.PlanID, req.BatchID, operation, phase, string(requestHash), raw)
	return err
}
func durableReceipt(req port.TransferApplyRequest) (port.TransferReceipt, error) {
	root, err := port.TransferLogicalRoot(req.Headers)
	return port.TransferReceipt{Ref: ids.New(), PlanID: req.Scope.PlanID, BatchID: req.BatchID, TargetTenant: req.Scope.TargetTenant, TargetScopeRef: req.Scope.TargetScopeRef, BindingHash: req.Scope.BindingHash, ManifestHash: req.ManifestHash, MapHash: req.MapHash, ArtifactHash: req.Artifact.Hash, LogicalRoot: root, OwnerVersion: durableOwnerVersion, Items: []port.TransferAppliedRef{}}, err
}
func (o *durableOwner) appendEvent(ctx context.Context, tx pgx.Tx, req port.TransferApplyRequest, row durableOwnerExportRow, target, parent *uuid.UUID, operation uuid.UUID, action string) (port.TransferAppliedRef, error) {
	p, _ := authz.FromContext(ctx)
	event := ids.New()
	var rowHash string
	if target == nil {
		return port.TransferAppliedRef{}, port.ErrTransferConflict
	}
	parentNS := ""
	if row.Value.Parent != nil {
		parentNS = row.Value.Parent.Namespace
	}
	err := tx.QueryRow(ctx, `INSERT INTO pf_forms.transfer55_fixture_event(id,tenant_id,scope_ref,stream_key,entity_ref,namespace,action,value,parent_namespace,parent_ref,source_tenant,source_ref,source_row_hash,source_seq,operation_id,event_time,actor_ref,idem_key,rule_version)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,now(),$16,$17,'fixture-v1')RETURNING encode(row_hash,'hex')`, event, p.TenantID, p.OrgRef, "fixture:"+target.String(), *target, row.Header.Key.Namespace, action, row.Value.Value, parentNS, parent, row.Header.SourceTenant, row.Header.Key.Ref, string(row.Header.SourceRowHash), row.Header.SourceSeq, operation, p.ActorRef, action+":"+req.BatchID.String()+":"+operation.String()+":"+row.Header.Key.Ref.String()).Scan(&rowHash)
	if err != nil {
		return port.TransferAppliedRef{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_forms.transfer55_fixture_provenance(tenant_id,scope_ref,event_ref,plan_id,batch_id,operation_id,namespace,source_tenant,source_ref,source_row_hash,source_seq,target_ref)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, p.TenantID, p.OrgRef, event, req.Scope.PlanID, req.BatchID, operation, row.Header.Key.Namespace, row.Header.SourceTenant, row.Header.Key.Ref, string(row.Header.SourceRowHash), row.Header.SourceSeq, *target)
	logical, hashErr := durableHash(row.Value)
	if err != nil {
		return port.TransferAppliedRef{}, err
	}
	if hashErr != nil {
		return port.TransferAppliedRef{}, hashErr
	}
	return port.TransferAppliedRef{Key: row.Header.Key, TargetRef: *target, TargetRowHash: port.TransferDigest(rowHash), LogicalHash: logical}, nil
}
func (o *durableOwner) apply(ctx context.Context, req port.TransferApplyRequest) (port.TransferReceipt, error) {
	if o.beforeApply != nil {
		hook := o.beforeApply
		o.beforeApply = nil
		if err := hook(); err != nil {
			return port.TransferReceipt{}, err
		}
	}
	payload, err := o.payload(req)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	want, err := durableHash(req)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	var out port.TransferReceipt
	err = o.tx(ctx, func(tx pgx.Tx) error {
		if err := durableCheckMap(req.TransferBatchRequest); err != nil {
			return err
		}
		if _, err := o.authorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, &req.TargetFence); err != nil {
			return err
		}
		old, err := o.receiptTx(ctx, tx, req, "apply", uuid.Nil, want)
		if err == nil {
			out = old
			return nil
		}
		if !errors.Is(err, port.ErrTransferNotReady) {
			return err
		}
		out, err = durableReceipt(req)
		if err != nil {
			return err
		}
		mapping := map[port.TransferKey]uuid.UUID{}
		for _, m := range append(append([]port.TransferMapping{}, req.Mappings...), req.DependencyMappings...) {
			mapping[m.Key] = m.TargetRef
		}
		for _, row := range payload {
			target, ok := mapping[row.Header.Key]
			if !ok {
				return port.ErrTransferConflict
			}
			var parent *uuid.UUID
			if row.Value.Parent != nil {
				ref, ok := mapping[*row.Value.Parent]
				if !ok {
					return port.ErrTransferConflict
				}
				parent = &ref
				var exists bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pf_forms.transfer55_fixture_provenance p JOIN pf_forms.transfer55_fixture_event e ON e.id=p.event_ref AND e.tenant_id=p.tenant_id WHERE p.target_ref=$1 AND p.source_ref=$2 AND p.namespace=$3 AND e.action='imported')`, ref, row.Value.Parent.Ref, row.Value.Parent.Namespace).Scan(&exists); err != nil {
					return err
				}
				if !exists {
					return port.ErrTransferNotReady
				}
			}
			item, err := o.appendEvent(ctx, tx, req, row, &target, parent, uuid.Nil, "imported")
			if err != nil {
				return err
			}
			out.Items = append(out.Items, item)
		}
		if o.failBeforeReceipt {
			return errDurableOwnerInjected
		}
		return o.insertReceipt(ctx, tx, req, "apply", uuid.Nil, want, out)
	})
	if err == nil && o.failAfterCommit {
		o.failAfterCommit = false
		return port.TransferReceipt{}, errDurableOwnerInjected
	}
	return out, err
}

// Independent verification reconstructs source-key logical values from the
// actual target row plus predecessor provenance. It does not trust receipt hashes.
func (o *durableOwner) verifyRows(ctx context.Context, tx pgx.Tx, req port.TransferApplyRequest, out port.TransferReceipt, action string, operation uuid.UUID) error {
	if len(out.Items) != len(req.Headers) || len(out.Items) != len(req.Mappings) {
		return port.ErrTransferConflict
	}
	headers := map[port.TransferKey]port.TransferHeader{}
	for _, h := range req.Headers {
		headers[h.Key] = h
	}
	targets := map[port.TransferKey]uuid.UUID{}
	for _, m := range req.Mappings {
		targets[m.Key] = m.TargetRef
	}
	logicalHeaders := []port.TransferHeader{}
	for _, item := range out.Items {
		h, ok := headers[item.Key]
		if !ok || targets[item.Key] != item.TargetRef {
			return port.ErrTransferConflict
		}
		var value, parentNS, rowHash, sourceHash string
		var parent *uuid.UUID
		var source uuid.UUID
		var sourceSeq int64
		err := tx.QueryRow(ctx, `SELECT e.value,e.parent_namespace,e.parent_ref,encode(e.row_hash,'hex'),p.source_tenant,p.source_row_hash,p.source_seq FROM pf_forms.transfer55_fixture_event e JOIN pf_forms.transfer55_fixture_provenance p ON p.tenant_id=e.tenant_id AND p.event_ref=e.id WHERE p.plan_id=$1 AND p.batch_id=$2 AND p.operation_id=$3 AND p.namespace=$4 AND p.source_ref=$5 AND p.target_ref=$6 AND e.action=$7 AND e.seq=(SELECT max(current_event.seq) FROM pf_forms.transfer55_fixture_event current_event WHERE current_event.tenant_id=e.tenant_id AND current_event.entity_ref=e.entity_ref AND current_event.namespace=e.namespace)`, req.Scope.PlanID, req.BatchID, operation, item.Key.Namespace, item.Key.Ref, item.TargetRef, action).Scan(&value, &parentNS, &parent, &rowHash, &source, &sourceHash, &sourceSeq)
		if err != nil {
			return err
		}
		if source != h.SourceTenant || sourceHash != string(h.SourceRowHash) || sourceSeq != h.SourceSeq || rowHash != string(item.TargetRowHash) {
			return port.ErrTransferConflict
		}
		v := durableOwnerValue{Value: value, Active: action == "imported"}
		if parent != nil {
			var key port.TransferKey
			key.Namespace = parentNS
			if err := tx.QueryRow(ctx, `SELECT source_ref FROM pf_forms.transfer55_fixture_provenance WHERE target_ref=$1 AND namespace=$2 AND operation_id=$3`, *parent, parentNS, uuid.Nil).Scan(&key.Ref); err != nil {
				return err
			}
			v.Parent = &key
		}
		logical, err := durableHash(v)
		if err != nil {
			return err
		}
		if logical != item.LogicalHash || (action == "imported" && logical != h.LogicalHash) {
			return port.ErrTransferConflict
		}
		h.LogicalHash = logical
		logicalHeaders = append(logicalHeaders, h)
		delete(headers, item.Key)
	}
	if len(headers) != 0 {
		return port.ErrTransferConflict
	}
	root, err := port.TransferLogicalRoot(logicalHeaders)
	if err != nil || root != out.LogicalRoot {
		return port.ErrTransferConflict
	}
	return nil
}
func (o *durableOwner) verify(ctx context.Context, req port.TransferApplyRequest, out port.TransferReceipt) error {
	want, err := durableHash(req)
	if err != nil {
		return err
	}
	return o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.authorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, &req.TargetFence); err != nil {
			return err
		}
		stored, err := o.receiptTx(ctx, tx, req, "apply", uuid.Nil, want)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(stored, out) {
			return port.ErrTransferConflict
		}
		return o.verifyRows(ctx, tx, req, out, "imported", uuid.Nil)
	})
}
func (o *durableOwner) readCompensation(ctx context.Context, req port.TransferCompensationRequest) (port.TransferReceipt, error) {
	want, err := durableHash(req)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	var out port.TransferReceipt
	err = o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.authorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, &req.TargetFence); err != nil {
			return err
		}
		var err error
		out, err = o.receiptTx(ctx, tx, req.TransferApplyRequest, "compensate", req.OperationID, want)
		return err
	})
	return out, err
}
func (o *durableOwner) compensate(ctx context.Context, req port.TransferCompensationRequest) (port.TransferReceipt, error) {
	want, err := durableHash(req)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	payload, err := o.payload(req.TransferApplyRequest)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	var out port.TransferReceipt
	err = o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.authorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, &req.TargetFence); err != nil {
			return err
		}
		old, err := o.receiptTx(ctx, tx, req.TransferApplyRequest, "compensate", req.OperationID, want)
		if err == nil {
			out = old
			return nil
		}
		if !errors.Is(err, port.ErrTransferNotReady) {
			return err
		}
		if err := o.verifyRows(ctx, tx, req.TransferApplyRequest, req.Applied, "imported", uuid.Nil); err != nil {
			return err
		}
		out, err = durableReceipt(req.TransferApplyRequest)
		if err != nil {
			return err
		}
		out.OperationID, out.ReasonRef, out.CompensatesRef = req.OperationID, req.ReasonRef, req.Applied.Ref
		out.SourceLogicalRoot = req.Applied.LogicalRoot
		mapping := map[port.TransferKey]uuid.UUID{}
		for _, m := range append(append([]port.TransferMapping{}, req.Mappings...), req.DependencyMappings...) {
			mapping[m.Key] = m.TargetRef
		}
		logicalHeaders := []port.TransferHeader{}
		for _, row := range payload {
			target := mapping[row.Header.Key]
			var parent *uuid.UUID
			if row.Value.Parent != nil {
				ref := mapping[*row.Value.Parent]
				parent = &ref
			}
			row.Value.Active = false
			item, err := o.appendEvent(ctx, tx, req.TransferApplyRequest, row, &target, parent, req.OperationID, "compensated")
			if err != nil {
				return err
			}
			out.Items = append(out.Items, item)
			h := row.Header
			h.LogicalHash = item.LogicalHash
			logicalHeaders = append(logicalHeaders, h)
		}
		out.LogicalRoot, err = port.TransferLogicalRoot(logicalHeaders)
		if err != nil {
			return err
		}
		return o.insertReceipt(ctx, tx, req.TransferApplyRequest, "compensate", req.OperationID, want, out)
	})
	if err == nil && o.crashCompensation {
		o.crashCompensation = false
		return port.TransferReceipt{}, errDurableOwnerInjected
	}
	return out, err
}
func (o *durableOwner) verifyCompensation(ctx context.Context, req port.TransferCompensationRequest, out port.TransferReceipt) error {
	want, err := durableHash(req)
	if err != nil {
		return err
	}
	return o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.authorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, &req.TargetFence); err != nil {
			return err
		}
		stored, err := o.receiptTx(ctx, tx, req.TransferApplyRequest, "compensate", req.OperationID, want)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(stored, out) || out.OperationID != req.OperationID || out.ReasonRef != req.ReasonRef || out.CompensatesRef != req.Applied.Ref || out.SourceLogicalRoot != req.Applied.LogicalRoot {
			return port.ErrTransferConflict
		}
		return o.verifyRows(ctx, tx, req.TransferApplyRequest, out, "compensated", req.OperationID)
	})
}
func (o *durableOwner) adapter(namespace string) port.TransferAdapter {
	a := port.TransferAdapter{Module: "pf/forms", Namespace: namespace, Version: durableOwnerVersion, MaxBatch: 1}
	if namespace == durableChildNS {
		a.DependsOn = []string{durableParentNS}
	}
	a.Authority, a.RecoveryAuthority = o.authority, o.authority
	a.FenceSource, a.FenceTarget = o.fence, o.fence
	a.CheckSourceFence, a.CheckTargetFence = o.checkFence, o.checkFence
	a.Scan, a.Export, a.ReadExport = o.scan, o.export, o.readExport
	a.Apply, a.ReadReceipt, a.Verify = o.apply, o.readReceipt, o.verify
	a.Compensate, a.ReadCompensation, a.VerifyCompensation = o.compensate, o.readCompensation, o.verifyCompensation
	// Fence release is deliberately unavailable: these tests do not claim cutover.
	a.ReleaseSourceFence = func(context.Context, port.TransferRecoveryRequest, port.TransferFenceRequest, port.TransferFence) (port.TransferFenceRelease, error) {
		return port.TransferFenceRelease{}, port.ErrTransferNotReady
	}
	a.ReleaseTargetFence = a.ReleaseSourceFence
	a.ReadSourceFenceRelease = port.TransferFenceReleaseReader(a.ReleaseSourceFence)
	a.ReadTargetFenceRelease = port.TransferFenceReleaseReader(a.ReleaseSourceFence)
	return a
}
func (o *durableOwner) facade(t *testing.T, issuer *authztest.FakeIssuer) port.TenantTransfers {
	t.Helper()
	r, err := port.NewTransferRegistry([]port.TransferAdapter{o.adapter(durableParentNS), o.adapter(durableChildNS)}, issuer.Verifier(), nil)
	durableRequire(t, err)
	return party.New(o.pool, clock.System{}, party.Deps{Transfers: r}).Transfers()
}
func newDurableOwner(t *testing.T) (*durableOwner, *authztest.FakeIssuer) {
	t.Helper()
	env := pgtest.Get(t)
	durableOwnerDDLOnce.Do(func() {
		_, durableOwnerDDLErr = env.Pool(t, migrations.PF, dbadmin.Migrator).Exec(context.Background(), durableOwnerDDL)
	})
	durableRequire(t, durableOwnerDDLErr)
	issuer := authztest.New(t)
	st, ss, tt, ts := ids.New(), ids.New(), ids.New(), ids.New()
	o := &durableOwner{pool: env.Pool(t, migrations.PF, dbadmin.AppPlatform), pair: transferProtocolPair(t, issuer, st, ss, tt, ts)}
	for _, ctx := range []context.Context{o.pair.Source(), o.pair.Target()} {
		durableRequire(t, o.tx(ctx, func(tx pgx.Tx) error {
			p, _ := authz.FromContext(ctx)
			for _, ns := range []string{durableParentNS, durableChildNS} {
				if _, err := tx.Exec(ctx, `INSERT INTO pf_forms.transfer55_fixture_authority(tenant_id,scope_ref,namespace,actor_ref,approval_ref,epoch,valid_until,enabled)VALUES($1,$2,$3,$4,$5,'fixture-current-1',$6,true)`, p.TenantID, p.OrgRef, ns, p.ActorRef, ids.New(), time.Now().Add(time.Hour)); err != nil {
					return err
				}
			}
			return nil
		}))
	}
	parent, child := ids.New(), ids.New()
	durableRequire(t, o.tx(o.pair.Source(), func(tx pgx.Tx) error {
		p, _ := authz.FromContext(o.pair.Source())
		for i, key := range []port.TransferKey{{Namespace: durableParentNS, Ref: parent}, {Namespace: durableChildNS, Ref: child}} {
			var parentRef *uuid.UUID
			parentNS := ""
			if i == 1 {
				parentRef = &parent
				parentNS = durableParentNS
			}
			_, err := tx.Exec(o.pair.Source(), `INSERT INTO pf_forms.transfer55_fixture_event(id,tenant_id,scope_ref,stream_key,entity_ref,namespace,action,value,parent_namespace,parent_ref,source_tenant,source_ref,source_row_hash,source_seq,operation_id,event_time,actor_ref,idem_key,rule_version)VALUES($1,$2,$3,$4,$5,$6,'source',$7,$8,$9,$2,$5,'',0,$10,now(),$11,$12,'fixture-v1')`, ids.New(), p.TenantID, p.OrgRef, "fixture:"+key.Ref.String(), key.Ref, key.Namespace, fmt.Sprintf("synthetic-value-%d", i), parentNS, parentRef, uuid.Nil, p.ActorRef, "seed:"+key.Ref.String())
			if err != nil {
				return err
			}
		}
		return nil
	}))
	return o, issuer
}
func durableAcceptedPlan(t *testing.T, o *durableOwner, issuer *authztest.FakeIssuer) (port.TenantTransfers, port.TransferPlan) {
	t.Helper()
	svc := o.facade(t, issuer)
	sp, _ := authz.FromContext(o.pair.Source())
	tp, _ := authz.FromContext(o.pair.Target())
	intent, err := svc.TargetIntent(o.pair, port.TransferIntentInput{OperationID: ids.New(), SourceScopeRef: sp.OrgRef, TargetScopeRef: tp.OrgRef, Namespaces: []string{durableParentNS, durableChildNS}})
	durableRequire(t, err)
	_, err = svc.FreezeTarget(o.pair, port.TransferRef{ID: intent.ID})
	durableRequire(t, err)
	plan, err := svc.Propose(o.pair, port.TransferRef{ID: intent.ID})
	durableRequire(t, err)
	ref := port.TransferRef{ID: plan.ID}
	_, err = svc.FreezeSource(o.pair, ref)
	durableRequire(t, err)
	for i := 0; i < 5; i++ {
		_, err = svc.ScanNext(o.pair, ref)
		durableRequire(t, err)
	}
	_, err = svc.Seal(o.pair, ref)
	durableRequire(t, err)
	plan, err = svc.Accept(o.pair, ref)
	durableRequire(t, err)
	return svc, plan
}
func durableReserveExport(t *testing.T, o *durableOwner, svc port.TenantTransfers, plan port.TransferPlan) port.TransferBatch {
	t.Helper()
	batch, err := svc.ReserveBatch(o.pair, port.TransferRef{ID: plan.ID}, 1)
	durableRequire(t, err)
	_, err = svc.ExportBatch(o.pair, port.TransferBatchRef{PlanID: plan.ID, BatchID: batch.ID})
	durableRequire(t, err)
	return batch
}
func (o *durableOwner) count(ctx context.Context, action string) (int, error) {
	var n int
	err := o.tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pf_forms.transfer55_fixture_event WHERE action=$1`, action).Scan(&n)
	})
	return n, err
}
func durableAssertCount(t *testing.T, o *durableOwner, action string, want int) {
	t.Helper()
	n, err := o.count(o.pair.Target(), action)
	durableRequire(t, err)
	if n != want {
		t.Fatalf("owner %s rows=%d, want %d", action, n, want)
	}
}
func durableAssertCoreReceiptCount(t *testing.T, o *durableOwner, batch port.TransferBatch, operation string, want int) {
	t.Helper()
	var n int
	durableRequire(t, db.WithContextTx(o.pair.Target(), o.pool, db.Module("pf/party"), func(tx pgx.Tx) error {
		return tx.QueryRow(o.pair.Target(), `SELECT count(*) FROM pf_party.transfer_receipt WHERE plan_id=$1 AND batch_id=$2 AND operation=$3`, batch.PlanID, batch.ID, operation).Scan(&n)
	}))
	if n != want {
		t.Fatalf("core %s checkpoints=%d, want %d", operation, n, want)
	}
}

func TestTenantTransferDurableOwnerCommitCrashResumeAndDependencies(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc, plan := durableAcceptedPlan(t, o, issuer)
	batch := durableReserveExport(t, o, svc, plan)
	if batch.Namespace != durableParentNS {
		t.Fatal("dependency predecessor was not reserved first")
	}
	o.failAfterCommit = true
	ref := port.TransferBatchRef{PlanID: plan.ID, BatchID: batch.ID}
	if _, err := svc.ApplyBatch(o.pair, ref); err == nil {
		t.Fatal("injected owner commit crash was ignored")
	}
	durableAssertCount(t, o, "imported", 1)
	durableAssertCoreReceiptCount(t, o, batch, "apply", 0)
	// Discard both the facade and owner instance: no in-memory receipt is retained.
	restarted := &durableOwner{pool: o.pool, pair: o.pair}
	svc = restarted.facade(t, issuer)
	receipt, err := svc.ApplyBatch(o.pair, ref)
	durableRequire(t, err)
	again, err := svc.ApplyBatch(o.pair, ref)
	durableRequire(t, err)
	if !reflect.DeepEqual(receipt, again) {
		t.Fatal("durable apply replay changed receipt")
	}
	durableAssertCount(t, o, "imported", 1)
	durableAssertCoreReceiptCount(t, o, batch, "apply", 1)
	child := durableReserveExport(t, o, svc, plan)
	if child.Namespace != durableChildNS {
		t.Fatal("child did not become eligible after verified predecessor")
	}
	childReceipt, err := svc.ApplyBatch(o.pair, port.TransferBatchRef{PlanID: plan.ID, BatchID: child.ID})
	durableRequire(t, err)
	if len(childReceipt.Items) != 1 {
		t.Fatal("dependency mapping inflated primary receipt count")
	}
	durableAssertCount(t, o, "imported", 2)
	report, err := svc.Verify(o.pair, port.TransferRef{ID: plan.ID})
	durableRequire(t, err)
	if report.Count != 2 || report.Provisional {
		t.Fatal("verified durable report does not cover both source keys")
	}
	// Re-reading the source proves no source ledger was reparented or rewritten.
	n, err := o.count(o.pair.Source(), "source")
	durableRequire(t, err)
	if n != 2 {
		t.Fatal("source evidence changed")
	}
	n, err = o.count(o.pair.Source(), "imported")
	durableRequire(t, err)
	if n != 0 {
		t.Fatal("source participant read target ledger rows")
	}
	n, err = o.count(o.pair.Target(), "source")
	durableRequire(t, err)
	if n != 0 {
		t.Fatal("target participant read source ledger rows")
	}
}

func TestTenantTransferDurableOwnerRollbackBeforeReceipt(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc, plan := durableAcceptedPlan(t, o, issuer)
	batch := durableReserveExport(t, o, svc, plan)
	o.failBeforeReceipt = true
	ref := port.TransferBatchRef{PlanID: plan.ID, BatchID: batch.ID}
	if _, err := svc.ApplyBatch(o.pair, ref); err == nil {
		t.Fatal("owner transaction failure was ignored")
	}
	durableAssertCount(t, o, "imported", 0)
	durableAssertCoreReceiptCount(t, o, batch, "apply", 0)
	var provenance, receipts int
	durableRequire(t, o.tx(o.pair.Target(), func(tx pgx.Tx) error {
		return tx.QueryRow(o.pair.Target(), `SELECT (SELECT count(*) FROM pf_forms.transfer55_fixture_provenance),(SELECT count(*) FROM pf_forms.transfer55_fixture_receipt)`).Scan(&provenance, &receipts)
	}))
	if provenance != 0 || receipts != 0 {
		t.Fatal("owner rollback left provenance or receipt")
	}
	o.failBeforeReceipt = false
	_, err := svc.ApplyBatch(o.pair, ref)
	durableRequire(t, err)
	durableAssertCount(t, o, "imported", 1)
}

func TestTenantTransferDurableOwnerMalformedReceiptDoesNotCheckpoint(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc, plan := durableAcceptedPlan(t, o, issuer)
	batch := durableReserveExport(t, o, svc, plan)
	o.malformedRead = true
	ref := port.TransferBatchRef{PlanID: plan.ID, BatchID: batch.ID}
	if _, err := svc.ApplyBatch(o.pair, ref); err == nil {
		t.Fatal("malformed durable owner receipt was accepted")
	}
	durableAssertCoreReceiptCount(t, o, batch, "apply", 0)
	durableAssertCount(t, o, "imported", 1)
	o.malformedRead = false
	_, err := o.facade(t, issuer).ApplyBatch(o.pair, ref)
	durableRequire(t, err)
	durableAssertCount(t, o, "imported", 1)
}

func TestTenantTransferDurableOwnerRechecksAuthorityAndFenceInsideApply(t *testing.T) {
	for _, change := range []string{"authority", "fence"} {
		t.Run(change, func(t *testing.T) {
			o, issuer := newDurableOwner(t)
			svc, plan := durableAcceptedPlan(t, o, issuer)
			batch := durableReserveExport(t, o, svc, plan)
			o.beforeApply = func() error {
				return o.tx(o.pair.Target(), func(tx pgx.Tx) error {
					if change == "authority" {
						_, err := tx.Exec(o.pair.Target(), `UPDATE pf_forms.transfer55_fixture_authority SET enabled=false WHERE namespace=$1`, batch.Namespace)
						return err
					}
					var raw []byte
					if err := tx.QueryRow(o.pair.Target(), `SELECT document FROM pf_forms.transfer55_fixture_fence WHERE namespace=$1 AND side='target'`, batch.Namespace).Scan(&raw); err != nil {
						return err
					}
					var f port.TransferFence
					if err := json.Unmarshal(raw, &f); err != nil {
						return err
					}
					f.Ref = ids.New()
					f.Epoch = "replaced-current-fence"
					doc, err := durableJSON(f)
					if err != nil {
						return err
					}
					_, err = tx.Exec(o.pair.Target(), `UPDATE pf_forms.transfer55_fixture_fence SET document=$1 WHERE namespace=$2 AND side='target'`, doc, batch.Namespace)
					return err
				})
			}
			if _, err := svc.ApplyBatch(o.pair, port.TransferBatchRef{PlanID: plan.ID, BatchID: batch.ID}); err == nil {
				t.Fatal("owner applied after authority/fence changed")
			}
			durableAssertCount(t, o, "imported", 0)
			durableAssertCoreReceiptCount(t, o, batch, "apply", 0)
		})
	}
}

func TestTenantTransferDurableOwnerCompensationCommitCrashResumesSameOperation(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc, plan := durableAcceptedPlan(t, o, issuer)
	batch := durableReserveExport(t, o, svc, plan)
	ref := port.TransferBatchRef{PlanID: plan.ID, BatchID: batch.ID}
	applied, err := svc.ApplyBatch(o.pair, ref)
	durableRequire(t, err)
	reason := ids.New()
	o.crashCompensation = true
	if _, err := svc.Compensate(o.pair, ref, reason); err == nil {
		t.Fatal("compensation commit crash was ignored")
	}
	durableAssertCount(t, o, "compensated", 1)
	durableAssertCoreReceiptCount(t, o, batch, "compensate", 0)
	restarted := &durableOwner{pool: o.pool, pair: o.pair}
	svc = restarted.facade(t, issuer)
	compensated, err := svc.Compensate(o.pair, ref, reason)
	durableRequire(t, err)
	again, err := svc.Compensate(o.pair, ref, reason)
	durableRequire(t, err)
	if !reflect.DeepEqual(compensated, again) || compensated.OperationID == uuid.Nil || compensated.ReasonRef != reason || compensated.CompensatesRef != applied.Ref || compensated.SourceLogicalRoot != applied.LogicalRoot || compensated.LogicalRoot == applied.LogicalRoot {
		t.Fatal("compensation is not bound to the original operation and actual inactive state")
	}
	durableAssertCount(t, o, "imported", 1)
	durableAssertCount(t, o, "compensated", 1)
	durableAssertCoreReceiptCount(t, o, batch, "compensate", 1)
	var events, receipts, operations int
	durableRequire(t, o.tx(o.pair.Target(), func(tx pgx.Tx) error {
		return tx.QueryRow(o.pair.Target(), `SELECT (SELECT count(*) FROM pf_forms.transfer55_fixture_provenance WHERE operation_id=$1),(SELECT count(*) FROM pf_forms.transfer55_fixture_receipt WHERE phase='compensate'),(SELECT count(DISTINCT operation_id) FROM pf_forms.transfer55_fixture_receipt WHERE phase='compensate')`, compensated.OperationID).Scan(&events, &receipts, &operations)
	}))
	if events != 1 || receipts != 1 || operations != 1 {
		t.Fatal("compensation replay duplicated ledger/provenance/operation receipt")
	}
}
