package handler_test

// Test-only owner protocol evidence. These tables live only in pgtest's private
// database. This is not a production KN importer, producer fence, or cutover rehearsal.
// Stage-two contract dependencies: TransferLogicalRoot, VerifyCompensation and
// receipt OperationID/ReasonRef/CompensatesRef/SourceLogicalRoot and the separate
// ReadAppliedForRecovery contract. Normal and recovery authority have distinct
// persisted approvals; recovery never reuses normal source export authority.
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
 recovery_ref uuid NOT NULL, recovery_epoch text NOT NULL,
 recovery_valid_until timestamptz NOT NULL, recovery_enabled boolean NOT NULL,
 PRIMARY KEY(tenant_id,scope_ref,namespace)
);
CREATE TABLE pf_forms.transfer55_fixture_fence (
 tenant_id uuid NOT NULL, scope_ref uuid NOT NULL, namespace text NOT NULL,
 side text NOT NULL, holder uuid NOT NULL, document jsonb NOT NULL,
 active boolean NOT NULL, mode text NOT NULL DEFAULT 'forward',
 PRIMARY KEY(tenant_id,scope_ref,namespace,side)
);
CREATE TABLE pf_forms.transfer55_fixture_gate (
 tenant_id uuid NOT NULL, scope_ref uuid NOT NULL, namespace text NOT NULL,
 side text NOT NULL, holder uuid NOT NULL, operation_id uuid NOT NULL,
 reason_ref uuid NOT NULL, document jsonb NOT NULL,
 PRIMARY KEY(tenant_id,holder,namespace,side)
);
CREATE TABLE pf_forms.transfer55_fixture_release (
 tenant_id uuid NOT NULL, scope_ref uuid NOT NULL, namespace text NOT NULL,
 side text NOT NULL, holder uuid NOT NULL, operation_id uuid NOT NULL,
 reason_ref uuid NOT NULL, document jsonb NOT NULL,
 PRIMARY KEY(tenant_id,holder,namespace,side)
);
DO $fixture$
DECLARE n text;
BEGIN
 FOREACH n IN ARRAY ARRAY['provenance','receipt','artifact','authority','fence','gate','release'] LOOP
  EXECUTE format('SELECT shared.apply_tenant_rls(%L::regclass)','pf_forms.transfer55_fixture_'||n);
  EXECUTE format('CREATE POLICY p_scope ON pf_forms.transfer55_fixture_%I AS RESTRICTIVE USING(scope_ref=k_ref.ctx_uuid(''app.node_id'')) WITH CHECK(scope_ref=k_ref.ctx_uuid(''app.node_id''))',n);
 END LOOP;
