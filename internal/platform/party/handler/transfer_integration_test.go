package handler_test

// These tests exercise real r_party/RLS and generic protocol metadata. The fixture
// owner is deliberately not a production KN importer or a producer-fence rehearsal.
import (
	"context"
	"errors"
	"strconv"
	"strings"
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
	"github.com/jackc/pgx/v5/pgconn"
)

type transferProtocolOwner struct {
	mu         sync.Mutex
	headers    map[uuid.UUID][]port.TransferHeader
	approvals  map[string]port.TransferApproval
	holders    map[string]uuid.UUID
	fences     map[string]port.TransferFence
	revoked    bool
	omitLast   bool
	scanError  error
	emptyPages bool
	scanCalls  int
}

func newTransferProtocolOwner() *transferProtocolOwner {
	return &transferProtocolOwner{headers: map[uuid.UUID][]port.TransferHeader{}, approvals: map[string]port.TransferApproval{}, holders: map[string]uuid.UUID{}, fences: map[string]port.TransferFence{}}
}
func (o *transferProtocolOwner) adapter() port.TransferAdapter {
	a := port.TransferAdapter{Module: "pf/party", Namespace: "records", Version: "test-v1", MaxBatch: 1}
	a.Authority = func(ctx context.Context, req port.TransferAuthorityRequest) (port.TransferApproval, error) {
		o.mu.Lock()
		defer o.mu.Unlock()
		p, ok := authz.FromContext(ctx)
		tid, scope := req.Scope.SourceTenant, req.Scope.SourceScopeRef
		if req.Side == port.TransferTarget {
			tid, scope = req.Scope.TargetTenant, req.Scope.TargetScopeRef
		}
		if !ok || p.TenantID != tid || p.OrgRef != scope || o.revoked {
			return port.TransferApproval{}, port.ErrTransferAuthority
		}
		key := p.ActorRef.String() + ":" + scope.String()
		approval, ok := o.approvals[key]
		if !ok {
			approval = port.TransferApproval{Ref: ids.New(), ActorRef: p.ActorRef, Epoch: "test-current-1", ValidUntil: time.Now().Add(time.Hour)}
			o.approvals[key] = approval
		}
		return approval, nil
	}
	a.RecoveryAuthority = a.Authority
	fence := func(ctx context.Context, req port.TransferFenceRequest) (port.TransferFence, error) {
		o.mu.Lock()
		defer o.mu.Unlock()
		tid, scope, holder := req.Scope.SourceTenant, req.Scope.SourceScopeRef, req.Scope.PlanID
		if req.Side == port.TransferTarget {
			tid, scope, holder = req.Scope.TargetTenant, req.Scope.TargetScopeRef, req.Scope.IntentID
		}
		key := tid.String() + ":" + scope.String() + ":" + string(req.Side)
		if old, ok := o.holders[key]; ok && old != holder {
			return port.TransferFence{}, port.ErrTransferConflict
		}
		if f, ok := o.fences[key]; ok {
			return f, nil
		}
		headers := o.headers[scope]
		if req.Side == port.TransferTarget {
			headers = nil
		}
		root, err := port.TransferHeaderRoot(tid, scope, req.Namespace, headers)
		if err != nil {
			return port.TransferFence{}, err
		}
		f := port.TransferFence{Ref: ids.New(), Epoch: holder.String(), HighWatermark: "frozen-1", HeaderRoot: root, HeaderCount: int64(len(headers))}
		o.holders[key], o.fences[key] = holder, f
		return f, nil
	}
	check := func(ctx context.Context, req port.TransferFenceRequest, f port.TransferFence) error {
		current, err := fence(ctx, req)
		if err != nil {
			return err
		}
		if current != f {
			return port.ErrTransferConflict
		}
		return nil
	}
	a.FenceSource, a.FenceTarget = fence, fence
	a.CheckSourceFence, a.CheckTargetFence = check, check
	a.Scan = func(ctx context.Context, req port.TransferScanRequest) (port.TransferPage, error) {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.scanCalls++
		if o.scanError != nil {
			return port.TransferPage{}, o.scanError
		}
		if o.emptyPages {
			return port.TransferPage{Next: strconv.Itoa(o.scanCalls)}, nil
		}
		start := 0
		if req.Cursor != "" {
			var err error
			start, err = strconv.Atoi(req.Cursor)
			if err != nil {
				return port.TransferPage{}, err
			}
		}
		headers := o.headers[req.Scope.SourceScopeRef]
		if o.omitLast && len(headers) > 0 {
			headers = headers[:len(headers)-1]
		}
		if start > len(headers) {
			return port.TransferPage{}, port.ErrTransferConflict
		}
		end := min(start+req.Limit, len(headers))
		out := port.TransferPage{Items: append([]port.TransferHeader{}, headers[start:end]...), Complete: end == len(headers)}
		if !out.Complete {
			out.Next = strconv.Itoa(end)
		}
		return out, nil
	}
	a.Export = func(context.Context, port.TransferBatchRequest) (port.TransferArtifact, error) {
		return port.TransferArtifact{}, port.ErrTransferNotReady
	}
	a.ReadExport = port.TransferExportReader(a.Export)
	a.Apply = func(context.Context, port.TransferApplyRequest) (port.TransferReceipt, error) {
		return port.TransferReceipt{}, port.ErrTransferNotReady
	}
	a.ReadReceipt = port.TransferReceiptReader(a.Apply)
	a.Verify = func(context.Context, port.TransferApplyRequest, port.TransferReceipt) error {
		return port.ErrTransferNotReady
	}
	a.Compensate = func(context.Context, port.TransferCompensationRequest) (port.TransferReceipt, error) {
		return port.TransferReceipt{}, port.ErrTransferNotReady
	}
	a.ReadCompensation = port.TransferCompensationReader(a.Compensate)
	a.VerifyCompensation = func(context.Context, port.TransferCompensationRequest, port.TransferReceipt) error {
		return port.ErrTransferNotReady
	}
	a.ReleaseSourceFence = func(context.Context, port.TransferRecoveryRequest, port.TransferFenceRequest, port.TransferFence) (port.TransferFenceRelease, error) {
		return port.TransferFenceRelease{}, port.ErrTransferNotReady
	}
	a.ReleaseTargetFence = a.ReleaseSourceFence
	a.ReadSourceFenceRelease = port.TransferFenceReleaseReader(a.ReleaseSourceFence)
	a.ReadTargetFenceRelease = port.TransferFenceReleaseReader(a.ReleaseSourceFence)
	return a
}
func transferActualContext(t *testing.T, iss *authztest.FakeIssuer, tid, scope uuid.UUID) context.Context {
	t.Helper()
	raw := iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, OrgRef: scope, Roles: []string{authz.RolePlatformAdmin}, MFA: true})
	p, err := iss.Verifier().Verify(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return authz.WithRawToken(authz.WithPrincipal(tenant.WithID(context.Background(), tid), p), raw)
}
func transferProtocolService(t *testing.T, owner *transferProtocolOwner, iss *authztest.FakeIssuer) port.TenantTransfers {
	t.Helper()
	registry, err := port.NewTransferRegistry([]port.TransferAdapter{owner.adapter()}, iss.Verifier(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return party.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, party.Deps{Transfers: registry}).Transfers()
}
func transferProtocolPair(t *testing.T, iss *authztest.FakeIssuer, sourceTenant, sourceScope, targetTenant, targetScope uuid.UUID) port.TransferParticipants {
	t.Helper()
	p, err := port.NewTransferParticipants(transferActualContext(t, iss, sourceTenant, sourceScope), transferActualContext(t, iss, targetTenant, targetScope))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func transferProtocolPlan(t *testing.T, service port.TenantTransfers, p port.TransferParticipants, sourceScope, targetScope uuid.UUID) (port.TransferIntent, port.TransferPlan) {
	t.Helper()
	intent, err := service.TargetIntent(p, port.TransferIntentInput{OperationID: ids.New(), SourceScopeRef: sourceScope, TargetScopeRef: targetScope, Namespaces: []string{"records"}})
	if err != nil {
		t.Fatal(err)
	}
	// Target freeze deliberately precedes proposal and is resumed through a new facade.
	frozen, err := service.FreezeTarget(p, port.TransferRef{ID: intent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Scope.PlanID != uuid.Nil || len(frozen.TargetFences) != 1 {
		t.Fatal("intent holder changed before proposal")
	}
	plan, err := service.Propose(p, port.TransferRef{ID: intent.ID})
	if err != nil {
		t.Fatal(err)
	}
	again, err := service.Propose(p, port.TransferRef{ID: intent.ID})
	if err != nil || again.ID != plan.ID {
		t.Fatal("plan replay changed ID", err)
	}
	return frozen, plan
}
func transferProtocolSeal(t *testing.T, service port.TenantTransfers, p port.TransferParticipants, plan port.TransferPlan) port.TransferPlan {
	t.Helper()
	ref := port.TransferRef{ID: plan.ID}
	if _, err := service.FreezeSource(p, ref); err != nil {
		t.Fatal(err)
	}
	// Bound ensures a broken scanner cannot make the test loop forever.
	for i := 0; i < 5; i++ {
		if _, err := service.ScanNext(p, ref); err != nil {
			t.Fatal(err)
		}
	}
	sealed, err := service.Seal(p, ref)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}
func TestTenantTransferProtocolFreezeResumeSealAcceptAndRLS(t *testing.T) {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	owner := newTransferProtocolOwner()
	st, ss, tt, ts := ids.New(), ids.New(), ids.New(), ids.New()
	p := transferProtocolPair(t, iss, st, ss, tt, ts)
	digest := port.TransferDigest(strings.Repeat("a", 64))
	owner.headers[ss] = []port.TransferHeader{{Key: port.TransferKey{Namespace: "records", Ref: ids.New()}, SourceTenant: st, SourceScopeRef: ss, SourceRowHash: digest, LogicalHash: digest}, {Key: port.TransferKey{Namespace: "records", Ref: ids.New()}, SourceTenant: st, SourceScopeRef: ss, SourceRowHash: digest, LogicalHash: digest}}
	svc := transferProtocolService(t, owner, iss)
	intent, plan := transferProtocolPlan(t, svc, p, ss, ts)
	// Facade reconstruction uses durable party state, while the fixture owner retains its fences.
	resumed := transferProtocolService(t, owner, iss)
	frozen, err := resumed.FreezeTarget(p, port.TransferRef{ID: intent.ID})
	if err != nil || frozen.TargetFences["records"] != intent.TargetFences["records"] {
		t.Fatal("target fence replay changed", err)
	}
	sealed := transferProtocolSeal(t, resumed, p, plan)
	accepted, err := resumed.Accept(p, port.TransferRef{ID: plan.ID})
	if err != nil || accepted.Status != "accepted" || accepted.ManifestHash != sealed.ManifestHash {
		t.Fatal("accept", err)
	}
	replay, err := resumed.Accept(p, port.TransferRef{ID: plan.ID})
	if err != nil || replay.ID != accepted.ID {
		t.Fatal("accept replay", err)
	}
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	role := db.Module("pf/party")
	// Same tenant, other actual scope: metadata reads are hidden and relabelling cannot attach to plan A.
	otherScope := ids.New()
	other := transferActualContext(t, iss, st, otherScope)
	err = db.WithContextTx(other, pool, role, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(other, `SELECT count(*) FROM pf_party.transfer_plan WHERE id=$1`, plan.ID).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Fatal("foreign scope plan visible")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithContextTx(other, pool, role, func(tx pgx.Tx) error {
		_, err := tx.Exec(other, `INSERT INTO pf_party.transfer_item(tenant_id,scope_ref,plan_id,namespace,source_ref,document)VALUES($1,$2,$3,'records',$4,'{}')`, st, otherScope, plan.ID, ids.New())
		return err
	})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23503" {
		t.Fatal("scope relabel FK did not reject", err)
	}
	err = db.WithContextTx(other, pool, role, func(tx pgx.Tx) error {
		_, err := tx.Exec(other, `INSERT INTO pf_party.transfer_operation(tenant_id,scope_ref,entity_id,kind,semantic_key,operation_id,input_hash,document)VALUES($1,$2,$3,'release','same',$4,$5,'{}')`, st, otherScope, plan.ID, ids.New(), digest)
		return err
	})
	if !errors.As(err, &pg) || pg.Code != "23503" {
		t.Fatal("operation scope relabel did not reject", err)
	}
	err = db.WithContextTx(p.Target(), pool, role, func(tx pgx.Tx) error {
		_, err := tx.Exec(p.Target(), `INSERT INTO pf_party.transfer_entity(tenant_id,scope_ref,id,kind)VALUES($1,$2,$3,'plan')`, st, ss, ids.New())
		return err
	})
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Fatal("cross tenant insert accepted", err)
	}
	// Parent grants must not accidentally grant direct child partition access with only p_tenant.
	err = db.WithContextTx(p.Source(), pool, role, func(tx pgx.Tx) error {
		var unsafe int
		err := tx.QueryRow(p.Source(), `SELECT count(*) FROM pg_inherits i WHERE i.inhparent='pf_party.transfer_event'::regclass AND (has_table_privilege(current_user,i.inhrelid,'SELECT') OR has_table_privilege(current_user,i.inhrelid,'INSERT'))`).Scan(&unsafe)
		if err != nil {
			return err
		}
		if unsafe != 0 {
			t.Fatal("r_party has direct child partition rights without scope policy")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A revoke after the original approval must deny even a cached completed plan status.
	owner.mu.Lock()
	owner.revoked = true
	owner.mu.Unlock()
	if _, err := resumed.Status(p, port.TransferRef{ID: plan.ID}); !errors.Is(err, port.ErrTransferAuthority) {
		t.Fatal("revoked snapshot replay accepted", err)
	}
}
func TestTenantTransferProtocolMissingInventoryCannotSeal(t *testing.T) {
	pgtest.Get(t)
	iss := authztest.New(t)
	owner := newTransferProtocolOwner()
	st, ss, tt, ts := ids.New(), ids.New(), ids.New(), ids.New()
	p := transferProtocolPair(t, iss, st, ss, tt, ts)
	digest := port.TransferDigest(strings.Repeat("b", 64))
	owner.headers[ss] = []port.TransferHeader{{Key: port.TransferKey{Namespace: "records", Ref: ids.New()}, SourceTenant: st, SourceScopeRef: ss, SourceRowHash: digest, LogicalHash: digest}}
	svc := transferProtocolService(t, owner, iss)
	_, plan := transferProtocolPlan(t, svc, p, ss, ts)
	ref := port.TransferRef{ID: plan.ID}
	if _, err := svc.FreezeSource(p, ref); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.omitLast = true
	owner.mu.Unlock()
	if _, err := svc.ScanNext(p, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Seal(p, ref); !errors.Is(err, port.ErrTransferConflict) {
		t.Fatal("complete cursor with omitted header sealed", err)
	}
}

func TestTenantTransferProtocolOwnerErrorsAreOpaqueAndDryRunTerminates(t *testing.T) {
	pgtest.Get(t)
	iss := authztest.New(t)
	owner := newTransferProtocolOwner()
	st, ss, tt, ts := ids.New(), ids.New(), ids.New(), ids.New()
	p := transferProtocolPair(t, iss, st, ss, tt, ts)
	svc := transferProtocolService(t, owner, iss)
	_, plan := transferProtocolPlan(t, svc, p, ss, ts)
	ref := port.TransferRef{ID: plan.ID}
	owner.mu.Lock()
	owner.emptyPages = true
	owner.mu.Unlock()
	if _, err := svc.DryRun(p, ref); !errors.Is(err, port.ErrTransferConflict) {
		t.Fatal("empty advancing scanner was not rejected", err)
	}
	owner.mu.Lock()
	calls := owner.scanCalls
	owner.emptyPages = false
	owner.mu.Unlock()
	if calls != 1 {
		t.Fatal("empty scanner consumed unbounded callbacks", calls)
	}
	if _, err := svc.FreezeSource(p, ref); err != nil {
		t.Fatal(err)
	}
	marker := "private-owner-payload-do-not-echo"
	owner.mu.Lock()
	owner.scanError = errors.New(marker)
	owner.mu.Unlock()
	if _, err := svc.ScanNext(p, ref); !errors.Is(err, port.ErrTransferDependency) || strings.Contains(err.Error(), marker) {
		t.Fatal("owner error leaked or became absent receipt", err)
	}
	status, err := svc.Status(p, ref)
	if err != nil || status.Count != 0 || status.Status != "frozen" {
		t.Fatal("failed owner read advanced checkpoint", status, err)
	}
}
