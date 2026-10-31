package port

import (
	"context"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
	"testing"
)

type operationTestSource struct{ calls int }

func (s *operationTestSource) VerifyKHTCOperation(context.Context, authz.Principal, SessionContext, KHTCOperationProof, forms.Operation) (bool, error) {
	s.calls++
	return true, nil
}
func TestW1168PrivateOperationContextCannotBeBorrowed(t *testing.T) {
	k := SessionContext{ID: uuid.New(), Role: SessionRoleAdmin, Scope: authz.Scope{All: true}}
	actor := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), MFA: true}
	op := forms.Operation{Action: forms.OperationPublishBatch, Profile: "kn_khtc", IdemKey: "private-native-key", PayloadHash: "canonical-batch"}
	proof := KHTCOperationProof{IntentID: uuid.New(), SessionID: k.ID, Expected: op}
	for _, mode := range []string{"valid", "no-marker", "session", "operation", "mfa", "readonly", "role", "principal"} {
		t.Run(mode, func(t *testing.T) {
			current, p, candidate, request := k, actor, proof, op
			switch mode {
			case "session":
				candidate.SessionID = uuid.New()
			case "operation":
				request.PayloadHash = "foreign payload"
			case "mfa":
				p.MFA = false
			case "readonly":
				current.ViewOnly = true
			case "role":
				current.Role = SessionRoleProvince
				current.Department = "ke_hoach_tai_chinh"
			}
			selected, e := NewSessionContext(current)
			if e != nil {
				t.Fatal(e)
			}
			p.SelectedSession = &selected
			ctx := authz.WithPrincipal(context.Background(), p)
			source := &operationTestSource{}
			if mode != "no-marker" {
				ctx = WithKHTCOperation(ctx, source, candidate)
			}
			if mode == "principal" {
				p.ActorRef = uuid.New()
			}
			grant, e := AuthorizeKHTCOperation(ctx, p, request)
			if mode == "valid" {
				if e != nil || !grant.Allowed || source.calls != 1 {
					t.Fatal("valid linkage denied", grant, e)
				}
				return
			}
			if grant.Allowed || source.calls != 0 {
				t.Fatal("untrusted context reached canonical source or authorized operation", grant, source.calls, e)
			}
		})
	}
}
