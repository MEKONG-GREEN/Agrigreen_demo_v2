package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PublicationReceipts adds authenticated original creation evidence to the same
// native batch primitive. It never resolves unknown by calling a legacy writer.
type PublicationReceipts struct{ Svc *Service }

var _ port.PublishOperations = PublicationReceipts{}

type publishOperation224 struct {
	request           port.PublishOperationRequest
	digest, authority string
	actor             actor
	profile           port.Profile
	readOnly          bool
	receipt           port.PublishOperationReceipt
}

func unknownPublish224() port.PublishOperationReceipt {
	return port.PublishOperationReceipt{Outcome: port.PublishOperationUnknown}
}
func (c PublicationReceipts) prepare(ctx context.Context, in port.PublishOperationRequest) (*publishOperation224, error) {
	if c.Svc == nil || c.Svc.DB == nil {
		return nil, port.ErrPublishOperationUnavailable
	}
	if _, joined := db.JoinedTx(ctx); joined {
		return nil, port.ErrPublishOperationUnavailable
	}
	req, err := port.NormalizePublishOperation(in)
	if err != nil {
		return nil, err
	}
	policy := c.Svc.PublishOperationPolicies[req.Input.Profile]
	if policy == nil {
		return nil, port.ErrPublishOperationUnavailable
	}
	principal, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	if !principal.MFA || principal.ActorRef == uuid.Nil || principal.TenantID == uuid.Nil || principal.TenantID != req.TenantID {
		return nil, problem.Forbidden
	}
	prof, a, err := c.Svc.resolve(ctx, req.Input.Profile)
	if err != nil {
		return nil, err
	}
	policyRequest, err := port.NormalizePublishOperation(req)
	if err != nil {
		return nil, err
	}
	grant, err := policy(ctx, principal, policyRequest)
	if err != nil {
		return nil, err
	}
	if !grant.Allowed || !utf8.ValidString(grant.Binding) || strings.TrimSpace(grant.Binding) == "" || len(grant.Binding) > 512 || strings.ContainsRune(grant.Binding, 0) {
		return nil, problem.Forbidden
	}
	roles := slices.Clone(principal.Roles)
	slices.Sort(roles)
	units := slices.Clone(a.scope.Units)
	slices.Sort(units)
	raw, err := json.Marshal(struct {
		Actor, Tenant, Org, Node         uuid.UUID
		Roles, Units                     []string
		Role, Level, Department, Binding string
		All, MFA                         bool
	}{a.ref, a.tenant, principal.OrgRef, a.node, roles, units, a.role, a.level, a.dept, grant.Binding, a.scope.All, principal.MFA})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	digest, err := port.PublishOperationDigest(req)
	if err != nil {
		return nil, err
	}
	return &publishOperation224{request: req, digest: digest, authority: hex.EncodeToString(sum[:]), actor: a, profile: prof, receipt: unknownPublish224()}, nil
}
func publishIdem224(req port.PublishOperationRequest) string {
	return "forms.publish_operation:" + req.OperationID.String()
}
func (c PublicationReceipts) ExecutePublishOperation(ctx context.Context, in port.PublishOperationRequest) (port.PublishOperationReceipt, error) {
	return c.run(ctx, in, false)
}
func (c PublicationReceipts) ReadPublishOperation(ctx context.Context, in port.PublishOperationRequest) (port.PublishOperationReceipt, error) {
	return c.run(ctx, in, true)
}
func (c PublicationReceipts) run(ctx context.Context, in port.PublishOperationRequest, read bool) (port.PublishOperationReceipt, error) {
	op, err := c.prepare(ctx, in)
	if err != nil {
		return unknownPublish224(), err
	}
	op.readOnly = read
	_, err = Publisher(c).publishBatchCore(ctx, op.request.Input, publishIdem224(op.request), false, op)
	if err != nil {
		return unknownPublish224(), err
	} // Includes real COMMIT with lost ACK; no partial evidence.
	return op.receipt, nil
}

// before runs only after current native publication/template/target authorization.
// Existing anchor + any absent or inconsistent evidence stops all native writes.
func (op *publishOperation224) before(ctx context.Context, tx pgx.Tx) (bool, error) {
	if err := store.LockPublishOperation224(ctx, tx, op.actor.tenant, op.request.OperationID); err != nil {
		return false, err
	}
	key, err := store.PublishOperationByKey224(ctx, tx, op.actor.tenant, op.request.OperationID)
	if err != nil {
		return false, err
	}
	if key == nil {
		return op.readOnly, nil
	}
	if key.ActorRef != op.actor.ref || key.Profile != op.request.Input.Profile || key.Digest != op.digest || key.Authority != op.authority {
		return true, nil
	}
	var original port.PublishOperationRequest
	if json.Unmarshal(key.Command, &original) != nil {
		return true, nil
	}
	normalized, err := port.NormalizePublishOperation(original)
	if err != nil || !reflect.DeepEqual(normalized, op.request) {
		return true, nil
	}
	receipt, valid, err := store.PublishOperationReceipt224(ctx, tx, op.request, *key)
	if err != nil {
		return false, err
	}
	if receipt == nil || !valid || !op.valid(*receipt) {
		return true, nil
	}
	op.receipt = *receipt
	return true, nil
}
func (op *publishOperation224) valid(r port.PublishOperationReceipt) bool {
	req := op.request
	if r.OperationID != req.OperationID || r.TenantID != req.TenantID || r.Profile != req.Input.Profile || r.ActorRef != op.actor.ref || r.RequestDigest != op.digest || r.AuthorityHash != op.authority || r.Outcome != port.PublishOperationApplied || r.RecordedAt.IsZero() || len(r.Items) != len(req.Input.Periods) {
		return false
	}
	periods, events := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	for i, item := range r.Items {
		want := req.Input.Periods[i]
		if item.ID == uuid.Nil || periods[item.ID] || item.EventID == uuid.Nil || events[item.EventID] || item.EventRecordedAt.IsZero() || item.Key != want.Key || item.Label != want.Label || !item.DueAt.Equal(want.DueAt) || item.Applied.TenantID != req.TenantID || item.Applied.PeriodID != item.ID || item.Applied.Profile != req.Input.Profile || item.Applied.EventSeq < 1 || item.Applied.DueSeq != 0 {
			return false
		}
		periods[item.ID] = true
		events[item.EventID] = true
	}
	return true
}
func (op *publishOperation224) applied(ctx context.Context, tx pgx.Tx, items []port.PublishedCreation, at time.Time) error {
	req := op.request
	receipt := port.PublishOperationReceipt{OperationID: req.OperationID, TenantID: req.TenantID, Profile: req.Input.Profile, RequestDigest: op.digest, ActorRef: op.actor.ref, AuthorityHash: op.authority, Outcome: port.PublishOperationApplied, Items: items}
	if err := store.InsertPublishOperation224(ctx, tx, req, &receipt, at); err != nil {
		return err
	}
	if !op.valid(receipt) {
		return port.ErrPublishOperationUnavailable
	}
	op.receipt = receipt
	return nil
}