END $fixture$;
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON pf_forms.transfer55_fixture_provenance FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON pf_forms.transfer55_fixture_receipt FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON pf_forms.transfer55_fixture_artifact FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON pf_forms.transfer55_fixture_gate FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON pf_forms.transfer55_fixture_release FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
GRANT SELECT,INSERT ON pf_forms.transfer55_fixture_event,pf_forms.transfer55_fixture_provenance,
 pf_forms.transfer55_fixture_receipt,pf_forms.transfer55_fixture_artifact,
 pf_forms.transfer55_fixture_gate,pf_forms.transfer55_fixture_release TO r_forms;
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
	pool                    *pgxpool.Pool
	pair                    port.TransferParticipants
	missingExportRead       bool
	missingApplyRead        bool
	missingCompensationRead bool
	exportCalls             int
	compensationCalls       int
	// These are fault controls only. All authority, fences, source values, exports,
	// target values, provenance and receipts are durable PostgreSQL rows.
	beforeApply          func() error
	failBeforeReceipt    bool
	failAfterCommit      bool
	malformedRead        bool
	crashCompensation    bool
	verifyNotReady       bool
	applyCalls           int // Invocation spy only; never used as a receipt or authorization.
	recoveryApplyReads   int
	gateCalls            int
	releaseCalls         int
	denyRelease          bool
	hideGateReceipt      bool
	hideReleaseReceipt   bool
	hideFenceReceipt     bool
	fenceCalls           int
	failFenceAfterCommit bool
	beforeFenceLock      chan struct{}
	allowFenceLock       chan struct{}
	beforeApplyLock      chan struct{}
	allowApplyLock       chan struct{}
	insideApplyLock      chan struct{}
	allowApplyCommit     chan struct{}
	gateBackend          chan int32
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
func durableOwnerPause(ctx context.Context, entered chan struct{}, proceed chan struct{}) error {
	close(entered)
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case <-proceed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errDurableOwnerInjected
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
func (o *durableOwner) recoveryAuthorityTx(ctx context.Context, tx pgx.Tx, namespace string) (port.TransferApproval, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return port.TransferApproval{}, port.ErrTransferAuthority
	}
	var a port.TransferApproval
	var enabled bool
	err := tx.QueryRow(ctx, `SELECT recovery_ref,actor_ref,recovery_epoch,recovery_valid_until,recovery_enabled FROM pf_forms.transfer55_fixture_authority WHERE scope_ref=$1 AND namespace=$2 FOR SHARE`, p.OrgRef, namespace).Scan(&a.Ref, &a.ActorRef, &a.Epoch, &a.ValidUntil, &enabled)
	if err != nil {
		return a, err
	}
	if !enabled || a.ActorRef != p.ActorRef || !a.ValidUntil.After(time.Now()) {
		return a, port.ErrTransferAuthority
	}
	return a, nil
}
func (o *durableOwner) recoveryAuthority(ctx context.Context, req port.TransferAuthorityRequest) (port.TransferApproval, error) {
	if _, _, _, err := durableSide(ctx, req.Scope, req.Side); err != nil {
		return port.TransferApproval{}, err
	}
	var out port.TransferApproval
	err := o.tx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = o.recoveryAuthorityTx(ctx, tx, req.Namespace)
		return err
	})
	return out, err
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
	if err := o.fenceKeyLock(ctx, tx, req); err != nil {
		return port.TransferFence{}, err
	}
	if expected == nil {
		if _, err := o.authorityTx(ctx, tx, req.Namespace); err != nil {
			return port.TransferFence{}, err
		}
		// Historical same-holder tombstones forbid a late Freeze even after its
		// active barrier was closed. Other holders cannot pass an unreleased gate.
		var blocked bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pf_forms.transfer55_fixture_gate g WHERE g.scope_ref=$1 AND g.namespace=$2 AND g.side=$3 AND (g.holder=$4 OR NOT EXISTS(SELECT 1 FROM pf_forms.transfer55_fixture_release r WHERE r.tenant_id=g.tenant_id AND r.holder=g.holder AND r.namespace=g.namespace AND r.side=g.side)))`, node, req.Namespace, req.Side, holder).Scan(&blocked); err != nil {
			return port.TransferFence{}, err
		}
		if blocked {
			return port.TransferFence{}, port.ErrTransferConflict
		}
	}
	var raw []byte
	var currentHolder uuid.UUID
	var active bool
	// Every forward writer holds this exact exclusive row lock through its owner
	// commit. EnterRecoveryGate takes the same lock before acknowledging a gate.
	err = tx.QueryRow(ctx, `SELECT holder,document,active FROM pf_forms.transfer55_fixture_fence WHERE scope_ref=$1 AND namespace=$2 AND side=$3 FOR UPDATE`, node, req.Namespace, req.Side).Scan(&currentHolder, &raw, &active)
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
	o.fenceCalls++
	if o.beforeFenceLock != nil {
		entered, proceed := o.beforeFenceLock, o.allowFenceLock
		o.beforeFenceLock = nil
		if err := durableOwnerPause(ctx, entered, proceed); err != nil {
			return port.TransferFence{}, err
		}
	}
	var f port.TransferFence
	err := o.tx(ctx, func(tx pgx.Tx) error { var err error; f, err = o.fenceTx(ctx, tx, req, nil); return err })
	if err == nil && o.failFenceAfterCommit {
		o.failFenceAfterCommit = false
		return port.TransferFence{}, errDurableOwnerInjected
	}
	return f, err
}
func (o *durableOwner) fenceKeyLock(ctx context.Context, tx pgx.Tx, req port.TransferFenceRequest) error {
	tid, node, _, err := durableSide(ctx, req.Scope, req.Side)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "forms.transfer55.fixture.fence:"+tid.String()+":"+node.String()+":"+req.Namespace+":"+string(req.Side))
	return err
}
func (o *durableOwner) checkFence(ctx context.Context, req port.TransferFenceRequest, f port.TransferFence) error {
	return o.tx(ctx, func(tx pgx.Tx) error { _, err := o.fenceTx(ctx, tx, req, &f); return err })
}
func (o *durableOwner) checkModeTx(ctx context.Context, tx pgx.Tx, req port.TransferFenceRequest, want string) error {
	var mode string
	if err := tx.QueryRow(ctx, `SELECT mode FROM pf_forms.transfer55_fixture_fence WHERE namespace=$1 AND side=$2`, req.Namespace, req.Side).Scan(&mode); err != nil {
		return err
	}
	if mode != want {
		return port.ErrTransferConflict
	}
	return nil
}
func durableRecoveryBinding(recovery port.TransferRecoveryRequest, req port.TransferFenceRequest) error {
	a, b := recovery.Scope, req.Scope
	if recovery.OperationID == uuid.Nil || recovery.ReasonRef == uuid.Nil || a.BindingHash != b.BindingHash || a.IntentID != b.IntentID || a.SourceTenant != b.SourceTenant || a.SourceScopeRef != b.SourceScopeRef || a.TargetTenant != b.TargetTenant || a.TargetScopeRef != b.TargetScopeRef {
		return port.ErrTransferConflict
	}
	if req.Side == port.TransferSource && a.PlanID != b.PlanID {
		return port.ErrTransferConflict
	}
	return nil
}

// This deliberately keeps the original fence document/epoch immutable. Recovery
// mode and its receipt are separate evidence, under the same exclusive row lock.
func (o *durableOwner) currentFenceTx(ctx context.Context, tx pgx.Tx, req port.TransferFenceRequest, expected port.TransferFence) (uuid.UUID, string, bool, error) {
	_, node, holder, err := durableSide(ctx, req.Scope, req.Side)
	if err != nil {
		return uuid.Nil, "", false, err
	}
	if err := o.fenceKeyLock(ctx, tx, req); err != nil {
		return uuid.Nil, "", false, err
	}
	var actualHolder uuid.UUID
	var raw []byte
	var mode string
	var active bool
	err = tx.QueryRow(ctx, `SELECT holder,document,mode,active FROM pf_forms.transfer55_fixture_fence WHERE scope_ref=$1 AND namespace=$2 AND side=$3 FOR UPDATE`, node, req.Namespace, req.Side).Scan(&actualHolder, &raw, &mode, &active)
	if errors.Is(err, pgx.ErrNoRows) && expected == (port.TransferFence{}) {
		return holder, "absent", false, nil
	}
	if err != nil {
		return uuid.Nil, "", false, err
	}
	var f port.TransferFence
	if err := json.Unmarshal(raw, &f); err != nil {
		return uuid.Nil, "", false, err
	}
	if actualHolder != holder || f != expected {
		return uuid.Nil, "", false, port.ErrTransferConflict
	}
	return holder, mode, active, nil
}
func (o *durableOwner) readFenceForRecovery(ctx context.Context, recovery port.TransferRecoveryRequest, req port.TransferFenceRequest) (port.TransferFence, error) {
	if err := durableRecoveryBinding(recovery, req); err != nil {
		return port.TransferFence{}, err
	}
	var out port.TransferFence
	err := o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.recoveryAuthorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if o.gateBackend != nil && req.Side == port.TransferTarget && req.Namespace == durableParentNS {
			var pid int32
			if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			select {
			case o.gateBackend <- pid:
			default:
			}
		}
		if err := o.fenceKeyLock(ctx, tx, req); err != nil {
			return err
		}
		_, node, holder, err := durableSide(ctx, req.Scope, req.Side)
		if err != nil {
			return err
		}
		var actual uuid.UUID
		var raw []byte
		err = tx.QueryRow(ctx, `SELECT holder,document FROM pf_forms.transfer55_fixture_fence WHERE scope_ref=$1 AND namespace=$2 AND side=$3 FOR UPDATE`, node, req.Namespace, req.Side).Scan(&actual, &raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return port.ErrTransferNotReady
		}
		if err != nil {
			return err
		}
		if actual != holder {
			return port.ErrTransferConflict
		}
		if o.hideFenceReceipt {
			return port.ErrTransferNotReady
		}
		return json.Unmarshal(raw, &out)
	})
	return out, err
}
func (o *durableOwner) gateTx(ctx context.Context, tx pgx.Tx, recovery port.TransferRecoveryRequest, req port.TransferFenceRequest, f port.TransferFence) (port.TransferRecoveryGate, error) {
	holder, mode, active, err := o.currentFenceTx(ctx, tx, req, f)
	if err != nil {
		return port.TransferRecoveryGate{}, err
	}
	if (mode != "forward" && mode != "recovery" && mode != "released" && mode != "absent") || ((mode == "forward" || mode == "recovery") && !active) || ((mode == "released" || mode == "absent") && active) {
		return port.TransferRecoveryGate{}, port.ErrTransferConflict
	}
	var raw []byte
	var out port.TransferRecoveryGate
	err = tx.QueryRow(ctx, `SELECT document FROM pf_forms.transfer55_fixture_gate WHERE holder=$1 AND namespace=$2 AND side=$3`, holder, req.Namespace, req.Side).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, port.ErrTransferNotReady
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if mode == "forward" || out.OperationID != recovery.OperationID || out.ReasonRef != recovery.ReasonRef || out.IntentID != req.Scope.IntentID || out.PlanID != req.Scope.PlanID || out.BindingHash != req.Scope.BindingHash || out.Fence != f || out.Namespace != req.Namespace || out.Side != req.Side {
		return out, port.ErrTransferConflict
	}
	return out, nil
}
func (o *durableOwner) readRecoveryGate(ctx context.Context, recovery port.TransferRecoveryRequest, req port.TransferFenceRequest, f port.TransferFence) (port.TransferRecoveryGate, error) {
	if err := durableRecoveryBinding(recovery, req); err != nil {
		return port.TransferRecoveryGate{}, err
	}
	var out port.TransferRecoveryGate
	err := o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.recoveryAuthorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if o.hideGateReceipt {
			return port.ErrTransferNotReady
		}
		var err error
		out, err = o.gateTx(ctx, tx, recovery, req, f)
		return err
	})
	return out, err
}
func (o *durableOwner) enterRecoveryGate(ctx context.Context, recovery port.TransferRecoveryRequest, req port.TransferFenceRequest, f port.TransferFence) (port.TransferRecoveryGate, error) {
	o.gateCalls++
	if err := durableRecoveryBinding(recovery, req); err != nil {
		return port.TransferRecoveryGate{}, err
	}
	var out port.TransferRecoveryGate
	err := o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.recoveryAuthorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if o.gateBackend != nil && req.Side == port.TransferTarget && req.Namespace == durableParentNS {
			var pid int32
			if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			select {
			case o.gateBackend <- pid:
			default:
			}
		}
		holder, mode, active, err := o.currentFenceTx(ctx, tx, req, f)
		if err != nil {
			return err
		}
		if mode == "recovery" || mode == "released" {
			out, err = o.gateTx(ctx, tx, recovery, req, f)
			return err
		}
		if mode == "absent" {
			old, readErr := o.gateTx(ctx, tx, recovery, req, f)
			if readErr == nil {
				out = old
				return nil
			}
			if !errors.Is(readErr, port.ErrTransferNotReady) {
				return readErr
			}
		} else if mode != "forward" || !active {
			return port.ErrTransferConflict
		}
		out = port.TransferRecoveryGate{Ref: ids.New(), OperationID: recovery.OperationID, ReasonRef: recovery.ReasonRef, IntentID: req.Scope.IntentID, PlanID: req.Scope.PlanID, BindingHash: req.Scope.BindingHash, Fence: f, Side: req.Side, Namespace: req.Namespace}
		raw, err := durableJSON(out)
		if err != nil {
			return err
		}
		p, _ := authz.FromContext(ctx)
		if _, err := tx.Exec(ctx, `INSERT INTO pf_forms.transfer55_fixture_gate(tenant_id,scope_ref,namespace,side,holder,operation_id,reason_ref,document)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, p.TenantID, p.OrgRef, req.Namespace, req.Side, holder, recovery.OperationID, recovery.ReasonRef, raw); err != nil {
			return err
		}
		// No fake fence row is created for an absent acquisition. The immutable
		// gate is its tombstone, checked by every later same-holder Fence writer.
		if mode == "absent" {
			return nil
		}
		_, err = tx.Exec(ctx, `UPDATE pf_forms.transfer55_fixture_fence SET mode='recovery' WHERE namespace=$1 AND side=$2 AND holder=$3`, req.Namespace, req.Side, holder)
		return err
	})
	return out, err
}
func (o *durableOwner) releaseTx(ctx context.Context, tx pgx.Tx, recovery port.TransferRecoveryRequest, req port.TransferFenceRequest, f port.TransferFence) (port.TransferFenceRelease, error) {
	// A completed release is immutable historical evidence for its old holder.
	// Reading it must not inspect or close a later holder's current active fence.
	_, _, holder, err := durableSide(ctx, req.Scope, req.Side)
	if err != nil {
		return port.TransferFenceRelease{}, err
	}
	var raw []byte
	var out port.TransferFenceRelease
	err = tx.QueryRow(ctx, `SELECT document FROM pf_forms.transfer55_fixture_release WHERE holder=$1 AND namespace=$2 AND side=$3`, holder, req.Namespace, req.Side).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, port.ErrTransferNotReady
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.OperationID != recovery.OperationID || out.ReasonRef != recovery.ReasonRef || out.IntentID != req.Scope.IntentID || out.PlanID != req.Scope.PlanID || out.BindingHash != req.Scope.BindingHash || out.Fence != f || out.Namespace != req.Namespace || out.Side != req.Side {
		return out, port.ErrTransferConflict
	}
	return out, nil
}
func (o *durableOwner) readFenceRelease(ctx context.Context, recovery port.TransferRecoveryRequest, req port.TransferFenceRequest, f port.TransferFence) (port.TransferFenceRelease, error) {
	if err := durableRecoveryBinding(recovery, req); err != nil {
		return port.TransferFenceRelease{}, err
	}
	var out port.TransferFenceRelease
	err := o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.recoveryAuthorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if o.hideReleaseReceipt {
			return port.ErrTransferNotReady
		}
		var err error
		out, err = o.releaseTx(ctx, tx, recovery, req, f)
		return err
	})
	return out, err
}
func (o *durableOwner) releaseFence(ctx context.Context, recovery port.TransferRecoveryRequest, req port.TransferFenceRequest, f port.TransferFence) (port.TransferFenceRelease, error) {
	o.releaseCalls++
	if o.denyRelease {
		return port.TransferFenceRelease{}, errDurableOwnerInjected
	}
	if err := durableRecoveryBinding(recovery, req); err != nil {
		return port.TransferFenceRelease{}, err
	}
	var out port.TransferFenceRelease
	err := o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.recoveryAuthorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		holder, mode, active, err := o.currentFenceTx(ctx, tx, req, f)
		if err != nil {
			return err
		}
		if mode == "released" {
			out, err = o.releaseTx(ctx, tx, recovery, req, f)
			return err
		}
		if mode == "absent" {
			old, readErr := o.releaseTx(ctx, tx, recovery, req, f)
			if readErr == nil {
				out = old
				return nil
			}
			if !errors.Is(readErr, port.ErrTransferNotReady) {
				return readErr
			}
		} else if mode != "recovery" || !active {
			return port.ErrTransferConflict
		}
		// The phase operation differs from the permanent gate operation. Validate
		// the existing gate's holder/fence without pretending their op IDs match.
		var gateRaw []byte
		if err := tx.QueryRow(ctx, `SELECT document FROM pf_forms.transfer55_fixture_gate WHERE holder=$1 AND namespace=$2 AND side=$3`, holder, req.Namespace, req.Side).Scan(&gateRaw); err != nil {
			return err
		}
		var gate port.TransferRecoveryGate
		if err := json.Unmarshal(gateRaw, &gate); err != nil {
			return err
		}
		if gate.Fence != f || gate.IntentID != req.Scope.IntentID || gate.PlanID != req.Scope.PlanID || gate.BindingHash != req.Scope.BindingHash || gate.Namespace != req.Namespace || gate.Side != req.Side {
			return port.ErrTransferConflict
		}
		out = port.TransferFenceRelease{Ref: ids.New(), IntentID: req.Scope.IntentID, PlanID: req.Scope.PlanID, OperationID: recovery.OperationID, ReasonRef: recovery.ReasonRef, BindingHash: req.Scope.BindingHash, Fence: f, Namespace: req.Namespace, Side: req.Side}
		raw, err := durableJSON(out)
		if err != nil {
			return err
		}
		p, _ := authz.FromContext(ctx)
		if _, err := tx.Exec(ctx, `INSERT INTO pf_forms.transfer55_fixture_release(tenant_id,scope_ref,namespace,side,holder,operation_id,reason_ref,document)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, p.TenantID, p.OrgRef, req.Namespace, req.Side, holder, recovery.OperationID, recovery.ReasonRef, raw); err != nil {
			return err
		}
		if mode == "absent" {
			return nil
		}
		_, err = tx.Exec(ctx, `UPDATE pf_forms.transfer55_fixture_fence SET active=false,mode='released' WHERE namespace=$1 AND side=$2 AND holder=$3`, req.Namespace, req.Side, holder)
		return err
	})
	return out, err
}
func (o *durableOwner) scan(ctx context.Context, req port.TransferScanRequest) (port.TransferPage, error) {
	var out port.TransferPage
	err := o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.authorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if !req.DryRun {
			fr := port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferSource}
			if _, err := o.fenceTx(ctx, tx, fr, &req.Fence); err != nil {
				return err
			}
			if err := o.checkModeTx(ctx, tx, fr, "forward"); err != nil {
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
		if o.missingExportRead {
			return port.ErrTransferNotReady
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferSource}, &req.SourceFence); err != nil {
			return err
		}
		if err := o.checkModeTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferSource}, "forward"); err != nil {
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
	o.exportCalls++
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
		if err := o.checkModeTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferSource}, "forward"); err != nil {
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
		fr := port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferSource}
		if _, err := o.fenceTx(o.pair.Source(), tx, fr, &req.SourceFence); err != nil {
			return err
		}
		if err := o.checkModeTx(o.pair.Source(), tx, fr, "forward"); err != nil {
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
		if o.missingApplyRead {
			return port.ErrTransferNotReady
		}
		fr := port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}
		if _, err := o.fenceTx(ctx, tx, fr, &req.TargetFence); err != nil {
			return err
		}
		if err := o.checkModeTx(ctx, tx, fr, "forward"); err != nil {
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
	o.applyCalls++
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
	if o.beforeApplyLock != nil {
		entered, proceed := o.beforeApplyLock, o.allowApplyLock
		o.beforeApplyLock = nil
		if err := durableOwnerPause(ctx, entered, proceed); err != nil {
			return port.TransferReceipt{}, err
		}
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
		if err := o.checkModeTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, "forward"); err != nil {
			return err
		}
		if o.insideApplyLock != nil {
			entered, proceed := o.insideApplyLock, o.allowApplyCommit
			o.insideApplyLock = nil
			if err := durableOwnerPause(ctx, entered, proceed); err != nil {
				return err
			}
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
	return o.verifyRowsAt(ctx, tx, req, out, action, operation, true)
}
func (o *durableOwner) verifyRowsAt(ctx context.Context, tx pgx.Tx, req port.TransferApplyRequest, out port.TransferReceipt, action string, operation uuid.UUID, current bool) error {
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
		latest := ""
		if current {
			latest = ` AND e.seq=(SELECT max(current_event.seq) FROM pf_forms.transfer55_fixture_event current_event WHERE current_event.tenant_id=e.tenant_id AND current_event.entity_ref=e.entity_ref AND current_event.namespace=e.namespace)`
		}
		err := tx.QueryRow(ctx, `SELECT e.value,e.parent_namespace,e.parent_ref,encode(e.row_hash,'hex'),p.source_tenant,p.source_row_hash,p.source_seq FROM pf_forms.transfer55_fixture_event e JOIN pf_forms.transfer55_fixture_provenance p ON p.tenant_id=e.tenant_id AND p.event_ref=e.id WHERE p.plan_id=$1 AND p.batch_id=$2 AND p.operation_id=$3 AND p.namespace=$4 AND p.source_ref=$5 AND p.target_ref=$6 AND e.action=$7`+latest, req.Scope.PlanID, req.BatchID, operation, item.Key.Namespace, item.Key.Ref, item.TargetRef, action).Scan(&value, &parentNS, &parent, &rowHash, &source, &sourceHash, &sourceSeq)
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
	err = o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.authorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, &req.TargetFence); err != nil {
			return err
		}
		if err := o.checkModeTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, "forward"); err != nil {
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
	if err == nil && o.verifyNotReady {
		return port.ErrTransferNotReady
	}
	return err
}
func (o *durableOwner) readAppliedForRecovery(ctx context.Context, recovery port.TransferRecoveryRequest, req port.TransferApplyRequest) (port.TransferReceipt, error) {
	o.recoveryApplyReads++
	if recovery.Scope != req.Scope {
		return port.TransferReceipt{}, port.ErrTransferConflict
	}
	want, err := durableHash(req)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	var out port.TransferReceipt
	err = o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.recoveryAuthorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, &req.TargetFence); err != nil {
			return err
		}
		if err := o.checkModeTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, "recovery"); err != nil {
			return err
		}
		var err error
		out, err = o.receiptTx(ctx, tx, req, "apply", uuid.Nil, want)
		if err != nil {
			return err
		}
		// The original apply row remains valid evidence after compensation. It is
		// deliberately not required to be the latest active business state here.
		return o.verifyRowsAt(ctx, tx, req, out, "imported", uuid.Nil, false)
	})
	return out, err
}

// Recovery reconstructs its typed payload from the immutable imported target
// ledger and provenance, under current recovery rights in the same owner tx.
// It does not call normal source readExport/payload or require normal authority.
func (o *durableOwner) recoveryPayloadTx(ctx context.Context, tx pgx.Tx, req port.TransferApplyRequest) ([]durableOwnerExportRow, error) {
	out := []durableOwnerExportRow{}
	for _, h := range req.Headers {
		var value, parentNS string
		var parent *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT e.value,e.parent_namespace,e.parent_ref FROM pf_forms.transfer55_fixture_event e JOIN pf_forms.transfer55_fixture_provenance p ON p.tenant_id=e.tenant_id AND p.event_ref=e.id WHERE p.plan_id=$1 AND p.batch_id=$2 AND p.namespace=$3 AND p.source_ref=$4 AND p.operation_id=$5 AND e.action='imported'`, req.Scope.PlanID, req.BatchID, h.Key.Namespace, h.Key.Ref, uuid.Nil).Scan(&value, &parentNS, &parent); err != nil {
			return nil, err
		}
		v := durableOwnerValue{Value: value, Active: true}
		if parent != nil {
			key := port.TransferKey{Namespace: parentNS}
			if err := tx.QueryRow(ctx, `SELECT source_ref FROM pf_forms.transfer55_fixture_provenance WHERE target_ref=$1 AND namespace=$2 AND source_tenant=$3 AND operation_id=$4`, *parent, parentNS, req.Scope.SourceTenant, uuid.Nil).Scan(&key.Ref); err != nil {
				return nil, err
			}
			v.Parent = &key
		}
		out = append(out, durableOwnerExportRow{Header: h, Value: v})
	}
	return out, nil
}
func (o *durableOwner) readCompensation(ctx context.Context, req port.TransferCompensationRequest) (port.TransferReceipt, error) {
	want, err := durableHash(req)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	var out port.TransferReceipt
	err = o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.recoveryAuthorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if o.missingCompensationRead {
			return port.ErrTransferNotReady
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, &req.TargetFence); err != nil {
			return err
		}
		if err := o.checkModeTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, "recovery"); err != nil {
			return err
		}
		var err error
		out, err = o.receiptTx(ctx, tx, req.TransferApplyRequest, "compensate", req.OperationID, want)
		return err
	})
	return out, err
}
func (o *durableOwner) compensate(ctx context.Context, req port.TransferCompensationRequest) (port.TransferReceipt, error) {
	o.compensationCalls++
	want, err := durableHash(req)
	if err != nil {
		return port.TransferReceipt{}, err
	}
	var out port.TransferReceipt
	err = o.tx(ctx, func(tx pgx.Tx) error {
		if _, err := o.recoveryAuthorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, &req.TargetFence); err != nil {
			return err
		}
		if err := o.checkModeTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, "recovery"); err != nil {
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
		payload, err := o.recoveryPayloadTx(ctx, tx, req.TransferApplyRequest)
		if err != nil {
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
		if _, err := o.recoveryAuthorityTx(ctx, tx, req.Namespace); err != nil {
			return err
		}
		if _, err := o.fenceTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, &req.TargetFence); err != nil {
			return err
		}
		if err := o.checkModeTx(ctx, tx, port.TransferFenceRequest{Scope: req.Scope, Namespace: req.Namespace, Side: port.TransferTarget}, "recovery"); err != nil {
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
	a.Authority, a.RecoveryAuthority = o.authority, o.recoveryAuthority
	a.ReadAppliedForRecovery = o.readAppliedForRecovery
	a.EnterRecoveryGate, a.ReadRecoveryGate = o.enterRecoveryGate, o.readRecoveryGate
	a.ReadFenceForRecovery = o.readFenceForRecovery
	a.FenceSource, a.FenceTarget = o.fence, o.fence
	a.CheckSourceFence, a.CheckTargetFence = o.checkFence, o.checkFence
	a.Scan, a.Export, a.ReadExport = o.scan, o.export, o.readExport
	a.Apply, a.ReadReceipt, a.Verify = o.apply, o.readReceipt, o.verify
	a.Compensate, a.ReadCompensation, a.VerifyCompensation = o.compensate, o.readCompensation, o.verifyCompensation
	// Exact owner release is available, but there is no identity rollback proof.
	// Applied-data release must remain closed even after successful compensation.
	a.ReleaseSourceFence, a.ReleaseTargetFence = o.releaseFence, o.releaseFence
	a.ReadSourceFenceRelease, a.ReadTargetFenceRelease = o.readFenceRelease, o.readFenceRelease
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
				if _, err := tx.Exec(ctx, `INSERT INTO pf_forms.transfer55_fixture_authority(tenant_id,scope_ref,namespace,actor_ref,approval_ref,epoch,valid_until,enabled,recovery_ref,recovery_epoch,recovery_valid_until,recovery_enabled)VALUES($1,$2,$3,$4,$5,'fixture-current-1',$6,true,$7,'fixture-recovery-1',$6,true)`, p.TenantID, p.OrgRef, ns, p.ActorRef, ids.New(), time.Now().Add(time.Hour), ids.New()); err != nil {
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
	// Successful data compensation is not an identity rollback attestation.
	// The facade has no RecoveryCheck, so applied-data release must stay closed.
	if _, err := svc.ReleaseFences(o.pair, port.TransferRef{ID: plan.ID}, ids.New()); err == nil {
		t.Fatal("applied-data release succeeded without identity rollback proof")
	}
	durableAssertTargetFence(t, o, batch.Namespace, true, "recovery")
}

func TestTenantTransferDurableOwnerRecoveryAfterNormalAuthorityRevoked(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc, plan := durableAcceptedPlan(t, o, issuer)
	batch := durableReserveExport(t, o, svc, plan)
	ref := port.TransferBatchRef{PlanID: plan.ID, BatchID: batch.ID}
	applied, err := svc.ApplyBatch(o.pair, ref)
	durableRequire(t, err)
	for _, ctx := range []context.Context{o.pair.Source(), o.pair.Target()} {
		durableRequire(t, o.tx(ctx, func(tx pgx.Tx) error {
			// Fresh, separate recovery approval persists after normal transfer
			// authority is revoked. No old approval is relabeled as recovery.
			_, err := tx.Exec(ctx, `UPDATE pf_forms.transfer55_fixture_authority SET enabled=false,recovery_ref=$1,recovery_epoch='fresh-recovery-2',recovery_valid_until=$2`, ids.New(), time.Now().Add(time.Hour))
			return err
		}))
	}
	beforeCalls := o.applyCalls
	if _, err := svc.ApplyBatch(o.pair, ref); err == nil {
		t.Fatal("forward apply replay survived revoked normal authority")
	}
	if o.applyCalls != beforeCalls {
		t.Fatal("revoked forward replay called the owner writer")
	}
	reason := ids.New()
	compensated, err := svc.Compensate(o.pair, ref, reason)
	durableRequire(t, err)
	if compensated.CompensatesRef != applied.Ref || compensated.SourceLogicalRoot != applied.LogicalRoot || compensated.LogicalRoot == applied.LogicalRoot {
		t.Fatal("fresh recovery did not compensate actual imported state")
	}
	// Recovery reads the original immutable apply receipt after the latest row
	// is already inactive; normal Verify must not be used for this read.
	restarted := &durableOwner{pool: o.pool, pair: o.pair}
	again, err := restarted.facade(t, issuer).Compensate(o.pair, ref, reason)
	durableRequire(t, err)
	if !reflect.DeepEqual(compensated, again) {
		t.Fatal("recovery replay changed its durable operation")
	}
	durableAssertCount(t, o, "imported", 1)
	durableAssertCount(t, o, "compensated", 1)
	for _, ctx := range []context.Context{o.pair.Source(), o.pair.Target()} {
		durableRequire(t, o.tx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE pf_forms.transfer55_fixture_authority SET recovery_enabled=false`)
			return err
		}))
	}
	if _, err := restarted.facade(t, issuer).Compensate(o.pair, ref, reason); err == nil {
		t.Fatal("recovery replay ignored revoked recovery authority")
	}
	durableAssertCount(t, o, "compensated", 1)
}

func TestTenantTransferDurableOwnerVerifyNotReadyNeverReapplies(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc, plan := durableAcceptedPlan(t, o, issuer)
	batch := durableReserveExport(t, o, svc, plan)
	ref := port.TransferBatchRef{PlanID: plan.ID, BatchID: batch.ID}
	o.failAfterCommit = true
	if _, err := svc.ApplyBatch(o.pair, ref); err == nil {
		t.Fatal("injected post-commit interruption was ignored")
	}
	if o.applyCalls != 1 {
		t.Fatal("fixture did not perform exactly one owner write")
	}
	durableAssertCoreReceiptCount(t, o, batch, "apply", 0)
	// ReadReceipt now succeeds with a valid durable receipt. Verify deliberately
	// returns ErrNotReady only after checking that receipt and actual target rows.
	// This is not a missing receipt and must never enter the Apply path again.
	o.verifyNotReady = true
	if _, err := o.facade(t, issuer).ApplyBatch(o.pair, ref); err == nil {
		t.Fatal("owner Verify ErrNotReady was ignored")
	}
	if o.applyCalls != 1 {
		t.Fatal("valid receipt plus Verify ErrNotReady called Apply again")
	}
	durableAssertCount(t, o, "imported", 1)
	durableAssertCoreReceiptCount(t, o, batch, "apply", 0)
	o.verifyNotReady = false
	_, err := o.facade(t, issuer).ApplyBatch(o.pair, ref)
	durableRequire(t, err)
	if o.applyCalls != 1 {
		t.Fatal("receipt recovery retried the owner writer")
	}
	durableAssertCount(t, o, "imported", 1)
	durableAssertCoreReceiptCount(t, o, batch, "apply", 1)
}

func durableAssertTargetFence(t *testing.T, o *durableOwner, namespace string, wantActive bool, wantMode string) {
	t.Helper()
	var active bool
	var mode string
	durableRequire(t, o.tx(o.pair.Target(), func(tx pgx.Tx) error {
		return tx.QueryRow(o.pair.Target(), `SELECT active,mode FROM pf_forms.transfer55_fixture_fence WHERE namespace=$1 AND side='target'`, namespace).Scan(&active, &mode)
	}))
	if active != wantActive || mode != wantMode {
		t.Fatalf("target fence active=%t mode=%s, want active=%t mode=%s", active, mode, wantActive, wantMode)
	}
}
func durableRevokeNormal(t *testing.T, o *durableOwner) {
	t.Helper()
	for _, ctx := range []context.Context{o.pair.Source(), o.pair.Target()} {
		durableRequire(t, o.tx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE pf_forms.transfer55_fixture_authority SET enabled=false`)
			return err
		}))
	}
}
func durableTargetOnly(t *testing.T, o *durableOwner, issuer *authztest.FakeIssuer) (port.TenantTransfers, port.TransferIntent) {
	t.Helper()
	svc := o.facade(t, issuer)
	sp, _ := authz.FromContext(o.pair.Source())
	tp, _ := authz.FromContext(o.pair.Target())
	intent, err := svc.TargetIntent(o.pair, port.TransferIntentInput{OperationID: ids.New(), SourceScopeRef: sp.OrgRef, TargetScopeRef: tp.OrgRef, Namespaces: []string{durableParentNS, durableChildNS}})
	durableRequire(t, err)
	intent, err = svc.FreezeTarget(o.pair, port.TransferRef{ID: intent.ID})
	durableRequire(t, err)
	if intent.Scope.PlanID != uuid.Nil || len(intent.TargetFences) != 2 {
		t.Fatal("target-only preparation synthesized a source plan")
	}
	return svc, intent
}
func durableReleaseTargetOnly(t *testing.T, o *durableOwner, issuer *authztest.FakeIssuer) (port.TenantTransfers, port.TransferIntent, port.TransferPlan, uuid.UUID) {
	t.Helper()
	svc, intent := durableTargetOnly(t, o, issuer)
	durableRevokeNormal(t, o)
	ref := port.TransferRef{ID: intent.ID}
	aborted, err := svc.Abort(o.pair, ref, ids.New())
	durableRequire(t, err)
	if aborted.Status != "aborted" {
		t.Fatal("target-only abort did not persist aborted status")
	}
	durableAssertTargetFence(t, o, durableParentNS, true, "recovery")
	reason := ids.New()
	released, err := svc.ReleaseFences(o.pair, ref, reason)
	durableRequire(t, err)
	if released.Status != "released" || released.ID != aborted.ID {
		t.Fatal("target-only release changed its recovery association")
	}
	return svc, intent, released, reason
}
func TestTenantTransferDurableOwnerTargetOnlyAbortReleaseBeforeProposal(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc, intent, released, reason := durableReleaseTargetOnly(t, o, issuer)
	gateCalls, releaseCalls := o.gateCalls, o.releaseCalls
	if gateCalls != 4 || releaseCalls != 4 {
		t.Fatal("target-only recovery did not gate/release every source and target namespace, including absent acquisitions")
	}
	replayed, err := svc.ReleaseFences(o.pair, port.TransferRef{ID: intent.ID}, reason)
	durableRequire(t, err)
	if replayed.ID != released.ID || replayed.Status != "released" || o.gateCalls != gateCalls || o.releaseCalls != releaseCalls {
		t.Fatal("target-only release replay rewrote a gate/release")
	}
	durableAssertCount(t, o, "imported", 0)
	durableAssertTargetFence(t, o, durableParentNS, false, "released")
	durableAssertTargetFence(t, o, durableChildNS, false, "released")
	var bindings int
	durableRequire(t, db.WithContextTx(o.pair.Target(), o.pool, db.Module("pf/party"), func(tx pgx.Tx) error {
		return tx.QueryRow(o.pair.Target(), `SELECT count(*) FROM pf_party.transfer_binding`).Scan(&bindings)
	}))
	if bindings != 0 {
		t.Fatal("target-only recovery fabricated an accepted transfer binding")
	}
	durableRequire(t, o.tx(o.pair.Target(), func(tx pgx.Tx) error {
		rows, err := tx.Query(o.pair.Target(), `SELECT g.document,r.document FROM pf_forms.transfer55_fixture_gate g JOIN pf_forms.transfer55_fixture_release r ON r.tenant_id=g.tenant_id AND r.holder=g.holder AND r.namespace=g.namespace AND r.side=g.side`)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var gr, rr []byte
			if err := rows.Scan(&gr, &rr); err != nil {
				return err
			}
			var gate port.TransferRecoveryGate
			var release port.TransferFenceRelease
			if err := json.Unmarshal(gr, &gate); err != nil {
				return err
			}
			if err := json.Unmarshal(rr, &release); err != nil {
				return err
			}
			if gate.PlanID != uuid.Nil || release.PlanID != uuid.Nil || gate.IntentID != intent.ID || release.IntentID != intent.ID || gate.Fence != intent.TargetFences[gate.Namespace] || release.Fence != gate.Fence || gate.OperationID == release.OperationID || gate.ReasonRef == release.ReasonRef {
				return port.ErrTransferConflict
			}
			count++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count != 2 {
			return port.ErrTransferConflict
		}
		return nil
	}))
}

func TestTenantTransferDurableOwnerMissingFenceGateOrReleaseWitnessNeverRewrites(t *testing.T) {
	for _, missing := range []string{"fence", "gate", "release"} {
		t.Run(missing, func(t *testing.T) {
			o, issuer := newDurableOwner(t)
			var svc port.TenantTransfers
			var intent port.TransferIntent
			var reason uuid.UUID
			if missing == "release" {
				svc, intent, _, reason = durableReleaseTargetOnly(t, o, issuer)
			} else {
				// Fence/gate witnesses are required before release approval. After a
				// completed release, only its bound historical release proof is needed.
				svc, intent = durableTargetOnly(t, o, issuer)
				durableRevokeNormal(t, o)
				_, err := svc.Abort(o.pair, port.TransferRef{ID: intent.ID}, ids.New())
				durableRequire(t, err)
				reason = ids.New()
				durableAssertTargetFence(t, o, durableParentNS, true, "recovery")
			}
			gateCalls, releaseCalls, fenceCalls := o.gateCalls, o.releaseCalls, o.fenceCalls
			if missing == "fence" {
				o.hideFenceReceipt = true
			} else if missing == "gate" {
				o.hideGateReceipt = true
			} else {
				o.hideReleaseReceipt = true
			}
			if _, err := svc.ReleaseFences(o.pair, port.TransferRef{ID: intent.ID}, reason); err == nil {
				t.Fatal("core witness overrode missing durable owner witness")
			}
			if o.gateCalls != gateCalls || o.releaseCalls != releaseCalls || o.fenceCalls != fenceCalls {
				t.Fatal("missing owner witness invoked a fence/gate/release writer again")
			}
			if missing == "release" {
				durableAssertTargetFence(t, o, durableParentNS, false, "released")
			} else {
				durableAssertTargetFence(t, o, durableParentNS, true, "recovery")
			}
		})
	}
}

func TestTenantTransferDurableOwnerOldReleaseCannotCloseReplacementHolder(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc, intent, released, reason := durableReleaseTargetOnly(t, o, issuer)
	gateCalls, releaseCalls, fenceCalls := o.gateCalls, o.releaseCalls, o.fenceCalls
	newHolder := ids.New()
	replacement := intent.TargetFences[durableParentNS]
	replacement.Ref = ids.New()
	replacement.Epoch = newHolder.String()
	doc, err := durableJSON(replacement)
	durableRequire(t, err)
	// Adversarial current-state replacement represents a later owner holder.
	// No new accepted core binding or identity proof is fabricated by this test.
	durableRequire(t, o.tx(o.pair.Target(), func(tx pgx.Tx) error {
		_, err := tx.Exec(o.pair.Target(), `UPDATE pf_forms.transfer55_fixture_fence SET holder=$1,document=$2,active=true,mode='forward' WHERE namespace=$3 AND side='target'`, newHolder, doc, durableParentNS)
		return err
	}))
	again, err := svc.ReleaseFences(o.pair, port.TransferRef{ID: intent.ID}, reason)
	durableRequire(t, err)
	if again.ID != released.ID || again.Status != "released" {
		t.Fatal("completed release replay lost its immutable historical result")
	}
	if o.gateCalls != gateCalls || o.releaseCalls != releaseCalls || o.fenceCalls != fenceCalls {
		t.Fatal("old recovery invoked a writer against the replacement holder")
	}
	durableAssertTargetFence(t, o, durableParentNS, true, "forward")
	var current uuid.UUID
	durableRequire(t, o.tx(o.pair.Target(), func(tx pgx.Tx) error {
		return tx.QueryRow(o.pair.Target(), `SELECT holder FROM pf_forms.transfer55_fixture_fence WHERE namespace=$1 AND side='target'`, durableParentNS).Scan(&current)
	}))
	if current != newHolder {
		t.Fatal("replacement holder was changed by old recovery")
	}
}

func durableWaitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-ch:
		return
	case <-timer.C:
		t.Fatal("owner coordination signal was not reached")
	}
}
func durableWaitError(t *testing.T, ch <-chan error) error {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case err := <-ch:
		return err
	case <-timer.C:
		t.Fatal("owner operation did not finish")
		return nil
	}
}
func durableWaitBlockingBackend(t *testing.T, pid int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	for {
		var blocked bool
		durableRequire(t, admin.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1))>0`, pid).Scan(&blocked))
		if blocked {
			return
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			t.Fatal("recovery gate did not wait for the actual owner fence lock")
		}
	}
}
func TestTenantTransferDurableOwnerRecoveryGateDrainsInFlightApply(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc, plan := durableAcceptedPlan(t, o, issuer)
	batch := durableReserveExport(t, o, svc, plan)
	entered, commit := make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(commit) }) }
	defer resume()
	o.insideApplyLock, o.allowApplyCommit = entered, commit
	o.gateBackend = make(chan int32, 1)
	o.failAfterCommit = true
	applyDone := make(chan error, 1)
	go func() {
		_, err := svc.ApplyBatch(o.pair, port.TransferBatchRef{PlanID: plan.ID, BatchID: batch.ID})
		applyDone <- err
	}()
	durableWaitSignal(t, entered)
	abortDone := make(chan error, 1)
	go func() { _, err := svc.Abort(o.pair, port.TransferRef{ID: plan.ID}, ids.New()); abortDone <- err }()
	var pid int32
	timer := time.NewTimer(10 * time.Second)
	select {
	case pid = <-o.gateBackend:
		timer.Stop()
	case <-timer.C:
		t.Fatal("target recovery gate was not attempted")
	}
	durableWaitBlockingBackend(t, pid)
	select {
	case err := <-abortDone:
		t.Fatalf("abort acknowledged before owner commit drained: %v", err)
	default:
	}
	resume()
	if err := durableWaitError(t, applyDone); err == nil {
		t.Fatal("injected post-owner-commit interruption was ignored")
	}
	// The now-visible imported owner receipt requires compensation. Abort must
	// return NotReady and keep every producer fence in recovery mode.
	if err := durableWaitError(t, abortDone); !errors.Is(err, port.ErrTransferNotReady) {
		t.Fatalf("abort must remain pending compensation after the owner commit: %v", err)
	}
	durableAssertCount(t, o, "imported", 1)
	durableAssertCoreReceiptCount(t, o, batch, "apply", 0)
	var receipts int
	durableRequire(t, o.tx(o.pair.Target(), func(tx pgx.Tx) error {
		return tx.QueryRow(o.pair.Target(), `SELECT count(*) FROM pf_forms.transfer55_fixture_receipt WHERE phase='apply'`).Scan(&receipts)
	}))
	if receipts != 1 {
		t.Fatal("drained owner commit was lost")
	}
	if _, err := svc.ReleaseFences(o.pair, port.TransferRef{ID: plan.ID}, ids.New()); err == nil {
		t.Fatal("release ignored owner commit without core apply checkpoint and identity rollback proof")
	}
	if o.recoveryApplyReads == 0 {
		t.Fatal("recovery never probed the durable owner apply receipt")
	}
	durableAssertTargetFence(t, o, durableParentNS, true, "recovery")
}

func TestTenantTransferDurableOwnerLateApplyDeniedAfterRecoveryGate(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc, plan := durableAcceptedPlan(t, o, issuer)
	batch := durableReserveExport(t, o, svc, plan)
	entered, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(proceed) }) }
	defer resume()
	o.beforeApplyLock, o.allowApplyLock = entered, proceed
	done := make(chan error, 1)
	go func() {
		_, err := svc.ApplyBatch(o.pair, port.TransferBatchRef{PlanID: plan.ID, BatchID: batch.ID})
		done <- err
	}()
	durableWaitSignal(t, entered)
	aborted, err := svc.Abort(o.pair, port.TransferRef{ID: plan.ID}, ids.New())
	durableRequire(t, err)
	if aborted.Status != "aborted" {
		t.Fatal("unapplied recovery did not persist aborted status")
	}
	durableAssertTargetFence(t, o, durableParentNS, true, "recovery")
	resume()
	if err := durableWaitError(t, done); err == nil {
		t.Fatal("late owner writer passed the recovery-mode fence")
	}
	durableAssertCount(t, o, "imported", 0)
	durableAssertCoreReceiptCount(t, o, batch, "apply", 0)
}

func TestTenantTransferDurableOwnerReverseDependencySeesUncheckpointedChild(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc, plan := durableAcceptedPlan(t, o, issuer)
	parent := durableReserveExport(t, o, svc, plan)
	parentRef := port.TransferBatchRef{PlanID: plan.ID, BatchID: parent.ID}
	_, err := svc.ApplyBatch(o.pair, parentRef)
	durableRequire(t, err)
	child := durableReserveExport(t, o, svc, plan)
	childRef := port.TransferBatchRef{PlanID: plan.ID, BatchID: child.ID}
	if child.Namespace != durableChildNS {
		t.Fatal("expected the dependent child batch")
	}
	o.failAfterCommit = true
	if _, err := svc.ApplyBatch(o.pair, childRef); err == nil {
		t.Fatal("child owner commit interruption was ignored")
	}
	durableAssertCount(t, o, "imported", 2)
	durableAssertCoreReceiptCount(t, o, child, "apply", 0)
	parentReason := ids.New()
	if _, err := svc.Compensate(o.pair, parentRef, parentReason); err == nil {
		t.Fatal("predecessor compensation ignored an owner-committed child without a core checkpoint")
	}
	durableAssertCount(t, o, "compensated", 0)
	_, err = svc.Compensate(o.pair, childRef, ids.New())
	durableRequire(t, err)
	durableAssertCount(t, o, "compensated", 1)
	durableAssertCoreReceiptCount(t, o, child, "compensate", 1)
	_, err = svc.Compensate(o.pair, parentRef, parentReason)
	durableRequire(t, err)
	durableAssertCount(t, o, "compensated", 2)
	durableAssertCoreReceiptCount(t, o, parent, "compensate", 1)
	// Child recovery does not manufacture a normal apply checkpoint that was
	// never committed. The original receipt is consumed only as recovery evidence.
	durableAssertCoreReceiptCount(t, o, child, "apply", 0)
	if _, err := svc.ReleaseFences(o.pair, port.TransferRef{ID: plan.ID}, ids.New()); err == nil {
		t.Fatal("fully compensated data was treated as an identity rollback proof")
	}
	durableAssertTargetFence(t, o, durableParentNS, true, "recovery")
	durableAssertTargetFence(t, o, durableChildNS, true, "recovery")
}

func TestTenantTransferDurableOwnerRecoveryDiscoversUncheckpointedFreeze(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc := o.facade(t, issuer)
	sp, _ := authz.FromContext(o.pair.Source())
	tp, _ := authz.FromContext(o.pair.Target())
	intent, err := svc.TargetIntent(o.pair, port.TransferIntentInput{OperationID: ids.New(), SourceScopeRef: sp.OrgRef, TargetScopeRef: tp.OrgRef, Namespaces: []string{durableParentNS, durableChildNS}})
	durableRequire(t, err)
	o.failFenceAfterCommit = true
	if _, err := svc.FreezeTarget(o.pair, port.TransferRef{ID: intent.ID}); err == nil {
		t.Fatal("post-owner-freeze commit interruption was ignored")
	}
	var original port.TransferFence
	durableRequire(t, o.tx(o.pair.Target(), func(tx pgx.Tx) error {
		var raw []byte
		var holder uuid.UUID
		if err := tx.QueryRow(o.pair.Target(), `SELECT holder,document FROM pf_forms.transfer55_fixture_fence WHERE namespace=$1 AND side='target'`, durableParentNS).Scan(&holder, &raw); err != nil {
			return err
		}
		if holder != intent.ID {
			return port.ErrTransferConflict
		}
		return json.Unmarshal(raw, &original)
	}))
	var state struct {
		Intent port.TransferIntent `json:"intent"`
	}
	durableRequire(t, db.WithContextTx(o.pair.Target(), o.pool, db.Module("pf/party"), func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(o.pair.Target(), `SELECT document FROM pf_party.transfer_event WHERE entity_id=$1 ORDER BY seq DESC LIMIT 1`, intent.ID).Scan(&raw); err != nil {
			return err
		}
		return json.Unmarshal(raw, &state)
	}))
	if len(state.Intent.TargetFences) != 0 {
		t.Fatal("fixture did not stop before the core fence checkpoint")
	}
	if o.fenceCalls != 1 {
		t.Fatal("fixture unexpectedly acquired another owner fence")
	}
	durableRevokeNormal(t, o)
	ref := port.TransferRef{ID: intent.ID}
	_, err = svc.Abort(o.pair, ref, ids.New())
	durableRequire(t, err)
	released, err := svc.ReleaseFences(o.pair, ref, ids.New())
	durableRequire(t, err)
	if released.Status != "released" || o.fenceCalls != 1 {
		t.Fatal("recovery reacquired a normal fence instead of discovering its durable owner commit")
	}
	durableAssertTargetFence(t, o, durableParentNS, false, "released")
	durableRequire(t, o.tx(o.pair.Target(), func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(o.pair.Target(), `SELECT document FROM pf_forms.transfer55_fixture_release WHERE namespace=$1 AND side='target'`, durableParentNS).Scan(&raw); err != nil {
			return err
		}
		var release port.TransferFenceRelease
		if err := json.Unmarshal(raw, &release); err != nil {
			return err
		}
		if release.Fence != original || release.IntentID != intent.ID || release.PlanID != uuid.Nil {
			return port.ErrTransferConflict
		}
		return nil
	}))
	durableAssertCount(t, o, "imported", 0)
}

func TestTenantTransferDurableOwnerAbsentGateBlocksLateFreezeAfterRelease(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc := o.facade(t, issuer)
	sp, _ := authz.FromContext(o.pair.Source())
	tp, _ := authz.FromContext(o.pair.Target())
	intent, err := svc.TargetIntent(o.pair, port.TransferIntentInput{OperationID: ids.New(), SourceScopeRef: sp.OrgRef, TargetScopeRef: tp.OrgRef, Namespaces: []string{durableParentNS, durableChildNS}})
	durableRequire(t, err)
	entered, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(proceed) }) }
	defer resume()
	o.beforeFenceLock, o.allowFenceLock = entered, proceed
	done := make(chan error, 1)
	go func() { _, err := svc.FreezeTarget(o.pair, port.TransferRef{ID: intent.ID}); done <- err }()
	durableWaitSignal(t, entered)
	// Normal authority remains valid. Only the recovery tombstone under the
	// shared acquisition lock can reject this already-authorized late writer.
	_, err = svc.Abort(o.pair, port.TransferRef{ID: intent.ID}, ids.New())
	durableRequire(t, err)
	released, err := svc.ReleaseFences(o.pair, port.TransferRef{ID: intent.ID}, ids.New())
	durableRequire(t, err)
	if released.Status != "released" {
		t.Fatal("unapplied absent-fence recovery did not release")
	}
	resume()
	if err := durableWaitError(t, done); err == nil {
		t.Fatal("late Freeze bypassed the historical same-holder tombstone after release")
	}
	for _, ctx := range []context.Context{o.pair.Source(), o.pair.Target()} {
		durableRequire(t, o.tx(ctx, func(tx pgx.Tx) error {
			var fences, gates, releases int
			if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM pf_forms.transfer55_fixture_fence),(SELECT count(*) FROM pf_forms.transfer55_fixture_gate),(SELECT count(*) FROM pf_forms.transfer55_fixture_release)`).Scan(&fences, &gates, &releases); err != nil {
				return err
			}
			if fences != 0 || gates != 2 || releases != 2 {
				return port.ErrTransferConflict
			}
			return nil
		}))
	}
	if o.fenceCalls != 1 {
		t.Fatal("coordinator reacquired another normal fence during absent recovery")
	}
	durableAssertCount(t, o, "imported", 0)
}

// Preserve the original stage-two witness regression. The new fence/gate/release
// scenarios supplement this coverage; they do not replace any owner data witness.
func TestTenantTransferDurableOwnerMissingWitnessNeverRepeatsWriter(t *testing.T) {
	o, issuer := newDurableOwner(t)
	svc, plan := durableAcceptedPlan(t, o, issuer)
	batch := durableReserveExport(t, o, svc, plan)
	ref := port.TransferBatchRef{PlanID: plan.ID, BatchID: batch.ID}
	exportCalls := o.exportCalls
	o.missingExportRead = true
	if _, err := svc.ExportBatch(o.pair, ref); !errors.Is(err, port.ErrTransferNotReady) {
		t.Fatal("missing owner export after checkpoint was not denied", err)
	}
	if o.exportCalls != exportCalls {
		t.Fatal("missing export evidence invoked writer again")
	}
	o.missingExportRead = false
	_, err := svc.ApplyBatch(o.pair, ref)
	durableRequire(t, err)
	applyCalls := o.applyCalls
	o.missingApplyRead = true
	if _, err := svc.ApplyBatch(o.pair, ref); !errors.Is(err, port.ErrTransferNotReady) {
		t.Fatal("missing apply witness after checkpoint was not denied", err)
	}
	if o.applyCalls != applyCalls {
		t.Fatal("missing apply evidence invoked writer again")
	}
	o.missingApplyRead = false
	reason := ids.New()
	_, err = svc.Compensate(o.pair, ref, reason)
	durableRequire(t, err)
	compensationCalls := o.compensationCalls
	o.missingCompensationRead = true
	if _, err := svc.Compensate(o.pair, ref, reason); !errors.Is(err, port.ErrTransferNotReady) {
		t.Fatal("missing compensation evidence after checkpoint was not denied", err)
	}
	if o.compensationCalls != compensationCalls {
		t.Fatal("missing compensation evidence invoked writer again")
	}
	durableAssertCount(t, o, "imported", 1)
	durableAssertCount(t, o, "compensated", 1)
}

// Hide retained evidence with an additional test-only SELECT policy instead of
// mutating append-only history. Its predicate is limited to this test's plan.
func TestTenantTransferDurableOwnerMissingCoreRecoveryHistoryNeverCallsWriter(t *testing.T) {
	for _, phase := range []string{"pending-gate", "completed-release"} {
		t.Run(phase, func(t *testing.T) {
			o, issuer := newDurableOwner(t)
			svc, intent := durableTargetOnly(t, o, issuer)
			durableRevokeNormal(t, o)
			plan, err := svc.Abort(o.pair, port.TransferRef{ID: intent.ID}, ids.New())
			durableRequire(t, err)
			reason := ids.New()
			operation := "release_source"
			if phase == "pending-gate" {
				o.denyRelease = true
				operation = "gate_target"
				if _, err := svc.ReleaseFences(o.pair, port.TransferRef{ID: intent.ID}, reason); !errors.Is(err, port.ErrTransferDependency) {
					t.Fatal("release was not interrupted after approval", err)
				}
				o.denyRelease = false
			} else {
				_, err := svc.ReleaseFences(o.pair, port.TransferRef{ID: intent.ID}, reason)
				durableRequire(t, err)
			}
			admin := pgtest.Get(t).AdminPool(t, migrations.PF)
			policy := "p_transfer55_test_hidden_history"
			_, err = admin.Exec(context.Background(), fmt.Sprintf(`CREATE POLICY %s ON pf_party.transfer_receipt AS RESTRICTIVE FOR SELECT TO r_party USING(plan_id<>'%s'::uuid OR operation<>'%s')`, policy, plan.ID, operation))
			durableRequire(t, err)
			t.Cleanup(func() {
				_, err := admin.Exec(context.Background(), `DROP POLICY IF EXISTS p_transfer55_test_hidden_history ON pf_party.transfer_receipt`)
				if err != nil {
					t.Errorf("remove private test policy: %v", err)
				}
			})
			var visible, retained int
			durableRequire(t, admin.QueryRow(context.Background(), `SELECT count(*) FROM pf_party.transfer_receipt WHERE plan_id=$1 AND operation=$2`, plan.ID, operation).Scan(&retained))
			durableRequire(t, db.WithContextTx(o.pair.Target(), o.pool, db.Module("pf/party"), func(tx pgx.Tx) error {
				return tx.QueryRow(o.pair.Target(), `SELECT count(*) FROM pf_party.transfer_receipt WHERE plan_id=$1 AND operation=$2`, plan.ID, operation).Scan(&visible)
			}))
			if retained != 2 || visible != 0 {
				t.Fatal("test policy did not hide exactly the retained core witnesses", retained, visible)
			}
			gateCalls, releaseCalls, fenceCalls := o.gateCalls, o.releaseCalls, o.fenceCalls
			if _, err := svc.ReleaseFences(o.pair, port.TransferRef{ID: intent.ID}, reason); !errors.Is(err, port.ErrTransferConflict) {
				t.Fatal("missing core recovery history was accepted", err)
			}
			if o.gateCalls != gateCalls || o.releaseCalls != releaseCalls || o.fenceCalls != fenceCalls {
				t.Fatal("missing core history invoked an owner writer")
			}
		})
	}
}
